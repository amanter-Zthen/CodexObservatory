package main

import (
	"os"
	"path/filepath"
	"testing"
)

func tierFixture() *FileState {
	f := newState("home", "file", "fixture-key")
	parseLine(f, rec("session_meta", Obj{"id": "current"}), 0)
	return f
}
func applyTierFixture(f *FileState, tier any, present bool, owner string) {
	settings := Obj{"model": "wanted", "reasoning_effort": "xhigh"}
	if present {
		settings["service_tier"] = tier
	}
	parseLine(f, rec("event_msg", Obj{"type": "thread_settings_applied", "thread_id": owner, "thread_settings": settings}), 1)
}

func TestServiceTierAppliedSettingSurvivesOmittedTurnField(t *testing.T) {
	f := tierFixture()
	applyTierFixture(f, "default", true, "current")
	parseLine(f, rec("turn_context", Obj{"model": "wanted", "effort": "xhigh"}), 2)
	if f.Summary.Tier != "default" || f.Summary.TierSource != appliedTierSource || f.Summary.ThreadTierOwner != "current" {
		t.Fatal(f.Summary)
	}
	parseLine(f, rec("turn_context", Obj{"model": "next-model", "effort": "low"}), 3)
	if f.Summary.Tier != "default" || f.Summary.Model != "next-model" {
		t.Fatal(f.Summary)
	}
}
func TestServiceTierPerTurnOverrideAndExplicitNull(t *testing.T) {
	f := tierFixture()
	applyTierFixture(f, "default", true, "current")
	parseLine(f, rec("turn_context", Obj{"model": "wanted", "effort": "xhigh", "service_tier": "priority"}), 2)
	if f.Summary.Tier != "priority" || f.Summary.TierSource != "turn_context.payload.service_tier" {
		t.Fatal(f.Summary)
	}
	parseLine(f, rec("turn_context", Obj{"model": "wanted", "effort": "xhigh", "service_tier": nil}), 3)
	if f.Summary.Tier != "" {
		t.Fatal("explicit null was replaced by a guessed default")
	}
	parseLine(f, rec("turn_context", Obj{"model": "wanted", "effort": "xhigh"}), 4)
	if f.Summary.Tier != "default" {
		t.Fatal("next turn lost its independently applied thread setting")
	}
}
func TestServiceTierMissingAppliedSnapshotClearsCheckpoint(t *testing.T) {
	f := tierFixture()
	applyTierFixture(f, "priority", true, "current")
	applyTierFixture(f, nil, false, "current")
	parseLine(f, rec("turn_context", Obj{"model": "wanted", "effort": "xhigh"}), 3)
	if f.Summary.Tier != "" || f.Summary.ThreadTier != "" {
		t.Fatal(f.Summary)
	}
}
func TestServiceTierDoesNotInheritFromForeignThread(t *testing.T) {
	f := tierFixture()
	applyTierFixture(f, "default", true, "current")
	applyTierFixture(f, "priority", true, "parent")
	parseLine(f, rec("turn_context", Obj{"model": "wanted", "effort": "xhigh"}), 3)
	if f.Summary.Tier != "default" || f.Summary.ThreadTierOwner != "current" {
		t.Fatal(f.Summary)
	}
	parseLine(f, rec("session_meta", Obj{"id": "fork"}), 4)
	parseLine(f, rec("turn_context", Obj{"model": "wanted", "effort": "xhigh"}), 5)
	if f.Summary.Tier != "" {
		t.Fatal("parent thread's tier leaked into a fork")
	}
}
func TestServiceTierOmittedWithoutAppliedSettingRemainsUnknown(t *testing.T) {
	f := tierFixture()
	parseLine(f, rec("turn_context", Obj{"model": "wanted", "effort": "xhigh", "service_tier": "flex"}), 1)
	parseLine(f, rec("turn_context", Obj{"model": "wanted", "effort": "xhigh"}), 2)
	if f.Summary.Tier != "" {
		t.Fatal("per-turn override was silently carried forward")
	}
}
func TestServiceTierLegacyCheckpointBackfillKeepsCursorAndUsage(t *testing.T) {
	a, home := testApp(t)
	f := newState(home, "file", "fixture-key")
	f.Summary.Thread = "current"
	f.Offset = 4096
	f.LastMod = 55
	f.Anchor = "existing-anchor"
	f.Summary.Latest = &Usage{Input: 123, Output: 45}
	e := Event{ID: "existing-setting", At: now(), Kind: "settings", Field: "event_msg.payload.thread_settings.model", Thread: "current", Detail: Obj{"owner_thread_id": "current", "service_tier": "default"}}
	if err := os.WriteFile(filepath.Join(a.data, "history", f.Summary.Key+".jsonl"), append(jsonBytes(e), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(a.data, "history", f.Summary.Key+".jsonl"))
	if err := a.backfillServiceTier(f); err != nil {
		t.Fatal(err)
	}
	if f.Summary.Tier != "default" || f.TierSchema != 1 || f.Offset != 4096 || f.LastMod != 55 || f.Anchor != "existing-anchor" || f.Summary.Latest.Input != 123 {
		t.Fatal(f)
	}
	if err := a.backfillServiceTier(f); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(a.data, "history", f.Summary.Key+".jsonl"))
	if string(before) != string(after) {
		t.Fatal("checkpoint recovery rewrote history")
	}
}
func TestServiceTierLegacyCheckpointRejectsForeignEvidence(t *testing.T) {
	a, home := testApp(t)
	f := newState(home, "file", "fixture-key")
	f.Summary.Thread = "current"
	f.Recent = []Event{{At: now(), Kind: "settings", Field: "event_msg.payload.thread_settings.model", Thread: "current", Detail: Obj{"owner_thread_id": "parent", "service_tier": "priority"}}}
	if err := a.backfillServiceTier(f); err != nil {
		t.Fatal(err)
	}
	if f.Summary.Tier != "" {
		t.Fatal(f.Summary)
	}
}
func TestServiceTierInitializationRecoversLegacyCache(t *testing.T) {
	a, home := testApp(t)
	key := shortHash("fixture")
	f := newState(home, "file", key)
	f.Summary.Thread = "current"
	f.Offset = 42
	f.Recent = []Event{{At: now(), Kind: "settings", Field: "event_msg.payload.thread_settings.model", Thread: "current", Detail: Obj{"owner_thread_id": "current", "service_tier": "default"}}}
	if err := saveJSON(filepath.Join(a.data, "sources", key+".json"), f); err != nil {
		t.Fatal(err)
	}
	if err := saveJSON(filepath.Join(a.data, "settings.json"), a.cfg); err != nil {
		t.Fatal(err)
	}
	if err := a.initStore(); err != nil {
		t.Fatal(err)
	}
	if a.files[key].Summary.Tier != "default" || a.files[key].Offset != 42 {
		t.Fatal(a.files[key])
	}
	var checkpoint FileState
	if err := loadJSON(filepath.Join(a.data, "sources", key+".json"), &checkpoint); err != nil {
		t.Fatal(err)
	}
	if checkpoint.TierSchema != 1 || checkpoint.Summary.Tier != "default" {
		t.Fatal(checkpoint)
	}
}
func TestServiceTierValueKeepsOnlyBoundedMetadata(t *testing.T) {
	for _, value := range []any{nil, Obj{"secret": "body"}, "private text with spaces", "<tag>", "token\nvalue"} {
		if serviceTierValue(value) != "" {
			t.Fatal(value)
		}
	}
	if serviceTierValue("default") != "default" || serviceTierValue("priority") != "priority" {
		t.Fatal("valid tiers were lost")
	}
}
