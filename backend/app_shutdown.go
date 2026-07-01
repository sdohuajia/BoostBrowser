package backend

import (
	"boost-browser/backend/internal/logger"
	"fmt"
	"os/exec"
)

type browserProcessSnapshot struct {
	profileID string
	cmd       *exec.Cmd
}

func (a *App) stopRuntimeServices() {
	a.stopServicesOnce.Do(func() {
		if a.speedScheduler != nil {
			a.speedScheduler.Stop()
			a.speedScheduler = nil
		}

		// 跟随上游 Ant-Browser：退出时按顺序停止浏览器实例，避免并发 taskkill
		// 和残留进程扫描把 Wails 主进程/子进程状态打乱，造成主程序闪退或 watchdog 重启。
		a.stopTrackedBrowserProcesses()

		if a.xrayMgr != nil {
			a.xrayMgr.StopAll()
		}
		a.clearProfileXrayBridges()

		if a.clashMgr != nil {
			a.clashMgr.StopAll()
		}
		if a.singboxMgr != nil {
			a.singboxMgr.StopAll()
		}
		if a.standardRelayMgr != nil {
			a.standardRelayMgr.StopAll()
		}
	})
}

func (a *App) stopTrackedBrowserProcesses() {
	if a.browserMgr == nil {
		return
	}

	a.browserMgr.Mutex.Lock()
	cmds := make([]*exec.Cmd, 0, len(a.browserMgr.BrowserProcesses))
	for _, cmd := range a.browserMgr.BrowserProcesses {
		cmds = append(cmds, cmd)
	}
	a.browserMgr.Mutex.Unlock()

	for _, cmd := range cmds {
		_ = a.stopProcessCmd(cmd)
	}

	a.browserMgr.Mutex.Lock()
	defer a.browserMgr.Mutex.Unlock()

	for profileID, profile := range a.browserMgr.Profiles {
		if profile == nil {
			continue
		}
		if profile.Running || a.browserMgr.BrowserProcesses[profileID] != nil {
			a.markProfileStoppedLocked(profileID, profile)
		}
	}
	a.browserMgr.BrowserProcesses = make(map[string]*exec.Cmd)
}

func (a *App) finalizeShutdown() {
	a.finalizeOnce.Do(func() {
		// 跟随上游 Ant-Browser 的生命周期：运行期服务/浏览器先停，数据库随后关闭，
		// launch server 最后关闭，避免 shutdown 过程中仍有 relay/state goroutine 访问已关闭服务。
		if a.db != nil {
			a.db.Close()
			a.db = nil
		}
		if a.launchServer != nil {
			_ = a.launchServer.Stop()
			a.launchServer = nil
		}
		if err := logger.Close(); err != nil {
			fmt.Printf("关闭日志系统失败: %v\n", err)
		}
	})
}
