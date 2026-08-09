//go:build windows

package backend

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestInputSyncerURLSyncDefaultsToEnabledAndCanBeDisabled(t *testing.T) {
	t.Setenv("BOOST_BROWSER_ENABLE_SYNC_URL_SYNC", "")
	if !syncURLSyncEnabled() {
		t.Fatalf("URL sync should be enabled by default for self-healing navigation")
	}

	t.Setenv("BOOST_BROWSER_ENABLE_SYNC_URL_SYNC", "0")
	if syncURLSyncEnabled() {
		t.Fatalf("URL sync should be disabled when BOOST_BROWSER_ENABLE_SYNC_URL_SYNC=0")
	}
}

func TestIsSyncableURLAllowsOnlyWebPages(t *testing.T) {
	for _, rawURL := range []string{"https://example.com", "http://example.com/path"} {
		if !isSyncableURL(rawURL) {
			t.Fatalf("expected URL to be syncable: %q", rawURL)
		}
	}
	for _, rawURL := range []string{"", "about:blank", "chrome://settings", "chrome-extension://wallet"} {
		if isSyncableURL(rawURL) {
			t.Fatalf("expected URL to be ignored: %q", rawURL)
		}
	}
}

func TestCDPMouseEventForMessage(t *testing.T) {
	cases := []struct {
		message uint32
		event   string
		button  string
		buttons int
	}{
		{WM_LBUTTONDOWN, "mousePressed", "left", 1},
		{WM_LBUTTONUP, "mouseReleased", "left", 0},
		{WM_RBUTTONDOWN, "mousePressed", "right", 2},
		{WM_RBUTTONUP, "mouseReleased", "right", 0},
	}
	for _, tc := range cases {
		event, button, buttons, ok := cdpMouseEventForMessage(tc.message)
		if !ok || event != tc.event || button != tc.button || buttons != tc.buttons {
			t.Fatalf("unexpected mapping for %#x: %q %q %d ok=%v", tc.message, event, button, buttons, ok)
		}
	}
	if _, _, _, ok := cdpMouseEventForMessage(WM_MOUSEMOVE); ok {
		t.Fatal("mouse move must not be treated as a click event")
	}
}

func TestNewInputSyncerWithLoggerStoresLifecycleLogger(t *testing.T) {
	called := false
	s := NewInputSyncerWithLogger(func(event string, fields ...string) {
		called = event == "sync-test" && len(fields) == 1 && fields[0] == "ok=true"
	})

	s.lifecycle("sync-test", "ok=true")
	if !called {
		t.Fatalf("expected lifecycle logger to be called")
	}
}

func TestGetFollowerSnapshotReturnsCopy(t *testing.T) {
	s := NewInputSyncer()
	s.followerMu.Lock()
	s.followerSnapshot = []windows.HWND{1, 2}
	s.followerMu.Unlock()

	snap := s.getFollowerSnapshot()
	snap[0] = 99

	s.followerMu.RLock()
	defer s.followerMu.RUnlock()
	if s.followerSnapshot[0] != 1 {
		t.Fatalf("snapshot mutation leaked into syncer state: got %v", s.followerSnapshot[0])
	}
}

func TestSyncDebugLogEnabledByEnv(t *testing.T) {
	old := os.Getenv("BOOST_BROWSER_SYNC_DEBUG_LOG")
	defer os.Setenv("BOOST_BROWSER_SYNC_DEBUG_LOG", old)

	os.Unsetenv("BOOST_BROWSER_SYNC_DEBUG_LOG")
	if syncDebugLogEnabled() {
		t.Fatalf("sync debug log should be off by default")
	}
	os.Setenv("BOOST_BROWSER_SYNC_DEBUG_LOG", "true")
	if !syncDebugLogEnabled() {
		t.Fatalf("sync debug log should be enabled by env")
	}
}
