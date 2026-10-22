package main

import "testing"

func TestParseLaunchArgsCommandOrderIndependent(t *testing.T) {
	a := parseLaunchArgs([]string{"launcher.exe", "--data", `C:\tmp\d`, "--monitor-only"})
	b := parseLaunchArgs([]string{"launcher.exe", "--monitor-only", "--data", `C:\tmp\d`})
	if a.Command != cmdMonitorOnly || b.Command != cmdMonitorOnly {
		t.Fatalf("commands: %q / %q", a.Command, b.Command)
	}
	if a.Data != b.Data || !a.HasData || !b.HasData {
		t.Fatalf("data: %q / %q", a.Data, b.Data)
	}
	if err := a.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestParseLaunchArgsRejectsMissingValues(t *testing.T) {
	for _, args := range [][]string{
		{"launcher.exe", "--data"},
		{"launcher.exe", "--home"},
		{"launcher.exe", "--watch", "--data"},
	} {
		a := parseLaunchArgs(args)
		if err := a.validate(); err == nil {
			t.Fatalf("args %v must fail closed", args)
		}
	}
}

func TestParseLaunchArgsRejectsMutuallyExclusiveCommands(t *testing.T) {
	a := parseLaunchArgs([]string{"launcher.exe", "--watch", "--health-check"})
	if err := a.validate(); err == nil {
		t.Fatal("two commands must fail closed, not silently pick one")
	}
	a = parseLaunchArgs([]string{"launcher.exe", "--monitor-only", "--disable-follow"})
	if err := a.validate(); err == nil {
		t.Fatal("two commands must fail closed")
	}
}

func TestParseLaunchArgsRejectsUnknownAndRelativePaths(t *testing.T) {
	if err := parseLaunchArgs([]string{"launcher.exe", "--nope"}).validate(); err == nil {
		t.Fatal("unknown flag must fail closed")
	}
	if err := parseLaunchArgs([]string{"launcher.exe", "--data", "relative\\dir"}).validate(); err == nil {
		t.Fatal("relative --data must fail closed")
	}
	if err := parseLaunchArgs([]string{"launcher.exe", "--home", "relative"}).validate(); err == nil {
		t.Fatal("relative --home must fail closed")
	}
}

func TestParseLaunchArgsAcceptsEveryCommandAlone(t *testing.T) {
	for _, tc := range []struct{ flag, want string }{
		{"--watch", cmdWatch},
		{"--health-check", cmdHealth},
		{"--repair-stale-native-home", cmdRepair},
		{"--monitor-only", cmdMonitorOnly},
		{"--enable-follow", cmdEnable},
		{"--disable-follow", cmdDisable},
	} {
		a := parseLaunchArgs([]string{"launcher.exe", tc.flag, "--data", `C:\tmp\d`})
		if err := a.validate(); err != nil {
			t.Fatalf("%s: %v", tc.flag, err)
		}
		if a.Command != tc.want {
			t.Fatalf("%s: command=%q want %q", tc.flag, a.Command, tc.want)
		}
	}
}
