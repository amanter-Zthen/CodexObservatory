//go:build windows

package main

import "testing"

func TestIsCodexWindowsAppsDesktopPath(t *testing.T) {
	if isCodexWindowsAppsDesktopPath(`C:\Temp\ChatGPT.exe`) {
		t.Fatal("a temp stub path must not be treated as a packaged Codex path")
	}
	if isCodexWindowsAppsDesktopPath("") {
		t.Fatal("empty path must be false")
	}
	pkg := `C:\Program Files\WindowsApps\OpenAI.Codex_26.928.2636.0_x64__2p2nqsd0c76g0\app\ChatGPT.exe`
	if !isCodexWindowsAppsDesktopPath(pkg) {
		t.Fatal("a packaged Codex path must be true")
	}
}
