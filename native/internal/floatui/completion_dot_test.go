package floatui

import "testing"

func TestCompletionDotSurvivesEveryQualityAndUnknownQuota(t *testing.T) {
	for _, q := range []Quality{Full, Balanced, Economy, Still} {
		a := NewAnimator()
		a.Set(Snapshot{PendingCompleted: 1}, 0, 500, 0)
		scene := a.Frame(1)
		scene.Quality = q
		scene.Stage.Text = 1
		paint := NewPainter().Paint(scene)
		found := false
		for _, shape := range paint.Shapes {
			if shape.Color&0xffffff == 0x4284b2 {
				found = true
			}
		}
		if !found {
			t.Fatal("completion dot disappeared", q)
		}
	}
}
