package floatui

import (
	"math"
	"time"
)

const ZoomQuiet = 220 * time.Millisecond
const ZoomMaxWait = 800 * time.Millisecond

// ViewZoom keeps one target and one in-flight preparation. Input changes
// immediately; expensive work trails the input and stale work is discarded.
// The native host owns this value on its window thread.
type ViewZoom struct {
	Target, Applied float64
	Version         uint64
	changed, first  time.Time
	inFlight        bool
	preparedVersion uint64
	hasPrepared     bool
}

func ClampViewZoom(value, dpi float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		value = 1
	}
	upper := math.Min(2, 4/math.Max(1, dpi))
	return math.Max(.75, math.Min(upper, math.Round(value*4)/4))
}
func NewViewZoom(value, dpi float64) *ViewZoom {
	v := ClampViewZoom(value, dpi)
	return &ViewZoom{Target: v, Applied: v}
}
func (z *ViewZoom) Set(value, dpi float64, now time.Time) bool {
	value = ClampViewZoom(value, dpi)
	if value == z.Target {
		return false
	}
	if z.Target == z.Applied {
		z.first = now
	}
	z.Target, z.changed = value, now
	z.Version++
	return true
}
func (z *ViewZoom) Start(now time.Time) (uint64, float64, bool) {
	if z.inFlight || z.Target == z.Applied || z.hasPrepared && z.preparedVersion == z.Version || now.Sub(z.changed) < ZoomQuiet && now.Sub(z.first) < ZoomMaxWait {
		return 0, 0, false
	}
	z.inFlight = true
	return z.Version, z.Target, true
}
func (z *ViewZoom) Matches(version uint64, value float64) bool {
	return version == z.Version && value == z.Target
}
func (z *ViewZoom) Receive(version uint64, value float64) bool {
	z.inFlight = false
	if !z.Matches(version, value) {
		return false
	}
	z.preparedVersion, z.hasPrepared = version, true
	return true
}
func (z *ViewZoom) Apply(version uint64, value float64) bool {
	if !z.Matches(version, value) {
		return false
	}
	z.Applied = value
	return true
}
func (z *ViewZoom) Accept(version uint64, value float64) bool {
	return z.Receive(version, value) && z.Apply(version, value)
}
func (z *ViewZoom) Retry(now time.Time) { z.inFlight = false; z.first, z.changed = now, now }

// PrepareCompositor builds only CPU-owned buffers, suitable for a bounded
// background worker. The window thread adopts the ready compositor exclusively.
func PrepareCompositor(scene Scene, scale float64) *Compositor {
	c := NewCompositor()
	warmOptics()
	c.prepare(scene, scale)
	return c
}
