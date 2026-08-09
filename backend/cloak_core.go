package backend

import (
	"boost-browser/backend/internal/browser"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// cloakMarkerFilename 是部署时放进 cloak 内核目录的标记文件名。
// 用户为了绕过 launcher 硬编码的 google-148 路径，会把 cloak 目录重命名为
// google-<ver>，这时 core_id / core_name / core_path 都不再含 "cloak"。
// 因此还需要一个文件级 marker 兜底。部署时在 cloak chrome 目录下创建空文件
// 即可：`type nul > cloak.marker`。
const cloakMarkerFilename = "cloak.marker"

// isCloakCore reports whether the selected core is the bundled CloakBrowser kernel.
//
// 检测顺序（任一命中即视为 cloak）：
//  1. core_id / core_name / core_path / exePath 任一字段含 "cloak"
//  2. exePath 同目录、或 core_path（resolveAppPath 之后）下存在 cloak.marker 标记文件
func isCloakCore(core browser.Core, exePath string) bool {
	text := strings.ToLower(strings.Join([]string{core.CoreId, core.CoreName, core.CorePath, exePath}, " "))
	if strings.Contains(text, "cloak") {
		return true
	}
	// 兜底：标记文件检测
	candidates := make([]string, 0, 2)
	if exePath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(exePath), cloakMarkerFilename))
	}
	if core.CorePath != "" {
		candidates = append(candidates, filepath.Join(core.CorePath, cloakMarkerFilename))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func (a *App) isProfileUsingCloakCore(profileId string) bool {
	if a == nil || a.browserMgr == nil {
		return false
	}
	a.browserMgr.Mutex.Lock()
	profile, ok := a.browserMgr.Profiles[profileId]
	if !ok || profile == nil {
		a.browserMgr.Mutex.Unlock()
		return false
	}
	coreId := strings.TrimSpace(profile.CoreId)
	a.browserMgr.Mutex.Unlock()

	var core browser.Core
	var found bool
	if coreId != "" {
		core, found = a.browserMgr.GetCore(coreId)
	}
	if !found {
		core, found = a.browserMgr.GetDefaultCore()
	}
	return found && isCloakCore(core, "")
}

// buildEffectiveFingerprintArgs returns the launch-time fingerprint args for the selected core.
//
// 非 cloak 内核：直接透传 profile.FingerprintArgs（兼容旧 ungoogled-chromium）。
//
// cloak 内核：保留 cloak 原生支持的所有 fingerprint-* 指纹 switch + 几个网络/locale 字段。
// 之前白名单过严，把 noise / hardware-concurrency / device-memory / screen-* / brand 全
// drop 了，导致 cloak 用默认 seed-based 随机生成低端 VM 般的硬件值，被风控判 Virtual Machine。
//
// timezone / locale 的 stale 值（如 profile 里写死的 Asia/Shanghai）在 cloak 路径要被丢弃，
// 让 cloak_geoip.resolveCloakGeoArgs 按代理 IP 反推的真实值生效。
func buildEffectiveFingerprintArgs(profile *browser.Profile, selectedCore browser.Core, chromeBinaryPath string) []string {
	args := append([]string{}, profile.FingerprintArgs...)
	if !isCloakCore(selectedCore, chromeBinaryPath) {
		return args
	}

	result := make([]string, 0, len(args)+4)
	addUnique := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		key := launchArgKey(v)
		for _, existing := range result {
			if strings.EqualFold(launchArgKey(existing), key) {
				return
			}
		}
		result = append(result, v)
	}

	// stale 的 timezone 要丢弃，由 geoip 按代理 IP 动态注入；
	// locale/lang 由 profile 自己定（用户可能挂日本代理但要中文界面），不丢；
	// 别的 cloak switch 全部透传（特别是 noise/hardware-concurrency/device-memory/screen-* —— 关 VM 检测）。
	dropPrefixes := []string{
		"--fingerprint-timezone=",
		"--timezone=",
	}
	hasSeed := false
	hasPlatform := false
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		drop := false
		for _, p := range dropPrefixes {
			if strings.HasPrefix(lower, p) {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		if strings.HasPrefix(lower, "--fingerprint=") {
			hasSeed = true
		}
		if strings.HasPrefix(lower, "--fingerprint-platform=") {
			hasPlatform = true
		}
		addUnique(trimmed)
	}
	if !hasSeed {
		addUnique(stableFingerprintSeedArg(profile.ProfileId))
	}
	if !hasPlatform {
		addUnique("--fingerprint-platform=windows")
	}
	addUnique("--no-sandbox")
	// Chromium 137+ 默认开启 DisableLoadExtensionCommandLineSwitch，会静默忽略
	// --load-extension 命令行参数（Google 加的反恶意启动器限制）。Cloak 146 含这个限制，
	// 直接结果是 chromium-web-store helper 加载不上 → 用户在 Web Store 点"添加至 Chrome"
	// 只会下载 .crx 而无法 inline install。这里显式 disable 掉这个 feature。
	addUnique("--disable-features=DisableLoadExtensionCommandLineSwitch")
	// 注意：之前为了压"unsupported command line"infobar 加了 --test-type，
	// 但 fingerprint.com 把 --test-type 当作 bot 身份信号（type=google），
	// 直接判 Bad Bot。Cloak 内核场景下我们宁可让 infobar 出现，也不要丢身份。
	// 用户可以手动关掉 infobar，或交给 cloak 的更新版处理。
	return result
}

func stableFingerprintSeedArg(profileId string) string {
	seed := 0
	for _, char := range profileId {
		seed = (seed << 5) - seed + int(char)
	}
	if seed < 0 {
		seed = -seed
	}
	if seed == 0 {
		seed = 1
	}
	return "--fingerprint=" + strconv.Itoa(seed)
}

func launchArgKey(arg string) string {
	arg = strings.TrimSpace(arg)
	if i := strings.Index(arg, "="); i >= 0 {
		return strings.ToLower(arg[:i])
	}
	return strings.ToLower(arg)
}

// mergeUniqueLaunchArgs 把 extras 追加到 base，但若 base 中已经存在同名 switch
// （如 --fingerprint-timezone=...）则保留 base 中的值，extras 中的同名条目被丢弃。
// 这让 cloak geoip 自动注入只在 profile / config 没显式设置时才生效。
func mergeUniqueLaunchArgs(base, extras []string) []string {
	if len(extras) == 0 {
		return base
	}
	existing := make(map[string]struct{}, len(base))
	for _, arg := range base {
		key := launchArgKey(arg)
		if key == "" {
			continue
		}
		existing[key] = struct{}{}
	}
	for _, arg := range extras {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		key := launchArgKey(trimmed)
		if _, ok := existing[key]; ok {
			continue
		}
		existing[key] = struct{}{}
		base = append(base, trimmed)
	}
	return base
}
