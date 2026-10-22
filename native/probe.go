package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type ProbeRequest struct {
	Binary     string `json:"binary"`
	Home       string `json:"home"`
	Model      string `json:"model"`
	Effort     string `json:"effort"`
	Tier       string `json:"tier"`
	SessionKey string `json:"session_key"`
	Confirm    bool   `json:"confirm"`
}
type ProbeResponse struct {
	ID            string   `json:"id"`
	Model         string   `json:"model,omitempty"`
	Models        []string `json:"models"`
	Status        string   `json:"status,omitempty"`
	Tier          string   `json:"service_tier,omitempty"`
	Previous      string   `json:"previous_response_id,omitempty"`
	CreatedAt     int64    `json:"created_at,omitempty"`
	ErrorCategory string   `json:"error_category,omitempty"`
	Usage         *Usage   `json:"usage,omitempty"`
	FrameHashes   []string `json:"frame_sha256"`
}
type Probe struct {
	ID            string          `json:"id"`
	Version       string          `json:"tool_version"`
	StartedAt     string          `json:"started_at"`
	FinishedAt    string          `json:"finished_at,omitempty"`
	Scope         string          `json:"scope"`
	Request       ProbeRequest    `json:"request"`
	BinaryVersion string          `json:"binary_version,omitempty"`
	BinarySHA256  string          `json:"binary_sha256,omitempty"`
	BannerModel   string          `json:"banner_model,omitempty"`
	Status        string          `json:"status"`
	Conclusion    string          `json:"conclusion"`
	Note          string          `json:"note"`
	Responses     []ProbeResponse `json:"responses"`
	Errors        []string        `json:"errors"`
	Frames        int             `json:"frames"`
	Unparsed      int             `json:"unparsed"`
	SawPong       bool            `json:"saw_pong"`
	ExitCode      *int            `json:"exit_code,omitempty"`
	DurationMS    int64           `json:"duration_ms"`
}

var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+-]{0,159}$`)
var tracePattern = regexp.MustCompile(`tungstenite::protocol:\s+Received message\s+(\{.*)$`)
var bannerPattern = regexp.MustCompile(`^model:\s*(\S+)\s*$`)

type probeCapture struct {
	mu   sync.Mutex
	p    *Probe
	byID map[string]int
}

func (c *probeCapture) feed(line string, stdout bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if stdout {
		if strings.TrimSpace(line) == "pong" {
			c.p.SawPong = true
		}
		return
	}
	if m := bannerPattern.FindStringSubmatch(strings.TrimSpace(line)); len(m) > 1 {
		c.p.BannerModel = limitText(m[1], 160)
	}
	m := tracePattern.FindStringSubmatch(line)
	if len(m) < 2 {
		return
	}
	frame, e := decodeObj([]byte(m[1]))
	if e != nil {
		c.p.Unparsed++
		return
	}
	c.p.Frames++
	typ := str(frame["type"])
	if typ == "error" {
		cat := classifyError(errorCode(frame) + " " + str(obj(frame["error"])["message"]) + " " + str(frame["message"]))
		c.p.Errors = appendUnique(c.p.Errors, cat)
		return
	}
	if typ != "response.created" && typ != "response.completed" && typ != "response.failed" && typ != "response.incomplete" && typ != "response.cancelled" {
		return
	}
	r := obj(frame["response"])
	id := ident(r["id"])
	if id == "" {
		return
	}
	idx, ok := c.byID[id]
	if !ok {
		idx = len(c.p.Responses)
		c.byID[id] = idx
		c.p.Responses = append(c.p.Responses, ProbeResponse{ID: id, Models: []string{}, FrameHashes: []string{}})
	}
	rec := &c.p.Responses[idx]
	if v := ident(r["model"]); v != "" {
		rec.Model = v
		rec.Models = appendUnique(rec.Models, v)
	}
	rec.Status = first(ident(r["status"]), rec.Status)
	if typ == "response.completed" {
		rec.Status = "completed"
	}
	if typ == "response.failed" {
		rec.Status = "failed"
	}
	if typ == "response.incomplete" {
		rec.Status = "incomplete"
	}
	if typ == "response.cancelled" {
		rec.Status = "cancelled"
	}
	rec.Tier = first(ident(r["service_tier"]), rec.Tier)
	rec.Previous = first(ident(r["previous_response_id"]), rec.Previous)
	if n := num(r["created_at"]); n > 0 {
		rec.CreatedAt = n
	}
	if u := usage(r["usage"]); u != nil {
		detail := obj(obj(r["usage"])["input_tokens_details"])
		if u.Cached == 0 {
			u.Cached = num(detail["cached_tokens"])
		}
		detail = obj(obj(r["usage"])["output_tokens_details"])
		if u.Reasoning == 0 {
			u.Reasoning = num(detail["reasoning_tokens"])
		}
		rec.Usage = u
	}
	if er := obj(r["error"]); len(er) > 0 {
		rec.ErrorCategory = classifyError(ident(er["code"]) + " " + str(er["message"]))
		c.p.Errors = appendUnique(c.p.Errors, rec.ErrorCategory)
	}
	if len(rec.FrameHashes) < 16 {
		rec.FrameHashes = appendUnique(rec.FrameHashes, hashBytes([]byte(m[1])))
	}
}
func appendUnique(a []string, s string) []string {
	for _, x := range a {
		if x == s {
			return a
		}
	}
	return append(a, s)
}
func probeConclusion(p *Probe) string {
	for _, e := range p.Errors {
		if e == "UNSUPPORTED" {
			return "UNSUPPORTED"
		}
	}
	if p.BannerModel != "" && !strings.EqualFold(p.BannerModel, p.Request.Model) {
		return "INCOMPARABLE"
	}
	different := false
	variant := false
	allComplete := len(p.Responses) > 0
	modelsKnown := true
	chained := false
	for _, r := range p.Responses {
		if r.Status != "completed" {
			allComplete = false
		}
		if len(r.Models) == 0 {
			modelsKnown = false
		}
		if r.Previous != "" {
			chained = true
		}
		for _, m := range r.Models {
			if strings.EqualFold(m, p.Request.Model) {
				continue
			}
			rx := regexp.MustCompile("(?i)^" + regexp.QuoteMeta(p.Request.Model) + `-\d{4}-\d{2}-\d{2}$`)
			if rx.MatchString(m) {
				variant = true
			} else {
				different = true
			}
		}
	}
	// A difference is an observation about sampled response labels, not proof about a Desktop turn.
	if different {
		return "SERVER_LABEL_DIFFERENCE"
	}
	if variant {
		return "SERVER_LABEL_VARIANT"
	}
	if p.Status == "CANCELLED" || p.Status == "TIMEOUT" || p.Status == "INTERRUPTED" {
		return "UNOBSERVED"
	}
	if len(p.Errors) > 0 || p.Status == "ERROR" {
		return "PROBE_ERROR"
	}
	if allComplete && modelsKnown && p.SawPong && (chained || len(p.Responses) >= 2) && p.ExitCode != nil && *p.ExitCode == 0 {
		return "SERVER_LABEL_MATCH"
	}
	return "UNOBSERVED"
}
func discoverBinaries() []Candidate {
	out := []Candidate{}
	seen := map[string]bool{}
	add := func(p, k string) {
		if p == "" {
			return
		}
		abs, e := filepath.Abs(p)
		if e != nil {
			return
		}
		s, e := os.Stat(abs)
		if e == nil && !s.IsDir() && !seen[canonical(abs)] {
			if isWindows() && !strings.EqualFold(filepath.Ext(abs), ".exe") {
				return
			}
			seen[canonical(abs)] = true
			out = append(out, Candidate{abs, k})
		}
	}
	add(os.Getenv("CODEX_BIN"), "CODEX_BIN")
	base := os.Getenv("LOCALAPPDATA")
	if base != "" {
		paths, _ := filepath.Glob(filepath.Join(base, "OpenAI", "Codex", "bin", "*", "codex.exe"))
		sort.Slice(paths, func(i, j int) bool {
			a, ae := os.Stat(paths[i])
			b, be := os.Stat(paths[j])
			return ae == nil && be == nil && a.ModTime().After(b.ModTime())
		})
		for _, p := range paths {
			add(p, "Desktop bundle candidate")
		}
	}
	if p, e := exec.LookPath("codex.exe"); e == nil {
		add(p, "PATH native binary")
	}
	if p, e := exec.LookPath("codex"); e == nil {
		add(p, "PATH binary")
	}
	appdata := os.Getenv("APPDATA")
	if appdata != "" {
		for _, pat := range []string{filepath.Join(appdata, "npm", "node_modules", "@openai", "codex", "vendor", "*", "codex", "codex.exe"), filepath.Join(appdata, "npm", "node_modules", "@openai", "codex-*", "vendor", "*", "codex", "codex.exe")} {
			paths, _ := filepath.Glob(pat)
			for _, p := range paths {
				add(p, "npm native binary candidate")
			}
		}
	}
	return out
}
func (a *App) startProbe(r ProbeRequest) (*Probe, error) {
	if !r.Confirm {
		return nil, fmt.Errorf("explicit confirmation required")
	}
	if a.demo || a.preview {
		return nil, fmt.Errorf("synthetic preview mode cannot start a real probe")
	}
	if !modelPattern.MatchString(r.Model) || !modelPattern.MatchString(r.Effort) {
		return nil, fmt.Errorf("enter the exact model and effort; no automatic downgrade is allowed")
	}
	if r.Tier != "" && !modelPattern.MatchString(r.Tier) {
		return nil, fmt.Errorf("invalid service tier")
	}
	if r.Binary == "" {
		return nil, fmt.Errorf("select the installed native codex.exe")
	}
	abs, e := filepath.Abs(r.Binary)
	if e != nil {
		return nil, e
	}
	r.Binary = abs
	if isWindows() && !strings.EqualFold(filepath.Ext(r.Binary), ".exe") {
		return nil, fmt.Errorf("a native .exe is required, not a .cmd wrapper")
	}
	st, e := os.Stat(r.Binary)
	if e != nil || st.IsDir() {
		return nil, fmt.Errorf("Codex executable does not exist")
	}
	if !isDir(r.Home) {
		return nil, fmt.Errorf("Codex home does not exist")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.probeRunning {
		return nil, fmt.Errorf("a probe is already running")
	}
	if !a.rootEnabled(r.Home) {
		return nil, fmt.Errorf("probe home must be one of the configured observed roots")
	}
	p := &Probe{ID: randomID(), Version: Version, StartedAt: now(), Scope: "INDEPENDENT_NEW_CODEX_EXEC_NOT_DESKTOP_TURN", Request: r, Status: "RUNNING", Conclusion: "UNOBSERVED", Responses: []ProbeResponse{}, Errors: []string{}, Note: "Only manually requested, new-session trace sampling. No behavior fingerprint. Existing Codex configuration and integrations may load. No automatic retries."}
	a.probes = append([]*Probe{p}, a.probes...)
	a.probeRunning = true
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	a.probeCancel = cancel
	if e := saveJSON(filepath.Join(a.data, "probes", p.ID+".json"), p); e != nil {
		a.probeRunning = false
		a.probeCancel = nil
		cancel()
		return nil, e
	}
	// Return a detached value: the worker owns the mutable probe until final commit.
	initial := *p
	go a.runProbe(ctx, cancel, p)
	return &initial, nil
}
func (a *App) runProbe(ctx context.Context, cancel context.CancelFunc, original *Probe) {
	start := time.Now()
	p := *original
	p.Responses = []ProbeResponse{}
	p.Errors = []string{}
	defer func() {
		cancel()
		p.FinishedAt = now()
		p.DurationMS = time.Since(start).Milliseconds()
		p.Conclusion = probeConclusion(&p)
		a.mu.Lock()
		defer a.mu.Unlock()
		*original = p
		a.probeRunning = false
		a.probeCancel = nil
		if e := saveJSON(filepath.Join(a.data, "probes", p.ID+".json"), &p); e != nil {
			original.Note += " Report write failed."
		}
	}()
	if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("CODEX_API_KEY") != "" {
		p.Status = "ERROR"
		p.Errors = append(p.Errors, "API_KEY_ENV_PRESENT")
		p.Note = "API-key environment variable detected. Stopped to avoid accidental API billing. Passive monitoring remains available."
		return
	}
	localEnv := setEnv(os.Environ(), "CODEX_HOME", p.Request.Home)
	for _, arg := range []string{"version", "help"} {
		var cmd *exec.Cmd
		if arg == "version" {
			cmd = exec.Command(p.Request.Binary, "--version")
		} else {
			cmd = exec.Command(p.Request.Binary, "exec", "--help")
		}
		cmd.Env = localEnv
		pc, c := context.WithTimeout(ctx, 12*time.Second)
		out, err := runSmall(pc, cmd, 256*1024)
		c()
		if err != nil {
			p.Status = "ERROR"
			p.Errors = append(p.Errors, "LOCAL_PREFLIGHT_FAILED")
			p.Note = "Codex --version/exec --help failed. No model request was intentionally started."
			return
		}
		if arg == "version" {
			p.BinaryVersion = limitText(strings.TrimSpace(out), 180)
		} else if !strings.Contains(out, "--ephemeral") {
			p.Status = "ERROR"
			p.Errors = append(p.Errors, "EPHEMERAL_UNSUPPORTED")
			p.Note = "This Codex does not advertise --ephemeral. Stopped without silently creating a saved probe session."
			return
		}
	}
	login := exec.Command(p.Request.Binary, "login", "status")
	login.Env = localEnv
	lc, lcc := context.WithTimeout(ctx, 12*time.Second)
	loginOut, loginErr := runSmall(lc, login, 16*1024)
	lcc()
	if loginErr != nil || !strings.Contains(strings.ToLower(loginOut), "chatgpt") {
		p.Status = "ERROR"
		p.Errors = append(p.Errors, "CHATGPT_LOGIN_UNCONFIRMED")
		p.Note = "Could not confirm ChatGPT login via codex login status. No model request was intentionally started; API-key mode is not supported by this build."
		return
	}
	bf, e := os.Open(p.Request.Binary)
	if e == nil {
		h := sha256.New()
		_, _ = io.Copy(h, bf)
		_ = bf.Close()
		p.BinarySHA256 = hex.EncodeToString(h.Sum(nil))
	}
	if ctx.Err() != nil {
		p.Status = "CANCELLED"
		return
	}
	work := filepath.Join(a.data, "probe-work", p.ID)
	if e := os.MkdirAll(work, 0700); e != nil {
		p.Status = "ERROR"
		p.Errors = append(p.Errors, "WORKDIR_FAILED")
		return
	}
	defer os.RemoveAll(work)
	q := func(s string) string { return string(jsonBytes(s)) }
	args := []string{"exec", "--ephemeral", "-s", "read-only", "--skip-git-repo-check", "--color", "never", "-c", "model=" + q(p.Request.Model), "-c", "model_reasoning_effort=" + q(p.Request.Effort), "-c", "notify=[]", "-c", `approval_policy="never"`}
	if p.Request.Tier != "" {
		args = append(args, "-c", "service_tier="+q(p.Request.Tier))
	}
	args = append(args, "Do not use any tools, files, network, plugins, or subagents. Reply with exactly the single word: pong")
	cmd := exec.Command(p.Request.Binary, args...)
	cmd.Dir = work
	cmd.Env = setEnv(localEnv, "RUST_LOG", "tungstenite::protocol=trace,tungstenite::protocol::frame=off")
	cmd.Env = setEnv(cmd.Env, "RUST_LOG_STYLE", "never")
	cap := &probeCapture{p: &p, byID: map[string]int{}}
	err := runStreaming(ctx, cmd, func(s string, out bool) { cap.feed(s, out) })
	if cmd.ProcessState != nil {
		n := cmd.ProcessState.ExitCode()
		p.ExitCode = &n
	}
	if ctx.Err() == context.DeadlineExceeded {
		p.Status = "TIMEOUT"
	} else if ctx.Err() != nil {
		p.Status = "CANCELLED"
	} else if err != nil {
		p.Status = "ERROR"
		p.Errors = appendUnique(p.Errors, "CODEX_PROCESS_FAILED")
	} else {
		p.Status = "COMPLETED"
	}
	if p.Frames == 0 {
		p.Note += " No supported received WebSocket trace frames were observed; this is not a model verdict. No proxy or certificate fallback was attempted."
	}
}
func setEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, s := range env {
		if k, _, ok := strings.Cut(s, "="); ok && strings.EqualFold(k, key) {
			continue
		}
		out = append(out, s)
	}
	return append(out, key+"="+value)
}
func runSmall(ctx context.Context, cmd *exec.Cmd, limit int) (string, error) {
	var mu sync.Mutex
	var b strings.Builder
	e := runStreaming(ctx, cmd, func(s string, _ bool) {
		mu.Lock()
		defer mu.Unlock()
		if b.Len() < limit {
			left := limit - b.Len()
			if len(s) > left {
				s = s[:left]
			}
			b.WriteString(s)
			b.WriteByte('\n')
		}
	})
	return b.String(), e
}
func runStreaming(ctx context.Context, cmd *exec.Cmd, feed func(string, bool)) error {
	configureChild(cmd)
	out, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	errpipe, e := cmd.StderrPipe()
	if e != nil {
		return e
	}
	cmd.Stdin = nil
	if e = cmd.Start(); e != nil {
		return e
	}
	kill, cleanup, e := guardChild(cmd)
	if e != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("cannot guard child process tree: %w", e)
	}
	defer cleanup()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			kill()
		case <-done:
		}
	}()
	var wg sync.WaitGroup
	wg.Add(2)
	read := func(r io.Reader, stdout bool) {
		defer wg.Done()
		br := bufio.NewReaderSize(r, 64*1024)
		for {
			line, n, complete, over, er := readRecord(br)
			if !over && n > 0 {
				feed(strings.TrimRight(string(line), "\r\n"), stdout)
			}
			if !complete || er != nil {
				break
			}
		}
	}
	go read(out, true)
	go read(errpipe, false)
	wg.Wait()
	e = cmd.Wait()
	close(done)
	return e
}
