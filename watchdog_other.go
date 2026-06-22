//go:build !windows

package main

func runUnexpectedExitWatchdogMode() bool        { return false }
func startUnexpectedExitWatchdog(appRoot string) {}
