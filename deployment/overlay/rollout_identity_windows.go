//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
)

// A rollout has one owner. Copied parent headers describe inherited history;
// they do not change the identity or source of the file containing them.
func ownRolloutMetadata(f *FileState, p Obj) bool {
	id := first(str(p["id"]), str(p["thread_id"]), str(p["session_id"]))
	return id == "" || f.Summary.Thread == "" || id == f.Summary.Thread
}

// Upgrade existing source checkpoints from the authoritative physical header.
// Only mismatched checkpoints restart the existing bounded scanner; no session
// file, capture configuration, or conversation content is changed.
func (a *App) backfillRolloutIdentity(f *FileState) error {
	if f.IdentitySchema >= 1 {
		return nil
	}
	file, err := os.Open(f.Summary.File)
	if err != nil {
		return err
	}
	defer file.Close()
	b, _, complete, oversize, err := readRecord(bufio.NewReaderSize(file, 128*1024))
	if err != nil || !complete || oversize {
		return fmt.Errorf("rollout header is not yet available")
	}
	record, err := decodeObj(b)
	if err != nil || str(record["type"]) != "session_meta" {
		return fmt.Errorf("rollout first record has no session identity")
	}
	payload := obj(record["payload"])
	owner := first(str(payload["id"]), str(payload["thread_id"]), str(payload["session_id"]))
	if owner == "" {
		return fmt.Errorf("rollout header has no owner")
	}
	reset := owner != f.Summary.Thread
	for _, event := range f.Recent {
		if event.Thread != "" && event.Thread != owner {
			reset = true
			break
		}
	}
	if reset {
		fresh := newState(f.Summary.Home, f.Summary.File, f.Summary.Key)
		fresh.Generation = f.Generation + 1
		fresh.Summary.Title = f.Summary.Title
		fresh.Summary.Archived = f.Summary.Archived
		*f = *fresh
	}
	f.IdentitySchema = 1
	return nil
}
