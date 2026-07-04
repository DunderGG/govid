@echo off
:: build.bat — Build script for GoVid (Windows).
::
:: Prerequisites:
::   - Go 1.21+    https://go.dev/dl/
::   - go-winres   https://github.com/tc-hib/go-winres
::   - GCC (MinGW-w64 via MSYS2)  https://www.msys2.org/
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
where gcc >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo Error: GCC is not installed or not in PATH.
    echo GoVid uses the Fyne toolkit which requires CGO and a C compiler to build.
    echo Install MSYS2 from https://www.msys2.org/, then run in the MSYS2 shell:
    echo   pacman -S mingw-w64-x86_64-gcc
    echo Finally, add C:\msys64\mingw64\bin to your system PATH and restart this terminal.
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
