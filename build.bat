@echo off
:: build.bat — Build script for GoVid (Windows).
::
:: Prerequisites:
::   - Go 1.21+    https://go.dev/dl/
::   - go-winres   https://github.com/tc-hib/go-winres
::
:: Usage:
::   .\build.bat
::
:: The output binary (GoVid.exe) will be placed in the project root.
:: GoVid requires yt-dlp and ffmpeg to be available on your PATH at runtime.
setlocal

:: Check required tools are installed
where go >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo Error: Go is not installed or not in PATH. https://go.dev/dl/
    exit /b 1
)
where go-winres >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo Error: go-winres is not installed or not in PATH. https://github.com/tc-hib/go-winres
    exit /b 1
)

:: Run Go-WinRes for Windows resources
echo [GoVid] Generating Windows resources...
go-winres make --in release\winres\winres.json --out rsrc
if %ERRORLEVEL% neq 0 (
    echo.
    echo Error: go-winres failed. Check that winres.json and appicon.png are present in release\winres\.
    exit /b 1
)

:: Build the application
:: -H windowsgui: Hide the terminal window
echo [GoVid] Compiling GoVid.exe...
go build -ldflags="-H windowsgui" -o "GoVid.exe" .

if %ERRORLEVEL% equ 0 (
    echo.
    echo Build Successful! You can now run .\GoVid.exe.
) else (
    echo.
    echo Build Failed. Please check the errors above.
    exit /b 1
)
