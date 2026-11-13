//go:build windows

package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRolloutIdentityMigrationRescansOnlyWrongOwner(t *testing.T) {
	a, home := testApp(t)
	path := writeFixture(t, home, "child.jsonl", rec("session_meta", Obj{"id": "child", "source": Obj{"subagent": Obj{}}}), rec("session_meta", Obj{"id": "parent", "source": "vscode"}), rec("event_msg", Obj{"type": "task_started", "turn_id": "own"}), rec("event_msg", Obj{"type": "task_complete", "turn_id": "own"}))
	f := newState(home, path, shortHash(canonical(home)+"\x00"+filepath.Base(path)))
	f.Summary.Thread = "parent"
	f.Summary.Title = "kept title"
	f.Summary.Archived = true
	f.Offset = 99
	f.LastMod = 9
	f.LastSize = 99
	f.Generation = 3
	f.Recent = []Event{{Thread: "parent", Turn: "own", Kind: "turn_started"}}
	if err := a.backfillRolloutIdentity(f); err != nil {
		t.Fatal(err)
	}
	if f.Offset != 0 || f.Generation != 4 || f.IdentitySchema != 1 || f.Summary.Title != "kept title" || !f.Summary.Archived || len(f.Recent) != 0 {
		t.Fatalf("bad migration: %+v", f)
	}
	a.files[f.Summary.Key] = f
	a.scanOnce()
	repaired := a.files[f.Summary.Key]
	if repaired.Summary.Thread != "child" || !repaired.Summary.CaughtUp {
		t.Fatalf("bounded rescan did not recover child: %+v", repaired.Summary)
	}
	var ended bool
	for _, e := range repaired.Recent {
		if e.Kind == "turn_completed" && e.Thread == "child" && e.Turn == "own" {
			ended = true
		}
	}
	if !ended {
		t.Fatal("completed lifecycle was not recovered")
	}
	beforeOffset, beforeGeneration := repaired.Offset, repaired.Generation
	repaired.IdentitySchema = 0
	if err := a.backfillRolloutIdentity(repaired); err != nil {
		t.Fatal(err)
	}
	if repaired.Offset != beforeOffset || repaired.Generation != beforeGeneration {
		t.Fatal("correct checkpoint unnecessarily rescanned")
	}
}

func TestRolloutIdentityMigrationRunsBeforeOldEventsSeed(t *testing.T) {
	a, home := testApp(t)
	clock := time.Now()
	path := writeFixture(t, home, "child.jsonl", identityRecord("session_meta", Obj{"id": "child"}, clock.Add(-time.Minute)))
	f := newState(home, path, "checkpoint")
	f.Summary.Thread = "parent"
	f.Recent = []Event{{ID: "wrong", Thread: "parent", Turn: "wrong", Kind: "turn_started", At: clock.Format(time.RFC3339Nano)}}
	if err := saveJSON(filepath.Join(a.data, "sources", "checkpoint.json"), f); err != nil {
		t.Fatal(err)
	}
	if err := a.initStore(); err != nil {
		t.Fatal(err)
	}
	if len(a.floatingEngine().Snapshot(clock).Rows) != 0 {
		t.Fatal("corrupt cached lifecycle leaked into fresh display")
	}
}
