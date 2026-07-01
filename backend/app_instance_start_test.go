package backend

import (
	"boost-browser/backend/internal/browser"
	"boost-browser/backend/internal/config"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildChromeUAFromFingerprintArgs(t *testing.T) {
	t.Parallel()

	winUA := buildChromeUAFromFingerprintArgs("146.0.7680.168", []string{"--fingerprint-platform=windows"})
	if !strings.Contains(winUA, "Windows NT 10.0") || !strings.Contains(winUA, "Chrome/146.0.7680.168") {
		t.Fatalf("windows UA 不正确: %s", winUA)
	}

	linuxUA := buildChromeUAFromFingerprintArgs("146.0.7678.101", []string{"--fingerprint-platform=linux"})
	if !strings.Contains(linuxUA, "X11; Linux x86_64") || !strings.Contains(linuxUA, "Chrome/146.0.7678.101") {
		t.Fatalf("linux UA 不正确: %s", linuxUA)
	}

	macUA := buildChromeUAFromFingerprintArgs("146.0.7680.177", []string{"--fingerprint-platform=mac", "--fingerprint-platform-version=15.0.0"})
	if !strings.Contains(macUA, "Macintosh; Intel Mac OS X 15_0_0") || !strings.Contains(macUA, "Chrome/146.0.7680.177") {
		t.Fatalf("mac UA 不正确: %s", macUA)
	}
}

func TestEnsureNewWindowLaunchArgAddsFlagOnce(t *testing.T) {
	t.Parallel()

	got := ensureNewWindowLaunchArg([]string{"--lang=en-US"})
	want := []string{"--lang=en-US", "--new-window"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ensureNewWindowLaunchArg 结果错误: got=%v want=%v", got, want)
	}

	got = ensureNewWindowLaunchArg([]string{"--new-window", "--lang=en-US"})
	want = []string{"--new-window", "--lang=en-US"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ensureNewWindowLaunchArg 不应重复追加: got=%v want=%v", got, want)
	}
}

func TestIsBrowserProfileLive(t *testing.T) {
	t.Parallel()

	ln := mustListenLoopback(t)
	defer ln.Close()

	profile := &BrowserProfile{
		Running:   true,
		DebugPort: listenerPort(t, ln),
	}
	if !isBrowserProfileLive(profile, nil) {
		t.Fatal("期望存活中的调试端口被识别为运行中实例")
	}

	if isBrowserProfileLive(&BrowserProfile{Running: true, DebugPort: 0}, nil) {
		t.Fatal("debugPort=0 不应被识别为运行中实例")
	}
}

func TestIsBrowserProfileLiveKeepsPendingDebugProcessAlive(t *testing.T) {
	t.Parallel()

	cmd := longLivedCommand(2 * time.Second)
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动长生命周期测试进程失败: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	profile := &BrowserProfile{
		Running:    true,
		Pid:        cmd.Process.Pid,
		DebugPort:  0,
		DebugReady: false,
	}
	if !isBrowserProfileLive(profile, cmd) {
		t.Fatal("期望调试接口未就绪但进程仍存活时识别为运行中实例")
	}
}

func TestWaitBrowserDebugPortStableKeepsListeningPort(t *testing.T) {
	t.Parallel()

	server := startDevToolsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			_, _ = w.Write([]byte(`{"Browser":"Chrome/142.0","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/browser"}`))
		case "/json/list":
			_, _ = w.Write([]byte(`[{"id":"page-1"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if _, err := waitBrowserDebugPortStable(server.port, "", time.Second, 250*time.Millisecond, nil); err != nil {
		t.Fatalf("waitBrowserDebugPortStable 返回错误: %v", err)
	}
}

func TestWaitBrowserDebugPortStableRejectsEphemeralPort(t *testing.T) {
	t.Parallel()

	server := startDevToolsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			_, _ = w.Write([]byte(`{"Browser":"Chrome/142.0","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/browser"}`))
		case "/json/list":
			_, _ = w.Write([]byte(`[{"id":"page-1"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	port := server.port
	time.AfterFunc(120*time.Millisecond, func() {
		_ = server.Close()
	})

	_, err := waitBrowserDebugPortStable(port, "", time.Second, 400*time.Millisecond, nil)
	if err == nil {
		t.Fatal("期望短暂就绪后关闭的端口被判定为失败")
	}
}

func TestWaitBrowserDebugPortStableRejectsPlainTCPPort(t *testing.T) {
	t.Parallel()

	ln := mustListenLoopback(t)
	defer ln.Close()

	_, err := waitBrowserDebugPortStable(listenerPort(t, ln), "", 700*time.Millisecond, 250*time.Millisecond, nil)
	if err == nil {
		t.Fatal("期望仅开放 TCP 端口但无 DevTools HTTP 时启动失败")
	}
}

func TestWaitBrowserDebugPortStableDiscoversPortFromStderr(t *testing.T) {
	t.Parallel()

	server := startDevToolsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			_, _ = w.Write([]byte(`{"Browser":"Chrome/142.0","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/browser"}`))
		case "/json/list":
			_, _ = w.Write([]byte(`[{"id":"page-1"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cmd := stderrPortCommand(server.port, 2*time.Second)
	monitor, err := newBrowserProcessMonitor(cmd)
	if err != nil {
		t.Fatalf("初始化浏览器进程监控失败: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动测试命令失败: %v", err)
	}
	monitor.Start()

	debugPort, err := waitBrowserDebugPortStable(0, "", 2*time.Second, 250*time.Millisecond, monitor)
	if err != nil {
		t.Fatalf("期望从 stderr 自动发现调试端口，实际错误: %v", err)
	}
	if debugPort != server.port {
		t.Fatalf("期望发现调试端口 %d，实际=%d", server.port, debugPort)
	}
}

func TestWaitBrowserDebugPortStableDiscoversPortFromDevToolsFile(t *testing.T) {
	t.Parallel()

	server := startDevToolsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			_, _ = w.Write([]byte(`{"Browser":"Chrome/142.0","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/browser"}`))
		case "/json/list":
			_, _ = w.Write([]byte(`[{"id":"page-1"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	userDataDir := t.TempDir()
	writeDevToolsActivePortFile(t, userDataDir, server.port)

	debugPort, err := waitBrowserDebugPortStable(0, userDataDir, time.Second, 250*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("期望从 DevToolsActivePort 自动发现调试端口，实际错误: %v", err)
	}
	if debugPort != server.port {
		t.Fatalf("期望发现调试端口 %d，实际=%d", server.port, debugPort)
	}
}

func TestWaitBrowserDebugPortStableReturnsProcessExitDetail(t *testing.T) {
	t.Parallel()

	cmd := stderrFailingCommand("missing libEGL.dll")
	monitor, err := newBrowserProcessMonitor(cmd)
	if err != nil {
		t.Fatalf("初始化浏览器进程监控失败: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动测试命令失败: %v", err)
	}
	monitor.Start()

	startedAt := time.Now()
	_, err = waitBrowserDebugPortStable(0, "", 2*time.Second, 250*time.Millisecond, monitor)
	if err == nil {
		t.Fatal("期望启动前退出被判定为失败")
	}
	if time.Since(startedAt) >= 2*time.Second {
		t.Fatalf("期望在超时前返回进程退出错误，实际耗时=%s", time.Since(startedAt))
	}

	var exitErr *browserStartupExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("期望 browserStartupExitError，实际=%T %v", err, err)
	}
	if !strings.Contains(exitErr.Detail(), "missing libEGL.dll") {
		t.Fatalf("期望 stderr 细节被捕获，实际=%q", exitErr.Detail())
	}
}

func TestWaitBrowserDebugPortStableAllowsDebugPortAfterLauncherExit(t *testing.T) {
	t.Parallel()

	port := freeLoopbackPort(t)
	cmd := shortLivedCommand()
	monitor, err := newBrowserProcessMonitor(cmd)
	if err != nil {
		t.Fatalf("初始化浏览器进程监控失败: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动短命测试命令失败: %v", err)
	}
	monitor.Start()

	serverReady := make(chan *devToolsTestServer, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		serverReady <- startDevToolsServerOnPort(t, port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/json/version":
				_, _ = w.Write([]byte(`{"Browser":"Chrome/142.0","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/browser"}`))
			case "/json/list":
				_, _ = w.Write([]byte(`[{"id":"page-1"}]`))
			default:
				http.NotFound(w, r)
			}
		}))
	}()

	debugPort, err := waitBrowserDebugPortStable(port, "", 100*time.Millisecond, 250*time.Millisecond, monitor)
	server := <-serverReady
	defer server.Close()

	if err != nil {
		t.Fatalf("期望启动器退出后仍能等待到调试端口就绪，实际错误: %v", err)
	}
	if debugPort != port {
		t.Fatalf("期望发现调试端口 %d，实际=%d", port, debugPort)
	}
}

func TestWaitBrowserProcessKeepsRunningWhileDebugPortAlive(t *testing.T) {
	ln := mustListenLoopback(t)
	port := listenerPort(t, ln)

	app := NewApp("")
	app.browserMgr = browser.NewManager(config.DefaultConfig(), "")
	app.browserMgr.Profiles = map[string]*BrowserProfile{
		"profile-detached": {
			ProfileId:   "profile-detached",
			ProfileName: "Detached Browser",
			Running:     true,
			DebugPort:   port,
			DebugReady:  true,
			Pid:         12345,
		},
	}
	app.browserMgr.BrowserProcesses = make(map[string]*exec.Cmd)

	cmd := shortLivedCommand()
	monitor, err := newBrowserProcessMonitor(cmd)
	if err != nil {
		t.Fatalf("初始化测试进程监控失败: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动短命测试进程失败: %v", err)
	}
	monitor.Start()
	app.browserMgr.BrowserProcesses["profile-detached"] = cmd

	done := make(chan struct{})
	go func() {
		app.waitBrowserProcess("profile-detached", monitor)
		close(done)
	}()

	waitForCondition(t, 3*time.Second, func() bool {
		app.browserMgr.Mutex.Lock()
		defer app.browserMgr.Mutex.Unlock()

		profile := app.browserMgr.Profiles["profile-detached"]
		_, tracked := app.browserMgr.BrowserProcesses["profile-detached"]
		return profile != nil && profile.Running && !tracked
	})

	_ = ln.Close()

	waitForCondition(t, 4*time.Second, func() bool {
		app.browserMgr.Mutex.Lock()
		defer app.browserMgr.Mutex.Unlock()

		profile := app.browserMgr.Profiles["profile-detached"]
		return profile != nil && !profile.Running && profile.DebugPort == 0 && profile.Pid == 0
	})

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("waitBrowserProcess 未在调试端口关闭后结束")
	}
}

func TestWaitForBrowserDebugReadyMarksProfileReady(t *testing.T) {
	t.Parallel()

	port := freeLoopbackPort(t)
	app := NewApp("")
	app.browserMgr = browser.NewManager(config.DefaultConfig(), "")
	app.browserMgr.Profiles = map[string]*BrowserProfile{
		"profile-ready": {
			ProfileId:      "profile-ready",
			ProfileName:    "Ready Browser",
			Running:        true,
			DebugPort:      port,
			DebugReady:     false,
			RuntimeWarning: "pending",
			LastStartAt:    time.Now().Format(time.RFC3339),
		},
	}
	app.browserMgr.BrowserProcesses = make(map[string]*exec.Cmd)

	serverReady := make(chan *devToolsTestServer, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		serverReady <- startDevToolsServerOnPort(t, port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/json/version":
				_, _ = w.Write([]byte(`{"Browser":"Chrome/142.0","webSocketDebuggerUrl":"ws://127.0.0.1/devtools/browser"}`))
			case "/json/list":
				_, _ = w.Write([]byte(`[{"id":"page-1"}]`))
			default:
				http.NotFound(w, r)
			}
		}))
	}()

	snapshot, changed := app.waitForBrowserDebugReady("profile-ready", port, 2*time.Second)
	server := <-serverReady
	defer server.Close()

	if snapshot == nil {
		t.Fatal("期望等待到调试接口就绪")
	}
	if !changed {
		t.Fatal("期望调试接口就绪后标记实例状态变更")
	}
	if !snapshot.DebugReady {
		t.Fatal("期望实例被标记为调试接口已就绪")
	}
	if snapshot.RuntimeWarning != "" {
		t.Fatalf("期望调试接口就绪后清空警告，实际=%q", snapshot.RuntimeWarning)
	}
}

func TestSanitizeManagedLaunchArgsRemovesSystemManagedFlags(t *testing.T) {
	t.Parallel()

	got, removed := sanitizeManagedLaunchArgs([]string{
		"--lang=en-US",
		"--remote-debugging-port=9222",
		"--user-data-dir", "D:\\profiles\\demo",
		"--proxy-server", "http://127.0.0.1:9000",
		"--search-provider-search-url", "http://{searchTerms}",
		"--search-provider-name=No Search",
		"--remote-debugging-pipe",
		"https://example.com",
	})

	wantArgs := []string{"--lang=en-US", "https://example.com"}
	if !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("sanitizeManagedLaunchArgs args mismatch: got=%v want=%v", got, wantArgs)
	}

	wantRemoved := []string{
		"--remote-debugging-port",
		"--user-data-dir",
		"--proxy-server",
		"--search-provider-search-url",
		"--search-provider-name",
		"--remote-debugging-pipe",
	}
	if !reflect.DeepEqual(removed, wantRemoved) {
		t.Fatalf("sanitizeManagedLaunchArgs removed mismatch: got=%v want=%v", removed, wantRemoved)
	}
}

func TestSanitizeManagedLaunchArgsKeepsUnmanagedFlags(t *testing.T) {
	t.Parallel()

	input := []string{"--lang=en-US", "--disable-sync", "https://example.com"}
	got, removed := sanitizeManagedLaunchArgs(input)
	if !reflect.DeepEqual(got, input) {
		t.Fatalf("sanitizeManagedLaunchArgs should preserve unmanaged args: got=%v want=%v", got, input)
	}
	if len(removed) != 0 {
		t.Fatalf("sanitizeManagedLaunchArgs should not report managed args, got=%v", removed)
	}
}

func TestSanitizeManagedWindowPlacementArgsRemovesWindowSizeAndPosition(t *testing.T) {
	t.Parallel()

	got, removed := sanitizeManagedWindowPlacementArgs([]string{
		"--lang=en-US",
		"--window-size=1600,900",
		"--window-position", "20,40",
		"https://example.com",
	})

	wantArgs := []string{"--lang=en-US", "https://example.com"}
	if !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("sanitizeManagedWindowPlacementArgs args mismatch: got=%v want=%v", got, wantArgs)
	}

	wantRemoved := []string{"--window-size", "--window-position"}
	if !reflect.DeepEqual(removed, wantRemoved) {
		t.Fatalf("sanitizeManagedWindowPlacementArgs removed mismatch: got=%v want=%v", removed, wantRemoved)
	}
}

func TestAppendChromeTestingInfobarSuppressArgAddsTestTypeAndDisableInfobars(t *testing.T) {
	t.Parallel()

	got := appendChromeTestingInfobarSuppressArg([]string{"--user-data-dir=D:\\profiles\\demo"}, false)
	want := []string{"--user-data-dir=D:\\profiles\\demo", "--test-type", "--disable-infobars"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("appendChromeTestingInfobarSuppressArg mismatch: got=%v want=%v", got, want)
	}
}

func TestAppendChromeTestingInfobarSuppressArgDoesNotDuplicateSuppressArgs(t *testing.T) {
	t.Parallel()

	input := []string{"--user-data-dir=D:\\profiles\\demo", "--test-type", "--disable-infobars"}
	got := appendChromeTestingInfobarSuppressArg(input, false)
	if !reflect.DeepEqual(got, input) {
		t.Fatalf("appendChromeTestingInfobarSuppressArg should preserve existing suppress args: got=%v want=%v", got, input)
	}
}

func TestAppendChromeTestingInfobarSuppressArgCloakIncludesTestType(t *testing.T) {
	t.Parallel()

	// 用户接受 fingerprint.com Bot type=google 红灯换 infobar 体验，
	// cloak 路径也加 --test-type 压住 "unsupported command line" 黄条。
	got := appendChromeTestingInfobarSuppressArg([]string{"--user-data-dir=D:\\profiles\\demo"}, true)
	want := []string{"--user-data-dir=D:\\profiles\\demo", "--test-type", "--disable-infobars"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cloak path should include --test-type: got=%v want=%v", got, want)
	}
}

func TestAppendDefaultSearchProviderLaunchArgsEnabled(t *testing.T) {
	t.Parallel()

	got := appendDefaultSearchProviderLaunchArgs([]string{"--user-data-dir=D:\\profiles\\demo"}, true)
	want := []string{
		"--user-data-dir=D:\\profiles\\demo",
		defaultSearchProviderNameArg,
		defaultSearchProviderKeywordArg,
		defaultSearchProviderSearchArg,
		defaultSearchProviderSuggestArg,
		defaultSearchProviderEncodingArg,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("appendDefaultSearchProviderLaunchArgs mismatch: got=%v want=%v", got, want)
	}
}

func TestAppendDefaultSearchProviderLaunchArgsDisabled(t *testing.T) {
	t.Parallel()

	input := []string{"--user-data-dir=D:\\profiles\\demo"}
	got := appendDefaultSearchProviderLaunchArgs(input, false)
	if !reflect.DeepEqual(got, input) {
		t.Fatalf("appendDefaultSearchProviderLaunchArgs should preserve args when disabled: got=%v want=%v", got, input)
	}
}

func mustListenLoopback(t *testing.T) net.Listener {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听测试端口失败: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	return ln
}

func listenerPort(t *testing.T, ln net.Listener) int {
	t.Helper()

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("解析监听地址失败: %T", ln.Addr())
	}
	return tcpAddr.Port
}

func shortLivedCommand() *exec.Cmd {
	if goruntime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "exit", "0")
	}
	return exec.Command("sh", "-c", "exit 0")
}

func longLivedCommand(duration time.Duration) *exec.Cmd {
	if goruntime.GOOS == "windows" {
		seconds := int(duration / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		return exec.Command("cmd", "/c", fmt.Sprintf("ping -n %d 127.0.0.1 >nul", seconds+1))
	}
	return exec.Command("sh", "-c", fmt.Sprintf("sleep %.1f", duration.Seconds()))
}

func stderrFailingCommand(message string) *exec.Cmd {
	if goruntime.GOOS == "windows" {
		return exec.Command("cmd", "/c", fmt.Sprintf("echo %s 1>&2 & exit 5", message))
	}
	return exec.Command("sh", "-c", fmt.Sprintf("echo '%s' 1>&2; exit 5", message))
}

func stderrPortCommand(port int, holdFor time.Duration) *exec.Cmd {
	if goruntime.GOOS == "windows" {
		seconds := int(holdFor / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		// ping -n N waits roughly N-1 seconds on Windows.
		return exec.Command("cmd", "/c", fmt.Sprintf("echo DevTools listening on ws://127.0.0.1:%d/devtools/browser/test 1>&2 & ping -n %d 127.0.0.1 >nul", port, seconds+1))
	}
	return exec.Command("sh", "-c", fmt.Sprintf("echo 'DevTools listening on ws://127.0.0.1:%d/devtools/browser/test' 1>&2; sleep %.1f", port, holdFor.Seconds()))
}

func waitForCondition(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("等待条件成立超时")
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()

	ln := mustListenLoopback(t)
	port := listenerPort(t, ln)
	_ = ln.Close()
	return port
}

type devToolsTestServer struct {
	port   int
	server *http.Server
	done   chan struct{}
}

func startDevToolsServer(t *testing.T, handler http.Handler) *devToolsTestServer {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动 DevTools 测试服务失败: %v", err)
	}

	srv := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ln)
	}()

	return &devToolsTestServer{
		port:   listenerPort(t, ln),
		server: srv,
		done:   done,
	}
}

func startDevToolsServerOnPort(t *testing.T, port int, handler http.Handler) *devToolsTestServer {
	t.Helper()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("在指定端口启动 DevTools 测试服务失败: %v", err)
	}

	srv := &http.Server{Handler: handler}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ln)
	}()

	return &devToolsTestServer{
		port:   port,
		server: srv,
		done:   done,
	}
}

func (s *devToolsTestServer) Close() error {
	if s == nil || s.server == nil {
		return nil
	}
	err := s.server.Close()
	<-s.done
	return err
}

func writeDevToolsActivePortFile(t *testing.T, userDataDir string, port int) {
	t.Helper()

	content := fmt.Sprintf("%d\n/devtools/browser/test\n", port)
	if err := os.WriteFile(filepath.Join(userDataDir, "DevToolsActivePort"), []byte(content), 0644); err != nil {
		t.Fatalf("写入 DevToolsActivePort 失败: %v", err)
	}
}
