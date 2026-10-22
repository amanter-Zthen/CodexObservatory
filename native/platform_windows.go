//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	kernel       = syscall.NewLazyDLL("kernel32.dll")
	user         = syscall.NewLazyDLL("user32.dll")
	shell        = syscall.NewLazyDLL("shell32.dll")
	pMessage     = user.NewProc("MessageBoxW")
	pShell       = shell.NewProc("ShellExecuteW")
	pCreateMutex = kernel.NewProc("CreateMutexW")
	pClose       = kernel.NewProc("CloseHandle")
	pMove        = kernel.NewProc("MoveFileExW")
)

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func isWindows() bool      { return true }
func replaceFile(from, to string) error {
	r, _, e := pMove.Call(uintptr(unsafe.Pointer(u16(from))), uintptr(unsafe.Pointer(u16(to))), 0x1|0x8)
	if r == 0 {
		return e
	}
	return nil
}
func message(title, text string) {
	pMessage.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), 0x40)
}
func openUI(url string) error {
	r, _, e := pShell.Call(0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(url))), 0, 0, 1)
	if r <= 32 {
		return fmt.Errorf("ShellExecute failed (%d): %v", r, e)
	}
	return nil
}
func openFolder(path string) error { return openUI(path) }
func instanceLock(data string) (func(), bool, error) {
	h, _, e := pCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(u16("Local\\CodexObservatory-"+shortHash(data)))))
	if h == 0 {
		return nil, false, e
	}
	if e == syscall.Errno(183) {
		pClose.Call(h)
		return func() {}, true, nil
	}
	return func() { pClose.Call(h) }, false, nil
}
func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00000200}
}

type jobBasic struct {
	PerProcess int64
	PerJob     int64
	Flags      uint32
	MinWork    uintptr
	MaxWork    uintptr
	Active     uint32
	Affinity   uintptr
	Priority   uint32
	Scheduling uint32
}
type ioCounters struct{ ReadOps, WriteOps, OtherOps, ReadBytes, WriteBytes, OtherBytes uint64 }
type jobExtended struct {
	Basic                                    jobBasic
	IO                                       ioCounters
	ProcessMem, JobMem, PeakProcess, PeakJob uintptr
}

func guardChild(cmd *exec.Cmd) (func(), func(), error) {
	create := kernel.NewProc("CreateJobObjectW")
	set := kernel.NewProc("SetInformationJobObject")
	assign := kernel.NewProc("AssignProcessToJobObject")
	open := kernel.NewProc("OpenProcess")
	terminate := kernel.NewProc("TerminateJobObject")
	job, _, e := create.Call(0, 0)
	if job == 0 {
		return nil, nil, e
	}
	info := jobExtended{}
	info.Basic.Flags = 0x2000
	r, _, e := set.Call(job, 9, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if r == 0 {
		pClose.Call(job)
		return nil, nil, e
	}
	process, _, e := open.Call(0x0100|0x0001, falseToUint(false), uintptr(cmd.Process.Pid))
	if process == 0 {
		pClose.Call(job)
		return nil, nil, e
	}
	defer pClose.Call(process)
	r, _, e = assign.Call(job, process)
	if r == 0 {
		pClose.Call(job)
		return nil, nil, e
	}
	var once sync.Once
	cleanup := func() { once.Do(func() { pClose.Call(job) }) }
	kill := func() { terminate.Call(job, 1) }
	return kill, cleanup, nil
}
func falseToUint(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

type wndClass struct {
	Size       uint32
	Style      uint32
	Proc       uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	Menu       *uint16
	Name       *uint16
	SmallIcon  uintptr
}
type point struct{ X, Y int32 }
type winMsg struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
	Private        uint32
}
type notifyIcon struct {
	Size        uint32
	Window      uintptr
	ID          uint32
	Flags       uint32
	Callback    uint32
	Icon        uintptr
	Tip         [128]uint16
	State       uint32
	StateMask   uint32
	Info        [256]uint16
	Timeout     uint32
	Title       [64]uint16
	InfoFlags   uint32
	GUID        [16]byte
	BalloonIcon uintptr
}

func startPlatform(url, data string, stop func()) func() {
	ready := make(chan uintptr, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		instance, _, _ := kernel.NewProc("GetModuleHandleW").Call(0)
		icon, _, _ := user.NewProc("LoadIconW").Call(0, 32516)
		className := u16("CodexObservatoryTray-" + shortHash(data))
		var hwnd uintptr
		shellNotify := shell.NewProc("Shell_NotifyIconW")
		ni := notifyIcon{ID: 1, Flags: 1 | 2 | 4, Callback: 0x8001, Icon: icon}
		ni.Size = uint32(unsafe.Sizeof(ni))
		copy(ni.Tip[:], syscall.StringToUTF16("Codex Observatory · 只读监控 · 双击打开"))
		taskbar, _, _ := user.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(u16("TaskbarCreated"))))
		proc := syscall.NewCallback(func(h uintptr, msg uint32, wp, lp uintptr) uintptr {
			switch msg {
			case 0x8001:
				if lp == 0x0203 {
					showFloating(data)
					_ = openUI(url)
				}
				if lp == 0x0205 {
					menu, _, _ := user.NewProc("CreatePopupMenu").Call()
					appendMenu := user.NewProc("AppendMenuW")
					appendMenu.Call(menu, 0, 1, uintptr(unsafe.Pointer(u16("打开仪表盘"))))
					appendMenu.Call(menu, 0, 2, uintptr(unsafe.Pointer(u16("打开数据目录"))))
					appendMenu.Call(menu, 0, 4, uintptr(unsafe.Pointer(u16("显示悬浮路由状态"))))
					appendMenu.Call(menu, 0x800, 0, 0)
					appendMenu.Call(menu, 0, 3, uintptr(unsafe.Pointer(u16("退出（停止采集）"))))
					var pt point
					user.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&pt)))
					user.NewProc("SetForegroundWindow").Call(h)
					choice, _, _ := user.NewProc("TrackPopupMenu").Call(menu, 0x100|2, uintptr(pt.X), uintptr(pt.Y), 0, h, 0)
					user.NewProc("DestroyMenu").Call(menu)
					switch choice {
					case 1:
						_ = openUI(url)
					case 2:
						_ = openFolder(data)
					case 3:
						stop()
					case 4:
						showFloating(data)
					}
				}
				return 0
			case 0x0010:
				shellNotify.Call(2, uintptr(unsafe.Pointer(&ni)))
				user.NewProc("DestroyWindow").Call(h)
				return 0
			case 0x0002:
				user.NewProc("PostQuitMessage").Call(0)
				return 0
			}
			if uintptr(msg) == taskbar {
				shellNotify.Call(0, uintptr(unsafe.Pointer(&ni)))
				return 0
			}
			r, _, _ := user.NewProc("DefWindowProcW").Call(h, uintptr(msg), wp, lp)
			return r
		})
		wc := wndClass{Proc: proc, Instance: instance, Name: className, Icon: icon}
		wc.Size = uint32(unsafe.Sizeof(wc))
		if r, _, _ := user.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			ready <- 0
			return
		}
		hwnd, _, _ = user.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(u16("Codex Observatory"))), 0, 0, 0, 0, 0, 0, 0, instance, 0)
		if hwnd == 0 {
			ready <- 0
			return
		}
		ni.Window = hwnd
		shellNotify.Call(0, uintptr(unsafe.Pointer(&ni)))
		ready <- hwnd
		var m winMsg
		for {
			r, _, _ := user.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if r == 0 || int32(r) == -1 {
				break
			}
			user.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&m)))
			user.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
	hwnd := <-ready
	return func() {
		if hwnd != 0 {
			user.NewProc("PostMessageW").Call(hwnd, 0x0010, 0, 0)
		}
	}
}

var _ = os.Getenv
