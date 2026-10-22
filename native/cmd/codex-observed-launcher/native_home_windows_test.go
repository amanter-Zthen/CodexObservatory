//go:build windows

package main

import (
	"bytes"
	"codex-observatory/internal/capturehome"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeHomeSetupIsStableAndDisablePreservesUserSettings(t *testing.T) {
	root := t.TempDir()
	cfg := launchConfig{Home: root, Data: filepath.Join(root, "application-data"), Monitor: filepath.Join(root, "monitor.exe")}
	for _, key := range []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "CODEX_CA_CERTIFICATE", "SSL_CERT_FILE"} {
		t.Setenv(key, "")
	}
	path := filepath.Join(root, ".env")
	base := []byte("USER_SETTING='original'\r\n")
	if err := os.WriteFile(path, base, 0600); err != nil {
		t.Fatal(err)
	}
	if err := setupNativeHome(cfg); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	identity, _ := os.ReadFile(nativeIdentityPath(cfg))
	// A repeated enable should work while the production-style host is bound.
	host, err := startNativeHomeCapture(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer host.close()
	if !nativeCaptureReady(cfg) || host.collector.Stats()["metadata_records"] != 0 {
		t.Fatal("local readiness check generated a model request or failed")
	}
	if err = setupNativeHome(cfg); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	saved, _ := os.ReadFile(nativeIdentityPath(cfg))
	if !bytes.Equal(first, second) || !bytes.Equal(identity, saved) {
		t.Fatal("re-enable changed startup identity")
	}
	if bytes.Count(second, []byte(capturehome.Begin)) != 1 {
		t.Fatal("capture block duplicated")
	}
	if err = os.WriteFile(path, append(second, []byte("LATER_SETTING='retained'\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err = removeNativeHome(cfg); err != nil {
		t.Fatal(err)
	}
	removed, _ := os.ReadFile(path)
	if !bytes.Equal(removed, append(base, []byte("LATER_SETTING='retained'\n")...)) {
		t.Fatal("disable changed user settings")
	}
}
