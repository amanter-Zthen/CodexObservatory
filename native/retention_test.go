package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func retentionEvent(id, kind string, at time.Time) Event {
	return Event{ID: id, Kind: kind, At: at.UTC().Format(time.RFC3339Nano), ObservedAt: now(), Detail: Obj{}}
}

func TestRetentionBoundaryAndEvidence(t *testing.T) {
	clock := time.Now()
	boundary := retentionEvent("edge", "usage_snapshot", clock.Add(-reportRetention))
	if !keepEvent(boundary, clock) {
		t.Fatal("exact five-day boundary should be retained")
	}
	old := retentionEvent("old", "error", clock.Add(-reportRetention-time.Nanosecond))
	if keepEvent(old, clock) {
		t.Fatal("ordinary error should expire by event time, even when imported now")
	}
	old.Kind = "model_reroute"
	if keepEvent(old, clock) {
		t.Fatal("empty reroute is not sufficient route evidence")
	}
	old.Detail = Obj{"from_model": "model-a", "to_model": "model-b"}
	if !keepEvent(old, clock) {
		t.Fatal("explicit reroute must survive expiry")
	}
	old.Detail["to_model"] = "MODEL-A"
	if keepEvent(old, clock) {
		t.Fatal("same model should not become a permanent anomaly")
	}
	old.At = "invalid"
	if !keepEvent(old, clock) {
		t.Fatal("invalid timestamp should use observation time")
	}
}

func TestRetentionPrunesJournalAndCacheKeepsCursor(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	key := shortHash("retention-test")
	old := retentionEvent("old", "usage_snapshot", clock.Add(-6*24*time.Hour))
	recent := retentionEvent("recent", "usage_snapshot", clock.Add(-time.Hour))
	route := retentionEvent("route", "model_reroute", clock.Add(-90*24*time.Hour))
	route.Detail = Obj{"from_model": "model-a", "to_model": "model-b"}
	f := newState("home", "file", key)
	f.Offset = 98765
	f.Anchor = "cursor-anchor"
	f.LastMod = 4321
	f.LastSize = 98765
	f.Recent = []Event{old, recent, route}
	f.Summary.LastReroute = &route
	f.Summary.UsageAt = old.At
	f.Summary.Latest = &Usage{Total: 9}
	a.files[key] = f
	p := filepath.Join(a.data, "history", key+".jsonl")
	if e := os.WriteFile(p, append(append(append(jsonBytes(old), '\n'), append(jsonBytes(recent), '\n')...), append(jsonBytes(route), '\n')...), 0600); e != nil {
		t.Fatal(e)
	}
	if e := a.pruneRetention(clock); e != nil {
		t.Fatal(e)
	}
	if a.retention.DeletedEvents != 1 || len(f.Recent) != 2 {
		t.Fatalf("unexpected cleanup: %+v", a.retention)
	}
	if f.Offset != 98765 || f.Anchor != "cursor-anchor" || f.LastMod != 4321 || f.LastSize != 98765 {
		t.Fatal("checkpoint was reset")
	}
	if f.Summary.Latest != nil {
		t.Fatal("expired usage remains in cache")
	}
	h, _, e := a.history(key, 5000)
	if e != nil || len(h) != 2 {
		t.Fatalf("retained history: %v %d", e, len(h))
	}
	archives, _ := filepath.Glob(filepath.Join(a.data, "route-errors", "*.json"))
	if len(archives) != 1 {
		t.Fatal("route evidence not archived")
	}
	before, _ := os.ReadFile(archives[0])
	if e = a.pruneRetention(clock.Add(100 * 24 * time.Hour)); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(archives[0])
	if string(before) != string(after) {
		t.Fatal("permanent evidence was modified")
	}
	h, _, e = a.history(key, 5000)
	if e != nil || len(h) != 1 || h[0].ID != "route" {
		t.Fatal("future cleanup must keep route evidence alone")
	}
}

func TestRetentionProbeAndMarkClassification(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	old := clock.Add(-6 * 24 * time.Hour).Format(time.RFC3339Nano)
	diff := &Probe{ID: "diff", StartedAt: old, Status: "DONE", Conclusion: "SERVER_LABEL_DIFFERENCE", Request: ProbeRequest{Model: "model-a"}, Responses: []ProbeResponse{{Models: []string{"model-b"}}}}
	failed := &Probe{ID: "failed", StartedAt: old, Status: "ERROR", Conclusion: "PROBE_ERROR", Errors: []string{"NETWORK"}}
	variant := &Probe{ID: "variant", StartedAt: old, Status: "DONE", Conclusion: "SERVER_LABEL_VARIANT", Request: ProbeRequest{Model: "model-a"}, Responses: []ProbeResponse{{Models: []string{"model-a-2026-09-01"}}}}
	running := &Probe{ID: "running", StartedAt: old, Status: "RUNNING"}
	a.probes = []*Probe{diff, failed, variant, running}
	for _, p := range a.probes {
		if e := saveJSON(filepath.Join(a.data, "probes", p.ID+".json"), p); e != nil {
			t.Fatal(e)
		}
	}
	a.marks = []Mark{{ID: "old-manual", At: old, Kind: "HUMAN_OBSERVATION"}, {ID: "new-manual", At: now(), Kind: "HUMAN_OBSERVATION"}}
	if e := a.pruneRetention(clock); e != nil {
		t.Fatal(e)
	}
	if len(a.probes) != 2 || a.probes[0].ID != "diff" || a.probes[1].ID != "running" {
		t.Fatal("probe classification wrong")
	}
	if len(a.marks) != 1 || a.marks[0].ID != "new-manual" {
		t.Fatal("manual suspicion should expire normally")
	}
	if _, e := os.Stat(filepath.Join(a.data, "probes", "failed.json")); !os.IsNotExist(e) {
		t.Fatal("expired failed probe remains on disk")
	}
	if _, e := os.Stat(filepath.Join(a.data, "probes", "diff.json")); e != nil {
		t.Fatal("mismatch probe deleted")
	}
}

func TestRetentionOldImportAndRestartDoNotRepopulate(t *testing.T) {
	a, home := testApp(t)
	old := time.Now().Add(-6 * 24 * time.Hour).Format(time.RFC3339Nano)
	writeFixture(t, home, "old.jsonl", fixtureRecord("session_meta", Obj{"id": "old-thread"}, old), fixtureRecord("turn_context", Obj{"model": "model-a"}, old), fixtureRecord("event_msg", Obj{"type": "task_started", "turn_id": "old-turn"}, old))
	a.scanOnce()
	f := oneState(t, a)
	cursor := f.Offset
	h, _, e := a.history(f.Summary.Key, 5000)
	if e != nil || len(h) != 0 {
		t.Fatal("old ordinary imports must not be persisted")
	}
	if e = a.pruneRetention(time.Now()); e != nil {
		t.Fatal(e)
	}
	if e = a.initStore(); e != nil {
		t.Fatal(e)
	}
	a.cfg.Roots = []string{home}
	a.scanOnce()
	f = oneState(t, a)
	if f.Offset != cursor || len(f.Recent) != 0 || visibleReports(f, time.Now()) {
		t.Fatal("restart brought old reports back")
	}
}

func TestRetentionCorruptJournalLeavesOriginalIntact(t *testing.T) {
	a, _ := testApp(t)
	p := filepath.Join(a.data, "history", "bad.jsonl")
	original := []byte("{invalid}\n")
	if e := os.WriteFile(p, original, 0600); e != nil {
		t.Fatal(e)
	}
	if e := a.pruneRetention(time.Now()); e == nil {
		t.Fatal("corrupt report should produce retryable error")
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(original) {
		t.Fatal("failed cleanup changed the journal")
	}
	temps, _ := filepath.Glob(filepath.Join(a.data, "history", ".retention-*"))
	if len(temps) != 0 {
		t.Fatal("temporary files leaked")
	}
}

func TestRetentionJournalReadersAndCleanup(t *testing.T) {
	a, _ := testApp(t)
	key := shortHash("parallel")
	ev := retentionEvent("recent", "usage_snapshot", time.Now())
	if e := os.WriteFile(filepath.Join(a.data, "history", key+".jsonl"), append(jsonBytes(ev), '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if _, _, e := a.history(key, 100); e != nil {
					t.Error(e)
				}
			}
		}()
	}
	for i := 0; i < 10; i++ {
		if e := a.pruneRetention(time.Now()); e != nil {
			t.Fatal(e)
		}
	}
	wg.Wait()
	var stored Event
	b, _ := os.ReadFile(filepath.Join(a.data, "history", key+".jsonl"))
	if json.Unmarshal(b, &stored) != nil || stored.ID != ev.ID {
		t.Fatal("reader or cleanup damaged journal")
	}
}
