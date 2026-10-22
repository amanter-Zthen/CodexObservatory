package main

import (
	"bufio"
	"codex-observatory/internal/routetrace"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

const reportRetention = 5 * 24 * time.Hour

type RetentionStatus struct {
	Days          int    `json:"days"`
	CheckedAt     string `json:"checked_at"`
	DeletedEvents int    `json:"deleted_events"`
	DeletedProbes int    `json:"deleted_probes"`
	DeletedMarks  int    `json:"deleted_marks"`
	PermanentRule string `json:"permanent_rule"`
	Error         string `json:"error,omitempty"`
}

func freshReport(at, fallback string, clock time.Time) bool {
	stamp, e := time.Parse(time.RFC3339Nano, at)
	if e != nil {
		stamp, e = time.Parse(time.RFC3339Nano, fallback)
	}
	return e == nil && !stamp.Before(clock.Add(-reportRetention))
}

func routeErrorEvent(e Event) bool {
	from, to := strings.TrimSpace(str(e.Detail["from_model"])), strings.TrimSpace(str(e.Detail["to_model"]))
	return e.Kind == "model_reroute" && from != "" && to != "" && !strings.EqualFold(from, to)
}

func routeErrorProbe(p *Probe) bool {
	return p != nil && p.Status != "RUNNING" && p.Conclusion == "SERVER_LABEL_DIFFERENCE" && probeConclusion(p) == "SERVER_LABEL_DIFFERENCE"
}

func routeErrorMark(m Mark) bool {
	if m.Session.LastReroute != nil && routeErrorEvent(*m.Session.LastReroute) {
		return true
	}
	for _, e := range m.Nearby {
		if routeErrorEvent(e) {
			return true
		}
	}
	return false
}

func keepEvent(e Event, clock time.Time) bool {
	return routeErrorEvent(e) || freshReport(e.At, e.ObservedAt, clock)
}

func trimStateReports(f *FileState, clock time.Time) {
	recent := []Event{}
	for _, e := range f.Recent {
		if keepEvent(e, clock) {
			recent = append(recent, e)
		}
	}
	f.Recent = recent
	s := &f.Summary
	if s.LastError != nil && !keepEvent(*s.LastError, clock) {
		s.LastError = nil
	}
	if s.LastReroute != nil && !keepEvent(*s.LastReroute, clock) {
		s.LastReroute = nil
	}
	if !freshReport(s.UsageAt, "", clock) {
		s.Latest, s.Total = nil, nil
		s.ContextValid, s.ContextWindow, s.UsageAt = false, 0, ""
	}
	for id, q := range s.Quotas {
		if !freshReport(q.At, "", clock) {
			delete(s.Quotas, id)
		}
	}
	// Model/settings and file cursors are parser checkpoints for continuation.
	// Old checkpoints are excluded from dashboard and export report listings.
}

func visibleReports(f *FileState, clock time.Time) bool {
	if freshReport(f.Summary.At, "", clock) {
		return true
	}
	if f.Summary.LastReroute != nil && routeErrorEvent(*f.Summary.LastReroute) {
		return true
	}
	for _, e := range f.Recent {
		if keepEvent(e, clock) {
			return true
		}
	}
	return false
}

// Operate on exact application-owned directories. Following an external link
// would make it somebody else's data rather than this app's report store.
func (a *App) reportDirectory(name string) (string, error) {
	base, e := filepath.EvalSymlinks(a.data)
	if e != nil {
		return "", e
	}
	p := filepath.Join(a.data, name)
	if e = os.MkdirAll(p, 0700); e != nil {
		return "", e
	}
	resolved, e := filepath.EvalSymlinks(p)
	if e != nil {
		return "", e
	}
	if canonical(resolved) != canonical(filepath.Join(base, name)) {
		return "", fmt.Errorf("report directory points outside application data: %s", name)
	}
	return p, nil
}

func (a *App) archiveRouteEvidence(kind, id string, value any) error {
	dir, e := a.reportDirectory("route-errors")
	if e != nil {
		return e
	}
	if id == "" {
		id = hashBytes(jsonBytes(value))
	}
	path := filepath.Join(dir, shortHash(kind+":"+id)+".json")
	if st, er := os.Lstat(path); er == nil {
		if !st.Mode().IsRegular() {
			return fmt.Errorf("route evidence is not a regular file")
		}
		return nil
	} else if !errors.Is(er, os.ErrNotExist) {
		return er
	}
	return saveJSON(path, Obj{"kind": kind, "archived_at": now(), "retention": "permanent_explicit_route_error", "report": value})
}

// Scanner and cleanup share one journal lock. Replacement is atomic, and a
// failed rewrite leaves the original report intact and readable for retry.
func (a *App) pruneJournal(path string, clock time.Time) (deleted int, err error) {
	in, e := os.Open(path)
	if e != nil {
		return 0, e
	}
	defer in.Close()
	out, e := os.CreateTemp(filepath.Dir(path), ".retention-*")
	if e != nil {
		return 0, e
	}
	temp := out.Name()
	defer func() { out.Close(); os.Remove(temp) }()
	w := bufio.NewWriter(out)
	r := bufio.NewReader(in)
	for {
		line, er := r.ReadBytes('\n')
		if len(line) > 0 {
			var event Event
			if json.Unmarshal(line, &event) != nil || event.ID == "" {
				return 0, fmt.Errorf("invalid report record; journal left intact")
			}
			if routeErrorEvent(event) {
				if e = a.archiveRouteEvidence("event", event.ID, event); e != nil {
					return 0, e
				}
			}
			if keepEvent(event, clock) {
				if _, e = w.Write(append(jsonBytes(event), '\n')); e != nil {
					return 0, e
				}
			} else {
				deleted++
			}
		}
		if er != nil {
			if er != io.EOF {
				return 0, er
			}
			break
		}
	}
	if deleted == 0 {
		return 0, nil
	}
	if e = w.Flush(); e != nil {
		return 0, e
	}
	if e = out.Sync(); e != nil {
		return 0, e
	}
	if e = out.Close(); e != nil {
		return 0, e
	}
	if e = in.Close(); e != nil {
		return 0, e
	}
	if e = replaceFile(temp, path); e != nil {
		return 0, e
	}
	return deleted, nil
}

func (a *App) pruneRetention(clock time.Time) (err error) {
	a.journalMu.Lock()
	defer a.journalMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	status := RetentionStatus{Days: 5, CheckedAt: clock.UTC().Format(time.RFC3339Nano), PermanentRule: "explicit_model_reroute_or_server_label_difference"}
	defer func() {
		if err != nil {
			status.Error = err.Error()
		}
		a.retention = status
	}()
	if e := a.pruneLiveReports(clock); e != nil {
		return e
	}
	for _, cache := range []map[string]routetrace.Record{a.liveRoutes, a.routeResults} {
		for thread, row := range cache {
			if !freshReport(row.At, "", clock) && !liveRouteError(row) {
				delete(cache, thread)
			}
		}
	}
	historyDir, e := a.reportDirectory("history")
	if e != nil {
		return e
	}
	sourceDir, e := a.reportDirectory("sources")
	if e != nil {
		return e
	}
	probeDir, e := a.reportDirectory("probes")
	if e != nil {
		return e
	}
	paths, e := filepath.Glob(filepath.Join(historyDir, "*.jsonl"))
	if e != nil {
		return e
	}
	for _, p := range paths {
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("history report is not a regular file")
		}
		n, e := a.pruneJournal(p, clock)
		if e != nil {
			return e
		}
		status.DeletedEvents += n
	}
	for key, f := range a.files {
		if f.Summary.LastReroute != nil && routeErrorEvent(*f.Summary.LastReroute) {
			if e = a.archiveRouteEvidence("event", f.Summary.LastReroute.ID, *f.Summary.LastReroute); e != nil {
				return e
			}
		}
		before := jsonBytes(f)
		trimStateReports(f, clock)
		if string(before) != string(jsonBytes(f)) {
			if e = saveJSON(filepath.Join(sourceDir, key+".json"), f); e != nil {
				return e
			}
		}
	}
	probes := []*Probe{}
	for _, p := range a.probes {
		if routeErrorProbe(p) {
			if e = a.archiveRouteEvidence("probe", p.ID, p); e != nil {
				return e
			}
			probes = append(probes, p)
		} else if p.Status == "RUNNING" || freshReport(first(p.FinishedAt, p.StartedAt), "", clock) {
			probes = append(probes, p)
		} else {
			// Report filenames are generated hex IDs; derive no path from a report payload.
			paths, e := filepath.Glob(filepath.Join(probeDir, "*.json"))
			if e != nil {
				return e
			}
			for _, path := range paths {
				var stored Probe
				st, er := os.Lstat(path)
				if er != nil {
					return er
				}
				if !st.Mode().IsRegular() {
					return fmt.Errorf("probe report is not a regular file")
				}
				if loadJSON(path, &stored) == nil && stored.ID == p.ID {
					if e = os.Remove(path); e != nil {
						return e
					}
				}
			}
			status.DeletedProbes++
		}
	}
	a.probes = probes
	marks := []Mark{}
	for _, m := range a.marks {
		if routeErrorMark(m) {
			if e = a.archiveRouteEvidence("mark", m.ID, m); e != nil {
				return e
			}
			marks = append(marks, m)
		} else if freshReport(m.At, "", clock) {
			f := FileState{Summary: m.Session, Recent: m.Nearby}
			trimStateReports(&f, clock)
			m.Session, m.Nearby = f.Summary, f.Recent
			marks = append(marks, m)
		} else {
			status.DeletedMarks++
		}
	}
	if !reflect.DeepEqual(a.marks, marks) {
		if e = saveJSON(filepath.Join(a.data, "marks.json"), marks); e != nil {
			return e
		}
		a.marks = marks
	}
	return nil
}
