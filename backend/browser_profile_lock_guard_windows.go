//go:build windows
// +build windows

package backend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func ensureBrowserUserDataDirReadyForFreshLaunch(chromeBinaryPath string, userDataDir string) error {
	userDataDir = strings.TrimSpace(userDataDir)
	if userDataDir == "" {
		return nil
	}
	if err := failIfUserDataDirOwnedByLiveBrowser(userDataDir); err != nil {
		return err
	}
	if err := clearBrowserSingletonArtifacts(userDataDir); err != nil {
		return err
	}
	return nil
}

func failIfUserDataDirOwnedByLiveBrowser(userDataDir string) error {
	psScript := `param([string]$UserDataDir)
$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($UserDataDir) -or -not (Test-Path -LiteralPath $UserDataDir)) { exit 0 }
$target = [System.IO.Path]::GetFullPath($UserDataDir)
$needle1 = ('--user-data-dir="' + $target + '"').ToLowerInvariant()
$needle2 = ('--user-data-dir=' + $target).ToLowerInvariant()
$matches = @()
Get-CimInstance Win32_Process | ForEach-Object {
  $proc = $_
  if (-not $proc.CommandLine) { return }
  $cmd = $proc.CommandLine.ToLowerInvariant()
  if ($cmd.Contains($needle1) -or $cmd.Contains($needle2)) {
    $matches += $proc
  }
}
$matches = @($matches | Sort-Object ProcessId)
if ($matches.Count -gt 0) {
  $first = $matches[0]
  $exe = ''
  if ($first.ExecutablePath) { $exe = $first.ExecutablePath }
  Write-Output ($first.ProcessId.ToString() + '|' + $first.Name + '|' + $exe)
}
`
	output, err := runPowerShellScript(psScript, 6*time.Second, "-UserDataDir", userDataDir)
	if err != nil {
		return fmt.Errorf("实例启动失败：检测用户目录占用状态失败。用户目录：%s。原因：%w。", userDataDir, err)
	}
	line := strings.TrimSpace(output)
	if line == "" {
		return nil
	}
	parts := strings.SplitN(line, "|", 3)
	pid := parts[0]
	name := "browser"
	if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
		name = strings.TrimSpace(parts[1])
	}
	exePath := ""
	if len(parts) >= 3 {
		exePath = strings.TrimSpace(parts[2])
	}
	if exePath != "" {
		return fmt.Errorf("实例启动失败：该实例的用户目录已被现有浏览器进程占用。用户目录：%s。占用进程：%s（PID=%s，%s）。请先关闭这个浏览器窗口后重试。", userDataDir, name, pid, exePath)
	}
	return fmt.Errorf("实例启动失败：该实例的用户目录已被现有浏览器进程占用。用户目录：%s。占用进程：%s（PID=%s）。请先关闭这个浏览器窗口后重试。", userDataDir, name, pid)
}

func clearBrowserSingletonArtifacts(userDataDir string) error {
	var errs []string
	for _, name := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
		artifactPath := filepath.Join(userDataDir, name)
		if err := os.Remove(artifactPath); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("%s: %v", artifactPath, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("实例启动失败：无法清理浏览器用户目录中的 Singleton 锁文件。用户目录：%s。原因：%s。请关闭占用该目录的浏览器进程后重试。", userDataDir, strings.Join(errs, "；"))
	}
	return nil
}

func runPowerShellScript(script string, timeout time.Duration, args ...string) (string, error) {
	tempFile, err := os.CreateTemp("", "boost-browser-*.ps1")
	if err != nil {
		return "", err
	}
	scriptPath := tempFile.Name()
	if _, err := tempFile.WriteString(script); err != nil {
		tempFile.Close()
		_ = os.Remove(scriptPath)
		return "", err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(scriptPath)
		return "", err
	}
	defer os.Remove(scriptPath)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	powershellPath := `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
	if _, err := os.Stat(powershellPath); err != nil {
		fallbackPath, lookErr := exec.LookPath("powershell.exe")
		if lookErr != nil {
			return "", fmt.Errorf("未找到 powershell.exe")
		}
		powershellPath = fallbackPath
	}

	cmdArgs := []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Clean(scriptPath)}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.CommandContext(ctx, powershellPath, cmdArgs...)
	hideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("PowerShell 执行超时")
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("%s", message)
	}
	return strings.TrimSpace(string(output)), nil
}
