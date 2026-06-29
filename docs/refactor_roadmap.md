# GoVid Refactoring Roadmap — Priority Sorted

This version groups the audit items into priority buckets so you can tackle the biggest maintainability wins first. See the audit for details. [audit_review.md](audit_review.md)

## High Priority

- [ ] Refactor ui.go into smaller helpers — Split the large window construction into helpers for menus, dialogs, history, and preferences so the file is easier to scan and change.
- [ ] Split download.go into phases — Separate yt-dlp argument building, process startup, output parsing, and retry handling into smaller functions. 
- [ ] Break postprocess.go into smaller pipelines — Move FFmpeg option building, UI state handling, and feature-specific logic into smaller functions or separate files.
- [ ] Use context.Context consistently for cancellation — Pass context through the download pipeline so stopping a job does not leave background work running.
- [ ] Group UIWidgets into smaller structs — Break the large UIWidgets type into smaller feature-specific structs like download controls and preferences controls.
- [ ] Keep main.go thin — Use main.go as a bootstrapper only, and move app-specific setup into smaller constructors or services.

## Medium Priority

- [ ] Centralize preference loading — Move preference reads and default values into a small settings-loading layer so UI code stays focused on layout and event wiring.
- [ ] Extract shared window-focus logic — Create one helper for the repeated focus-or-create pattern so every dialog and tool window behaves consistently.
- [ ] Replace hard-coded post-processing thresholds with constants — Name the thresholds and cost values so the code self-documents what each value means and is easier to tune later.
- [ ] Keep LogManager focused on one job — Separate file appending and log persistence from mutex and error-handling details if the type grows further.
- [ ] Move history handling behind a service boundary — Keep storage and schema changes away from the UI so history can evolve without touching the main window code.
- [ ] Keep log parsing tolerant — Treat yt-dlp output parsing as best-effort so small wording changes do not break downloads.

## Low Priority

- [ ] Organize helpers.go by purpose — Split helpers into groups like parsing, filesystem, UI, and formatting so the file does not become a dumping ground.
- [ ] Make helper functions narrowly named and testable — Use descriptive helper names and prefer deterministic helpers for time, byte, and formatting logic so they are easy to test.
- [ ] Keep theme code isolated and reusable — Keep theme colors and helpers separate from UI construction, and use named constants or helpers for repeated colors.
- [ ] Isolate icon and embedded asset code — Keep generated or embedded asset files separate from application logic so they stay predictable and easier to update.
- [ ] Preserve platform-specific wrappers — Keep Windows and non-Windows process handling in dedicated build-tag files so the rest of the app can stay cross-platform and simple.

## DownloadEngine

**Done:** `DownloadEngine` struct introduced in `download_engine.go`. It owns the yt-dlp and ffmpeg binary paths and exposes two methods: `BuildArgs(DownloadRequest) DownloadArgs` (pure argument construction, no I/O) and `Execute(ctx, args, autoRetry, index, total, ProcessCallbacks) (scanResult, error)` (retry loop with exponential backoff). `ProcessCallbacks` bridges log, status, and output-scanning events back to the UI without Fyne imports. `download.go` is now a thin orchestration layer that collects UI state and delegates to the engine.

**Next steps:**

1. **Move `watchOutput` / `parseProgress` out of `DownloaderApp`** — these are called through `ProcessCallbacks.WatchOutput` but still live on `DownloaderApp` because they write to `app.stats` (progress tracking state). Extracting them requires either moving `DownloadStats` into `DownloadEngine` or passing a `OnProgress(pct float64, size string)` callback so the engine owns no mutable UI state.

2. **Move `finalizeDownloadedFiles` to `DownloadEngine`** — it is called right after `Execute` in `runYtDlp` and is purely file-system work (glob, rename, uniquePath). A `FinalizeFiles(savePath, downloadID string, onLog func(...)) []string` method would complete the engine's ownership of a single URL's full lifecycle.

3. **Move `runYtDlp` to `DownloadEngine`** — once the two steps above are done, `runYtDlp` becomes a pure composition of `BuildArgs` + `Execute` + `FinalizeFiles` with no remaining UI state reads, and can become `engine.Run(ctx, req, callbacks)`.

4. **Refactor `execute`** — The function `Execute()` takes too many arguments.

## PPEngine

**Done:** `PPEngine` struct introduced in `pp_engine.go`. It owns the ffmpeg and ffprobe binary paths and exposes `ApplyFilters(ctx, filePaths, vfFilters, afFilters, PPCallbacks)`. `PPCallbacks` bridges log, status, and failure events to the UI. Private methods `detectCropFilter`, `resolveAutoCrop`, and `runJob` are fully engine-owned. `postprocess.go` is now a thin layer containing `buildPostProcessFilters` (reads UI state) and a 5-line `applyFFmpegFilters` wrapper.

**Next steps:**

1. **Move `buildPostProcessFilters` out of `DownloaderApp`** — it currently reads checkbox and slider values directly from `*UIWidgets`. The clean solution is a `PostProcessSettings` value struct (all plain fields) that `buildPostProcessFilters` accepts as input instead of reading `app.ui.*`. The caller (UIManager or DownloaderApp) populates it from widget state and passes it to a free function or a `PPEngine` method.

2. **Move probe functions to `PPEngine`** — `probeFrameCount`, `probeDuration`, `computeOutputFrameCount`, and `parseRationalFPS` are free functions in `postprocess.go` only because `PPEngine` did not exist when they were written. They have no UI dependency and belong as private methods on `PPEngine`.

3. **Move `buildFFmpegArgs` and `patchThreadCount` to `PPEngine`** — same reasoning as the probe functions; both are pure helpers used exclusively by `PPEngine.runJob` and `PPEngine.ApplyFilters`.

4. **Refactor `runJob()`** — Or document it better. The multiple renaming and error handling is quite confusing.

5. **One operation per line** — For example, break the "totalFrames: computeOutputFrameCount(ctx, engine.FFprobePath, inputPath, probeFrameCount(ctx, engine.FFprobePath, inputPath), activeVF)" into separate operations.

## UIManager

**Done:** `UIManager` struct introduced in `ui_manager.go`. It owns the five singleton window fields (`aboutWindow`, `helpWindow`, `historyWindow`, `prefsWindow`, `ppWindow`) previously scattered on `DownloaderApp`. The three self-contained show methods (`showAbout`, `showHistory`, `showConfigHelp`) have moved to `UIManager`; their counterparts on `DownloaderApp` are now one-line delegates.

**Next steps — blocked until other services are extracted:**

1. **Move `showPreferences` to UIManager** — currently calls `savePreferences`, `resetPreferences`, `loadConfigFromFile`, `applyConfig`, `createUI`. Once `PreferenceService` owns those, `showPreferences` needs only a `PreferenceService` reference and an `onThemeChange` callback, which is manageable.

2. **Move `showPostProcessing` to UIManager** — currently calls `computeProcessingLoad`, `buildPostProcessFilters`, `savePreferences`. Once `PreferenceService` and `PPEngine` are the named dependencies, the callback surface shrinks to two objects.

3. **Move `createMainMenu` to UIManager** — menu item callbacks (`startDownload`, `runUpdateInUI`, `showPostProcessing`, etc.) become `UIManager` callback fields, wired at construction time. Depends on `DependencyService` for the updater action.

4. **Move `createUI` to UIManager** — the largest step. The main window layout reads from `*UIWidgets` and calls back into almost every service. This should be last, after all other services exist, so callbacks are typed references rather than raw closures over `DownloaderApp`.
