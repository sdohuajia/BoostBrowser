package backend

import (
	"boost-browser/backend/internal/config"
	"testing"
)

func TestBrowserStartTimingSettingsUsesDefaultsWhenUnset(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Browser.StartReadyTimeoutMs = 0
	cfg.Browser.StartStableWindowMs = -1

	readyMs := browserStartReadyTimeoutMillis(cfg)
	stableMs := browserStartStableWindowMillis(cfg)

	if readyMs != 3000 {
		t.Fatalf("expected default ready timeout 3000ms, got %d", readyMs)
	}
	if stableMs != 1200 {
		t.Fatalf("expected default stable window 1200ms, got %d", stableMs)
	}
}

func TestSaveBrowserSettingsPreservesExistingStartTimingWhenOmitted(t *testing.T) {
	app := NewApp(t.TempDir())
	app.config = config.DefaultConfig()
	app.config.Browser.StartReadyTimeoutMs = 15000
	app.config.Browser.StartStableWindowMs = 2400

	if err := app.SaveBrowserSettings(BrowserSettings{
		UserDataRoot:           app.config.Browser.UserDataRoot,
		DefaultFingerprintArgs: append([]string{}, app.config.Browser.DefaultFingerprintArgs...),
		DefaultLaunchArgs:      append([]string{}, app.config.Browser.DefaultLaunchArgs...),
		DefaultProxy:           app.config.Browser.DefaultProxy,
	}); err != nil {
		t.Fatalf("SaveBrowserSettings returned error: %v", err)
	}

	if app.config.Browser.StartReadyTimeoutMs != 15000 {
		t.Fatalf("expected ready timeout to be preserved, got %d", app.config.Browser.StartReadyTimeoutMs)
	}
	if app.config.Browser.StartStableWindowMs != 2400 {
		t.Fatalf("expected stable window to be preserved, got %d", app.config.Browser.StartStableWindowMs)
	}
}

func TestSaveBrowserSettingsAppliesExplicitStartTiming(t *testing.T) {
	app := NewApp(t.TempDir())
	app.config = config.DefaultConfig()

	if err := app.SaveBrowserSettings(BrowserSettings{
		UserDataRoot:           app.config.Browser.UserDataRoot,
		DefaultFingerprintArgs: append([]string{}, app.config.Browser.DefaultFingerprintArgs...),
		DefaultLaunchArgs:      append([]string{}, app.config.Browser.DefaultLaunchArgs...),
		DefaultProxy:           app.config.Browser.DefaultProxy,
		StartReadyTimeoutMs:    18000,
		StartStableWindowMs:    3000,
	}); err != nil {
		t.Fatalf("SaveBrowserSettings returned error: %v", err)
	}

	if app.config.Browser.StartReadyTimeoutMs != 18000 {
		t.Fatalf("expected ready timeout 18000ms, got %d", app.config.Browser.StartReadyTimeoutMs)
	}
	if app.config.Browser.StartStableWindowMs != 3000 {
		t.Fatalf("expected stable window 3000ms, got %d", app.config.Browser.StartStableWindowMs)
	}
}
