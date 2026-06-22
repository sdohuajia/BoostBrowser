package backend

import (
	"boost-browser/backend/internal/apppath"
	"boost-browser/backend/internal/browser"
	"boost-browser/backend/internal/config"
	"boost-browser/backend/internal/logger"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// 工具函数
// ============================================================================

// resolveAppPath 将相对路径解析为绝对路径（基于 appRoot）。
// 如果传入的已经是绝对路径则直接返回。
func (a *App) resolveAppPath(p string) string {
	return apppath.Resolve(a.appRoot, p)
}

func generateUUID() string {
	return uuid.NewString()
}

func nextAvailablePort() (int, error) {
	// 二次验证策略：分配端口后立即再次绑定确认未被抢占，最多重试 10 次
	for i := 0; i < 10; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			continue
		}
		port := l.Addr().(*net.TCPAddr).Port
		l.Close()
		// 短暂等待 OS 释放端口
		time.Sleep(5 * time.Millisecond)
		// 二次验证端口未被其他进程抢占
		v, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		v.Close()
		return port, nil
	}
	return 0, fmt.Errorf("无法分配可用端口")
}

// ============================================================================
// 内核初始化
// ============================================================================

func (a *App) ensureDefaultCores() {
	log := logger.New("Browser")

	// 扫描 chrome/ 目录，无论配置是否已有内核都执行一次，确保新增子目录被发现
	detected := a.scanChromeDir("chrome")

	if len(a.config.Browser.Cores) == 0 {
		// 配置为空：直接用扫描结果，或兜底写一个占位
		if len(detected) > 0 {
			a.config.Browser.Cores = detected
		} else {
			a.config.Browser.Cores = []browser.Core{}
		}
		if err := a.config.Save(a.resolveAppPath("config.yaml")); err != nil {
			log.Error("内核配置初始化失败", logger.F("error", err))
			return
		}
		log.Info("内核配置初始化完成", logger.F("count", len(a.config.Browser.Cores)))
		return
	}

	// 配置已有内核：将扫描到的新目录追加进去（不覆盖已有的）
	changed := false
	for _, newCore := range detected {
		exists := false
		for _, existing := range a.config.Browser.Cores {
			if existing.CorePath == newCore.CorePath {
				exists = true
				break
			}
		}
		if !exists {
			a.config.Browser.Cores = append(a.config.Browser.Cores, newCore)
			log.Info("发现新内核，已注册", logger.F("path", newCore.CorePath))
			changed = true
		}
	}
	if changed {
		if err := a.config.Save(a.resolveAppPath("config.yaml")); err != nil {
			log.Error("新内核注册保存失败", logger.F("error", err))
		}
	}
}

func installedChromeVersion(chromeExe string) string {
	versionDir := filepath.Base(filepath.Dir(chromeExe))
	if looksLikeChromeVersion(versionDir) {
		return versionDir
	}
	entries, err := os.ReadDir(filepath.Dir(chromeExe))
	if err != nil {
		return ""
	}
	latest := ""
	for _, entry := range entries {
		if !entry.IsDir() || !looksLikeChromeVersion(entry.Name()) {
			continue
		}
		if latest == "" || compareChromeVersion(entry.Name(), latest) > 0 {
			latest = entry.Name()
		}
	}
	return latest
}

func looksLikeChromeVersion(s string) bool {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func compareChromeVersion(a, b string) int {
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	maxLen := len(ap)
	if len(bp) > maxLen {
		maxLen = len(bp)
	}
	for i := 0; i < maxLen; i++ {
		av, bv := 0, 0
		if i < len(ap) {
			_, _ = fmt.Sscanf(ap[i], "%d", &av)
		}
		if i < len(bp) {
			_, _ = fmt.Sscanf(bp[i], "%d", &bv)
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}

// ensureBundledGoogleChromeCore registers the Chromium kernels bundled in this
// Boost Browser runtime. Two kernel families are recognised:
//
//  1. Google Chrome (chrome\google-<ver> 或 chrome\chrome-latest 目录)
//  2. CloakBrowser stealth Chromium (chrome\cloak-<ver> 目录，或任意目录里
//     带 cloak.marker 标记文件)
//
// 如果两个内核都存在，cloak 内核作为默认（49 个源码级指纹补丁，比原版更适合
// 风控/反检测场景）。已有 profile 的 core_id 不会被强制改写——只有那些指向
// 已不存在内核的 profile 会被迁移到默认内核。
func (a *App) ensureBundledGoogleChromeCore() {
	if a.browserMgr == nil {
		return
	}

	googlePath, googleVersion := a.findBundledGoogleChromeCore()
	cloakPath, cloakVersion := a.findBundledCloakChromeCore()

	type bundledCore struct {
		Id        string
		Name      string
		Path      string
		IsDefault bool
	}
	var registered []bundledCore

	// cloak 优先：如果存在则作为默认
	if cloakPath != "" {
		// 对外只显示 "Chromium <major>"，不暴露 Cloak/补丁数等内部细节。
		name := "Chromium"
		if cloakVersion != "" {
			major := strings.SplitN(cloakVersion, ".", 2)[0]
			if major == "" {
				major = cloakVersion
			}
			name = fmt.Sprintf("Chromium %s", major)
		}
		registered = append(registered, bundledCore{
			Id:        "bundled-cloak-chromium-latest",
			Name:      name,
			Path:      cloakPath,
			IsDefault: true,
		})
	}
	if googlePath != "" {
		name := "Google Chrome（内置隔离）"
		if googleVersion != "" {
			name = fmt.Sprintf("Google Chrome %s（内置隔离）", googleVersion)
		}
		registered = append(registered, bundledCore{
			Id:        "bundled-google-chrome-latest",
			Name:      name,
			Path:      googlePath,
			IsDefault: cloakPath == "", // cloak 不存在时 google 才是默认
		})
	}

	if len(registered) == 0 {
		a.lifecycleLog("bundled-chrome-core-skip", "reason=not-found")
		return
	}

	// 注册所有发现的内核
	keepIds := make(map[string]struct{}, len(registered))
	defaultId := ""
	for _, c := range registered {
		if err := a.browserMgr.SaveCore(browser.CoreInput{
			CoreId:    c.Id,
			CoreName:  c.Name,
			CorePath:  c.Path,
			IsDefault: c.IsDefault,
		}); err != nil {
			a.lifecycleLog("bundled-chrome-core-failed", "path="+c.Path, "error="+err.Error())
			logger.New("Browser").Warn("内置内核注册失败", logger.F("path", c.Path), logger.F("error", err.Error()))
			continue
		}
		keepIds[strings.ToLower(c.Id)] = struct{}{}
		if c.IsDefault {
			defaultId = c.Id
		}
		a.lifecycleLog("bundled-chrome-core-registered", "path="+c.Path, "name="+c.Name, "id="+c.Id)
	}

	// 已有 profile 如果指向不存在的内核，迁移到默认内核（单内核场景的旧行为）
	if a.db != nil && defaultId != "" {
		if conn := a.db.GetConn(); conn != nil {
			placeholders := make([]string, 0, len(keepIds))
			args := make([]any, 0, len(keepIds)+1)
			args = append(args, defaultId)
			for id := range keepIds {
				placeholders = append(placeholders, "?")
				args = append(args, id)
			}
			query := fmt.Sprintf(
				"UPDATE browser_profiles SET core_id = ? WHERE (core_id IS NULL OR core_id = '' OR LOWER(core_id) NOT IN (%s))",
				strings.Join(placeholders, ","),
			)
			if res, err := conn.Exec(query, args...); err == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					a.lifecycleLog("bundled-chrome-profiles-migrated", fmt.Sprintf("count=%d default=%s", n, defaultId))
				}
			}
		}
	}

	// 清理不在白名单里的旧 core 条目（旧版残留的实验性内核等）
	for _, core := range a.browserMgr.ListCores() {
		if _, ok := keepIds[strings.ToLower(core.CoreId)]; !ok {
			_ = a.browserMgr.DeleteCore(core.CoreId)
		}
	}
	if defaultId != "" {
		_ = a.browserMgr.SetDefaultCore(defaultId)
	}

	// 同步 config.yaml 的 cores 段（只写注册成功的内核）
	cfgCores := make([]browser.Core, 0, len(registered))
	for _, c := range registered {
		if _, ok := keepIds[strings.ToLower(c.Id)]; ok {
			cfgCores = append(cfgCores, browser.Core{
				CoreId:    c.Id,
				CoreName:  c.Name,
				CorePath:  c.Path,
				IsDefault: c.IsDefault,
			})
		}
	}
	a.config.Browser.Cores = cfgCores
	_ = a.config.Save(a.resolveAppPath("config.yaml"))
	_ = a.browserMgr.ListCores()
}

// findBundledCloakChromeCore 扫描 chrome\ 目录，定位 CloakBrowser 内核。
// 识别规则（任一命中即认作 cloak 内核）：
//   - 目录名以 "cloak-" 开头（标准命名）
//   - 目录里存在 cloak.marker 文件（用户自定义命名时的兜底，例如把 cloak
//     重命名成 google-148 来骗过老 launcher 时仍能识别）
//
// 命中多个时按 chrome.exe 内嵌版本号取最新。
func (a *App) findBundledCloakChromeCore() (string, string) {
	baseDir := a.resolveAppPath("chrome")
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return "", ""
	}
	bestName := ""
	bestVersion := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		absDir := filepath.Join(baseDir, name)

		isCloak := strings.HasPrefix(lower, "cloak-") || strings.EqualFold(name, "cloak")
		if !isCloak {
			markerPath := filepath.Join(absDir, cloakMarkerFilename)
			if _, statErr := os.Stat(markerPath); statErr == nil {
				isCloak = true
			}
		}
		if !isCloak {
			continue
		}
		if _, _, ok := browser.FindCoreExecutable(absDir); !ok {
			continue
		}
		version := strings.TrimPrefix(lower, "cloak-")
		if version == lower || !looksLikeChromeVersion(version) {
			version = installedChromeVersion(filepath.Join(absDir, "chrome.exe"))
		}
		if bestName == "" || compareChromeVersion(version, bestVersion) > 0 {
			bestName = name
			bestVersion = version
		}
	}
	if bestName == "" {
		return "", ""
	}
	return filepath.Join("chrome", bestName), bestVersion
}

func (a *App) findBundledGoogleChromeCore() (string, string) {
	baseDir := a.resolveAppPath("chrome")
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return "", ""
	}
	bestName := ""
	bestVersion := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasPrefix(lower, "google-") && !strings.HasPrefix(lower, "chrome-latest") {
			continue
		}
		absDir := filepath.Join(baseDir, name)
		// 跳过带 cloak.marker 的目录：用户为了绕过 launcher 旧版硬编码可能把 cloak
		// 目录改名成 google-<ver>，这种情况下要交给 findBundledCloakChromeCore 处理。
		if _, statErr := os.Stat(filepath.Join(absDir, cloakMarkerFilename)); statErr == nil {
			continue
		}
		if _, _, ok := browser.FindCoreExecutable(absDir); !ok {
			continue
		}
		version := strings.TrimPrefix(name, "google-")
		if version == name || !looksLikeChromeVersion(version) {
			version = installedChromeVersion(filepath.Join(absDir, "chrome.exe"))
		}
		if bestName == "" || compareChromeVersion(version, bestVersion) > 0 {
			bestName = name
			bestVersion = version
		}
	}
	if bestName == "" {
		return "", ""
	}
	return filepath.Join("chrome", bestName), bestVersion
}

func (a *App) autoDetectCores() {
	log := logger.New("Browser")
	// ensureDefaultCores 已完成扫描注册，这里只做路径有效性日志。
	// SQLite 模式下以内核表为准，避免与 config.yaml 历史条目不一致。
	cores := a.config.Browser.Cores
	if a.browserMgr != nil {
		cores = a.browserMgr.ListCores()
	}
	for _, core := range cores {
		result := a.browserMgr.ValidateCorePath(core.CorePath)
		if result.Valid {
			log.Debug("内核路径有效", logger.F("core_id", core.CoreId), logger.F("path", core.CorePath))
		} else {
			log.Warn("内核路径无效", logger.F("core_id", core.CoreId), logger.F("path", core.CorePath), logger.F("message", result.Message))
		}
	}
}

// scanChromeDir 扫描指定目录，将包含浏览器可执行文件的子文件夹识别为内核。
// 如果目录本身包含可执行文件（旧版单内核结构），则直接返回该目录作为内核。
func (a *App) scanChromeDir(chromeRoot string) []browser.Core {
	log := logger.New("Browser")

	baseDir := a.resolveAppPath(chromeRoot)

	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		return nil
	}

	// 如果根目录本身就有浏览器可执行文件，视为单内核结构
	if _, _, ok := browser.FindCoreExecutable(baseDir); ok {
		return []browser.Core{
			{
				CoreId:    "default",
				CoreName:  "默认内核",
				CorePath:  chromeRoot,
				IsDefault: true,
			},
		}
	}

	// 扫描子文件夹
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		log.Warn("扫描 chrome 目录失败", logger.F("path", baseDir), logger.F("error", err.Error()))
		return nil
	}

	var cores []browser.Core
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subPath := filepath.Join(chromeRoot, entry.Name())
		absCoreDir := filepath.Join(baseDir, entry.Name())
		if _, _, ok := browser.FindCoreExecutable(absCoreDir); !ok {
			continue // 没有浏览器可执行文件，跳过
		}
		isDefault := len(cores) == 0
		cores = append(cores, browser.Core{
			CoreId:    fmt.Sprintf("core-%s", entry.Name()),
			CoreName:  fmt.Sprintf("Chrome %s", entry.Name()),
			CorePath:  subPath,
			IsDefault: isDefault,
		})
		log.Debug("发现内核", logger.F("name", entry.Name()), logger.F("path", subPath))
	}
	return cores
}

// ============================================================================
// 代理数据加载
// ============================================================================

// loadProxies 启动时加载代理数据。
// 优先从 ProxyDAO（SQLite）读取；若 DAO 未注入则降级到 proxies.yaml，最后降级到 config.yaml。
func (a *App) loadProxies() {
	log := logger.New("Browser")

	builtins := []browser.Proxy{
		{ProxyId: "__direct__", ProxyName: "直连（不走代理）", ProxyConfig: "direct://"},
		{ProxyId: "__local__", ProxyName: "本地代理", ProxyConfig: "http://127.0.0.1:7890"},
	}

	ensureBuiltins := func(list []browser.Proxy) []browser.Proxy {
		for _, b := range builtins {
			found := false
			for _, p := range list {
				if p.ProxyId == b.ProxyId {
					found = true
					break
				}
			}
			if !found {
				list = append([]browser.Proxy{b}, list...)
			}
		}
		return list
	}

	// 优先从 SQLite 读取
	if a.browserMgr.ProxyDAO != nil {
		list, err := a.browserMgr.ProxyDAO.List()
		if err != nil {
			log.Error("从数据库读取代理失败", logger.F("error", err.Error()))
		} else if len(list) > 0 {
			a.config.Browser.Proxies = list
			log.Info("代理数据从数据库加载完成", logger.F("count", len(list)))
			return
		}
	}

	// 降级：从 proxies.yaml 加载
	loaded, err := config.LoadProxies(a.resolveAppPath("proxies.yaml"))
	if err != nil {
		log.Warn("读取 proxies.yaml 失败", logger.F("error", err.Error()))
	}
	if loaded != nil {
		proxies := ensureBuiltins(loaded)
		a.config.Browser.Proxies = proxies
		log.Info("代理数据从 proxies.yaml 加载完成", logger.F("count", len(proxies)))
		return
	}

	// 最终降级：使用 config.yaml 中的数据
	proxies := ensureBuiltins(a.config.Browser.Proxies)
	a.config.Browser.Proxies = proxies
	log.Info("代理数据使用 config.yaml 默认值", logger.F("count", len(proxies)))
}
