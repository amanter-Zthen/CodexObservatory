//go:build windows

package main

import (
	"bufio"
	"codex-observatory/internal/routetrace"
	"codex-observatory/internal/wirecapture"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDirectNativeClientUsesDesktopCaptureEnvironmentWithoutCLIWrapper(t *testing.T) {
	testNativeCapture(t, false, "")
}

// Exercise the installed native core's own startup loader. The child receives
// no capture proxy or CA variables from the launcher and reads only its home.
func TestNativeCoreLoadsCaptureFromHomeDotEnv(t *testing.T) {
	t.Run("cached_core", func(t *testing.T) { testNativeCapture(t, true, "") })
	packaged, _ := filepath.Glob(filepath.Join(os.Getenv("ProgramFiles"), "WindowsApps", "OpenAI.Codex_*_x64__*", "app", "resources", "codex.exe"))
	// WindowsApps parent enumeration may be denied while registered package
	// metadata and the exact bundled executable remain readable.
	if len(packaged) == 0 {
		if desktop := installedDesktop(""); desktop != "" {
			candidate := filepath.Join(filepath.Dir(desktop), "resources", "codex.exe")
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				packaged = append(packaged, candidate)
			}
		}
	}
	if len(packaged) > 0 {
		t.Run("official_package_core", func(t *testing.T) { testNativeCapture(t, true, newest(packaged)) })
	} else {
		t.Log("bundled package core unavailable; cached installed core was tested")
	}
}

func testNativeCapture(t *testing.T, fromHome bool, nativeOverride string) {
	candidates, _ := filepath.Glob(filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenAI", "Codex", "bin", "*", "codex.exe"))
	if len(candidates) == 0 {
		t.Skip("native Codex is not installed on this host")
	}
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"models":[],"data":[]}`)
			return
		}
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		item := map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "synthetic fixture only", "annotations": []any{}}}}
		response := map[string]any{"id": "resp_direct_fixture", "object": "response", "model": "gpt-6.1-sol", "status": "in_progress", "output": []any{}}
		events := []map[string]any{
			{"type": "response.metadata", "headers": map[string]any{"openai-model": []any{"gpt-5.6-luna"}}},
			{"type": "response.created", "response": response},
			{"type": "response.output_item.done", "output_index": 0, "item": item},
			{"type": "response.completed", "response": map[string]any{"id": "resp_direct_fixture", "object": "response", "model": "gpt-6.1-sol", "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}},
		}
		for _, event := range events {
			b, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", b)
			w.(http.Flusher).Flush()
		}
	}))
	defer backend.Close()
	root := t.TempDir()
	backendCA := filepath.Join(root, "backend-ca.pem")
	os.WriteFile(backendCA, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: backend.Certificate().Raw}), 0600)
	t.Setenv("CODEX_CA_CERTIFICATE", backendCA)
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	collector := routetrace.New(1)
	var mu sync.Mutex
	rows := []routetrace.Record{}
	proxy, err := wirecapture.Start(collector, func(in []routetrace.Record) { mu.Lock(); defer mu.Unlock(); rows = append(rows, in...) }, true)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	captureCA := filepath.Join(root, "capture-ca.pem")
	os.WriteFile(captureCA, proxy.CA, 0600)
	config := fmt.Sprintf("model = \"gpt-6.1-sol\"\nmodel_provider = \"offline\"\n[model_providers.offline]\nname = \"Loopback fixture\"\nbase_url = \"%s/v1\"\nwire_api = \"responses\"\nenv_key = \"OBSERVATORY_OFFLINE_TEST_KEY\"\nrequires_openai_auth = false\nsupports_websockets = false\nrequest_max_retries = 0\nstream_max_retries = 0\n", backend.URL)
	os.WriteFile(filepath.Join(root, "config.toml"), []byte(config), 0600)
	cfg := launchConfig{Desktop: newest(candidates), Monitor: filepath.Join(root, "CodexObservatory.exe"), Bridge: filepath.Join(root, "CodexRoutingBridge-old.exe"), Data: root}
	if nativeOverride != "" {
		cfg.Desktop = nativeOverride
	}
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "OPENAI_API_KEY") && !strings.EqualFold(key, "CODEX_API_KEY") && !strings.EqualFold(key, "CODEX_CLI_PATH") {
			env = append(env, entry)
		}
	}
	env = append(env, "CODEX_CLI_PATH="+cfg.Bridge)
	env = setLaunchEnvironment(env, "CODEX_HOME", root)
	env = setLaunchEnvironment(env, "OBSERVATORY_OFFLINE_TEST_KEY", "loopback-synthetic-placeholder")
	command := captureDesktopCommand(cfg, env, proxy.URL, captureCA)
	if fromHome {
		command.Env = env
		filtered := []string{}
		for _, entry := range command.Env {
			key, _, _ := strings.Cut(entry, "=")
			switch strings.ToUpper(key) {
			case "CODEX_CLI_PATH", "CODEX_CA_CERTIFICATE", "SSL_CERT_FILE", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
				continue
			}
			filtered = append(filtered, entry)
		}
		command.Env = filtered
		// Single quotes preserve Windows paths without backslash escape rules.
		dotenv := "HTTPS_PROXY='" + proxy.URL + "'\nHTTP_PROXY='" + proxy.URL + "'\nSSL_CERT_FILE='" + captureCA + "'\nNO_PROXY=''\n"
		if err = os.WriteFile(filepath.Join(root, ".env"), []byte(dotenv), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command.Args = append(command.Args, "-c", "features.code_mode_host=true", "app-server", "--listen", "stdio://")
	command.Stderr = io.Discard
	stdin, _ := command.StdinPipe()
	stdout, _ := command.StdoutPipe()
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); command.Process.Kill(); command.Wait() }()
	incoming := make(chan map[string]any, 256)
	go func() {
		defer close(incoming)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 65536), 4*1024*1024)
		for scanner.Scan() {
			var message map[string]any
			if json.Unmarshal(scanner.Bytes(), &message) == nil {
				incoming <- message
			}
		}
	}()
	send := func(message any) {
		t.Helper()
		if err := json.NewEncoder(stdin).Encode(message); err != nil {
			t.Fatal(err)
		}
	}
	receive := func(predicate func(map[string]any) bool) map[string]any {
		t.Helper()
		timeout := time.After(15 * time.Second)
		for {
			select {
			case message, ok := <-incoming:
				if !ok {
					t.Fatal("native fixture closed before reply")
				}
				if predicate(message) {
					if message["error"] != nil {
						t.Fatalf("fixture RPC error: %v", message["error"])
					}
					return message
				}
			case <-timeout:
				t.Fatal("direct capture fixture timed out")
			}
		}
	}
	send(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]any{"name": "observatory_direct_fixture", "version": "0.2.4"}, "capabilities": map[string]any{"experimentalApi": true}}})
	receive(func(m map[string]any) bool { return m["id"] == float64(1) })
	send(map[string]any{"method": "initialized"})
	send(map[string]any{"id": 2, "method": "thread/start", "params": map[string]any{"cwd": root, "model": "gpt-6.1-sol", "modelProvider": "offline", "ephemeral": true, "approvalPolicy": "never", "sandbox": "read-only"}})
	result := receive(func(m map[string]any) bool { return m["id"] == float64(2) })
	thread := result["result"].(map[string]any)["thread"].(map[string]any)["id"].(string)
	send(map[string]any{"id": 3, "method": "turn/start", "params": map[string]any{"threadId": thread, "input": []any{map[string]any{"type": "text", "text": "LOCAL SYNTHETIC FIXTURE"}}, "model": "gpt-6.1-sol", "effort": "low", "approvalPolicy": "never"}})
	receive(func(m map[string]any) bool { return m["id"] == float64(3) })
	receive(func(m map[string]any) bool { return m["method"] == "turn/completed" })
	mu.Lock()
	defer mu.Unlock()
	evidence := false
	for _, row := range rows {
		if row.Stage == "response.completed" && row.Thread == thread && row.Verdict == "mismatch" && row.Reported == "gpt-5.6-luna" && row.OutputTokens == 2 && row.TokensPerSecond > 0 {
			evidence = true
		}
	}
	if !evidence {
		t.Fatalf("direct native response, identity or speed missing: %+v", rows)
	}
	serialized, _ := json.Marshal(rows)
	if strings.Contains(string(serialized), "synthetic fixture only") || strings.Contains(string(serialized), "LOCAL SYNTHETIC FIXTURE") {
		t.Fatal("fixture text retained")
	}
}
