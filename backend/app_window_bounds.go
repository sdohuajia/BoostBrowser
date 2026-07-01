package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"boost-browser/backend/internal/logger"

	"github.com/gorilla/websocket"
)

// 每实例的窗口 bounds 追踪器：
// 启动后周期性通过 CDP 抓窗口位置/尺寸，关闭时把最后一次值写回 profile + 持久化。
//
// 设计要点：
// - 不能等关窗那一刻才查 — 进程已死 CDP 连不上。所以后台轮询。
// - 每 2 秒一次，CDP 连接复用一个 ws，断了就跳过那一轮（实例可能正在切换页面）。
// - 用全局 map 按 profileId 索引；启动新实例 / 重启时先 stop 老的。
// - markProfileStoppedLocked 触发时调 finalize：取最新值、停轮询、把 profile 字段写库。

const (
	windowBoundsPollInterval = 2 * time.Second
	// 太小的窗口大概率是最小化或异常状态，不保存。
	windowBoundsMinWidth  = 320
	windowBoundsMinHeight = 240
)

type windowBoundsSnapshot struct {
	x          int
	y          int
	width      int
	height     int
	state      string // "normal" / "minimized" / "maximized" / "fullscreen"
	hasValid   bool   // 是否曾经成功取过一次合理值
	lastUpdate time.Time
}

type windowBoundsTracker struct {
	profileId string
	debugPort int
	stopCh    chan struct{}
	stopOnce  sync.Once

	mu       sync.Mutex
	snapshot windowBoundsSnapshot
}

func (t *windowBoundsTracker) stop() {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() { close(t.stopCh) })
}

var (
	windowBoundsTrackersMu sync.Mutex
	windowBoundsTrackers   = map[string]*windowBoundsTracker{}
)

// startWindowBoundsTracker 启动一个新的轮询追踪器。
// 若同 profileId 已有，先停掉再起新的（应对重启场景）。
func (a *App) startWindowBoundsTracker(profileId string, debugPort int) {
	if profileId == "" || debugPort <= 0 {
		return
	}

	windowBoundsTrackersMu.Lock()
	if old, ok := windowBoundsTrackers[profileId]; ok {
		old.stop()
		delete(windowBoundsTrackers, profileId)
	}
	t := &windowBoundsTracker{
		profileId: profileId,
		debugPort: debugPort,
		stopCh:    make(chan struct{}),
	}
	windowBoundsTrackers[profileId] = t
	windowBoundsTrackersMu.Unlock()

	go t.run(a)
}

// stopWindowBoundsTrackerAndFinalize 停止追踪器并把最后一次合理 bounds 写入 profile + 持久化。
// 调用方必须已持有 a.browserMgr.Mutex（在 markProfileStoppedLocked 内）。
func (a *App) stopWindowBoundsTrackerAndFinalize(profileId string, profile *BrowserProfile) {
	if profileId == "" {
		return
	}

	windowBoundsTrackersMu.Lock()
	t, ok := windowBoundsTrackers[profileId]
	if ok {
		delete(windowBoundsTrackers, profileId)
	}
	windowBoundsTrackersMu.Unlock()

	if !ok || t == nil {
		return
	}
	t.stop()

	if profile == nil {
		return
	}

	// 关窗前再尝试同步抓一次最新值（这一刻 Chrome 可能已经在收尾，但通常还能响应）。
	if snap, ok := fetchWindowBoundsOnce(t.debugPort); ok && isReasonableBounds(snap) {
		t.mu.Lock()
		t.snapshot = snap
		t.snapshot.hasValid = true
		t.mu.Unlock()
	}

	t.mu.Lock()
	snap := t.snapshot
	t.mu.Unlock()

	if !snap.hasValid {
		return
	}
	// 最小化/全屏不要覆写已有的 bounds，避免下次启动直接全屏或最小化。
	if snap.state != "" && snap.state != "normal" {
		return
	}

	profile.LastWindowX = snap.x
	profile.LastWindowY = snap.y
	profile.LastWindowWidth = snap.width
	profile.LastWindowHeight = snap.height

	// 异步持久化 — 走 SaveProfiles 而不是直接 DAO，兼顾未注入 DAO 的降级路径。
	saveProfile := profile
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.New("Browser").Error("窗口 bounds 异步持久化 panic 已拦截（非致命）",
					logger.F("profile_id", saveProfile.ProfileId),
					logger.F("panic", fmt.Sprintf("%v", r)),
				)
			}
		}()
		if a.browserMgr == nil {
			return
		}
		// SaveProfiles 内部会 lock，必须不持锁调用。
		if err := a.browserMgr.SaveProfiles(); err != nil {
			logger.New("Browser").Warn("窗口 bounds 持久化失败（非致命）",
				logger.F("profile_id", saveProfile.ProfileId),
				logger.F("error", err.Error()),
			)
		}
	}()
}

func (t *windowBoundsTracker) run(a *App) {
	defer func() {
		if r := recover(); r != nil {
			logger.New("Browser").Error("窗口 bounds 追踪 goroutine panic 已拦截（非致命）",
				logger.F("profile_id", t.profileId),
				logger.F("debug_port", t.debugPort),
				logger.F("panic", fmt.Sprintf("%v", r)),
			)
		}
	}()
	// 启动后稍等一下，等 Chrome 把窗口创建完毕。
	timer := time.NewTimer(800 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-t.stopCh:
		return
	case <-timer.C:
	}

	ticker := time.NewTicker(windowBoundsPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			snap, ok := fetchWindowBoundsOnce(t.debugPort)
			if !ok {
				continue
			}
			if !isReasonableBounds(snap) {
				continue
			}
			t.mu.Lock()
			t.snapshot = snap
			t.snapshot.hasValid = true
			t.mu.Unlock()
		}
	}
}

func isReasonableBounds(s windowBoundsSnapshot) bool {
	if s.width < windowBoundsMinWidth || s.height < windowBoundsMinHeight {
		return false
	}
	// 只在 normal 状态下记尺寸 — 最小化/最大化/全屏不当作目标尺寸（最大化会下次启动直接最大化）。
	if s.state != "" && s.state != "normal" {
		return false
	}
	return true
}

// fetchWindowBoundsOnce 通过 CDP 一次性获取窗口 bounds。
// 走 Browser.getWindowForTarget — 它直接返回浏览器主窗口的 bounds，不用挑 tab。
func fetchWindowBoundsOnce(debugPort int) (windowBoundsSnapshot, bool) {
	var snap windowBoundsSnapshot

	// 1. /json/version 拿浏览器级 ws URL。
	resp, err := httpGetWithTimeout(fmt.Sprintf("http://127.0.0.1:%d/json/version", debugPort), 1*time.Second)
	if err != nil {
		return snap, false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var version struct {
		WebSocketDebuggerUrl string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &version); err != nil {
		return snap, false
	}
	wsURL := strings.TrimSpace(version.WebSocketDebuggerUrl)
	if wsURL == "" {
		return snap, false
	}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 1 * time.Second
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return snap, false
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_ = conn.SetWriteDeadline(time.Now().Add(1 * time.Second))

	// Browser.getWindowForTarget 不传 targetId 时返回当前会话所属的窗口，
	// 但浏览器级 ws 没有页面 target，会报错。改成先 Target.getTargets 取一个 page，
	// 再 attachToTarget 拿 sessionId，再 getWindowForTarget 传该 targetId。
	// 简化：直接列 targets，找一个 type=page 的 targetId 传进去。
	if err := conn.WriteJSON(map[string]any{
		"id":     1,
		"method": "Target.getTargets",
	}); err != nil {
		return snap, false
	}

	var targetResp struct {
		Id     int `json:"id"`
		Result struct {
			TargetInfos []struct {
				TargetId string `json:"targetId"`
				Type     string `json:"type"`
				Attached bool   `json:"attached"`
			} `json:"targetInfos"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := readCDPMessageById(conn, 1, &targetResp); err != nil {
		return snap, false
	}
	if targetResp.Error != nil {
		return snap, false
	}

	pageTargetId := ""
	for _, info := range targetResp.Result.TargetInfos {
		if info.Type == "page" {
			pageTargetId = info.TargetId
			break
		}
	}
	if pageTargetId == "" {
		return snap, false
	}

	if err := conn.WriteJSON(map[string]any{
		"id":     2,
		"method": "Browser.getWindowForTarget",
		"params": map[string]any{"targetId": pageTargetId},
	}); err != nil {
		return snap, false
	}

	var winResp struct {
		Id     int `json:"id"`
		Result struct {
			WindowId int `json:"windowId"`
			Bounds   struct {
				Left        int    `json:"left"`
				Top         int    `json:"top"`
				Width       int    `json:"width"`
				Height      int    `json:"height"`
				WindowState string `json:"windowState"`
			} `json:"bounds"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := readCDPMessageById(conn, 2, &winResp); err != nil {
		return snap, false
	}
	if winResp.Error != nil {
		return snap, false
	}

	state := strings.TrimSpace(winResp.Result.Bounds.WindowState)
	if state == "" {
		state = "normal"
	}
	snap = windowBoundsSnapshot{
		x:          winResp.Result.Bounds.Left,
		y:          winResp.Result.Bounds.Top,
		width:      winResp.Result.Bounds.Width,
		height:     winResp.Result.Bounds.Height,
		state:      state,
		hasValid:   true,
		lastUpdate: time.Now(),
	}
	return snap, true
}

// readCDPMessageById 读 ws 消息直到拿到指定 id 的响应，跳过事件通知。
func readCDPMessageById(conn *websocket.Conn, wantId int, dest any) error {
	for i := 0; i < 16; i++ {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var probe struct {
			Id int `json:"id"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			continue
		}
		if probe.Id != wantId {
			continue
		}
		return json.Unmarshal(raw, dest)
	}
	return fmt.Errorf("CDP 响应未在限度内到达（id=%d）", wantId)
}

func httpGetWithTimeout(url string, timeout time.Duration) (*http.Response, error) {
	client := &http.Client{Timeout: timeout}
	return client.Get(url)
}
