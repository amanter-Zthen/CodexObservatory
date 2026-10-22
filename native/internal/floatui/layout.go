package floatui

import "math"

type Layout struct {
	Width, Height, Font, RowHeight, QuotaY float64
	Visible, Offset                        int
}

const PanelWidth = 388.0
const MaxVisibleRows = 16
const MaxAnimatedRows = 32 // live viewport plus a bounded outgoing page
const MaxPanelHeight = 500.0

func TargetLayout(count, offset int, maxHeight float64) Layout {
	if maxHeight < 76 {
		maxHeight = 76
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= count {
		offset = max(0, count-1)
	}
	font := math.Max(13.2, 17.8-1.1*math.Log1p(float64(max(0, count-1))))
	visibleCount := min(count, MaxVisibleRows)
	height := 76 + 22*float64(visibleCount) + 17*math.Log1p(float64(visibleCount))
	height = math.Min(height, maxHeight)
	rowHeight := math.Max(25, 37-3.5*math.Log1p(float64(max(0, count-1))))
	visible := min(count, MaxVisibleRows, int(math.Max(1, math.Floor((height-36)/rowHeight))))
	if count == 0 {
		visible = 0
	}
	if offset+visible > count {
		offset = max(0, count-visible)
	}
	return Layout{Width: PanelWidth, Height: height, Font: font, RowHeight: rowHeight, QuotaY: height - 66, Visible: visible, Offset: offset}
}

type Point struct{ X, Y float64 }

// Tween is a quintic ease with an incoming tangent. Re-targeting reads the
// displayed value first. A bounded tangent prevents bursty updates overshooting
// a layout or opacity envelope while avoiding a restart from the old origin.
type Tween struct{ From, To, Start, Duration, Tangent float64 }

func smoother(x float64) float64 { x = clamp01(x); return x * x * x * (x*(x*6-15) + 10) }
func (t Tween) Value(now float64) float64 {
	if t.Duration <= 0 {
		return t.To
	}
	p := clamp01((now - t.Start) / t.Duration)
	h := p - 6*p*p*p + 8*p*p*p*p - 3*p*p*p*p*p
	return t.From + (t.To-t.From)*smoother(p) + t.Tangent*t.Duration*h
}
func (t Tween) velocity(now float64) float64 {
	if t.Duration <= 0 || now <= t.Start || now >= t.Start+t.Duration {
		return 0
	}
	p := (now - t.Start) / t.Duration
	return (t.To-t.From)*(30*p*p*(p-1)*(p-1))/t.Duration + t.Tangent*(1-18*p*p+32*p*p*p-15*p*p*p*p)
}
func (t *Tween) Retarget(target, now, duration float64) {
	if target == t.To {
		return
	}
	value, v := t.Value(now), t.velocity(now)
	delta := target - value
	if duration <= 0 || v*delta <= 0 {
		v = 0
	} else {
		v = math.Copysign(math.Min(math.Abs(v), 1.5*math.Abs(delta)/duration), v)
	}
	*t = Tween{From: value, To: target, Start: now, Duration: duration, Tangent: v}
}
func settled(value float64) Tween { return Tween{From: value, To: value} }

// RetargetAfter captures the value NOW, not at a future delayed start. This
// matters when entry interrupts exit: delayed components must not teleport.
func (t *Tween) RetargetAfter(target, now, delay, duration float64) {
	if target == t.To {
		return
	}
	*t = Tween{From: t.Value(now), To: target, Start: now + delay, Duration: duration}
}
