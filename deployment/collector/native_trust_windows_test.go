//go:build windows

package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func nativeTrustTestCA(t *testing.T, serial int64, usage []x509.ExtKeyUsage) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "isolated trust fixture"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		ExtKeyUsage: usage,
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

func nativeTrustTestPEM(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestNativeTrustBundleVerifiesBothRoots(t *testing.T) {
	observer, observerKey := nativeTrustTestCA(t, 1, nil)
	system, systemKey := nativeTrustTestCA(t, 2, nil)
	bundle, err := nativeTrustBundle(nativeTrustTestPEM(observer), [][]byte{system, observer, system})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(bundle, []byte("-----BEGIN CERTIFICATE-----")) != 2 {
		t.Fatal("DER duplicate roots were not removed")
	}
	other, _ := nativeTrustBundle(nativeTrustTestPEM(observer), [][]byte{observer, system})
	if !bytes.Equal(bundle, other) {
		t.Fatal("store order changed the resulting bundle")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundle) {
		t.Fatal("invalid PEM trust bundle")
	}
	for _, anchor := range []struct {
		der []byte
		key *ecdsa.PrivateKey
	}{{observer, observerKey}, {system, systemKey}} {
		ca, _ := x509.ParseCertificate(anchor.der)
		leaf := &x509.Certificate{
			SerialNumber: big.NewInt(3), DNSNames: []string{"fixture.invalid"},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &observerKey.PublicKey, anchor.key)
		if err != nil {
			t.Fatal(err)
		}
		server, _ := x509.ParseCertificate(der)
		if _, err = server.Verify(x509.VerifyOptions{Roots: pool, DNSName: "fixture.invalid"}); err != nil {
			t.Fatal("merged bundle cannot verify a supported chain:", err)
		}
	}
}

func TestNativeTrustWindowsUsage(t *testing.T) {
	for _, test := range []struct {
		name          string
		usage         []x509.ExtKeyUsage
		explicitEmpty bool
		want          bool
	}{
		{"unrestricted", nil, false, true},
		{"server", []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, false, true},
		{"client-only", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, false, false},
		{"explicitly-disabled", nil, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			der, _ := nativeTrustTestCA(t, 4, test.usage)
			ctx, err := syscall.CertCreateCertificateContext(1, &der[0], uint32(len(der)))
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.CertFreeCertificateContext(ctx)
			if test.explicitEmpty {
				// A temporary memory context only; no Windows store is changed.
				usage := syscall.CertEnhKeyUsage{}
				ok, _, err := syscall.NewLazyDLL("crypt32.dll").NewProc("CertSetEnhancedKeyUsage").Call(
					uintptr(unsafe.Pointer(ctx)), uintptr(unsafe.Pointer(&usage)))
				if ok == 0 {
					t.Fatal(err)
				}
			}
			got, err := nativeCertificateAllowsServer(ctx)
			if err != nil || got != test.want {
				t.Fatalf("server usage=%v, want %v, error=%v", got, test.want, err)
			}
		})
	}
}

func TestNativeTrustRegeneration(t *testing.T) {
	root := t.TempDir()
	cfg := launchConfig{Home: filepath.Join(root, "home"), Data: filepath.Join(root, "data")}
	dir := filepath.Join(cfg.Data, "native-capture")
	for _, path := range []string{cfg.Home, dir} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	der, _ := nativeTrustTestCA(t, 5, nil)
	ca := nativeTrustTestPEM(der)
	certPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(certPath, ca, 0600); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual projected initialization writer, in a temporary home.
	if err := writeNativeHome(cfg, "http://observer:fixture-only@127.0.0.1:1", ca); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(certPath)
	count := bytes.Count(first, []byte("-----BEGIN CERTIFICATE-----"))
	if count < 2 || !bytes.HasPrefix(first, ca) {
		t.Fatal("initialization did not retain the observer plus system roots")
	}
	envBefore, _ := os.ReadFile(filepath.Join(cfg.Home, ".env"))
	stBefore, _ := os.Stat(certPath)
	if err := writeNativeHome(cfg, "http://observer:fixture-only@127.0.0.1:1", ca); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(certPath)
	envAfter, _ := os.ReadFile(filepath.Join(cfg.Home, ".env"))
	stAfter, _ := os.Stat(certPath)
	if !bytes.Equal(first, second) || !bytes.Equal(envBefore, envAfter) || stBefore.ModTime() != stAfter.ModTime() {
		t.Fatal("reinitialization changed an unchanged bundle or environment")
	}
	if err := writeNativeTrustBundle(certPath, nil); err == nil {
		t.Fatal("empty signing CA was accepted")
	}
	kept, _ := os.ReadFile(certPath)
	if !bytes.Equal(kept, first) {
		t.Fatal("invalid input replaced the working bundle")
	}
	// A client can temporarily hold the old file without delete sharing.
	// A failed replacement must leave the previously working bundle intact.
	name, _ := syscall.UTF16PtrFromString(certPath)
	reader, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	replaceErr := replaceNativeTrustFile(certPath, append(append([]byte(nil), first...), '\n'))
	syscall.CloseHandle(reader)
	kept, _ = os.ReadFile(certPath)
	if replaceErr == nil || !bytes.Equal(kept, first) {
		t.Fatal("a reader-blocked replacement lost the working bundle")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(first) {
		t.Fatal("generated file cannot be loaded as PEM roots")
	}
	t.Logf("observer_certificates=1 unique_windows_roots=%d stable_reinitialization=true", count-1)
	if output := os.Getenv("OBSERVATORY_TRUST_TEST_OUTPUT"); output != "" {
		if err := os.WriteFile(output, first, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
