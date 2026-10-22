package main

import (
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routetrace"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyntheticPreviewScopeAndProbeIsolation(t *testing.T) {
	a, _ := testApp(t)
	a.preview = true
	a.cfg.Roots = nil
	if _, err := a.startProbe(ProbeRequest{Confirm: true}); err == nil || !strings.Contains(err.Error(), "synthetic") {
		t.Fatal("synthetic preview allowed a real probe", err)
	}
	w := httptest.NewRecorder()
	a.apiSettings(w, httptest.NewRequest("POST", "/api/settings", strings.NewReader(`{"roots":["real-home"]}`)))
	if w.Code != 409 || len(a.cfg.Roots) != 0 {
		t.Fatal("preview observation scope changed", w.Code, a.cfg.Roots)
	}
	for _, row := range []struct {
		at    float64
		count int
	}{{0, 0}, {3, 1}, {10, 2}, {18, 3}, {29, 2}, {32, 0}} {
		s := floatingPreviewSnapshot(row.at)
		if s.Mode != "synthetic" || len(s.Rows) != row.count || !s.Quota.Known {
			t.Fatal(row, s)
		}
	}
}

func TestFloatingAdapterAcceptsLiveBeforeReportFileExists(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	a.floatingEngine().SetHealth(floatui.Health{At: clock, Ready: true, PIDs: []int{7}})
	r := routetrace.Record{ID: "one", At: clock.Format(time.RFC3339Nano), Thread: "thread", Turn: "turn", Response: "response", PID: 7, Requested: "wanted", Reported: "wanted", Stage: "response.created", Verdict: "match", Source: "native_wire_response", Scope: "original_desktop_request"}
	a.acceptFloatingRoute(r)
	a.publishFloating()
	if rows := a.readFloatingSnapshot().Rows; len(rows) != 1 || rows[0].Color != floatui.Green {
		t.Fatal(rows)
	}
	files, _ := filepath.Glob(filepath.Join(a.data, "live-routing-inbox", "*.json"))
	if len(files) != 0 {
		t.Fatal("live adapter needed a report file")
	}
}
func TestMirrorLifecycleDeduplicatedAndOldHistoryExcluded(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	e := Event{ID: "file-one", Thread: "thread", Turn: "turn", Kind: "turn_started", At: clock.Format(time.RFC3339Nano), LineSHA256: "same-source-line", Model: "wanted"}
	a.acceptFloatingEvents([]Event{e})
	e.ID = "mirror-two"
	a.acceptFloatingEvents([]Event{e})
	e.Thread = "old"
	e.At = a.started.Add(-time.Hour).Format(time.RFC3339Nano)
	a.acceptFloatingEvents([]Event{e})
	a.publishFloating()
	if view := a.readFloatingSnapshot(); len(view.Rows) != 1 || view.Diagnostics.Duplicates != 1 {
		t.Fatal(view)
	}
}

func TestRestartReplayRestoresCompletedLifecycleInsteadOfGhostActivity(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	end := Event{Thread: "thread", Turn: "turn", Kind: "turn_completed", At: clock.Add(-time.Minute).Format(time.RFC3339Nano), LineSHA256: "terminal"}
	a.acceptFloatingEvents([]Event{end})
	a.acceptFloatingRoute(routetrace.Record{ID: "old-response", At: clock.Add(-2 * time.Minute).Format(time.RFC3339Nano), Thread: "thread", Turn: "turn", Response: "response", PID: 7, Requested: "wanted", Reported: "wanted", Stage: "response.completed", Verdict: "match", Source: "native_wire_response", Scope: "original_desktop_request"})
	a.publishFloating()
	if len(a.readFloatingSnapshot().Rows) != 0 {
		t.Fatal("completed history became a live conversation")
	}
}
