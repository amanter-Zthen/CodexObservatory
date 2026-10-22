package main

import (
	"codex-observatory/internal/completion"
	"codex-observatory/internal/routetrace"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const readyTestThread = "01a0ef5e-dc04-7562-8459-18a5446f12e4"

func TestResponseFinishedIsNotTurnFinishedAndOldHistoryIsQuiet(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	a.acceptFloatingRoute(routetrace.Record{ID: "response", Thread: readyTestThread, Turn: "one", Stage: "response.completed", At: clock.Format(time.RFC3339Nano), Source: "native_wire_response", Scope: "original_desktop_request"})
	if c, _ := a.completionCount(); c != 0 {
		t.Fatal(c)
	}
	a.acceptFloatingEvents([]Event{{Thread: readyTestThread, Turn: "old", Kind: "turn_completed", At: a.started.Add(-time.Minute).Format(time.RFC3339Nano)}})
	if c, _ := a.completionCount(); c != 0 {
		t.Fatal("cold history generated a badge")
	}
	a.acceptFloatingEvents([]Event{{Thread: readyTestThread, Turn: "one", Kind: "turn_completed", At: time.Now().Format(time.RFC3339Nano)}})
	if c, _ := a.completionCount(); c != 1 {
		t.Fatal(c)
	}
}
func TestCompletionJumpAcknowledgesOnlySuccessfulExactItem(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	a.observeCompletion(readyTestThread, "one", "finish", "completed", "model", clock)
	id := completion.ID(readyTestThread, "one")
	if err := a.openCompleted(id, func(string) error { return errors.New("fixture OS refusal") }); err == nil {
		t.Fatal("open failure swallowed")
	}
	if c, _ := a.completionCount(); c != 1 {
		t.Fatal(c)
	}
	var opened string
	if err := a.openCompleted(id, func(url string) error { opened = url; return nil }); err != nil {
		t.Fatal(err)
	}
	if opened != "codex://threads/"+readyTestThread {
		t.Fatal(opened)
	}
	state, _ := a.completionInbox().Export()
	if err := saveJSON(filepath.Join(a.data, "completed-notifications.json"), state); err != nil {
		t.Fatal(err)
	}
	restarted, _ := testApp(t)
	restarted.data = a.data
	if c, _ := restarted.completionCount(); c != 0 {
		t.Fatal("read item resurrected on restart")
	}
}
func TestSyntheticCompletionClearsWithoutOpeningCodex(t *testing.T) {
	a, _ := testApp(t)
	a.preview = true
	a.started = time.Now().Add(-29 * time.Second)
	if c, _ := a.completionCount(); c != 1 {
		t.Fatal(c)
	}
	calls := 0
	if err := a.openCompleted("synthetic-completed", func(string) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if c, _ := a.completionCount(); c != 0 || calls != 0 {
		t.Fatal(c, calls)
	}
}
func TestFirstReadSubagentDoesNotGenerateHumanCompletion(t *testing.T) {
	a, home := testApp(t)
	now := time.Now().Format(time.RFC3339Nano)
	events := []Event{{Thread: readyTestThread, Turn: "child", Kind: "turn_started", At: now}, {Thread: readyTestThread, Turn: "child", Kind: "turn_completed", At: now}}
	a.acceptFloatingEvents(events, Summary{Thread: readyTestThread, Source: "subagent", Home: home})
	if n, _ := a.completionCount(); n != 0 {
		t.Fatal("child generated a pending chat")
	}
	if e := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(`{"id":"`+readyTestThread+`","thread_name":"本地标题 & 测试"}`+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	a.acceptFloatingEvents(events, Summary{Thread: readyTestThread, Source: "desktop", Home: home})
	items := a.pendingCompletions()
	if len(items) != 1 || items[0].Title != "本地标题 & 测试" {
		t.Fatal(items)
	}
}
