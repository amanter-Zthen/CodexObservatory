package main

import (
	"reflect"
	"testing"
)

func TestDesktopPrefixConfigSelectsOriginalAppServerWithoutChangingArguments(t *testing.T) {
	args := []string{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled", "-c", "plugins.example.enabled=true"}
	original := append([]string{}, args...)
	if codexSubcommand(args) != "app-server" {
		t.Fatal("real Desktop command was treated as passthrough")
	}
	if !reflect.DeepEqual(args, original) {
		t.Fatal("original argv changed")
	}
}
func TestSubcommandDetectionRespectsConfigValuesAndOtherCommands(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{{[]string{"app-server"}, "app-server"}, {[]string{"--config=features.test=true", "--strict-config", "app-server"}, "app-server"}, {[]string{"-ckey=true", "--enable", "example", "app-server"}, "app-server"}, {[]string{"-c", "key=\"app-server\"", "exec", "app-server"}, "exec"}, {[]string{"exec-server", "--remote", "https://example.invalid"}, "exec-server"}, {[]string{"--", "app-server"}, ""}, {[]string{"-c"}, ""}, {[]string{"--unknown-option", "app-server"}, ""}, {[]string{"--model", "app-server", "exec"}, "exec"}}
	for _, tc := range cases {
		if got := codexSubcommand(tc.args); got != tc.want {
			t.Fatalf("%v -> %s expected %s", tc.args, got, tc.want)
		}
	}
}
