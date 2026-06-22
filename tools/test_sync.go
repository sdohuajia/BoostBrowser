//go:build ignore
// +build ignore

// Test tool: Simulate exact InputSyncer coordinate mapping
// 1. Find all Chrome windows and their render children
// 2. Calculate coordinates exactly as InputSyncer does
// 3. Send test clicks to verify mapping
// 4. Compare PostMessage to RenderChild vs TopLevel window

package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var user32 = syscall.NewLazyDLL("user32.dll")

var (
	procEnumWindows      = user32.NewProc("EnumWindows")
	procGetWindowRect    = user32.NewProc("GetWindowRect")
	procGetClassNameW    = user32.NewProc("GetClassNameW")
	procGetWindowTextW   = user32.NewProc("GetWindowTextW")
	procEnumChildWindows = user32.NewProc("EnumChildWindows")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
	procScreenToClient   = user32.NewProc("ScreenToClient")
	procClientToScreen   = user32.NewProc("ClientToScreen")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procSetCursorPos     = user32.NewProc("SetCursorPos")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procSendMessageW     = user32.NewProc("SendMessageW")
)

type RECT struct{ Left, Top, Right, Bottom int32 }
type POINT struct{ X, Y int32 }

func getClassName(hwnd syscall.Handle) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf[:n])
}

func getWindowText(hwnd syscall.Handle) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), 512)
	return syscall.UTF16ToString(buf[:n])
}

func getWindowRect(hwnd syscall.Handle) RECT {
	var rect RECT
	procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
	return rect
}

func screenToClient(hwnd syscall.Handle, x, y int32) POINT {
	pt := POINT{X: x, Y: y}
	procScreenToClient.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pt)))
	return pt
}

func clientToScreen(hwnd syscall.Handle, x, y int32) POINT {
	pt := POINT{X: x, Y: y}
	procClientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pt)))
	return pt
}

func isWindowVisible(hwnd syscall.Handle) bool {
	ret, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
	return ret != 0
}

func findRenderChild(parent syscall.Handle) syscall.Handle {
	var found syscall.Handle
	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		className := getClassName(hwnd)
		if className == "Chrome_RenderWidgetHostHWND" && isWindowVisible(hwnd) {
			r := getWindowRect(hwnd)
			w := r.Right - r.Left
			h := r.Bottom - r.Top
			if w > 50 && h > 50 && found == 0 {
				found = hwnd
			}
		}
		return 1
	})
	procEnumChildWindows.Call(uintptr(parent), cb, 0)
	if found == 0 {
		return parent // fallback
	}
	return found
}

func MAKELONG(lo, hi uint16) uintptr {
	return uintptr(lo) | (uintptr(hi) << 16)
}

func main() {
	type WinInfo struct {
		Hwnd       syscall.Handle
		Title      string
		WindowRect RECT
		RenderHwnd syscall.Handle
		RenderRect RECT
	}

	var wins []WinInfo

	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		className := getClassName(hwnd)
		if className == "Chrome_WidgetWin_1" && isWindowVisible(hwnd) {
			title := getWindowText(hwnd)
			renderHwnd := findRenderChild(hwnd)
			renderRect := getWindowRect(renderHwnd)
			wins = append(wins, WinInfo{
				Hwnd:       hwnd,
				Title:      title,
				WindowRect: getWindowRect(hwnd),
				RenderHwnd: renderHwnd,
				RenderRect: renderRect,
			})
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)

	fmt.Printf("\nFound %d Chrome windows:\n", len(wins))
	for i, w := range wins {
		fmt.Printf("[%d] %#x: %q\n", i, w.Hwnd, w.Title)
		fmt.Printf("     Window: (%d,%d,%d,%d) size=%dx%d\n",
			w.WindowRect.Left, w.WindowRect.Top, w.WindowRect.Right, w.WindowRect.Bottom,
			w.WindowRect.Right-w.WindowRect.Left, w.WindowRect.Bottom-w.WindowRect.Top)
		fmt.Printf("     Render: %#x (%s parent)\n", w.RenderHwnd, map[bool]string{true: "same as", false: "different from"}[w.RenderHwnd == w.Hwnd])
		fmt.Printf("     RenderRect: (%d,%d,%d,%d) size=%dx%d\n",
			w.RenderRect.Left, w.RenderRect.Top, w.RenderRect.Right, w.RenderRect.Bottom,
			w.RenderRect.Right-w.RenderRect.Left, w.RenderRect.Bottom-w.RenderRect.Top)
		ct := clientToScreen(w.RenderHwnd, 0, 0)
		fmt.Printf("     ClientToScreen(0,0) = (%d,%d)\n", ct.X, ct.Y)
	}

	if len(wins) < 2 {
		fmt.Println("ERROR: Need at least 2 windows!")
		return
	}

	// Use windows[0] as master, windows[1] as follower
	master := wins[0]
	follower := wins[1]

	fmt.Printf("\n=== Using Master=[%d] Follower=[%d] ===\n", 0, 1)

	mR := master.RenderRect
	fR := follower.RenderRect
	mW := mR.Right - mR.Left
	mH := mR.Bottom - mR.Top
	fW := fR.Right - fR.Left
	fH := fR.Bottom - fR.Top

	fmt.Printf("Master render: (%d,%d,%d,%d) size=%dx%d\n", mR.Left, mR.Top, mR.Right, mR.Bottom, mW, mH)
	fmt.Printf("Follower render: (%d,%d,%d,%d) size=%dx%d\n", fR.Left, fR.Top, fR.Right, fR.Bottom, fW, fH)

	// Test: simulate the exact mapCoordsPython calculation
	// Pick a test point in the master render area
	testPoints := []struct {
		name   string
		sx, sy int32
	}{
		{"top-left+10", mR.Left + 10, mR.Top + 10},
		{"center", (mR.Left + mR.Right) / 2, (mR.Top + mR.Bottom) / 2},
		{"quarter", mR.Left + mW/4, mR.Top + mH/4},
		{"middle-bottom", mR.Left + mW/2, mR.Top + mH*3/4},
	}

	fmt.Println("\n=== mapCoordsPython simulation ===")
	for _, tp := range testPoints {
		relX := float64(int(tp.sx)-int(mR.Left)) / float64(mW)
		relY := float64(int(tp.sy)-int(mR.Top)) / float64(mH)
		targetScreenX := int32(fR.Left) + int32(float64(fW)*relX)
		targetScreenY := int32(fR.Top) + int32(float64(fH)*relY)
		clientPt := screenToClient(follower.RenderHwnd, targetScreenX, targetScreenY)

		fmt.Printf("  %s: screen(%d,%d) -> rel(%.4f,%.4f) -> targetScreen(%d,%d) -> client(%d,%d)\n",
			tp.name, tp.sx, tp.sy, relX, relY, targetScreenX, targetScreenY, clientPt.X, clientPt.Y)
	}

	// Now test PostMessage to render child
	fmt.Println("\n=== Sending test click to follower render child ===")
	fmt.Println("Moving cursor to follower render area center, then right-clicking...")

	// Calculate center of follower render area
	centerScreenX := (fR.Left + fR.Right) / 2
	centerScreenY := (fR.Top + fR.Bottom) / 2
	centerClient := screenToClient(follower.RenderHwnd, centerScreenX, centerScreenY)

	fmt.Printf("  Center: screen(%d,%d) -> client(%d,%d)\n", centerScreenX, centerScreenY, centerClient.X, centerClient.Y)

	lparam := MAKELONG(uint16(centerClient.X), uint16(centerClient.Y))

	// Save cursor pos
	var origPos POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&origPos)))

	// Move cursor to the target position
	procSetCursorPos.Call(uintptr(centerScreenX), uintptr(centerScreenY))

	// Send WM_MOUSEMOVE then WM_RBUTTONDOWN/UP to render child
	procPostMessageW.Call(uintptr(follower.RenderHwnd), 0x0200, 0, lparam) // WM_MOUSEMOVE
	procPostMessageW.Call(uintptr(follower.RenderHwnd), 0x0204, 0, lparam) // WM_RBUTTONDOWN
	procPostMessageW.Call(uintptr(follower.RenderHwnd), 0x0205, 0, 0)      // WM_RBUTTONUP

	fmt.Println("  Sent to RenderChild. Check if context menu appeared at CENTER of follower window.")

	// Wait for user to see result
	fmt.Println("\n  Waiting 3 seconds...")
	time.Sleep(3 * time.Second)

	// Restore cursor
	procSetCursorPos.Call(uintptr(origPos.X), uintptr(origPos.Y))

	// Now test sending to top-level window with coordinates adjusted for tab bar offset
	fmt.Println("\n=== Sending test click to follower TOP-LEVEL window ===")
	fmt.Println("Using client coordinates relative to top-level window...")

	// Calculate client coordinates relative to top-level window
	topClient := screenToClient(follower.Hwnd, centerScreenX, centerScreenY)
	fmt.Printf("  Top-level client: screen(%d,%d) -> client(%d,%d)\n", centerScreenX, centerScreenY, topClient.X, topClient.Y)

	topLparam := MAKELONG(uint16(topClient.X), uint16(topClient.Y))

	procSetCursorPos.Call(uintptr(centerScreenX), uintptr(centerScreenY))
	procPostMessageW.Call(uintptr(follower.Hwnd), 0x0200, 0, topLparam) // WM_MOUSEMOVE
	procPostMessageW.Call(uintptr(follower.Hwnd), 0x0204, 0, topLparam) // WM_RBUTTONDOWN
	procPostMessageW.Call(uintptr(follower.Hwnd), 0x0205, 0, 0)         // WM_RBUTTONUP

	fmt.Println("  Sent to top-level. Check if context menu appeared at CENTER of follower window.")

	// Wait for user to see result
	fmt.Println("\n  Waiting 3 seconds...")
	time.Sleep(3 * time.Second)
	procSetCursorPos.Call(uintptr(origPos.X), uintptr(origPos.Y))

	// Test scroll via PostMessage
	fmt.Println("\n=== Testing WM_MOUSEWHEEL to follower ===")

	// WM_MOUSEWHEEL: wParam = MAKELONG(flags, delta), lParam = MAKELONG(xScreen, yScreen)
	delta := int16(120) // one scroll unit up
	wparam := MAKELONG(0, uint16(delta))

	// Method 1: Send to follower top-level with screen coordinates in lParam
	lparam_wheel1 := uintptr(int16(centerScreenX))&0xFFFF | (uintptr(int16(centerScreenY))&0xFFFF)<<16
	procPostMessageW.Call(uintptr(follower.Hwnd), 0x020A, wparam, lparam_wheel1) // WM_MOUSEWHEEL
	fmt.Printf("  Method 1: PostMessage(topLevel, WM_MOUSEWHEEL, wparam=%#x, lparam=%#x) screen(%d,%d)\n", wparam, lparam_wheel1, centerScreenX, centerScreenY)

	fmt.Println("\n  Check if scroll happened in follower window.")
	time.Sleep(2 * time.Second)

	// Method 2: Send to render child
	lparam_wheel2 := uintptr(int16(centerScreenX))&0xFFFF | (uintptr(int16(centerScreenY))&0xFFFF)<<16
	procPostMessageW.Call(uintptr(follower.RenderHwnd), 0x020A, wparam, lparam_wheel2)
	fmt.Printf("  Method 2: PostMessage(renderChild, WM_MOUSEWHEEL, wparam=%#x, lparam=%#x) screen(%d,%d)\n", wparam, lparam_wheel2, centerScreenX, centerScreenY)

	fmt.Println("\n  Check if scroll happened in follower window.")
	time.Sleep(2 * time.Second)

	fmt.Println("\n=== All tests done ===")
}
