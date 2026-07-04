#!/bin/bash
# build.sh — Build script for GoVid (Linux / macOS / Git Bash on Windows).
#
# Prerequisites:
#   - Go 1.21+  https://go.dev/dl/
#   - GCC: required by Fyne (CGO). Install via your system package manager.
#       Linux:  sudo apt install gcc  (Debian/Ubuntu)  or  sudo dnf install gcc  (Fedora)
#       macOS:  xcode-select --install
#       Windows (MSYS2): pacman -S mingw-w64-x86_64-gcc
#   - go-winres  https://github.com/tc-hib/go-winres  (Windows only)
#
# Usage:
#   ./build.sh
#
# The output binary (GoVid or GoVid.exe) will be placed in the project root.
# GoVid requires yt-dlp and ffmpeg to be available on your PATH at runtime.
set -e

# Check required tools are installed
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed or not in PATH. https://go.dev/dl/"
    exit 1
fi
if ! command -v gcc &> /dev/null; then
    echo "Error: GCC is not installed or not in PATH."
    echo "GoVid uses the Fyne toolkit which requires CGO and a C compiler to build."
    if [[ "$OSTYPE" == "darwin"* ]]; then
        echo "Run: xcode-select --install"
    elif [[ "$OSTYPE" == "msys" || "$OSTYPE" == "win32" ]]; then
        echo "Install MSYS2 from https://www.msys2.org/, then run in the MSYS2 shell:"
        echo "  pacman -S mingw-w64-x86_64-gcc"
        echo "Then add C:\\msys64\\mingw64\\bin to your system PATH and restart this terminal."
    else
        echo "Debian/Ubuntu: sudo apt install gcc"
        echo "Fedora/RHEL:   sudo dnf install gcc"
    fi
    exit 1
fi
if [[ "$OSTYPE" == "msys" || "$OSTYPE" == "win32" ]] && ! command -v go-winres &> /dev/null; then
    echo "Error: go-winres is not installed or not in PATH. https://github.com/tc-hib/go-winres"
    exit 1
fi

# 1. Generate Windows resources
if [[ "$OSTYPE" == "msys" || "$OSTYPE" == "win32" ]]; then
    echo "[GoVid] Generating Windows resources..."
    go-winres make --in release/winres/winres.json --out rsrc
    if [ $? -ne 0 ]; then
        echo -e "\nError: go-winres failed. Check that winres.json and appicon.png are present in release/winres/."
        exit 1
    fi
fi

# 2. Build the application
echo "[GoVid] Compiling GoVid..."
if [[ "$OSTYPE" == "msys" || "$OSTYPE" == "win32" ]]; then
    go build -ldflags="-H windowsgui" -o "GoVid.exe" .
    echo -e "\nBuild Successful! You can now run .\\GoVid.exe."
else
    go build -o "GoVid" .
    echo -e "\nBuild Successful! You can now run ./GoVid."
fi
