//go:build windows

package backend

import (
	"fmt"
	"sync"
	"unsafe"

	"boost-browser/backend/internal/logger"

	"golang.org/x/sys/windows"
)

// ============================================================================
// 窗口同步 API（供前端调用）
// ============================================================================

// syncState 全局同步状态
var syncState struct {
	mu          sync.Mutex
	syncer      *InputSyncer
	masterHwnd  windows.HWND
	followerIds []string
	masterId    string
	active      bool
}

// SyncProfileInfo 同步页面的实例信息
type SyncProfileInfo struct {
	ProfileId   string `json:"profileId"`
	ProfileName string `json:"profileName"`
	Pid         int    `json:"pid"`
	DebugPort   int    `json:"debugPort"`
	Hwnd        int64  `json:"hwnd"`
	Running     bool   `json:"running"`
	Status      string `json:"status"` // "running" | "stopped"
}

// GetSyncProfiles 获取所有可用于同步的实例列表
func (a *App) GetSyncProfiles() []SyncProfileInfo {
	a.reconcileBrowserRuntimeStateOnce()
	// NOTE: 不要在这里加 browserMgr.Mutex 锁！List() 内部会自行加锁，
	// 如果外层再锁一次会导致死锁（Go sync.Mutex 不可重入）。
	profiles := a.browserMgr.List()
	result := make([]SyncProfileInfo, 0, len(profiles))

	for _, p := range profiles {
		// 只返回运行中的实例，未启动的不展示
		if !p.Running || p.Pid <= 0 {
			continue
		}

		info := SyncProfileInfo{
			ProfileId:   p.ProfileId,
			ProfileName: p.ProfileName,
			Pid:         p.Pid,
			DebugPort:   p.DebugPort,
			Running:     p.Running,
		}

		hwnd, err := findProcessWindow(p.Pid)
		if err == nil {
			info.Hwnd = int64(hwnd)
			info.Status = "running"
		} else {
			info.Status = "no_window"
		}

		result = append(result, info)
	}

	return result
}

// StartInputSync 启动输入同步
// masterProfileId: 主控实例 ID
// followerProfileIds: 跟随实例 ID 列表
func (a *App) StartInputSync(masterProfileId string, followerProfileIds []string) error {
	log := logger.New("SyncAPI")
	a.reconcileBrowserRuntimeStateOnce()

	a.browserMgr.Mutex.Lock()
	defer a.browserMgr.Mutex.Unlock()

	// 查找主控实例
	masterProfile, ok := a.browserMgr.Profiles[masterProfileId]
	if !ok {
		return fmt.Errorf("未找到主控实例：%s", masterProfileId)
	}
	if !masterProfile.Running || masterProfile.Pid <= 0 {
		return fmt.Errorf("主控实例未在运行：%s", masterProfileId)
	}

	masterHwnd, err := findProcessWindow(masterProfile.Pid)
	if err != nil {
		return fmt.Errorf("未找到主控实例窗口：%v", err)
	}

	// 收集跟随窗口
	var followerHwnds []windows.HWND
	var followerDebugPorts []int
	validFollowerIds := make([]string, 0)

	for _, fid := range followerProfileIds {
		if fid == masterProfileId {
			continue // 主控不能同时是跟随
		}
		fp, ok := a.browserMgr.Profiles[fid]
		if !ok || !fp.Running || fp.Pid <= 0 {
			continue
		}
		fhwnd, err := findProcessWindow(fp.Pid)
		if err != nil {
			continue
		}
		followerHwnds = append(followerHwnds, fhwnd)
		followerDebugPorts = append(followerDebugPorts, fp.DebugPort)
		validFollowerIds = append(validFollowerIds, fid)
	}

	if len(followerHwnds) == 0 {
		return fmt.Errorf("没有可用的跟随实例")
	}

	// 如果已有同步器在运行，先停止
	if syncState.syncer != nil && syncState.syncer.IsActive() {
		syncState.syncer.Stop()
	}

	// 创建并启动同步器（带 CDP URL 同步）
	syncer := NewInputSyncer()
	masterDebugPort := masterProfile.DebugPort
	if err := syncer.StartWithURLSync(masterHwnd, followerHwnds, masterProfile.Pid, masterDebugPort, followerDebugPorts); err != nil {
		return fmt.Errorf("启动同步失败：%v", err)
	}

	syncState.mu.Lock()
	syncState.syncer = syncer
	syncState.masterHwnd = masterHwnd
	syncState.masterId = masterProfileId
	syncState.followerIds = validFollowerIds
	syncState.active = true
	syncState.mu.Unlock()

	log.Info("输入同步已启动",
		logger.F("master", masterProfileId),
		logger.F("followers", fmt.Sprintf("%v", validFollowerIds)),
	)

	return nil
}

// StopInputSync 停止输入同步
func (a *App) StopInputSync() error {
	log := logger.New("SyncAPI")

	syncState.mu.Lock()
	defer syncState.mu.Unlock()

	if syncState.syncer != nil {
		syncState.syncer.Stop()
		syncState.syncer = nil
	}
	syncState.active = false
	syncState.masterHwnd = 0
	syncState.masterId = ""
	syncState.followerIds = nil

	log.Info("输入同步已停止")
	return nil
}

// GetSyncStatus 获取当前同步状态
func (a *App) GetSyncStatus() map[string]interface{} {
	syncState.mu.Lock()
	defer syncState.mu.Unlock()

	config := SyncConfig{MouseEnabled: true, KeyEnabled: true}
	if syncState.syncer != nil {
		config = syncState.syncer.GetConfig()
	}

	return map[string]interface{}{
		"active":       syncState.active,
		"masterId":     syncState.masterId,
		"followerIds":  syncState.followerIds,
		"mouseEnabled": config.MouseEnabled,
		"keyEnabled":   config.KeyEnabled,
	}
}

// UpdateSyncConfig 更新同步配置（鼠标/键盘开关）
func (a *App) UpdateSyncConfig(mouseEnabled, keyEnabled bool) error {
	syncState.mu.Lock()
	defer syncState.mu.Unlock()

	if syncState.syncer == nil {
		return fmt.Errorf("同步未启动")
	}
	syncState.syncer.SetConfig(mouseEnabled, keyEnabled)
	return nil
}

// TileWindowsResult 平铺结果
type TileWindowsResult struct {
	Count    int      `json:"count"`
	TiledIds []string `json:"tiledIds"`
	Layout   string   `json:"layout"` // "grid" | "horizontal" | "vertical"
}

// SyncTileWindows 平铺所有已选中实例的窗口
// masterProfileId: 主控实例ID，主控窗口始终放在最左边（index 0）
// layoutMode: grid | horizontal | vertical
func (a *App) SyncTileWindows(profileIds []string, masterProfileId string, layoutMode string) (*TileWindowsResult, error) {
	a.browserMgr.Mutex.Lock()
	defer a.browserMgr.Mutex.Unlock()

	// 收集运行中的实例窗口
	type winInfo struct {
		hwnd      windows.HWND
		profileId string
	}
	var wins []winInfo
	for _, pid := range profileIds {
		profile, ok := a.browserMgr.Profiles[pid]
		if !ok || !profile.Running || profile.Pid <= 0 {
			continue
		}
		hwnd, err := findProcessWindow(profile.Pid)
		if err != nil {
			continue
		}
		wins = append(wins, winInfo{hwnd: hwnd, profileId: pid})
	}

	if len(wins) == 0 {
		return nil, fmt.Errorf("没有可用的运行实例窗口")
	}

	// 确定主控ID：优先用参数传入的，否则从同步状态取
	effectiveMaster := masterProfileId
	if effectiveMaster == "" {
		syncState.mu.Lock()
		effectiveMaster = syncState.masterId
		syncState.mu.Unlock()
	}

	// 确保主控排在最左边（index 0 = 左侧）
	if effectiveMaster != "" {
		masterIdx := -1
		for i, w := range wins {
			if w.profileId == effectiveMaster {
				masterIdx = i
				break
			}
		}
		if masterIdx > 0 {
			masterWin := wins[masterIdx]
			wins = append(wins[:masterIdx], wins[masterIdx+1:]...)
			wins = append([]winInfo{masterWin}, wins...)
		}
	}

	// 获取屏幕可用工作区（排除任务栏）
	// 使用 SystemParametersInfo 获取 SPI_GETWORKAREA
	type RECT struct {
		Left, Top, Right, Bottom int32
	}
	var workArea RECT
	procSystemParametersInfoW := user32dll.NewProc("SystemParametersInfoW")
	procSystemParametersInfoW.Call(0x0030, 0, uintptr(unsafe.Pointer(&workArea)), 0) // SPI_GETWORKAREA = 0x0030
	screenW := int(workArea.Right - workArea.Left)
	screenH := int(workArea.Bottom - workArea.Top)
	originX := int(workArea.Left)
	originY := int(workArea.Top)

	if screenW <= 0 || screenH <= 0 {
		// 回退：使用全屏尺寸
		smCXScreen, _, _ := procGetSystemMetrics.Call(0)
		smCYScreen, _, _ := procGetSystemMetrics.Call(1)
		screenW = int(smCXScreen)
		screenH = int(smCYScreen)
		originX = 0
		originY = 0
	}

	n := len(wins)
	resolvedLayout := layoutMode
	switch resolvedLayout {
	case "horizontal", "vertical", "grid":
		// ok
	default:
		resolvedLayout = "grid"
	}
	if resolvedLayout == "grid" && n <= 2 {
		resolvedLayout = "horizontal"
	}

	var cols, rows int
	switch resolvedLayout {
	case "horizontal":
		cols = n
		rows = 1
	case "vertical":
		cols = 1
		rows = n
	default:
		if n <= 2 {
			cols = n
			rows = 1
		} else if n <= 4 {
			cols = 2
			rows = (n + 1) / 2
		} else if n <= 6 {
			cols = 3
			rows = 2
		} else {
			cols = 4
			rows = (n + 3) / 4
		}
	}

	cellW := screenW / cols
	cellH := screenH / rows

	// Chrome/Windows 在顶层窗口外缘会画 1~数 px 的深色 resize frame/DWM 阴影。
	// 只把最外侧窗口边缘向工作区外轻推，遮掉黑边；内部相邻窗口不再互相重叠，
	// 避免上下/左右内容被压盖。
	const tileWindowBleedPx = 8

	// SW_RESTORE = 9
	procShowWindow := user32dll.NewProc("ShowWindow")

	tiledIds := make([]string, 0, n)
	for i, w := range wins {
		col := i % cols
		row := i / cols
		x := originX + col*cellW
		y := originY + row*cellH
		winW := cellW
		winH := cellH

		if col == 0 {
			x -= tileWindowBleedPx
			winW += tileWindowBleedPx
		}
		if col == cols-1 {
			winW += tileWindowBleedPx
		}
		if row == 0 {
			y -= tileWindowBleedPx
			winH += tileWindowBleedPx
		}
		if row == rows-1 {
			winH += tileWindowBleedPx
		}

		// 先恢复窗口（如果被最小化）
		procShowWindow.Call(uintptr(w.hwnd), 9) // SW_RESTORE

		procSetWindowPos.Call(
			uintptr(w.hwnd),
			0, // HWND_TOP
			uintptr(x),
			uintptr(y),
			uintptr(winW),
			uintptr(winH),
			uintptr(SWP_NOZORDER|SWP_SHOWWINDOW),
		)
		tiledIds = append(tiledIds, w.profileId)
	}

	// 平铺后将主控窗口设为前台焦点（与 Python 版本一致）
	if effectiveMaster != "" && len(wins) > 0 && wins[0].profileId == effectiveMaster {
		procSetForegroundWindow := user32dll.NewProc("SetForegroundWindow")
		procSetForegroundWindow.Call(uintptr(wins[0].hwnd))
	}

	return &TileWindowsResult{
		Count:    len(tiledIds),
		TiledIds: tiledIds,
		Layout:   resolvedLayout,
	}, nil
}

// SyncCloseAll 关闭所有已选中实例
func (a *App) SyncCloseAll(profileIds []string) []string {
	a.browserMgr.Mutex.Lock()
	defer a.browserMgr.Mutex.Unlock()

	closed := make([]string, 0)
	for _, pid := range profileIds {
		profile, ok := a.browserMgr.Profiles[pid]
		if !ok || !profile.Running {
			continue
		}
		if a.browserMgr.BrowserProcesses[pid] != nil && a.browserMgr.BrowserProcesses[pid].Process != nil {
			a.browserMgr.BrowserProcesses[pid].Process.Kill()
		}
		profile.Running = false
		profile.Pid = 0
		profile.DebugPort = 0
		profile.DebugReady = false
		closed = append(closed, pid)
	}
	return closed
}
