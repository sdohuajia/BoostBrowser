package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) RecordLifecycleEvent(event string, fields []string) {
	a.lifecycleLog(event, fields...)
}

func IntentionalExitMarkerPath(appRoot string) string {
	return ResolveRuntimePath(appRoot, filepath.Join("logs", "intentional-exit.flag"))
}

func (a *App) markIntentionalExit(reason string) {
	if a == nil {
		return
	}
	path := IntentionalExitMarkerPath(a.appRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	content := fmt.Sprintf("%s | pid=%d | reason=%s\n", time.Now().Format("2006-01-02 15:04:05.000"), os.Getpid(), strings.TrimSpace(reason))
	_ = os.WriteFile(path, []byte(content), 0644)
}

func (a *App) ClearIntentionalExitMarker() {
	a.clearIntentionalExitMarker()
}

func (a *App) clearIntentionalExitMarker() {
	if a == nil {
		return
	}
	_ = os.Remove(IntentionalExitMarkerPath(a.appRoot))
}

func (a *App) lifecycleLog(event string, fields ...string) {
	if a == nil {
		return
	}
	path := a.resolveAppPath(filepath.Join("logs", "app-lifecycle.log"))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}

	parts := []string{
		time.Now().Format("2006-01-02 15:04:05.000"),
		strings.TrimSpace(event),
		fmt.Sprintf("pid=%d", os.Getpid()),
	}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			parts = append(parts, field)
		}
	}
	line := strings.Join(parts, " | ") + "\n"
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
}
