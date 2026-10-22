package floatui

import (
	"testing"
	"time"
)

func TestZoomBurstCoalescesAndDropsStalePreparation(t *testing.T) {
	z := NewViewZoom(1, 1.5)
	now := time.Unix(1000, 0)
	for n := 0; n < 10000; n++ {
		z.Set([]float64{1.25, 1.5, 1.75, 2}[n%4], 1.5, now.Add(time.Duration(n)*time.Microsecond))
	}
	if _, _, ok := z.Start(now.Add(100 * time.Millisecond)); ok {
		t.Fatal("burst ran expensive work")
	}
	v, value, ok := z.Start(now.Add(240 * time.Millisecond))
	if !ok || value != 2 {
		t.Fatal(v, value, ok)
	}
	if _, _, ok := z.Start(now.Add(time.Second)); ok {
		t.Fatal("parallel preparation")
	}
	z.Set(.75, 1.5, now.Add(250*time.Millisecond))
	if z.Accept(v, value) {
		t.Fatal("stale zoom displayed")
	}
	v, value, ok = z.Start(now.Add(500 * time.Millisecond))
	if !ok || value != .75 || !z.Accept(v, value) {
		t.Fatal("latest target lost")
	}
	if _, _, ok := z.Start(now.Add(time.Second)); ok {
		t.Fatal("same target applied twice")
	}
}
func TestZoomMaximumWaitAndBounds(t *testing.T) {
	z := NewViewZoom(1, 1)
	now := time.Unix(1000, 0)
	for n := 0; n < 9; n++ {
		z.Set([]float64{1.25, 1.5}[n%2], 1, now.Add(time.Duration(n)*100*time.Millisecond))
	}
	if _, _, ok := z.Start(now.Add(801 * time.Millisecond)); !ok {
		t.Fatal("continuous input starved update")
	}
	if ClampViewZoom(3, 4) != 1 || ClampViewZoom(.01, 1) != .75 {
		t.Fatal("bounds")
	}
}
