//go:build windows

package main

import (
	"runtime"
	"sort"
	"testing"
	"time"
	"unsafe"
)

// Test messages address this application's temporary native window only.
func TestNativeZoomBurstKeepsMessagePathResponsive(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if p := user.NewProc("SetThreadDpiAwarenessContext"); p.Find() == nil {
		old, _, _ := p.Call(^uintptr(3))
		defer p.Call(old)
	}
	a, _ := testApp(t)
	a.preview = true
	a.inspectWindow = true
	a.visualQuality = "full"
	closeWindow := a.startFloating()
	defer closeWindow()
	hwnd, _, _ := user.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(u16("CodexObservatoryRouting-"+shortHash(a.data)))), 0)
	if hwnd == 0 {
		t.Fatal("own window unavailable")
	}
	getRect := func() rect {
		var r rect
		user.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&r)))
		return r
	}
	before := getRect()
	time.Sleep(1900 * time.Millisecond) // first reveal finishes before interaction
	current, _, _ := kernel.NewProc("GetCurrentProcess").Call()
	gdiCount := func() uintptr { n, _, _ := user.NewProc("GetGuiResources").Call(current, 0); return n }
	gdiBefore := gdiCount()
	samples := make([]float64, 0, 2003)
	command := func(id uintptr) {
		began := time.Now()
		var result uintptr
		ok, _, _ := user.NewProc("SendMessageTimeoutW").Call(hwnd, 0x0111, id, 0, 2, 250, uintptr(unsafe.Pointer(&result)))
		if ok == 0 {
			t.Fatal("zoom command exceeded 250ms response budget")
		}
		samples = append(samples, float64(time.Since(began).Nanoseconds())/1e6)
	}
	for n := 0; n < 2000; n++ {
		if n%2 == 0 {
			command(16)
		} else {
			command(17)
		}
	}
	want := float64(before.Right-before.Left) * 1.25
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		r := getRect()
		if float64(r.Right-r.Left) >= want-3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	after := getRect()
	if diff := float64(after.Right-after.Left) - want; diff < -3 || diff > 3 {
		t.Fatal("one accepted click was not applied", before, after, want)
	}
	time.Sleep(1900 * time.Millisecond) // new reveal finishes, interaction returns
	command(15)
	time.Sleep(3600 * time.Millisecond)
	reset := getRect()
	if reset.Right-reset.Left != before.Right-before.Left {
		t.Fatal("reset did not return to original size", before, reset)
	}
	gdiAfter := gdiCount()
	if gdiAfter > gdiBefore+2 {
		t.Fatal("zoom accumulated GDI resources", gdiBefore, gdiAfter)
	}
	sort.Float64s(samples)
	t.Logf("zoom commands=%d p95_ms=%.3f max_ms=%.3f first_click_target=125%% next_after_reveal=reset100%% gdi_before=%d gdi_after=%d scope=own_native_window_message_path_and_disappear_reappear", len(samples), samples[len(samples)*95/100], samples[len(samples)-1], gdiBefore, gdiAfter)
}
