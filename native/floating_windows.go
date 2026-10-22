//go:build windows

package main

import (
	"fmt"
	"math"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type rect struct{ Left, Top, Right, Bottom int32 }

func showFloating(data string) {
	class := u16("CodexObservatoryRouting-" + shortHash(data))
	h, _, _ := user.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(class)), 0)
	if h != 0 {
		user.NewProc("ShowWindow").Call(h, 4)
	}
}

var jumpThreadID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type paintStruct struct {
	DC                 uintptr
	Erase              int32
	Paint              rect
	Restore, IncUpdate int32
	Reserved           [32]byte
}

func (a *App) floatingLines() (string, string, uint32, []string, []string, float64) {
	a.mu.RLock()
	rows := a.liveRouteSnapshot()
	a.mu.RUnlock()
	bridge := a.bridgeStatus()
	active, _ := bridge["active"].(bool)
	activePIDs := map[int]bool{}
	if clients, ok := bridge["clients"].([]Obj); ok {
		for _, c := range clients {
			activePIDs[int(num(c["client_pid"]))] = true
		}
	}
	title, detail, color := "等待接入实时路由", "原 Codex 入口启动后自动接入", uint32(0xa6a6a6)
	if bridge["reason"] == "desktop_restart_required" {
		title = "重启 Codex 接入实时路由"
		detail = "以后随 Codex 自动启动和退出"
	}
	if bridge["reason"] == "passive_only" {
		title = "本地监控 · 路由未验证"
		detail = "实时采集需从专用入口启动 Codex"
	}
	labels, links := []string{}, []string{}
	count, matched, bad := 0, 0, 0
	for _, r := range rows {
		if active && !activePIDs[r.PID] {
			continue
		}
		at, e := time.Parse(time.RFC3339Nano, r.At)
		if e != nil || time.Since(at) > 10*time.Minute {
			continue
		}
		count++
		status := "待确认"
		switch r.Verdict {
		case "match":
			matched++
			status = "标签一致"
		case "mismatch":
			bad++
			status = "模型标签不同"
		case "request_failed":
			status = "请求失败"
		case "conflicting_evidence":
			status = "证据冲突"
		}
		labels = append(labels, fmt.Sprintf("%s · %s → %s · %s", shortHash(r.Thread)[:6], r.Requested, first(r.Reported, "待响应"), status))
		link := ""
		if jumpThreadID.MatchString(r.Thread) {
			link = "codex://threads/" + r.Thread
		}
		links = append(links, link)
		if count == 1 {
			detail = first(r.Requested, "等待请求模型") + " → " + first(r.Reported, "待响应") + " · " + at.Local().Format("15:04:05")
		}
	}
	chosenDetail := detail
	if active {
		title = "采集器在线 · 等待实际响应"
		detail = "只显示原工作请求的响应证据"
		if count > 0 {
			detail = chosenDetail
			if bad > 0 {
				title = fmt.Sprintf("路由标签异常 · %d 个会话", bad)
				color = 0x5b5bff
			} else if matched == count {
				title = fmt.Sprintf("响应模型标签一致 · %d 个对话", count)
				color = 0x80d060
			} else {
				title = fmt.Sprintf("%d 个已确认 · %d 个待确认", matched, count-matched)
				color = 0x5eb5ee
			}
		}
	}
	if !active && count > 0 {
		title = "采集器离线 · 显示最近结果"
		color = 0xa6a6a6
	}
	if clients, ok := bridge["clients"].([]Obj); ok {
		for _, c := range clients {
			if num(c["metadata_write_errors"]) > 0 {
				title = "路由记录保存失败 · 待检查"
				color = 0x5eb5ee
			}
		}
	}
	weekly := -1.0
	latest := ""
	a.mu.RLock()
	for _, f := range a.files {
		for _, q := range f.Summary.Quotas {
			for _, w := range []*Window{q.Primary, q.Secondary} {
				if w != nil && w.Minutes != nil && *w.Minutes == 10080 && q.At > latest {
					latest = q.At
					weekly = math.Max(0, math.Min(100, 100-w.Used))
				}
			}
		}
	}
	a.mu.RUnlock()
	if a.demo {
		title = "演示 · " + title
	}
	return title, detail, color, labels, links, weekly
}
func (a *App) startLegacyFloating() func() {
	ready := make(chan uintptr, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		module, _, _ := kernel.NewProc("GetModuleHandleW").Call(0)
		className := u16("CodexObservatoryRouting-" + shortHash(a.data))
		gdi := syscall.NewLazyDLL("gdi32.dll")
		proc := syscall.NewCallback(func(h uintptr, msg uint32, wp, lp uintptr) uintptr {
			switch msg {
			case 0x000f:
				var ps paintStruct
				dc, _, _ := user.NewProc("BeginPaint").Call(h, uintptr(unsafe.Pointer(&ps)))
				title, detail, color, _, _, weekly := a.floatingLines()
				brush, _, _ := gdi.NewProc("CreateSolidBrush").Call(0x242424)
				area := rect{0, 0, 340, 108}
				user.NewProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&area)), brush)
				gdi.NewProc("DeleteObject").Call(brush)
				font, _, _ := gdi.NewProc("CreateFontW").Call(uintptr(^uintptr(15)), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16("Segoe UI"))))
				previous, _, _ := gdi.NewProc("SelectObject").Call(dc, font)
				gdi.NewProc("SetBkMode").Call(dc, 1)
				draw := func(text string, r rect, c uint32) {
					gdi.NewProc("SetTextColor").Call(dc, uintptr(c))
					user.NewProc("DrawTextW").Call(dc, uintptr(unsafe.Pointer(u16(text))), uintptr(len(syscall.StringToUTF16(text))-1), uintptr(unsafe.Pointer(&r)), 0x20|0x8000)
				}
				draw(title, rect{88, 14, 330, 39}, color)
				draw(detail, rect{88, 42, 330, 66}, 0xd8d8d8)
				draw(first(a.floatingSpeed(), "拖动移动 · 右键选对话 / 打开控制台"), rect{88, 73, 334, 100}, 0xaaaaaa)
				pen, _, _ := gdi.NewProc("CreatePen").Call(0, 6, 0x777777)
				oldPen, _, _ := gdi.NewProc("SelectObject").Call(dc, pen)
				nullBrush, _, _ := gdi.NewProc("GetStockObject").Call(5)
				oldBrush, _, _ := gdi.NewProc("SelectObject").Call(dc, nullBrush)
				gdi.NewProc("Ellipse").Call(dc, 12, 19, 77, 84)
				if weekly > 0 {
					gdi.NewProc("SelectObject").Call(dc, oldPen)
					gdi.NewProc("DeleteObject").Call(pen)
					pen, _, _ = gdi.NewProc("CreatePen").Call(0, 6, 0xc78f51)
					gdi.NewProc("SelectObject").Call(dc, pen)
					gdi.NewProc("SetArcDirection").Call(dc, 2)
					angle := weekly/100*2*math.Pi - math.Pi/2
					endX, endY := int32(44+32*math.Cos(angle)), int32(51+32*math.Sin(angle))
					gdi.NewProc("Arc").Call(dc, 12, 19, 77, 84, 44, 19, uintptr(endX), uintptr(endY))
				}
				draw(func() string {
					if weekly < 0 {
						return "周 —"
					}
					return fmt.Sprintf("%.0f%%", weekly)
				}(), rect{19, 40, 78, 62}, 0xffffff)
				draw("周剩余", rect{18, 85, 80, 106}, 0xaaaaaa)
				gdi.NewProc("SelectObject").Call(dc, oldPen)
				gdi.NewProc("SelectObject").Call(dc, oldBrush)
				gdi.NewProc("DeleteObject").Call(pen)
				gdi.NewProc("SelectObject").Call(dc, previous)
				gdi.NewProc("DeleteObject").Call(font)
				user.NewProc("EndPaint").Call(h, uintptr(unsafe.Pointer(&ps)))
				return 0
			case 0x0113:
				user.NewProc("InvalidateRect").Call(h, 0, 0)
				return 0
			case 0x0201:
				user.NewProc("ReleaseCapture").Call()
				user.NewProc("SendMessageW").Call(h, 0x00a1, 2, 0)
				return 0
			case 0x0205:
				_, _, _, labels, links, _ := a.floatingLines()
				menu, _, _ := user.NewProc("CreatePopupMenu").Call()
				appendMenu := user.NewProc("AppendMenuW")
				for i, label := range labels {
					appendMenu.Call(menu, 0, uintptr(100+i), uintptr(unsafe.Pointer(u16(label))))
				}
				appendMenu.Call(menu, 0, 1, uintptr(unsafe.Pointer(u16("打开总控制台"))))
				appendMenu.Call(menu, 0, 18, uintptr(unsafe.Pointer(u16("重新同步路由"))))
				appendMenu.Call(menu, 0, 2, uintptr(unsafe.Pointer(u16("隐藏悬浮状态（继续采集）"))))
				appendMenu.Call(menu, 0, 3, uintptr(unsafe.Pointer(u16("退出监视器"))))
				var pt point
				user.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&pt)))
				user.NewProc("SetForegroundWindow").Call(h)
				choice, _, _ := user.NewProc("TrackPopupMenu").Call(menu, 0x100|2, uintptr(pt.X), uintptr(pt.Y), 0, h, 0)
				user.NewProc("DestroyMenu").Call(menu)
				if choice >= 100 && int(choice-100) < len(links) {
					link := links[choice-100]
					if strings.HasPrefix(link, "codex://threads/") {
						_ = openUI(link)
					}
				}
				switch choice {
				case 18:
					a.requestRouteSync()
				case 1:
					_ = openUI(a.address + "/#" + a.token)
				case 2:
					user.NewProc("ShowWindow").Call(h, 0)
				case 3:
					a.stop()
				}
				return 0
			case 0x0010:
				user.NewProc("DestroyWindow").Call(h)
				return 0
			case 0x0002:
				user.NewProc("PostQuitMessage").Call(0)
				return 0
			}
			r, _, _ := user.NewProc("DefWindowProcW").Call(h, uintptr(msg), wp, lp)
			return r
		})
		wc := wndClass{Proc: proc, Instance: module, Name: className}
		wc.Size = uint32(unsafe.Sizeof(wc))
		if r, _, _ := user.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			ready <- 0
			return
		}
		width, _, _ := user.NewProc("GetSystemMetrics").Call(0)
		x := int32(width) - 370
		if x < 10 {
			x = 10
		}
		hwnd, _, _ := user.NewProc("CreateWindowExW").Call(8|0x80|0x08000000, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(u16("Codex Observatory · 实时路由"))), 0x80000000, uintptr(x), 90, 340, 108, 0, 0, module, 0)
		if hwnd == 0 {
			ready <- 0
			return
		}
		user.NewProc("SetTimer").Call(hwnd, 1, 500, 0)
		user.NewProc("ShowWindow").Call(hwnd, 4)
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
	h := <-ready
	return func() {
		if h != 0 {
			user.NewProc("PostMessageW").Call(h, 0x0010, 0, 0)
		}
	}
}
