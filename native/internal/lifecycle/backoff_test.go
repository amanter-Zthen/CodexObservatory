package lifecycle

import (
	"testing"
	"time"
)

func TestBackoffSequenceGrowsAndCaps(t *testing.T) {
	var b Backoff
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60, 60}
	for i, w := range want {
		if got := b.Next(); got != w*time.Second {
			t.Fatalf("step %d: got %v want %v", i, got, w*time.Second)
		}
	}
}

func TestBackoffDegradedThresholdIsConsistentWithSequence(t *testing.T) {
	// DegradedAfter (5) is crossed after the fifth Next(), when the ramp is 16s.
	var b Backoff
	seen := []time.Duration{}
	for i := 0; i < DegradedAfter; i++ {
		if b.Degraded() {
			t.Fatalf("degraded before %d attempts (delays %v)", DegradedAfter, seen)
		}
		seen = append(seen, b.Next())
	}
	if last := seen[len(seen)-1]; last != 16*time.Second {
		t.Fatalf("fifth delay = %v, expected 16s", last)
	}
	if !b.Degraded() {
		t.Fatalf("not degraded after %d attempts", DegradedAfter)
	}
	if d := b.Next(); d != 32*time.Second {
		t.Fatalf("sixth delay = %v, expected 32s (retries continue)", d)
	}
}

func TestBackoffNoOverflowAtLargeAttempts(t *testing.T) {
	var b Backoff
	for i := 0; i < 200; i++ {
		if d := b.Next(); d <= 0 || d > MaxBackoff {
			t.Fatalf("step %d: invalid delay %v", i, d)
		}
	}
}

func TestBackoffResetReturnsToMinimum(t *testing.T) {
	var b Backoff
	b.Next()
	b.Next()
	b.Next()
	b.Reset()
	if b.Attempts() != 0 {
		t.Fatalf("attempts=%d want 0", b.Attempts())
	}
	if d := b.Next(); d != MinBackoff {
		t.Fatalf("after reset got %v want %v", d, MinBackoff)
	}
}

func TestBackoffCrashLoop100msIntervals(t *testing.T) {
	// A child that crashes 100ms after every start must produce the ramp.
	var b Backoff
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60}
	for i, w := range want {
		if got := b.Next(); got != w*time.Second {
			t.Fatalf("crash-loop step %d: got %v want %v", i, got, w*time.Second)
		}
	}
}
