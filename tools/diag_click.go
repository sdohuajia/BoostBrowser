//go:build ignore
// +build ignore

// Diagnostic tool: Simulate the exact coordinate mapping that InputSyncer does
// and test sending a click to verify that PostMessage to Chrome_RenderWidgetHostHWND works

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var user32 = syscall.NewLazyDLL("user32.dll")

var (
	procEnumWindows         = user32.NewProc("EnumWindows")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procGetClassNameW       = user32.NewProc("GetClassNameW")
	procGetWindowTextW      = user32.NewProc("GetWindowTextW")
	procEnumChildWindows    = user32.NewProc("EnumChildWindows")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procClientToScreen      = user32.NewProc("ClientToScreen")
	procScreenToClient      = user32.NewProc("ScreenToClient")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetCursorPos        = user32.NewProc("SetCursorPos")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

type POINT struct {
	X, Y int32
}

type WindowInfo struct {
	Hwnd       syscall.Handle
	ClassName  string
	Title      string
	WindowRect RECT
	RenderHwnd syscall.Handle
	RenderRect RECT
	ChildCount int
}

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

func getClientRect(hwnd syscall.Handle) RECT {
	var rect RECT
	procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
	return rect
}

func clientToScreen(hwnd syscall.Handle, x, y int32) POINT {
	pt := POINT{X: x, Y: y}
	procClientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pt)))
	return pt
}

func screenToClient(hwnd syscall.Handle, x, y int32) POINT {
	pt := POINT{X: x, Y: y}
	procScreenToClient.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pt)))
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
		if className == "Chrome_RenderWidgetHostHWND" {
			r := getWindowRect(hwnd)
			w := r.Right - r.Left
			h := r.Bottom - r.Top
			vis := isWindowVisible(hwnd)
			if vis && w > 50 && h > 50 && found == 0 {
				found = hwnd
			}
		}
		return 1
	})
	procEnumChildWindows.Call(uintptr(parent), cb, 0)
	if found == 0 {
		return parent
	}
	return found
}

// Enumerate ALL child windows to see the full hierarchy
func dumpChildWindows(parent syscall.Handle, indent string) {
	procEnumChildWindows.Call(uintptr(parent), syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		className := getClassName(hwnd)
		r := getWindowRect(hwnd)
		vis := isWindowVisible(hwnd)
		fmt.Printf("%s  child=%#x class=%q vis=%v rect=(%d,%d,%d,%d) size=%dx%d\n",
			indent, hwnd, className, vis, r.Left, r.Top, r.Right, r.Bottom, r.Right-r.Left, r.Bottom-r.Top)
		return 1
	}), 0)
}

func MAKELONG(lo, hi uint16) uintptr {
	return uintptr(lo) | (uintptr(hi) << 16)
}

func main() {
	f, _ := os.Create(`C:\diag_click.txt`)
	defer f.Close()

	write := func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		fmt.Print(msg)
		f.WriteString(msg)
	}

	// Find all visible Chrome_WidgetWin_1 windows
	var windows []WindowInfo

	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		className := getClassName(hwnd)
		if !isWindowVisible(hwnd) {
			return 1
		}
		if className == "Chrome_WidgetWin_1" {
			title := getWindowText(hwnd)
			rect := getWindowRect(hwnd)
			renderHwnd := findRenderChild(hwnd)
			renderRect := getWindowRect(renderHwnd)

			wi := WindowInfo{
				Hwnd:       hwnd,
				ClassName:  className,
				Title:      title,
				WindowRect: rect,
				RenderHwnd: renderHwnd,
				RenderRect: renderRect,
			}
			windows = append(windows, wi)
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)

	if len(windows) < 2 {
		write("ERROR: Need at least 2 Chrome windows, found %d\n", len(windows))
		return
	}

	// Use first two Chromium windows as master and follower
	master := windows[0]
	follower := windows[1]

	write("=== Master Window ===\n")
	write("HWND: %#x Title: %q\n", master.Hwnd, master.Title)
	write("WindowRect: (%d,%d,%d,%d) size=%dx%d\n",
		master.WindowRect.Left, master.WindowRect.Top, master.WindowRect.Right, master.WindowRect.Bottom,
		master.WindowRect.Right-master.WindowRect.Left, master.WindowRect.Bottom-master.WindowRect.Top)
	write("RenderHWND: %#x (same as parent: %v)\n", master.RenderHwnd, master.RenderHwnd == master.Hwnd)
	write("RenderRect: (%d,%d,%d,%d) size=%dx%d\n",
		master.RenderRect.Left, master.RenderRect.Top, master.RenderRect.Right, master.RenderRect.Bottom,
		master.RenderRect.Right-master.RenderRect.Left, master.RenderRect.Bottom-master.RenderRect.Top)

	// Dump all children of master
	write("Master child windows:\n")
	dumpChildWindows(master.Hwnd, "  ")

	write("\n=== Follower Window ===\n")
	write("HWND: %#x Title: %q\n", follower.Hwnd, follower.Title)
	write("WindowRect: (%d,%d,%d,%d) size=%dx%d\n",
		follower.WindowRect.Left, follower.WindowRect.Top, follower.WindowRect.Right, follower.WindowRect.Bottom,
		follower.WindowRect.Right-follower.WindowRect.Left, follower.WindowRect.Bottom-follower.WindowRect.Top)
	write("RenderHWND: %#x (same as parent: %v)\n", follower.RenderHwnd, follower.RenderHwnd == follower.Hwnd)
	write("RenderRect: (%d,%d,%d,%d) size=%dx%d\n",
		follower.RenderRect.Left, follower.RenderRect.Top, follower.RenderRect.Right, follower.RenderRect.Bottom,
		follower.RenderRect.Right-follower.RenderRect.Left, follower.RenderRect.Bottom-follower.RenderRect.Top)

	// Dump all children of follower
	write("Follower child windows:\n")
	dumpChildWindows(follower.Hwnd, "  ")

	// Check if render child is same as parent (missing render child)
	if master.RenderHwnd == master.Hwnd {
		write("\n*** WARNING: Master has NO Chrome_RenderWidgetHostHWND child! Using parent instead. ***\n")
		write("*** This means clicks will be offset by the tab bar / address bar height! ***\n")
	}
	if follower.RenderHwnd == follower.Hwnd {
		write("\n*** WARNING: Follower has NO Chrome_RenderWidgetHostHWND child! Using parent instead. ***\n")
	}

	// Test mapping: simulate clicks at various positions in master render area
	write("\n=== Coordinate Mapping Tests ===\n")

	mR := master.RenderRect
	fR := follower.RenderRect
	mW := mR.Right - mR.Left
	mH := mR.Bottom - mR.Top
	fW := fR.Right - fR.Left
	fH := fR.Bottom - fR.Top

	// Test points: top-left, center, bottom-right of master render area
	testPoints := []struct {
		name   string
		sx, sy int32
	}{
		{"top-left", mR.Left + 10, mR.Top + 10},
		{"center", (mR.Left + mR.Right) / 2, (mR.Top + mR.Bottom) / 2},
		{"bottom-right", mR.Right - 10, mR.Bottom - 10},
		// Click at a known position in the content area
		{"1/4 from left-top", mR.Left + mW/4, mR.Top + mH/4},
	}

	for _, tp := range testPoints {
		relX := float64(tp.sx-mR.Left) / float64(mW)
		relY := float64(tp.sy-mR.Top) / float64(mH)
		targetSX := int32(fR.Left) + int32(float64(fW)*relX)
		targetSY := int32(fR.Top) + int32(float64(fH)*relY)
		clientPt := screenToClient(follower.RenderHwnd, targetSX, targetSY)

		write("  %s: screen(%d,%d) rel(%.3f,%.3f) -> targetScreen(%d,%d) -> client(%d,%d)\n",
			tp.name, tp.sx, tp.sy, relX, relY, targetSX, targetSY, clientPt.X, clientPt.Y)
	}

	// Now test with PostMessage: send a right-click at the center of the master render area
	// and see if it appears at the correct position in the follower
	write("\n=== PostMessage Click Test ===\n")
	write("Sending WM_RBUTTONDOWN at center of follower render area...\n")

	centerX := int32(fR.Left+fR.Right) / 2
	centerY := int32(fR.Top+fR.Bottom) / 2
	clientPt := screenToClient(follower.RenderHwnd, centerX, centerY)

	write("  Target: screen(%d,%d) -> client(%d,%d)\n", centerX, centerY, clientPt.X, clientPt.Y)

	lparam := MAKELONG(uint16(clientPt.X), uint16(clientPt.Y))
	write("  MAKELONG(%d, %d) = %#x\n", clientPt.X, clientPt.Y, lparam)

	// First send WM_MOUSEMOVE
	procPostMessageW.Call(uintptr(follower.RenderHwnd), 0x0200, 0, lparam) // WM_MOUSEMOVE
	// Then send right-click
	procPostMessageW.Call(uintptr(follower.RenderHwnd), 0x0204, 0, lparam) // WM_RBUTTONDOWN
	procPostMessageW.Call(uintptr(follower.RenderHwnd), 0x0205, 0, lparam) // WM_RBUTTONUP

	write("  Sent WM_RBUTTONDOWN + WM_RBUTTONUP to follower render child %#x\n", follower.RenderHwnd)
	write("  Check if right-click context menu appeared at the center of the follower window!\n")

	// Also test sending to the top-level window
	write("\n=== PostMessage to Top-Level Window Test ===\n")
	topCenterX := int32(fR.Left+fR.Right) / 2
	topCenterY := int32(fR.Top+fR.Bottom) / 2
	topClientPt := screenToClient(follower.Hwnd, topCenterX, topCenterY)

	write("  Target: screen(%d,%d) -> top-level client(%d,%d)\n", topCenterX, topCenterY, topClientPt.X, topClientPt.Y)

	lparam2 := MAKELONG(uint16(topClientPt.X), uint16(topClientPt.Y))

	// Move cursor to follower window center first
	procSetCursorPos.Call(uintptr(topCenterX), uintptr(topCenterY))

	// Send to top-level window
	procPostMessageW.Call(uintptr(follower.Hwnd), 0x0200, 0, lparam2) // WM_MOUSEMOVE
	procPostMessageW.Call(uintptr(follower.Hwnd), 0x0204, 0, lparam2) // WM_RBUTTONDOWN
	procPostMessageW.Call(uintptr(follower.Hwnd), 0x0205, 0, 0)       // WM_RBUTTONUP

	write("  Sent WM_RBUTTONDOWN + WM_RBUTTONUP to follower top-level %#x\n", follower.Hwnd)

	write("\n=== Done ===\n")
}
