//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codex-observatory/internal/capturehome"
)

// Short-lived maintenance commands, dispatched from run() before any desktop
// discovery, path validation or bridge-config persistence:
//
//	CodexObservedLauncher.exe --health-check  [--data D] [--home H]
//	CodexObservedLauncher.exe --repair-stale-native-home [--data D] [--home H]
//
// --health-check is strictly read-only. health_guard.ps1 consumes this JSON and
// must not fall back to counting same-named processes.

type healthReport struct {
	At                 string `json:"at"`
	OK                 bool   `json:"ok"`
	Reason             string `json:"reason,omitempty"`
	DataRoot           string `json:"data_root"`
	CodexHome          string `json:"codex_home,omitempty"`
	ExpectedAddress    string `json:"expected_address,omitempty"`
	ListenerReady      bool   `json:"listener_ready"`
	ListenerPort       int    `json:"listener_port,omitempty"`
	ListenerOwnerPid   int    `json:"listener_owner_pid,omitempty"`
	WatcherPid         int    `json:"watcher_pid,omitempty"`
	WatcherCommandOK   bool   `json:"watcher_command_ok"`
	WatcherIsWatchMode bool   `json:"watcher_is_watch_mode"`
	LifecycleFresh     bool   `json:"lifecycle_fresh"`
	InstanceOwner      string `json:"instance_owner"`
	EnvBlockPresent    bool   `json:"env_block_present"`
	EnvBlockParsable   bool   `json:"env_block_parsable"`
	EnvBlockPort       int    `json:"env_block_port,omitempty"`
	Repaired           bool   `json:"repaired,omitempty"`
}

func runHealthCheck(cfg launchConfig) int {
	rep := collectHealth(cfg)
	emitHealth(rep)
	if rep.OK {
		return 0
	}
	return 1
}

// runRepairStaleNativeHome removes the marked .env block ONLY for the
// NO_LISTENER + NO_WATCHER + proven-stale case. A foreign listener is never
// treated as healthy and never triggers automatic removal.
func runRepairStaleNativeHome(cfg launchConfig) int {
	rep := collectHealth(cfg)

	if rep.ListenerReady && rep.WatcherPid != 0 && rep.ListenerOwnerPid == rep.WatcherPid {
		rep.OK = true
		rep.Reason = "correct listener owned by the watcher; nothing to repair"
		emitHealth(rep)
		return 0
	}
	if rep.ListenerReady && rep.ListenerOwnerPid != rep.WatcherPid {
		rep.OK = false
		rep.Reason = "FOREIGN_LISTENER: refusing to remove the env block; manual review required"
		emitHealth(rep)
		return 1
	}
	if rep.WatcherPid != 0 {
		rep.OK = false
		rep.Reason = "watcher alive but listener not ready; leaving block for reconnect"
		emitHealth(rep)
		return 1
	}
	if !rep.EnvBlockPresent {
		rep.OK = true
		rep.Reason = "no observatory env block; nothing to repair"
		emitHealth(rep)
		return 0
	}
	if !rep.EnvBlockParsable {
		rep.OK = false
		rep.Reason = "env block present but not parsable; manual review required"
		emitHealth(rep)
		return 1
	}
	if rep.ExpectedAddress == "" || rep.EnvBlockPort != portOfAddress(rep.ExpectedAddress) {
		rep.OK = false
		rep.Reason = "env block port differs from expected address; manual review required"
		emitHealth(rep)
		return 1
	}
	if err := removeNativeHome(cfg); err != nil {
		rep.OK = false
		rep.Reason = "removeNativeHome failed: " + err.Error()
		emitHealth(rep)
		return 1
	}
	rep.Repaired = true
	rep.OK = true
	rep.Reason = "stale observatory env block removed"
	emitHealth(rep)
	return 0
}

func collectHealth(cfg launchConfig) healthReport {
	rep := healthReport{At: time.Now().UTC().Format(time.RFC3339Nano), DataRoot: cfg.Data, InstanceOwner: ownerNone}

	if identity, err := loadNativeIdentity(cfg); err == nil && identity.Address != "" {
		rep.ExpectedAddress = identity.Address
		rep.ListenerPort = portOfAddress(identity.Address)
		if rep.ListenerPort > 0 {
			rep.ListenerOwnerPid = tcpListenerPID(uint16(rep.ListenerPort))
			rep.ListenerReady = rep.ListenerOwnerPid != 0
		}
	}

	if b, err := os.ReadFile(filepath.Join(cfg.Data, "lifecycle-status.json")); err == nil && len(b) < 8192 {
		var st struct {
			At         string `json:"at"`
			WatcherPID int    `json:"watcher_pid"`
		}
		if json.Unmarshal(b, &st) == nil {
			rep.WatcherPid = st.WatcherPID
			if t, err := time.Parse(time.RFC3339Nano, st.At); err == nil {
				rep.LifecycleFresh = time.Since(t) < 10*time.Second
			}
		}
	}
	if rep.WatcherPid != 0 {
		if !processAlive(rep.WatcherPid) {
			rep.WatcherPid = 0
		} else {
			img := strings.ToLower(imagePathOfPID(uint32(rep.WatcherPid)))
			rep.WatcherCommandOK = strings.HasSuffix(img, strings.ToLower(watcherExeName()))
			rep.WatcherIsWatchMode = watcherHasWatchFlag(cfg, rep.WatcherPid)
		}
	}

	if home, err := nativeHome(cfg); err == nil {
		rep.CodexHome = home
		if raw, err := os.ReadFile(filepath.Join(home, ".env")); err == nil {
			if _, err := capturehome.Remove(raw); err != nil {
				rep.EnvBlockPresent = true
				rep.EnvBlockParsable = false
			} else if capturehome.HasBlock(raw) {
				rep.EnvBlockPresent = true
				rep.EnvBlockParsable = true
				rep.EnvBlockPort = capturehome.BlockPort(raw)
			}
		}
	}

	if rep.ListenerReady && rep.WatcherPid != 0 && rep.ListenerOwnerPid == rep.WatcherPid {
		rep.InstanceOwner = ownerTracked
	} else if rep.ListenerReady && rep.WatcherPid == 0 {
		rep.InstanceOwner = ownerForeignLive
	}

	rep.OK = rep.ListenerReady &&
		rep.LifecycleFresh &&
		rep.WatcherPid != 0 &&
		rep.WatcherCommandOK &&
		rep.WatcherIsWatchMode &&
		rep.ListenerOwnerPid == rep.WatcherPid &&
		rep.ExpectedAddress != "" &&
		rep.ExpectedAddress == expectedAddressWithPort(rep.ListenerPort) &&
		rep.EnvBlockPresent && rep.EnvBlockParsable && rep.EnvBlockPort == rep.ListenerPort
	if !rep.OK && rep.Reason == "" {
		rep.Reason = "health criteria not satisfied"
	}
	return rep
}

func emitHealth(rep healthReport) {
	b, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Println(string(b))
}

func portOfAddress(address string) int {
	i := strings.LastIndex(address, ":")
	if i < 0 {
		return 0
	}
	n := 0
	for _, r := range address[i+1:] {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func expectedAddressWithPort(port int) string {
	if port <= 0 {
		return ""
	}
	return fmt.Sprintf("127.0.0.1:%d", port)
}

func watcherExeName() string { return "CodexObservedLauncher.exe" }

// watcherHasWatchFlag reads the mode the watch loop publishes for itself.
//
// Strength of evidence: WatcherCommandOK (image-name check) proves the PID is a
// CodexObservedLauncher.exe process; watcher_mode is written by the watch loop
// itself. Together they establish "a launcher running the watch loop", the
// strongest check available without reading another process's command line.
func watcherHasWatchFlag(cfg launchConfig, pid int) bool {
	b, err := os.ReadFile(filepath.Join(cfg.Data, "lifecycle-status.json"))
	if err != nil || len(b) > 8192 {
		return false
	}
	var st struct {
		WatcherPID  int    `json:"watcher_pid"`
		WatcherMode string `json:"watcher_mode"`
	}
	if json.Unmarshal(b, &st) != nil {
		return false
	}
	return st.WatcherPID == pid && st.WatcherMode == "watch"
}
