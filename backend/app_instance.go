package backend

import (
	"boost-browser/backend/internal/browser"
	"boost-browser/backend/internal/logger"
	"boost-browser/backend/internal/proxy"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ============================================================================
// 浏览器实例管理 API
// ============================================================================

func fingerprintArgValue(args []string, prefix string) string {
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if strings.HasPrefix(strings.ToLower(trimmed), strings.ToLower(prefix)) {
			return strings.TrimSpace(trimmed[len(prefix):])
		}
	}
	return ""
}

func buildChromeUAFromFingerprintArgs(chromeVersion string, fpArgs []string) string {
	platform := strings.ToLower(fingerprintArgValue(fpArgs, "--fingerprint-platform="))
	switch platform {
	case "mac", "macos", "darwin":
		platformVersion := strings.TrimSpace(fingerprintArgValue(fpArgs, "--fingerprint-platform-version="))
		macVersion := "10_15_7"
		if platformVersion != "" {
			parts := strings.Split(platformVersion, ".")
			if len(parts) >= 2 {
				macVersion = parts[0] + "_" + parts[1] + "_0"
			} else if len(parts) == 1 {
				macVersion = parts[0] + "_0_0"
			}
		}
		return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X %s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", macVersion, chromeVersion)
	case "linux":
		return fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", chromeVersion)
	default:
		return fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", chromeVersion)
	}
}

// extractBadgeNumberFromName 从 ProfileName 里抽出 badge 显示的数字。
// 规则：取名字里**最后一段**连续数字。
//   - "1"        → 1
//   - "11"       → 11
//   - "实例-11"  → 11
//   - "Profile 5"→ 5
//   - "abc"      → 0（无数字，调用方应回退到顺序号）
//
// 取最后一段而不是第一段：避免类似 "2024年-3号" 被错误识别成 2024。
// 数字最大保留 4 位（badge 图标渲染上限），超过的截尾。
func extractBadgeNumberFromName(name string) int {
	end := -1
	for i := len(name) - 1; i >= 0; i-- {
		c := name[i]
		if c >= '0' && c <= '9' {
			end = i
			break
		}
	}
	if end < 0 {
		return 0
	}
	start := end
	for start-1 >= 0 && name[start-1] >= '0' && name[start-1] <= '9' {
		start--
	}
	// 跳过前导零
	for start < end && name[start] == '0' {
		start++
	}
	digits := name[start : end+1]
	if len(digits) > 4 {
		digits = digits[len(digits)-4:]
	}
	n := 0
	for _, c := range []byte(digits) {
		n = n*10 + int(c-'0')
	}
	return n
}

func (a *App) BrowserInstanceStart(profileId string) (*BrowserProfile, error) {
	return a.browserInstanceStartInternal(profileId, nil, nil, false, false)
}

// BrowserInstanceStartWithParams 通过额外参数启动实例（仅本次启动生效，不落库）
func (a *App) BrowserInstanceStartWithParams(profileId string, extraLaunchArgs []string, startURLs []string, skipDefaultStartURLs bool) (*BrowserProfile, error) {
	return a.browserInstanceStartInternal(profileId, extraLaunchArgs, startURLs, skipDefaultStartURLs, true)
}

func (a *App) browserInstanceStartInternal(profileId string, extraLaunchArgs []string, startURLs []string, skipDefaultStartURLs bool, preferVisibleWindow bool) (*BrowserProfile, error) {
	log := logger.New("Browser")
	a.browserMgr.Mutex.Lock()
	defer a.browserMgr.Mutex.Unlock()

	normalizedExtraLaunchArgs := normalizeNonEmptyStrings(extraLaunchArgs)
	normalizedStartURLs := normalizeNonEmptyStrings(startURLs)
	if preferVisibleWindow {
		normalizedExtraLaunchArgs = ensureNewWindowLaunchArg(normalizedExtraLaunchArgs)
	}

	profile, exists := a.browserMgr.Profiles[profileId]
	if !exists {
		err := fmt.Errorf("实例启动失败：未找到实例配置（ID=%s）。请刷新列表后重试。", profileId)
		log.Error("实例不存在", logger.F("profile_id", profileId), logger.F("reason", err.Error()))
		return nil, err
	}
	if profile.Running {
		if !isBrowserProfileLive(profile, a.browserMgr.BrowserProcesses[profileId]) {
			log.Info("检测到实例运行状态已失效，准备重新启动",
				logger.F("profile_id", profileId),
				logger.F("pid", profile.Pid),
				logger.F("debug_port", profile.DebugPort),
			)
			a.markProfileStoppedLocked(profileId, profile)
		} else {
			if preferVisibleWindow {
				if err := a.openBrowserWindowForRunningProfile(profile, normalizedExtraLaunchArgs, normalizedStartURLs); err != nil {
					startErr := fmt.Errorf("实例已在运行，但窗口唤起失败：%w", err)
					log.Error("运行中实例窗口唤起失败",
						logger.F("profile_id", profileId),
						logger.F("debug_port", profile.DebugPort),
						logger.F("error", err.Error()),
						logger.F("reason", startErr.Error()),
					)
					profile.LastError = startErr.Error()
					return profile, startErr
				}
			}
			if a.launchServer != nil && profile.DebugReady {
				a.launchServer.SetActiveProfile(profile)
			}
			a.emitBrowserInstanceStarted(profile, true)
			return profile, nil
		}
	}
	sanitizedProfileLaunchArgs, managedProfileArgs := sanitizeManagedLaunchArgs(profile.LaunchArgs)
	sanitizedProfileLaunchArgs, managedWindowPlacementArgs := sanitizeManagedWindowPlacementArgs(sanitizedProfileLaunchArgs)
	sanitizedExtraLaunchArgs, managedExtraArgs := sanitizeManagedLaunchArgs(normalizedExtraLaunchArgs)
	logManagedLaunchArgOverrides(log, profileId, "profile.launchArgs", managedProfileArgs)
	logManagedLaunchArgOverrides(log, profileId, "profile.launchArgs.windowPlacement", managedWindowPlacementArgs)
	logManagedLaunchArgOverrides(log, profileId, "start.extraLaunchArgs", managedExtraArgs)

	proxyChanged := a.browserMgr.ApplyDefaults(profile)
	if proxyChanged {
		_ = a.browserMgr.SaveProfiles()
	}

	chromeBinaryPath, err := a.browserMgr.ResolveChromeBinary(profile)
	if err != nil {
		startErr := fmt.Errorf("实例启动失败：%w", err)
		log.Error("内核路径解析失败", logger.F("profile_id", profileId), logger.F("error", err.Error()), logger.F("reason", startErr.Error()))
		profile.LastError = startErr.Error()
		return profile, startErr
	}
	selectedCore := browser.Core{}
	selectedCoreFound := false
	coreId := strings.TrimSpace(profile.CoreId)
	if coreId != "" {
		selectedCore, selectedCoreFound = a.browserMgr.GetCore(coreId)
	}
	if !selectedCoreFound {
		selectedCore, selectedCoreFound = a.browserMgr.GetDefaultCore()
	}
	isCloakSelectedCore := selectedCoreFound && isCloakCore(selectedCore, chromeBinaryPath)
	effectiveFingerprintArgs := buildEffectiveFingerprintArgs(profile, selectedCore, chromeBinaryPath)

	userDataDir := a.browserMgr.ResolveUserDataDir(profile)
	if err := os.MkdirAll(userDataDir, 0755); err != nil {
		startErr := fmt.Errorf("实例启动失败：无法创建用户数据目录 %s。原因：%w。请检查目录权限或路径配置。", userDataDir, err)
		log.Error("用户数据目录创建失败", logger.F("profile_id", profileId), logger.F("dir", userDataDir), logger.F("error", err.Error()), logger.F("reason", startErr.Error()))
		profile.LastError = startErr.Error()
		return profile, startErr
	}
	// 启动前关闭 Chrome 的“恢复上次会话”，避免上次遗留的扩展 welcome/options 页面
	// 在重开实例时再次弹出。
	sanitizeChromeStartupPreferences(userDataDir)
	if err := ensureBrowserUserDataDirReadyForFreshLaunch(chromeBinaryPath, userDataDir); err != nil {
		log.Error("浏览器用户目录启动前检查失败", logger.F("profile_id", profileId), logger.F("chrome", chromeBinaryPath), logger.F("dir", userDataDir), logger.F("error", err.Error()))
		profile.LastError = err.Error()
		return profile, err
	}
	// 搜索引擎修复分两条路径：
	//   - cloak 内核：启动时禁止再做静态 Web Data/Preferences seed，避免留下
	//     dead guid / partial Google state；只允许在 debug port 就绪后走 runtime
	//     CDP settings UI 路径，这是 packaged 目标里唯一稳定不会回退成 No Search
	//     的方案。
	//   - 非 cloak：仍保留启动前静态 seed 作为兜底。
	if !isCloakSelectedCore {
		seedDefaultSearchEngine(userDataDir)
	}

	// 每次启动时合并默认书签（已存在的 URL 不重复添加）
	if err := browser.EnsureDefaultBookmarks(userDataDir, a.BookmarkList()); err != nil {
		log.Error("默认书签写入失败", logger.F("error", err.Error()))
	}

	proxies := a.getLatestProxies()
	acquiredXrayBridgeKey := ""
	releaseXrayBridge := false
	acquiredStandardRelay := false
	defer func() {
		if releaseXrayBridge && acquiredXrayBridgeKey != "" && a.xrayMgr != nil {
			a.xrayMgr.ReleaseBridge(acquiredXrayBridgeKey)
		}
		if acquiredStandardRelay && a.standardRelayMgr != nil {
			a.standardRelayMgr.Release(profileId)
		}
	}()

	// 解析实际代理配置（可能来自 proxyId 引用）
	resolvedProxyConfig := strings.TrimSpace(profile.ProxyConfig)
	if profile.ProxyId != "" {
		for _, item := range proxies {
			if strings.EqualFold(item.ProxyId, profile.ProxyId) {
				resolvedProxyConfig = strings.TrimSpace(item.ProxyConfig)
				break
			}
		}
	}
	effectiveProxy := resolvedProxyConfig
	log.Info("代理配置检查",
		logger.F("profile_id", profileId),
		logger.F("proxy_id", profile.ProxyId),
		logger.F("profile_proxy_config", profile.ProxyConfig),
		logger.F("resolved_proxy_config", resolvedProxyConfig),
	)
	if supported, errorMsg := proxy.ValidateProxyConfig(resolvedProxyConfig, proxies, profile.ProxyId); !supported {
		startErr := fmt.Errorf("实例启动失败：%s", errorMsg)
		profile.LastError = startErr.Error()
		log.Error("代理配置无效", logger.F("profile_id", profileId), logger.F("proxy_id", profile.ProxyId), logger.F("error", errorMsg), logger.F("reason", startErr.Error()))
		return profile, startErr
	}

	if proxy.IsSingBoxProtocol(resolvedProxyConfig) {
		// hysteria2 / tuic → sing-box 桥接
		socksURL, bridgeErr := a.singboxMgr.EnsureBridge(resolvedProxyConfig, proxies, profile.ProxyId)
		if bridgeErr != nil {
			startErr := fmt.Errorf("实例启动失败：代理桥接启动失败（sing-box）。原因：%v。请检查代理节点配置、sing-box 可执行文件是否存在，以及本地端口是否被占用。", bridgeErr)
			log.Error("代理桥接失败(sing-box)", logger.F("error", bridgeErr.Error()), logger.F("reason", startErr.Error()))
			profile.LastError = startErr.Error()
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "proxy:bridge:failed", map[string]interface{}{
					"profileId":   profileId,
					"profileName": profile.ProfileName,
					"error":       startErr.Error(),
				})
			}
			return profile, startErr
		}
		effectiveProxy = socksURL
		log.Info("sing-box 桥接成功", logger.F("socks_url", socksURL))
	} else if proxy.RequiresBridge(resolvedProxyConfig, proxies, profile.ProxyId) {
		// vmess / vless / trojan / ss → xray 桥接
		socksURL, bridgeKey, bridgeErr := a.xrayMgr.AcquireBridge(resolvedProxyConfig, proxies, profile.ProxyId)
		if bridgeErr != nil {
			startErr := fmt.Errorf("实例启动失败：代理桥接启动失败（xray）。原因：%v。请检查代理节点配置、xray 可执行文件是否存在，以及本地端口是否被占用。", bridgeErr)
			log.Error("代理桥接失败(xray)", logger.F("error", bridgeErr.Error()), logger.F("reason", startErr.Error()))
			profile.LastError = startErr.Error()
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "proxy:bridge:failed", map[string]interface{}{
					"profileId":   profileId,
					"profileName": profile.ProfileName,
					"error":       startErr.Error(),
				})
			}
			return profile, startErr
		}
		acquiredXrayBridgeKey = bridgeKey
		releaseXrayBridge = bridgeKey != ""
		effectiveProxy = socksURL
		log.Info("xray 桥接成功", logger.F("socks_url", socksURL))
	} else if proxy.IsStandardProxyURL(resolvedProxyConfig) && a.standardRelayMgr != nil {
		localProxy, relayKey, relayErr := a.standardRelayMgr.Acquire(profileId, resolvedProxyConfig)
		if relayErr != nil {
			startErr := fmt.Errorf("实例启动失败：标准代理本地转发启动失败。原因：%v。请检查代理协议、账号密码和节点可用性。", relayErr)
			log.Error("标准代理本地转发失败", logger.F("profile_id", profileId), logger.F("proxy_id", profile.ProxyId), logger.F("error", relayErr.Error()), logger.F("reason", startErr.Error()))
			profile.LastError = startErr.Error()
			return profile, startErr
		}
		if relayKey != "" {
			acquiredStandardRelay = true
			effectiveProxy = localProxy
			log.Info("标准代理已切换为本地转发", logger.F("profile_id", profileId), logger.F("local_proxy", localProxy), logger.F("upstream", relayKey))
		}
	}

	startReadyTimeout, startStableWindow := a.browserStartTimingSettings()
	maxStartAttempts := browserStartAttemptCount()
	totalReadyTimeout := time.Duration(maxStartAttempts) * startReadyTimeout
	var lastStartErr error
	assignedDebugPort, err := nextAvailablePort()
	if err != nil {
		startErr := fmt.Errorf("实例启动失败：本地调试端口分配失败。原因：%v。请关闭占用端口的程序后重试。", err)
		log.Error("调试端口分配失败", logger.F("profile_id", profileId), logger.F("error", err.Error()), logger.F("reason", startErr.Error()))
		profile.LastError = startErr.Error()
		return profile, startErr
	}

	args := []string{
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
		fmt.Sprintf("--remote-debugging-port=%d", assignedDebugPort),
		"--remote-allow-origins=http://127.0.0.1",
		"--disable-session-crashed-bubble",
		"--no-first-run",
		"--no-default-browser-check",
	}
	// 窗口尺寸：优先用上次关闭时保存的；没有就用 1280x900 的默认值。
	if profile.LastWindowWidth >= windowBoundsMinWidth && profile.LastWindowHeight >= windowBoundsMinHeight {
		args = append(args, fmt.Sprintf("--window-size=%d,%d", profile.LastWindowWidth, profile.LastWindowHeight))
		// 位置只在尺寸有效且坐标看起来合理时附加，避免把窗口放到屏幕外。
		// (-32000, -32000) 是 Windows 最小化的标志位，不能复用。
		if profile.LastWindowX > -10000 && profile.LastWindowY > -10000 {
			args = append(args, fmt.Sprintf("--window-position=%d,%d", profile.LastWindowX, profile.LastWindowY))
		}
	} else {
		// Chrome for Testing 已能正常启动，不再把浏览器先移到屏幕外压制扩展自动窗口；
		// 直接使用正常可见窗口，避免启动阶段出现 Default IME/任务栏缩略图等副作用。
		args = append(args, "--window-size=1280,900")
	}

	// 非 Cloak 内核仍保留 --search-provider-* 作为启动期兜底。
	// Cloak 路径下禁止再注入这组命令行参数：实际 packaged 目标里它会留下
	// default_search_provider 与 runtime row 不一致的 mixed state，最终仍可能
	// 回退成 No Search。Cloak 统一只走 runtime CDP seed，避免 dead guid / drift。
	args = appendDefaultSearchProviderLaunchArgs(args, !isCloakSelectedCore)

	hasFingerprint := false
	for _, arg := range effectiveFingerprintArgs {
		if strings.HasPrefix(arg, "--fingerprint=") {
			hasFingerprint = true
			break
		}
	}
	if !hasFingerprint {
		seed := 0
		for _, char := range profile.ProfileId {
			seed = (seed << 5) - seed + int(char)
		}
		if seed < 0 {
			seed = -seed
		}
		args = append(args, fmt.Sprintf("--fingerprint=%d", seed))
	}

	if effectiveProxy == "direct://" {
		// 强制直连，覆盖系统全局代理
		args = append(args, "--proxy-server=direct://")
	} else if effectiveProxy != "" {
		args = append(args, fmt.Sprintf("--proxy-server=%s", effectiveProxy))
	}
	// 从内核/指纹参数提取版本号，构造 --user-agent 参数。
	// Cloak 的 --fingerprint-brand-version 会影响 UA-CH，但 navigator.userAgent
	// 仍会保留内核默认版本；这里必须显式补 --user-agent，旧实例重启后 UA 才会不同。
	if selectedCoreFound && selectedCore.CorePath != "" {
		chromeVersion := ""
		if isCloakSelectedCore {
			chromeVersion = fingerprintArgValue(effectiveFingerprintArgs, "--fingerprint-brand-version=")
		}
		if chromeVersion == "" {
			chromeVersion = a.browserMgr.GetChromeVersion(selectedCore.CorePath)
		}
		if chromeVersion != "" {
			chromeUA := buildChromeUAFromFingerprintArgs(chromeVersion, effectiveFingerprintArgs)
			args = append(args, fmt.Sprintf("--user-agent=%s", chromeUA))
			log.Info("已设置 Chrome UA 启动参数",
				logger.F("profile_id", profileId),
				logger.F("version", chromeVersion),
				logger.F("cloak", isCloakSelectedCore),
			)

			// 跟随上游 Ant-Browser：不再强制注入内置 Header Fix 扩展。
			// 该 DNR 扩展会在 chrome://extensions/工具栏里显示成一个折叠/异常的内置扩展，
			// 且与当前内置 Google Chrome 内核自身的 UA-CH 能力重复。
		}
	}

	args = append(args, effectiveFingerprintArgs...)
	args = append(args, sanitizedProfileLaunchArgs...)
	args = append(args, sanitizedExtraLaunchArgs...)
	args = appendChromeTestingInfobarSuppressArg(args, isCloakSelectedCore)

	// cloak 路径下额外剥掉几个会暴露 chromium 身份的 launch arg：
	//   - --extension-mime-request-handling   (Chromium-only debug switch)
	//   - --disable-sync                       (经常被 anti-bot 当作 chromium 信号)
	// 它们多半来自 config.yaml 的 default_launch_args，删掉对正常使用无影响。
	if isCloakSelectedCore {
		stripPrefixes := []string{
			"--extension-mime-request-handling",
			"--disable-sync",
		}
		filtered := args[:0]
		for _, arg := range args {
			drop := false
			low := strings.ToLower(strings.TrimSpace(arg))
			for _, p := range stripPrefixes {
				if strings.EqualFold(low, p) || strings.HasPrefix(low, p+"=") {
					drop = true
					break
				}
			}
			if !drop {
				filtered = append(filtered, arg)
			}
		}
		args = filtered
	}

	// CloakBrowser 内核：通过代理 IP 反推 timezone/locale，避免
	// "VPN: timezone mismatch" 误判（fingerprint.com Smart Signals）。
	//   - 有代理：查询 ipapi.co/json，把得到的 timezone + 主语言追加为
	//     --fingerprint-timezone / --fingerprint-locale / --lang
	//   - 无代理 / direct://：不追加，让浏览器跟随系统时区+语言
	//
	// 注意：profile 里旧的 stale 值（用户曾经写死的 Asia/Shanghai / zh-CN 之类）
	// 在 cloak 模式必须被 geoip 覆盖，否则代理切到日本 IP 还跑 Asia/Shanghai
	// 时区，fingerprint.com 直接判 VPN timezone mismatch 红灯。
	if isCloakSelectedCore {
		// 默认开启 chrome://flags / extension-mime-request-handling = "Always prompt for install"。
		// 不开这个 flag，cloak 内核里从 chromewebstore.google.com 下载 .crx 不会自动弹
		// "添加扩展程序？"对话框（用户得手动拖到 chrome://extensions），开了就和普通 Chrome
		// 一样下载完直接弹安装。
		if err := ensureCloakLocalStateFlags(userDataDir); err != nil {
			logger.New("CloakFlags").Warn("写入 cloak 默认 flags 失败（不阻塞启动）",
				logger.F("profile_id", profileId),
				logger.F("user_data_dir", userDataDir),
				logger.F("error", err.Error()),
			)
		}

		if geoArgs := resolveCloakGeoArgs(effectiveProxy); len(geoArgs) > 0 {
			geoKeys := make(map[string]struct{}, len(geoArgs))
			for _, ga := range geoArgs {
				geoKeys[launchArgKey(ga)] = struct{}{}
			}
			filtered := args[:0]
			for _, existing := range args {
				if _, isGeoOverride := geoKeys[launchArgKey(existing)]; isGeoOverride {
					continue
				}
				filtered = append(filtered, existing)
			}
			args = append(filtered, geoArgs...)
			log.Info("CloakBrowser 内核已根据代理 IP 注入 timezone/locale（覆盖 stale 值）",
				logger.F("profile_id", profileId),
				logger.F("geo_args", strings.Join(geoArgs, " ")),
			)
		}

		// chromium-web-store helper 注入：cloak/ungoogled-chromium 默认禁用了
		// CWS inline install（"添加至 Chrome"按钮变成下载 .crx）。NeverDecaf 写的
		// chromium-web-store helper 扩展能恢复这个能力。
		//
		// 之前 v1.1.0 把 helper 路径硬编码成开发机 <app-root>\... ，
		// 一旦用户那边路径不存在 → helper 加载失败 → Web Store 装扩展直接报
		// "无法从该网站添加应用、扩展程序"。
		//
		// 修复：startup 已经把 helper 解压到 <appRoot>/extensions/chromium-web-store，
		// 这里把所有 --load-extension= 里的旧 helper 路径替换成规范路径，再 dedupe。
		profileHelper, helperErr := ensureProfileCloakWebStoreHelper(a.appRoot, userDataDir, profileId, a.launchServer.Port(), a.launchServer.APIAuthHeader(), a.launchServer.APIAuthKey())
		if helperErr != nil {
			log.Warn("准备 profile 专属 chromium-web-store helper 失败（将回退共享 helper）",
				logger.F("profile_id", profileId),
				logger.F("error", helperErr.Error()),
			)
			profileHelper = cloakWebStoreHelperPath(a.appRoot)
		}
		if profileHelper != "" {
			cleaned := args[:0]
			helperReplaced := false
			for _, arg := range args {
				trimmed := strings.TrimSpace(arg)
				if strings.HasPrefix(trimmed, "--load-extension=") {
					value := strings.TrimPrefix(trimmed, "--load-extension=")
					kept := []string{}
					for _, part := range strings.Split(value, ",") {
						p := strings.TrimSpace(part)
						if p == "" {
							continue
						}
						if looksLikeStaleCloakExtensionPath(p, a.appRoot) || strings.EqualFold(filepath.Clean(p), filepath.Clean(cloakWebStoreHelperPath(a.appRoot))) || strings.EqualFold(filepath.Clean(p), filepath.Clean(profileCloakWebStoreHelperPath(userDataDir))) {
							helperReplaced = true
							continue
						}
						kept = append(kept, p)
					}
					if len(kept) > 0 {
						cleaned = append(cleaned, "--load-extension="+strings.Join(kept, ","))
					}
					continue
				}
				cleaned = append(cleaned, arg)
			}
			args = cleaned
			// 始终把当前 profile 专属 helper 路径加进去（normalizeLoadExtensionArgs 后续会 dedupe）。
			if _, err := os.Stat(profileHelper); err == nil {
				args = append(args, "--load-extension="+profileHelper)
				if helperReplaced {
					log.Info("已用 profile 专属 chromium-web-store helper 替换旧路径",
						logger.F("profile_id", profileId),
						logger.F("helper", profileHelper),
					)
				}
			}
		}
	}

	args = normalizeLoadExtensionArgs(args)
	enableDeveloperModeForManagedUnpackedExtensions(userDataDir, args)
	// 清理 profile 中旧的 unpacked 扩展记录，避免同一个钱包/Header Fix 因旧路径残留显示两份。
	cleanupStaleManagedUnpackedExtensions(userDataDir, args, a.appRoot)
	pinAllLoadedExtensionsToToolbar(userDataDir, args)
	// 不在启动参数中传入目标 URL，让浏览器先以 about:blank 启动。
	// 等 CDP 就绪后先注入 stealth + UA override（确保 Sec-CH-UA 和 navigator.userAgentData
	// 在目标页面首次请求前就正确），然后再通过 CDP Page.navigate 导航到目标 URL。
	// 这解决了 Chrome Web Store 首次请求时 Sec-CH-UA 仍为 "Chromium" 导致
	// 显示「切换到 Chrome」横幅的问题。
	targetURLs := buildTargetURLs(profile, normalizedStartURLs, skipDefaultStartURLs)

	cmd := exec.Command(chromeBinaryPath, args...)
	cmd.Dir = filepath.Dir(chromeBinaryPath)
	monitor, err := newBrowserProcessMonitor(cmd)
	if err != nil {
		startErr := fmt.Errorf("实例启动失败：无法建立浏览器错误输出捕获。可执行文件：%s。原因：%v。", chromeBinaryPath, err)
		log.Error("浏览器错误输出捕获初始化失败", logger.F("profile_id", profileId), logger.F("chrome", chromeBinaryPath), logger.F("error", err.Error()), logger.F("reason", startErr.Error()))
		profile.LastError = startErr.Error()
		return profile, startErr
	}
	if err := cmd.Start(); err != nil {
		startErr := fmt.Errorf("%s", describeChromeProcessStartError(chromeBinaryPath, err))
		log.Error("浏览器进程启动失败", logger.F("profile_id", profileId), logger.F("chrome", chromeBinaryPath), logger.F("error", err.Error()), logger.F("reason", startErr.Error()))
		profile.LastError = startErr.Error()
		return profile, startErr
	}
	monitor.Start()

	for attempt := 1; attempt <= maxStartAttempts; attempt++ {
		stableDebugPort, readyErr := waitBrowserDebugPortStable(assignedDebugPort, userDataDir, startReadyTimeout, startStableWindow, monitor)
		if readyErr == nil {
			a.markProfileRunningLocked(profileId, profile, cmd, cmd.Process.Pid, stableDebugPort, true, "")
			if acquiredXrayBridgeKey != "" {
				a.bindProfileXrayBridge(profileId, acquiredXrayBridgeKey)
				releaseXrayBridge = false
			}
			acquiredStandardRelay = false

			log.Info("实例启动",
				logger.F("profile_id", profileId),
				logger.F("debug_port", stableDebugPort),
				logger.F("pid", profile.Pid),
				logger.F("proxy", effectiveProxy),
				logger.F("attempt", attempt),
				logger.F("max_attempts", maxStartAttempts),
				logger.F("args", strings.Join(args, " ")),
			)

			// 调试接口一就绪，先执行启动期扩展弹窗抑制：关闭 Chrome 自动恢复/自动
			// 打开的 extension 页面/窗口，再把真正的浏览器主窗口恢复到可见位置。
			// 之前 closeExtensionStartupPages/restoreBrowserWindowsAfterStartup 已实现
			// 但没有接入启动链路，导致扩展首次安装页/解锁页在后续每次启动仍可能被
			// Chrome 拉起并留在前台。
			finalizeBrowserStartupExtensionSuppression(stableDebugPort, profile.Pid, profileId)

			// 任务栏 badge 数字直接来自实例名字里的数字段：
			//   名字 "1"        → badge 1
			//   名字 "11"       → badge 11
			//   名字 "实例-11"  → badge 11
			// 这样改名后 badge 会跟着变，不再依赖排序位置。
			// 名字里完全没数字时回退到当前 Profiles map 顺序号，保证至少有个非 0 值
			// （setBadgeForInstance 在 displayNumber<=0 时会跳过）。
			displayNumber := extractBadgeNumberFromName(profile.ProfileName)
			if displayNumber <= 0 {
				idx := 0
				for _, p := range a.browserMgr.Profiles {
					idx++
					if p.ProfileId == profileId {
						displayNumber = idx
						break
					}
				}
			}

			// 同步注入反检测隐身脚本 + UA 覆写（必须在导航到目标 URL 前完成，
			// 否则 Chrome Web Store 的首次请求仍会携带错误的 Sec-CH-UA）
			//
			// CloakBrowser 内核时完全跳过 wrapper 级 CDP 注入：
			//   - cloak 内核已在 C++ 源码层面修复了所有 navigator/chrome.* 字段
			//   - wrapper 再用 Page.addScriptToEvaluateOnNewDocument 重叠注入会
			//     被 Fingerprint Pro 等深度检测识别成 "Browser Tampering" 与
			//     "Bot: nodriver" 双红灯（CDP 注入痕迹是 nodriver 检测的核心信号）
			//   - 同理 Turnstile 自动点击也跳过：cloak 已经能让 CF 验证以人类
			//     身份直接通过，无需再走 CDP Input 路径制造可被识别的 trusted=false
			//     mouse event 序列
			if isCloakSelectedCore {
				// 不注入 wrapper stealth，避免破坏 Cloak 自带指纹；但必须保留
				// Chrome UA/UA-CH。否则 Chrome Web Store 会把页面识别成 Chromium，
				// 显示“切换到 Chrome”并禁用“添加至 Chrome”。
				if uaErr := applyUserAgentOverrideToAllPages(stableDebugPort); uaErr != nil {
					log.Warn("CloakBrowser Chrome UA/UA-CH 覆写失败（非致命）",
						logger.F("profile_id", profileId),
						logger.F("debug_port", stableDebugPort),
						logger.F("error", uaErr.Error()),
					)
				} else {
					log.Info("CloakBrowser 已应用 Chrome UA/UA-CH 覆写（保留原生指纹隐身）",
						logger.F("profile_id", profileId),
						logger.F("debug_port", stableDebugPort),
					)
				}
				log.Info("CloakBrowser 内核启用，已跳过 wrapper 级 stealth/Turnstile 注入",
					logger.F("profile_id", profileId),
					logger.F("debug_port", stableDebugPort),
				)
				// Cloak 内核下只能走 runtime CDP seed。它在后台打开临时 settings tab，
				// 通过与用户手动“添加 → 设为默认”同一条 UI 路径写入 TemplateURLService。
				// settings/private API 在 brand-new profile 上常晚于 debug port 就绪，因此
				// 这里必须异步重试；成功后会写 .boost_search_seeded marker，后续启动跳过。
				go seedDefaultSearchEngineViaCDPWithRetry(userDataDir, stableDebugPort, 8, 1500*time.Millisecond)
			} else {
				if stealthErr := injectStealthToAllPagesWithUA(stableDebugPort, true); stealthErr != nil {
					log.Warn("反检测脚本注入失败（非致命）",
						logger.F("profile_id", profileId),
						logger.F("debug_port", stableDebugPort),
						logger.F("error", stealthErr.Error()),
					)
				} else {
					log.Info("反检测脚本注入成功",
						logger.F("profile_id", profileId),
						logger.F("debug_port", stableDebugPort),
					)
				}
			}

			importedCookieCount, skippedCookieCount, cookieErr := applyPendingImportedCookieSeed(stableDebugPort, userDataDir)
			if cookieErr != nil {
				log.Warn("导入环境 Cookie 注入失败",
					logger.F("profile_id", profileId),
					logger.F("debug_port", stableDebugPort),
					logger.F("error", cookieErr.Error()),
				)
			} else if importedCookieCount > 0 {
				log.Info("导入环境 Cookie 注入成功",
					logger.F("profile_id", profileId),
					logger.F("debug_port", stableDebugPort),
					logger.F("applied", importedCookieCount),
					logger.F("skipped", skippedCookieCount),
				)
			}

			// stealth 注入完成后，通过 CDP 导航到目标 URL
			// （浏览器以 about:blank 启动，首页面不载入任何真正的网站）
			//
			// CloakBrowser 内核分支只用 Target.createTarget(url) 直接打开标签页，
			// 完全跳过 Page.addScriptToEvaluateOnNewDocument + Emulation.setUserAgentOverride，
			// 否则 nodriver / Browser Tampering 检测会捕获到 CDP 注入痕迹。
			if len(targetURLs) > 0 {
				navigateToTargetURLs(stableDebugPort, targetURLs, profileId, isCloakSelectedCore)
			}

			// crashprobe: 临时停用实例启动后的 Turnstile 自动点击监控，继续收缩每实例后台
			// CDP 监控/注入链路，验证是否仍会出现 watchdog exit_code=2。
			// if !isCloakSelectedCore {
			// 	go startTurnstileMonitor(stableDebugPort, profileId)
			// }

			// crashprobe: 先停用每实例 popup/devtools 长驻巡检。
			// 最近 1.7.2 的 packaged 日志已证明：global-window-watchers 与 tray 都是 disabled，
			// 但宿主仍出现 watchdog-restart | unexpected-parent-exit | exit_code=2。
			// 当前更高嫌疑是 startExtensionPopupSizer 在多实例下各起长期 ticker，且共享
			// clampPidSet / restoreDevToolsPidSet 这类全局可变状态；先关掉验证稳定性，后续若
			// 要恢复弹窗修复，再改成串行 worker 或启动期短时巡检。
			// startExtensionPopupSizer(profile.Pid)

			// 恢复实例数字 badge，但只走一次性异步设置；底层 setBadgeForInstance 已改成
			// 启动阶段有限重试，不再持有长期 watchdog。
			go func() {
				defer func() {
					if r := recover(); r != nil {
						logger.New("Browser").Error("badge icon goroutine panic recovered",
							logger.F("profile_id", profileId),
							logger.F("error", r),
						)
					}
				}()
				if displayNumber > 0 && profile.Pid > 0 {
					if badgeErr := setBadgeForInstance(profile.Pid, displayNumber); badgeErr != nil {
						log.Warn("任务栏 badge 图标设置失败（非致命）",
							logger.F("profile_id", profileId),
							logger.F("pid", profile.Pid),
							logger.F("display_number", displayNumber),
							logger.F("error", badgeErr.Error()),
						)
					}
				}
			}()

			a.emitBrowserInstanceStarted(profile, false)

			go func() {
				defer func() {
					if r := recover(); r != nil {
						logger.New("Browser").Error("waitBrowserProcess goroutine panic recovered",
							logger.F("profile_id", profileId),
							logger.F("error", r),
						)
					}
				}()
				a.waitBrowserProcess(profileId, monitor)
			}()
			return profile, nil
		}

		startErr := fmt.Errorf("%s", describeBrowserReadyFailure(chromeBinaryPath, assignedDebugPort, totalReadyTimeout, readyErr))
		lastStartErr = startErr
		log.Error("浏览器启动未就绪",
			logger.F("profile_id", profileId),
			logger.F("chrome", chromeBinaryPath),
			logger.F("debug_port", assignedDebugPort),
			logger.F("attempt", attempt),
			logger.F("max_attempts", maxStartAttempts),
			logger.F("error", readyErr.Error()),
			logger.F("reason", startErr.Error()),
		)

		if attempt < maxStartAttempts && shouldRetryBrowserReadyFailure(readyErr) {
			log.Warn("浏览器启动未就绪，继续检测",
				logger.F("profile_id", profileId),
				logger.F("debug_port", assignedDebugPort),
				logger.F("attempt", attempt),
				logger.F("next_attempt", attempt+1),
				logger.F("max_attempts", maxStartAttempts),
				logger.F("timeout_ms", startReadyTimeout.Milliseconds()),
			)
			continue
		}

		break
	}

	pendingStartNotice := ""
	if shouldKeepBrowserRunningPendingDebugReady(assignedDebugPort, monitor) {
		runtimeWarning := browserDebugPendingWarning(totalReadyTimeout)
		pendingStartNotice = browserDebugPendingStartNotice(totalReadyTimeout)
		a.markProfileRunningLocked(profileId, profile, cmd, cmd.Process.Pid, assignedDebugPort, false, runtimeWarning)
		if acquiredXrayBridgeKey != "" {
			a.bindProfileXrayBridge(profileId, acquiredXrayBridgeKey)
			releaseXrayBridge = false
		}
		acquiredStandardRelay = false

		log.Warn("浏览器窗口已启动，但调试接口在等待窗口内未就绪，转入后台附着",
			logger.F("profile_id", profileId),
			logger.F("debug_port", assignedDebugPort),
			logger.F("pid", profile.Pid),
			logger.F("max_attempts", maxStartAttempts),
			logger.F("warning", runtimeWarning),
		)
		a.emitBrowserInstanceStarted(profile, false)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.New("Browser").Error("waitBrowserProcess goroutine panic recovered",
						logger.F("profile_id", profileId),
						logger.F("error", r),
					)
				}
			}()
			a.waitBrowserProcess(profileId, monitor)
		}()
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.New("Browser").Error("waitBrowserDebugReadyAsync goroutine panic recovered",
						logger.F("profile_id", profileId),
						logger.F("error", r),
					)
				}
			}()
			a.waitBrowserDebugReadyAsync(profileId, assignedDebugPort, browserAsyncDebugAttachTimeout)
		}()
	}

	if pendingStartNotice != "" {
		profile.LastError = pendingStartNotice
		return profile, fmt.Errorf("%s", pendingStartNotice)
	}

	if lastStartErr != nil {
		profile.LastError = lastStartErr.Error()
		return profile, lastStartErr
	}
	return profile, fmt.Errorf("实例启动失败：浏览器在等待窗口内仍未就绪")
}

func (a *App) BrowserInstanceStop(profileId string) (*BrowserProfile, error) {
	log := logger.New("Browser")
	a.browserMgr.Mutex.Lock()
	defer a.browserMgr.Mutex.Unlock()

	profile, exists := a.browserMgr.Profiles[profileId]
	if !exists {
		return nil, fmt.Errorf("profile not found")
	}

	cmd := a.browserMgr.BrowserProcesses[profileId]
	debugPort := profile.DebugPort
	// Browser.close 成功后调试端口会立即断开；如果等 markProfileStoppedLocked
	// 再抓标签页，通常已经太晚，只能保留旧的 LastTabs。这里趁 CDP 还活着先保存
	// 当前普通 http(s) 标签页，后续 markProfileStoppedLocked 仍会做一次兜底。
	if debugPort > 0 {
		if tabs := captureRestorableTabsViaCDP(debugPort); len(tabs) > 0 {
			a.updateProfileLastTabsLocked(profile, tabs)
		}
	}
	if tryCloseBrowserViaCDP(debugPort, 5*time.Second) {
		a.markProfileStoppedLocked(profileId, profile)
		log.Info("实例停止", logger.F("profile_id", profileId), logger.F("method", "cdp"), logger.F("debug_port", debugPort))
		return profile, nil
	}

	if cmd != nil && cmd.Process != nil {
		if err := a.stopBrowserProcess(cmd); err != nil {
			log.Error("实例停止失败", logger.F("profile_id", profileId), logger.F("error", err))
			profile.LastError = err.Error()
			return profile, err
		}
	}

	if debugPort > 0 && canConnectDebugPort(debugPort, 250*time.Millisecond) {
		err := fmt.Errorf("实例停止失败：浏览器仍在运行（调试端口 %d 仍可访问）", debugPort)
		log.Error("实例停止失败", logger.F("profile_id", profileId), logger.F("debug_port", debugPort), logger.F("reason", err.Error()))
		profile.LastError = err.Error()
		return profile, err
	}

	a.markProfileStoppedLocked(profileId, profile)
	log.Info("实例停止", logger.F("profile_id", profileId))
	return profile, nil
}

func (a *App) BrowserInstanceRestart(profileId string) (*BrowserProfile, error) {
	if _, err := a.BrowserInstanceStop(profileId); err != nil {
		return nil, err
	}
	return a.BrowserInstanceStart(profileId)
}

// BrowserProfileBatchSetTags 批量为实例设置标签（追加模式：将 tags 加入已有标签；replace 模式：直接替换）
func (a *App) BrowserProfileBatchSetTags(profileIds []string, tags []string, replace bool) error {
	log := logger.New("Browser")
	a.browserMgr.Mutex.Lock()
	defer a.browserMgr.Mutex.Unlock()

	for _, profileId := range profileIds {
		profile, exists := a.browserMgr.Profiles[profileId]
		if !exists {
			continue
		}
		if replace {
			profile.Tags = tags
		} else {
			// 追加去重
			existing := make(map[string]struct{})
			for _, t := range profile.Tags {
				existing[t] = struct{}{}
			}
			for _, t := range tags {
				if _, ok := existing[t]; !ok {
					profile.Tags = append(profile.Tags, t)
					existing[t] = struct{}{}
				}
			}
		}
		profile.UpdatedAt = time.Now().Format(time.RFC3339)
		if a.browserMgr.ProfileDAO != nil {
			if err := a.browserMgr.ProfileDAO.Upsert(profile); err != nil {
				log.Error("批量设置标签失败", logger.F("profile_id", profileId), logger.F("error", err))
				return err
			}
		}
	}
	return nil
}

// BrowserProfileBatchRemoveTags 批量从实例移除指定标签
func (a *App) BrowserProfileBatchRemoveTags(profileIds []string, tags []string) error {
	log := logger.New("Browser")
	a.browserMgr.Mutex.Lock()
	defer a.browserMgr.Mutex.Unlock()

	removeSet := make(map[string]struct{})
	for _, t := range tags {
		removeSet[t] = struct{}{}
	}

	for _, profileId := range profileIds {
		profile, exists := a.browserMgr.Profiles[profileId]
		if !exists {
			continue
		}
		filtered := profile.Tags[:0]
		for _, t := range profile.Tags {
			if _, ok := removeSet[t]; !ok {
				filtered = append(filtered, t)
			}
		}
		profile.Tags = filtered
		profile.UpdatedAt = time.Now().Format(time.RFC3339)
		if a.browserMgr.ProfileDAO != nil {
			if err := a.browserMgr.ProfileDAO.Upsert(profile); err != nil {
				log.Error("批量移除标签失败", logger.F("profile_id", profileId), logger.F("error", err))
				return err
			}
		}
	}
	return nil
}

// BrowserRenameTag 重命名所有实例中的指定标签
func (a *App) BrowserRenameTag(oldName string, newName string) error {
	log := logger.New("Browser")
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("标签名称不能为空")
	}

	a.browserMgr.Mutex.Lock()
	defer a.browserMgr.Mutex.Unlock()

	changedCount := 0
	for profileId, profile := range a.browserMgr.Profiles {
		tagChanged := false
		var newTags []string
		for _, t := range profile.Tags {
			if strings.EqualFold(t, oldName) {
				newTags = append(newTags, newName)
				tagChanged = true
			} else {
				newTags = append(newTags, t)
			}
		}

		if tagChanged {
			// 去重
			uniqueTags := make([]string, 0)
			seen := make(map[string]struct{})
			for _, t := range newTags {
				if _, ok := seen[t]; !ok {
					uniqueTags = append(uniqueTags, t)
					seen[t] = struct{}{}
				}
			}

			profile.Tags = uniqueTags
			profile.UpdatedAt = time.Now().Format(time.RFC3339)
			if a.browserMgr.ProfileDAO != nil {
				if err := a.browserMgr.ProfileDAO.Upsert(profile); err != nil {
					log.Error("重命名标签保存失败", logger.F("profile_id", profileId), logger.F("error", err))
					return err
				}
			}
			changedCount++
		}
	}

	if changedCount > 0 && a.browserMgr.ProfileDAO == nil {
		if err := a.browserMgr.SaveProfiles(); err != nil {
			return err
		}
	}

	if changedCount > 0 {
		log.Info("重命名标签成功", logger.F("old", oldName), logger.F("new", newName), logger.F("changed_profiles", changedCount))
	}
	return nil
}

func (a *App) BrowserInstanceStatus(profileId string) (*BrowserProfile, error) {
	a.browserMgr.Mutex.Lock()
	defer a.browserMgr.Mutex.Unlock()
	profile, exists := a.browserMgr.Profiles[profileId]
	if !exists {
		return nil, fmt.Errorf("profile not found")
	}
	return profile, nil
}

func (a *App) BrowserInstanceOpenUrl(profileId string, targetUrl string) bool {
	a.browserMgr.Mutex.Lock()
	profile, exists := a.browserMgr.Profiles[profileId]
	a.browserMgr.Mutex.Unlock()
	if !exists || !profile.Running {
		return false
	}
	return true
}

func (a *App) BrowserInstanceGetTabs(profileId string) []BrowserTab {
	return []BrowserTab{
		{TabId: "tab-1", Title: "新标签页", Url: "about:blank", Active: true},
		{TabId: "tab-2", Title: "示例站点", Url: "https://example.com", Active: false},
	}
}

func (a *App) waitBrowserProcess(profileId string, monitor *browserProcessMonitor) {
	err := monitor.Wait()

	log := logger.New("Browser")
	debugPort := 0
	profileName := profileId
	shouldMonitorDetached := false

	a.browserMgr.Mutex.Lock()
	profile, exists := a.browserMgr.Profiles[profileId]
	wasRunning := exists && profile.Running
	if exists {
		profileName = profile.ProfileName
		debugPort = profile.DebugPort
	}
	a.browserMgr.Mutex.Unlock()

	if wasRunning && debugPort > 0 {
		snapshot, changed := a.waitForBrowserDebugReady(profileId, debugPort, browserLauncherDetachGraceWindow)
		if snapshot != nil {
			if changed {
				log.Info("浏览器启动器进程退出后，调试接口延迟就绪",
					logger.F("profile_id", profileId),
					logger.F("debug_port", debugPort),
				)
				a.emitBrowserInstanceUpdated(snapshot)
			}
		}

		a.browserMgr.Mutex.Lock()
		profile, exists = a.browserMgr.Profiles[profileId]
		if exists && profile.Running && profile.DebugPort == debugPort && profile.DebugReady && canConnectDebugPort(debugPort, 250*time.Millisecond) {
			delete(a.browserMgr.BrowserProcesses, profileId)
			profile.Pid = 0
			shouldMonitorDetached = true
		}
		a.browserMgr.Mutex.Unlock()
		if shouldMonitorDetached {
			log.Info("浏览器启动器进程已退出，切换为调试端口存活监控",
				logger.F("profile_id", profileId),
				logger.F("profile_name", profileName),
				logger.F("debug_port", debugPort),
			)
			a.waitDetachedBrowser(profileId, debugPort)
			return
		}
	}

	a.browserMgr.Mutex.Lock()
	profile, exists = a.browserMgr.Profiles[profileId]
	wasRunning = exists && profile.Running
	if exists {
		profileName = profile.ProfileName
		a.markProfileStoppedLocked(profileId, profile)
	}
	a.browserMgr.Mutex.Unlock()

	if a.ctx == nil {
		return
	}

	// 进程是正常退出（用户手动关闭）还是异常崩溃
	if wasRunning && err != nil {
		// 异常退出，推送崩溃通知
		if exists && profile != nil {
			profile.LastError = fmt.Sprintf("实例运行异常退出：%s", err.Error())
		}
		log.Error("浏览器进程异常退出", logger.F("profile_id", profileId), logger.F("profile_name", profileName), logger.F("error", err))
		runtime.EventsEmit(a.ctx, "browser:instance:crashed", map[string]interface{}{
			"profileId":   profileId,
			"profileName": profileName,
			"error":       err.Error(),
		})
	} else {
		runtime.EventsEmit(a.ctx, "browser:instance:stopped", profileId)
	}
}

func (a *App) waitDetachedBrowser(profileId string, debugPort int) {
	const (
		pollInterval = 500 * time.Millisecond
		maxMisses    = 3
	)

	log := logger.New("Browser")
	misses := 0
	for {
		if canConnectDebugPort(debugPort, 250*time.Millisecond) {
			misses = 0
			time.Sleep(pollInterval)
			continue
		}

		misses++
		if misses < maxMisses {
			time.Sleep(pollInterval)
			continue
		}

		profileName := profileId
		a.browserMgr.Mutex.Lock()
		profile, exists := a.browserMgr.Profiles[profileId]
		if !exists || !profile.Running || profile.DebugPort != debugPort {
			a.browserMgr.Mutex.Unlock()
			return
		}
		profileName = profile.ProfileName
		a.markProfileStoppedLocked(profileId, profile)
		a.browserMgr.Mutex.Unlock()

		log.Info("检测到浏览器调试端口关闭，实例已停止",
			logger.F("profile_id", profileId),
			logger.F("profile_name", profileName),
			logger.F("debug_port", debugPort),
		)
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "browser:instance:stopped", profileId)
		}
		return
	}
}

func tryCloseBrowserViaCDP(debugPort int, timeout time.Duration) bool {
	if debugPort <= 0 || !canConnectDebugPort(debugPort, 250*time.Millisecond) {
		return false
	}

	_ = cdpBrowserCall(debugPort, "Browser.close", nil)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !canConnectDebugPort(debugPort, 250*time.Millisecond) {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}

func normalizeNonEmptyStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		v := strings.TrimSpace(item)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func ensureNewWindowLaunchArg(args []string) []string {
	for _, arg := range args {
		if strings.EqualFold(strings.TrimSpace(arg), "--new-window") {
			return args
		}
	}
	return append(args, "--new-window")
}

func appendLaunchTargets(args []string, profile *BrowserProfile, startURLs []string, skipDefaultStartURLs bool) []string {
	if len(startURLs) > 0 {
		return append(args, startURLs...)
	}
	if !skipDefaultStartURLs {
		return browser.BuildLaunchArgs(args, profile)
	}
	return args
}

// buildTargetURLs 收集浏览器启动后需要导航到的目标 URL 列表。
// 优先级：本次显式传入 startURLs > 上次保存的普通网页标签页 > 默认验证页。
func buildTargetURLs(profile *BrowserProfile, startURLs []string, skipDefaultStartURLs bool) []string {
	if len(startURLs) > 0 {
		return startURLs
	}
	if !skipDefaultStartURLs {
		if profile != nil && len(profile.LastTabs) > 0 {
			return append([]string{}, profile.LastTabs...)
		}
		return browser.GetDefaultVerificationURLs()
	}
	return nil
}

func (a *App) markProfileStoppedLocked(profileId string, profile *BrowserProfile) {
	if profile == nil {
		return
	}
	// 清空 DebugPort 之前最后抓一次当前普通网页标签页。手动点“关闭实例”时可以即时保存，
	// 用户直接关浏览器窗口时则由运行期 tracker 兜底保存最近一次状态。
	if profile.DebugPort > 0 {
		if tabs := captureRestorableTabsViaCDP(profile.DebugPort); len(tabs) > 0 {
			a.updateProfileLastTabsLocked(profile, tabs)
		}
	}
	stopLastTabsTracker(profileId)

	// 先尝试拿到最新一次窗口 bounds 并写入 profile + 落盘。
	// 必须在清空 DebugPort 之前调用，因为 finalize 会用 tracker 里记的 debugPort。
	a.stopWindowBoundsTrackerAndFinalize(profileId, profile)

	profile.Running = false
	profile.DebugReady = false
	profile.Pid = 0
	profile.DebugPort = 0
	profile.RuntimeWarning = ""
	profile.LastStopAt = time.Now().Format(time.RFC3339)
	delete(a.browserMgr.BrowserProcesses, profileId)
	a.releaseProfileXrayBridge(profileId)
	a.releaseProfileStandardRelay(profileId)
	if a.launchServer != nil {
		a.launchServer.ClearActiveProfile(profileId)
	}
}

func (a *App) openBrowserWindowForRunningProfile(profile *BrowserProfile, extraLaunchArgs []string, startURLs []string) error {
	chromeBinaryPath, err := a.browserMgr.ResolveChromeBinary(profile)
	if err != nil {
		return err
	}

	userDataDir := a.browserMgr.ResolveUserDataDir(profile)
	if err := os.MkdirAll(userDataDir, 0755); err != nil {
		return fmt.Errorf("无法创建用户数据目录 %s：%w", userDataDir, err)
	}

	args := []string{
		fmt.Sprintf("--user-data-dir=%s", userDataDir),
	}

	// 跟随上游 Ant-Browser：已运行实例打开新窗口时也不再注入 Header Fix 扩展。
	coreId := strings.TrimSpace(profile.CoreId)
	var core browser.Core
	var coreFound bool
	if coreId != "" {
		core, coreFound = a.browserMgr.GetCore(coreId)
	}
	if !coreFound {
		core, coreFound = a.browserMgr.GetDefaultCore()
	}
	isCloakCoreForOpen := coreFound && isCloakCore(core, chromeBinaryPath)
	if coreFound && core.CorePath != "" && !isCloakCoreForOpen {
		// CloakBrowser 内核自身按 --fingerprint seed 生成 UA/UA-CH，避免 wrapper 强制覆写引发 Browser Tampering。
		chromeVersion := a.browserMgr.GetChromeVersion(core.CorePath)
		if chromeVersion != "" {
			chromeUA := fmt.Sprintf(
				"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
				chromeVersion,
			)
			args = append(args, fmt.Sprintf("--user-agent=%s", chromeUA))
		}
	}

	sanitizedExtraLaunchArgs, managedExtraArgs := sanitizeManagedLaunchArgs(extraLaunchArgs)
	logManagedLaunchArgOverrides(logger.New("Browser"), profile.ProfileId, "running-window.extraLaunchArgs", managedExtraArgs)
	args = append(args, sanitizedExtraLaunchArgs...)
	args = appendChromeTestingInfobarSuppressArg(args, isCloakCoreForOpen)
	args = normalizeLoadExtensionArgs(args)
	enableDeveloperModeForManagedUnpackedExtensions(userDataDir, args)
	if len(startURLs) > 0 {
		args = append(args, startURLs...)
	} else {
		args = append(args, "about:blank")
	}

	cmd := exec.Command(chromeBinaryPath, args...)
	cmd.Dir = filepath.Dir(chromeBinaryPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s", describeChromeProcessStartError(chromeBinaryPath, err))
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.New("Browser").Error("browser cmd.Wait goroutine panic recovered",
					logger.F("error", r),
				)
			}
		}()
		_ = cmd.Wait()
	}()
	return nil
}

func (a *App) stopBrowserProcess(cmd *exec.Cmd) error {
	return a.stopProcessCmd(cmd)
}

func (a *App) stopProcessCmd(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Windows 下优先非强制 taskkill，尽量让 Chromium 走正常退出路径，减少“恢复页面”提示。
	if stdruntime.GOOS == "windows" {
		pid := cmd.Process.Pid
		if pid > 0 {
			softKillCmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid), "/T")
			hideWindow(softKillCmd)
			if err := softKillCmd.Run(); err == nil {
				if waitProcessExitWindows(pid, 3*time.Second) {
					return nil
				}
				forceKillCmd := exec.Command("taskkill", "/F", "/PID", fmt.Sprintf("%d", pid), "/T")
				hideWindow(forceKillCmd)
				if forceErr := forceKillCmd.Run(); forceErr == nil {
					_ = waitProcessExitWindows(pid, 2*time.Second)
					return nil
				}
			}
		}
	}

	err := cmd.Process.Kill()
	if err == nil || isProcessAlreadyFinished(err) {
		return nil
	}
	return err
}

func isProcessAlreadyFinished(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "process already finished") {
		return true
	}
	if strings.Contains(msg, "not found") {
		return true
	}
	if strings.Contains(msg, "no process") {
		return true
	}
	if strings.Contains(msg, "不存在") {
		return true
	}
	return false
}

func waitProcessExitWindows(pid int, timeout time.Duration) bool {
	if pid <= 0 {
		return true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		alive, err := isProcessAliveWindows(pid)
		if err == nil && !alive {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	alive, err := isProcessAliveWindows(pid)
	if err != nil {
		return false
	}
	return !alive
}

func isProcessAliveWindows(pid int) (bool, error) {
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return false, nil
	}
	if strings.HasPrefix(strings.ToUpper(line), "INFO:") {
		return false, nil
	}
	token := fmt.Sprintf("\"%d\",", pid)
	return strings.Contains(line, token), nil
}

// isChromeWebStoreURL 判断是否是需要 Chrome UA/UA-CH 的应用商店页面。
// 只匹配 Chrome Web Store，不影响普通业务页面的 Cloak 原生导航路径。
func isChromeWebStoreURL(rawURL string) bool {
	low := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.Contains(low, "chrome.google.com/webstore") ||
		strings.Contains(low, "chromewebstore.google.com/")
}

// navigateCloakWebStoreURL 为 Chrome Web Store 创建空白标签页，在首次请求前
// 只应用 Chrome UA/UA-CH 覆写，然后导航到真实商店 URL。这样不注入 wrapper
// stealth，也不会改变普通业务标签页的 Cloak 行为。
func navigateCloakWebStoreURL(browserConn *websocket.Conn, debugPort int, targetURL, profileID string, msgID int) bool {
	log := logger.New("Browser")
	targetID, err := createBlankTab(browserConn, msgID)
	if err != nil {
		log.Warn("CDP 导航(cloak webstore)：创建空白标签页失败",
			logger.F("profile_id", profileID), logger.F("url", targetURL), logger.F("error", err.Error()))
		return false
	}

	fixedUA, metadata, err := getUserAgentOverride(debugPort)
	if err != nil {
		log.Warn("CDP 导航(cloak webstore)：获取 Chrome UA 失败",
			logger.F("profile_id", profileID), logger.F("url", targetURL), logger.F("error", err.Error()))
		return false
	}
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/%s", debugPort, targetID)
	pageConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		log.Warn("CDP 导航(cloak webstore)：连接空白标签页失败",
			logger.F("profile_id", profileID), logger.F("url", targetURL), logger.F("error", err.Error()))
		return false
	}
	defer pageConn.Close()

	if err := setUserAgentOverrideOnTarget(wsURL, fixedUA, metadata); err != nil {
		log.Warn("CDP 导航(cloak webstore)：页面级 Chrome UA 覆写失败",
			logger.F("profile_id", profileID), logger.F("url", targetURL), logger.F("error", err.Error()))
		return false
	}

	navMsg := cdpMessage{Id: msgID + 1000, Method: "Page.navigate", Params: map[string]any{"url": targetURL}}
	if err := pageConn.WriteJSON(navMsg); err != nil {
		log.Warn("CDP 导航(cloak webstore)：导航写入失败",
			logger.F("profile_id", profileID), logger.F("url", targetURL), logger.F("error", err.Error()))
		return false
	}
	pageConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var navResp cdpResponse
	if err := pageConn.ReadJSON(&navResp); err != nil {
		log.Warn("CDP 导航(cloak webstore)：读取导航响应失败",
			logger.F("profile_id", profileID), logger.F("url", targetURL), logger.F("error", err.Error()))
		return false
	}
	if navResp.Error != nil {
		log.Warn("CDP 导航(cloak webstore)：Page.navigate 返回错误",
			logger.F("profile_id", profileID), logger.F("url", targetURL), logger.F("error", navResp.Error.Message))
		return false
	}
	log.Info("CDP 导航(cloak webstore)：已应用 Chrome UA/UA-CH 后导航",
		logger.F("profile_id", profileID), logger.F("url", targetURL), logger.F("targetId", targetID))
	return true
}

// navigateToTargetURLs 通过 CDP 将浏览器导航到目标 URL。
//
// 非 Cloak 内核（ungoogled-chromium 等）：
//
//	先创建空白标签页 → 注入 UA override + stealth JS（确保 Sec-CH-UA 在首次请求前就正确）
//	→ 再用 Page.navigate 导航到真实 URL。
//	这解决了 Chrome Web Store 检测 Sec-CH-UA 为 "Chromium" 而非 "Google Chrome"
//	导致显示「切换到 Chrome」横幅的问题。
//
// Cloak 内核（cloakOnly=true）：
//
//	只用 Target.createTarget(url) 直接打开目标 URL 的新标签页。完全不走
//	Page.addScriptToEvaluateOnNewDocument / Emulation.setUserAgentOverride，
//	因为：
//	1. cloak 已在 C++ 源码层面处理 UA / Sec-CH-UA / navigator.* 字段；
//	2. wrapper 端再次 CDP 注入会被 Fingerprint Pro 等检测识别成
//	   Browser Tampering / Bot: nodriver 双红灯。
func navigateToTargetURLs(debugPort int, urls []string, profileId string, cloakOnly bool) {
	log := logger.New("Browser")
	if len(urls) == 0 {
		return
	}

	// 获取 browser target 的 WebSocket URL（用于 Target.createTarget）
	browserWsURL, err := getBrowserWebSocketURL(debugPort)
	if err != nil {
		log.Warn("CDP 导航：获取浏览器 WebSocket 失败",
			logger.F("profile_id", profileId),
			logger.F("error", err.Error()),
		)
		return
	}

	browserConn, _, err := websocket.DefaultDialer.Dial(browserWsURL, nil)
	if err != nil {
		log.Warn("CDP 导航：浏览器 WebSocket 连接失败",
			logger.F("profile_id", profileId),
			logger.F("error", err.Error()),
		)
		return
	}
	browserConn.SetReadDeadline(time.Now().Add(15 * time.Second))

	// Cloak 内核：普通业务 URL 仍直接 Target.createTarget(url)，不注入 wrapper
	// stealth。Chrome Web Store 是例外：它会在首次请求前检查 UA/UA-CH，必须先
	// 创建 about:blank、只应用 Chrome UA 覆写，再导航到商店页面。
	if cloakOnly {
		for i, url := range urls {
			if isChromeWebStoreURL(url) && navigateCloakWebStoreURL(browserConn, debugPort, url, profileId, i+200) {
				continue
			}
			createMsg := cdpMessage{
				Id:     i + 200,
				Method: "Target.createTarget",
				Params: map[string]any{"url": url},
			}
			if err := browserConn.WriteJSON(createMsg); err != nil {
				log.Warn("CDP 导航(cloak)：Target.createTarget 写入失败",
					logger.F("profile_id", profileId),
					logger.F("url", url),
					logger.F("error", err.Error()),
				)
				continue
			}
			browserConn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var resp cdpResponse
			_ = browserConn.ReadJSON(&resp)
			log.Info("CDP 导航(cloak)：目标页面已打开（普通 URL 保留原生路径）",
				logger.F("profile_id", profileId),
				logger.F("url", url),
			)
		}
		browserConn.Close()
		return
	}

	// 获取 UA 覆写参数（将 Chromium 替换为 Chrome）—— 仅非 cloak 路径需要
	fixedUA, uaMetadata, uaErr := getUserAgentOverride(debugPort)
	if uaErr != nil {
		log.Warn("CDP 导航：获取 UA 覆写参数失败，将直接导航（可能导致 Chrome Web Store 检测异常）",
			logger.F("profile_id", profileId),
			logger.F("error", uaErr.Error()),
		)
	}

	// 逐个创建标签页：先 about:blank → 注入 → 再导航
	for i, url := range urls {
		targetId, createErr := createBlankTab(browserConn, i+1)
		if createErr != nil {
			log.Warn("CDP 导航：创建空白标签页失败，回退到直接导航",
				logger.F("profile_id", profileId),
				logger.F("url", url),
				logger.F("error", createErr.Error()),
			)
			// 回退：直接用 Target.createTarget(url) 创建
			fallbackMsg := cdpMessage{
				Id:     i + 100,
				Method: "Target.createTarget",
				Params: map[string]any{"url": url},
			}
			_ = browserConn.WriteJSON(fallbackMsg)
			var fallbackResp cdpResponse
			_ = browserConn.ReadJSON(&fallbackResp)
			continue
		}

		// 获取新标签页的 WebSocket URL
		targetWsURL := fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/%s", debugPort, targetId)

		// 连接到新标签页并注入 UA override + stealth JS
		pageConn, _, dialErr := websocket.DefaultDialer.Dial(targetWsURL, nil)
		if dialErr != nil {
			log.Warn("CDP 导航：连接新标签页失败，回退到直接导航",
				logger.F("profile_id", profileId),
				logger.F("url", url),
				logger.F("targetId", targetId),
				logger.F("error", dialErr.Error()),
			)
			continue
		}

		injectSuccess := false

		// 注入 stealth JS（Page.addScriptToEvaluateOnNewDocument）
		stealthMsg := cdpMessage{
			Id:     1,
			Method: "Page.addScriptToEvaluateOnNewDocument",
			Params: map[string]any{
				"source": stealthJS,
			},
		}
		if writeErr := pageConn.WriteJSON(stealthMsg); writeErr == nil {
			pageConn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var stealthResp cdpResponse
			_ = pageConn.ReadJSON(&stealthResp)
		}

		// 注入 UA override（Emulation.setUserAgentOverride）
		if fixedUA != "" && uaMetadata != nil {
			uaMsg := cdpMessage{
				Id:     2,
				Method: "Emulation.setUserAgentOverride",
				Params: map[string]any{
					"userAgent":         fixedUA,
					"platform":          "Win32",
					"userAgentMetadata": uaMetadata,
				},
			}
			if writeErr := pageConn.WriteJSON(uaMsg); writeErr == nil {
				pageConn.SetReadDeadline(time.Now().Add(5 * time.Second))
				var uaResp cdpResponse
				_ = pageConn.ReadJSON(&uaResp)
				injectSuccess = true
				log.Info("CDP 导航：UA override 注入成功",
					logger.F("profile_id", profileId),
					logger.F("targetId", targetId),
				)
			}
		} else {
			injectSuccess = true // 没有 UA override 也继续导航
		}

		if !injectSuccess {
			log.Warn("CDP 导航：UA override 注入失败，继续导航（可能触发 Chrome Web Store 横幅检测）",
				logger.F("profile_id", profileId),
				logger.F("targetId", targetId),
			)
		}

		// 导航到目标 URL
		navMsg := cdpMessage{
			Id:     3,
			Method: "Page.navigate",
			Params: map[string]any{
				"url": url,
			},
		}
		if navErr := pageConn.WriteJSON(navMsg); navErr != nil {
			log.Warn("CDP 导航：Page.navigate 失败",
				logger.F("profile_id", profileId),
				logger.F("url", url),
				logger.F("error", navErr.Error()),
			)
		} else {
			pageConn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var navResp cdpResponse
			_ = pageConn.ReadJSON(&navResp)
			log.Info("CDP 导航：目标页面已导航",
				logger.F("profile_id", profileId),
				logger.F("url", url),
				logger.F("targetId", targetId),
			)
		}
		pageConn.Close()
	}
	browserConn.Close()
}

// createBlankTab 通过 CDP Target.createTarget 创建一个 about:blank 空白标签页，
// 并返回新标签页的 targetId 用于后续注入和导航。
func createBlankTab(browserConn *websocket.Conn, msgId int) (string, error) {
	createMsg := cdpMessage{
		Id:     msgId,
		Method: "Target.createTarget",
		Params: map[string]any{
			"url": "about:blank",
		},
	}
	if err := browserConn.WriteJSON(createMsg); err != nil {
		return "", fmt.Errorf("发送 Target.createTarget 失败: %w", err)
	}

	var resp cdpResponse
	if err := browserConn.ReadJSON(&resp); err != nil {
		return "", fmt.Errorf("读取 Target.createTarget 响应失败: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("Target.createTarget 错误: %s", resp.Error.Message)
	}

	// 从 result 中提取 targetId
	result := resp.Result
	if result == nil {
		return "", fmt.Errorf("Target.createTarget 返回空 result")
	}
	targetId, ok := result["targetId"].(string)
	if !ok || targetId == "" {
		return "", fmt.Errorf("Target.createTarget 未返回有效的 targetId")
	}
	return targetId, nil
}
