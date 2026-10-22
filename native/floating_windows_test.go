//go:build windows

package main

import (
	"codex-observatory/internal/routetrace"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFloatingStateUsesOnlyCurrentBackendEvidence(t *testing.T) {
	a, _ := testApp(t)
	dir := filepath.Join(a.data, "routing-bridge-status")
	os.MkdirAll(dir, 0700)
	saveJSON(filepath.Join(dir, "active.json"), Obj{"at": now(), "active": true, "client_pid": 42})
	a.liveRoutes = map[string]routetrace.Record{"old": {At: now(), Thread: "old", Turn: "turn", PID: 10, Verdict: "match", Requested: "old-model", Reported: "old-model"}}
	title, _, _, _, _, _ := a.floatingLines()
	if !strings.Contains(title, "等待实际响应") {
		t.Fatalf("old backend caused false success: %s", title)
	}
	a.liveRoutes["current"] = routetrace.Record{At: now(), Thread: "019c6e27-e55b-73d1-87d8-4e01f1f75043", Turn: "turn", PID: 42, Verdict: "pending", Requested: "wanted"}
	title, _, _, _, links, _ := a.floatingLines()
	if !strings.Contains(title, "待确认") || len(links) != 1 || links[0] == "" {
		t.Fatalf("pending=%s links=%v", title, links)
	}
	r := a.liveRoutes["current"]
	r.Verdict = "match"
	r.Reported = "wanted"
	a.liveRoutes["current"] = r
	title, _, _, _, _, _ = a.floatingLines()
	if !strings.Contains(title, "标签一致") {
		t.Fatal(title)
	}
	r.Verdict = "mismatch"
	r.Reported = "other"
	a.liveRoutes["current"] = r
	title, _, _, _, _, _ = a.floatingLines()
	if !strings.Contains(title, "异常") {
		t.Fatal(title)
	}
	r.At = time.Now().Add(-11 * time.Minute).UTC().Format(time.RFC3339Nano)
	a.liveRoutes["current"] = r
	title, _, _, _, _, _ = a.floatingLines()
	if !strings.Contains(title, "等待实际响应") {
		t.Fatal("stale result presented as current")
	}
}
