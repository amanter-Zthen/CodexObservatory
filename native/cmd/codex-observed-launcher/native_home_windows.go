//go:build windows

package main

import (
	"bytes"
	"codex-observatory/internal/capturehome"
	"codex-observatory/internal/routefeed"
	"codex-observatory/internal/routetrace"
	"codex-observatory/internal/wirecapture"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func nativeHome(cfg launchConfig) (string, error) {
	home := cfg.Home
	if home == "" {
		home = os.Getenv("CODEX_HOME")
	}
	if home == "" {
		user, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(user, ".codex")
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("Codex Home needs an absolute path")
	}
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", fmt.Errorf("Codex Home is unavailable")
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("Codex Home is unavailable")
	}
	return resolved, nil
}

func nativeIdentityPath(cfg launchConfig) string {
	return filepath.Join(cfg.Data, "native-capture", "identity.json")
}

func loadNativeIdentity(cfg launchConfig) (wirecapture.Options, error) {
	var identity wirecapture.Options
	b, err := os.ReadFile(nativeIdentityPath(cfg))
	if os.IsNotExist(err) {
		return identity, nil
	}
	if err != nil || len(b) > 16384 || json.Unmarshal(b, &identity) != nil || identity.Address == "" || identity.Password == "" || len(identity.Key) == 0 || len(identity.CA) == 0 {
		return identity, fmt.Errorf("saved native capture identity needs review")
	}
	return identity, nil
}

func writeNativeHome(cfg launchConfig, proxyURL string, ca []byte) error {
	home, err := nativeHome(cfg)
	if err != nil {
		return err
	}
	certPath := filepath.Join(cfg.Data, "native-capture", "ca.pem")
	if err = os.WriteFile(certPath, ca, 0600); err != nil {
		return err
	}
	path := filepath.Join(home, ".env")
	original, err := os.ReadFile(path)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(original) > 1024*1024 {
		return fmt.Errorf("native environment file needs review")
	}
	updated, err := capturehome.Install(original, proxyURL, certPath)
	if err != nil {
		return err
	}
	backupPath := filepath.Join(cfg.Data, "native-capture", "original-dotenv.json")
	if _, err = os.Stat(backupPath); os.IsNotExist(err) {
		if err = saveConfig(backupPath, map[string]any{"home": home, "existed": existed, "content": original}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if bytes.Equal(original, updated) {
		return nil
	}
	return replacePrivateFile(path, updated)
}

func replacePrivateFile(path string, content []byte) error {
	temp := path + fmt.Sprintf(".observatory-%d.tmp", os.Getpid())
	if err := os.WriteFile(temp, content, 0600); err != nil {
		return err
	}
	return moveReplacing(temp, path)
}

func removeNativeHome(cfg launchConfig) error {
	home, err := nativeHome(cfg)
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".env")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	updated, err := capturehome.Remove(b)
	if err != nil {
		return err
	}
	if bytes.Equal(updated, b) {
		return nil
	}
	if len(updated) == 0 {
		return os.Remove(path)
	}
	return replacePrivateFile(path, updated)
}

func cleanCaptureHost(cfg launchConfig) {
	original := os.Environ()
	remaining := map[string]bool{}
	for _, entry := range cleanLaunchEnvironment(original, cfg) {
		key, _, _ := strings.Cut(entry, "=")
		remaining[strings.ToUpper(key)] = true
	}
	for _, entry := range original {
		key, _, _ := strings.Cut(entry, "=")
		if !remaining[strings.ToUpper(key)] {
			_ = os.Unsetenv(key)
		}
	}
}

// Setup runs only in the explicitly enabled companion. Native Codex loads the
// marked home block itself; Windows' registered GUI activation stays intact.
func setupNativeHome(cfg launchConfig) error {
	cleanCaptureHost(cfg)
	if err := os.MkdirAll(filepath.Dir(nativeIdentityPath(cfg)), 0700); err != nil {
		return err
	}
	identity, err := loadNativeIdentity(cfg)
	if err != nil {
		return err
	}
	options := identity
	// Validate retained credentials on a temporary port, so re-enabling while
	// the watcher is already listening does not interrupt its current clients.
	if options.Address != "" {
		options.Address = "127.0.0.1:0"
	}
	proxy, err := wirecapture.StartWithOptions(routetrace.New(0), func([]routetrace.Record) {}, false, options)
	if err != nil {
		return fmt.Errorf("native capture endpoint unavailable: %v", err)
	}
	defer proxy.Close()
	if identity.Address == "" {
		identity = proxy.Identity()
	}
	if err = saveConfig(nativeIdentityPath(cfg), identity); err != nil {
		return err
	}
	u := url.URL{Scheme: "http", Host: identity.Address, User: url.UserPassword("observer", identity.Password)}
	return writeNativeHome(cfg, u.String(), proxy.CA)
}

type nativeCaptureHost struct {
	proxy       *wirecapture.Proxy
	collector   *routetrace.Collector
	mu          sync.Mutex
	writeErrors uint64
	data        string
	pid         int
	feed        *routefeed.Sink
	feedServer  *routefeed.Server
}

func startNativeHomeCapture(cfg launchConfig) (*nativeCaptureHost, error) {
	cleanCaptureHost(cfg)
	identity, err := loadNativeIdentity(cfg)
	if err != nil || identity.Address == "" {
		return nil, fmt.Errorf("enable native home capture before watching")
	}
	host := &nativeCaptureHost{collector: routetrace.New(0), data: cfg.Data}
	for _, name := range []string{"live-routing-inbox", "routing-bridge-status"} {
		if err = os.MkdirAll(filepath.Join(cfg.Data, name), 0700); err != nil {
			return nil, err
		}
	}
	host.feed, err = routefeed.NewSink(func(row routetrace.Record) error {
		return saveConfig(filepath.Join(cfg.Data, "live-routing-inbox", row.ID+".json"), row)
	}, 4096)
	if err != nil {
		return nil, err
	}
	host.feedServer, err = routefeed.Listen(host.feed.Hub)
	if err == nil {
		err = saveConfig(filepath.Join(cfg.Data, routefeed.DiscoveryFile), host.feedServer.Description)
	}
	if err != nil {
		host.closeFeed()
		return nil, err
	}
	host.proxy, err = wirecapture.StartWithOptions(host.collector, host.feed.Publish, false, identity)
	if err != nil {
		host.closeFeed()
		return nil, fmt.Errorf("native capture listener failed: %v", err)
	}
	if err = writeNativeHome(cfg, host.proxy.URL, host.proxy.CA); err != nil {
		host.proxy.Close()
		host.closeFeed()
		return nil, err
	}
	return host, nil
}

func (h *nativeCaptureHost) update(desktopPID int, known bool, running bool) {
	h.collector.SetPID(desktopPID)
	h.pid = desktopPID
	h.mu.Lock()
	failures := h.writeErrors
	h.mu.Unlock()
	writer := h.feed.Stats()
	failures += writer.WriteErrors + writer.CriticalOverruns
	stats := h.collector.Stats()
	packageIdentity := desktopPackageIdentity(desktopPID)
	_ = saveConfig(filepath.Join(h.data, "routing-bridge-status", "native-home.json"), map[string]any{
		"at": time.Now().UTC().Format(time.RFC3339Nano), "active": running && stats["metadata_records"] > 0,
		"listener_ready": true, "client_pid": desktopPID, "bridge_pid": os.Getpid(), "client_role": "desktop",
		"protocol": "native-home-forward-proxy", "capture_revision": wirecapture.CaptureRevision, "inspection_known": known,
		"original_desktop_entry_preserved": true, "metadata_write_errors": failures, "diagnostics": stats, "model_requests_generated": 0,
		"desktop_package_identity": packageIdentity,
		"live_feed_version":        1, "report_writer": writer,
	})
}

func (h *nativeCaptureHost) close() {
	h.proxy.Close()
	h.closeFeed()
	h.update(h.pid, true, false)
}

func (h *nativeCaptureHost) closeFeed() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if h.feedServer != nil {
		_ = h.feedServer.Close(ctx)
	}
	if h.feed != nil {
		if h.feed.Close(ctx) != nil {
			h.mu.Lock()
			h.writeErrors++
			h.mu.Unlock()
		}
	}
}

func nativeCaptureReady(cfg launchConfig) bool {
	identity, err := loadNativeIdentity(cfg)
	if err != nil || identity.Address == "" {
		return false
	}
	u := &url.URL{Scheme: "http", Host: identity.Address, User: url.UserPassword("observer", identity.Password)}
	transport := &http.Transport{Proxy: http.ProxyURL(u), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://codex-observatory.invalid/ready")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	b, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	return err == nil && response.StatusCode == 200 && string(b) == `{"capture":"CodexObservatory","ready":true}`
}
