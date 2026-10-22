//go:build windows

package main

import (
	"testing"
	"time"
	"unsafe"
)

// Exercises this application's own native host and existing show API.
// No external windows, input injection, Codex process or real account is used.
func TestNativeLocalHideResumeAndClose(t *testing.T) {
	a, _ := testApp(t)
	a.preview = true
	a.visualQuality = "auto"
	a.started = time.Now().Add(-18 * time.Second)
	closeWindow := a.startFloating()
	defer closeWindow()
	class := u16("CodexObservatoryRouting-" + shortHash(a.data))
	hwnd, _, _ := user.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(class)), 0)
	if hwnd == 0 {
		t.Fatal("native test host unavailable")
	}
	visible := func() bool { v, _, _ := user.NewProc("IsWindowVisible").Call(hwnd); return v != 0 }
	if !visible() {
		t.Fatal("new native host is not visible")
	}
	current, _, _ := kernel.NewProc("GetCurrentProcess").Call()
	cpu := func() uint64 {
		var created, exit, kernelTime, userTime uint64
		ok, _, _ := kernel.NewProc("GetProcessTimes").Call(current, uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernelTime)), uintptr(unsafe.Pointer(&userTime)))
		if ok == 0 {
			t.Fatal("test process counters unavailable")
		}
		return kernelTime + userTime
	}
	measure := func() float64 { start := cpu(); time.Sleep(time.Second); return float64(cpu()-start) / 1e7 }
	activeCPU := measure()
	user.NewProc("ShowWindow").Call(hwnd, 0)
	if visible() {
		t.Fatal("test host did not hide")
	}
	time.Sleep(100 * time.Millisecond)
	hiddenCPU := measure()
	if hiddenCPU > activeCPU*.5+.025 {
		t.Fatal("hidden host retained excessive animation work", activeCPU, hiddenCPU)
	}
	showFloating(a.data)
	deadline := time.Now().Add(time.Second)
	for !visible() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !visible() {
		t.Fatal("existing show API did not resume native host")
	}
	time.Sleep(800 * time.Millisecond)
	closeWindow()
	valid, _, _ := user.NewProc("IsWindow").Call(hwnd)
	if valid != 0 {
		t.Fatal("native host survived close")
	}
	t.Logf("own native host visible->hidden->shown->closed; active_cpu_seconds=%.4f hidden_cpu_seconds=%.4f per_one_second", activeCPU, hiddenCPU)
}
