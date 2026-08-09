//go:build windows

package main

import "testing"

func TestIsBoostBrowserExecutable(t *testing.T) {
	valid := []string{
		`C:\Boost Browser\Boost Browser.exe`,
		`C:\Boost Browser\boost-browser.exe`,
		`c:\boost browser\BOOST BROWSER.EXE`,
	}
	for _, path := range valid {
		if !isBoostBrowserExecutable(path) {
			t.Fatalf("expected production executable to be accepted: %q", path)
		}
	}

	invalid := []string{
		`C:\Windows\py.exe`,
		`C:\Windows\System32\cmd.exe`,
		`C:\Python\python.exe`,
		`C:\Boost Browser\Boost Browser HubSDK Test.exe`,
		``,
	}
	for _, path := range invalid {
		if isBoostBrowserExecutable(path) {
			t.Fatalf("expected non-production executable to be rejected: %q", path)
		}
	}
}
