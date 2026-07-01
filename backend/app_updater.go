package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"boost-browser/backend/internal/logger"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// =============================================================================
// 方案 A：纯 GitHub Releases 自动升级
//
// 发版流程（你只需做这件事）：
//   1. 升 wails.json 里 productVersion，比如 1.1.0 → 1.1.1
//   2. wails build -clean -platform windows/amd64
//   3. 计算 sha256: certutil -hashfile boost-browser.exe SHA256
//   4. 在 GitHub 上 Create new release，tag 填 v1.1.1
//   5. 上传两个文件作为 release assets：
//        - boost-browser.exe              (主程序)
//        - boost-browser.exe.sha256       (纯文本，里面就一行 sha256 hash)
//   6. Publish release，结束
//
// 客户端流程（自动）：
//   启动 5s 后 GET https://api.github.com/repos/{owner}/{repo}/releases/latest
//     → 比较版本号
//     → 有新版 → 弹 Dialog
//     → 用户确认 → 流式下载 + sha256 校验
//     → 启动 updater.exe → 主程序退出 → updater 替换 exe → 启动新版
// =============================================================================

const (
	// GitHub 仓库：https://github.com/sdohuajia/BoostBrowser
	githubOwner = "sdohuajia"
	githubRepo  = "BoostBrowser"

	updateDirName  = "updates"
	updaterExeName = "updater.exe"
	successMarker  = ".update_success"
)

// githubReleaseAPI 实时查询 GitHub Releases
func githubReleaseAPI() string {
	return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", githubOwner, githubRepo)
}

// githubLatestReleasePage 不走 GitHub API；/releases/latest 会 302 到最新 tag。
func githubLatestReleasePage() string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/latest", githubOwner, githubRepo)
}

func githubReleaseAssetURL(tag, assetName string) string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", githubOwner, githubRepo, tag, assetName)
}

// UpdateCheckResult 给前端的检查结果
type UpdateCheckResult struct {
	HasUpdate    bool   `json:"hasUpdate"`
	Current      string `json:"current"`
	Latest       string `json:"latest"`
	Force        bool   `json:"force"`
	ReleaseNotes string `json:"releaseNotes"`
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
}

type ghAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type ghRelease struct {
	TagName    string    `json:"tag_name"`
	Name       string    `json:"name"`
	Body       string    `json:"body"`
	Prerelease bool      `json:"prerelease"`
	Draft      bool      `json:"draft"`
	Assets     []ghAsset `json:"assets"`
}

// CheckUpdate Wails binding：用户点检查更新 / 启动后自动调用
func (a *App) CheckUpdate() (*UpdateCheckResult, error) {
	log := logger.New("Updater")
	current := a.appVersion()

	client := &http.Client{Timeout: 15 * time.Second}
	rel, err := fetchLatestReleaseWithFallback(client, githubReleaseAPI(), githubLatestReleasePage())
	if err != nil {
		log.Info("更新信息获取失败", logger.F("error", err.Error()))
		return nil, err
	}
	if rel.Draft || rel.Prerelease {
		log.Info("最新 release 是草稿或预发布，跳过", logger.F("tag", rel.TagName))
		return &UpdateCheckResult{HasUpdate: false, Current: current, Latest: current}, nil
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	var exeURL, sha256URL string
	var exeSize int64
	for _, asset := range rel.Assets {
		switch asset.Name {
		case "boost-browser.exe":
			exeURL = asset.BrowserDownloadURL
			exeSize = asset.Size
		case "boost-browser.exe.sha256":
			sha256URL = asset.BrowserDownloadURL
		}
	}
	if exeURL == "" {
		return nil, fmt.Errorf("release %s 缺少 boost-browser.exe asset", rel.TagName)
	}
	if sha256URL == "" {
		return nil, fmt.Errorf("release %s 缺少 boost-browser.exe.sha256 asset", rel.TagName)
	}

	// 拉 sha256 文件内容
	sha256Hex, err := fetchSHA256Asset(sha256URL)
	if err != nil {
		return nil, fmt.Errorf("sha256 文件下载失败：%w", err)
	}

	// release notes 里可以写 [force] 触发强制升级
	force := strings.Contains(strings.ToLower(rel.Body), "[force]")

	hasUpdate := compareVersion(latest, current) > 0
	log.Info("更新检查完成",
		logger.F("current", current),
		logger.F("latest", latest),
		logger.F("has_update", hasUpdate),
		logger.F("force", force),
	)

	return &UpdateCheckResult{
		HasUpdate:    hasUpdate,
		Current:      current,
		Latest:       latest,
		Force:        force,
		ReleaseNotes: rel.Body,
		URL:          exeURL,
		SHA256:       sha256Hex,
		Size:         exeSize,
	}, nil
}

func fetchLatestReleaseWithFallback(client *http.Client, apiURL, latestPageURL string) (*ghRelease, error) {
	rel, err := fetchLatestReleaseFromAPI(client, apiURL)
	if err == nil {
		return rel, nil
	}
	if !strings.Contains(err.Error(), "GitHub API 限流") {
		return nil, err
	}
	fallback, fallbackErr := fetchLatestReleaseFromRedirect(client, latestPageURL)
	if fallbackErr != nil {
		return nil, fmt.Errorf("GitHub API 限流，备用更新地址也不可用：%w", fallbackErr)
	}
	return fallback, nil
}

func fetchLatestReleaseFromAPI(client *http.Client, apiURL string) (*ghRelease, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "BoostBrowser-Updater/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接更新服务器：%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("尚未发布任何 release")
	}
	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("GitHub API 限流")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API 返回 HTTP %d", resp.StatusCode)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("release 数据解析失败：%w", err)
	}
	return &rel, nil
}

func fetchLatestReleaseFromRedirect(client *http.Client, latestPageURL string) (*ghRelease, error) {
	req, err := http.NewRequest("GET", latestPageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BoostBrowser-Updater/1.0")

	redirectClient := *client
	redirectClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := redirectClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法访问最新版本页面：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently && resp.StatusCode != http.StatusTemporaryRedirect && resp.StatusCode != http.StatusPermanentRedirect {
		return nil, fmt.Errorf("最新版本页面返回 HTTP %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return nil, fmt.Errorf("最新版本页面未返回跳转地址")
	}
	locURL, err := resp.Request.URL.Parse(location)
	if err != nil {
		return nil, fmt.Errorf("最新版本跳转地址异常：%w", err)
	}
	parts := strings.Split(strings.Trim(locURL.Path, "/"), "/")
	var tag string
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "tag" {
			tag = parts[i+1]
			break
		}
	}
	if tag == "" {
		return nil, fmt.Errorf("最新版本跳转地址未包含 tag：%s", locURL.String())
	}
	return &ghRelease{
		TagName: tag,
		Assets: []ghAsset{
			{Name: "boost-browser.exe", BrowserDownloadURL: githubReleaseAssetURL(tag, "boost-browser.exe")},
			{Name: "boost-browser.exe.sha256", BrowserDownloadURL: githubReleaseAssetURL(tag, "boost-browser.exe.sha256")},
		},
	}, nil
}

func fetchSHA256Asset(url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	// 容忍 "hash filename" / "hash" / "hash\n" 等格式
	first := strings.TrimSpace(strings.SplitN(string(body), "\n", 2)[0])
	parts := strings.Fields(first)
	if len(parts) == 0 {
		return "", fmt.Errorf("sha256 文件为空")
	}
	hash := strings.ToLower(parts[0])
	if len(hash) != 64 {
		return "", fmt.Errorf("sha256 长度异常：%d", len(hash))
	}
	return hash, nil
}

// DownloadUpdate Wails binding：流式下载，进度通过 update:progress 事件推送给前端
// 返回值是临时文件路径，传给 ApplyUpdate
func (a *App) DownloadUpdate(url, expectedSHA256 string) (string, error) {
	log := logger.New("Updater")

	updateDir := a.resolveAppPath(filepath.Join("data", updateDirName))
	if err := os.MkdirAll(updateDir, 0755); err != nil {
		return "", fmt.Errorf("创建下载目录失败：%w", err)
	}
	dst := filepath.Join(updateDir, "boost-browser.new.exe")
	_ = os.Remove(dst)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "BoostBrowser-Updater/1.0")

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("下载失败：HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()

	hasher := sha256.New()
	var downloaded int64
	buf := make([]byte, 64*1024)
	lastEmit := time.Now()
	emitProgress := func(force bool) {
		if !force && time.Since(lastEmit) < 200*time.Millisecond {
			return
		}
		lastEmit = time.Now()
		percent := 0
		if total > 0 {
			percent = int(float64(downloaded) * 100 / float64(total))
		}
		runtime.EventsEmit(a.ctx, "update:progress", map[string]any{
			"downloaded": downloaded,
			"total":      total,
			"percent":    percent,
		})
	}

	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return "", werr
			}
			hasher.Write(buf[:n])
			downloaded += int64(n)
			emitProgress(false)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", fmt.Errorf("下载中断：%w", rerr)
		}
	}
	if err := out.Close(); err != nil {
		return "", err
	}

	gotHash := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(gotHash, expectedSHA256) {
		_ = os.Remove(dst)
		return "", fmt.Errorf("SHA256 校验失败，文件已损坏或被篡改\n期望: %s\n实际: %s", expectedSHA256, gotHash)
	}

	emitProgress(true)
	log.Info("更新包下载完成",
		logger.F("path", dst),
		logger.F("size", downloaded),
		logger.F("sha256", gotHash),
	)
	return dst, nil
}

// applyUpdateDebugLog 同步写应急日志，绕过 async logger（os.Exit 时丢消息的问题）
func applyUpdateDebugLog(currentExe, msg string) {
	debugLogPath := filepath.Join(filepath.Dir(currentExe), "data", "logs", "apply_update.debug.log")
	_ = os.MkdirAll(filepath.Dir(debugLogPath), 0755)
	f, err := os.OpenFile(debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), msg)
		_ = f.Sync()
		_ = f.Close()
	}
}

// ApplyUpdate Wails binding：启动 updater.exe，主程序退出
func (a *App) ApplyUpdate(newExePath string) error {
	log := logger.New("Updater")

	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前 exe 路径失败：%w", err)
	}
	currentExe, _ = filepath.EvalSymlinks(currentExe)

	applyUpdateDebugLog(currentExe, fmt.Sprintf("ApplyUpdate 被调用 newExePath=%s", newExePath))

	updaterPath := filepath.Join(filepath.Dir(currentExe), updaterExeName)
	if _, err := os.Stat(updaterPath); err != nil {
		applyUpdateDebugLog(currentExe, fmt.Sprintf("ERROR updater.exe 不存在 path=%s err=%v", updaterPath, err))
		return fmt.Errorf("updater.exe 不存在：%s（请重装应用）", updaterPath)
	}
	if _, err := os.Stat(newExePath); err != nil {
		applyUpdateDebugLog(currentExe, fmt.Sprintf("ERROR 新版本文件不存在 path=%s err=%v", newExePath, err))
		return fmt.Errorf("新版本文件不存在：%s", newExePath)
	}

	// 清理可能残留的成功标记
	_ = os.Remove(a.resolveAppPath(filepath.Join("data", successMarker)))
	// 本次退出是为了交给 updater 替换主程序，不是崩溃。提前写 intentional-exit，
	// 避免常驻 watchdog 在 updater 替换窗口期把旧进程重新拉起，和新版启动/marker
	// 检测互相竞争，最终被误判失败并回滚。
	a.markIntentionalExit("apply-update")

	pid := os.Getpid()
	cmd := exec.Command(updaterPath, strconv.Itoa(pid), currentExe, newExePath)
	cmd.SysProcAttr = detachedProcessAttrs()
	if err := cmd.Start(); err != nil {
		applyUpdateDebugLog(currentExe, fmt.Sprintf("ERROR cmd.Start 失败 err=%v", err))
		return fmt.Errorf("启动 updater 失败：%w", err)
	}

	updaterPID := cmd.Process.Pid
	applyUpdateDebugLog(currentExe, fmt.Sprintf("updater.exe 已启动 pid=%d", updaterPID))

	// 关键修复：Release 让 Go runtime 释放对子进程 handle，
	// 否则父进程 os.Exit 时 Windows 会把 Go runtime 跟踪的子进程一起带走（即使有 DETACHED_PROCESS）。
	if rerr := cmd.Process.Release(); rerr != nil {
		applyUpdateDebugLog(currentExe, fmt.Sprintf("WARN cmd.Process.Release 失败 err=%v", rerr))
	} else {
		applyUpdateDebugLog(currentExe, "cmd.Process.Release 成功，updater 与主进程已脱钩")
	}

	log.Info("updater 已启动，主程序即将退出",
		logger.F("updater_pid", updaterPID),
		logger.F("self_pid", pid),
		logger.F("current_exe", currentExe),
		logger.F("new_exe", newExePath),
	)

	// 关键修复：强制 flush async logger（默认 1s flush 间隔），避免 os.Exit 时丢消息
	_ = logger.Close()

	go func() {
		time.Sleep(800 * time.Millisecond)
		applyUpdateDebugLog(currentExe, "调用 runtime.Quit")
		runtime.Quit(a.ctx)
		time.Sleep(2 * time.Second)
		applyUpdateDebugLog(currentExe, "调用 os.Exit(0)")
		os.Exit(0)
	}()
	return nil
}

// WriteUpdateSuccessMarker 升级后第一次启动时调用，告诉 updater 升级成功
// 由 main.go / startup 在检测到 --post-update 参数时调用
func (a *App) WriteUpdateSuccessMarker() {
	marker := a.resolveAppPath(filepath.Join("data", successMarker))
	_ = os.MkdirAll(filepath.Dir(marker), 0755)
	_ = os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)+"\nversion="+a.appVersion()+"\n"), 0644)
	logger.New("Updater").Info("升级后首次启动，已写入成功标记", logger.F("marker", marker))
}

// compareVersion 简单 semver 比较：a > b 返回 1，相等返回 0，小于返回 -1
// 不支持 pre-release / build metadata，只比 major.minor.patch
func compareVersion(a, b string) int {
	a = strings.TrimPrefix(strings.TrimSpace(a), "v")
	b = strings.TrimPrefix(strings.TrimSpace(b), "v")
	pa := splitVersion(a)
	pb := splitVersion(b)
	for i := 0; i < 3; i++ {
		var na, nb int
		if i < len(pa) {
			na = pa[i]
		}
		if i < len(pb) {
			nb = pb[i]
		}
		if na > nb {
			return 1
		}
		if na < nb {
			return -1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	parts := strings.Split(v, ".")
	out := make([]int, 0, 3)
	for _, p := range parts {
		// 截断 1.1.1-beta 这种后缀
		if idx := strings.IndexAny(p, "-+"); idx >= 0 {
			p = p[:idx]
		}
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		out = append(out, n)
	}
	return out
}
