$ErrorActionPreference = 'Stop'

# Boost Browser 完整版安装包（带 chromium 内核 + 代理二进制 + 资源）
#
# 与 build_release.ps1（薄安装包，~17MB，仅 wails app + updater）独立，
# 不互相覆盖，输出到 build\release\Boost-Browser-Setup-Full-v<ver>.exe。
#
# 内容（对齐老 BoostBrowser_cloak_Setup.exe 425MB 的功能集）：
#   - boost-browser.exe          ← 刚 wails build 出来的最新版
#   - updater.exe                ← 刚 go build 出来的最新版
#   - chrome\cloak-146.0.7680.177\ ← cloak chromium 内核（默认）
#   - chrome\google-148.0.7778.167\ ← google chrome 备用内核
#   - bin\sing-box.exe + bin\xray.exe ← 代理后端
#   - app.ico + app.png          ← 应用图标资源
#
# 使用：
#   powershell -ExecutionPolicy Bypass -File scripts\build_full_installer.ps1
#   可选参数:
#     -SkipBuild        跳过 wails+go 编译，直接用 build\release\ 现有产物
#     -KernelSrc <dir>  覆盖 chromium 内核源（默认 <kernel-source>）

# 配置（用环境变量覆盖，避免 param block 在某些 PowerShell 版本下的怪异行为）
#   $env:BOOST_SKIP_BUILD = '1'    跳过 wails+go 编译，直接复用 build\release\
#   $env:BOOST_KERNEL_SRC = 'D:\xxx'  覆盖 chromium 内核源
$SkipBuild = ($env:BOOST_SKIP_BUILD -eq '1')
$KernelSrc = $env:BOOST_KERNEL_SRC

$RepoRoot   = Split-Path -Parent $PSScriptRoot
$ReleaseDir = Join-Path $RepoRoot 'build\release'
$Stage      = 'C:\Temp\BoostBrowser_full_staging'
$NshPath    = Join-Path $RepoRoot 'build\release\full_installer_files.nsh'
$NsiPath    = Join-Path $RepoRoot 'build\release\full_installer.nsi'
$Icon       = Join-Path $RepoRoot 'build\windows\icon.ico'
$SidebarBmp = Join-Path $RepoRoot 'build\release\full_sidebar.bmp'
$HeaderBmp  = Join-Path $RepoRoot 'build\release\full_header.bmp'

Set-Location $RepoRoot

if (!(Test-Path $Icon))      { throw "Missing icon: $Icon" }
if ([string]::IsNullOrWhiteSpace($KernelSrc)) { throw "Set BOOST_KERNEL_SRC to the directory containing the Chromium kernels and runtime assets." }
if (!(Test-Path $KernelSrc)) { throw "Missing kernel source dir: $KernelSrc" }

# 读 wails.json 拿版本号
$wailsJson = Get-Content "$RepoRoot\wails.json" -Raw | ConvertFrom-Json
$Version = $wailsJson.info.productVersion
if (-not $Version) { throw "wails.json 中未找到 info.productVersion" }
$OutExe = Join-Path $ReleaseDir "Boost-Browser-Setup-Full-v$Version.exe"
Write-Host "==> 目标版本: v$Version" -ForegroundColor Cyan
Write-Host "==> 输出: $OutExe" -ForegroundColor Cyan

# ---------------------------------------------------------------------------
# [1/7] 先跑 build_release.ps1 编译最新 boost-browser.exe + updater.exe
# ---------------------------------------------------------------------------
if (-not $SkipBuild) {
    Write-Host "`n==> [1/7] 调用 build_release.ps1 编译最新 boost-browser.exe + updater.exe" -ForegroundColor Yellow
    & powershell -NoProfile -ExecutionPolicy Bypass -File "$RepoRoot\scripts\build_release.ps1"
    if ($LASTEXITCODE -ne 0) { throw "build_release.ps1 失败" }
} else {
    Write-Host "`n==> [1/7] -SkipBuild，跳过编译" -ForegroundColor DarkYellow
}

$BoostExe   = Join-Path $ReleaseDir 'boost-browser.exe'
$UpdaterExe = Join-Path $ReleaseDir 'updater.exe'
if (!(Test-Path $BoostExe))   { throw "Missing $BoostExe" }
if (!(Test-Path $UpdaterExe)) { throw "Missing $UpdaterExe" }

# ---------------------------------------------------------------------------
# [2/7] Stage 资源到 C:\Temp（NSIS 处理含点号路径或 <legacy-path> 网络路径偶发问题）
# ---------------------------------------------------------------------------
Write-Host "`n==> [2/7] Staging -> $Stage" -ForegroundColor Yellow
Remove-Item -Recurse -Force $Stage -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $Stage | Out-Null

# 主程序 + updater
Copy-Item $BoostExe   $Stage -Force
Copy-Item $UpdaterExe $Stage -Force

# 资源文件（图标、png）
$kernelIco = Join-Path $KernelSrc 'app.ico'
$kernelPng = Join-Path $KernelSrc 'app.png'
if (Test-Path $kernelIco) { Copy-Item $kernelIco $Stage -Force }
if (Test-Path $kernelPng) { Copy-Item $kernelPng $Stage -Force }

# bin\ 代理后端
$binStage = Join-Path $Stage 'bin'
New-Item -ItemType Directory -Force -Path $binStage | Out-Null
foreach ($exe in @('sing-box.exe', 'xray.exe')) {
    $src = Join-Path $KernelSrc "bin\$exe"
    if (Test-Path -LiteralPath $src) {
        Copy-Item -LiteralPath $src -Destination $binStage -Force
    } else {
        Write-Host "    -> 警告: 未找到 $src" -ForegroundColor DarkYellow
    }
}

# chrome\ 内核：cloak-146 必带，google-148 可选
$chromeStage = Join-Path $Stage 'chrome'
New-Item -ItemType Directory -Force -Path $chromeStage | Out-Null
$cloakKernel  = Join-Path $KernelSrc 'chrome\cloak-146.0.7680.177'
$googleKernel = Join-Path $KernelSrc 'chrome\google-148.0.7778.167'
if (!(Test-Path -LiteralPath $cloakKernel)) { throw "Missing cloak kernel: $cloakKernel" }
Write-Host "    -> 复制 cloak-146.0.7680.177 ..." -ForegroundColor Gray
Copy-Item -LiteralPath $cloakKernel -Destination $chromeStage -Recurse -Force
if (Test-Path -LiteralPath $googleKernel) {
    Write-Host "    -> 复制 google-148.0.7778.167 ..." -ForegroundColor Gray
    Copy-Item -LiteralPath $googleKernel -Destination $chromeStage -Recurse -Force
}

# 清理混入 staging 的临时锁文件
Get-ChildItem $Stage -Recurse -File -Force | Where-Object {
    $_.Name -in @('LOCK', 'LOG', 'LOG.old') -or $_.Name -like '*.tmp'
} | Remove-Item -Force -ErrorAction SilentlyContinue

$stageStats = Get-ChildItem $Stage -Recurse -File -Force | Measure-Object -Property Length -Sum
Write-Host ("    -> Stage: {0:N0} files, {1:N1} MB" -f $stageStats.Count, ($stageStats.Sum / 1MB)) -ForegroundColor Green

# ---------------------------------------------------------------------------
# [3/7] 生成 NSIS 用 BMP 资源（深蓝渐变，简洁品牌风）
# ---------------------------------------------------------------------------
Write-Host "`n==> [3/7] 生成 NSIS BMP 资源" -ForegroundColor Yellow
Add-Type -AssemblyName System.Drawing
function New-GradientBitmap([string]$Path, [int]$Width, [int]$Height, [bool]$Header) {
    $bmp = New-Object System.Drawing.Bitmap $Width, $Height
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $rect = New-Object System.Drawing.Rectangle 0, 0, $Width, $Height
    $c1 = [System.Drawing.Color]::FromArgb(18, 28, 48)
    $c2 = [System.Drawing.Color]::FromArgb(42, 92, 170)
    $brush = New-Object System.Drawing.Drawing2D.LinearGradientBrush $rect, $c1, $c2, 45
    $g.FillRectangle($brush, $rect)
    $brush.Dispose()
    $accent = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(90, 200, 255))
    $white  = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(245, 250, 255))
    $muted  = New-Object System.Drawing.SolidBrush ([System.Drawing.Color]::FromArgb(175, 205, 235))
    if ($Header) {
        $font  = New-Object System.Drawing.Font 'Segoe UI', 10, ([System.Drawing.FontStyle]::Bold)
        $font2 = New-Object System.Drawing.Font 'Segoe UI', 7
        $g.DrawString('Boost Browser', $font, $white, 12, 7)
        $g.DrawString("Full installer v$Version", $font2, $muted, 12, 29)
        $g.FillEllipse($accent, $Width - 44, 10, 24, 24)
        $font.Dispose(); $font2.Dispose()
    } else {
        $font  = New-Object System.Drawing.Font 'Segoe UI', 18, ([System.Drawing.FontStyle]::Bold)
        $font2 = New-Object System.Drawing.Font 'Segoe UI', 8
        $g.DrawString('Boost', $font, $white, 18, 32)
        $g.DrawString('Browser', $font, $white, 18, 60)
        $g.DrawString('Fingerprint browser', $font2, $muted, 20, 108)
        $g.DrawString('Chromium 146 + proxy', $font2, $muted, 20, 128)
        $g.FillEllipse($accent, 102, 180, 34, 34)
        $g.FillEllipse($accent, 122, 205, 16, 16)
        $font.Dispose(); $font2.Dispose()
    }
    $accent.Dispose(); $white.Dispose(); $muted.Dispose(); $g.Dispose()
    $bmp.Save($Path, [System.Drawing.Imaging.ImageFormat]::Bmp)
    $bmp.Dispose()
}
New-GradientBitmap $SidebarBmp 164 314 $false
New-GradientBitmap $HeaderBmp  150  57 $true

# ---------------------------------------------------------------------------
# [4/7] 生成 NSIS File 清单（关键：避开 File /r 在含点号路径上的静默失败）
# ---------------------------------------------------------------------------
Write-Host "`n==> [4/7] 生成 NSIS File 指令清单" -ForegroundColor Yellow
$out = New-Object System.Collections.Generic.List[string]
function Escape-Nsis([string]$s) { return $s.Replace('$', '$$').Replace('`', '``') }
function Process-Dir([string]$dir, [string]$relPath) {
    if ($relPath -eq '') { $out.Add('SetOutPath "$INSTDIR"') }
    else { $out.Add('SetOutPath "$INSTDIR\' + (Escape-Nsis $relPath) + '"') }

    Get-ChildItem -LiteralPath $dir -File -Force | Sort-Object Name | ForEach-Object {
        $out.Add('File "' + (Escape-Nsis $_.FullName) + '"')
    }
    Get-ChildItem -LiteralPath $dir -Directory -Force | Sort-Object Name | ForEach-Object {
        $childRel = if ($relPath -eq '') { $_.Name } else { $relPath + '\' + $_.Name }
        Process-Dir $_.FullName $childRel
    }
}
Process-Dir $Stage ''
Set-Content -Path $NshPath -Value $out -Encoding Unicode
Write-Host ("    -> {0} 条 NSIS 指令 -> {1}" -f $out.Count, $NshPath) -ForegroundColor Green

# ---------------------------------------------------------------------------
# [5/7] 生成 .nsi 主脚本（UTF-16 LE，让 NSIS 正确解析中文）
# ---------------------------------------------------------------------------
Write-Host "`n==> [5/7] 生成 .nsi 主脚本" -ForegroundColor Yellow
$nsi = @"
Unicode True
!define PRODUCT_NAME "Boost Browser"
!define PRODUCT_EXE "boost-browser.exe"
!define PRODUCT_VERSION "$Version"
!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\BoostBrowser"
!define APP_ICON "$Icon"
!define SIDEBAR_BMP "$SidebarBmp"
!define HEADER_BMP "$HeaderBmp"

!include "MUI2.nsh"
!include "LogicLib.nsh"

Name "`${PRODUCT_NAME} `${PRODUCT_VERSION}"
OutFile "$OutExe"
InstallDir "`$LOCALAPPDATA\Programs\Boost Browser"
InstallDirRegKey HKCU "`${UNINSTALL_KEY}" "InstallLocation"
RequestExecutionLevel user
SetCompressor zlib
Icon "`${APP_ICON}"
UninstallIcon "`${APP_ICON}"
!define MUI_ICON "`${APP_ICON}"
!define MUI_UNICON "`${APP_ICON}"
!define MUI_WELCOMEFINISHPAGE_BITMAP "`${SIDEBAR_BMP}"
!define MUI_HEADERIMAGE
!define MUI_HEADERIMAGE_BITMAP "`${HEADER_BMP}"
!define MUI_HEADERIMAGE_UNBITMAP "`${HEADER_BMP}"
!define MUI_ABORTWARNING

!define MUI_WELCOMEPAGE_TITLE "欢迎安装 Boost Browser v`${PRODUCT_VERSION}"
!define MUI_WELCOMEPAGE_TEXT "Boost Browser 是一款指纹浏览器，安装包内含：`$\r`$\n  · Chromium 146 (cloak 内核，默认)`$\r`$\n  · Google Chrome 148 (备用内核)`$\r`$\n  · sing-box / xray 代理后端`$\r`$\n`$\r`$\n安装完成后即可使用，所有用户数据保存在安装目录下。"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "`$INSTDIR\`${PRODUCT_EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "立即启动 Boost Browser"
!define MUI_FINISHPAGE_TITLE "安装完成"
!define MUI_FINISHPAGE_TEXT "默认内核 Chromium 146 已就绪。`$\r`$\n如需切换内核，可在应用内的内核管理界面操作。"
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

Function CloseBoostProcesses
retry_close:
  IfFileExists "`$INSTDIR" 0 done

  ; 先杀子进程（chrome、xray、sing-box），再杀主进程，顺序很重要。
  ExecWait '"`$SYSDIR\taskkill.exe" /F /T /IM chrome.exe'
  ExecWait '"`$SYSDIR\taskkill.exe" /F /T /IM xray.exe'
  ExecWait '"`$SYSDIR\taskkill.exe" /F /T /IM sing-box.exe'
  ExecWait '"`$SYSDIR\taskkill.exe" /F /T /IM updater.exe'
  ExecWait '"`$SYSDIR\taskkill.exe" /F /T /IM `${PRODUCT_EXE}'
  Sleep 600

  ; PowerShell 强制解锁：杀掉 ExecutablePath 在安装目录下的所有进程，
  ; 然后等关键文件可独占打开（chrome.dll 经常被子进程多锁一段时间）。
  FileOpen `$9 "`$TEMP\boost_full_unlock.ps1" w
  FileWrite `$9 "param([string]`$`$InstallDir)`$\r`$\n"
  FileWrite `$9 "`$`$ErrorActionPreference = 'SilentlyContinue'`$\r`$\n"
  FileWrite `$9 "`$`$root = [IO.Path]::GetFullPath(`$`$InstallDir).TrimEnd('\\')`$\r`$\n"
  FileWrite `$9 "`$`$rootSlash = `$`$root + '\\'`$\r`$\n"
  FileWrite `$9 "`$`$critical = @([IO.Path]::Combine(`$`$root, 'boost-browser.exe'))`$\r`$\n"
  FileWrite `$9 "Get-ChildItem -LiteralPath ([IO.Path]::Combine(`$`$root, 'chrome')) -Directory -ErrorAction SilentlyContinue | ForEach-Object { `$`$critical += [IO.Path]::Combine(`$`$_.FullName, 'chrome.exe'); `$`$critical += [IO.Path]::Combine(`$`$_.FullName, 'chrome.dll') }`$\r`$\n"
  FileWrite `$9 "for (`$`$round = 0; `$`$round -lt 5; `$`$round++) {`$\r`$\n"
  FileWrite `$9 "  `$`$procs = Get-CimInstance Win32_Process | Where-Object { `$`$_.ExecutablePath -and ([IO.Path]::GetFullPath(`$`$_.ExecutablePath).StartsWith(`$`$rootSlash, [StringComparison]::OrdinalIgnoreCase)) }`$\r`$\n"
  FileWrite `$9 "  if (`$`$procs.Count -eq 0) { break }`$\r`$\n"
  FileWrite `$9 "  foreach (`$`$p in `$`$procs) { try { & `$`$env:WINDIR\System32\taskkill.exe /F /T /PID `$`$p.ProcessId 2>&1 | Out-Null } catch {} }`$\r`$\n"
  FileWrite `$9 "  Start-Sleep -Milliseconds 800`$\r`$\n"
  FileWrite `$9 "}`$\r`$\n"
  FileWrite `$9 "function Test-Lock([string]`$`$p) { if (!(Test-Path -LiteralPath `$`$p)) { return `$`$false }; try { `$`$fs = [IO.File]::Open(`$`$p, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None); `$`$fs.Close(); return `$`$false } catch { return `$`$true } }`$\r`$\n"
  FileWrite `$9 "for (`$`$i = 0; `$`$i -lt 60; `$`$i++) { `$`$locked = `$`$false; foreach (`$`$t in `$`$critical) { if (Test-Lock `$`$t) { `$`$locked = `$`$true; break } }; if (-not `$`$locked) { exit 0 }; Start-Sleep -Milliseconds 500 }`$\r`$\n"
  FileWrite `$9 "`$`$lockedFiles = @(`$`$critical | Where-Object { Test-Lock `$`$_ })`$\r`$\n"
  FileWrite `$9 "if (`$`$lockedFiles.Count -eq 0) { exit 11 }`$\r`$\n"
  FileWrite `$9 "Add-Type @'`$\r`$\n"
  FileWrite `$9 "using System;`$\r`$\n"
  FileWrite `$9 "using System.Runtime.InteropServices;`$\r`$\n"
  FileWrite `$9 "using System.Runtime.InteropServices.ComTypes;`$\r`$\n"
  FileWrite `$9 "public static class RmUtil {`$\r`$\n"
  FileWrite `$9 "  public const int CCH_RM_MAX_APP_NAME = 255;`$\r`$\n"
  FileWrite `$9 "  public const int CCH_RM_MAX_SVC_NAME = 63;`$\r`$\n"
  FileWrite `$9 "  [StructLayout(LayoutKind.Sequential)] public struct RM_UNIQUE_PROCESS { public int dwProcessId; public FILETIME ProcessStartTime; }`$\r`$\n"
  FileWrite `$9 "  [StructLayout(LayoutKind.Sequential, CharSet=CharSet.Unicode)] public struct RM_PROCESS_INFO { public RM_UNIQUE_PROCESS Process; [MarshalAs(UnmanagedType.ByValTStr, SizeConst=CCH_RM_MAX_APP_NAME + 1)] public string strAppName; [MarshalAs(UnmanagedType.ByValTStr, SizeConst=CCH_RM_MAX_SVC_NAME + 1)] public string strServiceShortName; public int ApplicationType; public uint AppStatus; public uint TSSessionId; [MarshalAs(UnmanagedType.Bool)] public bool bRestartable; }`$\r`$\n"
  FileWrite `$9 "  [DllImport(`$\"rstrtmgr.dll`$\", CharSet=CharSet.Unicode)] public static extern int RmStartSession(out uint pSessionHandle, int dwSessionFlags, string strSessionKey);`$\r`$\n"
  FileWrite `$9 "  [DllImport(`$\"rstrtmgr.dll`$\", CharSet=CharSet.Unicode)] public static extern int RmRegisterResources(uint pSessionHandle, UInt32 nFiles, string[] rgsFilenames, UInt32 nApplications, IntPtr rgApplications, UInt32 nServices, string[] rgsServiceNames);`$\r`$\n"
  FileWrite `$9 "  [DllImport(`$\"rstrtmgr.dll`$\")] public static extern int RmGetList(uint dwSessionHandle, out uint pnProcInfoNeeded, ref uint pnProcInfo, [In, Out] RM_PROCESS_INFO[] rgAffectedApps, ref uint lpdwRebootReasons);`$\r`$\n"
  FileWrite `$9 "  [DllImport(`$\"rstrtmgr.dll`$\")] public static extern int RmEndSession(uint pSessionHandle);`$\r`$\n"
  FileWrite `$9 "}`$\r`$\n"
  FileWrite `$9 "'@`$\r`$\n"
  FileWrite `$9 "`$`$session = 0; `$`$key = [guid]::NewGuid().ToString()`$\r`$\n"
  FileWrite `$9 "`$`$rm = [RmUtil]::RmStartSession([ref]`$`$session, 0, `$`$key)`$\r`$\n"
  FileWrite `$9 "if (`$`$rm -eq 0) {`$\r`$\n"
  FileWrite `$9 "  [RmUtil]::RmRegisterResources(`$`$session, [uint32]`$`$lockedFiles.Count, [string[]]`$`$lockedFiles, 0, [IntPtr]::Zero, 0, `$`$null) | Out-Null`$\r`$\n"
  FileWrite `$9 "  `$`$needed = 0; `$`$count = 0; `$`$reasons = 0`$\r`$\n"
  FileWrite `$9 "  [RmUtil]::RmGetList(`$`$session, [ref]`$`$needed, [ref]`$`$count, `$`$null, [ref]`$`$reasons) | Out-Null`$\r`$\n"
  FileWrite `$9 "  if (`$`$needed -gt 0) {`$\r`$\n"
  FileWrite `$9 "    `$`$arr = New-Object RmUtil+RM_PROCESS_INFO[] `$`$needed`$\r`$\n"
  FileWrite `$9 "    `$`$count = `$`$needed`$\r`$\n"
  FileWrite `$9 "    [RmUtil]::RmGetList(`$`$session, [ref]`$`$needed, [ref]`$`$count, `$`$arr, [ref]`$`$reasons) | Out-Null`$\r`$\n"
  FileWrite `$9 "    `$`$names = @(); foreach (`$`$p in `$`$arr) { if (`$`$p.strAppName) { `$`$names += `$`$p.strAppName } elseif (`$`$p.Process.dwProcessId) { `$`$names += ('PID ' + `$`$p.Process.dwProcessId) } }`$\r`$\n"
  FileWrite `$9 "    `$`$names = @(`$`$names | Select-Object -Unique | Select-Object -First 4)`$\r`$\n"
  FileWrite `$9 "    [RmUtil]::RmEndSession(`$`$session) | Out-Null`$\r`$\n"
  FileWrite `$9 "    if (`$`$names.Count -gt 0) { [Console]::Out.Write(('LOCKED_BY=' + (`$`$names -join ', '))); exit 12 }`$\r`$\n"
  FileWrite `$9 "  }`$\r`$\n"
  FileWrite `$9 "  [RmUtil]::RmEndSession(`$`$session) | Out-Null`$\r`$\n"
  FileWrite `$9 "}`$\r`$\n"
  FileWrite `$9 "exit 11`$\r`$\n"
  FileClose `$9

  nsExec::ExecToStack '"`$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "`$TEMP\boost_full_unlock.ps1" "`$INSTDIR"'
  Pop `$0
  Pop `$1
  Delete "`$TEMP\boost_full_unlock.ps1"

  `${If} `$0 == 12
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "安装目录文件仍被占用。请关闭相关程序后重试。`$\r`$\n`$\r`$\n阻塞进程：`$1" IDRETRY retry_close IDCANCEL abort_install
  `${ElseIf} `$0 != 0
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "Boost Browser 或内置 Chromium 仍在运行。请关闭所有浏览器窗口、扩展弹窗后重试。" IDRETRY retry_close IDCANCEL abort_install
  `${EndIf}
  Sleep 300
  Goto done

abort_install:
  Abort
done:
FunctionEnd

Section "Boost Browser" SecMain
  SectionIn RO
  Call CloseBoostProcesses
  SetOutPath "`$INSTDIR"
  !include "$NshPath"
  SetOutPath "`$INSTDIR"
  WriteUninstaller "`$INSTDIR\Uninstall.exe"

  CreateDirectory "`$SMPROGRAMS\`${PRODUCT_NAME}"
  CreateShortcut "`$SMPROGRAMS\`${PRODUCT_NAME}\`${PRODUCT_NAME}.lnk" "`$INSTDIR\`${PRODUCT_EXE}" "" "`$INSTDIR\`${PRODUCT_EXE}" 0
  CreateShortcut "`$SMPROGRAMS\`${PRODUCT_NAME}\Uninstall `${PRODUCT_NAME}.lnk" "`$INSTDIR\Uninstall.exe"
  CreateShortcut "`$DESKTOP\`${PRODUCT_NAME}.lnk" "`$INSTDIR\`${PRODUCT_EXE}" "" "`$INSTDIR\`${PRODUCT_EXE}" 0

  WriteRegStr   HKCU "`${UNINSTALL_KEY}" "DisplayName"     "`${PRODUCT_NAME}"
  WriteRegStr   HKCU "`${UNINSTALL_KEY}" "DisplayVersion"  "`${PRODUCT_VERSION}"
  WriteRegStr   HKCU "`${UNINSTALL_KEY}" "Publisher"       "Boost Browser"
  WriteRegStr   HKCU "`${UNINSTALL_KEY}" "InstallLocation" "`$INSTDIR"
  WriteRegStr   HKCU "`${UNINSTALL_KEY}" "UninstallString" "`$INSTDIR\Uninstall.exe"
  WriteRegStr   HKCU "`${UNINSTALL_KEY}" "DisplayIcon"     "`$INSTDIR\`${PRODUCT_EXE}"
  WriteRegDWORD HKCU "`${UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "`${UNINSTALL_KEY}" "NoRepair" 1
SectionEnd

Section "Uninstall"
  ExecWait '"`$SYSDIR\taskkill.exe" /F /T /IM chrome.exe'
  ExecWait '"`$SYSDIR\taskkill.exe" /F /T /IM xray.exe'
  ExecWait '"`$SYSDIR\taskkill.exe" /F /T /IM sing-box.exe'
  ExecWait '"`$SYSDIR\taskkill.exe" /F /T /IM updater.exe'
  ExecWait '"`$SYSDIR\taskkill.exe" /F /T /IM `${PRODUCT_EXE}'
  Sleep 800
  Delete "`$DESKTOP\`${PRODUCT_NAME}.lnk"
  Delete "`$SMPROGRAMS\`${PRODUCT_NAME}\`${PRODUCT_NAME}.lnk"
  Delete "`$SMPROGRAMS\`${PRODUCT_NAME}\Uninstall `${PRODUCT_NAME}.lnk"
  RMDir  "`$SMPROGRAMS\`${PRODUCT_NAME}"

  FindFirst `$0 `$1 "`$INSTDIR\*"
  uninstall_loop:
    StrCmp `$1 "" uninstall_done
    StrCmp `$1 "." uninstall_next
    StrCmp `$1 ".." uninstall_next
    StrCmp `$1 "data" uninstall_next
    StrCmp `$1 "config.yaml" uninstall_next
    StrCmp `$1 ".boost-license.json" uninstall_next
    IfFileExists "`$INSTDIR\`$1\*" 0 +3
      RMDir /r "`$INSTDIR\`$1"
      Goto uninstall_next
    Delete "`$INSTDIR\`$1"
  uninstall_next:
    FindNext `$0 `$1
    IfErrors uninstall_done
    Goto uninstall_loop
  uninstall_done:
  FindClose `$0
  RMDir "`$INSTDIR"
  DeleteRegKey HKCU "`${UNINSTALL_KEY}"
SectionEnd
"@
Set-Content -Path $NsiPath -Value $nsi -Encoding Unicode
Write-Host "    -> $NsiPath" -ForegroundColor Green

# ---------------------------------------------------------------------------
# [6/7] makensis 编译
# ---------------------------------------------------------------------------
Write-Host "`n==> [6/7] 调用 makensis 打包..." -ForegroundColor Yellow
$makensisCandidates = @(
    'C:\Program Files (x86)\NSIS\makensis.exe',
    'C:\Program Files\NSIS\makensis.exe'
)
$makensis = $makensisCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if (!$makensis) { throw 'makensis.exe 未找到，请安装 NSIS' }

Remove-Item -Force $OutExe -ErrorAction SilentlyContinue
& $makensis $NsiPath
$code = $LASTEXITCODE
if ($code -ne 0) { throw "makensis 失败: 退出码 $code" }
Unblock-File $OutExe -ErrorAction SilentlyContinue

# ---------------------------------------------------------------------------
# [7/7] 总结
# ---------------------------------------------------------------------------
Write-Host "`n================ DONE ================" -ForegroundColor Green
$hash = Get-FileHash $OutExe -Algorithm SHA256
$size = (Get-Item $OutExe).Length
Write-Host "OUT    = $OutExe"
Write-Host ("SIZE   = {0:N0} bytes ({1:N1} MB)" -f $size, ($size / 1MB))
Write-Host "SHA256 = $($hash.Hash)"
Write-Host "STAGE  = $Stage"
Write-Host ("FILES  = {0}" -f (Get-ChildItem $Stage -Recurse -File).Count)
Write-Host ""
Write-Host "对比老版安装包:" -ForegroundColor Cyan
$oldExe = $env:BOOST_PREVIOUS_INSTALLER
if (Test-Path $oldExe) {
    $oldSize = (Get-Item $oldExe).Length
    Write-Host ("  老 cloak 安装包 = {0:N1} MB" -f ($oldSize / 1MB))
    Write-Host ("  新完整安装包    = {0:N1} MB" -f ($size / 1MB))
}
