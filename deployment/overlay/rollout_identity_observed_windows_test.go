//go:build windows

package main

import (
	"bufio"
	"codex-observatory/internal/routetrace"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Optional bounded local replay: only reads the named rollouts and writes the
// identity/lifecycle result, never message or tool content, to the report.
func TestRolloutIdentityObservedReplay(t *testing.T) {
	listPath := os.Getenv("OBSERVATORY_ROLLOUT_PROBE_LIST")
	if listPath == "" {
		t.Skip("observed replay is opt-in")
	}
	raw, err := os.ReadFile(listPath)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	if err = json.Unmarshal(raw, &paths); err != nil {
		t.Fatal(err)
	}
	reports := []Obj{}
	for _, path := range paths {
		a, _ := testApp(t)
		a.started = time.Now().Add(-24 * time.Hour)
		f := newState("isolated", path, filepath.Base(path))
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReaderSize(file, 128*1024)
		var offset int64
		owner := ""
		var events []Event
		for {
			b, n, complete, oversize, readErr := readRecord(reader)
			if !complete {
				break
			}
			if readErr != nil || oversize {
				file.Close()
				t.Fatalf("unexpected rollout record: %v", readErr)
			}
			if owner == "" {
				o, _ := decodeObj(b)
				if str(o["type"]) == "session_meta" {
					owner = str(obj(o["payload"])["id"])
				}
			}
			events = append(events, parseLine(f, b, offset)...)
			offset += n
		}
		file.Close()
		if owner == "" || f.Summary.Thread != owner {
			t.Fatalf("owner mismatch: %s", filepath.Base(path))
		}
		a.files[f.Summary.Key] = f
		a.acceptFloatingEvents(events, f.Summary)
		endCount := 0
		for _, event := range events {
			if event.Kind == "turn_completed" && event.Thread == owner {
				endCount++
				at, err := time.Parse(time.RFC3339Nano, event.At)
				if err != nil {
					t.Fatal(err)
				}
				// Replay the wire response after local completion, the same ordering
				// that occurs when a backend restarts and discovers its feed.
				a.acceptFloatingRouteReplay(routetrace.Record{ID: "replay-" + event.Turn, Thread: owner, Turn: event.Turn, Response: "response-" + event.Turn, At: at.Add(-time.Second).Format(time.RFC3339Nano), Stage: "response.completed", Requested: "wanted", Reported: "wanted", Source: "native_wire_response", Scope: "original_desktop_request", PID: 7}, true)
			}
		}
		rows := a.floatingEngine().Snapshot(time.Now().Add(time.Minute)).Rows
		if endCount == 0 || len(rows) != 0 {
			t.Fatalf("completed rollout left %d rows (%s)", len(rows), filepath.Base(path))
		}
		reports = append(reports, Obj{"file": filepath.Base(path), "owner": owner, "source": f.Summary.Source, "completed_events": endCount, "remaining_rows": len(rows), "bytes_read": offset})
	}
	if output := os.Getenv("OBSERVATORY_ROLLOUT_PROBE_OUTPUT"); output != "" {
		b, _ := json.MarshalIndent(reports, "", "  ")
		if err := os.WriteFile(output, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("observed rollout files: %d; remaining completed rows: 0", len(reports))
}
