//go:build windows

package main

import (
	"codex-observatory/internal/floatui"
	"sync"
	"testing"
	"time"
	"unsafe"
)

// Exercises our own temporary host's real native popup. Observations happen
// after presentation; the test publishes synthetic data through the same atomic
// snapshot the normal host reads. Menu cancellation addresses this host only.
func TestNativePopupKeepsAnimationAndSnapshotUpdatesRunning(t *testing.T) {
	a, _ := testApp(t)
	a.visualQuality = "full"
	fixture := floatui.Snapshot{Mode: "synthetic", Revision: 1, Quota: floatui.QuotaView{Known: true, Fresh: true, Remaining: 87}, Rows: []floatui.Row{{Thread: "synthetic-menu-thread", Turn: "fixture-turn", Model: "fixture-model", Color: floatui.Green, Activity: floatui.Running, Outputting: true, EstimatedTokenRate: 19}}}
	a.floatSnapshot.Store(&fixture)
	var mu sync.Mutex
	var opened bool
	var samples []floatui.Scene
	entered, left := make(chan struct{}, 4), make(chan struct{}, 4)
	a.floatMenuObserved = func(active bool) {
		mu.Lock()
		opened = active
		mu.Unlock()
		if active {
			entered <- struct{}{}
		} else {
			left <- struct{}{}
		}
	}
	a.floatFrameObserved = func(scene floatui.Scene) {
		mu.Lock()
		if opened {
			scene.Bands = append([]floatui.Band{}, scene.Bands...)
			scene.Rows = append([]floatui.DrawRow{}, scene.Rows...)
			samples = append(samples, scene)
		}
		mu.Unlock()
	}
	closeWindow := a.startFloating()
	defer closeWindow()
	hwnd, _, _ := user.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(u16("CodexObservatoryRouting-"+shortHash(a.data)))), 0)
	if hwnd == 0 {
		t.Fatal("own test host unavailable")
	}
	getRect := func() rect {
		var r rect
		user.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&r)))
		return r
	}
	time.Sleep(1800 * time.Millisecond)
	baselineBounds := getRect()
	for cycle := 0; cycle < 3; cycle++ {
		user.NewProc("PostMessageW").Call(hwnd, 0x0205, 0, 0)
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("native menu did not open")
		}
		mu.Lock()
		start := len(samples)
		mu.Unlock()
		time.Sleep(250 * time.Millisecond)
		update := fixture
		update.Revision = uint64(cycle + 2)
		update.Quota.Remaining = 76 - float64(cycle)
		update.Rows = append([]floatui.Row{}, fixture.Rows...)
		update.Rows[0].Color = floatui.Red
		update.Rows[0].Model = "updated-model"
		update.PendingCompleted = 1
		a.floatSnapshot.Store(&update)
		time.Sleep(700 * time.Millisecond)
		mu.Lock()
		seen := append([]floatui.Scene{}, samples[start:]...)
		mu.Unlock()
		user.NewProc("PostMessageW").Call(hwnd, 0x001f, 0, 0) // WM_CANCELMODE, this popup only
		select {
		case <-left:
		case <-time.After(2 * time.Second):
			t.Fatal("native menu did not close")
		}
		if len(seen) < 5 {
			t.Fatal("animation stalled while menu remained open", cycle, len(seen))
		}
		if seen[len(seen)-1].FlowTime <= seen[0].FlowTime {
			t.Fatal("flow clock stopped")
		}
		changed, updated := false, false
		for _, scene := range seen {
			if len(scene.Bands) > 0 && scene.Bands[0].Points != seen[0].Bands[0].Points {
				changed = true
			}
			if len(scene.Rows) > 0 && scene.Rows[0].Row.Model == "updated-model" && scene.Rows[0].Row.Color == floatui.Red && scene.PendingCompleted == 1 {
				updated = true
			}
		}
		if !changed || !updated {
			t.Fatal("menu paused flow geometry or live data", cycle, changed, updated)
		}
		for _, scene := range seen {
			if scene.Stage.Entering || scene.Stage.Exiting || scene.Stage.Hidden {
				t.Fatal("opening popup changed the primary appearance", cycle)
			}
		}
		if bounds := getRect(); bounds != baselineBounds {
			t.Fatal("opening or dismissing popup changed primary bounds", cycle, baselineBounds, bounds)
		}
		t.Logf("popup_cycle=%d presented_frames=%d flow_seconds=%.3f geometry_changed=%v model_color_and_badge_updated=%v", cycle+1, len(seen), seen[len(seen)-1].FlowTime-seen[0].FlowTime, changed, updated)
	}
}
