package backend

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type ExtensionImportResult struct {
	ExtensionDir    string   `json:"extensionDir"`
	ExtensionID     string   `json:"extensionId"`
	UpdatedProfiles []string `json:"updatedProfiles"`
	Message         string   `json:"message"`
}

var chromeWebStoreIDPattern = regexp.MustCompile(`[a-p]{32}`)

// BrowserProfileRemoveExtension 解除扩展与实例启动参数的绑定，并清理 profile 内残留的扩展记录。
func (a *App) BrowserProfileRemoveExtension(profileIds []string, downloadAddress string) (*ExtensionImportResult, error) {
	profileIds = normalizeProfileIDs(profileIds)
	if len(profileIds) == 0 {
		return nil, fmt.Errorf("请选择要移除扩展的实例")
	}
	downloadAddress = strings.TrimSpace(downloadAddress)
	if downloadAddress == "" {
		return nil, fmt.Errorf("缺少扩展标识")
	}
	if a == nil || a.browserMgr == nil {
		return nil, fmt.Errorf("浏览器管理器未初始化")
	}

	extID := extractExtensionID(downloadAddress)
	if extID == "" {
		return nil, fmt.Errorf("无法识别扩展 ID")
	}
	extDir := filepath.Join(a.appRoot, "extensions", "imported", safePathName(extID))

	a.browserMgr.Mutex.Lock()
	updated := make([]string, 0, len(profileIds))
	for _, id := range profileIds {
		profile, ok := a.browserMgr.Profiles[id]
		if !ok || profile == nil {
			continue
		}
		nextArgs, changed := removeExtensionDirFromLaunchArgs(profile.LaunchArgs, extDir)
		if !changed {
			continue
		}
		profile.LaunchArgs = nextArgs
		profile.UpdatedAt = time.Now().Format(time.RFC3339)
		cleanupRemovedManagedExtension(profile.UserDataDir, extDir, extID, a.appRoot)
		updated = append(updated, id)
	}
	if len(updated) == 0 {
		a.browserMgr.Mutex.Unlock()
		return nil, fmt.Errorf("选中的实例未绑定该扩展")
	}
	stillReferenced := false
	for _, profile := range a.browserMgr.Profiles {
		if profile == nil {
			continue
		}
		if hasExtensionDirInLaunchArgs(profile.LaunchArgs, extDir) {
			stillReferenced = true
			break
		}
	}
	if err := a.browserMgr.SaveProfiles(); err != nil {
		a.browserMgr.Mutex.Unlock()
		return nil, fmt.Errorf("保存实例扩展配置失败：%w", err)
	}
	a.browserMgr.Mutex.Unlock()

	if !stillReferenced {
		_ = os.RemoveAll(extDir)
	}

	return &ExtensionImportResult{
		ExtensionDir:    extDir,
		ExtensionID:     extID,
		UpdatedProfiles: updated,
		Message:         fmt.Sprintf("扩展已从 %d 个实例解绑，重启实例后不再出现", len(updated)),
	}, nil
}

// BrowserProfileImportExtension 下载扩展并绑定到选中的实例。
// 支持 Chrome Web Store 详情页、32 位扩展 ID、直接 .crx/.zip 下载地址。
func (a *App) BrowserProfileImportExtension(profileIds []string, downloadAddress string) (*ExtensionImportResult, error) {
	profileIds = normalizeProfileIDs(profileIds)
	if len(profileIds) == 0 {
		return nil, fmt.Errorf("请选择要导入扩展的实例")
	}
	downloadAddress = strings.TrimSpace(downloadAddress)
	if downloadAddress == "" {
		return nil, fmt.Errorf("请输入扩展程序下载地址")
	}
	if a == nil || a.browserMgr == nil {
		return nil, fmt.Errorf("浏览器管理器未初始化")
	}

	extID := extractExtensionID(downloadAddress)
	downloadURL := resolveExtensionDownloadURL(downloadAddress, extID)
	payload, err := downloadExtensionPayload(downloadURL)
	if err != nil {
		return nil, err
	}
	zipPayload, err := extractZipPayloadFromCRX(payload)
	if err != nil {
		return nil, err
	}

	if extID == "" {
		sum := sha256.Sum256(payload)
		extID = "external-" + hex.EncodeToString(sum[:])[:16]
	}
	extDir := filepath.Join(a.appRoot, "extensions", "imported", safePathName(extID))
	if err := os.RemoveAll(extDir); err != nil {
		return nil, fmt.Errorf("清理旧扩展目录失败：%w", err)
	}
	if err := unzipBytes(zipPayload, extDir); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(extDir, "manifest.json")); err != nil {
		return nil, fmt.Errorf("扩展解包失败：未找到 manifest.json")
	}

	updated, err := a.bindExtensionDirToProfiles(profileIds, extDir)
	if err != nil {
		return nil, err
	}
	return &ExtensionImportResult{
		ExtensionDir:    extDir,
		ExtensionID:     extID,
		UpdatedProfiles: updated,
		Message:         fmt.Sprintf("扩展已导入并绑定到 %d 个实例，重启实例后生效", len(updated)),
	}, nil
}

func normalizeProfileIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func extractExtensionID(input string) string {
	lower := strings.ToLower(strings.TrimSpace(input))
	if m := chromeWebStoreIDPattern.FindString(lower); m != "" {
		return m
	}
	return ""
}

func resolveExtensionDownloadURL(input string, extID string) string {
	input = strings.TrimSpace(input)
	if extID != "" && (strings.Contains(strings.ToLower(input), "chromewebstore.google.com") || input == extID) {
		return "https://clients2.google.com/service/update2/crx?response=redirect&prodversion=144.0.7559.61&acceptformat=crx2,crx3&x=" + url.QueryEscape("id="+extID+"&installsource=ondemand&uc")
	}
	return input
}

func downloadExtensionPayload(downloadURL string) ([]byte, error) {
	client := &http.Client{Timeout: 90 * time.Second}
	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("扩展下载地址无效：%w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.7559.61 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("扩展下载失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("扩展下载失败：HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 300*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("读取扩展数据失败：%w", err)
	}
	if len(data) < 4 {
		return nil, fmt.Errorf("扩展下载失败：文件为空或格式错误")
	}
	return data, nil
}

func extractZipPayloadFromCRX(data []byte) ([]byte, error) {
	if len(data) >= 4 && bytes.Equal(data[:4], []byte("PK\x03\x04")) {
		return data, nil
	}
	if len(data) < 16 || !bytes.Equal(data[:4], []byte("Cr24")) {
		return nil, fmt.Errorf("扩展格式不支持：请提供 Chrome Web Store 地址、.crx 或 .zip 下载地址")
	}
	version := binary.LittleEndian.Uint32(data[4:8])
	var offset uint32
	switch version {
	case 2:
		pubLen := binary.LittleEndian.Uint32(data[8:12])
		sigLen := binary.LittleEndian.Uint32(data[12:16])
		offset = 16 + pubLen + sigLen
	case 3:
		headerLen := binary.LittleEndian.Uint32(data[8:12])
		offset = 12 + headerLen
	default:
		return nil, fmt.Errorf("扩展格式不支持：CRX version %d", version)
	}
	if int(offset)+4 > len(data) || !bytes.Equal(data[offset:offset+4], []byte("PK\x03\x04")) {
		return nil, fmt.Errorf("CRX 解包失败：未找到 ZIP 数据")
	}
	return data[offset:], nil
}

func unzipBytes(data []byte, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("扩展 ZIP 解析失败：%w", err)
	}
	cleanDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cleanDest, 0755); err != nil {
		return err
	}
	for _, f := range zr.File {
		name := filepath.Clean(f.Name)
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		target := filepath.Join(cleanDest, name)
		absTarget, err := filepath.Abs(target)
		if err != nil || !strings.HasPrefix(absTarget, cleanDest+string(os.PathSeparator)) && absTarget != cleanDest {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(absTarget, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(absTarget), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(absTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		closeErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func (a *App) bindExtensionDirToProfiles(profileIds []string, extDir string) ([]string, error) {
	a.browserMgr.Mutex.Lock()
	defer a.browserMgr.Mutex.Unlock()
	updated := make([]string, 0, len(profileIds))
	for _, id := range profileIds {
		profile, ok := a.browserMgr.Profiles[id]
		if !ok || profile == nil {
			continue
		}
		profile.LaunchArgs = addExtensionDirToLaunchArgs(profile.LaunchArgs, extDir)
		profile.UpdatedAt = time.Now().Format(time.RFC3339)
		updated = append(updated, id)
	}
	if len(updated) == 0 {
		return nil, fmt.Errorf("未找到可更新的实例")
	}
	if err := a.browserMgr.SaveProfiles(); err != nil {
		return nil, fmt.Errorf("保存实例扩展配置失败：%w", err)
	}
	return updated, nil
}

// readManifestNameFromDir 从已解包的扩展目录里读 manifest.name（用于 toast 提示）。
func readManifestNameFromDir(extDir string) string {
	data, err := os.ReadFile(filepath.Join(extDir, "manifest.json"))
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	if name, _ := m["name"].(string); name != "" {
		return strings.TrimSpace(name)
	}
	return ""
}

// InstallExtensionFromCRXURL 实现 launchcode.ExtensionInstaller。
// helper 扩展走 LaunchServer 调进来：拉 .crx → 解包 → 绑定到指定 profile。
// 返回的 extName 给 helper 弹 toast 用，失败时也保证 extID 至少能给个 fallback。
func (a *App) InstallExtensionFromCRXURL(profileID string, crxURL string) (string, string, error) {
	if a == nil || a.browserMgr == nil {
		return "", "", fmt.Errorf("浏览器管理器未初始化")
	}
	profileID = strings.TrimSpace(profileID)
	crxURL = strings.TrimSpace(crxURL)
	if profileID == "" {
		return "", "", fmt.Errorf("profileId 为空")
	}
	if crxURL == "" {
		return "", "", fmt.Errorf("crxUrl 为空")
	}

	extID := extractExtensionID(crxURL)
	payload, err := downloadExtensionPayload(crxURL)
	if err != nil {
		return extID, "", err
	}
	zipPayload, err := extractZipPayloadFromCRX(payload)
	if err != nil {
		return extID, "", err
	}
	if extID == "" {
		sum := sha256.Sum256(payload)
		extID = "external-" + hex.EncodeToString(sum[:])[:16]
	}
	extDir := filepath.Join(a.appRoot, "extensions", "imported", safePathName(extID))
	if err := os.RemoveAll(extDir); err != nil {
		return extID, "", fmt.Errorf("清理旧扩展目录失败：%w", err)
	}
	if err := unzipBytes(zipPayload, extDir); err != nil {
		return extID, "", err
	}
	if _, err := os.Stat(filepath.Join(extDir, "manifest.json")); err != nil {
		return extID, "", fmt.Errorf("扩展解包失败：未找到 manifest.json")
	}
	if _, err := a.bindExtensionDirToProfiles([]string{profileID}, extDir); err != nil {
		return extID, "", err
	}
	return extID, readManifestNameFromDir(extDir), nil
}

func addExtensionDirToLaunchArgs(args []string, extDir string) []string {
	return normalizeLoadExtensionArgs(append(append([]string{}, args...), "--load-extension="+extDir))
}

func removeExtensionDirFromLaunchArgs(args []string, extDir string) ([]string, bool) {
	target := normalizeExtensionPath(extDir)
	if target == "" {
		return append([]string{}, args...), false
	}
	out := make([]string, 0, len(args))
	changed := false
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if !strings.HasPrefix(trimmed, "--load-extension=") {
			out = append(out, arg)
			continue
		}
		kept := make([]string, 0)
		for _, part := range strings.Split(strings.TrimSpace(strings.TrimPrefix(trimmed, "--load-extension=")), ",") {
			part = strings.TrimSpace(strings.Trim(part, `"`))
			if part == "" {
				continue
			}
			if normalizeExtensionPath(part) == target {
				changed = true
				continue
			}
			kept = append(kept, part)
		}
		if len(kept) > 0 {
			out = append(out, "--load-extension="+strings.Join(kept, ","))
		}
	}
	return normalizeLoadExtensionArgs(out), changed
}

func hasExtensionDirInLaunchArgs(args []string, extDir string) bool {
	target := normalizeExtensionPath(extDir)
	if target == "" {
		return false
	}
	for _, part := range activeLoadExtensionDirs(args) {
		if normalizeExtensionPath(part) == target {
			return true
		}
	}
	return false
}

func normalizeLoadExtensionArgs(args []string) []string {
	out := make([]string, 0, len(args))
	exts := []string{}
	seen := map[string]bool{}
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if strings.HasPrefix(trimmed, "--load-extension=") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "--load-extension="))
			for _, part := range strings.Split(value, ",") {
				part = strings.TrimSpace(part)
				if part == "" || seen[part] {
					continue
				}
				seen[part] = true
				exts = append(exts, part)
			}
			continue
		}
		out = append(out, arg)
	}
	if len(exts) > 0 {
		out = append(out, "--load-extension="+strings.Join(exts, ","))
	}
	return out
}

// cleanupStaleManagedUnpackedExtensions removes stale unpacked-extension records
// left in Chrome profile Preferences after a managed --load-extension path was
// changed or removed. Without this, Chrome can show both the old profile copy and
// the current --load-extension copy in the toolbar extension list.
func cleanupStaleManagedUnpackedExtensions(userDataDir string, args []string, appRoot string) {
	active := activeLoadExtensionDirs(args)
	if len(active) == 0 {
		return
	}
	activeNames := map[string]bool{}
	activeIDs := map[string]bool{}
	for _, dir := range active {
		activeIDs[strings.ToLower(filepath.Base(dir))] = true
		if name := readExtensionManifestName(dir); name != "" {
			activeNames[name] = true
		}
	}
	managedRoots := []string{
		normalizeExtensionPath(filepath.Join(appRoot, "extensions", "imported")),
		normalizeExtensionPath(filepath.Join(appRoot, "data", "stealth_header_ext")),
	}

	for _, prefPath := range chromeProfilePreferencePaths(userDataDir) {
		cleanupStaleUnpackedExtensionsInPreferences(prefPath, active, activeNames, activeIDs, managedRoots)
	}
}

func cleanupRemovedManagedExtension(userDataDir string, extDir string, extID string, appRoot string) {
	normExtDir := normalizeExtensionPath(extDir)
	if userDataDir == "" || normExtDir == "" {
		return
	}
	managedRoots := []string{
		normalizeExtensionPath(filepath.Join(appRoot, "extensions", "imported")),
		normalizeExtensionPath(filepath.Join(appRoot, "data", "stealth_header_ext")),
	}
	manifestName := readExtensionManifestName(extDir)
	for _, prefPath := range chromeProfilePreferencePaths(userDataDir) {
		removeManagedExtensionFromPreferences(prefPath, normExtDir, strings.ToLower(strings.TrimSpace(extID)), manifestName, managedRoots)
	}
}

func removeManagedExtensionFromPreferences(prefPath string, normExtDir string, extID string, manifestName string, managedRoots []string) {
	data, err := os.ReadFile(prefPath)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return
	}
	var prefs map[string]any
	if json.Unmarshal(data, &prefs) != nil {
		return
	}
	extensions, ok := prefs["extensions"].(map[string]any)
	if !ok {
		return
	}
	settings, ok := extensions["settings"].(map[string]any)
	if !ok || len(settings) == 0 {
		return
	}
	removedIDs := []string{}
	for id, raw := range settings {
		setting, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		extPath, _ := setting["path"].(string)
		normPath := normalizeExtensionPath(extPath)
		name := extensionSettingManifestName(setting)
		isManagedPath := false
		for _, root := range managedRoots {
			if root != "" && normPath != "" && (normPath == root || strings.HasPrefix(normPath, root+string(os.PathSeparator))) {
				isManagedPath = true
				break
			}
		}
		if (normPath != "" && normPath == normExtDir) ||
			(extID != "" && strings.EqualFold(id, extID)) ||
			(manifestName != "" && name == manifestName && isManagedPath) {
			delete(settings, id)
			removedIDs = append(removedIDs, id)
		}
	}
	if len(removedIDs) == 0 {
		return
	}
	if pinnedRaw, ok := extensions["pinned_extensions"]; ok {
		extensions["pinned_extensions"] = removePinnedExtensionIDs(pinnedRaw, removedIDs)
	}
	if out, err := json.MarshalIndent(prefs, "", "   "); err == nil {
		_ = os.WriteFile(prefPath, out, 0644)
	}
	profileDir := filepath.Dir(prefPath)
	for _, id := range removedIDs {
		removeStaleExtensionProfileData(profileDir, id)
	}
}

func removePinnedExtensionIDs(pinnedRaw any, removedIDs []string) []string {
	removed := map[string]bool{}
	for _, id := range removedIDs {
		removed[strings.ToLower(strings.TrimSpace(id))] = true
	}
	values := []string{}
	switch v := pinnedRaw.(type) {
	case []string:
		values = append(values, v...)
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	}
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if removed[strings.ToLower(strings.TrimSpace(value))] {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func activeLoadExtensionDirs(args []string) map[string]string {
	out := map[string]string{}
	const prefix = "--load-extension="
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if !strings.HasPrefix(strings.ToLower(trimmed), prefix) {
			continue
		}
		value := strings.TrimSpace(trimmed[len(prefix):])
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(strings.Trim(part, `"`))
			if part == "" {
				continue
			}
			out[normalizeExtensionPath(part)] = part
		}
	}
	return out
}

func chromeProfilePreferencePaths(userDataDir string) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(path string) {
		clean := filepath.Clean(path)
		key := strings.ToLower(clean)
		if !seen[key] {
			seen[key] = true
			out = append(out, clean)
		}
	}
	add(filepath.Join(userDataDir, "Preferences"))
	add(filepath.Join(userDataDir, "Default", "Preferences"))
	if entries, err := os.ReadDir(userDataDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == "Default" || strings.HasPrefix(name, "Profile ") || name == "Guest Profile" {
				add(filepath.Join(userDataDir, name, "Preferences"))
			}
		}
	}
	return out
}

func cleanupStaleUnpackedExtensionsInPreferences(prefPath string, active map[string]string, activeNames map[string]bool, activeIDs map[string]bool, managedRoots []string) {
	data, err := os.ReadFile(prefPath)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return
	}
	var prefs map[string]any
	if json.Unmarshal(data, &prefs) != nil {
		return
	}
	extensions, ok := prefs["extensions"].(map[string]any)
	if !ok {
		return
	}
	settings, ok := extensions["settings"].(map[string]any)
	if !ok || len(settings) == 0 {
		return
	}
	removedIDs := []string{}
	for id, raw := range settings {
		setting, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		extPath, _ := setting["path"].(string)
		if strings.TrimSpace(extPath) == "" {
			continue
		}
		normPath := normalizeExtensionPath(extPath)
		if active[normPath] != "" {
			continue
		}
		name := extensionSettingManifestName(setting)
		isManagedPath := false
		for _, root := range managedRoots {
			if root != "" && (normPath == root || strings.HasPrefix(normPath, root+string(os.PathSeparator))) {
				isManagedPath = true
				break
			}
		}
		if isManagedPath || activeIDs[strings.ToLower(id)] || (name != "" && activeNames[name]) {
			delete(settings, id)
			removedIDs = append(removedIDs, id)
		}
	}
	if len(removedIDs) == 0 {
		return
	}
	if out, err := json.MarshalIndent(prefs, "", "   "); err == nil {
		_ = os.WriteFile(prefPath, out, 0644)
	}
	profileDir := filepath.Dir(prefPath)
	for _, id := range removedIDs {
		removeStaleExtensionProfileData(profileDir, id)
	}
}

func normalizeExtensionPath(path string) string {
	path = strings.TrimSpace(strings.Trim(path, `"`))
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return strings.ToLower(filepath.Clean(path))
}

func readExtensionManifestName(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return ""
	}
	var manifest map[string]any
	if json.Unmarshal(data, &manifest) != nil {
		return ""
	}
	name, _ := manifest["name"].(string)
	return strings.TrimSpace(strings.ToLower(name))
}

func extensionSettingManifestName(setting map[string]any) string {
	manifest, ok := setting["manifest"].(map[string]any)
	if !ok {
		return ""
	}
	name, _ := manifest["name"].(string)
	return strings.TrimSpace(strings.ToLower(name))
}

func removeStaleExtensionProfileData(profileDir string, id string) {
	id = strings.TrimSpace(strings.ToLower(id))
	if id == "" {
		return
	}
	for _, rel := range []string{
		filepath.Join("Extensions", id),
		filepath.Join("Local Extension Settings", id),
		filepath.Join("Sync Extension Settings", id),
		filepath.Join("Managed Extension Settings", id),
	} {
		_ = os.RemoveAll(filepath.Join(profileDir, rel))
	}
	for _, pattern := range []string{
		filepath.Join(profileDir, "IndexedDB", "chrome-extension_"+id+"_*"),
		filepath.Join(profileDir, "Service Worker", "ScriptCache", "*"+id+"*"),
	} {
		if matches, err := filepath.Glob(pattern); err == nil {
			for _, match := range matches {
				_ = os.RemoveAll(match)
			}
		}
	}
}

func safePathName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "extension"
	}
	return b.String()
}

// pinAllLoadedExtensionsToToolbar ensures all extensions loaded via --load-extension
// are shown on the Chrome toolbar (not hidden behind the puzzle icon) by adding
// their IDs to the pinned_extensions list in Preferences.
func pinAllLoadedExtensionsToToolbar(userDataDir string, args []string) {
	active := activeLoadExtensionDirs(args)
	if len(active) == 0 {
		return
	}

	// Collect the extension IDs from the loaded directories
	extensionIDs := map[string]bool{}
	for dir := range active {
		extID := strings.ToLower(strings.TrimSpace(filepath.Base(dir)))
		if extID != "" {
			extensionIDs[extID] = true
		}
	}
	if len(extensionIDs) == 0 {
		return
	}

	for _, prefPath := range chromeProfilePreferencePaths(userDataDir) {
		pinExtensionsInPreferences(prefPath, extensionIDs)
	}
}

// pinExtensionsInPreferences adds the given extension IDs to the pinned_extensions
// list in a Chrome Preferences JSON file.
func pinExtensionsInPreferences(prefPath string, extensionIDs map[string]bool) {
	data, err := os.ReadFile(prefPath)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return
	}
	var prefs map[string]any
	if json.Unmarshal(data, &prefs) != nil {
		return
	}

	extensions, ok := prefs["extensions"].(map[string]any)
	if !ok {
		extensions = make(map[string]any)
		prefs["extensions"] = extensions
	}

	// Read the current pinned_extensions list
	pinnedRaw, ok := extensions["pinned_extensions"]
	var pinnedList []string
	if ok {
		switch v := pinnedRaw.(type) {
		case []string:
			pinnedList = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					pinnedList = append(pinnedList, s)
				}
			}
		default:
			pinnedList = []string{}
		}
	}
	pinnedSet := map[string]bool{}
	for _, id := range pinnedList {
		pinnedSet[strings.ToLower(id)] = true
	}

	changed := false
	for id := range extensionIDs {
		if !pinnedSet[id] {
			pinnedList = append(pinnedList, id)
			pinnedSet[id] = true
			changed = true
		}
	}

	// Also include the stealth header extension if it exists in settings
	settings, ok := extensions["settings"].(map[string]any)
	if ok {
		for id := range settings {
			idLower := strings.ToLower(strings.TrimSpace(id))
			if !pinnedSet[idLower] {
				pinnedSet[idLower] = true
				pinnedList = append(pinnedList, idLower)
				changed = true
			}
		}
	}

	if !changed {
		return
	}

	extensions["pinned_extensions"] = pinnedList
	out, err := json.Marshal(prefs)
	if err != nil {
		return
	}
	_ = os.WriteFile(prefPath, out, 0644)
}
