//go:build windows

package main

import (
	"math"
)

func nextViewZoom(current float64, direction int, dpi float64) float64 {
	upper := math.Min(2, 4/math.Max(1, dpi))
	return math.Max(.75, math.Min(upper, math.Round(current*4+float64(direction))/4))
}
func copyGlyphCache(to, from *floatRenderer) {
	for k, v := range from.glyphs {
		to.glyphs[k] = v
	}
	to.glyphBytes, to.glyphClock, to.GlyphBuilds = from.glyphBytes, from.glyphClock, from.GlyphBuilds
}
