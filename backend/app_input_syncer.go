//go:build windows

package backend

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"boost-browser/backend/internal/logger"

	"golang.org/x/sys/windows"
)

// ============================================================================
// InputSyncer — 输入同步引擎（参照 Python Chrome-Manager 实现）
//
// 核心原则（与 Chrome-Manager 一致）：
// 1. 坐标映射：用 GetWindowRect（包含标题栏）的比例换算，不做 ScreenToClient
// 2. 所有消息 PostMessage 到顶层窗口（让 Chrome 内部路由到 render child）
// 3. 滚轮用键盘模拟（VK_UP/DOWN），平均 1 notch = 1.75次方向键
// 4. Ctrl+滚轮用 Ctrl+PLUS/MINUS 键模拟缩放
// ============================================================================

type InputSyncer struct {
	mu            sync.Mutex
	masterHwnd    windows.HWND
	followerHwnds []windows.HWND
	masterPid     int
	masterDebug   int
	followerDebug []int

	// 原子状态：钩子回调中只读 atomic，不加锁
	active       int32 // 1=活跃, 0=停止
	mouseEnabled int32 // 1=启用, 0=禁用
	keyEnabled   int32 // 1=启用, 0=禁用

	mouseHook    uintptr
	keyHook      uintptr
	stopCh       chan struct{}
	hookThreadID uint32

	lastMoveTime int64 // Unix nano

	// URL 同步
	urlStopCh   chan struct{}
	lastSyncURL string

	// 跟随窗口列表原子快照
	followerSnapshot []windows.HWND
	followerMu       sync.RWMutex

	// 诊断计数器
	clickCount   int32
	moveCount    int32
	wheelCount   int32
	keyCount     int32
	hookInstalls int32

	// 滚轮速度微调：每4格额外补3次方向键，平均 1.75 次/格
	wheelRemainder int32
	lastWheelDir   int32
}

// SyncConfig 同步配置
type SyncConfig struct {
	MouseEnabled bool `json:"mouseEnabled"`
	KeyEnabled   bool `json:"keyEnabled"`
}

// NewInputSyncer 创建输入同步器
func NewInputSyncer() *InputSyncer {
	return &InputSyncer{
		stopCh: make(chan struct{}),
	}
}

// Start 启动输入同步
func (s *InputSyncer) Start(masterHwnd windows.HWND, followerHwnds []windows.HWND, masterPid int) error {
	if atomic.LoadInt32(&s.active) == 1 {
		// Stop takes s.mu internally. Do not call it while holding s.mu or a
		// restart from the sync panel deadlocks the backend request.
		s.Stop()
		time.Sleep(100 * time.Millisecond)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.masterHwnd = masterHwnd
	s.masterPid = masterPid
	// 过滤掉主控窗口本身
	filtered := make([]windows.HWND, 0, len(followerHwnds))
	for _, h := range followerHwnds {
		if h != masterHwnd {
			filtered = append(filtered, h)
		}
	}
	s.followerHwnds = filtered

	// 更新原子快照
	s.followerMu.Lock()
	s.followerSnapshot = make([]windows.HWND, len(filtered))
	copy(s.followerSnapshot, filtered)
	s.followerMu.Unlock()

	atomic.StoreInt32(&s.active, 1)
	atomic.StoreInt32(&s.mouseEnabled, 1)
	atomic.StoreInt32(&s.keyEnabled, 1)
	s.stopCh = make(chan struct{})

	// 重置诊断计数器
	atomic.StoreInt32(&s.clickCount, 0)
	atomic.StoreInt32(&s.moveCount, 0)
	atomic.StoreInt32(&s.wheelCount, 0)
	atomic.StoreInt32(&s.keyCount, 0)
	atomic.StoreInt32(&s.wheelRemainder, 0)
	atomic.StoreInt32(&s.lastWheelDir, 0)

	log := logger.New("InputSyncer")
	log.Info("输入同步已启动",
		logger.F("master_hwnd", masterHwnd),
		logger.F("master_pid", masterPid),
		logger.F("follower_count", len(filtered)),
	)

	// 诊断日志
	syncLog("=== InputSyncer Start (Chrome-Manager style) ===")
	syncLog("masterHwnd=%#x pid=%d", masterHwnd, masterPid)
	mRLeft, mRTop, mRRight, mRBottom := getWindowRect(masterHwnd)
	syncLog("master rect=(%d,%d,%d,%d) size=%dx%d", mRLeft, mRTop, mRRight, mRBottom, mRRight-mRLeft, mRBottom-mRTop)
	for i, fhwnd := range filtered {
		fL, fT, fR, fB := getWindowRect(fhwnd)
		syncLog("follower[%d]=%#x rect=(%d,%d,%d,%d) size=%dx%d", i, fhwnd, fL, fT, fR, fB, fR-fL, fB-fT)
	}

	// 安装全局鼠标和键盘钩子
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.New("InputSyncer").Error("installHooks goroutine panic recovered",
					logger.F("error", r),
				)
			}
		}()
		s.installHooks()
	}()

	return nil
}

// StartWithURLSync 启动带 CDP URL 同步的输入同步
func (s *InputSyncer) StartWithURLSync(masterHwnd windows.HWND, followerHwnds []windows.HWND, masterPid int, masterDebugPort int, followerDebugPorts []int) error {
	err := s.Start(masterHwnd, followerHwnds, masterPid)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.masterDebug = masterDebugPort
	s.followerDebug = followerDebugPorts
	s.mu.Unlock()

	// 启动 URL 同步协程
	s.urlStopCh = make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.New("InputSyncer").Error("urlSyncLoop goroutine panic recovered",
					logger.F("error", r),
				)
			}
		}()
		s.urlSyncLoop()
	}()

	log := logger.New("InputSyncer")
	log.Info("CDP URL 同步已启动",
		logger.F("master_debug", masterDebugPort),
		logger.F("follower_count", len(followerDebugPorts)),
	)

	return nil
}

// Stop 停止输入同步
func (s *InputSyncer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if atomic.LoadInt32(&s.active) == 0 {
		return
	}

	syncLog("=== InputSyncer Stop (clicks=%d, moves=%d, wheels=%d, keys=%d) ===",
		atomic.LoadInt32(&s.clickCount), atomic.LoadInt32(&s.moveCount),
		atomic.LoadInt32(&s.wheelCount), atomic.LoadInt32(&s.keyCount))

	atomic.StoreInt32(&s.active, 0)
	close(s.stopCh)

	// 停止 URL 同步
	if s.urlStopCh != nil {
		select {
		case <-s.urlStopCh:
		default:
			close(s.urlStopCh)
		}
		s.urlStopCh = nil
	}

	// 卸载钩子
	if s.mouseHook != 0 {
		procUnhookWindowsHookEx := user32dll.NewProc("UnhookWindowsHookEx")
		procUnhookWindowsHookEx.Call(uintptr(s.mouseHook))
		s.mouseHook = 0
	}
	if s.keyHook != 0 {
		procUnhookWindowsHookEx := user32dll.NewProc("UnhookWindowsHookEx")
		procUnhookWindowsHookEx.Call(uintptr(s.keyHook))
		s.keyHook = 0
	}
	if s.hookThreadID != 0 {
		procPostThreadMessageW := user32dll.NewProc("PostThreadMessageW")
		procPostThreadMessageW.Call(uintptr(s.hookThreadID), 0x0012, 0, 0) // WM_QUIT
		s.hookThreadID = 0
	}

	log := logger.New("InputSyncer")
	log.Info("输入同步已停止")
}

// IsActive 返回同步是否活跃
func (s *InputSyncer) IsActive() bool {
	return atomic.LoadInt32(&s.active) == 1
}

// SetConfig 更新同步配置
func (s *InputSyncer) SetConfig(mouseEnabled, keyEnabled bool) {
	if mouseEnabled {
		atomic.StoreInt32(&s.mouseEnabled, 1)
	} else {
		atomic.StoreInt32(&s.mouseEnabled, 0)
	}
	if keyEnabled {
		atomic.StoreInt32(&s.keyEnabled, 1)
	} else {
		atomic.StoreInt32(&s.keyEnabled, 0)
	}
}

// GetConfig 返回当前同步配置
func (s *InputSyncer) GetConfig() SyncConfig {
	return SyncConfig{
		MouseEnabled: atomic.LoadInt32(&s.mouseEnabled) == 1,
		KeyEnabled:   atomic.LoadInt32(&s.keyEnabled) == 1,
	}
}

// GetStats 返回同步诊断统计
func (s *InputSyncer) GetStats() map[string]int32 {
	return map[string]int32{
		"clicks": atomic.LoadInt32(&s.clickCount),
		"moves":  atomic.LoadInt32(&s.moveCount),
		"wheels": atomic.LoadInt32(&s.wheelCount),
		"keys":   atomic.LoadInt32(&s.keyCount),
		"hooks":  atomic.LoadInt32(&s.hookInstalls),
	}
}

// ============================================================================
// 窗口辅助函数
// ============================================================================

var procGetAncestor = user32dll.NewProc("GetAncestor")

func getAncestor(hwnd windows.HWND, flags uint32) windows.HWND {
	ret, _, _ := procGetAncestor.Call(uintptr(hwnd), uintptr(flags))
	return windows.HWND(ret)
}

const GA_ROOT = 2

// isMasterForeground 检查主控窗口或其子窗口是否在前台
func (s *InputSyncer) isMasterForeground() bool {
	fg, _, _ := procGetForegroundWindow.Call()
	if windows.HWND(fg) == s.masterHwnd {
		return true
	}
	root := getAncestor(windows.HWND(fg), GA_ROOT)
	return root == s.masterHwnd
}

// getFollowerSnapshot 获取跟随窗口列表的原子快照（钩子回调中使用）
func (s *InputSyncer) getFollowerSnapshot() []windows.HWND {
	s.followerMu.RLock()
	defer s.followerMu.RUnlock()
	return s.followerSnapshot
}

// ============================================================================
// 坐标映射（Chrome-Manager 风格，不找 render child）
//
// 关键思路：直接用顶层窗口的 GetWindowRect 做比例换算
// 主控窗口 rect (包含标题栏/地址栏) → 计算相对坐标 → 跟随窗口 rect → 计算客户区坐标
// Chrome 内部会根据 Y 坐标将消息路由到标签栏/地址栏/render child
// ============================================================================

// mapCoordsChromeManager 将主控屏幕坐标映射到跟随窗口的客户区坐标。
//
// 多平铺方式（横向/竖列/网格）引入后，单纯基于顶层窗口 / render child 的混合映射
// 在某些宽高比下会把坐标送偏，表现为“同步像失效了一样”。
// 这里改回与 Python 稳定版一致的策略：
// 1) 先把屏幕坐标转成主控 top-level client 坐标
// 2) 按 master/follower 的 client 区比例做映射
// 3) render-content 映射只保留为兜底
func mapCoordsChromeManager(screenX, screenY int, masterHwnd, followerHwnd windows.HWND) (uintptr, bool) {
	if lparam, ok := mapCoordsViaClientArea(screenX, screenY, masterHwnd, followerHwnd); ok {
		return lparam, true
	}

	// 兜底：保留 render child 内容区映射，避免特殊窗口结构完全失效。
	if lparam, ok := mapCoordsViaRenderContent(screenX, screenY, masterHwnd, followerHwnd); ok {
		return lparam, true
	}

	return 0, false
}

func mapCoordsViaClientArea(screenX, screenY int, masterHwnd, followerHwnd windows.HWND) (uintptr, bool) {
	mClientX, mClientY := screenToClient(masterHwnd, screenX, screenY)
	mW, mH, ok := getClientSize(masterHwnd)
	if !ok || mW <= 0 || mH <= 0 || mW > 10000 || mH > 10000 {
		return 0, false
	}

	relX := float64(mClientX) / float64(mW)
	relY := float64(mClientY) / float64(mH)
	if relX < 0 {
		relX = 0
	}
	if relX > 1 {
		relX = 1
	}
	if relY < 0 {
		relY = 0
	}
	if relY > 1 {
		relY = 1
	}

	fW, fH, ok := getClientSize(followerHwnd)
	if !ok || fW <= 0 || fH <= 0 || fW > 10000 || fH > 10000 {
		return 0, false
	}

	clientX := int(float64(fW) * relX)
	clientY := int(float64(fH) * relY)
	if clientX < -32768 || clientX > 32767 || clientY < -32768 || clientY > 32767 {
		return 0, false
	}
	return MAKELONG(uint16(int16(clientX)), uint16(int16(clientY))), true
}

var procEnumChildWindows = user32dll.NewProc("EnumChildWindows")

func mapCoordsViaRenderContent(screenX, screenY int, masterHwnd, followerHwnd windows.HWND) (uintptr, bool) {
	masterRender := findChromeRenderChild(masterHwnd)
	followerRender := findChromeRenderChild(followerHwnd)
	if masterRender == 0 || followerRender == 0 {
		return 0, false
	}

	mLeft, mTop, mRight, mBottom := getWindowRect(masterRender)
	mW := mRight - mLeft
	mH := mBottom - mTop
	if mW <= 50 || mH <= 50 || mW > 10000 || mH > 10000 {
		return 0, false
	}
	if screenX < int(mLeft) || screenX > int(mRight) || screenY < int(mTop) || screenY > int(mBottom) {
		return 0, false
	}

	relX := float64(screenX-int(mLeft)) / float64(mW)
	relY := float64(screenY-int(mTop)) / float64(mH)
	if relX < 0 || relX > 1 || relY < 0 || relY > 1 {
		return 0, false
	}

	fLeft, fTop, fRight, fBottom := getWindowRect(followerRender)
	fW := fRight - fLeft
	fH := fBottom - fTop
	if fW <= 50 || fH <= 50 || fW > 10000 || fH > 10000 {
		return 0, false
	}

	targetScreenX := int(fLeft) + int(float64(fW)*relX)
	targetScreenY := int(fTop) + int(float64(fH)*relY)
	clientX, clientY := screenToClient(followerHwnd, targetScreenX, targetScreenY)
	if clientX < -32768 || clientX > 32767 || clientY < -32768 || clientY > 32767 {
		return 0, false
	}
	return MAKELONG(uint16(int16(clientX)), uint16(int16(clientY))), true
}

func findChromeRenderChild(hwnd windows.HWND) windows.HWND {
	var found windows.HWND
	cb := windows.NewCallback(func(child windows.HWND, lParam uintptr) uintptr {
		if !isWindowVisible(child) {
			return 1
		}
		if getWindowClassName(child) != "Chrome_RenderWidgetHostHWND" {
			return 1
		}
		left, top, right, bottom := getWindowRect(child)
		if right-left <= 50 || bottom-top <= 50 {
			return 1
		}
		found = child
		return 0
	})
	procEnumChildWindows.Call(uintptr(hwnd), cb, 0)
	return found
}

// ============================================================================
// 钩子安装和消息循环
// ============================================================================

func (s *InputSyncer) installHooks() {
	syncLog("installHooks: 开始安装钩子...")

	procGetCurrentThreadId := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCurrentThreadId")
	threadID, _, _ := procGetCurrentThreadId.Call()
	s.mu.Lock()
	s.hookThreadID = uint32(threadID)
	s.mu.Unlock()

	mouseHookProc := windows.NewCallback(s.mouseHookCallback)
	keyHookProc := windows.NewCallback(s.keyHookCallback)

	// WH_MOUSE_LL = 14, WH_KEYBOARD_LL = 13
	setHookEx := user32dll.NewProc("SetWindowsHookExW")
	mouseHook, mouseErr, mouseErrno := setHookEx.Call(14, mouseHookProc, 0, 0)
	keyHook, keyErr, keyErrno := setHookEx.Call(13, keyHookProc, 0, 0)

	syncLog("installHooks: mouseHook=%#x err=%v errno=%d", mouseHook, mouseErr, mouseErrno)
	syncLog("installHooks: keyHook=%#x err=%v errno=%d", keyHook, keyErr, keyErrno)

	if mouseHook == 0 {
		syncLog("installHooks: ❌ 鼠标钩子安装失败！err=%v errno=%d", mouseErr, mouseErrno)
	}
	if keyHook == 0 {
		syncLog("installHooks: ❌ 键盘钩子安装失败！err=%v errno=%d", keyErr, keyErrno)
	}

	s.mu.Lock()
	s.mouseHook = mouseHook
	s.keyHook = keyHook
	s.mu.Unlock()

	if mouseHook != 0 && keyHook != 0 {
		atomic.StoreInt32(&s.hookInstalls, 2)
		syncLog("installHooks: ✅ 钩子安装成功，开始消息循环")
	} else {
		syncLog("installHooks: ⚠️ 部分钩子安装失败，继续尝试消息循环")
	}

	type MSG struct {
		HWnd    windows.HWND
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      struct{ X, Y int32 }
	}

	getMessageW := user32dll.NewProc("GetMessageW")
	translateMessage := user32dll.NewProc("TranslateMessage")
	dispatchMessageW := user32dll.NewProc("DispatchMessageW")

	for {
		select {
		case <-s.stopCh:
			syncLog("installHooks: 收到停止信号，退出消息循环")
			return
		default:
		}

		var msg MSG
		ret, _, _ := getMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 || ret == 0xFFFFFFFF {
			syncLog("installHooks: GetMessageW 返回 %d，退出消息循环", ret)
			return
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// ============================================================================
// 鼠标钩子回调
// ============================================================================

// MSLLHOOKSTRUCT 结构体（64位对齐）
type MSLLHOOKSTRUCT struct {
	Pt          struct{ X, Y int32 }
	MouseData   uint32
	Flags       uint32
	Time        uint32
	_           uint32 // padding，确保 dwExtraInfo 在 8 字节边界
	DwExtraInfo uintptr
}

func (s *InputSyncer) mouseHookCallback(nCode int, wParam uintptr, lParam uintptr) uintptr {
	defer func() {
		if r := recover(); r != nil {
			logger.New("InputSyncer").Error("mouse hook callback panic recovered",
				logger.F("error", r),
			)
		}
	}()

	if nCode < 0 || atomic.LoadInt32(&s.active) == 0 || atomic.LoadInt32(&s.mouseEnabled) == 0 || !s.isMasterForeground() {
		return callNextHook(nCode, wParam, lParam)
	}
	if lParam == 0 {
		return callNextHook(nCode, wParam, lParam)
	}

	hook := (*MSLLHOOKSTRUCT)(unsafe.Pointer(lParam))
	msg := uint32(wParam)
	screenX := int(hook.Pt.X)
	screenY := int(hook.Pt.Y)

	// 获取快照（原子读取，不加锁）
	followers := s.getFollowerSnapshot()

	switch msg {
	case WM_LBUTTONDOWN, WM_LBUTTONUP, WM_RBUTTONDOWN, WM_RBUTTONUP, WM_MBUTTONDOWN, WM_MBUTTONUP:
		atomic.AddInt32(&s.clickCount, 1)
		for _, hwnd := range followers {
			if !isWindow(hwnd) {
				continue
			}

			// 映射坐标到跟随窗口客户区坐标（Chrome-Manager 风格）
			lparam, ok := mapCoordsChromeManager(screenX, screenY, s.masterHwnd, hwnd)
			if !ok {
				continue
			}

			// 构造 wParam（按键状态）
			var wparam uintptr
			switch msg {
			case WM_LBUTTONDOWN:
				wparam = MK_LBUTTON
			case WM_LBUTTONUP:
				wparam = 0
			case WM_RBUTTONDOWN:
				wparam = MK_RBUTTON
			case WM_RBUTTONUP:
				wparam = 0
			case WM_MBUTTONDOWN:
				wparam = MK_MBUTTON
			case WM_MBUTTONUP:
				wparam = 0
			}

			// 发到顶层窗口：先 WM_MOUSEMOVE 让 Chrome 更新 hover 状态
			procPostMessageW.Call(uintptr(hwnd), WM_MOUSEMOVE, wparam, lparam)
			// 再发点击消息
			procPostMessageW.Call(uintptr(hwnd), uintptr(msg), wparam, lparam)

			// 仅在首次点击时记录详细日志（避免日志过多）
			if atomic.LoadInt32(&s.clickCount) <= 5 {
				mL, mT, mR, mB := getWindowRect(s.masterHwnd)
				fL, fT, fR, fB := getWindowRect(hwnd)
				syncLog("CLICK #%d: msg=%#x screen(%d,%d) masterRect=(%d,%d,%d,%d) followerRect=(%d,%d,%d,%d) lparam=%#x",
					atomic.LoadInt32(&s.clickCount), msg, screenX, screenY,
					mL, mT, mR, mB, fL, fT, fR, fB, lparam)
			}
		}

	case WM_MOUSEWHEEL:
		atomic.AddInt32(&s.wheelCount, 1)
		delta := int16(hook.MouseData >> 16) // Windows 原始值，通常 ±120 (WHEEL_DELTA)

		// Ctrl+滚轮 → 缩放
		if isKeyDown(VK_CONTROL) {
			for _, hwnd := range followers {
				if !isWindow(hwnd) {
					continue
				}
				if delta > 0 {
					procPostMessageW.Call(uintptr(hwnd), WM_KEYDOWN, VK_CONTROL, makeKeyLParam(VK_CONTROL, true))
					procPostMessageW.Call(uintptr(hwnd), WM_KEYDOWN, 0xBB, makeKeyLParam(0xBB, true))
					procPostMessageW.Call(uintptr(hwnd), WM_KEYUP, 0xBB, makeKeyLParam(0xBB, false))
					procPostMessageW.Call(uintptr(hwnd), WM_KEYUP, VK_CONTROL, makeKeyLParam(VK_CONTROL, false))
				} else {
					procPostMessageW.Call(uintptr(hwnd), WM_KEYDOWN, VK_CONTROL, makeKeyLParam(VK_CONTROL, true))
					procPostMessageW.Call(uintptr(hwnd), WM_KEYDOWN, 0xBD, makeKeyLParam(0xBD, true))
					procPostMessageW.Call(uintptr(hwnd), WM_KEYUP, 0xBD, makeKeyLParam(0xBD, false))
					procPostMessageW.Call(uintptr(hwnd), WM_KEYUP, VK_CONTROL, makeKeyLParam(VK_CONTROL, false))
				}
			}
			return callNextHook(nCode, wParam, lParam)
		}

		// Windows delta 通常为 ±120 (WHEEL_DELTA)。跟随速度调为：平均 1 notch = 2.5次方向键。
		// 实现方式：每格至少2次方向键，每累计2格额外补1次；不使用 PageUp/PageDown，避免跳太远。
		scrollUp := delta > 0
		notches := int(delta) / 120
		if notches == 0 {
			notches = 1 // 高精度滚轮不足120时，也按1格处理
		}
		if notches < 0 {
			notches = -notches
		}

		dir := int32(1)
		if !scrollUp {
			dir = -1
		}
		if atomic.LoadInt32(&s.lastWheelDir) != dir {
			atomic.StoreInt32(&s.wheelRemainder, 0)
			atomic.StoreInt32(&s.lastWheelDir, dir)
		}

		keyPresses := notches * 2
		rem := atomic.AddInt32(&s.wheelRemainder, int32(notches))
		if rem >= 2 {
			extra := rem / 2
			keyPresses += int(extra)
			atomic.AddInt32(&s.wheelRemainder, -extra*2)
		}
		if keyPresses > 20 {
			keyPresses = 20
		}

		vk := uint32(VK_UP)
		if !scrollUp {
			vk = VK_DOWN
		}

		for _, hwnd := range followers {
			if !isWindow(hwnd) {
				continue
			}
			for i := 0; i < keyPresses; i++ {
				procPostMessageW.Call(uintptr(hwnd), WM_KEYDOWN, uintptr(vk), makeKeyLParam(vk, true))
				procPostMessageW.Call(uintptr(hwnd), WM_KEYUP, uintptr(vk), makeKeyLParam(vk, false))
			}
		}

	case WM_MOUSEMOVE:
		atomic.AddInt32(&s.moveCount, 1)
		// 跟随窗口越多，鼠标移动同步越容易把整机拖卡。
		// 这里按窗口数动态降采样：少量窗口保留手感，多窗口优先稳。
		throttle := 16 * time.Millisecond
		switch followerCount := len(followers); {
		case followerCount >= 10:
			throttle = 50 * time.Millisecond
		case followerCount >= 6:
			throttle = 33 * time.Millisecond
		case followerCount >= 3:
			throttle = 24 * time.Millisecond
		}
		now := time.Now().UnixNano()
		last := atomic.LoadInt64(&s.lastMoveTime)
		if now-last < int64(throttle) {
			return callNextHook(nCode, wParam, lParam)
		}
		atomic.StoreInt64(&s.lastMoveTime, now)

		for _, hwnd := range followers {
			if !isWindow(hwnd) {
				continue
			}
			lparam, ok := mapCoordsChromeManager(screenX, screenY, s.masterHwnd, hwnd)
			if !ok {
				continue
			}
			procPostMessageW.Call(uintptr(hwnd), WM_MOUSEMOVE, 0, lparam)
		}
	}

	return callNextHook(nCode, wParam, lParam)
}

// ============================================================================
// 键盘钩子回调
// ============================================================================

func (s *InputSyncer) keyHookCallback(nCode int, wParam uintptr, lParam uintptr) uintptr {
	defer func() {
		if r := recover(); r != nil {
			logger.New("InputSyncer").Error("key hook callback panic recovered",
				logger.F("error", r),
			)
		}
	}()

	if nCode < 0 || atomic.LoadInt32(&s.active) == 0 || atomic.LoadInt32(&s.keyEnabled) == 0 || !s.isMasterForeground() {
		return callNextHook(nCode, wParam, lParam)
	}
	if lParam == 0 {
		return callNextHook(nCode, wParam, lParam)
	}

	type KBDLLHOOKSTRUCT struct {
		VkCode      uint32
		ScanCode    uint32
		Flags       uint32
		Time        uint32
		DwExtraInfo uintptr
	}
	hook := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))

	vk := hook.VkCode
	msg := uint32(wParam)

	if msg != WM_KEYDOWN && msg != WM_KEYUP && msg != WM_SYSKEYDOWN && msg != WM_SYSKEYUP {
		return callNextHook(nCode, wParam, lParam)
	}

	atomic.AddInt32(&s.keyCount, 1)

	// 获取跟随窗口快照
	followers := s.getFollowerSnapshot()

	// 检测修饰键状态
	ctrlPressed := isKeyDown(VK_CONTROL)
	altPressed := isKeyDown(VK_MENU)

	for _, hwnd := range followers {
		if !isWindow(hwnd) {
			continue
		}

		if msg == WM_KEYDOWN {
			// 构造正确的 lParam
			keyParam := makeKeyLParam(vk, true)

			// Ctrl+A/C/V/X/Z 组合键
			if ctrlPressed {
				switch vk {
				case 0x41, 0x43, 0x56, 0x58, 0x5A: // A, C, V, X, Z
					procPostMessageW.Call(uintptr(hwnd), WM_KEYDOWN, VK_CONTROL, makeKeyLParam(VK_CONTROL, true))
					procPostMessageW.Call(uintptr(hwnd), WM_KEYDOWN, uintptr(vk), keyParam)
					procPostMessageW.Call(uintptr(hwnd), WM_KEYUP, uintptr(vk), makeKeyLParam(vk, false))
					procPostMessageW.Call(uintptr(hwnd), WM_KEYUP, VK_CONTROL, makeKeyLParam(VK_CONTROL, false))
					continue
				}
			}

			// Alt 组合键
			if altPressed {
				procPostMessageW.Call(uintptr(hwnd), WM_SYSKEYDOWN, uintptr(vk), keyParam)
				continue
			}

			// 特殊键：只发 WM_KEYDOWN
			if isSpecialKey(vk) {
				procPostMessageW.Call(uintptr(hwnd), WM_KEYDOWN, uintptr(vk), keyParam)
			} else {
				// 普通字符：只发 WM_CHAR
				ch := toUnicode(uint16(vk), uint16(hook.ScanCode), (hook.Flags&0x01) != 0)
				if ch != 0 {
					procPostMessageW.Call(uintptr(hwnd), WM_CHAR, uintptr(ch), keyParam)
				}
			}
		} else if msg == WM_KEYUP {
			if !isSpecialKey(vk) && vk != VK_CONTROL && vk != VK_SHIFT && vk != VK_MENU {
				continue
			}
			if vk == VK_CONTROL || vk == VK_SHIFT || vk == VK_MENU {
				continue
			}
			procPostMessageW.Call(uintptr(hwnd), WM_KEYUP, uintptr(vk), makeKeyLParam(vk, false))
		} else if msg == WM_SYSKEYDOWN {
			procPostMessageW.Call(uintptr(hwnd), uintptr(msg), uintptr(vk), makeKeyLParam(vk, true))
		} else if msg == WM_SYSKEYUP {
			procPostMessageW.Call(uintptr(hwnd), uintptr(msg), uintptr(vk), makeKeyLParam(vk, false))
		}
	}

	return callNextHook(nCode, wParam, lParam)
}

// isSpecialKey 判断是否为非打印特殊键
func isSpecialKey(vk uint32) bool {
	switch vk {
	case 0x08, 0x09, 0x0D, 0x1B, 0x20, // Backspace, Tab, Enter, Esc, Space
		0x25, 0x26, 0x27, 0x28, // Left, Up, Right, Down
		0x21, 0x22, 0x23, 0x24, // Page Up, Page Down, End, Home
		0x2D, 0x2E, // Insert, Delete
		0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7A, 0x7B: // F1-F12
		return true
	}
	return false
}

// ============================================================================
// 辅助函数
// ============================================================================

var procGetWindowRect = user32dll.NewProc("GetWindowRect")

func getWindowRect(hwnd windows.HWND) (left, top, right, bottom int32) {
	type RECT struct {
		Left, Top, Right, Bottom int32
	}
	var rect RECT
	procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
	return rect.Left, rect.Top, rect.Right, rect.Bottom
}

func isKeyDown(vk uint32) bool {
	r1, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return r1&0x8000 != 0
}

func buildKeyLParam(sc uint32, isExtended bool, isUp bool) uintptr {
	var lparam uintptr
	lparam = uintptr(sc) << 16
	if isExtended {
		lparam |= 1 << 24
	}
	if isUp {
		lparam |= 1<<30 | 1<<31
	}
	return lparam
}

var procToUnicodeEx = user32dll.NewProc("ToUnicodeEx")

func toUnicode(vk uint16, sc uint16, isExtended bool) rune {
	if isExtended {
		return 0
	}
	if vk >= 0x03 {
		switch {
		case vk >= 0x08 && vk <= 0x09:
			return 0
		case vk >= 0x0D && vk <= 0x0E:
			return 0
		case vk >= 0x10 && vk <= 0x12:
			return 0
		case vk == 0x1B:
			return 0
		case vk >= 0x20 && vk <= 0x2E:
			return 0
		case vk >= 0x70 && vk <= 0x87:
			return 0
		case vk >= 0x90 && vk <= 0x97:
			return 0
		}
	}

	var state [256]byte
	var buf [4]uint16

	scanCode := uint32(sc)
	if isExtended {
		scanCode |= 0x100
	}

	ret, _, _ := procToUnicodeEx.Call(
		uintptr(vk),
		uintptr(scanCode),
		uintptr(unsafe.Pointer(&state[0])),
		uintptr(unsafe.Pointer(&buf[0])),
		4,
		0,
		0,
	)

	if ret == 1 {
		return rune(buf[0])
	}
	return 0
}

func callNextHook(nCode int, wParam uintptr, lParam uintptr) uintptr {
	procCallNextHook := user32dll.NewProc("CallNextHookEx")
	ret, _, _ := procCallNextHook.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

// ============================================================================
// 调试日志
// ============================================================================

const syncDebugLog = false

var syncLogFile *os.File
var syncLogOnce sync.Once

func syncLog(format string, args ...interface{}) {
	if !syncDebugLog {
		return
	}
	syncLogOnce.Do(func() {
		var err error
		syncLogFile, err = os.OpenFile(`C:\sync_debug.txt`, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return
		}
	})
	if syncLogFile != nil {
		msg := fmt.Sprintf(format, args...)
		syncLogFile.WriteString(time.Now().Format("15:04:05.000") + " " + msg + "\n")
		syncLogFile.Sync()
	}
}

// ============================================================================
// CDP URL 同步
// ============================================================================

func (s *InputSyncer) urlSyncLoop() {
	for {
		select {
		case <-s.urlStopCh:
			return
		default:
		}

		if atomic.LoadInt32(&s.active) == 0 {
			return
		}

		s.mu.Lock()
		masterDebug := s.masterDebug
		followerDebug := make([]int, len(s.followerDebug))
		copy(followerDebug, s.followerDebug)
		s.mu.Unlock()

		if masterDebug > 0 && len(followerDebug) > 0 {
			url := s.getMasterURL(masterDebug)
			if url != "" && url != s.lastSyncURL && !isAboutBlank(url) {
				s.lastSyncURL = url
				for _, port := range followerDebug {
					if port > 0 {
						s.navigateFollower(port, url)
					}
				}
			}
		}

		time.Sleep(900 * time.Millisecond)
	}
}

func (s *InputSyncer) getMasterURL(debugPort int) string {
	result, err := cdpCall(debugPort, "Runtime.evaluate", map[string]any{
		"expression":    "location.href",
		"returnByValue": true,
	})
	if err != nil {
		return ""
	}
	val, ok := result["value"]
	if !ok {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return str
}

func (s *InputSyncer) navigateFollower(debugPort int, url string) {
	_, _ = cdpCall(debugPort, "Page.navigate", map[string]any{
		"url": url,
	})
}

func isAboutBlank(url string) bool {
	return url == "about:blank" || url == "about:blank#" || url == ""
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
