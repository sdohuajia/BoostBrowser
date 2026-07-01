package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPatchChromePreferencesFileDisablesSessionRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Preferences")
	initial := map[string]any{
		"session": map[string]any{
			"restore_on_startup": float64(1),
			"startup_urls":       []any{"chrome-extension://abcdef/options.html"},
		},
		"profile": map[string]any{
			"exited_cleanly": false,
			"exit_type":      "Crashed",
		},
	}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := patchChromePreferencesFile(path); err != nil {
		t.Fatal(err)
	}
	outData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(outData, &out); err != nil {
		t.Fatal(err)
	}
	session := out["session"].(map[string]any)
	if got := session["restore_on_startup"]; got != float64(5) {
		t.Fatalf("restore_on_startup=%v, want 5", got)
	}
	if _, ok := session["startup_urls"]; ok {
		t.Fatalf("startup_urls should be removed")
	}
	profile := out["profile"].(map[string]any)
	if profile["exited_cleanly"] != true || profile["exit_type"] != "Normal" {
		t.Fatalf("profile clean exit flags not patched: %#v", profile)
	}
	// 默认搜索引擎现在由 seedDefaultSearchEngine 接管（需要 Web Data 文件），
	// patchChromePreferencesFile 不再负责 search provider 字段。
}

func TestSanitizeChromeStartupPreferencesDoesNotCreateSearchProvider(t *testing.T) {
	dir := t.TempDir()
	sanitizeChromeStartupPreferences(dir)
	prefsPath := filepath.Join(dir, "Default", "Preferences")
	outData, err := os.ReadFile(prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(outData, &out); err != nil {
		t.Fatal(err)
	}
	// sanitize 路径不再写默认搜索引擎；那由 seedDefaultSearchEngine 在
	// chrome.exe 启动前以 Web Data SQLite 为权威源的方式处理。
	if _, ok := out["default_search_provider"]; ok {
		t.Fatalf("sanitize should not create default_search_provider; got %#v", out["default_search_provider"])
	}
}

func TestPatchChromePreferencesFileKeepsExistingSearchProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Preferences")
	initial := map[string]any{
		"default_search_provider": map[string]any{
			"enabled":    true,
			"name":       "Custom",
			"keyword":    "custom.local",
			"search_url": "https://custom.local/search?q={searchTerms}",
		},
	}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := patchChromePreferencesFile(path); err != nil {
		t.Fatal(err)
	}
	outData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(outData, &out); err != nil {
		t.Fatal(err)
	}
	provider := out["default_search_provider"].(map[string]any)
	if provider["name"] != "Custom" || provider["search_url"] != "https://custom.local/search?q={searchTerms}" {
		t.Fatalf("existing search provider should be preserved: %#v", provider)
	}
}

func TestIsExtensionStartupURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"chrome-extension://nkbihfbeogaeaoehlefnkodbefgpgknn/home.html", true},
		{"chrome-extension://ekaiemolceheaedaknpealhgfjljmica/home.html#/unlock", true},
		{"chrome://extensions/", true},
		{"https://example.com/chrome-extension://not-a-scheme", false},
		{"about:blank", false},
	}
	for _, tc := range cases {
		if got := isExtensionStartupURL(tc.url); got != tc.want {
			t.Fatalf("isExtensionStartupURL(%q)=%v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestRemoveChromeSessionRestoreFiles(t *testing.T) {
	dir := t.TempDir()
	sessionsDir := filepath.Join(dir, "Default", "Sessions")
	profileSessionsDir := filepath.Join(dir, "Profile 1", "Sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(profileSessionsDir, 0755); err != nil {
		t.Fatal(err)
	}
	files := []string{
		filepath.Join(sessionsDir, "Session_123"),
		filepath.Join(sessionsDir, "Tabs_123"),
		filepath.Join(sessionsDir, "keep.txt"),
		filepath.Join(dir, "Default", "Last Session"),
		filepath.Join(dir, "Default", "Last Tabs"),
		filepath.Join(profileSessionsDir, "Session_456"),
		filepath.Join(profileSessionsDir, "Tabs_456"),
		filepath.Join(dir, "Profile 1", "Current Session"),
		filepath.Join(dir, "Profile 1", "Current Tabs"),
	}
	for _, file := range files {
		if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	removeChromeSessionRestoreFiles(dir)
	for _, file := range []string{files[0], files[1], files[3], files[4], files[5], files[6], files[7], files[8]} {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("session restore file should be removed: %s", file)
		}
	}
	if _, err := os.Stat(files[2]); err != nil {
		t.Fatalf("unrelated session dir file should stay: %v", err)
	}
}

func TestIsAutoExtensionStartupTarget(t *testing.T) {
	cases := []struct {
		target cdpTarget
		want   bool
	}{
		{cdpTarget{Type: "page", URL: "chrome-extension://ekaiemolceheaedaknpealhgfjljmica/home.html#/unlock"}, true},
		{cdpTarget{Type: "other", URL: "chrome-extension://mcohilncbfahbmgdjkbpemcciiolgcge/popup.html"}, true},
		{cdpTarget{Type: "iframe", URL: "chrome-extension://abcdef/home.html"}, false},
		{cdpTarget{Type: "page", URL: "https://metamask.io/"}, false},
	}
	for _, tc := range cases {
		if got := isAutoExtensionStartupTarget(tc.target); got != tc.want {
			t.Fatalf("isAutoExtensionStartupTarget(%#v)=%v, want %v", tc.target, got, tc.want)
		}
	}
}
