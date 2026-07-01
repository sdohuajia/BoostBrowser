//go:build windows

package backend

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type runningMoreLoginProcessSnapshot struct {
	ProcessId      int    `json:"ProcessId"`
	ExecutablePath string `json:"ExecutablePath"`
	CommandLine    string `json:"CommandLine"`
}

func discoverMoreLoginRunningProfiles() ([]moreLoginRunningProfile, error) {
	script := `
$items = Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object {
  $_.ExecutablePath -and $_.CommandLine -and $_.CommandLine.Contains('--user-data-dir=')
} | Select-Object ProcessId, ExecutablePath, CommandLine
@($items) | ConvertTo-Json -Depth 3 -Compress
`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShellCommand(script))
	hideWindow(cmd)
	done := make(chan struct{})
	var out []byte
	var cmdErr error
	go func() {
		defer close(done)
		out, cmdErr = cmd.Output()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("枚举 MoreLogin 运行中环境超时")
	}
	if cmdErr != nil {
		return nil, cmdErr
	}
	text := strings.TrimSpace(string(out))
	if text == "" || text == "null" {
		return nil, nil
	}

	var snapshots []runningMoreLoginProcessSnapshot
	if strings.HasPrefix(text, "[") {
		if err := json.Unmarshal([]byte(text), &snapshots); err != nil {
			return nil, err
		}
	} else {
		var one runningMoreLoginProcessSnapshot
		if err := json.Unmarshal([]byte(text), &one); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, one)
	}

	byUserDataDir := make(map[string]moreLoginRunningProfile)
	for _, item := range snapshots {
		userDataDir, _ := parseChromeRuntimeCommandLine(item.CommandLine)
		if item.ProcessId <= 0 || !looksLikeMoreLoginRuntimeUserDataDir(userDataDir) {
			continue
		}
		profile := enrichRunningMoreLoginProfile(moreLoginRunningProfile{
			ProcessID:        item.ProcessId,
			ProfileID:        extractMoreLoginRuntimeProfileID(userDataDir),
			UserDataDir:      strings.TrimSpace(userDataDir),
			ProxyInformation: extractChromeCommandArg(item.CommandLine, "proxy-server"),
			UA:               extractChromeCommandArg(item.CommandLine, "user-agent"),
			CommandLine:      item.CommandLine,
		})
		key := normalizeRuntimePathKey(profile.UserDataDir)
		if key == "" {
			continue
		}
		if existing, ok := byUserDataDir[key]; !ok || profile.ProcessID < existing.ProcessID {
			byUserDataDir[key] = profile
		}
	}

	result := make([]moreLoginRunningProfile, 0, len(byUserDataDir))
	for _, item := range byUserDataDir {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(result[i].ProfileName))
		right := strings.ToLower(strings.TrimSpace(result[j].ProfileName))
		if left == right {
			return result[i].ProcessID < result[j].ProcessID
		}
		return left < right
	})
	return result, nil
}

func extractChromeCommandArg(commandLine string, argName string) string {
	argName = strings.ToLower(strings.TrimSpace(argName))
	if argName == "" {
		return ""
	}
	patterns := []string{
		fmt.Sprintf(" --%s=\"", argName),
		fmt.Sprintf(" \"--%s=", argName),
		fmt.Sprintf(" --%s=", argName),
	}
	for _, pattern := range patterns {
		idx := strings.Index(strings.ToLower(commandLine), strings.ToLower(pattern))
		if idx < 0 {
			continue
		}
		value := commandLine[idx+len(pattern):]
		if strings.HasSuffix(pattern, `"`) {
			if end := strings.Index(value, `"`); end >= 0 {
				return strings.TrimSpace(value[:end])
			}
			continue
		}
		if end := strings.IndexAny(value, " \t\r\n"); end >= 0 {
			return strings.Trim(strings.TrimSpace(value[:end]), `"`)
		}
		return strings.Trim(strings.TrimSpace(value), `"`)
	}
	return ""
}
