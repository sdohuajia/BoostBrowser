//go:build windows

package backend

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ============================================================================
// Win32 常量（共享）
// ============================================================================

const (
	WM_MOUSEMOVE   = 0x0200
	WM_LBUTTONDOWN = 0x0201
	WM_LBUTTONUP   = 0x0202
	WM_RBUTTONDOWN = 0x0204
	WM_RBUTTONUP   = 0x0205
	WM_MBUTTONDOWN = 0x0207
	WM_MBUTTONUP   = 0x0208
	WM_MOUSEWHEEL  = 0x020A
	WM_KEYDOWN     = 0x0100
	WM_KEYUP       = 0x0101
	WM_CHAR        = 0x0102
	WM_SYSKEYDOWN  = 0x0104
	WM_SYSKEYUP    = 0x0105
	MK_LBUTTON     = 0x0001
	MK_RBUTTON     = 0x0002
	MK_MBUTTON     = 0x0010
	MK_NONE        = 0x0000
	SWP_NOZORDER   = 0x0004
	SWP_NOACTIVATE = 0x0010
	SWP_SHOWWINDOW = 0x0040

	// Virtual Key Codes
	VK_CONTROL = 0x11
	VK_SHIFT   = 0x10
	VK_MENU    = 0x12 // Alt
	VK_F1      = 0x70
	VK_F2      = 0x71
	VK_F3      = 0x72
	VK_F4      = 0x73
	VK_F5      = 0x74
	VK_F6      = 0x75
	VK_F7      = 0x76
	VK_F8      = 0x77
	VK_F9      = 0x78
	VK_F10     = 0x79
	VK_F11     = 0x7A
	VK_F12     = 0x7B

	// Arrow keys and navigation keys (used for scroll sync)
	VK_UP     = 0x26
	VK_DOWN   = 0x28
	VK_LEFT   = 0x25
	VK_RIGHT  = 0x27
	VK_PRIOR  = 0x21 // Page Up
	VK_NEXT   = 0x22 // Page Down
	VK_HOME   = 0x24
	VK_END    = 0x23
	VK_INSERT = 0x2D
	VK_DELETE = 0x2E
)

var (
	user32dll = windows.NewLazyDLL("user32.dll")
	gdi32dll  = windows.NewLazyDLL("gdi32.dll")

	procFindWindowW              = user32dll.NewProc("FindWindowW")
	procEnumWindows              = user32dll.NewProc("EnumWindows")
	procGetWindowThreadProcessID = user32dll.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32dll.NewProc("IsWindowVisible")
	procGetWindowTextLengthW     = user32dll.NewProc("GetWindowTextLengthW")
	procGetForegroundWindow      = user32dll.NewProc("GetForegroundWindow")
	procShowWindow               = user32dll.NewProc("ShowWindow")
	procSetForegroundWindow      = user32dll.NewProc("SetForegroundWindow")
	procSendMessageW             = user32dll.NewProc("SendMessageW")
	procPostMessageW             = user32dll.NewProc("PostMessageW")
	procScreenToClient           = user32dll.NewProc("ScreenToClient")
	procSetWindowPos             = user32dll.NewProc("SetWindowPos")
	procGetSystemMetrics         = user32dll.NewProc("GetSystemMetrics")
	procGetDC                    = user32dll.NewProc("GetDC")
	procReleaseDC                = user32dll.NewProc("ReleaseDC")
	procCreateCompatibleDC       = gdi32dll.NewProc("CreateCompatibleDC")
	procDeleteDC                 = gdi32dll.NewProc("DeleteDC")
	procCreateDIBSection         = gdi32dll.NewProc("CreateDIBSection")
	procDeleteObject             = gdi32dll.NewProc("DeleteObject")
	procLoadImageW               = user32dll.NewProc("LoadImageW")
	procDestroyIcon              = user32dll.NewProc("DestroyIcon")
	procIsWindow                 = user32dll.NewProc("IsWindow")
	procDrawIconEx               = user32dll.NewProc("DrawIconEx")
	procGetAsyncKeyState         = user32dll.NewProc("GetAsyncKeyState")
	procSelectObject             = gdi32dll.NewProc("SelectObject")
	procGetClassLongPtrW         = user32dll.NewProc("GetClassLongPtrW")
	procGetWindowTextW           = user32dll.NewProc("GetWindowTextW")
	procGetClassNameW            = user32dll.NewProc("GetClassNameW")
	procGetClientRect            = user32dll.NewProc("GetClientRect")
	procClientToScreen           = user32dll.NewProc("ClientToScreen")
	procMapVirtualKeyW           = user32dll.NewProc("MapVirtualKeyW")
)

// MAKELONG 模拟 Win32 MAKELONG 宏
func MAKELONG(lo, hi uint16) uintptr {
	return uintptr(lo) | (uintptr(hi) << 16)
}

// makeKeyLParam 生成 WM_KEYDOWN/WM_KEYUP 的 lParam
// https://learn.microsoft.com/en-us/windows/win32/inputdev/wm-keydown
// lParam 格式:
//
//	Bits 0-15:  Repeat count (keydown 为 1, keyup 为 0 不重要)
//	Bits 16-23: Scan code (MapVirtualKey 获取)
//	Bit 24:     Extended key flag (方向键、Page Up/Down 等为 1)
//	Bits 25-28: Reserved
//	Bit 29:     Context code (Alt 按下时为 1)
//	Bit 30:     Previous key state (keydown 为 0, keyup 为 1)
//	Bit 31:     Transition state (keydown 为 0, keyup 为 1)
func makeKeyLParam(vk uint32, isKeyDown bool) uintptr {
	scanCode, _, _ := procMapVirtualKeyW.Call(uintptr(vk), 0) // MAPVK_VK_TO_VSC = 0

	var extended uint32
	// Extended keys: 方向键、Page Up/Down、Insert/Delete、Home/End 等
	switch vk {
	case 0x25, 0x26, 0x27, 0x28, // VK_LEFT, VK_UP, VK_RIGHT, VK_DOWN
		0x21, 0x22, // VK_PRIOR, VK_NEXT (Page Up, Page Down)
		0x24, 0x23, // VK_HOME, VK_END
		0x2D, 0x2E, // VK_INSERT, VK_DELETE
		0x11: // VK_CONTROL
		extended = 1
	}

	repeatCount := uint32(1)
	previousState := uint32(0)
	transitionState := uint32(0)
	if !isKeyDown {
		previousState = 1
		transitionState = 1
		repeatCount = 0 // keyup 不重要
	}

	lparam := uintptr(repeatCount & 0xFFFF)    // Bits 0-15
	lparam |= uintptr((scanCode & 0xFF) << 16) // Bits 16-23
	lparam |= uintptr(extended << 24)          // Bit 24
	lparam |= uintptr(previousState << 30)     // Bit 30
	lparam |= uintptr(transitionState << 31)   // Bit 31

	return lparam
}

func isWindow(hwnd windows.HWND) bool {
	ret, _, _ := procIsWindow.Call(uintptr(hwnd))
	return ret != 0
}

func isWindowVisible(hwnd windows.HWND) bool {
	ret, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
	return ret != 0
}

func isAuxiliaryIMEWindowTitleOrClass(title, className string) bool {
	lowerTitle := strings.ToLower(strings.TrimSpace(title))
	lowerClass := strings.ToLower(strings.TrimSpace(className))
	return strings.Contains(lowerTitle, "default ime") ||
		strings.Contains(lowerTitle, "ime") ||
		strings.Contains(lowerClass, "ime")
}

func screenToClient(hwnd windows.HWND, x, y int) (int, int) {
	type POINT struct{ X, Y int32 }
	pt := POINT{X: int32(x), Y: int32(y)}
	procScreenToClient.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pt)))
	return int(pt.X), int(pt.Y)
}

func getClientSize(hwnd windows.HWND) (int, int, bool) {
	type RECT struct{ Left, Top, Right, Bottom int32 }
	var rect RECT
	ret, _, _ := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
	if ret == 0 {
		return 0, 0, false
	}
	return int(rect.Right - rect.Left), int(rect.Bottom - rect.Top), true
}

// findProcessWindow 通过进程 PID 找到该进程的主浏览器窗口句柄。
// Windows/Chrome 有时会给同一 PID 暴露 "Default IME" 等辅助顶级窗口；
// 如果直接按标题最长选择，会给这些 IME 辅助窗口设置任务栏 badge，导致 Alt+Tab/任务栏
// 出现“不应该有”的 Default IME 缩略图。因此这里优先选择 Chrome_WidgetWin_* 主窗口，
// 并显式排除 IME/输入法辅助窗口。
func findProcessWindow(pid int) (windows.HWND, error) {
	type winCandidate struct {
		hwnd     windows.HWND
		title    string
		class    string
		titleLen int
		score    int
	}
	var candidates []winCandidate

	cb := windows.NewCallback(func(hwnd windows.HWND, lParam uintptr) uintptr {
		var windowPID uint32
		procGetWindowThreadProcessID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&windowPID)))
		if int(windowPID) != pid {
			return 1 // 继续
		}

		visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
		if visible == 0 {
			return 1
		}

		title := getWindowTitle(hwnd)
		className := getWindowClassName(hwnd)
		if isAuxiliaryIMEWindowTitleOrClass(title, className) {
			return 1
		}
		if title == "" && !strings.HasPrefix(className, "Chrome_WidgetWin_") {
			return 1
		}

		score := len(title)
		if strings.HasPrefix(className, "Chrome_WidgetWin_") {
			score += 10000
			if title == "" {
				// 新版浏览器窗口有时会在启动后一段时间内保持空标题，但仍然是可见的主窗口。
				// 允许这类 Chrome 顶层窗口参与候选，避免同步页误判为“无窗口”。
				score += 50
			}
		}
		if strings.EqualFold(className, "Chrome_WidgetWin_0") {
			// Chrome_WidgetWin_0 更常见于辅助/隐藏窗口，仍可兜底但不要优先。
			score -= 100
		}
		candidates = append(candidates, winCandidate{hwnd: hwnd, title: title, class: className, titleLen: len(title), score: score})
		return 1 // 继续找
	})

	procEnumWindows.Call(cb, 0)

	if len(candidates) == 0 {
		return 0, fmt.Errorf("未找到 PID=%d 的浏览器主窗口", pid)
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}
	return best.hwnd, nil
}
