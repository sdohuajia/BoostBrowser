package backend

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"boost-browser/backend/internal/browser"
)

type moreLoginRunningProfile struct {
	ProcessID        int
	ProfileID        string
	ProfileName      string
	Platform         string
	LoginAccount     string
	ProfileGroup     string
	ProfileTag       string
	ProfileNote      string
	CustomNumber     string
	PlatformDomain   string
	UA               string
	ProxyInformation string
	UserDataDir      string
	CommandLine      string
}

type moreLoginRuntimeImportCopyResult struct {
	CopiedFiles  int
	SkippedFiles int
}

func (a *App) BrowserProfileImportRunningMoreLogin() (map[string]interface{}, error) {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()

	profiles, err := discoverMoreLoginRunningProfiles()
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("未发现正在运行的 MoreLogin 环境，请先在 MoreLogin 中打开一个环境后再导入")
	}
	if len(profiles) > 1 {
		return nil, fmt.Errorf("当前检测到 %d 个正在运行的 MoreLogin 环境，请先只保留一个打开后再导入：%s", len(profiles), strings.Join(summarizeRunningMoreLoginProfiles(profiles), "、"))
	}
	return a.importRunningMoreLoginProfile(profiles[0])
}

func (a *App) importRunningMoreLoginProfile(runtimeProfile moreLoginRunningProfile) (map[string]interface{}, error) {
	row := runtimeProfile.asImportRow()
	if strings.TrimSpace(row.ProfileName) == "" {
		return nil, fmt.Errorf("未能识别 MoreLogin 环境名称")
	}
	if strings.TrimSpace(runtimeProfile.UserDataDir) == "" {
		return nil, fmt.Errorf("未能识别 MoreLogin 运行时 profile 目录")
	}

	groupNameToID, createdGroups, err := a.ensureImportGroups([]moreLoginImportRow{row})
	if err != nil {
		return nil, err
	}
	proxyConfigToID, err := a.ensureImportProxies([]moreLoginImportRow{row})
	if err != nil {
		return nil, err
	}

	defaultCoreID := ""
	if core, ok := a.browserMgr.GetDefaultCore(); ok {
		defaultCoreID = strings.TrimSpace(core.CoreId)
	}

	normalizedProxyConfig := normalizeImportedProxyConfig(row.ProxyInformation)
	importedFingerprintArgs, importedLaunchArgs := buildImportedProfileArgs(row, importedProxyGeoArgsResolver(normalizedProxyConfig))
	profileInput := browser.ProfileInput{
		ProfileName:     row.ProfileName,
		UserDataDir:     "",
		CoreId:          defaultCoreID,
		FingerprintArgs: importedFingerprintArgs,
		ProxyId:         proxyConfigToID[normalizedProxyConfig],
		ProxyConfig:     normalizedProxyConfig,
		LaunchArgs:      mergeUniqueLaunchArgs(importedLaunchArgs, buildImportedRunningLaunchArgs(runtimeProfile)),
		Tags:            splitImportTags(row.ProfileTag),
		Keywords:        buildImportedKeywords(row),
		GroupId:         groupNameToID[strings.TrimSpace(row.ProfileGroup)],
	}

	createdProfile, err := a.browserMgr.Create(profileInput)
	if err != nil {
		return nil, fmt.Errorf("导入运行中环境 %q 失败: %w", row.ProfileName, err)
	}

	destDir := a.browserMgr.ResolveUserDataDir(createdProfile)
	copyResult, copyErr := copyImportedRuntimeProfileDir(runtimeProfile.UserDataDir, destDir)
	if copyErr != nil {
		_ = a.browserMgr.DeleteWithCache(createdProfile.ProfileId, true)
		return nil, fmt.Errorf("导入运行中环境 %q 失败: %w", row.ProfileName, copyErr)
	}

	warnings := []string{
		"已复制 MoreLogin 运行中的浏览器数据目录；首次建议先单独启动导入后的 Boost Browser 实例检查登录态。",
	}
	if copyResult.SkippedFiles > 0 {
		warnings = append(warnings, fmt.Sprintf("导入时已自动跳过 %d 个被占用或临时文件。", copyResult.SkippedFiles))
	}
	if normalizedProxyConfig == "" {
		warnings = append(warnings, "当前未从运行参数中识别到代理配置；如源环境依赖代理，请导入后手动核对代理绑定。")
	}

	message := fmt.Sprintf("成功导入运行中的 MoreLogin 环境：%s", row.ProfileName)
	return map[string]interface{}{
		"cancelled":     false,
		"source":        "morelogin-running",
		"imported":      1,
		"skipped":       0,
		"createdGroups": createdGroups,
		"profiles":      []string{row.ProfileName},
		"warnings":      warnings,
		"message":       message,
		"copiedFiles":   copyResult.CopiedFiles,
		"skippedFiles":  copyResult.SkippedFiles,
		"userDataDir":   runtimeProfile.UserDataDir,
		"profileId":     runtimeProfile.ProfileID,
	}, nil
}

func (p moreLoginRunningProfile) asImportRow() moreLoginImportRow {
	profileName := strings.TrimSpace(p.ProfileName)
	if profileName == "" {
		profileName = defaultRunningMoreLoginProfileName(p.ProfileID)
	}
	groupName := strings.TrimSpace(p.ProfileGroup)
	if groupName == "" {
		groupName = "MoreLogin 运行中导入"
	}
	return moreLoginImportRow{
		ProfileName:      profileName,
		Platform:         strings.TrimSpace(p.Platform),
		PlatformDomain:   strings.TrimSpace(p.PlatformDomain),
		LoginAccount:     strings.TrimSpace(p.LoginAccount),
		ProfileID:        strings.TrimSpace(p.ProfileID),
		ProxyInformation: strings.TrimSpace(p.ProxyInformation),
		ProfileGroup:     groupName,
		ProfileTag:       strings.TrimSpace(p.ProfileTag),
		ProfileNote:      strings.TrimSpace(p.ProfileNote),
		CustomNumber:     strings.TrimSpace(p.CustomNumber),
		UA:               strings.TrimSpace(p.UA),
	}
}

func buildImportedRunningFingerprintArgs(profile moreLoginRunningProfile) []string {
	row := profile.asImportRow()
	args := buildImportedFingerprintArgs(row)
	if len(args) == 0 {
		args = append(args, "--fingerprint-platform=windows")
	}
	seed := stableImportedFingerprintSeed(profile.ProfileID)
	args = append(args, fmt.Sprintf("--fingerprint=%d", seed))
	return args
}

func buildImportedRunningLaunchArgs(profile moreLoginRunningProfile) []string {
	ua := strings.TrimSpace(profile.UA)
	if ua == "" {
		return nil
	}
	return []string{"--user-agent=" + ua}
}

func stableImportedFingerprintSeed(profileID string) uint32 {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return 1
	}
	seed := crc32.ChecksumIEEE([]byte(profileID)) & 0x7fffffff
	if seed == 0 {
		return 1
	}
	return seed
}

func summarizeRunningMoreLoginProfiles(profiles []moreLoginRunningProfile) []string {
	result := make([]string, 0, len(profiles))
	for _, item := range profiles {
		name := strings.TrimSpace(item.ProfileName)
		if name == "" {
			name = defaultRunningMoreLoginProfileName(item.ProfileID)
		}
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func defaultRunningMoreLoginProfileName(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return "MoreLogin-运行中环境"
	}
	return "MoreLogin-" + profileID
}

func copyImportedRuntimeProfileDir(src, dst string) (moreLoginRuntimeImportCopyResult, error) {
	var result moreLoginRuntimeImportCopyResult
	src = strings.TrimSpace(src)
	dst = strings.TrimSpace(dst)
	if src == "" || dst == "" {
		return result, fmt.Errorf("运行时 profile 目录无效")
	}
	info, err := os.Stat(src)
	if err != nil {
		return result, fmt.Errorf("读取运行时 profile 目录失败: %w", err)
	}
	if !info.IsDir() {
		return result, fmt.Errorf("运行时 profile 路径不是目录: %s", src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return result, fmt.Errorf("创建导入目标目录失败: %w", err)
	}

	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := dst
		if rel != "." {
			target = filepath.Join(dst, rel)
		}
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			return os.MkdirAll(target, 0o755)
		}
		if shouldSkipImportedRuntimeFile(rel) {
			result.SkippedFiles++
			return nil
		}
		if err := copyImportedRuntimeFile(path, target); err != nil {
			if shouldIgnoreImportedRuntimeCopyError(rel, err) {
				result.SkippedFiles++
				return nil
			}
			return err
		}
		result.CopiedFiles++
		return nil
	})
	if walkErr != nil {
		return result, walkErr
	}
	return result, nil
}

func copyImportedRuntimeFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func shouldSkipImportedRuntimeFile(rel string) bool {
	rel = filepath.ToSlash(strings.ToLower(strings.TrimSpace(rel)))
	if rel == "" {
		return false
	}
	base := strings.ToLower(filepath.Base(rel))
	if base == "singletonlock" || base == "singletoncookie" || base == "singletonsocket" {
		return true
	}
	if base == ".boost-browser-import-cookies.json" {
		return true
	}
	if base == "lock" || strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".tmp") {
		return true
	}
	skipDirMarkers := []string{
		"/cache/",
		"/code cache/",
		"/gpucache/",
		"/shadercache/",
		"/grshadercache/",
		"/graphitedawncache/",
		"/crashpad/",
		"/browsermetrics/",
		"/deferredbrowsermetrics/",
		"/component_crx_cache/",
		"/extensions_crx_cache/",
	}
	for _, marker := range skipDirMarkers {
		if strings.Contains(rel, marker) {
			return true
		}
	}
	return false
}

func shouldIgnoreImportedRuntimeCopyError(rel string, err error) bool {
	if shouldSkipImportedRuntimeFile(rel) {
		return true
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(msg, "used by another process") || strings.Contains(msg, "being used by another process") || strings.Contains(msg, "sharing violation") {
		return true
	}
	return false
}

type moreLoginCacheLookup struct {
	roots []string
}

func newMoreLoginCacheLookup(preferredRoots ...string) moreLoginCacheLookup {
	return moreLoginCacheLookup{roots: discoverMoreLoginCacheRoots(preferredRoots...)}
}

func (l moreLoginCacheLookup) findProfileUserDataDir(profileID string) (string, bool) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return "", false
	}
	for _, root := range l.roots {
		for _, candidate := range moreLoginCacheProfileCandidates(root, profileID) {
			info, err := os.Stat(candidate)
			if err != nil || !info.IsDir() {
				continue
			}
			return candidate, true
		}
	}
	return "", false
}

func discoverMoreLoginCacheRoots(preferredRoots ...string) []string {
	seen := make(map[string]struct{})
	roots := make([]string, 0, 12)
	appendRoot := func(path string) {
		path = strings.Trim(strings.TrimSpace(path), `"`)
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		roots = append(roots, path)
	}

	for _, item := range preferredRoots {
		appendRoot(item)
	}
	for _, item := range splitMoreLoginCacheRootEnv(os.Getenv("MORELOGIN_CACHE_ROOTS")) {
		appendRoot(item)
	}

	for _, home := range []string{os.Getenv("USERPROFILE"), os.Getenv("HOME")} {
		home = strings.TrimSpace(home)
		if home == "" {
			continue
		}
		appendRoot(filepath.Join(home, ".MoreLogin", "cache"))
	}

	for drive := 'C'; drive <= 'Z'; drive++ {
		appendRoot(fmt.Sprintf("%c:\\.MoreLogin\\cache", drive))
	}
	appendRoot(filepath.Join(string(os.PathSeparator), ".MoreLogin", "cache"))
	return roots
}

func firstExistingMoreLoginCacheRoot(preferredRoots ...string) string {
	for _, root := range discoverMoreLoginCacheRoots(preferredRoots...) {
		info, err := os.Stat(root)
		if err == nil && info.IsDir() {
			return root
		}
	}
	return ""
}

func splitMoreLoginCacheRootEnv(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, ";", "\n")
	parts := strings.Split(raw, "\n")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return result
}

func moreLoginCacheProfileCandidates(root, profileID string) []string {
	root = strings.Trim(strings.TrimSpace(root), `"`)
	profileID = strings.TrimSpace(profileID)
	if root == "" || profileID == "" {
		return nil
	}
	root = filepath.Clean(root)
	return []string{
		filepath.Join(root, "chrome_"+profileID, "profile"),
		filepath.Join(root, "chrome_"+profileID),
		filepath.Join(root, profileID, "profile"),
		filepath.Join(root, profileID),
	}
}

func looksLikeMoreLoginRuntimeUserDataDir(path string) bool {
	path = filepath.ToSlash(strings.ToLower(strings.Trim(strings.TrimSpace(path), `"`)))
	if path == "" {
		return false
	}
	return strings.Contains(path, "/morelogin/env-kit/chrome_") && strings.HasSuffix(path, "/profile")
}

func extractMoreLoginRuntimeProfileID(path string) string {
	cleaned := strings.Trim(strings.TrimSpace(path), `"`)
	if cleaned == "" {
		return ""
	}
	parent := filepath.Base(filepath.Dir(filepath.Clean(cleaned)))
	if !strings.HasPrefix(strings.ToLower(parent), "chrome_") {
		return ""
	}
	return strings.TrimSpace(parent[len("chrome_"):])
}

func enrichRunningMoreLoginProfile(profile moreLoginRunningProfile) moreLoginRunningProfile {
	profile.ProfileID = strings.TrimSpace(profile.ProfileID)
	if profile.ProfileID == "" {
		profile.ProfileID = extractMoreLoginRuntimeProfileID(profile.UserDataDir)
	}
	meta := loadRunningMoreLoginMetadata(profile.ProfileID, profile.UserDataDir)
	if strings.TrimSpace(profile.ProfileName) == "" {
		profile.ProfileName = firstNonEmpty(meta.ProfileName, defaultRunningMoreLoginProfileName(profile.ProfileID))
	}
	profile.Platform = firstNonEmpty(profile.Platform, meta.Platform)
	profile.LoginAccount = firstNonEmpty(profile.LoginAccount, meta.LoginAccount)
	profile.ProfileGroup = firstNonEmpty(profile.ProfileGroup, meta.ProfileGroup)
	profile.ProfileTag = firstNonEmpty(profile.ProfileTag, meta.ProfileTag)
	profile.ProfileNote = firstNonEmpty(profile.ProfileNote, meta.ProfileNote)
	profile.CustomNumber = firstNonEmpty(profile.CustomNumber, meta.CustomNumber)
	profile.PlatformDomain = firstNonEmpty(profile.PlatformDomain, meta.PlatformDomain)
	profile.UA = firstNonEmpty(profile.UA, meta.UA)
	profile.ProxyInformation = firstNonEmpty(profile.ProxyInformation, meta.ProxyInformation)
	return profile
}

func loadRunningMoreLoginMetadata(profileID, userDataDir string) moreLoginRunningProfile {
	for _, candidate := range runningMoreLoginMetadataCandidates(profileID, userDataDir) {
		data, err := os.ReadFile(candidate)
		if err != nil || len(data) == 0 {
			continue
		}
		var payload any
		if err := json.Unmarshal(data, &payload); err != nil {
			continue
		}
		return moreLoginRunningProfile{
			ProfileName:      readMoreLoginMetadataString(payload, "profileName", "profile_name", "name", "browserName", "browser_name"),
			Platform:         readMoreLoginMetadataString(payload, "platform", "os", "system", "devicePlatform"),
			LoginAccount:     readMoreLoginMetadataString(payload, "loginAccount", "login_account", "account", "username"),
			ProfileGroup:     readMoreLoginMetadataString(payload, "profileGroup", "profile_group", "groupName", "group_name"),
			ProfileTag:       readMoreLoginMetadataString(payload, "profileTag", "profile_tag", "tags", "tag"),
			ProfileNote:      readMoreLoginMetadataString(payload, "profileNote", "profile_note", "note", "remark"),
			CustomNumber:     readMoreLoginMetadataString(payload, "customNumber", "custom_number", "number", "no"),
			PlatformDomain:   readMoreLoginMetadataString(payload, "platformDomain", "platform_domain", "domain", "platformUrl", "platform_url"),
			UA:               readMoreLoginMetadataString(payload, "ua", "userAgent", "user_agent"),
			ProxyInformation: readMoreLoginMetadataString(payload, "proxyInformation", "proxy_information", "proxy", "proxyConfig", "proxy_config"),
		}
	}
	return moreLoginRunningProfile{}
}

func runningMoreLoginMetadataCandidates(profileID, userDataDir string) []string {
	userDataDir = strings.Trim(strings.TrimSpace(userDataDir), `"`)
	profileID = strings.TrimSpace(profileID)
	paths := make([]string, 0, 4)
	if userDataDir != "" {
		parent := filepath.Dir(filepath.Clean(userDataDir))
		paths = append(paths,
			filepath.Join(parent, "env.json"),
			filepath.Join(filepath.Dir(parent), "env.json"),
		)
		if profileID != "" {
			baseRoot := filepath.Dir(filepath.Dir(parent))
			paths = append(paths,
				filepath.Join(baseRoot, "sdk", "cache", "chrome_"+profileID, "env.json"),
				filepath.Join(baseRoot, "sdk", "cache", profileID, "env.json"),
			)
		}
	}
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, item := range paths {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		item = filepath.Clean(item)
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func readMoreLoginMetadataString(value any, keys ...string) string {
	normalized := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		normalized[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	return strings.TrimSpace(readMoreLoginMetadataStringRecursive(value, normalized))
}

func readMoreLoginMetadataStringRecursive(value any, keys map[string]struct{}) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if _, ok := keys[strings.ToLower(strings.TrimSpace(key))]; ok {
				if text := stringifyMoreLoginMetadataValue(item); text != "" {
					return text
				}
			}
		}
		for _, item := range typed {
			if text := readMoreLoginMetadataStringRecursive(item, keys); text != "" {
				return text
			}
		}
	case []any:
		for _, item := range typed {
			if text := readMoreLoginMetadataStringRecursive(item, keys); text != "" {
				return text
			}
		}
	}
	return ""
}

func stringifyMoreLoginMetadataValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := stringifyMoreLoginMetadataValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ",")
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
