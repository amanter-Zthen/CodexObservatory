package main

import (
	"codex-observatory/internal/floatui"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Explicit opt-in synthetic server for browser verification. Its own temporary
// home and random loopback port keep it independent of the installed monitor.
func TestConsoleBrowserFixture(t *testing.T) {
	control := os.Getenv("OBS_CONSOLE_FIXTURE_CONTROL")
	if control == "" {
		t.Skip("browser fixture requires explicit isolated control directory")
	}
	a, home := testApp(t)
	a.demo = true
	key := consoleFixture(a, home, "fixture-main")
	second := consoleFixture(a, home, "fixture-second")
	a.files[key].Summary.Title = "合成测试 · 主对话"
	a.files[second].Summary.Title = "合成测试 <img src=x onerror=alert(1)>"
	a.cfg.Aliases[key] = a.files[key].Summary.Title
	a.cfg.Aliases[second] = a.files[second].Summary.Title
	a.cfg.Pinned = key
	applyThreadServiceTier(&a.files[key].Summary, Obj{"service_tier": "default"}, "fixture-main", now())
	historical := consoleRecord("saved-response", "fixture-main", time.Now().Add(-2*time.Hour))
	historical.Requested = "gpt-6.1-sol"
	historical.Reported = "gpt-6.1-sol"
	historical.Stage = "response.completed"
	historical.OutputTokens = 400
	historical.TokensPerSecond = 20
	historical.ElapsedSeconds = 20
	consoleSave(t, a, "routing-observations", historical)
	progress := historical
	progress.ID = "saved-sample"
	progress.Stage = "response.output_text.delta"
	consoleSave(t, a, "live-routing-inbox", progress)
	permanent := historical
	permanent.ID = "saved-error"
	permanent.Reported = "gpt-6-sol"
	permanent.Verdict = "mismatch"
	permanent.At = time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339Nano)
	if err := a.archiveRouteEvidence("live-route", permanent.ID, permanent); err != nil {
		t.Fatal(err)
	}
	a.readLiveRoutes(map[string]bool{})
	server := httptest.NewServer(a.handler())
	defer server.Close()
	a.address = server.URL
	if err := saveJSON(filepath.Join(control, "ready.json"), Obj{"url": server.URL + "#" + a.token, "primary_key": key, "second_key": second, "synthetic": true}); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(8 * time.Minute)
	defer deadline.Stop()
	lastScenario := ""
	var engine *floatui.Engine
	for {
		select {
		case <-deadline.C:
			return
		case <-ticker.C:
		}
		if _, err := os.Stat(filepath.Join(control, "stop")); err == nil {
			return
		}
		var input struct {
			Scenario string `json:"scenario"`
		}
		if b, err := os.ReadFile(filepath.Join(control, "scenario.json")); err == nil {
			_ = json.Unmarshal(b, &input)
		}
		if input.Scenario == "" {
			input.Scenario = "match"
		}
		if input.Scenario != lastScenario {
			clock := time.Now()
			engine = floatui.New(floatui.DefaultOptions())
			if input.Scenario != "none" && input.Scenario != "historical" {
				r := consoleRecord("fixture-current-"+input.Scenario, "fixture-main", clock)
				r.Turn = "live-turn"
				r.Requested = "gpt-6.1-sol"
				r.Reported = "gpt-6.1-sol"
				if input.Scenario == "pending" {
					r.Stage = "request.sent"
					r.Response = ""
					r.Reported = ""
					r.Verdict = "pending"
					r.Source = "native_wire_request"
				}
				kind := floatui.ResponseCreated
				if input.Scenario == "pending" {
					kind = floatui.RequestSent
				}
				engine.Apply(floatui.Event{ID: r.ID, Thread: r.Thread, Turn: r.Turn, Response: r.Response, At: clock, Kind: kind, PID: 7, Requested: r.Requested, Reported: r.Reported, Verdict: r.Verdict, Trusted: true}, clock)
				if input.Scenario == "concurrent" {
					engine.Apply(floatui.Event{ID: "fixture-other", Thread: "fixture-second", Turn: "live-turn-2", Response: "other-response", At: clock, Kind: floatui.ResponseCreated, PID: 7, Requested: "gpt-6.1-sol", Reported: "gpt-6-sol", Verdict: "mismatch", Trusted: true}, clock)
				}
			}
			lastScenario = input.Scenario
		}
		clock := time.Now()
		if input.Scenario == "streaming" {
			engine.Apply(floatui.Event{ID: "stream-" + clock.Format(time.RFC3339Nano), Thread: "fixture-main", Turn: "live-turn", Response: "fixture-response", At: clock, Kind: floatui.OutputProgress, PID: 7, Requested: "gpt-6.1-sol", Reported: "gpt-6.1-sol", Verdict: "match", Trusted: true, EstimatedTokenRate: 19.6}, clock)
		}
		engine.SetHealth(floatui.Health{At: clock, Ready: input.Scenario != "stale", PIDs: []int{7}})
		engine.ObserveQuota(floatui.QuotaEvent{Bucket: "synthetic-weekly", Source: "synthetic", At: clock, WindowMinutes: 10080, Used: 25})
		snapshot := engine.Snapshot(clock)
		a.floatSnapshot.Store(&snapshot)
	}
}
