package floatui

import (
	"bytes"
	"math"
	"testing"
)

func edgeScene(q Quality) Scene {
	a := NewAnimator()
	a.Set(sample(0), 0, 400, 0)
	s := a.Frame(2)
	s.Quality = q
	s.WaterCount = 0
	return s
}
func pixelDelta(a, b []byte) int {
	n := 0
	for i := range a {
		n += int(math.Abs(float64(a[i]) - float64(b[i])))
	}
	return n
}

func TestEmitterOutsideWindowKeepsItsInsideHaloUntilWholeEffectLeaves(t *testing.T) {
	for _, scale := range []float64{.75, 1, 1.5, 2, 4} {
		for _, q := range []Quality{Full, Balanced, Economy, Still} {
			s := edgeScene(q)
			c := NewCompositor()
			baseline := append([]byte{}, c.Render(s, Painting{}, scale).BGRA...)
			sp := Sprite{X: s.Width + 1, Y: s.Height * .5, Radius: 3, Aspect: 1, Opacity: 1, Color: 0xfffff7df, Kind: 4, Emission: 7}
			close := pixelDelta(baseline, c.Render(s, Painting{Sprites: []Sprite{sp}}, scale).BGRA)
			if close == 0 {
				t.Fatal("center crossing removed partial light", scale, q)
			}
			if q <= Balanced {
				sp.X = s.Width + 8
				if delta := pixelDelta(baseline, c.Render(s, Painting{Sprites: []Sprite{sp}}, scale).BGRA); delta == 0 {
					t.Fatal("outside emitter lost its interior bloom", scale, q)
				}
			}
			sp.X = s.Width + 60
			if !bytes.Equal(baseline, c.Render(s, Painting{Sprites: []Sprite{sp}}, scale).BGRA) {
				t.Fatal("fully outside source kept lighting", scale, q)
			}
		}
	}
}

func TestParticleBodyRotationHaloAndWrapUseWholeBounds(t *testing.T) {
	s := Sprite{X: PanelWidth + 2, Y: 40, Radius: 9.5, Aspect: 1.65, Opacity: 1, Emission: 1, Angle: .8}
	if !spriteIntersects(s, PanelWidth, 76, 24) {
		t.Fatal("center used as visibility bounds")
	}
	s.X = PanelWidth + 70
	if spriteIntersects(s, PanelWidth, 76, 24) {
		t.Fatal("fully outside element not culled")
	}
	p := NewPainter()
	p.limit = MaxParticles
	p.viewportWidth, p.viewportHeight = PanelWidth, 76
	p.sprite(s)
	if len(p.result.Sprites) != 0 {
		t.Fatal("outside element took particle budget")
	}
	for _, k := range []int{0, 1, 2} {
		period := 1 + particleTravelMargin + .52
		phase := (period - .31*float64(k)) / .177
		before, after := heroHead(phase-1e-7, k), heroHead(phase+1e-7, k)
		for _, u := range []float64{before, after} {
			if u >= 0 && u <= 1 {
				t.Fatal("source wrapped with center inside", k, u)
			}
		}
		for j := 0; j <= 32; j++ {
			u := float64(j) / 32
			if heroPulse(u, (phase-1e-7), k) != 0 || heroPulse(u, (phase+1e-7), k) != 0 {
				t.Fatal("luminous wake still inside during wrap", k, u)
			}
		}
	}
}

func TestBoundaryHaloHasNoCliffWhenItsCenterCrosses(t *testing.T) {
	s := edgeScene(Full)
	c := NewCompositor()
	base := append([]byte{}, c.Render(s, Painting{}, 1.5).BGRA...)
	energy := []int{}
	for _, d := range []float64{-1, -.25, 0, .25, 1, 3, 8, 20, 40} {
		sp := Sprite{X: s.Width + d, Y: s.Height * .5, Radius: 9.5, Aspect: 1.65, Opacity: .62, Kind: 0, Color: 0xffffebc6, Emission: 1}
		energy = append(energy, pixelDelta(base, c.Render(s, Painting{Sprites: []Sprite{sp}}, 1.5).BGRA))
	}
	if energy[3] < energy[2]/2 || energy[4] == 0 || energy[6] == 0 || energy[len(energy)-1] != 0 {
		t.Fatal("cliff or infinite halo", energy)
	}
	t.Logf("edge_halo_pixel_delta=%v distances=-1,-.25,0,.25,1,3,8,20,40_DIP", energy)
}

func TestExtendedParticlesStayCleanAcrossAppearanceSizeQualityAndReducedMotion(t *testing.T) {
	a := NewAnimator()
	p := NewPainter()
	c := NewCompositor()
	a.Set(sample(3), 0, 500, 0)
	for _, q := range []Quality{Full, Balanced, Economy, Still} {
		a.SetReducedMotion(q == Still)
		for cycle := 0; cycle < 3; cycle++ {
			at := float64(cycle)*5 + float64(q)*20
			a.BeginReveal(at)
			first := a.Frame(at)
			first.Quality = q
			for _, v := range c.Render(first, p.Paint(first), 1.5).BGRA {
				if v != 0 {
					t.Fatal("old halo leaked into new appearance", q, cycle)
				}
			}
			for j := 1; j < 35; j++ {
				now := at + float64(j)*.04
				if j == 10 {
					a.Set(sample(8), 0, 240, now)
				}
				s := a.Frame(now)
				s.Quality = q
				scale := []float64{1, 1.5, 2}[j%3]
				out := p.Paint(s)
				m := c.Render(s, out, scale)
				if len(out.Sprites) > q.ParticleLimit() || c.LightStats().Bytes > 3*1024*1024 {
					t.Fatal("budget grew during resize/quality", q)
				}
				for i := 0; i < len(m.BGRA); i += 4 {
					if m.BGRA[i] > m.BGRA[i+3] || m.BGRA[i+1] > m.BGRA[i+3] || m.BGRA[i+2] > m.BGRA[i+3] {
						t.Fatal("invalid alpha during transition")
					}
				}
			}
			a.BeginExit(at + 1.5)
			hidden := a.Frame(at + 3.2)
			hidden.Quality = q
			for _, v := range c.Render(hidden, p.Paint(hidden), 1.5).BGRA {
				if v != 0 {
					t.Fatal("exit retained exterior light", q, cycle)
				}
			}
			if c.LightStats().ActiveCells != 0 {
				t.Fatal("hidden stage kept computing emitters")
			}
		}
	}
}
