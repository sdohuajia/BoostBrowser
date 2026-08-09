@echo off
setlocal EnableExtensions

set "SCRIPT_DIR=%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_DIR%publish-public.ps1" -AllowDirtyWorkingTree %*
set "EXIT_CODE=%ERRORLEVEL%"

echo.
if "%EXIT_CODE%"=="0" (
    echo Publish finished successfully.
) else (
    echo Publish failed with exit code %EXIT_CODE%.
)
pause

endlocal & exit /b %EXIT_CODE%
