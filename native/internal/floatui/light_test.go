package floatui

import (
	"bytes"
	"fmt"
	"math"
	"testing"
)

func TestBloomDoesNotIlluminateTheWholeWhiteShell(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(0), 0, 400, 0)
	s := a.Frame(2)
	s.WaterCount = 0
	c := NewCompositor()
	m := append([]byte(nil), c.Render(s, Painting{}, 1).BGRA...)
	c.LightEnabled = false
	n := c.Render(s, Painting{}, 1).BGRA
	if !bytes.Equal(m, n) {
		t.Fatal("white material incorrectly admitted to emissive selection")
	}
}
func TestBloomSpreadsBeyondTheSharpParticle(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(3), 0, 400, 0)
	s := a.Frame(2)
	s.WaterCount = 0
	p := Painting{Sprites: []Sprite{{X: 190, Y: 75, Radius: 3, Aspect: 1, Opacity: 1, Color: 0xfffffff8, Kind: 4, Emission: 7}}}
	c := NewCompositor()
	c.LightEnabled = false
	no := append([]byte(nil), c.Render(s, p, 1).BGRA...)
	c.LightEnabled = true
	yes := c.Render(s, p, 1)
	changed := 0
	outside := 0
	for y := 0; y < yes.Height; y++ {
		for x := 0; x < yes.Width; x++ {
			i := (y*yes.Width + x) * 4
			if int(yes.BGRA[i+2])-int(no[i+2]) > 2 {
				changed++
				if math.Hypot(float64(x)-196, float64(y)-81) > 4 {
					outside++
				}
			}
		}
	}
	if changed < 30 || outside < 12 {
		t.Fatal("bloom exists only in metadata or the sharp core", changed, outside)
	}
	if c.LightStats().Peak <= 1 {
		t.Fatal("radiance was clamped before the blur")
	}
}
func TestRibbonsRingsAndFragmentsEachEmitActualLight(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(3), 0, 400, 0)
	s := a.Frame(2)
	s.WaterCount = 0
	all := NewPainter().Paint(s)
	sets := []Painting{{Bands: all.Bands}, {Rings: all.Rings}, {Sprites: all.Sprites}}
	for i, p := range sets {
		c := NewCompositor()
		m := append([]byte(nil), c.Render(s, p, 1).BGRA...)
		ls := c.LightStats()
		c.LightEnabled = false
		n := c.Render(s, p, 1).BGRA
		if ls.ActiveCells < 8 || bytes.Equal(m, n) {
			t.Fatal("non-emitting component", i, ls)
		}
	}
}
func TestRadianceBuffersAreClearedAfterExit(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(3), 0, 400, 0)
	p := NewPainter()
	c := NewCompositor()
	s := a.Frame(2)
	c.Render(s, p.Paint(s), 1)
	a.BeginExit(2)
	s = a.Frame(4)
	m := c.Render(s, p.Paint(s), 1)
	for _, v := range m.BGRA {
		if v != 0 {
			t.Fatal("stale light leaked after exit")
		}
	}
	if c.LightStats().ActiveCells != 0 {
		t.Fatal("stale emissive cache retained after exit")
	}
	a.BeginReveal(4)
	s = a.Frame(4)
	m = c.Render(s, p.Paint(s), 1)
	for _, v := range m.BGRA {
		if v != 0 {
			t.Fatal("reveal starts with old luminous pixels")
		}
	}
}
func TestPackedBilinearInterpolationPreservesEveryChannel(t *testing.T) {
	for aa := 0; aa <= 255; aa += 17 {
		for bb := 0; bb <= 255; bb += 15 {
			a := argb(247, 184, 37, float64(aa)/255)
			b := argb(17, 241, 129, float64(bb)/255)
			for w := uint32(0); w <= 256; w += 7 {
				got := lerpPacked(a, b, w)
				for sh := uint(0); sh < 32; sh += 8 {
					want := (uint32(byte(a>>sh))*(256-w) + uint32(byte(b>>sh))*w) >> 8
					if uint32(byte(got>>sh)) != want {
						t.Fatal("packed lane bleed", a, b, w, sh)
					}
				}
				if byte(got) > byte(got>>24) || byte(got>>8) > byte(got>>24) || byte(got>>16) > byte(got>>24) {
					t.Fatal("nonpremultiplied interpolation")
				}
			}
		}
	}
}
func TestRibbonAtlasHasNoBlueChannelWrap(t *testing.T) {
	warmOptics()
	for l, row := range ribbonAtlas {
		for i, v := range row {
			r, g, b := byte(v>>16), byte(v>>8), byte(v)
			if b > g || g > r || int(r)-int(g) > 3 || int(r)-int(b) > 12 {
				t.Fatal("unnormalized optical weights caused chromatic overflow", l, i, v)
			}
		}
	}
}
func TestLightMemoryIsBoundedAcrossDPIAndQuality(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(16), 0, 500, 0)
	p := NewPainter()
	c := NewCompositor()
	for _, scale := range []float64{.75, 1, 1.5, 2, 4, 1, 2} {
		for _, q := range []Quality{Full, Balanced, Economy, Still} {
			s := a.Frame(2)
			s.Quality = q
			out := p.Paint(s)
			m := c.Render(s, out, scale)
			ls := c.LightStats()
			if len(out.Rings) > 2 || len(out.Sprites) > q.ParticleLimit() {
				t.Fatal("object budget exceeded")
			}
			if ls.Bytes > 3*1024*1024 || ls.Width > 176 || ls.Height > 221 {
				t.Fatal("unbounded light buffers", ls)
			}
			if ls.ActiveCells < 5 {
				t.Fatal("fallback quietly removed every light source", q)
			}
			for i := 0; i < len(m.BGRA); i += 4 {
				if m.BGRA[i] > m.BGRA[i+3] || m.BGRA[i+1] > m.BGRA[i+3] || m.BGRA[i+2] > m.BGRA[i+3] {
					t.Fatal("quality/DPI broke alpha", q, scale)
				}
			}
		}
	}
}
func TestParticleHasRigidSplitAndLightSeedPhases(t *testing.T) {
	p := NewPainter()
	p.limit = MaxParticles
	p.crystal(150, 80, .20, 1, 0, 1, 1)
	if len(p.result.Sprites) != 1 || p.result.Sprites[0].Kind != 2 {
		t.Fatal("missing initial hard facet")
	}
	p.result.Sprites = p.result.Sprites[:0]
	p.crystal(150, 80, .53, 1, 0, 1, 1)
	if len(p.result.Sprites) < 4 {
		t.Fatal("facet does not split")
	}
	flash := false
	for _, s := range p.result.Sprites {
		if s.Kind == 4 && s.Emission > 3 {
			flash = true
		}
	}
	if !flash {
		t.Fatal("no luminous fragmentation peak")
	}
	p.result.Sprites = p.result.Sprites[:0]
	p.crystal(150, 80, .88, 1, 0, 1, 1)
	if len(p.result.Sprites) != 3 {
		t.Fatal("unbounded/missing finite child set")
	}
	for _, s := range p.result.Sprites {
		if s.Kind != 4 || s.Radius >= 1.4 {
			t.Fatal("fragment did not become a small light seed")
		}
	}
	p.result.Sprites = p.result.Sprites[:0]
	p.crystal(150, 80, 1, 1, 0, 1, 1)
	if len(p.result.Sprites) != 0 {
		t.Fatal("expired group leaves lingering particles")
	}
}
func TestGlyphAuraExistsOutsideInkBoxAndRemainsReadable(t *testing.T) {
	w, h := 100, 64
	alpha := make([]byte, w*h)
	for y := 12; y < 45; y++ {
		for x := 12; x < 82; x++ {
			if x%15 < 9 {
				alpha[y*w+x] = 255
			}
		}
	}
	g := MakeGlyphMask(w, h, 76, alpha)
	if g.Bytes() != w*h*3 {
		t.Fatal("cache accounting omitted the aura")
	}
	a := NewAnimator()
	a.Set(sample(3), 0, 400, 0)
	s := a.Frame(2)
	s.WaterCount = 0
	c := NewCompositor()
	l := PaintLabel{Text: "synthetic", X: 120, Y: 55, W: 78, H: 45, Size: 32, Color: RouteInk(Green), Light: 0xff64c7a4, Glow: 1, Opacity: 1, Sheen: -1}
	c.LightEnabled = false
	c.Render(s, Painting{}, 1)
	c.DrawGlyph(l, g)
	no := append([]byte(nil), c.surface.BGRA...)
	c.LightEnabled = true
	c.Render(s, Painting{}, 1)
	c.DrawGlyph(l, g)
	changed := 0
	for y := 65; y < 95; y++ {
		for x := 119; x < 126; x++ {
			i := (y*c.surface.Width + x) * 4
			if !bytes.Equal(no[i:i+4], c.surface.BGRA[i:i+4]) {
				changed++
			}
		}
	}
	if changed == 0 {
		t.Fatal("outer glow is still clipped to text rectangle")
	}
	// A fully covered ink centre keeps its original color even above the aura.
	x, y := float64(127+6), float64(70+6)
	i := (int(y)*c.surface.Width + int(x)) * 4
	if !bytes.Equal(no[i:i+4], c.surface.BGRA[i:i+4]) || c.surface.BGRA[i+1] != byte(RouteInk(Green)>>8) {
		t.Fatal("glow bleached the dark letter core")
	}
}
func TestSteadyLightHasNoHeapChurn(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(3), 0, 400, 0)
	p := NewPainter()
	c := NewCompositor()
	s := a.Frame(2)
	c.Render(s, p.Paint(s), 1)
	allocs := testing.AllocsPerRun(12, func() { s = a.Frame(2.1); c.Render(s, p.Paint(s), 1) })
	if allocs != 0 {
		t.Fatal("steady compositor allocates", allocs)
	}
}

// Includes animator, painter and optical compositor. Glyph rendering, OS font
// rasterization and desktop presentation are excluded.
func BenchmarkOpticalQuality(b *testing.B) {
	for _, n := range []int{3, 16} {
		for _, scale := range []float64{1, 2} {
			for _, q := range []Quality{Full, Economy} {
				b.Run(fmt.Sprintf("rows%d_dpi%.1f_q%d", n, scale, q), func(b *testing.B) {
					a := NewAnimator()
					a.Set(sample(n), 0, 600, 0)
					p, c := NewPainter(), NewCompositor()
					s := a.Frame(2)
					s.Quality = q
					out := p.Paint(s)
					c.Render(s, out, scale)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						s = a.Frame(3 + float64(i)/60)
						s.Quality = q
						renderSink = c.Render(s, p.Paint(s), scale)
					}
					b.StopTimer()
					b.ReportMetric(float64(c.LightStats().Bytes), "light-B")
				})
			}
		}
	}
}
