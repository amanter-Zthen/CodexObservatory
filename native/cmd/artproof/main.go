// artproof generates honest synthetic evidence using the production scene and
// paint-list. SVG font rasterisation is not Windows GDI+ validation.
package main

import (
	"codex-observatory/internal/floatui"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	dir := flag.String("out", "artproof", "Output directory")
	flag.Parse()
	must(os.MkdirAll(*dir, 0755))
	cases := []struct {
		name         string
		n            int
		q            float64
		known, fresh bool
		speed        bool
		offset       int
	}{
		{"01_single", 1, 87, true, true, true, 0}, {"02_concurrent", 3, 87, true, true, true, 0},
		{"03_idle", 0, 87, true, true, false, 0}, {"04_unknown", 0, 0, false, false, false, 0},
		{"05_stale", 2, 32, true, false, false, 0}, {"06_many", 16, 13, true, true, true, 5},
		{"07_zero_quota", 1, 0, true, true, false, 0}, {"08_full_quota", 1, 100, true, true, true, 0},
	}
	for _, c := range cases {
		s := sample(c.n, c.q, c.known, c.fresh, c.speed)
		a := floatui.NewAnimator()
		a.Set(s, c.offset, 340, 0)
		for i := 0; i < 180; i++ {
			a.Frame(float64(i) / 60)
		}
		scene := a.Frame(3)
		f, err := os.Create(filepath.Join(*dir, c.name+".svg"))
		must(err)
		must(floatui.WriteSVG(f, scene, 2))
		must(f.Close())
		data, err := json.MarshalIndent(s, "", "  ")
		must(err)
		must(os.WriteFile(filepath.Join(*dir, c.name+".snapshot.json"), data, 0644))
	}
	// This tool exports still inspection cases only. The obsolete 0.3 frame-list
	// recorder did not encode optical atlases and is intentionally removed.
	fmt.Println("Synthetic SVG diagnostics written; fonts are not Windows-native. For live motion use cmd/liveproof or --ui-preview.")
}

func sample(n int, q float64, known, fresh, speed bool) floatui.Snapshot {
	s := floatui.Snapshot{Mode: "synthetic", At: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC), Quota: floatui.QuotaView{Known: known, Fresh: fresh, Remaining: q}}
	models := []string{"gpt-6.1-sol", "gpt-6-sol", "gpt-6.1-sol"}
	colors := []floatui.Color{floatui.Green, floatui.Red, floatui.Yellow}
	for i := 0; i < n; i++ {
		s.Rows = append(s.Rows, floatui.Row{Thread: fmt.Sprintf("synthetic-%02d", i), Model: models[i%3], Color: colors[i%3], Activity: floatui.Running, Outputting: speed && i%3 != 2, EstimatedTokenRate: []float64{19.6, 12.4, 0}[i%3]})
	}
	return s
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
