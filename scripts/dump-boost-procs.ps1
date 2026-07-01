$ErrorActionPreference = 'Stop'
Get-CimInstance Win32_Process |
  Where-Object { $_.Name -match '^(boost-browser|chrome|AntigravityAds|BoostLogin)\.exe$' } |
  Select-Object ProcessId, ParentProcessId, Name, ExecutablePath, CommandLine |
  Sort-Object Name, ProcessId |
  Format-List
