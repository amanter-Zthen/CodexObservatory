package floatui

import (
	"bytes"
	"fmt"
	"math"
	"testing"
)

func TestEntranceFromEmptyPixelsAndFiniteTwoSidedPaths(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(3), 0, 500, 0)
	a.BeginReveal(0)
	p := NewPainter()
	c := NewCompositor()
	scene := a.Frame(0)
	m := c.Render(scene, p.Paint(scene), 1)
	for _, v := range m.BGRA {
		if v != 0 {
			t.Fatal("startup flashed a visible background")
		}
	}
	scene = a.Frame(.55)
	if scene.Stage.mask(.02, 0) <= scene.Stage.mask(.98, 0) {
		t.Fatal("left stream did not enter along its path")
	}
	if scene.Stage.mask(.98, 1) <= scene.Stage.mask(.02, 1) {
		t.Fatal("right stream did not enter along its path")
	}
	if !(scene.Stage.Shell > scene.Stage.Details && scene.Stage.Details < scene.Stage.Text) {
		t.Fatal("appearance channels are synchronized", scene.Stage)
	}
	scene = a.Frame(2)
	if scene.Stage.Transitioning || scene.Stage.Shell != 1 || scene.Stage.Text != 1 {
		t.Fatal("entry did not settle")
	}
}
func TestExitKeepsPhaseThenEndsFullyTransparent(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(3), 0, 500, 0)
	s := a.Frame(2)
	phase := s.FlowTime
	a.BeginExit(2)
	s = a.Frame(2.6)
	if s.FlowTime <= phase {
		t.Fatal("exit froze the curves")
	}
	if s.Stage.mask(.02, 0) >= s.Stage.mask(.98, 0) {
		t.Fatal("tail did not travel forward along curve")
	}
	s = a.Frame(3.6)
	m := NewCompositor().Render(s, NewPainter().Paint(s), 1.5)
	if !s.Stage.Hidden {
		t.Fatal("exit did not complete")
	}
	for _, v := range m.BGRA {
		if v != 0 {
			t.Fatal("exit left particles or text behind")
		}
	}
}
func TestAppearanceInterruptionDoesNotTeleportDelayedComponents(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(1), 0, 500, 0)
	a.Frame(2)
	a.BeginExit(2)
	before := a.Frame(2.65).Stage
	a.BeginReveal(2.65)
	after := a.Frame(2.65).Stage
	for _, pair := range [][2]float64{{before.Shell, after.Shell}, {before.Text, after.Text}, {before.Details, after.Details}, {before.Head[1], after.Head[1]}, {before.Tail[1], after.Tail[1]}} {
		if math.Abs(pair[0]-pair[1]) > 1e-9 {
			t.Fatal("interruption jump", pair)
		}
	}
	if a.Frame(5).Stage.Hidden {
		t.Fatal("interruption failed to reveal")
	}
}
func TestCurveDeformsInsteadOfOnlyScalingOrVerticalTranslation(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(3), 0, 500, 0)
	for i := 0; i <= 120; i++ {
		a.Frame(float64(i) / 60)
	}
	before := a.Frame(2).Bands[0]
	for i := 121; i <= 300; i++ {
		a.Frame(float64(i) / 60)
	}
	after := a.Frame(5).Bands[0]
	var sumX, sumY, sumXX, sumXY float64
	for i := 0; i < 33; i++ {
		x := (before.Points[i].Y + before.Points[65-i].Y) / 2
		y := (after.Points[i].Y + after.Points[65-i].Y) / 2
		sumX += x
		sumY += y
		sumXX += x * x
		sumXY += x * y
	}
	scale := (33*sumXY - sumX*sumY) / (33*sumXX - sumX*sumX)
	offset := (sumY - scale*sumX) / 33
	residual := 0.0
	for i := 0; i < 33; i++ {
		x := (before.Points[i].Y + before.Points[65-i].Y) / 2
		y := (after.Points[i].Y + after.Points[65-i].Y) / 2
		residual += math.Pow(y-scale*x-offset, 2)
	}
	if math.Sqrt(residual/33) < .8 {
		t.Fatal("curve shape was only transformed")
	}
}
func TestOpticsAndCompositedAlphaRemainPremultiplied(t *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		a := NewAnimator()
		a.Set(sample(3), 0, 500, 0)
		a.BeginReveal(0)
		c := NewCompositor()
		p := NewPainter()
		for _, t0 := range []float64{.0, .25, .55, 1, 2} {
			s := a.Frame(t0)
			m := c.Render(s, p.Paint(s), scale)
			for i := 0; i < len(m.BGRA); i += 4 {
				if m.BGRA[i] > m.BGRA[i+3] || m.BGRA[i+1] > m.BGRA[i+3] || m.BGRA[i+2] > m.BGRA[i+3] {
					t.Fatalf("alpha violation scale=%v time=%v index=%d", scale, t0, i)
				}
			}
			if m.BGRA[3] != 0 {
				t.Fatal("transparent corner changed")
			}
		}
	}
	for _, row := range ribbonAtlas {
		for _, v := range row {
			a := byte(v >> 24)
			if byte(v) > a || byte(v>>8) > a || byte(v>>16) > a {
				t.Fatal("atlas is not premultiplied")
			}
		}
	}
}
func TestSceneBudgetsAndMaterialReuse(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(16), 0, 340, 0)
	c := NewCompositor()
	p := NewPainter()
	for i := 0; i < 90; i++ {
		s := a.Frame(2 + float64(i)/60)
		out := p.Paint(s)
		c.Render(s, out, 1)
		if len(out.Sprites) > MaxParticles || len(out.Labels) > 128 || len(out.Shapes) > 96 {
			t.Fatal("budget overrun")
		}
	}
	if c.MaterialBuilds != 1 {
		t.Fatal("steady material rebuilt", c.MaterialBuilds)
	}
	for _, q := range []Quality{Full, Balanced, Economy, Still} {
		s := a.Frame(4)
		s.Quality = q
		out := p.Paint(s)
		if len(out.Sprites) > q.ParticleLimit() {
			t.Fatal("tier exceeds particle cap", q)
		}
	}
	if AtlasBytes > 100*1024 {
		t.Fatal("atlas unexpectedly expanded")
	}
}
func TestGovernorHysteresisAndManualControls(t *testing.T) {
	g := NewGovernor()
	for i := 0; i < 40; i++ {
		g.Observe(16)
	}
	if g.Quality != Economy || g.Changes != 2 {
		t.Fatal("sustained slow draw was not reduced", g)
	}
	for i := 0; i < 80; i++ {
		g.Observe(1)
	}
	if g.Quality != Economy {
		t.Fatal("quality bounced back too quickly")
	}
	for i := 0; i < 700; i++ {
		g.Observe(1)
	}
	if g.Quality != Full {
		t.Fatal("quality never recovered")
	}
	if g.Interval(true, true, false) != 17 || g.Interval(false, true, false) != 33 || g.Interval(false, false, false) != 50 {
		t.Fatal("wrong cadence")
	}
	g.SetManual(3)
	g.Observe(1)
	if g.Quality != Still || g.Interval(false, true, true) != 250 {
		t.Fatal("still setting ignored")
	}
	g.SetManual(-1)
	if g.Quality != Full {
		t.Fatal("auto did not reset")
	}
}
func testMask() GlyphMask {
	w, h := 200, 44
	alpha := make([]byte, w*h)
	for y := 8; y < 36; y++ {
		for x := 6; x < 188; x++ {
			if x%12 < 6 {
				alpha[y*w+x] = 255
			}
		}
	}
	return MakeGlyphMask(w, h, 190, alpha)
}
func TestGlyphMotionIsSubpixelAndNoNewMaskIsNeeded(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(1), 0, 300, 0)
	s := a.Frame(2)
	p := NewPainter().Paint(s)
	c := NewCompositor()
	g := testMask()
	label := PaintLabel{Text: "test", X: 110, Y: 25, W: 176, H: 30, Size: 17.37, Opacity: 1, Color: 0xff306858, Sheen: .4, Light: 0xffc0e0d4, Glow: .2}
	m := c.Render(s, p, 1.5)
	c.DrawGlyph(label, g)
	before := append([]byte(nil), m.BGRA...)
	c.Render(s, p, 1.5)
	label.X += .07
	c.DrawGlyph(label, g)
	if bytes.Equal(before, m.BGRA) {
		t.Fatal("fractional motion was snapped to a whole pixel")
	}
	for i := 0; i < len(m.BGRA); i += 4 {
		if m.BGRA[i] > m.BGRA[i+3] || m.BGRA[i+1] > m.BGRA[i+3] || m.BGRA[i+2] > m.BGRA[i+3] {
			t.Fatal("glyph alpha violation")
		}
	}
}
func TestReducedMotionFreezesPhaseNotData(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(1), 0, 340, 0)
	a.Frame(2)
	a.SetReducedMotion(true)
	s := a.Frame(3)
	phase := s.FlowTime
	a.Set(sample(3), 0, 340, 3)
	s = a.Frame(4)
	if s.FlowTime != phase || s.TotalRows != 3 {
		t.Fatal("reduced motion lost live data or kept phase running")
	}
}

var renderSink Material

func BenchmarkCompositorSteady(b *testing.B) {
	for _, n := range []int{0, 1, 3, 8, 16} {
		for _, scale := range []float64{1, 1.5, 2} {
			b.Run(fmt.Sprintf("rows%d_dpi%.1f", n, scale), func(b *testing.B) {
				a := NewAnimator()
				a.Set(sample(n), 0, 600, 0)
				p := NewPainter()
				c := NewCompositor()
				s := a.Frame(2)
				c.Render(s, p.Paint(s), scale)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					s = a.Frame(3 + float64(i)/60)
					renderSink = c.Render(s, p.Paint(s), scale)
				}
			})
		}
	}
}
func BenchmarkCompositorIncludingCachedGlyphBlends(b *testing.B) {
	a := NewAnimator()
	a.Set(sample(3), 0, 600, 0)
	p := NewPainter()
	c := NewCompositor()
	g := testMask()
	s := a.Frame(2)
	c.Render(s, p.Paint(s), 1.5)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s = a.Frame(3 + float64(i)/60)
		out := p.Paint(s)
		renderSink = c.Render(s, out, 1.5)
		for _, l := range out.Labels {
			c.DrawGlyph(l, g)
		}
	}
}

func TestFastScrollingHasFixedPoseAndPaintBudgets(t *testing.T) {
	a := NewAnimator()
	p := NewPainter()
	s := sample(256)
	for i := 0; i < 100; i++ {
		now := float64(i) * .016
		a.Set(s, i*13%240, 2000, now)
		scene := a.Frame(now)
		paint := p.Paint(scene)
		if len(a.poses) > MaxAnimatedRows || scene.VisibleRows > MaxVisibleRows || len(scene.Bands) > 3+MaxAnimatedRows || len(paint.Labels) > 128 || scene.Height > MaxPanelHeight {
			t.Fatal("unbounded scrolling state")
		}
	}
}
func TestStillReopeningDoesNotLeaveHiddenStage(t *testing.T) {
	a := NewAnimator()
	a.HideImmediate()
	if !a.Frame(1).Stage.Hidden {
		t.Fatal("did not hide")
	}
	a.ShowImmediate()
	s := a.Frame(2)
	if s.Stage.Hidden || s.Stage.Shell != 1 {
		t.Fatal("still reopen stayed invisible")
	}
}
func TestZeroQuotaHasNoOpticalWaterRidge(t *testing.T) {
	a := NewAnimator()
	s := sample(0)
	s.Quota.Remaining = 0
	a.Set(s, 0, 340, 0)
	scene := a.Frame(2)
	// Turn off decorative bands so only the zero quota surface could differ.
	p := NewPainter().Paint(scene)
	p.Bands = nil
	p.Sprites = nil
	p.Shapes = nil
	c := NewCompositor()
	water := append([]byte(nil), c.Render(scene, p, 1).BGRA...)
	scene.WaterCount = 0
	dry := c.Render(scene, p, 1).BGRA
	if !bytes.Equal(water, dry) {
		t.Fatal("zero quota invented a residual ridge")
	}
}

// Includes cold-size material/clip rebuilds during row growth and shrinkage.
// This is intentionally NOT the zero-allocation steady-state microbenchmark.
func BenchmarkAppearanceAndResize(b *testing.B) {
	for _, scale := range []float64{1, 2} {
		b.Run(fmt.Sprintf("dpi%.1f", scale), func(b *testing.B) {
			a := NewAnimator()
			p := NewPainter()
			c := NewCompositor()
			a.BeginReveal(0)
			snapshots := []Snapshot{sample(0), sample(1), sample(3), sample(8), sample(16), sample(0)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				now := float64(i) / 60
				if i%60 == 0 {
					a.Set(snapshots[(i/60)%len(snapshots)], 0, 500, now)
				}
				s := a.Frame(now)
				renderSink = c.Render(s, p.Paint(s), scale)
			}
		})
	}
}
