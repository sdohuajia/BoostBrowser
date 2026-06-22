//go:build windows

package backend

import "testing"

func TestParseChromeRuntimeCommandLine(t *testing.T) {
	cmd := `"C:\BoostBrowserTest\chrome\cloak\chrome.exe" "--user-data-dir=C:\BoostBrowserTest\data\profile-1" --remote-debugging-port=55089 --no-first-run`
	userDataDir, debugPort := parseChromeRuntimeCommandLine(cmd)
	if userDataDir != `C:\BoostBrowserTest\data\profile-1` {
		t.Fatalf("unexpected user data dir: %q", userDataDir)
	}
	if debugPort != 55089 {
		t.Fatalf("unexpected debug port: %d", debugPort)
	}
}

func TestParseChromeRuntimeCommandLineUnquotedUserDataDir(t *testing.T) {
	cmd := `C:\chrome.exe --remote-debugging-port=61234 --user-data-dir=C:\Temp\profile --window-size=1280,900`
	userDataDir, debugPort := parseChromeRuntimeCommandLine(cmd)
	if userDataDir != `C:\Temp\profile` {
		t.Fatalf("unexpected user data dir: %q", userDataDir)
	}
	if debugPort != 61234 {
		t.Fatalf("unexpected debug port: %d", debugPort)
	}
}

func TestNormalizeRuntimePathKeyIgnoresCaseAndQuotes(t *testing.T) {
	left := normalizeRuntimePathKey(`"C:\BoostBrowserTest\data\profile-1\"`)
	right := normalizeRuntimePathKey(`<legacy-path>boost browser\data\profile-1`)
	if left != right {
		t.Fatalf("normalized keys differ: %q vs %q", left, right)
	}
}
