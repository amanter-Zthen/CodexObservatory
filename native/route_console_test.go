package main

import (
	"archive/zip"
	"bytes"
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routefeed"
	"codex-observatory/internal/routetrace"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func consoleFixture(a *App, home, thread string) string {
	key := shortHash(thread)
	a.files[key] = &FileState{Summary: Summary{Key: key, Home: home, Thread: thread, At: now(), FileModified: now(), Source: "desktop", Model: "local-setting"}}
	return key
}
func consoleRecord(id, thread string, at time.Time) routetrace.Record {
	return routetrace.Record{ID: id, At: at.UTC().Format(time.RFC3339Nano), Thread: thread, Turn: "fixture-turn", Response: "fixture-response", PID: 7, Requested: "wanted", Reported: "wanted", Stage: "response.created", Verdict: "match", Source: "native_wire_response", Scope: "original_desktop_request"}
}
func consoleSave(t *testing.T, a *App, directory string, r routetrace.Record) {
	t.Helper()
	dir, err := a.reportDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err = saveJSON(filepath.Join(dir, shortHash(r.ID)+".json"), r); err != nil {
		t.Fatal(err)
	}
}

func TestConsoleFeedIsVisibleAndPersistedWhileProducerWriterBlocked(t *testing.T) {
	a, home := testApp(t)
	key := consoleFixture(a, home, "fixture-thread")
	blocked, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	sink, err := routefeed.NewSink(func(routetrace.Record) error { once.Do(func() { close(blocked) }); <-release; return nil }, 8)
	if err != nil {
		t.Fatal(err)
	}
	server, err := routefeed.Listen(sink.Hub)
	if err != nil {
		close(release)
		_ = sink.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() { close(release); _ = server.Close(context.Background()); _ = sink.Close(context.Background()) })
	if err = saveJSON(filepath.Join(a.data, routefeed.DiscoveryFile), server.Description); err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	a.floatingEngine().SetHealth(floatui.Health{At: clock, Ready: true, PIDs: []int{7}})
	r := consoleRecord("fast-route", "fixture-thread", clock)
	sink.Publish([]routetrace.Record{r})
	<-blocked
	done := make(chan struct{})
	go func() { defer close(done); a.liveFeedLoop() }()
	t.Cleanup(func() { a.stop(); <-done })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response := request(a, "GET", "/api/state?key="+key, "", a.token, "", "127.0.0.1:12345")
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
		var state struct {
			Live      []routetrace.Record `json:"live_routes"`
			Selected  SelectedRouteView   `json:"selected_route"`
			Recording struct {
				Writer routefeed.WriterStats `json:"writer"`
			} `json:"route_recording"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if len(state.Live) == 1 && state.Selected.Current != nil && state.Recording.Writer.Saved > 0 {
			if state.Selected.Current.Reported != "wanted" || state.Selected.Current.Color != floatui.Green {
				t.Fatal(state.Selected)
			}
			if sink.Stats().Saved != 0 {
				t.Fatal("producer disk writer was not blocked")
			}
			h := request(a, "GET", "/api/history?key="+key, "", a.token, "", "127.0.0.1:12345")
			var history struct {
				Routes []routetrace.Record `json:"routes"`
			}
			if h.Code != 200 || json.Unmarshal(h.Body.Bytes(), &history) != nil || len(history.Routes) != 1 || history.Routes[0].ID != r.ID {
				t.Fatal(h.Body.String())
			}
			b, _ := testApp(t)
			b.data = a.data
			b.files = a.files
			b.cfg = a.cfg
			b.readLiveRoutes(map[string]bool{})
			b.mu.RLock()
			restored := b.selectedRouteView(r.Thread, floatui.Snapshot{})
			b.mu.RUnlock()
			if restored.Result == nil || restored.Result.Reported != "wanted" {
				t.Fatal("saved fast feed was not recovered", restored)
			}
			t.Logf("feed -> console -> independent disk -> history -> restart: %.1f ms; producer saved=0", float64(time.Since(clock).Microseconds())/1000)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("console or independent audit writer did not receive the fast feed")
}

func TestConsoleHistoryRetainsOldRoutesDeduplicatesAndOrdersSourceTime(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now().UTC().Truncate(time.Second)
	old := consoleRecord("old-visible", "thread", clock.Add(-2*time.Hour))
	consoleSave(t, a, "live-routing-inbox", old)
	consoleSave(t, a, "routing-observations", old)
	expired := consoleRecord("expired", "thread", clock.Add(-6*24*time.Hour))
	consoleSave(t, a, "routing-observations", expired)
	errorRecord := consoleRecord("permanent", "thread", clock.Add(-8*24*time.Hour))
	errorRecord.Reported = "other"
	errorRecord.Verdict = "mismatch"
	if err := a.archiveRouteEvidence("live-route", errorRecord.ID, errorRecord); err != nil {
		t.Fatal(err)
	}
	consoleSave(t, a, "live-routing-inbox", consoleRecord("other-thread", "unrelated", clock))
	zero := consoleRecord("whole-second", "thread", clock.Add(-time.Second))
	zero.At = clock.Add(-time.Second).Format(time.RFC3339)
	fraction := consoleRecord("fraction", "thread", clock.Add(-time.Second+100*time.Millisecond))
	consoleSave(t, a, "routing-observations", zero)
	consoleSave(t, a, "routing-observations", fraction)
	rows, truncated, err := a.routeHistory("thread", 1200)
	if err != nil || truncated || len(rows) != 4 {
		t.Fatalf("rows=%+v truncated=%v err=%v", rows, truncated, err)
	}
	if rows[0].ID != "permanent" || rows[1].ID != "old-visible" || rows[2].ID != "whole-second" || rows[3].ID != "fraction" {
		t.Fatal(rows)
	}
	rows, truncated, err = a.routeHistory("thread", 2)
	if err != nil || !truncated || len(rows) != 2 || rows[1].ID != "fraction" {
		t.Fatal(rows, truncated, err)
	}
	rows, _, err = a.routeHistory("none", 1200)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

func TestConsoleNewTurnDoesNotBorrowHistoricalResponse(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	a.floatingEngine().SetHealth(floatui.Health{At: clock, Ready: true, PIDs: []int{7}})
	old := consoleRecord("old-response", "thread", clock.Add(-2*time.Second))
	a.acceptFloatingRoute(old)
	next := old
	next.ID = "new-request"
	next.Turn = "new-turn"
	next.Response = ""
	next.Reported = ""
	next.Requested = "new-model"
	next.Stage = "request.sent"
	next.Verdict = "pending"
	next.Source = "native_wire_request"
	next.At = clock.Format(time.RFC3339Nano)
	a.acceptFloatingRoute(next)
	a.publishFloating()
	a.mu.RLock()
	view := a.selectedRouteView("thread", a.readFloatingSnapshot())
	a.mu.RUnlock()
	if view.Current == nil || view.Current.Color != floatui.Yellow || view.Current.Reported != "" || view.Current.Requested != "new-model" {
		t.Fatal(view)
	}
	if view.Result == nil || view.Result.ID != old.ID || view.Latest == nil || view.Latest.ID != next.ID {
		t.Fatal(view)
	}
	// Fractional source times must supersede a whole-second timestamp.
	whole := consoleRecord("whole", "another", clock.Truncate(time.Second).Add(-time.Second))
	whole.At = routeAt(whole).Format(time.RFC3339)
	fraction := whole
	fraction.ID = "fraction"
	fraction.At = routeAt(whole).Add(100 * time.Millisecond).Format(time.RFC3339Nano)
	a.cacheConsoleRoute(whole)
	a.cacheConsoleRoute(fraction)
	if a.liveRoutes["another"].ID != "fraction" {
		t.Fatal(a.liveRoutes["another"])
	}
}

func TestConsolePermanentErrorRemainsDiscoverableAndExportable(t *testing.T) {
	a, home := testApp(t)
	key := consoleFixture(a, home, "thread")
	a.files[key].Summary.At = time.Now().Add(-7 * 24 * time.Hour).Format(time.RFC3339Nano)
	errRecord := consoleRecord("permanent", "thread", time.Now().Add(-7*24*time.Hour))
	errRecord.Reported = "other"
	errRecord.Verdict = "mismatch"
	if err := a.archiveRouteEvidence("live-route", errRecord.ID, errRecord); err != nil {
		t.Fatal(err)
	}
	a.readLiveRoutes(map[string]bool{})
	state := request(a, "GET", "/api/state?key="+key, "", a.token, "", "127.0.0.1:12345")
	var payload struct {
		Count    int               `json:"session_count"`
		Selected SelectedRouteView `json:"selected_route"`
	}
	if state.Code != 200 || json.Unmarshal(state.Body.Bytes(), &payload) != nil || payload.Count != 1 || payload.Selected.Result == nil {
		t.Fatal(state.Body.String())
	}
	consoleSave(t, a, "routing-observations", consoleRecord("recent", "thread", time.Now().Add(-2*time.Hour)))
	export := request(a, "POST", "/api/export", string(jsonBytes(Obj{"key": key})), a.token, "", "127.0.0.1:12345")
	if export.Code != 200 {
		t.Fatal(export.Body.String())
	}
	z, err := zip.NewReader(bytes.NewReader(export.Body.Bytes()), int64(export.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range z.File {
		if file.Name == "original_request_routes.json" {
			r, _ := file.Open()
			b, _ := io.ReadAll(r)
			_ = r.Close()
			var rows []routetrace.Record
			if json.Unmarshal(b, &rows) != nil || len(rows) != 2 {
				t.Fatal(string(b))
			}
			return
		}
	}
	t.Fatal("route evidence absent from export")
}

func TestConsoleOwnedReportRetentionAndWriterErrors(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	expired := consoleRecord("expired", "thread", clock.Add(-6*24*time.Hour))
	a.liveRoutes = map[string]routetrace.Record{"expired": expired}
	a.routeResults = map[string]routetrace.Record{"expired": expired}
	consoleSave(t, a, "routing-observations", expired)
	permanent := expired
	permanent.ID = "permanent"
	permanent.Reported = "other"
	permanent.Verdict = "mismatch"
	consoleSave(t, a, "routing-observations", permanent)
	if err := a.pruneRetention(clock); err != nil {
		t.Fatal(err)
	}
	if len(a.liveRoutes) != 0 || len(a.routeResults) != 0 {
		t.Fatal("expired route cache not pruned")
	}
	if _, err := os.Stat(filepath.Join(a.data, "routing-observations", shortHash(expired.ID)+".json")); !os.IsNotExist(err) {
		t.Fatal("ordinary report not expired", err)
	}
	if rows, _, err := a.routeHistory("thread", 1200); err != nil || len(rows) != 1 || rows[0].ID != "permanent" {
		t.Fatal(rows, err)
	}
	b, _ := testApp(t)
	if err := os.WriteFile(filepath.Join(b.data, "routing-observations"), []byte("not-directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if b.routeAuditSink() != nil || b.routeRecordingStatus()["error"] == nil {
		t.Fatal("writer initialization error was hidden")
	}
}
