package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"boost-browser/backend/internal/logger"

	"github.com/gorilla/websocket"
)

// stealthJS 是在页面加载前注入的精简反检测脚本。
// 仅覆盖 fingerprint-chromium 内核无法处理的自动化检测特征，
// 不覆盖内核已通过 --fingerprint-* 参数设置的指纹项。
const stealthJS = `
// === Anti-Automation Detection ===
// 只覆盖 fingerprint-chromium 不处理的自动化信号

// 1. Chrome automation 属性 — CF 通过检测这些判断自动化浏览器
if (window.chrome) {
    try {
        if (!window.chrome.csi) {
            Object.defineProperty(window.chrome, 'csi', {
                get: () => function() { return {}; },
                configurable: true, enumerable: true
            });
        }
        if (!window.chrome.loadTimes) {
            Object.defineProperty(window.chrome, 'loadTimes', {
                get: () => function() {
                    return {
                        requestTime: performance.timing.navigationStart / 1000,
                        startLoadTime: performance.timing.navigationStart / 1000,
                        commitLoadTime: performance.timing.responseStart / 1000,
                        finishDocumentLoadTime: performance.timing.domContentLoadedEventEnd / 1000,
                        finishLoadTime: performance.timing.loadEventEnd / 1000,
                        firstPaintTime: 0, firstPaintAfterLoadTime: 0,
                        navigationType: 'Other', wasFetchedViaSpdy: false,
                        wasNpnNegotiated: true, npnNegotiatedProtocol: 'h2',
                        wasAlternateProtocolAvailable: false, connectionInfo: 'h2'
                    };
                }, configurable: true, enumerable: true
            });
        }
    } catch(e) {}
}

// 2. chrome.app — ungoogled-chromium 移除了此 API，导致 Chrome Web Store
//    显示"切换到 Chrome"并禁止安装扩展。恢复此对象使 Web Store 识别为 Chrome。
if (window.chrome && !window.chrome.app) {
    try {
        Object.defineProperty(window.chrome, 'app', {
            get: () => ({
                isInstalled: false,
                getDetails: function() { return null; },
                getIsInstalled: function() { return false; },
                installState: function(callback) {
                    var state = 'not_installed';
                    if (callback) callback(state);
                    return Promise.resolve(state);
                },
                InstallState: { DISABLED: 'disabled', INSTALLED: 'installed', NOT_INSTALLED: 'not_installed' },
                runningState: function() { return 'cannot_run'; },
                RunningState: { CANNOT_RUN: 'cannot_run', READY_TO_RUN: 'ready_to_run', RUNNING: 'running' }
            }),
            configurable: true, enumerable: true
        });
    } catch(e) {}
}

// 3. chrome.webstore — Chrome Web Store 依赖此 API 触发扩展安装。
//    ungoogled-chromium 移除了 webstorePrivate 扩展，导致点击
//    "添加到 Chrome" 按钮无反应。模拟此 API 使按钮可点击，
//    点击后引导用户通过 chrome://extensions 手动安装。
if (window.chrome && !window.chrome.webstore) {
    try {
        Object.defineProperty(window.chrome, 'webstore', {
            get: () => ({
                install: function(url, onSuccess, onFailure) {
                    // Web Store 安装不可用时，自动跳转到 chrome://extensions
                    // 以便用户通过开发者模式手动安装
                    if (typeof chrome !== 'undefined' && chrome.tabs && chrome.tabs.create) {
                        chrome.tabs.create({url: 'chrome://extensions'});
                    } else {
                        window.open('chrome://extensions', '_blank');
                    }
                    if (onFailure) {
                        onFailure('Chrome Web Store 安装功能在此浏览器不可用，请通过 chrome://extensions 手动安装扩展。');
                    }
                },
                onInstallStageStateChanged: null,
                onInstallProgressChanged: null
            }),
            configurable: true, enumerable: true
        });
    } catch(e) {}
}

// 4. Permissions API — 防止 CF 通过 permissions.query 检测自动化
if (navigator.permissions && navigator.permissions.query) {
    try {
        const origPermQuery = navigator.permissions.query.bind(navigator.permissions);
        navigator.permissions.query = function(params) {
            if (params && params.name === 'notifications') {
                return Promise.resolve({state: Notification.permission || 'default'});
            }
            return origPermQuery(params);
        };
    } catch(e) {}
}
`

// turnstileDetectJS 用于检测页面中的 CF Turnstile iframe 位置
const turnstileDetectJS = `(function() {
    var frames = document.querySelectorAll('iframe');
    for (var i = 0; i < frames.length; i++) {
        var src = frames[i].src || '';
        if (src.indexOf('challenges.cloudflare.com') !== -1 || src.indexOf('turnstile') !== -1) {
            var rect = frames[i].getBoundingClientRect();
            if (rect.width > 0 && rect.height > 0) {
                return JSON.stringify({found: true, x: Math.round(rect.left + 28), y: Math.round(rect.top + rect.height / 2), vw: rect.width, vh: rect.height});
            }
        }
    }
    var containers = document.querySelectorAll('[class*="turnstile"], [id*="turnstile"], .cf-turnstile, [data-sitekey]');
    for (var i = 0; i < containers.length; i++) {
        var iframe = containers[i].querySelector('iframe');
        if (iframe) {
            var src = iframe.src || '';
            if (src.indexOf('challenges.cloudflare.com') !== -1 || src.indexOf('turnstile') !== -1 || src.indexOf('cdn-cgi') !== -1) {
                var rect = iframe.getBoundingClientRect();
                if (rect.width > 0 && rect.height > 0) {
                    return JSON.stringify({found: true, x: Math.round(rect.left + 28), y: Math.round(rect.top + rect.height / 2), vw: rect.width, vh: rect.height});
                }
            }
        }
    }
    return JSON.stringify({found: false});
})()`

// injectStealthJS 通过 CDP 向浏览器注入反检测脚本。
func injectStealthJS(debugPort int) error {
	wsURL, err := getBrowserWebSocketURL(debugPort)
	if err != nil {
		return fmt.Errorf("获取浏览器调试地址失败: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket 连接失败: %w", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	msg := cdpMessage{
		Id:     1,
		Method: "Page.addScriptToEvaluateOnNewDocument",
		Params: map[string]any{
			"source": stealthJS,
		},
	}
	if err := conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("CDP 隐身脚本注入命令发送失败: %w", err)
	}

	var cdpResp cdpResponse
	if err := conn.ReadJSON(&cdpResp); err != nil {
		return nil
	}
	if cdpResp.Error != nil {
		return fmt.Errorf("CDP 隐身脚本注入错误: %s", cdpResp.Error.Message)
	}

	return nil
}

// getUserAgentOverride 获取浏览器当前 UA 并生成修正后的 Chrome UA 覆写参数。
// ungoogled-chromium 默认 UA 包含 "Chromium" 而非 "Chrome"，导致 Chrome Web Store
// 显示"切换到 Chrome"横幅。此函数获取浏览器的真实版本号，生成匹配的 Chrome UA。
func getUserAgentOverride(debugPort int) (fixedUA string, metadata map[string]any, err error) {
	// 1. 获取浏览器当前版本信息
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", debugPort))
	if err != nil {
		return "", nil, fmt.Errorf("获取浏览器版本失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var versionInfo struct {
		UserAgent string `json:"User-Agent"`
	}
	if err := json.Unmarshal(body, &versionInfo); err != nil || versionInfo.UserAgent == "" {
		return "", nil, fmt.Errorf("解析浏览器版本 UA 失败")
	}

	// 2. 将 UA 中的 "Chromium" 替换为 "Chrome"
	fixedUA = strings.ReplaceAll(versionInfo.UserAgent, "Chromium", "Chrome")
	// 如果 UA 中含 "HeadlessChrome" 则替换为普通 Chrome
	fixedUA = strings.ReplaceAll(fixedUA, "HeadlessChrome", "Chrome")
	// 即使启动参数里的 UA 已经是 Chrome，也必须继续生成 userAgentMetadata。
	// Chrome Web Store 前端主要看 navigator.userAgentData / UA-CH，
	// 只改 --user-agent 不会同步修正 JS 可见的品牌信息。

	// 3. 从 UA 中提取版本号（格式如 "Chrome/144.0.7559.132"）
	majorVersion := "144"
	fullVersion := "144.0.7559.132"
	versionRegex := regexp.MustCompile(`Chrome/(\d+\.\d+\.\d+\.\d+)`)
	if matches := versionRegex.FindStringSubmatch(fixedUA); len(matches) >= 2 {
		fullVersion = matches[1]
		parts := strings.SplitN(fullVersion, ".", 2)
		if len(parts) > 0 {
			majorVersion = parts[0]
		}
	}

	// 4. 构建 userAgentMetadata（Sec-CH-UA Client Hints）
	// 必须同时包含 brands 与 fullVersionList，否则 navigator.userAgentData
	// 高熵字段仍可能暴露 Chromium/版本不一致。
	brands := []map[string]any{
		{"brand": "Not/A)Brand", "version": "8"},
		{"brand": "Google Chrome", "version": majorVersion},
		{"brand": "Chromium", "version": majorVersion},
	}
	fullVersionList := []map[string]any{
		{"brand": "Not/A)Brand", "version": "8.0.0.0"},
		{"brand": "Google Chrome", "version": fullVersion},
		{"brand": "Chromium", "version": fullVersion},
	}
	metadata = map[string]any{
		"brands":          brands,
		"fullVersionList": fullVersionList,
		"fullVersion":     fullVersion,
		"platform":        "Windows",
		"platformVersion": "10.0.0",
		"architecture":    "x86",
		"bitness":         "64",
		"wow64":           false,
		"model":           "",
		"mobile":          false,
	}

	return fixedUA, metadata, nil
}

// setUserAgentOverrideOnTarget 通过 CDP 向指定页面 target 发送
// Emulation.setUserAgentOverride，在浏览器引擎层面修改该页面的 UA。
// 这比 JS 覆写更可靠，因为它同时影响 navigator.userAgent、
// navigator.userAgentData 和 HTTP 请求头。
func setUserAgentOverrideOnTarget(wsURL string, fixedUA string, metadata map[string]any) error {
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket 连接失败: %w", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	msg := cdpMessage{
		Id:     1,
		Method: "Emulation.setUserAgentOverride",
		Params: map[string]any{
			"userAgent":         fixedUA,
			"platform":          "Win32",
			"userAgentMetadata": metadata,
		},
	}
	if err := conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("UA 覆写命令发送失败: %w", err)
	}

	var cdpResp cdpResponse
	if err := conn.ReadJSON(&cdpResp); err != nil {
		return nil // 超时等非致命错误
	}
	if cdpResp.Error != nil {
		return fmt.Errorf("UA 覆写 CDP 错误: %s", cdpResp.Error.Message)
	}
	return nil
}

// applyUserAgentOverrideToAllPages 只应用 Chrome UA/UA-CH 覆写，不注入 wrapper
// stealth 脚本。Cloak 内核自身负责指纹隐身，但 Chrome Web Store 仍要求
// navigator.userAgent / userAgentData 表现为 Google Chrome，否则会显示
// “切换到 Chrome 即可安装扩展程序和主题背景”并禁用安装入口。
func applyUserAgentOverrideToAllPages(debugPort int) error {
	fixedUA, metadata, err := getUserAgentOverride(debugPort)
	if err != nil {
		return err
	}

	var firstErr error
	targets, err := getCDPPageTargetInfos(debugPort)
	if err != nil {
		firstErr = err
	} else {
		for _, target := range targets {
			if shouldSkipWrapperInjectionURL(target.URL) {
				continue
			}
			if err := setUserAgentOverrideOnTarget(target.WebSocketDebuggerUrl, fixedUA, metadata); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	// 不再设置浏览器级全局 UA 覆写。全局覆写会污染后续新开的扩展/OAuth 页面
	// （如 Moss 的 plugin.moss.site / X 授权页），导致插件登录请求异常。
	// Chrome Web Store 兼容只针对已打开的 Web Store target 单独处理。

	// 老实例常见失败路径：上次关闭时已经停在 Chrome Web Store，重开时
	// Chrome 会先把这个标签页恢复并完成商店前端的身份判定，随后我们再
	// setUserAgentOverride 已经太晚，页面仍保留“切换到 Chrome”的禁用状态。
	// 新实例没有历史商店标签页，所以表现正常。这里只针对已存在的 Web Store
	// page target 重新应用 UA/UA-CH 并 reload，一次性刷新商店判定；普通业务页
	// 不受影响。
	if err := reloadChromeWebStorePageTargets(debugPort, fixedUA, metadata); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func isChromeWebStoreTarget(target cdpTarget) bool {
	return strings.EqualFold(strings.TrimSpace(target.Type), "page") &&
		target.WebSocketDebuggerUrl != "" &&
		isChromeWebStoreURL(target.URL)
}

func reloadChromeWebStorePageTargets(debugPort int, fixedUA string, metadata map[string]any) error {
	targets, err := getCDPPageTargetInfos(debugPort)
	if err != nil {
		return err
	}
	var firstErr error
	for _, target := range targets {
		if !isChromeWebStoreTarget(target) {
			continue
		}
		if err := setUserAgentOverrideOnTarget(target.WebSocketDebuggerUrl, fixedUA, metadata); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := reloadCDPPageTarget(target.WebSocketDebuggerUrl); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func shouldSkipWrapperInjectionURL(rawURL string) bool {
	u := strings.ToLower(strings.TrimSpace(rawURL))
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "chrome-extension://") || strings.HasPrefix(u, "chrome://") || strings.HasPrefix(u, "devtools://") {
		return true
	}
	return strings.Contains(u, "plugin.moss.site") ||
		strings.Contains(u, "devai.moss.site") ||
		strings.Contains(u, "moss.site") ||
		strings.Contains(u, "x.com/") ||
		strings.Contains(u, "twitter.com/")
}

func reloadCDPPageTarget(wsURL string) error {
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	msg := cdpMessage{
		Id:     1,
		Method: "Page.reload",
		Params: map[string]any{"ignoreCache": true},
	}
	if err := conn.WriteJSON(msg); err != nil {
		return err
	}
	var cdpResp cdpResponse
	if err := conn.ReadJSON(&cdpResp); err != nil {
		return nil
	}
	if cdpResp.Error != nil {
		return fmt.Errorf("Web Store 页面 reload CDP 错误: %s", cdpResp.Error.Message)
	}
	return nil
}

// injectStealthToAllPages 对浏览器级 target 注入脚本，设置 UA 覆写，并对所有已打开的 page 也注入。
func injectStealthToAllPages(debugPort int) error {
	return injectStealthToAllPagesWithUA(debugPort, true)
}

// injectStealthToAllPagesWithUA 注入精简 stealth；applyUA=false 时跳过 UA/UA-CH CDP 覆写，
// 用于 CloakBrowser 这类已经在内核层处理身份的浏览器，避免 wrapper 级覆写冲突。
func injectStealthToAllPagesWithUA(debugPort int, applyUA bool) error {
	// 不再做浏览器级全局 stealth 注入。全局注入会影响后续新开的插件/OAuth 页面，
	// Moss 这类扩展登录页会因此出现 Authorization page could not be loaded。
	// 仅对当前已打开且非扩展/非 OAuth 的普通网页 target 做局部处理。

	// 1. 获取 UA 覆写参数（将 Chromium 替换为 Chrome）
	fixedUA := ""
	var uaMetadata map[string]any
	if applyUA {
		var uaErr error
		fixedUA, uaMetadata, uaErr = getUserAgentOverride(debugPort)
		if uaErr != nil {
			logger.New("Stealth").Warn("UA 覆写参数获取失败（非致命）",
				logger.F("error", uaErr.Error()),
			)
		}
	}

	// 2. 对所有已打开的普通 page 注入隐身脚本 + UA 覆写；跳过扩展/OAuth 页面。
	targets, err := getCDPPageTargetInfos(debugPort)
	if err != nil {
		return nil
	}
	for _, target := range targets {
		if shouldSkipWrapperInjectionURL(target.URL) {
			continue
		}
		wsURL := target.WebSocketDebuggerUrl
		_ = injectStealthToTarget(wsURL)
		// 对每个 page target 单独发送 Emulation.setUserAgentOverride（必须发到 page 级别才有效）
		if fixedUA != "" {
			if err := setUserAgentOverrideOnTarget(wsURL, fixedUA, uaMetadata); err != nil {
				logger.New("Stealth").Warn("页面级 UA 覆写失败（非致命）",
					logger.F("error", err.Error()),
				)
			} else {
				logger.New("Stealth").Info("页面级 UA 覆写成功",
					logger.F("fixed_ua", fixedUA),
				)
			}
		}
	}

	// 不再设置浏览器级全局 UA 覆写，避免污染后续插件/OAuth 页面。

	return nil
}

// startTurnstileMonitor 启动 CF Turnstile 自动点击监控。
// 浏览器启动后周期性检测页面中的 Turnstile 验证框，自动点击确认按钮。
func startTurnstileMonitor(debugPort int, profileId string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.New("Turnstile").Error("turnstile monitor goroutine panic recovered",
					logger.F("profile_id", profileId),
					logger.F("error", r),
				)
			}
		}()
		log := logger.New("Turnstile")
		deadline := time.Now().Add(120 * time.Second)
		clicked := false

		for !clicked && time.Now().Before(deadline) {
			time.Sleep(2000 * time.Millisecond)

			targets, err := getCDPPageTargets(debugPort)
			if err != nil {
				continue
			}

			for _, wsURL := range targets {
				ok := tryClickTurnstileOnTarget(wsURL)
				if ok {
					clicked = true
					log.Info("Turnstile 确认按钮自动点击成功",
						logger.F("profile_id", profileId),
					)
					break
				}
			}
		}
	}()
}

// tryClickTurnstileOnTarget 在指定页面 target 上检测并点击 CF Turnstile 确认按钮。
func tryClickTurnstileOnTarget(wsURL string) bool {
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return false
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// Step 1: 检测 Turnstile iframe 位置
	detectMsg := cdpMessage{
		Id:     1,
		Method: "Runtime.evaluate",
		Params: map[string]any{
			"expression":    turnstileDetectJS,
			"returnByValue": true,
		},
	}
	if err := conn.WriteJSON(detectMsg); err != nil {
		return false
	}

	var resp cdpResponse
	if err := conn.ReadJSON(&resp); err != nil {
		return false
	}
	if resp.Error != nil {
		return false
	}

	// 解析结果
	resultObj, ok := resp.Result["result"].(map[string]any)
	if !ok {
		return false
	}
	valueStr, ok := resultObj["value"].(string)
	if !ok {
		return false
	}

	type turnstilePos struct {
		Found bool `json:"found"`
		X     int  `json:"x"`
		Y     int  `json:"y"`
	}
	var pos turnstilePos
	if err := json.Unmarshal([]byte(valueStr), &pos); err != nil || !pos.Found {
		return false
	}

	// Step 2: 等待 iframe 内部渲染完成
	time.Sleep(800 * time.Millisecond)

	// Step 3: 模拟鼠标移动 + 点击
	clickEvents := []cdpMessage{
		{Id: 2, Method: "Input.dispatchMouseEvent", Params: map[string]any{
			"type":   "mouseMoved",
			"x":      float64(pos.X),
			"y":      float64(pos.Y),
			"button": "none",
		}},
		{Id: 3, Method: "Input.dispatchMouseEvent", Params: map[string]any{
			"type":       "mousePressed",
			"x":          float64(pos.X),
			"y":          float64(pos.Y),
			"button":     "left",
			"clickCount": 1,
		}},
		{Id: 4, Method: "Input.dispatchMouseEvent", Params: map[string]any{
			"type":       "mouseReleased",
			"x":          float64(pos.X),
			"y":          float64(pos.Y),
			"button":     "left",
			"clickCount": 1,
		}},
	}

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	for _, clickMsg := range clickEvents {
		if err := conn.WriteJSON(clickMsg); err != nil {
			break
		}
		// 读取响应但不处理
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var clickResp cdpResponse
		conn.ReadJSON(&clickResp)
	}

	return true
}

// --- 辅助函数 ---

func getBrowserWebSocketURL(debugPort int) (string, error) {
	return getBrowserWebSocketURLWithRetry(debugPort, 3)
}

func getBrowserWebSocketURLWithRetry(debugPort int, maxRetries int) (string, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		wsURL, err := cdpBrowserWebSocketURL(debugPort)
		if err == nil {
			return wsURL, nil
		}
		lastErr = err
		time.Sleep(200 * time.Millisecond)
	}
	return "", lastErr
}

func injectStealthToTarget(wsURL string) error {
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	msg := cdpMessage{
		Id:     1,
		Method: "Page.addScriptToEvaluateOnNewDocument",
		Params: map[string]any{
			"source": stealthJS,
		},
	}
	if err := conn.WriteJSON(msg); err != nil {
		return err
	}

	var cdpResp cdpResponse
	if err := conn.ReadJSON(&cdpResp); err != nil {
		return nil
	}
	if cdpResp.Error != nil {
		return fmt.Errorf("CDP page 注入错误: %s", cdpResp.Error.Message)
	}
	return nil
}

// ensureStealthHeaderExtension 创建或更新 MV3 declarativeNetRequest 扩展，
// 用于在网络层面覆写 Sec-CH-UA 等 Client Hints 请求头。
// ungoogled-chromium 默认发送 "Chromium" 品牌，Chrome Web Store 等网站据此判定非 Chrome 浏览器。
// CDP Emulation.setUserAgentOverride 只影响 JS 层 API，不能可靠地修改 HTTP 请求头。
// 此扩展通过 declarativeNetRequest 在请求发出前直接替换 Sec-CH-UA 等请求头，
// 从网络层面彻底解决 Chrome Web Store "切换到 Chrome" 横幅问题。
// 返回扩展目录的绝对路径，用于 --load-extension 启动参数。
func ensureStealthHeaderExtension(appRoot string, chromeVersion string) string {
	log := logger.New("Stealth")
	extDir := filepath.Join(appRoot, "data", "stealth_header_ext")

	// 从 chromeVersion 提取主版本号和完整版本号
	majorVersion := "144"
	fullVersion := "144.0.7559.132"
	// 匹配 x.x.x.x 格式的版本号
	versionRegex := regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)\.(\d+)`)
	if matches := versionRegex.FindStringSubmatch(chromeVersion); len(matches) >= 2 {
		majorVersion = matches[1]
		fullVersion = chromeVersion
	}

	// 构造 Chrome UA 版本字符串
	// 格式: "Chromium";v="147", "Google Chrome";v="147", "Not/A)Brand";v="8"
	secChUA := fmt.Sprintf(`"Chromium";v="%s", "Google Chrome";v="%s", "Not/A)Brand";v="8"`, majorVersion, majorVersion)
	secChUAFullVersionList := fmt.Sprintf(`"Chromium";v="%s", "Google Chrome";v="%s", "Not/A)Brand";v="8.0.0.0"`, fullVersion, fullVersion)

	// 检查是否需要重新生成（版本号变化时重新写入）
	manifestPath := filepath.Join(extDir, "manifest.json")
	rulesPath := filepath.Join(extDir, "rules.json")
	needRebuild := false

	existingRules, err := os.ReadFile(rulesPath)
	if err != nil {
		needRebuild = true
	} else {
		// 检查版本号是否匹配
		if !strings.Contains(string(existingRules), majorVersion) {
			needRebuild = true
		}
	}

	if !needRebuild {
		// 检查 manifest 是否存在
		if _, err := os.Stat(manifestPath); err != nil {
			needRebuild = true
		}
	}

	// 构造 Chrome UA 字符串
	chromeUA := fmt.Sprintf(
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
		fullVersion,
	)

	if !needRebuild {
		log.Info("隐身请求头扩展无需更新",
			logger.F("version", fullVersion),
		)
		return extDir
	}

	// 创建扩展目录
	if err := os.MkdirAll(extDir, 0755); err != nil {
		log.Warn("创建隐身请求头扩展目录失败",
			logger.F("path", extDir),
			logger.F("error", err.Error()),
		)
		return extDir
	}

	// 写入 manifest.json（MV3，使用 declarativeNetRequest）
	manifest := map[string]any{
		"manifest_version": 3,
		"name":             "BoostBrowser Header Fix",
		"version":          "1.0",
		"permissions":      []string{"declarativeNetRequest"},
		"host_permissions": []string{"<all_urls>"},
		"declarative_net_request": map[string]any{
			"rule_resources": []map[string]any{
				{
					"id":      "ruleset_headers",
					"enabled": true,
					"path":    "rules.json",
				},
			},
		},
	}
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(manifestPath, manifestJSON, 0644); err != nil {
		log.Warn("写入 manifest.json 失败",
			logger.F("path", manifestPath),
			logger.F("error", err.Error()),
		)
		return extDir
	}

	// 写入 rules.json（declarativeNetRequest 规则）
	// 操作: set — 覆盖现有值或追加不存在的头
	rules := []map[string]any{
		{
			"id":       1,
			"priority": 1,
			"condition": map[string]any{
				"urlFilter": "*",
				"resourceTypes": []string{
					"main_frame", "sub_frame", "stylesheet", "script",
					"image", "xmlhttprequest", "other", "font",
					"media", "websocket", "manifest", "worker",
					"shared_worker", "service_worker",
				},
			},
			"action": map[string]any{
				"type": "modifyHeaders",
				"requestHeaders": []map[string]any{
					{"header": "sec-ch-ua", "operation": "set", "value": secChUA},
					{"header": "sec-ch-ua-mobile", "operation": "set", "value": "?0"},
					{"header": "sec-ch-ua-platform", "operation": "set", "value": `"Windows"`},
					{"header": "sec-ch-ua-full-version-list", "operation": "set", "value": secChUAFullVersionList},
					{"header": "sec-ch-ua-arch", "operation": "set", "value": `"x86"`},
					{"header": "sec-ch-ua-bitness", "operation": "set", "value": `"64"`},
					{"header": "sec-ch-ua-wow64", "operation": "set", "value": "?0"},
					{"header": "sec-ch-ua-full-version", "operation": "set", "value": `"` + fullVersion + `"`},
					{"header": "user-agent", "operation": "set", "value": chromeUA},
				},
			},
		},
	}
	rulesJSON, _ := json.MarshalIndent(rules, "", "  ")
	if err := os.WriteFile(rulesPath, rulesJSON, 0644); err != nil {
		log.Warn("写入 rules.json 失败",
			logger.F("path", rulesPath),
			logger.F("error", err.Error()),
		)
		return extDir
	}

	log.Info("隐身请求头扩展已生成",
		logger.F("version", fullVersion),
		logger.F("ext_dir", extDir),
	)

	return extDir
}
