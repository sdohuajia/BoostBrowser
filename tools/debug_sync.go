//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var user32 = syscall.NewLazyDLL("user32.dll")

var (
	pGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	pGetWindowRect       = user32.NewProc("GetWindowRect")
	pGetClassName        = user32.NewProc("GetClassNameW")
	pGetWindowText       = user32.NewProc("GetWindowTextW")
	pGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")
	pEnumWindows         = user32.NewProc("EnumWindows")
	pGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	pIsWindowVisible     = user32.NewProc("IsWindowVisible")
	pPostMessage         = user32.NewProc("PostMessageW")
	pFindWindow          = user32.NewProc("FindWindowW")
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

func getWindowRect(hwnd syscall.Handle) RECT {
	var rect RECT
	pGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
	return rect
}

func main() {
	// Find all Chrome windows
	type WinInfo struct {
		Hwnd  syscall.Handle
		Title string
		Class string
		Pid   uint32
		Rect  RECT
	}

	var windows []WinInfo

	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		var pid uint32
		pGetWindowThreadPID.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))

		visible, _, _ := pIsWindowVisible.Call(uintptr(hwnd))
		if visible == 0 {
			return 1
		}

		titleLen, _, _ := pGetWindowTextLength.Call(uintptr(hwnd))
		if titleLen == 0 {
			return 1
		}

		var buf [512]uint16
		pGetClassName.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), 512)
		className := syscall.UTF16ToString(buf[:])

		pGetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), 512)
		title := syscall.UTF16ToString(buf[:])

		rect := getWindowRect(hwnd)

		windows = append(windows, WinInfo{
			Hwnd:  hwnd,
			Title: title,
			Class: className,
			Pid:   pid,
			Rect:  rect,
		})
		return 1
	})

	pEnumWindows.Call(cb, 0)

	// Find Chrome windows
	fmt.Println("=== 所有可见窗口 ===")
	for i, w := range windows {
		if len(w.Title) > 0 {
			fmt.Printf("[%d] hwnd=%#x pid=%d class=%s title=%-40s rect=(%d,%d,%d,%d) size=%dx%d\n",
				i, w.Hwnd, w.Pid, w.Class, w.Title,
				w.Rect.Left, w.Rect.Top, w.Rect.Right, w.Rect.Bottom,
				w.Rect.Right-w.Rect.Left, w.Rect.Bottom-w.Rect.Top)
		}
	}

	// Test: PostMessage WM_MOUSEMOVE and click to a Chrome window
	fmt.Println("\n=== 测试 PostMessage 到 Chrome 窗口 ===")

	// Find Chrome_WidgetWin_1 windows
	var chromeWins []WinInfo
	for _, w := range windows {
		if w.Class == "Chrome_WidgetWin_1" {
			chromeWins = append(chromeWins, w)
		}
	}

	if len(chromeWins) < 2 {
		fmt.Println("找到少于2个 Chrome 窗口，无法测试")
		os.Exit(1)
	}

	// Use first Chrome window as source, second as target
	master := chromeWins[0]
	follower := chromeWins[1]
	fmt.Printf("主控: hwnd=%#x title=%s rect=(%d,%d,%d,%d)\n",
		master.Hwnd, master.Title, master.Rect.Left, master.Rect.Top, master.Rect.Right, master.Rect.Bottom)
	fmt.Printf("跟随: hwnd=%#x title=%s rect=(%d,%d,%d,%d)\n",
		follower.Hwnd, follower.Title, follower.Rect.Left, follower.Rect.Top, follower.Rect.Right, follower.Rect.Bottom)

	// Calculate center of follower as target
	fw := follower.Rect.Right - follower.Rect.Left
	fh := follower.Rect.Bottom - follower.Rect.Top

	// Calculate what Chrome-Manager would compute for center click
	clientX := int(float64(fw) * 0.5)
	clientY := int(float64(fh) * 0.5)
	lparam := uintptr(uint16(clientX)) | (uintptr(uint16(clientY)) << 16)

	fmt.Printf("\n=== 坐标映射测试 ===\n")
	fmt.Printf("跟随窗口大小: %dx%d\n", fw, fh)
	fmt.Printf("目标点击位置(中心): clientX=%d, clientY=%d\n", clientX, clientY)
	fmt.Printf("lparam = MAKELONG(%d, %d) = %#x\n", clientX, clientY, lparam)

	// Send right-click to follower center (safer than left-click)
	fmt.Println("\n发送 WM_RBUTTONDOWN 到跟随窗口中心...")
	const WM_RBUTTONDOWN = 0x0204
	const WM_RBUTTONUP = 0x0205
	const MK_RBUTTON = 0x0002

	pPostMessage.Call(uintptr(follower.Hwnd), WM_RBUTTONDOWN, MK_RBUTTON, lparam)
	pPostMessage.Call(uintptr(follower.Hwnd), WM_RBUTTONUP, 0, lparam)
	fmt.Println("已发送右键点击！请检查跟随窗口是否弹出了右键菜单。")

	// Now test coordinate mapping from master to follower
	fmt.Println("\n=== 坐标映射可以从主控(0.3, 0.5)映射到跟随窗口 ===\n")
	relX := 0.3
	relY := 0.5
	targetX := int(float64(fw) * relX)
	targetY := int(float64(fh) * relY)
	lparam2 := uintptr(uint16(targetX)) | (uintptr(uint16(targetY)) << 16)
	fmt.Printf("relX=%.2f, relY=%.2f -> targetX=%d, targetY=%d, lparam2=%#x\n", relX, relY, targetX, targetY, lparam2)

	fmt.Println("\n3秒后发送左键点击到跟随窗口 (0.3, 0.5) 位置...")
	fmt.Scanln()
	pPostMessage.Call(uintptr(follower.Hwnd), 0x0201, 0x0001, lparam2) // WM_LBUTTONDOWN
	pPostMessage.Call(uintptr(follower.Hwnd), 0x0202, 0, lparam2)      // WM_LBUTTONUP
	fmt.Println("已发送左键点击！")
}
