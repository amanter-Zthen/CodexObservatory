//go:build windows

package wirecapture

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

func systemProxyConfig() (string, string) {
	key, _ := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
	var h syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, key, 0, 0x20019, &h) != nil {
		return "", ""
	}
	defer syscall.RegCloseKey(h)
	read := func(name string) ([]byte, uint32) {
		n, _ := syscall.UTF16PtrFromString(name)
		var typ, size uint32
		if syscall.RegQueryValueEx(h, n, nil, &typ, nil, &size) != nil || size > 65536 || size == 0 {
			return nil, 0
		}
		b := make([]byte, size)
		if syscall.RegQueryValueEx(h, n, nil, &typ, &b[0], &size) != nil {
			return nil, 0
		}
		return b, typ
	}
	enabled, typ := read("ProxyEnable")
	if typ != 4 || len(enabled) < 4 || binary.LittleEndian.Uint32(enabled) != 1 {
		return "", ""
	}
	text := func(name string) string {
		b, typ := read(name)
		if typ != 1 || len(b) < 2 {
			return ""
		}
		words := unsafe.Slice((*uint16)(unsafe.Pointer(&b[0])), len(b)/2)
		return syscall.UTF16ToString(words)
	}
	return text("ProxyServer"), text("ProxyOverride")
}
