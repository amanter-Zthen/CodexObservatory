//go:build windows

// Build-only overlay. The frozen 0.4.8 native tree is never edited.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	displayConfigLimit = 16 * 1024
	displayPathLimit   = 2048
	displayExeLimit    = 128 * 1024 * 1024
	displayShowMessage = 0x8002
)

type displayHostConfig struct {
	Enabled    bool   `json:"enabled"`
	Executable string `json:"executable"`
	State      string `json:"state"`
	SHA256     string `json:"sha256"`
}

func displayLocalPath(p string) bool {
	if len(utf16.Encode([]rune(p))) > displayPathLimit || len(p) < 4 ||
		!filepath.IsAbs(p) || p[1] != ':' || p[2] != '\\' ||
		!((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) ||
		filepath.Clean(p) != p || strings.TrimSpace(p) != p ||
		strings.ContainsAny(p, "\x00\r\n\t*?\"<>|/") || strings.Contains(p[2:], ":") {
		return false
	}
	for _, component := range strings.Split(p[3:], "\\") {
		if component == "" || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return false
		}
	}
	return true
}

func readDisplayHostConfig(backendExe string) (displayHostConfig, error) {
	var c displayHostConfig
	f, err := os.Open(filepath.Join(filepath.Dir(backendExe), "display-host.json"))
	if err != nil {
		return c, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, displayConfigLimit+1))
	if err != nil || len(raw) > displayConfigLimit {
		return c, errors.New("display configuration exceeds its size limit")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&c); err != nil {
		return c, errors.New("invalid display configuration")
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return c, errors.New("display configuration has trailing content")
	}
	if !c.Enabled {
		return c, errors.New("external display disabled")
	}
	if !displayLocalPath(c.Executable) || !displayLocalPath(c.State) ||
		!strings.EqualFold(filepath.Ext(c.Executable), ".exe") {
		return c, errors.New("display paths must be canonical absolute local paths")
	}
	if strings.EqualFold(c.Executable, filepath.Clean(backendExe)) {
		return c, errors.New("display executable must differ from the backend")
	}
	digest, err := hex.DecodeString(c.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return c, errors.New("display executable digest is invalid")
	}
	c.SHA256 = strings.ToLower(c.SHA256)
	if info, stateErr := os.Stat(c.State); stateErr == nil && !info.IsDir() {
		return c, errors.New("display state path is not a directory")
	} else if stateErr != nil && !os.IsNotExist(stateErr) {
		return c, errors.New("display state path unavailable")
	}
	return c, verifyDisplayExecutable(c)
}

func verifyDisplayExecutable(c displayHostConfig) error {
	f, err := os.Open(c.Executable)
	if err != nil {
		return errors.New("display executable unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > displayExeLimit {
		return errors.New("display executable exceeds its file limits")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, displayExeLimit+1))
	if err != nil || n != info.Size() || hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
		return errors.New("display executable digest mismatch")
	}
	return nil
}

// Resolve the on-disk selection only for a new launch. Showing a live child
// uses that child's existing identity, so an in-place deployment cannot disturb it.
func configuredDisplayStarter(backendExe string, launch func(displayHostConfig) (*displayProcess, error)) func() (*displayProcess, error) {
	return func() (*displayProcess, error) {
		config, err := readDisplayHostConfig(backendExe)
		if err != nil {
			return nil, err
		}
		return launch(config)
	}
}

// Match the host's actual UTF16/towlower, nonstandard FNV seed and decimal format.
var displayLower = syscall.NewLazyDLL("ucrtbase.dll").NewProc("towlower")

func displayWindowClass(state string, owner int) string {
	h := uint64(1469598103934665603)
	for _, code := range utf16.Encode([]rune(state)) {
		lower, _, _ := displayLower.Call(uintptr(code))
		h = (h ^ uint64(uint16(lower))) * uint64(1099511628211)
	}
	name := "CodexObservatoryDisplay-" + strconv.FormatUint(h, 10)
	if owner > 0 {
		name += "-" + strconv.Itoa(owner)
	}
	return name
}

func displayCommand(c displayHostConfig, data string, owner int) *exec.Cmd {
	cmd := exec.Command(c.Executable, "--instance", filepath.Join(data, "instance.json"),
		"--state", c.State, "--owner-pid", strconv.Itoa(owner))
	cmd.Dir = filepath.Dir(c.Executable)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}

type displayProcess struct {
	pid   int
	done  <-chan error
	show  func() bool
	close func()
	kill  func()
}

type displayOperations struct {
	start       func() (*displayProcess, error)
	legacy      func() func()
	showLegacy  func()
	shutdownFor time.Duration
}

// One controller, one replaceable show request, at most one process waiter.
// There is no automatic crash/restart loop.
type displayController struct {
	show chan struct{}
	stop chan struct{}
	done chan struct{}
	once sync.Once
	ops  displayOperations
	ctx  <-chan struct{}
}

func newDisplayController(ctx <-chan struct{}, ops displayOperations) *displayController {
	if ops.shutdownFor <= 0 {
		ops.shutdownFor = 7 * time.Second
	}
	c := &displayController{show: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), ops: ops, ctx: ctx}
	go c.run()
	return c
}

func (c *displayController) requestShow() {
	select {
	case <-c.stop:
		return
	default:
	}
	select {
	case c.show <- struct{}{}:
	default:
	}
}

func (c *displayController) shutdown() {
	c.once.Do(func() { close(c.stop) })
	<-c.done
}

func (c *displayController) run() {
	defer close(c.done)
	var child *displayProcess
	var exited <-chan error
	var legacyClose func()
	var reopenPending bool
	var retryTimer *time.Timer
	var retry <-chan time.Time
	clearRetry := func() {
		if retryTimer != nil {
			retryTimer.Stop()
		}
		retryTimer, retry = nil, nil
	}
	scheduleRetry := func() {
		if retryTimer == nil {
			retryTimer = time.NewTimer(100 * time.Millisecond)
			retry = retryTimer.C
		}
	}
	useLegacy := func() {
		if legacyClose == nil {
			legacyClose = c.ops.legacy()
		}
	}
	startOrShow := func() {
		if legacyClose != nil {
			c.ops.showLegacy()
			return
		}
		if child != nil {
			reopenPending = !child.show()
			if reopenPending {
				scheduleRetry()
			} else {
				clearRetry()
			}
			return
		}
		var err error
		child, err = c.ops.start()
		if err != nil {
			log.Printf("external display unavailable; using original UI: %v", err)
			child = nil
			useLegacy()
			return
		}
		exited = child.done
	}
	closeOwned := func() {
		clearRetry()
		if legacyClose != nil {
			legacyClose()
		}
		if child != nil {
			child.close()
			timer := time.NewTimer(c.ops.shutdownFor)
			defer timer.Stop()
			select {
			case <-exited:
			case <-timer.C:
				// Only our verified child, after the host's 6 s graceful close.
				child.kill()
				select {
				case <-exited:
				case <-time.After(time.Second):
					log.Printf("owned display process did not acknowledge shutdown")
				}
			}
		}
	}
	stopping := func() bool {
		select {
		case <-c.stop:
			return true
		case <-c.ctx:
			return true
		default:
			return false
		}
	}
	if stopping() {
		return
	}
	startOrShow()
	for {
		select {
		case <-c.stop:
			closeOwned()
			return
		case <-c.ctx:
			closeOwned()
			return
		case <-c.show:
			if stopping() {
				closeOwned()
				return
			}
			startOrShow()
		case <-retry:
			retryTimer, retry = nil, nil
			if child != nil && reopenPending {
				reopenPending = !child.show()
				if reopenPending {
					scheduleRetry()
				}
			}
		case err := <-exited:
			child, exited = nil, nil
			shouldReopen := reopenPending
			reopenPending = false
			clearRetry()
			if stopping() {
				closeOwned()
				return
			}
			// Exit 0 is a normal display-only close. A later show starts it again.
			if err != nil {
				log.Printf("external display exited unexpectedly; using original UI")
				useLegacy()
			} else if shouldReopen {
				// A show during the final HWND teardown belongs to the next
				// process, not to the just-completed normal close.
				startOrShow()
			}
		}
	}
}

func displayFindWindow(class string, expectedPID int) uintptr {
	h, _, _ := user.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(u16(class))), 0)
	if h == 0 {
		return 0
	}
	var pid uint32
	user.NewProc("GetWindowThreadProcessId").Call(h, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || (expectedPID > 0 && pid != uint32(expectedPID)) {
		return 0
	}
	return h
}

func displayProcessImage(pid int) string {
	handle, err := syscall.OpenProcess(0x1000, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	ok, _, _ := kernel.NewProc("QueryFullProcessImageNameW").Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:size])
}

func displayParentPID(pid int) int {
	h, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer syscall.CloseHandle(h)
	var item syscall.ProcessEntry32
	item.Size = uint32(unsafe.Sizeof(item))
	for err = syscall.Process32First(h, &item); err == nil; err = syscall.Process32Next(h, &item) {
		if item.ProcessID == uint32(pid) {
			return int(item.ParentProcessID)
		}
	}
	return 0
}

func launchDisplay(c displayHostConfig, data string, owner int) (*displayProcess, error) {
	if !displayLocalPath(data) || owner <= 0 {
		return nil, errors.New("display owner data path is invalid")
	}
	if err := verifyDisplayExecutable(c); err != nil {
		return nil, err
	}
	class := displayWindowClass(c.State, owner)
	var process *os.Process
	var wait func() error
	if existing := displayFindWindow(class, 0); existing != 0 {
		var pid uint32
		user.NewProc("GetWindowThreadProcessId").Call(existing, uintptr(unsafe.Pointer(&pid)))
		if displayParentPID(int(pid)) != owner || !strings.EqualFold(filepath.Clean(displayProcessImage(int(pid))), c.Executable) {
			return nil, errors.New("existing display is not owned by this backend")
		}
		var err error
		process, err = os.FindProcess(int(pid))
		if err != nil {
			return nil, err
		}
		wait = func() error {
			state, err := process.Wait()
			if err != nil {
				return err
			}
			if !state.Success() {
				return fmt.Errorf("display exited with status %d", state.ExitCode())
			}
			return nil
		}
	} else {
		cmd := displayCommand(c, data, owner)
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		process, wait = cmd.Process, cmd.Wait
	}
	done := make(chan error, 1)
	p := &displayProcess{pid: process.Pid, done: done}
	post := func(message uintptr) bool {
		h := displayFindWindow(class, p.pid)
		if h == 0 {
			return false
		}
		ok, _, _ := user.NewProc("PostMessageW").Call(h, message, 0, 0)
		return ok != 0
	}
	p.show = func() bool { return post(displayShowMessage) }
	p.close = func() { post(0x0010) }
	p.kill = func() { _ = process.Kill() }
	go func() { done <- wait(); close(done) }()
	p.show()
	return p, nil
}

var displayControllers = struct {
	sync.Mutex
	items map[string]*displayController
}{items: make(map[string]*displayController)}

func (a *App) startFloating() func() {
	if a.preview {
		return a.startLegacyFloating()
	}
	exe, err := os.Executable()
	if err != nil {
		return a.startLegacyFloating()
	}
	displayControllers.Lock()
	if old := displayControllers.items[a.data]; old != nil {
		displayControllers.Unlock()
		old.requestShow()
		return func() {}
	}
	if _, err := readDisplayHostConfig(exe); err != nil {
		displayControllers.Unlock()
		return a.startLegacyFloating()
	}
	c := newDisplayController(a.ctxDone, displayOperations{
		start: configuredDisplayStarter(exe, func(config displayHostConfig) (*displayProcess, error) {
			return launchDisplay(config, a.data, os.Getpid())
		}),
		legacy: a.startLegacyFloating, showLegacy: func() { showLegacyFloating(a.data) },
	})
	displayControllers.items[a.data] = c
	displayControllers.Unlock()
	closeControl, err := startDisplayControlWindow(a.data, c.requestShow)
	if err != nil {
		log.Printf("display reopen control unavailable: %v", err)
		c.shutdown()
		displayControllers.Lock()
		delete(displayControllers.items, a.data)
		displayControllers.Unlock()
		return a.startLegacyFloating()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			c.shutdown()
			if closeControl != nil {
				closeControl()
			}
			displayControllers.Lock()
			delete(displayControllers.items, a.data)
			displayControllers.Unlock()
		})
	}
}

// Both tray actions and a second, short-lived backend invocation come here.
// The latter only posts to the established owner; it never launches a display.
func showFloating(data string) {
	displayControllers.Lock()
	c := displayControllers.items[data]
	displayControllers.Unlock()
	if c != nil {
		c.requestShow()
		return
	}
	if showDisplayOwner(data) {
		return
	}
	showLegacyFloating(data)
}

func displayControlClass(data string) string {
	return "CodexObservatoryDisplayControl-" + shortHash(data)
}

func showDisplayOwner(data string) bool {
	h := displayFindWindow(displayControlClass(data), 0)
	if h == 0 {
		return false
	}
	var pid uint32
	user.NewProc("GetWindowThreadProcessId").Call(h, uintptr(unsafe.Pointer(&pid)))
	exe, err := os.Executable()
	if err != nil || !strings.EqualFold(filepath.Clean(displayProcessImage(int(pid))), filepath.Clean(exe)) {
		return false
	}
	ok, _, _ := user.NewProc("PostMessageW").Call(h, displayShowMessage, 0, 0)
	return ok != 0
}

func startDisplayControlWindow(data string, show func()) (func(), error) {
	ready, done := make(chan uintptr, 1), make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		instance, _, _ := kernel.NewProc("GetModuleHandleW").Call(0)
		className := u16(displayControlClass(data))
		procedure := syscall.NewCallback(func(hwnd uintptr, message uint32, wp, lp uintptr) uintptr {
			switch message {
			case displayShowMessage:
				show()
				return 0
			case 0x0010:
				user.NewProc("DestroyWindow").Call(hwnd)
				return 0
			case 0x0002:
				user.NewProc("PostQuitMessage").Call(0)
				return 0
			}
			r, _, _ := user.NewProc("DefWindowProcW").Call(hwnd, uintptr(message), wp, lp)
			return r
		})
		wc := wndClass{Proc: procedure, Instance: instance, Name: className}
		wc.Size = uint32(unsafe.Sizeof(wc))
		if ok, _, _ := user.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc))); ok == 0 {
			ready <- 0
			return
		}
		defer user.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(className)), instance)
		// A hidden top-level window can be located by the existing-instance entry.
		hwnd, _, _ := user.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(className)), 0, 0x80000000, 0, 0, 0, 0, 0, 0, instance, 0)
		ready <- hwnd
		if hwnd == 0 {
			return
		}
		var message winMsg
		for {
			ok, _, _ := user.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			if int32(ok) <= 0 {
				break
			}
			user.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
			user.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
		}
	}()
	hwnd := <-ready
	if hwnd == 0 {
		return nil, errors.New("cannot create display owner control")
	}
	var once sync.Once
	return func() {
		once.Do(func() { user.NewProc("PostMessageW").Call(hwnd, 0x0010, 0, 0) })
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}, nil
}
