package backend

import (
	"boost-browser/backend/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupEnsureZipSuffix(t *testing.T) {
	if got := backupEnsureZipSuffix("c:/tmp/a.zip"); got != "c:/tmp/a.zip" {
		t.Fatalf("zip 后缀重复追加: %s", got)
	}
	if got := backupEnsureZipSuffix("c:/tmp/a"); got != "c:/tmp/a.zip" {
		t.Fatalf("zip 后缀追加失败: %s", got)
	}
}

func TestBackupMergeConfigDedup(t *testing.T) {
	current := config.DefaultConfig()
	current.App.MaxProfileLimit = 12
	current.App.UsedCDKeys = []string{"A1", "B2"}
	current.Browser.DefaultBookmarks = []config.BrowserBookmark{
		{Name: "Google", URL: "https://www.google.com/"},
	}
	current.Browser.Proxies = []config.BrowserProxy{
		{ProxyId: "p1", ProxyName: "P1", ProxyConfig: "http://127.0.0.1:7890"},
	}
	current.Browser.Cores = []config.BrowserCore{
		{CoreId: "c1", CoreName: "C1", CorePath: "chrome/c1"},
	}
	current.Browser.Profiles = []config.BrowserProfileConfig{
		{ProfileId: "u1", ProfileName: "U1", UserDataDir: "u1"},
	}

	incoming := config.DefaultConfig()
	incoming.App.UsedCDKeys = []string{"b2", "C3"}
	incoming.Browser.DefaultBookmarks = []config.BrowserBookmark{
		{Name: "Google Dup", URL: "https://www.google.com/"},
		{Name: "ChatGPT", URL: "https://chatgpt.com/"},
	}
	incoming.Browser.Proxies = []config.BrowserProxy{
		{ProxyId: "p1", ProxyName: "P1 Dup", ProxyConfig: "http://127.0.0.1:7890"},
		{ProxyId: "p2", ProxyName: "P2", ProxyConfig: "socks5://127.0.0.1:1080"},
	}
	incoming.Browser.Cores = []config.BrowserCore{
		{CoreId: "c1", CoreName: "C1 Dup", CorePath: "chrome/c1"},
		{CoreId: "c2", CoreName: "C2", CorePath: "chrome/c2"},
	}
	incoming.Browser.Profiles = []config.BrowserProfileConfig{
		{ProfileId: "u1", ProfileName: "U1 Dup", UserDataDir: "u1"},
		{ProfileId: "u2", ProfileName: "U2", UserDataDir: "u2"},
	}

	merged := backupMergeConfig(current, incoming)
	if merged == nil {
		t.Fatalf("merged 为空")
	}

	if merged.App.MaxProfileLimit != 12 {
		t.Fatalf("license limit 不应被导入配置改写: got=%d", merged.App.MaxProfileLimit)
	}
	if len(merged.App.UsedCDKeys) != 2 {
		t.Fatalf("used cd keys 不应被导入配置改写: %+v", merged.App.UsedCDKeys)
	}
	if len(merged.Browser.DefaultBookmarks) != 2 {
		t.Fatalf("bookmarks 判重失败: %+v", merged.Browser.DefaultBookmarks)
	}
	if len(merged.Browser.Proxies) != 2 {
		t.Fatalf("proxies 判重失败: %+v", merged.Browser.Proxies)
	}
	if len(merged.Browser.Cores) != 2 {
		t.Fatalf("cores 判重失败: %+v", merged.Browser.Cores)
	}
	if len(merged.Browser.Profiles) != 2 {
		t.Fatalf("profiles 判重失败: %+v", merged.Browser.Profiles)
	}
}

func TestBackupSyncDirConflictAndOverwrite(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}

	srcFile := filepath.Join(src, "a.txt")
	dstFile := filepath.Join(dst, "a.txt")
	if err := os.WriteFile(srcFile, []byte("new-content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dstFile, []byte("old-content"), 0644); err != nil {
		t.Fatal(err)
	}

	stats := &backupMergeStats{}
	if err := backupSyncDir(src, dst, false, stats, nil); err != nil {
		t.Fatal(err)
	}
	if stats.Conflicts != 1 || stats.Imported != 0 {
		t.Fatalf("非覆盖模式统计异常: %+v", stats)
	}
	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-content" {
		t.Fatalf("非覆盖模式不应改写目标文件: %s", string(got))
	}

	stats2 := &backupMergeStats{}
	if err := backupSyncDir(src, dst, true, stats2, nil); err != nil {
		t.Fatal(err)
	}
	if stats2.Imported != 1 {
		t.Fatalf("覆盖模式导入统计异常: %+v", stats2)
	}
	got2, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got2) != "new-content" {
		t.Fatalf("覆盖模式应改写目标文件: %s", string(got2))
	}
}
