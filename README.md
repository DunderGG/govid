# <img src="https://github.com/user-attachments/assets/d81ed71e-cc17-4944-aafc-d94f7af758b4" alt="GoVid icon" width="64" height="64" /> **GoVid**

Fast, cross-platform desktop video downloader for `yt-dlp` with optional FFmpeg post-processing.

[![Latest Release](https://img.shields.io/github/v/release/DunderGG/govid?label=release)](https://github.com/DunderGG/govid/releases/latest)
[![License](https://img.shields.io/github/license/DunderGG/govid)](LICENSE)
![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8)
![Platforms](https://img.shields.io/badge/platform-Windows%20%7C%20Linux%20%7C%20macOS-2ea44f)

Download: [Latest Release](https://github.com/DunderGG/govid/releases/latest)
Quick links: [Features](#-features) · [Getting Started](#-getting-started) · [Usage](#-usage)

Why GoVid: a native GUI focused on speed, batch workflows, and quality controls without command-line setup.

## ✨ Features

- **Cross-Platform GUI**: Modern interface built with the [Fyne toolkit](https://fyne.io/).
- **Multiple Formats**: Support for MP4, MKV, WebM, MP3, and M4A.
- **Quality Control**: Select your preferred maximum resolution for downloads.
- **Video Trimming**: Download only a specific segment — specify a start time, an end time, or both (`HH:MM:SS` / `MM:SS` / seconds).
- **Batch Processing**: Download multiple URLs at once by switching to Batch Mode (one URL per line).
- **Real-time Progress**: Live progress tracking with per-download progress bars and a scrollable activity log.
- **Optional Post-Processing**: Seamless integration with FFmpeg for frame interpolation (60FPS), sharpening, and audio normalization.
- **Motion Smoothing**: Three interpolation modes (Precise, Balanced, Fast) for smoother motion at higher frame rates.
- **Download Management**: Start, monitor, and cancel active downloads from a single queue view.
- **Speed Limiting**: Cap download bandwidth to avoid saturating your network.
- **Config Support**: Configuration file support via `govid.json` for startup defaults and repeatable workflows.
- **Log Export**: Option to save download logs to `.txt` files for troubleshooting.
- **Completion Notifications**: Optional desktop notifications when downloads complete.
- **Dark / Light Theme**: Built-in light and dark themes configurable in Preferences.

## 📥 Download

You can download the latest pre-compiled executables from the **[Releases Page](https://github.com/DunderGG/govid/releases/latest)**.

1. Download the bundled `.zip` for your operating system.
2. Extract the zip — `yt-dlp` and `ffmpeg` are included in the `bin/` folder.
3. Run `GoVid.exe` (Windows) or `GoVid` (Linux/macOS) and start downloading!

## 🚀 Getting Started

### Prerequisites

> **Using a release build?** The bundled `.zip` from the [Releases Page](https://github.com/DunderGG/govid/releases/latest) already includes `yt-dlp` and `ffmpeg` in a `bin/` folder — no manual installation needed.
>
> **Optional:** `ffprobe.exe` is not bundled due to its size (~98MB) but can be placed in the `bin/` folder alongside `ffmpeg.exe` for enhanced metadata support. Download it from [gyan.dev/ffmpeg/builds](https://www.gyan.dev/ffmpeg/builds/) (included in the `ffmpeg-release-essentials.zip`).

If you are building from source, you must have the following tools installed and available in your system's `PATH`:

1.  **[yt-dlp](https://github.com/yt-dlp/yt-dlp)**: The core engine for video downloading.
2.  **[FFmpeg](https://ffmpeg.org/)**: Required for high-quality video/audio post-processing and conversion.
3.  **A GCC C compiler**: GoVid uses [Fyne](https://fyne.io/), which requires CGO and a C compiler to build.
    - **Windows**: Install [MSYS2](https://www.msys2.org/), then run `pacman -S mingw-w64-x86_64-gcc` in the MSYS2 shell and add `C:\msys64\mingw64\bin` to your system `PATH`.
    - **Linux**: Install GCC via your package manager, e.g. `sudo apt install gcc` (Debian/Ubuntu) or `sudo dnf install gcc` (Fedora).
    - **macOS**: Install the Xcode Command Line Tools via `xcode-select --install`.

### Installation

Ensure you have [Go 1.26+](https://go.dev/dl/) installed.

1.  **Clone the Repository**:
    ```bash
    git clone https://github.com/DunderGG/govid.git
    cd govid
    ```

2.  **Build the application**:
    Run the build script for your platform:

    **On Windows**:
    ```cmd
    .\build.bat
    ```

    **On Linux / macOS**:
    ```bash
    chmod +x build.sh
    ./build.sh
    ```

3.  **Run the application**:
    ```bash
    ./GoVid.exe  # Windows
    ./GoVid      # Linux/macOS
    ```

## 📖 Usage

1. **Launch**: Open GoVid.
2. **URL or Batch Mode**: Paste a video URL. Enable **Batch Mode** to paste multiple URLs (one per line).
3. **Format and Quality**: Choose the output format (MP4, MKV, WebM, MP3, or M4A) and maximum resolution.
4. **Trim (Optional)**: Enter a start time, end time, or both (for example `00:01:30` and `00:05:00`) to download only part of the video.
5. **Post-Processing (Advanced)**: Open **Tools → Preferences** to enable:
    - **Smooth Motion**: Interpolates video to 60 fps.
    - **Sharpen Video**: Restores edge detail.
    - **Normalize Audio**: Balances volume levels.
6. **JSON Config (Optional)**: Place a `govid.json` file in the app folder for startup defaults and repeatable workflows, then click **Load from Config** in Preferences.
7. **Save Location**: Choose where the output file should be saved.
8. **Download**: Click **Download Now** to start.

> **Note:** If a download fails, run `--update` to refresh `yt-dlp` and try again.

### Command Line Options

- `--update`: Updates the underlying `yt-dlp` tool to the latest version.

## ⚙️ Advanced Configuration (govid.json)

For power users, GoVid supports a `govid.json` file in the application directory. This allows you to define startup defaults or automated paths.

Example `govid.json`:
```json
{
  "path": "C:\\Downloads\\YouTube",
  "format": "MP4",
  "quality": "1080p",
  "maxSpeed": "5M"
}
```

| Field | Supported Values |
| :--- | :--- |
| `format` | `MP4`, `MKV`, `WebM`, `MP3 (Audio Only)`, `M4A (Apple Audio)` |
| `quality` | `Best Quality`, `1080p`, `720p`, `480p`, `360p` |
| `path` | Any valid absolute folder path |
| `maxSpeed` | Numeric value with unit (e.g., `50K`, `5M`, `1G`) |

> **Note:** Standard JSON does not support comments. Adding them will cause a loading error. Use the **Load from Config** button in the Preferences window to apply changes.

## 🛠️ Built With

- [Fyne](https://fyne.io/) - An easy-to-use UI toolkit and app API written in Go
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) - YouTube-dl fork with additional features
- [FFmpeg](https://ffmpeg.org/) - A collection of libraries and tools to process multimedia content

## 👤 Author

**David Bennehag** - [@DunderGG](https://github.com/DunderGG) - [dunder.gg](https://dunder.gg)

## 📄 License

This project is licensed under the GPL-3.0 - see the [LICENSE](LICENSE) file for details.

