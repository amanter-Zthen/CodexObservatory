package routefeed

import (
	"codex-observatory/internal/routetrace"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func record(i int, stage string) routetrace.Record {
	return routetrace.Record{ID: fmt.Sprint(i), At: time.Date(2026, 9, 30, 8, 0, i, 0, time.UTC).Format(time.RFC3339Nano), Thread: "thread", Turn: "turn", Response: "response", Stage: stage, Requested: "wanted", Reported: "wanted", Verdict: "match", Scope: "original_desktop_request"}
}
func TestBlockedReportWriterDoesNotBlockLiveFeed(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s, err := NewSink(func(routetrace.Record) error { once.Do(func() { close(entered) }); <-release; return nil }, 8)
	if err != nil {
		t.Fatal(err)
	}
	s.Publish([]routetrace.Record{record(0, "response.created")})
	<-entered
	done := make(chan struct{})
	go func() { s.Publish([]routetrace.Record{record(1, "response.completed")}); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("live path waited for disk")
	}
	b := s.Hub.Read("", 0)
	if !b.Reset || len(b.Items) != 2 || b.Items[1].Record.Stage != "response.completed" {
		t.Fatal(b)
	}
	close(release)
	if err = s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Stats().Saved != 2 {
		t.Fatal(s.Stats())
	}
}
func TestReconnectKeepsStartCompletionAndAnomaly(t *testing.T) {
	h, _ := NewHub(8)
	begin := record(0, "request.sent")
	begin.Response = ""
	begin.Reported = ""
	begin.Verdict = "pending"
	h.Publish([]routetrace.Record{begin})
	bad := record(1, "response.created")
	bad.Reported = "other"
	bad.Verdict = "mismatch"
	h.Publish([]routetrace.Record{bad})
	for i := 2; i < 20; i++ {
		r := record(i, "response.output_text.delta")
		h.Publish([]routetrace.Record{r})
	}
	h.Publish([]routetrace.Record{record(20, "response.completed")})
	b := h.Read("", 0)
	stages := map[string]bool{}
	mismatch := false
	for _, item := range b.Items {
		stages[item.Record.Stage] = true
		mismatch = mismatch || item.Record.Verdict == "mismatch"
	}
	if !b.Reset || !stages["request.sent"] || !stages["response.created"] || !stages["response.completed"] || !mismatch {
		t.Fatal(b)
	}
	if !h.Read(b.Epoch, 1).Reset {
		t.Fatal("cursor gap silently ignored")
	}
	if len(h.Read(b.Epoch, b.Sequence).Items) != 0 {
		t.Fatal("duplicate incremental batch")
	}
}
func TestSlowWriterCoalescesProgressButPreservesCompletion(t *testing.T) {
	var mu sync.Mutex
	written := []string{}
	s, _ := NewSink(func(r routetrace.Record) error {
		mu.Lock()
		written = append(written, r.Stage)
		mu.Unlock()
		return nil
	}, 8)
	for i := 0; i < 100; i++ {
		s.Publish([]routetrace.Record{record(i, "response.output_text.delta")})
	}
	s.Publish([]routetrace.Record{record(100, "response.completed")})
	s.Close(context.Background())
	stats := s.Stats()
	if stats.ProgressCoalesced < 90 || stats.WriteErrors != 0 {
		t.Fatal(stats)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(written) != 2 {
		t.Fatal(written)
	}
}
func TestCapacityFailureIsVisibleAndLiveEvidenceRetained(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s, _ := NewSink(func(r routetrace.Record) error {
		once.Do(func() { close(entered) })
		<-release
		return errors.New("fixture disk failure")
	}, 8)
	s.Publish([]routetrace.Record{record(0, "response.created")})
	<-entered
	for i := 1; i < 20; i++ {
		s.Publish([]routetrace.Record{record(i, "response.completed")})
	}
	if s.Stats().CriticalOverruns == 0 || s.Hub.Read("", 0).Sequence != 20 {
		close(release)
		t.Fatal(s.Stats())
	}
	close(release)
	s.Close(context.Background())
	if s.Stats().WriteErrors == 0 {
		t.Fatal("write failure concealed")
	}
}
func TestAuthenticatedFeedAndLocalOnlyDiscovery(t *testing.T) {
	h, _ := NewHub(8)
	h.Publish([]routetrace.Record{record(0, "response.created")})
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	server := httptest.NewServer(Handler(h, token))
	defer server.Close()
	d := Description{URL: server.URL + "/events", Token: token, Version: 1}
	b, err := Fetch(context.Background(), server.Client(), d, "", 0)
	if err != nil || len(b.Items) != 1 {
		t.Fatal(b, err)
	}
	r, _ := http.NewRequest("GET", d.URL+"?after=0", nil)
	resp, _ := server.Client().Do(r)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal(resp.StatusCode)
	}
	d.URL = "https://example.com/events"
	if ValidDescription(d) {
		t.Fatal("nonlocal discovery accepted")
	}
}
func TestUnassociatedMetadataExcludedAndTurnReset(t *testing.T) {
	h, _ := NewHub(8)
	r := record(0, "response.created")
	r.Scope = "probe"
	h.Publish([]routetrace.Record{r})
	if h.Read("", 0).Sequence != 0 {
		t.Fatal("probe entered original feed")
	}
	r = record(1, "response.created")
	r.Verdict = "mismatch"
	r.Reported = "other"
	h.Publish([]routetrace.Record{r})
	r = record(2, "request.sent")
	r.Turn = "next"
	r.Response = ""
	r.Reported = ""
	r.Verdict = "pending"
	h.Publish([]routetrace.Record{r})
	for _, item := range h.Read("", 0).Items {
		if item.Record.Turn != "next" {
			t.Fatal("old turn checkpoint survived", item)
		}
	}
}
