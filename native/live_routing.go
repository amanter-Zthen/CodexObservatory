package main

import (
	"codex-observatory/internal/routetrace"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func liveRouteError(r routetrace.Record) bool {
	return r.Stage != "response.output_text.delta" && r.Verdict == "mismatch" && r.Requested != "" && r.Reported != "" && !strings.EqualFold(r.Requested, r.Reported) && r.Thread != "" && r.Turn != "" && r.Scope == "original_desktop_request"
}
func (a *App) routeLoop() {
	seen := map[string]bool{}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		a.readLiveRoutes(seen)
		a.refreshFloating()
		select {
		case <-a.ctxDone:
			return
		case <-ticker.C:
		}
	}
}
func (a *App) readLiveRoutes(seen map[string]bool) {
	dir, e := a.reportDirectory("live-routing-inbox")
	if e != nil {
		return
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	owned, e := a.reportDirectory("routing-observations")
	if e != nil {
		a.routeAuditError.Store(e.Error())
		return
	}
	ownPaths, _ := filepath.Glob(filepath.Join(owned, "*.json"))
	paths = append(paths, ownPaths...)
	a.routeFileCount.Store(uint64(len(paths)))
	archive, e := a.reportDirectory("route-errors")
	if e != nil {
		a.routeAuditError.Store(e.Error())
		return
	}
	archives, _ := filepath.Glob(filepath.Join(archive, "*.json"))
	paths = append(paths, archives...)
	for _, path := range paths {
		if seen[path] {
			continue
		}
		st, e := os.Lstat(path)
		if e != nil || !st.Mode().IsRegular() || st.Size() > 8192 {
			continue
		}
		var r routetrace.Record
		if filepath.Base(filepath.Dir(path)) == "route-errors" {
			var wrapper struct {
				Kind   string            `json:"kind"`
				Report routetrace.Record `json:"report"`
			}
			if loadJSON(path, &wrapper) != nil || wrapper.Kind != "live-route" {
				continue
			}
			r = wrapper.Report
		} else if loadJSON(path, &r) != nil {
			continue
		}
		if !validConsoleRoute(r) {
			continue
		}
		seen[path] = true
		a.acceptFloatingRoute(r)
		if !freshReport(r.At, "", time.Now()) && !liveRouteError(r) {
			continue
		}
		if liveRouteError(r) {
			a.journalMu.Lock()
			e = a.archiveRouteEvidence("live-route", r.ID, r)
			a.journalMu.Unlock()
			if e != nil {
				continue
			}
		}
	}
	// Keep the dedup map proportional to currently retained report files.
	if len(seen) > len(paths)+100 {
		present := map[string]bool{}
		for _, p := range paths {
			present[p] = true
		}
		for p := range seen {
			if !present[p] {
				delete(seen, p)
			}
		}
	}
}
func (a *App) liveRouteSnapshot() []routetrace.Record {
	// Caller holds a.mu.
	out := []routetrace.Record{}
	for _, r := range a.liveRoutes {
		if freshReport(r.At, "", time.Now()) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, out[i].At)
		b, _ := time.Parse(time.RFC3339Nano, out[j].At)
		return a.After(b)
	})
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}
func (a *App) bridgeStatus() Obj {
	paths, _ := filepath.Glob(filepath.Join(a.data, "routing-bridge-status", "*.json"))
	clients := []Obj{}
	for _, path := range paths {
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() || st.Size() > 8192 {
			continue
		}
		var state Obj
		if loadJSON(path, &state) != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, str(state["at"]))
		// Once the original packaged GUI is present and the listener is ready,
		// show readiness while awaiting its first normal response. This never
		// manufactures a model verdict; records still require real metadata.
		readyOriginal := state["listener_ready"] == true && state["inspection_known"] == true && num(state["client_pid"]) > 0 && obj(state["desktop_package_identity"])["verified"] == true
		if err == nil && time.Since(at) <= 10*time.Second && time.Since(at) >= -time.Minute && (state["active"] == true || readyOriginal) {
			clients = append(clients, state)
		}
	}
	reason := "awaiting_desktop_restart_with_bridge"
	if len(paths) > 0 {
		reason = "bridge_not_running"
	}
	if len(clients) > 0 {
		reason = "bridge_online"
	}
	var follow Obj
	if loadJSON(filepath.Join(a.data, "lifecycle-status.json"), &follow) == nil {
		at, err := time.Parse(time.RFC3339Nano, str(follow["at"]))
		if err != nil || time.Since(at) > 10*time.Second {
			follow = nil
		}
	}
	if len(clients) == 0 && follow["following"] == true && follow["desktop_running"] == true {
		reason = "desktop_restart_required"
		if follow["routing_configured"] == false {
			reason = "passive_only"
		}
	}
	return Obj{"active": len(clients) > 0, "clients": clients, "active_clients": len(clients), "reason": reason, "lifecycle": follow, "model_requests_generated": 0}
}
func (a *App) pruneLiveReports(clock time.Time) error {
	statuses, _ := filepath.Glob(filepath.Join(a.data, "routing-bridge-status", "*.json"))
	for _, p := range statuses {
		if st, err := os.Lstat(p); err == nil && st.Mode().IsRegular() && clock.Sub(st.ModTime()) > 5*24*time.Hour {
			_ = os.Remove(p)
		}
	}
	dir, e := a.reportDirectory("live-routing-inbox")
	if e != nil {
		return e
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	owned, e := a.reportDirectory("routing-observations")
	if e != nil {
		return e
	}
	ownPaths, _ := filepath.Glob(filepath.Join(owned, "*.json"))
	paths = append(paths, ownPaths...)
	for _, path := range paths {
		st, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			continue
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		var r routetrace.Record
		if json.Unmarshal(b, &r) != nil {
			continue
		}
		if liveRouteError(r) {
			if e = a.archiveRouteEvidence("live-route", r.ID, r); e != nil {
				return e
			}
			continue
		}
		if !freshReport(r.At, "", clock) {
			if e = os.Remove(path); e != nil {
				return e
			}
		}
	}
	return nil
}
