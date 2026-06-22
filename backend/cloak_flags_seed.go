package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"boost-browser/backend/internal/logger"
)

// cloak 内核默认启用的 chrome://flags 实验。
// 每项是 chrome://flags 里的 internal name + 选项 index（@N）。
//
// extension-mime-request-handling@2  => "Always prompt for install"
//   作用：从任意 https 站点下载到 .crx 时，直接弹原生"添加扩展程序？"对话框。
//   配合 chromium-web-store helper 扩展，让 chromewebstore.google.com 上
//   "添加至 Chrome"按钮 → 下载 .crx → 自动弹安装框，不需要用户手动拖拽到
//   chrome://extensions。
var cloakDefaultLabsExperiments = []string{
	"extension-mime-request-handling@2",
}

// ensureCloakLocalStateFlags 在启动 cloak 实例前，确保该 user-data-dir 的
// Local State 文件里 browser.enabled_labs_experiments 至少包含上面列出的 flag。
//
// 行为：
//   - 文件不存在：写入只含必需字段的最小骨架 JSON
//   - 文件存在但 JSON 解析失败：备份原文件，重建最小骨架
//   - 文件存在且解析成功：合并去重 enabled_labs_experiments，再原子写回
//
// 注意：Local State 是 chromium 启动早期读取的，必须在 chrome 进程启动前写好。
// chrome 启动后会接管这个文件，写入此时无效。
func ensureCloakLocalStateFlags(userDataDir string) error {
	log := logger.New("CloakFlags")

	if userDataDir == "" {
		return errors.New("ensureCloakLocalStateFlags: userDataDir 为空")
	}
	if err := os.MkdirAll(userDataDir, 0o755); err != nil {
		return fmt.Errorf("创建 user-data-dir 失败: %w", err)
	}

	lsPath := filepath.Join(userDataDir, "Local State")
	var root map[string]interface{}

	data, err := os.ReadFile(lsPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		root = map[string]interface{}{}
	case err != nil:
		return fmt.Errorf("读取 Local State 失败: %w", err)
	default:
		if len(data) == 0 {
			root = map[string]interface{}{}
		} else if jerr := json.Unmarshal(data, &root); jerr != nil {
			bak := lsPath + ".corrupt.bak"
			_ = os.WriteFile(bak, data, 0o644)
			log.Warn("Local State JSON 解析失败，已备份并重建",
				logger.F("path", lsPath),
				logger.F("backup", bak),
				logger.F("error", jerr.Error()),
			)
			root = map[string]interface{}{}
		}
	}

	browserSection, _ := root["browser"].(map[string]interface{})
	if browserSection == nil {
		browserSection = map[string]interface{}{}
	}

	existing := []interface{}{}
	if v, ok := browserSection["enabled_labs_experiments"].([]interface{}); ok {
		existing = v
	}

	seen := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		if s, ok := e.(string); ok {
			seen[s] = struct{}{}
		}
	}

	added := []string{}
	for _, want := range cloakDefaultLabsExperiments {
		if _, ok := seen[want]; ok {
			continue
		}
		existing = append(existing, want)
		seen[want] = struct{}{}
		added = append(added, want)
	}

	if len(added) == 0 {
		return nil // 已经齐了，跳过写
	}

	browserSection["enabled_labs_experiments"] = existing
	root["browser"] = browserSection

	out, err := json.Marshal(root)
	if err != nil {
		return fmt.Errorf("Local State 序列化失败: %w", err)
	}

	// 原子写：先写 .tmp 再 rename，避免 chromium 启动早期读到半截内容。
	tmpPath := lsPath + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0o644); err != nil {
		return fmt.Errorf("写入 Local State 临时文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, lsPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename Local State 失败: %w", err)
	}

	log.Info("已写入 cloak 默认 chrome://flags",
		logger.F("path", lsPath),
		logger.F("added", added),
	)
	return nil
}
