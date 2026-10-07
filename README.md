# <img src="https://github.com/user-attachments/assets/d81ed71e-cc17-4944-aafc-d94f7af758b4" alt="GoVid icon" width="64" height="64" /> **GoVid**

Fast, cross-platform desktop video downloader, powered by `yt-dlp` with optional FFmpeg post-processing.

[![Latest Release](https://img.shields.io/github/v/release/DunderGG/govid?label=release)](https://github.com/DunderGG/govid/releases/latest)
[![License](https://img.shields.io/github/license/DunderGG/govid)](LICENSE)
![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8)
![Platforms](https://img.shields.io/badge/platform-Windows%20%7C%20Linux-2ea44f)
[![CI](https://github.com/DunderGG/govid/actions/workflows/ci.yml/badge.svg)](https://github.com/DunderGG/govid/actions/workflows/ci.yml)

Download: [Latest Release](https://github.com/DunderGG/govid/releases/latest)
Quick links: [Features](#-features) · [Getting Started](#-getting-started) · [Usage](#-usage)

Why GoVid: a native GUI focused on speed, batch workflows, and quality controls without command-line setup.

## ✨ Features

- **Cross-Platform GUI**: Modern interface built with the [Fyne toolkit](https://fyne.io/).
- **Multiple Formats**: Support for MP4, MKV, WebM, MP3, and M4A.
- **Metadata & Cover Art**: Writes title, artist, and date tags, the thumbnail as cover art, and (optionally) chapters into downloaded files, and keeps them through post-processing.
- **Download History**: **File → History** lists past downloads with their real titles; search it, re-add a URL, show the file in its folder, or copy the URL. GoVid warns before downloading the same video again, even from a different link to it. History can be turned off in Preferences.
- **Subtitles**: Embed subtitles in the video, save them as `.srt` files beside it, or both, in the languages you choose (optionally including auto-generated captions).
- **Quality Control**: Select your preferred maximum resolution for downloads. GoVid tells you when a video is not available at that resolution, and names the file after the resolution it actually downloaded.
- **Format Browser**: **Formats…** shows every format a video offers (resolution, frame rate, HDR, codecs, bitrate, container, size), marks what your settings would download, and lets you pick a video and an audio stream yourself. The log says what each download will fetch, and a **Preferred Video Codec** setting (H.264, VP9, or AV1) works for every video.
- **Video Trimming**: Download only a specific segment — specify a start time, an end time, or both (`HH:MM:SS` / `MM:SS` / seconds).
- **Batch Processing**: Download multiple URLs at once by switching to Batch Mode (one URL per line). Load a `.txt` list of URLs, paste several links at once, or drop a list or an internet shortcut (`.url`) onto the window.
- **Presets**: Switch between setups (e.g. audio-only, 1080p MP4, archive) with the Preset dropdown. Save the current settings as a preset, choosing which groups of settings it holds, and import or export presets to move them between machines.
- **Queue Panel**: With more than one video queued, a Queue panel above the log shows each video's status and progress. Remove or reorder waiting videos, pause or skip the one downloading, resume a paused one, or retry one that failed, while the queue runs.
- **Simultaneous Downloads**: Download up to three videos of a batch or playlist at once (Preferences → Simultaneous Downloads). The progress bar shows the whole queue, and GoVid drops back to one at a time if a site rate-limits or asks for a bot check.
- **Pause, Resume, and Restart-Proof Downloads**: Pause a download and resume it later from where it stopped. Interrupted downloads (a network error, a retry) continue rather than start over, and when you close GoVid with downloads queued or paused, it offers to resume them at the next start.
- **Live Streams**: Record a live stream from now or (YouTube, Twitch) from its start, or wait for a scheduled stream or premiere and record it when it begins. **Stop recording** keeps what was recorded, saved in your chosen format and added to the history; free space is watched while recording.
- **Playlists**: Paste a playlist URL to pick all of it, a range (e.g. `5-8`), or just the linked video; each video is queued separately.
- **Real-time Progress**: Live progress tracking with per-download progress bars and a scrollable activity log.
- **Optional Post-Processing**: Seamless integration with FFmpeg for frame interpolation (60FPS), sharpening, and audio normalization.
- **GPU-Accelerated Encoding**: Optional hardware-accelerated final encode (NVIDIA NVENC, Intel QSV, AMD AMF, or VAAPI) with automatic CPU fallback if the selected backend is unavailable.
- **Motion Smoothing**: Three interpolation modes (Precise, Balanced, Fast) for smoother motion at higher frame rates.
- **Download Management**: Start, monitor, and cancel active downloads from a single queue view.
- **Sign-in Cookies**: Pass your login to yt-dlp from a browser (Firefox works best on Windows) or a `cookies.txt` file, for age-restricted, members-only, and private videos and YouTube's bot check. When cookies cannot be read, or a site asks you to sign in, the log says what to do.
- **Speed Limiting**: Cap download bandwidth to avoid saturating your network.
- **Disk Space Check**: Warns before a download that will not fit in the save folder.
- **Portable Mode**: Keep settings and presets in `settings.json` beside `GoVid.exe` (Preferences → Portable Mode), so a GoVid folder on a USB stick carries them along with its history and tools.
- **Config Support**: Configuration file support via `govid.json` for startup defaults and repeatable workflows.
- **Bug Reports**: **Help → Copy diagnostics** puts versions, settings, the queue, and recent log lines on the clipboard, with your user name, user folder, and cookies left out. With Debug Output on, a heartbeat in the log shows when the window stops responding.
- **Log Export**: Option to save download logs to `.txt` files for troubleshooting.
- **Completion Notifications**: Optional desktop notifications when downloads complete.
- **Tools and Components**: **Tools → Components** installs, updates, or repairs yt-dlp, FFmpeg (with ffprobe), and Deno in the `bin/` folder, each download checked against its published SHA-256. GoVid passes yt-dlp a JavaScript runtime (Deno, or Node or Bun on `PATH`), which YouTube now needs, and offers to install Deno or any missing tool at startup.
- **Update Checks**: Warns when the installed `yt-dlp` is out of date and updates it with one click, and tells you when a newer GoVid release is available (also under **Tools → Check for GoVid updates**). On Windows, **Update now** downloads the new release, checks it against its published SHA-256, replaces GoVid, and restarts it.
- **Themes and Shortcuts**: A System theme (the default) follows Windows' light or dark setting, or choose Dark or Light. Keyboard shortcuts: Ctrl+Enter download, Ctrl+O open the save folder, Ctrl+L load URLs from a file, Ctrl+Shift+V paste URLs, Ctrl+H History, Ctrl+, Preferences, F1 the guide, Esc to close a window.

## 📥 Download

You can download the latest pre-compiled executables from the **[Releases Page](https://github.com/DunderGG/govid/releases/latest)**.

1. Download the bundled `.zip` for your operating system.
2. Extract the zip — `yt-dlp` and `ffmpeg` are included in the `bin/` folder. GoVid offers to install Deno, the JavaScript runtime YouTube downloads need, on first start (or use **Tools → Components**).
3. Run `GoVid.exe` (Windows) or `GoVid` (Linux) and start downloading!

## 🚀 Getting Started

### Prerequisites

> **Using a release build?** The bundled `.zip` from the [Releases Page](https://github.com/DunderGG/govid/releases/latest) already includes `yt-dlp` and `ffmpeg` in a `bin/` folder — no manual installation needed.
>
> **Optional:** `ffprobe.exe` is not bundled due to its size (~100 MB). **Tools → Components** → FFmpeg → **Install** or **Update** downloads gyan.dev's essentials build with both `ffmpeg.exe` and `ffprobe.exe`; with ffprobe, post-processing shows a percentage. Deno (the JavaScript runtime YouTube needs) is installed the same way.

If you are building from source, you must have the following tools installed and available in your system's `PATH`:

1.  **[yt-dlp](https://github.com/yt-dlp/yt-dlp)**: The core engine for video downloading.
2.  **[FFmpeg](https://ffmpeg.org/)**: Required for high-quality video/audio post-processing and conversion.
    - **A JavaScript runtime** for YouTube: [Deno](https://deno.com/) 2.3 or newer (recommended), or Node.js 22 or newer. GoVid finds it on `PATH`, or installs Deno into `bin/` from **Tools → Components** on Windows.
3.  **A GCC C compiler**: GoVid uses [Fyne](https://fyne.io/), which requires CGO and a C compiler to build.
    - **Windows**: Install [MSYS2](https://www.msys2.org/), then run `pacman -S mingw-w64-x86_64-gcc` in the MSYS2 shell and add `C:\msys64\mingw64\bin` to your system `PATH`.
    - **Linux**: Install GCC via your package manager, e.g. `sudo apt install gcc` (Debian/Ubuntu) or `sudo dnf install gcc` (Fedora).

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

    **On Linux**:
    ```bash
    chmod +x build.sh
    ./build.sh
    ```

3.  **Run the application**:
    ```bash
    ./GoVid.exe  # Windows
    ./GoVid      # Linux
    ```

## 📖 Usage

1. **Launch**: Open GoVid.
2. **URL or Batch Mode**: Paste a video or playlist URL. Enable **Batch Mode** to paste multiple URLs (one per line). For a playlist, GoVid asks which videos to download. **Load from file…** reads a `.txt` list (blank lines and lines starting with `#` are skipped), the paste button next to the field adds the links on the clipboard, and you can drop a `.txt` list or `.url` shortcut onto the window. To drop a link from a browser, drag it to the desktop first and drop the shortcut it makes.
3. **Format and Quality**: Choose the output format (MP4, MKV, WebM, MP3, or M4A) and maximum resolution.
4. **Trim (Optional)**: Enter a start time, end time, or both (for example `00:01:30` and `00:05:00`) to download only part of the video.
5. **Post-Processing (Advanced)**: Open **Tools → Post-Processing** to enable:
    - **Smooth Motion**: Interpolates video to 60 fps.
    - **Sharpen Video**: Restores edge detail.
    - **Normalize Audio**: Balances volume levels.
    - **Encoder Backend**: Pick `Auto`, a specific GPU backend, or `Off` to control whether the final encode uses hardware acceleration.
6. **JSON Config (Optional)**: Place a `govid.json` file in the app folder for startup defaults and repeatable workflows, then click **Load from Config** in Preferences.
7. **Save Location**: Choose where the output file should be saved.
8. **Download**: Click **Download Now** to start.

> **Note:** GoVid checks once a day whether a newer `yt-dlp` is available and offers to update it. If a download fails, use **Tools → Update yt-dlp** (or run `GoVid --update`) and try again. An outdated `yt-dlp` is the most common reason downloads stop working.

### Command Line Options

- `--update`: Updates the underlying `yt-dlp` tool to the latest version.

## ⚙️ Advanced Configuration (govid.json)

Every setting can be set from a JSON file. **Tools → Export settings…** writes all of your current settings to a file, and **Tools → Import settings…** applies one, for example on another machine. A file named `govid.json` in the application directory is applied by **Load from Config** in the Preferences window.

A key left out of the file leaves that setting unchanged, so a file can hold just the settings you want to change:
```json
{
  "path": "C:\Downloads\YouTube",
  "format": "MP4",
  "quality": "1080p",
  "maxSpeed": "5M"
}
```

| Key | Supported Values (default) |
| :--- | :--- |
| `path` | An existing folder (the application folder) |
| `format` | `MP4`, `MKV`, `WebM`, `MP3`, `M4A` (`MP4` on Windows and macOS, `MKV` elsewhere) |
| `quality` | `Best Quality`, `1080p`, `720p`, `480p`, `360p` (`Best Quality`) |
| `maxSpeed` | A rate with unit, e.g. `50K`, `5M`, `1G`; `""` for unlimited (`""`) |
| `cookiesPath` | An existing cookies.txt file, or `""` for none (`""`) |
| `cookieSource` | `None`, `From file` (use `cookiesPath`), `From browser` (use `cookieBrowser`) (`None`; settings saved before this choice existed keep using their cookies file) |
| `cookieBrowser` | `Firefox`, `Chrome`, `Edge`, `Brave`, `Chromium`, `Opera`, `Vivaldi`, `Whale`, `Safari` (`Firefox`) |
| `cookieProfile` | The browser profile to read cookies from; `""` for the default profile (`""`) |
| `simultaneousDownloads` | `1`, `2`, `3`: videos downloaded at the same time (`1`) |
| `preferredCodec` | `Any`, `H.264`, `VP9`, `AV1`: the video codec downloads prefer, even over a higher resolution in another codec (`Any`) |
| `themeMode` | `System`, `Dark`, `Light` (`System` for new installs) |
| `logLimit` | `100`, `200`, `500`, `1000`, `5000`, `Unlimited` (`200`) |
| `savePrefs` | `true` or `false`: remember settings between sessions (`true`) |
| `showDebug`, `checkUpdates` | `true` or `false`: show yt-dlp's debug lines (`false`); check for updates at startup (`true`) |
| `embedMetadata`, `embedThumbnail`, `embedChapters` | `true` or `false`: write tags, cover art (skipped for WebM), chapters into downloads (`true`, `true`, `false`) |
| `subtitles` | `Off`, `Embed`, `Save as .srt`, `Both` (`Off`) |
| `subtitleLangs` | Subtitle languages in yt-dlp's `--sub-langs` syntax, e.g. `en.*,de` (`en.*`) |
| `autoSubtitles` | `true` or `false`: also take auto-generated captions (`false`) |
| `keepHistory` | `true` or `false`: record downloads and warn before repeating one (`true`) |
| `batchMode`, `saveLog`, `notify`, `autoRetry` | `true` or `false`: the main window's toggles (all `false`) |
| `postProcess` | `true` or `false`: the main window's Post-Processing toggle (`true`) |
| `smoothMotion`, `sharpen`, `normalizeAudio`, `vividMode`, `denoise`, `hdrToSdr`, `deband`, `autoCrop`, `stabilize`, `deinterlace`, `nightMode`, `upscaleVideo` | `true` or `false`: the post-processing filters (all `false`) |
| `smoothMotionMode` | `Precise (slow)`, `Balanced`, `Fast` (`Balanced`) |
| `smoothFPS` | A number from 24 to 120 (`60`) |
| `sharpenAmount` | A number from 0 to 2 (`1`) |
| `denoiseMode` | `NLMeans (HQ, slow)`, `hqdn3d (Balanced)` (`hqdn3d (Balanced)`) |
| `upscaleTarget` | `2× (Double)`, `1080p`, `1440p`, `4K (2160p)` (`2× (Double)`) |
| `gpuBackend` | `Auto (Recommended)`, `Off`, or a backend the Post-Processing window offers on your system (`Auto (Recommended)`) |

A value that is not allowed is skipped, and GoVid lists every skipped value when it loads the file. For a setting with fixed choices, `""` also leaves it unchanged. The [govid.json](govid.json) in the repository lists every key.

> **Note:** Standard JSON does not support comments. Adding them will cause a loading error.


## 🛠️ Built With

- [Fyne](https://fyne.io/) - An easy-to-use UI toolkit and app API written in Go
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) - YouTube-dl fork with additional features
- [FFmpeg](https://ffmpeg.org/) - A collection of libraries and tools to process multimedia content

## 🤝 Contributing

Interested in contributing? See [CONTRIBUTING.md](CONTRIBUTING.md) for the complete development, dependency, testing, and packaging workflow.

## 👤 Author

**David Bennehag** - [@DunderGG](https://github.com/DunderGG) - [dunder.gg](https://dunder.gg)

## 📄 License

This project is licensed under the GPL-3.0 - see the [LICENSE](LICENSE) file for details.

