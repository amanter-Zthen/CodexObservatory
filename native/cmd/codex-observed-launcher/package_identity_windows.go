//go:build windows

package main

import (
	"strings"
	"syscall"
	"unsafe"
)

func desktopPackageIdentity(pid int) map[string]any {
	result := map[string]any{"verified": false}
	if pid == 0 {
		return result
	}
	process, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		return result
	}
	defer syscall.CloseHandle(process)
	dll := syscall.NewLazyDLL("kernel32.dll")
	read := func(name string) string {
		proc := dll.NewProc(name)
		var size uint32
		proc.Call(uintptr(process), uintptr(unsafe.Pointer(&size)), 0)
		if size == 0 || size > 32768 {
			return ""
		}
		buffer := make([]uint16, size)
		code, _, _ := proc.Call(uintptr(process), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&buffer[0])))
		if code != 0 {
			return ""
		}
		return syscall.UTF16ToString(buffer)
	}
	packageName := read("GetPackageFullName")
	appID := read("GetApplicationUserModelId")
	result["package_full_name"] = packageName
	result["app_user_model_id"] = appID
	result["verified"] = strings.HasPrefix(packageName, "OpenAI.Codex_") && appID == "OpenAI.Codex_2p2nqsd0c76g0!App"
	return result
}
