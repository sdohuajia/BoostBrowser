package backend

import (
	"archive/zip"
	"boost-browser/backend/internal/browser"
	"boost-browser/backend/internal/logger"
	internalproxy "boost-browser/backend/internal/proxy"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type moreLoginImportRow struct {
	ProfileName        string
	Platform           string
	PlatformDomain     string
	LoginAccount       string
	LoginPassword      string
	TwoFAKey           string
	PasswordProtection string
	ProfileID          string
	Cookie             string
	ProxyInformation   string
	ProxyNumber        string
	ProfileGroup       string
	ProfileTag         string
	ProfileNote        string
	CustomNumber       string
	UA                 string
	EndToEndEncryption string
}

type xlsxWorkbook struct {
	Sheets []xlsxSheetMeta `xml:"sheets>sheet"`
}

var importedProxyGeoArgsResolver = resolveCloakGeoArgs

type xlsxSheetMeta struct {
	Name string `xml:"name,attr"`
	ID   string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
}

type xlsxRelationships struct {
	Items []xlsxRelationship `xml:"Relationship"`
}

type xlsxRelationship struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
}

type xlsxWorksheet struct {
	Rows []xlsxRow `xml:"sheetData>row"`
}

type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}

type xlsxCell struct {
	Ref          string      `xml:"r,attr"`
	Type         string      `xml:"t,attr"`
	Value        string      `xml:"v"`
	InlineString *xlsxInline `xml:"is"`
}

type xlsxInline struct {
	Texts []string  `xml:"t"`
	Runs  []xlsxRun `xml:"r"`
}

type xlsxRun struct {
	Text string `xml:"t"`
}

type xlsxSharedStrings struct {
	Items []xlsxSharedString `xml:"si"`
}

type xlsxSharedString struct {
	Text string    `xml:"t"`
	Runs []xlsxRun `xml:"r"`
}

func (a *App) BrowserProfileImportMoreLoginXLSX() (map[string]interface{}, error) {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()

	if a.ctx == nil {
		return nil, fmt.Errorf("应用上下文未初始化")
	}

	filePath, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "选择 MoreLogin 导出的环境文件",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "MoreLogin 环境文件 (*.xlsx;*.txt)", Pattern: "*.xlsx;*.txt"},
			{DisplayName: "Excel 文件 (*.xlsx)", Pattern: "*.xlsx"},
			{DisplayName: "文本文件 (*.txt)", Pattern: "*.txt"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("打开文件对话框失败: %w", err)
	}
	if strings.TrimSpace(filePath) == "" {
		return map[string]interface{}{
			"cancelled": true,
			"message":   "已取消导入",
		}, nil
	}

	selectedCacheRoot, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "可选：选择 MoreLogin 本地环境缓存目录（建议选 H:\\.MoreLogin\\cache，取消则自动探测）",
		DefaultDirectory: firstExistingMoreLoginCacheRoot(a.savedMoreLoginCacheRoot()),
	})
	if err != nil {
		return nil, fmt.Errorf("打开缓存目录选择框失败: %w", err)
	}
	selectedCacheRoot = strings.TrimSpace(selectedCacheRoot)
	preview, err := a.previewMoreLoginImport(filePath, selectedCacheRoot)
	if err != nil {
		return nil, err
	}
	if preview["cancelled"] == true {
		return preview, nil
	}
	if selectedCacheRoot == "" {
		return a.importMoreLoginProfilesFromPath(filePath)
	}
	a.persistMoreLoginCacheRoot(selectedCacheRoot)
	return a.importMoreLoginProfilesFromPathWithCacheRoot(filePath, selectedCacheRoot)
}

func (a *App) previewMoreLoginImport(filePath, preferredCacheRoot string) (map[string]interface{}, error) {
	rows, _, err := parseMoreLoginExportFile(filePath)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("未在导出文件中解析到可导入的环境")
	}
	cacheLookup := newMoreLoginCacheLookup(preferredCacheRoot)
	matched := 0
	missingNames := make([]string, 0, 3)
	for _, row := range rows {
		profileName := strings.TrimSpace(row.ProfileName)
		if profileName == "" {
			profileName = strings.TrimSpace(row.ProfileID)
		}
		if _, ok := cacheLookup.findProfileUserDataDir(row.ProfileID); ok {
			matched++
			continue
		}
		if len(missingNames) < 3 && profileName != "" {
			missingNames = append(missingNames, profileName)
		}
	}
	cacheRootLabel := strings.TrimSpace(preferredCacheRoot)
	if cacheRootLabel == "" {
		cacheRootLabel = firstExistingMoreLoginCacheRoot(a.savedMoreLoginCacheRoot())
		if cacheRootLabel == "" {
			cacheRootLabel = "自动探测"
		}
	}
	message := fmt.Sprintf("即将导入 %d 个 MoreLogin 环境。\n\n完整缓存命中：%d\n仅元数据回退：%d\n缓存目录：%s", len(rows), matched, len(rows)-matched, cacheRootLabel)
	if len(missingNames) > 0 {
		message = fmt.Sprintf("%s\n未命中示例：%s", message, strings.Join(missingNames, "、"))
	}
	choice, err := wailsruntime.MessageDialog(a.ctx, wailsruntime.MessageDialogOptions{
		Type:          wailsruntime.QuestionDialog,
		Title:         "确认导入 MoreLogin 环境",
		Message:       message,
		Buttons:       []string{"继续导入", "取消"},
		DefaultButton: "继续导入",
		CancelButton:  "取消",
	})
	if err != nil {
		return nil, fmt.Errorf("打开导入确认框失败: %w", err)
	}
	if !isAffirmativeDialogChoice(choice, "继续导入") {
		return map[string]interface{}{
			"cancelled": true,
			"message":   "已取消导入",
		}, nil
	}
	return map[string]interface{}{
		"cancelled": false,
		"matched":   matched,
		"missing":   len(rows) - matched,
		"cacheRoot": cacheRootLabel,
	}, nil
}

func isAffirmativeDialogChoice(choice string, expected string) bool {
	choice = strings.TrimSpace(choice)
	expected = strings.TrimSpace(expected)
	if choice == "" {
		return false
	}
	if expected != "" && strings.EqualFold(choice, expected) {
		return true
	}
	normalized := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(choice, "（", "("), "）", ")")))
	switch normalized {
	case "yes", "yes(y)", "是", "是(y)", "ok", "确定", "确认", "继续", "继续导入":
		return true
	default:
		return false
	}
}

func (a *App) savedMoreLoginCacheRoot() string {
	if a == nil || a.config == nil {
		return ""
	}
	return strings.TrimSpace(a.config.Browser.LastMoreLoginCacheRoot)
}

func (a *App) persistMoreLoginCacheRoot(root string) {
	root = strings.TrimSpace(root)
	if root == "" || a == nil || a.config == nil {
		return
	}
	cleaned := filepath.Clean(root)
	if strings.EqualFold(strings.TrimSpace(a.config.Browser.LastMoreLoginCacheRoot), cleaned) {
		return
	}
	a.config.Browser.LastMoreLoginCacheRoot = cleaned
	if err := a.config.Save(a.resolveAppPath("config.yaml")); err != nil {
		logger.New("MoreLoginImport").Error("保存 MoreLogin 缓存目录失败", logger.F("error", err))
	}
}

func (a *App) importMoreLoginProfilesFromPath(filePath string) (map[string]interface{}, error) {
	return a.importMoreLoginProfilesFromPathWithCacheRoot(filePath, "")
}

func (a *App) importMoreLoginProfilesFromPathWithCacheRoot(filePath, preferredCacheRoot string) (map[string]interface{}, error) {
	rows, source, err := parseMoreLoginExportFile(filePath)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("未在导出文件中解析到可导入的环境")
	}

	groupNameToID, createdGroups, err := a.ensureImportGroups(rows)
	if err != nil {
		return nil, err
	}
	proxyConfigToID, err := a.ensureImportProxies(rows)
	if err != nil {
		return nil, err
	}

	defaultCoreID := ""
	if core, ok := a.browserMgr.GetDefaultCore(); ok {
		defaultCoreID = strings.TrimSpace(core.CoreId)
	}

	warnings := make([]string, 0, 4)
	warnings = append(warnings, "登录密码、2FA、密码保护等敏感字段当前不会写入实例配置。")
	proxyGeoArgsCache := make(map[string][]string)
	hasImportedCookies := false
	pendingCookieSeeds := make([][]importedCookieSeedEntry, len(rows))
	for i, row := range rows {
		cookies, cookieErr := normalizeImportedCookieSeedEntries(row.Cookie)
		if cookieErr != nil {
			name := strings.TrimSpace(row.ProfileName)
			if name == "" {
				name = fmt.Sprintf("第 %d 行", i+1)
			}
			return nil, fmt.Errorf("解析环境 %q 的 Cookie 失败: %w", name, cookieErr)
		}
		pendingCookieSeeds[i] = cookies
		if len(cookies) > 0 {
			hasImportedCookies = true
		}
	}
	if hasImportedCookies {
		warnings = append(warnings, "Cookie 会在实例首次启动前自动导入浏览器。")
	}

	imported := 0
	skipped := 0
	restoredBrowserData := 0
	metadataOnly := 0
	createdNames := make([]string, 0, len(rows))
	cacheLookup := newMoreLoginCacheLookup(preferredCacheRoot)
	for idx, row := range rows {
		profileName := strings.TrimSpace(row.ProfileName)
		if profileName == "" {
			skipped++
			continue
		}

		normalizedProxyConfig := normalizeImportedProxyConfig(row.ProxyInformation)
		importedFingerprintArgs, importedLaunchArgs := buildImportedProfileArgs(row, importedProxyGeoArgsFor(normalizedProxyConfig, proxyGeoArgsCache))
		profileInput := browser.ProfileInput{
			ProfileName:     profileName,
			UserDataDir:     "",
			CoreId:          defaultCoreID,
			FingerprintArgs: importedFingerprintArgs,
			ProxyId:         proxyConfigToID[normalizedProxyConfig],
			ProxyConfig:     normalizedProxyConfig,
			LaunchArgs:      importedLaunchArgs,
			Tags:            splitImportTags(row.ProfileTag),
			Keywords:        buildImportedKeywordsWithCookies(row, pendingCookieSeeds[idx]),
			GroupId:         groupNameToID[strings.TrimSpace(row.ProfileGroup)],
		}

		createdProfile, err := a.browserMgr.Create(profileInput)
		if err != nil {
			return nil, fmt.Errorf("导入环境 %q 失败: %w", profileName, err)
		}
		if err := writeImportedCookieSeed(createdProfile.UserDataDir, pendingCookieSeeds[idx]); err != nil {
			_ = a.browserMgr.DeleteWithCache(createdProfile.ProfileId, true)
			return nil, fmt.Errorf("导入环境 %q 失败: %w", profileName, err)
		}
		sourceUserDataDir, sourceFound := cacheLookup.findProfileUserDataDir(row.ProfileID)
		if sourceFound {
			destDir := a.browserMgr.ResolveUserDataDir(createdProfile)
			if _, copyErr := copyImportedRuntimeProfileDir(sourceUserDataDir, destDir); copyErr != nil {
				_ = a.browserMgr.DeleteWithCache(createdProfile.ProfileId, true)
				return nil, fmt.Errorf("导入环境 %q 失败: 恢复 MoreLogin 本地浏览器数据失败: %w", profileName, copyErr)
			}
			restoredBrowserData++
		} else {
			metadataOnly++
		}
		imported++
		createdNames = append(createdNames, profileName)
	}

	if imported == 0 {
		return nil, fmt.Errorf("未导入任何环境，请检查 Excel 内容是否有效")
	}
	if restoredBrowserData > 0 {
		warnings = append(warnings, fmt.Sprintf("已从 MoreLogin 本地缓存恢复 %d 个环境的完整浏览器数据目录。", restoredBrowserData))
	}
	if metadataOnly > 0 {
		warnings = append(warnings, fmt.Sprintf("有 %d 个环境未找到对应的 MoreLogin 本地缓存目录，已回退为仅导入元数据 + Cookie seed。", metadataOnly))
	}

	message := fmt.Sprintf("成功导入 %d 个环境", imported)
	if restoredBrowserData > 0 {
		message = fmt.Sprintf("成功导入 %d 个环境，其中 %d 个已恢复完整浏览器数据", imported, restoredBrowserData)
	}
	if skipped > 0 {
		message = fmt.Sprintf("%s，跳过 %d 行空白数据", message, skipped)
	}
	usedCacheRoot := strings.TrimSpace(preferredCacheRoot)
	return map[string]interface{}{
		"cancelled":           false,
		"filePath":            filePath,
		"source":              source,
		"imported":            imported,
		"skipped":             skipped,
		"createdGroups":       createdGroups,
		"profiles":            createdNames,
		"warnings":            warnings,
		"message":             message,
		"restoredBrowserData": restoredBrowserData,
		"metadataOnly":        metadataOnly,
		"cacheRoot":           usedCacheRoot,
	}, nil
}
func parseMoreLoginExportFile(filePath string) ([]moreLoginImportRow, string, error) {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(filePath)))
	switch ext {
	case ".xlsx":
		rows, err := parseMoreLoginExportXLSX(filePath)
		return rows, "morelogin-xlsx", err
	case ".txt":
		rows, err := parseMoreLoginExportTXT(filePath)
		return rows, "morelogin-txt", err
	default:
		return nil, "", fmt.Errorf("暂不支持的导入文件类型: %s", ext)
	}
}

func (a *App) ensureImportGroups(rows []moreLoginImportRow) (map[string]string, []string, error) {
	nameToID := make(map[string]string)
	created := make([]string, 0)
	if a.browserMgr.GroupDAO == nil {
		return nameToID, created, nil
	}

	existing, err := a.browserMgr.GroupDAO.List()
	if err != nil {
		return nil, nil, fmt.Errorf("读取实例分组失败: %w", err)
	}
	for _, group := range existing {
		name := strings.TrimSpace(group.GroupName)
		if name == "" {
			continue
		}
		nameToID[name] = group.GroupId
	}

	pendingNames := make([]string, 0)
	seen := make(map[string]struct{})
	for _, row := range rows {
		name := strings.TrimSpace(row.ProfileGroup)
		if name == "" {
			continue
		}
		if _, ok := nameToID[name]; ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		pendingNames = append(pendingNames, name)
	}
	sort.Strings(pendingNames)

	for _, name := range pendingNames {
		group, err := a.browserMgr.GroupDAO.Create(browser.GroupInput{
			GroupName: name,
			ParentId:  "",
			SortOrder: len(nameToID) + len(created),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("创建分组 %q 失败: %w", name, err)
		}
		nameToID[name] = group.GroupId
		created = append(created, name)
	}

	return nameToID, created, nil
}

func buildImportedProfileArgs(row moreLoginImportRow, proxyGeoArgs []string) ([]string, []string) {
	fingerprintArgs := browser.RandomFingerprintIdentity()
	launchArgs := make([]string, 0, 4)

	platform := strings.ToLower(strings.TrimSpace(row.Platform))
	switch platform {
	case "windows", "win":
		fingerprintArgs = replaceImportedArgValue(fingerprintArgs, "--fingerprint-platform", "windows")
	case "mac", "macos", "darwin":
		fingerprintArgs = replaceImportedArgValue(fingerprintArgs, "--fingerprint-platform", "mac")
	case "linux":
		fingerprintArgs = replaceImportedArgValue(fingerprintArgs, "--fingerprint-platform", "linux")
	case "android":
		fingerprintArgs = replaceImportedArgValue(fingerprintArgs, "--fingerprint-platform", "android")
	}

	if seed := stableImportedFingerprintSeed(strings.TrimSpace(row.ProfileID)); seed > 0 {
		fingerprintArgs = replaceImportedArgValue(fingerprintArgs, "--fingerprint", fmt.Sprintf("%d", seed))
	}
	for _, arg := range proxyGeoArgs {
		key := launchArgKey(arg)
		if key == "--fingerprint-timezone" || key == "--timezone" {
			fingerprintArgs = replaceImportedFullArg(fingerprintArgs, "--fingerprint-timezone", arg)
			continue
		}
		if key == "--lang" || key == "--accept-lang" {
			launchArgs = replaceImportedFullArg(launchArgs, key, arg)
		}
	}

	ua := strings.TrimSpace(row.UA)
	if ua != "" {
		launchArgs = append(launchArgs, "--user-agent="+ua)
	}
	return fingerprintArgs, launchArgs
}

func replaceImportedArgValue(args []string, key, value string) []string {
	return replaceImportedFullArg(args, strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(key)+"="+strings.TrimSpace(value))
}

func replaceImportedFullArg(args []string, key, fullArg string) []string {
	key = strings.ToLower(strings.TrimSpace(key))
	fullArg = strings.TrimSpace(fullArg)
	if fullArg == "" {
		return args
	}
	replaced := false
	for i, existing := range args {
		if launchArgKey(existing) != key {
			continue
		}
		if !replaced {
			args[i] = fullArg
			replaced = true
			continue
		}
		args = append(args[:i], args[i+1:]...)
		i--
	}
	if !replaced {
		args = append(args, fullArg)
	}
	return args
}

func importedProxyGeoArgsFor(proxyConfig string, cache map[string][]string) []string {
	proxyConfig = strings.TrimSpace(proxyConfig)
	if proxyConfig == "" {
		return nil
	}
	if cached, ok := cache[proxyConfig]; ok {
		return append([]string{}, cached...)
	}
	resolved := importedProxyGeoArgsResolver(proxyConfig)
	cache[proxyConfig] = append([]string{}, resolved...)
	return append([]string{}, resolved...)
}

func buildImportedFingerprintArgs(row moreLoginImportRow) []string {
	args, _ := buildImportedProfileArgs(row, nil)
	return args
}

func buildImportedLaunchArgs(row moreLoginImportRow) []string {
	_, args := buildImportedProfileArgs(row, nil)
	return args
}

func splitImportTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '，', ';', '；', '\n', '\r', '\t':
			return true
		default:
			return false
		}
	})
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{})
	for _, item := range parts {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func buildImportedKeywords(row moreLoginImportRow) []string {
	return buildImportedKeywordsWithCookies(row, nil)
}

func buildImportedKeywordsWithCookies(row moreLoginImportRow, cookies []importedCookieSeedEntry) []string {
	result := make([]string, 0, 16)
	seen := make(map[string]struct{})
	appendKeyword := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	appendSplitKeywords := func(value string) {
		for _, item := range splitImportTags(value) {
			appendKeyword(item)
		}
	}

	appendKeyword(row.ProfileID)
	appendKeyword(row.Platform)
	appendKeyword(row.PlatformDomain)
	appendKeyword(row.LoginAccount)
	appendKeyword(row.ProfileGroup)
	appendSplitKeywords(row.ProfileTag)
	appendKeyword(row.ProfileNote)
	appendKeyword(row.CustomNumber)
	appendKeyword(row.EndToEndEncryption)
	for _, item := range collectImportedCookieKeywordHints(cookies) {
		appendKeyword(item)
	}

	return result
}

func collectImportedCookieKeywordHints(cookies []importedCookieSeedEntry) []string {
	if len(cookies) == 0 {
		return nil
	}
	result := make([]string, 0, len(cookies))
	seen := make(map[string]struct{})
	appendHint := func(value string) {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	for _, cookie := range cookies {
		domain := normalizeImportedCookieKeywordDomain(cookie.Domain)
		if domain == "" && strings.TrimSpace(cookie.URL) != "" {
			if parsed, err := url.Parse(strings.TrimSpace(cookie.URL)); err == nil {
				domain = normalizeImportedCookieKeywordDomain(parsed.Hostname())
			}
		}
		if domain == "" {
			continue
		}
		appendHint(domain)
		appendHint(importedCookieRegistrableDomain(domain))
	}
	return result
}

func normalizeImportedCookieKeywordDomain(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, ".")
	return strings.TrimSpace(value)
}

func importedCookieRegistrableDomain(host string) string {
	host = normalizeImportedCookieKeywordDomain(host)
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func (a *App) ensureImportProxies(rows []moreLoginImportRow) (map[string]string, error) {
	result := make(map[string]string)
	existing := a.getLatestProxies()
	configToProxy := make(map[string]BrowserProxy, len(existing))
	maxSortOrder := -1
	for _, item := range existing {
		cfg := strings.TrimSpace(item.ProxyConfig)
		if cfg != "" {
			configToProxy[cfg] = item
		}
		if item.SortOrder > maxSortOrder {
			maxSortOrder = item.SortOrder
		}
	}
	created := make([]BrowserProxy, 0)

	for _, row := range rows {
		normalized := normalizeImportedProxyConfig(row.ProxyInformation)
		if normalized == "" {
			continue
		}
		if hit, ok := configToProxy[normalized]; ok {
			result[normalized] = strings.TrimSpace(hit.ProxyId)
			continue
		}

		maxSortOrder++
		proxyID := generateUUID()
		proxyItem := BrowserProxy{
			ProxyId:     proxyID,
			ProxyName:   buildImportedProxyName(row),
			ProxyConfig: normalized,
			GroupName:   "MoreLogin 导入",
			SortOrder:   maxSortOrder,
		}
		if a.browserMgr.ProxyDAO != nil {
			if err := a.browserMgr.ProxyDAO.Upsert(proxyItem); err != nil {
				return nil, fmt.Errorf("写入导入代理失败: %w", err)
			}
		} else {
			created = append(created, proxyItem)
		}
		configToProxy[normalized] = proxyItem
		result[normalized] = proxyID
	}
	if len(created) > 0 {
		a.config.Browser.Proxies = append(a.config.Browser.Proxies, created...)
		a.browserMgr.Config.Browser.Proxies = append(a.browserMgr.Config.Browser.Proxies, created...)
	}

	return result, nil
}

func buildImportedProxyName(row moreLoginImportRow) string {
	proxyNumber := strings.TrimSpace(row.ProxyNumber)
	profileName := strings.TrimSpace(row.ProfileName)
	if proxyNumber != "" {
		if profileName != "" {
			return fmt.Sprintf("MoreLogin-%s-%s", proxyNumber, profileName)
		}
		return fmt.Sprintf("MoreLogin-%s", proxyNumber)
	}
	if profileName != "" {
		return fmt.Sprintf("MoreLogin-%s", profileName)
	}
	return "MoreLogin 导入代理"
}

func normalizeImportedProxyConfig(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if normalized, ok := normalizeMoreLoginStandardProxy(raw); ok {
		raw = normalized
	}
	standard, outbound, err := internalproxy.ParseProxyNode(raw)
	if err == nil {
		if strings.TrimSpace(standard) != "" {
			return strings.TrimSpace(standard)
		}
		if outbound != nil {
			data, marshalErr := json.Marshal(outbound)
			if marshalErr == nil {
				return string(data)
			}
		}
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "socks5://") || strings.HasPrefix(lower, "socks://") {
		return raw
	}
	return "http://" + raw
}

func normalizeMoreLoginStandardProxy(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	lower := strings.ToLower(raw)
	var scheme string
	switch {
	case strings.HasPrefix(lower, "socks5://"):
		scheme = "socks5://"
	case strings.HasPrefix(lower, "socks://"):
		scheme = "socks://"
	case strings.HasPrefix(lower, "http://"):
		scheme = "http://"
	case strings.HasPrefix(lower, "https://"):
		scheme = "https://"
	default:
		return "", false
	}
	body := strings.TrimSpace(raw[len(scheme):])
	if body == "" || strings.Contains(body, "@") {
		return "", false
	}
	parts := strings.Split(body, ":")
	if len(parts) != 4 {
		return "", false
	}
	host := strings.TrimSpace(parts[0])
	port := strings.TrimSpace(parts[1])
	username := strings.TrimSpace(parts[2])
	password := strings.TrimSpace(parts[3])
	if host == "" || port == "" || username == "" {
		return "", false
	}
	userInfo := url.UserPassword(username, password).String()
	return fmt.Sprintf("%s%s@%s:%s", scheme, userInfo, host, port), true
}

func parseMoreLoginExportTXT(filePath string) ([]moreLoginImportRow, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取 TXT 失败: %w", err)
	}
	blocks := splitTXTImportBlocks(string(data))
	if len(blocks) == 0 {
		return nil, fmt.Errorf("TXT 中没有可导入的数据块")
	}
	result := make([]moreLoginImportRow, 0, len(blocks))
	for _, block := range blocks {
		item := moreLoginImportRow{
			ProfileName:        block[normalizeImportHeader("Profile name")],
			Platform:           block[normalizeImportHeader("Platform")],
			PlatformDomain:     block[normalizeImportHeader("User-defined platform domain name")],
			LoginAccount:       block[normalizeImportHeader("Login account")],
			LoginPassword:      block[normalizeImportHeader("Login password")],
			TwoFAKey:           block[normalizeImportHeader("2FA key")],
			PasswordProtection: block[normalizeImportHeader("Password protection")],
			ProfileID:          block[normalizeImportHeader("Profile ID")],
			Cookie:             block[normalizeImportHeader("Cookie")],
			ProxyInformation:   block[normalizeImportHeader("Proxy information")],
			ProxyNumber:        block[normalizeImportHeader("Proxy Number")],
			ProfileGroup:       block[normalizeImportHeader("Profile group")],
			ProfileTag:         block[normalizeImportHeader("Profile tag")],
			ProfileNote:        block[normalizeImportHeader("Profile note")],
			CustomNumber:       block[normalizeImportHeader("Custom number")],
			UA:                 block[normalizeImportHeader("UA")],
			EndToEndEncryption: block[normalizeImportHeader("End-to-end encryption")],
		}
		if isMoreLoginImportRowEmpty(item) {
			continue
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("TXT 中没有可导入的有效环境")
	}
	return result, nil
}

func splitTXTImportBlocks(content string) []map[string]string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	segments := strings.Split(content, "\n\n")
	blocks := make([]map[string]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		block := make(map[string]string)
		for _, line := range strings.Split(segment, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			block[normalizeImportHeader(key)] = strings.TrimSpace(value)
		}
		if len(block) > 0 {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

func parseMoreLoginExportXLSX(filePath string) ([]moreLoginImportRow, error) {
	rows, err := readXLSXFirstSheetRows(filePath)
	if err != nil {
		return nil, fmt.Errorf("解析 Excel 失败: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("Excel 中没有可导入的数据行")
	}

	headers := make(map[string]int)
	for idx, header := range rows[0] {
		headers[normalizeImportHeader(header)] = idx
	}
	if _, ok := headers[normalizeImportHeader("Profile name")]; !ok {
		return nil, fmt.Errorf("未识别到 MoreLogin 导出格式：缺少 Profile name 列")
	}

	result := make([]moreLoginImportRow, 0, len(rows)-1)
	for _, row := range rows[1:] {
		item := moreLoginImportRow{
			ProfileName:        xlsxRowValue(row, headers, "Profile name"),
			Platform:           xlsxRowValue(row, headers, "Platform"),
			PlatformDomain:     xlsxRowValue(row, headers, "User-defined platform domain name"),
			LoginAccount:       xlsxRowValue(row, headers, "Login account"),
			LoginPassword:      xlsxRowValue(row, headers, "Login password"),
			TwoFAKey:           xlsxRowValue(row, headers, "2FA key"),
			PasswordProtection: xlsxRowValue(row, headers, "Password protection"),
			ProfileID:          xlsxRowValue(row, headers, "Profile ID"),
			Cookie:             xlsxRowValue(row, headers, "Cookie"),
			ProxyInformation:   xlsxRowValue(row, headers, "Proxy information"),
			ProxyNumber:        xlsxRowValue(row, headers, "Proxy Number"),
			ProfileGroup:       xlsxRowValue(row, headers, "Profile group"),
			ProfileTag:         xlsxRowValue(row, headers, "Profile tag"),
			ProfileNote:        xlsxRowValue(row, headers, "Profile note"),
			CustomNumber:       xlsxRowValue(row, headers, "Custom number"),
			UA:                 xlsxRowValue(row, headers, "UA"),
			EndToEndEncryption: xlsxRowValue(row, headers, "End-to-end encryption"),
		}
		if isMoreLoginImportRowEmpty(item) {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

func isMoreLoginImportRowEmpty(row moreLoginImportRow) bool {
	return strings.TrimSpace(row.ProfileName) == "" &&
		strings.TrimSpace(row.ProfileID) == "" &&
		strings.TrimSpace(row.ProxyInformation) == "" &&
		strings.TrimSpace(row.UA) == ""
}

func normalizeImportHeader(header string) string {
	return strings.ToLower(strings.TrimSpace(header))
}

func xlsxRowValue(row []string, headers map[string]int, key string) string {
	idx, ok := headers[normalizeImportHeader(key)]
	if !ok || idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

func readXLSXFirstSheetRows(filePath string) ([][]string, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	sharedStrings := make([]string, 0)
	if data, err := readZipEntry(&zr.Reader, "xl/sharedStrings.xml"); err == nil {
		sharedStrings, err = parseSharedStrings(data)
		if err != nil {
			return nil, err
		}
	}

	workbookData, err := readZipEntry(&zr.Reader, "xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	relsData, err := readZipEntry(&zr.Reader, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, err
	}

	var workbook xlsxWorkbook
	if err := xml.Unmarshal(workbookData, &workbook); err != nil {
		return nil, err
	}
	if len(workbook.Sheets) == 0 {
		return nil, fmt.Errorf("工作簿中没有工作表")
	}

	var rels xlsxRelationships
	if err := xml.Unmarshal(relsData, &rels); err != nil {
		return nil, err
	}
	relMap := make(map[string]string, len(rels.Items))
	for _, item := range rels.Items {
		relMap[item.ID] = item.Target
	}

	firstSheet := workbook.Sheets[0]
	target := strings.TrimSpace(relMap[firstSheet.ID])
	if target == "" {
		return nil, fmt.Errorf("未找到工作表关系: %s", firstSheet.Name)
	}
	target = strings.TrimPrefix(target, "/")
	if !strings.HasPrefix(target, "xl/") {
		target = filepath.ToSlash(filepath.Join("xl", target))
	}
	worksheetData, err := readZipEntry(&zr.Reader, target)
	if err != nil {
		return nil, err
	}

	var sheet xlsxWorksheet
	if err := xml.Unmarshal(worksheetData, &sheet); err != nil {
		return nil, err
	}

	rows := make([][]string, 0, len(sheet.Rows))
	for _, row := range sheet.Rows {
		parsed := parseWorksheetRow(row, sharedStrings)
		rows = append(rows, parsed)
	}
	return rows, nil
}

func readZipEntry(zr *zip.Reader, name string) ([]byte, error) {
	for _, file := range zr.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("缺少文件: %s", name)
}

func parseSharedStrings(data []byte) ([]string, error) {
	var payload xlsxSharedStrings
	if err := xml.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(payload.Items))
	for _, item := range payload.Items {
		if item.Text != "" {
			result = append(result, item.Text)
			continue
		}
		builder := strings.Builder{}
		for _, run := range item.Runs {
			builder.WriteString(run.Text)
		}
		result = append(result, builder.String())
	}
	return result, nil
}

func parseWorksheetRow(row xlsxRow, sharedStrings []string) []string {
	maxColumn := -1
	values := make(map[int]string, len(row.Cells))
	for _, cell := range row.Cells {
		col := xlsxColumnIndex(cell.Ref)
		if col < 0 {
			continue
		}
		if col > maxColumn {
			maxColumn = col
		}
		values[col] = parseWorksheetCellValue(cell, sharedStrings)
	}
	if maxColumn < 0 {
		return []string{}
	}
	result := make([]string, maxColumn+1)
	for idx, value := range values {
		result[idx] = strings.TrimSpace(value)
	}
	return result
}

func parseWorksheetCellValue(cell xlsxCell, sharedStrings []string) string {
	switch cell.Type {
	case "s":
		index := atoiSafe(strings.TrimSpace(cell.Value))
		if index >= 0 && index < len(sharedStrings) {
			return sharedStrings[index]
		}
		return ""
	case "inlineStr":
		if cell.InlineString == nil {
			return ""
		}
		builder := strings.Builder{}
		for _, text := range cell.InlineString.Texts {
			builder.WriteString(text)
		}
		for _, run := range cell.InlineString.Runs {
			builder.WriteString(run.Text)
		}
		return builder.String()
	default:
		return cell.Value
	}
}

func xlsxColumnIndex(ref string) int {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return -1
	}
	index := 0
	seenLetter := false
	for i := 0; i < len(ref); i++ {
		c := ref[i]
		if c >= 'A' && c <= 'Z' {
			index = index*26 + int(c-'A'+1)
			seenLetter = true
			continue
		}
		if c >= 'a' && c <= 'z' {
			index = index*26 + int(c-'a'+1)
			seenLetter = true
			continue
		}
		break
	}
	if !seenLetter {
		return -1
	}
	return index - 1
}

func atoiSafe(raw string) int {
	value := 0
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return -1
		}
		value = value*10 + int(ch-'0')
	}
	return value
}
