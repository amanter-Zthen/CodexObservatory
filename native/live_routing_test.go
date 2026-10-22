package main

import (
	"codex-observatory/internal/routetrace"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLiveRouteRetentionPermanentMismatchAndExpiredUnknown(t *testing.T) {
	a, _ := testApp(t)
	clock := time.Now()
	dir, e := a.reportDirectory("live-routing-inbox")
	if e != nil {
		t.Fatal(e)
	}
	for _, verdict := range []string{"match", "unknown", "mismatch", "version_variant"} {
		r := routetrace.Record{ID: verdict, At: clock.Add(-6 * 24 * time.Hour).UTC().Format(time.RFC3339Nano), Thread: "thread", Turn: "turn", Requested: "wanted", Reported: "different", Verdict: verdict, Scope: "original_desktop_request"}
		if e = saveJSON(filepath.Join(dir, verdict+".json"), r); e != nil {
			t.Fatal(e)
		}
	}
	progress := routetrace.Record{ID: "old-progress", At: clock.Add(-6 * 24 * time.Hour).UTC().Format(time.RFC3339Nano), Thread: "thread", Turn: "turn", Requested: "wanted", Reported: "different", Verdict: "mismatch", Scope: "original_desktop_request", Stage: "response.output_text.delta"}
	if e = saveJSON(filepath.Join(dir, "old-progress.json"), progress); e != nil {
		t.Fatal(e)
	}
	if e = a.pruneLiveReports(clock); e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 || filepath.Base(files[0]) != "mismatch.json" {
		t.Fatalf("retained=%v", files)
	}
	archives, _ := filepath.Glob(filepath.Join(a.data, "route-errors", "*.json"))
	if len(archives) != 1 {
		t.Fatalf("archive count=%d", len(archives))
	}
}
func TestBridgeHealthAggregatesConcurrentProcessesAndExpiresStaleStatus(t *testing.T) {
	a, _ := testApp(t)
	dir := filepath.Join(a.data, "routing-bridge-status")
	os.MkdirAll(dir, 0700)
	saveJSON(filepath.Join(dir, "active.json"), Obj{"at": now(), "active": true, "client_pid": 1})
	saveJSON(filepath.Join(dir, "stopped.json"), Obj{"at": now(), "active": false, "client_pid": 2})
	saveJSON(filepath.Join(dir, "stale.json"), Obj{"at": time.Now().Add(-20 * time.Second).UTC().Format(time.RFC3339Nano), "active": true, "client_pid": 3})
	health := a.bridgeStatus()
	if health["active"] != true || health["active_clients"] != 1 {
		t.Fatalf("health=%v", health)
	}
}

func TestNativeListenerReadinessNeedsOriginalPackageAndDoesNotInventVerdicts(t *testing.T) {
	a, _ := testApp(t)
	dir := filepath.Join(a.data, "routing-bridge-status")
	os.MkdirAll(dir, 0700)
	state := Obj{"at": now(), "active": false, "listener_ready": true, "inspection_known": true, "client_pid": 1, "desktop_package_identity": Obj{"verified": false}}
	path := filepath.Join(dir, "native-home.json")
	saveJSON(path, state)
	if a.bridgeStatus()["active"] != false {
		t.Fatal("unpackaged instance was treated as original startup")
	}
	state["desktop_package_identity"] = Obj{"verified": true}
	saveJSON(path, state)
	if a.bridgeStatus()["active"] != true || len(a.liveRouteSnapshot()) != 0 {
		t.Fatal("original listener readiness failed or a verdict was invented")
	}
}
