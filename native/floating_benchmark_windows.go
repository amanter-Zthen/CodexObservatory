//go:build windows

package main

import (
	"codex-observatory/internal/floatui"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

// Runs before store, feed, network or Codex initialization. This measures the
// actual Windows renderer, but deliberately DOES NOT claim desktop-present FPS.
func runVisualBenchmark(path string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	type result struct {
		Rows           int     `json:"rows"`
		Scale          float64 `json:"scale"`
		Quality        string  `json:"quality"`
		MeanMS         float64 `json:"mean_ms"`
		P95MS          float64 `json:"p95_ms"`
		MaxMS          float64 `json:"max_ms"`
		GlyphBytes     int     `json:"glyph_bytes"`
		GlyphBuilds    uint64  `json:"glyph_builds"`
		MaterialBuilds uint64  `json:"material_builds"`
		LightBytes     int     `json:"light_bytes"`
		LightCells     int     `json:"light_cells"`
	}
	results := []result{}
	for _, scale := range []float64{1, 1.5, 2} {
		for _, n := range []int{0, 3, 8, 16} {
			for _, quality := range []floatui.Quality{floatui.Full, floatui.Economy} {
				r, err := newFloatRenderer(scale, 620)
				if err != nil {
					return err
				}
				a := floatui.NewAnimator()
				s := floatui.PreviewSnapshot(19)
				s.Rows = nil
				for i := 0; i < n; i++ {
					s.Rows = append(s.Rows, floatui.Row{Thread: fmt.Sprint(i), Model: "synthetic-model", Color: []floatui.Color{floatui.Green, floatui.Yellow, floatui.Red}[i%3], Activity: floatui.Running, Outputting: true, EstimatedTokenRate: 19.6})
				}
				a.Set(s, 0, 600, 0)
				for i := 0; i < 12; i++ {
					scene := a.Frame(2 + float64(i)/60)
					scene.Quality = quality
					r.draw(scene)
				}
				samples := make([]float64, 90)
				for i := range samples {
					scene := a.Frame(3 + float64(i)/60)
					scene.Quality = quality
					start := time.Now()
					r.draw(scene)
					samples[i] = float64(time.Since(start).Nanoseconds()) / 1e6
				}
				sort.Float64s(samples)
				total := 0.0
				for _, v := range samples {
					total += v
				}
				name := "full"
				if quality == floatui.Economy {
					name = "economy"
				}
				light := r.compositor.LightStats()
				results = append(results, result{n, scale, name, total / 90, samples[85], samples[89], r.glyphBytes, r.GlyphBuilds, r.compositor.MaterialBuilds, light.Bytes, light.ActiveCells})
				r.close()
			}
		}
	}
	report := map[string]any{"version": Version, "go_version": runtime.Version(), "platform": runtime.GOOS + "/" + runtime.GOARCH, "at": time.Now().Format(time.RFC3339), "synthetic": true, "model_requests": 0, "scope": "native CPU compositor + real GDI glyph cache + copy to DIB; excludes UpdateLayeredWindow, desktop present, acquisition, and long-run leaks", "atlas_bytes": floatui.AtlasBytes, "frames_per_case": 90, "warmup_frames": 12, "results": results}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
