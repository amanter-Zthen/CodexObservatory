//go:build windows

package main

import (
	"codex-observatory/internal/routefeed"
	"codex-observatory/internal/routetrace"
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"time"
)

// A pulse is one wake-up of the existing feed loop. It retains no route data,
// creates no worker and leaves manual resynchronization's cursor policy intact.
type routePulseControl struct {
	mu           sync.Mutex
	wake         chan struct{}
	busy, queued bool
	closed       bool
}

var routePulseControls sync.Map // *App -> *routePulseControl, owned by liveFeedLoop.

func (a *App) registerRoutePulse() *routePulseControl {
	control := &routePulseControl{wake: make(chan struct{}, 1)}
	if _, loaded := routePulseControls.LoadOrStore(a, control); loaded {
		return nil
	}
	return control
}

func (a *App) releaseRoutePulse(control *routePulseControl) {
	control.mu.Lock()
	control.closed = true
	control.busy, control.queued = false, false
	select {
	case <-control.wake:
	default:
	}
	control.mu.Unlock()
	routePulseControls.CompareAndDelete(a, control)
}

func (a *App) requestRoutePulse() bool {
	if a.preview || a.demo || a.routeSyncBusy.Load() {
		return false
	}
	select {
	case <-a.ctxDone:
		return false
	default:
	}
	value, ok := routePulseControls.Load(a)
	if !ok {
		return false
	}
	control := value.(*routePulseControl)
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.closed || control.busy || control.queued || a.routeSyncBusy.Load() {
		return false
	}
	control.queued = true
	select {
	case control.wake <- struct{}{}:
		return true
	default:
		control.queued = false
		return false
	}
}

func (control *routePulseControl) begin() {
	control.mu.Lock()
	defer control.mu.Unlock()
	control.busy, control.queued = true, false
	// A regular timer/manual wake can satisfy a pulse accepted just before it.
	// Consuming that slot here prevents a second identical fetch afterwards.
	select {
	case <-control.wake:
	default:
	}
}

func (control *routePulseControl) end() {
	control.mu.Lock()
	control.busy = false
	control.mu.Unlock()
}

func (a *App) apiRoutePulse(w http.ResponseWriter, r *http.Request) {
	var request struct{}
	if !body(w, r, &request) {
		return
	}
	writeJSON(w, Obj{"accepted": a.requestRoutePulse()})
}

// The injected operations let contract tests feed memory batches into the same
// loop. Production defaults are the original feed, state and audit operations.
type routePulseRuntime struct {
	ticks    <-chan time.Time
	discover func() (routefeed.Description, error)
	fetch    func(context.Context, *http.Client, routefeed.Description, string, uint64) (routefeed.Batch, error)
	accept   func(routetrace.Record, bool)
	publish  func()
	refresh  func()
	complete func()
	audit    func([]routetrace.Record)
}

func (a *App) routePulseRuntimeDefaults(runtime routePulseRuntime) routePulseRuntime {
	if runtime.discover == nil {
		runtime.discover = func() (routefeed.Description, error) {
			var description routefeed.Description
			err := loadJSON(filepath.Join(a.data, routefeed.DiscoveryFile), &description)
			return description, err
		}
	}
	if runtime.fetch == nil {
		runtime.fetch = routefeed.Fetch
	}
	if runtime.accept == nil {
		runtime.accept = a.acceptFloatingRouteReplay
	}
	if runtime.publish == nil {
		runtime.publish = a.publishFloating
	}
	if runtime.refresh == nil {
		runtime.refresh = a.refreshFloating
	}
	if runtime.complete == nil {
		runtime.complete = a.completeRouteSync
	}
	if runtime.audit == nil {
		runtime.audit = func(records []routetrace.Record) {
			if sink := a.routeAuditSink(); sink != nil {
				sink.Publish(records)
			}
		}
	}
	return runtime
}
