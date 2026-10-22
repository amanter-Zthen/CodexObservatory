//go:build windows

package main

import (
	"codex-observatory/internal/floatui"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
	"unsafe"
)

func nativeSample(count int) floatui.Snapshot {
	s := floatui.Snapshot{Mode: "synthetic", Quota: floatui.QuotaView{Known: true, Fresh: true, Remaining: 87}}
	for i := 0; i < count; i++ {
		color := []floatui.Color{floatui.Green, floatui.Red, floatui.Yellow}[i%3]
		s.Rows = append(s.Rows, floatui.Row{Thread: fmt.Sprint(i), Model: "gpt-6.1-sol", Color: color, Activity: floatui.Running, Outputting: true, EstimatedTokenRate: 19.6})
	}
	return s
}

func TestNativeRendererPixelsAndResourceRelease(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	current, _, _ := kernel.NewProc("GetCurrentProcess").Call()
	resources := func() uintptr { n, _, _ := user.NewProc("GetGuiResources").Call(current, 0); return n }
	before := resources()
	for n := 0; n < 3; n++ {
		r, err := newFloatRenderer(1.5, 700)
		if err != nil {
			t.Fatal(err)
		}
		a := floatui.NewAnimator()
		a.Set(nativeSample(3), 0, 650, 0)
		scene := a.Frame(1)
		path := filepath.Join(t.TempDir(), "synthetic.png")
		err = r.savePNG(path, scene)
		r.close()
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		im, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if im.Bounds().Dx() != int(math.Ceil((scene.Width+12)*1.5)) {
			t.Fatal(im.Bounds())
		}
		_, _, _, center := im.At(im.Bounds().Dx()/2, im.Bounds().Dy()/2).RGBA()
		_, _, _, corner := im.At(0, 0).RGBA()
		if center == 0 || corner != 0 {
			t.Fatal("native drawing did not create an opaque panel with a transparent margin", center, corner)
		}
	}
	if after := resources(); after > before+2 {
		t.Fatalf("GDI resources increased from %d to %d", before, after)
	}
}

func TestNativeWindowCreatedVisibleAndClosed(t *testing.T) {
	a, _ := testApp(t)
	a.preview = true
	a.started = time.Now().Add(-18 * time.Second)
	closeWindow := a.startFloating()
	name := u16("CodexObservatoryRouting-" + shortHash(a.data))
	hwnd, _, _ := user.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(name)), 0)
	if hwnd == 0 {
		closeWindow()
		t.Fatal("native host window was not created")
	}
	visible, _, _ := user.NewProc("IsWindowVisible").Call(hwnd)
	var bounds rect
	user.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
	closeWindow()
	if visible == 0 || bounds.Right-bounds.Left < 300 || bounds.Bottom-bounds.Top < int32(88*floatDPI(hwnd))-1 {
		t.Fatal("native window is hidden or has invalid dimensions", visible, bounds)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		valid, _, _ := user.NewProc("IsWindow").Call(hwnd)
		if valid == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native window did not close")
}

func TestNativeDrawTimings(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, count := range []int{0, 1, 4, 8} {
		r, err := newFloatRenderer(1.5, 700)
		if err != nil {
			t.Fatal(err)
		}
		a := floatui.NewAnimator()
		a.Set(nativeSample(count), 0, 650, 0)
		for i := 0; i < 10; i++ {
			r.draw(a.Frame(2 + float64(i)/60))
		}
		samples := make([]float64, 120)
		for i := range samples {
			scene := a.Frame(3 + float64(i)/60)
			start := time.Now()
			r.draw(scene)
			samples[i] = float64(time.Since(start).Nanoseconds()) / 1e6
		}
		r.close()
		sort.Float64s(samples)
		var total float64
		for _, value := range samples {
			total += value
		}
		t.Logf("native_draw rows=%d dpi=144 mean_ms=%.4f p95_ms=%.4f max_ms=%.4f frames=120 scope=native_CPU_compositor_and_GDI_glyphs_draw_only", count, total/float64(len(samples)), samples[113], samples[119])
	}
}

func TestNativeGlyphCacheReuseAcrossContinuousSizes(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r, err := newFloatRenderer(1.5, 400)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	a := floatui.NewAnimator()
	a.Set(nativeSample(3), 0, 400, 0)
	scene := a.Frame(2)
	label := floatui.PaintLabel{Text: "gpt-6.1-sol", Bold: true, Size: 17.8}
	g, ok := r.glyph(label)
	if !ok {
		t.Fatal("native glyph failed")
	}
	found := false
	for _, v := range g.Alpha {
		if v > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("native glyph mask is empty")
	}
	first := r.GlyphBuilds
	for i := 0; i < 100; i++ {
		label.Size = 13.01 + float64(i)*.04
		label.X = float64(i) * .03
		r.glyph(label)
	}
	if r.GlyphBuilds != first {
		t.Fatal("continuous sizing rerasterizes glyph")
	}
	for i := 0; i < 300; i++ {
		label.Text = fmt.Sprintf("%d.1/s", i)
		r.glyph(label)
	}
	if len(r.glyphs) > 128 || r.glyphBytes > glyphCacheLimit {
		t.Fatal("unbounded native glyph cache")
	}
	r.draw(scene)
}
