package backend

import (
	"boost-browser/backend/internal/config"
	"path/filepath"
	"testing"
)

func TestReloadConfigLoadsFromDisk(t *testing.T) {
	root := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.App.Name = "Reload-Test-App"
	if err := cfg.Save(filepath.Join(root, "config.yaml")); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	app := NewApp(root)
	app.config = config.DefaultConfig()

	if err := app.ReloadConfig(); err != nil {
		t.Fatalf("ReloadConfig 失败: %v", err)
	}

	if app.config == nil {
		t.Fatalf("ReloadConfig 后 config 为空")
	}
	if app.config.App.Name != "Reload-Test-App" {
		t.Fatalf("ReloadConfig 未生效，got=%q", app.config.App.Name)
	}
}

func TestReloadConfigKeepsLocalLicenseState(t *testing.T) {
	root := t.TempDir()

	cfg := config.DefaultConfig()
	if err := cfg.Save(filepath.Join(root, "config.yaml")); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}
	if err := saveLocalLicenseState(filepath.Join(root, "config.yaml"), &localLicenseState{
		MaxProfileLimit: config.GithubStarProfileTotal + config.StandardCDKeyProfileBonus,
		UsedCDKeys:      []string{"BOOST-AAAA-BBBB-CCCC-DDDD-EEEEEEEE", "GITHUB_STAR_REWARD"},
	}); err != nil {
		t.Fatalf("写入本机额度状态失败: %v", err)
	}

	app := NewApp(root)
	app.config = config.DefaultConfig()

	if err := app.ReloadConfig(); err != nil {
		t.Fatalf("ReloadConfig 失败: %v", err)
	}

	if app.config.App.MaxProfileLimit != config.GithubStarProfileTotal+config.StandardCDKeyProfileBonus {
		t.Fatalf("ReloadConfig 未恢复本机额度状态: got=%d", app.config.App.MaxProfileLimit)
	}
	if len(app.config.App.UsedCDKeys) != 2 {
		t.Fatalf("ReloadConfig 未恢复兑换记录: %+v", app.config.App.UsedCDKeys)
	}
}
