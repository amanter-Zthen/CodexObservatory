//go:build windows

package main

import "testing"

func TestInstalledDesktopProcessCanBeDetectedBeforeRestart(t *testing.T) {
	path := installedDesktop("")
	if path == "" {
		t.Skip("official Desktop not installed on this test host")
	}
	found, e := running(path)
	if e != nil {
		t.Fatal(e)
	}
	if !found {
		t.Skip("official Desktop is not running on this test host")
	}
}
