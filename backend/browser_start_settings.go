package backend

import (
	"boost-browser/backend/internal/config"
	"errors"
	"time"
)

const (
	defaultBrowserStartReadyTimeout = 3 * time.Second
	defaultBrowserStartStableWindow = 1200 * time.Millisecond
	defaultBrowserStartMaxAttempts  = 5
)

func browserStartReadyTimeoutMillis(cfg *config.Config) int {
	fallback := int(defaultBrowserStartReadyTimeout / time.Millisecond)
	if cfg == nil {
		return fallback
	}
	if cfg.Browser.StartReadyTimeoutMs > 0 {
		return cfg.Browser.StartReadyTimeoutMs
	}
	return fallback
}

func browserStartStableWindowMillis(cfg *config.Config) int {
	fallback := int(defaultBrowserStartStableWindow / time.Millisecond)
	if cfg == nil {
		return fallback
	}
	if cfg.Browser.StartStableWindowMs > 0 {
		return cfg.Browser.StartStableWindowMs
	}
	return fallback
}

func (a *App) browserStartTimingSettings() (time.Duration, time.Duration) {
	return time.Duration(browserStartReadyTimeoutMillis(a.config)) * time.Millisecond,
		time.Duration(browserStartStableWindowMillis(a.config)) * time.Millisecond
}

func browserStartAttemptCount() int {
	return defaultBrowserStartMaxAttempts
}

func shouldRetryBrowserReadyFailure(err error) bool {
	if err == nil {
		return false
	}

	var exitErr *browserStartupExitError
	return !errors.As(err, &exitErr)
}
