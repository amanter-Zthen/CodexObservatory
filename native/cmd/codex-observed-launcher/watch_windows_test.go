//go:build windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestFollowerShutdownAuthenticatesOnlyApplicationLoopback(t *testing.T) {
	resetSupervisorState()
	t.Cleanup(resetSupervisorState)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/shutdown" || r.Header.Get("X-Observatory-Token") != "synthetic-token" {
			w.WriteHeader(403)
			return
		}
		calls.Add(1)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	data := t.TempDir()
	if e := saveConfig(filepath.Join(data, "instance.json"), map[string]string{"url": server.URL + "/#synthetic-token"}); e != nil {
		t.Fatal(e)
	}
	// The test server belongs to this test process. Prove that an untracked
	// listener is protected before declaring it to be the managed test child.
	if e := stopMonitor(data); e == nil || calls.Load() != 0 {
		t.Fatal("untracked live instance must not receive shutdown")
	}
	seedChild(1, time.Now(), false, "")
	childMu.Lock()
	tracked.pid = os.Getpid()
	childMu.Unlock()
	if owner := ownerOfInstance(launchConfig{Data: data}); owner != ownerTracked {
		t.Fatalf("test endpoint owner = %s, want %s", owner, ownerTracked)
	}
	if e := stopMonitor(data); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("shutdown not received")
	}
}
func TestFollowerRejectsForeignEndpointAndDuplicates(t *testing.T) {
	resetSupervisorState()
	t.Cleanup(resetSupervisorState)
	data := t.TempDir()
	if e := stopMonitor(data); e != nil {
		t.Fatal("missing instance should already be stopped")
	}
	// A managed child whose API has not been established must still validate
	// endpoint metadata before making any shutdown request.
	seedChild(1, time.Now(), false, "")
	childMu.Lock()
	tracked.pid = os.Getpid()
	childMu.Unlock()
	for _, endpoint := range []string{"https://127.0.0.1/#token", "http://external.invalid/#token", "http://127.0.0.1/#", "http://user:pass@127.0.0.1/#token"} {
		b, _ := json.Marshal(map[string]string{"url": endpoint})
		os.WriteFile(filepath.Join(data, "instance.json"), b, 0600)
		if stopMonitor(data) == nil {
			t.Fatalf("foreign endpoint accepted: %s", endpoint)
		}
	}
	close1, exists, e := watcherLock(data)
	if e != nil || exists {
		t.Fatal("initial watcher lock failed")
	}
	defer close1()
	close2, exists, e := watcherLock(data)
	if e != nil || !exists {
		t.Fatal("duplicate watcher not recognized")
	}
	close2()
}
