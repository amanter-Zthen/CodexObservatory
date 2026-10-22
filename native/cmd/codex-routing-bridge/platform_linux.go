//go:build linux

package main

import (
	"os"
	"os/exec"
)

func hideChild(cmd *exec.Cmd)       {}
func replaceStatus(from, to string) { _ = os.Rename(from, to) }
