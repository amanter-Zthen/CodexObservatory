package main

import (
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routetrace"
	"strings"
	"testing"
	"time"
)

func TestMeasuredCompletionRateReachesVisibleLabelWithoutCreatedFrame(t *testing.T) {
	a, _ := testApp(t)
	at := time.Now()
	r := routetrace.Record{ID: "completion-speed", At: at.Format(time.RFC3339Nano), Thread: "synthetic-rate", Turn: "turn", Response: "response", Requested: "wanted", Reported: "wanted", Stage: "response.completed", Source: "native_wire_response", Scope: "original_desktop_request", Verdict: "match", OutputTokens: 188, ElapsedSeconds: 10, TokensPerSecond: 18.8}
	a.acceptFloatingRoute(r)
	a.publishFloating()
	snapshot := a.readFloatingSnapshot()
	if len(snapshot.Rows) != 1 || !snapshot.Rows[0].SpeedFresh || !snapshot.Rows[0].SpeedValid || snapshot.Rows[0].Outputting {
		t.Fatal("measured completion rate was dropped or output revived", snapshot.Rows)
	}
	animator := floatui.NewAnimator()
	animator.Set(snapshot, 0, 400, 0)
	scene := animator.Frame(.8)
	labels := floatui.NewPainter().Paint(scene).Labels
	visible := false
	for _, label := range labels {
		if label.Text == "18.8/s" && label.Color>>24 > 100 {
			visible = true
		}
		if strings.Contains(label.Text, "≈") {
			t.Fatal("approximation glyph returned")
		}
	}
	if !visible {
		t.Fatal("rate did not reach actual paint labels")
	}
	snapshot = a.floatingEngine().Snapshot(at.Add(2 * time.Second))
	animator.Set(snapshot, 0, 400, 1)
	scene = animator.Frame(1.5)
	for _, label := range floatui.NewPainter().Paint(scene).Labels {
		if strings.HasSuffix(label.Text, "/s") {
			t.Fatal("completed rate stayed visible")
		}
	}
	// A history replay keeps its measurement but has no fresh output pulse.
	if snapshot.Rows[0].SpeedFresh || snapshot.Rows[0].Outputting {
		t.Fatal("historical measurement looked live")
	}
}

func TestFastSnapshotRefreshKeepsRateFadeInAndEndFadeOut(t *testing.T) {
	a, _ := testApp(t)
	animator := floatui.NewAnimator()
	for n := 0; n < 30; n++ {
		at := time.Now()
		a.acceptFloatingRoute(routetrace.Record{ID: strings.Repeat("x", n+1), At: at.Format(time.RFC3339Nano), Thread: "rate", Turn: "turn", Response: "response", Requested: "wanted", Stage: "response.output_text.delta", Source: "native_wire_response", Scope: "original_desktop_request", EstimatedTokensPerSecond: 19.6})
		a.publishFloating()
		s := a.readFloatingSnapshot()
		if len(s.Rows) != 1 || !s.Rows[0].Outputting {
			t.Fatal("estimate-only progress did not mark output")
		}
		now := float64(n) * .04
		animator.Set(s, 0, 400, now)
		animator.Frame(now)
	}
	if row := animator.Frame(1.2).Rows[0]; row.RateOpacity < .99 || row.Speed != "19.6/s" {
		t.Fatal("refresh continually postponed fade-in", row)
	}
	a.acceptFloatingRoute(routetrace.Record{ID: "end", At: time.Now().Format(time.RFC3339Nano), Thread: "rate", Turn: "turn", Response: "response", Requested: "wanted", Stage: "response.completed", Source: "native_wire_response", Scope: "original_desktop_request"})
	a.publishFloating()
	animator.Set(a.readFloatingSnapshot(), 0, 400, 1.2)
	if animator.Frame(1.8).Rows[0].RateOpacity != 0 {
		t.Fatal("zero/end did not fade away")
	}
}
