package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"boost-browser/backend"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const mainWindowBoundsStorageKey = "boost:main-window-bounds:v1"

type MainWindowBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func mainWindowBoundsPath(appRoot string) string {
	return backend.ResolveRuntimePath(appRoot, filepath.Join("window-state", "main-window-bounds.json"))
}

func validMainWindowBounds(bounds MainWindowBounds) bool {
	return bounds.Width >= 1200 && bounds.Height >= 700 && bounds.Width <= 10000 && bounds.Height <= 10000
}

func (a *App) SaveNativeMainWindowBounds(bounds MainWindowBounds) bool {
	if a == nil || !validMainWindowBounds(bounds) {
		return false
	}
	if err := saveNativeMainWindowBounds(appRoot, bounds); err != nil {
		a.RecordLifecycleEvent("main-window-bounds-save-failed", []string{fmt.Sprintf("error=%v", err)})
		return false
	}
	return true
}

func saveNativeMainWindowBounds(appRoot string, bounds MainWindowBounds) error {
	if !validMainWindowBounds(bounds) {
		return fmt.Errorf("invalid bounds: %+v", bounds)
	}
	path := mainWindowBoundsPath(appRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(bounds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0644)
}

func loadNativeMainWindowBounds(appRoot string) (*MainWindowBounds, string) {
	path := mainWindowBoundsPath(appRoot)
	if data, err := os.ReadFile(path); err == nil {
		var bounds MainWindowBounds
		if json.Unmarshal(data, &bounds) == nil && validMainWindowBounds(bounds) {
			return &bounds, "native-json"
		}
	}
	if bounds, ok := readLatestWebViewLocalStorageMainWindowBounds(); ok {
		_ = saveNativeMainWindowBounds(appRoot, bounds)
		return &bounds, "webview-localstorage-migrated"
	}
	return nil, "missing"
}

func restoreNativeMainWindowBounds(ctx context.Context, app *App) {
	bounds, source := loadNativeMainWindowBounds(appRoot)
	if bounds == nil {
		if app != nil {
			app.RecordLifecycleEvent("main-window-bounds-restore-skip", []string{fmt.Sprintf("source=%s", source)})
		}
		return
	}

	restored := *bounds
	restored, adjusted, adjustReason := sanitizeMainWindowBoundsForRestore(restored)

	runtime.WindowSetSize(ctx, restored.Width, restored.Height)
	time.Sleep(120 * time.Millisecond)
	runtime.WindowSetPosition(ctx, restored.X, restored.Y)
	time.Sleep(180 * time.Millisecond)
	// WebView2/Wails can still adjust the native window once during startup. Re-apply once
	// after a short settle delay so the saved coordinates win over default centering.
	runtime.WindowSetSize(ctx, restored.Width, restored.Height)
	runtime.WindowSetPosition(ctx, restored.X, restored.Y)
	if app != nil {
		fields := []string{
			fmt.Sprintf("source=%s", source),
			fmt.Sprintf("x=%d", restored.X),
			fmt.Sprintf("y=%d", restored.Y),
			fmt.Sprintf("width=%d", restored.Width),
			fmt.Sprintf("height=%d", restored.Height),
		}
		if adjusted {
			fields = append(fields,
				fmt.Sprintf("saved_x=%d", bounds.X),
				fmt.Sprintf("saved_y=%d", bounds.Y),
				fmt.Sprintf("adjust_reason=%s", adjustReason),
			)
		}
		app.RecordLifecycleEvent("main-window-bounds-restored", fields)
	}
}

func readLatestWebViewLocalStorageMainWindowBounds() (MainWindowBounds, bool) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return MainWindowBounds{}, false
	}
	exePath, err := os.Executable()
	if err != nil {
		return MainWindowBounds{}, false
	}
	levelDBDir := filepath.Join(configDir, filepath.Base(exePath), "EBWebView", "Default", "Local Storage", "leveldb")
	entries, err := os.ReadDir(levelDBDir)
	if err != nil {
		return MainWindowBounds{}, false
	}

	var newest MainWindowBounds
	found := false
	pattern := regexp.MustCompile(regexp.QuoteMeta(mainWindowBoundsStorageKey) + `[^{}]*(\{"x"\s*:\s*-?\d+\s*,\s*"y"\s*:\s*-?\d+\s*,\s*"width"\s*:\s*\d+\s*,\s*"height"\s*:\s*\d+\})`)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".log") && !strings.HasSuffix(lower, ".ldb") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(levelDBDir, name))
		if err != nil {
			continue
		}
		matches := pattern.FindAllSubmatch(data, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			var bounds MainWindowBounds
			if json.Unmarshal(match[1], &bounds) == nil && validMainWindowBounds(bounds) {
				newest = bounds
				found = true
			}
		}
	}
	return newest, found
}
