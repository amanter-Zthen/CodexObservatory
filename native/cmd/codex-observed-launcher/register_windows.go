//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func broadcastEnvironment() {
	key, _ := syscall.UTF16PtrFromString("Environment")
	var result uintptr
	syscall.NewLazyDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(0xffff, 0x001a, 0, uintptr(unsafe.Pointer(key)), 2, 2000, uintptr(unsafe.Pointer(&result)))
}
func enableFollow(dir string, cfg launchConfig) error {
	if err := os.MkdirAll(cfg.Data, 0700); err != nil {
		return err
	}
	backup := filepath.Join(cfg.Data, "follow-original-environment.json")
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		data, err := child("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "[pscustomobject]@{value=[Environment]::GetEnvironmentVariable('CODEX_CLI_PATH','User')}|ConvertTo-Json -Compress").Output()
		if err != nil {
			return fmt.Errorf("读取 Codex 启动配置失败")
		}
		var value map[string]any
		if json.Unmarshal(data, &value) != nil {
			return fmt.Errorf("启动配置备份无效")
		}
		if err = saveConfig(backup, value); err != nil {
			return err
		}
	}
	if err := restoreCoreLaunchChoice(cfg); err != nil {
		return err
	}
	if err := setupNativeHome(cfg); err != nil {
		return fmt.Errorf("设置原生采集失败：%v", err)
	}
	launcher, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := child("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop';$key='HKCU:\Software\Microsoft\Windows\CurrentVersion\Run';New-Item -Path $key -Force|Out-Null;New-ItemProperty -Path $key -Name CodexObservatory -PropertyType String -Value ([char]34+$env:OBSERVATORY_LAUNCHER_PATH+[char]34+' --watch') -Force|Out-Null`)
	cmd.Env = append(os.Environ(), "OBSERVATORY_BRIDGE_PATH="+cfg.Bridge, "OBSERVATORY_LAUNCHER_PATH="+launcher)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("设置 Codex 启动跟随失败")
	}
	if err := saveConfig(filepath.Join(cfg.Data, "follow-settings.json"), map[string]any{"enabled": true}); err != nil {
		return err
	}
	broadcastEnvironment()
	return nil
}

// Recover only this application's previous CLI override. Startup following
// leaves the official Desktop free to choose its registered Windows core.
func restoreCoreLaunchChoice(cfg launchConfig) error {
	var backup struct {
		Value *string `json:"value"`
	}
	b, err := os.ReadFile(filepath.Join(cfg.Data, "follow-original-environment.json"))
	if err != nil || json.Unmarshal(b, &backup) != nil {
		return fmt.Errorf("原始 Codex 启动配置备份未找到")
	}
	cmd := child("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop';$current=[Environment]::GetEnvironmentVariable('CODEX_CLI_PATH','User');if($current -eq $env:OBSERVATORY_BRIDGE_PATH){if($env:OBSERVATORY_PREVIOUS_PRESENT -eq '1'){[Environment]::SetEnvironmentVariable('CODEX_CLI_PATH',$env:OBSERVATORY_PREVIOUS_VALUE,'User')}else{Remove-ItemProperty -LiteralPath 'HKCU:\Environment' -Name CODEX_CLI_PATH -ErrorAction SilentlyContinue}}`)
	cmd.Env = append(os.Environ(), "OBSERVATORY_BRIDGE_PATH="+cfg.Bridge)
	if backup.Value != nil {
		cmd.Env = append(cmd.Env, "OBSERVATORY_PREVIOUS_PRESENT=1", "OBSERVATORY_PREVIOUS_VALUE="+*backup.Value)
	}
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("恢复官方 Codex 核心启动选择失败")
	}
	return nil
}
func disableFollow(dir string, cfg launchConfig) error {
	if err := removeNativeHome(cfg); err != nil {
		return err
	}
	var backup struct {
		Value *string `json:"value"`
	}
	b, err := os.ReadFile(filepath.Join(cfg.Data, "follow-original-environment.json"))
	if err != nil || json.Unmarshal(b, &backup) != nil {
		return fmt.Errorf("原始 Codex 启动配置备份未找到")
	}
	cmd := child("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop';$current=[Environment]::GetEnvironmentVariable('CODEX_CLI_PATH','User');if($current -eq $env:OBSERVATORY_BRIDGE_PATH){$previous=$null;if($env:OBSERVATORY_PREVIOUS_PRESENT -eq '1'){$previous=$env:OBSERVATORY_PREVIOUS_VALUE};[Environment]::SetEnvironmentVariable('CODEX_CLI_PATH',$previous,'User')};$key='HKCU:\Software\Microsoft\Windows\CurrentVersion\Run';$registered=(Get-ItemProperty -Path $key -Name CodexObservatory -ErrorAction SilentlyContinue).CodexObservatory;if($registered -like ('*'+$env:OBSERVATORY_LAUNCHER_PATH+'*')){Remove-ItemProperty -Path $key -Name CodexObservatory}`)
	launcher, err := os.Executable()
	if err != nil {
		return err
	}
	cmd.Env = append(os.Environ(), "OBSERVATORY_BRIDGE_PATH="+cfg.Bridge, "OBSERVATORY_LAUNCHER_PATH="+launcher)
	if backup.Value != nil {
		cmd.Env = append(cmd.Env, "OBSERVATORY_PREVIOUS_PRESENT=1", "OBSERVATORY_PREVIOUS_VALUE="+*backup.Value)
	}
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("恢复 Codex 启动设置失败")
	}
	if err = saveConfig(filepath.Join(cfg.Data, "follow-settings.json"), map[string]any{"enabled": false}); err != nil {
		return err
	}
	broadcastEnvironment()
	return nil
}
