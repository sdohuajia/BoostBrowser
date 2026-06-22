param(
  [string]$TitleLike = 'Phantom - Chrome',
  [string]$Keys = '%+p'
)
$ErrorActionPreference = 'Stop'
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class WinFocus {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
}
"@
$p = Get-Process chrome | Where-Object { $_.MainWindowTitle -like "*$TitleLike*" } | Select-Object -First 1
if (-not $p) { throw "chrome window not found: $TitleLike" }
[WinFocus]::ShowWindow($p.MainWindowHandle, 5) | Out-Null
Start-Sleep -Milliseconds 200
[WinFocus]::SetForegroundWindow($p.MainWindowHandle) | Out-Null
Start-Sleep -Milliseconds 300
$wshell = New-Object -ComObject WScript.Shell
$wshell.SendKeys($Keys)
Write-Host "SENT $Keys to PID=$($p.Id) HWND=$($p.MainWindowHandle)"
