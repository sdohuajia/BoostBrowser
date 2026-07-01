//go:build !windows

package backend

import "syscall"

// detachedProcessAttrs Linux/macOS 占位实现，更新功能仅在 Windows 上启用
func detachedProcessAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
