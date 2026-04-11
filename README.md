# <img width="64" height="64" alt="appicon" src="https://github.com/user-attachments/assets/d81ed71e-cc17-4944-aafc-d94f7af758b4" /> &emsp; **GoVid**




GoVid is a high-performance, cross-platform video downloader built with Go. It provides a clean graphical interface for the powerful `yt-dlp` tool, allowing you to easily download and convert videos from various online platforms.

## ✨ Features

- **Cross-Platform GUI**: Modern interface built with the [Fyne toolkit](https://fyne.io/).
- **Multiple Formats**: Support for MP4, MKV, and MP3 (audio only).
- **Quality Control**: Select your preferred maximum resolution for downloads.
- **Real-time Progress**: Visual progress bars and a live scrollable log view.
- **Professional Post-Processing**: Seamless integration with FFMPEG for high-quality encoding.
- **Download Management**: Easily start, monitor, and cancel active downloads.
- **Log Export**: Option to save download logs to `.txt` files for troubleshooting.

## 🚀 Getting Started

### Prerequisites

To use GoVid, you must have the following tools installed and available in your system's `PATH`:

1.  **[yt-dlp](https://github.com/yt-dlp/yt-dlp)**: The core engine for video downloading.
2.  **[FFmpeg](https://ffmpeg.org/)**: Required for high-quality video/audio post-processing and conversion.

### Installation

Ensure you have [Go](https://go.dev/dl/) installed.

1. Clone the repository:
   ```bash
   git clone https://github.com/dunder/govid.git
   cd govid
   ```

2. Build the application:
   ```bash
   go build -o GoVid
   ```

3. Run the application:
   ```bash
   ./GoVid
   ```

## 📖 Usage

1. **Launch**: Open the GoVid application.
2. **URL**: Paste the video URL into the input field.
3. **Configuration**: Select your desired output format (MP4, MKV, or MP3) and maximum resolution.
4. **Save Location**: Choose the directory where you want to save the file.
5. **Download**: Click the "Download" button to start the process. You can monitor the progress in real-time.

## 🛠️ Built With

- [Fyne](https://fyne.io/) - An easy-to-use UI toolkit and app API written in Go
- [yt-dlp](https://github.com/yt-dlp/yt-dlp) - YouTube-dl fork with additional features
- [FFmpeg](https://ffmpeg.org/) - A collection of libraries and tools to process multimedia content

## 👤 Author

**David Bennehag** - [@dunder](https://github.com/dunder) - [dunder.gg](https://dunder.gg)

## 📄 License

This project is licensed under the GPL-3.0 - see the [LICENSE](LICENSE) file for details.

