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
├── preference_service.go   PreferenceService — preference keys, defaults, Load/Save/Reset, LoadFromFile, MergeConfig;
│                           savePreferences, parseAppConfig, isValidOption co-located
├── history_service.go      HistoryService — Load/AppendAll/Clear; DownloadRecord and DownloadHistoryEntry types
├── history_window.go       UIManager.showHistory — the searchable History list with Re-add / Show in folder / Copy URL
├── duplicates.go           skipDownloaded / askDuplicate — "Already downloaded" check before a session downloads
├── log_service.go          LogService — session log open/close, error log routing, buffer-limit management
├── dependency_service.go   DependencyService — binary path resolution, dependency checks, yt-dlp updater
├── ui_manager.go           UIManager — main window layout (createUI, createMainMenu), secondary window lifecycle
│                           (About, Help, History, Prefs, PP), and preference/dependency UI wrapper methods
├── gpu_capability.go       GPUCapabilityService — GPU backend capability detection and cache (see docs/gpu-acceleration.md)
├── release_service.go      ReleaseService — latest GitHub release lookups with a daily cache; version comparison
├── update_check.go         Startup yt-dlp and GoVid update checks, their notices, installed/latest yt-dlp versions
├── release_dialog.go       Tools → "Check for GoVid updates" and the release-notes dialog
│
├── ── Orchestration ───────────────────────────────────────────────
├── download.go             DownloaderApp.startDownload / runYtDlp — UI orchestration for a download session
├── playlist.go             checkURLs / checkItem — probes each URL, expands playlists into queue items, probes entries before download; range parsing
├── playlist_dialog.go      UIManager.askPlaylist — the "Playlist detected" prompt
├── url_input.go            UIManager load-from-file / paste / drop of URLs; parseURLList, shortcutURL
├── subtitles.go            matchSubLangs / reportSubtitles — which subtitle languages a video has and which are downloaded
├── disk_space.go           checkDiskSpace — free-space check before each queued item; the "Low disk space" prompt
├── postprocess.go          PostProcessSettings, buildPostProcessFilters / applyFFmpegFilters — value struct + thin UI wrapper; shared format/scan helpers
├── logscanner.go           DownloadEngine.watchOutput / parseProgress — yt-dlp stdout/stderr parsing goroutines
│
├── ── UI ──────────────────────────────────────────────────────────
├── ui.go                   Thin DownloaderApp delegates to UIManager's secondary windows; shared roundedCard/accentBar helpers
├── options.go              Named labels and option lists for every enum-like selector (format, quality, theme, PP modes…)
├── helpers.go              Thread-safe UI updates, applyPreferencesToWidgets, cancellation callback guard, GPU detection kickoff
├── throttle.go             latestValueThrottle — applies the newest of a stream of values at most once per interval (status label)
│
├── ── Assets / Platform ───────────────────────────────────────────
├── theme.go                darkTheme and lightTheme (implement fyne.Theme)
├── icons.go                SVG icon registry; themedIcon() helper
├── embedded_icon.go        Bundled app icon (resourceAppiconPng)
├── process.go              newToolCommand — starts yt-dlp/FFmpeg so cancelling kills the whole process tree
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
| `askPlaylist func(ctx, playlistPrompt) playlistDecision` | Asks which videos of a playlist to download; set to `UIManager.askPlaylist`, stubbed in tests |
| `freeBytes func(path string) (uint64, error)` | Free space on a folder's volume; `freeDiskBytes`, faked in tests |
| `askDiskSpace func(ctx, diskSpacePrompt) diskSpaceDecision` | Asks what to do when a download will not fit; set to `UIManager.askDiskSpace`, stubbed in tests |
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
- **`PreferenceControls`** — maximum speed, theme mode, cookies path, save-preferences toggle, log-limit selector, the "Debug Output" and "Check for updates on startup" toggles, the "Embed in File" toggles (metadata, thumbnail, chapters), and the subtitle mode, languages, and "Include auto-generated" toggle.
- **`PostProcessControls`** — the post-processing master toggle, FFmpeg filter controls, upscale settings, and GPU backend selector.

Widgets are wired with callbacks in `UIManager.createUI()` and accessed through `ui.download`, `ui.prefs`, and `ui.postProcess`.

---

### 4.3 `UIManager` — main window and secondary window owner  
*Defined in:* `ui_manager.go`

Owns the primary window reference (`mainWindow`) plus the five singleton secondary windows (About, Help, History, Preferences, Post-Processing). Calling a `show*` method re-focuses an already-open window rather than opening a duplicate, via the shared `focusOrCreate`/`onWindowClosed` helpers. `UIManager` holds no direct service references — every service access is bridged through injected callbacks (`onLoadHistory`, `onCheckDependencies`, `onSavePreferences`, etc.), wired once in `newDownloaderApp`.

Beyond the five `show*` methods, `UIManager` also owns:
- **`createUI()`** — builds the main window layout, split into focused helpers (`buildHeader`, `configureEntryMode`, `wireToggleHandlers`, `wireActionButtons`, `buildInputCard`, `buildStatusCard`, `buildLogPane`, `buildFooter`).
- **`createMainMenu()`** — builds the menu bar.
- **`showLoadURLFile` / `loadURLList`, `pasteURLs`, `handleDrop`** (`url_input.go`) — the "Load from file…" button, the paste button, and the window's drop handler (`SetOnDropped`, set in `createUI`). All go through `addURLs`, which appends to the URL field without duplicates (`mergeURLs`) and switches on batch mode when the field then holds more than one URL. `parseURLList` skips blank lines, `#` comments (which `collectURLs` also skips in batch mode), and lines that are not http(s) URLs (`looksLikeURL`), and the log says what was skipped. Paste takes the clipboard only if every non-blank line is a URL. A dropped `.txt` is loaded as a list, and a `.url` or `.desktop` shortcut adds its `URL=` line (`shortcutURL`). Links dragged straight from a browser do not arrive on Windows, because GLFW accepts only dropped files there (`WM_DROPFILES`).
- **`savePreferences`, `resetPreferences`, `rebuildUI`** — preference persistence and full UI-rebuild-on-reset, used by `showPreferences`.
- **`checkDependencies`, `runUpdateInUI`** — thin delegates to the injected `onCheckDependencies`/`onRunUpdate` callbacks for the startup tool check and the "Update yt-dlp" menu action.
- **`appendLogLine` / `flushLog`** — batched log rendering. `appendLogLine` is safe to call from any goroutine: it queues the line under `logMu` and, for the first queued line, arms a `logFlushInterval` (100 ms) timer. `flushLog` then renders every queued line in a single `fyne.Do`: it adds the lines, trims once to `screenLogLimit` (the Log Buffer Limit from `onLogBufferLimit`, but never more than `maxScreenLogLines` = 5000, even for "Unlimited"), refreshes once, and scrolls to the bottom only if the view was already there (`isScrolledToBottom`), so a user who scrolled up is not pulled back down. `finishSessionUI` calls `flushLog` directly so the session summary appears at once, and `clearTerminalOutput` drops lines still queued (see §4.7).

`ui.go` is what remains outside `UIManager`: thin one-line `DownloaderApp` delegates to the `show*` methods above (`showHistory`, `showPostProcessing`, `showPreferences`, `showConfigHelp`), plus the shared `roundedCard`/`accentBar` container helpers `UIManager` uses when building widgets.

---

### 4.4 `DownloadEngine` — yt-dlp executor  
*Defined in:* `download_engine.go`

A stateless service that owns the resolved paths to `yt-dlp` and `ffmpeg` and provides four methods:

- **`BuildArgs(DownloadRequest) DownloadArgs`** — pure function; assembles the yt-dlp command-line arguments from a request value struct. No I/O. The format selector comes from `formatSelection(format, quality)`, which `Probe` shares; it also returns the height cap, which is empty for Best and for the audio formats. A capped video is labelled with the height actually downloaded, through the `heightLabel` template field `%(height&_{}p|)s` (nothing when the height is unknown). `embedArgs` adds `--embed-metadata`, `--embed-thumbnail --convert-thumbnails jpg` (except for WebM, which sets `DownloadArgs.ThumbnailSkipped` so `Run` can log why), and `--embed-chapters` from the request's `EmbedMetadata`/`EmbedThumbnail`/`EmbedChapters`. `subtitleArgs` adds `--write-subs [--write-auto-subs] --sub-langs <langs> --convert-subs srt` for every subtitle mode except Off, plus `--embed-subs` for Embed and Both. yt-dlp keeps the subtitle files after embedding when `--write-subs` is given, so Embed adds `--compat-options no-keep-subs` to delete them; `--write-subs` is needed because `--write-auto-subs` alone takes only auto-generated captions. Subtitles embedded in WebM stay WebVTT (`--convert-subs vtt`), the only format WebM holds, and audio formats skip subtitles (`DownloadArgs.SubtitlesSkipped`).
- **`Probe(ctx, DownloadRequest) (MediaInfo, error)`** — runs `yt-dlp -J --flat-playlist --no-warnings` with the download's `-f` selector and cookies, and without `--no-playlist` (`probe.go`). It returns `MediaInfo{Type, Title, Duration, Entries}`; `IsPlaylist()` is true for `_type == "playlist"`, and each `PlaylistEntry.DownloadURL()` is the video's URL. `--flat-playlist` lists a playlist's entries without extracting them, but a single video is still extracted in full. For a single video, `EstimatedSize()` adds up the `filesize` (or `filesize_approx`) of the requested formats, for the disk space check, and the `MediaInfo` keeps the JSON itself (`raw`) and when it was read (`probedAt`). `ProbeVideo` is the same probe with `--no-playlist`, for playlist entries and "Only this video" links. `isFresh(now)` is false once the answer is older than `probeMaxAge` (30 min), because the format URLs in it expire.
- **`Execute(ctx, args []string, opts DownloadOptions, ProcessCallbacks) (scanResult, error)`** — starts the process, streams stdout/stderr through its own private `watchOutput` method (defined in `logscanner.go`), and retries on transient errors with 1 s / 5 s / 30 s back-off when `opts.AutoRetry` is set.
- **`FinalizeFiles(savePath, downloadID string, onLog func(string, color.Color)) []string`** — globs the temp files written under `downloadID`, strips the token, and renames each to its final conflict-free name via the private `uniquePath` helper. Reports rename events through `onLog` rather than touching the UI directly.
- **`RemovePartialFiles(savePath, downloadID string, onLog func(string, color.Color))`** — deletes every file written under `downloadID` after a failed or cancelled download (yt-dlp runs with `--no-part`, so these are incomplete media files), retrying briefly while Windows still holds a lock, and logs each removal.
- **`Run(ctx, req DownloadRequest, opts DownloadOptions, ProcessCallbacks) DownloadResult`** — composes the methods above into the full lifecycle of a single URL download: `FinalizeFiles` on success, `RemovePartialFiles` on failure or cancellation. When `req.InfoJSON` holds the probe's JSON, `saveInfoJSON` writes it to a temporary `govid-*.info.json` (in the temp folder, so `FinalizeFiles` cannot pick it up), `BuildArgs` passes `--load-info-json <file>` instead of the URL, and the file is removed when `Run` returns. yt-dlp runs format selection again on the loaded info, so `-f`, cookies, `--download-sections`, and the embed flags all still apply, and the video is extracted once per download instead of twice. If that run fails with HTTP 403 or 410 (`scanResult.hadExpiredLinkErr`: the format URLs expired or were issued to another IP address), `Run` repeats it once from the URL. A subtitle that cannot be downloaded (YouTube often answers 429) fails the whole yt-dlp run, so when `scanResult.hadSubtitleErr` is set `Run` repeats it once without subtitles and says so. `splitSubtitleFiles` moves the subtitle files `FinalizeFiles` renamed into `DownloadResult.SubtitlePaths`, so `FinalPaths`, which post-processing and history use, holds only media. Reads no UI state; `DownloaderApp.runYtDlp` builds the `DownloadRequest` and `DownloadOptions` from widget values, calls `Run`, then handles history recording and the UI completion report from the returned `DownloadResult{FinalPaths, Extension, Scan, Err}`.

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

**GPU job lifecycle:** `gpuSem` (buffered channel, capacity `maxConcurrentGPUJobs = 2`) caps how many GPU-encoded jobs run concurrently, since hardware encoders like NVENC enforce a low concurrent session limit. `runJob` wraps each job's GPU-only bookkeeping in a `gpuJobGuard` (`newGPUJobGuard`, `arm`, `pet`, `release`): it acquires a `gpuSem` slot up front (no-op for CPU jobs), arms a `gpuStallTimeout` (30 s) watchdog that is `pet()` on every stderr line, and `release()`s the slot/watchdog exactly once regardless of exit path. If a GPU-encoded job fails — `cmd.Start()` error or a non-zero exit — `retryWithCPU` rebuilds the job's args with `BackendOff` and re-runs `runJob` once, so a driver hiccup or hung encoder falls back to the CPU baseline instead of failing the file outright.

`DownloaderApp.applyFFmpegFilters()` in `postprocess.go` is the thin wrapper that constructs `PPEngine` and wires `PPCallbacks` back to UI helpers; it also sets `engine.GPUBackend` from the Post-Processing dialog's selector and `engine.GPUCapabilities` from `app.gpuSvc.Detect(ctx)` before calling `ApplyFilters`.

---

### 4.6 `PreferenceService` — preference persistence  
*Defined in:* `preference_service.go`

Every Fyne preference storage key is a named constant here (`prefSavedPath`, `prefFormat`, …). Default values are separate named constants (`defaultThemeMode`, `defaultSmoothFPS`, …) in the same file; `Load()` applies those defaults when a stored value is absent.

- **`Load() AppPreferences`** — reads the Fyne store and returns a fully-defaulted plain struct. Called once at startup and again each time a secondary window refreshes its controls.
- **`Save(AppPreferences)`** — writes the struct back. Honours the `savePrefs` gate: if the user has disabled persistence, only the toggle itself is written.
- **`Reset()`** — removes all managed keys so the next `Load` returns defaults.
- **`LoadFromFile(path string) (*AppConfig, error)`** — reads and parses a `govid.json` override file. Delegates JSON parsing to the package-level `parseAppConfig` helper in `preference_service.go`. Besides `format`, `quality`, `path`, and `maxSpeed`, `AppConfig` has the `embedMetadata`/`embedThumbnail`/`embedChapters` toggles as `*bool`, so a field missing from the file leaves the stored value alone.
- **`MergeConfig(cfg, base, validFormats, validQualities) (AppPreferences, []string)`** — validates each non-empty config field against the supplied option slices and confirms the path exists as a directory, then merges valid fields onto `base`. Returns the merged struct and a slice of validation error strings for any skipped fields. No widget dependency.

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
- Before a session opens, `WriteToFile` keeps timestamped lines in the bounded `preSession` buffer. `OpenSessionLog` flushes those lines into the newly opened file, preserving startup diagnostics such as dependency warnings and GPU detection output.
- **`WriteSessionConfig(cfg SessionConfig, writeFn func(string, color.Color))`** — writes the session's starting configuration (save path, format/quality, trim, toggles, preferences, URL list, post-process settings) as one log line per setting via `writeFn`. Driven entirely by `SessionConfig`, a plain value struct with no widget references, built by `newSessionConfig(ui *UIWidgets, urls []string, savePath, trimStart, trimEnd string) SessionConfig` — it embeds the existing `PostProcessSettings` (§4.5) for its post-process fields rather than duplicating them.

Package-level helpers: `IsErrorLine(line string) bool` (matches ERROR/FAILED), `ParseBufferLimit(s string) int` (converts the preference string to an integer), `SessionLogPath(dir string)`, `ErrorLogPath(dir string)`.

`appendOutput()` in `helpers.go` is the single call-site for all log writes; it calls `logSvc.WriteToFile` for session logging and `logSvc.WriteToErrorLog` for error mirroring. It passes yt-dlp's `[debug]` lines (`IsDebugLine`) to the log view only when `DownloaderApp.showDebug` is set (the "Debug Output" preference, `prefShowDebug`); the log file always gets every line. `UIManager.renderLogLines` also shows consecutive yt-dlp progress lines (`IsProgressLine` in `logscanner.go`) as one line updated in place, so the view holds a few dozen lines per download instead of hundreds.

---

### 4.8 `HistoryService` — download history persistence
*Defined in:* `history_service.go`

Owns the path to `download_history.json` (beside the executable) and exposes three methods:

- **`Load() ([]DownloadHistoryEntry, error)`** — reads all entries in chronological order. Returns nil with no error when the file does not yet exist.
- **`AppendAll(rec DownloadRecord)`** — builds one `DownloadHistoryEntry` per path in `rec.FinalPaths` and writes the updated array in a single write. When `rec.FinalPaths` is empty a placeholder entry is appended so the URL is still recorded.
- **`Clear() error`** — overwrites the file with an empty JSON array.

The private `buildEntries` helper and `inferOriginalTitle` live here; neither has a UI dependency. `buildEntries` uses `rec.Title` (the probe's or the playlist's title) and falls back to `inferOriginalTitle` only when it is empty. `findDownloaded(entries, url, videoID, extractor)` returns the newest entry for the same video: one with the same video ID and extractor key, so `youtu.be/x` and `watch?v=x&t=1` match, or, for entries recorded before IDs were kept, the identical URL. `DownloaderApp` holds `historySvc *HistoryService`; `UIManager` uses injected `onLoadHistory` and `onClearHistory` callbacks so `showHistory` never touches the file path directly.

`DownloadHistoryEntry` is a plain JSON-serialisable value struct (url, originalTitle, finalFilename, savedPath, format, quality, downloadedAt, postProcessed, and the optional videoId and extractor), with `FilePath`, `DisplayTitle`, and `DisplayFile` helpers.
`DownloadRecord` is the plain input value passed to `AppendAll`: URL, final paths, save path, format, quality, post-processing state, and the title, video ID, and extractor the queue item carries (`queueItem.withInfo` takes them from the probe; playlist entries start with the playlist's title, ID, and `ie_key`).

**Keep download history.** The `KeepHistory` preference (on by default) is mirrored in `DownloaderApp.keepHistory` (an `atomic.Bool`, set at startup and by `UIManager.applyRuntimePrefs`). When it is off, `recordHistory` writes nothing and `skipDownloaded` does not check. Unticking it in Preferences (`onKeepHistoryChanged`) offers to delete the history kept so far.

**Repeat downloads** (`duplicates.go`). After `checkURLs`, `runSession` calls `skipDownloaded`, which reads the history once and, for each queued item that `findDownloaded` matches, asks through `askDuplicate`: "Already downloaded on <date> as <file>" with Download again / Skip, plus "Skip all duplicates" in a batch. Skipped items are logged and dropped from the queue.

**History window** (`history_window.go`). `showHistory` shows a `widget.List` of `historyRow`s, newest first, built from a `historyView` (the entries, which of their files no longer exist, and the search result). Each row has the title, a details line (date, format/quality, file name), and **Re-add** (`readdHistoryURL` → `addURLs`, switching on batch mode when the field already has a URL), **Show in folder** (`revealFileCommand`: `explorer /select,"<file>"` on Windows, `open -R` on macOS, the folder on Linux), and **Copy URL**. Rows whose file is gone are greyed out (`LowImportance`) with Show in folder disabled. The search field filters on title, URL, file name, and format.


---

### 4.9 `DependencyService` — binary discovery and updater
*Defined in:* `dependency_service.go`

Owns the `binDir` path (resolved once at construction from the executable location) and exposes:

- **`LocalPath(toolName string) string`** — returns the path to `toolName` inside `binDir`, appending `.exe` on Windows.
- **`Resolve(toolName string) string`** — returns the bundled path when it exists on disk, otherwise the bare name for system PATH lookup. Called by `runYtDlp` and `applyFFmpegFilters` when constructing `DownloadEngine` and `PPEngine`.
- **`Check(onWarning func(msg string))`** — verifies `yt-dlp` and `ffmpeg` are reachable; calls `onWarning` for each missing tool. Called at startup via `UIManager.checkDependencies()`, a thin delegate to the injected `onCheckDependencies` callback.
- **`RunUpdate(cb UpdateCallbacks)`** — runs `yt-dlp -U` in a background goroutine and reports lines/success/failure through `UpdateCallbacks`. Called via `UIManager.runUpdateInUI()`, wired to the injected `onRunUpdate` callback. When the update fails and the folder holding yt-dlp cannot be written to (`updateFailureHint`, using `dirWritable` or the test-injected `isWritable`), it explains that and how to fix it; `UpdateCLI` adds the same hint to its error.
- **`Version(toolName string) (string, error)`** — runs `<tool> --version`; used by the yt-dlp update check (§4.12).

`UpdateCallbacks` is a bridge struct (`OnLog`, `OnStatus`, `OnSuccess`, `OnFailure`) with no Fyne dependency, following the same pattern as `PPCallbacks` and `ProcessCallbacks`.

`UpdateCLI()` is used by the `--update` CLI flag in `main()`. It updates the same resolved yt-dlp binary as `RunUpdate`, synchronously, with output to stdout.

---

### 4.10 `darkTheme` / `lightTheme`  
*Defined in:* `theme.go`

Both implement `fyne.Theme`. `darkTheme` is the default; `lightTheme` is applied when the user selects "Light" in Preferences. The active theme is stored as a preference and applied at startup before the window is created.

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

**Notices:** `UIManager.showNotice(notice{id, text, actionLabel, action})` shows a non-blocking bar above the input card. A notice with the same `id` replaces the old one; the action button and the dismiss button both remove it (`dismissNotice`). Notices live in `UIManager.notices` and are re-rendered by `createUI`, so they survive a theme change. A successful yt-dlp update dismisses the yt-dlp notice.

---

## 5. Data flows

### 5.1 Download flow (happy path)

```
User clicks Download
  └─ startDownload()          validate URLs; open log file; spawn the sequential download worker
       ├─ checkURLs()          engine.Probe per URL; a playlist → askPlaylist prompt → its chosen videos become queue items
       ├─ skipDownloaded()     history match by video ID + extractor (or URL) → askDuplicate: Download again / Skip / Skip all
       └─ downloadItem()       per queue item
            ├─ checkItem()          engine.ProbeVideo when the item has no fresh probe answer (playlist entries; answers over 30 min old)
            ├─ reportQualityFit()   qualityFit(format, quality, probe height) → log line + notice when it differs from the cap
            ├─ reportSubtitles()    the probe's subtitle languages; warns when none matches --sub-langs (matchSubLangs)
            ├─ checkDiskSpace()     probe size estimate × 1.1 (× 2 with post-processing) vs freeBytes(save folder)
            └─ runYtDlp()
                 ├─ engine.BuildArgs(DownloadRequest)   → []string args (--load-info-json <probe JSON> instead of the URL)
                 ├─ engine.Execute(ctx, args, opts, cb)   → scanResult
                 │    ├─ cmd.StdoutPipe / StderrPipe
                 │    └─ engine.watchOutput() goroutines (parse % / size / phase) → cb.OnProgress, cb.OnPhase
                 ├─ engine.FinalizeFiles()               glob → rename  (RemovePartialFiles on failure/cancel)
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
| Download queue worker | `startDownload()` launches one background goroutine; URLs are processed sequentially | `queueCtx` via `context.WithCancel` |
| `DownloadEngine.watchOutput` stdout/stderr | `DownloadEngine.Execute()` | process exit + pipe close |
| Progress bar smoother | `startDownload()` → 33 ms ticker goroutine (frames that would change the bar by less than 0.002 are skipped) | `queueCtx` cancellation |
| Status dot pulse | `setStatusIndicator("active")` | `stopPulse` channel close |
| Post-process worker pool | `PPEngine.ApplyFilters()`; GPU jobs additionally wait on `gpuSem` (capacity 2) | same context |

The download queue itself is sequential. Before it starts, `checkURLs` probes
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

**Process trees:** yt-dlp, ffmpeg, and ffprobe are started through
`newToolCommand` (`process.go`), which sets `cmd.Cancel` to kill the whole
process tree (`taskkill /T` on Windows, the process group on Unix) and
`cmd.WaitDelay` so `Wait` cannot hang on pipes a surviving grandchild holds.
This matters because yt-dlp runs its own ffmpeg for merging and trimming.

**Shutdown:** closing the window during a session asks for confirmation, then
calls `DownloaderApp.Shutdown(quit)`. It calls `StopSession` (which cancels
`queueCtx`, unlike the Cancel button that only skips the current batch item),
shows "Stopping…", waits off the UI thread for the `sessions` wait group (up to
`shutdownTimeout`, 5 s) so partial files are removed, closes the session log,
and finally calls `quit` inside `fyne.Do`.

**UI thread rule:** every widget mutation must run inside `fyne.Do(func() { … })` when called from a non-main goroutine. Fyne panics on direct cross-thread access.

---

## 8. Persistence layer

| Store | Location | Format | Owner |
|---|---|---|---|
| User preferences | Fyne app data (`com.govid.downloader`) | Fyne KV store | `PreferenceService` |
| Session log | `<save dir>/GoVid_log_YYYY-MM-DD.txt` | Plain text | `LogService` |
| Error log | `<save dir>/GoVid_errors_YYYY-MM-DD.txt` | Plain text | `LogService` |
| Download history | `<exe dir>/download_history.json` | JSON array | `HistoryService` |
| Latest-release cache | Fyne app data (`latestRelease:<owner>/<repo>` keys) | JSON in the Fyne KV store | `ReleaseService` |
| Override config | `<cwd>/govid.json` | JSON object | `PreferenceService` (`LoadFromFile` / `MergeConfig`); `AppConfig` is defined in `preference_service.go` |

---

## 9. External tools

| Tool | Invoked by | Purpose |
|---|---|---|
| `yt-dlp` | `DownloadEngine.Execute()` | Download video/audio from URLs |
| `ffmpeg` | `PPEngine.runJob()`, `PPEngine.detectCropFilter()` | Post-processing encode / cropdetect |
| GitHub REST API | `ReleaseService.Latest()` | Latest yt-dlp and GoVid releases for the update checks (at most once a day at startup; always for the Tools menu check) |
| `ffprobe` | `PPEngine` probe methods (`pp_engine.go`) | Frame count, duration, and colour-tag queries (optional; `probeColorInfo` falls back to `ffmpeg -i`) |

Tools are resolved with `depSvc.Resolve(toolName)`: prefers `./bin/<tool>[.exe]` beside the executable, falls back to `$PATH`. If neither is found, `depSvc.Check()` (called via `uiManager.checkDependencies()` at startup) prints a warning to the log.

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
