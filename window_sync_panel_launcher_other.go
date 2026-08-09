//go:build !windows

package main

import "fmt"

func openWindowSyncPanel() error {
	return fmt.Errorf("窗口同步独立弹窗当前仅实现 Windows 版本")
}
