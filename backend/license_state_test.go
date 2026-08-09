package backend

import (
	appconfig "boost-browser/backend/internal/config"
	"path/filepath"
	"testing"
)

func TestLoadConfigRestoresLocalLicenseState(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")

	cfg := appconfig.DefaultConfig()
	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}
	if err := saveLocalLicenseState(configPath, &localLicenseState{
		MaxProfileLimit: appconfig.GithubStarProfileTotal + appconfig.StandardCDKeyProfileBonus,
		UsedCDKeys:      []string{"GITHUB_STAR_REWARD", "BOOST-AAAA-BBBB-CCCC-DDDD-EEEEEEEE"},
	}); err != nil {
		t.Fatalf("写入本机额度状态失败: %v", err)
	}

	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}

	if loaded.App.MaxProfileLimit != appconfig.GithubStarProfileTotal+appconfig.StandardCDKeyProfileBonus {
		t.Fatalf("本机额度状态未恢复: got=%d", loaded.App.MaxProfileLimit)
	}
	if len(loaded.App.UsedCDKeys) != 2 {
		t.Fatalf("兑换记录未恢复: %+v", loaded.App.UsedCDKeys)
	}
}

func TestLoadConfigSeedsLocalLicenseStateFromConfig(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")

	cfg := appconfig.DefaultConfig()
	cfg.App.MaxProfileLimit = appconfig.GithubStarProfileTotal
	cfg.App.UsedCDKeys = []string{"GITHUB_STAR_REWARD"}
	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	if loaded.App.MaxProfileLimit != appconfig.GithubStarProfileTotal {
		t.Fatalf("LoadConfig 读取额度失败: got=%d", loaded.App.MaxProfileLimit)
	}

	state, exists, err := loadLocalLicenseState(configPath)
	if err != nil {
		t.Fatalf("读取本机额度状态失败: %v", err)
	}
	if !exists {
		t.Fatalf("应当从现有配置补建本机额度状态")
	}
	if state.MaxProfileLimit != appconfig.GithubStarProfileTotal {
		t.Fatalf("本机额度状态未补建: got=%d", state.MaxProfileLimit)
	}
	if len(state.UsedCDKeys) != 1 || state.UsedCDKeys[0] != "GITHUB_STAR_REWARD" {
		t.Fatalf("本机兑换记录未补建: %+v", state.UsedCDKeys)
	}
}

func TestRedeemGithubStarPersistsLocalLicenseState(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")

	cfg := appconfig.DefaultConfig()
	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	app := NewApp(root)
	app.config = cfg

	if err := app.RedeemGithubStar(); err != nil {
		t.Fatalf("RedeemGithubStar 失败: %v", err)
	}

	state, exists, err := loadLocalLicenseState(configPath)
	if err != nil {
		t.Fatalf("读取本机额度状态失败: %v", err)
	}
	if !exists {
		t.Fatalf("兑换后应写入本机额度状态")
	}
	if state.MaxProfileLimit != appconfig.GithubStarProfileTotal {
		t.Fatalf("兑换后本机额度状态错误: got=%d", state.MaxProfileLimit)
	}
	if len(state.UsedCDKeys) != 1 || state.UsedCDKeys[0] != "GITHUB_STAR_REWARD" {
		t.Fatalf("兑换后本机兑换记录错误: %+v", state.UsedCDKeys)
	}
}
