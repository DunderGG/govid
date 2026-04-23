# GoVid — Development Roadmap

This document outlines planned features, improvements, and known limitations for GoVid. Items are organized by category and priority.

---

## 🚀 High Priority

### Batch Downloading
> Allow users to download multiple videos in a single session.

- [ ] Add a multi-line URL input field (one URL per line).
- [ ] Support loading a `.txt` file of URLs via a "Load from file" button.
- [ ] Show per-download progress rows in the log, and an overall queue counter.
- [ ] Ensure cancellation applies only to the active download, not the whole queue.

### Playlist Support
> Handle YouTube and Vimeo playlists gracefully.

- [ ] Detect when a pasted URL is a playlist and prompt the user to confirm downloading all items.
- [ ] Add a "Download playlist index X to Y" range option.
- [ ] Show total playlist size and estimated time before starting.
- [ ] Use `--yes-playlist` / `--no-playlist` flags in yt-dlp automatically based on user choice.

### In-App yt-dlp Updater
> Let users update yt-dlp from inside the app.

- [x] Add an "Update yt-dlp" option in the Tools menu.
- [x] Add a `--update` CLI flag for headless / scripted use.

### Self-Updating GoVid
> Let users update the app itself, not just yt-dlp.

- [ ] Add a "Check for GoVid updates" option in the Tools menu.
- [ ] Query the GitHub Releases API for the latest version tag.
- [ ] Compare against the current embedded version string and notify the user if out of date.
- [ ] Provide a direct download link or auto-replace the binary (with backup).

---

## 🛠️ Medium Priority

### Metadata & Thumbnail Embedding
> Embed rich metadata into downloaded files automatically.

- [ ] Use `--embed-thumbnail` and `--embed-chapters` yt-dlp flags.
- [ ] Embed title, artist, and upload date tags into MP3 and M4A files.
- [ ] Allow the user to toggle thumbnail embedding from the UI options.
- [ ] Automatic thumbnail and chapter injection via FFMPEG.

### Download History
> Keep a record of previously downloaded files.

- [ ] Maintain a local SQLite or JSON file storing URL, filename, date, and format.
- [ ] Show a "History" panel or tab in the UI.
- [ ] Warn the user when they paste a URL that has already been downloaded.

### Video Trimming
> Allow users to download only a specific segment of a video.

- [x] Add "Start Time" and "End Time" inputs to the UI (e.g., `00:01:30` – `00:05:00`).
- [x] Pass the range to yt-dlp via the `--download-sections "*HH:MM:SS-HH:MM:SS"` flag.
- [x] Use `--force-keyframes-at-cuts` to ensure clean cuts without re-encoding where possible.
- [x] Validate that both fields are filled or both left empty before starting.
- [x] Leave both fields empty to download the full video (default behaviour).

### Subtitle Support
> Download and optionally embed subtitles.

- [ ] Add a "Download subtitles" checkbox.
- [ ] Allow the user to select preferred subtitle language(s).
- [ ] Support both `.srt` sidecar files and embedded soft-subs in MKV.

### Custom Output Filename Template
> Let power users control how downloaded files are named.

- [ ] Add an advanced "Filename Template" input in the options.
- [ ] Pre-populate with the current default (`GoVid_%(title)s.%(ext)s`).
- [ ] Show a live preview of what the filename will look like.

### User Preference Persistence
> Remember the user's last-used settings across restarts.

- [x] Persist save destination using `fyne.CurrentApp().Preferences()`.
- [x] Persist selected format and quality between sessions.
- [x] Default save path to the executable's own directory for portability.

### Speed & Concurrency Limits
> Prevent downloads from saturating the user's connection.

- [ ] Add a "Max Download Speed" input (e.g., `5M` for 5 MB/s).
- [ ] Pass the value to yt-dlp via `--limit-rate`.
- [ ] Persist the setting alongside other preferences.

---

## 🎨 UI & UX Improvements

### Download Controls
> Give users control over active download sessions.

- [x] Add a Cancel button to abort an active download mid-session.
- [x] Show a real-time progress bar with smooth interpolation between yt-dlp updates.
- [x] Display a download summary on completion (duration, average speed, file size).
- [x] Add an "Open Folder" button to open the save destination in Explorer.
- [x] Add a "Save output to log file" checkbox to persist session logs to `.txt`.

### Dark / Light Mode Toggle
> Give users manual control over the application theme.

- [ ] Add a "Theme" option in the Tools menu or a toggle button in the header.
- [ ] Persist the theme preference using `fyne.CurrentApp().Preferences()`.
- [ ] Default to the OS system theme, but allow override.

### Resizable & Responsive Layout
> Improve behavior when the window is resized.

- [ ] Ensure the log output area grows vertically as the window is enlarged.
- [ ] Prevent the header branding from overlapping on small window sizes.
- [ ] Test and fix layout behavior on common resolutions (1366×768, 1920×1080).

### Notifications on Completion
> Alert the user when a download finishes, even if the window is minimized.

- [ ] Use OS-native notifications via `fyne.NewNotification` on completion.
- [ ] Show the filename in the notification body.
- [ ] Add a setting to enable/disable notifications.

### Clipboard Paste Button
> Streamline the URL entry workflow.

- [ ] Add a small clipboard icon next to the URL field.
- [ ] When clicked, paste the current clipboard text into the URL entry automatically.
- [ ] Validate that the pasted text looks like a URL before accepting it.

---

## 🔧 Technical Improvements

### Windows Distribution
> Make the app feel native and professional on Windows.

- [x] Create `build.bat` and `build.sh` scripts to simplify building from source.
- [x] Create `package.ps1` to automate building a release ZIP with bundled dependencies.
- [x] Bundle yt-dlp and ffmpeg in a `bin/` subfolder so no PATH setup is needed.
- [x] Add startup dependency check with a user-friendly dialog if tools are missing.

### Proper Version String
> Embed a build version for display and update-checking purposes.

- [ ] Define a `version` constant or use `go build -ldflags "-X main.version=1.0.0"`.
- [ ] Display the version in the window title bar or in a Help → About dialog.
- [ ] Use the version string when querying the GitHub Releases API.

### Error Recovery & Retry Logic
> Handle transient network failures more gracefully.

- [ ] Detect common transient errors (timeout, rate limit) in yt-dlp stderr output.
- [ ] Offer an automatic retry with exponential backoff (1, 5, 30 seconds).
- [ ] Surface a "Retry" button in the UI after a failed download.

### Config File Support
> Allow users to configure defaults via a config file.

- [ ] Support a `govid.json` or `govid.toml` config file in the app directory.
- [ ] Override format, quality, path, and speed limit defaults from the file.
- [ ] Useful for IT deployments or power users who script workflows.

### macOS & Linux Polish
> Close the gap on non-Windows platforms.

- [ ] Test and fix `build.sh` on macOS and Ubuntu.
- [ ] Add macOS `.app` bundle packaging with correct `Info.plist` metadata.
- [ ] Verify that `openDownloadFolder` works correctly on all supported distros.

---

## 🐛 Known Limitations

- [ ] `--recode-video` re-encodes every video even if already in the correct container — use `--remux-video` instead where possible to save time and quality.
- [ ] Progress bar resets to 0% between the video and audio merge phases — parse the `[Merger]` yt-dlp stage and hold the bar at 95%.
- [ ] Very long video titles can overflow the log panel horizontally — add text wrapping or truncation to log label widgets.
- [ ] The app has no minimum window size enforced — add `SetFixedSize` or `SetMinSize` guards on the main window.
- [ ] Log buffer is capped at 200 lines, which may truncate long download sessions — make the buffer limit user-configurable.
