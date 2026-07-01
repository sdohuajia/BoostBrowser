Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$ErrorActionPreference = 'Stop'
$p = Get-Process boost-browser | Select-Object -First 1
if (-not $p) { throw 'boost-browser not running' }
$root = [System.Windows.Automation.AutomationElement]::FromHandle($p.MainWindowHandle)
$trueCond = [System.Windows.Automation.Condition]::TrueCondition
$els = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, $trueCond)
$out = New-Object System.Collections.Generic.List[string]
foreach ($el in $els) {
    try {
        $name = $el.Current.Name
        $type = $el.Current.LocalizedControlType
        if ($name -or $type) {
            $out.Add("[$type] $name")
        }
    } catch {}
}
$out | Select-Object -First 200 | Out-String -Width 260 | Write-Host
