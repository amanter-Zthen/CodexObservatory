package floatui

// Shared SYNTHETIC fixture. It is never account information or route evidence.
import (
	"math"
	"time"
)

func PreviewSnapshot(t float64) Snapshot {
	cycle := math.Mod(t, 40)
	snapshot := Snapshot{Rows: []Row{}, Revision: uint64(t*5) + 1, Mode: "synthetic", At: time.Now(), Quota: QuotaView{Known: true, Fresh: true, Remaining: 89}}
	if cycle < 2 || cycle >= 31 {
		return snapshot
	}
	snapshot.Rows = append(snapshot.Rows, Row{Thread: "synthetic-one", Model: "gpt-6.1-sol", Color: Green, Activity: Running, Outputting: cycle < 24, EstimatedTokenRate: 18 + 3*math.Sin(t*.6)})
	if cycle >= 9 {
		color := Yellow
		if cycle >= 12 {
			color = Green
		}
		snapshot.Rows = append(snapshot.Rows, Row{Thread: "synthetic-two", Model: "gpt-6.1-sol", Color: color, Activity: Running, Outputting: cycle >= 12 && cycle < 26, EstimatedTokenRate: 24 + 4*math.Sin(t*.4)})
	}
	if cycle >= 17 && cycle < 28 {
		snapshot.Rows = append(snapshot.Rows, Row{Thread: "synthetic-three", Model: "gpt-6-sol", Color: Red, Activity: Running, Outputting: cycle < 23, EstimatedTokenRate: 11 + 2*math.Sin(t*.8)})
	}
	return snapshot
}
