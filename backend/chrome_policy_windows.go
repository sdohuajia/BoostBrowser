//go:build windows

package backend

import (
	"boost-browser/backend/internal/logger"

	"golang.org/x/sys/windows/registry"
)

// applyChromeEnterprisePolicies 写入 Chrome / Chromium 企业策略到 HKCU。
//
// 主要目的：关闭 "您使用的是不受支持的命令行标记: --no-sandbox" 的黄色 infobar。
// Cloak 内核必须 --no-sandbox 才能跑（patched chromium 默认关闭了沙箱通道），
// 但 Chrome 会对 unsupported flag 显示安全警告。Google 官方提供企业策略
// CommandLineFlagSecurityWarningsEnabled 来抑制该 infobar，写到 HKCU 不需要管理员，
// 也不会被 fingerprint.com 等检测当作 bot 信号（与 --test-type 不同）。
//
// 同时写 Google\Chrome 和 Chromium 两个分支，覆盖 cloak 内核（Chromium）和原版 Chrome。
func applyChromeEnterprisePolicies() {
	log := logger.New("ChromePolicy")
	keys := []string{
		`Software\Policies\Google\Chrome`,
		`Software\Policies\Chromium`,
	}
	for _, path := range keys {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
		if err != nil {
			log.Debug("create policy key failed",
				logger.F("path", path),
				logger.F("error", err.Error()),
			)
			continue
		}
		// 0 = 关闭命令行 flag 安全警告 infobar（"--no-sandbox" 黄条等）
		if err := k.SetDWordValue("CommandLineFlagSecurityWarningsEnabled", 0); err != nil {
			log.Debug("set policy value failed",
				logger.F("path", path),
				logger.F("key", "CommandLineFlagSecurityWarningsEnabled"),
				logger.F("error", err.Error()),
			)
		}
		// 默认搜索引擎策略：让地址栏输入非 URL 时走 Google 搜索。
		// ungoogled-chromium / cloak 内核默认不带任何搜索引擎，
		// 不设的话 typing "foo" 会被当成域名访问 http://foo 失败。
		if err := k.SetDWordValue("DefaultSearchProviderEnabled", 1); err != nil {
			log.Debug("set policy value failed",
				logger.F("path", path),
				logger.F("key", "DefaultSearchProviderEnabled"),
				logger.F("error", err.Error()),
			)
		}
		// 注意：cloak 内核会扫描 google.com 字面量并拒绝（包括 policy URL！），
		// 所以必须用 cloak 自己的混淆域 9oo91e.qjz9zk —— 网络层会自动重写到真正
		// 的 google.com。和 keywords 表里 prepopulated id=10 (Gemini) / id=12
		// (Google AI 模式) 同样的用法。
		searchPolicies := map[string]string{
			"DefaultSearchProviderName":         "Google",
			"DefaultSearchProviderKeyword":      "9oo91e.qjz9zk",
			"DefaultSearchProviderSearchURL":    "https://www.9oo91e.qjz9zk/search?q={searchTerms}",
			"DefaultSearchProviderSuggestURL":   "https://www.9oo91e.qjz9zk/complete/search?output=chrome&q={searchTerms}",
			"DefaultSearchProviderIconURL":      "https://www.9oo91e.qjz9zk/favicon.ico",
			"DefaultSearchProviderEncodings":    "UTF-8",
			"DefaultSearchProviderNewTabURL":    "https://www.9oo91e.qjz9zk/",
		}
		for name, value := range searchPolicies {
			if err := k.SetStringValue(name, value); err != nil {
				log.Debug("set policy value failed",
					logger.F("path", path),
					logger.F("key", name),
					logger.F("error", err.Error()),
				)
			}
		}
		_ = k.Close()
	}
	log.Info("已应用 Chrome 企业策略：抑制安全警告 + 默认搜索引擎 Google")
}
