//go:build windows

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

// Exercise the real projected main() flag registration and parser, exiting at
// --help before data-directory, instance, feed, browser or window operations.
func displayStartupFlagsHelper() {
	if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "--help" {
		os.Exit(91)
	}
	for _, arg := range os.Args[1 : len(os.Args)-1] {
		if arg != "--no-open" && arg != "--no-open=true" && arg != "--no-open=false" {
			os.Exit(92)
		}
	}
	flag.CommandLine = flag.NewFlagSet("isolated-startup-flags", flag.ExitOnError)
	flag.CommandLine.Usage = func() {
		selected := flag.CommandLine.Lookup("no-open")
		if selected == nil {
			os.Exit(93)
		}
		fmt.Printf("no-open=%s;default=%s\n", selected.Value.String(), selected.DefValue)
	}
	main()
	os.Exit(94)
}

func TestDisplayOverlayStartupShowsOnlyFloatingByDefault(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"normal-start", []string{"--help"}, "no-open=true;default=true"},
		{"existing-watcher-flag", []string{"--no-open", "--help"}, "no-open=true;default=true"},
		{"explicit-dashboard", []string{"--no-open=false", "--help"}, "no-open=false;default=true"},
		{"explicit-silent-start", []string{"--no-open=true", "--help"}, "no-open=true;default=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(exe, tc.args...)
			cmd.Env = append(os.Environ(), "OBS_DISPLAY_OVERLAY_HELPER=startup-flags")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			output, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(output)) != tc.want {
				t.Fatalf("startup flags mismatch: err=%v output=%q want=%q", err, output, tc.want)
			}
		})
	}
}
