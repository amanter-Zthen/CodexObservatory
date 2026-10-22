//go:build windows

package main

import (
	"codex-observatory/internal/floatui"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

func floatDPI(hwnd uintptr) float64 {
	name := "GetDpiForSystem"
	if hwnd != 0 {
		name = "GetDpiForWindow"
	}
	proc := user.NewProc(name)
	if proc.Find() != nil {
		return 1
	}
	dpi, _, _ := proc.Call(hwnd)
	if dpi < 96 {
		return 1
	}
	return math.Min(4, float64(dpi)/96)
}

// Use the actual host monitor rather than clamping every window to the
// primary screen. Negative coordinates are legitimate on a left-hand monitor.
func floatingWorkArea(hwnd uintptr) rect {
	type monitorInfo struct {
		Size          uint32
		Monitor, Work rect
		Flags         uint32
	}
	info := monitorInfo{}
	info.Size = uint32(unsafe.Sizeof(info))
	monitor, _, _ := user.NewProc("MonitorFromWindow").Call(hwnd, 2)
	if monitor != 0 {
		ok, _, _ := user.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info)))
		if ok != 0 {
			return info.Work
		}
	}
	var work rect
	user.NewProc("SystemParametersInfoW").Call(48, 0, uintptr(unsafe.Pointer(&work)), 0)
	if work.Right <= work.Left || work.Bottom <= work.Top {
		work = rect{0, 0, 1920, 1080}
	}
	return work
}

func (a *App) startFloating() func() {
	ready := make(chan uintptr, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if p := user.NewProc("SetThreadDpiAwarenessContext"); p.Find() == nil {
			p.Call(^uintptr(3))
		}
		dpiScale := floatDPI(0)
		var savedView struct {
			Zoom float64 `json:"zoom"`
		}
		_ = loadJSON(filepath.Join(a.data, "window-view.json"), &savedView)
		zoom := floatui.NewViewZoom(savedView.Zoom, dpiScale)
		scale := dpiScale * zoom.Applied
		work := floatingWorkArea(0)
		maxHeight := math.Max(76, float64(work.Bottom-work.Top)*.7/scale-12)
		// Reserve one bounded output surface for all menu sizes on this monitor.
		// Menu zoom reuses its GDI resources and canonical glyph masks.
		renderer, err := newFloatRenderer(math.Min(4, dpiScale*2), floatui.MaxPanelHeight)
		if err != nil {
			log.Printf("floating renderer: %v", err)
			ready <- 0
			return
		}
		renderer.scale = scale
		defer func() { renderer.close() }()
		type zoomJob struct {
			version    uint64
			value, dpi float64
			scene      floatui.Scene
		}
		type zoomReady struct {
			job        zoomJob
			compositor *floatui.Compositor
		}
		jobs := make(chan zoomJob, 1)
		prepared := make(chan zoomReady, 1)
		preferences := make(chan float64, 1)
		workerStop, workerDone := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(workerDone)
			save := func(value float64) {
				if e := saveJSON(filepath.Join(a.data, "window-view.json"), struct {
					Zoom float64 `json:"zoom"`
				}{value}); e != nil {
					log.Printf("window view save: %v", e)
				}
			}
			for {
				select {
				case <-workerStop:
					select {
					case value := <-preferences:
						save(value)
					default:
					}
					return
				case value := <-preferences:
					save(value)
				case job := <-jobs:
					ready := zoomReady{job, floatui.PrepareCompositor(job.scene, job.dpi*job.value)}
					select {
					case prepared <- ready:
					case <-workerStop:
						return
					}
				}
			}
		}()
		defer func() { close(workerStop); <-workerDone }()
		menuOpen := false // guards popup re-entry; animation keeps its timer
		animator := floatui.NewAnimator()
		governor := floatui.NewGovernor()
		switch a.visualQuality {
		case "full":
			governor.SetManual(0)
		case "economy":
			governor.SetManual(2)
		case "still":
			governor.SetManual(3)
		}
		pendingExit := uint8(0) // 1 hides; 2 destroys after the final transparent frame
		var pendingZoom *zoomReady
		zoomLeaving := false
		zoomBusy := false
		demoCycle := -1
		demoLeaving := false
		frameCount := uint64(0)
		start := time.Now()
		offset := 0
		interval := uint32(17)
		var lastRevision uint64
		lastOffset := -1
		requestZoom := func(choice uintptr) {
			now := time.Since(start).Seconds()
			if zoomBusy || pendingExit != 0 || animator.Frame(now).Stage.Entering {
				return
			}
			value := zoom.Target
			switch choice {
			case 15:
				value = 1
			case 16:
				value += .25
			case 17:
				value -= .25
			default:
				return
			}
			if zoom.Set(value, dpiScale, time.Now()) {
				zoomBusy, zoomLeaving = true, true
				if governor.Quality == floatui.Still {
					animator.HideImmediate()
				} else {
					animator.BeginExit(now)
				}
				select {
				case <-preferences:
				default:
				}
				preferences <- zoom.Target
			}
		}
		syncMonitor := func(hwnd uintptr, nextScale float64, force bool) {
			nextWork := floatingWorkArea(hwnd)
			nextScale = math.Min(4, nextScale)
			if nextScale <= 0 {
				nextScale = dpiScale
			}
			if !force && nextWork == work && nextScale == dpiScale {
				return
			}
			nextZoom := floatui.ClampViewZoom(zoom.Applied, nextScale)
			effectiveScale := nextScale * nextZoom
			nextMax := math.Max(76, float64(nextWork.Bottom-nextWork.Top)*.7/effectiveScale-12)
			next, er := newFloatRenderer(math.Min(4, nextScale*2), floatui.MaxPanelHeight)
			if er != nil {
				log.Printf("floating monitor resize: %v", er)
				return
			}
			copyGlyphCache(next, renderer)
			next.scale = effectiveScale
			renderer.close()
			renderer, scale, maxHeight, work = next, effectiveScale, nextMax, nextWork
			dpiScale = nextScale
			zoom.Applied = nextZoom
			zoom.Set(zoom.Target, nextScale, time.Now())
			zoom.Version++ // background preparation belongs to its original DPI
			lastOffset = -1
		}
		lastMonitorCheck := time.Now()
		frame := func(hwnd uintptr) {
			visible, _, _ := user.NewProc("IsWindowVisible").Call(hwnd)
			if visible == 0 {
				return
			}
			select {
			case ready := <-prepared:
				if ready.job.dpi == dpiScale && zoom.Receive(ready.job.version, ready.job.value) {
					pendingZoom = &ready
				} else if ready.job.dpi != dpiScale {
					zoom.Retry(time.Now())
				}
			default:
			}
			if time.Since(lastMonitorCheck) >= time.Second {
				syncMonitor(hwnd, floatDPI(hwnd), false)
				lastMonitorCheck = time.Now()
			}
			now := time.Since(start).Seconds()
			if a.preview && !a.inspectWindow && pendingExit == 0 && !zoomBusy {
				cycle := int(now / 40)
				if cycle != demoCycle {
					demoCycle = cycle
					demoLeaving = false
					if governor.Quality == floatui.Still {
						animator.ShowImmediate()
					} else {
						animator.BeginReveal(now)
					}
				}
				if math.Mod(now, 40) >= 36 && !demoLeaving {
					if governor.Quality == floatui.Still {
						animator.HideImmediate()
					} else {
						animator.BeginExit(now)
					}
					demoLeaving = true
				}
			}
			snapshot := a.readFloatingSnapshot()
			if snapshot.Revision != lastRevision || offset != lastOffset {
				animator.Set(snapshot, offset, maxHeight, now)
				lastRevision, lastOffset = snapshot.Revision, offset
			}
			animator.SetReducedMotion(governor.Quality == floatui.Still)
			scene := animator.Frame(now)
			if pendingZoom != nil && !zoom.Matches(pendingZoom.job.version, pendingZoom.job.value) {
				pendingZoom = nil
			}
			if pendingExit == 0 {
				if pendingZoom != nil && !zoomBusy && !zoomLeaving && !scene.Stage.Entering {
					zoomBusy = true
					zoomLeaving = true
					if governor.Quality == floatui.Still {
						animator.HideImmediate()
					} else {
						animator.BeginExit(now)
					}
					scene = animator.Frame(now)
				}
				if zoomLeaving && scene.Stage.Hidden {
					if pendingZoom != nil && zoom.Apply(pendingZoom.job.version, pendingZoom.job.value) {
						renderer.scale, scale = dpiScale*zoom.Applied, dpiScale*zoom.Applied
						renderer.compositor = pendingZoom.compositor
						maxHeight = math.Max(76, float64(work.Bottom-work.Top)*.7/scale-12)
						animator.Set(snapshot, offset, maxHeight, now)
						lastRevision, lastOffset = snapshot.Revision, offset
						pendingZoom = nil
						zoomLeaving = false
						if governor.Quality == floatui.Still {
							animator.ShowImmediate()
						} else {
							animator.BeginReveal(now)
						}
						scene = animator.Frame(now)
					} else if zoom.Target == zoom.Applied {
						zoomLeaving = false
						if governor.Quality == floatui.Still {
							animator.ShowImmediate()
						} else {
							animator.BeginReveal(now)
						}
						scene = animator.Frame(now)
					}
				}
			}
			if zoomBusy && !zoomLeaving && !scene.Stage.Entering && !scene.Stage.Hidden {
				zoomBusy = false
			}
			scene.Quality = governor.Quality
			if version, value, ok := zoom.Start(time.Now()); ok && pendingExit == 0 {
				select {
				case jobs <- zoomJob{version, value, dpiScale, scene}:
				default:
					zoom.Retry(time.Now())
				}
			}
			measured := time.Now()
			renderer.draw(scene)
			if !renderer.present(hwnd, scene) {
				log.Printf("floating present failed")
			} else if a.floatFrameObserved != nil {
				a.floatFrameObserved(scene)
			}
			governor.Observe(float64(time.Since(measured).Nanoseconds()) / 1e6)
			frameCount++
			if frameCount%900 == 0 {
				log.Printf("visual_render mean_ms=%.3f tier=%s atlas_bytes=%d glyph_bytes=%d material_builds=%d", governor.MeanMS, governor.Quality.String(), floatui.AtlasBytes, renderer.glyphBytes, renderer.compositor.MaterialBuilds)
			}
			if scene.Stage.Hidden && pendingExit != 0 {
				user.NewProc("KillTimer").Call(hwnd, 1)
				if pendingExit == 2 {
					user.NewProc("DestroyWindow").Call(hwnd)
				} else {
					user.NewProc("ShowWindow").Call(hwnd, 0)
				}
				return
			}
			var bounds rect
			user.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
			x := max(work.Left, min(bounds.Left, work.Right-(bounds.Right-bounds.Left)))
			y := max(work.Top, min(bounds.Top, work.Bottom-(bounds.Bottom-bounds.Top)))
			if x != bounds.Left || y != bounds.Top {
				user.NewProc("SetWindowPos").Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0, 0x15)
			}
			next := governor.Interval(scene.Transitioning, len(scene.Rows) > 0, governor.Quality == floatui.Still)
			if zoom.Target != zoom.Applied {
				next = min(next, 50)
			}
			if next != interval {
				user.NewProc("SetTimer").Call(hwnd, 1, uintptr(next), 0)
				interval = next
			}
		}
		beginExit := func(hwnd uintptr, kind uint8) {
			visible, _, _ := user.NewProc("IsWindowVisible").Call(hwnd)
			if visible == 0 || governor.Quality == floatui.Still {
				user.NewProc("KillTimer").Call(hwnd, 1)
				if kind == 2 {
					user.NewProc("DestroyWindow").Call(hwnd)
				} else {
					user.NewProc("ShowWindow").Call(hwnd, 0)
				}
				return
			}
			if kind > pendingExit {
				pendingExit = kind
			}
			animator.BeginExit(time.Since(start).Seconds())
			user.NewProc("SetTimer").Call(hwnd, 1, 17, 0)
			interval = 17
		}
		module, _, _ := kernel.NewProc("GetModuleHandleW").Call(0)
		name := u16("CodexObservatoryRouting-" + shortHash(a.data))
		proc := syscall.NewCallback(func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
			switch msg {
			case 0x000f:
				var ps paintStruct
				user.NewProc("BeginPaint").Call(hwnd, uintptr(unsafe.Pointer(&ps)))
				user.NewProc("EndPaint").Call(hwnd, uintptr(unsafe.Pointer(&ps)))
				return 0
			case 0x0014:
				return 1
			case 0x0113:
				frame(hwnd)
				return 0
			case 0x0018: // hidden windows have zero animation timer work
				if wp != 0 {
					pendingExit = 0
					if governor.Quality != floatui.Still {
						animator.BeginReveal(time.Since(start).Seconds())
					} else {
						animator.ShowImmediate()
					}
					user.NewProc("SetTimer").Call(hwnd, 1, 17, 0)
					interval = 17
				} else {
					user.NewProc("KillTimer").Call(hwnd, 1)
				}
				return 0
			case 0x0201:
				if zoomBusy {
					return 0
				}
				user.NewProc("ReleaseCapture").Call()
				user.NewProc("SendMessageW").Call(hwnd, 0x00a1, 2, 0)
				return 0
			case 0x020a:
				delta := int16(wp >> 16)
				if delta < 0 {
					offset++
				} else {
					offset = max(0, offset-1)
				}
				frame(hwnd)
				return 0
			case 0x0205:
				if zoomBusy || menuOpen {
					return 0
				}
				menuOpen = true
				if a.floatMenuObserved != nil {
					a.floatMenuObserved(true)
				}
				choice := a.floatingMenu(hwnd, zoom.Target, zoom.Applied, dpiScale)
				menuOpen = false
				if a.floatMenuObserved != nil {
					a.floatMenuObserved(false)
				}
				switch choice {
				case 18:
					a.requestRouteSync()
				case 15, 16, 17:
					requestZoom(choice)
				case 2:
					beginExit(hwnd, 1)
				case 3:
					a.stop()
				case 10:
					governor.SetManual(-1)
				case 11:
					governor.SetManual(0)
				case 12:
					governor.SetManual(2)
				case 13:
					governor.SetManual(3)
				case 14:
					if governor.Quality == floatui.Still {
						animator.ShowImmediate()
					} else {
						animator.BeginReveal(time.Since(start).Seconds())
					}
				}
				return 0
			case 0x0111: // same lightweight path for native zoom commands
				if wp&0xffff == 18 {
					a.requestRouteSync()
				} else {
					requestZoom(wp & 0xffff)
				}
				return 0
			case 0x02e0: // WM_DPICHANGED
				// Adopt the suggested monitor position; UpdateLayeredWindow below
				// applies the current animated DIP dimensions at the new scale.
				if lp != 0 {
					var suggested rect
					current, _, _ := kernel.NewProc("GetCurrentProcess").Call()
					copied, _, _ := kernel.NewProc("ReadProcessMemory").Call(current, lp, uintptr(unsafe.Pointer(&suggested)), unsafe.Sizeof(suggested), 0)
					if copied != 0 {
						user.NewProc("SetWindowPos").Call(hwnd, 0, uintptr(suggested.Left), uintptr(suggested.Top), 0, 0, 0x15)
					}
				}
				syncMonitor(hwnd, float64(wp&0xffff)/96, false)
				frame(hwnd)
				return 0
			case 0x007e, 0x0232: // display change / end of interactive move
				syncMonitor(hwnd, floatDPI(hwnd), msg == 0x007e)
				frame(hwnd)
				return 0
			case 0x0010:
				beginExit(hwnd, 2)
				return 0
			case 0x0002:
				user.NewProc("KillTimer").Call(hwnd, 1)
				user.NewProc("PostQuitMessage").Call(0)
				return 0
			}
			value, _, _ := user.NewProc("DefWindowProcW").Call(hwnd, uintptr(msg), wp, lp)
			return value
		})
		class := wndClass{Proc: proc, Instance: module, Name: name}
		class.Size = uint32(unsafe.Sizeof(class))
		if v, _, _ := user.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class))); v == 0 {
			ready <- 0
			return
		}
		x := max(work.Left+10, work.Right-int32((floatui.PanelWidth+32)*scale))
		y := work.Top + 90
		exStyle := uintptr(0x80000 | 8 | 0x80 | 0x08000000)
		title := "Codex Observatory · 潮汐状态"
		if a.inspectWindow {
			exStyle = 0x80000 | 8
			title = "Nacre 0.4.6 · 隔离检视"
		}
		hwnd, _, _ := user.NewProc("CreateWindowExW").Call(exStyle, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(u16(title))), 0x80000000, uintptr(x), uintptr(y), uintptr((floatui.PanelWidth+12)*scale), uintptr(102*scale), 0, 0, module, 0)
		if hwnd == 0 {
			ready <- 0
			return
		}
		user.NewProc("ShowWindow").Call(hwnd, 4)
		user.NewProc("SetTimer").Call(hwnd, 1, 17, 0)
		frame(hwnd)
		ready <- hwnd
		var message winMsg
		for {
			v, _, _ := user.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			if int32(v) <= 0 {
				break
			}
			user.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
			user.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
		}
	}()
	hwnd := <-ready
	if hwnd == 0 {
		return a.startLegacyFloating()
	}
	return func() {
		user.NewProc("PostMessageW").Call(hwnd, 0x0010, 0, 0)
		// main must not exit before the requested disappearance finishes.
		select {
		case <-done:
		case <-time.After(1900 * time.Millisecond):
			log.Printf("floating close exceeded animation grace period")
		}
	}
}

func (a *App) floatingMenu(hwnd uintptr, target, applied, dpi float64) uintptr {
	rows := a.readFloatingSnapshot().Rows
	completed := a.pendingCompletions()
	menu, _, _ := user.NewProc("CreatePopupMenu").Call()
	defer user.NewProc("DestroyMenu").Call(menu)
	for i, row := range rows {
		if row.Activity == floatui.Completed {
			continue
		}
		label := fmt.Sprintf("%s · %s", row.Model, shortHash(row.Thread)[:6])
		flags := uintptr(0)
		if !jumpThreadID.MatchString(row.Thread) || a.preview {
			flags = 3
		}
		user.NewProc("AppendMenuW").Call(menu, flags, uintptr(100+i), uintptr(unsafe.Pointer(u16(label))))
	}
	user.NewProc("AppendMenuW").Call(menu, 0x800, 0, 0)
	user.NewProc("AppendMenuW").Call(menu, 3, 0, uintptr(unsafe.Pointer(u16(fmt.Sprintf("待查看对话（%d）", len(completed))))))
	for i, notice := range completed {
		user.NewProc("AppendMenuW").Call(menu, 0, uintptr(1000+i), uintptr(unsafe.Pointer(u16(completionMenuLabel(notice)))))
	}
	user.NewProc("AppendMenuW").Call(menu, 0x800, 0, 0)
	zoomLabel := fmt.Sprintf("窗口大小：%.0f%%", target*100)
	if target != applied {
		zoomLabel += "（稍后应用）"
	}
	user.NewProc("AppendMenuW").Call(menu, 3, 0, uintptr(unsafe.Pointer(u16(zoomLabel))))
	for _, item := range []struct {
		id       uintptr
		text     string
		disabled bool
	}{{16, "放大 25%", nextViewZoom(target, 1, dpi) == target}, {17, "缩小 25%", nextViewZoom(target, -1, dpi) == target}, {15, "恢复 100%", target == 1}} {
		flags := uintptr(0)
		if item.disabled {
			flags = 3
		}
		user.NewProc("AppendMenuW").Call(menu, flags, item.id, uintptr(unsafe.Pointer(u16(item.text))))
	}
	user.NewProc("AppendMenuW").Call(menu, 0x800, 0, 0)
	flags := uintptr(0)
	label := "重新同步路由"
	if a.routeSyncBusy.Load() {
		flags, label = 3, "正在重新同步路由…"
	} else if a.preview || a.demo {
		flags = 3
	}
	user.NewProc("AppendMenuW").Call(menu, flags, 18, uintptr(unsafe.Pointer(u16(label))))
	if status := a.routeSyncMenuLabel(); status != "" {
		user.NewProc("AppendMenuW").Call(menu, 3, 0, uintptr(unsafe.Pointer(u16(status))))
	}
	user.NewProc("AppendMenuW").Call(menu, 0x800, 0, 0)
	for _, item := range []struct {
		id   uintptr
		text string
	}{{1, "打开总控制台"}, {14, "重播启动动画"}, {10, "光效：自动调节"}, {11, "光效：完整"}, {12, "光效：节能"}, {13, "光效：减少动态"}, {2, "柔和隐藏（继续采集）"}, {3, "退出监视器"}} {
		user.NewProc("AppendMenuW").Call(menu, 0, item.id, uintptr(unsafe.Pointer(u16(item.text))))
	}
	var cursor point
	user.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&cursor)))
	user.NewProc("SetForegroundWindow").Call(hwnd)
	// The popup appears immediately. The OS menu fade has its own modal startup
	// and can delay owner-window timers; the instrument owns its animation clock.
	choice, _, _ := user.NewProc("TrackPopupMenu").Call(menu, 0x4102, uintptr(cursor.X), uintptr(cursor.Y), 0, hwnd, 0)
	// Keep consecutive popup invocations reliable after their nested message loop.
	user.NewProc("PostMessageW").Call(hwnd, 0, 0, 0)
	if choice >= 1000 && int(choice-1000) < len(completed) {
		if err := a.openCompleted(completed[choice-1000].ID, openUI); err != nil {
			user.NewProc("MessageBoxW").Call(hwnd, uintptr(unsafe.Pointer(u16("未能打开对话，已保留完成提示。"))), uintptr(unsafe.Pointer(u16("Codex Observatory"))), 0x40)
		}
		return 0
	}
	if choice >= 100 && int(choice-100) < len(rows) {
		id := rows[choice-100].Thread
		if jumpThreadID.MatchString(id) {
			_ = openUI("codex://threads/" + id)
		}
	}
	switch choice {
	case 1:
		_ = openUI(a.address + "/#" + a.token)
	}
	return choice
}

func renderFloatingFixture(path string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r, err := newFloatRenderer(1.5, 700)
	if err != nil {
		return err
	}
	defer r.close()
	snapshot := floatui.Snapshot{Mode: "synthetic", Quota: floatui.QuotaView{Known: true, Fresh: true, Remaining: 87}, Rows: []floatui.Row{
		{Thread: "synthetic-one", Model: "gpt-6.1-sol", Color: floatui.Green, Activity: floatui.Running, Outputting: true, EstimatedTokenRate: 19.6},
		{Thread: "synthetic-two", Model: "gpt-6-sol", Color: floatui.Red, Activity: floatui.Running, Outputting: true, EstimatedTokenRate: 12.4},
		{Thread: "synthetic-three", Model: "gpt-6.1-sol", Color: floatui.Yellow, Activity: floatui.Running},
	}}
	a := floatui.NewAnimator()
	a.Set(snapshot, 0, 650, 0)
	return r.savePNG(path, a.Frame(1))
}
