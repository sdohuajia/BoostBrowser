//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func openWindowSyncPanel() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位主程序失败: %w", err)
	}

	cmd := exec.Command(exePath, "--sync-panel")
	cmd.Dir = filepath.Dir(exePath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动窗口同步面板失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
