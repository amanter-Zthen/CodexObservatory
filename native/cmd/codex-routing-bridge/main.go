package main

import (
	"bufio"
	"bytes"
	"codex-observatory/internal/routetrace"
	"codex-observatory/internal/wirecapture"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type config struct {
	Native  string `json:"native_codex"`
	Data    string `json:"observatory_data"`
	Mode    string `json:"capture_mode"`
	Fixture bool   `json:"loopback_fixture"`
}
type sink struct {
	mu        sync.Mutex
	dir       string
	lastClean time.Time
	errors    uint64
}

func (s *sink) write(rows []routetrace.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastClean) > time.Minute {
		s.lastClean = time.Now()
		paths, _ := filepath.Glob(filepath.Join(s.dir, "*.json"))
		for _, path := range paths {
			info, e := os.Lstat(path)
			if e != nil || !info.Mode().IsRegular() || time.Since(info.ModTime()) <= 5*24*time.Hour {
				continue
			}
			var r routetrace.Record
			b, e := os.ReadFile(path)
			if e == nil && json.Unmarshal(b, &r) == nil && (r.Verdict != "mismatch" || r.Stage == "response.output_text.delta") {
				_ = os.Remove(path)
			}
		}
	}
	for _, row := range rows {
		b, e := json.Marshal(row)
		if e != nil {
			continue
		}
		final := filepath.Join(s.dir, row.ID+".json")
		temp := final + ".tmp"
		if e = os.WriteFile(temp, b, 0600); e == nil {
			e = os.Rename(temp, final)
		}
		if e != nil {
			s.errors++
		}
	}
}
func (s *sink) failures() uint64 { s.mu.Lock(); defer s.mu.Unlock(); return s.errors }
func envSet(env []string, key, value string) []string {
	out := []string{}
	for _, e := range env {
		if !strings.EqualFold(strings.SplitN(e, "=", 2)[0], key) {
			out = append(out, e)
		}
	}
	return append(out, key+"="+value)
}

// Forward exact byte fragments; parse only bounded copies of complete lines.
func relay(in io.Reader, out io.Writer, inspect func([]byte)) error {
	reader := bufio.NewReaderSize(in, 128*1024)
	var line []byte
	oversize := false
	for {
		fragment, e := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			if out != nil {
				if _, we := out.Write(fragment); we != nil {
					return we
				}
			}
			if !oversize {
				if len(line)+len(fragment) > 32*1024*1024 {
					oversize = true
					line = nil
				} else {
					line = append(line, fragment...)
				}
			}
		}
		if e == nil || e == io.EOF {
			if !oversize && len(line) > 0 && inspect != nil {
				inspect(bytes.TrimSpace(line))
			}
			line = nil
			oversize = false
		}
		if e == io.EOF {
			return nil
		}
		if e != nil && e != bufio.ErrBufferFull {
			return e
		}
	}
}
func main() { os.Exit(run()) }
func run() int {
	exe, e := os.Executable()
	if e != nil {
		return 1
	}
	var cfg config
	b, e := os.ReadFile(filepath.Join(filepath.Dir(exe), "bridge-config.json"))
	if e != nil || json.Unmarshal(b, &cfg) != nil || !filepath.IsAbs(cfg.Native) || !filepath.IsAbs(cfg.Data) || strings.EqualFold(cfg.Native, exe) {
		fmt.Fprintln(os.Stderr, "Codex routing bridge configuration is unavailable.")
		return 1
	}
	args := os.Args[1:]
	observe := codexSubcommand(args) == "app-server"
	command := exec.Command(cfg.Native, args...)
	hideChild(command)
	command.Env = os.Environ()
	collector := routetrace.New(0)
	var recorder *sink
	if observe {
		if !cfg.Fixture {
			launcher := filepath.Join(filepath.Dir(exe), "CodexObservedLauncher.exe")
			follow := exec.Command(launcher, "--watch")
			hideChild(follow)
			if follow.Start() == nil {
				go follow.Wait()
			}
		}
		if os.MkdirAll(filepath.Join(cfg.Data, "routing-bridge-status"), 0700) != nil {
			return 1
		}
		dir := filepath.Join(cfg.Data, "live-routing-inbox")
		if e = os.MkdirAll(dir, 0700); e != nil {
			fmt.Fprintln(os.Stderr, "Codex routing bridge cannot open its metadata directory.")
			return 1
		}
		recorder = &sink{dir: dir}
		if cfg.Mode == "local-proxy" {
			proxy, er := wirecapture.Start(collector, recorder.write, cfg.Fixture)
			if er != nil {
				fmt.Fprintln(os.Stderr, "Local routing capture could not start.")
				return 1
			}
			defer proxy.Close()
			certDir := filepath.Join(cfg.Data, "route-capture", fmt.Sprintf("%d", os.Getpid()))
			if os.MkdirAll(certDir, 0700) != nil {
				return 1
			}
			certPath := filepath.Join(certDir, "ca.pem")
			if os.WriteFile(certPath, proxy.CA, 0600) != nil {
				return 1
			}
			defer os.Remove(certDir)
			defer os.Remove(certPath)
			command.Env = envSet(command.Env, "HTTPS_PROXY", proxy.URL)
			command.Env = envSet(command.Env, "HTTP_PROXY", proxy.URL)
			command.Env = envSet(command.Env, "CODEX_CA_CERTIFICATE", certPath)
			// The upstream proxy independently honors the original bypass rules.
			command.Env = envSet(command.Env, "NO_PROXY", "")
		} else {
			trace := strings.TrimSpace(os.Getenv("RUST_LOG"))
			if trace == "" {
				trace = "info"
			}
			trace += ",tungstenite::protocol=trace,tungstenite::protocol::frame=off,codex_api::sse=trace,codex_core::session=trace"
			command.Env = envSet(command.Env, "RUST_LOG", trace)
			command.Env = envSet(command.Env, "LOG_FORMAT", "json")
		}
	}
	stdin, e := command.StdinPipe()
	if e != nil {
		return 1
	}
	stdout, e := command.StdoutPipe()
	if e != nil {
		return 1
	}
	stderr, e := command.StderrPipe()
	if e != nil {
		return 1
	}
	if e = command.Start(); e != nil {
		fmt.Fprintln(os.Stderr, "Codex routing bridge cannot start the original client.")
		return 1
	}
	collector.SetPID(command.Process.Pid)
	healthPath := filepath.Join(cfg.Data, "routing-bridge-status", fmt.Sprintf("%d.json", os.Getpid()))
	var healthMu sync.Mutex
	health := func(active bool) {
		healthMu.Lock()
		defer healthMu.Unlock()
		if !observe {
			return
		}
		data, _ := json.Marshal(map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "active": active, "client_pid": command.Process.Pid, "bridge_pid": os.Getpid(), "protocol": cfg.Mode, "model_requests_generated": 0, "metadata_write_errors": recorder.failures(), "diagnostics": collector.Stats()})
		temp := healthPath + fmt.Sprintf(".%d.tmp", os.Getpid())
		if os.WriteFile(temp, data, 0600) == nil {
			replaceStatus(temp, healthPath)
		}
	}
	health(true)
	done := make(chan struct{})
	var healthDone sync.WaitGroup
	if observe {
		healthDone.Add(1)
		go func() {
			defer healthDone.Done()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					health(true)
				}
			}
		}()
	}
	var outputDone sync.WaitGroup
	outputDone.Add(2)
	go func() {
		_ = relay(os.Stdin, stdin, func(line []byte) {
			if observe {
				recorder.write(collector.FeedIPC(line, true))
			}
		})
		_ = stdin.Close()
	}()
	go func() {
		defer outputDone.Done()
		_ = relay(stdout, os.Stdout, func(line []byte) {
			if observe {
				recorder.write(collector.FeedIPC(line, false))
			}
		})
	}()
	go func() {
		defer outputDone.Done()
		if !observe {
			_, _ = io.Copy(os.Stderr, stderr)
			return
		}
		_ = relay(stderr, nil, func(line []byte) {
			rows, consumed := collector.FeedTrace(line)
			recorder.write(rows)
			if !consumed {
				_, _ = os.Stderr.Write(append(append([]byte{}, line...), '\n'))
			}
		})
	}()
	outputDone.Wait()
	e = command.Wait()
	close(done)
	healthDone.Wait()
	health(false)
	if e != nil {
		if exit, ok := e.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		return 1
	}
	return 0
}
