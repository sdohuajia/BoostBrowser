$ErrorActionPreference='Stop'
Get-Process chrome | Where-Object { $_.MainWindowTitle -ne '' } | Select-Object Id,MainWindowTitle,MainWindowHandle | Format-List
