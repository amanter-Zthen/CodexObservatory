//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopCaptureKeepsOfficialCoreChoiceAndSeparateProxy(t *testing.T) {
	cfg := launchConfig{Desktop: `C:\Program Files\WindowsApps\OpenAI.Codex_test\app\ChatGPT.exe`, Monitor: `D:\monitor\CodexObservatory.exe`, Bridge: `D:\monitor\CodexRoutingBridge-v0.2.3.exe`, Data: `C:\Local\CodexObservatory`}
	old := []string{"CODEX_CLI_PATH=" + cfg.Bridge, "HTTPS_PROXY=http://observer:synthetic@127.0.0.1:1234", "CODEX_CA_CERTIFICATE=" + filepath.Join(cfg.Data, "route-capture", "old", "ca.pem"), `CODEX_HOME=D:\OriginalHome`, `PATH=C:\OriginalPath`}
	command := captureDesktopCommand(cfg, old, "http://observer:new-synthetic@127.0.0.1:2345", `C:\new\ca.pem`)
	if command.Path != cfg.Desktop || len(command.Args) != 1 {
		t.Fatal("official GUI program or arguments changed")
	}
	got := strings.Join(command.Env, "\n")
	if strings.Contains(got, "CODEX_CLI_PATH=") || strings.Contains(got, "CODEX_WINDOWS_REGISTERED_CORE=") || strings.Contains(got, "old\\ca.pem") {
		t.Fatal("observer interfered with official core selection")
	}
	for _, expected := range []string{`CODEX_HOME=D:\OriginalHome`, `PATH=C:\OriginalPath`, "HTTPS_PROXY=http://observer:new-synthetic@127.0.0.1:2345", `CODEX_CA_CERTIFICATE=C:\new\ca.pem`} {
		if !strings.Contains(got, expected) {
			t.Fatalf("missing preserved child setting: %s", expected)
		}
	}
	if old[0] != "CODEX_CLI_PATH="+cfg.Bridge {
		t.Fatal("parent environment mutated by child environment builder")
	}
}

func TestCaptureCleanupPreservesUserOverridesAndCustomProxy(t *testing.T) {
	cfg := launchConfig{Monitor: `D:\monitor\CodexObservatory.exe`, Bridge: `D:\monitor\CodexRoutingBridge-v0.2.3.exe`, Data: `C:\Local\CodexObservatory`}
	env := []string{`CODEX_CLI_PATH=C:\UserTools\codex.exe`, "HTTPS_PROXY=http://user-proxy.example:8080", `CODEX_CA_CERTIFICATE=C:\UserTools\root.pem`}
	got := cleanLaunchEnvironment(env, cfg)
	if strings.Join(got, "\n") != strings.Join(env, "\n") {
		t.Fatal("user configuration removed")
	}
}
