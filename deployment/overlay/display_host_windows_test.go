//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func displayEventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		if f() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("deadline waiting for " + what)
}

func TestDisplayOverlayConfigurationAndHash(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "display.exe")
	if err := os.WriteFile(exe, []byte("MZ-isolated-display-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("MZ-isolated-display-fixture"))
	base := displayHostConfig{Enabled: true, Executable: exe, State: filepath.Join(dir, "state"), SHA256: hex.EncodeToString(digest[:])}
	config := filepath.Join(dir, "display-host.json")
	write := func(c displayHostConfig) {
		raw, _ := json.Marshal(c)
		if err := os.WriteFile(config, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(base)
	if _, err := readDisplayHostConfig(filepath.Join(dir, "backend.exe")); err != nil {
		t.Fatal(err)
	}
	if _, err := readDisplayHostConfig(exe); err == nil {
		t.Fatal("backend executable accepted as its own display")
	}
	for _, tc := range []struct {
		name string
		edit func(*displayHostConfig)
	}{
		{"disabled", func(c *displayHostConfig) { c.Enabled = false }},
		{"relative-executable", func(c *displayHostConfig) { c.Executable = "display.exe" }},
		{"relative-state", func(c *displayHostConfig) { c.State = "state" }},
		{"network-state", func(c *displayHostConfig) { c.State = `\\server\share\state` }},
		{"dot-segment", func(c *displayHostConfig) { c.State = dir + `\..\state` }},
		{"stream-path", func(c *displayHostConfig) { c.State = dir + `\state:alternate` }},
		{"trailing-dot", func(c *displayHostConfig) { c.State = dir + `\state.` }},
		{"too-long", func(c *displayHostConfig) { c.State = dir + `\` + strings.Repeat("s", displayPathLimit) }},
		{"wrong-hash", func(c *displayHostConfig) { c.SHA256 = strings.Repeat("0", 64) }},
		{"short-hash", func(c *displayHostConfig) { c.SHA256 = "00" }},
		{"non-exe", func(c *displayHostConfig) { c.Executable = filepath.Join(dir, "display.cmd") }},
		{"state-is-file", func(c *displayHostConfig) { c.State = exe }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.edit(&c)
			write(c)
			if _, err := readDisplayHostConfig(filepath.Join(dir, "backend.exe")); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	for _, raw := range []string{`{"enabled":true,"unexpected":1}`, `{"enabled":false} {}`, strings.Repeat(" ", displayConfigLimit+1)} {
		if err := os.WriteFile(config, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readDisplayHostConfig(filepath.Join(dir, "backend.exe")); err == nil {
			t.Fatal("malformed or oversized configuration accepted")
		}
	}
	write(base)
	if err := os.WriteFile(exe, []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyDisplayExecutable(base); err == nil {
		t.Fatal("replacement executable bypassed the pinned digest")
	}
}

func TestDisplayOverlayCommandIsDirectHiddenAndOwnerBound(t *testing.T) {
	c := displayHostConfig{Executable: `D:\Display Folder\display.exe`, State: `D:\Display Folder\state`}
	cmd := displayCommand(c, `D:\Data Folder`, 12345)
	want := []string{c.Executable, "--instance", `D:\Data Folder\instance.json`, "--state", c.State, "--owner-pid", "12345"}
	if strings.Join(cmd.Args, "\x00") != strings.Join(want, "\x00") || cmd.Path != c.Executable {
		t.Fatalf("unexpected launch arguments: %#v", cmd.Args)
	}
	if !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags != 0x08000000 || cmd.Dir != `D:\Display Folder` {
		t.Fatal("launch must be direct and console-hidden")
	}
	if displayWindowClass(`D:\状态\DISPLAY`, 321) != displayWindowClass(`d:\状态\display`, 321) ||
		displayWindowClass(`D:\状态\display`, 321) == displayWindowClass(`D:\状态\display`, 322) {
		t.Fatal("window identity does not separate owners or normalize case")
	}
}

type displayFake struct {
	done           chan error
	shows, closes  atomic.Int32
	kills          atomic.Int32
	closeCompletes bool
	noWindow       atomic.Bool
}

func newDisplayFake(complete bool) *displayFake {
	return &displayFake{done: make(chan error, 1), closeCompletes: complete}
}

func (f *displayFake) process() *displayProcess {
	return &displayProcess{pid: 99, done: f.done, show: func() bool { f.shows.Add(1); return !f.noWindow.Load() },
		close: func() {
			f.closes.Add(1)
			if f.closeCompletes {
				f.done <- nil
			}
		}, kill: func() { f.kills.Add(1); f.done <- nil }}
}

func displayConfigFixture(t *testing.T) (string, displayHostConfig, func(displayHostConfig)) {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "display.exe")
	raw := []byte("MZ-isolated-reopen-fixture")
	if err := os.WriteFile(exe, raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	config := displayHostConfig{Enabled: true, Executable: exe, State: filepath.Join(dir, "state"), SHA256: hex.EncodeToString(digest[:])}
	write := func(c displayHostConfig) {
		t.Helper()
		content, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "display-host.json"), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(config)
	return filepath.Join(dir, "backend.exe"), config, write
}

func TestDisplayOverlayReopenLoadsCurrentConfiguration(t *testing.T) {
	backend, original, write := displayConfigFixture(t)
	first, second := newDisplayFake(true), newDisplayFake(true)
	launched := make(chan displayHostConfig, 3)
	var starts, fallback atomic.Int32
	c := newDisplayController(nil, displayOperations{
		start: configuredDisplayStarter(backend, func(config displayHostConfig) (*displayProcess, error) {
			launched <- config
			if starts.Add(1) == 1 {
				return first.process(), nil
			}
			return second.process(), nil
		}),
		legacy: func() func() { fallback.Add(1); return func() {} }, showLegacy: func() {},
	})
	defer c.shutdown()
	displayEventually(t, "initial selected configuration", func() bool { return starts.Load() == 1 })
	if got := <-launched; got != original {
		t.Fatal("initial selection differs from the on-disk configuration")
	}

	invalid := original
	invalid.SHA256 = strings.Repeat("0", 64)
	write(invalid)
	c.requestShow()
	displayEventually(t, "existing child stays usable", func() bool { return first.shows.Load() > 0 })
	if starts.Load() != 1 || fallback.Load() != 0 {
		t.Fatal("showing an existing child reread or replaced its configuration")
	}

	updatedBytes := []byte("MZ-isolated-updated-display-fixture")
	if err := os.WriteFile(original.Executable, updatedBytes, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(updatedBytes)
	updated := original
	updated.SHA256 = hex.EncodeToString(digest[:])
	updated.State = filepath.Join(filepath.Dir(backend), "updated-state")
	write(updated)
	first.noWindow.Store(true)
	first.done <- nil
	c.requestShow()
	displayEventually(t, "updated configuration on reopening", func() bool { return starts.Load() == 2 })
	if got := <-launched; got != updated || fallback.Load() != 0 {
		t.Fatal("reopening used the prior digest or state selection")
	}
}

func TestDisplayOverlayReopenInvalidConfigurationFallsBack(t *testing.T) {
	for _, mode := range []string{"missing", "disabled", "digest-changed", "invalid-json"} {
		t.Run(mode, func(t *testing.T) {
			backend, original, write := displayConfigFixture(t)
			first := newDisplayFake(true)
			var starts, fallback, closed atomic.Int32
			c := newDisplayController(nil, displayOperations{
				start: configuredDisplayStarter(backend, func(config displayHostConfig) (*displayProcess, error) {
					starts.Add(1)
					return first.process(), nil
				}),
				legacy: func() func() { fallback.Add(1); return func() { closed.Add(1) } }, showLegacy: func() {},
			})
			displayEventually(t, "initial display before config change", func() bool { return starts.Load() == 1 })
			configPath := filepath.Join(filepath.Dir(backend), "display-host.json")
			var err error
			switch mode {
			case "missing":
				err = os.Remove(configPath)
			case "disabled":
				original.Enabled = false
				write(original)
			case "digest-changed":
				err = os.WriteFile(original.Executable, []byte("replaced-without-updating-config"), 0600)
			case "invalid-json":
				err = os.WriteFile(configPath, []byte("{"), 0600)
			}
			if err != nil {
				c.shutdown()
				t.Fatal(err)
			}
			first.noWindow.Store(true)
			first.done <- nil
			c.requestShow()
			displayEventually(t, "fallback after invalid new selection", func() bool { return fallback.Load() == 1 })
			c.shutdown()
			if starts.Load() != 1 || fallback.Load() != 1 || closed.Load() != 1 {
				t.Fatal("invalid new selection launched a child or repeated fallback")
			}
		})
	}
}

func TestDisplayOverlayRequestStormAndNormalReopen(t *testing.T) {
	before := runtime.NumGoroutine()
	var starts, legacy atomic.Int32
	first, second := newDisplayFake(true), newDisplayFake(true)
	c := newDisplayController(nil, displayOperations{
		start: func() (*displayProcess, error) {
			if starts.Add(1) == 1 {
				return first.process(), nil
			}
			return second.process(), nil
		},
		legacy: func() func() { legacy.Add(1); return func() {} }, showLegacy: func() {},
	})
	displayEventually(t, "first display", func() bool { return starts.Load() == 1 })
	var callers sync.WaitGroup
	for i := 0; i < 20; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			for j := 0; j < 500; j++ {
				c.requestShow()
			}
		}()
	}
	callers.Wait()
	displayEventually(t, "bounded show consumption", func() bool { return first.shows.Load() > 0 && len(c.show) == 0 })
	if starts.Load() != 1 || cap(c.show) != 1 || runtime.NumGoroutine() > before+4 {
		t.Fatal("show storm created extra launches, a backlog or workers")
	}
	first.done <- nil
	time.Sleep(25 * time.Millisecond)
	c.requestShow()
	displayEventually(t, "normal-close reopen", func() bool { return starts.Load() == 2 })
	c.shutdown()
	c.shutdown()
	if legacy.Load() != 0 || second.closes.Load() != 1 || second.kills.Load() != 0 {
		t.Fatal("normal display close affected fallback or graceful shutdown")
	}
	displayEventually(t, "controller worker exit", func() bool { return runtime.NumGoroutine() <= before+2 })
}

func TestDisplayOverlayStartupAndAbnormalExitFallbackOnce(t *testing.T) {
	for _, startFailure := range []bool{true, false} {
		var starts, fallback, closed, shows atomic.Int32
		child := newDisplayFake(true)
		c := newDisplayController(nil, displayOperations{
			start: func() (*displayProcess, error) {
				starts.Add(1)
				if startFailure {
					return nil, errors.New("isolated launch failure")
				}
				return child.process(), nil
			},
			legacy:     func() func() { fallback.Add(1); return func() { closed.Add(1) } },
			showLegacy: func() { shows.Add(1) },
		})
		displayEventually(t, "one launch attempt", func() bool { return starts.Load() == 1 })
		if !startFailure {
			child.done <- errors.New("isolated abnormal exit")
		}
		displayEventually(t, "legacy fallback", func() bool { return fallback.Load() == 1 })
		for i := 0; i < 1000; i++ {
			c.requestShow()
		}
		displayEventually(t, "legacy show", func() bool { return shows.Load() > 0 })
		c.shutdown()
		if starts.Load() != 1 || fallback.Load() != 1 || closed.Load() != 1 {
			t.Fatal("failure caused a restart loop or duplicate fallback")
		}
	}
}

func TestDisplayOverlayShowDuringCloseIsNotLost(t *testing.T) {
	for _, beforeExit := range []bool{true, false} {
		first, second := newDisplayFake(true), newDisplayFake(true)
		first.noWindow.Store(true)
		var starts atomic.Int32
		c := newDisplayController(nil, displayOperations{
			start: func() (*displayProcess, error) {
				if starts.Add(1) == 1 {
					return first.process(), nil
				}
				return second.process(), nil
			},
			legacy: func() func() { t.Error("unexpected fallback"); return func() {} }, showLegacy: func() {},
		})
		displayEventually(t, "initial launch", func() bool { return starts.Load() == 1 })
		if beforeExit {
			c.requestShow()
			displayEventually(t, "show during HWND teardown", func() bool { return first.shows.Load() > 0 })
			first.done <- nil
		} else {
			first.done <- nil
			c.requestShow()
		}
		displayEventually(t, "coalesced reopening", func() bool { return starts.Load() == 2 })
		c.shutdown()
		if starts.Load() != 2 {
			t.Fatal("close/reopen race duplicated the child")
		}
	}
}

func TestDisplayOverlayStartupShowDoesNotLatchForever(t *testing.T) {
	child := newDisplayFake(true)
	child.noWindow.Store(true)
	var starts atomic.Int32
	c := newDisplayController(nil, displayOperations{
		start:  func() (*displayProcess, error) { starts.Add(1); return child.process(), nil },
		legacy: func() func() { t.Error("unexpected fallback"); return func() {} }, showLegacy: func() {},
	})
	displayEventually(t, "initial launch", func() bool { return starts.Load() == 1 })
	c.requestShow()
	displayEventually(t, "show before HWND readiness", func() bool { return child.shows.Load() > 0 })
	child.noWindow.Store(false)
	displayEventually(t, "bounded readiness retry", func() bool { return child.shows.Load() >= 2 })
	child.done <- nil
	time.Sleep(150 * time.Millisecond)
	if starts.Load() != 1 {
		t.Fatal("stale startup show revived a normally closed window")
	}
	c.shutdown()
}

func TestDisplayOverlayShutdownIsBoundedAndNeverRestarts(t *testing.T) {
	stop := make(chan struct{})
	child := newDisplayFake(false)
	var starts, fallback atomic.Int32
	c := newDisplayController(stop, displayOperations{
		start:  func() (*displayProcess, error) { starts.Add(1); return child.process(), nil },
		legacy: func() func() { fallback.Add(1); return func() {} }, showLegacy: func() {},
		shutdownFor: 25 * time.Millisecond,
	})
	displayEventually(t, "child launch", func() bool { return starts.Load() == 1 })
	at := time.Now()
	close(stop)
	c.shutdown()
	if time.Since(at) > time.Second || child.closes.Load() != 1 || child.kills.Load() != 1 || fallback.Load() != 0 {
		t.Fatal("shutdown did not bound a stuck owned child")
	}
	c.requestShow()
	if starts.Load() != 1 {
		t.Fatal("show after shutdown launched another child")
	}
}

func TestDisplayOverlayCancelledOwnerDoesNotLaunch(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	c := newDisplayController(stop, displayOperations{
		start: func() (*displayProcess, error) {
			t.Error("launched after owner stopped")
			return nil, errors.New("unexpected")
		},
		legacy: func() func() { t.Error("fallback after owner stopped"); return func() {} }, showLegacy: func() {},
	})
	c.shutdown()
}

func TestDisplayOverlayExistingInstanceOnlySignalsOwner(t *testing.T) {
	data := t.TempDir()
	var shows atomic.Int32
	stop, err := startDisplayControlWindow(data, func() { shows.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	showFloating(data)
	displayEventually(t, "hidden owner control message", func() bool { return shows.Load() == 1 })
	if !showDisplayOwner(data) {
		t.Fatal("existing-instance owner could not be located")
	}
	displayEventually(t, "second owner control message", func() bool { return shows.Load() == 2 })
	if showDisplayOwner(filepath.Join(data, "different-owner")) {
		t.Fatal("unrelated owner received message")
	}
}

// The helper is this test executable itself. It creates only its own hidden HWND
// and performs no HTTP, collector, watcher, service or production-file action.
func TestMain(m *testing.M) {
	if os.Getenv("OBS_DISPLAY_OVERLAY_HELPER") == "startup-flags" {
		displayStartupFlagsHelper()
		return
	}
	if os.Getenv("OBS_DISPLAY_OVERLAY_HELPER") == "window" {
		state := ""
		owner := 0
		for i := 1; i+1 < len(os.Args); i++ {
			if os.Args[i] == "--state" {
				state = os.Args[i+1]
			}
			if os.Args[i] == "--owner-pid" {
				fmtSscanOwner(os.Args[i+1], &owner)
			}
		}
		os.Exit(displayHelperWindow(state, owner))
	}
	os.Exit(m.Run())
}

func fmtSscanOwner(text string, owner *int) {
	for _, c := range text {
		if c < '0' || c > '9' {
			*owner = 0
			return
		}
		*owner = *owner*10 + int(c-'0')
	}
}

func displayHelperWindow(state string, owner int) int {
	if state == "" || owner != os.Getppid() {
		return 2
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	instance, _, _ := kernel.NewProc("GetModuleHandleW").Call(0)
	name := u16(displayWindowClass(state, owner))
	proc := syscall.NewCallback(func(h uintptr, message uint32, wp, lp uintptr) uintptr {
		if message == 0x0010 {
			user.NewProc("DestroyWindow").Call(h)
			return 0
		}
		if message == 0x0002 {
			user.NewProc("PostQuitMessage").Call(0)
			return 0
		}
		r, _, _ := user.NewProc("DefWindowProcW").Call(h, uintptr(message), wp, lp)
		return r
	})
	wc := wndClass{Proc: proc, Instance: instance, Name: name}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if ok, _, _ := user.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc))); ok == 0 {
		return 3
	}
	h, _, _ := user.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(name)), 0, 0x80000000, 0, 0, 0, 0, 0, 0, instance, 0)
	if h == 0 {
		return 4
	}
	var msg winMsg
	for {
		ok, _, _ := user.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ok) <= 0 {
			break
		}
		user.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
		user.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
	}
	return 0
}

func TestDisplayOverlayRealOwnedChildReuseAndClose(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	dir := t.TempDir()
	config := displayHostConfig{Enabled: true, Executable: exe, State: filepath.Join(dir, "state"), SHA256: hex.EncodeToString(digest[:])}
	t.Setenv("OBS_DISPLAY_OVERLAY_HELPER", "window")
	first, err := launchDisplay(config, dir, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.kill)
	class := displayWindowClass(config.State, os.Getpid())
	displayEventually(t, "owned helper HWND", func() bool { return displayFindWindow(class, first.pid) != 0 })
	if displayFindWindow(class, first.pid+1) != 0 {
		t.Fatal("wrong PID accepted")
	}
	if displayParentPID(first.pid) != os.Getpid() {
		t.Fatal("helper lost direct-parent identity")
	}
	second, err := launchDisplay(config, dir, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if second.pid != first.pid || !second.show() {
		t.Fatal("existing owned helper was duplicated")
	}
	first.close()
	for _, child := range []*displayProcess{first, second} {
		select {
		case err := <-child.done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("owned helper did not close")
		}
	}
}
