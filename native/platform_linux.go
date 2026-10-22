//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func isWindows() bool                                    { return false }
func replaceFile(from, to string) error                  { return os.Rename(from, to) }
func message(title, text string)                         { fmt.Fprintln(os.Stderr, title+"\n"+text) }
func openUI(url string) error                            { return exec.Command("xdg-open", url).Start() }
func openFolder(path string) error                       { return exec.Command("xdg-open", path).Start() }
func startPlatform(url, data string, stop func()) func() { return func() {} }
func instanceLock(data string) (func(), bool, error) {
	f, e := os.OpenFile(filepath.Join(data, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, false, e
	}
	e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if e != nil {
		_ = f.Close()
		return func() {}, true, nil
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, false, nil
}
func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}
func guardChild(cmd *exec.Cmd) (func(), func(), error) {
	kill := func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return kill, kill, nil
}
