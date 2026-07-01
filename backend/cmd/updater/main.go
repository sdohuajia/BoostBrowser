// updater.exe —— 独立的小工具，专门负责替换主程序 exe 后重启。
//
// 调用方式（由主程序 ApplyUpdate 启动）：
//   updater.exe <main_pid> <old_exe_path> <new_exe_path>
//
// 流程：
//   1. 等主进程退出（最多 30s，超时强杀）
//   2. 把 old.exe 重命名为 old.exe.bak
//   3. 把 new.exe 重命名为 old.exe
//   4. 启动新版（带 --post-update 参数）
//   5. 等 30 秒看新进程是否写 .update_success 标记
//   6. 写了 → 删 .bak，更新成功；没写 → 回滚 .bak，启动旧版
//   7. 自删（schtasks 异步）

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	successMarker  = ".update_success"
	waitMainExitS  = 30
	waitNewBootS   = 30
	updaterLogName = "updater.log"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: updater.exe <pid> <old_exe> <new_exe>")
		os.Exit(2)
	}
	mainPID, _ := strconv.Atoi(os.Args[1])
	oldExe := os.Args[2]
	newExe := os.Args[3]

	logf := openLog(oldExe)
	defer func() {
		if logf != nil {
			logf.Close()
		}
	}()

	logln(logf, "==== updater start ====")
	logln(logf, fmt.Sprintf("pid=%d old=%s new=%s", mainPID, oldExe, newExe))

	// 1. 等主进程退出
	if !waitProcessExit(mainPID, waitMainExitS) {
		logln(logf, fmt.Sprintf("main pid %d 未在 %ds 内退出，强杀", mainPID, waitMainExitS))
		_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(mainPID)).Run()
		time.Sleep(2 * time.Second)
	}

	// 多等 1s 让 OS 释放文件句柄
	time.Sleep(1 * time.Second)

	// 2. 备份旧 exe
	bakPath := oldExe + ".bak"
	_ = os.Remove(bakPath)
	if err := os.Rename(oldExe, bakPath); err != nil {
		logln(logf, fmt.Sprintf("备份 old→bak 失败：%v", err))
		// 可能 Windows 还在锁文件，再等一下重试
		time.Sleep(3 * time.Second)
		if err2 := os.Rename(oldExe, bakPath); err2 != nil {
			logln(logf, fmt.Sprintf("重试备份仍失败：%v，放弃", err2))
			os.Exit(3)
		}
	}

	// 3. 用新 exe 替换旧 exe
	if err := os.Rename(newExe, oldExe); err != nil {
		logln(logf, fmt.Sprintf("替换 new→old 失败：%v，回滚", err))
		_ = os.Rename(bakPath, oldExe)
		os.Exit(4)
	}

	// 旧实现是在启动新版之后才删除 marker；如果新版启动很快写入 marker，
	// updater 会把它删掉，然后误判失败回滚。必须在 Start 前清理。
	markerPath := filepath.Join(filepath.Dir(oldExe), "data", successMarker)
	_ = os.Remove(markerPath)

	// 4. 启动新版
	cmd := exec.Command(oldExe, "--post-update")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
		CreationFlags: 0x00000008 | 0x00000200,
	}
	cmd.Dir = filepath.Dir(oldExe)
	if err := cmd.Start(); err != nil {
		logln(logf, fmt.Sprintf("启动新版失败：%v，回滚", err))
		_ = os.Remove(oldExe)
		_ = os.Rename(bakPath, oldExe)
		_ = exec.Command("cmd", "/C", "start", "", oldExe).Start()
		os.Exit(5)
	}
	newPID := cmd.Process.Pid
	logln(logf, fmt.Sprintf("新版已启动 pid=%d", newPID))

	// 5. 等 success marker
	deadline := time.Now().Add(waitNewBootS * time.Second)
	success := false
	for time.Now().Before(deadline) {
		if _, err := os.Stat(markerPath); err == nil {
			success = true
			break
		}
		if !processAlive(newPID) {
			logln(logf, "新版进程已退出但未写 marker")
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !success {
		// 6a. 回滚
		logln(logf, "新版启动失败，回滚到旧版本")
		_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(newPID)).Run()
		time.Sleep(2 * time.Second)
		_ = os.Remove(oldExe)
		if err := os.Rename(bakPath, oldExe); err != nil {
			logln(logf, fmt.Sprintf("回滚 rename 失败：%v", err))
		}
		// 起回旧版
		rollback := exec.Command(oldExe)
		rollback.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: 0x00000008 | 0x00000200,
		}
		rollback.Dir = filepath.Dir(oldExe)
		_ = rollback.Start()
	} else {
		// 6b. 成功，清理
		logln(logf, "升级成功，清理 .bak")
		_ = os.Remove(bakPath)
		_ = os.Remove(markerPath)
	}

	// 7. 结束。注意：updater.exe 是安装目录里的常驻组件，下一次增量升级还要继续复用，
	// 绝不能在本次升级完成后把自己删掉；否则当前这次升级虽然成功，下一次点“重启更新”
	// 就会因为 updater.exe 缺失而直接失败。
	logln(logf, "==== updater exit ====")
	if logf != nil {
		logf.Close()
	}
}

func openLog(oldExe string) *os.File {
	logDir := filepath.Join(filepath.Dir(oldExe), "data", "logs")
	_ = os.MkdirAll(logDir, 0755)
	f, err := os.OpenFile(filepath.Join(logDir, updaterLogName),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil
	}
	return f
}

func logln(f *os.File, s string) {
	line := fmt.Sprintf("[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), s)
	if f != nil {
		_, _ = f.WriteString(line)
	}
	fmt.Fprint(os.Stderr, line)
}

func waitProcessExit(pid int, sec int) bool {
	deadline := time.Now().Add(time.Duration(sec) * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return !processAlive(pid)
}

// processAlive 通过 tasklist 查询 pid 是否活着
func processAlive(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH", "/FO", "CSV").Output()
	if err != nil {
		return false
	}
	// 不存在时输出 "信息: 没有运行..." / "INFO: No tasks..."；存在时输出 CSV 行
	s := string(out)
	if len(s) < 30 {
		return false
	}
	// CSV 行包含进程名（带引号）
	return s[0] == '"'
}
