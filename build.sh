#!/bin/bash
# build.sh — Build script for GoVid (Linux / macOS / Git Bash on Windows).
#
# Prerequisites:
#   - Go 1.21+  https://go.dev/dl/
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
