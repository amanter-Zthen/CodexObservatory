package main

import (
	"bytes"
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routefeed"
	"codex-observatory/internal/routetrace"
	"codex-observatory/internal/wirecapture"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHeaderlessFallbackEndToEndRestoresDisplayDuringStream(t *testing.T) {
	a, _ := testApp(t)
	syncHealth(t, a, wirecapture.CaptureRevision)
	a.acceptFloatingEvents([]Event{{Kind: "turn_started", Thread: "thread", Turn: "turn", At: now(), Model: "wanted"}})
	sink, err := routefeed.NewSink(func(routetrace.Record) error { return nil }, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close(context.Background())
	runSyncLoop(t, a, sink.Hub)
	proxy, err := wirecapture.Start(routetrace.New(7), sink.Publish, true)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	var calls atomic.Int32
	finish := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(finish) }) }
	defer release()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header()["Content-Type"] = nil
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"real-fixture-response\",\"model\":\"wanted\"}}\n\n")
		w.(http.Flusher).Flush()
		<-finish
		io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"real-fixture-response\",\"model\":\"wanted\"}}\n\n")
	}))
	defer func() { release(); backend.Close() }()
	u, _ := url.Parse(proxy.URL)
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	requestBody := []byte(`{"model":"wanted","stream":true,"client_metadata":{"thread_id":"thread","turn_id":"turn"}}`)
	first, err := client.Post(backend.URL+"/responses", "application/json", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, first.Body)
	first.Body.Close()
	if first.StatusCode != 503 {
		t.Fatal("fixture did not exercise unavailable upstream")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows := a.readFloatingSnapshot().Rows
		if len(rows) == 1 && rows[0].HealthValid && rows[0].Color == floatui.Yellow {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows := a.readFloatingSnapshot().Rows
	if len(rows) != 1 || rows[0].Color != floatui.Yellow {
		t.Fatal("failed request should remain unconfirmed", rows)
	}
	done := make(chan error, 1)
	go func() {
		response, err := client.Post(backend.URL+"/responses", "application/json", bytes.NewReader(requestBody))
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		done <- err
	}()
	defer func() {
		release()
		if err := <-done; err != nil {
			t.Error("repair interrupted the original response", err)
		}
	}()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows = a.readFloatingSnapshot().Rows
		if len(rows) == 1 && rows[0].Color == floatui.Green {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(rows) != 1 || rows[0].Color != floatui.Green || rows[0].Reported != "wanted" || rows[0].Response != "real-fixture-response" {
		t.Fatal("recovered stream did not update live display", rows)
	}
	if calls.Load() != 2 {
		t.Fatal("observer generated requests")
	}
	// Manual repair while the response is still streaming uses the same feed,
	// and must neither stop that stream nor lose its verified label.
	a.requestRouteSync()
	if status := waitSync(t, a); status.State != "synced" {
		t.Fatal(status)
	}
}
