//go:build windows

package main

import (
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routefeed"
	"codex-observatory/internal/routetrace"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func TestNativeRouteSyncCommandKeepsFramesAndDoesNotExit(t *testing.T) {
	a, _ := testApp(t)
	a.visualQuality = "full"
	syncHealth(t, a, "native-home-v2-sse")
	hub, _ := routefeed.NewHub(8)
	hub.Publish([]routetrace.Record{syncRecord("native-sync", "response.created", "wanted", "match", time.Now())})
	runSyncLoop(t, a, hub)
	var frames atomic.Int32
	a.floatFrameObserved = func(floatui.Scene) { frames.Add(1) }
	closeWindow := a.startFloating()
	defer closeWindow()
	hwnd, _, _ := user.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(u16("CodexObservatoryRouting-"+shortHash(a.data)))), 0)
	if hwnd == 0 {
		t.Fatal("own isolated test window was not created")
	}
	before := frames.Load()
	for i := 0; i < 64; i++ {
		user.NewProc("PostMessageW").Call(hwnd, 0x0111, 18, 0)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a.routeSyncStatus().State == "synced" && frames.Load() > before+3 {
			select {
			case <-a.ctxDone:
				t.Fatal("repair command stopped the monitor")
			default:
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native repair command stalled", a.routeSyncStatus(), frames.Load()-before)
}
