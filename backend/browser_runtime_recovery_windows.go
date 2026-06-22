//go:build windows

package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"boost-browser/backend/internal/logger"
)

type browserRuntimeProcess struct {
	PID            int
	ExecutablePath string
	CommandLine    string
	UserDataDir    string
	DebugPort      int
}

type winProcessSnapshot struct {
	ProcessId      int    `json:"ProcessId"`
	ExecutablePath string `json:"ExecutablePath"`
	CommandLine    string `json:"CommandLine"`
}

var (
	remoteDebugPortRe       = regexp.MustCompile(`(?i)(?:^|\s)"?--remote-debugging-port=(\d+)"?`)
	quotedUserDataArgRe     = regexp.MustCompile(`(?i)(?:^|\s)"--user-data-dir=([^"]+)"`)
	quotedUserDataValueRe   = regexp.MustCompile(`(?i)(?:^|\s)--user-data-dir="([^"]+)"`)
	unquotedUserDataValueRe = regexp.MustCompile(`(?i)(?:^|\s)--user-data-dir=([^"\s]+)`)
)

func (a *App) startBrowserRuntimeReconciler() {
	if a == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.New("Browser").Error("runtime reconciler goroutine panic recovered",
					logger.F("error", r),
				)
			}
		}()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.New("Browser").Error("reconcileBrowserRuntimeStateOnce panic recovered",
							logger.F("error", r),
						)
					}
				}()
				a.reconcileBrowserRuntimeStateOnce()
			}()
		}
	}()
}

func (a *App) reconcileBrowserRuntimeStateOnce() {
	if a == nil || a.browserMgr == nil {
		return
	}

	processes, err := discoverBoostBrowserProcesses(a.appRoot)
	if err != nil {
		return
	}

	type update struct {
		profile *BrowserProfile
		kind    string
	}
	var updates []update
	var recoveredIDs []string
	var stoppedIDs []string
	log := logger.New("Browser")

	a.browserMgr.Mutex.Lock()
	defer func() {
		a.browserMgr.Mutex.Unlock()
		for _, item := range updates {
			if item.profile != nil {
				a.emitBrowserInstanceUpdated(item.profile)
			}
		}
	}()

	byUserDataDir := make(map[string][]browserRuntimeProcess)
	for _, proc := range processes {
		key := normalizeRuntimePathKey(proc.UserDataDir)
		if key == "" || proc.DebugPort <= 0 {
			continue
		}
		byUserDataDir[key] = append(byUserDataDir[key], proc)
	}
	for key := range byUserDataDir {
		sort.Slice(byUserDataDir[key], func(i, j int) bool {
			return byUserDataDir[key][i].PID < byUserDataDir[key][j].PID
		})
	}

	for profileId, profile := range a.browserMgr.Profiles {
		if profile == nil {
			continue
		}

		userDataKey := normalizeRuntimePathKey(a.browserMgr.ResolveUserDataDir(profile))
		proc, exists := pickRuntimeProcessForSync(byUserDataDir[userDataKey])
		if profile.Running && isBrowserProfileLive(profile, a.browserMgr.BrowserProcesses[profileId]) {
			if profile.Pid > 0 {
				continue
			}
			if !exists {
				continue
			}

			debugReady := canConnectDebugPort(proc.DebugPort, 300*time.Millisecond)
			runtimeWarning := profile.RuntimeWarning
			if !debugReady {
				runtimeWarning = "主程序已重新接管该浏览器进程，但调试接口暂未就绪；实例仍保持运行状态。"
			} else {
				runtimeWarning = ""
			}
			profile.Pid = proc.PID
			profile.DebugPort = proc.DebugPort
			profile.DebugReady = debugReady
			profile.RuntimeWarning = runtimeWarning
			updates = append(updates, update{profile: copyBrowserProfileSnapshot(profile), kind: "recovered"})
			recoveredIDs = append(recoveredIDs, profileId)
			continue
		}

		if !exists {
			if profile.Running {
				a.markProfileStoppedLocked(profileId, profile)
				updates = append(updates, update{profile: copyBrowserProfileSnapshot(profile), kind: "stopped"})
				stoppedIDs = append(stoppedIDs, profileId)
			}
			continue
		}

		debugReady := canConnectDebugPort(proc.DebugPort, 300*time.Millisecond)
		runtimeWarning := ""
		if !debugReady {
			runtimeWarning = "主程序已重新接管该浏览器进程，但调试接口暂未就绪；实例仍保持运行状态。"
		}
		a.markProfileRunningLocked(profileId, profile, nil, proc.PID, proc.DebugPort, debugReady, runtimeWarning)
		updates = append(updates, update{profile: copyBrowserProfileSnapshot(profile), kind: "recovered"})
		recoveredIDs = append(recoveredIDs, profileId)
	}
	for _, profileId := range recoveredIDs {
		log.Info("已重新接管宿主重启前存活的浏览器实例", logger.F("profile_id", profileId))
	}
	for _, profileId := range stoppedIDs {
		log.Info("运行实例已无存活浏览器进程，状态已同步为停止", logger.F("profile_id", profileId))
	}
}

func pickRuntimeProcessForSync(candidates []browserRuntimeProcess) (browserRuntimeProcess, bool) {
	if len(candidates) == 0 {
		return browserRuntimeProcess{}, false
	}
	for _, proc := range candidates {
		if proc.PID <= 0 {
			continue
		}
		if _, err := findProcessWindow(proc.PID); err == nil {
			return proc, true
		}
	}
	return candidates[0], true
}

func discoverBoostBrowserProcesses(appRoot string) ([]browserRuntimeProcess, error) {
	root := strings.TrimSpace(appRoot)
	if root == "" {
		return nil, nil
	}
	root = filepath.Clean(root)
	script := fmt.Sprintf(`
$root = %s
$chromeRoot = [System.IO.Path]::GetFullPath((Join-Path $root 'chrome'))
$items = Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object {
  $_.ExecutablePath -and $_.CommandLine -and $_.ExecutablePath.StartsWith($chromeRoot, [System.StringComparison]::OrdinalIgnoreCase) -and $_.CommandLine.Contains('--user-data-dir=') -and $_.CommandLine.Contains('--remote-debugging-port=')
} | Select-Object ProcessId, ExecutablePath, CommandLine
@($items) | ConvertTo-Json -Depth 3 -Compress
`, psSingleQuoted(root))

	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShellCommand(script))
	hideWindow(cmd)
	// Add a timeout to prevent PowerShell from hanging indefinitely.
	done := make(chan struct{})
	var out []byte
	var cmdErr error
	go func() {
		defer close(done)
		out, cmdErr = cmd.Output()
	}()
	select {
	case <-done:
		// Command completed.
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("powershell process discovery timed out after 5s")
	}
	if cmdErr != nil {
		return nil, cmdErr
	}
	text := strings.TrimSpace(string(out))
	if text == "" || text == "null" {
		return nil, nil
	}

	var snapshots []winProcessSnapshot
	if strings.HasPrefix(text, "[") {
		if err := json.Unmarshal([]byte(text), &snapshots); err != nil {
			return nil, err
		}
	} else {
		var one winProcessSnapshot
		if err := json.Unmarshal([]byte(text), &one); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, one)
	}

	processes := make([]browserRuntimeProcess, 0, len(snapshots))
	for _, item := range snapshots {
		userDataDir, debugPort := parseChromeRuntimeCommandLine(item.CommandLine)
		if item.ProcessId <= 0 || userDataDir == "" || debugPort <= 0 {
			continue
		}
		processes = append(processes, browserRuntimeProcess{
			PID:            item.ProcessId,
			ExecutablePath: item.ExecutablePath,
			CommandLine:    item.CommandLine,
			UserDataDir:    userDataDir,
			DebugPort:      debugPort,
		})
	}
	sort.Slice(processes, func(i, j int) bool { return processes[i].PID < processes[j].PID })
	return processes, nil
}

func parseChromeRuntimeCommandLine(commandLine string) (string, int) {
	userDataDir := ""
	for _, re := range []*regexp.Regexp{quotedUserDataArgRe, quotedUserDataValueRe, unquotedUserDataValueRe} {
		if match := re.FindStringSubmatch(commandLine); len(match) >= 2 {
			userDataDir = strings.TrimSpace(match[1])
			break
		}
	}
	debugPort := 0
	if match := remoteDebugPortRe.FindStringSubmatch(commandLine); len(match) >= 2 {
		if port, err := strconv.Atoi(match[1]); err == nil {
			debugPort = port
		}
	}
	return userDataDir, debugPort
}

func normalizeRuntimePathKey(path string) string {
	path = strings.Trim(strings.TrimSpace(path), `"`)
	if path == "" {
		return ""
	}
	return strings.ToLower(filepath.Clean(path))
}

func encodePowerShellCommand(script string) string {
	encoded := utf16.Encode([]rune(script))
	buf := make([]byte, len(encoded)*2)
	for i, v := range encoded {
		buf[i*2] = byte(v)
		buf[i*2+1] = byte(v >> 8)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func psSingleQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
