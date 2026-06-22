//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var boostBrowserSingleInstanceMutex windows.Handle
var boostBrowserSyncPanelMutex windows.Handle

var (
	user32SingleInstance           = windows.NewLazySystemDLL("user32.dll")
	procEnumWindowsSingleInstance  = user32SingleInstance.NewProc("EnumWindows")
	procGetWindowTextLengthW       = user32SingleInstance.NewProc("GetWindowTextLengthW")
	procGetWindowTextW             = user32SingleInstance.NewProc("GetWindowTextW")
	procIsWindowVisibleSingle      = user32SingleInstance.NewProc("IsWindowVisible")
	procShowWindow                 = user32SingleInstance.NewProc("ShowWindow")
	procSetForegroundWindow        = user32SingleInstance.NewProc("SetForegroundWindow")
	procBringWindowToTop           = user32SingleInstance.NewProc("BringWindowToTop")
	procGetWindowThreadProcessIDSI = user32SingleInstance.NewProc("GetWindowThreadProcessId")
)

const (
	swRestore = 9
	swShow    = 5
)

func acquireNamedMutex(name string, target *windows.Handle) (bool, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false, err
	}

	h, err := windows.CreateMutex(nil, false, namePtr)
	if err != nil {
		if err == windows.ERROR_ALREADY_EXISTS {
			if h != 0 {
				windows.CloseHandle(h)
			}
			*target = 0
			return false, nil
		}
		return false, fmt.Errorf("create named mutex: %w", err)
	}
	*target = h

	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		windows.CloseHandle(h)
		*target = 0
		return false, nil
	}
	return true, nil
}

func releaseNamedMutex(target *windows.Handle) {
	if *target != 0 {
		windows.CloseHandle(*target)
		*target = 0
	}
}

func acquireSingleInstanceLock() (bool, error) {
	return acquireNamedMutex(`Local\BoostBrowser_SingleInstance_Mutex_v1`, &boostBrowserSingleInstanceMutex)
}

func releaseSingleInstanceLock() {
	releaseNamedMutex(&boostBrowserSingleInstanceMutex)
}

func acquireSyncPanelLock() (bool, error) {
	return acquireNamedMutex(`Local\BoostBrowser_WindowSyncPanel_Mutex_v1`, &boostBrowserSyncPanelMutex)
}

func releaseSyncPanelLock() {
	releaseNamedMutex(&boostBrowserSyncPanelMutex)
}

// focusExistingMainWindow is only used when a second boost-browser.exe is launched.
// It activates the already-running Wails main window. Browser profile windows are not limited.
func focusExistingMainWindow() bool {
	return focusExistingWindowByKeywords([]string{"Boost Browser", "Ant Browser", "boost-browser"})
}

func focusExistingSyncPanelWindow() bool {
	return focusExistingWindowByKeywords([]string{"Boost Browser · 窗口同步", "Boost Browser - 窗口同步", "窗口同步"})
}

func focusExistingWindowByKeywords(keywords []string) bool {
	currentPID := uint32(windows.GetCurrentProcessId())
	var target windows.Handle

	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		if hwnd == 0 {
			return 1
		}
		var pid uint32
		procGetWindowThreadProcessIDSI.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 || pid == currentPID {
			return 1
		}

		length, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if length == 0 {
			return 1
		}
		buf := make([]uint16, length+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), length+1)
		title := strings.TrimSpace(windows.UTF16ToString(buf))
		if title == "" {
			return 1
		}
		lowerTitle := strings.ToLower(title)
		for _, keyword := range keywords {
			if strings.Contains(lowerTitle, strings.ToLower(keyword)) {
				target = windows.Handle(hwnd)
				return 0
			}
		}
		return 1
	})

	procEnumWindowsSingleInstance.Call(cb, 0)
	if target == 0 {
		return false
	}

	visible, _, _ := procIsWindowVisibleSingle.Call(uintptr(target))
	if visible == 0 {
		procShowWindow.Call(uintptr(target), swShow)
	}
	procShowWindow.Call(uintptr(target), swRestore)
	procBringWindowToTop.Call(uintptr(target))
	procSetForegroundWindow.Call(uintptr(target))
	return true
}
