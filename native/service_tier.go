package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const appliedTierSource = "event_msg.payload.thread_settings.service_tier"

func serviceTierValue(v any) string {
	value, ok := v.(string)
	if !ok || len(value) > 64 {
		return ""
	}
	value = strings.TrimSpace(value)
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return ""
		}
	}
	return value
}

func contextServiceTier(s *Summary, p Obj, at string) (string, string, string) {
	if value, present := p["service_tier"]; present {
		return serviceTierValue(value), "turn_context.payload.service_tier", at
	}
	if s.Thread != "" && s.ThreadTierOwner == s.Thread {
		return s.ThreadTier, appliedTierSource, s.ThreadTierAt
	}
	return "", "", ""
}

func applyThreadServiceTier(s *Summary, settings Obj, owner, at string) {
	s.Tier = serviceTierValue(settings["service_tier"])
	s.TierSource, s.TierAt = appliedTierSource, at
	// Explicitly applied settings belong to a thread, not its parent or fork.
	// A settings snapshot without a tier clears the prior applied checkpoint.
	s.ThreadTier, s.ThreadTierAt, s.ThreadTierOwner = "", "", ""
	if owner != "" && owner == s.Thread {
		s.ThreadTier, s.ThreadTierAt, s.ThreadTierOwner = s.Tier, at, owner
	}
}

// Recover the missing checkpoint from existing telemetry without resetting the
// rollout cursor, rereading conversation bodies or duplicating usage/history.
func (a *App) backfillServiceTier(f *FileState) error {
	if f.TierSchema >= 1 {
		return nil
	}
	key := f.Summary.Key
	if key == "" || len(key) > 128 || filepath.Base(key) != key || filepath.VolumeName(key) != "" || strings.ContainsAny(key, `/\:`) || key == "." || key == ".." {
		return fmt.Errorf("invalid tier checkpoint key")
	}
	events, _, err := a.history(key, 5000)
	if err != nil {
		return err
	}
	events = append(events, f.Recent...)
	var newest *Event
	var newestAt time.Time
	for _, event := range events {
		if event.Kind != "settings" || event.Field != "event_msg.payload.thread_settings.model" || event.Thread != f.Summary.Thread || str(event.Detail["owner_thread_id"]) != f.Summary.Thread || f.Summary.Thread == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, event.At)
		if err != nil || at.After(time.Now().Add(time.Minute)) || !at.After(newestAt) {
			continue
		}
		copy := event
		newest, newestAt = &copy, at
	}
	if newest != nil {
		previous := f.Summary.Tier
		applyThreadServiceTier(&f.Summary, Obj{"service_tier": newest.Detail["service_tier"]}, f.Summary.Thread, newest.At)
		if previous != "" && previous != f.Summary.Tier {
			// An explicit per-turn override in a legacy checkpoint retains its
			// own value; it is not turned into a server-reported observation.
			f.Summary.Tier, f.Summary.TierSource, f.Summary.TierAt = previous, "legacy_client_checkpoint", f.Summary.SettingsAt
		}
	} else if f.Summary.Tier != "" {
		f.Summary.TierSource, f.Summary.TierAt = "legacy_client_checkpoint", f.Summary.SettingsAt
	}
	f.TierSchema = 1
	return nil
}
