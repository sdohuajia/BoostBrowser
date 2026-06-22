//go:build windows

package backend

import "syscall"

// detachedProcessAttrs 让 updater.exe 完全脱离主程序的进程组，
// 主程序退出时 updater 不会被一起带走。
func detachedProcessAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow: true,
		// DETACHED_PROCESS (0x8) | CREATE_NEW_PROCESS_GROUP (0x200)
		CreationFlags: 0x00000008 | 0x00000200,
	}
}
