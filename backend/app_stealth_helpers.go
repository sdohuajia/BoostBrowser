package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// cdpBrowserWebSocketURL 获取浏览器级 WebSocket 调试地址
func cdpBrowserWebSocketURL(debugPort int) (string, error) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", debugPort))
	if err != nil {
		return "", fmt.Errorf("CDP /json/version 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var version cdpBrowserVersion
	if err := json.Unmarshal(body, &version); err != nil {
		return "", fmt.Errorf("CDP browser target 解析失败: %w", err)
	}
	wsURL := version.WebSocketDebuggerUrl
	if wsURL == "" {
		return "", fmt.Errorf("浏览器级 WebSocket 调试地址为空")
	}
	return wsURL, nil
}

// getCDPPageTargetInfos 获取所有 page 类型的 CDP target 信息。
func getCDPPageTargetInfos(debugPort int) ([]cdpTarget, error) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json", debugPort))
	if err != nil {
		return nil, fmt.Errorf("CDP /json 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var targets []cdpTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, fmt.Errorf("CDP targets 解析失败: %w", err)
	}

	var pages []cdpTarget
	for _, t := range targets {
		if t.Type == "page" && t.WebSocketDebuggerUrl != "" {
			pages = append(pages, t)
		}
	}
	return pages, nil
}

// getCDPPageTargets 获取所有 page 类型的 WebSocket 调试地址
func getCDPPageTargets(debugPort int) ([]string, error) {
	pages, err := getCDPPageTargetInfos(debugPort)
	if err != nil {
		return nil, err
	}
	var wsURLs []string
	for _, t := range pages {
		wsURLs = append(wsURLs, t.WebSocketDebuggerUrl)
	}
	return wsURLs, nil
}

// getVisibleCDPPageTarget avoids unstable /json ordering by choosing the tab
// that Chrome reports as visible for this browser instance.
func getVisibleCDPPageTarget(debugPort int) (cdpTarget, error) {
	pages, err := getCDPPageTargetInfos(debugPort)
	if err != nil {
		return cdpTarget{}, err
	}
	if len(pages) == 0 {
		return cdpTarget{}, fmt.Errorf("no available page debugging target")
	}

	fallback := pages[0]
	for _, page := range pages {
		if page.URL != "" && page.URL != "about:blank" {
			fallback = page
		}
		result, callErr := cdpCallWebSocket(page.WebSocketDebuggerUrl, "Runtime.evaluate", map[string]any{
			"expression":    "document.visibilityState === 'visible'",
			"returnByValue": true,
		})
		if callErr != nil {
			continue
		}
		if visible, ok := result["value"].(bool); ok && visible {
			return page, nil
		}
	}
	return fallback, nil
}
