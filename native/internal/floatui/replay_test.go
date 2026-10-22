package floatui

import (
	"testing"
	"time"
)

func TestReplayKeepsNewRequestPendingUntilItsOwnResponse(t *testing.T) {
	e := New(DefaultOptions())
	clock := time.Now()
	e.SetHealth(Health{At: clock, Ready: true, PIDs: []int{7}})
	old := Event{ID: "old-response", Thread: "thread", Turn: "turn", Response: "old", Requested: "wanted", Reported: "wanted", Kind: ResponseFinished, Verdict: "match", Trusted: true, At: clock, PID: 7}
	e.Apply(old, clock)
	e.Apply(Event{ID: "new-request", Thread: "thread", Turn: "turn", Requested: "wanted", Kind: RequestSent, At: clock.Add(time.Second), PID: 7}, clock.Add(time.Second))
	e.Replay(old, clock.Add(time.Second))
	row := e.Snapshot(clock.Add(time.Second)).Rows[0]
	if row.Color != Yellow || row.Reported != "" || row.Response != "" {
		t.Fatal("older match was reused for a new request", row)
	}
	fresh := old
	fresh.ID, fresh.Response, fresh.At = "fresh-response", "fresh", clock.Add(2*time.Second)
	fresh.Kind = ResponseCreated
	e.Replay(fresh, fresh.At)
	if row = e.Snapshot(fresh.At).Rows[0]; row.Color != Green || row.Response != "fresh" {
		t.Fatal("new response did not recover green", row)
	}
}

func TestReplayPreservesTerminalTurnAndResponseScope(t *testing.T) {
	e := New(DefaultOptions())
	clock := time.Now()
	e.SetHealth(Health{At: clock, Ready: true, PIDs: []int{7}})
	start := Event{ID: "start", Thread: "thread", Turn: "turn", Requested: "wanted", Kind: RequestSent, At: clock, PID: 7}
	e.Apply(start, clock)
	end := start
	end.ID, end.Kind, end.At = "end", TurnFinished, clock.Add(time.Second)
	e.Apply(end, end.At)
	e.Replay(start, end.At)
	if row := e.Snapshot(end.At).Rows[0]; row.Activity != Completed || row.Ended.IsZero() {
		t.Fatal("replay revived an ended turn", row)
	}
	if rows := e.Snapshot(clock.Add(20 * time.Second)).Rows; len(rows) != 0 {
		t.Fatal("finished row persisted after resync", rows)
	}
}
