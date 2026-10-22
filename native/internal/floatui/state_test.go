package floatui

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

var epoch = time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)

func engine() *Engine {
	e := New(DefaultOptions())
	e.SetHealth(Health{At: epoch, Ready: true, PIDs: []int{7}})
	return e
}
func event(kind Kind, seconds int, response string) Event {
	return Event{ID: fmt.Sprintf("%d/%d/%s", kind, seconds, response), Thread: "thread", Turn: "turn", Response: response,
		At: epoch.Add(time.Duration(seconds) * time.Second), Kind: kind, PID: 7, Requested: "wanted", Trusted: true}
}
func apply(e *Engine, v Event) { e.Apply(v, v.At) }
func one(t *testing.T, e *Engine, seconds int) Row {
	t.Helper()
	rows := e.Snapshot(epoch.Add(time.Duration(seconds) * time.Second)).Rows
	if len(rows) != 1 {
		t.Fatalf("rows=%+v", rows)
	}
	return rows[0]
}

func TestRequestNeedsOwnModelEvidence(t *testing.T) {
	e := engine()
	apply(e, event(RequestSent, 0, ""))
	if r := one(t, e, 0); r.Color != Yellow || r.Model != "wanted" || r.Speed != "—/s" {
		t.Fatal(r)
	}
	v := event(ResponseCreated, 1, "one")
	v.Reported = "wanted"
	apply(e, v)
	if one(t, e, 1).Color != Green {
		t.Fatal("own response not confirmed")
	}
	apply(e, event(RequestSent, 2, ""))
	if r := one(t, e, 2); r.Color != Yellow || r.Reported != "" || r.SpeedValid {
		t.Fatal("old response carried into new request", r)
	}
}

func TestResponseCompletionDoesNotFinishTurn(t *testing.T) {
	e := engine()
	apply(e, event(RequestSent, 0, ""))
	v := event(ResponseCreated, 1, "one")
	v.Reported = "wanted"
	apply(e, v)
	v = event(ResponseFinished, 3, "one")
	v.Reported = "wanted"
	v.OutputTokens = 40
	v.ElapsedSeconds = 2
	apply(e, v)
	if r := one(t, e, 3); r.Activity != Running || r.Speed != "20.0/s" {
		t.Fatal(r)
	}
	apply(e, event(ToolStarted, 4, ""))
	if r := one(t, e, 4); r.Activity != ToolWait || r.Color != Green {
		t.Fatal(r)
	}
	apply(e, event(RequestSent, 5, ""))
	if r := one(t, e, 5); r.Activity != Running || r.Speed != "—/s" {
		t.Fatal(r)
	}
}

func TestCompletionHoldThenQuotaOnly(t *testing.T) {
	e := engine()
	apply(e, event(TurnStarted, 0, ""))
	apply(e, event(TurnFinished, 1, ""))
	if one(t, e, 8).Activity != Completed {
		t.Fatal("result vanished too soon")
	}
	if len(e.Snapshot(epoch.Add(9*time.Second)).Rows) != 0 {
		t.Fatal("completed turn never disappeared")
	}
}

func TestTurnMismatchSurvivesNextSegmentButNotNextTurn(t *testing.T) {
	e := engine()
	apply(e, event(RequestSent, 0, ""))
	v := event(ResponseCreated, 1, "one")
	v.Reported = "other"
	apply(e, v)
	apply(e, event(RequestSent, 2, ""))
	v = event(ResponseCreated, 3, "two")
	v.Reported = "wanted"
	apply(e, v)
	if r := one(t, e, 3); r.Color != Red || !r.Anomaly || r.Route != Match {
		t.Fatal(r)
	}
	v = event(RequestSent, 4, "")
	v.Turn = "next"
	apply(e, v)
	if r := one(t, e, 4); r.Color != Yellow || r.Anomaly {
		t.Fatal(r)
	}
}

func TestNoCurrentProcessCannotProduceGreen(t *testing.T) {
	e := engine()
	v := event(ResponseCreated, 1, "one")
	v.Reported = "wanted"
	apply(e, v)
	e.SetHealth(Health{At: epoch.Add(time.Second), Ready: true, PIDs: []int{8}})
	if r := one(t, e, 1); r.Color != Yellow || r.HealthValid {
		t.Fatal(r)
	}
	e.SetHealth(Health{At: epoch.Add(2 * time.Second), Ready: true, PIDs: []int{7}})
	if one(t, e, 2).Color != Green {
		t.Fatal("valid process lost")
	}
	if one(t, e, 13).Color != Yellow {
		t.Fatal("expired health turned green")
	}
}

func TestCharactersAreActivityNotTokenSpeed(t *testing.T) {
	e := engine()
	v := event(OutputProgress, 1, "one")
	v.Reported = "wanted"
	v.CharacterRate = 80
	v.TokenRate = 200
	apply(e, v)
	if r := one(t, e, 1); r.SpeedValid || r.Speed != "—/s" || r.OutputActivity != 80 {
		t.Fatal(r)
	}
	v = event(ResponseFinished, 2, "one")
	v.OutputTokens = 40
	v.ElapsedSeconds = 2
	apply(e, v)
	if one(t, e, 2).SpeedValid {
		t.Fatal("midstream observation became a full interval")
	}
}

func TestReorderedCreatedAndRequestDoNotEraseCompletion(t *testing.T) {
	e := engine()
	v := event(ResponseFinished, 3, "one")
	v.Reported = "wanted"
	v.OutputTokens = 40
	v.ElapsedSeconds = 2
	apply(e, v)
	v = event(ResponseCreated, 1, "one")
	v.Reported = "wanted"
	e.Apply(v, epoch.Add(4*time.Second))
	v = event(RequestSent, 0, "")
	e.Apply(v, epoch.Add(4*time.Second))
	if r := one(t, e, 4); !r.SpeedValid || r.Speed != "20.0/s" || r.Color != Green {
		t.Fatal(r)
	}
}

func TestOldTurnAndTerminalLateStartCannotRevive(t *testing.T) {
	e := engine()
	apply(e, event(TurnStarted, 0, ""))
	apply(e, event(TurnFinished, 2, ""))
	v := event(TurnStarted, 1, "")
	e.Apply(v, epoch.Add(3*time.Second))
	if one(t, e, 3).Activity != Completed {
		t.Fatal("late start revived completed turn")
	}
	v = event(RequestSent, 4, "")
	v.Turn = "new"
	apply(e, v)
	v = event(ResponseCreated, 5, "old")
	v.Reported = "other"
	apply(e, v)
	if r := one(t, e, 5); r.Turn != "new" || r.Anomaly {
		t.Fatal(r)
	}
}

func TestStableOrderAndIndependentColors(t *testing.T) {
	e := engine()
	v := event(ResponseCreated, 1, "one")
	v.Reported = "wanted"
	apply(e, v)
	v = event(ResponseCreated, 2, "two")
	v.Thread = "second"
	v.Reported = "other"
	apply(e, v)
	v = event(OutputProgress, 3, "one")
	v.Reported = "wanted"
	apply(e, v)
	rows := e.Snapshot(epoch.Add(3 * time.Second)).Rows
	if len(rows) != 2 || rows[0].Thread != "thread" || rows[0].Color != Green || rows[1].Color != Red {
		t.Fatal(rows)
	}
}

func TestDuplicateAndReplayDoNotCreateGhosts(t *testing.T) {
	e := engine()
	v := event(TurnStarted, 0, "")
	apply(e, v)
	apply(e, v)
	if s := e.Snapshot(epoch); len(s.Rows) != 1 || s.Diagnostics.Duplicates != 1 {
		t.Fatal(s)
	}
	v.Thread = "old"
	v.ID = "old"
	e.Apply(v, epoch.Add(3*time.Hour))
	if len(e.Snapshot(epoch).Rows) != 1 {
		t.Fatal("old history appeared active")
	}
}

func TestQuotaBucketStableAndUnknownDistinctFromZero(t *testing.T) {
	e := engine()
	if e.Snapshot(epoch).Quota.Known {
		t.Fatal("unknown became zero")
	}
	e.ObserveQuota(QuotaEvent{Bucket: "codex", Source: "home", At: epoch, Used: 10, WindowMinutes: 10080})
	e.ObserveQuota(QuotaEvent{Bucket: "other", Source: "home", At: epoch.Add(time.Second), Used: 70, WindowMinutes: 10080})
	if q := e.Snapshot(epoch.Add(time.Second)).Quota; q.Bucket != "codex" || q.Remaining != 90 {
		t.Fatal(q)
	}
	e.ObserveQuota(QuotaEvent{Bucket: "codex", Source: "home", At: epoch.Add(2 * time.Second), Used: 100, WindowMinutes: 10080})
	if q := e.Snapshot(epoch.Add(2 * time.Second)).Quota; !q.Known || q.Remaining != 0 {
		t.Fatal(q)
	}
	if e.Snapshot(epoch.Add(time.Hour)).Quota.Fresh {
		t.Fatal("old quota fresh")
	}
}

func TestInvalidSpeedQuotaAndUntrustedEvidence(t *testing.T) {
	e := engine()
	v := event(ResponseCreated, 1, "one")
	v.Trusted = false
	v.Reported = "wanted"
	apply(e, v)
	if one(t, e, 1).Color != Yellow {
		t.Fatal("untrusted model accepted")
	}
	e.ObserveQuota(QuotaEvent{Bucket: "codex", Source: "home", At: epoch, Used: math.NaN(), WindowMinutes: 10080})
	if e.Snapshot(epoch).Quota.Known {
		t.Fatal("invalid quota accepted")
	}
}

func TestEvidenceConflictAndVariantRemainYellow(t *testing.T) {
	for _, verdict := range []string{"version_variant", "conflicting_evidence"} {
		e := engine()
		v := event(ResponseCreated, 1, "one")
		v.Verdict = verdict
		v.Reported = "wanted-2026-09-30"
		apply(e, v)
		if r := one(t, e, 1); r.Color != Yellow || r.Anomaly {
			t.Fatal(r)
		}
	}
}

func TestBoundsAndPrune(t *testing.T) {
	o := DefaultOptions()
	o.MaxThreads = 4
	o.DedupCapacity = 8
	e := New(o)
	for i := 0; i < 20; i++ {
		v := event(RequestSent, i, "")
		v.Thread = fmt.Sprint(i)
		apply(e, v)
	}
	if len(e.threads) != 4 || len(e.seen) > 8 || e.Snapshot(epoch.Add(20*time.Second)).Diagnostics.CapacityEvictions != 16 {
		t.Fatal("unbounded state")
	}
	e.Prune(epoch.Add(48 * time.Hour))
	if len(e.threads) != 0 || len(e.order) != 0 {
		t.Fatal("prune failed")
	}
}

func TestConcurrentSnapshotsAndInputs(t *testing.T) {
	e := engine()
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				v := event(RequestSent, i, "")
				v.ID = fmt.Sprintf("%d-%d", w, i)
				v.Thread = fmt.Sprint(w)
				apply(e, v)
				e.SetHealth(Health{At: v.At, Ready: true, PIDs: []int{7}})
				e.Snapshot(v.At)
			}
		}(worker)
	}
	wg.Wait()
}

var benchmarkSnapshot Snapshot

func TestOutputEstimateAndVisibilityDoNotReplaceExactSpeed(t *testing.T) {
	e := engine()
	v := event(ResponseCreated, 1, "one")
	v.Reported = "wanted"
	apply(e, v)
	if one(t, e, 1).Outputting {
		t.Fatal("reasoning showed output speed")
	}
	v = event(OutputProgress, 2, "one")
	v.CharacterRate = 40
	v.EstimatedTokenRate = 10
	apply(e, v)
	if r := one(t, e, 2); !r.Outputting || r.EstimatedTokenRate != 10 || r.SpeedValid {
		t.Fatal(r)
	}
	if one(t, e, 4).Outputting {
		t.Fatal("silent output stayed visible")
	}
	v = event(ResponseFinished, 5, "one")
	v.OutputTokens = 40
	v.ElapsedSeconds = 4
	apply(e, v)
	if r := one(t, e, 5); r.Outputting || r.EstimatedTokenRate != 0 || !r.SpeedValid {
		t.Fatal(r)
	}
}

func BenchmarkProjection(b *testing.B) {
	for _, n := range []int{0, 1, 2, 4, 8, 16, 32} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			e := engine()
			for i := 0; i < n; i++ {
				v := event(RequestSent, 0, "")
				v.Thread = fmt.Sprint(i)
				v.ID = v.Thread
				apply(e, v)
			}
			buffer := make([]Row, 0, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkSnapshot = e.AppendSnapshot(buffer[:0], epoch)
			}
		})
	}
}
