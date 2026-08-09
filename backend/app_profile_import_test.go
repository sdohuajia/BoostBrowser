package backend

import (
	"archive/zip"
	"boost-browser/backend/internal/browser"
	"boost-browser/backend/internal/config"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestImportMoreLoginProfilesFromPath_TXTCreatesProfilesFromExportData(t *testing.T) {
	app := newImportTestApp(t)
	importFile := filepath.Join(t.TempDir(), "export_profile.txt")
	mustWriteFile(t, importFile, strings.Join([]string{
		"Profile name=P-142",
		"Platform=Windows",
		"User-defined platform domain name=amazon.com",
		"Login account=buyer-142",
		"Profile ID=2044430739248320512",
		"Proxy information=socks5://127.0.0.1:1080",
		"UA=Mozilla/5.0 Test",
		"Profile group=Group A",
		"Profile tag=tag-1,tag-2",
		"Profile note=important buyer profile",
		"Custom number=NO-142",
		"End-to-end encryption=enabled",
		"",
	}, "\n"))

	result, err := app.importMoreLoginProfilesFromPath(importFile)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if got := result["imported"]; got != 1 {
		t.Fatalf("imported mismatch: %#v", got)
	}
	if got := result["source"]; got != "morelogin-txt" {
		t.Fatalf("source mismatch: %#v", got)
	}
	if got := result["profiles"]; !equalStringSlice(got, []string{"P-142"}) {
		t.Fatalf("profiles mismatch: %#v", got)
	}

	profile := onlyImportedProfile(t, app.browserMgr)
	if profile.ProfileName != "P-142" {
		t.Fatalf("profile name mismatch: %q", profile.ProfileName)
	}
	if profile.ProxyConfig != "socks5://127.0.0.1:1080" {
		t.Fatalf("proxy config mismatch: %q", profile.ProxyConfig)
	}
	if profile.ProxyId == "" {
		t.Fatalf("expected imported proxy to bind into proxy pool")
	}
	assertProxyExists(t, app, profile.ProxyId, "socks5://127.0.0.1:1080")
	if len(profile.LaunchArgs) != 1 || profile.LaunchArgs[0] != "--user-agent=Mozilla/5.0 Test" {
		t.Fatalf("launch args mismatch: %#v", profile.LaunchArgs)
	}
	if !equalStringSlice(profile.Tags, []string{"tag-1", "tag-2"}) {
		t.Fatalf("tags mismatch: %#v", profile.Tags)
	}
	if !containsAll(profile.FingerprintArgs, []string{"--fingerprint-platform=windows"}) {
		t.Fatalf("fingerprint args mismatch: %#v", profile.FingerprintArgs)
	}
	if !hasArgPrefix(profile.FingerprintArgs, "--fingerprint=") {
		t.Fatalf("expected stable fingerprint seed, got: %#v", profile.FingerprintArgs)
	}
	if !equalStringSliceAnyOrder(profile.Keywords, []string{"2044430739248320512", "Windows", "amazon.com", "buyer-142", "Group A", "tag-1", "tag-2", "important buyer profile", "NO-142", "enabled"}) {
		t.Fatalf("keywords mismatch: %#v", profile.Keywords)
	}
}

func TestImportMoreLoginProfilesFromPath_TXTAlignsFingerprintAndLanguageToProxy(t *testing.T) {
	app := newImportTestApp(t)
	withImportedProxyGeoArgsResolver(func(proxy string) []string {
		if proxy != "socks5://127.0.0.1:1080" {
			t.Fatalf("unexpected proxy passed to resolver: %q", proxy)
		}
		return []string{
			"--fingerprint-timezone=Asia/Tokyo",
			"--lang=ja-JP",
			"--accept-lang=ja-JP,ja;q=0.9,en-US;q=0.8,en;q=0.7",
		}
	}, func() {
		importFile := filepath.Join(t.TempDir(), "export_profile.txt")
		mustWriteFile(t, importFile, strings.Join([]string{
			"Profile name=P-geo",
			"Platform=Windows",
			"Profile ID=2044430739248320555",
			"Proxy information=socks5://127.0.0.1:1080",
			"UA=Mozilla/5.0 Geo",
			"",
		}, "\n"))

		if _, err := app.importMoreLoginProfilesFromPath(importFile); err != nil {
			t.Fatalf("import failed: %v", err)
		}
	})

	profile := onlyImportedProfile(t, app.browserMgr)
	if !containsAll(profile.FingerprintArgs, []string{"--fingerprint-platform=windows", "--fingerprint-timezone=Asia/Tokyo"}) {
		t.Fatalf("fingerprint args mismatch: %#v", profile.FingerprintArgs)
	}
	if !hasArgPrefix(profile.FingerprintArgs, "--fingerprint=") {
		t.Fatalf("expected stable fingerprint seed, got: %#v", profile.FingerprintArgs)
	}
	if !containsAll(profile.LaunchArgs, []string{"--lang=ja-JP", "--accept-lang=ja-JP,ja;q=0.9,en-US;q=0.8,en;q=0.7", "--user-agent=Mozilla/5.0 Geo"}) {
		t.Fatalf("launch args mismatch: %#v", profile.LaunchArgs)
	}
}

func TestImportMoreLoginProfilesFromPath_TXTWritesCookieSeedForFirstLaunch(t *testing.T) {
	app := newImportTestApp(t)
	importFile := filepath.Join(t.TempDir(), "export_profile.txt")
	mustWriteFile(t, importFile, strings.Join([]string{
		"Profile name=P-142",
		"Profile ID=2044430739248320512",
		"Cookie=[{\"name\":\"session\",\"value\":\"abc\",\"domain\":\".claude.ai\",\"path\":\"/\",\"secure\":true,\"http_only\":true,\"expires\":\"2027-05-27T00:23:10+08:00\",\"same_site\":\"0\"}]",
		"Custom number=0",
		"End-to-end encryption=Disable",
		"",
	}, "\n"))

	if _, err := app.importMoreLoginProfilesFromPath(importFile); err != nil {
		t.Fatalf("import failed: %v", err)
	}

	profile := onlyImportedProfile(t, app.browserMgr)
	if profile.ProxyId != "" {
		t.Fatalf("profile without proxy should not bind proxyId: %q", profile.ProxyId)
	}
	seedCookies, err := loadImportedCookieSeed(profile.UserDataDir)
	if err != nil {
		t.Fatalf("load imported cookie seed: %v", err)
	}
	if len(seedCookies) != 1 {
		t.Fatalf("seed cookie count mismatch: %#v", seedCookies)
	}
	if seedCookies[0].Name != "session" || seedCookies[0].Value != "abc" {
		t.Fatalf("seed cookie mismatch: %#v", seedCookies[0])
	}
	if seedCookies[0].URL != "https://claude.ai/" {
		t.Fatalf("seed cookie url mismatch: %#v", seedCookies[0])
	}
	if seedCookies[0].SameSite != "None" {
		t.Fatalf("seed cookie sameSite mismatch: %#v", seedCookies[0])
	}
	if seedCookies[0].Expires <= 0 {
		t.Fatalf("seed cookie expires mismatch: %#v", seedCookies[0])
	}
	if !equalStringSliceAnyOrder(profile.Keywords, []string{"2044430739248320512", "0", "Disable", "claude.ai"}) {
		t.Fatalf("keywords mismatch: %#v", profile.Keywords)
	}
}

func TestImportMoreLoginProfilesFromPath_TXTRestoresFullBrowserDataFromPreferredCacheRoot(t *testing.T) {
	app := newImportTestApp(t)
	preferredCacheRoot := filepath.Join(t.TempDir(), ".MoreLogin", "cache")
	srcDir := filepath.Join(preferredCacheRoot, "chrome_2044430739248320512", "profile")
	mustMkdirAll(t, filepath.Join(srcDir, "Default", "Network"))
	mustWriteFile(t, filepath.Join(srcDir, "Local State"), `{"profile":{"last_used":"Default"}}`)
	mustWriteFile(t, filepath.Join(srcDir, "Default", "Preferences"), `{"account_info":[{"email":"buyer@example.com"}]}`)
	mustWriteFile(t, filepath.Join(srcDir, "Default", "Network", "Cookies"), "sqlite-cookie-db")
	t.Setenv("MORELOGIN_CACHE_ROOTS", filepath.Join(t.TempDir(), "missing-cache"))

	importFile := filepath.Join(t.TempDir(), "export_profile.txt")
	mustWriteFile(t, importFile, strings.Join([]string{
		"Profile name=P-142",
		"Platform=Windows",
		"Profile ID=2044430739248320512",
		"Proxy information=socks5://127.0.0.1:1080",
		"UA=Mozilla/5.0 Test",
		"",
	}, "\n"))

	result, err := app.importMoreLoginProfilesFromPathWithCacheRoot(importFile, preferredCacheRoot)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if got := result["restoredBrowserData"]; got != 1 {
		t.Fatalf("restoredBrowserData mismatch: %#v", got)
	}
	if got := result["metadataOnly"]; got != 0 {
		t.Fatalf("metadataOnly mismatch: %#v", got)
	}
	if got := result["cacheRoot"]; got != preferredCacheRoot {
		t.Fatalf("cacheRoot mismatch: %#v", got)
	}

	profile := onlyImportedProfile(t, app.browserMgr)
	dstDir := app.browserMgr.ResolveUserDataDir(profile)
	if _, err := os.Stat(filepath.Join(dstDir, "Default", "Network", "Cookies")); err != nil {
		t.Fatalf("expected copied cookies db: %v", err)
	}
}

func TestImportMoreLoginProfilesFromPath_TXTRestoresFullBrowserDataFromLocalCache(t *testing.T) {
	app := newImportTestApp(t)
	cacheRoot := filepath.Join(t.TempDir(), ".MoreLogin", "cache")
	srcDir := filepath.Join(cacheRoot, "chrome_2044430739248320512", "profile")
	mustMkdirAll(t, filepath.Join(srcDir, "Default", "Network"))
	mustWriteFile(t, filepath.Join(srcDir, "Local State"), `{"profile":{"last_used":"Default"}}`)
	mustWriteFile(t, filepath.Join(srcDir, "Default", "Preferences"), `{"account_info":[{"email":"buyer@example.com"}]}`)
	mustWriteFile(t, filepath.Join(srcDir, "Default", "Network", "Cookies"), "sqlite-cookie-db")
	t.Setenv("MORELOGIN_CACHE_ROOTS", cacheRoot)

	importFile := filepath.Join(t.TempDir(), "export_profile.txt")
	mustWriteFile(t, importFile, strings.Join([]string{
		"Profile name=P-142",
		"Platform=Windows",
		"Login account=buyer-142",
		"Profile ID=2044430739248320512",
		"Cookie=[{\"name\":\"session\",\"value\":\"abc\",\"domain\":\".claude.ai\",\"path\":\"/\",\"secure\":true}]",
		"",
	}, "\n"))

	result, err := app.importMoreLoginProfilesFromPath(importFile)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if got := result["restoredBrowserData"]; got != 1 {
		t.Fatalf("restoredBrowserData mismatch: %#v", got)
	}
	if got := result["metadataOnly"]; got != 0 {
		t.Fatalf("metadataOnly mismatch: %#v", got)
	}

	profile := onlyImportedProfile(t, app.browserMgr)
	dstDir := app.browserMgr.ResolveUserDataDir(profile)
	for rel, want := range map[string]string{
		filepath.Join("Local State"):                   `{"profile":{"last_used":"Default"}}`,
		filepath.Join("Default", "Preferences"):        `{"account_info":[{"email":"buyer@example.com"}]}`,
		filepath.Join("Default", "Network", "Cookies"): "sqlite-cookie-db",
	} {
		data, err := os.ReadFile(filepath.Join(dstDir, rel))
		if err != nil {
			t.Fatalf("read restored file %s: %v", rel, err)
		}
		if string(data) != want {
			t.Fatalf("restored file mismatch for %s: %s", rel, string(data))
		}
	}
	seedCookies, err := loadImportedCookieSeed(profile.UserDataDir)
	if err != nil {
		t.Fatalf("load imported cookie seed: %v", err)
	}
	if len(seedCookies) != 1 || seedCookies[0].Name != "session" {
		t.Fatalf("seed cookie mismatch: %#v", seedCookies)
	}
}

func TestImportMoreLoginProfilesFromPath_TXTFallsBackToMetadataOnlyWhenCacheMissing(t *testing.T) {
	app := newImportTestApp(t)
	t.Setenv("MORELOGIN_CACHE_ROOTS", filepath.Join(t.TempDir(), "missing-cache"))
	importFile := filepath.Join(t.TempDir(), "export_profile.txt")
	mustWriteFile(t, importFile, strings.Join([]string{
		"Profile name=P-143",
		"Profile ID=2044430739248320888",
		"Cookie=[{\"name\":\"session\",\"value\":\"xyz\",\"domain\":\".x.com\",\"path\":\"/\",\"secure\":true}]",
		"",
	}, "\n"))

	result, err := app.importMoreLoginProfilesFromPath(importFile)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if got := result["restoredBrowserData"]; got != 0 {
		t.Fatalf("restoredBrowserData mismatch: %#v", got)
	}
	if got := result["metadataOnly"]; got != 1 {
		t.Fatalf("metadataOnly mismatch: %#v", got)
	}
	warnings, ok := result["warnings"].([]string)
	if !ok || !containsStringWith(warnings, "仅导入元数据 + Cookie seed") {
		t.Fatalf("warnings mismatch: %#v", result["warnings"])
	}

	profile := onlyImportedProfile(t, app.browserMgr)
	seedCookies, err := loadImportedCookieSeed(profile.UserDataDir)
	if err != nil {
		t.Fatalf("load imported cookie seed: %v", err)
	}
	if len(seedCookies) != 1 || seedCookies[0].Name != "session" || seedCookies[0].Value != "xyz" {
		t.Fatalf("seed cookie mismatch: %#v", seedCookies)
	}
}

func TestImportMoreLoginProfilesFromPath_XLSXCreatesProfilesFromExportData(t *testing.T) {
	app := newImportTestApp(t)
	importFile := filepath.Join(t.TempDir(), "export_profile.xlsx")
	writeMinimalMoreLoginXLSX(t, importFile, [][]string{
		{"Profile name", "Platform", "User-defined platform domain name", "Login account", "Profile ID", "Proxy information", "UA", "Profile group", "Profile tag", "Profile note", "Custom number", "End-to-end encryption"},
		{"P-201", "Linux", "shop.example", "buyer-201", "2044430739248320999", "http://127.0.0.1:8080", "Mozilla/5.0 XLSX", "Group B", "tag-x;tag-y", "xlsx note", "C-201", "disabled"},
	})

	result, err := app.importMoreLoginProfilesFromPath(importFile)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if got := result["imported"]; got != 1 {
		t.Fatalf("imported mismatch: %#v", got)
	}
	if got := result["source"]; got != "morelogin-xlsx" {
		t.Fatalf("source mismatch: %#v", got)
	}

	profile := onlyImportedProfile(t, app.browserMgr)
	if profile.ProfileName != "P-201" {
		t.Fatalf("profile name mismatch: %q", profile.ProfileName)
	}
	if profile.ProxyConfig != "http://127.0.0.1:8080" {
		t.Fatalf("proxy config mismatch: %q", profile.ProxyConfig)
	}
	if profile.ProxyId == "" {
		t.Fatalf("expected imported xlsx proxy to bind into proxy pool")
	}
	assertProxyExists(t, app, profile.ProxyId, "http://127.0.0.1:8080")
	if len(profile.LaunchArgs) != 1 || profile.LaunchArgs[0] != "--user-agent=Mozilla/5.0 XLSX" {
		t.Fatalf("launch args mismatch: %#v", profile.LaunchArgs)
	}
	if !equalStringSlice(profile.Tags, []string{"tag-x", "tag-y"}) {
		t.Fatalf("tags mismatch: %#v", profile.Tags)
	}
	if !equalStringSliceAnyOrder(profile.Keywords, []string{"2044430739248320999", "Linux", "shop.example", "buyer-201", "Group B", "tag-x", "tag-y", "xlsx note", "C-201", "disabled"}) {
		t.Fatalf("keywords mismatch: %#v", profile.Keywords)
	}
}

func TestImportMoreLoginProfilesFromPath_TXTNormalizesMoreLoginProxyAndImportsCookieDomainsAsKeywords(t *testing.T) {
	app := newImportTestApp(t)
	importFile := filepath.Join(t.TempDir(), "export_profile.txt")
	mustWriteFile(t, importFile, strings.Join([]string{
		"Profile name=P-300",
		"Profile ID=300",
		"Proxy Number=344",
		"Proxy information=socks5://203.0.113.10:1080:user:pass",
		"Cookie=[{\"name\":\"sid\",\"value\":\"1\",\"domain\":\".twitter.com\",\"path\":\"/\",\"secure\":true},{\"name\":\"sid2\",\"value\":\"2\",\"domain\":\"accounts.google.com\",\"path\":\"/\",\"secure\":true}]",
		"",
	}, "\n"))

	if _, err := app.importMoreLoginProfilesFromPath(importFile); err != nil {
		t.Fatalf("import failed: %v", err)
	}

	profile := onlyImportedProfile(t, app.browserMgr)
	if profile.ProxyConfig != "socks5://user:pass@203.0.113.10:1080" {
		t.Fatalf("normalized proxy mismatch: %q", profile.ProxyConfig)
	}
	if profile.ProxyId == "" {
		t.Fatalf("expected proxy id after import")
	}
	assertProxyExists(t, app, profile.ProxyId, "socks5://user:pass@203.0.113.10:1080")
	if !containsAll(profile.Keywords, []string{"twitter.com", "google.com", "accounts.google.com"}) {
		t.Fatalf("expected cookie domain keywords, got: %#v", profile.Keywords)
	}
}

func TestImportRunningMoreLoginProfile_CopiesRuntimeUserDataAndBindsProxy(t *testing.T) {
	app := newImportTestApp(t)
	srcDir := filepath.Join(t.TempDir(), "MoreLogin", "env-kit", "chrome_2044430739248320512", "profile")
	mustMkdirAll(t, filepath.Join(srcDir, "Default", "Local Storage"))
	mustWriteFile(t, filepath.Join(srcDir, "Default", "Preferences"), `{"profile":{"name":"Imported"}}`)
	mustWriteFile(t, filepath.Join(srcDir, "Default", "Local Storage", "leveldb.log"), "runtime-state")

	result, err := app.importRunningMoreLoginProfile(moreLoginRunningProfile{
		ProcessID:        9527,
		ProfileID:        "2044430739248320512",
		ProfileName:      "ML-Running-1",
		Platform:         "Windows",
		LoginAccount:     "buyer-9527",
		PlatformDomain:   "x.com",
		ProxyInformation: "socks5://127.0.0.1:1080",
		UA:               "Mozilla/5.0 Runtime",
		UserDataDir:      srcDir,
	})
	if err != nil {
		t.Fatalf("import running profile failed: %v", err)
	}
	if got := result["source"]; got != "morelogin-running" {
		t.Fatalf("source mismatch: %#v", got)
	}
	if got := result["imported"]; got != 1 {
		t.Fatalf("imported mismatch: %#v", got)
	}

	profile := onlyImportedProfile(t, app.browserMgr)
	if profile.ProfileName != "ML-Running-1" {
		t.Fatalf("profile name mismatch: %q", profile.ProfileName)
	}
	if profile.ProxyId == "" {
		t.Fatalf("expected proxy id after runtime import")
	}
	assertProxyExists(t, app, profile.ProxyId, "socks5://127.0.0.1:1080")
	if !containsAll(profile.Keywords, []string{"2044430739248320512", "buyer-9527", "x.com"}) {
		t.Fatalf("keywords mismatch: %#v", profile.Keywords)
	}
	if len(profile.LaunchArgs) != 1 || profile.LaunchArgs[0] != "--user-agent=Mozilla/5.0 Runtime" {
		t.Fatalf("launch args mismatch: %#v", profile.LaunchArgs)
	}
	if !containsAll(profile.FingerprintArgs, []string{"--fingerprint-platform=windows"}) {
		t.Fatalf("fingerprint args mismatch: %#v", profile.FingerprintArgs)
	}

	dstDir := app.browserMgr.ResolveUserDataDir(profile)
	preferencesPath := filepath.Join(dstDir, "Default", "Preferences")
	data, err := os.ReadFile(preferencesPath)
	if err != nil {
		t.Fatalf("read copied preferences: %v", err)
	}
	if string(data) != `{"profile":{"name":"Imported"}}` {
		t.Fatalf("copied preferences mismatch: %s", string(data))
	}
}

func TestExtractMoreLoginRuntimeProfileID(t *testing.T) {
	got := extractMoreLoginRuntimeProfileID(`C:\Users\testuser\AppData\Local\MoreLogin\env-kit\chrome_2044430739248320512\profile`)
	if got != "2044430739248320512" {
		t.Fatalf("profile id mismatch: %q", got)
	}
}

func TestLooksLikeMoreLoginRuntimeUserDataDir(t *testing.T) {
	if !looksLikeMoreLoginRuntimeUserDataDir(`C:\Users\testuser\AppData\Roaming\MoreLogin\env-kit\chrome_123\profile`) {
		t.Fatalf("expected true")
	}
	if looksLikeMoreLoginRuntimeUserDataDir(`C:\tmp\profile`) {
		t.Fatalf("expected false")
	}
}

func TestIsAffirmativeDialogChoice(t *testing.T) {
	cases := []string{"继续导入", "是", "是(Y)", "Yes", "YES(Y)", "确定"}
	for _, choice := range cases {
		if !isAffirmativeDialogChoice(choice, "继续导入") {
			t.Fatalf("expected affirmative choice for %q", choice)
		}
	}
	for _, choice := range []string{"取消", "否", "No", ""} {
		if isAffirmativeDialogChoice(choice, "继续导入") {
			t.Fatalf("expected negative choice for %q", choice)
		}
	}
}

func newImportTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig()
	return &App{
		config:     cfg,
		appRoot:    root,
		browserMgr: browser.NewManager(cfg, root),
	}
}

func onlyImportedProfile(t *testing.T, mgr *browser.Manager) *browser.Profile {
	t.Helper()
	if len(mgr.Profiles) != 1 {
		t.Fatalf("expected exactly one imported profile, got %d", len(mgr.Profiles))
	}
	for _, profile := range mgr.Profiles {
		return profile
	}
	t.Fatal("profile map unexpectedly empty")
	return nil
}

func assertProxyExists(t *testing.T, app *App, proxyID, wantConfig string) {
	t.Helper()
	list := app.getLatestProxies()
	for _, item := range list {
		if item.ProxyId == proxyID {
			if item.ProxyConfig != wantConfig {
				t.Fatalf("proxy config mismatch: got=%q want=%q", item.ProxyConfig, wantConfig)
			}
			return
		}
	}
	t.Fatalf("proxy %q not found in pool", proxyID)
}

func containsAll(got []string, want []string) bool {
	set := make(map[string]struct{}, len(got))
	for _, item := range got {
		set[item] = struct{}{}
	}
	for _, item := range want {
		if _, ok := set[item]; !ok {
			return false
		}
	}
	return true
}

func containsStringWith(got []string, wantSubstring string) bool {
	for _, item := range got {
		if strings.Contains(item, wantSubstring) {
			return true
		}
	}
	return false
}

func hasArgPrefix(args []string, prefix string) bool {
	for _, item := range args {
		if strings.HasPrefix(strings.TrimSpace(item), prefix) {
			return true
		}
	}
	return false
}

func withImportedProxyGeoArgsResolver(resolver func(string) []string, fn func()) {
	prev := importedProxyGeoArgsResolver
	importedProxyGeoArgsResolver = resolver
	defer func() { importedProxyGeoArgsResolver = prev }()
	fn()
}

func equalStringSlice(got any, want []string) bool {
	slice, ok := got.([]string)
	if !ok {
		return false
	}
	if len(slice) != len(want) {
		return false
	}
	for i := range want {
		if slice[i] != want[i] {
			return false
		}
	}
	return true
}

func equalStringSliceAnyOrder(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	counts := make(map[string]int, len(want))
	for _, item := range want {
		counts[item]++
	}
	for _, item := range got {
		counts[item]--
		if counts[item] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeMinimalMoreLoginXLSX(t *testing.T, path string, rows [][]string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create xlsx %s: %v", path, err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	writeZipEntry := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}

	writeZipEntry("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
  <Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>
</Types>`)
	writeZipEntry("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`)
	writeZipEntry("xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="Sheet1" sheetId="1" r:id="rId1"/>
  </sheets>
</workbook>`)
	writeZipEntry("xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`)
	writeZipEntry("xl/worksheets/sheet1.xml", buildWorksheetXML(rows))

	if err := zw.Close(); err != nil {
		t.Fatalf("close xlsx writer: %v", err)
	}
}

func buildWorksheetXML(rows [][]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r, row := range rows {
		b.WriteString(`<row r="`)
		b.WriteString(intToString(r + 1))
		b.WriteString(`">`)
		for c, value := range row {
			b.WriteString(`<c r="`)
			b.WriteString(columnName(c + 1))
			b.WriteString(intToString(r + 1))
			b.WriteString(`" t="inlineStr"><is><t>`)
			b.WriteString(xmlEscape(value))
			b.WriteString(`</t></is></c>`)
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

func columnName(idx int) string {
	name := ""
	for idx > 0 {
		idx--
		name = string(rune('A'+(idx%26))) + name
		idx /= 26
	}
	return name
}

func intToString(v int) string {
	return strconv.Itoa(v)
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}
