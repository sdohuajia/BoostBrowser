//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func takeoverExistingMainInstanceForPostUpdate(appRoot string) {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	exePath = filepath.Clean(exePath)
	currentPID := os.Getpid()

	// 兼容从旧版升级：旧版主进程退出时，旧 watchdog 可能抢在 updater 替换前
	// 重新拉起旧 EXE，导致新版 --post-update 因单实例锁退出，用户仍看到旧版本。
	// 新版已被 updater 启动后，主动清掉同路径的其它 boost-browser.exe 主/看门狗进程，
	// 再继续正常启动，确保界面真正切到新版本。
	for i := 0; i < 12; i++ {
		killSiblingProcessesByExecutablePath(exePath, currentPID)
		time.Sleep(250 * time.Millisecond)
	}
}

func killSiblingProcessesByExecutablePath(exePath string, currentPID int) {
	escapedPath := strings.ReplaceAll(exePath, "'", "''")
	script := fmt.Sprintf(`$ErrorActionPreference='SilentlyContinue'; Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -eq '%s' -and $_.ProcessId -ne %d } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }`, escapedPath, currentPID)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200}
	_ = cmd.Run()
}
