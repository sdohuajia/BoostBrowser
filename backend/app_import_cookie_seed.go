package backend

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const importedCookieSeedFileName = ".boost-browser-import-cookies.json"

type importedCookieSeed struct {
	Cookies []importedCookieSeedEntry `json:"cookies"`
}

type importedCookieSeedEntry struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain,omitempty"`
	Path     string  `json:"path,omitempty"`
	URL      string  `json:"url,omitempty"`
	Expires  float64 `json:"expires,omitempty"`
	HttpOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	SameSite string  `json:"sameSite,omitempty"`
}

func importedCookieSeedPath(userDataDir string) string {
	return filepath.Join(strings.TrimSpace(userDataDir), importedCookieSeedFileName)
}

func normalizeImportedCookieSeedEntries(raw string) ([]importedCookieSeedEntry, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var payload []map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("Cookie JSON 解析失败: %w", err)
	}

	result := make([]importedCookieSeedEntry, 0, len(payload))
	for _, item := range payload {
		entry := importedCookieSeedEntry{
			Name:     strings.TrimSpace(readCookieString(item, "name")),
			Value:    readCookieString(item, "value"),
			Domain:   strings.TrimSpace(readCookieString(item, "domain")),
			Path:     strings.TrimSpace(readCookieString(item, "path")),
			HttpOnly: readCookieBool(item, "httpOnly", "http_only"),
			Secure:   readCookieBool(item, "secure"),
			SameSite: normalizeImportedCookieSameSite(readCookieString(item, "sameSite", "same_site")),
		}
		if entry.Path == "" {
			entry.Path = "/"
		}
		if expires, ok := readCookieExpiry(item, "expires", "expirationDate"); ok {
			entry.Expires = expires
		}
		entry.URL = strings.TrimSpace(readCookieString(item, "url"))
		if entry.URL == "" {
			entry.URL = buildImportedCookieURL(entry.Domain, entry.Path, entry.Secure)
		}
		if entry.Name == "" || entry.URL == "" {
			continue
		}
		result = append(result, entry)
	}
	return result, nil
}

func writeImportedCookieSeed(userDataDir string, cookies []importedCookieSeedEntry) error {
	if len(cookies) == 0 {
		return nil
	}
	if err := os.MkdirAll(userDataDir, 0o755); err != nil {
		return fmt.Errorf("创建 Cookie 暂存目录失败: %w", err)
	}
	data, err := json.MarshalIndent(importedCookieSeed{Cookies: cookies}, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 Cookie 暂存文件失败: %w", err)
	}
	if err := os.WriteFile(importedCookieSeedPath(userDataDir), data, 0o644); err != nil {
		return fmt.Errorf("写入 Cookie 暂存文件失败: %w", err)
	}
	return nil
}

func loadImportedCookieSeed(userDataDir string) ([]importedCookieSeedEntry, error) {
	data, err := os.ReadFile(importedCookieSeedPath(userDataDir))
	if err != nil {
		return nil, err
	}
	var seed importedCookieSeed
	if err := json.Unmarshal(data, &seed); err != nil {
		return nil, fmt.Errorf("读取 Cookie 暂存文件失败: %w", err)
	}
	return seed.Cookies, nil
}

func applyPendingImportedCookieSeed(debugPort int, userDataDir string) (applied int, skipped int, err error) {
	seedPath := importedCookieSeedPath(userDataDir)
	cookies, loadErr := loadImportedCookieSeed(userDataDir)
	if loadErr != nil {
		if os.IsNotExist(loadErr) {
			return 0, 0, nil
		}
		return 0, 0, loadErr
	}
	applied, skipped, err = importCookiesViaCDP(debugPort, cookies)
	if err != nil {
		return applied, skipped, err
	}
	if removeErr := os.Remove(seedPath); removeErr != nil && !os.IsNotExist(removeErr) {
		return applied, skipped, fmt.Errorf("删除 Cookie 暂存文件失败: %w", removeErr)
	}
	return applied, skipped, nil
}

func importCookiesViaCDP(debugPort int, cookies []importedCookieSeedEntry) (applied int, skipped int, err error) {
	if len(cookies) == 0 {
		return 0, 0, nil
	}
	_, _ = cdpCall(debugPort, "Network.enable", nil)
	var firstErr error
	for _, cookie := range cookies {
		params := map[string]any{
			"name":     cookie.Name,
			"value":    cookie.Value,
			"path":     cookie.Path,
			"secure":   cookie.Secure,
			"httpOnly": cookie.HttpOnly,
		}
		if cookie.URL != "" {
			params["url"] = cookie.URL
		}
		if cookie.Domain != "" {
			params["domain"] = cookie.Domain
		}
		if cookie.SameSite != "" {
			params["sameSite"] = cookie.SameSite
		}
		if cookie.Expires > 0 {
			params["expires"] = cookie.Expires
		}
		result, callErr := cdpCall(debugPort, "Network.setCookie", params)
		if callErr != nil {
			skipped++
			if firstErr == nil {
				firstErr = callErr
			}
			continue
		}
		if success, ok := result["success"].(bool); ok && !success {
			skipped++
			if firstErr == nil {
				firstErr = fmt.Errorf("CDP 未接受 Cookie %q", cookie.Name)
			}
			continue
		}
		applied++
	}
	if applied == 0 && firstErr != nil {
		return applied, skipped, firstErr
	}
	return applied, skipped, nil
}

func readCookieString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := item[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return typed
		case float64:
			if math.Mod(typed, 1) == 0 {
				return strconv.FormatInt(int64(typed), 10)
			}
			return strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			if typed {
				return "true"
			}
			return "false"
		default:
			data, err := json.Marshal(typed)
			if err == nil {
				return string(data)
			}
		}
	}
	return ""
}

func readCookieBool(item map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := item[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "1", "true", "yes", "y":
				return true
			case "0", "false", "no", "n", "":
				return false
			}
		case float64:
			return typed != 0
		}
	}
	return false
}

func readCookieExpiry(item map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		value, ok := item[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case float64:
			if typed > 0 {
				return typed, true
			}
		case string:
			t := strings.TrimSpace(typed)
			if t == "" {
				continue
			}
			if f, err := strconv.ParseFloat(t, 64); err == nil && f > 0 {
				return f, true
			}
			if ts, err := time.Parse(time.RFC3339, t); err == nil {
				return float64(ts.Unix()), true
			}
		}
	}
	return 0, false
}

func normalizeImportedCookieSameSite(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "-1", "unspecified":
		return ""
	case "0", "none":
		return "None"
	case "1", "lax":
		return "Lax"
	case "2", "strict":
		return "Strict"
	default:
		return ""
	}
}

func buildImportedCookieURL(domain, path string, secure bool) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	host := strings.TrimPrefix(domain, ".")
	if host == "" {
		return ""
	}
	if path == "" {
		path = "/"
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return scheme + "://" + host + path
}
