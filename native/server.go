package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed ui/*
var webAssets embed.FS

func (a *App) handler() http.Handler {
	mux := http.NewServeMux()
	public, _ := fs.Sub(webAssets, "ui")
	mux.Handle("/", http.FileServer(http.FS(public)))
	mux.HandleFunc("/api/state", a.apiState)
	mux.HandleFunc("/api/routing/resync", func(w http.ResponseWriter, r *http.Request) {
		var request struct{}
		if !body(w, r, &request) {
			return
		}
		accepted := a.requestRouteSync()
		writeJSON(w, Obj{"accepted": accepted, "route_sync": a.routeSyncStatus()})
	})
	mux.HandleFunc("/api/completions", a.apiCompletions)
	mux.HandleFunc("/api/completions/open", a.apiOpenCompletion)
	mux.HandleFunc("/api/floating-state", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.readFloatingSnapshot()) })
	mux.HandleFunc("/api/history", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if !a.validKey(key) {
			fail(w, 404, "unknown session")
			return
		}
		events, truncated, e := a.history(key, 1200)
		if e != nil {
			fail(w, 500, e.Error())
			return
		}
		a.mu.RLock()
		thread := ""
		if f := a.files[key]; f != nil {
			thread = f.Summary.Thread
		}
		a.mu.RUnlock()
		if thread == "" {
			fail(w, 404, "unknown session")
			return
		}
		routes, routeTruncated, e := a.routeHistory(thread, 1200)
		if e != nil {
			fail(w, 500, e.Error())
			return
		}
		writeJSON(w, Obj{"events": events, "truncated": truncated, "routes": routes, "routes_truncated": routeTruncated})
	})
	mux.HandleFunc("/api/settings", a.apiSettings)
	mux.HandleFunc("/api/pin", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key   string `json:"key"`
			Title string `json:"title"`
		}
		if !body(w, r, &req) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if req.Key != "" && a.files[req.Key] == nil {
			fail(w, 404, "unknown session")
			return
		}
		a.cfg.Pinned = req.Key
		if req.Title != "" {
			a.cfg.Aliases[req.Key] = limitText(req.Title, 120)
		}
		if e := saveJSON(filepath.Join(a.data, "settings.json"), a.cfg); e != nil {
			fail(w, 500, e.Error())
			return
		}
		writeJSON(w, Obj{"ok": true})
	})
	mux.HandleFunc("/api/mark", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Key  string `json:"key"`
			Note string `json:"note"`
		}
		if !body(w, r, &req) {
			return
		}
		m, e := a.addMark(req.Key, req.Note)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
		writeJSON(w, m)
	})
	mux.HandleFunc("/api/probe", func(w http.ResponseWriter, r *http.Request) {
		var req ProbeRequest
		if !body(w, r, &req) {
			return
		}
		p, e := a.startProbe(req)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
		writeJSON(w, p)
	})
	mux.HandleFunc("/api/cancel-probe", func(w http.ResponseWriter, r *http.Request) {
		var req Obj
		if !body(w, r, &req) {
			return
		}
		a.mu.Lock()
		if a.probeCancel != nil {
			a.probeCancel()
		}
		a.mu.Unlock()
		writeJSON(w, Obj{"ok": true})
	})
	mux.HandleFunc("/api/rescan", func(w http.ResponseWriter, r *http.Request) {
		var req Obj
		if !body(w, r, &req) {
			return
		}
		select {
		case a.scanKick <- struct{}{}:
		default:
		}
		writeJSON(w, Obj{"ok": true})
	})
	mux.HandleFunc("/api/open-data", func(w http.ResponseWriter, r *http.Request) {
		var req Obj
		if !body(w, r, &req) {
			return
		}
		if e := openFolder(a.data); e != nil {
			fail(w, 500, e.Error())
			return
		}
		writeJSON(w, Obj{"ok": true})
	})
	mux.HandleFunc("/api/self-test", func(w http.ResponseWriter, r *http.Request) {
		var req Obj
		if !body(w, r, &req) {
			return
		}
		result := runSelfCheck()
		_ = saveJSON(filepath.Join(a.data, "self-check.json"), result)
		writeJSON(w, result)
	})
	mux.HandleFunc("/api/export", a.apiExport)
	mux.HandleFunc("/api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		var req Obj
		if !body(w, r, &req) {
			return
		}
		writeJSON(w, Obj{"ok": true})
		go func() { time.Sleep(150 * time.Millisecond); a.stop() }()
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := url.Parse(a.address)
		if u == nil || r.Host != u.Host {
			fail(w, 403, "invalid host")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != a.address {
			fail(w, 403, "foreign origin")
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			fail(w, 403, "cross-site request")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			supplied := r.Header.Get("X-Observatory-Token")
			if subtle.ConstantTimeCompare([]byte(supplied), []byte(a.token)) != 1 {
				fail(w, 403, "unauthorized; reopen from the tray or exe")
				return
			}
			if r.Method != "GET" && r.Method != "POST" {
				fail(w, 405, "method not allowed")
				return
			}
		} else if r.Method != "GET" && r.Method != "HEAD" {
			fail(w, 405, "method not allowed")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, x any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(x)
}
func fail(w http.ResponseWriter, status int, s string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Obj{"error": limitText(s, 600)})
}
func body(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Method != "POST" {
		fail(w, 405, "POST required")
		return false
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "application/json required")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, 400, "invalid JSON request: "+e.Error())
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, "trailing JSON")
		return false
	}
	return true
}
func (a *App) validKey(key string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	f := a.files[key]
	return f != nil && a.visibleSessionReports(f, time.Now())
}
func (a *App) apiState(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		fail(w, 405, "GET required")
		return
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	sessions := []Summary{}
	for _, f := range a.files {
		if a.rootEnabled(f.Summary.Home) && a.visibleSessionReports(f, time.Now()) {
			copyF := cloneState(f)
			trimStateReports(copyF, time.Now())
			s := copyF.Summary
			if alias := a.cfg.Aliases[s.Key]; alias != "" {
				s.Title = alias
			}
			sessions = append(sessions, s)
		}
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].FileModified > sessions[j].FileModified })
	key := r.URL.Query().Get("key")
	if key == "" {
		key = a.cfg.Pinned
	}
	if key == "" && len(sessions) > 0 {
		key = sessions[0].Key
	}
	var selected *FileState
	if f := a.files[key]; f != nil && a.visibleSessionReports(f, time.Now()) {
		selected = cloneState(f)
		trimStateReports(selected, time.Now())
	}
	count := len(sessions)
	if len(sessions) > 2000 {
		sessions = sessions[:2000]
	}
	marks := []Mark{}
	for _, m := range a.marks {
		if routeErrorMark(m) || freshReport(m.At, "", time.Now()) {
			marks = append(marks, m)
		}
	}
	if len(marks) > 120 {
		marks = marks[len(marks)-120:]
	}
	// Avoid duplicating mark snapshots in the frequently refreshed response.
	markRows := []Obj{}
	for _, m := range marks {
		markRows = append(markRows, Obj{"id": m.ID, "at": m.At, "note": m.Note, "key": m.Session.Key, "thread_id": m.Session.Thread, "model": m.Session.Model})
	}
	probes := []*Probe{}
	for _, p := range a.probes {
		if p.Status == "RUNNING" || routeErrorProbe(p) || freshReport(first(p.FinishedAt, p.StartedAt), "", time.Now()) {
			probes = append(probes, p)
		}
	}
	if len(probes) > 50 {
		probes = probes[:50]
	}
	floating := a.readFloatingSnapshot()
	thread := ""
	if selected != nil {
		thread = selected.Summary.Thread
	}
	writeJSON(w, Obj{"version": Version, "retention": a.retention, "live_routes": a.liveRouteSnapshot(), "floating_state": floating, "selected_route": a.selectedRouteView(thread, floating), "route_recording": a.routeRecordingStatus(), "routing_bridge": a.bridgeStatus(), "parser_version": ParserVersion, "demo": a.demo, "now": now(), "started_at": a.started.UTC().Format(time.RFC3339), "data_dir": a.data, "config": a.cfg, "sessions": sessions, "session_count": count, "selected": selected, "marks": markRows, "probes": probes, "probe_running": a.probeRunning, "scan": Obj{"at": a.lastScan, "busy": a.scanBusy, "error": a.scanError, "discovered": a.discovered, "bytes_read": a.scanBytes, "duration_ms": a.scanDuration}})
}
func (a *App) apiSettings(w http.ResponseWriter, r *http.Request) {
	if a.preview {
		if r.Method != "GET" {
			fail(w, 409, "synthetic preview uses an isolated empty observation scope")
			return
		}
		a.mu.RLock()
		cfg := a.cfg
		a.mu.RUnlock()
		writeJSON(w, Obj{"config": cfg, "suggested_roots": []string{}, "binaries": []Candidate{}})
		return
	}
	if r.Method == "GET" {
		a.mu.RLock()
		cfg := a.cfg
		a.mu.RUnlock()
		writeJSON(w, Obj{"config": cfg, "suggested_roots": defaultRoots(), "binaries": discoverBinaries()})
		return
	}
	var req struct {
		Roots       []string `json:"roots"`
		PollSeconds int      `json:"poll_seconds"`
		CodexBinary string   `json:"codex_binary"`
	}
	if !body(w, r, &req) {
		return
	}
	if req.PollSeconds < 2 || req.PollSeconds > 60 {
		fail(w, 400, "poll interval must be 2–60 seconds")
		return
	}
	if len(req.Roots) > 8 {
		fail(w, 400, "at most 8 explicit roots")
		return
	}
	roots := []string{}
	seen := map[string]bool{}
	for _, p := range req.Roots {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		abs, e := filepath.Abs(p)
		if e != nil || !isDir(abs) {
			fail(w, 400, "directory does not exist: "+p)
			return
		}
		rel, er := filepath.Rel(abs, a.data)
		if er == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			fail(w, 400, "observed root must not contain application data")
			return
		}
		if !seen[canonical(abs)] {
			seen[canonical(abs)] = true
			roots = append(roots, abs)
		}
	}
	a.mu.Lock()
	cfg := a.cfg
	cfg.Roots = roots
	cfg.PollSeconds = req.PollSeconds
	cfg.CodexBinary = strings.TrimSpace(req.CodexBinary)
	e := saveJSON(filepath.Join(a.data, "settings.json"), cfg)
	if e == nil {
		a.cfg = cfg
	}
	a.mu.Unlock()
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	select {
	case a.scanKick <- struct{}{}:
	default:
	}
	writeJSON(w, Obj{"ok": true})
}
func (a *App) apiExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key          string `json:"key"`
		IncludeNotes bool   `json:"include_notes"`
	}
	if !body(w, r, &req) {
		return
	}
	a.mu.RLock()
	f := a.files[req.Key]
	if f == nil || !a.visibleSessionReports(f, time.Now()) {
		a.mu.RUnlock()
		fail(w, 404, "session not found")
		return
	}
	copyF := cloneState(f)
	trimStateReports(copyF, time.Now())
	summary := copyF.Summary
	marks := []Mark{}
	for _, m := range a.marks {
		if m.Session.Key == req.Key && (routeErrorMark(m) || freshReport(m.At, "", time.Now())) {
			var c Mark
			_ = json.Unmarshal(jsonBytes(m), &c)
			c.Session = redactSummary(c.Session)
			if !req.IncludeNotes {
				c.Note = "[omitted]"
			}
			marks = append(marks, c)
		}
	}
	probes := []Probe{}
	for _, p := range a.probes {
		if p.Request.SessionKey == req.Key && (p.Status == "RUNNING" || routeErrorProbe(p) || freshReport(first(p.FinishedAt, p.StartedAt), "", time.Now())) {
			var c Probe
			_ = json.Unmarshal(jsonBytes(p), &c)
			c.Request.Binary = "[local executable path omitted]"
			c.Request.Home = "[home omitted]"
			probes = append(probes, c)
		}
	}
	a.mu.RUnlock()
	summary = redactSummary(summary)
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	manifest := Obj{"tool_version": Version, "parser_version": ParserVersion, "created_at": now(), "scope": "Local telemetry and explicitly linked independent probes, not a backend model attestation", "redaction": "No prompt/tool bodies/credentials copied; local paths and titles removed; human notes omitted unless requested", "hash_definition": "line_sha256 hashes the whitespace-trimmed source JSONL record; offset is its physical byte offset. Hashes are integrity aids, not third-party authentication.", "files": Obj{}}
	add := func(name string, b []byte) error {
		entry, e := z.Create(name)
		if e != nil {
			return e
		}
		_, e = entry.Write(b)
		obj(manifest["files"])[name] = Obj{"sha256": hashBytes(b), "bytes": len(b)}
		return e
	}
	_ = add("session.json", jsonBytes(summary))
	_ = add("human_marks.json", jsonBytes(marks))
	_ = add("independent_probes.json", jsonBytes(probes))
	liveEvidence, routeTruncated, routeErr := a.routeHistory(copyF.Summary.Thread, 8192)
	if routeErr != nil {
		fail(w, 500, routeErr.Error())
		return
	}
	manifest["routes_export_truncated"] = routeTruncated
	_ = add("original_request_routes.json", jsonBytes(liveEvidence))
	// Stream the persisted telemetry, not the underlying Codex rollout. Deduplicate crash replays.
	a.journalMu.RLock()
	defer a.journalMu.RUnlock()
	hist, e := os.Open(filepath.Join(a.data, "history", req.Key+".jsonl"))
	var histOut bytes.Buffer
	seen := map[string]bool{}
	truncated := false
	if e == nil {
		defer hist.Close()
		br := bufio.NewReader(hist)
		for {
			line, er := br.ReadBytes('\n')
			if len(line) > 0 {
				var ev Event
				if json.Unmarshal(line, &ev) == nil && !seen[ev.ID] && keepEvent(ev, time.Now()) {
					seen[ev.ID] = true
					histOut.Write(jsonBytes(ev))
					histOut.WriteByte('\n')
				}
			}
			if histOut.Len() > 64*1024*1024 {
				truncated = true
				break
			}
			if er != nil {
				break
			}
		}
	}
	manifest["history_export_truncated_at_64MiB"] = truncated
	_ = add("observations.jsonl", histOut.Bytes())
	_ = add("READ_ME.txt", []byte("This archive contains local telemetry, manual observations, and explicitly associated new-session probes. A local model field is not proof of actual weights. A new probe does not reconstruct a historical Desktop turn. response.model is a server-reported label, not independent authentication. No behavior fingerprint is included in v0.1.0. Original rollout content remains on the owner's computer.\n"))
	b, _ := json.MarshalIndent(manifest, "", "  ")
	entry, _ := z.Create("manifest.json")
	_, _ = entry.Write(b)
	if e = z.Close(); e != nil {
		fail(w, 500, e.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="Codex-Evidence-%s-%s.zip"`, time.Now().Format("20060102-150405"), req.Key[:8]))
	_, _ = w.Write(out.Bytes())
}
func redactSummary(s Summary) Summary {
	s.Home = "[home omitted]"
	s.File = filepath.Base(s.File)
	s.CWD = "[cwd omitted]"
	s.Title = "[title omitted]"
	s.ReadError = ""
	return s
}
