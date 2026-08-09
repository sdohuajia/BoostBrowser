//go:build windows

package main

import "syscall"

const (
	mainWindowVisibleMargin = 160
	smXVirtualScreen        = 76
	smYVirtualScreen        = 77
	smCxVirtualScreen       = 78
	smCyVirtualScreen       = 79
)

var (
	user32DLL            = syscall.NewLazyDLL("user32.dll")
	procGetSystemMetrics = user32DLL.NewProc("GetSystemMetrics")
)

type virtualScreenBounds struct {
	left   int
	top    int
	right  int
	bottom int
	ok     bool
}

func sanitizeMainWindowBoundsForRestore(bounds MainWindowBounds) (MainWindowBounds, bool, string) {
	vs := getVirtualScreenBounds()
	if !vs.ok {
		return bounds, false, ""
	}
	origX, origY := bounds.X, bounds.Y
	minX := vs.left - bounds.Width + mainWindowVisibleMargin
	maxX := vs.right - mainWindowVisibleMargin
	minY := vs.top
	maxY := vs.bottom - mainWindowVisibleMargin
	if minX > maxX {
		minX = vs.left
		maxX = vs.left
	}
	if minY > maxY {
		minY = vs.top
		maxY = vs.top
	}
	if bounds.X < minX {
		bounds.X = minX
	} else if bounds.X > maxX {
		bounds.X = maxX
	}
	if bounds.Y < minY {
		bounds.Y = minY
	} else if bounds.Y > maxY {
		bounds.Y = maxY
	}
	if bounds.X != origX || bounds.Y != origY {
		return bounds, true, "clamped-to-virtual-screen"
	}
	return bounds, false, ""
}

func getVirtualScreenBounds() virtualScreenBounds {
	left := getSystemMetric(smXVirtualScreen)
	top := getSystemMetric(smYVirtualScreen)
	width := getSystemMetric(smCxVirtualScreen)
	height := getSystemMetric(smCyVirtualScreen)
	if width <= 0 || height <= 0 {
		return virtualScreenBounds{}
	}
	return virtualScreenBounds{left: left, top: top, right: left + width, bottom: top + height, ok: true}
}

func getSystemMetric(index int32) int {
	ret, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int(int32(ret))
}
