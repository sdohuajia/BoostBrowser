//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"boost-browser/backend"

	"golang.org/x/sys/windows"
)

const (
	watchdogEnvParent = "BOOST_BROWSER_WATCHDOG_PARENT_PID"
	watchdogEnvRoot   = "BOOST_BROWSER_WATCHDOG_APP_ROOT"
	watchdogEnvExe    = "BOOST_BROWSER_WATCHDOG_EXE"
)

func runUnexpectedExitWatchdogMode() bool {
	pidText := strings.TrimSpace(os.Getenv(watchdogEnvParent))
	if pidText == "" {
		return false
	}
	parentPID, err := strconv.Atoi(pidText)
	if err != nil || parentPID <= 0 {
		return true
	}
	appRoot := strings.TrimSpace(os.Getenv(watchdogEnvRoot))
	exePath := strings.TrimSpace(os.Getenv(watchdogEnvExe))
	if appRoot == "" || exePath == "" {
		return true
	}

	logWatchdog(appRoot, "watchdog-start", fmt.Sprintf("parent=%d", parentPID), fmt.Sprintf("exe=%s", exePath))
	exitCode, exitCodeOK := waitForProcessExit(parentPID)
	exitFields := []string{fmt.Sprintf("parent=%d", parentPID)}
	if exitCodeOK {
		exitFields = append(exitFields, fmt.Sprintf("exit_code=%d", exitCode), fmt.Sprintf("exit_code_hex=0x%08X", exitCode))
	}

	marker := backend.IntentionalExitMarkerPath(appRoot)
	if isFreshIntentionalExitMarker(marker, 10*time.Minute) {
		logWatchdog(appRoot, "watchdog-skip-restart", append([]string{"reason=intentional-exit"}, exitFields...)...)
		return true
	}

	logWatchdog(appRoot, "watchdog-restart", append([]string{"reason=unexpected-parent-exit"}, exitFields...)...)
	cmd := exec.Command(exePath)
	cmd.Dir = appRoot
	cmd.Env = filteredWatchdogEnv(os.Environ())
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | 0x00000008}
	if err := cmd.Start(); err != nil {
		logWatchdog(appRoot, "watchdog-restart-failed", "error="+err.Error())
		return true
	}
	logWatchdog(appRoot, "watchdog-restarted", fmt.Sprintf("child_pid=%d", cmd.Process.Pid))
	_ = cmd.Process.Release()
	return true
}

func startUnexpectedExitWatchdog(appRoot string) {
	if strings.TrimSpace(os.Getenv(watchdogEnvParent)) != "" {
		return
	}
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	if shouldSkipUnexpectedExitWatchdog(exePath) {
		logWatchdog(appRoot, "watchdog-skip-spawn", "reason=non-packaged-host", fmt.Sprintf("exe=%s", exePath))
		return
	}
	cmd := exec.Command(exePath)
	cmd.Dir = appRoot
	cmd.Env = append(filteredWatchdogEnv(os.Environ()),
		fmt.Sprintf("%s=%d", watchdogEnvParent, os.Getpid()),
		fmt.Sprintf("%s=%s", watchdogEnvRoot, appRoot),
		fmt.Sprintf("%s=%s", watchdogEnvExe, exePath),
	)
	cmd.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | 0x00000008}
	if err := cmd.Start(); err != nil {
		logWatchdog(appRoot, "watchdog-spawn-failed", "error="+err.Error())
		return
	}
	logWatchdog(appRoot, "watchdog-spawned", fmt.Sprintf("watchdog_pid=%d", cmd.Process.Pid))
	_ = cmd.Process.Release()
}

func shouldSkipUnexpectedExitWatchdog(exePath string) bool {
	if envFlagEnabled("BOOST_BROWSER_DISABLE_WATCHDOG") || envFlagEnabled("BOOST_BROWSER_SKIP_SINGLE_INSTANCE") {
		return true
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(exePath)))
	if base == "wailsbindings.exe" || strings.Contains(base, "wailsbindings") {
		return true
	}
	if resolvedExe, err := filepath.EvalSymlinks(filepath.Dir(exePath)); err == nil {
		exeDirLower := strings.ToLower(filepath.Clean(resolvedExe))
		tempDir := strings.ToLower(filepath.Clean(os.TempDir()))
		if strings.HasPrefix(exeDirLower, tempDir) {
			return true
		}
		if strings.HasSuffix(filepath.ToSlash(exeDirLower), "/build/bin") {
			return true
		}
	}
	return false
}

func filteredWatchdogEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key := entry
		if idx := strings.Index(entry, "="); idx >= 0 {
			key = entry[:idx]
		}
		switch strings.ToUpper(key) {
		case watchdogEnvParent, watchdogEnvRoot, watchdogEnvExe:
			continue
		default:
			out = append(out, entry)
		}
	}
	return out
}

func waitForProcessExit(pid int) (uint32, bool) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, windows.INFINITE)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return 0, false
	}
	return exitCode, true
}

func isFreshIntentionalExitMarker(path string, maxAge time.Duration) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(st.ModTime()) >= 0 && time.Since(st.ModTime()) <= maxAge
}

func logWatchdog(appRoot, event string, fields ...string) {
	path := backend.ResolveRuntimePath(appRoot, filepath.Join("logs", "app-lifecycle.log"))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	parts := []string{time.Now().Format("2006-01-02 15:04:05.000"), event, fmt.Sprintf("pid=%d", os.Getpid())}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			parts = append(parts, field)
		}
	}
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		_, _ = f.WriteString(strings.Join(parts, " | ") + "\n")
		_ = f.Close()
	}
}
