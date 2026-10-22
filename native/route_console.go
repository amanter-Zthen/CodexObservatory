package main

import (
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routefeed"
	"codex-observatory/internal/routetrace"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func validConsoleRoute(r routetrace.Record) bool {
	at, e := time.Parse(time.RFC3339Nano, r.At)
	return e == nil && !at.After(time.Now().Add(time.Minute)) && r.ID != "" && len(r.ID) <= 256 && r.Thread != "" && len(r.Thread) <= 256 && r.Turn != "" && len(r.Turn) <= 256 && r.Scope == "original_desktop_request"
}
func routeAt(r routetrace.Record) time.Time {
	at, _ := time.Parse(time.RFC3339Nano, r.At)
	return at
}
func (a *App) cacheConsoleRoute(r routetrace.Record) {
	if !validConsoleRoute(r) || !freshReport(r.At, "", time.Now()) && !liveRouteError(r) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.liveRoutes == nil {
		a.liveRoutes = map[string]routetrace.Record{}
	}
	old, exists := a.liveRoutes[r.Thread]
	if !exists || routeAt(r).After(routeAt(old)) {
		a.liveRoutes[r.Thread] = r
	}
	if r.Reported != "" && (r.Source == "native_wire_response" || r.Source == "native_response_trace") {
		if a.routeResults == nil {
			a.routeResults = map[string]routetrace.Record{}
		}
		old, exists := a.routeResults[r.Thread]
		if !exists || routeAt(r).After(routeAt(old)) {
			a.routeResults[r.Thread] = r
		}
	}
	if liveRouteError(r) {
		if a.routeAnomalies == nil {
			a.routeAnomalies = map[string]routetrace.Record{}
		}
		old, exists := a.routeAnomalies[r.Thread]
		if !exists || routeAt(r).After(routeAt(old)) {
			a.routeAnomalies[r.Thread] = r
		}
	}
}

// Caller holds a.mu. Route evidence keeps its exact thread discoverable even
// when rollout telemetry has aged out of the ordinary report window.
func (a *App) visibleSessionReports(f *FileState, clock time.Time) bool {
	if f == nil {
		return false
	}
	_, permanent := a.routeAnomalies[f.Summary.Thread]
	r, recent := a.liveRoutes[f.Summary.Thread]
	return visibleReports(f, clock) || permanent || recent && freshReport(r.At, "", clock)
}
func (a *App) routeAuditSink() *routefeed.Sink {
	a.routeAuditOnce.Do(func() {
		dir, e := a.reportDirectory("routing-observations")
		if e != nil {
			a.routeAuditError.Store(e.Error())
			return
		}
		sink, e := routefeed.NewSink(func(r routetrace.Record) error {
			if !validConsoleRoute(r) || !freshReport(r.At, "", time.Now()) && !liveRouteError(r) {
				return nil
			}
			a.journalMu.Lock()
			defer a.journalMu.Unlock()
			if liveRouteError(r) {
				if e := a.archiveRouteEvidence("live-route", r.ID, r); e != nil {
					return e
				}
			}
			// Producer and monitor have separate stores and atomic temporary files.
			// IDs deduplicate source reconnects; the bounded sink samples progress.
			return saveJSON(filepath.Join(dir, shortHash(r.ID)+".json"), r)
		}, 2048)
		if e != nil {
			a.routeAuditError.Store(e.Error())
			return
		}
		a.routeAudit.Store(sink)
	})
	return a.routeAudit.Load()
}
func (a *App) routeRecordingStatus() Obj {
	status := Obj{"files": a.routeFileCount.Load(), "source": "original_desktop_request", "retention_days": 5}
	if err := a.routeAuditError.Load(); err != nil {
		status["error"] = err
	}
	if sink := a.routeAudit.Load(); sink != nil {
		status["writer"] = sink.Stats()
		status["active"] = true
	} else {
		status["active"] = false
	}
	return status
}
func (a *App) closeRouteAudit() {
	if sink := a.routeAudit.Load(); sink != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := sink.Close(ctx); err != nil {
			a.routeAuditError.Store(err.Error())
		}
	}
}

type SelectedRouteView struct {
	Thread  string             `json:"thread_id"`
	Current *floatui.Row       `json:"current,omitempty"`
	Latest  *routetrace.Record `json:"latest_record,omitempty"`
	Result  *routetrace.Record `json:"last_response,omitempty"`
}

// The caller holds a.mu.RLock; current state comes from the same publication as
// the floating window. A historical record stays distinct from the current turn.
func (a *App) selectedRouteView(thread string, floating floatui.Snapshot) SelectedRouteView {
	view := SelectedRouteView{Thread: thread}
	for _, row := range floating.Rows {
		if row.Thread == thread {
			copy := row
			view.Current = &copy
			break
		}
	}
	if row, ok := a.liveRoutes[thread]; ok && (freshReport(row.At, "", time.Now()) || liveRouteError(row)) {
		copy := row
		view.Latest = &copy
	}
	if row, ok := a.routeResults[thread]; ok && (freshReport(row.At, "", time.Now()) || liveRouteError(row)) {
		copy := row
		view.Result = &copy
	}
	return view
}
func (a *App) routeHistory(thread string, limit int) ([]routetrace.Record, bool, error) {
	a.journalMu.RLock()
	defer a.journalMu.RUnlock()
	if limit < 1 || limit > 8192 {
		limit = 1200
	}
	rows := map[string]routetrace.Record{}
	bytesRead := int64(0)
	truncated := false
	for _, name := range []string{"live-routing-inbox", "routing-observations", "route-errors"} {
		dir := filepath.Join(a.data, name)
		info, e := os.Lstat(dir)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, false, e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, false, fmt.Errorf("route history directory must be local")
		}
		base, e := filepath.EvalSymlinks(a.data)
		if e != nil {
			return nil, false, e
		}
		resolved, e := filepath.EvalSymlinks(dir)
		if e != nil || canonical(resolved) != canonical(filepath.Join(base, name)) {
			return nil, false, fmt.Errorf("route history outside data directory")
		}
		paths, e := filepath.Glob(filepath.Join(dir, "*.json"))
		if e != nil {
			return nil, false, e
		}
		for _, path := range paths {
			st, e := os.Lstat(path)
			if e != nil || !st.Mode().IsRegular() || st.Size() > 32768 {
				continue
			}
			bytesRead += st.Size()
			if bytesRead > 64*1024*1024 {
				truncated = true
				break
			}
			b, e := os.ReadFile(path)
			if e != nil {
				continue
			}
			var row routetrace.Record
			if name == "route-errors" {
				var wrapper struct {
					Kind   string            `json:"kind"`
					Report routetrace.Record `json:"report"`
				}
				if json.Unmarshal(b, &wrapper) != nil || wrapper.Kind != "live-route" {
					continue
				}
				row = wrapper.Report
			} else if json.Unmarshal(b, &row) != nil {
				continue
			}
			if row.Thread == thread && validConsoleRoute(row) && (freshReport(row.At, "", time.Now()) || liveRouteError(row)) {
				rows[row.ID] = row
			}
		}
	}
	out := make([]routetrace.Record, 0, len(rows))
	for _, r := range rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if routeAt(out[i]).Equal(routeAt(out[j])) {
			return out[i].ID < out[j].ID
		}
		return routeAt(out[i]).Before(routeAt(out[j]))
	})
	if len(out) > limit {
		out = out[len(out)-limit:]
		truncated = true
	}
	return out, truncated, nil
}
