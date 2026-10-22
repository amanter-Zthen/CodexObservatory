//go:build windows

package main

import (
	"codex-observatory/internal/routefeed"
	"codex-observatory/internal/routetrace"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func displayPulseFixture(t *testing.T) *App {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &App{data: t.TempDir(), address: "http://127.0.0.1:43210", token: "pulse-isolated-token", ctxDone: ctx.Done(), stop: cancel}
}

func displayPulseIdle(a *App) bool {
	value, ok := routePulseControls.Load(a)
	if !ok {
		return false
	}
	control := value.(*routePulseControl)
	control.mu.Lock()
	defer control.mu.Unlock()
	return !control.busy && !control.queued && len(control.wake) == 0
}

func TestDisplayOverlayPulseBoundedLifecycle(t *testing.T) {
	a := displayPulseFixture(t)
	if a.requestRoutePulse() {
		t.Fatal("pulse accepted without a live feed owner")
	}
	control := a.registerRoutePulse()
	if control == nil || cap(control.wake) != 1 || a.registerRoutePulse() != nil {
		t.Fatal("feed ownership or pending-slot bound is invalid")
	}
	defer a.releaseRoutePulse(control)
	var accepted atomic.Int32
	var callers sync.WaitGroup
	for i := 0; i < 64; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			for j := 0; j < 20; j++ {
				if a.requestRoutePulse() {
					accepted.Add(1)
				}
			}
		}()
	}
	callers.Wait()
	if accepted.Load() != 1 || len(control.wake) != 1 {
		t.Fatal("pulse storm accumulated more than one pending wake")
	}
	control.begin()
	for i := 0; i < 128; i++ {
		if a.requestRoutePulse() {
			t.Fatal("pulse queued work while the existing feed loop was busy")
		}
	}
	if len(control.wake) != 0 {
		t.Fatal("regular feed work did not consume the coalesced pending pulse")
	}
	control.end()
	if !a.requestRoutePulse() {
		t.Fatal("idle feed did not accept the next pulse")
	}
	a.releaseRoutePulse(control)
	if _, ok := routePulseControls.Load(a); ok || a.requestRoutePulse() || len(control.wake) != 0 {
		t.Fatal("finished feed retained its pulse slot")
	}
}

func TestDisplayOverlayPulseDisabledModes(t *testing.T) {
	for _, mode := range []string{"preview", "demo", "manual-sync-busy", "owner-stopped"} {
		t.Run(mode, func(t *testing.T) {
			a := displayPulseFixture(t)
			control := a.registerRoutePulse()
			defer a.releaseRoutePulse(control)
			switch mode {
			case "preview":
				a.preview = true
			case "demo":
				a.demo = true
			case "manual-sync-busy":
				a.routeSyncBusy.Store(true)
			case "owner-stopped":
				a.stop()
			}
			if a.requestRoutePulse() || len(control.wake) != 0 {
				t.Fatal("disabled mode accepted an automatic pulse")
			}
		})
	}
}

func TestDisplayOverlayPulseAPIContract(t *testing.T) {
	for _, tc := range []struct {
		name, method, token, contentType, content, origin string
		want                                              int
	}{
		{"missing-token", "POST", "", "application/json", "{}", "", 403},
		{"wrong-token", "POST", "wrong", "application/json", "{}", "", 403},
		{"get-rejected", "GET", "pulse-isolated-token", "application/json", "{}", "", 405},
		{"foreign-origin", "POST", "pulse-isolated-token", "application/json", "{}", "https://unrelated.invalid", 403},
		{"content-type", "POST", "pulse-isolated-token", "text/plain", "{}", "", 415},
		{"unknown-body-field", "POST", "pulse-isolated-token", "application/json", `{"history":true}`, "", 400},
		{"trailing-body", "POST", "pulse-isolated-token", "application/json", "{} {}", "", 400},
		{"accepted", "POST", "pulse-isolated-token", "application/json", "{}", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := displayPulseFixture(t)
			control := a.registerRoutePulse()
			defer a.releaseRoutePulse(control)
			before := &RouteSyncStatus{State: "idle"}
			a.routeSyncState.Store(before)
			handler := a.handler()
			call := func() *httptest.ResponseRecorder {
				request := httptest.NewRequest(tc.method, a.address+"/api/routing/pulse", strings.NewReader(tc.content))
				request.Header.Set("X-Observatory-Token", tc.token)
				request.Header.Set("Content-Type", tc.contentType)
				request.Header.Set("Origin", tc.origin)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				return response
			}
			response := call()
			if response.Code != tc.want {
				t.Fatalf("got %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
			if tc.want == 200 {
				var result map[string]bool
				if json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result) != 1 || !result["accepted"] {
					t.Fatal("unexpected pulse acceptance response")
				}
				response = call()
				if json.Unmarshal(response.Body.Bytes(), &result) != nil || result["accepted"] || len(control.wake) != 1 {
					t.Fatal("second pulse did not coalesce into the existing slot")
				}
			} else if len(control.wake) != 0 {
				t.Fatal("rejected request queued work")
			}
			if a.routeSyncState.Load() != before || a.routeSyncBusy.Load() || a.routeSyncWake != nil {
				t.Fatal("automatic pulse modified the manual resync state")
			}
		})
	}
}

type displayPulseCursor struct {
	epoch    string
	sequence uint64
}

func displayRunPulseLoop(t *testing.T, a *App, runtime routePulseRuntime) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); a.liveFeedLoopWithPulse(runtime) }()
	t.Cleanup(func() {
		a.stop()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Error("pulse feed loop did not stop")
		}
		if _, found := routePulseControls.Load(a); found {
			t.Error("pulse registry retained a stopped app")
		}
	})
	displayEventually(t, "pulse loop registration", func() bool {
		_, found := routePulseControls.Load(a)
		return found
	})
}

func TestDisplayOverlayPulseKeepsCursorWithoutReplayOrDuplicateAudit(t *testing.T) {
	a := displayPulseFixture(t)
	ticks := make(chan time.Time, 1)
	epoch := strings.Repeat("a", 32)
	var fetches, publishes, refreshes, completes atomic.Int32
	var mu sync.Mutex
	var cursors []displayPulseCursor
	var accepted, audited []string
	var replay []bool
	record := func(id string, sequence uint64) routefeed.Item {
		return routefeed.Item{Sequence: sequence, Record: routetrace.Record{ID: id}}
	}
	displayRunPulseLoop(t, a, routePulseRuntime{
		ticks: ticks,
		discover: func() (routefeed.Description, error) {
			return routefeed.Description{Version: 1, URL: "http://127.0.0.1:1/events", Token: strings.Repeat("t", 64)}, nil
		},
		fetch: func(_ context.Context, _ *http.Client, _ routefeed.Description, current string, after uint64) (routefeed.Batch, error) {
			mu.Lock()
			cursors = append(cursors, displayPulseCursor{current, after})
			mu.Unlock()
			switch fetches.Add(1) {
			case 1:
				return routefeed.Batch{Epoch: epoch, Sequence: 41, Reset: true, Items: []routefeed.Item{record("initial", 41)}}, nil
			case 2:
				return routefeed.Batch{Epoch: epoch, Sequence: 41}, nil
			case 3:
				return routefeed.Batch{Epoch: epoch, Sequence: 42, Items: []routefeed.Item{record("incremental", 42)}}, nil
			case 4:
				return routefeed.Batch{Epoch: epoch, Sequence: 42}, nil
			case 5:
				return routefeed.Batch{Epoch: epoch, Sequence: 42, Reset: true, Items: []routefeed.Item{record("manual-checkpoint", 42)}}, nil
			default:
				return routefeed.Batch{}, errors.New("unexpected extra fetch")
			}
		},
		accept: func(record routetrace.Record, forced bool) {
			mu.Lock()
			defer mu.Unlock()
			accepted, replay = append(accepted, record.ID), append(replay, forced)
		},
		publish: func() { publishes.Add(1) },
		refresh: func() { refreshes.Add(1) },
		complete: func() {
			completes.Add(1)
			a.finishRouteSync("synced", "isolated manual sync complete")
		},
		audit: func(records []routetrace.Record) {
			mu.Lock()
			defer mu.Unlock()
			for _, record := range records {
				audited = append(audited, record.ID)
			}
		},
	})
	ticks <- time.Now()
	displayEventually(t, "initial feed publication", func() bool { return publishes.Load() == 1 && displayPulseIdle(a) })
	for want := int32(2); want <= 4; want++ {
		if !a.requestRoutePulse() {
			t.Fatal("idle feed rejected pulse")
		}
		displayEventually(t, "pulse increment publication", func() bool { return publishes.Load() == want && displayPulseIdle(a) })
	}
	mu.Lock()
	gotCursors := append([]displayPulseCursor(nil), cursors...)
	gotAccepted := append([]string(nil), accepted...)
	gotAudited := append([]string(nil), audited...)
	gotReplay := append([]bool(nil), replay...)
	mu.Unlock()
	wantCursors := []displayPulseCursor{{"", 0}, {epoch, 41}, {epoch, 41}, {epoch, 42}}
	if !reflect.DeepEqual(gotCursors, wantCursors) ||
		!reflect.DeepEqual(gotAccepted, []string{"initial", "incremental"}) ||
		!reflect.DeepEqual(gotAudited, gotAccepted) || !reflect.DeepEqual(gotReplay, []bool{false, false}) {
		t.Fatalf("pulse reset/replayed/duplicated feed state: cursors=%v accepted=%v audited=%v forced=%v", gotCursors, gotAccepted, gotAudited, gotReplay)
	}
	if refreshes.Load() != 0 || completes.Load() != 0 || a.routeSyncState.Load() != nil {
		t.Fatal("pulse entered manual history/health resynchronization")
	}
	if !a.requestRouteSync() {
		t.Fatal("original explicit manual resync was disabled")
	}
	displayEventually(t, "original manual resync completion", func() bool { return completes.Load() == 1 && displayPulseIdle(a) })
	mu.Lock()
	lastCursor, lastReplay := cursors[len(cursors)-1], replay[len(replay)-1]
	mu.Unlock()
	if lastCursor != (displayPulseCursor{"", 0}) || !lastReplay || refreshes.Load() != 1 || a.routeSyncBusy.Load() {
		t.Fatal("manual resync lost its original reset/replay semantics")
	}
}

func TestDisplayOverlayPulseBusyFailureRecoversWithoutCursorReset(t *testing.T) {
	a := displayPulseFixture(t)
	ticks := make(chan time.Time, 1)
	started, release := make(chan struct{}), make(chan struct{})
	var fetches, publishes atomic.Int32
	var mu sync.Mutex
	var cursors []displayPulseCursor
	epoch := strings.Repeat("b", 32)
	displayRunPulseLoop(t, a, routePulseRuntime{
		ticks: ticks,
		discover: func() (routefeed.Description, error) {
			return routefeed.Description{Version: 1, URL: "http://127.0.0.1:1/events", Token: strings.Repeat("t", 64)}, nil
		},
		fetch: func(ctx context.Context, _ *http.Client, _ routefeed.Description, current string, after uint64) (routefeed.Batch, error) {
			mu.Lock()
			cursors = append(cursors, displayPulseCursor{current, after})
			mu.Unlock()
			if fetches.Add(1) == 1 {
				return routefeed.Batch{Epoch: epoch, Sequence: 9}, nil
			}
			if fetches.Load() == 2 {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return routefeed.Batch{}, errors.New("isolated fetch failure")
			}
			return routefeed.Batch{Epoch: epoch, Sequence: 9}, nil
		},
		accept:   func(routetrace.Record, bool) { t.Error("empty fixture produced a route") },
		publish:  func() { publishes.Add(1) },
		refresh:  func() { t.Error("automatic pulse refreshed history/health") },
		complete: func() { t.Error("automatic pulse completed manual resync") },
		audit:    func([]routetrace.Record) { t.Error("empty fixture produced audit output") },
	})
	ticks <- time.Now()
	displayEventually(t, "first cursor publication", func() bool { return publishes.Load() == 1 && displayPulseIdle(a) })
	if !a.requestRoutePulse() {
		t.Fatal("idle loop rejected blocked-fetch fixture")
	}
	select {
	case <-started:
	case <-time.After(4 * time.Second):
		t.Fatal("blocked fixture did not enter fetch")
	}
	for i := 0; i < 128; i++ {
		if a.requestRoutePulse() {
			t.Fatal("busy fetch accepted additional work")
		}
	}
	close(release)
	displayEventually(t, "failed fetch releases busy flag", func() bool { return displayPulseIdle(a) })
	if !a.requestRoutePulse() {
		t.Fatal("failure permanently disabled pulses")
	}
	displayEventually(t, "recovered feed publication", func() bool { return publishes.Load() == 2 && displayPulseIdle(a) })
	mu.Lock()
	got := append([]displayPulseCursor(nil), cursors...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []displayPulseCursor{{"", 0}, {epoch, 9}, {epoch, 9}}) {
		t.Fatalf("failed pulse reset or duplicated the cursor: %v", got)
	}
}
