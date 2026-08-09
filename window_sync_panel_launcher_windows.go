//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// isBoostBrowserExecutable prevents a development/test host (for example
// C:\\Windows\\py.exe) from being recursively launched as the sync panel.
// Production launchers are deliberately limited to the two shipped names.
func isBoostBrowserExecutable(exePath string) bool {
	name := strings.ToLower(strings.TrimSpace(filepath.Base(exePath)))
	return name == "boost browser.exe" || name == "boost-browser.exe"
}

func openWindowSyncPanel() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位主程序失败: %w", err)
	}
	if !isBoostBrowserExecutable(exePath) {
		return fmt.Errorf("窗口同步只能从正式 Boost Browser.exe 启动；当前启动器为 %q，已拒绝启动以避免产生空白命令行窗口", exePath)
	}

	cmd := exec.Command(exePath, "--sync-panel")
	cmd.Dir = filepath.Dir(exePath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动窗口同步面板失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
