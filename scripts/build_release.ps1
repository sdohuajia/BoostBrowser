# build_release.ps1
# 一键打包发版脚本（直传 GitHub Release 资产）
#
# 用法：
#   1. 改 wails.json 里 productVersion，比如 1.1.0 → 1.1.1
#   2. 在仓库根目录执行：powershell -ExecutionPolicy Bypass -File scripts\\build_release.ps1
#   3. 脚本完成后，build\\release\\ 下会有 3 个文件：boost-browser.exe + boost-browser.exe.sha256 + updater.exe

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $RepoRoot

Write-Host "==> 工作目录: $RepoRoot" -ForegroundColor Cyan

# 1. 读取版本号
$wailsJson = Get-Content "$RepoRoot\wails.json" -Raw | ConvertFrom-Json
$Version = $wailsJson.info.productVersion
if (-not $Version) { throw "wails.json 中未找到 info.productVersion" }
Write-Host "==> 当前版本: v$Version" -ForegroundColor Cyan

# 2. 输出目录
$ReleaseDir = "$RepoRoot\build\release"
New-Item -ItemType Directory -Force -Path $ReleaseDir | Out-Null
Get-ChildItem $ReleaseDir | Remove-Item -Force -Recurse -ErrorAction SilentlyContinue

# 3. Stage chromium 内核 + 代理二进制 + 资源文件，并生成 bb_files.nsh
Write-Host "==> Stage assets (scripts\stage_assets.ps1) ..." -ForegroundColor Yellow
& powershell -ExecutionPolicy Bypass -File "$RepoRoot\scripts\stage_assets.ps1"
if ($LASTEXITCODE -ne 0) { throw "stage_assets.ps1 failed" }

# 4. 编译主程序（仅产出 boost-browser.exe，不打安装包）
Write-Host "==> 编译 boost-browser.exe (wails build) ..." -ForegroundColor Yellow
$env:GOOS = "windows"
$env:GOARCH = "amd64"
& wails build -clean -platform windows/amd64 -tags native_webview2loader
if ($LASTEXITCODE -ne 0) { throw "wails build 失败" }
$BuiltExe = "$RepoRoot\build\bin\boost-browser.exe"
if (-not (Test-Path $BuiltExe)) { throw "未找到 $BuiltExe" }
Copy-Item $BuiltExe "$ReleaseDir\boost-browser.exe" -Force
Write-Host "    -> $ReleaseDir\boost-browser.exe ($([math]::Round((Get-Item "$ReleaseDir\boost-browser.exe").Length/1MB, 2)) MB)" -ForegroundColor Green

# 5. 编译 updater.exe
Write-Host "==> 编译 updater.exe (go build) ..." -ForegroundColor Yellow
& go build -o "$ReleaseDir\updater.exe" ".\backend\cmd\updater"
if ($LASTEXITCODE -ne 0) { throw "updater.exe 构建失败" }
if (-not (Test-Path "$ReleaseDir\updater.exe")) { throw "未找到 $ReleaseDir\updater.exe" }
Write-Host "    -> $ReleaseDir\updater.exe ($([math]::Round((Get-Item "$ReleaseDir\updater.exe").Length/1MB, 2)) MB)" -ForegroundColor Green

# 6. 计算 SHA256
Write-Host "==> 计算 SHA256 ..." -ForegroundColor Yellow
$hash = (Get-FileHash "$ReleaseDir\boost-browser.exe" -Algorithm SHA256).Hash.ToLower()
$hash | Out-File "$ReleaseDir\boost-browser.exe.sha256" -Encoding ascii -NoNewline
Write-Host "    -> boost-browser.exe SHA256: $hash" -ForegroundColor Green

# 7. 总结
Write-Host ""
Write-Host "================================================================" -ForegroundColor Cyan
Write-Host "  打包完成！请上传以下文件到 GitHub Release tag = v$Version" -ForegroundColor Cyan
Write-Host "================================================================" -ForegroundColor Cyan
Get-ChildItem $ReleaseDir | ForEach-Object {
    Write-Host ("  - {0} ({1:N0} bytes)" -f $_.Name, $_.Length) -ForegroundColor White
}
Write-Host ""
Write-Host "GitHub 操作步骤：" -ForegroundColor Yellow
Write-Host "  1. 访问 https://github.com/<你的用户>/<仓库>/releases/new"
Write-Host "  2. Choose a tag → 输入 v$Version → Create new tag"
Write-Host "  3. Release title 填: v$Version"
Write-Host "  4. 在 Description 写本次更新内容（中文也行，会显示在用户的更新弹窗里）"
Write-Host "  5. 上传 boost-browser.exe、boost-browser.exe.sha256 和 updater.exe"
Write-Host "  6. 点 Publish release"
Write-Host ""
Write-Host "  提示：在 Description 里加 [force] 字符串可强制所有用户必须升级"
