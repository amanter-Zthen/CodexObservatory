//go:build windows

// The frozen server.go registers this handler through its build-only overlay.
package main

import "net/http"

func (a *App) apiDisplayControls(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		fail(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	writeJSON(w, Obj{"version": Version, "route_sync": a.routeSyncStatus()})
}
