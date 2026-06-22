package main

import "os"

var syncPanelMode = hasCLIArg("--sync-panel")

func hasCLIArg(target string) bool {
	for _, arg := range os.Args[1:] {
		if arg == target {
			return true
		}
	}
	return false
}

func (a *App) IsWindowSyncPanelMode() bool {
	return syncPanelMode
}

func (a *App) OpenWindowSyncPanel() error {
	if err := openWindowSyncPanel(); err != nil {
		a.RecordLifecycleEvent("sync-panel-launch", []string{"status=failed", "error=" + err.Error()})
		return err
	}
	a.RecordLifecycleEvent("sync-panel-launch", []string{"status=started"})
	return nil
}
