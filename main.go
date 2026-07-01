package main

import (
	"boost-browser/backend"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed wails.json
var wailsConfigJSON []byte

//go:embed build/appicon.png
var linuxAppIcon []byte

// appRoot 应用根目录，所有相对路径基于此目录解析。
// 生产环境 = exe 所在目录；dev 环境 = 项目源码根目录（CWD）。
var appRoot string

// isDevMode 标识当前是否为 wails dev 模式（exe 在临时目录）
var isDevMode bool

type App struct {
	*backend.App
	syncPanelCloseMu      sync.Mutex
	syncPanelCloseAllowed bool
	syncPanelStartedAt    time.Time
	wailsCtx              context.Context
}

type wailsBuildConfig struct {
	Info struct {
		ProductVersion string `json:"productVersion"`
	} `json:"info"`
}

func envFlagEnabled(name string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func shouldEnableGlobalWindowWatchers() bool {
	return envFlagEnabled("BOOST_BROWSER_ENABLE_GLOBAL_WINDOW_WATCHERS")
}

func shouldEnableTray() bool {
	return envFlagEnabled("BOOST_BROWSER_ENABLE_TRAY")
}

func resolveBuildVersion() string {
	var cfg wailsBuildConfig
	if err := json.Unmarshal(wailsConfigJSON, &cfg); err != nil {
		log.Printf("解析 wails.json 版本信息失败: %v", err)
		return "unknown"
	}

	version := strings.TrimSpace(cfg.Info.ProductVersion)
	if version == "" {
		log.Printf("wails.json 未配置 info.productVersion，回退为 unknown")
		return "unknown"
	}

	return version
}

func NewApp(appRoot string, panelMode bool, version string) *App {
	app := &App{App: backend.NewApp(appRoot, panelMode, version)}
	if panelMode {
		app.syncPanelStartedAt = time.Now()
	}
	return app
}

func isPostUpdateMode() bool {
	return hasCLIArg("--post-update")
}

func directUpdateSuccessMarkerPath() string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	exeDir := filepath.Dir(exePath)
	if resolved, err := filepath.EvalSymlinks(exeDir); err == nil {
		exeDir = resolved
	}
	return filepath.Join(exeDir, "data", ".update_success")
}

func writeDirectUpdateSuccessMarker(reason string) {
	marker := directUpdateSuccessMarkerPath()
	if marker == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(marker), 0755)
	content := fmt.Sprintf("%s\nversion=%s\nreason=%s\npid=%d\n", time.Now().Format(time.RFC3339), resolveBuildVersion(), strings.TrimSpace(reason), os.Getpid())
	_ = os.WriteFile(marker, []byte(content), 0644)
}

func startPostUpdateMarkerRepeater() func() {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		writeDirectUpdateSuccessMarker("post-update-start")
		for {
			select {
			case <-stop:
				return
			case <-deadline.C:
				return
			case <-ticker.C:
				writeDirectUpdateSuccessMarker("post-update-repeat")
			}
		}
	}()
	return func() { close(stop) }
}

func (a *App) startup(ctx context.Context) {
	backend.Start(a.App, ctx)
}

func (a *App) shutdown(ctx context.Context) {
	backend.Stop(a.App, ctx)
}

func (a *App) shouldBlockClose(ctx context.Context) bool {
	if syncPanelMode {
		a.syncPanelCloseMu.Lock()
		allowed := a.syncPanelCloseAllowed
		a.syncPanelCloseMu.Unlock()
		if allowed {
			a.RecordLifecycleEvent("before-close", []string{"action=allow", "reason=sync-panel-explicit-close"})
			return false
		}
		a.RecordLifecycleEvent("before-close", []string{"action=block", "reason=sync-panel-unexpected-close"})
		return true
	}
	return backend.ShouldBlockClose(a.App, ctx)
}

func (a *App) CloseWindowSyncPanel() {
	if !syncPanelMode {
		a.RecordLifecycleEvent("sync-panel-close-request", []string{"action=ignore", "reason=not-sync-panel-mode"})
		return
	}
	if !a.syncPanelStartedAt.IsZero() && time.Since(a.syncPanelStartedAt) < 3*time.Second {
		a.RecordLifecycleEvent("sync-panel-close-request", []string{"action=ignore", "reason=startup-close-cooldown"})
		return
	}
	a.syncPanelCloseMu.Lock()
	a.syncPanelCloseAllowed = true
	ctx := a.wailsCtx
	a.syncPanelCloseMu.Unlock()
	a.RecordLifecycleEvent("sync-panel-close-request", []string{"action=quit-panel-only"})
	if ctx != nil {
		runtime.Quit(ctx)
	}
}

func main() {
	if runUnexpectedExitWatchdogMode() {
		return
	}
	postUpdateMode := isPostUpdateMode()
	stopPostUpdateMarkerRepeater := func() {}
	if postUpdateMode {
		// 老版本 updater 会在启动新版后再删除 marker，且 watchdog/单实例竞争可能让
		// --post-update 进程很快退出。这里在启动早期持续写 marker，保证旧 updater
		// 不会把已经成功启动/接管的新版误判为失败并回滚。
		stopPostUpdateMarkerRepeater = startPostUpdateMarkerRepeater()
		defer stopPostUpdateMarkerRepeater()
		// 从旧版升级时，旧 watchdog 可能抢先重启旧主进程并占住单实例锁。
		// 新版 post-update 进程必须接管同路径旧进程，否则更新文件已替换但界面仍是旧版本。
		takeoverExistingMainInstanceForPostUpdate(appRoot)
	}

	if !envFlagEnabled("BOOST_BROWSER_SKIP_SINGLE_INSTANCE") {
		if syncPanelMode {
			locked, lockErr := acquireSyncPanelLock()
			if lockErr != nil {
				log.Printf("同步面板单实例锁创建失败: %v", lockErr)
			} else if !locked {
				focusExistingSyncPanelWindow()
				log.Printf("窗口同步面板已在运行，当前进程退出")
				return
			} else {
				defer releaseSyncPanelLock()
			}
		} else {
			locked, lockErr := acquireSingleInstanceLock()
			if lockErr != nil {
				log.Printf("单实例锁创建失败: %v", lockErr)
			} else if !locked {
				if postUpdateMode {
					writeDirectUpdateSuccessMarker("post-update-existing-instance")
					time.Sleep(3 * time.Second)
				}
				focusExistingMainWindow()
				log.Printf("Boost Browser 主程序已经在运行，当前进程退出；浏览器实例多开不受影响")
				return
			} else {
				defer releaseSingleInstanceLock()
			}
		}
	}

	// 确定应用根目录：
	// 1. 生产环境：exe 所在目录（快捷方式启动时 CWD 可能不对，需要修正）
	// 2. dev 环境：wails dev 时 exe 可能在 temp 目录或 build/bin 目录，使用当前工作目录
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		tempDir := os.TempDir()
		if resolved, err := filepath.EvalSymlinks(exeDir); err == nil {
			exeDir = resolved
		}
		if resolved, err := filepath.EvalSymlinks(tempDir); err == nil {
			tempDir = resolved
		}

		exeDirLower := strings.ToLower(exeDir)
		inTemp := strings.HasPrefix(exeDirLower, strings.ToLower(tempDir))
		// wails dev 会把 exe 编译到 build/bin/ 目录
		inBuildBin := strings.HasSuffix(filepath.ToSlash(exeDirLower), "/build/bin")

		if inTemp || inBuildBin {
			// dev 模式：exe 在临时目录或 build/bin，使用 CWD 作为根目录
			isDevMode = true
			if cwd, err := os.Getwd(); err == nil {
				appRoot = cwd
			} else {
				appRoot = "."
			}
		} else {
			// 生产模式：使用 exe 所在目录
			isDevMode = false
			appRoot = exeDir
			os.Chdir(exeDir)
		}
	} else {
		// 兜底：使用 CWD
		if cwd, err := os.Getwd(); err == nil {
			appRoot = cwd
		} else {
			appRoot = "."
		}
	}

	startupDebugEnabled := envFlagEnabled("BOOST_BROWSER_DEBUG_STARTUP")
	if startupDebugEnabled {
		log.Printf("应用根目录: %s (dev=%v)", appRoot, isDevMode)
	}
	if err := backend.EnsureRuntimeLayout(appRoot); err != nil {
		log.Printf("准备用户数据目录失败: %v", err)
	}
	if startupDebugEnabled && backend.RuntimeUsesDetachedState(appRoot) {
		log.Printf("检测到安装目录需要只读运行，状态目录切换到: %s", backend.RuntimeStateRoot(appRoot))
	}
	buildVersion := resolveBuildVersion()
	if startupDebugEnabled {
		log.Printf("应用版本: %s", buildVersion)
		log.Printf(
			"Wails 启动环境: GOOS=%s GOARCH=%s DISPLAY=%q WAYLAND_DISPLAY=%q XDG_SESSION_TYPE=%q XDG_CURRENT_DESKTOP=%q",
			goruntime.GOOS,
			goruntime.GOARCH,
			os.Getenv("DISPLAY"),
			os.Getenv("WAYLAND_DISPLAY"),
			os.Getenv("XDG_SESSION_TYPE"),
			os.Getenv("XDG_CURRENT_DESKTOP"),
		)
	}
	if startupDebugEnabled && goruntime.GOOS == "linux" && strings.TrimSpace(os.Getenv("DISPLAY")) == "" && strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) == "" {
		log.Printf("检测到 Linux 图形环境变量为空：DISPLAY / WAYLAND_DISPLAY 都未设置，GUI 窗口大概率无法创建")
	}

	// 加载配置
	cfg, err := backend.LoadConfig(backend.ResolveRuntimePath(appRoot, "config.yaml"))
	if err != nil {
		log.Printf("加载配置失败，使用默认配置: %v", err)
		cfg = backend.DefaultConfig()
	}

	// 创建应用实例
	app := NewApp(appRoot, syncPanelMode, buildVersion)
	app.RecordLifecycleEvent("startup", []string{
		fmt.Sprintf("root=%s", appRoot),
		fmt.Sprintf("dev=%t", isDevMode),
		fmt.Sprintf("version=%s", buildVersion),
	})
	app.ClearIntentionalExitMarker()
	// 诊断 + 兜底：如果主 Wails/WebView2 宿主无 OnShutdown、无 wails-run-return 就消失，
	// watchdog 会记录 unexpected-parent-exit 并拉起主程序；正常 ForceQuit/QuitAppOnly
	// 会写 intentional-exit.flag，不会被自动重启。
	// 注意：同步控制面板是同一个 exe 的子模式，不能启动 watchdog；否则面板关闭/崩溃会被
	// 错误地重启成主窗口，造成“用着用着主程序闪退/重开”的假象和进程串扰。
	if !syncPanelMode {
		startUnexpectedExitWatchdog(appRoot)
	}
	defer func() {
		if r := recover(); r != nil {
			app.RecordLifecycleEvent("panic", []string{fmt.Sprintf("value=%v", r), fmt.Sprintf("stack=%s", strings.ReplaceAll(string(debug.Stack()), "\n", "\\n"))})
			panic(r)
		}
	}()

	var wailsCtx context.Context
	startupReached := make(chan struct{})

	if startupDebugEnabled {
		go func() {
			select {
			case <-startupReached:
				return
			case <-time.After(12 * time.Second):
				log.Printf("Wails OnStartup 在 12 秒内未触发。若终端一直转圈但没有窗口，优先检查 Linux 图形环境、libgtk-3、libwebkit2gtk，以及是否运行在 SSH/容器/无桌面会话中")
			}
		}()
	}

	// 启动应用
	if startupDebugEnabled {
		log.Printf("准备调用 wails.Run 创建 GUI 窗口")
	}
	title := cfg.App.Name
	width := cfg.App.Window.Width
	height := cfg.App.Window.Height
	minWidth := cfg.App.Window.MinWidth
	minHeight := cfg.App.Window.MinHeight
	backgroundColour := &options.RGBA{R: 245, G: 247, B: 250, A: 255}
	windowsOptions := &windows.Options{
		WebviewIsTransparent: false,
		WindowIsTranslucent:  false,
	}
	frameless := false
	alwaysOnTop := false
	disableResize := false
	if syncPanelMode {
		title = fmt.Sprintf("%s · 窗口同步", cfg.App.Name)
		width = 520
		height = 600
		// 同步面板现在默认直接以小悬浮窗启动；最小尺寸仅覆盖折叠状态和“显示下面功能”展开状态。
		minWidth = 448
		minHeight = 120
		// 同步器悬浮窗恢复为真实透明宿主，前端再根据聚焦态决定面板的不透明度。
		// 这样空闲时尽量接近“只有一层透明浮层”，聚焦/操作时再抬高可见度。
		backgroundColour = &options.RGBA{R: 0, G: 0, B: 0, A: 0}
		frameless = true
		alwaysOnTop = true
		disableResize = true
		windowsOptions = &windows.Options{
			WebviewIsTransparent:              true,
			WindowIsTranslucent:               true,
			DisableFramelessWindowDecorations: true,
		}
	}
	err = wails.Run(&options.App{
		Title:         title,
		Width:         width,
		Height:        height,
		MinWidth:      minWidth,
		MinHeight:     minHeight,
		Frameless:     frameless,
		AlwaysOnTop:   alwaysOnTop,
		DisableResize: disableResize,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: backgroundColour,
		OnStartup: func(ctx context.Context) {
			close(startupReached)
			app.RecordLifecycleEvent("wails-startup", nil)
			if startupDebugEnabled {
				log.Printf("Wails OnStartup 已触发，GUI 宿主已创建")
			}
			wailsCtx = ctx
			app.syncPanelCloseMu.Lock()
			app.wailsCtx = ctx
			app.syncPanelCloseMu.Unlock()
			if syncPanelMode {
				runtime.WindowCenter(wailsCtx)
			} else {
				restoreNativeMainWindowBounds(wailsCtx, app)
				if shouldEnableGlobalWindowWatchers() {
					app.RecordLifecycleEvent("global-window-watchers", []string{"state=enabled", "source=env:BOOST_BROWSER_ENABLE_GLOBAL_WINDOW_WATCHERS"})
					backend.StartGlobalSerializedWindowWatchers(appRoot)
				} else {
					app.RecordLifecycleEvent("global-window-watchers", []string{"state=disabled", "reason=default-off-crashprobe"})
				}
				if shouldEnableTray() {
					app.RecordLifecycleEvent("tray", []string{"state=enabled", "source=env:BOOST_BROWSER_ENABLE_TRAY"})
					// 启动系统托盘（非阻塞）
					go backend.RunTray(backend.TrayCallbacks{
						OnShow: func() {
							app.RecordLifecycleEvent("tray-click", []string{"action=show"})
							runtime.WindowShow(wailsCtx)
							runtime.WindowUnminimise(wailsCtx)
						},
						OnQuitAppOnly: func() {
							app.RecordLifecycleEvent("tray-click", []string{"action=hide-to-tray"})
							runtime.WindowHide(wailsCtx)
						},
						OnQuit: func() {
							app.RecordLifecycleEvent("tray-click", []string{"action=quit-app-and-browser"})
							app.ForceQuit()
						},
					})
				} else {
					app.RecordLifecycleEvent("tray", []string{"state=disabled", "reason=default-off-crashprobe"})
				}
			}
			app.startup(ctx)
			if startupDebugEnabled {
				log.Printf("后端 startup 已完成")
			}
			// 升级后首次启动检测：updater 看到 .update_success 标记才会清掉 .bak 备份
			for _, arg := range os.Args[1:] {
				if arg == "--post-update" {
					app.WriteUpdateSuccessMarker()
					break
				}
			}
		},
		OnShutdown: func(ctx context.Context) {
			app.RecordLifecycleEvent("wails-shutdown", nil)
			if startupDebugEnabled {
				log.Printf("Wails OnShutdown 已触发")
			}
			if !syncPanelMode {
				backend.QuitTray()
			}
			app.shutdown(ctx)
		},
		// 拦截关闭按钮事件，由前端处理自定义对话框
		OnBeforeClose: func(ctx context.Context) bool {
			return app.shouldBlockClose(ctx)
		},
		Bind: []interface{}{
			app,
		},
		Linux: &linux.Options{
			// Expose a real window icon to Linux desktop environments.
			Icon:             linuxAppIcon,
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
		},
		Windows: windowsOptions,
	})

	if err != nil {
		app.RecordLifecycleEvent("wails-run-error", []string{fmt.Sprintf("error=%v", err)})
		log.Fatal("启动应用失败:", err)
	}
	app.RecordLifecycleEvent("wails-run-return", nil)
	if startupDebugEnabled {
		log.Printf("wails.Run 已退出")
	}
}
