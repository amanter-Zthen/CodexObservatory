package floatui

import "math"

type Quality uint8

const (
	Full Quality = iota
	Balanced
	Economy
	Still
)
const MaxParticles = 64

func (q Quality) ParticleLimit() int {
	switch q {
	case Full:
		return 64
	case Balanced:
		return 40
	case Economy:
		return 20
	default:
		return 0
	}
}
func (q Quality) String() string {
	switch q {
	case Full:
		return "full"
	case Balanced:
		return "balanced"
	case Economy:
		return "economy"
	default:
		return "still"
	}
}

// Governor uses measured draw+present cost, not machine names. Downgrades need
// sustained over-budget samples; recovery is deliberately much slower. Time
// remains monotonic across tier changes, so a lower frame rate never slows down
// or restarts the animation itself.
type Governor struct {
	Quality           Quality
	MeanMS            float64
	overloaded, spare int
	Changes           int
	Manual            int // -1 = adaptive; 0..3 = explicit quality cap, not an FPS claim
}

func NewGovernor() *Governor { return &Governor{Manual: -1} }
func (g *Governor) Observe(ms float64) {
	if math.IsNaN(ms) || math.IsInf(ms, 0) || ms < 0 {
		return
	}
	if g.MeanMS == 0 {
		g.MeanMS = ms
	} else {
		g.MeanMS += .08 * (ms - g.MeanMS)
	}
	if g.Manual >= 0 {
		g.Quality = Quality(g.Manual)
		return
	}
	if g.MeanMS > 7 {
		g.overloaded++
		g.spare = 0
	} else if g.MeanMS < 3 {
		g.spare++
		g.overloaded = 0
	} else {
		g.overloaded = 0
		g.spare = 0
	}
	if g.overloaded >= 18 && g.Quality < Economy {
		g.Quality++
		g.Changes++
		g.overloaded = 0
		g.spare = 0
	}
	if g.spare >= 300 && g.Quality > Full {
		g.Quality--
		g.Changes++
		g.spare = 0
		g.overloaded = 0
	}
}
func (g *Governor) SetManual(q int) {
	g.Manual = max(-1, min(3, q))
	g.overloaded, g.spare = 0, 0
	if g.Manual >= 0 {
		g.Quality = Quality(g.Manual)
	} else {
		g.Quality = Full
	}
}
func (g *Governor) Interval(transition, active, reduced bool) uint32 {
	if reduced || g.Quality == Still {
		return 250
	}
	if g.Quality == Economy {
		return 50
	}
	if transition {
		if g.Quality == Full {
			return 17
		}
		return 33
	}
	if active {
		return 33
	}
	return 50 // idle still breathes; old 8fps idle made curves visibly step
}
