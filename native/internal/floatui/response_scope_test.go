package floatui

import (
	"testing"
	"time"
)

func scopedEvent(kind Kind, seconds int, response string) Event {
	v := event(kind, seconds, response)
	v.ResponseScoped = true
	v.Reported = "wanted"
	return v
}

func TestResponseScopedCompletionHasBoundedHoldAndPreservesAnomaly(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		e := engine()
		apply(e, scopedEvent(RequestSent, 0, ""))
		apply(e, scopedEvent(ResponseCreated, 1, "one"))
		v := scopedEvent(ResponseFinished, 2, "one")
		if mismatch {
			v.Reported, v.Verdict = "other", "mismatch"
		}
		apply(e, v)
		r := one(t, e, 3)
		if r.Activity != Completed || r.ActivityScope != "response" || r.Ended.IsZero() || r.Anomaly != mismatch {
			t.Fatal(r)
		}
		hold := DefaultOptions().NormalHold
		if mismatch {
			hold = DefaultOptions().ErrorHold
		}
		if rows := e.Snapshot(epoch.Add(2*time.Second + hold)).Rows; len(rows) != 0 {
			t.Fatal("response-only row exceeded bounded hold", rows)
		}
	}
}

func TestResponseScopedFailureEndsOnlyItsOwnDisplay(t *testing.T) {
	e := engine()
	apply(e, scopedEvent(RequestSent, 0, ""))
	apply(e, scopedEvent(ResponseFailed, 1, "failed"))
	if r := one(t, e, 2); r.Activity != Failed || r.Ended.IsZero() {
		t.Fatal(r)
	}
}

func TestLateTaskEvidenceUpgradesProvisionalResponseFinish(t *testing.T) {
	e := engine()
	apply(e, scopedEvent(ResponseCreated, 1, "one"))
	apply(e, scopedEvent(ResponseFinished, 2, "one"))
	e.Apply(event(TurnStarted, 0, ""), epoch.Add(3*time.Second))
	if r := one(t, e, 3); r.Activity != Running || r.ActivityScope != "turn" || !r.Ended.IsZero() {
		t.Fatal(r)
	}
	apply(e, scopedEvent(ResponseFinished, 4, "two"))
	if r := one(t, e, 5); r.Activity != Running || !r.Ended.IsZero() {
		t.Fatal("task evidence was not sticky", r)
	}
	apply(e, event(TurnFinished, 6, ""))
	e.Apply(event(TurnStarted, 0, ""), epoch.Add(7*time.Second))
	if r := one(t, e, 7); r.Activity != Completed || r.Ended.IsZero() {
		t.Fatal("late start revived a genuine completed turn", r)
	}
}

func TestNewResponseScopedRequestReopensWithoutRevivingOldFrames(t *testing.T) {
	e := engine()
	apply(e, scopedEvent(RequestSent, 0, ""))
	apply(e, scopedEvent(ResponseFinished, 1, "one"))
	apply(e, scopedEvent(RequestSent, 2, ""))
	if r := one(t, e, 2); r.Activity != Running || !r.Ended.IsZero() || r.Reported != "" {
		t.Fatal(r)
	}
	apply(e, scopedEvent(ResponseFinished, 3, "two"))
	e.Apply(scopedEvent(RequestSent, 0, ""), epoch.Add(4*time.Second))
	if r := one(t, e, 4); r.Activity != Completed || r.Response != "two" {
		t.Fatal("old request replay revived the auxiliary display", r)
	}
}

func TestUntrackedAuxiliaryTurnCannotReplaceKnownActiveTurn(t *testing.T) {
	e := engine()
	apply(e, event(TurnStarted, 0, ""))
	apply(e, event(RequestSent, 1, ""))
	aux := scopedEvent(RequestSent, 2, "")
	aux.ID, aux.Turn, aux.Requested = "aux-start", "aux-turn", "other-model"
	apply(e, aux)
	aux = scopedEvent(ResponseFinished, 3, "aux-response")
	aux.ID, aux.Turn, aux.Requested = "aux-end", "aux-turn", "other-model"
	apply(e, aux)
	if r := one(t, e, 4); r.Turn != "turn" || r.Requested != "wanted" || r.Activity != Running {
		t.Fatal("auxiliary activity overwrote the main task", r)
	}
}

func TestRetiredClientHiddenAndPrunedOnlyWithFreshAuthoritativeHealth(t *testing.T) {
	e := engine()
	apply(e, event(RequestSent, 0, ""))
	e.SetHealth(Health{At: epoch.Add(8 * time.Second), Ready: true, PIDs: []int{8}})
	if rows := e.Snapshot(epoch.Add(8 * time.Second)).Rows; len(rows) != 0 {
		t.Fatal("retired client was still advertised as active", rows)
	}
	e.Prune(epoch.Add(8 * time.Second))
	if e.HasTurn("thread", "turn") {
		t.Fatal("retired client state was not removed")
	}
	for _, health := range []Health{
		{At: epoch.Add(8 * time.Second), Ready: false, PIDs: []int{8}},
		{At: epoch.Add(8 * time.Second), Ready: true},
		{At: epoch, Ready: true, PIDs: []int{8}},
	} {
		e := engine()
		apply(e, event(RequestSent, 0, ""))
		e.SetHealth(health)
		now := epoch.Add(20 * time.Second)
		e.Prune(now)
		if !e.HasTurn("thread", "turn") || len(e.Snapshot(now).Rows) != 1 {
			t.Fatal("uncertain collector health manufactured a client exit")
		}
	}
}
