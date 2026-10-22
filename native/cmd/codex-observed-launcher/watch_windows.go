//go:build windows

package main

import (
	"codex-observatory/internal/lifecycle"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

func watcherLock(data string) (func(), bool, error) {
	hash := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(data))))
	name, _ := syscall.UTF16PtrFromString("Local\\CodexObservatory-follow-" + hex.EncodeToString(hash[:12]))
	handle, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, false, err
	}
	return func() { syscall.CloseHandle(syscall.Handle(handle)) }, err == syscall.ERROR_ALREADY_EXISTS, nil
}
func saveConfig(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	temp := path + fmt.Sprintf(".%d.tmp", os.Getpid())
	if e = os.WriteFile(temp, b, 0600); e != nil {
		return e
	}
	return moveReplacing(temp, path)
}
func moveReplacing(temp, path string) error {
	from, _ := syscall.UTF16PtrFromString(temp)
	to, _ := syscall.UTF16PtrFromString(path)
	ok, _, e := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), 1|8)
	if ok == 0 {
		return e
	}
	return nil
}

// ---- child supervisor state (B node) ----
//
// startMonitor keeps its signature so the three call sites in main_windows.go
// are unchanged. spawnChild is the single launch entry point used by both the
// Follower edge and the reconcile supervisor.

type childHandle struct {
	cmd          *exec.Cmd
	pid          int
	started      time.Time
	reason       string // launch reason: "follower-edge" | "reconcile"
	gen          uint64 // generation; stale exit events must not affect a new child
	expectedStop bool   // true once a deliberate shutdown was requested for this generation
	stopReason   string // DESKTOP_ABSENT | EXPLICIT_DISABLE | MAINTENANCE
}

// Stop reasons recorded on the tracked generation before a deliberate shutdown.
const (
	stopReasonDesktopAbsent   = "DESKTOP_ABSENT"
	stopReasonExplicitDisable = "EXPLICIT_DISABLE"
	stopReasonMaintenance     = "MAINTENANCE"
)

// markExpectedStop flags the CURRENT tracked generation as a deliberate stop.
// Must be called immediately before stopMonitorOwned.
func markExpectedStop(reason string) {
	childMu.Lock()
	if tracked != nil {
		tracked.expectedStop = true
		tracked.stopReason = reason
	}
	childMu.Unlock()
}

// childExitEvent is delivered by the Wait goroutine and consumed by the loop.
type childExitEvent struct {
	PID          int
	Generation   uint64
	ExitCode     int
	StartedAt    time.Time
	ExitedAt     time.Time
	LaunchReason string
}

var (
	childMu        sync.Mutex
	tracked        *childHandle
	childGen       uint64
	childExited    = make(chan childExitEvent, 16)
	pendingExits   []childExitEvent
	restartCount   int
	lastExitCode   = -1
	lastExitTime   time.Time
	lastRestartErr string
	instanceOwner  = ownerNone
)

// errChildAlreadyTracked is returned instead of overwriting a live child.
var errChildAlreadyTracked = fmt.Errorf("a tracked child is already running")

// Errors that must fail fast (non-zero exit) so Task Scheduler can restart.
var (
	errNativeListenerNotReady = fmt.Errorf("native listener did not become ready after start")
	errNativeListenerLost     = fmt.Errorf("native listener stopped accepting connections")
)

// listenerFailureLimit is the consecutive-probe threshold before the watcher
// gives up and lets the scheduler restart it.
const listenerFailureLimit = 5

// foreignProbeInterval is the fixed low-frequency probe used when a foreign
// process owns the instance; it must never touch the crash backoff.
const foreignProbeInterval = 3 * time.Second

func childTracked() bool {
	childMu.Lock()
	defer childMu.Unlock()
	return tracked != nil
}
func childPID() int {
	childMu.Lock()
	defer childMu.Unlock()
	if tracked == nil {
		return 0
	}
	return tracked.pid
}
func childStartTime() time.Time {
	childMu.Lock()
	defer childMu.Unlock()
	if tracked == nil {
		return time.Time{}
	}
	return tracked.started
}
func childLaunchReason() string {
	childMu.Lock()
	defer childMu.Unlock()
	if tracked == nil {
		return ""
	}
	return tracked.reason
}
func restarts() int {
	childMu.Lock()
	defer childMu.Unlock()
	return restartCount
}
func lastExit() int {
	childMu.Lock()
	defer childMu.Unlock()
	return lastExitCode
}
func lastExitAt() string {
	childMu.Lock()
	defer childMu.Unlock()
	if lastExitTime.IsZero() {
		return ""
	}
	return lastExitTime.UTC().Format(time.RFC3339Nano)
}
func lastRestartError() string {
	childMu.Lock()
	defer childMu.Unlock()
	return lastRestartErr
}
func clearRestartError() {
	childMu.Lock()
	lastRestartErr = ""
	childMu.Unlock()
}
func markInstanceOwner(kind string) {
	childMu.Lock()
	instanceOwner = kind
	childMu.Unlock()
}
func instanceOwnerKind() string {
	childMu.Lock()
	defer childMu.Unlock()
	return instanceOwner
}

func startMonitor(cfg launchConfig) error { return spawnChild(cfg, "follower-edge") }

// currentGeneration returns the monotonic generation counter.
func currentGeneration() uint64 {
	childMu.Lock()
	defer childMu.Unlock()
	return childGen
}

// handleChildExit applies one exit event. It is the ONLY place that clears
// tracked for an exit and the ONLY place that adjusts the crash backoff.
//
// Determinism: for the current generation, recording the exit, clearing tracked
// and arming the backoff happen under one lock, so the reconcile branch that
// runs later in the same tick cannot spawn before the delay elapses.
func handleChildExit(ev childExitEvent, now time.Time, reconcile *lifecycle.Backoff) (time.Duration, bool) {
	childMu.Lock()
	defer childMu.Unlock()

	if ev.Generation < childGen {
		// Stale, out-of-order event: diagnostics only. It must never modify
		// tracked, the backoff, restart scheduling or the instance owner.
		lastExitCode = ev.ExitCode
		lastExitTime = ev.ExitedAt
		return 0, false
	}

	expected := false
	if tracked != nil && tracked.gen == ev.Generation {
		expected = tracked.expectedStop
		tracked = nil
		instanceOwner = ownerNone
	}
	lastExitCode = ev.ExitCode
	lastExitTime = ev.ExitedAt

	if expected {
		// EXPECTED_STOP: not a crash. No backoff, no degraded, no restart count.
		reconcile.Reset()
		lastRestartErr = ""
		return 0, true
	}
	lifetime := ev.ExitedAt.Sub(ev.StartedAt)
	if lifetime < lifecycle.StablePeriod {
		return reconcile.Next(), true
	}
	reconcile.Reset()
	lastRestartErr = ""
	return 0, true
}

// spawnChild launches the Observatory child and reports its exit as a typed
// event. It refuses to overwrite a live tracked child, and records the child
// under a fresh generation so a stale exit can never clear the current one.
func spawnChild(cfg launchConfig, reason string) error {
	childMu.Lock()
	if tracked != nil {
		childMu.Unlock()
		return errChildAlreadyTracked
	}
	childMu.Unlock()

	command := child(cfg.Monitor, "--no-open", "--data", cfg.Data)
	command.Dir = filepath.Dir(cfg.Monitor)
	if e := command.Start(); e != nil {
		return e
	}
	childMu.Lock()
	childGen++
	gen := childGen
	started := time.Now()
	tracked = &childHandle{cmd: command, pid: command.Process.Pid, started: started, reason: reason, gen: gen}
	childMu.Unlock()

	go func(c *exec.Cmd, pid int, gen uint64, started time.Time, reason string) {
		e := c.Wait()
		code := 0
		if e != nil {
			if ee, ok := e.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = -1
			}
		}
		// The Wait goroutine ONLY publishes the event. It must not clear tracked
		// and must not touch instanceOwner: doing so from here races with the
		// watch loop's event drain, which could then spawn a replacement before
		// the exit event was applied, silently skipping Backoff.Next().
		ev := childExitEvent{PID: pid, Generation: gen, ExitCode: code, StartedAt: started, ExitedAt: time.Now(), LaunchReason: reason}
		select {
		case childExited <- ev:
		default:
			// Reliable delivery: never silently drop an exit event. The buffered
			// channel absorbs normal traffic; if it is full the event goes to the
			// overflow queue, which the watch loop drains every tick.
			childMu.Lock()
			pendingExits = append(pendingExits, ev)
			childMu.Unlock()
		}
	}(command, command.Process.Pid, gen, started, reason)
	return nil
}

func stopMonitor(data string) error {
	return stopMonitorOwned(launchConfig{Data: data})
}

// stopMonitorOwned is ownership-aware: a FOREIGN_LIVE instance is never shut
// down because it is not ours to stop.
func stopMonitorOwned(cfg launchConfig) error {
	owner := probeChildOwner(cfg)
	switch owner {
	case ownerForeignLive:
		return fmt.Errorf("refusing to stop FOREIGN_LIVE instance")
	case ownerNone:
		return nil
	}
	path := filepath.Join(cfg.Data, "instance.json")
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
		return fmt.Errorf("monitor instance metadata unavailable")
	}
	var instance map[string]string
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if json.Unmarshal(b, &instance) != nil {
		return fmt.Errorf("invalid monitor instance metadata")
	}
	u, e := url.Parse(instance["url"])
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Fragment == "" || u.User != nil {
		return fmt.Errorf("monitor endpoint must be local")
	}
	token := u.Fragment
	u.Fragment = ""
	u.Path = "/api/shutdown"
	u.RawQuery = ""
	req, e := http.NewRequest("POST", u.String(), strings.NewReader("{}"))
	if e != nil {
		return e
	}
	req.Header.Set("X-Observatory-Token", token)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, e := client.Do(req)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("monitor shutdown status %d", response.StatusCode)
	}
	return nil
}

// waitNativeCaptureReady polls nativeCaptureReady without blocking forever.
func waitNativeCaptureReady(cfg launchConfig, attempts int, pause time.Duration) bool {
	for i := 0; i < attempts; i++ {
		if nativeCaptureReady(cfg) {
			return true
		}
		time.Sleep(pause)
	}
	return false
}

func watch(cfg launchConfig) error {
	if e := os.MkdirAll(cfg.Data, 0700); e != nil {
		return e
	}
	release, already, e := watcherLock(cfg.Data)
	if e != nil {
		return e
	}
	defer release()
	if already {
		return nil
	}
	capture, e := startNativeHomeCapture(cfg)
	if e != nil {
		// The listener could not be created. Fail fast so Task Scheduler's
		// RestartOnFailure can act. The .env block is intentionally KEPT: an
		// UNEXPECTED_TRANSIENT_EXIT must preserve it so already running Codex
		// sessions can reconnect to the same port once the watcher restarts.
		// It is removed only for EXPLICIT_DISABLE or a prolonged unrecoverable
		// failure (see the A failure state machine).
		watchFailure(cfg, e)
		return e
	}
	defer func() {
		if capture != nil {
			capture.close()
		}
	}()
	if !waitNativeCaptureReady(cfg, 5, 600*time.Millisecond) {
		watchFailure(cfg, errNativeListenerNotReady)
		return errNativeListenerNotReady
	}
	lastCaptureAttempt := time.Now()
	state := lifecycle.Follower{Grace: 3 * time.Second}
	lastAttempt := time.Time{}
	stopPending := false
	lastStatus := time.Time{}
	reconcile := lifecycle.Backoff{}
	reconcileAfter := time.Time{}
	listenerFailures := 0
	for {
		var settings struct {
			Enabled *bool `json:"enabled"`
		}
		if b, err := os.ReadFile(filepath.Join(cfg.Data, "follow-settings.json")); err == nil && json.Unmarshal(b, &settings) == nil && settings.Enabled != nil && !*settings.Enabled {
			// EXPLICIT_DISABLE: ownership-aware stop, then exit cleanly.
			markExpectedStop(stopReasonExplicitDisable)
			_ = stopMonitorOwned(cfg)
			_ = saveConfig(filepath.Join(cfg.Data, "lifecycle-status.json"), map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "following": false})
			present, inspectionErr := running(cfg.Desktop)
			// Existing cores retain their startup proxy. Drain their lifetime
			// before closing the relay; future cores use the restored home file.
			if !present && inspectionErr == nil {
				return nil
			}
			time.Sleep(time.Second)
			continue
		}
		now := time.Now()
		if capture == nil && now.Sub(lastCaptureAttempt) >= 30*time.Second {
			lastCaptureAttempt = now
			capture, e = startNativeHomeCapture(cfg)
			if e != nil {
				_ = removeNativeHome(cfg)
			}
		}
		desktopPID, err := runningDesktopPID(cfg.Desktop)
		present := desktopPID != 0
		if capture != nil {
			capture.update(desktopPID, err == nil, present)
		}
		action := state.Update(present, err == nil, now)
		if action == lifecycle.Start {
			stopPending = false
			if now.Sub(lastAttempt) < 10*time.Second {
				state.Active = false
			} else {
				lastAttempt = now
				// Ownership-aware edge start: never spawn on top of a live owner.
				// FOREIGN_LIVE / LEGACY / STARTING must not create a child, so they
				// cannot produce an exit event that would pollute the backoff.
				edgeOwner := probeChildOwner(cfg)
				markInstanceOwner(edgeOwner)
				if edgeOwner == ownerNone {
					if startMonitor(cfg) != nil {
						state.Active = false
					}
				}
			}
		}
		if action == lifecycle.Stop || !present && err == nil && !state.Active {
			stopPending = true
		}
		if stopPending {
			if present && err == nil {
				stopPending = false
			} else if err == nil {
				// Desktop absent: this is a deliberate stop, not a crash.
				markExpectedStop(stopReasonDesktopAbsent)
				if stopMonitorOwned(cfg) == nil {
					stopPending = false
				}
			}
		}

		// ---- child exit events drive the crash backoff ----
		// Drain the channel and the overflow queue; every event is applied once,
		// and a stale generation can never modify current state.
		for {
			var ev childExitEvent
			got := false
			select {
			case ev = <-childExited:
				got = true
			default:
			}
			if !got {
				childMu.Lock()
				if len(pendingExits) > 0 {
					ev = pendingExits[0]
					pendingExits = pendingExits[1:]
					got = true
				}
				childMu.Unlock()
			}
			if !got {
				break
			}
			// Staleness is decided against the MONOTONIC generation counter, not
			// against `tracked != nil`: a delayed event from an older generation
			// must stay stale even when tracked is nil again (e.g. gen1 arrives
			// after gen2 has already exited).
			if ev.Generation < currentGeneration() {
				_, _ = handleChildExit(ev, now, &reconcile) // diagnostics only
				continue
			}
			if delay, applied := handleChildExit(ev, now, &reconcile); applied {
				if delay > 0 {
					reconcileAfter = now.Add(delay)
				} else {
					reconcileAfter = time.Time{}
				}
			}
		}

		// ---- reconcile supervisor (single spawning authority) ----
		if present && err == nil && !stopPending {
			if !childTracked() && !now.Before(reconcileAfter) {
				switch probeChildOwner(cfg) {
				case ownerTracked:
					markInstanceOwner(ownerTracked)
				case ownerLegacySameTarget:
					markInstanceOwner(ownerLegacySameTarget)
				case ownerStarting:
					markInstanceOwner(ownerStarting)
				case ownerForeignLive:
					// Foreign owner: fixed low-frequency probe, never the crash
					// backoff, never restart_count, never degraded.
					markInstanceOwner(ownerForeignLive)
					reconcileAfter = now.Add(foreignProbeInterval)
				default:
					markInstanceOwner(ownerNone)
					childMu.Lock()
					restartCount++
					childMu.Unlock()
					if e := spawnChild(cfg, "reconcile"); e != nil {
						childMu.Lock()
						lastRestartErr = e.Error()
						childMu.Unlock()
						reconcileAfter = now.Add(reconcile.Next())
					} else {
						markInstanceOwner(ownerStarting)
					}
				}
			}
			if childTracked() && now.Sub(childStartTime()) >= lifecycle.StablePeriod {
				if reconcile.Attempts() > 0 {
					reconcile.Reset()
				}
				clearRestartError()
			}
		}

		// ---- listener self-healing: never "process alive + listener dead" ----
		if capture != nil {
			if nativeCaptureReady(cfg) {
				listenerFailures = 0
			} else {
				listenerFailures++
				if listenerFailures >= listenerFailureLimit {
					watchFailure(cfg, errNativeListenerLost)
					return errNativeListenerLost
				}
			}
		}

		if now.Sub(lastStatus) >= 2*time.Second {
			// Probe the real API owner so status never claims OWNED_TRACKED for a
			// process that has merely been started.
			markInstanceOwner(probeChildOwner(cfg))
			childMu.Lock()
			childPid := 0
			childStart := ""
			launchReason := ""
			if tracked != nil {
				childPid = tracked.pid
				childStart = tracked.started.UTC().Format(time.RFC3339Nano)
				launchReason = tracked.reason
			}
			alive := tracked != nil
			rc := restartCount
			lc := lastExitCode
			lt := ""
			if !lastExitTime.IsZero() {
				lt = lastExitTime.UTC().Format(time.RFC3339Nano)
			}
			le := lastRestartErr
			io := instanceOwner
			childMu.Unlock()
			status := map[string]any{"at": now.UTC().Format(time.RFC3339Nano), "watcher_pid": os.Getpid(), "watcher_mode": "watch", "desktop_running": present, "inspection_known": err == nil, "following": true, "routing_configured": capture != nil, "capture_entry_available": true, "native_listener_ready": capture != nil && listenerFailures == 0, "original_entry_preserved": true, "model_requests_generated": 0,
				"instance_owner":     io,
				"child_pid":          childPid,
				"child_start":        childStart,
				"child_alive":        alive,
				"launch_reason":      launchReason,
				"restart_count":      rc,
				"last_child_exit":    lc,
				"last_child_exit_at": lt,
				"last_restart_error": le,
				"reconcile_attempts": reconcile.Attempts(),
				"reconcile_degraded": reconcile.Degraded(),
			}
			_ = saveConfig(filepath.Join(cfg.Data, "lifecycle-status.json"), status)
			lastStatus = now
		}
		time.Sleep(time.Second)
	}
}
func ensureWatcher(dir string, cfg launchConfig) error {
	launcher, err := os.Executable()
	if err != nil {
		return err
	}
	command := child(launcher, "--watch")
	command.Dir = dir
	command.Env = cleanLaunchEnvironment(os.Environ(), cfg)
	if e := command.Start(); e != nil {
		return e
	}
	go command.Wait()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if nativeCaptureReady(cfg) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("本地采集器尚未就绪，请查看监视器运行记录")
}
func watchFailure(cfg launchConfig, err error) {
	data := cfg.Data
	if data == "" {
		cache, e := os.UserCacheDir()
		if e != nil {
			return
		}
		data = filepath.Join(cache, "CodexObservatory")
	}
	_ = os.MkdirAll(data, 0700)
	log, e := os.OpenFile(filepath.Join(data, "lifecycle-error.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e == nil {
		defer log.Close()
		fmt.Fprintf(log, "%s startup: %s\n", time.Now().UTC().Format(time.RFC3339Nano), err.Error())
	}
}
