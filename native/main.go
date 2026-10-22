package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func jsonDecode(b []byte, v any) error { return json.Unmarshal(b, v) }
func main() {
	noOpen := flag.Bool("no-open", false, "Do not open the local dashboard")
	data := flag.String("data", "", "Application data directory (not a Codex directory)")
	home := flag.String("home", "", "Read-only Codex home to observe")
	demo := flag.Bool("demo", false, "Use isolated synthetic fixture data")
	self := flag.Bool("self-test", false, "Offline checks only; no Codex requests")
	version := flag.Bool("version", false, "Show application version")
	uiFixture := flag.String("floating-fixture", "", "Save a synthetic native UI image and exit")
	uiPreview := flag.Bool("ui-preview", false, "Use isolated synthetic animation preview")
	inspectWindow := flag.Bool("inspect-window", false, "Expose a regular isolated synthetic window for local UI acceptance")
	visualBench := flag.String("visual-benchmark", "", "Write isolated Windows renderer benchmark JSON; no model or network requests")
	quality := flag.String("visual-quality", "auto", "Visual quality: auto, full, economy, still (display only)")
	flag.Parse()
	switch *quality {
	case "auto", "full", "economy", "still":
	default:
		fmt.Fprintln(os.Stderr, "invalid --visual-quality")
		os.Exit(2)
	}
	if *visualBench != "" {
		if err := runVisualBenchmark(*visualBench); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *uiFixture != "" {
		if err := renderFloatingFixture(*uiFixture); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if *version {
		fmt.Println("Codex Observatory", Version)
		return
	}
	if *data == "" {
		base, e := os.UserCacheDir()
		if e != nil {
			base = os.TempDir()
		}
		*data = filepath.Join(base, "CodexObservatory")
		if *demo {
			*data += "-Demo"
		}
		if *uiPreview {
			*data += "-UIPreview"
		}
	}
	absolute, e := filepath.Abs(*data)
	if e != nil {
		fatal(e)
		return
	}
	*data = absolute
	// Even a mistakenly supplied production --data cannot be pruned or
	// connected to the live feed by the visual preview.
	if *uiPreview {
		*data = filepath.Join(*data, "nacre-preview-only")
	}
	if e = os.MkdirAll(*data, 0700); e != nil {
		fatal(e)
		return
	}
	if *self {
		report := runSelfCheck()
		path := filepath.Join(*data, "self-check.json")
		_ = saveJSON(path, report)
		if !*noOpen {
			message("Codex Observatory · 离线自检", fmt.Sprintf("通过 %v / %v\n报告：%s\n没有启动 Codex 或发送模型请求。", report["passed"], report["total"], path))
		}
		return
	}
	release, already, err := instanceLock(*data)
	if err != nil {
		fatal(err)
		return
	}
	if already {
		showFloating(*data)
		var rec map[string]string
		if !*noOpen && loadJSON(filepath.Join(*data, "instance.json"), &rec) == nil {
			_ = openUI(rec["url"])
		}
		return
	}
	defer release()
	logfile, e := os.OpenFile(filepath.Join(*data, "application.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if e == nil {
		defer logfile.Close()
		log.SetOutput(logfile)
	}
	log.Printf("start version=%s platform=%s/%s", Version, runtime.GOOS, runtime.GOARCH)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &App{data: *data, started: time.Now(), token: randomID() + randomID(), ctxDone: ctx.Done(), stop: cancel, scanKick: make(chan struct{}, 1), demo: *demo}
	a.preview = *uiPreview
	a.inspectWindow = *inspectWindow && a.preview
	a.visualQuality = *quality
	if e = a.initStore(); e != nil {
		fatal(e)
		return
	}
	if *home != "" {
		p, e := filepath.Abs(*home)
		if e != nil {
			fatal(e)
			return
		}
		a.cfg.Roots = []string{p}
	}
	if *demo {
		p := filepath.Join(*data, "demo-home")
		if e = writeDemo(p); e != nil {
			fatal(e)
			return
		}
		a.cfg.Roots = []string{p}
	}
	if *uiPreview {
		a.cfg.Roots = nil
	}
	for _, root := range a.cfg.Roots {
		rel, er := filepath.Rel(root, a.data)
		if er == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			fatal(fmt.Errorf("application data must not be inside an observed Codex directory"))
			return
		}
	}
	if e = a.pruneRetention(time.Now()); e != nil {
		log.Printf("retention cleanup: %v", e)
	}
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		fatal(e)
		return
	}
	a.address = "http://" + l.Addr().String()
	url := a.address + "/#" + a.token
	if e = saveJSON(filepath.Join(a.data, "instance.json"), map[string]string{"url": url, "version": Version}); e != nil {
		_ = l.Close()
		fatal(e)
		return
	}
	srv := &http.Server{Handler: a.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024}
	go func() {
		if er := srv.Serve(l); er != nil && er != http.ErrServerClosed {
			log.Printf("http: %v", er)
			cancel()
		}
	}()
	scanDone := make(chan struct{})
	completionDone := make(chan struct{})
	feedDone := make(chan struct{})
	if a.preview {
		close(scanDone)
		close(completionDone)
		close(feedDone)
	} else {
		go func() { defer close(scanDone); a.scanLoop() }()
		go a.routeLoop()
		go func() { defer close(feedDone); a.liveFeedLoop() }()
		go func() { defer close(completionDone); a.completionLoop() }()
	}
	closeFloating := a.startFloating()
	defer closeFloating()
	closeTray := startPlatform(url, a.data, cancel)
	defer closeTray()
	if !*noOpen {
		if er := openUI(url); er != nil {
			message("Codex Observatory", "请打开：\n"+url)
		}
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	select {
	case <-ctx.Done():
	case <-signals:
		cancel()
	}
	a.mu.Lock()
	if a.probeCancel != nil {
		a.probeCancel()
	}
	a.mu.Unlock()
	c, cc := context.WithTimeout(context.Background(), 3*time.Second)
	defer cc()
	_ = srv.Shutdown(c)
	select {
	case <-scanDone:
	case <-time.After(3 * time.Second):
		log.Println("scanner did not finish within shutdown grace period")
	}
	_ = os.Remove(filepath.Join(a.data, "instance.json"))
	select {
	case <-feedDone:
	case <-time.After(2300 * time.Millisecond):
		log.Println("routing audit flush exceeded shutdown grace period")
	}
	select {
	case <-completionDone:
	case <-time.After(2 * time.Second):
		log.Println("completion state flush exceeded shutdown grace period")
	}
	log.Println("stopped")
}
func fatal(e error) { log.Println(e); message("Codex Observatory · 启动失败", e.Error()) }
