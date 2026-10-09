# GoVid — Architecture Overview

> **Audience:** contributors and maintainers.  
> **Keep this file current:** update it after every refactoring step (see the bottom of this page for the checklist).

---

## 1. What is GoVid?

GoVid is a desktop video-downloader built on top of [yt-dlp](https://github.com/yt-dlp/yt-dlp).
It provides a graphical interface, real-time progress feedback, optional FFmpeg post-processing, and a persistent download history.

| Property | Value |
|---|---|
| Language | Go 1.24+ |
| UI toolkit | [Fyne v2](https://fyne.io/) |
| External tools | `yt-dlp`, `ffmpeg`, `ffprobe` |
| Platforms | Windows, Linux |

---

## 2. Diagram index

| Diagram | File | Description |
|---|---|---|
| Class diagram | `docs/classes.puml` | All major structs, their fields, methods, and relationships |
| Download sequence | `docs/sequence-full.puml` | Full startup → download → post-process → teardown flow |

---

## 3. File map

```
govid/
├── main.go                 Entry point; constructs DownloaderApp, wires close-intercept (→ Shutdown), calls ShowAndRun
├── types.go                Shared app and widget types (DownloaderApp, UIWidgets and its control groups, DownloadStats)
│
├── ── Services / Engines ──────────────────────────────────────────
├── download_engine.go      DownloadEngine — yt-dlp arg builder and retry executor
├── probe.go                DownloadEngine.Probe / ProbeVideo — yt-dlp -J --flat-playlist; MediaInfo / PlaylistEntry
├── pp_engine.go            PPEngine — concurrent FFmpeg post-processing worker pool
├── preference_service.go   PreferenceService — preference keys, defaults, Load/Save/Reset
├── portable.go             Portable Mode: fileStore (fyne.Preferences in settings.json), chooseSettingsStore (GoVid.portable marker), setPortable (copy and switch), flushSettings
├── config_file.go          AppConfig / configRules — govid.json: LoadFromFile, MergeConfig, ExportConfig, WriteConfigFile
├── presets.go              Preset — starter presets, groups, list edits; LoadPresets / SavePresets / ReadPresetFile / WritePresetFile
├── preset_ui.go            UIManager preset dropdown, "(modified)" marker, Save / Manage / Import / Export dialogs
├── history_service.go      HistoryService — Load/AppendAll/Clear; DownloadRecord and DownloadHistoryEntry types
├── history_window.go       UIManager.showHistory — the searchable History list with Re-add / Show in folder / Copy URL, loaded off the UI thread
├── duplicates.go           skipDownloaded / askDuplicate — "Already downloaded" check before a session downloads
├── queue_model.go          QueueModel — the session's queue: items, per-item status, Next / Move / Remove / Retry
├── queue_panel.go          UIManager.showQueue — the collapsible Queue panel above the log
├── parallel.go             runParallel (Simultaneous Downloads workers), itemControls (per-item Skip/Pause), itemRun, showParallelProgress, backOff, reserveSpace
├── pause_resume.go         downloadContext (Pause / Cancel / quit causes), Pause/Resume button, waitForResume, finishQueue, offerQueueRestore / resumeQueue
├── queue_store.go          QueueStore — queue.json: the waiting and paused items saved when GoVid quits
├── log_service.go          LogService — session log open/close, error log routing, buffer-limit management
├── dependency_service.go   DependencyService — binary path resolution, dependency checks, tool versions (Installed), JS runtime discovery (JSRuntime), yt-dlp updater
├── tool_installer.go       ToolInstaller — installs yt-dlp, FFmpeg + ffprobe, and Deno into bin/: verified download, staged .new → rename swap with rollback
├── http_fetch.go           httpFetcher — GET with GoVid's User-Agent; text files, and files hashed with SHA-256 as they stream (SelfUpdater, ToolInstaller)
├── components.go           componentStatuses / installComponent / checkTools — Tools → Components actions, startup "missing tool" notices, FFmpeg filter check
├── components_window.go    UIManager.showComponents — the Components window (installed / latest / Install · Update · Reinstall)

├── ui_manager.go           UIManager — main window layout (createUI, createMainMenu), secondary window lifecycle
│                           (About, Help, History, Prefs, PP), and preference/dependency UI wrapper methods
├── gpu_capability.go       GPUCapabilityService — GPU backend capability detection and cache (see docs/gpu-acceleration.md)
├── release_service.go      ReleaseService — latest GitHub release lookups with a daily cache; version comparison
├── update_check.go         Startup yt-dlp and GoVid update checks, their notices, installed/latest yt-dlp versions
├── self_update.go          SelfUpdater — Update now: download ZIP + SHA256SUMS, verify, swap GoVid.exe, restart; startup cleanup
├── release_dialog.go       Tools → "Check for GoVid updates" and the release-notes dialog
│
├── ── Orchestration ───────────────────────────────────────────────
├── download.go             DownloaderApp.startDownload / runYtDlp — UI orchestration for a download session
├── playlist.go             checkURLs / checkItem — probes each URL, expands playlists into queue items, probes entries before download; range parsing
├── playlist_dialog.go      UIManager.askPlaylist — the "Playlist detected" prompt
├── url_input.go            UIManager load-from-file / paste / drop of URLs; parseURLList, shortcutURL
├── cookies.go             cookieArgs (--cookies-from-browser / --cookies), cookieLabel (log names only the source), classifyAccessError / accessHint (cookie failures, sign-in errors)
├── filename_template.go    outputTemplate (the -o template: the Filename Template, {quality}, _TRIM, the download token), validateFilenameTemplate, previewFilename and the Preferences row
├── formats.go              FormatInfo (one format of the info JSON), formatRows (the Format Browser's table), describeDownload / selectedIDs ("Will download"), formatArgs (-f pick or selector, -S preferred codec)
├── formats_window.go       UIManager.showFormatWindow, formatChoice; showFormatsForURL / showFormatsForItem and the per-URL formatPicks
├── subtitles.go            matchSubLangs / reportSubtitles — which subtitle languages a video has and which are downloaded
├── disk_space.go           checkDiskSpace — free-space check before each queued item; the "Low disk space" prompt
├── live.go                 Live and scheduled streams: MediaInfo.IsLive/IsUpcoming, prepareLive, monitorRecording, finishRecording (remux), recording view and free-space watch
├── live_dialog.go          UIManager.askLive — Record from now / from the start / Skip, or Wait and record / Skip
├── postprocess.go          PostProcessSettings, buildPostProcessFilters / applyFFmpegFilters — value struct + thin UI wrapper; shared format/scan helpers
├── logscanner.go           DownloadEngine.watchOutput / parseProgress — yt-dlp stdout/stderr parsing goroutines
│
├── ── UI ──────────────────────────────────────────────────────────
├── ui.go                   Thin DownloaderApp delegates to UIManager's secondary windows; shared roundedCard/accentBar helpers
├── options.go              Named labels and option lists for every enum-like selector (format, quality, theme, PP modes…)
├── helpers.go              Thread-safe UI updates, applyPreferencesToWidgets, cancellation callback guard, GPU detection kickoff
├── throttle.go             latestValueThrottle — applies the newest of a stream of values at most once per interval (status label)
├── diagnostics.go          Help → Copy diagnostics (diagnosticsReport, anonymizer), the Debug Output heartbeat (UI round trip, goroutines, tools running), trackTool, markLoop
│
├── ── Assets / Platform ───────────────────────────────────────────
├── theme.go                darkTheme, lightTheme, and systemTheme (implement fyne.Theme); resolveThemeMode
├── icons.go                SVG icon registry; themedIcon() helper
├── shortcuts.go            Keyboard shortcuts: menu items and canvas shortcuts (Ctrl+Enter, Ctrl+O, Ctrl+L, Ctrl+Shift+V, Ctrl+H, Ctrl+,), F1, closeOnEscape
├── embedded_icon.go        Bundled app icon (resourceAppiconPng)
├── process.go              newToolCommand, newOutputScanner / drainOutput — starts yt-dlp/FFmpeg so cancelling kills the whole process tree; reads their output
├── sys_windows.go          Windows-only: hide console windows; kill process trees with taskkill /T; freeDiskBytes (GetDiskFreeSpaceEx)
├── sys_others.go           Non-Windows: no-op hideWindow; kill process trees via a process group; freeDiskBytes (statfs)
│
├── ── Config / Build ──────────────────────────────────────────────
├── govid.json              Optional override config (loaded via "Load from Config" in Preferences)
├── go.mod / go.sum         Module definition
├── build.bat / build.sh    Release build scripts (inject version via -ldflags)
```

---

## 4. Component reference

### 4.1 `DownloaderApp` — application coordinator  
*Defined in:* `types.go`; methods spread across `download.go`, `postprocess.go`, `helpers.go`, `ui.go`

The central type. It holds pointers to every service and is the sole owner of the Fyne main window. All Fyne widget mutations **must** go through `fyne.Do(func() { … })` when called from a background goroutine.

| Field | Purpose |
|---|---|
| `window` | The primary Fyne window |
| `ui *UIWidgets` | All Fyne widgets (see §4.2) |
| `uiManager *UIManager` | Secondary window lifecycle (see §4.3) |
| `prefSvc *PreferenceService` | Preference persistence (see §4.6) |
| `historySvc *HistoryService` | Download history persistence (see §4.8) |
| `logSvc *LogService` | Session and error log files (see §4.7) |
| `depSvc *DependencyService` | Binary path resolution, dependency checks, updater (see §4.9) |
| `gpuSvc *GPUCapabilityService` | GPU backend capability detection and cache (see §4.11) |
| `releaseSvc *ReleaseService` | Latest-release lookups for update checks (see §4.12) |
| `toolInstaller *ToolInstaller` | Installs yt-dlp, FFmpeg, and Deno into `bin/` for Tools → Components (see §4.9) |
| `installing atomic.Bool` | Set while a tool is being installed or yt-dlp updated (`installComponent`, `updateYtDlp`); the Download button is disabled and `startDownload` refuses meanwhile |
| `askPlaylist func(ctx, playlistPrompt) playlistDecision` | Asks which videos of a playlist to download; set to `UIManager.askPlaylist`, stubbed in tests |
| `freeBytes func(path string) (uint64, error)` | Free space on a folder's volume; `freeDiskBytes`, faked in tests |
| `askDiskSpace func(ctx, diskSpacePrompt) diskSpaceDecision` | Asks what to do when a download will not fit; set to `UIManager.askDiskSpace`, stubbed in tests |
| `askLive func(ctx, livePrompt) liveDecision` | Asks how to record a live or scheduled stream; set to `UIManager.askLive`, stubbed in tests |
| `recording atomic.Bool` | Set while a live stream is recorded (`setRecordingView`); the Cancel button then reads "Stop recording" and does not log a cancel |
| `controls map[int]*itemControls` | Skip and Pause of each item downloading, by queue ID (`registerSkip`, `registerPause`; guarded by `cancelMu`), so the Queue panel's row buttons act on their own item; `requestPause` pauses them all |
| `promptMu sync.Mutex` | One prompt at a time (live stream, disk space) when several items download at once |
| `reservedBytes atomic.Int64` | Disk space the downloads in progress are expected to need; `checkDiskSpace` leaves it for them (`reserveSpace`) |
| `quitting atomic.Bool` | Set by `Shutdown`: a download it stops is paused and the queue saved |
| `queueStore *QueueStore` | `queue.json` (see §7, Pause and resume) |
| `askRestoreQueue func(count, answer)` | Asks whether to resume the saved queue; set to `UIManager.askRestoreQueue`, stubbed in tests |
| `stats *DownloadStats` | Real-time download metrics for progress smoothing |
| `statusThrottle *latestValueThrottle[string]` | Rate-limits status label updates to one per 150 ms and skips repeats; `updateStatus` goes through it (`throttle.go`) |
| `cancelMu sync.Mutex` | Guards access to the active cancellation callbacks (`cancelFn`, `stopFn`) |
| `cancelFn context.CancelFunc` | Cancels the active download context; in batch mode it skips only the current URL (`RequestCancel`) |
| `stopFn context.CancelFunc` | Stops the whole session, including the rest of a batch queue and post-processing (`StopSession`) |
| `sessions sync.WaitGroup` | Counts running sessions so `Shutdown` can wait for them to clean up before quitting |
| `stopPulse` | Channel closed to stop the status-dot animation goroutine |
| `onLogLine func(string, color.Color)` | Renders log lines through `UIManager.appendLogLine` |
| `sessionFailed atomic.Bool` | Set when any download or post-processing job in the session fails; turns the download button into "Retry" |
| `isRunning atomic.Bool` | Prevents close without confirmation while jobs are active |
| `showDebug atomic.Bool` | Shows yt-dlp `[debug]` lines in the log view; set from the "Debug Output" preference at startup and through `UIManager.onSetShowDebug` |

---

### 4.2 `UIWidgets` — widget bag  
*Defined in:* `types.go`

`UIWidgets` is a widget bag with no application logic. It groups widgets into three feature-scoped structs, each constructed by its own constructor and composed by `NewUIWidgets()`:

- **`DownloadControls`** — URL and save-path inputs, format/quality selectors, trim fields, batch/session toggles, progress/status widgets, action buttons, and the log view.
- **`PreferenceControls`** — maximum speed, theme mode, the Cookies choice (source, browser, profile, and cookies file path), save-preferences toggle, log-limit selector, the "Debug Output" and "Check for updates on startup" toggles, the "Embed in File" toggles (metadata, thumbnail, chapters), and the subtitle mode, languages, and "Include auto-generated" toggle.
- **`PostProcessControls`** — the post-processing master toggle, FFmpeg filter controls, upscale settings, and GPU backend selector.

Widgets are wired with callbacks in `UIManager.createUI()` and accessed through `ui.download`, `ui.prefs`, and `ui.postProcess`.

---

### 4.3 `UIManager` — main window and secondary window owner  
*Defined in:* `ui_manager.go`

Owns the primary window reference (`mainWindow`) plus the singleton secondary windows (About, Help, History, Preferences, Post-Processing, Components). Calling a `show*` method re-focuses an already-open window rather than opening a duplicate, via the shared `focusOrCreate`/`onWindowClosed` helpers. `UIManager` holds no direct service references — every service access is bridged through injected callbacks (`onLoadHistory`, `onCheckDependencies`, `onSavePreferences`, etc.), wired once in `newDownloaderApp`.

Beyond the five `show*` methods, `UIManager` also owns:
- **`createUI()`** — builds the main window layout, split into focused helpers (`buildHeader`, `configureEntryMode`, `wireToggleHandlers`, `wireActionButtons`, `buildInputCard`, `buildStatusCard`, `buildLogPane`, `buildFooter`).
- **`createMainMenu()`** — builds the menu bar.
- **`showLoadURLFile` / `loadURLList`, `pasteURLs`, `handleDrop`** (`url_input.go`) — the "Load from file…" button, the paste button, and the window's drop handler (`SetOnDropped`, set in `createUI`). All go through `addURLs`, which appends to the URL field without duplicates (`mergeURLs`) and switches on batch mode when the field then holds more than one URL. `parseURLList` skips blank lines, `#` comments (which `collectURLs` also skips in batch mode), and lines that are not http(s) URLs (`looksLikeURL`), and the log says what was skipped. Paste takes the clipboard only if every non-blank line is a URL. A dropped `.txt` is loaded as a list, and a `.url` or `.desktop` shortcut adds its `URL=` line (`shortcutURL`). Links dragged straight from a browser do not arrive on Windows, because GLFW accepts only dropped files there (`WM_DROPFILES`).
- **`savePreferences`, `resetPreferences`, `rebuildUI`** — preference persistence and full UI-rebuild-on-reset, used by `showPreferences`.
- **`checkDependencies`, `runUpdateInUI`** — thin delegates to the injected `onCheckDependencies`/`onUpdateYtDlp` callbacks for the startup tool check and the "Update yt-dlp" menu action. `runUpdateInUI` shows an error when `DownloaderApp.updateYtDlp` refuses; `runUpdateThen` sets up the status and calls `onRunUpdate`.
- **`appendLogLine` / `flushLog`** — batched log rendering. `appendLogLine` is safe to call from any goroutine: it queues the line under `logMu` and, for the first queued line, arms a `logFlushInterval` (100 ms) timer. `flushLog` then renders every queued line in a single `fyne.Do`: it adds the lines, trims once to `screenLogLimit` (the Log Buffer Limit from `onLogBufferLimit`, but never more than `maxScreenLogLines` = 5000, even for "Unlimited"), refreshes once, and scrolls to the bottom only if the view was already there (`isScrolledToBottom`), so a user who scrolled up is not pulled back down. `finishSessionUI` calls `flushLog` directly so the session summary appears at once, and `clearTerminalOutput` drops lines still queued (see §4.7).

`ui.go` is what remains outside `UIManager`: thin one-line `DownloaderApp` delegates to the `show*` methods above (`showHistory`, `showPostProcessing`, `showPreferences`, `showConfigHelp`), plus the shared `roundedCard`/`accentBar` container helpers `UIManager` uses when building widgets.

---

### 4.4 `DownloadEngine` — yt-dlp executor  
*Defined in:* `download_engine.go`

A stateless service that owns the resolved paths to `yt-dlp` and `ffmpeg`, and the JavaScript runtime yt-dlp uses for YouTube (`JSRuntime`, from `DependencyService.JSRuntime`; `BuildArgs` and `Probe` both pass it as `--js-runtimes name:path`, because both extract the video). It provides these methods:

- **`BuildArgs(DownloadRequest) DownloadArgs`** — pure function; assembles the yt-dlp command-line arguments from a request value struct. No I/O. The `-o` template comes from `outputTemplate` (`filename_template.go`): the request's `FilenameTemplate` (the Filename Template preference, in yt-dlp's syntax; `defaultFilenameTemplate`, `GoVid_%(title)s{quality}`, when empty or invalid) with `{quality}` replaced by the height label below for a capped download, then `_TRIM` for a trimmed one and `_<download ID>`, then `.%(ext)s`. A template may not hold `/` or ``, because `FinalizeFiles` only looks in the save folder. The format selector comes from `formatSelection(format, quality)`, which `Probe` shares; it also returns the height cap, which is empty for Best and for the audio formats. A capped video is labelled with the height actually downloaded, through the `heightLabel` template field `%(height&_{}p|)s` (nothing when the height is unknown). `embedArgs` adds `--embed-metadata`, `--embed-thumbnail --convert-thumbnails jpg` (except for WebM, which sets `DownloadArgs.ThumbnailSkipped` so `Run` can log why), and `--embed-chapters` from the request's `EmbedMetadata`/`EmbedThumbnail`/`EmbedChapters`. `subtitleArgs` adds `--write-subs [--write-auto-subs] --sub-langs <langs> --convert-subs srt` for every subtitle mode except Off, plus `--embed-subs` for Embed and Both. yt-dlp keeps the subtitle files after embedding when `--write-subs` is given, so Embed adds `--compat-options no-keep-subs` to delete them; `--write-subs` is needed because `--write-auto-subs` alone takes only auto-generated captions. Subtitles embedded in WebM stay WebVTT (`--convert-subs vtt`), the only format WebM holds, and audio formats skip subtitles (`DownloadArgs.SubtitlesSkipped`).
- **`Probe(ctx, DownloadRequest) (MediaInfo, error)`** — runs `yt-dlp -J --flat-playlist --no-warnings` with the download's `-f` selector and cookies, and without `--no-playlist` (`probe.go`). It returns `MediaInfo{Type, Title, Duration, Entries}`; `IsPlaylist()` is true for `_type == "playlist"`, and each `PlaylistEntry.DownloadURL()` is the video's URL. `--flat-playlist` lists a playlist's entries without extracting them, but a single video is still extracted in full. For a single video, `EstimatedSize()` adds up the `filesize` (or `filesize_approx`) of the requested formats, for the disk space check, and the `MediaInfo` keeps the JSON itself (`raw`) and when it was read (`probedAt`). `ProbeVideo` is the same probe with `--no-playlist`, for playlist entries and "Only this video" links. `isFresh(now)` is false once the answer is older than `probeMaxAge` (30 min), because the format URLs in it expire.
- **`Execute(ctx, args []string, opts DownloadOptions, ProcessCallbacks) (scanResult, error)`** — starts the process, streams stdout/stderr through its own private `watchOutput` method (defined in `logscanner.go`), and retries on transient errors with 1 s / 5 s / 30 s back-off when `opts.AutoRetry` is set.
- **`FinalizeFiles(savePath, downloadID string, onLog func(string, color.Color)) []string`** — finds the temp files written under `downloadID` (`filesWithID` lists the save folder; `filepath.Glob` would read brackets in its name as a pattern), strips the token, and renames each to its final conflict-free name via the private `uniquePath` helper. The rename goes through `renameWithRetry`, which retries for up to about 2 s as `removeWithRetry` does, because on Windows an antivirus scanner, the search indexer, or Explorer's thumbnails can briefly hold a new file open; `DownloadEngine.rename` (`os.Rename` when nil) is replaced in tests. A file that still cannot be renamed keeps its temporary name, and that path is returned, so history and post-processing get a file that exists. Reports rename events through `onLog` rather than touching the UI directly.
- **`RemovePartialFiles(savePath, downloadID string, onLog func(string, color.Color))`** — deletes every file written under `downloadID` after a failed or cancelled download (its `.part` and fragment files, and any media it finished first), retrying briefly while Windows still holds a lock, and logs each removal.
- **`RemoveLeftoverPartials(savePath, downloadID string, onLog func(string, color.Color))`** — the same, but deletes only what `isPartialFile` matches. A successful run calls it after `FinalizeFiles` to remove the partial files it left (an earlier run's format, say), and so keeps a finished file whose rename failed, which still carries the token.
- **`Run(ctx, req DownloadRequest, opts DownloadOptions, ProcessCallbacks) DownloadResult`** — composes the methods above into the full lifecycle of a single URL download: `FinalizeFiles` on success, `RemovePartialFiles` on failure or cancellation. When `req.InfoJSON` holds the probe's JSON, `saveInfoJSON` writes it to a temporary `govid-*.info.json` (in the temp folder, so `FinalizeFiles` cannot pick it up), `BuildArgs` passes `--load-info-json <file>` instead of the URL, and the file is removed when `Run` returns. yt-dlp runs format selection again on the loaded info, so `-f`, cookies, `--download-sections`, and the embed flags all still apply, and the video is extracted once per download instead of twice. If that run fails with HTTP 403 or 410 (`scanResult.hadExpiredLinkErr`: the format URLs expired or were issued to another IP address), `Run` repeats it once from the URL. A subtitle that cannot be downloaded (YouTube often answers 429) fails the whole yt-dlp run, so when `scanResult.hadSubtitleErr` is set `Run` repeats it once without subtitles and says so. `splitSubtitleFiles` moves the subtitle files `FinalizeFiles` renamed into `DownloadResult.SubtitlePaths`, so `FinalPaths`, which post-processing and history use, holds only media. Reads no UI state; `DownloaderApp.runYtDlp` builds the `DownloadRequest` and `DownloadOptions` from widget values, calls `Run`, then handles history recording and the UI completion report from the returned `DownloadResult{FinalPaths, Extension, Scan, Err}`.

**Stopped, keep the output.** A download whose context is cancelled with the cause `errStopKeep` (`context.WithCancelCause`) is stopped but kept: `runArgs` skips `RemovePartialFiles`, runs `FinalizeFiles`, and returns `DownloadResult.Stopped` with `Err` nil, so history and post-processing treat it as finished. A live recording (`req.Live`) that ends with an error after writing something is kept the same way. For a kept live recording, `finishRecording` (`live.go`) then remuxes what yt-dlp left into the chosen container with ffmpeg, without re-encoding (`recordingRemuxArgs`; MP3 is re-encoded). yt-dlp writes live HLS as MPEG-TS (`--hls-use-mpegts`), which stays readable when the process tree is killed, but under the target's extension; it fixes that itself (`FixupM3u8`) only when a stream ends normally. A recording made `--live-from-start` can leave a video and an audio file, which are merged. A container that cannot hold the codecs (WebM for H.264) falls back to MKV, and a single file ffmpeg cannot remux at all is kept as `.ts`. While a live or scheduled stream runs, `monitorRecording` reports through the optional `ProcessCallbacks.OnRecording(started, elapsed, size)` once a second, measuring the files under the download ID. `liveArgs` adds `--hls-use-mpegts`, `--live-from-start`, and `--wait-for-video 60-300` from `req.Live`, `LiveFromStart`, and `WaitForVideo`. The probe passes `--ignore-no-formats-error`, because a scheduled stream has no formats yet and yt-dlp would otherwise fail instead of reporting its `live_status` and `release_timestamp` (`MediaInfo.LiveStatus`, `ReleaseTimestamp`).

**Formats** (`formats.go`, `formats_window.go`). `MediaInfo.Formats` holds every format of the probe's JSON (`FormatInfo`: codecs, size, dynamic range, …), and `RequestedFormats` the ones the selector chose. `formatArgs` gives the probe and the download the same format options: `-f` with the user's pick (`DownloadRequest.FormatPick`, e.g. `247+251`) or the Format / Max Quality selector, plus, without a pick, `-S vcodec:avc|vp9|av01` for the Preferred Video Codec (`codecSort`; a codec named in `-S` ranks before resolution). The container still follows Format. A pick made with **Formats…** next to the URL field is kept per URL in `DownloaderApp.formatPicks` (used by `checkURLs`' probe, then moved onto the queued item); one made on a waiting Queue row is set with `QueueModel.SetFormatPick`, and it is saved in `queue.json`. Before each download `downloadItem` logs "Will download 401+251: 2160p AV1 + Opus → MP4 (~232.5 MiB)" (`selectedIDs`, `describeDownload`), which names the same IDs as yt-dlp's own "Downloading 1 format(s)" line; the Max Quality notice is skipped for a pick. `formatRows` lists the formats in yt-dlp's order, without storyboards; a codec yt-dlp does not know counts as present, as `yt-dlp -F` shows it.

**Cookies** (`cookies.go`). The Cookies preference is None, From file, or From browser (`cookieSourceOptions`). `newDownloadRequest` turns it into `DownloadRequest.CookiesPath` or `CookiesFromBrowser` (`firefox`, or `firefox:<profile>`), and `cookieArgs` gives both `BuildArgs` and the probe `--cookies-from-browser` or, for a file that exists, `--cookies`. `scanResult.accessProblem` holds the first `ERROR:` line `classifyAccessError` recognises: the cookie errors recorded from the bundled yt-dlp on Windows (a running Chromium browser's locked database, "Could not copy Chrome cookie database"; app-bound encryption, "Failed to decrypt with DPAPI"; a missing Firefox profile), and the errors cookies fix (YouTube's bot check, age-restricted, members-only, and private videos). After a failure, `failureHints` gives `reportDownloadResult` the matching `accessHint`, which names the setting to change and takes the cookies already passed into account, or else the "update yt-dlp" hint for a site change. The session configuration logs `cookieLabel` ("Firefox", "file set"), never the path.

`DownloadOptions{AutoRetry bool; Index, Total int}` bundles the retry policy and this URL's 1-based position within a batch (both 1 for single downloads) — the three runtime options shared by `Execute` and `Run`.

The private `watchOutput(stdout, stderr, cb) scanResult` and `parseProgress(line, cb)` methods own all output-scanning; they hold no UI state and report every line and progress tick through `ProcessCallbacks`.

`scanResult` records the source extensions seen in `[download] Destination:` lines, whether yt-dlp converted or merged the media (a `[Merger]`/`[VideoConvertor]` line on either stream; real yt-dlp prints them to stdout), whether a retryable network/rate-limit error was observed, and whether an `ERROR:` line looked like a site change (`hadExtractorErr`: "Unable to extract", "Sign in to confirm", "HTTP Error 403"). The final result retains this metadata for the completion summary, the retry decision, and the "update yt-dlp" hint `reportDownloadResult` logs after such a failure.

`ProcessCallbacks` is a bridge struct: it carries closures (`OnLog`, `OnStatus`, `OnProgress`, `OnPhase`) that let the engine report progress back to the UI without importing Fyne. `OnProgress(pct float64, size string)` is called for each parsed percentage; `size` is the last reported downloaded-size token, or empty when the line had none. `OnPhase(phase string)` is called when yt-dlp starts a post-download ffmpeg step (`detectPhase`: `[Merger]` → `phaseMerging`; `[VideoConvertor]`/`[ExtractAudio]` → `phaseConverting`) on either stream; `DownloaderApp.showDownloadPhase` then holds the progress bar at 95% and shows "Merging…"/"Converting…". `DownloaderApp.runYtDlp()` is the only caller.

**Division of responsibility with `download.go`:** `DownloadEngine` is UI-agnostic — it never reads widget state and reports everything through `ProcessCallbacks`. `download.go` is the layer that still needs the UI/app context: `startDownload()` owns the batch/session lifecycle (validating widget input, the queue `context.Context` for cancel/skip across multiple URLs, opening/closing the session log, running post-processing over the whole batch), and `runYtDlp()` is the per-URL adapter — it translates widget state into a `DownloadRequest`/`DownloadOptions`, calls `engine.Run`, then handles app-specific side effects the engine has no business knowing about (history recording, the "DOWNLOAD COMPLETE/ABORTED" log block, status indicator updates, OS notifications).

---

### 4.5 `PPEngine` — FFmpeg post-processing engine  
*Defined in:* `pp_engine.go`

Owns the resolved paths to `ffmpeg` and `ffprobe`, plus the GPU acceleration state needed for the final encode step: `GPUBackend` (the resolved backend selection), `GPUCapabilities` (the `map[GPUBackend]BackendCapability` copied from `GPUCapabilityService.Detect()`), and `gpuSem` (a semaphore with capacity `maxConcurrentGPUJobs = 2` — see §4.11). Construct it with `NewPPEngine(ffmpegPath, ffprobePath)`. Exposes one public method:

- **`ApplyFilters(ctx, filePaths, vfFilters, afFilters, PPCallbacks)`** — builds one `PostProcessJob` per file, then runs them concurrently through a worker pool bounded to `runtime.NumCPU()` goroutines. Each worker calls the private `runJob()`.

Internal flow per file:
1. `resolveAutoCrop` — replaces the `__autocrop__` sentinel by running a 60-second `cropdetect` pass via `detectCropFilter`.
2. `resolveToneMap` — replaces the `__tonemap__` sentinel with `toneMapFilter(transfer)` for the file's HDR transfer (PQ or HLG), or drops it for SDR files. `probeColorInfo` reads the colour tags with ffprobe, falling back to parsing ffmpeg's input summary because ffprobe is not bundled; a BT.2020 file with no transfer tag is taken to be PQ. The chain states the input transfer, matrix, and primaries explicitly, and `buildFFmpegArgsForBackend` tags tone-mapped output as BT.709.
3. `runJob` — runs the main FFmpeg encode. Streams stderr in real-time. On success, renames the `_pp` temp file over the original. On failure, deletes the temp file (CPU jobs) or retries once on the CPU (GPU jobs — see below).

Private probe methods (`probeFrameCount`, `probeDuration`, `computeOutputFrameCount`, `parseRationalFPS`) use `engine.FFprobePath` to measure frame counts and durations for progress reporting. Private argument builders `buildFFmpegArgs`/`buildFFmpegArgsForBackend` assemble the FFmpeg command-line for each job, resolving the video encoder via the package-level `PlanEncoder(requested, capabilities, containerExt) EncoderPlan` function (§4.11), with the per-job thread count passed in once `ApplyFilters` has sized the worker pool. When the job's `streamLayout` is known (`probeStreamLayout` parses `ffmpeg -i`'s summary), streams are mapped explicitly: the main video stream first (filtered with `-filter:v:0` and encoded with the plan's `-c:v` scoped to `-c:v:0` by `encodeFirstVideoOnly`), then every cover (`(attached pic)`) stream-copied with `-disposition attached_pic`, plus `0:a?`, `0:s?`, `0:t?`, `-map_metadata 0`, and `-map_chapters 0`. Without video filters, `0:V?` copies the non-cover video. ffmpeg would write a mapped cover back into Matroska as an ordinary video track, so for `.mkv`/`.mka` outputs `extractCovers` saves each cover to a temporary image and `-attach` re-adds it with its file name and MIME type; `ApplyFilters` removes those files once all jobs have run. When the layout cannot be read, the job falls back to ffmpeg's default stream selection.

**GPU job lifecycle:** `gpuSem` (buffered channel, capacity `maxConcurrentGPUJobs = 2`) caps how many GPU-encoded jobs run concurrently, since hardware encoders like NVENC enforce a low concurrent session limit. `runJob` wraps each job's GPU-only bookkeeping in a `gpuJobGuard` (`newGPUJobGuard`, `arm`, `pet`, `release`): it acquires a `gpuSem` slot up front (no-op for CPU jobs), arms a `gpuStallTimeout` (30 s) watchdog that is `pet()` on every stderr line, and `release()`s the slot/watchdog exactly once regardless of exit path. If a GPU-encoded job fails — `cmd.Start()` error or a non-zero exit — `retryWithCPU` rebuilds the job's args with `BackendOff` and re-runs `runJob` once, so a driver hiccup or hung encoder falls back to the CPU baseline instead of failing the file outright. A job stopped by a cancel is neither retried nor failed: when `ctx` is done, `runJob` hands it to `cancelJob`, which removes `tmpOutput` and logs the file as canceled without calling `OnFailure`, and the `ApplyFilters` workers start no further jobs.

`DownloaderApp.applyFFmpegFilters()` in `postprocess.go` is the thin wrapper that constructs `PPEngine` and wires `PPCallbacks` back to UI helpers; it also sets `engine.GPUBackend` to the session's `gpuBackend` (the Post-Processing dialog's selector, read by `readSessionSettings` as the session starts) and `engine.GPUCapabilities` from `app.gpuSvc.Detect(ctx)` before calling `ApplyFilters`.

---

### 4.6 `PreferenceService` — preference persistence  
*Defined in:* `preference_service.go`

Every Fyne preference storage key is a named constant here (`prefSavedPath`, `prefFormat`, …). Default values are separate named constants (`defaultThemeMode`, `defaultSmoothFPS`, …) in the same file; `Load()` applies those defaults when a stored value is absent.

- **`Load() AppPreferences`** — returns the preferences last saved this session (`saved`, guarded by `mu`, since the session goroutine also calls it) or, before any save, reads the Fyne store; either way `normalizePreferences` returns a fully-defaulted plain struct. Called once at startup and again each time a secondary window refreshes its controls.
- **`Save(AppPreferences)`** — keeps the struct as `saved` and writes it back. Honours the `savePrefs` gate: if the user has disabled persistence, only the toggle itself is written, but the saved settings still apply until GoVid quits, so reopening a settings window does not revert them to the stale store.
- **`Reset()`** — forgets `saved` and removes all managed keys so the next `Load` returns defaults.
- **`LoadFromFile(path string) (*AppConfig, error)`** — reads and parses a settings file (`parseAppConfig`, in `config_file.go`). `AppConfig` has one pointer field per `AppPreferences` field, with the same name (the JSON keys are listed in the README), so a key left out of the file is `nil` and leaves the setting alone. `TestEveryPreferenceHasAConfigKey` checks the two structs with reflection, so a new preference without a config key fails the build's tests.
- **`MergeConfig(cfg, base) (AppPreferences, []string)`** — walks `AppConfig`'s fields with reflection and copies each set value onto the `AppPreferences` field of the same name, unless `configRules` rejects it: choices must be one of the lists in `options.go` (`GPUBackendOptions()` for the GPU backend), `smoothFPS` and `sharpenAmount` must be within their sliders' ranges, `path` must be an existing folder, and `cookiesPath` an existing file or `""`; `cookieSource` and `cookieBrowser` must be one of `cookieSourceOptions` and `cookieBrowserOptions`. Settings saved before the Cookies choice existed have no `cookieSource`; `resolveDefaults` makes it From file when a cookies file is set, so they keep working. For a choice (and `path`), `""` leaves the setting unchanged. It returns every rejected value as a message, so all problems are reported at once.
- **`ExportConfig(AppPreferences) AppConfig`** and **`WriteConfigFile(path, AppConfig) error`** — turn preferences into a complete `AppConfig`, every key set, and write it as indented JSON (atomically, through `writeFileAtomic`). Tools → Export settings… uses them; Tools → Import settings… and the Preferences window's **Load from Config** go through `UIManager.importConfig` → `MergeConfig` → `applyAndSavePreferences` (widgets, runtime preferences, store, theme).
- **Presets** (`presets.go`, `preset_ui.go`). A `Preset` is a name plus a partial `AppConfig`. They are stored as JSON under the `presets` key (`LoadPresets` / `SavePresets`), saved even when "Save preferences" is off, and kept by Restore Defaults. Until some are stored, `LoadPresets` returns `starterPresets()`. `UIManager.applyPreset` runs `ValidateConfig` and `applyConfig` (the halves of `MergeConfig`) onto the current widget values, so settings the preset does not hold are untouched, then applies and saves the result and rebuilds the window only if the theme changed. `refreshPresetState`, called from `savePreferences` and the Format/Quality handlers, shows "(modified)" when `configMatches` finds that a setting the preset holds has changed. "Save current as preset…" keeps the ticked `presetGroups` of `configFromPreferences(current)` (`configFields`). `ReadPresetFile` validates each preset like `govid.json` and drops invalid values, so presets exported on one machine import on another.


**Portable Mode** (`portable.go`). `newDownloaderApp` calls `chooseSettingsStore` before reading any setting: with a `GoVid.portable` marker beside the executable (`executableDir`) and a folder it can write to, the store is a `fileStore`, a `fyne.Preferences` kept in `settings.json` there, saved atomically `fileStoreSaveDelay` (300 ms) after a burst of changes and flushed on quit (`flushSettings`); otherwise it is the Fyne store, which is then the only one touched. A marker in a folder GoVid cannot write to falls back to the Fyne store with a startup warning (`settingsNote`). `PreferenceService` and `ReleaseService` take whichever store it chose. Preferences → **Portable Mode** acts at once (`onPortableChanged` → `setPortable`): `copySettings` writes every preference in use (`prefSvc.Load()`, through `PreferenceService.write`, which ignores "Save preferences") and the presets into the other store, the marker is created or deleted, and GoVid offers to restart (`restart`: `startDetached` and `Shutdown`).

`AppPreferences` is a plain value struct with no widget references. `applyPreferencesToWidgets(AppPreferences)` in `helpers.go` is the single translator from struct → widget state. `UIManager.savePreferences(path)` reads widget state and delegates to `prefSvc.Save`; `DownloaderApp.savePreferences` in `preference_service.go` is a one-line delegate to it. `UIManager.resetPreferences()` (data + log-buffer reset) and `UIManager.rebuildUI()` (dark theme + `createUI`) together handle a full application reset; separating them lets callers invoke only what they need.

---

### 4.7 `LogService` — file logging
*Defined in:* `log_service.go`

Owns the session log file handle, two mutexes, daily rotation policy, the UI buffer-limit value, and a pre-session line buffer. Session and error files use `GoVid_log_YYYY-MM-DD.txt` and `GoVid_errors_YYYY-MM-DD.txt`; old daily files are retained rather than automatically deleted. `DownloaderApp` holds `logSvc *LogService`.

- **`OpenSessionLog(dir string) (string, error)`** — opens (or appends to) the daily `GoVid_log_YYYY-MM-DD.txt` in `dir`. Returns the resolved path.
- **`CloseSessionLog()`** — writes a closing marker and closes the file.
- **`WriteToFile(line string)`** — appends a timestamped line to the open session log.
- **`WriteToErrorLog(line string)`** — appends a timestamped line to the daily `GoVid_errors_YYYY-MM-DD.txt`. Uses the session directory cached by `OpenSessionLog`; falls back to the executable directory when no session is active. Opens and closes the file on each call.
- **`SetBufferLimit(n int)` / `BufferLimit() int`** — gets/sets the UI log line cap (replaces the former `logBufferLimit` global).
- **`Recent(n)`** — the latest lines written with `WriteToFile` (up to `recentLogLines` = 200, kept whether or not a session log is open), for Copy diagnostics.
- **`EndStartup()`** — stops the `preSession` buffering described below. `DownloaderApp.openSessionLog` calls it as every session starts, with or without a log file.
- Until `EndStartup`, `WriteToFile` keeps timestamped lines written while no session log is open in the `preSession` buffer, capped at `preSessionMaxLines` (500) whatever the Log Buffer Limit is. `OpenSessionLog` flushes those lines into the newly opened file, preserving startup diagnostics such as dependency warnings and GPU detection output. After `EndStartup`, lines written while no session log is open are not kept, so a session log holds the startup lines and its own lines, not the output of earlier sessions run without a log file.
- **`WriteSessionConfig(cfg SessionConfig, writeFn func(string, color.Color))`** — writes the session's starting configuration (save path, format/quality, trim, toggles, preferences, URL list, post-process settings) as one log line per setting via `writeFn`. Driven entirely by `SessionConfig`, a plain value struct with no widget references, built by `newSessionConfig(ui *UIWidgets, urls []string, savePath, trimStart, trimEnd string) SessionConfig` — it embeds the existing `PostProcessSettings` (§4.5) for its post-process fields rather than duplicating them.

Package-level helpers: `IsErrorLine(line string) bool` (matches ERROR/FAILED), `ParseBufferLimit(s string) int` (converts the preference string to an integer), `SessionLogPath(dir string)`, `ErrorLogPath(dir string)`.

**Diagnostics** (`diagnostics.go`). Help → **Copy diagnostics** builds `diagnosticsReport` off the UI thread: GoVid's version and build type, the OS, each tool's version and source (`DependencyService.Installed`) and the JavaScript runtime, `FormatGPUDiagnostics`, every setting as `govid.json` holds it with cookies shown only as `cookieLabel` (`diagnosticsSettings`), the queue's summary, the goroutines and tool processes running (`trackTool` counts yt-dlp downloads and probes, ffmpeg jobs and remuxes), and `Recent(200)`. An `anonymizer` then replaces the user profile folder (with either slash, and with doubled backslashes as yt-dlp prints its command line) with `%USERPROFILE%`, the user name, as a whole word, with `%USERNAME%`, and the cookies file's path with `<cookies file>`. The report goes to the clipboard, with **Save as file…**. With Debug Output on, `setDebug` starts the heartbeat, which every `heartbeatInterval` (10 s) writes the round trip of a function sent through `fyne.Do` (`doOnUI`; `runOnUI` replaces it in tests), the goroutine count, the tools running, and the log lines waiting to be shown (`UIManager.pendingLogLines`), and logs when the UI thread has not answered for an interval; it also turns on `markLoop` (see §7).

`appendOutput()` in `helpers.go` is the single call-site for all log writes; it calls `logSvc.WriteToFile` for session logging and `logSvc.WriteToErrorLog` for error mirroring. It passes yt-dlp's `[debug]` lines (`IsDebugLine`) to the log view only when `DownloaderApp.showDebug` is set (the "Debug Output" preference, `prefShowDebug`); the log file always gets every line. `UIManager.renderLogLines` also shows consecutive yt-dlp progress lines (`IsProgressLine` in `logscanner.go`) as one line updated in place, so the view holds a few dozen lines per download instead of hundreds.

---

### 4.8 `HistoryService` — download history persistence
*Defined in:* `history_service.go`

Owns the path to `download_history.json` (beside the executable) and exposes three methods:

- **`Load() ([]DownloadHistoryEntry, error)`** — reads all entries in chronological order. Returns nil with no error when the file does not yet exist.
- **`AppendAll(rec DownloadRecord)`** — under `mu` (downloads may finish at once), builds one `DownloadHistoryEntry` per path in `rec.FinalPaths` and writes the updated array in a single write. When `rec.FinalPaths` is empty a placeholder entry is appended so the URL is still recorded.
- **`Clear() error`** — overwrites the file with an empty JSON array.

The private `buildEntries` helper and `inferOriginalTitle` live here; neither has a UI dependency. `buildEntries` uses `rec.Title` (the probe's or the playlist's title) and falls back to `inferOriginalTitle` only when it is empty. `findDownloaded(entries, url, videoID, extractor)` returns the newest entry for the same video: one with the same video ID and extractor key, so `youtu.be/x` and `watch?v=x&t=1` match, or, for entries recorded before IDs were kept, the identical URL. `DownloaderApp` holds `historySvc *HistoryService`; `UIManager` uses injected `onLoadHistory` and `onClearHistory` callbacks so `showHistory` never touches the file path directly.

`DownloadHistoryEntry` is a plain JSON-serialisable value struct (url, originalTitle, finalFilename, savedPath, format, quality, downloadedAt, postProcessed, and the optional videoId and extractor), with `FilePath`, `DisplayTitle`, and `DisplayFile` helpers.
`DownloadRecord` is the plain input value passed to `AppendAll`: URL, final paths, save path, format, quality, post-processing state (whether the session runs any post-processing filter, `session.hasPostProcess()`), and the title, video ID, and extractor the queue item carries (`queueItem.withInfo` takes them from the probe; playlist entries start with the playlist's title, ID, and `ie_key`).

**Keep download history.** The `KeepHistory` preference (on by default) is mirrored in `DownloaderApp.keepHistory` (an `atomic.Bool`, set at startup and by `UIManager.applyRuntimePrefs`). When it is off, `recordHistory` writes nothing and `skipDownloaded` does not check. Unticking it in Preferences (`onKeepHistoryChanged`) offers to delete the history kept so far.

**Repeat downloads** (`duplicates.go`). After `checkURLs`, `runSession` calls `skipDownloaded`, which reads the history once and, for each queued item that `findDownloaded` matches, asks through `askDuplicate`: "Already downloaded on <date> as <file>" with Download again / Skip, plus "Skip all duplicates" in a batch. Skipped items are logged and dropped from the queue.

**History window** (`history_window.go`). `showHistory` opens the window at once (`openHistory`) with a `historyPanel` in its loading state: "Loading the download history…" over the empty list, the search and Clear History disabled. `loadHistory` then reads the history and checks every entry's file off the UI thread, since a file on a disconnected drive or network share can take seconds to answer, and fills the panel through `fyne.DoAndWait` unless the window was closed meanwhile; if the history cannot be read, the panel says why and enables Clear History. The panel shows a `widget.List` of `historyRow`s, newest first, built from a `historyView` (the entries, which of their files no longer exist, and the search result). Each row has the title, a details line (date, format/quality, file name), and **Re-add** (`readdHistoryURL` → `addURLs`, switching on batch mode when the field already has a URL), **Show in folder** (`revealFileCommand`: `explorer /select,"<file>"` on Windows, `open -R` on macOS, the folder on Linux), and **Copy URL**. Rows whose file is gone are greyed out (`LowImportance`) with Show in folder disabled. The search field filters on title, URL, file name, and format.


---

### 4.9 `DependencyService` — binary discovery and updater
*Defined in:* `dependency_service.go`

Owns the `binDir` path (resolved once at construction from the executable location) and exposes:

- **`LocalPath(toolName string) string`** — returns the path to `toolName` inside `binDir`, appending `.exe` on Windows.
- **`Resolve(toolName string) string`** — returns the bundled path when it exists on disk, otherwise the bare name for system PATH lookup. Called by `runYtDlp` and `applyFFmpegFilters` when constructing `DownloadEngine` and `PPEngine`.
- **`Check(onWarning func(msg string))`** — verifies `yt-dlp` and `ffmpeg` are reachable; calls `onWarning` for each missing tool. Called at startup via `UIManager.checkDependencies()`, a thin delegate to the injected `onCheckDependencies` callback.
- **`RunUpdate(cb UpdateCallbacks)`** — runs `yt-dlp -U` in a background goroutine and reports lines/success/failure through `UpdateCallbacks`. Called via `UIManager.runUpdateThen`, wired to the injected `onRunUpdate` callback. Every entry point (Tools → Update yt-dlp, the out-of-date notice, and Components) goes through `DownloaderApp.updateYtDlp`, which refuses with `errToolsInUse` while a session runs and `errAlreadyInstall` while `installing` is set, and otherwise holds `installing` with the Download button disabled until the update has finished, since Windows cannot replace a running `yt-dlp.exe`. When the update fails and the folder holding yt-dlp cannot be written to (`updateFailureHint`, using `dirWritable` or the test-injected `isWritable`), it explains that and how to fix it; `UpdateCLI` adds the same hint to its error.
- **`Version(toolName string) (string, error)`** — runs `<tool> --version`; used by the yt-dlp update check (§4.12).
- **`Installed(toolName string) InstalledTool`** — where the tool was found (`bin/`, which takes precedence, or `PATH`) and the version it reports (`ffmpeg -version`, `<tool> --version`, parsed by `parseToolVersion`); used by the Components window.
- **`JSRuntime() (JSRuntime, bool)`** — the JavaScript runtime yt-dlp needs to solve YouTube's player challenges (without one it falls back to a deprecated client that may miss formats). It looks for `bin/deno` first, then `deno`, `node`, and `bun` on `PATH`, runs each with `--version`, and takes the first whose version yt-dlp supports (`jsRuntimeRules`, from yt-dlp's EJS wiki: Deno 2.3.0+, Node 22.0.0+, Bun 1.2.11–1.3.14). The search is cached; `ResetJSRuntime` forgets it after Deno is installed, and `JSRuntimeNotes` says why found runtimes were skipped. `JSRuntime.Arg()` is the `--js-runtimes name:path` value; the path is always given, because `bin/` is not on `PATH`.

`UpdateCallbacks` is a bridge struct (`OnLog`, `OnStatus`, `OnSuccess`, `OnFailure`) with no Fyne dependency, following the same pattern as `PPCallbacks` and `ProcessCallbacks`.

`UpdateCLI()` is used by the `--update` CLI flag in `main()`. It updates the same resolved yt-dlp binary as `RunUpdate`, synchronously, with output to stdout.

**Installing tools** (`tool_installer.go`, `components.go`, `components_window.go`). `ToolInstaller` installs the tools in `installableTools` into `bin/`. Each `toolSpec` names the files it puts there and how to find its newest `toolRelease`: yt-dlp's `yt-dlp.exe` and `SHA2-256SUMS` (read by `parseSHA256Sums`) and Deno's `deno-x86_64-pc-windows-msvc.zip` and its `.sha256sum` (PowerShell `Get-FileHash` output, `parseGetFileHash`) from their latest GitHub releases through `ReleaseService` — or, when GitHub's API rate-limits, from `/releases/latest/download/`, with the version taken from where `/releases/latest` redirects — and FFmpeg's essentials ZIP from gyan.dev, whose `release-version` names the version, so the ZIP and its bare-hash `.sha256` (`parseBareHash`) both come from that version's `packages/` folder. `Install` checks that `bin/` is writable, downloads the checksum and then the file into a temporary folder (through `httpFetcher`, hashing as it streams, with progress), refuses a mismatch (`errChecksumMismatch`), writes each file as `<file>.new` (`extractNamed` finds `ffmpeg.exe`/`ffprobe.exe` in the ZIP's versioned `bin/` folder), and `swap`s them in: the old file to `.old`, `.new` into place, then the `.old` files deleted. A failed rename puts every old file back (`restoreSwapped`). `removeOldTools` deletes leftovers at startup.

`DownloaderApp.installComponent` (the Components window's buttons and the notices' **Install**) refuses while a session or another install runs, disables the Download button, shows progress in the status label, and calls `afterInstall`: a new Deno resets the runtime search, a new FFmpeg resets `GPUCapabilityService` (`Reset`) and re-runs detection and `checkFFmpegFilters` (`ffmpeg -filters` must list `zscale` and `tonemap`, which HDR to SDR needs). yt-dlp's **Update** runs `yt-dlp -U` through `updateYtDlp` (see `RunUpdate` above), with the same guard; its **Reinstall** downloads a fresh `yt-dlp.exe`. `componentStatuses` finds each tool's installed and latest version concurrently; `componentStatus.action` picks Install (missing, or only on `PATH`), Update (older than the latest), or Reinstall. Where the downloads do not apply (`canInstallTools`: not Windows), the window only lists versions and says to use the package manager.

At startup `checkTools` shows a notice with **Install** for a missing yt-dlp or FFmpeg and for a missing runtime, and writes the runtime it found to the log file. A download whose output has yt-dlp's "No supported JavaScript runtime" warning (`scanResult.hadNoJSRuntime`) shows the runtime notice too. The runtime appears in About and, as `SessionConfig.JSRuntime`, in the session configuration, which `writeSessionConfig` writes from the session goroutine because finding the runtime may run it.

---

### 4.10 `darkTheme` / `lightTheme`  
*Defined in:* `theme.go`

`darkTheme`, `lightTheme`, and `systemTheme` implement `fyne.Theme`. `systemTheme`, the default for new installs ("System"), passes each `Color(name, variant)` call to the light or dark theme by the variant Fyne passes in, which on Windows follows the "apps use light theme" setting; its sizes, fonts, and icons follow the current variant (`systemVariant`). `resolveThemeMode` turns "System" into the variant showing, for `themedIcon`. `UIManager.followSystemTheme` listens to Fyne's settings and rebuilds the main window when the variant changes while the theme is System, because the window's own colours and icons are chosen when it is built. The active theme is stored as a preference and applied at startup before the window is created; saved Dark and Light choices are kept.

**Shortcuts** (`shortcuts.go`). The main window's shortcuts are `desktop.CustomShortcut`s with `KeyModifierShortcutDefault`, set on File, Tools, and Help menu items, which Fyne runs before the focused widget sees the keys (so Ctrl+Enter works in the URL field), and added to the canvas (`registerShortcuts`). Keys without a modifier are never shortcuts to Fyne and go to the focused widget, or to the canvas's typed-key handler when nothing has the focus: F1 opens the guide from there, and `closeOnEscape` makes Esc close About, the guide, Preferences, Post-Processing, History, and Components.

---

### 4.11 `GPUCapabilityService` — GPU backend capability detection
*Defined in:* `gpu_capability.go`; see `docs/gpu-acceleration.md` for the design behind it.

Detects, once per app run, which GPU acceleration backends the bundled ffmpeg binary can actually use for final-encode acceleration. Holds `ffmpegPath` and a mutex-guarded cache keyed by `GPUBackend` (`auto`/`off`/`nvidia`/`intel`/`amd`/`vaapi`/`videotoolbox`).

- **`Detect(ctx context.Context) map[GPUBackend]BackendCapability`** — on first call, runs `ffmpeg -encoders` once and, per backend applicable to the current `runtime.GOOS`, checks whether its H.264 encoder is compiled in (`isEncoderCompiled`) and then probes it with a short synthetic encode (`probeEncoder`). Caches the result; later calls return the cached copy. Started in the background via `startGPUDetection()` in `helpers.go`, called from `main()` alongside `uiManager.checkDependencies()`.
- **`Capability(backend GPUBackend) (BackendCapability, bool)`** — reads a single cached backend result.

`BackendCapability` records `Applicable`, `Compiled`, `Available`, the target `Encoder`, and a `Reason` string for diagnostics.

**Encoder resolution:** the package-level `PlanEncoder(requested GPUBackend, capabilities map[GPUBackend]BackendCapability, containerExt string) EncoderPlan` function (not a service method) always resolves to a runnable `-c:v` argument set. It resolves `BackendAuto` to the highest-priority `Available` backend (`backendPriority`: NVIDIA → Intel → AMD → VAAPI → VideoToolbox), falls back to the existing CPU encoder (`libx264` CRF 18, or `libvpx-vp9` CRF 31 for WebM — WebM always stays on CPU regardless of the requested backend) when the resolved backend is unavailable or `containerExt` is `.webm`, and otherwise returns the backend's constant-quality GPU args (e.g. `h264_nvenc -rc constqp -qp 19`). `EncoderPlan{Args, Label, UsedGPU, Backend}` carries the result; `Label` is a human-readable string used in job summaries and logs. `PPEngine.buildFFmpegArgsForBackend` calls `PlanEncoder` when building each job's FFmpeg command line (§4.5).

**UI and preference wiring:** `GPUBackendOptions() []string` returns the backend labels applicable to the current OS, in priority order, for the Post-Processing dialog's "Encoder Backend" `*widget.Select` (`UIWidgets.postProcess.gpuBackend`); `GPUBackendFromLabel(label string) GPUBackend` maps a selected label back to its identifier. The selection persists via `PreferenceService`'s `GPUBackend` field/`prefGPUBackend` key. `FormatGPUDiagnostics(capabilities) []string` renders one availability line per applicable backend for the startup session log and the About window's GPU Acceleration section.

---

### 4.12 `ReleaseService` — update checks
*Defined in:* `release_service.go`; used by `update_check.go`

Looks up the latest release of a GitHub repository through `GET /repos/<owner>/<repo>/releases/latest`, with a 5 s timeout and a `GoVid/<version>` User-Agent. It has no UI dependency.

- **`Latest(ctx, owner, repo string, maxAge time.Duration) (Release, error)`** — returns `Release{TagName, HTMLURL, Body, Assets}`. An answer younger than `maxAge` is served from the cache (`releaseCheckInterval` = 24 h for the startup check; `0` always asks GitHub). The cache is a JSON entry with the check time, kept in the Fyne preferences store under `latestRelease:<owner>/<repo>`; the store is injected as the two-method `releaseCache` interface. HTTP 403/429 (the unauthenticated rate limit) returns `errReleaseUnknown` and is cached too, so a rate-limited check is not repeated on every start; other failures are returned and not cached.
- **`compareVersions(a, b)` / `isOlderVersion(installed, latest)`** — numeric, part-by-part ordering of version strings, ignoring a leading `v` and a `stable@` channel prefix. It handles yt-dlp's date versions (`2025.09.26`, nightly `2025.09.26.232302`) and GoVid's own release tags.

`update_check.go` holds the `DownloaderApp` side. `startUpdateChecks(enabled)` runs `checkYtDlpUpdate` in the background after `checkDependencies` at startup, when the "Check for updates on startup" preference (`prefCheckUpdates`, on by default) is set. When the installed yt-dlp (`DependencyService.Version`) is older than the latest release, it logs one line and calls `UIManager.showNotice` with an "Update now" button wired to `runUpdateInUI`. A check that cannot complete is written to the log file only. `ytDlpVersions()` returns the installed and latest versions (or "unknown") for the Update yt-dlp confirmation dialog and the About window, which fetch them off the UI thread.

**GoVid's own updates:** `checkGoVidUpdate` runs after the yt-dlp check, against `DunderGG/govid`, with the same daily cache and preference. `isNewerRelease(version, tag)` compares the build's `main.version` with the release tag using `compareVersions`, not semver, because GoVid's tags are dates such as `2026.09.17`. A `dev` build never prompts and does not ask GitHub. A newer release logs one line and shows a notice whose "What's new" button opens `UIManager.showGoVidRelease` (`release_dialog.go`): the release notes rendered from Markdown, plus an "Open download page" button that calls `fyne.CurrentApp().OpenURL(html_url)`. Tools → "Check for GoVid updates" (`checkForGoVidUpdates` → `DownloaderApp.checkGoVidRelease`) always asks GitHub (`maxAge` 0) and reports a newer release, "up to date", a development build, or a rate limit. Updating in place (download, verify, swap the running `.exe`) is not implemented. `main.version` comes from the git tag on the built commit: `build.bat` and `build.sh` pass `git describe --tags --exact-match` (without a leading `v`) to `-X main.version`, and fall back to `dev`.

**Updating in place** (`self_update.go`). The release dialog shows **Update now** when `DownloaderApp.canSelfUpdate` holds: a release build (`main.buildType == "release"`, injected only by the release script, so builds from `build.bat`/`build.sh` never replace themselves), on Windows, with no session or update running, and with the release's `GoVid_<tag>_Ready.zip` and `SHA256SUMS` among its assets (`findUpdateAssets`). Releases made before checksums were published only get "Open download page". `runSelfUpdate` disables the Download button and runs `SelfUpdater` in the background. `Download` fetches `SHA256SUMS` (`parseSHA256Sums` reads the `<hash>  <name>` lines) and then the ZIP into a new temp folder, hashing it as it streams, with progress in the status label. On a mismatch it returns `errChecksumMismatch` and keeps the ZIP for inspection. `Install` checks that the folder is writable (`dirWritable`, as the yt-dlp updater does) and extracts only `GoVid.exe` as `GoVid.exe.new`, accepting backslash entry names because `Compress-Archive` writes them. It then renames the running `GoVid.exe` to `GoVid.exe.old` (Windows allows renaming a running executable, not overwriting it), moves `.new` into place, and starts it. Any failure puts the old executable back. On success the app quits through `Shutdown`. At startup, `cleanUpAfterUpdate` deletes `GoVid.exe.old`, retrying for a while, because the previous process may still be closing and Windows refuses to delete a running executable.


**Notices:** `UIManager.showNotice(notice{id, text, actionLabel, action})` shows a non-blocking bar above the input card. A notice with the same `id` replaces the old one; the action button and the dismiss button both remove it (`dismissNotice`). Notices live in `UIManager.notices` and are re-rendered by `createUI`, so they survive a theme change. A successful yt-dlp update dismisses the yt-dlp notice.

---

## 5. Data flows

### 5.1 Download flow (happy path)

```
User clicks Download
  └─ startDownload()          validate URLs; open log file; spawn the sequential download worker
       ├─ checkURLs()          engine.Probe per URL; a playlist → askPlaylist prompt → its chosen videos become queue items
       ├─ skipDownloaded()     history match by video ID + extractor (or URL) → askDuplicate: Download again / Skip / Skip all
       └─ runQueue()           QueueModel.Next() → the first waiting item, until none waits (the Queue panel may remove, move, or retry items meanwhile)
            └─ downloadItem()    per item; status Checking → Downloading x% → Done / Failed / Skipped
                 ├─ checkItem()          engine.ProbeVideo when the item has no fresh probe answer (playlist entries; answers over 30 min old)
                 ├─ reportQualityFit()   qualityFit(format, quality, probe height) → log line + notice when it differs from the cap
                 ├─ prepareLive()        live or scheduled stream → askLive: Record from now / from the start / Wait and record / Skip
                 ├─ reportSubtitles()    the probe's subtitle languages; warns when none matches --sub-langs (matchSubLangs)
                 ├─ checkDiskSpace()     probe size estimate × 1.1 (× 2 with post-processing) vs freeBytes(save folder) — not for live streams
                 └─ runYtDlp()
                      ├─ engine.BuildArgs(DownloadRequest)   → []string args (--load-info-json <probe JSON> instead of the URL)
                      ├─ engine.Execute(ctx, args, opts, cb)   → scanResult
                      │    ├─ cmd.StdoutPipe / StderrPipe
                      │    └─ engine.watchOutput() goroutines (parse % / size / phase) → cb.OnProgress, cb.OnPhase
                      ├─ engine.FinalizeFiles()               find → rename  (then RemoveLeftoverPartials; RemovePartialFiles on failure/cancel)
                      └─ historySvc.AppendAll(DownloadRecord) JSON append
  └─ applyFFmpegFilters()     if post-processing enabled
       └─ PPEngine.ApplyFilters(ctx, files, vf, af, cb)
              ├─ resolveAutoCrop() / resolveToneMap() per file (sentinels → crop / tone-map filters)
              └─ runJob() per file (CPU worker pool; GPU jobs also use gpuSem, max 2)
```

### 5.2 Preference flow

```
Startup:
  prefSvc.Load() → AppPreferences → applyPreferencesToWidgets() → widgets

User saves Prefs window:
  widget state → savePreferences(path) → AppPreferences → prefSvc.Save()

User resets Prefs:
  prefSvc.Reset() → createUI() → applyPreferencesToWidgets(prefSvc.Load())
```

---

## 6. Error tracing

GoVid uses a standard error-tracing chain that preserves both human-readable
context and machine-readable error types.

### 6.1 Pattern

1. **Origin:** a low-level call returns a concrete error type (for example,
   an `*exec.ExitError` from a failed subprocess).
2. **Wrap with context:** the caller adds message context using `%w`:
   `fmt.Errorf("meaningful context: %w", err)`.
3. **Propagate upward:** intermediate layers return the wrapped error unchanged
   (or re-wrap with more context), without converting it to a string.
4. **Classify at boundary:** at the process/UI boundary, use `errors.AsType`
   (or `errors.As`) to recover the concrete error type and map it to policy
   (exit code, user-facing status, retry choice, telemetry category, etc.).

### 6.2 Why `%w` matters

`%w` preserves the unwrap chain. This allows upper layers to recover specific
types even after multiple wraps. Using `%v` instead would lose the type chain
and prevent typed matching later.

### 6.3 Generic template

```go
func doThing() error {
	if err := lowLevelCall(); err != nil {
		return fmt.Errorf("doThing failed: %w", err)
	}
	return nil
}

func classify(err error) Category {
	if typedErr, ok := errors.AsType[*SomeConcreteError](err); ok {
		_ = typedErr // inspect fields/methods as needed
		return CategorySpecific
	}
	return CategoryGeneric
}
```

### 6.4 Guidance for new code

- Wrap errors at layer boundaries with enough context to explain "what failed"
  at that layer.
- Prefer returning `error` up the call stack; avoid terminating (`os.Exit`) in
  deep helpers/services.
- Centralize final classification/mapping at boundaries (`main`, CLI command
  handlers, top-level UI orchestrators).
- Keep fallback behavior explicit when no specific type matches.

### 6.5 Implementation Details

- Process exit code constants are defined in `types.go` (`type ExitCode` and
  `Exit*` constants).
- The error-to-exit-code mapping helper is defined in `helpers.go`
  (`exitCodeFromError(err error) ExitCode`).
- An implementation example can be found at the start of `main()`, where it is 
  used when calling `DependencyService.UpdateCLI()`.

---

## 7. Concurrency model

| Goroutine | Started by | Cancelled by |
|---|---|---|
| Download queue worker | `startDownload()` launches one background goroutine; URLs are processed sequentially, or by `Simultaneous Downloads` workers (`runParallel`, up to 3, started `workerStagger` = 3 s apart) | `queueCtx` via `context.WithCancel` |
| `DownloadEngine.watchOutput` stdout/stderr | `DownloadEngine.Execute()` | process exit + pipe close |
| Progress bar smoother | `startDownload()` → 33 ms ticker goroutine (frames that would change the bar by less than 0.002 are skipped) | `queueCtx` cancellation |
| Status dot pulse | `setStatusIndicator("active")` | `stopPulse` channel close |
| Post-process worker pool | `PPEngine.ApplyFilters()`; GPU jobs additionally wait on `gpuSem` (capacity 2) | same context |
| Recording monitor | `DownloadEngine.monitorRecording()` while a live or scheduled stream runs; reports once a second through `OnRecording` | the function it returns, called as soon as `Execute` returns |

**Background loops: owners and stop paths.** Audited for [priorities_3.md](priorities_3.md) #7. Each loop below has one owner and one way to stop; "marker" means it logs `[DEBUG] Loop started/stopped: <name>` with the goroutine count while Debug Output is on (`markLoop`).

| Loop | Owner (starts it) | Stops when | Waited for | Marker |
|---|---|---|---|---|
| Session goroutine (`runSession`) | `startSession` | the queue is done or `queueCtx` is cancelled (Cancel, `StopSession`, Shutdown) | `sessions` wait group (Shutdown, up to 5 s) | – |
| Download workers (`runParallel`) | `runQueue` with Simultaneous Downloads > 1 | nothing waits, `queueCtx` ends, a disk-space Stop, or `backOff` lowers the limit below the worker's number | `wg.Wait` in `runParallel` | yes |
| Progress smoother (`runProgressSmoother`, 33 ms ticker) | `startSession` | `queueCtx` cancelled (`runSession` cancels it when the session ends) | yes: `runSession` waits for `smootherDone` before it clears the session state, so two sessions' smoothers never run at once | yes |
| Status dot pulse (50 ms ticker) | `setStatusIndicator` (UI thread), for the Active and Processing states | `stopStatusPulse`: every state change closes `stopPulse` | yes: `stopStatusPulse` waits for `pulseDone` | yes |
| yt-dlp output readers (`watchOutput`, two goroutines) | `Execute` | EOF on stdout/stderr once the process tree exits (`cmd.WaitDelay` bounds a pipe a grandchild holds) | `waitGroup.Wait` in `watchOutput` | – |
| Recording monitor (1 s ticker) | `runArgs`, for live and scheduled streams | the function `monitorRecording` returns, called when `Execute` returns | yes: it waits for `finished` | yes |
| FFmpeg progress reader (inline scanner in `runJob`) | each post-processing job | EOF when ffmpeg exits; the GPU stall watchdog kills a hung encoder | runs on the job's own worker | yes |
| Post-processing worker pool | `PPEngine.ApplyFilters` | its jobs run out; the session context cancels ffmpeg | `ApplyFilters` waits | – |
| Heartbeat (`runHeartbeat`, 10 s ticker) | `setDebug(true)` | `setDebug(false)` → `stopHeartbeat` cancels it | yes: `stopHeartbeat` waits | yes |
| Throttles (status label, Pause button, Queue panel) | `latestValueThrottle.Set` | no goroutine: one `time.AfterFunc` per burst of changes, which `Flush` stops; nothing re-arms it without a new `Set` | – | – (one would fire on every change) |
| Log flush timer | `appendLogLine` | no goroutine: one `time.AfterFunc` per burst; `flushLog` clears it | – | – |
| GPU stall watchdog | `gpuJobGuard.arm` | `release` stops the timer once per job | – | – |
| Download context hooks (`context.AfterFunc` in `downloadContext`) | `downloadItem` | `release` unhooks them when the item ends | – | – |
| Short-lived workers: update checks, tool checks, installs, probes for Formats…, Components statuses, the History window's load, partial-file removal for a discarded paused row, self-update, `cleanUpAfterUpdate` (15 retries) | their UI actions or startup | they finish their one task | – | – |

No loop is shared between owners, and none outlives its owner: those without a wait end on their own once their context or pipe closes.


The session's queue is a `QueueModel` (`queue_model.go`): the items plus each one's status (Waiting, Checking, Downloading with its progress, Post-processing, Done, Failed, Skipped), behind a mutex. `runQueue` does not index a slice; it asks `Next()` for the first waiting item each time, so the Queue panel (`queue_panel.go`) can remove or move waiting items and put failed or skipped ones back at the end (`Retry`) while the queue runs. Moves only swap neighbouring waiting items, so finished and running items keep their place. `DownloaderApp.queue` (an `atomic.Pointer`) lets `updateProgress` report download progress to the downloading row. The model's `OnChanged` goes through a `latestValueThrottle`, so the panel is redrawn at most every 150 ms. The panel is a collapsible card above the log, titled with `Summary()` ("Queue — 7 of 20 done, 1 failed"), shown while the queue holds more than one item. Its rows offer Move up / Move down / Remove while waiting, Skip (the per-item cancel, as the Cancel button; `skipItem` does nothing for an item that has already finished) while running, and Retry for failed or skipped items while the session runs. Items not reached when the queue stops are marked Skipped. Paused items and saving the queue across restarts are described under **Pause and resume** below.

The download queue is sequential unless **Simultaneous Downloads** is above 1 (see **Simultaneous downloads** below). Before it starts, `checkURLs` probes
each URL under `queueCtx` (so Cancel stops the probes) and builds
`downloadSession.items`. A playlist's chosen videos become separate items, so a
single playlist URL turns the session into a batch. The playlist prompt
(`UIManager.askPlaylist`) blocks the session goroutine until the user answers,
and is hidden if `queueCtx` is cancelled first. When the queue holds more than
one item, each item gets a child `runCtx` of the queue-level `queueCtx`.
Cancelling the child skips only the active item; with a single item, `runCtx`
is the queue context, so cancellation stops the session. Before each item,
`checkItem` probes it under `runCtx` (so Cancel skips the probe too) when it
has no fresh probe answer: playlist entries, which `--flat-playlist` only
listed, and items whose answer is older than `probeMaxAge`, which can happen
in long batches. An item whose first probe failed is not probed again. The
download then loads the probe's JSON (`DownloadRequest.InfoJSON`), so each
video is extracted once. Then `checkDiskSpace` compares the probe's size estimate (scaled down for a trim
range, plus a 10% margin, doubled when post-processing will write a second
copy) with `freeBytes` of the save folder. It checks every item, not just the
first, because earlier items of a batch use up space. When the space is too
small it asks through `askDiskSpace`: Continue anyway / Cancel for a single
item, or Skip / Continue (for the rest of the session) / Stop in a batch. An
unknown size, for example when the probe failed or the site lists no sizes,
skips the check and logs that it did. Post-processing runs afterward over the collected successful paths and
can process multiple files concurrently.

**Live streams:** after `checkItem`, `prepareLive` looks at the probe's `live_status`. A stream that is live, or scheduled (`is_upcoming`), is put to the user through `askLive` (blocking the session goroutine like the other prompts); `post_live` only logs a warning. A recording skips `checkDiskSpace` and the probe's info JSON (its manifest changes, and a scheduled stream has no formats yet). It runs under `downloadContext` (see **Pause and resume**): a context derived from `context.WithoutCancel` of the item's context, cancelled with `errStopKeep` either by its stop function (which `SetCancelFunc` hands to the Cancel button and the Queue panel's row, both reading "Stop recording") or, through `context.AfterFunc`, when the item's or session's context is cancelled. So skipping the item, stopping the session, and quitting all keep the recording. `setRecordingView` swaps the progress bar for a `ProgressBarInfinite` meanwhile. `recordingCallback` shows "Recording 00:12:34 · 410 MiB" (or the countdown to a scheduled start) and every `liveSpaceCheckInterval` (30 s) checks `freeBytes`, stopping the recording, keeping it, below `liveMinFreeBytes` (1 GiB).

**Process trees:** yt-dlp, ffmpeg, and ffprobe are started through
`newToolCommand` (`process.go`), which sets `cmd.Cancel` to kill the whole
process tree (`taskkill /T` on Windows, the process group on Unix) and
`cmd.WaitDelay` so `Wait` cannot hang on pipes a surviving grandchild holds.
This matters because yt-dlp runs its own ffmpeg for merging and trimming.
The readers of a running tool's output (`watchOutput`'s two goroutines and
`runJob`'s FFmpeg stderr loop) use `newOutputScanner`, which accepts lines up
to `maxOutputLine` (1 MiB) instead of `bufio.Scanner`'s 64 KiB. If a scanner
still stops early, the reader logs it and `drainOutput` reads the rest, since
a tool blocked on a full pipe never exits and `WaitDelay` only starts once it
has; `runJob` pets the GPU stall watchdog while it drains.

**Simultaneous downloads** (`parallel.go`). With the preference at 2 or 3 (`session.workers`, read once), `runQueue` hands the queue to `runParallel`: that many workers, each taking `Next()` until nothing waits; a worker that finds only paused items, with none active, waits for a resume. Each item gets an `itemRun` with its own `DownloadStats`; `parallelCallbacks` prefixes its log lines with "[n/total] " (`lineItem`; `renderLogLines` keeps one in-place progress line per prefix in `UIManager.progressLines`), sends its progress to its row (`QueueModel.SetProgress`), and shows the whole queue in the bar and status (`showParallelProgress`: `OverallProgress`, `ActiveCount`); `reportDownloadResult` then leaves the status, dot, and bar to the queue, and `runParallel` sets them when all are done. Cancel stops the session; each row's Skip and Pause act on their own item through `controls`. Shared state is serialised: prompts by `promptMu`, picking a free name and renaming by `renameMu` (`FinalizeFiles`, `finishRecording`), history writes by `HistoryService.mu`, and the disk check subtracts `reservedBytes`. After an HTTP 429 (`scanResult.hadRateLimit`) or a bot check, `backOff` lowers `queueMode.limit` to 1, so the other workers stop taking items, and logs it once.

**Pause and resume** (`pause_resume.go`, `queue_store.go`). yt-dlp runs with
`--continue` and writes `.part` files (a live recording keeps `--no-part`, so a
stopped recording is not left as a `.part` file). Each queue item gets a download
ID once, in `NewQueueModel` (`newDownloadID`, strictly increasing), and its own
`request`: a copy of the settings `readSession` read from the widgets once (through `readSessionSettings`, which `resumeQueue` shares; it also reads Auto-retry, Notify on Completion, and the Encoder Backend into the session, so the session goroutine reads no widget)
(`downloadSession.request`, `queueItem.withRequest`). So a retry, an auto-retry,
a resume, and a restored item write the same names, and yt-dlp continues their
partial files; `FinalizeFiles` skips those (`isPartialFile`). Each download runs
under `downloadContext`, derived with `context.WithoutCancel` from the item's
context: **Pause** (`setPauseFunc` → `requestPause`, the main window's Pause
button and the Queue panel's row) cancels it with `errPaused`, so `runArgs` keeps
the partial files and returns `DownloadResult.Paused`, and the item becomes
`queuePaused`. When the item's or session's context ends, `context.AfterFunc`
cancels it plainly (removing the files), or with `errPaused` when `quitting`.
With only paused items left, `runQueue` blocks in `waitForResume` on
`QueueModel.Changed()` (the button reads Resume and calls `ResumeAll`; `Resume`
puts an item first among the waiting). `finishQueue` then settles the queue:
when quitting, `saveQueue` writes the waiting and paused items (the one
downloading counts as paused) to `queue.json` beside the history
(`QueueStore`; `savedQueueItem` keeps the URL, title, IDs, status, and the
settings, not cookies or the probe JSON); otherwise waiting items are marked
Skipped and paused ones discarded, their partial files removed. At the next
start `offerQueueRestore` asks through `askRestoreQueue`; Resume calls
`resumeQueue`, which starts a `restored` session with those items (no URL check;
each is probed again, and `checkItem` logs when the probe's `format_id` differs
from the saved one, since that stream starts over), and Discard deletes their
partial files. A paused item keeps its probe JSON, so resuming it in the same
session does not extract it again.

**Shutdown:** closing the window during a session asks for confirmation, then
calls `DownloaderApp.Shutdown(quit)`. It calls `StopSession` (which cancels
`queueCtx`, unlike the Cancel button that only skips the current batch item),
shows "Stopping…", waits off the UI thread for the `sessions` wait group (up to
`shutdownTimeout`, 5 s), closes the session log, and finally calls `quit`
inside `fyne.Do`. It first sets `quitting`, so the running download is paused
rather than cancelled and the queue is saved (see **Pause and resume**).

**UI thread rule:** every widget mutation must run inside `fyne.Do(func() { … })` when called from a non-main goroutine. Fyne panics on direct cross-thread access. The session goroutine also never reads a widget: `readSessionSettings` copies every setting a session needs into `downloadSession` on the UI thread before it starts.

---

## 8. Persistence layer

| Store | Location | Format | Owner |
|---|---|---|---|
| User preferences | Fyne app data (`com.govid.downloader`), or `<exe dir>/settings.json` in Portable Mode | Fyne KV store | `PreferenceService` |
| Session log | `<save dir>/GoVid_log_YYYY-MM-DD.txt` | Plain text | `LogService` |
| Error log | `<save dir>/GoVid_errors_YYYY-MM-DD.txt` | Plain text | `LogService` |
| Download history | `<exe dir>/download_history.json` | JSON array | `HistoryService` |
| Saved queue | `<exe dir>/queue.json` (only after quitting with downloads waiting or paused) | JSON object | `QueueStore` |
| Latest-release cache | The preferences store (`latestRelease:<owner>/<repo>` keys) | JSON in the Fyne KV store | `ReleaseService` |
| Override config | `<cwd>/govid.json`, or any file picked in Tools → Import settings… | JSON object | `PreferenceService` (`LoadFromFile` / `MergeConfig`; `ExportConfig` / `WriteConfigFile` for Tools → Export settings…); `AppConfig` is defined in `config_file.go` |

---

## 9. External tools

| Tool | Invoked by | Purpose |
|---|---|---|
| `yt-dlp` | `DownloadEngine.Execute()` | Download video/audio from URLs |
| `ffmpeg` | `PPEngine.runJob()`, `PPEngine.detectCropFilter()` | Post-processing encode / cropdetect |
| GitHub REST API | `ReleaseService.Latest()` | Latest yt-dlp and GoVid releases for the update checks (at most once a day at startup; always for the Tools menu check) |
| `ffprobe` | `PPEngine` probe methods (`pp_engine.go`) | Frame count, duration, and colour-tag queries (optional; `probeColorInfo` falls back to `ffmpeg -i`). Installed with FFmpeg from Tools → Components |
| `deno` / `node` / `bun` | yt-dlp, through `--js-runtimes` (`DependencyService.JSRuntime`) | Solving YouTube's player challenges. Deno is installed on demand into `bin/` (Tools → Components) |
| GitHub releases, gyan.dev | `ToolInstaller` | Downloads of yt-dlp, Deno, and FFmpeg, each checked against its published SHA-256 |

Tools are resolved with `depSvc.Resolve(toolName)`: prefers `./bin/<tool>[.exe]` beside the executable, falls back to `$PATH`. If neither is found, `depSvc.Check()` (called via `uiManager.checkDependencies()` at startup) prints a warning to the log, and `checkTools` shows a notice offering to install it.

---

## 10. Updating this document

After each refactoring step:

1. **`docs/classes.puml`** — add or update the class that changed; update its relationships and fields.
2. **`docs/architecture.md`** (this file) — update the relevant section in §4; add a row to §8 if a new persistence store is introduced; update §5 if data flows change.
3. **`docs/refactor_roadmap.md`** — mark completed items and add next-step notes for the new component.
4. **`docs/sequence-full.puml`** — update if the startup or download sequence changes.

If a new service is extracted, add it to:
- The `File map` table (§3)
- The `Component reference` (§4), following the same structure as existing entries
- `docs/classes.puml` as a new `class` block with its relationships

> Keep descriptions factual and code-level. Avoid prose that could go stale.
