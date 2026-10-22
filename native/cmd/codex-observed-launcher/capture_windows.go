//go:build windows

package main

import (
	"codex-observatory/internal/lifecycle"
	"codex-observatory/internal/routetrace"
	"codex-observatory/internal/wirecapture"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func ownedCLIPath(path string, cfg launchConfig) bool {
	if strings.EqualFold(path, cfg.Bridge) && path != "" {
		return true
	}
	return strings.EqualFold(filepath.Dir(path), filepath.Dir(cfg.Monitor)) && strings.HasPrefix(strings.ToLower(filepath.Base(path)), "codexroutingbridge")
}

func ownedProxy(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.User != nil && u.User.Username() == "observer" && u.Hostname() == "127.0.0.1"
}

func ownedCertificate(path string, cfg launchConfig) bool {
	for _, name := range []string{"route-capture", "native-capture"} {
		root := strings.ToLower(filepath.Clean(filepath.Join(cfg.Data, name))) + string(filepath.Separator)
		if strings.HasPrefix(strings.ToLower(filepath.Clean(path)), root) {
			return true
		}
	}
	return false
}

// A launch from a tool running under an older observer can inherit its private
// relay. Build the official Desktop's environment from the original settings.
func cleanLaunchEnvironment(env []string, cfg launchConfig) []string {
	result := []string{}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "CODEX_CLI_PATH":
			if ownedCLIPath(value, cfg) {
				continue
			}
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY":
			if ownedProxy(value) {
				continue
			}
		case "CODEX_CA_CERTIFICATE", "SSL_CERT_FILE":
			if ownedCertificate(value, cfg) {
				continue
			}
		}
		result = append(result, entry)
	}
	return result
}

func setLaunchEnvironment(env []string, key, value string) []string {
	result := []string{}
	for _, entry := range env {
		if !strings.EqualFold(strings.SplitN(entry, "=", 2)[0], key) {
			result = append(result, entry)
		}
	}
	return append(result, key+"="+value)
}

func captureDesktopCommand(cfg launchConfig, env []string, proxyURL, caPath string) *exec.Cmd {
	command := exec.Command(cfg.Desktop)
	command.Dir = filepath.Dir(cfg.Desktop)
	command.Env = cleanLaunchEnvironment(env, cfg)
	command.Env = setLaunchEnvironment(command.Env, "HTTP_PROXY", proxyURL)
	command.Env = setLaunchEnvironment(command.Env, "HTTPS_PROXY", proxyURL)
	command.Env = setLaunchEnvironment(command.Env, "CODEX_CA_CERTIFICATE", caPath)
	command.Env = setLaunchEnvironment(command.Env, "NO_PROXY", "")
	return command
}

func launchDesktopCapture(cfg launchConfig) error {
	release, already, err := watcherLock(cfg.Data + "-capture")
	if err != nil {
		return err
	}
	defer release()
	if already {
		return startMonitor(cfg)
	}
	// This process is the capture host; upstream networking keeps the user's
	// original configuration, independently of the child's loopback relay.
	original := os.Environ()
	cleaned := cleanLaunchEnvironment(original, cfg)
	remaining := map[string]bool{}
	for _, entry := range cleaned {
		key, _, _ := strings.Cut(entry, "=")
		remaining[strings.ToUpper(key)] = true
	}
	for _, entry := range original {
		key, _, _ := strings.Cut(entry, "=")
		if !remaining[strings.ToUpper(key)] {
			_ = os.Unsetenv(key)
		}
	}
	inbox := filepath.Join(cfg.Data, "live-routing-inbox")
	statuses := filepath.Join(cfg.Data, "routing-bridge-status")
	certDir := filepath.Join(cfg.Data, "route-capture", fmt.Sprintf("entry-%d", os.Getpid()))
	for _, dir := range []string{inbox, statuses, certDir} {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	defer os.Remove(certDir)
	collector := routetrace.New(0)
	var recordMu sync.Mutex
	failures := uint64(0)
	proxy, err := wirecapture.Start(collector, func(rows []routetrace.Record) {
		recordMu.Lock()
		defer recordMu.Unlock()
		for _, row := range rows {
			if saveConfig(filepath.Join(inbox, row.ID+".json"), row) != nil {
				failures++
			}
		}
	}, false)
	if err != nil {
		return fmt.Errorf("实时采集启动失败：%v", err)
	}
	defer proxy.Close()
	certPath := filepath.Join(certDir, "ca.pem")
	if err = os.WriteFile(certPath, proxy.CA, 0600); err != nil {
		return err
	}
	defer os.Remove(certPath)
	command := captureDesktopCommand(cfg, cleaned, proxy.URL, certPath)
	if err = command.Start(); err != nil {
		return fmt.Errorf("Codex 启动失败：%v", err)
	}
	desktopPID := command.Process.Pid
	collector.SetPID(desktopPID)
	go command.Wait()
	healthPath := filepath.Join(statuses, fmt.Sprintf("entry-%d.json", os.Getpid()))
	health := func(active bool, known bool) {
		recordMu.Lock()
		errors := failures
		recordMu.Unlock()
		_ = saveConfig(healthPath, map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "active": active, "client_pid": desktopPID, "client_role": "desktop", "bridge_pid": os.Getpid(), "protocol": "desktop-environment-proxy", "capture_revision": "core-preserving-v1", "official_cli_selection_preserved": true, "inspection_known": known, "model_requests_generated": 0, "metadata_write_errors": errors, "diagnostics": collector.Stats()})
	}
	defer health(false, true)
	health(true, true)
	follower := lifecycle.Follower{Grace: 3 * time.Second}
	follower.Update(true, true, time.Now())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for now := range ticker.C {
		present, inspectionErr := running(cfg.Desktop)
		if follower.Update(present, inspectionErr == nil, now) == lifecycle.Stop {
			return nil
		}
		health(true, inspectionErr == nil)
	}
	return nil
}
