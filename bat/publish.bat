@echo off
setlocal EnableExtensions

set "SCRIPT_DIR=%~dp0"
if not exist "%SCRIPT_DIR%publish.ps1" (
    echo [ERROR] Missing bat\publish.ps1
    if /I not "%NO_PAUSE%"=="1" if /I not "%CI%"=="1" pause
    endlocal & exit /b 1
)
powershell -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_DIR%publish.ps1" %*
set "EXIT_CODE=%ERRORLEVEL%"

echo.
if "%EXIT_CODE%"=="0" (
    echo Publish finished successfully.
) else (
    echo Publish failed with exit code %EXIT_CODE%.
)

if /I not "%NO_PAUSE%"=="1" if /I not "%CI%"=="1" pause

endlocal & exit /b %EXIT_CODE%
