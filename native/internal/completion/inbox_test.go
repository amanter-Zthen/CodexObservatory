package completion

import (
	"fmt"
	"testing"
	"time"
)

const thread = "01a0ef5e-dc04-7562-8459-18a5446f12e4"

func TestPendingSurvivesViewWindowAndRestartWithoutResurrection(t *testing.T) {
	now := time.Now()
	b := New()
	b.Begin(thread, "one", "标题", "model", now, now)
	b.Finish(thread, "one", "", "", "completed", now.Add(time.Second), now.Add(time.Second))
	if c, _ := b.Count(); c != 1 {
		t.Fatal(c)
	}
	s, _ := b.Export()
	r := New()
	r.Restore(s, now.Add(time.Hour))
	if c, _ := r.Count(); c != 1 {
		t.Fatal(c)
	}
	r.Viewed(ID(thread, "one"))
	s, _ = r.Export()
	r = New()
	r.Restore(s, now.Add(time.Hour))
	r.Finish(thread, "one", "", "", "completed", now.Add(2*time.Second), now.Add(time.Hour))
	if c, _ := r.Count(); c != 0 {
		t.Fatal("viewed completion reappeared")
	}
}
func TestLateOldTurnCannotClearNewPendingAndFailureDoesNotLookDone(t *testing.T) {
	now := time.Now()
	b := New()
	b.Begin(thread, "one", "", "", now, now)
	b.Finish(thread, "one", "", "", "completed", now.Add(time.Second), now.Add(time.Second))
	b.Begin(thread, "two", "", "", now.Add(2*time.Second), now.Add(2*time.Second))
	b.Finish(thread, "two", "", "", "completed", now.Add(3*time.Second), now.Add(3*time.Second))
	b.Viewed(ID(thread, "one"))
	if rows := b.Pending(); len(rows) != 1 || rows[0].Turn != "two" {
		t.Fatal(rows)
	}
	b.Finish(thread, "two", "", "", "failed", now.Add(4*time.Second), now.Add(4*time.Second))
	if c, _ := b.Count(); c != 0 {
		t.Fatal("failed completion presented as done")
	}
	b.Finish(thread, "two", "", "", "completed", now.Add(5*time.Second), now.Add(5*time.Second))
	if c, _ := b.Count(); c != 0 {
		t.Fatal("failure overwritten by duplicate")
	}
}
func TestFiveDayExpiryIdentityAndCapacity(t *testing.T) {
	now := time.Now()
	b := New()
	for n := 0; n < Capacity+20; n++ {
		id := fmt.Sprintf("%08x-dc04-7562-8459-18a5446f12e4", n)
		b.Finish(id, "one", "", "", "completed", now, now)
	}
	s, _ := b.Export()
	if len(s.Records) > Capacity {
		t.Fatal(len(s.Records))
	}
	b.Prune(now.Add(Retention + time.Second))
	if c, _ := b.Count(); c != 0 {
		t.Fatal(c)
	}
	for _, id := range []string{"bad", "../thread", "https://evil.invalid", thread + "?send=1"} {
		if _, ok := ThreadURL(id); ok {
			t.Fatal(id)
		}
	}
	if url, ok := ThreadURL(thread); !ok || url != "codex://threads/"+thread {
		t.Fatal(url, ok)
	}
}
func TestCancelledAndMismatchedTurnNeverQueued(t *testing.T) {
	now := time.Now()
	b := New()
	b.Begin(thread, "one", "", "", now, now)
	if b.Finish(thread, "wrong", "", "", "completed", now.Add(time.Second), now.Add(time.Second)) {
		t.Fatal("foreign turn accepted")
	}
	b.Finish(thread, "one", "", "", "cancelled", now.Add(time.Second), now.Add(time.Second))
	if c, _ := b.Count(); c != 0 {
		t.Fatal(c)
	}
}
