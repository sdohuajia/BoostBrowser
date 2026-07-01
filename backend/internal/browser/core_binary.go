package browser

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// CoreExecutableCandidates 返回当前平台可接受的浏览器可执行文件候选名。
func CoreExecutableCandidates() []string {
	switch goruntime.GOOS {
	case "windows":
		return []string{"chrome.exe"}
	case "linux":
		return []string{"chrome", "chrome-bin", "chrome.exe"}
	case "darwin":
		return []string{
			"Google Chrome.app/Contents/MacOS/Google Chrome",
			"Chromium.app/Contents/MacOS/Chromium",
			"chrome",
		}
	default:
		return []string{"chrome"}
	}
}

// FindCoreExecutable 在指定目录查找可执行文件，返回绝对路径和命中的候选名。
func FindCoreExecutable(baseDir string) (string, string, bool) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return "", "", false
	}
	if directPath, directCandidate, ok := findDirectCoreExecutable(baseDir); ok {
		return directPath, directCandidate, true
	}
	if bundlePath, bundleCandidate, ok := findAppBundleExecutable(baseDir); ok {
		return bundlePath, bundleCandidate, true
	}
	for _, candidate := range CoreExecutableCandidates() {
		p := filepath.Join(baseDir, filepath.FromSlash(candidate))
		if _, err := os.Stat(p); err == nil {
			return p, candidate, true
		}
	}
	return "", "", false
}

func findDirectCoreExecutable(path string) (string, string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", "", false
	}

	normalized := filepath.ToSlash(filepath.Clean(path))
	for _, candidate := range CoreExecutableCandidates() {
		candidatePath := filepath.ToSlash(candidate)
		if strings.HasSuffix(normalized, candidatePath) || filepath.Base(normalized) == filepath.Base(candidatePath) {
			return path, candidate, true
		}
	}

	return "", "", false
}

func findAppBundleExecutable(path string) (string, string, bool) {
	if goruntime.GOOS != "darwin" {
		return "", "", false
	}

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", "", false
	}

	normalized := filepath.ToSlash(filepath.Clean(path))
	if !strings.HasSuffix(strings.ToLower(normalized), ".app") {
		return "", "", false
	}

	for _, candidate := range CoreExecutableCandidates() {
		candidatePath := filepath.ToSlash(candidate)
		appMarker := ".app/"
		index := strings.Index(strings.ToLower(candidatePath), appMarker)
		if index < 0 {
			continue
		}
		if !strings.EqualFold(filepath.Base(normalized), filepath.Base(candidatePath[:index+len(".app")])) {
			continue
		}

		relativeExecutable := candidatePath[index+len(appMarker):]
		if relativeExecutable == "" {
			continue
		}

		p := filepath.Join(path, filepath.FromSlash(relativeExecutable))
		if _, err := os.Stat(p); err == nil {
			return p, candidate, true
		}
	}

	return "", "", false
}
