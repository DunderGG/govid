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
| Platforms | Windows, macOS, Linux |

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
├── main.go                 Entry point; constructs DownloaderApp, wires close-intercept, calls ShowAndRun
├── types.go                Central struct definitions (DownloaderApp, UIWidgets, LogManager, DownloadStats, AppConfig, PostProcessJob)
│
├── ── Services / Engines ──────────────────────────────────────────
├── download_engine.go      DownloadEngine — yt-dlp arg builder and retry executor
├── pp_engine.go            PPEngine — concurrent FFmpeg post-processing worker pool
├── preference_service.go   PreferenceService — all preference keys, defaults, Load/Save/Reset
├── ui_manager.go           UIManager — owns secondary window lifecycle (About, Help, History, Prefs, PP)
│
├── ── Orchestration ───────────────────────────────────────────────
├── download.go             DownloaderApp.startDownload / runYtDlp — UI orchestration for a download session
├── postprocess.go          DownloaderApp.buildPostProcessFilters / applyFFmpegFilters — thin UI wrapper + utility functions
├── logscanner.go           watchOutput / parseProgress — yt-dlp stdout/stderr parsing goroutines
│
├── ── UI ──────────────────────────────────────────────────────────
├── ui.go                   createUI, createMainMenu, showPreferences, showPostProcessing
├── helpers.go              Thread-safe UI updates, applyPreferencesToWidgets, savePreferences, resetPreferences, dependency checks
├── history.go              DownloadHistoryEntry type; loadDownloadHistory / appendDownloadHistory / buildDownloadHistoryEntries
│
├── ── Assets / Platform ───────────────────────────────────────────
├── theme.go                darkTheme and lightTheme (implement fyne.Theme)
├── icons.go                SVG icon registry; themedIcon() helper
├── embedded_icon.go        Bundled app icon (resourceAppiconPng)
├── sys_windows.go          Windows-only: SysProcAttr to hide console windows spawned by FFmpeg/yt-dlp
├── sys_others.go           Non-Windows stub for the same function
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
| `log *LogManager` | Session and error log files |
| `stats *DownloadStats` | Real-time download metrics for progress smoothing |
| `cancelFn` | Cancels the active download context |
| `stopPulse` | Channel closed to stop the status-dot animation goroutine |
| `ppFailed atomic.Int32` | Counts post-processing failures across concurrent workers |
| `isRunning atomic.Bool` | Prevents close without confirmation while jobs are active |

---

### 4.2 `UIWidgets` — widget bag  
*Defined in:* `types.go`

A flat struct holding every Fyne widget. It intentionally carries no logic — widgets are wired with callbacks in `createUI()`. Grouped conceptually into download controls, session options, settings controls, and post-processing controls.

> **Planned:** split into smaller feature-scoped structs (see `docs/refactor_roadmap.md`).

---

### 4.3 `UIManager` — secondary window owner  
*Defined in:* `ui_manager.go`

Owns the five singleton secondary windows (About, Help, History, Preferences, Post-Processing). Calling a `show*` method on `UIManager` will re-focus an already-open window rather than opening a duplicate. `DownloaderApp` delegates its public `showAbout()`, `showHistory()`, and `showConfigHelp()` methods to `UIManager`.

> **Planned next:** `showPreferences` and `showPostProcessing` will move here once all their dependencies are named services.

---

### 4.4 `DownloadEngine` — yt-dlp executor  
*Defined in:* `download_engine.go`

A stateless service that owns the resolved paths to `yt-dlp` and `ffmpeg` and provides two methods:

- **`BuildArgs(DownloadRequest) DownloadArgs`** — pure function; assembles the yt-dlp command-line arguments from a request value struct. No I/O.
- **`Execute(ctx, args, autoRetry, index, total, ProcessCallbacks) (scanResult, error)`** — starts the process, streams stdout/stderr through `ProcessCallbacks.WatchOutput`, and retries on transient errors with 1 s / 5 s / 30 s back-off.

`ProcessCallbacks` is a bridge struct: it carries closures that let the engine report progress back to the UI without importing Fyne. `DownloaderApp.runYtDlp()` is the only caller.

---

### 4.5 `PPEngine` — FFmpeg post-processing engine  
*Defined in:* `pp_engine.go`

Owns the resolved paths to `ffmpeg` and `ffprobe`. Exposes one public method:

- **`ApplyFilters(ctx, filePaths, vfFilters, afFilters, PPCallbacks)`** — builds one `PostProcessJob` per file, then runs them concurrently through a worker pool bounded to `runtime.NumCPU()` goroutines. Each worker calls the private `runJob()`.

Internal flow per file:
1. `resolveAutoCrop` — replaces the `__autocrop__` sentinel by running a 60-second `cropdetect` pass via `detectCropFilter`.
2. `runJob` — runs the main FFmpeg encode. Streams stderr in real-time. On success, renames the `_pp` temp file over the original. On failure, deletes the temp file.

`DownloaderApp.applyFFmpegFilters()` in `postprocess.go` is the thin wrapper that constructs `PPEngine` and wires `PPCallbacks` back to UI helpers.

---

### 4.6 `PreferenceService` — preference persistence  
*Defined in:* `preference_service.go`

All 30 Fyne preference storage keys are named constants here (`prefSavedPath`, `prefFormat`, …). Default values are separate named constants (`defaultThemeMode`, `defaultSmoothFPS`, …).

- **`Load() AppPreferences`** — reads the Fyne store and returns a fully-defaulted plain struct. Called once at startup and again each time a secondary window refreshes its controls.
- **`Save(AppPreferences)`** — writes the struct back. Honours the `savePrefs` gate: if the user has disabled persistence, only the toggle itself is written.
- **`Reset()`** — removes all managed keys so the next `Load` returns defaults.

`AppPreferences` is a plain value struct with no widget references. `applyPreferencesToWidgets(AppPreferences)` in `helpers.go` is the single translator from struct → widget state. `savePreferences(path)` in `helpers.go` is the reverse translator (widget state → struct → `prefSvc.Save`).

---

### 4.7 `LogManager` — file logging  
*Defined in:* `types.go`; used by `helpers.go` and `download.go`

Holds the open session log file handle and two mutexes. The session log is `GoVid_log_YYYY-MM-DD.txt` in the save directory. Lines containing `ERROR` or `FAILED` are also mirrored to a separate `GoVid_errors_YYYY-MM-DD.txt` file via `appendErrorOutput()`.

---

### 4.8 `DownloadHistoryEntry` + persistence  
*Defined in:* `history.go`

JSON records written to `download_history.json` beside the executable. Each successful download appends one entry per output file. Fields include `url`, `originalTitle`, `finalFilename`, `savedPath`, `format`, `quality`, `downloadedAt`, and `postProcessed`. The History window (shown via `UIManager.showHistory()`) reads and renders all entries in reverse-chronological order.

---

### 4.9 `darkTheme` / `lightTheme`  
*Defined in:* `theme.go`

Both implement `fyne.Theme`. `darkTheme` is the default; `lightTheme` is applied when the user selects "Light" in Preferences. The active theme is stored as a preference and applied at startup before the window is created.

---

## 5. Data flows

### 5.1 Download flow (happy path)

```
User clicks Download
  └─ startDownload()          validate URLs; open log file; spawn worker goroutines
       └─ runYtDlp()           per URL
            ├─ engine.BuildArgs(DownloadRequest)   → []string args
            ├─ engine.Execute(ctx, args, cb)        → scanResult
            │    ├─ cmd.StdoutPipe / StderrPipe
            │    └─ cb.WatchOutput → watchOutput() goroutines (parse % / size)
            ├─ finalizeDownloadedFiles()            glob → rename
            └─ appendDownloadHistory()              JSON append
  └─ applyFFmpegFilters()     if post-processing enabled
       └─ PPEngine.ApplyFilters(ctx, files, vf, af, cb)
            └─ runJob() per file (worker pool)
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

## 6. Concurrency model

| Goroutine | Started by | Cancelled by |
|---|---|---|
| Per-URL download worker | `startDownload()` via `sync.WaitGroup` | `context.WithCancel` (Cancel button) |
| `watchOutput` stdout/stderr | `DownloadEngine.Execute()` | process exit + pipe close |
| Progress bar smoother | `createUI()` → ticker goroutine | same context cancel |
| Status dot pulse | `setStatusIndicator("active")` | `stopPulse` channel close |
| Post-process worker pool | `PPEngine.ApplyFilters()` | same context |

**UI thread rule:** every widget mutation must run inside `fyne.Do(func() { … })` when called from a non-main goroutine. Fyne panics on direct cross-thread access.

---

## 7. Persistence layer

| Store | Location | Format | Owner |
|---|---|---|---|
| User preferences | Fyne app data (`com.govid.downloader`) | Fyne KV store | `PreferenceService` |
| Session log | `<save dir>/GoVid_log_YYYY-MM-DD.txt` | Plain text | `LogManager` |
| Error log | `<save dir>/GoVid_errors_YYYY-MM-DD.txt` | Plain text | `LogManager` |
| Download history | `<exe dir>/download_history.json` | JSON array | `history.go` |
| Override config | `<cwd>/govid.json` | JSON object | `helpers.go` |

---

## 8. External tools

| Tool | Invoked by | Purpose |
|---|---|---|
| `yt-dlp` | `DownloadEngine.Execute()` | Download video/audio from URLs |
| `ffmpeg` | `PPEngine.runJob()`, `PPEngine.detectCropFilter()` | Post-processing encode / cropdetect |
| `ffprobe` | `postprocess.go` probe functions | Frame count and duration queries for progress estimation |

Tools are resolved with `resolvedBinPath(toolName)`: prefers `./bin/<tool>[.exe]` beside the executable, falls back to `$PATH`. If neither is found, `checkDependencies()` prints a warning to the log at startup.

---

## 9. Updating this document

After each refactoring step:

1. **`docs/classes.puml`** — add or update the class that changed; update its relationships and fields.
2. **`docs/architecture.md`** (this file) — update the relevant section in §4; add a row to §7 if a new persistence store is introduced; update §5 if data flows change.
3. **`docs/refactor_roadmap.md`** — mark completed items and add next-step notes for the new component.
4. **`docs/sequence-full.puml`** — update if the startup or download sequence changes.

If a new service is extracted, add it to:
- The `File map` table (§3)
- The `Component reference` (§4), following the same structure as existing entries
- `docs/classes.puml` as a new `class` block with its relationships

> Keep descriptions factual and code-level. Avoid prose that could go stale.
