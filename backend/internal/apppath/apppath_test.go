package apppath

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

func TestResolveReadOnlyLinuxInstallUsesUserDataRoot(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("linux-only path behavior")
	}

	xdgDataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdgDataHome)

	installRoot := filepath.Join(t.TempDir(), "opt-app")
	if err := os.MkdirAll(filepath.Join(installRoot, "bin"), 0755); err != nil {
		t.Fatalf("创建 installRoot 失败: %v", err)
	}
	if err := os.Chmod(installRoot, 0555); err != nil {
		t.Fatalf("设置 installRoot 权限失败: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(installRoot, 0755)
		_ = os.Chmod(filepath.Join(installRoot, "bin"), 0755)
	})

	configPath := resolveForOS(installRoot, "config.yaml", "linux")
	binPath := resolveForOS(installRoot, "bin/xray", "linux")

	expectedStateRoot := filepath.Join(xdgDataHome, appStateDirName)
	if !strings.HasPrefix(configPath, expectedStateRoot+string(os.PathSeparator)) {
		t.Fatalf("config path 应落到用户目录，got=%s want-prefix=%s", configPath, expectedStateRoot)
	}
	if binPath != filepath.Join(installRoot, "bin", "xray") {
		t.Fatalf("bin path 不应迁移到用户目录，got=%s", binPath)
	}
}

func TestResolveDarwinAppBundleUsesApplicationSupportStateRoot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	installRoot := filepath.Join(t.TempDir(), "Boost Browser.app", "Contents", "MacOS")
	if err := os.MkdirAll(filepath.Join(installRoot, "bin"), 0755); err != nil {
		t.Fatalf("创建 installRoot 失败: %v", err)
	}

	configPath := resolveForOS(installRoot, "config.yaml", "darwin")
	binPath := resolveForOS(installRoot, "bin/xray", "darwin")
	expectedStateRoot := filepath.Join(homeDir, "Library", "Application Support", appStateDirName)

	if configPath != filepath.Join(expectedStateRoot, "config.yaml") {
		t.Fatalf("darwin config path 应落到 Application Support，got=%s want=%s", configPath, filepath.Join(expectedStateRoot, "config.yaml"))
	}
	if binPath != filepath.Join(installRoot, "bin", "xray") {
		t.Fatalf("darwin bin path 不应迁移到用户目录，got=%s", binPath)
	}

	root := detectForOS(installRoot, "darwin")
	if !root.detached {
		t.Fatal("expected darwin .app bundle root to use detached state")
	}
	if root.stateRoot != expectedStateRoot {
		t.Fatalf("unexpected darwin state root: got=%s want=%s", root.stateRoot, expectedStateRoot)
	}
}

func TestEnsureWritableLayoutSeedsConfigAndChrome(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("linux-only path behavior")
	}

	xdgDataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdgDataHome)

	installRoot := filepath.Join(t.TempDir(), "opt-app")
	if err := os.MkdirAll(filepath.Join(installRoot, "chrome"), 0755); err != nil {
		t.Fatalf("创建 chrome 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "config.yaml"), []byte("name: linux\n"), 0644); err != nil {
		t.Fatalf("写入 config.yaml 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "chrome", "README.md"), []byte("placeholder\n"), 0644); err != nil {
		t.Fatalf("写入 README 失败: %v", err)
	}
	if err := os.Chmod(installRoot, 0555); err != nil {
		t.Fatalf("设置 installRoot 权限失败: %v", err)
	}
	if err := os.Chmod(filepath.Join(installRoot, "chrome"), 0555); err != nil {
		t.Fatalf("设置 chrome 目录权限失败: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(installRoot, 0755)
		_ = os.Chmod(filepath.Join(installRoot, "chrome"), 0755)
	})

	if err := ensureWritableLayoutForOS(installRoot, "linux"); err != nil {
		t.Fatalf("EnsureWritableLayout 返回错误: %v", err)
	}

	stateRoot := filepath.Join(xdgDataHome, appStateDirName)
	assertFileContent(t, filepath.Join(stateRoot, "config.yaml"), "name: linux\n")
	assertFileContent(t, filepath.Join(stateRoot, "chrome", "README.md"), "placeholder\n")
	if _, err := os.Stat(filepath.Join(stateRoot, "data")); err != nil {
		t.Fatalf("data 目录未创建: %v", err)
	}
}

func TestEnsureWritableLayoutSeedsDarwinBundleStateRoot(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	installRoot := filepath.Join(t.TempDir(), "Boost Browser.app", "Contents", "MacOS")
	if err := os.MkdirAll(filepath.Join(installRoot, "chrome"), 0755); err != nil {
		t.Fatalf("创建 chrome 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "config.yaml"), []byte("name: mac\n"), 0644); err != nil {
		t.Fatalf("写入 config.yaml 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "chrome", "README.md"), []byte("mac placeholder\n"), 0644); err != nil {
		t.Fatalf("写入 README 失败: %v", err)
	}

	if err := ensureWritableLayoutForOS(installRoot, "darwin"); err != nil {
		t.Fatalf("ensureWritableLayoutForOS 返回错误: %v", err)
	}

	stateRoot := filepath.Join(homeDir, "Library", "Application Support", appStateDirName)
	assertFileContent(t, filepath.Join(stateRoot, "config.yaml"), "name: mac\n")
	assertFileContent(t, filepath.Join(stateRoot, "chrome", "README.md"), "mac placeholder\n")
	if _, err := os.Stat(filepath.Join(stateRoot, "data")); err != nil {
		t.Fatalf("data 目录未创建: %v", err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败 %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("文件内容不符合预期 %s: got=%q want=%q", path, string(data), want)
	}
}
