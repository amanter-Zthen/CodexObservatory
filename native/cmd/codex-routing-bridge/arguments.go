package main

import "strings"

// Read only the root options needed to locate the subcommand. Forward the
// original argv untouched, including config overrides before or after it.
func codexSubcommand(args []string) string {
	values := map[string]bool{"-c": true, "--config": true, "--enable": true, "--disable": true, "--remote": true, "--remote-auth-token-env": true, "-m": true, "--model": true, "--local-provider": true, "-p": true, "--profile": true, "-s": true, "--sandbox": true, "-a": true, "--ask-for-approval": true, "-C": true, "--cd": true, "--add-dir": true}
	switches := map[string]bool{"--strict-config": true, "--oss": true, "--approve-for-me": true, "--dangerously-bypass-approvals-and-sandbox": true, "--dangerously-bypass-hook-trust": true, "--worktree": true, "--search": true, "--no-alt-screen": true, "--no-daemon": true}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || arg == "-h" || arg == "--help" || arg == "-V" || arg == "--version" {
			return ""
		}
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
		if values[arg] {
			i++
			if i >= len(args) {
				return ""
			}
			continue
		}
		key, _, attached := strings.Cut(arg, "=")
		if attached && values[key] {
			continue
		}
		if strings.HasPrefix(arg, "-c") && !strings.HasPrefix(arg, "--") && len(arg) > 2 {
			continue
		}
		if switches[arg] {
			continue
		}
		return ""
	}
	return ""
}
