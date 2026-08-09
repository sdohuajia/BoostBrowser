package backend

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"boost-browser/backend/internal/logger"

	"github.com/gorilla/websocket"
)

// seedDefaultSearchEngineViaCDP 通过 CDP 模拟用户在 chrome://settings/searchEngines
// UI 上的「添加 → 设为默认」操作，把 Google 注册为默认搜索引擎。
//
// 背景：cloak Chromium（基于 ungoogled-chromium）启动时会扫描 keywords 表 +
// HKCU policy + 命令行 --search-provider-* 里的 google.com 字面量并清掉/拒绝，
// 但 UI 路径（chrome://settings 内调用 chrome.searchEnginesPrivate）不在扫描
// 范围内 —— 这就是用户手动「添加搜索引擎」能成功的根因。
//
// 我们用 CDP 在 chrome://settings/searchEngines 页面里 evaluate 一段 JS 调用
// 同样的私有 API，等价于用户手动操作。
//
// 仅首次运行；成功后写 marker 文件 .boost_search_seeded 避免重复。
// 现在默认开启 CDP seed。settings tab 是后台临时页，成功后立即关闭；
// 相比静态写 Web Data / Preferences，这是 cloak 内核下唯一稳定不会回退成
// "No Search" 的路径。
var searchEngineSeedDisabled = false

func seedDefaultSearchEngineViaCDPWithRetry(userDataDir string, debugPort int, maxAttempts int, delay time.Duration) {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	if delay <= 0 {
		delay = 1500 * time.Millisecond
	}
	log := logger.New("SearchEngineCDP")
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if seedDefaultSearchEngineViaCDP(userDataDir, debugPort) {
			return
		}
		if attempt >= maxAttempts {
			log.Info("多次尝试后仍未完成默认搜索引擎 seed，将在下次启动继续重试",
				logger.F("user_data_dir", userDataDir),
				logger.F("debug_port", debugPort),
				logger.F("attempts", maxAttempts),
			)
			return
		}
		time.Sleep(delay)
	}
}
func seedDefaultSearchEngineViaCDP(userDataDir string, debugPort int) bool {
	if searchEngineSeedDisabled {
		return false
	}
	if strings.TrimSpace(userDataDir) == "" || debugPort <= 0 {
		return false
	}
	log := logger.New("SearchEngineCDP")

	markerPath := filepath.Join(userDataDir, ".boost_search_seeded")
	if _, err := os.Stat(markerPath); err == nil {
		log.Debug("已 seed 过，跳过", logger.F("marker", markerPath))
		return true
	}

	// 1. 创建一个新 tab 打开 chrome://settings/searchEngines
	// 注意：Cloak/Chromium 这条路径下 Target.createTarget 返回的 targetId
	// 有时不是 /json/list 里 page target 的 id。此前我们按 id 精确匹配，
	// 结果 settings 页其实已经打开，但一直误判“未就绪”。
	existingTargetIDs := map[string]struct{}{}
	if existingTargets, err := listCDPTargets(debugPort); err == nil {
		for _, t := range existingTargets {
			existingTargetIDs[t.ID] = struct{}{}
		}
	}
	createResult, err := cdpCall(debugPort, "Target.createTarget", map[string]any{
		"url":        "chrome://settings/searchEngines",
		"background": true,
		"forTab":     true,
	})
	if err != nil {
		log.Info("创建 settings tab 失败，稍后重试", logger.F("error", err.Error()))
		return false
	}
	createdTargetID, _ := createResult["targetId"].(string)
	if createdTargetID == "" {
		log.Info("Target.createTarget 未返回 targetId，稍后重试")
		return false
	}

	actualTargetID := createdTargetID
	// 离开函数前关闭 tab，无论成功与否
	defer func() {
		_, _ = cdpCall(debugPort, "Target.closeTarget", map[string]any{"targetId": actualTargetID})
	}()

	// 2. 等 settings 页面真的可用（最多 ~6s）；通过查 /json/list 找到真正 page target 的 wsURL
	wsURL, resolvedTargetID := waitForSettingsTabWS(debugPort, createdTargetID, existingTargetIDs, 6*time.Second)
	if resolvedTargetID != "" {
		actualTargetID = resolvedTargetID
	}
	if wsURL == "" {
		log.Info("settings tab 未就绪，稍后重试", logger.F("created_target_id", createdTargetID))
		return false
	}

	// 3. 连 page WS，evaluate 一段 Promise JS 走 settings 页面内部 browserProxy
	// 路径（等价于用户真正点开“添加搜索引擎”对话框并点击添加/设为默认）。
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		log.Info("settings tab WebSocket 连接失败，稍后重试", logger.F("error", err.Error()))
		return false
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(20 * time.Second))

	// 等 settings-search-engines-page 和它的 browserProxy_ 真正就绪。
	// 在 Cloak/ungoogled 这条链路下，window.chrome 上通常根本没有
	// searchEnginesPrivate；但页面内部 Polymer 组件仍可通过 browserProxy_
	// 调用 chrome.send / sendWithPromise 成功完成同一条 UI 路径。
	if !waitSearchEnginesPageReady(conn, log) {
		log.Info("settings search engines page/browserProxy 一直不可用，稍后重试")
		return false
	}
	log.Info("settings search engines page 已就绪，开始 seed")

	const seedJS = `
(async () => {
  const getPage = () => document.querySelector('settings-ui')
      ?.shadowRoot?.querySelector('settings-main')
      ?.shadowRoot?.querySelector('settings-search-page-index')
      ?.shadowRoot?.querySelector('settings-search-engines-page');

  const page = getPage();
  const browserProxy = page && (page.browserProxy_ || page.browserProxy);
  if (!page || !browserProxy) {
    return { ok: false, reason: 'settings search page unavailable' };
  }

  const getList = () => browserProxy.getSearchEnginesList();
  const flat = (list) => [
    ...(list.defaults || []),
    ...(list.others || []),
    ...(list.actives || []),
    ...(list.extensions || []),
  ];

  const isOurGoogle = (e) => {
    if (!e || !e.name) return false;
    const url = (e.url || '').toLowerCase();
    return e.name === 'Google' &&
           (url.includes('google.com/search') || url.includes('9oo91e.qjz9zk/search'));
  };

  let list = await getList();
  let g = flat(list).find(isOurGoogle);

  if (!g) {
    // UI 路径添加 —— 等价于用户手动点「添加搜索引擎」对话框后点击“添加”。
    page.onAddSearchEngineClick_({preventDefault() {}});
    await new Promise(r => setTimeout(r, 50));
    const dialog = page.shadowRoot && page.shadowRoot.querySelector('settings-search-engine-edit-dialog');
    if (!dialog) return { ok: false, reason: 'edit dialog unavailable' };
    dialog.searchEngine_ = 'Google';
    dialog.keyword_ = 'google.com';
    dialog.queryUrl_ = 'https://www.google.com/search?q=%s';
    dialog.suggestionsUrl_ = 'https://www.google.com/complete/search?client=chrome&q=%s';
    dialog.onActionButtonClick_();

    // Chrome 内部异步写 SQLite + 通知前端，需要一点时间。
    for (let i = 0; i < 20; i++) {
      await new Promise(r => setTimeout(r, 200));
      list = await getList();
      g = flat(list).find(isOurGoogle);
      if (g) break;
    }
  }

  if (!g || typeof g.modelIndex !== 'number') {
    return { ok: false, reason: 'add failed', google: g, listAfterAdd: list };
  }

  // UI 返回的 SearchEngine object 在这条 Cloak 路径下没有 guid 字段，
  // 但 setDefaultSearchEngine 实际只需要 modelIndex。此前把 !g.guid 当成失败条件，
  // 会导致“Google 已成功添加到列表里，但函数提前返回 false”，外层持续重试，
  // 从而每次启动都重复弹 searchEngines 设置页且永远不写 marker。
  const already = (list.defaults || []).find(isOurGoogle);
  if (!already) {
    browserProxy.setDefaultSearchEngine(g.modelIndex, 0, false);
    await new Promise(r => setTimeout(r, 300));
  }

  const finalList = await getList();
  const def = (finalList.defaults || []).find(isOurGoogle);
  return { ok: !!def, keyword: g.keyword, modelIndex: g.modelIndex, defaultName: def && def.name };
})()
`

	const evalID = 9001
	msg := cdpMessage{
		Id:     evalID,
		Method: "Runtime.evaluate",
		Params: map[string]any{
			"expression":    seedJS,
			"awaitPromise":  true,
			"returnByValue": true,
		},
	}
	if err := conn.WriteJSON(msg); err != nil {
		log.Info("Runtime.evaluate 写入失败", logger.F("error", err.Error()))
		return false
	}

	resp, ok := readCDPResponseByID(conn, evalID, 18*time.Second)
	if !ok {
		log.Info("Runtime.evaluate 读响应超时/失败")
		return false
	}
	if resp.Error != nil {
		log.Info("Runtime.evaluate 返回错误", logger.F("error", resp.Error.Message))
		return false
	}

	// 解析 result.value
	result, _ := resp.Result["result"].(map[string]any)
	value, _ := result["value"].(map[string]any)
	if value == nil {
		log.Info("seed JS 未返回对象", logger.F("raw_result", result))
		return false
	}
	if okFlag, _ := value["ok"].(bool); !okFlag {
		reason, _ := value["reason"].(string)
		log.Info("seed 未成功，下次启动重试", logger.F("reason", reason), logger.F("value", value))
		return false
	}

	// 写 marker 表示已 seed，下次跳过
	if err := os.WriteFile(markerPath, []byte("1\n"), 0644); err != nil {
		log.Info("写 marker 失败（不影响功能）", logger.F("error", err.Error()))
	}
	log.Info("已通过 CDP 把 Google 设为默认搜索引擎",
		logger.F("user_data_dir", userDataDir),
		logger.F("keyword", value["keyword"]),
		logger.F("model_index", value["modelIndex"]),
		logger.F("default_name", value["defaultName"]),
	)
	return true
}

// readCDPResponseByID 反复 ReadJSON，丢掉所有事件帧（无 "id" 字段或 id 不匹配的），
// 直到拿到指定 id 的响应或超时。CDP 同一条 WebSocket 会推送 Page/Runtime 事件，
// 不过滤会导致 ReadJSON 读到事件帧后立刻返回，外层误判失败。
func readCDPResponseByID(conn *websocket.Conn, wantID int, timeout time.Duration) (*cdpResponse, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline)
		var resp cdpResponse
		if err := conn.ReadJSON(&resp); err != nil {
			return nil, false
		}
		if resp.Id == wantID {
			return &resp, true
		}
		// 其他帧（事件 / 其他 id）丢弃，继续读
	}
	return nil, false
}

// waitForSettingsTabWS 轮询 /json/list，优先找到“本次新建”的 settings page target，
// 返回它的 webSocketDebuggerUrl 和实际 target id。
func waitForSettingsTabWS(debugPort int, createdTargetID string, existingTargetIDs map[string]struct{}, timeout time.Duration) (string, string) {
	deadline := time.Now().Add(timeout)
	var fallbackURL string
	var fallbackID string
	for time.Now().Before(deadline) {
		targets, err := listCDPTargets(debugPort)
		if err == nil {
			for _, t := range targets {
				if !strings.HasPrefix(t.URL, "chrome://settings") {
					continue
				}
				if t.WebSocketDebuggerUrl == "" {
					continue
				}
				if t.ID == createdTargetID {
					return t.WebSocketDebuggerUrl, t.ID
				}
				if _, existed := existingTargetIDs[t.ID]; !existed {
					return t.WebSocketDebuggerUrl, t.ID
				}
				if fallbackURL == "" {
					fallbackURL = t.WebSocketDebuggerUrl
					fallbackID = t.ID
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fallbackURL, fallbackID
}

// waitSearchEnginesPageReady 通过 Runtime.evaluate 轮询 settings-search-engines-page
// 自身和 browserProxy_ 是否已经可用。这里不依赖 window.chrome.searchEnginesPrivate，
// 因为 Cloak/ungoogled 下该对象经常根本不暴露到全局，但页面内部 proxy 仍可工作。
func waitSearchEnginesPageReady(conn *websocket.Conn, log *logger.Logger) bool {
	deadline := time.Now().Add(10 * time.Second)
	id := 100
	for time.Now().Before(deadline) {
		id++
		msg := cdpMessage{
			Id:     id,
			Method: "Runtime.evaluate",
			Params: map[string]any{
				"expression": `(() => {
  const page = document.querySelector('settings-ui')
      ?.shadowRoot?.querySelector('settings-main')
      ?.shadowRoot?.querySelector('settings-search-page-index')
      ?.shadowRoot?.querySelector('settings-search-engines-page');
  const browserProxy = page && (page.browserProxy_ || page.browserProxy);
  return !!(page && browserProxy);
})()`,
				"returnByValue": true,
			},
		}
		if err := conn.WriteJSON(msg); err != nil {
			log.Info("waitReady 写入失败", logger.F("error", err.Error()))
			return false
		}
		resp, ok := readCDPResponseByID(conn, id, 2*time.Second)
		if !ok {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		if resp.Error == nil {
			result, _ := resp.Result["result"].(map[string]any)
			if val, ok := result["value"].(bool); ok && val {
				return true
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

