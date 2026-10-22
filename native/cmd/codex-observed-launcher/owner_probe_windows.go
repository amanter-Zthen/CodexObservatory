//go:build windows

package main

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Ownership classification for whoever currently serves instance.json.
//
// instanceLock (platform_windows.go) is a Windows named kernel mutex
// ("Local\\CodexObservatory-<shortHash(data)>"); a foreign holder cannot be
// enumerated directly, so ownership is derived from the published API listener:
//
//	instance.json -> url -> port -> GetExtendedTcpTable(LISTENER) -> OwningProcess
//
// instance.json carries no PID; the PID comes from the port.
const (
	ownerNone             = "NONE"
	ownerStarting         = "STARTING"           // we spawned a child; API not proven yet
	ownerTracked          = "OWNED_TRACKED"      // API listener proven to be our child
	ownerLegacySameTarget = "LEGACY_SAME_TARGET" // live PID, image == cfg.Monitor
	ownerForeignLive      = "FOREIGN_LIVE"       // live PID, image != cfg.Monitor
)

// probeChildOwner is a package-level variable so tests can substitute a stub.
var probeChildOwner = ownerOfInstance

type instanceFileRecord struct {
	URL     string `json:"url"`
	Version string `json:"version"`
}

func instancePath(cfg launchConfig) string { return filepath.Join(cfg.Data, "instance.json") }

func ownerOfInstance(cfg launchConfig) string {
	pid := instanceListenerPID(cfg)
	childMu.Lock()
	trackedPid := 0
	if tracked != nil {
		trackedPid = tracked.pid
	}
	childMu.Unlock()
	if pid == 0 {
		if trackedPid != 0 {
			return ownerStarting
		}
		return ownerNone
	}
	if trackedPid != 0 && pid == trackedPid {
		return ownerTracked
	}
	path := imagePathOfPID(uint32(pid))
	if path == "" {
		return ownerNone
	}
	if strings.EqualFold(path, cfg.Monitor) {
		return ownerLegacySameTarget
	}
	return ownerForeignLive
}

func instanceListenerPID(cfg launchConfig) int {
	b, err := os.ReadFile(instancePath(cfg))
	if err != nil || len(b) == 0 || len(b) > 8192 {
		return 0
	}
	rec := instanceFileRecord{}
	if json.Unmarshal(b, &rec) != nil || rec.URL == "" {
		return 0
	}
	port := portOfURL(rec.URL)
	if port <= 0 || port > 65535 {
		return 0
	}
	return tcpListenerPID(uint16(port))
}

func portOfURL(raw string) int {
	i := strings.Index(raw, "://")
	if i < 0 {
		return 0
	}
	rest := raw[i+3:]
	if j := strings.IndexAny(rest, "/#?"); j >= 0 {
		rest = rest[:j]
	}
	_, p, err := net.SplitHostPort(rest)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return 0
	}
	return n
}

// ---- port -> PID via GetExtendedTcpTable (LISTENER table only) ----
var (
	iphlpapi              = syscall.NewLazyDLL("iphlpapi.dll")
	pGetExtendedTcpTable  = iphlpapi.NewProc("GetExtendedTcpTable")
	pQueryFullProcessName = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")
)

const (
	tcpTableOwnerPIDListener = 3
	ipv4Table                = 2
	processQueryLimitedInfo  = 0x1000
	ownerPidRowSize          = 24
	errorInsufficientBuffer  = 122
)

func tcpListenerPID(port uint16) int {
	var size uint32
	ret, _, _ := pGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0,
		ipv4Table, uintptr(tcpTableOwnerPIDListener), 0)
	if ret != errorInsufficientBuffer || size == 0 {
		return 0
	}
	buf := make([]byte, size)
	ret, _, _ = pGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)), 0, ipv4Table, uintptr(tcpTableOwnerPIDListener), 0)
	if ret != 0 || len(buf) < 4 {
		return 0
	}
	count := int(binary.LittleEndian.Uint32(buf[:4]))
	for i := 0; i < count; i++ {
		off := 4 + i*ownerPidRowSize
		if off+ownerPidRowSize > len(buf) {
			break
		}
		row := buf[off : off+ownerPidRowSize]
		if binary.BigEndian.Uint16(row[8:10]) != port {
			continue
		}
		if pid := int(binary.LittleEndian.Uint32(row[20:24])); pid > 0 {
			return pid
		}
	}
	return 0
}

func imagePathOfPID(pid uint32) string {
	h, err := syscall.OpenProcess(processQueryLimitedInfo, false, pid)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	ok, _, _ := pQueryFullProcessName.Call(uintptr(h), 0,
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:size])
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimitedInfo, false, uint32(pid))
	if err != nil {
		return false
	}
	syscall.CloseHandle(h)
	return true
}
