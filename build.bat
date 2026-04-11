@echo off
setlocal

:: Sync the root icon for the embed and winres tools
if exist "release\winres\icon.png" (
    echo [GoVid] Preparing icon...
    copy /Y "release\winres\icon.png" "appicon.png" > nul
)

:: Run Go-WinRes for Windows resources
echo [GoVid] Generating Windows resources...
go-winres make --in release\winres\winres.json --out rsrc

:: Build the application (Optimized for size)
:: -s: Omit the symbol table and debug information
:: -w: Omit DWARF symbol table
:: -H windowsgui: Hide the terminal window
echo [GoVid] Compiling GoVid.exe...
go build -ldflags="-s -w -H windowsgui" -o "GoVid.exe" .

if %ERRORLEVEL% equ 0 (
    echo.
    echo Build Successful! You can now run .\GoVid.exe.
) else (
    echo.
    echo Build Failed. Please check the errors above.
)

pause
