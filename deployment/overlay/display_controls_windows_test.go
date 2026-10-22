//go:build windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDisplayOverlayControlsReadOnlyContract(t *testing.T) {
	var stopped atomic.Bool
	a := &App{
		address: "http://127.0.0.1:43210",
		token:   "isolated-contract-token",
		data:    "private-data-directory-sentinel",
		stop:    func() { stopped.Store(true) },
	}
	a.routeSyncWake = make(chan struct{}, 1)
	a.routeSyncBusy.Store(true)
	handler := a.handler()
	for _, state := range []string{"idle", "syncing", "synced", "health_pending", "awaiting_evidence", "collector_update_required", "synced_with_anomaly"} {
		t.Run(state, func(t *testing.T) {
			status := &RouteSyncStatus{State: state, Message: "只读状态夹具", At: "2026-10-21T00:00:00Z"}
			a.routeSyncState.Store(status)
			request := httptest.NewRequest(http.MethodGet, a.address+"/api/display-controls?key=ignored-history-key", nil)
			request.Header.Set("X-Observatory-Token", a.token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("read-only controls returned %d: %s", response.Code, response.Body.String())
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 2 || fields["version"] == nil || fields["route_sync"] == nil {
				t.Fatalf("controls include fields outside the current-state contract: %s", response.Body.String())
			}
			var version string
			var got RouteSyncStatus
			var statusFields map[string]json.RawMessage
			if json.Unmarshal(fields["version"], &version) != nil || version != Version ||
				json.Unmarshal(fields["route_sync"], &got) != nil || !reflect.DeepEqual(got, *status) ||
				json.Unmarshal(fields["route_sync"], &statusFields) != nil || len(statusFields) != 3 {
				t.Fatalf("unexpected control status: %s", response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" ||
				response.Header().Get("X-Content-Type-Options") != "nosniff" ||
				strings.Contains(response.Body.String(), a.data) || strings.Contains(response.Body.String(), a.token) {
				t.Fatal("controls bypassed original response protection")
			}
			if a.routeSyncState.Load() != status || !a.routeSyncBusy.Load() || len(a.routeSyncWake) != 0 || stopped.Load() {
				t.Fatal("reading controls changed route synchronization or backend lifecycle")
			}
		})
	}
}

func TestDisplayOverlayControlsAuthorizationAndMethods(t *testing.T) {
	a := &App{address: "http://127.0.0.1:43210", token: "isolated-contract-token"}
	handler := a.handler()
	for _, tc := range []struct {
		name, method, token, host, origin, site string
		want                                    int
	}{
		{"authorized-get", "GET", a.token, "", "", "", 200},
		{"missing-token", "GET", "", "", "", "", 403},
		{"wrong-token", "GET", "other-token", "", "", "", 403},
		{"foreign-host", "GET", a.token, "unrelated.invalid", "", "", 403},
		{"foreign-origin", "GET", a.token, "", "https://unrelated.invalid", "", 403},
		{"cross-site", "GET", a.token, "", "", "cross-site", 403},
		{"post-rejected", "POST", a.token, "", "", "", 405},
		{"head-rejected", "HEAD", a.token, "", "", "", 405},
		{"put-rejected", "PUT", a.token, "", "", "", 405},
		{"delete-rejected", "DELETE", a.token, "", "", "", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, a.address+"/api/display-controls", strings.NewReader("{}"))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Observatory-Token", tc.token)
			request.Header.Set("Origin", tc.origin)
			request.Header.Set("Sec-Fetch-Site", tc.site)
			if tc.host != "" {
				request.Host = tc.host
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("got %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
			if tc.method == "POST" && response.Header().Get("Allow") != "GET" {
				t.Fatal("endpoint did not enforce its GET-only contract")
			}
			if a.routeSyncBusy.Load() || a.routeSyncState.Load() != nil || a.routeSyncWake != nil {
				t.Fatal("control request scheduled route synchronization")
			}
		})
	}
}
