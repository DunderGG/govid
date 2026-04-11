#!/bin/bash

# 1. Sync the root icon for the embed and winres tools
if [ -f "release/winres/icon.png" ]; then
    echo "[GoVid] Preparing icon..."
    cp -f "release/winres/icon.png" "./appicon.png"
fi

# 2. Run Go-WinRes for resources (only relevant for Windows builds)
if [[ "$OSTYPE" == "msys" || "$OSTYPE" == "win32" ]]; then
    echo "[GoVid] Generating Windows resources..."
    go-winres make --in release/winres/winres.json --out rsrc
fi

# 3. Build the application
echo "[GoVid] Compiling GoVid..."
go build -o "GoVid" .

if [ $? -eq 0 ]; then
    echo -e "\nBuild Successful! You can now run ./GoVid."
else
    echo -e "\nBuild Failed. Please check the errors above."
fi
