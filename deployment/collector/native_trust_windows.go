//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"unsafe"
)

const (
	nativeCertStoreSystemW      = 10
	nativeCertStoreCurrentUser  = 0x00010000
	nativeCertStoreLocalMachine = 0x00020000
	nativeCertStoreOpenExisting = 0x00004000
	nativeCertStoreReadOnly     = 0x00008000
	nativeCryptNotFound         = syscall.Errno(0x80092004)
	nativeServerAuthOID         = "1.3.6.1.5.5.7.3.1"
)

var (
	nativeCertGetUsage = syscall.NewLazyDLL("crypt32.dll").NewProc("CertGetEnhancedKeyUsage")
	nativeReplaceFile  = syscall.NewLazyDLL("kernel32.dll").NewProc("ReplaceFileW")
)

// SSL_CERT_FILE replaces the client's normal roots. Export public ROOT
// certificates as well as the observer CA, without changing its signing identity.
// Stores are read only, and only roots usable for TLS server authentication are
// included. The bundle is a startup snapshot, not a Windows chain-policy engine.
func writeNativeTrustBundle(path string, observerCA []byte) error {
	roots, err := nativeWindowsRoots()
	if err != nil {
		return err
	}
	bundle, err := nativeTrustBundle(observerCA, roots)
	if err != nil {
		return err
	}
	return replaceNativeTrustFile(path, bundle)
}

func nativeWindowsRoots() ([][]byte, error) {
	var roots [][]byte
	for _, location := range []uint32{nativeCertStoreCurrentUser, nativeCertStoreLocalMachine} {
		certs, err := nativeRootsFromStore(location)
		if err != nil {
			return nil, err
		}
		roots = append(roots, certs...)
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("Windows ROOT stores contain no TLS server trust anchors")
	}
	return roots, nil
}

func nativeRootsFromStore(location uint32) ([][]byte, error) {
	name, _ := syscall.UTF16PtrFromString("ROOT")
	store, err := syscall.CertOpenStore(nativeCertStoreSystemW, 0, 0,
		location|nativeCertStoreOpenExisting|nativeCertStoreReadOnly, uintptr(unsafe.Pointer(name)))
	runtime.KeepAlive(name)
	if err != nil {
		return nil, fmt.Errorf("open Windows ROOT store 0x%x: %w", location, err)
	}
	defer syscall.CertCloseStore(store, 0)
	var roots [][]byte
	var ctx *syscall.CertContext
	for {
		// Enumeration frees the previous context, including on its final call.
		ctx, err = syscall.CertEnumCertificatesInStore(store, ctx)
		if err == nativeCryptNotFound || err == syscall.ERROR_NO_MORE_FILES {
			return roots, nil
		}
		if err != nil {
			return nil, fmt.Errorf("enumerate Windows ROOT store: %w", err)
		}
		allowed, usageErr := nativeCertificateAllowsServer(ctx)
		if usageErr != nil {
			syscall.CertFreeCertificateContext(ctx)
			return nil, usageErr
		}
		if allowed && ctx.EncodingType&1 != 0 {
			roots = append(roots, append([]byte(nil), unsafe.Slice(ctx.EncodedCert, ctx.Length)...))
		}
	}
}

func nativeCertificateAllowsServer(ctx *syscall.CertContext) (bool, error) {
	var size uint32
	ok, _, err := nativeCertGetUsage.Call(uintptr(unsafe.Pointer(ctx)), 0, 0, uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return false, fmt.Errorf("read ROOT certificate usage size: %w", err)
	}
	buffer := make([]byte, size)
	ok, _, err = nativeCertGetUsage.Call(uintptr(unsafe.Pointer(ctx)), 0,
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return false, fmt.Errorf("read ROOT certificate usage: %w", err)
	}
	usage := (*syscall.CertEnhKeyUsage)(unsafe.Pointer(&buffer[0]))
	if usage.Length == 0 {
		// Windows distinguishes unrestricted usage from an explicitly empty list
		// through GetLastError, even when CertGetEnhancedKeyUsage succeeds.
		return err == nativeCryptNotFound, nil
	}
	for _, oid := range unsafe.Slice(usage.UsageIdentifiers, usage.Length) {
		if nativeOIDEquals(oid, nativeServerAuthOID) || nativeOIDEquals(oid, "2.5.29.37.0") {
			return true, nil
		}
	}
	return false, nil
}

func nativeOIDEquals(oid *byte, expected string) bool {
	for i := range expected {
		if *(*byte)(unsafe.Add(unsafe.Pointer(oid), i)) != expected[i] {
			return false
		}
	}
	return *(*byte)(unsafe.Add(unsafe.Pointer(oid), len(expected))) == 0
}

func nativeTrustBundle(observerCA []byte, roots [][]byte) ([]byte, error) {
	seen := make(map[[32]byte]bool)
	var bundle bytes.Buffer
	add := func(der []byte) {
		digest := sha256.Sum256(der)
		if !seen[digest] {
			seen[digest] = true
			_ = pem.Encode(&bundle, &pem.Block{Type: "CERTIFICATE", Bytes: der})
		}
	}
	rest := bytes.TrimSpace(observerCA)
	for len(rest) > 0 {
		block, next := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("observer CA must contain public PEM certificates")
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return nil, fmt.Errorf("parse observer CA: %w", err)
		}
		add(block.Bytes)
		rest = bytes.TrimSpace(next)
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("observer CA is empty")
	}
	// Deterministic order keeps repeated initialization from rewriting the same
	// file merely because Windows enumerated its stores in a different order.
	ordered := make(map[string][]byte)
	for _, der := range roots {
		digest := sha256.Sum256(der)
		ordered[hex.EncodeToString(digest[:])] = der
	}
	keys := make([]string, 0, len(ordered))
	for key := range ordered {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		add(ordered[key])
	}
	return bundle.Bytes(), nil
}

func replaceNativeTrustFile(path string, bundle []byte) error {
	original, err := os.ReadFile(path)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if existed && bytes.Equal(original, bundle) {
		return nil
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".ca-*.pem")
	if err != nil {
		return err
	}
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(temp.Name())
		}
	}()
	if _, err = temp.Write(bundle); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if !existed {
		return os.Rename(temp.Name(), path)
	}
	// ReplaceFile preserves the existing file's ACL and lets concurrent readers
	// see a complete old/new bundle, rather than a truncated in-place write.
	to, _ := syscall.UTF16PtrFromString(path)
	from, _ := syscall.UTF16PtrFromString(temp.Name())
	backupPath := temp.Name() + ".previous"
	backup, _ := syscall.UTF16PtrFromString(backupPath)
	ok, _, replaceErr := nativeReplaceFile.Call(uintptr(unsafe.Pointer(to)), uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(backup)), 0, 0, 0)
	if ok == 0 {
		// ReplaceFile can move the old file aside before returning an error.
		// Restore that exact file if the destination disappeared.
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			if restoreErr := os.Rename(backupPath, path); restoreErr != nil {
				cleanupTemp = false
				return fmt.Errorf("replace native trust bundle: %v; restore: %v (backup %s)", replaceErr, restoreErr, backupPath)
			}
		}
		return fmt.Errorf("replace native trust bundle: %w", replaceErr)
	}
	_ = os.Remove(backupPath)
	return nil
}
