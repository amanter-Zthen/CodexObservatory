package main

import (
	"codex-observatory/internal/floatui"
	"codex-observatory/internal/routetrace"
	"testing"
	"time"
)

func scopedResponseFixture(a *App, at time.Time, thread, turn, stage string) {
	a.acceptFloatingRoute(routetrace.Record{ID: thread + turn + stage, At: at.Format(time.RFC3339Nano), Thread: thread, Turn: turn,
		Response: "response", PID: 7, Requested: "model", Reported: "model", Stage: stage,
		Verdict: "match", Source: "native_wire_response", Scope: "original_desktop_request"})
}

func TestResponseWithoutRecordedTurnEndsAtResponseCompletion(t *testing.T) {
	a, _ := testApp(t)
	at := time.Now()
	a.floatingEngine().SetHealth(floatui.Health{At: at, Ready: true, PIDs: []int{7}})
	scopedResponseFixture(a, at, "temporary", "temporary-turn", "response.created")
	scopedResponseFixture(a, at.Add(time.Second), "temporary", "temporary-turn", "response.completed")
	rows := a.floatingEngine().Snapshot(at.Add(2 * time.Second)).Rows
	if len(rows) != 1 || rows[0].Activity != floatui.Completed {
		t.Fatalf("response-only request remained running: %+v", rows)
	}
	if rows := a.floatingEngine().Snapshot(at.Add(10 * time.Second)).Rows; len(rows) != 0 {
		t.Fatalf("completed response-only request stayed on the floating window: %+v", rows)
	}
	if count, _ := a.completionCount(); count != 0 {
		t.Fatal("background response manufactured a human completed-chat notice")
	}
}

func TestResponseOnlyReplayDoesNotCreateGhostActivity(t *testing.T) {
	a, _ := testApp(t)
	at := time.Now()
	a.floatingEngine().SetHealth(floatui.Health{At: at, Ready: true, PIDs: []int{7}})
	scopedResponseFixture(a, at.Add(-time.Hour), "temporary", "temporary-turn", "response.completed")
	if rows := a.floatingEngine().Snapshot(at).Rows; len(rows) != 0 {
		t.Fatalf("old completed metadata was replayed as a running task: %+v", rows)
	}
}

func TestRecordedTurnRemainsActiveAcrossResponseAndToolWait(t *testing.T) {
	a, _ := testApp(t)
	at := time.Now()
	a.files["known"] = &FileState{Summary: Summary{Thread: "human", CurrentTurn: "main-turn", Source: "desktop", Model: "model"}}
	a.floatingEngine().SetHealth(floatui.Health{At: at, Ready: true, PIDs: []int{7}})
	scopedResponseFixture(a, at, "human", "main-turn", "response.created")
	scopedResponseFixture(a, at.Add(time.Second), "human", "main-turn", "response.completed")
	a.acceptFloatingEvents([]Event{{Thread: "human", Turn: "main-turn", Kind: "tool_call", At: at.Add(2 * time.Second).Format(time.RFC3339Nano)}})
	rows := a.floatingEngine().Snapshot(at.Add(time.Minute)).Rows
	if len(rows) != 1 || rows[0].Activity != floatui.ToolWait || !rows[0].Ended.IsZero() {
		t.Fatalf("normal task incorrectly ended with one model response: %+v", rows)
	}
}

func TestRecordedLunaIsNotClassifiedByItsModelName(t *testing.T) {
	a, _ := testApp(t)
	at := time.Now()
	a.files["known"] = &FileState{Summary: Summary{Thread: "human", CurrentTurn: "turn", Source: "desktop", Model: "gpt-6-luna"}}
	r := routetrace.Record{ID: "human-luna", At: at.Format(time.RFC3339Nano), Thread: "human", Turn: "turn", Response: "response", PID: 7,
		Requested: "gpt-6-luna", Reported: "gpt-6-luna", Stage: "response.completed", Verdict: "match", Source: "native_wire_response", Scope: "original_desktop_request"}
	a.acceptFloatingRoute(r)
	rows := a.floatingEngine().Snapshot(at.Add(10 * time.Second)).Rows
	if len(rows) != 1 || rows[0].Activity != floatui.Running || rows[0].ActivityScope != "turn" {
		t.Fatal("a user's real Luna task was treated as a naming request", rows)
	}
}

func TestAuxiliaryDoesNotReplaceMainTurnOrCompletionNotice(t *testing.T) {
	a, _ := testApp(t)
	at := time.Now()
	a.files["known"] = &FileState{Summary: Summary{Thread: readyTestThread, CurrentTurn: "main", Source: "desktop", Model: "main-model"}}
	a.acceptFloatingEvents([]Event{{Thread: readyTestThread, Turn: "main", Kind: "turn_started", Model: "main-model", At: at.Format(time.RFC3339Nano)}})
	a.acceptFloatingRoute(routetrace.Record{ID: "aux-on-human-thread", At: at.Add(time.Second).Format(time.RFC3339Nano), Thread: readyTestThread, Turn: "aux", PID: 7,
		Requested: "aux-model", Stage: "request.sent", Source: "native_wire_request", Scope: "original_desktop_request"})
	rows := a.floatingEngine().Snapshot(at.Add(2 * time.Second)).Rows
	if len(rows) != 1 || rows[0].Turn != "main" || rows[0].Requested != "main-model" {
		t.Fatal(rows)
	}
	a.observeCompletion(readyTestThread, "main", "finish", "completed", "main-model", at.Add(3*time.Second))
	items := a.pendingCompletions()
	if len(items) != 1 || items[0].Turn != "main" {
		t.Fatal("auxiliary start erased the real completion notice", items)
	}
}
