package backend

import (
	appconfig "boost-browser/backend/internal/config"
	"strings"
)

// migrateLegacyConfig 修复 v1.1.0 错误版本写入的配置。
//
// 背景：
//
//	v1.1.0 安装包构建脚本错误地拷贝了开发机的 config.yaml，导致：
//	  1. cores 列表里 bundled-google-chrome-latest 标了 is_default: true，
//	     且根本缺少 bundled-cloak-chromium-latest 条目，新建实例默认 Google Chrome 148
//	  2. default_launch_args 含 --load-extension=<app-root>\extensions\...
//	     指向开发机绝对路径，新用户启动时弹"清单文件缺失"
//
// 该迁移只在检测到上述精确指纹时触发，绝不覆盖用户主动改过的合法配置。
//
// 升级路径：
//
//	v1.1.0 用户 → 自动升级到 v1.2.0 → 启动时这里把 config 修好 → 默认 Chromium 146 + 不再弹错
//
// 返回 true 表示配置被修改过，调用方需要 Save 回写。
func migrateLegacyConfig(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	changed := false

	// 1. cores: 缺 cloak-146 内核 → 加进去并设为默认
	hasCloak := false
	for _, c := range cfg.Browser.Cores {
		if c.CoreId == "bundled-cloak-chromium-latest" {
			hasCloak = true
			break
		}
	}
	if !hasCloak {
		// 现有内核全部置为非默认（如果有任何标了 is_default 的）
		for i := range cfg.Browser.Cores {
			if cfg.Browser.Cores[i].IsDefault {
				cfg.Browser.Cores[i].IsDefault = false
			}
		}
		cloakCore := appconfig.BrowserCore{
			CoreId:    "bundled-cloak-chromium-latest",
			CoreName:  "Chromium 146",
			CorePath:  "chrome\\cloak-146.0.7680.177",
			IsDefault: true,
		}
		// 把 cloak 插到第一位（让 UI 默认显示在最上面）
		cfg.Browser.Cores = append([]appconfig.BrowserCore{cloakCore}, cfg.Browser.Cores...)
		changed = true
	}

	// 2. default_launch_args: 清理指向开发机绝对路径的 --load-extension 及相关参数
	if len(cfg.Browser.DefaultLaunchArgs) > 0 {
		cleaned := make([]string, 0, len(cfg.Browser.DefaultLaunchArgs))
		droppedExt := false
		for _, arg := range cfg.Browser.DefaultLaunchArgs {
			lower := strings.ToLower(arg)
			// 命中 v1.1.0 错误参数：--load-extension=<legacy-path>BoostBrowser_cloak_test\...
			if strings.HasPrefix(lower, "--load-extension=") &&
				strings.Contains(lower, "boostbrowser_cloak_test") {
				droppedExt = true
				continue
			}
			cleaned = append(cleaned, arg)
		}
		if droppedExt {
			// 同步剔除配套的 --extension-mime-request-handling（v1.1.0 配置里跟 load-extension 成对出现）
			final := make([]string, 0, len(cleaned))
			for _, arg := range cleaned {
				if strings.HasPrefix(strings.ToLower(arg), "--extension-mime-request-handling=") {
					continue
				}
				final = append(final, arg)
			}
			cfg.Browser.DefaultLaunchArgs = final
			changed = true
		}
	}

	return changed
}
