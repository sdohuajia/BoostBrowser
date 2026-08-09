package main

import "testing"

func TestGlobalWindowWatchersDefaultOff(t *testing.T) {
	t.Setenv("BOOST_BROWSER_ENABLE_GLOBAL_WINDOW_WATCHERS", "")
	t.Setenv("BOOST_BROWSER_DISABLE_GLOBAL_WINDOW_WATCHERS", "")
	if shouldEnableGlobalWindowWatchers() {
		t.Fatal("global window watchers must be disabled by default because the packaged Win32 polling loop correlates with host exit_code=2 restarts")
	}
}

func TestGlobalWindowWatchersRequireExplicitOptIn(t *testing.T) {
	t.Setenv("BOOST_BROWSER_ENABLE_GLOBAL_WINDOW_WATCHERS", "1")
	t.Setenv("BOOST_BROWSER_DISABLE_GLOBAL_WINDOW_WATCHERS", "")
	if !shouldEnableGlobalWindowWatchers() {
		t.Fatal("global window watchers should be enabled only by explicit opt-in")
	}
}

func TestGlobalWindowWatchersDisableFlagWins(t *testing.T) {
	t.Setenv("BOOST_BROWSER_ENABLE_GLOBAL_WINDOW_WATCHERS", "1")
	t.Setenv("BOOST_BROWSER_DISABLE_GLOBAL_WINDOW_WATCHERS", "1")
	if shouldEnableGlobalWindowWatchers() {
		t.Fatal("explicit disable flag must override enable flag")
	}
}

func TestExtensionPopupSizerDefaultOn(t *testing.T) {
	t.Setenv("BOOST_BROWSER_DISABLE_EXTENSION_POPUP_SIZER", "")
	if !shouldEnableExtensionPopupSizer() {
		t.Fatal("safe popup-only sizer must be enabled by default so visible oversized OKX and wallet popups are clamped")
	}
}

func TestExtensionPopupSizerCanBeDisabled(t *testing.T) {
	t.Setenv("BOOST_BROWSER_DISABLE_EXTENSION_POPUP_SIZER", "1")
	if shouldEnableExtensionPopupSizer() {
		t.Fatal("explicit popup sizer disable flag must win")
	}
}
