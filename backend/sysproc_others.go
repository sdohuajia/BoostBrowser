//go:build !windows
// +build !windows

package backend

import "os/exec"

func hideWindow(cmd *exec.Cmd) {
	// do nothing on non-windows platforms
}
