//go:build !windows

package main

func acquireSingleInstanceLock() (bool, error) {
	return true, nil
}

func releaseSingleInstanceLock() {}

func focusExistingMainWindow() bool { return false }

func acquireSyncPanelLock() (bool, error) {
	return true, nil
}

func releaseSyncPanelLock() {}

func focusExistingSyncPanelWindow() bool { return false }
