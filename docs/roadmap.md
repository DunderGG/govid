# GoVid — Development Roadmap

This document outlines planned features, improvements, and known limitations for GoVid. Items are organized by category and priority.

---

## 🚀 High Priority

### Batch Downloading
> Allow users to download multiple videos in a single session.

- [X] Add a multi-line URL input field (one URL per line).
- [x] Support loading a `.txt` file of URLs via a "Load from file" button. (`UIManager.showLoadURLFile` in `url_input.go`: blank lines, `#` comments, lines that are not http(s) URLs, and URLs already in the field are skipped and counted in the log; batch mode is switched on.)
- [X] Show per-download progress rows in the log, and an overall queue counter.
- [X] Ensure cancellation applies only to the active download, not the whole queue.

### Playlist Support
> Handle YouTube and Vimeo playlists gracefully.

- [x] Detect when a pasted URL is a playlist and prompt the user to confirm downloading all items. (`checkURLs` probes each URL with `yt-dlp -J --flat-playlist`; a playlist opens a prompt with Download / Only this video / Cancel.)
- [x] Add a "Download playlist index X to Y" range option. (The prompt takes ranges such as `1-10`, `5-`, or `3,5,8`.)
- [ ] Show total playlist size and estimated time before starting. (Partly done: the prompt shows the video count and total length. The size is shown as "unknown", because sizing every video would need a full probe of each one.)
- [x] Use `--yes-playlist` / `--no-playlist` flags in yt-dlp automatically based on user choice. (Done differently: the chosen videos are queued as separate URLs, each downloaded with `--no-playlist`, so each gets its own progress row, cancel, retry, and history entry.)

### In-App yt-dlp Updater
> Let users update yt-dlp from inside the app.

- [x] Add an "Update yt-dlp" option in the Tools menu.
- [x] Add a `--update` CLI flag for headless / scripted use.

### Self-Updating GoVid
> Let users update the app itself, not just yt-dlp.

- [x] Add a "Check for GoVid updates" option in the Tools menu.
- [x] Query the GitHub Releases API for the latest version tag. (`ReleaseService` in `release_service.go`, shared with the yt-dlp check; once a day at startup, always from the menu.)
- [x] Compare against the current embedded version string and notify the user if out of date. *(Depends on: Proper Version String)* (A notice at startup, or the menu check, opens the release notes. `dev` builds are never prompted.)
- [x] Provide a direct download link or auto-replace the binary (with backup). (The release dialog's "Open download page" button opens the release on GitHub.)
- [ ] Update in place: download the release ZIP, check it against a published SHA-256, swap the running `.exe` (rename it to `.old`, move the new one in, relaunch, and delete `.old` on the next start).

---

## 🛠️ Medium Priority

### Metadata & Thumbnail Embedding
> Embed rich metadata into downloaded files automatically.

- [x] Use `--embed-thumbnail` and `--embed-chapters` yt-dlp flags. (`embedArgs` in `download_engine.go`; the thumbnail is converted to JPEG and skipped for WebM, which cannot hold cover art.)
- [x] Embed title, artist, and upload date tags into MP3 and M4A files. (`--embed-metadata`, for every format.)
- [x] Allow the user to toggle thumbnail embedding from the UI options. (Preferences → "Embed in File": Metadata and Thumbnail on by default, Chapters off; also `embedMetadata`/`embedThumbnail`/`embedChapters` in `govid.json`.)
- [x] Automatic thumbnail and chapter injection via FFMPEG. (yt-dlp embeds them with the bundled ffmpeg. Post-processing now maps streams explicitly, so cover art, chapters, subtitles, and tags survive the re-encode; Matroska covers are re-attached from extracted files.)

### Queue Manager
> Give users better control over batch downloads.

- [ ] Add pause, resume, cancel, retry, and reordering for queued downloads. (Partly done: the Queue panel (`queue_panel.go`, backed by `QueueModel`) removes and reorders waiting items, skips the running one, and retries failed or skipped ones while the queue runs. Pause/resume is still open: it needs yt-dlp's `.part` files and `--continue`, which conflict with today's `--no-part --no-continue`, plus partial-file cleanup that understands them.)
- [ ] Show per-item status, ETA, and completion state in the queue list. (Partly done: each row shows Waiting, Checking, Downloading with its percentage, Post-processing, Done, Failed, or Skipped, and the panel title counts progress, e.g. "7 of 20 done, 1 failed". No per-item ETA yet.)
- [ ] Preserve queued items after app restarts if the user chooses to save the session.

### Presets / Profiles
> Save common download setups for quick reuse.

- [ ] Let users save named presets for common workflows like audio-only, 1080p MP4, and playlist downloads.
- [ ] Allow presets to store format, quality, output path, subtitles, metadata, and speed-limit settings.
- [ ] Add preset import/export so users can move their settings between machines.

### yt-dlp Auto-Update
> Keep the bundled downloader current without manual steps.

- [x] Check whether yt-dlp is outdated when the app starts or on demand. (A background check at startup, at most once a day via the GitHub Releases API, shows a notice with an "Update now" button; it can be turned off with the "Check for updates on startup" preference. Tools → Update yt-dlp checks on demand.)
- [x] Add a one-click update action for yt-dlp in the Tools menu.
- [x] Show the currently installed yt-dlp version alongside the latest available version. (In the Update yt-dlp dialog and the About window.)

### Format Browser
> Make yt-dlp format selection easier to understand.

- [ ] Show available formats in a readable table with resolution, codec, bitrate, and container.
- [ ] Add a preview of the final format choice before starting a download.
- [ ] Let users pin or favorite preferred formats for faster selection.

### Authentication Support
> Support downloading from websites like Twitter, which requires cookies.

- [X] Add a "Cookies File" selector in Preferences to pass `--cookies` to yt-dlp.

### Video Trimming
> Allow users to download only a specific segment of a video.

- [x] Add "Start Time" and "End Time" inputs to the UI (e.g., `00:01:30` – `00:05:00`).
- [x] Pass the range to yt-dlp via the `--download-sections "*HH:MM:SS-HH:MM:SS"` flag.
- [x] Use `--force-keyframes-at-cuts` to ensure clean cuts without re-encoding where possible.
- [x] Either field can be used alone (start-only downloads to end; end-only downloads from the beginning).
- [x] Leave both fields empty to download the full video (default behaviour).

### Subtitle Support
> Download and optionally embed subtitles.

- [x] Add a "Download subtitles" checkbox. (Preferences → **Subtitles**: Off / Embed / Save as .srt / Both, plus "Include auto-generated"; also `subtitles`/`autoSubtitles` in `govid.json` and the session log.)
- [x] Allow the user to select preferred subtitle language(s). (**Subtitle Languages**, in yt-dlp `--sub-langs` syntax, default `en.*`. The log lists the languages the probe found and warns when none matches.)
- [x] Support both `.srt` sidecar files and embedded soft-subs in MKV. (In MP4 and WebM too; `subtitleArgs` in `download_engine.go`. Sidecar files are kept out of post-processing and history, and a subtitle download that fails is retried once without subtitles.)

### Disk Space Pre-check
> Prevent mid-download failures due to full drives.

- [x] Query destination drive for available space before starting a download. (`freeDiskBytes` in `sys_windows.go`/`sys_others.go`, checked before every queued item, including each video picked from a playlist, which is probed just before it downloads.)
- [x] Notify user if estimated file size exceeds available bytes. (The size comes from the URL probe, plus a 10% margin, doubled with post-processing. The prompt offers Continue anyway / Cancel, or Skip / Continue / Stop in a batch.)

### User Preference Persistence
> Remember the user's last-used settings across restarts.

- [x] Persist save destination using `fyne.CurrentApp().Preferences()`.
- [x] Persist selected format and quality between sessions.
- [x] Default save path to the executable's own directory for portability.
- [x] Make the log buffer line limit user-configurable (currently hard-coded to 200 lines).

### Speed & Concurrency Limits
> Prevent downloads from saturating the user's connection.

- [X] Add a "Max Download Speed" input (e.g., `5M` for 5 MB/s).
- [X] Pass the value to yt-dlp via `--limit-rate`.
- [X] Persist the setting alongside other preferences.

---

## 🎨 UI & UX Improvements

### Download Controls
> Give users control over active download sessions.

- [x] Add a Cancel button to abort an active download mid-session.
- [x] Show a real-time progress bar with smooth interpolation between yt-dlp updates.
- [x] Display a download summary on completion (duration, average speed, file size).
- [x] Add an "Open Folder" button to open the save destination in Explorer.
- [x] Add a "Save output to log file" checkbox to persist session logs to `.txt`.
- [x] Hold the progress bar at ~95% during the `[Merger]` phase instead of resetting to 0%. (`ProcessCallbacks.OnPhase`; the status shows "Merging…" or "Converting…".)

### Dark / Light Mode Toggle
> Give users manual control over the application theme.

- [X] Add a "Theme" option in the Tools menu or a toggle button in the header.
- [X] Persist the theme preference using `fyne.CurrentApp().Preferences()`.
- [ ] Default to the OS system theme, but allow override.

### Drag-and-Drop Support
> Streamline adding URLs to the application.

- [x] ~~Allow dragging URLs from a web browser directly into the URL input/batch area.~~ Closed as not possible on Windows: Fyne's GLFW layer accepts only dropped files there (`WM_DROPFILES`), and browsers offer a dragged link as text. Done instead: dropping a `.txt` list or an internet shortcut (`.url`, `.desktop`) onto the window adds its URLs (`UIManager.handleDrop`), so dragging a link to the desktop first and dropping the shortcut works. On X11, GLFW passes a dropped link through as a path, which is accepted too (untested).

### Resizable & Responsive Layout
> Improve behavior when the window is resized.

- [X] Ensure the log output area grows vertically as the window is enlarged.
- [X] Prevent the header branding from overlapping on small window sizes.
- [X] Test and fix layout behavior on common resolutions (1366×768, 1920×1080).
- [ ] Add text wrapping or truncation to log label widgets to prevent horizontal overflow on very long video titles.

### Notifications on Completion
> Alert the user when a download finishes, even if the window is minimized.

- [X] Use OS-native notifications via `fyne.NewNotification` on completion.
- [X] Show download summary (duration, average speed, file size) in the notification body.
- [X] Add a setting to enable/disable notifications.

### Tray Integration
> Background the app during long batch downloads.

- [ ] Add "Minimize to Tray" support.
- [ ] Right-click menu for tray icon (Pause/Resume, Open Folder, Exit).

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

- [x] Inject version at build time via `go build -ldflags "-X main.version=1.0.0"`.
- [x] Display the version in the Help → About dialog.
- [x] Use the version string when querying the GitHub Releases API. (Sent as the `GoVid/<version>` User-Agent and compared with the latest release tag. `build.bat`/`build.sh` now take the version from the git tag on the built commit, falling back to `dev`.)

### Error Recovery & Retry Logic
> Handle transient network failures more gracefully.

- [x] Detect common transient errors (timeout, rate limit) in yt-dlp stderr output.
- [x] Offer an automatic retry with exponential backoff (1, 5, 30 seconds).
- [x] Surface a "Retry" button in the UI after a failed download.

### UX Improvements
- [x] Prevent duplicate application windows (Preferences, About, Help, Post-Processing).
- [ ] Add hotkeys for the UI, like escape to close windows or ctrl-o to open folder.
- [x] Add a button to each history entry to quickly re-add to URL field, or other actions. (The History window is now a searchable list; each row has Re-add, Show in folder, and Copy URL, and rows whose file is gone are greyed out.)

### Automatic "Best-Fit" Quality
> Smart handling of missing quality tiers.

- [x] Show a "Smart Downscale" notification if the requested resolution isn't available. (`qualityFit` compares the probe's `height` with the cap and logs and shows a notice, e.g. "1080p isn't available for this video; downloading 720p", or warns when the selector fell back to a higher resolution. Capped files are named after the height actually downloaded (`%(height&_{}p|)s`), and audio files get no quality label.)

### Linux Polish
> Close the gap on the currently supported non-Windows platform.

- [ ] Test and fix `build.sh` on Ubuntu.
- [ ] Verify that `openDownloadFolder` works correctly on all supported distros.

### macOS Investigation
> Assess the work required before macOS can be considered a supported platform.

- [ ] Investigate building and running GoVid on macOS, including Fyne/CGO prerequisites, `build.sh`, process handling, bundled FFmpeg/yt-dlp dependencies, and release packaging requirements.

### Post-Processing Features
> Improve output quality for downloaded files.

- [x] Use `--remux-video` instead of `--recode-video` where the container already matches, to avoid unnecessary re-encoding.
- [x] **Smooth Motion**: Interpolate frames up to 120 FPS for fluid playback.
- [x] **Sharpen Video**: Apply an unsharp mask to restore edge detail.
- [x] **Normalize Audio**: Loudness normalization via the `loudnorm` filter.
- [x] **Vivid Mode**: Automated color correction (brightness, contrast, saturation).
- [x] **Denoise (Advanced)**: HQ noise reduction via `nlmeans` or `atadenoise` to clean up low-quality web videos.
- [x] **HDR to SDR Tone Mapping**: Convert 4K HDR downloads for standard monitors via `zscale` to prevent "washed out" colors. (Fixed: each file's colour tags are probed, only PQ/HLG sources are tone mapped with the input colour space stated explicitly, SDR sources are left unchanged, and the output is tagged BT.709.)
- [x] **Fix Gradient Banding**: Use the `deband` filter to remove "steps" in skies or dark scenes caused by web compression.
- [x] **Auto-Crop Black Bars**: Detect and remove letterboxing/pillarboxing automatically.
- [x] **Video Stabilization**: Use `deshake` to smooth out handheld footage or shaky web uploads.
- [x] **Deinterlace**: Remove combing artifacts from older archival or TV-rip content via `bwdif`.
- [x] **Dynamic Compression**: "Night Mode" for audio to balance dialogue and loud action.
- [x] **Resolution Upscaling**: Professional upscaling (e.g., 720p to 1080p) via advanced FFmpeg scalars.
- [X] **Progress**: We are showing frame number, use that to show total progress.

### Portable Mode
> Carry settings alongside the executable.

- [ ] Add a "Portable Mode" toggle to store preferences in `settings.json` locally.

### General improvements
> Any general improvements we can think of
- [X] Make sure only one Preferences window can be opened.
- [x] We may want to start limiting how much we are logging. There is a lot of "noise" we don't care about. (yt-dlp's `[debug]` lines go to the log file only unless the new "Debug Output" preference is on, and the log view shows one in-place progress line per file instead of one line per update. `--verbose` is kept so the log file stays complete for bug reports.)
- [ ] The code for the guide window needs improving. Get rid of extremely long text strings.
- [X] Errors from ffmpeg sometimes gets buried in the verbose logs. Maybe Errors should be logged to separate file?
- [ ] Investigate GPU acceleration for FFmpeg.
	- [X] Identify [target acceleration backends](gpu-acceleration.md) per OS: `nvenc`/`cuda` (NVIDIA), `qsv` (Intel), `amf` (AMD), and `vaapi` (Linux).
	- [X] Verify which backends are available in our [current bundled FFmpeg build](gpu-acceleration.md#5-current-bundled-build-inventory) (`ffmpeg -hide_banner -encoders`, `-hwaccels`, `-decoders`, `-filters`). 
		- [ ] Re-run for future Linux artifacts.
	- [X] Decide [feature scope](gpu-acceleration.md#7-feature-scope-decision): which post-processing operations should use GPU first (e.g. scaling, tone mapping, denoise) and which remain CPU.
		- [ ] Revisit the deferred GPU scale/deinterlace fast-path once final-encode acceleration is implemented and benchmarked.
	- [X] Add [runtime capability detection](gpu-acceleration.md#8-recommended-implementation-order) in Go (`GPUCapabilityService` in `gpu_capability.go`) and cache results by backend/vendor so unsupported paths are never selected.
	- [X] Design [command builders](gpu-acceleration.md#7-feature-scope-decision) for GPU pipelines (`PlanEncoder`/`EncoderPlan` in `gpu_capability.go`) with safe CPU fallback equivalents.
	- [X] Add a [user setting](gpu-acceleration.md#8-recommended-implementation-order): `Auto` (recommended), explicit backend selection, and `Off` for troubleshooting. ("Encoder Backend" selector in the Post-Processing window, wired through `applyFFmpegFilters`.)
	- [X] Implement [strict fallback behavior](gpu-acceleration.md#6-pipeline-design-constraints): if GPU init fails, retry once with CPU and log a concise reason. (`PPEngine.retryWithCPU` in `pp_engine.go`.)
	- [ ] Benchmark representative jobs (1080p, 1440p, 4K; short and long clips) for speed, quality, and failure rate versus CPU.
	- [X] Add [guardrails for known edge cases](gpu-acceleration.md#10-runtime-guardrails): a concurrent hardware-encoder-session cap and a stall watchdog with CPU fallback. (`PPEngine.gpuSem`/`gpuStallTimeout` in `pp_engine.go`.)
	- [X] Expose diagnostics in logs (detected backend, selected path, fallback reason) to simplify bug reports.
	- [X] Document platform prerequisites (driver versions, required FFmpeg features) and add a quick verification checklist to release docs.

---

## 🧹 Code Quality & Refactoring

### Test Coverage
> Keep unit tests alongside the Go package they exercise; reserve `testdata/` for fixtures and a separate `tests/` directory for future end-to-end coverage.

- [X] Add `download_test.go` for URL validation, output filename derivation, cancellation, and download error handling. (Drives `startDownload`/`runYtDlp` through the Fyne test driver and the fake yt-dlp, including the batch queue: blank-line skipping, per-item cancel that keeps the queue running, and duplicate-name renaming.)
- [X] Add `download_engine_test.go` for yt-dlp argument construction, queue/concurrency behavior, and retry outcomes. (Covers `BuildArgs`, `Execute` retry/cancel/launch-failure paths, `Run`, `FinalizeFiles`, and `uniquePath` against a scripted fake yt-dlp. `DownloadEngine` handles one URL at a time; the batch queue lives in `startDownload` and is exercised by `download_test.go`.)
- [X] Add `postprocess_test.go` for post-processing filter generation, enabled-option combinations, and invalid settings. (Also covers processing-load scoring, filter labels, and the shared FFmpeg progress/byte/duration formatters.)
- [X] Add `history_service_test.go` for persistence round trips and missing or corrupted history files.
- [X] Add `dependency_service_test.go` for executable discovery, version parsing, and missing dependency behavior. (Also covers `RunUpdate`; external tools are simulated by the fake-tool harness in `fake_tool_test.go`.)
- [X] Add `helpers_test.go` for path, extension, and formatting edge cases. (Covers what `helpers.go` owns: exit-code mapping, cancel-func handoff, progress clamping, size-token parsing, and preference application. The path/extension/formatting helpers live elsewhere and are tested beside their owners: `uniquePath` in `download_engine_test.go`, `formatBytes`/`formatDuration`/`formatFFmpegProgress` in `postprocess_test.go`.)
- [X] Add `logscanner_test.go` for parsing representative yt-dlp and FFmpeg output. (yt-dlp stdout/stderr fixtures live in `testdata/`; `logscanner.go` only parses yt-dlp output, so FFmpeg progress parsing is covered by `formatFFmpegProgress` tests in `postprocess_test.go`.)
- [X] Add `log_service_test.go` for log buffering, rotation, and error-log behavior. (Rotation is the daily-filename scheme of `SessionLogPath`/`ErrorLogPath`; there is no size-based rotation to test.)
- [X] Add `testdata/` fixtures for representative logs, preference/config JSON, and media metadata responses.
- [X] Add `tests/` end-to-end coverage only after the core application logic has been separated from the Fyne UI.

### Window Management Boilerplate
> Eliminate repeated singleton-window guard patterns across ui.go.

- [ ] Extract the `focusOrCreate` guard (`if app.xWindow != nil { RequestFocus; return }`) into a reusable helper — repeated ~4 times.
- [ ] Extract `SetOnClosed(func() { app.xWindow = nil })` into a shared helper to remove identical closures on every dialog window.

### Named Constants for Magic Numbers
> Replace unexplained numeric literals with self-documenting names.

- [ ] Define constants for dialog window sizes (`prefsWindowWidth`, `postProcessWindowHeight`, etc.) currently scattered across ui.go.
- [ ] Define constants for post-processing load thresholds (e.g. `loadLightThreshold = 15`, `loadModerateThreshold = 35`) in postprocess.go.
- [ ] Define constants for per-filter processing cost values (e.g. `costSmoothMotionFast`, `costDenoiseHQ`) in postprocess.go.
- [X] Replace the `1<<31 - 1` unlimited sentinel in `parseLogLimit` with `math.MaxInt32` for clarity.

### SVG Icon Deduplication
> Halve icon code volume by templating the dark/light variants.

- [X] Extract a `svgWithColor(color string) string` helper in icons.go — dark and light variants of each icon differ only in their fill color.

### Split Long Functions
> Break up functions that mix multiple concerns into focused sub-functions.

- [ ] Split `runYtDlp()` (~180 lines) into `buildYtDlpArgs()` and `parseYtDlpOutput()` in download.go.
- [ ] Split `createUI()` (~560 lines) into `createInputCard()`, `createStatusCard()`, and `createLogSection()` in ui.go.
- [ ] Split `startDownload()` into `validateDownloadInputs()` and `initializeDownloadSession()` in download.go.

### Naming Consistency
> Align naming conventions across the codebase.

- [x] Fix the `smoothMotion` UI field vs. `"upscale"` preference key mismatch — both refer to the same setting.
- [X] Rename `ppJob` struct to `PostProcessJob` to match the full-word naming style of other structs (`DownloaderApp`, `UIWidgets`).

### Error Handling
> Remove silently ignored errors at system boundaries.

- [x] Check and handle errors from `cmd.StdoutPipe()` / `cmd.StderrPipe()` in download.go instead of discarding them with `_`.

### Deduplicate Status Indicator Animation
> The pulsing goroutines for "active" and "processing" states are nearly identical.

- [ ] Extract a `pulseColor(stopCh chan struct{}, baseColor color.RGBA)` helper in helpers.go and reuse it for both animation states.

---

## ⚡ UI Performance & Stability

### Event Queue Backpressure
> Prevent the UI thread from being overwhelmed by too many frequent `fyne.Do` calls.

- [x] Add throttling/coalescing for status text updates during FFmpeg progress (e.g. max 5-10 updates/sec). (`latestValueThrottle` in `throttle.go`; `updateStatus` applies the newest status at most every 150 ms.)
- [x] Add throttling/coalescing for progress-bar smoothing updates so idle frames are skipped when value changes are tiny. (The smoother ticks at ~30 fps and skips changes below 0.002.)
- [x] Avoid enqueueing repeated identical status/progress values to the UI thread.

### Log Rendering Efficiency
> Reduce UI work when many log lines are produced.

- [x] Add batched log flush (buffer lines and append on a short interval) instead of per-line UI updates. (`UIManager.appendLogLine` queues lines; `flushLog` renders them every 100 ms in one `fyne.Do`.)
- [x] Add a hard upper cap for on-screen log lines even when user selects "Unlimited" (file logging remains unlimited). (`maxScreenLogLines` = 5000.)
- [x] Avoid forced `ScrollToBottom()` on every append when user has manually scrolled up. (The view follows new lines only while it is at the bottom.)

### Goroutine Lifecycle Hygiene
> Ensure background tickers/goroutines never outlive their intended state.

- [ ] Add lightweight lifecycle diagnostics (start/stop markers + goroutine count) around pulse, smoother, and post-process progress loops.
- [ ] Audit all ticker-based loops to guarantee a single owner and deterministic stop path.
- [x] Add a safe shutdown path that cancels active contexts and confirms worker completion before quit. (`DownloaderApp.Shutdown` stops the whole session, waits up to 5 s for it off the UI thread, closes the session log, then quits. Cancelling now kills the whole yt-dlp/ffmpeg process tree via `newToolCommand` in `process.go`, and a failed or cancelled download's partial `GOVID` files are removed.)

### UI Thread Safety Audit
> Eliminate remaining direct widget mutations outside `fyne.Do`.

- [ ] Add/maintain a checklist of all widget writes and enforce UI-thread-safe wrappers.
- [ ] Add regression checks for known freeze scenarios (idle after long run, long batch with verbose logs).
- [x] Fix the data races `go test -race` reports in the `startDownload` session tests (`download_test.go`): the progress-smoother goroutine reads `stats.targetPct` and `progress.Value` off the UI thread while the yt-dlp scanner goroutine writes `targetPct` via `setProgress`. Guard `targetPct` (e.g. an atomic) and read the bar value inside `fyne.Do`, then add `-race` to the documented test command.

### Observability for Freeze Reports
> Make future freeze incidents diagnosable from logs.

- [ ] Add optional debug mode that periodically logs UI heartbeat, queued update counters, and active worker counts.
- [ ] Add a "Copy Diagnostics" action that captures runtime state (settings + goroutine/ticker snapshot) for bug reports.

---

## 💤 Low Priority

### Audio Controls
> Advanced overrides for audio-focused users.

- [ ] Add a selector for specific audio bitrates (128k–320k).
- [ ] Add a "Strip Audio" toggle to extract streams without full conversion.

### Post-Processing Scripts
> Automation for completed downloads.

- [ ] Allow specifying a shell command to run after completion (with file path placeholder).

### Custom Output Filename Template
> Let power users control how downloaded files are named.

- [ ] Add an advanced "Filename Template" input in the options.
- [ ] Pre-populate with the current default (`GoVid_%(title)s.%(ext)s`).
- [ ] Show a live preview of what the filename will look like.

### Config File Support
> Allow users to configure defaults via a config file.

- [X] Support a `govid.json` or `govid.toml` config file in the app directory.
- [X] Override format, quality, path, and speed limit defaults from the file.
- [ ] Override all the other preferences as well from the file.

### Download History
> Keep a record of previously downloaded files.

- [X] Maintain a local SQLite or JSON file storing URL, filename, date, and format.
- [x] Capture and store the real source title in history using a structured yt-dlp output field; fall back to filename/title inference if unavailable. (The probe's `title`, or the playlist entry's, travels on the queue item into `DownloadRecord.Title`; the probe's `id` and `extractor_key` are stored too.)
- [X] Show a "History" panel or tab in the UI.
- [x] Warn the user when they paste a URL that has already been downloaded. (`skipDownloaded` in `duplicates.go`, after the URLs are checked: matched by video ID and extractor, so other URL forms of the same video count, or by exact URL for older entries. Download again / Skip, plus Skip all duplicates in a batch.)
- [x] Add a toggle to keep history or not, and a button to clear history (warn first). (Preferences → "Keep download history", on by default, also `keepHistory` in `govid.json`; turning it off offers to delete the history. "Clear History" in the History window asks first.)

### Structural Refactoring
> Decouple core logic from the main UI controller.

See the [refactoring roadmap](refactor_roadmap.md) for component-level status and detailed next steps.

### Log Management
> Improve technical troubleshooting.

- [ ] Add a search/filter bar to the log output area.

### Clipboard Paste Button
> Streamline the URL entry workflow.

- [x] Add a small clipboard icon next to the URL field (or auto-detect URL on focus). (A paste button next to the field; no auto-detect on focus.)
- [x] When clicked, paste the current clipboard text into the URL entry automatically. (Several lines switch on batch mode; URLs already in the field are skipped.)
- [x] Validate that the pasted text looks like a URL before accepting it. (Every non-blank line must be an http(s) URL, or nothing is pasted.)

### FFmpeg On-Demand
> Keep the initial download size small.

- [ ] Offer to download/extract FFmpeg on-demand if missing instead of bundling.

---

## 🐛 Known Limitations

> No known limitations at this time — all previously identified issues have been moved to the relevant roadmap sections above.
