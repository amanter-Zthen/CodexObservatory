package floatui

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestMaterialPremultipliedAndTransparentCorners(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 2} {
		m := MaterialFor(PanelWidth, 145, scale)
		if len(m.BGRA) != m.Width*m.Height*4 {
			t.Fatal("buffer size")
		}
		for i := 0; i < len(m.BGRA); i += 4 {
			a := m.BGRA[i+3]
			if m.BGRA[i] > a || m.BGRA[i+1] > a || m.BGRA[i+2] > a {
				t.Fatalf("not premultiplied at %d", i)
			}
		}
		if m.BGRA[3] != 0 {
			t.Fatal("corner is not transparent")
		}
		center := ((m.Height/2)*m.Width + m.Width/2) * 4
		if m.BGRA[center+3] != 255 {
			t.Fatal("reading surface must shield desktop text")
		}
		img := m.Image()
		if img.Pix[center] != m.BGRA[center+2] {
			t.Fatal("BGRA channel conversion")
		}
	}
}
func TestQuotaPlacementAndTextRetargetWithoutTeleport(t *testing.T) {
	a := NewAnimator()
	s := sample(0)
	a.Set(s, 0, 500, 0)
	before := a.Frame(1)
	a.Set(sample(1), 0, 500, 1)
	after := a.Frame(1)
	if before.QuotaX != after.QuotaX || before.QuotaY != after.QuotaY {
		t.Fatal("quota teleported")
	}
	f := a.Frame(2)
	if f.QuotaX != 22 || f.QuotaFont != 29 {
		t.Fatal("quota failed to reach reading rail", f.QuotaX, f.QuotaFont)
	}
	s = sample(1)
	s.Quota.Remaining = 100
	a.Set(s, 0, 500, 2)
	f = a.Frame(2)
	if f.PreviousQuotaText != "87%" || f.QuotaMix != 0 {
		t.Fatal("quota replacement is not a crossfade")
	}
	f = a.Frame(3)
	if f.QuotaText != "100%" || f.QuotaFont != 27 {
		t.Fatal("full quota would crowd model marker")
	}
}
func TestPaintRouteInkAndSpeedContract(t *testing.T) {
	for _, color := range []Color{Green, Red, Yellow} {
		a := NewAnimator()
		s := sample(1)
		s.Rows[0].Color = color
		a.Set(s, 0, 500, 0)
		p := NewPainter()
		out := p.Paint(a.Frame(2))
		model, rate := false, false
		for _, l := range out.Labels {
			if l.Text == "wanted" {
				model = true
				if l.Color != RouteInk(color) {
					t.Fatal("model ink")
				}
			}
			if l.Text == "19.6/s" {
				rate = true
				if l.Color != RouteInk(color) {
					t.Fatal("speed ink")
				}
			}
			if strings.Contains(l.Text, "≈") || l.Text == "0.0/s" {
				t.Fatal("unsupported speed label")
			}
		}
		if !model || !rate {
			t.Fatal("missing evidence labels")
		}
		s.Rows[0].Outputting = false
		a.Set(s, 0, 500, 2)
		out = p.Paint(a.Frame(3))
		for _, l := range out.Labels {
			if strings.HasSuffix(l.Text, "/s") {
				t.Fatal("completed speed remained visible")
			}
		}
	}
}
func TestPaintUnknownStaleAndEmpty(t *testing.T) {
	for _, c := range []struct {
		known, fresh bool
		text         string
	}{{false, false, "额度未获取"}, {true, false, "周剩余 · 旧"}, {true, true, "周剩余"}} {
		a := NewAnimator()
		s := sample(0)
		s.Quota.Known = c.known
		s.Quota.Fresh = c.fresh
		a.Set(s, 0, 500, 0)
		scene := a.Frame(2)
		out := NewPainter().Paint(scene)
		found := false
		if len(scene.Rows) != 0 || len(scene.Bands) != 3 {
			t.Fatal("empty scene fabricated sessions")
		}
		for _, l := range out.Labels {
			if l.Text == c.text {
				found = true
			}
			if !c.fresh && l.WaterClip {
				t.Fatal("stale or missing quota uses fresh wet ink")
			}
			if strings.Contains(l.Text, "wanted") {
				t.Fatal("empty scene model")
			}
		}
		if !found {
			t.Fatal("missing quota qualification", c.text)
		}
		if !c.known && scene.WaterCount != 0 {
			t.Fatal("missing quota fabricated a water level")
		}
	}
}
func TestPaintingFiniteInResizeAndCrowdedViewport(t *testing.T) {
	a := NewAnimator()
	p := NewPainter()
	for i := 0; i < 360; i++ {
		now := float64(i) / 60
		if i%30 == 0 {
			a.Set(sample([]int{0, 1, 3, 16, 32, 2}[i/30%6]), i/30, 340, now)
		}
		s := a.Frame(now)
		out := p.Paint(s)
		if s.Height < 76 || s.Height > 340.0001 {
			t.Fatal("height outside envelope")
		}
		if len(s.Bands) != 3+len(s.Rows) {
			t.Fatal("one shared-field fine stream per row")
		}
		for _, shape := range out.Shapes {
			if shape.Count > len(shape.Points) {
				t.Fatal("polygon exceeds native scratch buffer")
			}
			for _, v := range []float64{shape.X, shape.Y, shape.W, shape.H} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatal("nonfinite ellipse")
				}
			}
			for _, v := range shape.Points[:shape.Count] {
				if math.IsNaN(v.X) || math.IsNaN(v.Y) || math.IsInf(v.Y, 0) {
					t.Fatal("nonfinite path")
				}
			}
		}
	}
}
func TestInkContrastOnProceduralReadingSurface(t *testing.T) {
	// Measured design contrast for opaque text centres, not an OS-level audit of
	// antialiased edges. Foreground modes must all beat 4.5:1 at model positions.
	lum := func(r, g, b byte) float64 {
		c := func(v byte) float64 {
			x := float64(v) / 255
			if x <= .04045 {
				return x / 12.92
			}
			return math.Pow((x+.055)/1.055, 2.4)
		}
		return .2126*c(r) + .7152*c(g) + .0722*c(b)
	}
	for _, n := range []int{1, 3, 16, 32} {
		a := NewAnimator()
		a.Set(sample(n), 0, 340, 0)
		scene := a.Frame(2)
		m := MaterialFor(scene.Width, scene.Height, 1)
		for _, row := range scene.Rows {
			x, y := int(row.X+50+FrameMargin), int(row.Y+10+FrameMargin)
			i := (y*m.Width + x) * 4
			bg := lum(m.BGRA[i+2], m.BGRA[i+1], m.BGRA[i])
			for _, c := range []Color{Green, Red, Yellow} {
				ink := RouteInk(c)
				fg := lum(byte(ink>>16), byte(ink>>8), byte(ink))
				ratio := (bg + .05) / (fg + .05)
				if ratio < 4.5 {
					t.Fatalf("contrast n=%d color=%s: %.2f", n, c, ratio)
				}
			}
		}
	}
}

var benchmarkPainting Painting
var benchmarkMaterial Material

func BenchmarkPaint(b *testing.B) {
	for _, n := range []int{0, 1, 3, 16, 32} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			a := NewAnimator()
			a.Set(sample(n), 0, 600, 0)
			scene := a.Frame(2)
			p := NewPainter()
			p.Paint(scene)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkPainting = p.Paint(scene)
			}
		})
	}
}
func BenchmarkMaterial(b *testing.B) {
	for _, scale := range []float64{1, 1.5, 2} {
		b.Run(fmt.Sprint(scale), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchmarkMaterial = MaterialFor(PanelWidth, 165, scale)
			}
		})
	}
}

func BenchmarkMaterialReuse(b *testing.B) {
	m := MaterialFor(PanelWidth, 340, 2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m = MaterialInto(m, PanelWidth, 165+float64(i%3), 2)
	}
	benchmarkMaterial = m
}
