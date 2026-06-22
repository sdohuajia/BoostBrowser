package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"boost-browser/backend/internal/logger"

	"github.com/gorilla/websocket"
)

// sanitizeChromeStartupPreferences prevents Chrome from restoring extension
// welcome/options tabs that were left open when the previous browser session was
// closed. Browser profiles in Boost Browser should start from the app-provided
// URL/default page, not from Chrome's last-session restore list.
func sanitizeChromeStartupPreferences(userDataDir string) {
	if strings.TrimSpace(userDataDir) == "" {
		return
	}
	defaultPrefsPath := filepath.Join(userDataDir, "Default", "Preferences")
	_ = ensureChromePreferencesFile(defaultPrefsPath)
	for _, rel := range []string{
		filepath.Join("Default", "Preferences"),
		"Preferences",
	} {
		path := filepath.Join(userDataDir, rel)
		_ = patchChromePreferencesFile(path)
	}
	removeChromeSessionRestoreFiles(userDataDir)
}

func ensureChromePreferencesFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("{}"), 0644)
}

func removeChromeSessionRestoreFiles(userDataDir string) {
	// Chrome 的实际 profile 目录通常是 Default，但用户/内核也可能通过
	// profile-directory 或迁移数据生成 Profile 1/Guest Profile 等目录。逐个清理
	// user-data-dir 下的 profile 会话文件，避免遗漏已恢复的扩展登录/欢迎页。
	profileDirs := []string{userDataDir, filepath.Join(userDataDir, "Default")}
	if entries, err := os.ReadDir(userDataDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(entry.Name()))
			if name == "default" || strings.HasPrefix(name, "profile ") || strings.Contains(name, "profile") {
				profileDirs = append(profileDirs, filepath.Join(userDataDir, entry.Name()))
			}
		}
	}

	seen := map[string]bool{}
	for _, dir := range profileDirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		_ = removeSessionFilesInDir(filepath.Join(dir, "Sessions"))
		for _, name := range []string{"Current Session", "Current Tabs", "Last Session", "Last Tabs"} {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func removeSessionFilesInDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasPrefix(name, "session_") || strings.HasPrefix(name, "tabs_") {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	return nil
}

func patchChromePreferencesFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return err
	}
	var prefs map[string]any
	if err := json.Unmarshal(data, &prefs); err != nil {
		return err
	}

	changed := false
	browserPrefs := ensureJSONMap(prefs, "browser")
	if browserPrefs["check_default_browser"] != false {
		browserPrefs["check_default_browser"] = false
		changed = true
	}

	sessionPrefs := ensureJSONMap(prefs, "session")
	// 5 = New Tab Page. This disables "Continue where you left off" for this
	// managed profile so extension onboarding/options tabs are not resurrected.
	if sessionPrefs["restore_on_startup"] != float64(5) {
		sessionPrefs["restore_on_startup"] = 5
		changed = true
	}
	if _, ok := sessionPrefs["startup_urls"]; ok {
		delete(sessionPrefs, "startup_urls")
		changed = true
	}

	profilePrefs := ensureJSONMap(prefs, "profile")
	if profilePrefs["exited_cleanly"] != true {
		profilePrefs["exited_cleanly"] = true
		changed = true
	}
	if profilePrefs["exit_type"] != "Normal" {
		profilePrefs["exit_type"] = "Normal"
		changed = true
	}

	// 默认搜索引擎由 seedDefaultSearchEngine（chrome_search_engine_seed.go）处理，
	// 那条路径会同时写 Web Data + Preferences 的 mirrored_template_url_data，
	// 与 cloak 内核 UI 操作产生的字段名一致。这里不再重复写。

	if !changed {
		return nil
	}
	out, err := json.MarshalIndent(prefs, "", "   ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

// ensureDefaultSearchProvider 已废弃。改用 seedDefaultSearchEngine
// （chrome_search_engine_seed.go）—— 那条路径会同时写 Web Data 和
// Preferences 的 mirrored_template_url_data，与 cloak 内核 UI 操作
//一致。保留空实现避免历史调用方编译失败。
func ensureDefaultSearchProvider(prefs map[string]any) bool {
	return false
}

func asJSONString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func ensureJSONMap(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	created := map[string]any{}
	parent[key] = created
	return created
}

// closeExtensionStartupPages suppresses extension UI created automatically during
// Chrome startup. Some wallets open onboarding/unlock pages a moment after the
// debug port becomes ready; a single immediate pass misses them. Keep this window
// short and synchronous before navigating to user pages so manually-clicked wallet
// popups later are not closed.
func closeExtensionStartupPages(debugPort int, profileId string) {
	deadline := time.Now().Add(4 * time.Second)
	closed := 0
	seenClosed := map[string]bool{}
	for {
		closed += closeExtensionStartupPagesOnce(debugPort, seenClosed)
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if closed > 0 {
		logger.New("Browser").Info("已关闭启动时自动弹出的扩展页面/窗口", logger.F("profile_id", profileId), logger.F("count", closed))
	}
}

func finalizeBrowserStartupExtensionSuppression(debugPort int, pid int, profileId string) {
	if debugPort <= 0 {
		return
	}
	closeExtensionStartupPages(debugPort, profileId)
	if pid > 0 {
		restoreBrowserWindowsAfterStartup(pid)
	}
}

func closeExtensionStartupPagesOnce(debugPort int, seenClosed map[string]bool) int {
	targets, err := listCDPTargets(debugPort)
	if err != nil {
		return 0
	}

	browserWsURL, err := getBrowserWebSocketURL(debugPort)
	if err != nil {
		return 0
	}
	browserConn, _, err := websocket.DefaultDialer.Dial(browserWsURL, nil)
	if err != nil {
		return 0
	}
	defer browserConn.Close()
	browserConn.SetReadDeadline(time.Now().Add(10 * time.Second))

	msgID := 3000
	closed := 0
	for _, target := range targets {
		if target.ID == "" || seenClosed[target.ID] || !isAutoExtensionStartupTarget(target) {
			continue
		}
		msgID++
		closeMsg := cdpMessage{
			Id:     msgID,
			Method: "Target.closeTarget",
			Params: map[string]any{"targetId": target.ID},
		}
		if err := browserConn.WriteJSON(closeMsg); err != nil {
			continue
		}
		var resp cdpResponse
		_ = browserConn.ReadJSON(&resp)
		if resp.Error == nil {
			seenClosed[target.ID] = true
			closed++
		}
	}
	return closed
}

func listCDPTargets(debugPort int) ([]cdpTarget, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", debugPort))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var targets []cdpTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func isExtensionStartupURL(rawURL string) bool {
	u := strings.TrimSpace(strings.ToLower(rawURL))
	if u == "" {
		return false
	}
	return strings.HasPrefix(u, "chrome-extension://") || strings.HasPrefix(u, "chrome://extensions")
}

func isAutoExtensionStartupTarget(target cdpTarget) bool {
	typeName := strings.TrimSpace(strings.ToLower(target.Type))
	// During the bounded startup suppression window, close both main extension tabs
	// (type=page) and automatically-created popup/window targets (often type=other).
	// The caller only runs this before normal navigation/interaction, so later user
	// clicks on wallet extensions are unaffected.
	if typeName != "page" && typeName != "other" {
		return false
	}
	return isExtensionStartupURL(target.URL)
}
