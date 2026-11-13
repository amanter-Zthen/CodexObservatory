//go:build windows

package main

import (
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routetrace"
	"fmt"
	"testing"
	"time"
)

// Matches a copied subagent rollout: its own header precedes the parent's
// copied header and interrupted history, then the child's local task begins.
func forkIdentityFixture(t *testing.T, child, parent string, at time.Time) (*FileState, []Event) {
	t.Helper()
	f := newState("home", "rollout-"+child+".jsonl", "key-"+child)
	var events []Event
	add := func(kind string, payload Obj) {
		events = append(events, parseLine(f, identityRecord(kind, payload, at), int64(len(events)))...)
	}
	add("session_meta", Obj{"id": child, "source": Obj{"subagent": Obj{"thread_spawn": Obj{"parent_thread_id": parent}}}})
	add("session_meta", Obj{"id": parent, "source": "vscode"})
	add("event_msg", Obj{"type": "task_started", "turn_id": "inherited-turn"})
	add("event_msg", Obj{"type": "turn_aborted", "turn_id": "inherited-turn"})
	add("event_msg", Obj{"type": "thread_settings_applied", "thread_id": child, "thread_settings": Obj{"model": "wanted", "service_tier": "default"}})
	return f, events
}

func TestRolloutIdentityCopiedHeaderKeepsChild(t *testing.T) {
	f, _ := forkIdentityFixture(t, "child", "parent", time.Now())
	if f.Summary.Thread != "child" || f.Summary.Source != "subagent" {
		t.Fatalf("inherited header replaced rollout owner: thread=%s source=%s", f.Summary.Thread, f.Summary.Source)
	}
	if f.Summary.ThreadTierOwner != "child" {
		t.Fatalf("child settings were assigned to %q", f.Summary.ThreadTierOwner)
	}
}

func TestRolloutIdentityCompletedChildLeavesParentRunning(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	a.started = clock.Add(-time.Hour)
	f, events := forkIdentityFixture(t, "child", "parent", clock.Add(-time.Minute))
	a.acceptFloatingEvents([]Event{{ID: "real-parent", Thread: "parent", Turn: "parent-turn", Kind: "turn_started", At: clock.Add(-2 * time.Minute).Format(time.RFC3339Nano)}})
	a.acceptFloatingEvents(events, f.Summary)
	start := parseLine(f, identityRecord("event_msg", Obj{"type": "task_started", "turn_id": "child-turn"}, clock.Add(-50*time.Second)), 100)
	a.acceptFloatingEvents(start, f.Summary)
	// Request identity comes from the separate wire path, as it did in production.
	a.acceptFloatingRoute(routetrace.Record{ID: "wire-child", Thread: "child", Turn: "child-turn", Response: "response", At: clock.Add(-45 * time.Second).Format(time.RFC3339Nano), Stage: "response.created", Requested: "wanted", Reported: "wanted", Source: "native_wire_response", Scope: "original_desktop_request", PID: 7})
	finish := parseLine(f, identityRecord("event_msg", Obj{"type": "task_complete", "turn_id": "child-turn"}, clock.Add(-20*time.Second)), 200)
	a.acceptFloatingEvents(finish, f.Summary)
	rows := a.floatingEngine().Snapshot(clock).Rows
	if len(rows) != 1 || rows[0].Thread != "parent" || rows[0].Turn != "parent-turn" {
		t.Fatalf("completion did not retire only child: %+v", rows)
	}
}

func TestRolloutIdentityFollowupAndTerminalKinds(t *testing.T) {
	for _, terminal := range []string{"task_complete", "turn_completed", "turn_aborted"} {
		t.Run(terminal, func(t *testing.T) {
			a, _ := testApp(t)
			clock := time.Now()
			a.started = clock.Add(-time.Hour)
			f, events := forkIdentityFixture(t, "child", "parent", clock.Add(-2*time.Minute))
			a.acceptFloatingEvents(events, f.Summary)
			for n := 1; n <= 2; n++ {
				turn := fmt.Sprintf("child-turn-%d", n)
				startAt := clock.Add(time.Duration(-120+n*30) * time.Second)
				a.acceptFloatingEvents(parseLine(f, identityRecord("event_msg", Obj{"type": "task_started", "turn_id": turn}, startAt), int64(n*10)), f.Summary)
				a.acceptFloatingEvents(parseLine(f, identityRecord("event_msg", Obj{"type": terminal, "turn_id": turn}, startAt.Add(5*time.Second)), int64(n*10+1)), f.Summary)
			}
			// A delayed response must not revive an explicitly completed turn.
			a.acceptFloatingRoute(routetrace.Record{ID: "late-wire", Thread: "child", Turn: "child-turn-2", Response: "r", At: clock.Add(-58 * time.Second).Format(time.RFC3339Nano), Stage: "response.completed", Requested: "wanted", Reported: "wanted", Source: "native_wire_response", Scope: "original_desktop_request", PID: 7})
			if rows := a.floatingEngine().Snapshot(clock).Rows; len(rows) != 0 {
				t.Fatalf("finished child revived: %+v", rows)
			}
		})
	}
}

func TestRolloutIdentityNestedHeadersAndNoOwnTurn(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	a.started = clock.Add(-time.Hour)
	f := newState("home", "grandchild.jsonl", "grandchild")
	var events []Event
	for _, id := range []string{"grandchild", "child", "parent"} {
		events = append(events, parseLine(f, identityRecord("session_meta", Obj{"id": id}, clock.Add(-time.Minute)), 0)...)
	}
	for _, kind := range []string{"task_started", "turn_aborted"} {
		events = append(events, parseLine(f, identityRecord("event_msg", Obj{"type": kind, "turn_id": "copied-turn"}, clock.Add(-time.Minute)), 1)...)
	}
	a.acceptFloatingEvents(events, f.Summary)
	if f.Summary.Thread != "grandchild" {
		t.Fatal(f.Summary.Thread)
	}
	if rows := a.floatingEngine().Snapshot(clock).Rows; len(rows) != 0 {
		t.Fatalf("inherited interrupted prefix became permanent activity: %+v", rows)
	}
}

func TestRolloutIdentityResponseCompletionKeepsToolTurn(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	a.started = clock.Add(-time.Hour)
	f, events := forkIdentityFixture(t, "child", "parent", clock.Add(-time.Minute))
	a.acceptFloatingEvents(events, f.Summary)
	a.files[f.Summary.Key] = f
	a.acceptFloatingEvents(parseLine(f, identityRecord("event_msg", Obj{"type": "task_started", "turn_id": "own"}, clock.Add(-40*time.Second)), 8), f.Summary)
	for n, stage := range []string{"response.created", "response.completed"} {
		a.acceptFloatingRoute(routetrace.Record{ID: fmt.Sprint(n), Thread: "child", Turn: "own", Response: "r", At: clock.Add(-30*time.Second + time.Duration(n)*time.Second).Format(time.RFC3339Nano), Stage: stage, Requested: "wanted", Reported: "wanted", Source: "native_wire_response", Scope: "original_desktop_request", PID: 7})
	}
	rows := a.floatingEngine().Snapshot(clock).Rows
	if len(rows) != 1 || rows[0].Thread != "child" || rows[0].Activity == floatui.Completed {
		t.Fatalf("response completion ended the whole tool turn: %+v", rows)
	}
}

func identityRecord(kind string, payload Obj, at time.Time) []byte {
	return fixtureRecord(kind, payload, at.Format(time.RFC3339Nano))
}
