//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procPostMessageW    = user32.NewProc("PostMessageW")
	procSendMessageW    = user32.NewProc("SendMessageW")
	procEnumWindows     = user32.NewProc("EnumWindows")
	procGetWindowTextW  = user32.NewProc("GetWindowTextW")
	procGetClassNameW   = user32.NewProc("GetClassNameW")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
	procGetWindowRect   = user32.NewProc("GetWindowRect")
)

const (
	WM_KEYDOWN    = 0x0100
	WM_KEYUP      = 0x0101
	WM_MOUSEWHEEL = 0x020A
	VK_DOWN       = 0x28
	VK_UP         = 0x26
	VK_PRIOR      = 0x21
	VK_NEXT       = 0x22
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

func MAKELONG(lo, hi uint16) uintptr {
	return uintptr(lo) | (uintptr(hi) << 16)
}

func main() {
	type windowInfo struct {
		hwnd  uintptr
		title string
		rect  RECT
	}

	var windows []windowInfo

	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		var buf [256]uint16
		procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
		className := syscall.UTF16ToString(buf[:])
		if className != "Chrome_WidgetWin_1" && className != "Edge_WidgetWin_1" {
			return 1
		}
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
		title := syscall.UTF16ToString(buf[:])
		var rect RECT
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
		w, h := rect.Right-rect.Left, rect.Bottom-rect.Top
		if w > 100 && h > 100 {
			windows = append(windows, windowInfo{hwnd, title, rect})
		}
		return 1
	})

	procEnumWindows.Call(cb, 0)

	if len(windows) < 2 {
		fmt.Printf("Need at least 2 visible Chrome windows, found %d\n", len(windows))
		for i, w := range windows {
			fmt.Printf("  Window %d: hwnd=%#x title=%q\n", i, w.hwnd, w.title)
		}
		return
	}

	fmt.Printf("Found %d windows:\n", len(windows))
	for i, w := range windows {
		fmt.Printf("  [%d] hwnd=%#x title=%q rect=(%d,%d,%d,%d)\n",
			i, w.hwnd, w.title, w.rect.Left, w.rect.Top, w.rect.Right, w.rect.Bottom)
	}

	follower := windows[len(windows)-1] // use last window
	fmt.Printf("\n=== Testing scroll on: %q (hwnd=%#x) ===\n", follower.title, follower.hwnd)
	fmt.Println("Watch this window for scrolling effects.\n")

	// Method 1: VK_DOWN (Arrow Down) - 3 times
	fmt.Println("[1] VK_DOWN x3 (Arrow keys)...")
	for i := 0; i < 3; i++ {
		procPostMessageW.Call(follower.hwnd, WM_KEYDOWN, VK_DOWN, 0)
		procPostMessageW.Call(follower.hwnd, WM_KEYUP, VK_DOWN, 0)
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Println("  Sent. Did it scroll down 3 lines?")
	time.Sleep(3 * time.Second)

	// Method 2: VK_NEXT (Page Down)
	fmt.Println("\n[2] VK_NEXT x1 (PageDown key)...")
	procPostMessageW.Call(follower.hwnd, WM_KEYDOWN, VK_NEXT, 0)
	procPostMessageW.Call(follower.hwnd, WM_KEYUP, VK_NEXT, 0)
	fmt.Println("  Sent. Did it scroll down 1 page?")
	time.Sleep(3 * time.Second)

	// Method 3: VK_UP x3
	fmt.Println("\n[3] VK_UP x3 (Arrow keys)...")
	for i := 0; i < 3; i++ {
		procPostMessageW.Call(follower.hwnd, WM_KEYDOWN, VK_UP, 0)
		procPostMessageW.Call(follower.hwnd, WM_KEYUP, VK_UP, 0)
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Println("  Sent. Did it scroll up 3 lines?")
	time.Sleep(3 * time.Second)

	// Method 4: VK_PRIOR (Page Up)
	fmt.Println("\n[4] VK_PRIOR x1 (PageUp key)...")
	procPostMessageW.Call(follower.hwnd, WM_KEYDOWN, VK_PRIOR, 0)
	procPostMessageW.Call(follower.hwnd, WM_KEYUP, VK_PRIOR, 0)
	fmt.Println("  Sent. Did it scroll up 1 page?")
	time.Sleep(3 * time.Second)

	// Method 5: WM_MOUSEWHEEL via PostMessage
	fmt.Println("\n[5] WM_MOUSEWHEEL delta=-120 via PostMessage...")
	centerX := int16((follower.rect.Left + follower.rect.Right) / 2)
	centerY := int16((follower.rect.Top + follower.rect.Bottom) / 2)
	lparam := MAKELONG(uint16(centerX), uint16(centerY))
	deltaDown := int16(-120) // scroll down
	wparam := MAKELONG(0, uint16(deltaDown))
	procPostMessageW.Call(follower.hwnd, WM_MOUSEWHEEL, wparam, lparam)
	fmt.Printf("  PostMessage(hwnd=%#x, WM_MOUSEWHEEL, wparam=%#x, lparam=%#x)\n", follower.hwnd, wparam, lparam)
	fmt.Println("  Did it scroll down?")
	time.Sleep(3 * time.Second)

	// Method 6: WM_MOUSEWHEEL via SendMessage
	fmt.Println("\n[6] WM_MOUSEWHEEL delta=-120 via SendMessage...")
	procSendMessageW.Call(follower.hwnd, WM_MOUSEWHEEL, wparam, lparam)
	fmt.Println("  Did it scroll down?")
	time.Sleep(3 * time.Second)

	// Method 7: PostMessage to foreground + WM_MOUSEWHEEL
	fmt.Println("\n[7] WM_MOUSEWHEEL delta=+120 (scroll UP) via PostMessage...")
	deltaUp := int16(120) // scroll up
	wparamUp := MAKELONG(0, uint16(deltaUp))
	procPostMessageW.Call(follower.hwnd, WM_MOUSEWHEEL, wparamUp, lparam)
	fmt.Println("  Did it scroll up?")
	time.Sleep(3 * time.Second)

	fmt.Println("\n=== Test complete ===")
	fmt.Println("Tell me which method numbers worked.")
}
