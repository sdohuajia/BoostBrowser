//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const hubSDKProductionDataRoot = `C:\BoostBrowserTest\data`

type hubSDKWindow struct {
	Hwnd      int64  `json:"hwnd"`
	PID       int    `json:"pid"`
	Title     string `json:"title"`
	ClassName string `json:"class_name"`
	Path      string `json:"path"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

func hubSDKProfileID(hwnd int64) string {
	return "hubsdk:" + strconv.FormatInt(hwnd, 10)
}

func isHubSDKProfileID(profileID string) bool {
	return strings.HasPrefix(strings.TrimSpace(profileID), "hubsdk:")
}

func (a *App) hubSDKBridgePath() string {
	if a == nil || strings.TrimSpace(a.appRoot) == "" {
		return ""
	}
	path := filepath.Join(a.appRoot, "hubsdk", "boost_hubsdk_sync_bridge.py")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

func (a *App) hubSDKBridgeAvailable() bool {
	return a.hubSDKBridgePath() != ""
}

// hubSDKBridgeRun invokes only the bridge shipped beside the isolated HubSDK test
// build. The bridge itself maintains a narrow allowlist: this test data root and
// the explicitly configured production Boost Browser data root, never arbitrary
// Chrome/Chromium windows.
func (a *App) hubSDKBridgeRun(timeout time.Duration, args ...string) ([]byte, error) {
	bridge := a.hubSDKBridgePath()
	if bridge == "" {
		return nil, fmt.Errorf("HubSDK bridge is unavailable in this installation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmdArgs := append([]string{"-3", bridge}, args...)
	cmd := exec.CommandContext(ctx, "py", cmdArgs...)
	cmd.Dir = filepath.Dir(bridge)
	cmd.Env = append(os.Environ(),
		"BOOST_BROWSER_TEST_ROOT="+a.appRoot,
		"BOOST_BROWSER_SOURCE_DATA_ROOT="+hubSDKProductionDataRoot,
	)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("HubSDK bridge timed out after %s", timeout)
	}
	if err != nil {
		return out, fmt.Errorf("HubSDK bridge failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (a *App) getHubSDKSyncProfiles() ([]SyncProfileInfo, error) {
	out, err := a.hubSDKBridgeRun(8*time.Second, "list-windows", "--json")
	if err != nil {
		return nil, err
	}
	var windows []hubSDKWindow
	if err := json.Unmarshal(out, &windows); err != nil {
		return nil, fmt.Errorf("invalid HubSDK window list: %w", err)
	}
	profiles := make([]SyncProfileInfo, 0, len(windows))
	for _, window := range windows {
		if window.Hwnd == 0 || window.PID <= 0 {
			continue
		}
		name := strings.TrimSpace(window.Title)
		if name == "" {
			name = fmt.Sprintf("HubSDK 窗口 %d", window.PID)
		}
		profiles = append(profiles, SyncProfileInfo{
			ProfileId:   hubSDKProfileID(window.Hwnd),
			ProfileName: name,
			Pid:         window.PID,
			Hwnd:        window.Hwnd,
			Running:     true,
			Status:      "running",
			Source:      "hubsdk",
		})
	}
	return profiles, nil
}

func (a *App) startHubSDKInputSync(masterProfileID string, followerProfileIDs []string) error {
	profiles, err := a.getHubSDKSyncProfiles()
	if err != nil {
		return err
	}
	byID := make(map[string]SyncProfileInfo, len(profiles))
	for _, profile := range profiles {
		byID[profile.ProfileId] = profile
	}
	master, ok := byID[masterProfileID]
	if !ok {
		return fmt.Errorf("未找到 HubSDK 主控窗口：%s", masterProfileID)
	}
	followerHwnds := make([]string, 0, len(followerProfileIDs))
	validFollowerIDs := make([]string, 0, len(followerProfileIDs))
	for _, followerID := range followerProfileIDs {
		if followerID == masterProfileID {
			continue
		}
		follower, ok := byID[followerID]
		if !ok || follower.Hwnd == 0 {
			continue
		}
		followerHwnds = append(followerHwnds, strconv.FormatInt(follower.Hwnd, 10))
		validFollowerIDs = append(validFollowerIDs, followerID)
	}
	if len(followerHwnds) == 0 {
		return fmt.Errorf("没有可用的 HubSDK 跟随实例")
	}
	if _, err := a.hubSDKBridgeRun(15*time.Second, "start-sync", "--main", strconv.FormatInt(master.Hwnd, 10), "--slaves", strings.Join(followerHwnds, ",")); err != nil {
		return err
	}
	syncState.mu.Lock()
	syncState.syncer = nil
	syncState.masterHwnd = 0
	syncState.masterId = masterProfileID
	syncState.followerIds = validFollowerIDs
	syncState.active = true
	syncState.hubSDK = true
	syncState.mu.Unlock()
	return nil
}

func (a *App) stopHubSDKInputSync() error {
	_, err := a.hubSDKBridgeRun(10*time.Second, "stop-sync")
	if err != nil {
		return err
	}
	// The SDK owns hooks and a localhost service. Stop it too, matching the
	// previously documented HubSDK stop batch, so no background sync process leaks.
	_, stopSDKError := a.hubSDKBridgeRun(10*time.Second, "stop-sdk")
	return stopSDKError
}
