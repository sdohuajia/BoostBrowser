param(
  [Parameter(Mandatory=$true)]
  [string]$Name
)
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$ErrorActionPreference = 'Stop'
$p = Get-Process boost-browser | Select-Object -First 1
if (-not $p) { throw 'boost-browser not running' }
$root = [System.Windows.Automation.AutomationElement]::FromHandle($p.MainWindowHandle)
$cond = New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::NameProperty, $Name)
$el = $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $cond)
if (-not $el) { throw "UI element not found: $Name" }
try {
  $invoke = $el.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern)
  $invoke.Invoke()
  Write-Host "INVOKED $Name"
} catch {
  $r = $el.Current.BoundingRectangle
  Add-Type @"
using System;
using System.Runtime.InteropServices;
public class WinClickFallback {
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, UIntPtr dwExtraInfo);
  public const uint MOUSEEVENTF_LEFTDOWN = 0x0002;
  public const uint MOUSEEVENTF_LEFTUP   = 0x0004;
}
"@
  $x = [int](($r.Left + $r.Right)/2)
  $y = [int](($r.Top + $r.Bottom)/2)
  [WinClickFallback]::SetCursorPos($x, $y) | Out-Null
  Start-Sleep -Milliseconds 100
  [WinClickFallback]::mouse_event([WinClickFallback]::MOUSEEVENTF_LEFTDOWN,0,0,0,[UIntPtr]::Zero)
  Start-Sleep -Milliseconds 60
  [WinClickFallback]::mouse_event([WinClickFallback]::MOUSEEVENTF_LEFTUP,0,0,0,[UIntPtr]::Zero)
  Write-Host "CLICKED $Name at $x,$y"
}
