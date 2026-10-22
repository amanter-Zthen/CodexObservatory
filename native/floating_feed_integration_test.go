package main

import (
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routefeed"
	"codex-observatory/internal/routetrace"
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Synthetic protocol integration: real HTTP feed + actual App consumer + UI
// projector/paint list. This is not a real Codex request or Windows execution.
func TestHTTPFeedToPaintingWhileReportWriterBlocked(t *testing.T) {
	a, _ := testApp(t)
	blocked, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	sink, err := routefeed.NewSink(func(routetrace.Record) error { once.Do(func() { close(blocked) }); <-release; return nil }, 8)
	if err != nil {
		t.Fatal(err)
	}
	server, err := routefeed.Listen(sink.Hub)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	t.Cleanup(func() { close(release); _ = server.Close(context.Background()); _ = sink.Close(context.Background()) })
	if err = saveJSON(filepath.Join(a.data, routefeed.DiscoveryFile), server.Description); err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	a.floatingEngine().SetHealth(floatui.Health{At: clock, Ready: true, PIDs: []int{7}})
	a.floatingEngine().ObserveQuota(floatui.QuotaEvent{Bucket: "fixture", Source: "synthetic-protocol", At: clock, WindowMinutes: 10080, Used: 13})
	sink.Publish([]routetrace.Record{{ID: "synthetic-wire", At: clock.Format(time.RFC3339Nano), Thread: "fixture-thread", Turn: "fixture-turn", Response: "fixture-response", PID: 7, Requested: "fixture-target", Reported: "fixture-other", Stage: "response.created", Verdict: "mismatch", Source: "native_wire_response", Scope: "original_desktop_request"}})
	<-blocked
	done := make(chan struct{})
	go func() { defer close(done); a.liveFeedLoop() }()
	t.Cleanup(func() { a.stop(); <-done })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s := a.readFloatingSnapshot()
		if len(s.Rows) == 1 {
			if s.Rows[0].Color != floatui.Red || s.Rows[0].Model != "fixture-other" || s.Quota.Remaining != 87 {
				t.Fatal(s)
			}
			if sink.Stats().Saved != 0 {
				t.Fatal("test did not hold the disk writer")
			}
			animator := floatui.NewAnimator()
			animator.Set(s, 0, 340, 0)
			paint := floatui.NewPainter().Paint(animator.Frame(1))
			found := false
			for _, l := range paint.Labels {
				if l.Text == "fixture-other" && l.Color == floatui.RouteInk(floatui.Red) {
					found = true
				}
			}
			if !found {
				t.Fatal("response label not in native paint list")
			}
			t.Logf("synthetic HTTP feed -> snapshot -> paint while Saved=0; observed_ms=%.2f", float64(time.Since(clock).Microseconds())/1000)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("HTTP feed failed to update display state")
}
