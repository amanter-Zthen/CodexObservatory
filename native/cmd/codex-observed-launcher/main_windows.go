//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

type launchConfig struct {
	Native  string `json:"native_codex"`
	Data    string `json:"observatory_data"`
	Mode    string `json:"capture_mode"`
	Fixture bool   `json:"loopback_fixture"`
	Desktop string `json:"desktop_app"`
	Monitor string `json:"monitor_exe"`
	Bridge  string `json:"bridge_exe,omitempty"`
	Home    string `json:"codex_home,omitempty"`
}

// Commands. Exactly one may be given; the name is resolved by the parser so
// that flag order never changes behaviour.
const (
	cmdNone        = ""
	cmdWatch       = "watch"
	cmdHealth      = "health-check"
	cmdRepair      = "repair-stale-native-home"
	cmdMonitorOnly = "monitor-only"
	cmdEnable      = "enable-follow"
	cmdDisable     = "disable-follow"
)

// launchArgs is the single flag parser for this binary; there must not be a
// second, divergent parser anywhere.
type launchArgs struct {
	Data        string
	Home        string
	HasData     bool
	HasHome     bool
	Command     string
	UnknownArgs []string
	ParseErrors []string
}

var currentLaunchArgs = launchArgs{}

func parseLaunchArgs(args []string) launchArgs {
	var a launchArgs
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--data":
			if i+1 >= len(args) {
				a.ParseErrors = append(a.ParseErrors, "--data requires a value")
				break
			}
			a.Data = args[i+1]
			a.HasData = true
			i++
		case "--home":
			if i+1 >= len(args) {
				a.ParseErrors = append(a.ParseErrors, "--home requires a value")
				break
			}
			a.Home = args[i+1]
			a.HasHome = true
			i++
		case "--watch":
			a.setCommand(cmdWatch)
		case "--health-check":
			a.setCommand(cmdHealth)
		case "--repair-stale-native-home":
			a.setCommand(cmdRepair)
		case "--monitor-only":
			a.setCommand(cmdMonitorOnly)
		case "--enable-follow":
			a.setCommand(cmdEnable)
		case "--disable-follow":
			a.setCommand(cmdDisable)
		default:
			if strings.HasPrefix(args[i], "--") {
				a.UnknownArgs = append(a.UnknownArgs, args[i])
			}
		}
	}
	return a
}

// setCommand fails closed when two mutually exclusive commands are requested.
func (a *launchArgs) setCommand(name string) {
	if a.Command != cmdNone && a.Command != name {
		a.ParseErrors = append(a.ParseErrors, "mutually exclusive commands: --"+a.Command+" and --"+name)
		return
	}
	a.Command = name
}

// validate reports every parse problem at once (fail closed, no silent default).
func (a launchArgs) validate() error {
	if len(a.ParseErrors) > 0 {
		return fmt.Errorf("invalid arguments: %s", strings.Join(a.ParseErrors, "; "))
	}
	if len(a.UnknownArgs) > 0 {
		return fmt.Errorf("unknown argument: %s", strings.Join(a.UnknownArgs, " "))
	}
	if a.HasData && !filepath.IsAbs(a.Data) {
		return fmt.Errorf("--data needs an absolute path")
	}
	if a.HasHome && !filepath.IsAbs(a.Home) {
		return fmt.Errorf("--home needs an absolute path")
	}
	return nil
}

// applyOverrides merges --data / --home into the config loaded from
// bridge-config.json. It is applied on every path (health-check, repair, watch,
// and the normal launcher path) before any logic runs.
func applyOverrides(cfg launchConfig, a launchArgs) launchConfig {
	if a.HasData && a.Data != "" {
		cfg.Data = a.Data
	}
	if a.HasHome && a.Home != "" {
		cfg.Home = a.Home
	}
	return cfg
}

// cfgFromArgs derives a config for failure-reporting paths, reusing the same
// parser so the two can never disagree.
func cfgFromArgs() launchConfig {
	return applyOverrides(launchConfig{}, currentLaunchArgs)
}

func message(text string) {
	dll := syscall.NewLazyDLL("user32.dll")
	body, _ := syscall.UTF16PtrFromString(text)
	title, _ := syscall.UTF16PtrFromString("Codex · 实时路由入口")
	dll.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0x40)
}
func child(path string, args ...string) *exec.Cmd {
	c := exec.Command(path, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}
func running(path string) (bool, error) {
	pid, err := runningDesktopPID(path)
	return pid != 0, err
}

// isCodexWindowsAppsDesktopPath reports whether path itself is a packaged Codex
// desktop binary. Only then may version-drift tolerance be applied.
func isCodexWindowsAppsDesktopPath(path string) bool {
	if path == "" {
		return false
	}
	root := strings.ToLower(filepath.Join(os.Getenv("ProgramFiles"), "WindowsApps", "OpenAI.Codex_"))
	low := strings.ToLower(path)
	return strings.HasPrefix(low, root) && strings.HasSuffix(low, `\app\chatgpt.exe`)
}
func runningDesktopPID(path string) (int, error) {
	snap, e := syscall.CreateToolhelp32Snapshot(2, 0)
	if e != nil {
		return 0, e
	}
	defer syscall.CloseHandle(snap)
	var p syscall.ProcessEntry32
	p.Size = uint32(unsafe.Sizeof(p))
	e = syscall.Process32First(snap, &p)
	query := syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")
	// Only a packaged Codex desktop path gets the WindowsApps version-drift
	// fallback. Any other configured path (e.g. a test stub) matches by exact
	// executable path only, so a running production Codex can never be mistaken
	// for the configured test desktop.
	allowPackageDrift := isCodexWindowsAppsDesktopPath(path)
	parents := map[uint32]uint32{}
	for e == nil {
		if strings.EqualFold(syscall.UTF16ToString(p.ExeFile[:]), "ChatGPT.exe") {
			h, err := syscall.OpenProcess(0x1000, false, p.ProcessID)
			if err != nil {
				return 0, err
			}
			buffer := make([]uint16, 32768)
			size := uint32(len(buffer))
			ok, _, _ := query.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
			syscall.CloseHandle(h)
			if ok == 0 {
				return 0, fmt.Errorf("Desktop process inspection unavailable")
			}
			image := syscall.UTF16ToString(buffer[:size])
			matched := strings.EqualFold(image, path)
			if !matched && allowPackageDrift {
				matched = strings.HasSuffix(strings.ToLower(image), `\app\chatgpt.exe`) &&
					strings.HasPrefix(strings.ToLower(image), strings.ToLower(filepath.Join(os.Getenv("ProgramFiles"), "WindowsApps", "OpenAI.Codex_")))
			}
			if matched {
				parents[p.ProcessID] = p.ParentProcessID
			}
		}
		e = syscall.Process32Next(snap, &p)
	}
	chosen := uint32(0)
	for pid, parent := range parents {
		if _, child := parents[parent]; !child && (chosen == 0 || pid < chosen) {
			chosen = pid
		}
	}
	return int(chosen), nil
}
func newest(paths []string) string {
	sort.Slice(paths, func(i, j int) bool {
		a, _ := os.Stat(paths[i])
		b, _ := os.Stat(paths[j])
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return a.ModTime().After(b.ModTime())
	})
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
}
func installedDesktop(fallback string) string {
	paths, _ := filepath.Glob(filepath.Join(os.Getenv("ProgramFiles"), "WindowsApps", "OpenAI.Codex_*_x64__*", "app", "ChatGPT.exe"))
	if len(paths) > 0 {
		return newest(paths)
	}
	// Appx locations may be readable while WindowsApps enumeration is restricted.
	output, err := child("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "(Get-AppxPackage -Name OpenAI.Codex).InstallLocation").Output()
	if err == nil {
		path := filepath.Join(strings.TrimSpace(string(output)), "app", "ChatGPT.exe")
		if filepath.IsAbs(path) {
			if _, err = os.Stat(path); err == nil {
				return path
			}
		}
	}
	return fallback
}
func main() {
	if err := run(); err != nil {
		// The watch path writes lifecycle-error.log once, inside watch(), using
		// cfg.Data. main() must not write a second record (and must never fall
		// back to the production UserCache), so commands are identified through
		// the parsed command rather than os.Args[1].
		if currentLaunchArgs.Command == cmdWatch {
			os.Exit(1)
		}
		message(err.Error())
		os.Exit(1)
	}
}
func run() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	cfgPath := filepath.Join(dir, "bridge-config.json")
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("启动配置未找到：%s", cfgPath)
	}
	var cfg launchConfig
	if json.Unmarshal(b, &cfg) != nil || cfg.Fixture || cfg.Mode != "local-proxy" {
		return fmt.Errorf("启动配置需要使用正式实时采集模式")
	}

	// --- unified --data / --home overrides (test isolation) ---
	parsed := parseLaunchArgs(os.Args)
	currentLaunchArgs = parsed
	if err := parsed.validate(); err != nil {
		return err
	}
	cfg = applyOverrides(cfg, parsed)

	if cfg.Data == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return err
		}
		cfg.Data = filepath.Join(cache, "CodexObservatory")
	}
	if cfg.Monitor == "" {
		cfg.Monitor = "CodexObservatory.exe"
	}
	if !filepath.IsAbs(cfg.Monitor) {
		cfg.Monitor = filepath.Join(dir, cfg.Monitor)
	}
	if cfg.Bridge == "" {
		cfg.Bridge = filepath.Join(dir, "CodexRoutingBridge.exe")
	}
	if !filepath.IsAbs(cfg.Bridge) {
		cfg.Bridge = filepath.Join(dir, cfg.Bridge)
	}

	// --- EARLY DISPATCH -------------------------------------------------
	// These commands run BEFORE desktop discovery, path validation and
	// bridge-config persistence. --health-check is strictly read-only; --repair
	// only touches the target Home/.env; neither rewrites bridge-config.json.
	switch parsed.Command {
	case cmdHealth:
		os.Exit(runHealthCheck(cfg))
	case cmdRepair:
		os.Exit(runRepairStaleNativeHome(cfg))
	case cmdWatch:
		return watch(cfg)
	}

	// --- normal launcher path (GUI / follow setup) ----------------------
	if candidates, _ := filepath.Glob(filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenAI", "Codex", "bin", "*", "codex.exe")); len(candidates) > 0 {
		cfg.Native = newest(candidates)
	}
	cfg.Desktop = installedDesktop(cfg.Desktop)
	for _, path := range []string{cfg.Desktop, cfg.Monitor} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("启动配置需要完整安装路径")
		}
		if st, e := os.Stat(path); e != nil || st.IsDir() {
			return fmt.Errorf("所需程序未找到：%s", path)
		}
	}
	if err = saveConfig(cfgPath, cfg); err != nil {
		return err
	}
	switch parsed.Command {
	case cmdMonitorOnly:
		return startMonitor(cfg)
	case cmdEnable:
		if err = enableFollow(dir, cfg); err != nil {
			return err
		}
		return ensureWatcher(dir, cfg)
	case cmdDisable:
		return disableFollow(dir, cfg)
	}
	if err = enableFollow(dir, cfg); err != nil {
		return err
	}
	if err = ensureWatcher(dir, cfg); err != nil {
		return err
	}
	active, err := running(cfg.Desktop)
	if err != nil {
		return fmt.Errorf("请先退出 Codex，再使用此入口启动；当前无法确认原程序是否已经退出")
	}
	if active {
		return startMonitor(cfg)
	}
	// Use the same registered application as the original taskbar pin. Its
	// native core obtains capture settings from its own Codex Home at startup.
	return child("explorer.exe", `shell:AppsFolder\OpenAI.Codex_2p2nqsd0c76g0!App`).Run()
}
