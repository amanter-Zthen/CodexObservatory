package main

import (
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routefeed"
	"codex-observatory/internal/routetrace"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func syncHealth(t *testing.T, a *App, revision string) {
	t.Helper()
	if err := saveJSON(filepath.Join(a.data, "routing-bridge-status", "fixture.json"), Obj{"at": now(), "active": true, "client_pid": 7, "capture_revision": revision}); err != nil {
		t.Fatal(err)
	}
	a.refreshFloating()
}

func syncRecord(id, stage, reported, verdict string, at time.Time) routetrace.Record {
	r := routetrace.Record{ID: id, At: at.Format(time.RFC3339Nano), Thread: "thread", Turn: "turn", PID: 7, Requested: "wanted", Reported: reported, Stage: stage, Verdict: verdict, Source: "native_wire_response", Scope: "original_desktop_request", Transport: "sse"}
	if stage == "request.sent" {
		r.Source = "native_wire_request"
	} else {
		r.Response = "response-" + id
	}
	return r
}

func runSyncLoop(t *testing.T, a *App, hub *routefeed.Hub) *routefeed.Server {
	t.Helper()
	server, err := routefeed.Listen(hub)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveJSON(filepath.Join(a.data, routefeed.DiscoveryFile), server.Description); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); a.liveFeedLoop() }()
	t.Cleanup(func() { a.stop(); <-done; server.Close(context.Background()) })
	return server
}

func waitSync(t *testing.T, a *App) RouteSyncStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for a.routeSyncBusy.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if a.routeSyncBusy.Load() {
		t.Fatal("resync remained busy")
	}
	return a.routeSyncStatus()
}

func TestRouteSyncCoalescesRapidClicks(t *testing.T) {
	a, _ := testApp(t)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if a.requestRouteSync() {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 || len(a.routeSyncWake) != 1 || a.routeSyncStatus().State != "syncing" {
		t.Fatal("repeated clicks queued duplicate work", accepted.Load())
	}
}

func TestRouteSyncReplaysEvidenceWithoutClearingAnomalyOrCompletion(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	syncHealth(t, a, "native-home-v2-sse")
	a.acceptFloatingEvents([]Event{{Kind: "turn_started", Thread: "thread", Turn: "turn", At: clock.Format(time.RFC3339Nano), Model: "wanted"}})
	anomaly := syncRecord("mismatch", "response.created", "other", "mismatch", clock.Add(time.Millisecond))
	a.acceptFloatingRoute(anomaly)
	a.observeCompletion("human-other", "completed-turn", "finish", "completed", "wanted", clock)
	completionBefore, _ := a.completionCount()
	hub, err := routefeed.NewHub(8)
	if err != nil {
		t.Fatal(err)
	}
	good := syncRecord("latest", "response.created", "wanted", "match", clock.Add(2*time.Millisecond))
	hub.Publish([]routetrace.Record{good})
	runSyncLoop(t, a, hub)
	a.requestRouteSync()
	if status := waitSync(t, a); status.State != "synced_with_anomaly" {
		t.Fatal(status)
	}
	rows := a.readFloatingSnapshot().Rows
	if len(rows) != 1 || rows[0].Route != floatui.Match || rows[0].Color != floatui.Red || !rows[0].Anomaly {
		t.Fatal("resync concealed earlier mismatch", rows)
	}
	if count, _ := a.completionCount(); count != completionBefore {
		t.Fatal("resync changed completion inbox")
	}
}

func TestRouteSyncDistinguishesOldCollectorFromMissingEvidence(t *testing.T) {
	for _, revision := range []string{"native-home-v1", "native-home-v2-sse"} {
		t.Run(revision, func(t *testing.T) {
			a, _ := testApp(t)
			syncHealth(t, a, revision)
			hub, _ := routefeed.NewHub(8)
			hub.Publish([]routetrace.Record{syncRecord("pending", "request.sent", "", "pending", time.Now())})
			runSyncLoop(t, a, hub)
			a.requestRouteSync()
			want := "awaiting_evidence"
			if revision == "native-home-v1" {
				want = "collector_update_required"
			}
			if status := waitSync(t, a); status.State != want {
				t.Fatal(status)
			}
			rows := a.readFloatingSnapshot().Rows
			if len(rows) != 1 || rows[0].Color != floatui.Yellow || rows[0].Reported != "" {
				t.Fatal("reset fabricated a match", rows)
			}
		})
	}
}

func TestRouteSyncTimeoutThenAutomaticFeedRecovery(t *testing.T) {
	a, _ := testApp(t)
	syncHealth(t, a, "native-home-v2-sse")
	hub, _ := routefeed.NewHub(8)
	server := runSyncLoop(t, a, hub)
	server.Close(context.Background())
	a.requestRouteSync()
	if status := waitSync(t, a); status.State != "unavailable" {
		t.Fatal(status)
	}
	// A new descriptor appears on an unrelated ephemeral loopback port. No
	// process restart is needed, and no control endpoint is contacted.
	replacement, err := routefeed.Listen(hub)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close(context.Background())
	saveJSON(filepath.Join(a.data, routefeed.DiscoveryFile), replacement.Description)
	hub.Publish([]routetrace.Record{syncRecord("recovered", "response.created", "wanted", "match", time.Now())})
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		rows := a.readFloatingSnapshot().Rows
		if len(rows) == 1 && rows[0].Color == floatui.Green {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("feed did not recover automatically")
}

func TestRouteSyncAPIUsesExistingAuthorizationAndPost(t *testing.T) {
	a, _ := testApp(t)
	for _, tc := range []struct {
		method, token string
		want          int
	}{{"POST", "wrong", 403}, {"GET", a.token, 405}} {
		response := request(a, tc.method, "/api/routing/resync", "{}", tc.token, "", "127.0.0.1:12345")
		if response.Code != tc.want || a.routeSyncBusy.Load() {
			t.Fatal("unauthorized/read request changed subscription", response.Code)
		}
	}
	response := request(a, "POST", "/api/routing/resync", "{}", a.token, "", "127.0.0.1:12345")
	if response.Code != 200 || !a.routeSyncBusy.Load() {
		t.Fatal("authorized resync was not queued", response.Code)
	}
}

func TestRouteSyncSubscriberOnlyUsesReadOnlyFeedRequests(t *testing.T) {
	a, _ := testApp(t)
	hub, _ := routefeed.NewHub(8)
	var wrong atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/events" {
			wrong.Store(true)
		}
		routefeed.Handler(hub, "0123456789012345678901234567890123456789012345678901234567890123").ServeHTTP(w, r)
	}))
	defer server.Close()
	d := routefeed.Description{URL: server.URL + "/events", Version: 1, Token: "0123456789012345678901234567890123456789012345678901234567890123"}
	saveJSON(filepath.Join(a.data, routefeed.DiscoveryFile), d)
	done := make(chan struct{})
	go func() { defer close(done); a.liveFeedLoop() }()
	defer func() { a.stop(); <-done }()
	a.requestRouteSync()
	waitSync(t, a)
	if wrong.Load() {
		t.Fatal("resync used a mutation/control request")
	}
}
