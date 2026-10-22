package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) (*App, string) {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "codex-home")
	data := filepath.Join(base, "obs-data")
	for _, p := range []string{filepath.Join(home, "sessions"), filepath.Join(home, "archived_sessions"), filepath.Join(data, "sources"), filepath.Join(data, "history"), filepath.Join(data, "probes")} {
		if e := os.MkdirAll(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := &App{data: data, cfg: Config{Roots: []string{home}, PollSeconds: 3, Aliases: map[string]string{}}, files: map[string]*FileState{}, ctxDone: ctx.Done(), stop: cancel, scanKick: make(chan struct{}, 1), started: time.Now(), token: "test-secret", address: "http://127.0.0.1:12345", probes: []*Probe{}, marks: []Mark{}}
	return a, home
}
func writeFixture(t *testing.T, home, name string, records ...[]byte) string {
	t.Helper()
	p := filepath.Join(home, "sessions", name)
	var b bytes.Buffer
	for _, r := range records {
		b.Write(r)
		b.WriteByte('\n')
	}
	if e := os.WriteFile(p, b.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func oneState(t *testing.T, a *App) *FileState {
	t.Helper()
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.files) != 1 {
		t.Fatalf("expected one file, got %d", len(a.files))
	}
	for _, f := range a.files {
		return cloneState(f)
	}
	return nil
}

// A stable timestamp within this test process keeps duplicate records identical
// while keeping ordinary fixtures inside the actual five-day retention window.
var testRecordAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)

func rec(typ string, p Obj) []byte { return fixtureRecord(typ, p, testRecordAt) }
func TestEmbeddedSelfCheck(t *testing.T) {
	r := runSelfCheck()
	if r["passed"] != r["total"] {
		t.Fatalf("%s", jsonBytes(r))
	}
}
func TestModelChangeInvalidatesWindow(t *testing.T) {
	f := newState("x", "x", "x")
	parseLine(f, rec("turn_context", Obj{"model": "a", "effort": "high"}), 0)
	parseLine(f, rec("event_msg", fixtureToken(2000, 4000)), 1)
	parseLine(f, rec("turn_context", Obj{"model": "b", "effort": "ultra"}), 2)
	if f.Summary.ContextValid || f.Summary.ContextWindow != 0 {
		t.Fatal("old model window retained")
	}
}
func TestCompactionDuplicateSnapshotStaysInvalid(t *testing.T) {
	f := newState("x", "x", "x")
	b := rec("event_msg", fixtureToken(2000, 4000))
	parseLine(f, b, 0)
	parseLine(f, rec("compacted", Obj{}), 1)
	parseLine(f, b, 2)
	if f.Summary.ContextValid {
		t.Fatal("replayed snapshot revalidated compacted context")
	}
}
func TestInheritedSettingsOwnerIsNotCurrentThread(t *testing.T) {
	f := newState("x", "x", "x")
	parseLine(f, rec("session_meta", Obj{"id": "current"}), 0)
	parseLine(f, rec("turn_context", Obj{"model": "correct", "effort": "ultra"}), 1)
	parseLine(f, rec("event_msg", Obj{"type": "thread_settings_applied", "thread_id": "parent", "thread_settings": Obj{"model": "wrong", "reasoning_effort": "low"}}), 2)
	if f.Summary.Model != "correct" {
		t.Fatal("foreign settings overwrote current thread")
	}
}
func TestIncrementalPartialLineAndNoDoubleCount(t *testing.T) {
	a, home := testApp(t)
	b := rec("event_msg", fixtureToken(1200, 2000))
	p := writeFixture(t, home, "one.jsonl", rec("turn_context", Obj{"model": "a", "effort": "ultra"}))
	f, e := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = f.Write(b[:len(b)/2])
	_ = f.Close()
	a.scanOnce()
	s := oneState(t, a)
	if s.Summary.Latest != nil || !s.WaitingPartial {
		t.Fatal("partial line consumed")
	}
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.Write(append(b[len(b)/2:], '\n'))
	_ = f.Close()
	a.scanOnce()
	s = oneState(t, a)
	if s.Summary.Total.Total != 2000 || !s.Summary.CaughtUp {
		t.Fatal("appended line lost")
	}
	count := len(s.Recent)
	a.scanOnce()
	if len(oneState(t, a).Recent) != count {
		t.Fatal("idle scan duplicated events")
	}
}
func TestArchiveMoveKeepsIdentity(t *testing.T) {
	a, home := testApp(t)
	p := writeFixture(t, home, "move.jsonl", rec("event_msg", fixtureToken(1000, 2000)))
	a.scanOnce()
	before := oneState(t, a)
	if e := os.Rename(p, filepath.Join(home, "archived_sessions", "move.jsonl")); e != nil {
		t.Fatal(e)
	}
	a.scanOnce()
	after := oneState(t, a)
	if after.Summary.Key != before.Summary.Key || !after.Summary.Archived || after.Summary.Total.Total != 2000 || len(after.Recent) != len(before.Recent) {
		t.Fatal("archive move created duplicates")
	}
}
func TestTruncateStartsNewGeneration(t *testing.T) {
	a, home := testApp(t)
	p := writeFixture(t, home, "reset.jsonl", rec("turn_context", Obj{"model": "long-model-name", "effort": "ultra"}), rec("event_msg", fixtureToken(1000, 2000)))
	a.scanOnce()
	b := append(rec("turn_context", Obj{"model": "b", "effort": "low"}), '\n')
	if e := os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	a.scanOnce()
	f := oneState(t, a)
	if f.Generation != 1 || f.Summary.Model != "b" || f.Summary.Total != nil {
		t.Fatal("truncation not handled")
	}
	h, _, e := a.history(f.Summary.Key, 1200)
	if e != nil || len(h) < 4 {
		t.Fatal("historical generation missing")
	}
}
func TestSameSizeRewriteDetected(t *testing.T) {
	a, home := testApp(t)
	p := writeFixture(t, home, "rewrite.jsonl", rec("turn_context", Obj{"model": "aaa", "effort": "high"}))
	a.scanOnce()
	before := oneState(t, a)
	b, _ := os.ReadFile(p)
	b = bytes.ReplaceAll(b, []byte("aaa"), []byte("bbb"))
	_ = os.WriteFile(p, b, 0600)
	tm := time.Unix(0, before.LastMod+1000000)
	_ = os.Chtimes(p, tm, tm)
	a.scanOnce()
	f := oneState(t, a)
	if f.Summary.Model != "bbb" || f.Generation != 1 {
		t.Fatal("same-size change missed")
	}
}
func TestJournalWriteFailureDoesNotAdvance(t *testing.T) {
	a, home := testApp(t)
	p := writeFixture(t, home, "writefail.jsonl", rec("event_msg", fixtureToken(1000, 2000)))
	key := shortHash(canonical(home) + "\x00" + filepath.Base(p))
	bad := filepath.Join(a.data, "history", key+".jsonl")
	_ = os.Mkdir(bad, 0700)
	a.scanOnce()
	f := oneState(t, a)
	if f.Offset != 0 || f.Summary.ReadError == "" {
		t.Fatal("failed persistence advanced offset")
	}
	_ = os.Remove(bad)
	a.scanOnce()
	f = oneState(t, a)
	if f.Summary.Total == nil || f.Summary.Total.Total != 2000 {
		t.Fatal("retry lost telemetry")
	}
}
func TestTornJournalTailRepair(t *testing.T) {
	p := filepath.Join(t.TempDir(), "journal.jsonl")
	_ = os.WriteFile(p, []byte("{\"ok\":true}\n{\"broken\":"), 0600)
	if e := repairJournalTail(p); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "{\"ok\":true}\n" {
		t.Fatalf("unexpected repaired bytes: %q", b)
	}
}
func TestJournalReplayDeduplication(t *testing.T) {
	a, home := testApp(t)
	writeFixture(t, home, "dupe.jsonl", rec("event_msg", fixtureToken(1000, 2000)))
	a.scanOnce()
	f := oneState(t, a)
	p := filepath.Join(a.data, "history", f.Summary.Key+".jsonl")
	b, _ := os.ReadFile(p)
	file, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = file.Write(b)
	_ = file.Close()
	h, _, e := a.history(f.Summary.Key, 1200)
	if e != nil || len(h) != len(f.Recent) {
		t.Fatal("replayed events not deduplicated")
	}
}
func TestLargeFileBeyond50MiB(t *testing.T) {
	if testing.Short() {
		t.Skip("large-file fixture")
	}
	a, home := testApp(t)
	p := filepath.Join(home, "sessions", "large.jsonl")
	file, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	payload := strings.Repeat("x", 1024*1024)
	line := append(rec("response_item", Obj{"type": "message", "role": "assistant", "content": payload}), '\n')
	for i := 0; i < 53; i++ {
		_, _ = file.Write(line)
	}
	_, _ = file.Write(append(rec("event_msg", fixtureToken(1234, 5678)), '\n'))
	_ = file.Close()
	for i := 0; i < 30; i++ {
		a.scanOnce()
		f := oneState(t, a)
		if f.Summary.CaughtUp {
			break
		}
	}
	f := oneState(t, a)
	if !f.Summary.CaughtUp || f.Summary.Total == nil || f.Summary.Total.Total != 5678 {
		t.Fatal("large file silently skipped or not fully read")
	}
}
func TestOversizeRecordReportedNotCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("oversize fixture")
	}
	a, home := testApp(t)
	p := filepath.Join(home, "sessions", "oversize.jsonl")
	file, _ := os.Create(p)
	_, _ = file.WriteString(strings.Repeat("x", maxLineBytes+16) + "\n")
	_, _ = file.Write(append(rec("event_msg", fixtureToken(100, 200)), '\n'))
	_ = file.Close()
	for i := 0; i < 4; i++ {
		a.scanOnce()
	}
	f := oneState(t, a)
	if f.Summary.OversizeLines != 1 || f.Summary.Total == nil {
		t.Fatal("oversize record wasn't reported and skipped safely")
	}
}
func request(a *App, method, path, body, token, origin, host string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, a.address+path, strings.NewReader(body))
	if host != "" {
		r.Host = host
	}
	r.Header.Set("X-Observatory-Token", token)
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	return w
}
func TestHTTPAuthenticationAndOrigin(t *testing.T) {
	a, _ := testApp(t)
	cases := []struct {
		name, token, origin, host string
		want                      int
	}{{"no token", "", "", "", 403}, {"wrong token", "wrong", "", "", 403}, {"cross origin", a.token, "https://evil.example", "", 403}, {"DNS rebinding", a.token, "", "evil.example", 403}, {"valid", a.token, a.address, "", 200}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := request(a, "GET", "/api/state", "", c.token, c.origin, c.host)
			if w.Code != c.want {
				t.Fatalf("got %d want %d", w.Code, c.want)
			}
		})
	}
}
func TestHTTPWriteRequiresPOSTAndSingleJSON(t *testing.T) {
	a, _ := testApp(t)
	if w := request(a, "GET", "/api/mark", "", a.token, "", ""); w.Code != 405 {
		t.Fatal("GET mutated state")
	}
	if w := request(a, "POST", "/api/rescan", "{}{}", a.token, "", ""); w.Code != 400 {
		t.Fatal("trailing JSON accepted")
	}
}
func TestExportIsRedactedAndHasManifest(t *testing.T) {
	a, home := testApp(t)
	writeFixture(t, home, "export.jsonl", rec("session_meta", Obj{"id": "thread", "cwd": "C:\\PRIVATE_USERNAME\\PRIVATE_PROJECT", "base_instructions": "SECRET_PROMPT"}), rec("turn_context", Obj{"model": "model-a", "effort": "ultra"}), rec("event_msg", fixtureToken(1000, 2000)))
	a.scanOnce()
	f := oneState(t, a)
	_, _ = a.addMark(f.Summary.Key, "PRIVATE_USER_NOTE")
	w := request(a, "POST", "/api/export", string(jsonBytes(Obj{"key": f.Summary.Key, "include_notes": false})), a.token, "", "")
	if w.Code != 200 {
		t.Fatalf("export: %s", w.Body.String())
	}
	zr, e := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, z := range zr.File {
		r, _ := z.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		for _, secret := range []string{"PRIVATE_USERNAME", "PRIVATE_PROJECT", "SECRET_PROMPT", "PRIVATE_USER_NOTE", home} {
			if bytes.Contains(b, []byte(secret)) {
				t.Fatalf("leaked %s in %s", secret, z.Name)
			}
		}
		if z.Name == "manifest.json" {
			found = true
		}
	}
	if !found {
		t.Fatal("no manifest")
	}
}
func TestProbeFrameWhitelistAndVerdict(t *testing.T) {
	zero := 0
	p := &Probe{Status: "COMPLETED", ExitCode: &zero, Request: ProbeRequest{Model: "model-a"}, Responses: []ProbeResponse{}, Errors: []string{}}
	c := &probeCapture{p: p, byID: map[string]int{}}
	c.feed(`2026-09-29T00:00:00Z TRACE tungstenite::protocol: Received message {"type":"response.completed","response":{"id":"r1","model":"model-a","status":"completed","output":["PRIVATE_RESPONSE"],"safety_identifier":"SECRET_ACCOUNT"}}`, false)
	c.feed(`2026-09-29T00:00:01Z TRACE tungstenite::protocol: Received message {"type":"response.completed","response":{"id":"r2","previous_response_id":"r1","model":"model-a","status":"completed"}}`, false)
	c.feed("pong", true)
	if probeConclusion(p) != "SERVER_LABEL_MATCH" {
		t.Fatalf("verdict %s", probeConclusion(p))
	}
	b := jsonBytes(p)
	if bytes.Contains(b, []byte("PRIVATE_RESPONSE")) || bytes.Contains(b, []byte("SECRET_ACCOUNT")) {
		t.Fatal("raw fields leaked")
	}
}
func TestProbeUnsupportedNotRerouteVerdict(t *testing.T) {
	p := &Probe{Errors: []string{"UNSUPPORTED"}, Request: ProbeRequest{Model: "a"}, Responses: []ProbeResponse{{ID: "r", Models: []string{"b"}}}}
	if probeConclusion(p) != "UNSUPPORTED" {
		t.Fatal("unsupported marked as substitution")
	}
}
func TestProbeBannerMismatchIsIncomparable(t *testing.T) {
	p := &Probe{BannerModel: "low-model", Request: ProbeRequest{Model: "pro-model"}}
	if probeConclusion(p) != "INCOMPARABLE" {
		t.Fatal("banner mismatch ignored")
	}
}
func TestProbeRequiresExplicitConfirmation(t *testing.T) {
	a, _ := testApp(t)
	if _, e := a.startProbe(ProbeRequest{Confirm: false}); e == nil {
		t.Fatal("unconfirmed probe accepted")
	}
}
func TestChildCancellationKillsProcessTree(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	cmd := exec.Command("/bin/sh", "-c", "sleep 50 & wait")
	start := time.Now()
	_ = runStreaming(ctx, cmd, func(string, bool) {})
	if time.Since(start) > 3*time.Second {
		t.Fatal("child process tree escaped cancellation")
	}
}
func TestEndToEndFakeProbeNoNetwork(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux fake binary")
	}
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEX_API_KEY", "")
	a, home := testApp(t)
	script := filepath.Join(t.TempDir(), "fake-codex")
	text := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "codex-cli TEST"; exit 0; fi
if [ "$2" = "--help" ]; then echo "--ephemeral -s --skip-git-repo-check"; exit 0; fi
if [ "$1" = "login" ]; then echo "Logged in using ChatGPT"; exit 0; fi
printf '%s\n' 'model: test-pro' >&2
printf '%s\n' '2026-09-29T00:00:00Z TRACE tungstenite::protocol: Received message {"type":"response.completed","response":{"id":"r1","model":"test-pro","status":"completed"}}' >&2
printf '%s\n' '2026-09-29T00:00:01Z TRACE tungstenite::protocol: Received message {"type":"response.completed","response":{"id":"r2","previous_response_id":"r1","model":"test-pro","status":"completed"}}' >&2
echo pong
`
	if e := os.WriteFile(script, []byte(text), 0700); e != nil {
		t.Fatal(e)
	}
	p, e := a.startProbe(ProbeRequest{Binary: script, Home: home, Model: "test-pro", Effort: "ultra", Confirm: true})
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 100; i++ {
		time.Sleep(20 * time.Millisecond)
		a.mu.RLock()
		done := !a.probeRunning
		a.mu.RUnlock()
		if done {
			break
		}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	got := a.probes[0]
	if got.ID != p.ID || got.Status != "COMPLETED" || got.Conclusion != "SERVER_LABEL_MATCH" {
		t.Fatalf("%s", jsonBytes(got))
	}
	if got.Request.Effort != "ultra" {
		t.Fatal("effort downgraded")
	}
}
func TestSelfCheckDoesNotNeedModels(t *testing.T) {
	r := runSelfCheck()
	if r["model_requests"] != 0 {
		t.Fatal("self-check made requests")
	}
}
func TestReadRecordNewlineOnly(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("{\"a\":\"x\u2028y\"}\n"))
	b, _, complete, _, e := readRecord(r)
	if !complete || e != nil {
		t.Fatal(e)
	}
	var v Obj
	if json.Unmarshal(b, &v) != nil {
		t.Fatal("unicode line separator split the JSON record")
	}
}

var _ = http.MethodGet
