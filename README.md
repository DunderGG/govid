# <img width="64" height="64" alt="appicon" src="https://github.com/user-attachments/assets/d81ed71e-cc17-4944-aafc-d94f7af758b4" />  **GoVid**

GoVid is a high-performance, cross-platform video downloader built with Go. It provides a clean graphical interface for the powerful `yt-dlp` tool, allowing you to easily download and convert videos from various online platforms.

## ✨ Features

- **Cross-Platform GUI**: Modern interface built with the [Fyne toolkit](https://fyne.io/).
- **Multiple Formats**: Support for MP4, MKV, WebM, MP3, and M4A (audio only).
- **Quality Control**: Select your preferred maximum resolution for downloads.
- **Video Trimming**: Download only a specific segment — specify a start time, an end time, or both (`HH:MM:SS` / `MM:SS` / seconds).
- **Real-time Progress**: Visual progress bars and a live scrollable log view.
- **Professional Post-Processing**: Seamless integration with FFMPEG for high-quality encoding.
- **Download Management**: Easily start, monitor, and cancel active downloads.
- **Speed Limiting**: Cap download bandwidth to avoid saturating your connection.
- **Log Export**: Option to save download logs to `.txt` files for troubleshooting.
- **Completion Notifications**: Optional system notifications when a download finishes.
- **Dark / Light Theme**: Switch between themes via Tools → Preferences.

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

1. **Launch**: Open the GoVid application.
2. **URL**: Paste the video URL into the input field.
3. **Configuration**: Select your desired output format (MP4, MKV, WebM, MP3, or M4A) and maximum resolution.
4. **Trim (optional)**: Enter a start time, end time, or both (e.g. `00:01:30` / `00:05:00`) to download only a portion of the video. Either field can be used alone — leave both blank to download the full video.
5. **Save Location**: Choose the directory where you want to save the file.
6. **Download**: Click the **Download** button to start. Monitor progress in real-time or cancel at any time.
7. **Options**: Check **Allow Duplicate Downloads** to prevent overwrites, **Save output to log file** to persist the session log, or **Notify on Completion** to receive a system notification when the download finishes.

### Command Line Options

- `--update`: Updates the underlying `yt-dlp` tool to the latest version.

## 🛠️ Built With

- [Fyne](https://fyne.io/) - An easy-to-use UI toolkit and app API written in Go
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) - YouTube-dl fork with additional features
- [FFmpeg](https://ffmpeg.org/) - A collection of libraries and tools to process multimedia content

## 👤 Author

**David Bennehag** - [@DunderGG](https://github.com/DunderGG) - [dunder.gg](https://dunder.gg)

## 📄 License

This project is licensed under the GPL-3.0 - see the [LICENSE](LICENSE) file for details.

