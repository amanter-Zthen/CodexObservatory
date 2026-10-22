//go:build windows

package main

import "testing"

func TestMenuZoomUsesQuarterStepsAndLimits(t *testing.T) {
	z := 1.0
	for _, want := range []float64{1.25, 1.5, 1.75, 2, 2} {
		z = nextViewZoom(z, 1, 1.5)
		if z != want {
			t.Fatal(z, want)
		}
	}
	for n := 0; n < 10; n++ {
		z = nextViewZoom(z, -1, 1.5)
	}
	if z != .75 {
		t.Fatal(z)
	}
	if nextViewZoom(1, 1, 4) != 1 {
		t.Fatal("renderer DPI cap exceeded")
	}
}
