//go:build js && wasm

// Browser inspection host. Animation, optics and mask compositor are exactly
// internal/floatui; browser-supplied system font masks are NOT Windows GDI tests.
package main

import (
	"codex-observatory/internal/floatui"
	"fmt"
	"math"
	"syscall/js"
	"time"
)

type entry struct {
	g   floatui.GlyphMask
	age uint64
}

var (
	animator               = floatui.NewAnimator()
	painter                = floatui.NewPainter()
	compositor             = floatui.NewCompositor()
	cache                  = map[string]entry{}
	clock                  uint64
	cacheBytes             int
	rgba                   []byte
	jsPixels               js.Value
	canvas                 = js.Global().Get("document").Call("getElementById", "panel")
	context                = canvas.Call("getContext", "2d")
	imgData                js.Value
	lastData               = -1.0
	lastPreset             = -99
	lastQuality            = -99
	lastQuota              = -99
	cycle                  = -1
	leaving                bool
	frameFunc, controlFunc js.Func
)

func glyph(l floatui.PaintLabel) floatui.GlyphMask {
	clock++
	key := fmt.Sprint(l.Bold) + l.Text
	if e, ok := cache[key]; ok {
		e.age = clock
		cache[key] = e
		return e.g
	}
	j := js.Global().Call("nacreGlyph", l.Text, l.Bold)
	w, h := j.Get("w").Int(), j.Get("h").Int()
	alpha := make([]byte, w*h)
	js.CopyBytesToGo(alpha, j.Get("a"))
	g := floatui.MakeGlyphMask(w, h, j.Get("advance").Float(), alpha)
	for len(cache) > 0 && (len(cache) >= 128 || cacheBytes+g.Bytes() > 4*1024*1024) {
		var oldest string
		age := ^uint64(0)
		for k, e := range cache {
			if e.age < age {
				oldest, age = k, e.age
			}
		}
		cacheBytes -= cache[oldest].g.Bytes()
		delete(cache, oldest)
	}
	cache[key] = entry{g, clock}
	cacheBytes += g.Bytes()
	return g
}
func main() {
	animator.BeginReveal(0)
	controlFunc = js.FuncOf(func(this js.Value, args []js.Value) any {
		action, t := args[0].String(), args[1].Float()
		switch action {
		case "light-off":
			compositor.LightEnabled = false
		case "light-on":
			compositor.LightEnabled = true
		case "reset":
			animator = floatui.NewAnimator()
			animator.BeginReveal(t)
			lastData = -1
			lastPreset = -99
			leaving = false
			cycle = -1

		case "reveal":
			animator.BeginReveal(t)
			leaving = false
		case "exit":
			animator.BeginExit(t)
			leaving = true
		case "show":
			animator.ShowImmediate()
			leaving = false
		case "hide":
			animator.HideImmediate()
			leaving = true
		}
		return nil
	})
	frameFunc = js.FuncOf(func(this js.Value, args []js.Value) any {
		t, preset, q, scale, quota := args[0].Float(), args[1].Int(), args[2].Int(), args[3].Float(), args[4].Int()
		start := time.Now()
		if preset < 0 {
			next := int(t / 40)
			if next != cycle {
				cycle = next
				leaving = false
				if q == 3 {
					animator.ShowImmediate()
				} else {
					animator.BeginReveal(t)
				}
			}
			if math.Mod(t, 40) >= 36 && !leaving {
				if q == 3 {
					animator.HideImmediate()
				} else {
					animator.BeginExit(t)
				}
				leaving = true
			}
		}
		if t-lastData >= .2 || preset != lastPreset || q != lastQuality || quota != lastQuota {
			s := floatui.PreviewSnapshot(t)
			if preset >= 0 {
				s.Rows = nil
				for i := 0; i < preset; i++ {
					s.Rows = append(s.Rows, floatui.Row{Thread: fmt.Sprint(i), Model: []string{"gpt-6.1-sol", "gpt-6.1-sol", "gpt-6-sol"}[i%3], Color: []floatui.Color{floatui.Green, floatui.Yellow, floatui.Red}[i%3], Activity: floatui.Running, Outputting: i%3 != 1, EstimatedTokenRate: 19.6 + 3*math.Sin(t*.3+float64(i))})
				}
			}
			if quota >= 0 {
				s.Quota.Remaining = float64(quota)
			}
			if quota == -2 {
				s.Quota.Known = false
			}
			if quota == -3 {
				s.Quota.Fresh = false
			}
			animator.Set(s, 0, 460, t)
			lastData, lastPreset, lastQuality, lastQuota = t, preset, q, quota
		}
		animator.SetReducedMotion(q == 3)
		s := animator.Frame(t)
		s.Quality = floatui.Quality(q)
		painting := painter.Paint(s)
		m := compositor.Render(s, painting, scale)
		for _, l := range painting.Labels {
			compositor.DrawGlyph(l, glyph(l))
		}
		rgba = compositor.CopyRGBA(rgba)
		if canvas.Get("width").Int() != m.Width || canvas.Get("height").Int() != m.Height || jsPixels.IsUndefined() {
			canvas.Set("width", m.Width)
			canvas.Set("height", m.Height)
			canvas.Get("style").Set("width", fmt.Sprintf("%gpx", s.Width+12))
			canvas.Get("style").Set("height", fmt.Sprintf("%gpx", s.Height+12))
			imgData = context.Call("createImageData", m.Width, m.Height)
			jsPixels = imgData.Get("data")
		}
		js.CopyBytesToJS(jsPixels, rgba)
		context.Call("putImageData", imgData, 0, 0)
		state := "运行"
		if s.Stage.Entering {
			state = "进入"
		}
		if s.Stage.Exiting {
			state = "退场"
		}
		if s.Stage.Hidden {
			state = "透明"
		}
		ls := compositor.LightStats()
		js.Global().Call("nacreStats", float64(time.Since(start).Microseconds())/1000, len(painting.Sprites), int(compositor.MaterialBuilds), state, ls.ActiveCells, ls.Bytes, ls.Peak, len(painting.Rings))
		return nil
	})
	js.Global().Set("nacreFrame", frameFunc)
	js.Global().Set("nacreControl", controlFunc)
	js.Global().Call("nacreReady")
	select {}
}
