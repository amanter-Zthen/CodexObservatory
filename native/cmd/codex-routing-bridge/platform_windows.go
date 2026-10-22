//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

func hideChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func replaceStatus(from, to string) {
	a, _ := syscall.UTF16PtrFromString(from)
	b, _ := syscall.UTF16PtrFromString(to)
	syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 1|8)
}
