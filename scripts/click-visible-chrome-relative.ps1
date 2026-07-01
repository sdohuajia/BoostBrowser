param(
  [int]$RelX,
  [int]$RelY
)
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class ChromeClick {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, UIntPtr dwExtraInfo);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left; public int Top; public int Right; public int Bottom; }
  public const uint MOUSEEVENTF_LEFTDOWN = 0x0002;
  public const uint MOUSEEVENTF_LEFTUP   = 0x0004;
}
"@
$ErrorActionPreference='Stop'
$p = Get-Process chrome | Where-Object { $_.MainWindowTitle -ne '' } | Select-Object -First 1
if (-not $p) { throw 'visible chrome window not found' }
$rect = New-Object ChromeClick+RECT
[ChromeClick]::GetWindowRect($p.MainWindowHandle, [ref]$rect) | Out-Null
$x = $rect.Left + $RelX
$y = $rect.Top + $RelY
[ChromeClick]::SetForegroundWindow($p.MainWindowHandle) | Out-Null
Start-Sleep -Milliseconds 200
[ChromeClick]::SetCursorPos($x, $y) | Out-Null
Start-Sleep -Milliseconds 80
[ChromeClick]::mouse_event([ChromeClick]::MOUSEEVENTF_LEFTDOWN, 0,0,0,[UIntPtr]::Zero)
Start-Sleep -Milliseconds 50
[ChromeClick]::mouse_event([ChromeClick]::MOUSEEVENTF_LEFTUP, 0,0,0,[UIntPtr]::Zero)
Write-Host "CLICKED $x,$y title=$($p.MainWindowTitle)"
