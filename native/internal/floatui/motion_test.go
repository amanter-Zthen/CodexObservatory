package floatui

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func sample(n int) Snapshot {
	s := Snapshot{Quota: QuotaView{Known: true, Fresh: true, Remaining: 87}}
	for i := 0; i < n; i++ {
		s.Rows = append(s.Rows, Row{Thread: fmt.Sprint(i), Model: "wanted", Color: Green, Outputting: true, EstimatedTokenRate: 19.6, Activity: Running})
	}
	return s
}
func TestLayoutFlatAndGrowthDecreasingWithReadableFloor(t *testing.T) {
	previous := TargetLayout(0, 0, 1000)
	lastDelta := math.Inf(1)
	for n := 1; n <= 8; n++ {
		current := TargetLayout(n, 0, 1000)
		delta := current.Height - previous.Height
		if current.Font < 13 || delta > lastDelta+.000001 || current.Height < previous.Height {
			t.Fatal(n, current, delta, lastDelta)
		}
		lastDelta = delta
		previous = current
	}
	if first := TargetLayout(1, 0, 1000); first.Width/first.Height < 2 {
		t.Fatal("first row is not a flat panel", first)
	}
	if l := TargetLayout(32, 100, 300); l.Visible+l.Offset > 32 || l.Height > 300 {
		t.Fatal(l)
	}
}
func TestAnimatedRetargetStartsAtCurrentFrame(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(1), 0, 600, 0)
	before := a.Frame(.2).Height
	a.Set(sample(4), 0, 600, .2)
	after := a.Frame(.2).Height
	if math.Abs(before-after) > .00001 {
		t.Fatal("height jumped", before, after)
	}
	if a.Frame(1).Height <= after {
		t.Fatal("growth did not finish")
	}
}
func TestOutputVisibilityFadesAndNoApproximationGlyph(t *testing.T) {
	a := NewAnimator()
	s := sample(1)
	a.Set(s, 0, 600, 0)
	f := a.Frame(1)
	if len(f.Rows) != 1 || f.Rows[0].RateOpacity < .99 || f.Rows[0].Speed != "19.6/s" || strings.Contains(f.Rows[0].Speed, "≈") {
		t.Fatal(f.Rows)
	}
	s.Rows[0].Outputting = false
	a.Set(s, 0, 600, 1)
	if a.Frame(1.1).Rows[0].RateOpacity >= 1 {
		t.Fatal("speed did not fade")
	}
	if a.Frame(1.5).Rows[0].RateOpacity != 0 {
		t.Fatal("speed still visible")
	}
	a.Set(Snapshot{}, 0, 600, 2)
	f = a.Frame(2.5)
	if len(f.Rows) != 0 || f.Quota.Known || len(f.Bands) != 3 {
		t.Fatal("empty state contains conversation data")
	}
}
func TestFixedMainCurvesThinVariableAndAddedCurvesKeepFrameWidth(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(4), 0, 600, 0)
	f := a.Frame(1)
	if len(f.Bands) != 7 || f.Width != PanelWidth || f.Water[33].X != PanelWidth {
		t.Fatal("geometry did not use panel width")
	}
	for k, band := range f.Bands {
		if band.Count != 66 {
			t.Fatal(k)
		}
		for j := 0; j <= 32; j++ {
			w := band.Points[65-j].Y - band.Points[j].Y
			if math.IsNaN(w) || w < 0 || w > 1.9 {
				t.Fatal("too broad or invalid", k, w)
			}
		}
		if band.Points[32].X != PanelWidth {
			t.Fatal("curve not spanning panel", k)
		}
	}
}
func TestExitReentryAndModelReplacementKeepIdentity(t *testing.T) {
	a := NewAnimator()
	a.Set(sample(1), 0, 600, 0)
	a.Frame(1)
	a.Set(Snapshot{}, 0, 600, 1)
	before := a.Frame(1.1).Rows[0].Opacity
	s := sample(1)
	s.Rows[0].Model = "other"
	a.Set(s, 0, 600, 1.1)
	f := a.Frame(1.1)
	if len(f.Rows) != 1 || math.Abs(f.Rows[0].Opacity-before) > .0001 || f.Rows[0].PreviousModel != "wanted" {
		t.Fatal(f.Rows)
	}
	if a.Frame(2).Rows[0].ModelMix != 1 {
		t.Fatal("replacement never finished")
	}
}

var benchmarkScene Scene

func BenchmarkScene(b *testing.B) {
	for _, n := range []int{0, 1, 2, 4, 8, 16, 32} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			a := NewAnimator()
			a.Set(sample(n), 0, 1000, 0)
			a.Frame(2)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkScene = a.Frame(2 + float64(i)/60)
			}
		})
	}
}
