package floatui

import "math"

// Scene coordinates describe the shell; the native window has a small transparent
// margin. Include the full rotated body and the finite bloom kernel in visibility.
func spriteExtent(s Sprite, halo float64) (x, y float64) {
	aspect := s.Aspect
	if aspect <= 0 {
		aspect = 1
	}
	sn, cs := math.Sincos(s.Angle)
	x = s.Radius * (math.Abs(cs)*aspect + math.Abs(sn))
	y = s.Radius * (math.Abs(sn)*aspect + math.Abs(cs))
	if s.Emission > 0 {
		glow := math.Max(2.2, s.Radius*1.05) + halo
		x = math.Max(x, glow)
		y = math.Max(y, glow)
	}
	return
}
func spriteIntersects(s Sprite, width, height, halo float64) bool {
	if !finitePositive(s.Radius) || math.IsNaN(s.X) || math.IsNaN(s.Y) || math.IsInf(s.X, 0) || math.IsInf(s.Y, 0) {
		return false
	}
	x, y := spriteExtent(s, halo)
	return s.X+x >= -FrameMargin && s.X-x <= width+FrameMargin && s.Y+y >= -FrameMargin && s.Y-y <= height+FrameMargin
}

// The largest current cluster, child displacement, wake and glow fit inside
// this travel allowance. Wrap occurs with both ends entirely outside the window.
const particleTravelMargin = 80.0 / PanelWidth

func particleTravel(position float64) float64 {
	period := 1 + 2*particleTravelMargin
	return position - math.Floor(position/period)*period - particleTravelMargin
}
