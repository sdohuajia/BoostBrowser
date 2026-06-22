Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$ErrorActionPreference = 'Stop'
$name = [string]([char]0x7A97) + [char]0x53E3 + [char]0x540C + [char]0x6B65
$p = Get-Process boost-browser | Select-Object -First 1
if (-not $p) { throw 'boost-browser not running' }
$root = [System.Windows.Automation.AutomationElement]::FromHandle($p.MainWindowHandle)
$cond = New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::NameProperty, $name)
$el = $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $cond)
if (-not $el) { throw 'sync nav not found' }
$invoke = $el.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern)
$invoke.Invoke()
Write-Host 'SYNC_NAV_OK'
