//go:build !windows

package backend

func startExtensionPopupSizer(pid int) {}

func StartGlobalExtensionPopupSizer(appRoot string) {}

func StartGlobalSerializedWindowWatchers(appRoot string) {}

func restoreBrowserWindowsAfterStartup(pid int) {}

func StartGlobalServiceWorkerDevToolsRestorer(appRoot string) {}
