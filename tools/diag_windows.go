//go:build ignore
// +build ignore

// Diagnostic tool: dumps all Chrome windows and their render child rects
// to help debug coordinate mapping issues

package main

import (
	"fmt"
	"os"
	"syscall"
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
	procGetClientRect    = user32.NewProc("GetClientRect")
	procClientToScreen   = user32.NewProc("ClientToScreen")
	procScreenToClient   = user32.NewProc("ScreenToClient")
	procFindWindowExW    = user32.NewProc("FindWindowExW")
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

type POINT struct {
	X, Y int32
}

func getClassName(hwnd syscall.Handle) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), 256)
	if n > 0 {
		return syscall.UTF16ToString(buf[:n])
	}
	return ""
}

func getWindowText(hwnd syscall.Handle) string {
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), 512)
	if n > 0 {
		return syscall.UTF16ToString(buf[:n])
	}
	return ""
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

func findRenderChild(parent syscall.Handle) (syscall.Handle, RECT) {
	var found syscall.Handle
	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		className := getClassName(hwnd)
		if className == "Chrome_RenderWidgetHostHWND" {
			r := getWindowRect(hwnd)
			w := r.Right - r.Left
			h := r.Bottom - r.Top
			vis := isWindowVisible(hwnd)
			fmt.Printf("    RenderChild=%#x class=%s vis=%v rect=(%d,%d,%d,%d) size=%dx%d\n",
				hwnd, className, vis, r.Left, r.Top, r.Right, r.Bottom, w, h)
			if vis && w > 50 && h > 50 && found == 0 {
				found = hwnd
			}
		}
		return 1
	})
	procEnumChildWindows.Call(uintptr(parent), cb, 0)
	if found != 0 {
		return found, getWindowRect(found)
	}
	return parent, getWindowRect(parent)
}

func main() {
	f, _ := os.Create(`C:\diag_windows.txt`)
	defer f.Close()

	write := func(format string, args ...interface{}) {
		msg := fmt.Sprintf(format, args...)
		fmt.Print(msg)
		f.WriteString(msg)
	}

	// Find all top-level windows with Chrome_WidgetWin_1 class
	write("=== All Chrome/Edge top-level windows ===\n")

	cb := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		className := getClassName(hwnd)
		if !isWindowVisible(hwnd) {
			return 1
		}
		if className == "Chrome_WidgetWin_1" || className == "Chrome_MainWindow" {
			title := getWindowText(hwnd)
			rect := getWindowRect(hwnd)
			clientRect := getClientRect(hwnd)
			clientPt := clientToScreen(hwnd, 0, 0)

			write("\n--- Window: %#x class=%s title=%q ---\n", hwnd, className, title)
			write("  WindowRect: (%d, %d, %d, %d) size=%dx%d\n",
				rect.Left, rect.Top, rect.Right, rect.Bottom,
				rect.Right-rect.Left, rect.Bottom-rect.Top)
			write("  ClientRect: (%d, %d, %d, %d) size=%dx%d\n",
				clientRect.Left, clientRect.Top, clientRect.Right, clientRect.Bottom,
				clientRect.Right-clientRect.Left, clientRect.Bottom-clientRect.Top)
			write("  ClientToScreen(0,0) = (%d, %d)\n", clientPt.X, clientPt.Y)

			// Find render child
			renderHwnd, renderRect := findRenderChild(hwnd)
			write("  RenderChild: %#x\n", renderHwnd)
			if renderHwnd != hwnd {
				write("  RenderRect: (%d, %d, %d, %d) size=%dx%d\n",
					renderRect.Left, renderRect.Top, renderRect.Right, renderRect.Bottom,
					renderRect.Right-renderRect.Left, renderRect.Bottom-renderRect.Top)

				renderClientRect := getClientRect(renderHwnd)
				renderClientPt := clientToScreen(renderHwnd, 0, 0)
				write("  RenderClientRect: (%d, %d, %d, %d) size=%dx%d\n",
					renderClientRect.Left, renderClientRect.Top, renderClientRect.Right, renderClientRect.Bottom,
					renderClientRect.Right-renderClientRect.Left, renderClientRect.Bottom-renderClientRect.Top)
				write("  RenderClientToScreen(0,0) = (%d, %d)\n", renderClientPt.X, renderClientPt.Y)

				// Calculate offsets
				tabBarHeight := renderRect.Top - rect.Top
				borderWidth := renderRect.Left - rect.Left
				write("  TabBar+AddressBar height = renderRect.Top - windowRect.Top = %d - %d = %d\n",
					renderRect.Top, rect.Top, tabBarHeight)
				write("  Border width = renderRect.Left - windowRect.Left = %d - %d = %d\n",
					renderRect.Left, rect.Left, borderWidth)

				// Test coordinate mapping: click at center of render area
				centerScreenX := (renderRect.Left + renderRect.Right) / 2
				centerScreenY := (renderRect.Top + renderRect.Bottom) / 2
				write("\n  === Test coordinate mapping (click at render center) ===\n")
				write("  Screen center of render: (%d, %d)\n", centerScreenX, centerScreenY)

				// ScreenToClient on render child
				clientPt := screenToClient(renderHwnd, centerScreenX, centerScreenY)
				write("  ScreenToClient(renderChild, %d, %d) = (%d, %d)\n",
					centerScreenX, centerScreenY, clientPt.X, clientPt.Y)

				// What the current mapCoordsPython would calculate
				relX := float64(int(centerScreenX)-int(renderRect.Left)) / float64(renderRect.Right-renderRect.Left)
				relY := float64(int(centerScreenY)-int(renderRect.Top)) / float64(renderRect.Bottom-renderRect.Top)
				mapX := int(float64(renderClientRect.Right-renderClientRect.Left) * relX)
				mapY := int(float64(renderClientRect.Bottom-renderClientRect.Top) * relY)
				write("  mapCoordsPython: rel=(%.4f, %.4f) -> client(%d, %d)\n", relX, relY, mapX, mapY)
				write("  (Should be ~center of client area: %d, %d)\n",
					(renderClientRect.Right-renderClientRect.Left)/2,
					(renderClientRect.Bottom-renderClientRect.Top)/2)
			}
		}
		return 1
	})

	procEnumWindows.Call(cb, 0)

	write("\n=== Done ===\n")
	fmt.Println("\nResults written to C:\\diag_windows.txt")
}
