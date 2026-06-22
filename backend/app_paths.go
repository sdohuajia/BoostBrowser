package backend

import (
	"boost-browser/backend/internal/apppath"
)

// appRootAbs 返回应用根目录的绝对路径，优先使用 App 注入的 appRoot。
func (a *App) appRootAbs() string {
	return apppath.InstallRoot(a.appRoot)
}

// appStateRootAbs 返回应用可写状态目录的绝对路径。
func (a *App) appStateRootAbs() string {
	return apppath.StateRoot(a.appRoot)
}

// appDataDir 返回 data 根目录绝对路径。
func (a *App) appDataDir() string {
	return a.resolveAppPath("data")
}

// appChromeDir 返回 chrome 根目录绝对路径。
func (a *App) appChromeDir() string {
	return a.resolveAppPath("chrome")
}
