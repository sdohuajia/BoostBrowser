//go:build windows

package backend

import (
	"boost-browser/backend/internal/logger"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	extensionPopupTargetWidth     = 390
	extensionPopupTargetHeight    = 620
	startupBrowserDefaultWidth    = 1280
	startupBrowserDefaultHeight   = 900
	startupBrowserMinUsableWidth  = 1000
	startupBrowserMinUsableHeight = 700
	swRestore                     = 9
	smCxScreen                    = 0
	smCyScreen                    = 1
)

type winRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

var globalServiceWorkerDevToolsRestorerOnce sync.Once

var globalExtensionPopupSizerOnce sync.Once

var globalSerializedWindowWatchersOnce sync.Once

// StartGlobalSerializedWindowWatchers runs the two packaged-runtime fallback
// window repairs inside one shared goroutine/ticker. The independent dual-loop
// version was stable when each watcher ran alone, but could trigger watchdog
// restarts when both global loops were active concurrently. Keep both behaviors,
// but serialize their window enumeration/mutation to avoid cross-loop races.
func StartGlobalSerializedWindowWatchers(appRoot string) {
	root := normalizeWindowsPath(appRoot)
	if root == "" {
		return
	}
	globalSerializedWindowWatchersOnce.Do(func() {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.New("PopupSizer").Error("serialized global window watchers goroutine panic recovered",
						logger.F("error", r),
					)
				}
			}()
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				clampExtensionPopupWindowsForAppRoot(root)
				restoreOffscreenServiceWorkerDevToolsWindowsForAppRoot(root)
			}
		}()
	})
}

// StartGlobalExtensionPopupSizer is a process-root fallback for extension
// popups. The per-profile sizer is tied to the launch PID, but Chromium can
// re-parent/rotate the visible browser process after startup or after the Wails
// host is restarted. In that case the old PID-scoped watcher exits and later
// wallet/login popups can appear fullscreen again. This global watcher only
// touches Chrome_WidgetWin popup-looking windows whose owning process lives
// under the packaged Boost Browser runtime directory.
func StartGlobalExtensionPopupSizer(appRoot string) {
	root := normalizeWindowsPath(appRoot)
	if root == "" {
		return
	}
	globalExtensionPopupSizerOnce.Do(func() {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.New("PopupSizer").Error("global extension popup sizer goroutine panic recovered",
						logger.F("error", r),
					)
				}
			}()
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				clampExtensionPopupWindowsForAppRoot(root)
			}
		}()
	})
}

// StartGlobalServiceWorkerDevToolsRestorer keeps extension Service Worker
// DevTools windows usable even after the main Wails host is restarted by the
// watchdog. Chromium is launched offscreen during startup suppression; later
// DevTools windows opened from chrome://extensions can inherit that offscreen
// position from the original chrome.exe command line, so per-profile goroutines
// are not enough when the host process has been restarted.
func StartGlobalServiceWorkerDevToolsRestorer(appRoot string) {
	root := normalizeWindowsPath(appRoot)
	if root == "" {
		return
	}
	globalServiceWorkerDevToolsRestorerOnce.Do(func() {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.New("PopupSizer").Error("service worker devtools restorer goroutine panic recovered",
						logger.F("error", r),
					)
				}
			}()
			ticker := time.NewTicker(300 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				restoreOffscreenServiceWorkerDevToolsWindowsForAppRoot(root)
			}
		}()
	})
}

// startExtensionPopupSizer keeps wallet extension popup windows at a normal
// extension-like size. Chromium may create extension prompts as oversized
// top-level Chrome_WidgetWin_1 windows. Do not touch normal browser windows;
// clamp only strong wallet/prompt titles such as "Petra - Prompt", "OKX Wallet"
// and "MetaMask Notification".
func startExtensionPopupSizer(pid int) {
	if pid <= 0 {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.New("PopupSizer").Error("extension popup sizer goroutine panic recovered",
					logger.F("pid", pid),
					logger.F("error", r),
				)
			}
		}()
		// 继续在浏览器存活期间低频观察：用户后续手动点钱包扩展时，Chromium 仍可能
		// 把 popup 创建到离屏位置，需要我们把它拉回可视区。但绝不能再 Show/抢焦点。
		fastTicker := time.NewTicker(300 * time.Millisecond)
		defer fastTicker.Stop()
		fastDeadline := time.After(10 * time.Second)
		fastPhase := true
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			if !isProcessAlive(pid) {
				return
			}
			if fastPhase {
				select {
				case <-fastTicker.C:
					func() {
						defer func() {
							if r := recover(); r != nil {
								logger.New("PopupSizer").Error("extension popup sizer tick panic recovered",
									logger.F("pid", pid),
									logger.F("error", r),
								)
							}
						}()
						clampExtensionPopupWindows(pid)
						restoreOffscreenServiceWorkerDevToolsWindows(pid)
					}()
				case <-fastDeadline:
					fastPhase = false
				}
			} else {
				select {
				case <-ticker.C:
					func() {
						defer func() {
							if r := recover(); r != nil {
								logger.New("PopupSizer").Error("extension popup sizer tick panic recovered",
									logger.F("pid", pid),
									logger.F("error", r),
								)
							}
						}()
						clampExtensionPopupWindows(pid)
						restoreOffscreenServiceWorkerDevToolsWindows(pid)
					}()
				}
			}
		}
	}()
}

// restoreBrowserWindowsAfterStartup is paired with the offscreen startup window
// position used during startup suppression. It brings only non-popup top-level
// windows owned by the launched browser process back onscreen after automatic
// extension targets were closed, so startup wallet/onboarding pages never flash
// onscreen while later manual toolbar clicks still open normally.
func restoreBrowserWindowsAfterStartup(pid int) {
	if pid <= 0 {
		return
	}
	pidSet := collectProcessTreePIDs(pid)
	cb := windows.NewCallback(func(hwnd windows.HWND, lParam uintptr) uintptr {
		var windowPID uint32
		procGetWindowThreadProcessID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&windowPID)))
		if !pidSet[windowPID] {
			return 1
		}

		title := strings.TrimSpace(getWindowTitle(hwnd))
		className := strings.TrimSpace(getWindowClassName(hwnd))
		if title == "" || isAuxiliaryIMEWindowTitleOrClass(title, className) || isStrongExtensionPopupTitle(strings.ToLower(title)) {
			return 1
		}

		procShowWindow.Call(uintptr(hwnd), swRestore)
		if rect, ok := getTopLevelWindowRect(hwnd); ok {
			if x, y, w, h, shouldMove := startupBrowserWindowPlacement(rect); shouldMove {
				procSetWindowPos.Call(
					uintptr(hwnd),
					0,
					uintptr(x),
					uintptr(y),
					uintptr(w),
					uintptr(h),
					SWP_NOZORDER|SWP_SHOWWINDOW,
				)
			}
		}
		procSetForegroundWindow.Call(uintptr(hwnd))
		return 1
	})
	procEnumWindows.Call(cb, 0)
}

func startupBrowserWindowPlacement(rect winRect) (int, int, int, int, bool) {
	currentW := int(rect.Right - rect.Left)
	currentH := int(rect.Bottom - rect.Top)
	if currentW <= 0 || currentH <= 0 {
		currentW = startupBrowserDefaultWidth
		currentH = startupBrowserDefaultHeight
	}

	screenW, _, _ := procGetSystemMetrics.Call(smCxScreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCyScreen)
	maxW := int(screenW) - 120
	maxH := int(screenH) - 160
	if maxW <= 0 {
		maxW = startupBrowserDefaultWidth
	}
	if maxH <= 0 {
		maxH = startupBrowserDefaultHeight
	}

	w := currentW
	h := currentH
	tooSmall := currentW < startupBrowserMinUsableWidth || currentH < startupBrowserMinUsableHeight
	if tooSmall {
		w = minInt(startupBrowserDefaultWidth, maxW)
		h = minInt(startupBrowserDefaultHeight, maxH)
	}

	offscreen := rect.Left < -10000 || rect.Top < -10000 || rect.Left > int32(maxW+120) || rect.Top > int32(maxH+160)
	if !offscreen && !tooSmall {
		return int(rect.Left), int(rect.Top), w, h, false
	}

	return 80, 80, w, h, true
}

// Cached callback state for clampExtensionPopupWindows to avoid creating
// a new windows.NewCallback on every tick (Windows has a hard callback limit).
var (
	clampCbOnce   sync.Once
	clampCb       uintptr
	clampPidSet   map[uint32]bool
	clampPidSetMu sync.Mutex
)

var (
	globalClampCbOnce sync.Once
	globalClampCb     uintptr
	globalClampRoot   string
	globalClampRootMu sync.Mutex
)

func clampExtensionPopupWindows(pid int) {
	clampPidSetMu.Lock()
	clampPidSet = collectProcessTreePIDs(pid)
	clampPidSetMu.Unlock()

	clampCbOnce.Do(func() {
		clampCb = windows.NewCallback(func(hwnd windows.HWND, lParam uintptr) uintptr {
			title := strings.ToLower(strings.TrimSpace(getWindowTitle(hwnd)))
			className := strings.ToLower(strings.TrimSpace(getWindowClassName(hwnd)))
			if !looksLikeWalletExtensionPopup(title) && !mayBeGenericExtensionPopupWindow(title, className) {
				return 1
			}

			var windowPID uint32
			procGetWindowThreadProcessID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&windowPID)))
			clampPidSetMu.Lock()
			ps := clampPidSet
			clampPidSetMu.Unlock()
			if !shouldHandleExtensionPopupWindowOwner(windowPID, ps, title) {
				return 1
			}

			rect, ok := getTopLevelWindowRect(hwnd)
			if !ok {
				return 1
			}
			visible := isWindowVisible(hwnd)
			if !visible && !shouldHandleHiddenExtensionPopupWindow(title, className, rect) {
				return 1
			}
			popupLike := looksLikeWalletExtensionPopup(title) || isStrongExtensionPopupTitle(title)
			if !popupLike {
				x, y, w, h, shouldMove := genericExtensionPopupWindowPlacement(title, className, rect)
				if !shouldMove {
					return 1
				}
				if isOffscreenWindowRect(rect) {
					procShowWindow.Call(uintptr(hwnd), swRestore)
				}
				procSetWindowPos.Call(
					uintptr(hwnd),
					0,
					uintptr(x),
					uintptr(y),
					uintptr(w),
					uintptr(h),
					SWP_NOZORDER|SWP_NOACTIVATE,
				)
				return 1
			}
			x, y, w, h, shouldMove := extensionPopupWindowPlacement(title, rect)
			if !shouldMove {
				return 1
			}
			procShowWindow.Call(uintptr(hwnd), swRestore)
			procSetWindowPos.Call(
				uintptr(hwnd),
				0,
				uintptr(x),
				uintptr(y),
				uintptr(w),
				uintptr(h),
				SWP_NOZORDER|SWP_NOACTIVATE,
			)
			return 1
		})
	})
	procEnumWindows.Call(clampCb, 0)
}

func clampExtensionPopupWindowsForAppRoot(appRoot string) {
	globalClampRootMu.Lock()
	globalClampRoot = appRoot
	globalClampRootMu.Unlock()

	globalClampCbOnce.Do(func() {
		globalClampCb = windows.NewCallback(func(hwnd windows.HWND, lParam uintptr) uintptr {
			title := strings.ToLower(strings.TrimSpace(getWindowTitle(hwnd)))
			className := strings.ToLower(strings.TrimSpace(getWindowClassName(hwnd)))
			if !looksLikeWalletExtensionPopup(title) && !mayBeGenericExtensionPopupWindow(title, className) {
				return 1
			}

			var windowPID uint32
			procGetWindowThreadProcessID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&windowPID)))
			globalClampRootMu.Lock()
			root := globalClampRoot
			globalClampRootMu.Unlock()
			if !processBelongsToBoostBrowserRuntime(windowPID, root) {
				return 1
			}

			rect, ok := getTopLevelWindowRect(hwnd)
			if !ok {
				return 1
			}
			visible := isWindowVisible(hwnd)
			// Root-cause fix for "instance startup pops extension password/login windows":
			// the global watcher runs continuously for the packaged runtime, so calling
			// ShowWindow on hidden popup candidates can surface startup-created wallet /
			// login windows that Chrome intentionally kept hidden after cleanup or from a
			// previous session restore. That turns a background suppressed extension
			// window into a visible password/unlock prompt on every instance launch.
			// The global fallback must only normalize windows that are already visible to
			// the user; it must never unhide extension/login popups on its own.
			if !visible {
				return 1
			}
			popupLike := looksLikeWalletExtensionPopup(title) || isStrongExtensionPopupTitle(title)
			if !popupLike {
				x, y, w, h, shouldMove := genericExtensionPopupWindowPlacement(title, className, rect)
				if !shouldMove {
					return 1
				}
				if isOffscreenWindowRect(rect) {
					procShowWindow.Call(uintptr(hwnd), swRestore)
				}
				procSetWindowPos.Call(
					uintptr(hwnd),
					0,
					uintptr(x),
					uintptr(y),
					uintptr(w),
					uintptr(h),
					SWP_NOZORDER|SWP_NOACTIVATE,
				)
				return 1
			}
			x, y, w, h, shouldMove := extensionPopupWindowPlacement(title, rect)
			if !shouldMove {
				return 1
			}
			procShowWindow.Call(uintptr(hwnd), swRestore)
			procSetWindowPos.Call(
				uintptr(hwnd),
				0,
				uintptr(x),
				uintptr(y),
				uintptr(w),
				uintptr(h),
				SWP_NOZORDER|SWP_NOACTIVATE,
			)
			return 1
		})
	})
	procEnumWindows.Call(globalClampCb, 0)
}

func extensionPopupWindowPlacement(title string, rect winRect) (int, int, int, int, bool) {
	currentW := int(rect.Right - rect.Left)
	currentH := int(rect.Bottom - rect.Top)
	if currentW <= 0 || currentH <= 0 {
		currentW = extensionPopupTargetWidth
		currentH = extensionPopupTargetHeight
	}

	shouldResize := needsExtensionPopupResize(title, currentW, currentH)
	shouldMoveOnscreen := isOffscreenWindowRect(rect)
	if !shouldResize && !shouldMoveOnscreen {
		return int(rect.Left), int(rect.Top), currentW, currentH, false
	}

	x := int(rect.Left)
	y := int(rect.Top)
	if shouldMoveOnscreen {
		x, y = 120, 120
	}
	w := currentW
	h := currentH
	if shouldResize {
		w = extensionPopupTargetWidth
		h = extensionPopupTargetHeight
	}
	return x, y, w, h, true
}

func genericExtensionPopupWindowPlacement(title, className string, rect winRect) (int, int, int, int, bool) {
	currentW := int(rect.Right - rect.Left)
	currentH := int(rect.Bottom - rect.Top)
	if currentW <= 0 || currentH <= 0 {
		currentW = extensionPopupTargetWidth
		currentH = extensionPopupTargetHeight
	}

	shouldMoveOnscreen := shouldRestoreGenericExtensionPopupWindow(title, className, rect)
	shouldResize := needsGenericExtensionPopupResize(title, className, currentW, currentH)
	if !shouldMoveOnscreen && !shouldResize {
		return int(rect.Left), int(rect.Top), currentW, currentH, false
	}

	x := int(rect.Left)
	y := int(rect.Top)
	if shouldMoveOnscreen {
		x, y = 120, 120
	}
	w := currentW
	h := currentH
	if shouldResize {
		w = extensionPopupTargetWidth
		h = extensionPopupTargetHeight
	}
	return x, y, w, h, true
}

func isOffscreenWindowRect(rect winRect) bool {
	screenW, _, _ := procGetSystemMetrics.Call(smCxScreen)
	screenH, _, _ := procGetSystemMetrics.Call(smCyScreen)
	maxW := int32(screenW)
	maxH := int32(screenH)
	if maxW <= 0 {
		maxW = 1920
	}
	if maxH <= 0 {
		maxH = 1080
	}
	return rect.Right < 0 || rect.Bottom < 0 || rect.Left > maxW || rect.Top > maxH || rect.Left < -10000 || rect.Top < -10000
}

func looksLikeWalletExtensionPopup(title string) bool {
	if title == "" {
		return false
	}
	keywords := []string{
		"okx wallet",
		"metamask",
		"rabby",
		"phantom",
		"bitget wallet",
		"keplr",
		"petra",
		"wallet notification",
		"wallet - prompt",
		" - prompt",
		"钱包",
	}
	for _, keyword := range keywords {
		if strings.Contains(title, keyword) {
			return true
		}
	}
	// Keep generic "wallet" as a weak match; it is only used with the original
	// browser PID guard, so normal web pages in other browsers are not affected.
	return strings.Contains(title, "wallet")
}

func mayBeGenericExtensionPopupWindow(title, className string) bool {
	t := strings.TrimSpace(strings.ToLower(title))
	c := strings.TrimSpace(strings.ToLower(className))
	if isAuxiliaryIMEWindowTitleOrClass(t, c) {
		return false
	}
	if c != "" && !strings.Contains(c, "chrome_widgetwin") {
		return false
	}
	if t == "" {
		return c != ""
	}
	if looksLikeMainBrowserWindowTitle(t) {
		return false
	}
	if looksLikeServiceWorkerDevToolsTitle(t) {
		return false
	}
	return true
}

func shouldRestoreGenericExtensionPopupWindow(title, className string, rect winRect) bool {
	if !mayBeGenericExtensionPopupWindow(title, className) {
		return false
	}
	t := strings.TrimSpace(strings.ToLower(title))
	if t == "" {
		w := int(rect.Right - rect.Left)
		h := int(rect.Bottom - rect.Top)
		if w <= 0 || h <= 0 {
			return false
		}
		if !isOffscreenWindowRect(rect) {
			return false
		}
		return w <= 900 && h <= 900
	}
	return isOffscreenWindowRect(rect)
}

func shouldHandleHiddenExtensionPopupWindow(title, className string, rect winRect) bool {
	_ = title
	_ = className
	_ = rect
	// Hidden extension/login windows must stay hidden. Surfacing them with
	// ShowWindow causes startup-created wallet unlock/login prompts to appear every
	// time an instance launches. We now only repair windows that Chromium already
	// chose to show to the user.
	return false
}

func shouldHandleExtensionPopupWindowOwner(windowPID uint32, pidSet map[uint32]bool, title string) bool {
	_ = title
	if pidSet[windowPID] {
		return true
	}
	globalRestoreRootMu.Lock()
	root := globalRestoreRoot
	globalRestoreRootMu.Unlock()
	return processBelongsToBoostBrowserRuntime(windowPID, root)
}

func needsGenericExtensionPopupResize(title, className string, width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	if !mayBeGenericExtensionPopupWindow(title, className) {
		return false
	}
	t := strings.TrimSpace(strings.ToLower(title))
	if t == "" {
		return false
	}
	if looksLikeMainBrowserWindowTitle(t) || looksLikeServiceWorkerDevToolsTitle(t) {
		return false
	}
	if looksLikeWalletExtensionPopup(t) || isStrongExtensionPopupTitle(t) {
		return false
	}
	if width >= 320 && width <= 460 && height >= 450 && height <= 720 {
		return false
	}
	return width >= 700 || height >= 760
}

func isStrongExtensionPopupTitle(title string) bool {
	if title == "" {
		return false
	}

	// Wallet web pages often include the wallet brand in their tab title, e.g.
	// "Web3 入口，一个就够 - OKX Wallet" or
	// "The crypto wallet for DeFi... | MetaMask". Treat a brand-only title as a
	// popup, or require explicit popup cue words. Do not clamp every title that
	// merely contains a wallet brand.
	if isExactKnownWalletPopupTitle(title) {
		return true
	}
	return hasExtensionPopupCue(title)
}

func isExactKnownWalletPopupTitle(title string) bool {
	t := strings.TrimSpace(strings.ToLower(title))
	knownTitles := []string{
		"okx wallet",
		"metamask",
		"rabby",
		"rabby wallet",
		"phantom",
		"phantom wallet",
		"bitget wallet",
		"keplr",
		"keplr wallet",
		"petra",
		"petra wallet",
	}
	for _, known := range knownTitles {
		if t == known {
			return true
		}
	}
	return false
}

func hasExtensionPopupCue(title string) bool {
	t := strings.TrimSpace(strings.ToLower(title))
	cuePhrases := []string{
		"notification",
		"prompt",
		"login request",
		"sign in request",
		"connect request",
		"signature request",
		"登录请求",
		"登录授权",
		"sign request",
		"confirm transaction",
		"transaction request",
		"approve",
		"查看权限",
		"连接请求",
		"签名请求",
		"确认交易",
		"授权",
		"request",
		"permission",
		"allow",
	}
	for _, cue := range cuePhrases {
		if strings.Contains(t, cue) {
			return true
		}
	}
	return false
}

func needsExtensionPopupResize(title string, width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}

	// Main browser windows carry the product suffix (for example
	// "OKX Wallet - Boost Browser") when the active tab title is a wallet page.
	// Never clamp those: otherwise opening a wallet web page shrinks the real
	// browser window instead of only fixing an extension popup.
	if looksLikeMainBrowserWindowTitle(title) && !isKnownWalletPopupProductTitle(title) && !isStrongExtensionPopupTitle(title) {
		return false
	}

	// Normal Chrome extension wallet popups are around 360-420 x 560-680.
	if width >= 320 && width <= 460 && height >= 450 && height <= 720 {
		return false
	}

	// Some wallet extension popups are exposed by Chromium as a Chrome_WidgetWin_1
	// top-level window with the product suffix (for example
	// "OKX Wallet - Boost Browser"). These must still be treated as wallet popups;
	// otherwise connect/sign requests can stay offscreen or fullscreen.
	if isKnownWalletPopupProductTitle(title) {
		return true
	}

	// Strong extension prompt/notification titles (e.g. "Petra - Prompt",
	// "MetaMask Notification") can be created oversized and should be clamped.
	if isStrongExtensionPopupTitle(title) {
		return true
	}

	// Do not resize generic wallet-looking titles by shape alone: normal wallet
	// web pages can be opened in narrower browser windows. If the title is not a
	// known popup title and has no prompt/notification cue, leave it untouched.
	return false
}

func looksLikeMainBrowserWindowTitle(title string) bool {
	t := strings.TrimSpace(strings.ToLower(title))
	if t == "" {
		return false
	}
	return strings.Contains(t, " - boost browser") ||
		strings.HasSuffix(t, "boost browser") ||
		strings.Contains(t, " - chromium") ||
		strings.HasSuffix(t, "chromium") ||
		strings.Contains(t, " - google chrome") ||
		strings.HasSuffix(t, "google chrome") ||
		strings.Contains(t, " - chrome") ||
		strings.HasSuffix(t, "chrome")
}

func isKnownWalletPopupProductTitle(title string) bool {
	t := strings.TrimSpace(strings.ToLower(title))
	knownProductTitles := []string{
		"okx wallet - boost browser",
		"petra - boost browser",
		"petra wallet - boost browser",
		"metamask - boost browser",
		"metamask notification - boost browser",
		"phantom - boost browser",
		"phantom wallet - boost browser",
		"rabby - boost browser",
		"rabby wallet - boost browser",
		"bitget wallet - boost browser",
		"keplr - boost browser",
		"keplr wallet - boost browser",
	}
	for _, known := range knownProductTitles {
		if t == known {
			return true
		}
	}
	return false
}

var (
	restoreDevToolsCbOnce   sync.Once
	restoreDevToolsCb       uintptr
	restoreDevToolsPidSet   map[uint32]bool
	restoreDevToolsPidSetMu sync.Mutex
)

func restoreOffscreenServiceWorkerDevToolsWindows(pid int) {
	if pid <= 0 {
		return
	}
	restoreDevToolsPidSetMu.Lock()
	restoreDevToolsPidSet = collectProcessTreePIDs(pid)
	restoreDevToolsPidSetMu.Unlock()

	restoreDevToolsCbOnce.Do(func() {
		restoreDevToolsCb = windows.NewCallback(func(hwnd windows.HWND, lParam uintptr) uintptr {
			var windowPID uint32
			procGetWindowThreadProcessID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&windowPID)))
			restoreDevToolsPidSetMu.Lock()
			ps := restoreDevToolsPidSet
			restoreDevToolsPidSetMu.Unlock()
			if !ps[windowPID] {
				return 1
			}
			restoreServiceWorkerDevToolsWindowIfNeeded(hwnd)
			return 1
		})
	})
	procEnumWindows.Call(restoreDevToolsCb, 0)
}

var (
	globalRestoreCbOnce sync.Once
	globalRestoreCb     uintptr
	globalRestoreRoot   string
	globalRestoreRootMu sync.Mutex
)

func restoreOffscreenServiceWorkerDevToolsWindowsForAppRoot(appRoot string) {
	globalRestoreRootMu.Lock()
	globalRestoreRoot = appRoot
	globalRestoreRootMu.Unlock()

	globalRestoreCbOnce.Do(func() {
		globalRestoreCb = windows.NewCallback(func(hwnd windows.HWND, lParam uintptr) uintptr {
			var windowPID uint32
			procGetWindowThreadProcessID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&windowPID)))
			globalRestoreRootMu.Lock()
			root := globalRestoreRoot
			globalRestoreRootMu.Unlock()
			if !processBelongsToBoostBrowserRuntime(windowPID, root) {
				return 1
			}
			restoreServiceWorkerDevToolsWindowIfNeeded(hwnd)
			return 1
		})
	})
	procEnumWindows.Call(globalRestoreCb, 0)
}

func restoreServiceWorkerDevToolsWindowIfNeeded(hwnd windows.HWND) {
	title := strings.ToLower(strings.TrimSpace(getWindowTitle(hwnd)))
	if !looksLikeServiceWorkerDevToolsTitle(title) {
		return
	}
	className := strings.ToLower(strings.TrimSpace(getWindowClassName(hwnd)))
	if className != "" && !strings.Contains(className, "chrome_widgetwin") {
		return
	}

	rect, ok := getTopLevelWindowRect(hwnd)
	if !ok {
		return
	}
	if x, y, w, h, shouldMove := startupBrowserWindowPlacement(rect); shouldMove {
		procShowWindow.Call(uintptr(hwnd), swRestore)
		procSetWindowPos.Call(
			uintptr(hwnd),
			0,
			uintptr(x),
			uintptr(y),
			uintptr(w),
			uintptr(h),
			SWP_NOZORDER|SWP_SHOWWINDOW,
		)
		procSetForegroundWindow.Call(uintptr(hwnd))
	}
}

func looksLikeServiceWorkerDevToolsTitle(title string) bool {
	if title == "" {
		return false
	}
	return strings.Contains(title, "service worker") ||
		strings.Contains(title, "devtools") ||
		strings.Contains(title, "developer tools") ||
		strings.Contains(title, "开发者工具")
}

func processBelongsToBoostBrowserRuntime(pid uint32, appRoot string) bool {
	if pid == 0 || appRoot == "" {
		return false
	}
	processPath := normalizeWindowsPath(queryProcessImagePath(pid))
	if processPath == "" {
		return false
	}
	chromeRoot := appRoot + string(filepath.Separator) + "chrome"
	return processPath == appRoot || strings.HasPrefix(processPath, appRoot+string(filepath.Separator)) || strings.HasPrefix(processPath, chromeRoot+string(filepath.Separator))
}

func queryProcessImagePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH*4)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil || size == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

func normalizeWindowsPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return strings.ToLower(filepath.Clean(path))
}

func collectProcessTreePIDs(rootPID int) map[uint32]bool {
	pidSet := map[uint32]bool{}
	if rootPID <= 0 {
		return pidSet
	}
	pidSet[uint32(rootPID)] = true

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return pidSet
	}
	defer windows.CloseHandle(snapshot)

	type procEntry struct {
		pid  uint32
		ppid uint32
	}
	entries := make([]procEntry, 0, 64)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snapshot, &pe); err == nil {
		for {
			entries = append(entries, procEntry{pid: pe.ProcessID, ppid: pe.ParentProcessID})
			if err := windows.Process32Next(snapshot, &pe); err != nil {
				break
			}
		}
	}

	changed := true
	for changed {
		changed = false
		for _, entry := range entries {
			if pidSet[entry.ppid] && !pidSet[entry.pid] {
				pidSet[entry.pid] = true
				changed = true
			}
		}
	}
	return pidSet
}

func getWindowClassName(hwnd windows.HWND) string {
	buf := make([]uint16, 256)
	ret, _, _ := procGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if ret == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func getWindowTitle(hwnd windows.HWND) string {
	length, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
	if length <= 0 {
		return ""
	}
	buf := make([]uint16, int(length)+1)
	procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}

func getTopLevelWindowRect(hwnd windows.HWND) (winRect, bool) {
	var rect winRect
	ret, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
	return rect, ret != 0
}
