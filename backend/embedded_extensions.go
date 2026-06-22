package backend

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"boost-browser/backend/internal/logger"
)

// chromium-web-store helper 扩展（让 cloak/ungoogled-chromium 能在
// chromewebstore.google.com 上点"添加至 Chrome"直接安装扩展）。
// 之前 v1.1.0/v1.2.x 把这个扩展放在开发机 <app-root>\extensions\
// 下，profile.launch_args 里硬编码了那个绝对路径。一旦用户那边路径不存在，
// helper 加载失败 → 用户在 Web Store 看到"无法从该网站添加应用、扩展程序"。
//
// 这里把扩展直接 embed 进 boost-browser.exe，启动时解压到 <appRoot>\extensions\
// chromium-web-store\，cloak_core 启动参数里按 appRoot 拼路径，旧 <legacy-path> 路径会被
// app_instance.go 在 cloak 路径上 strip 掉。
//
// 注意：_locales/ 以下划线开头，默认 //go:embed 不会包含，必须用 all: 前缀。
//
//go:embed all:embedded_extensions/chromium-web-store
var embeddedExtensionsFS embed.FS

const (
	embeddedExtensionsRoot       = "embedded_extensions"
	cloakWebStoreHelperDirName   = "chromium-web-store"
	cloakWebStoreHelperVersionFn = ".embedded_version"
)

// cloakWebStoreHelperPath 返回 helper 扩展在用户机上的目标路径。
func cloakWebStoreHelperPath(appRoot string) string {
	if strings.TrimSpace(appRoot) == "" {
		return ""
	}
	return filepath.Join(appRoot, "extensions", cloakWebStoreHelperDirName)
}

// embeddedHelperFingerprint 计算 embed 资源里 chromium-web-store 整棵树的 sha256
// 指纹（按相对路径排序后逐文件哈希）。任何文件变更都会改变指纹，从而触发
// ensureEmbeddedCloakExtensions 重新解压 —— 否则只看 manifest.json 时，
// util.js / background.js 等代码改动不会被部署。
func embeddedHelperFingerprint() (string, error) {
	srcRoot := embeddedExtensionsRoot + "/" + cloakWebStoreHelperDirName
	type fileEntry struct {
		rel string
		sum [32]byte
	}
	var entries []fileEntry
	err := fs.WalkDir(embeddedExtensionsFS, srcRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, e := embeddedExtensionsFS.ReadFile(p)
		if e != nil {
			return e
		}
		rel := strings.TrimPrefix(p, srcRoot)
		rel = strings.TrimPrefix(rel, "/")
		entries = append(entries, fileEntry{rel: rel, sum: sha256.Sum256(data)})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("遍历嵌入扩展失败: %w", err)
	}
	// 按相对路径排序，确保指纹稳定
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte(e.rel))
		h.Write([]byte{0})
		h.Write(e.sum[:])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ensureEmbeddedCloakExtensions 在 appRoot 下解压 chromium-web-store helper 扩展。
// 已存在且 manifest 指纹一致则直接返回；否则清空目录重新解压。
//
// 返回的 string 是 helper 扩展的绝对路径，调用方 (cloak 启动链路) 可以直接
// 拼到 --load-extension。失败返回空字符串 + 错误，由调用方决定是否阻塞启动。
func ensureEmbeddedCloakExtensions(appRoot string) (string, error) {
	log := logger.New("EmbedExt")
	dest := cloakWebStoreHelperPath(appRoot)
	if dest == "" {
		return "", fmt.Errorf("appRoot 为空，无法定位扩展目录")
	}

	wantFp, err := embeddedHelperFingerprint()
	if err != nil {
		return "", err
	}
	versionFile := filepath.Join(dest, cloakWebStoreHelperVersionFn)
	if existing, err := os.ReadFile(versionFile); err == nil {
		if strings.TrimSpace(string(existing)) == wantFp {
			// 已是最新版本 → 直接复用
			return dest, nil
		}
	}

	// 版本不一致或首次部署：清空 + 重写
	if err := os.RemoveAll(dest); err != nil {
		return "", fmt.Errorf("清理旧扩展目录失败: %w", err)
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		return "", fmt.Errorf("创建扩展目录失败: %w", err)
	}

	srcRoot := embeddedExtensionsRoot + "/" + cloakWebStoreHelperDirName
	count := 0
	walkErr := fs.WalkDir(embeddedExtensionsFS, srcRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, srcRoot)
		rel = strings.TrimPrefix(rel, "/")
		target := dest
		if rel != "" {
			target = filepath.Join(dest, filepath.FromSlash(rel))
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		in, err := embeddedExtensionsFS.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		count++
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("解压扩展失败: %w", walkErr)
	}

	// 写版本指纹
	if err := os.WriteFile(versionFile, []byte(wantFp), 0644); err != nil {
		// 不致命：下次启动会再解压一次
		log.Warn("写入扩展版本指纹失败", logger.F("path", versionFile), logger.F("error", err.Error()))
	}

	log.Info("已部署内置 chromium-web-store helper 扩展",
		logger.F("dest", dest),
		logger.F("files", count),
		logger.F("fingerprint", wantFp[:12]),
	)
	return dest, nil
}

// looksLikeStaleCloakExtensionPath 判断一条 --load-extension= 路径是否是
// 老版本残留的开发机绝对路径或者用户机上根本不存在的路径。
//
//   - 含 BoostBrowser_cloak_test 字面量（v1.1.0 错误硬编码）→ stale
//   - 路径不存在 → stale
//   - 是 helper 扩展的 appRoot 下规范路径 → 保留（由调用方负责注入）
func looksLikeStaleCloakExtensionPath(p string, appRoot string) bool {
	low := strings.ToLower(strings.TrimSpace(p))
	if low == "" {
		return true
	}
	if strings.Contains(low, "boostbrowser_cloak_test") {
		return true
	}
	// chromium-web-store helper 在 appRoot 之外的路径都视为 stale
	canon := strings.ToLower(cloakWebStoreHelperPath(appRoot))
	if canon != "" && strings.Contains(low, "chromium-web-store") && !strings.HasPrefix(low, canon) {
		return true
	}
	if _, err := os.Stat(p); err != nil {
		return true
	}
	return false
}

// writeHelperBoostEndpoint 把 LaunchServer 的本地 install endpoint 信息写到
// chromium-web-store helper 扩展目录里。helper 通过 chrome.runtime.getURL
// 读这份文件，得知 LaunchServer 端口与可选 API key。
//
// 必须在 LaunchServer.Start() 成功之后调用，因为 port 可能是随机分配的。
// helper 的 boost_endpoint.json 不进 fingerprint：它是运行期数据，每次启动
// 都重写，避免 LaunchServer 切端口后 helper 拿到老端口连不上。
func writeHelperBoostEndpoint(appRoot string, port int, apiHeader, apiKey string) error {
	dest := cloakWebStoreHelperPath(appRoot)
	if dest == "" {
		return fmt.Errorf("appRoot 为空")
	}
	if port <= 0 {
		return fmt.Errorf("LaunchServer 端口无效: %d", port)
	}
	if _, err := os.Stat(dest); err != nil {
		return fmt.Errorf("helper 扩展目录不存在: %w", err)
	}
	payload := map[string]any{
		"port":      port,
		"apiHeader": apiHeader,
		"apiKey":    apiKey,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(dest, "boost_endpoint.json")
	if err := os.WriteFile(target, data, 0644); err != nil {
		return fmt.Errorf("写 boost_endpoint.json 失败: %w", err)
	}
	return nil
}
