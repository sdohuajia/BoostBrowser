@echo off
chcp 65001 >nul
setlocal enabledelayedexpansion

REM Change to repository root (parent directory of this script).
cd /d "%~dp0.."

set "NO_PAUSE=0"
if /I "%~1"=="--no-pause" set "NO_PAUSE=1"

set "TEMP_DIST_CREATED=0"
set "TEMP_PLACEHOLDER_CREATED=0"

echo ========================================
echo   Generate Wails Bindings
echo ========================================
echo.
echo Working directory: %CD%
echo.

if not exist "wails.json" (
    echo [ERROR] wails.json not found in repository root.
    echo         This development branch must keep a complete Wails source tree.
    if not "%NO_PAUSE%"=="1" pause
    exit /b 1
)

echo [1/3] Ensure frontend\dist exists...
if not exist "frontend\dist" (
    mkdir "frontend\dist"
    set "TEMP_DIST_CREATED=1"
    echo Created temporary dist directory.
) else (
    echo Dist directory already exists.
)

if not exist "frontend\dist\__wails_placeholder__.txt" (
    echo placeholder> "frontend\dist\__wails_placeholder__.txt"
    set "TEMP_PLACEHOLDER_CREATED=1"
    echo Created temporary placeholder file.
)

echo.
echo [2/3] Regenerating Wails bindings...
wails generate module
if errorlevel 1 (
    echo Failed to regenerate Wails bindings.
    goto :cleanup_fail
)

echo.
echo [3/3] Verify bindings output...
if exist "frontend\wailsjs" (
    xcopy /E /I /Y "frontend\wailsjs" "frontend\src\wailsjs" >nul
    echo Bindings copied from frontend\wailsjs to frontend\src\wailsjs.
) else if exist "frontend\src\wailsjs" (
    echo Bindings already generated in frontend\src\wailsjs.
) else (
    echo Cannot find generated bindings in frontend\wailsjs or frontend\src\wailsjs.
    goto :cleanup_fail
)

call :cleanup

echo.
echo ========================================
echo   Done
echo ========================================
echo.

if not "%NO_PAUSE%"=="1" pause
exit /b 0

:cleanup
if "!TEMP_PLACEHOLDER_CREATED!"=="1" (
    del /Q "frontend\dist\__wails_placeholder__.txt" >nul 2>&1
)
if "!TEMP_DIST_CREATED!"=="1" (
    rmdir /S /Q "frontend\dist" >nul 2>&1
)
exit /b 0

:cleanup_fail
call :cleanup
if not "%NO_PAUSE%"=="1" pause
exit /b 1

