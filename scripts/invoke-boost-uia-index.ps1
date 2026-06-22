param(
  [Parameter(Mandatory=$true)]
  [string]$Name,
  [int]$Index = 0
)
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$ErrorActionPreference = 'Stop'
$p = Get-Process boost-browser | Select-Object -First 1
if (-not $p) { throw 'boost-browser not running' }
$root = [System.Windows.Automation.AutomationElement]::FromHandle($p.MainWindowHandle)
$cond = New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::NameProperty, $Name)
$els = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, $cond)
if (-not $els -or $els.Count -le $Index) { throw "UI element not found: $Name #$Index" }
$el = $els.Item($Index)
$invoke = $el.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern)
$invoke.Invoke()
Write-Host "INVOKED $Name #$Index"
