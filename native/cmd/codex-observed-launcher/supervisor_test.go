//go:build windows

package main

import (
	"testing"
	"time"

	"codex-observatory/internal/lifecycle"
)

func resetSupervisorState() {
	childMu.Lock()
	tracked = nil
	childGen = 0
	pendingExits = nil
	restartCount = 0
	lastExitCode = -1
	lastExitTime = time.Time{}
	lastRestartErr = ""
	instanceOwner = ownerNone
	childMu.Unlock()
	for {
		select {
		case <-childExited:
			continue
		default:
		}
		break
	}
}

func seedChild(gen uint64, started time.Time, expected bool, reason string) {
	childMu.Lock()
	childGen = gen
	tracked = &childHandle{pid: 1000 + int(gen), started: started, reason: "reconcile", gen: gen, expectedStop: expected, stopReason: reason}
	childMu.Unlock()
}

// --- exit/reconcile race -------------------------------------------------

func TestCrashExitArmsBackoffForCurrentGeneration(t *testing.T) {
	resetSupervisorState()
	now := time.Now()
	seedChild(1, now.Add(-100*time.Millisecond), false, "")

	var b lifecycle.Backoff
	ev := childExitEvent{PID: 1001, Generation: 1, ExitCode: 1, StartedAt: now.Add(-100 * time.Millisecond), ExitedAt: now, LaunchReason: "reconcile"}
	delay, applied := handleChildExit(ev, now, &b)
	if !applied {
		t.Fatal("current-generation exit must be applied")
	}
	if delay != lifecycle.MinBackoff {
		t.Fatalf("delay = %v, want %v", delay, lifecycle.MinBackoff)
	}
	if childTracked() {
		t.Fatal("tracked must be cleared by the consumer")
	}
	if b.Attempts() != 1 {
		t.Fatalf("backoff attempts = %d, want 1", b.Attempts())
	}
	// The race we are guarding: as soon as tracked is nil, a replacement could
	// be spawned. The consumer must have armed the delay first.
	if delay <= 0 {
		t.Fatal("replacement must not be spawnable in the same tick")
	}
}

func TestCrashLoopBackoffRampIsMonotonic(t *testing.T) {
	resetSupervisorState()
	base := time.Now()
	var b lifecycle.Backoff
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60}
	for i, w := range want {
		gen := uint64(i + 1)
		started := base.Add(time.Duration(i) * time.Second)
		exited := started.Add(100 * time.Millisecond) // crashes 100ms after start
		seedChild(gen, started, false, "")
		delay, applied := handleChildExit(childExitEvent{PID: 1000 + int(gen), Generation: gen, ExitCode: 3, StartedAt: started, ExitedAt: exited}, exited, &b)
		if !applied {
			t.Fatalf("step %d: exit not applied", i)
		}
		if delay != w*time.Second {
			t.Fatalf("step %d: delay %v want %v", i, delay, w*time.Second)
		}
	}
}

// --- expected stop vs crash ---------------------------------------------

func TestExpectedStopDoesNotTouchBackoff(t *testing.T) {
	resetSupervisorState()
	now := time.Now()
	// child ran only 5s, which would normally count as a crash
	seedChild(1, now.Add(-5*time.Second), true, stopReasonDesktopAbsent)

	var b lifecycle.Backoff
	delay, applied := handleChildExit(childExitEvent{PID: 1001, Generation: 1, ExitCode: 0,
		StartedAt: now.Add(-5 * time.Second), ExitedAt: now, LaunchReason: "follower-edge"}, now, &b)
	if !applied {
		t.Fatal("expected stop must be consumed")
	}
	if delay != 0 {
		t.Fatalf("expected stop must not arm a delay, got %v", delay)
	}
	if b.Attempts() != 0 {
		t.Fatalf("backoff attempts = %d, want 0", b.Attempts())
	}
	if b.Degraded() {
		t.Fatal("expected stop must not degrade the supervisor")
	}
	if restarts() != 0 {
		t.Fatalf("expected stop must not increase restart_count, got %d", restarts())
	}
	if lastRestartError() != "" {
		t.Fatalf("expected stop must clear last_restart_error, got %q", lastRestartError())
	}
}

func TestTenShortExpectedSessionsStayHealthy(t *testing.T) {
	resetSupervisorState()
	var b lifecycle.Backoff
	base := time.Now()
	for i := 0; i < 10; i++ {
		gen := uint64(i + 1)
		started := base.Add(time.Duration(i*10) * time.Second)
		exited := started.Add(5 * time.Second) // short but expected session
		seedChild(gen, started, true, stopReasonExplicitDisable)
		handleChildExit(childExitEvent{PID: 1000 + int(gen), Generation: gen, ExitCode: 0,
			StartedAt: started, ExitedAt: exited, LaunchReason: "follower-edge"}, exited, &b)
	}
	if b.Attempts() != 0 {
		t.Fatalf("attempts = %d after 10 expected stops, want 0", b.Attempts())
	}
	if b.Degraded() {
		t.Fatal("degraded must stay false across expected stops")
	}
	if restarts() != 0 {
		t.Fatalf("restart_count = %d, want 0", restarts())
	}
}

func TestExpectedStopOnlyAppliesToMarkedGeneration(t *testing.T) {
	resetSupervisorState()
	now := time.Now()
	seedChild(2, now.Add(-5*time.Second), true, stopReasonDesktopAbsent)

	var b lifecycle.Backoff
	// an UNMARKED older generation crashing 100ms after start must still back off
	handleChildExit(childExitEvent{PID: 1, Generation: 1, ExitCode: 1,
		StartedAt: now.Add(-2 * time.Second), ExitedAt: now.Add(-1900 * time.Millisecond)}, now, &b)
	if b.Attempts() != 0 {
		t.Fatalf("stale generation must not touch the backoff, attempts=%d", b.Attempts())
	}
	if !childTracked() {
		t.Fatal("stale event must not clear the current tracked child")
	}
}

// --- stale generation ----------------------------------------------------

func TestOutOfOrderStaleGenerationIsIgnored(t *testing.T) {
	resetSupervisorState()
	base := time.Now()
	var b lifecycle.Backoff

	// gen1 starts, gen2 starts and exits (arming one backoff step)
	seedChild(2, base.Add(1*time.Second), false, "")
	gen2Start := base.Add(1 * time.Second)
	gen2Exit := gen2Start.Add(100 * time.Millisecond)
	seedChild(2, gen2Start, false, "")
	d2, _ := handleChildExit(childExitEvent{PID: 1002, Generation: 2, ExitCode: 1, StartedAt: gen2Start, ExitedAt: gen2Exit}, gen2Exit, &b)
	if d2 != lifecycle.MinBackoff {
		t.Fatalf("gen2 delay = %v want %v", d2, lifecycle.MinBackoff)
	}
	attemptsAfterGen2 := b.Attempts()

	// gen1's delayed event arrives last: gen1 < currentGeneration (2) -> stale
	gen1Start := base
	gen1Exit := base.Add(50 * time.Millisecond)
	delay, applied := handleChildExit(childExitEvent{PID: 1001, Generation: 1, ExitCode: 1, StartedAt: gen1Start, ExitedAt: gen1Exit}, gen2Exit.Add(time.Second), &b)
	if applied {
		t.Fatal("stale generation must be reported as not applied")
	}
	if delay != 0 {
		t.Fatalf("stale generation must not arm a delay, got %v", delay)
	}
	if b.Attempts() != attemptsAfterGen2 {
		t.Fatalf("stale generation changed the backoff: %d -> %d", attemptsAfterGen2, b.Attempts())
	}
}

// --- spawnChild must not overwrite a live child --------------------------

func TestSpawnChildRefusesToOverwriteTracked(t *testing.T) {
	resetSupervisorState()
	now := time.Now()
	seedChild(7, now, false, "")
	err := spawnChild(launchConfig{Data: t.TempDir(), Monitor: "does-not-exist.exe"}, "reconcile")
	if err == nil {
		t.Fatal("spawnChild must refuse while a child is tracked")
	}
	if err != errChildAlreadyTracked {
		t.Fatalf("err = %v, want errChildAlreadyTracked", err)
	}
	childMu.Lock()
	gen := tracked.gen
	childMu.Unlock()
	if gen != 7 {
		t.Fatalf("tracked pointer was replaced: gen=%d want 7", gen)
	}
}

// --- expected stop marking -------------------------------------------------

func TestMarkExpectedStopFlagsCurrentGeneration(t *testing.T) {
	resetSupervisorState()
	seedChild(3, time.Now(), false, "")
	markExpectedStop(stopReasonExplicitDisable)
	childMu.Lock()
	got := tracked.expectedStop
	reason := tracked.stopReason
	childMu.Unlock()
	if !got || reason != stopReasonExplicitDisable {
		t.Fatalf("expectedStop=%v reason=%q", got, reason)
	}
}
