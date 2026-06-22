//go:build !windows

package main

func sanitizeMainWindowBoundsForRestore(bounds MainWindowBounds) (MainWindowBounds, bool, string) {
	return bounds, false, ""
}
