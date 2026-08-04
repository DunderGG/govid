# Contributing to GoVid

Thank you for contributing to GoVid. This guide covers the local development and Windows packaging setup.

## Development prerequisites

- [Go 1.26.1 or later](https://go.dev/dl/)
- A GCC C compiler, required by Fyne and CGO
- [yt-dlp](https://github.com/yt-dlp/yt-dlp/releases/latest)
- [FFmpeg](https://ffmpeg.org/download.html)
- [go-winres](https://github.com/tc-hib/go-winres) when building Windows resources

On Windows, install GCC through [MSYS2](https://www.msys2.org/):

```text
pacman -S mingw-w64-x86_64-gcc
```

Add `C:\msys64\mingw64\bin` to `PATH` after installation. On Linux, install GCC with your package manager. On macOS, run `xcode-select --install`.

## External tools

For ordinary development, make `yt-dlp` and `ffmpeg` available on `PATH`, or place them in a `bin/` directory beside the built GoVid executable. GoVid checks the local `bin/` directory first and then falls back to `PATH`.

Use these upstream sources:

- **yt-dlp:** Download `yt-dlp.exe` from the [latest yt-dlp release](https://github.com/yt-dlp/yt-dlp/releases/latest).
- **FFmpeg and FFprobe for Windows:** Download the release essentials archive from [gyan.dev](https://www.gyan.dev/ffmpeg/builds/), then extract `ffmpeg.exe` and, optionally, `ffprobe.exe` from its `bin/` directory.
- **FFmpeg for Linux and macOS:** Follow the platform links on the [official FFmpeg download page](https://ffmpeg.org/download.html), or use your system package manager.

`ffprobe` is optional. When present beside `ffmpeg`, it improves duration and frame-count metadata used for post-processing progress estimates.

## Build and test

Clone the repository, then download the Go modules:

```text
go mod download
```

Build on Windows:

```text
.\build.bat
```

Build on Linux or macOS:

```text
chmod +x build.sh
./build.sh
```

Run the test suite before submitting a change:

```text
go test ./...
```

Format changed Go files with `gofmt`.

## Windows release inputs

The maintainer's private Windows packaging tooling copies bundled executables from `external/`. Contributors working on packaging changes should prepare this layout in the repository root:

```text
external/
├── ffmpeg.exe
└── yt-dlp.exe
```

The packaging tooling builds GoVid, places these dependencies under `bin/` in the package, and creates a versioned release archive. The private packaging script is not part of the repository; use the normal build commands above when validating a contribution.

The `external/` executables are local packaging inputs. They are ignored by the repository's `*.exe` rule and should not be force-added to Git.

## Redistribution and repository hygiene

Before publishing a package containing third-party binaries:

- Record the downloaded versions and upstream sources.
- Preserve the license and notice files supplied with each binary distribution.
- Check the FFmpeg build configuration and comply with its LGPL or GPL terms and any enabled component licenses.
- Prefer publishing generated packages through GitHub Releases rather than committing binaries to Git history.

Never commit cookies, account sessions, API keys, download history, logs, generated executables, or release archives. In particular, browser-exported cookie files can grant access to accounts even when they look like ordinary text files.

## Pull requests

Keep changes focused, follow the existing Go style, and update documentation when behavior or setup changes. Include a short description of the change and the validation you performed.
