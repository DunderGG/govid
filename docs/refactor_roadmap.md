# GoVid Refactoring Roadmap — Priority Sorted

This version groups the audit items into priority buckets so you can tackle the biggest maintainability wins first. See the audit for details. [audit_review.md](audit_review.md)

---

## Refactoring Sequence

The per-component sections below list individual next steps. This chapter collects them into a recommended execution order based on their dependencies.

### Phase 1 — Independent improvements (no blocking dependencies)

These steps touch isolated areas with no cross-component dependencies and can be done in any order or in parallel.

- ~~**PPEngine steps 2 & 3**~~ — *Done. Probe functions and argument builders moved to `pp_engine.go` as private `PPEngine` methods; the explicit `ffprobePath` parameter replaced by `engine.FFprobePath`.*
- ~~**PreferenceService step 2**~~ — *Done. `LoadFromFile` and `MergeConfig` added to `PreferenceService`; `loadConfigFile` and `applyConfig` removed from `helpers.go`. `applyPreferencesToWidgets` extended with guarded writes for `Format`, `Quality`, and `SavedPath`.*
- ~~**PreferenceService step 3**~~ — *Done. The four direct-write `OnChanged` handlers (`saveLog`, `notify`, `autoRetry`, `enablePostProcess`) now call `app.savePreferences(app.ui.path.Text)`. The stray raw string `"saveLog"` was also replaced by the service call.*
- ~~**LogService step 1**~~ — *Done. `sessionDir` field added to `LogService`, set by `OpenSessionLog` and cleared by `CloseSessionLog`. `WriteToErrorLog` signature reduced to `(line string)`; the directory is now resolved internally, falling back to the executable directory when no session is active. The dir-computation block removed from `appendOutput`.*
- ~~**main.go cleanup**~~ — *Done. The `-update` success path in `main()` now exits via `return` (non-error path), and cancellation is encapsulated behind `DownloaderApp.RequestCancel()` with synchronized access to the active cancel callback.*

### Phase 2 — Complete DownloadEngine (sequential)

Each step depends on the previous one.

1. ~~**DownloadEngine step 1**~~ — *Done. `OnProgress(pct float64, size string)` added to `ProcessCallbacks`; `watchOutput` and `parseProgress` moved from `DownloaderApp` to private `DownloadEngine` methods in `logscanner.go`. `Execute` now calls `engine.watchOutput` directly (the `WatchOutput` callback field was removed); progress and size are reported to the caller via `OnProgress` instead of writing `app.stats` directly.*
2. ~~**DownloadEngine step 2**~~ — *Done. `FinalizeFiles(savePath, downloadID string, onLog func(string, color.Color)) []string` added to `download_engine.go`, along with the `uniquePath` helper it depends on. `finalizeDownloadedFiles` and `uniquePath` removed from `postprocess.go`; the call site in `runYtDlp` now uses `engine.FinalizeFiles(savePath, downloadID, app.appendOutput)`.*
3. ~~**DownloadEngine step 3**~~ — *Done. `Run(ctx, req DownloadRequest, autoRetry bool, index, total int, ProcessCallbacks) DownloadResult` added to `download_engine.go`, composing `BuildArgs` + `Execute` + `FinalizeFiles` with no UI state reads. `DownloaderApp.runYtDlp` now only builds the `DownloadRequest` from widget values, calls `engine.Run`, and handles history recording plus the UI completion report from the returned `DownloadResult{FinalPaths, Extension, Scan, Err}`.*
4. ~~**DownloadEngine step 4**~~ — *Done. `DownloadOptions{AutoRetry bool; Index, Total int}` added to bundle the three runtime options `Execute` and `Run` had in common; both signatures reduced to `(ctx, args, opts DownloadOptions, cb ProcessCallbacks)` and `(ctx, req DownloadRequest, opts DownloadOptions, cb ProcessCallbacks)` respectively. `runYtDlp`'s call site now constructs `DownloadOptions{AutoRetry: app.ui.autoRetry.Checked, Index: index, Total: total}`.*

### Phase 3 — Complete PPEngine (can overlap with Phase 2)

Phase 3 is independent of Phase 2 and can proceed in parallel.

1. ~~**PPEngine step 1**~~ — *Done. `PostProcessSettings` value struct added to `postprocess.go` along with the `newPostProcessSettings(ui *UIWidgets) PostProcessSettings` translator. `buildPostProcessFilters`, `computeProcessingLoad`, and `checkPostProcessingEnabled` — all three read the same widget fields — converted to free functions taking `PostProcessSettings` instead of a `*DownloaderApp` receiver. Call sites in `download.go` and `ui.go` now build the settings via `newPostProcessSettings(app.ui)`.*
2. ~~**PPEngine steps 4 & 5**~~ — *Done. `runJob`'s GPU semaphore/watchdog bookkeeping extracted into a dedicated `gpuJobGuard` type (`newGPUJobGuard`, `arm`, `pet`, `release`) in `pp_engine.go`; `runJob` itself now only builds/starts/streams/waits on the ffmpeg process and decides whether to retry on CPU, with a short phase-list doc comment added. Also fixed a `cmd.Start()` failure on a GPU job silently not retrying on CPU, unlike the other two failure paths. `ApplyFilters`'s nested `computeOutputFrameCount(..., probeFrameCount(...), ...)` call split into two named locals (`frameCount`, `totalFrames`).*

### Phase 4 — LogService follow-on (after Phase 2 step 3)

- ~~**LogService step 2**~~ — *Done. `SessionConfig` plain struct and `newSessionConfig(ui *UIWidgets, urls []string, savePath, trimStart, trimEnd string) SessionConfig` added to `log_service.go`, embedding the existing `PostProcessSettings` for its post-process fields. `LogService.WriteSessionConfig(cfg SessionConfig, writeFn func(string, color.Color))` ports the exact line sequence from the removed `logSessionConfiguration`. `download.go`'s `startDownload` now builds the config via `newSessionConfig(app.ui, ...)` and calls `app.logSvc.WriteSessionConfig(cfg, app.appendOutput)`.*

### Phase 5 — UIManager migration (after Phases 2, 3, and 4)

All steps depend on the preceding phases. Execute in order; each step shrinks the callback surface for the next.

1. ~~**UIManager step 1**~~ — *Done. `showPreferences`, `savePreferences`, `resetPreferences`, and `rebuildUI` moved to `UIManager` in `ui_manager.go`. `UIManager` gained `ui *UIWidgets`, `prefSvc *PreferenceService`, `logSvc *LogService`, and a temporary `onCreateUI func()` callback (removed again once `createUI` itself moved in step 4), all wired in `newDownloaderApp`. `applyPreferencesToWidgets` was converted from a `DownloaderApp` method into a package-level free function `applyPreferencesToWidgets(ui *UIWidgets, p AppPreferences)` since it only ever read `ui`. `DownloaderApp.showPreferences` and `DownloaderApp.savePreferences` are now one-line delegates to `UIManager`.*
2. ~~**UIManager step 2**~~ — *Done. `showPostProcessing` moved to `UIManager` in `ui_manager.go`, using the `ui` and `prefSvc` fields already added in step 1 — no new fields were needed since `computeProcessingLoad`/`newPostProcessSettings` were already free functions. `DownloaderApp.showPostProcessing` is now a one-line delegate to `UIManager`.*
3. ~~**UIManager step 3**~~ — *Done. `createMainMenu` moved to `UIManager` in `ui_manager.go`, using the `showHistory`/`showPreferences`/`showConfigHelp`/`showAbout`/`showPostProcessing` methods already living there and `manager.mainWindow` in place of `app.window`. `DependencyService` step 1 folded in: the `checkDependencies` / `runUpdateInUI` wrappers were removed from `DownloaderApp` and re-implemented directly as `UIManager` methods, backed by a new `depSvc *DependencyService` field (same dual-ownership pattern as `historySvc`) plus three injected callbacks (`onLog`, `onStatus`, `onSetStatusIndicator`) wired to `DownloaderApp.appendOutput`/`updateStatus`/`setStatusIndicator` in `newDownloaderApp`. `main.go`'s startup sequence now calls `dlApp.uiManager.createMainMenu()` and `dlApp.uiManager.checkDependencies()`.*
4. ~~**UIManager step 4**~~ — *Done. `createUI` moved to `UIManager` in `ui_manager.go`, using `manager.ui`/`manager.prefSvc`/`manager.mainWindow` in place of `app.ui`/`app.prefSvc`/`app.window`. Three new callbacks (`onStartDownload`, `onOpenFolder`, `onRequestCancel`) replaced the direct `app.startDownload()`/`app.openDownloadFolder()`/`app.RequestCancel()` calls, wired in `newDownloaderApp` alongside the existing `onLog`/`onStatus`/`onSetStatusIndicator`. The now-redundant `onCreateUI` callback was removed — `showPreferences`'s `OnSubmit` and `rebuildUI` call `manager.createUI()` directly. `DownloaderApp.createUI` was removed entirely (no delegate kept); `main.go` calls `dlApp.uiManager.createUI()` directly, matching the `createMainMenu` precedent. `ui.go` is now just thin `DownloaderApp` delegates plus the shared `roundedCard` helper (all delegates except `clearTerminalOutput` were later deleted as dead code; see 7.1).*
5. ~~**UIManager step 5**~~ — *Done. The `historySvc`, `depSvc`, `prefSvc`, and `logSvc` fields removed from `UIManager` and replaced with 10 injected callbacks (`onLoadHistory`, `onClearHistory`, `onCheckDependencies`, `onRunUpdate`, `onLoadPreferences`, `onSavePreferences`, `onResetPreferences`, `onLoadConfigFile`, `onMergeConfig`, `onSetLogBufferLimit`), matching the signatures of the service methods they replace. `newDownloaderApp` in `main.go` now wires each callback directly to the corresponding service method (e.g. `dlApp.uiManager.onLoadHistory = dlApp.historySvc.Load`) instead of handing `UIManager` the service instance itself. The `ui *UIWidgets` field was kept as-is since it is a widget bag, not a service. `NewUIManager`'s signature was unchanged — it already took no service-type parameters.*

### Phase 6 — Final cleanup (after Phase 5)

- ~~**High Priority: Group UIWidgets**~~ — *Done. `UIWidgets` split into `DownloadControls`, `PreferenceControls`, and `PostProcessControls`, each with its own constructor; `NewUIWidgets()` composes them. All call sites migrated to `ui.download.*`/`ui.prefs.*`/`ui.postProcess.*`.*
- ~~**High Priority: Refactor ui.go**~~ — *Done. Menus (`createMainMenu`) and dialogs (the five `show*` methods) were already extracted in Phase 5; the remaining piece was `createUI` itself, split into `buildHeader`, `configureEntryMode`, `wireToggleHandlers`, `loadMainWindowState`, `wireActionButtons`, `buildInputCard`, `buildStatusCard`, `buildLogPane`, and `buildFooter` in `ui_manager.go`, plus a shared `accentBar()` helper added next to `roundedCard` in `ui.go`. `createUI` is now a ~20-line composition of these helpers.*
- ~~**LogService step 3**~~ — *Done. `DownloaderApp` gained an `onLogLine func(line string, col color.Color)` field, wired in `main.go` to `uiManager.appendLogLine`. `appendOutput` in `helpers.go` now only handles file I/O (`logSvc.WriteToFile`/`WriteToErrorLog`) and calls `app.onLogLine(line, col)` for the UI part instead of touching `app.ui` directly. `appendLogLine(line, col)` — the `fyne.Do` block that adds the line, trims the buffer, and scrolls — moved to `ui_manager.go`, reading the buffer limit via a new `onLogBufferLimit func() int` callback (`LogService.BufferLimit`) instead of a direct service reference. `canvas`/`theme` imports removed from `helpers.go`.*
- ~~**Update documentation**~~ — *Done. `classes.puml` gained a new "GPU Capability Service" package (`GPUCapabilityService`, `GPUBackend` enum, `BackendCapability`, `EncoderPlan`, the free-function `PlanEncoder` shown as a stereotyped class), plus `DownloaderApp.gpuSvc`, `PPEngine`'s `GPUBackend`/`GPUCapabilities`/`gpuSem` fields and `buildFFmpegArgsForBackend`, `PostProcessJob.usedGPU`, `UIWidgets.postProcess.gpuBackend`, and `AppPreferences.GPUBackend`, tied together with new relationship arrows and annotation notes. `sequence-full.puml` gained the background `startGPUDetection()` call in the Startup group and a GPU-aware post-processing worker-pool flow (`PlanEncoder` → `buildFFmpegArgsForBackend` → `gpuJobGuard` acquire/arm/pet/release → `retryWithCPU` fallback on GPU failure). `architecture.md` §4.5 now documents the GPU job lifecycle (`gpuSem`, `gpuJobGuard`, `retryWithCPU`) and §4.11 documents `EncoderPlan`/`PlanEncoder` and the UI/preference wiring, replacing the stale "no encode-path integration yet" note; the `DownloaderApp` field table also gained `gpuSvc`.*

---

## High Priority

- [x] Refactor ui.go into smaller helpers — Split the large window construction into helpers for menus, dialogs, history, and preferences so the file is easier to scan and change. *(`showAbout`, `showHistory`, `showConfigHelp`, `showPreferences`, `showPostProcessing`, `createMainMenu`, and `createUI` have all moved to UIManager; `ui.go` is left with only the `clearTerminalOutput` delegate (used by `download.go`) plus the shared layout helpers `roundedCard`, `accentBar`, `sectionHeader`, `sectionDivider`, and `fixedWidth`; the other `DownloaderApp` delegates had no callers and were deleted (see 7.1). `createUI` itself is now split into 9 focused helpers — see Phase 6.)*
- [X] Split download.go into phases — Separate yt-dlp argument building, process startup, output parsing, and retry handling into smaller functions. *(`BuildArgs`, the retry loop, `FinalizeFiles`, and their composition are all in `DownloadEngine` now ✓ (`engine.Run`). `download.go`'s `runYtDlp` is now a ~40-line thin wrapper: build `DownloadRequest`/`DownloadOptions` from UI state, call `engine.Run`, then hand off to `recordHistory` and `reportDownloadResult`. The COMPLETE/ABORTED summary is built by `logDownloadSummary` and the pure, table-tested `describeOutputFormat` (see 5.3). `Execute()`'s argument count is also resolved ✓ (`DownloadOptions`). DownloadEngine has no further open steps.)*
- [x] Break postprocess.go into smaller pipelines — Move FFmpeg option building, UI state handling, and feature-specific logic into smaller functions or separate files. *(`PPEngine` owns filter execution. Probe functions and `buildFFmpegArgs`/`patchThreadCount` moved to `pp_engine.go` ✓. `buildPostProcessFilters`, `computeProcessingLoad`, and `checkPostProcessingEnabled` decoupled from `*UIWidgets` via the `PostProcessSettings` value struct ✓. `postprocess.go` is now settings + pure functions + a thin `applyFFmpegFilters` wrapper + shared format helpers.)*
- [x] Use context.Context consistently for cancellation — Pass context through the download pipeline so stopping a job does not leave background work running. *(Context flows correctly through `startDownload` → `runYtDlp` → `DownloadEngine.Execute` → `PPEngine.ApplyFilters`. Resolved as a side-effect of the service extractions.)*
- [x] Group UIWidgets into smaller structs — Break the large UIWidgets type into smaller feature-specific structs like download controls and preferences controls. *(`UIWidgets` now holds three sub-structs: `DownloadControls` (main window input/status/log widgets), `PreferenceControls` (Preferences dialog), and `PostProcessControls` (Post-Processing dialog plus the main-window master toggle). Each has its own `New*Controls()` constructor; `NewUIWidgets()` composes all three. All ~300 call sites across `download.go`, `helpers.go`, `log_service.go`, `postprocess.go`, and `ui_manager.go` updated to the new `ui.download.*`/`ui.prefs.*`/`ui.postProcess.*` paths.)*
- [x] Keep main.go thin — Use main.go as a bootstrapper only, and move app-specific setup into smaller constructors or services. *(`main()` is already a clean bootstrapper. `newDownloaderApp()`'s ~50-line inline widget literal collapsed to a single `ui: NewUIWidgets()` call now that "Group UIWidgets into smaller structs" is done.)*

## Medium Priority

- [x] Centralize preference loading — Move preference reads and default values into a small settings-loading layer so UI code stays focused on layout and event wiring. *(`PreferenceService` done.)*
- [x] Extract shared window-focus logic — Create one helper for the repeated focus-or-create pattern so every dialog and tool window behaves consistently. *(Added `focusOrCreate(win *fyne.Window) bool` and `onWindowClosed(win *fyne.Window) func()` to `ui_manager.go`; applied across all 5 singleton show methods in `ui_manager.go` and `ui.go`.)*
- [x] Replace hard-coded post-processing thresholds with constants — Name the thresholds and cost values so the code self-documents what each value means and is easier to tune later. *(Added 16 `cost*` constants and 4 `loadThreshold*` constants to `postprocess.go`; all magic numbers in `computeProcessingLoad` replaced. Block thresholds remain inline in `ui_manager.go` within `showPostProcessing()`, with a cross-reference comment to the load scale.)*
- [x] Keep LogManager focused on one job — Separate file appending and log persistence from mutex and error-handling details if the type grows further. *(`LogService` extracted; `LogManager` removed.)*
- [x] Move history handling behind a service boundary — Keep storage and schema changes away from the UI so history can evolve without touching the main window code. *(`HistoryService` done.)*
- [x] Keep log parsing tolerant — Treat yt-dlp output parsing as best-effort so small wording changes do not break downloads. *(Already satisfied: all parsing uses `strings.Contains` / `strings.CutPrefix` / `strings.Fields` with silent fallbacks. No parse failure can interrupt a download — worst case is a wrong progress value or incorrect format label in the summary.)*

## Low Priority

- [x] Organize helpers.go by purpose — Split helpers into groups like parsing, filesystem, UI, and formatting so the file does not become a dumping ground. *(Four named sections plus General: File I/O, UI updates, Preference management, and External tools.)*
- [x] Make helper functions narrowly named and testable — Use descriptive helper names and prefer deterministic helpers for time, byte, and formatting logic so they are easy to test. *(Extracted pure `parseAppConfig([]byte)` and `isValidOption(string, []string)` helpers; config-file loading now belongs to `PreferenceService.LoadFromFile`, and the obsolete `loadConfigFile` helper was removed.)*
- [x] Keep theme code isolated and reusable — Keep theme colors and helpers separate from UI construction, and use named constants or helpers for repeated colors. *(Added 12 named colour vars to theme.go (`colSystem`, `colInfo`, `colError`, `colWarning`, `colSuccess`, `colSuccessBorder`, `colDebug`, `colDotIdle`, `colDotSuccess`, `colDotFailed`, `colDotCanceled`, `colDotProcessing`); replaced ~70 inline `color.RGBA{...}` literals across 8 files; normalised the stray `{255,160,0}` to `colWarning`; removed unused `image/color` import from main.go.)*
- [x] Isolate icon and embedded asset code — Keep generated or embedded asset files separate from application logic so they stay predictable and easier to update. *(`icons.go` and `embedded_icon.go` were already well-isolated. Fixed the raw `"themeMode"` string in `themedIcon()` to use the `prefThemeMode` constant; added a comment linking `svgFillLight` to `accentCyan` in `theme.go` to prevent them drifting.)*
- [x] Preserve platform-specific wrappers — Keep Windows and non-Windows process handling in dedicated build-tag files so the rest of the app can stay cross-platform and simple. *(Extracted `openFolderCommand(path string) *exec.Cmd` into `sys_windows.go` (Explorer) and `sys_others.go` (open/xdg-open); `openDownloadFolder` in helpers.go is now a 3-line wrapper. `helpers.go` still imports `os/exec` for `exec.ExitError` in `exitCodeFromError()`. The `.exe` suffix check remains in `dependency_service.go`, and default-format UI logic is in `ui_manager.go` (`loadMainWindowState`); both are policy logic rather than process wrappers.)*

---

## Component Status

Breaking down the `DownloaderApp` "God Object" into specialized components:

- [x] **DownloadEngine** — yt-dlp execution, retries, cancellation, and progress parsing.
- [x] **PPEngine** — FFmpeg filter composition, crop detection, worker pool orchestration, and post-process execution.
- [x] **UIManager** — secondary window lifecycle (About, Help, History, Prefs, PP).
- [x] **PreferenceService** — preference load/save/reset logic and defaults.
- [x] **HistoryService** — download history persistence, schema evolution, and lookup helpers.
- [x] **LogService** — session log/error log routing, rotation policy, and structured log helpers.
- [x] **DependencyService** — binary discovery, dependency checks, and updater command execution.
- [x] **GPUCapabilityService** — FFmpeg GPU backend detection, capability caching, and encoder-plan resolution (see [gpu-acceleration.md](gpu-acceleration.md)).
- [x] **Update documentation** — architecture.md, classes.puml, and sequence diagrams fully reflect the extracted architecture.

See the sections below for per-component details and open next steps.


## DownloadEngine

**Done:** `DownloadEngine` struct introduced in `download_engine.go`. It owns the yt-dlp and ffmpeg binary paths and exposes four methods: `BuildArgs(DownloadRequest) DownloadArgs` (pure argument construction, no I/O), `Execute(ctx, args, opts DownloadOptions, ProcessCallbacks) (scanResult, error)` (retry loop with exponential backoff), `FinalizeFiles(savePath, downloadID string, onLog func(string, color.Color)) []string` (globs, strips the temp token, and renames each output to its final conflict-free path via the private `uniquePath` helper), and `Run(ctx, req, opts DownloadOptions, ProcessCallbacks) DownloadResult` (composes the three above into the full lifecycle of a single URL, reading no UI state). `DownloadOptions{AutoRetry bool; Index, Total int}` bundles the retry policy and this URL's position within a batch — the three runtime options `Execute` and `Run` share. `ProcessCallbacks` bridges log, status, and progress events back to the UI without Fyne imports. Its private `watchOutput`/`parseProgress` methods (moved from `DownloaderApp`, now living in `logscanner.go`) own all yt-dlp output scanning and report progress via `OnProgress(pct float64, size string)` instead of writing to `app.stats` directly. Plain output lines are reported with a `nil` colour, and `UIManager.appendLogLine` substitutes the theme foreground, so neither `download_engine.go` nor `logscanner.go` imports Fyne (see 5.2). `download.go`'s `runYtDlp` is now a thin wrapper: it builds a `DownloadRequest` and `DownloadOptions` from UI state, calls `engine.Run`, and passes the returned `DownloadResult` to two helpers for the app-specific side effects. `recordHistory` appends the history entries, and `reportDownloadResult` writes the completion/failure report. That report is built by `logDownloadSummary` and the pure `describeOutputFormat(extension, scan)`, which produces the "WEBM+M4A → MP4 (remuxed)" format line.

**Next steps:**

1. ~~**Move `watchOutput` / `parseProgress` out of `DownloaderApp`**~~ — *Done. Both are now private methods on `DownloadEngine` in `logscanner.go`, taking a `ProcessCallbacks` parameter. `ProcessCallbacks.WatchOutput` was removed; `Execute` calls `engine.watchOutput` directly, and a new `OnProgress(pct float64, size string)` field lets the caller update its own progress bar and `DownloadStats` without the engine touching UI state.*

2. ~~**Move `finalizeDownloadedFiles` to `DownloadEngine`**~~ — *Done. `FinalizeFiles(savePath, downloadID string, onLog func(string, color.Color)) []string` and the `uniquePath` helper it depends on moved from `postprocess.go` to `download_engine.go`. `runYtDlp` now calls `engine.FinalizeFiles(savePath, downloadID, app.appendOutput)` instead of `app.finalizeDownloadedFiles(...)`.*

3. ~~**Move `runYtDlp` to `DownloadEngine`**~~ — *Done. `Run(ctx, req DownloadRequest, autoRetry bool, index, total int, ProcessCallbacks) DownloadResult` added, composing `BuildArgs` + `Execute` + `FinalizeFiles` with no remaining UI state reads. `DownloaderApp.runYtDlp` is now a thin wrapper around it.*

4. ~~**Refactor `execute`**~~ — *Done. `DownloadOptions{AutoRetry bool; Index, Total int}` bundles the three loose params `Execute` and `Run` shared. Both methods now take `(ctx, ..., opts DownloadOptions, cb ProcessCallbacks)` — down from 6 params each to 4.*

## PPEngine

**Done:** `PPEngine` struct introduced in `pp_engine.go`. It owns the ffmpeg and ffprobe binary paths and exposes `ApplyFilters(ctx, filePaths, vfFilters, afFilters, PPCallbacks)`. `PPCallbacks` bridges log, status, and failure events to the UI. Private methods `detectCropFilter`, `resolveAutoCrop`, `runJob`, and `retryWithCPU` are fully engine-owned, and every terminal job failure goes through the shared `failJob` helper (see 4.4). The probe helpers (`probeFrameCount`, `probeDuration`, `computeOutputFrameCount`, `parseRationalFPS`) and the argument builder `buildFFmpegArgs` moved from `postprocess.go` to `pp_engine.go` as private methods, dropping their explicit `ffprobePath` parameters. `patchThreadCount` was later deleted: the per-job thread count is now passed straight into `buildFFmpegArgsForBackend` (see 6.9). `postprocess.go` is now a thin layer. It holds the `PostProcessSettings` value struct, whose `newPostProcessSettings(ui)` translator moved to `ui_snapshot.go` (see 5.4). It also holds the free functions `buildPostProcessFilters` and `computeProcessingLoad` (both take `PostProcessSettings`, no `*UIWidgets` reads; the test-only `checkPostProcessingEnabled` was deleted in 7.1), the `applyFFmpegFilters` wrapper, and shared format/scan helpers (`formatFFmpegProgress`, `formatBytes`, `formatDuration`, `filterShortName`, `scanCRLF`, `lastLine`). `runJob` uses these helpers, and `lastLine` is also used by `gpu_capability.go` to extract a probe's failure reason. GPU acceleration for the final encode step has since been layered on: `PPEngine.GPUBackend`/`GPUCapabilities` feed `PlanEncoder` inside `buildFFmpegArgsForBackend`, and `runJob` delegates its GPU semaphore/watchdog bookkeeping to a dedicated `gpuJobGuard` type (`newGPUJobGuard`, `arm`, `pet`, `release`), falling back to `retryWithCPU` on failure for GPU-encoded jobs (see [gpu-acceleration.md](gpu-acceleration.md) §6/§10).

**Next steps:**

1. ~~**Move `buildPostProcessFilters` out of `DownloaderApp`**~~ — *Done. `PostProcessSettings` (plain fields, no Fyne references) added to `postprocess.go`, along with `newPostProcessSettings(ui *UIWidgets) PostProcessSettings`. `buildPostProcessFilters`, `computeProcessingLoad`, and `checkPostProcessingEnabled` are now free functions taking `PostProcessSettings` — the `*DownloaderApp` receiver was dropped from all three since none of them need app state. `download.go` and `ui.go` call sites build the settings via `newPostProcessSettings(app.ui)` before calling in.*

2. ~~**Move probe functions to `PPEngine`**~~ — *Done. `probeFrameCount`, `probeDuration`, `computeOutputFrameCount`, and `parseRationalFPS` are now private methods on `PPEngine` in `pp_engine.go`. The explicit `ffprobePath` parameter was replaced by `engine.FFprobePath` throughout.*

3. ~~**Move `buildFFmpegArgs` and `patchThreadCount` to `PPEngine`**~~ — *Done. Both are now private methods on `PPEngine` in `pp_engine.go`. Call sites in `ApplyFilters` updated to use the `engine.` receiver.*

4. ~~**Refactor `runJob()`**~~ — *Done. The GPU semaphore/watchdog bookkeeping (previously an inline `releaseGPU` closure plus a raw `*time.Timer`) moved into a dedicated `gpuJobGuard` type (`newGPUJobGuard`, `arm`, `pet`, `release`) defined next to `maxConcurrentGPUJobs`/`gpuStallTimeout`. `runJob` now only builds/starts/streams/waits on the ffmpeg process and decides whether to hand off to `retryWithCPU`; `retryWithCPU` itself is unchanged and stays a separate method (business-logic fallback, not resource lifecycle). Also fixed: a GPU job's `cmd.Start()` failure previously logged-and-returned instead of retrying on CPU like the other two failure paths — it now calls `guard.release()` + `engine.retryWithCPU(...)` for consistency. A short phase-list doc comment was added to `runJob` covering the "document it better" half of this step.*

5. ~~**One operation per line**~~ — *Done. The nested `engine.computeOutputFrameCount(ctx, inputPath, engine.probeFrameCount(ctx, inputPath), activeVF)` call in `ApplyFilters`'s job-build loop split into `frameCount := engine.probeFrameCount(...)` followed by `totalFrames := engine.computeOutputFrameCount(..., frameCount, ...)`.*

## UIManager

**Done:** `UIManager` struct introduced in `ui_manager.go`. It owns the primary window reference (`mainWindow`) and the five singleton window fields (`aboutWindow`, `helpWindow`, `historyWindow`, `prefsWindow`, `ppWindow`) previously scattered on `DownloaderApp`. The five self-contained show methods (`showAbout`, `showHistory`, `showConfigHelp`, `showPreferences`, `showPostProcessing`) have moved to `UIManager`. Their one-line `DownloaderApp` delegates had no callers and were deleted (see 7.1). `UIManager` also gained `ui *UIWidgets` (plus the `savePreferences` and `restoreDefaults` helpers it needs; `restoreDefaults` replaced the original `resetPreferences`/`rebuildUI` pair) — wired in `newDownloaderApp`. `createMainMenu` has also moved here, along with `checkDependencies` and `runUpdateInUI` (previously thin `DownloaderApp` wrappers around `DependencyService`). `createUI` itself — the main window layout — has also moved here, backed by three further injected callbacks: `onStartDownload`, `onOpenFolder`, and `onRequestCancel`. The temporary `onCreateUI func()` callback was removed once `createUI` became a native `UIManager` method; internal recursion (batch-mode toggle) and other callers (`showPreferences`'s `OnSubmit`, `rebuildUI`) now call `manager.createUI()` directly. All previously direct service fields (`historySvc`, `depSvc`, `prefSvc`, `logSvc`) have since been replaced by injected callbacks — see UIManager step 5 below.

**Next steps:**

1. ~~**Move `showPreferences` to UIManager**~~ — *Done. See above.*

2. ~~**Move `showPostProcessing` to UIManager**~~ — *Done. See above.*

3. ~~**Move `createMainMenu` to UIManager**~~ — *Done. `createMainMenu`, `checkDependencies`, and `runUpdateInUI` are now `UIManager` methods, backed at the time by a new `depSvc *DependencyService` field and the `onLog`/`onStatus`/`onSetStatusIndicator` callbacks described under DependencyService below (the `depSvc` field itself was later removed in UIManager step 5).*

4. ~~**Move `createUI` to UIManager**~~ — *Done. See Phase 5 UIManager step 4 above.*

5. ~~**Remove direct service references from UIManager**~~ — *Done. The `historySvc`, `depSvc`, `prefSvc`, and `logSvc` fields removed from `UIManager` and replaced with 10 injected callbacks (`onLoadHistory`, `onClearHistory`, `onCheckDependencies`, `onRunUpdate`, `onLoadPreferences`, `onSavePreferences`, `onResetPreferences`, `onLoadConfigFile`, `onMergeConfig`, `onSetLogBufferLimit`), matching the signatures of the service methods they replace. `newDownloaderApp` in `main.go` now wires each callback directly to the corresponding service method (e.g. `dlApp.uiManager.onLoadHistory = dlApp.historySvc.Load`) instead of handing `UIManager` the service instance itself. The `ui *UIWidgets` field was kept as-is since it is a widget bag, not a service. `gpuSvc` never ended up needed on `UIManager` since `showPostProcessing` only reads `ui.postProcess.gpuBackend`/`prefs.GPUBackend`, never `GPUCapabilityService` directly. `NewUIManager`'s signature was unchanged — it already took no service-type parameters.

## HistoryService

**Done:** `HistoryService` struct introduced in `history_service.go`. It owns the path to `download_history.json` and exposes three methods: `Load() ([]DownloadHistoryEntry, error)` (reads all entries, tolerant of missing file), `AppendAll(rec DownloadRecord) error` (builds and persists one entry per output file in a single write), and `Clear() error` (resets to an empty array). The private `buildEntries` helper and `inferOriginalTitle` moved onto the service. All previous free functions (`historyFilePath`, `loadDownloadHistory`, `appendDownloadHistory`, `clearDownloadHistory`, `buildDownloadHistoryEntries`) have been removed. `DownloaderApp` holds `historySvc *HistoryService`; `UIManager` uses injected `onLoadHistory`/`onClearHistory` callbacks so `showHistory` and its Clear button never touch file paths directly. `download.go` now passes a `DownloadRecord` to `app.historySvc.AppendAll(...)`.

**No open next steps** — `HistoryService` is fully extracted. Future work would be covered by the medium-priority roadmap item "Move history handling behind a service boundary", which is now complete.

> **Coupling note (resolved):** `UIManager` previously held a direct `historySvc *HistoryService` reference, meaning both `DownloaderApp` and `UIManager` owned the same instance. UIManager step 5 replaced it with `onLoadHistory`/`onClearHistory` callbacks, so `UIManager` no longer references `HistoryService` at all.

## LogService

**Done:** `LogService` struct introduced in `log_service.go`. It owns the session log file handle, two mutexes, the daily rotation policy (daily `YYYY-MM-DD` filename scheme), the UI buffer-limit value, and the session directory cached at log-open time. `DownloaderApp` holds `logSvc *LogService` (previously `log *LogManager`).

Extracted from `helpers.go` and `download.go`:
- `OpenSessionLog(dir string) (string, error)` — replaces the inline `os.OpenFile` + `app.log.file = file` block in `startDownload`.
- `CloseSessionLog()` — replaces the inline mutex + write + close + nil block in `startDownload`.
- `WriteToFile(line string)` — replaces the `app.log.mutex.Lock` / `fmt.Fprintf` / `Unlock` block inside `appendOutput`.
- `WriteToErrorLog(line string)` — replaces `appendErrorOutput` + `dailyErrorLogPath` in `helpers.go`. The `dir` parameter has been removed; the service now uses `sessionDir` cached at `OpenSessionLog` time.
- `SetBufferLimit(n int)` / `BufferLimit() int` — replace the `logBufferLimit` package-level global.
- `IsErrorLine(line string) bool` — replaces `isErrorLogLine` (package-level helper, no instance needed).
- `ParseBufferLimit(s string) int` — replaces `parseLogLimit` (package-level helper).
- `SessionLogPath(dir string)` / `ErrorLogPath(dir string)` — replaces the `dateStamp` + `filepath.Join` inline logic in both `startDownload` and `dailyErrorLogPath`.

`appendOutput()` remains on `DownloaderApp` since it drives history/error-log side effects, but it no longer touches `UIWidgets` directly: it delegates the widget mutation to the `onLogLine` callback (`UIManager.appendLogLine`) and all file I/O to `logSvc`. `logSessionConfiguration()` has been removed — its formatting logic now lives in `LogService.WriteSessionConfig`, driven by the plain `SessionConfig` struct.

**Next steps:**

1. ~~**Cache the active session dir on `LogService`**~~ — *Done. `LogService` now stores `sessionDir` (set at `OpenSessionLog`, cleared at `CloseSessionLog`). `WriteToErrorLog` resolves the directory internally; `appendOutput` no longer reads `app.ui.path.Text` outside `fyne.Do`.*

2. ~~**Extract `logSessionConfiguration` into a `SessionConfig` value struct**~~ — *Done. See Phase 4 above.*

3. ~~**`appendOutput` UI part needs a callback now that `UIManager` owns `createUI`**~~ — *Done. `DownloaderApp.onLogLine func(line string, col color.Color)` wired in `main.go` to `uiManager.appendLogLine`. `appendOutput` calls `app.onLogLine(line, col)` instead of mutating `app.ui.download.logList`/`output` directly — the same bridging pattern `PPCallbacks.OnLog`/`ProcessCallbacks.OnLog` already use.*

## DependencyService

**Done:** `DependencyService` struct introduced in `dependency_service.go`. It owns `binDir` (resolved once at construction from the executable location) and exposes four members:

- `LocalPath(toolName string) string` — constructs the path inside `binDir`, appending `.exe` on Windows. Extracted from `getLocalBinPath` in `download.go`.
- `Resolve(toolName string) string` — returns the bundled path when it exists, otherwise the bare name for PATH lookup. Extracted from `resolvedBinPath` in `download.go`.
- `Check(onWarning func(string))` — verifies `yt-dlp` and `ffmpeg` are reachable and calls `onWarning` for each missing tool. Extracted from the body of `checkDependencies` in `helpers.go`.
- `RunUpdate(cb UpdateCallbacks)` — runs `yt-dlp -U` in a background goroutine. Extracted from the goroutine body of `runUpdateInUI` in `helpers.go`.

`UpdateCallbacks` is a bridge struct (`OnLog`, `OnStatus`, `OnSuccess`, `OnFailure`) with no Fyne dependency, following the same pattern as `PPCallbacks` and `ProcessCallbacks`.

The package-level `UpdateYtDlpCLI()` replaces the old `updateYtDlp()` free function used by the `--update` CLI flag.

`getLocalBinPath` and `resolvedBinPath` have been removed from `download.go` along with their `os`, `filepath`, and `runtime` imports. All callers (`runYtDlp`, `applyFFmpegFilters`) now use `app.depSvc.Resolve(...)`. The `checkDependencies()` and `runUpdateInUI()` wrappers have moved off `DownloaderApp` entirely and are now `UIManager` methods (see UIManager step 3 above), backed by the `onCheckDependencies`/`onRunUpdate` callbacks (see UIManager step 5) and the `onLog`/`onStatus`/`onSetStatusIndicator` callbacks.

**Next steps:**

1. ~~**Move `checkDependencies` and `runUpdateInUI` wrappers to `UIManager`**~~ — *Done. See UIManager step 3 above.*

2. ~~**Expose a `Version(toolName string) (string, error)` method**~~ — *Done. `Version` runs `<resolved tool path> --version` via `Resolve` and returns the trimmed output, or a wrapped error. Not yet wired into any UI — available for the "yt-dlp Auto-Update" roadmap feature (showing the installed version alongside the latest available).*

**No open next steps** — `DependencyService` is fully extracted.

## PreferenceService

**Done:** `PreferenceService` struct introduced in `preference_service.go`. It owns all preference key constants (`prefSavedPath`, `prefFormat`, etc.) and default value constants (`defaultThemeMode`, `defaultSmoothFPS`, etc.) that were previously scattered as inline string/numeric literals. `AppPreferences` is a plain value struct with no Fyne widget references — safe to construct and pass anywhere. `PreferenceService.Load()` reads the Fyne store and returns a fully-defaulted `AppPreferences`; `Save(AppPreferences)` writes it back with the savePrefs gate preserved; `Reset()` removes all managed keys in one call. `LoadFromFile(path)` reads and parses a `govid.json` override file; `MergeConfig(cfg, base, validFormats, validQualities)` validates and merges config fields onto `base` without touching any widget, returning the merged struct and any validation error strings. `DownloaderApp.prefSvc` is initialised in `newDownloaderApp`. `applyPreferencesToWidgets(ui *UIWidgets, p AppPreferences)` in `ui_snapshot.go` is the only writer of preference values into widgets. It is composed of `applyMainWindowPrefs`, `applyGeneralPrefs`, and `applyPostProcessPrefs`, and the Preferences and Post-Processing dialogs call their own group when they open (see 5.4 and 6.4). Saving goes the other way through `UIManager.savePreferences` in `ui_manager.go`. It snapshots the widgets into an `AppPreferences` via `snapshotPreferences` (also in `ui_snapshot.go`) and persists the result through the `onSavePreferences` callback (`PreferenceService.Save`). The former `DownloaderApp.savePreferences` delegate in `preference_service.go` was removed, and `startDownload` now calls `app.uiManager.savePreferences` directly (see 7.7). The former `resetPreferences`/`rebuildUI` pair is now a single `UIManager.restoreDefaults` in `ui_manager.go`. It calls `onResetPreferences`, reloads the cleared store to get the defaults, applies them with `applyPreferencesToWidgets`, resets the log buffer limit and the theme, and rebuilds the main window, so no default value is repeated in UI code (see 5.5). All raw `fyne.CurrentApp().Preferences()` reads have been removed from the UI code, along with the four direct `SetBool` writes in the `saveLog`, `notify`, `autoRetry`, and `enablePostProcess` `OnChanged` handlers. All four now route through `savePreferences`. The only remaining direct access to the store is the `NewPreferenceService(fyne.CurrentApp().Preferences())` construction in `newDownloaderApp`; `main()` reads the startup theme through `prefSvc.Load()` (see 5.1).

**Next steps:**

1. ~~**Move `showPreferences` to UIManager**~~ — *Done. See Phase 5 UIManager step 1 above.*

2. ~~**Move `loadConfigFromFile` / `applyConfig` to `PreferenceService`**~~ — *Done. `LoadFromFile(path) (*AppConfig, error)` and `MergeConfig(cfg, base, validFormats, validQualities) (AppPreferences, []string)` added to `preference_service.go`. `loadConfigFile` and `applyConfig` removed from `helpers.go`. The "Load from Config" button in `ui.go` (now `ui_manager.go`) runs `UIManager.loadConfigFile`, which calls `onLoadConfigFile` and `onMergeConfig`, applies the merged result with `applyPreferencesToWidgets`, and persists it through `onSavePreferences` (see UIManager step 5). It never touches `prefSvc` directly. `applyPreferencesToWidgets` gained guarded writes for `Format`, `Quality`, and `SavedPath` (skipped when empty to preserve platform-specific defaults at startup).*

3. ~~**Remove the three inline `fyne.CurrentApp().Preferences().SetBool(...)` onChanged handlers**~~ — *Done. The `OnChanged` callbacks for `notify`, `autoRetry`, and `enablePostProcess` now call `app.savePreferences(app.ui.path.Text)`. A fourth handler (`saveLog`) that used a raw `"saveLog"` string rather than `prefSaveLog` was brought in line at the same time.*

## GPUCapabilityService

**Done:** `GPUCapabilityService` struct introduced in `gpu_capability.go`, following the same plain-struct-plus-cache pattern as the other extracted services. It owns `ffmpegPath` and a mutex-guarded cache keyed by `GPUBackend`, populated once per app run by `Detect(ctx) map[GPUBackend]BackendCapability` (checks `ffmpeg -encoders` output, then runs a short synthetic-frame probe per applicable backend). Later `Detect` calls return a copy of the cache; the test-only `Capability(backend)` accessor was deleted in 7.1. `PlanEncoder(requested, capabilities, containerExt) EncoderPlan` is a pure function that always resolves to a runnable `-c:v` argument set, falling back to the CPU encoder when the requested/auto backend is unavailable. `GPUBackendOptions()`/`GPUBackendFromLabel()` map the UI selector's OS-filtered labels to `GPUBackend` values, and `FormatGPUDiagnostics()` renders per-backend availability lines for the session log. `DownloaderApp` holds `gpuSvc *GPUCapabilityService`, constructed in `main.go`; `PreferenceService` owns the `gpuBackend` preference key/default; `PostProcessControls` gained a `gpuBackend *widget.Select` field (`UIWidgets.postProcess.gpuBackend`) for the Post-Processing window's "Encoder Backend" selector. See [gpu-acceleration.md](gpu-acceleration.md) for the full design.

**No open next steps** — the service itself is fully self-contained and requires no further extraction. Its consumers are done as well: PPEngine step 4 moved `runJob`'s GPU semaphore/watchdog bookkeeping into `gpuJobGuard`, and `showPostProcessing` never needed a direct `gpuSvc` reference since it only reads `ui.postProcess.gpuBackend`/`prefs.GPUBackend`.

> **Coupling note (resolved):** UIManager step 5 confirmed `UIManager` never needed a direct `gpuSvc *GPUCapabilityService` reference — `showPostProcessing` only touches `ui.postProcess.gpuBackend` and `prefs.GPUBackend`, both already decoupled via the `onLoadPreferences` callback.

## main.go

- [x] Potential race/coupling around cancellation function access — *Done. Direct reads/invocations of `cancelFn` were removed from UI/close handlers; callers now use `RequestCancel()`, and cancel callback access is synchronized behind a mutex on `DownloaderApp`.*

- [x] Non-idiomatic os.Exit(0) in main normal flow — *Done. The `-update` success path exits via `return`; `os.Exit(...)` remains only for non-zero error exit code propagation.*

- [x] Open question: strict single-cancel semantics — *Addressed. `RequestCancel()` atomically takes-and-clears the active cancel callback before invocation, preventing repeated concurrent invocations of the same cancel function from multiple UI paths.*

---

## Post-Refactor Audit Findings & Follow-Up Plan

An end-to-end audit of the codebase against this roadmap confirmed that the architectural extractions (`DownloadEngine`, `PPEngine`, `UIManager`, `PreferenceService`, `HistoryService`, `LogService`, `DependencyService`, `GPUCapabilityService`, `UIWidgets` grouping, and `main.go` bootstrapping) are implemented and functional, and all unit tests pass.

However, the audit identified three categories of items that were either missed during extraction, incorrectly marked as done, or left stale in diagrams and documentation. Use the checklist below to work through each fix sequentially.

---

### Category 1 — Preference Loading & Persistence Bypasses (Code)

Although `PreferenceService` was extracted and the four primary toggle handlers were migrated, raw access to `fyne.CurrentApp().Preferences()` and unexported magic string literals still remain in several areas.

#### 1.1 Fix `batchMode.OnChanged` in `ui_manager.go`
* ~~**File:** [`ui_manager.go`](../ui_manager.go#L843-L858)~~ — *Done. `batchMode.OnChanged` now calls `manager.savePreferences(ui.download.path.Text)` instead of the raw `Preferences().SetBool("batchMode", checked)` call; `BatchMode` is persisted via `savePreferences`'s existing `ui.download.batchMode.Checked` read.*

#### 1.2 Fix `ui.download.path.OnChanged` in `ui_manager.go`
* ~~**File:** [`ui_manager.go`](../ui_manager.go#L878-L880)~~ — *Done. `ui.download.path.OnChanged` now delegates to `manager.savePreferences(text)`, keeping path persistence under the centralized preference service flow.*

#### 1.3 Fix speed limit fallback in `download.go`
* ~~**File:** [`download.go`](../download.go#L244-L248)~~ — *Done. `runYtDlp()` now falls back to `app.prefSvc.Load().MaxSpeed` when the UI speed limit is empty, keeping preference access behind `PreferenceService`.*

#### 1.4 Centralize or document `themedIcon` preference read in `icons.go`
* ~~**File:** [`icons.go`](../icons.go#L78-L83)~~ — *Done. `themedIcon` now receives the resolved `ThemeMode` from `createUI` instead of reading the global Fyne preference store, keeping icon selection deterministic and independent of `PreferenceService`.*

---

### Category 2 — Architecture & Diagram Synchronization (Documentation)

The "Update documentation" task was marked done, but `classes.puml`, `sequence-full.puml`, and `architecture.md` are out of sync with the refactored code.

#### 2.1 Synchronize `docs/classes.puml`
* ~~**File:** [`docs/classes.puml`](classes.puml)~~ — *Done. Synchronized the diagram with the current grouped UI controls, coordinator/UIManager ownership and callbacks, download lifecycle value types, history and logging records, dependency versioning, GPU helper functions, and relationships.*

#### 2.2 Synchronize `docs/sequence-full.puml`
* ~~**File:** [`docs/sequence-full.puml`](sequence-full.puml)~~ — *Done. Startup now shows `UIManager` ownership for UI creation and dependency checks, missing dependencies are logged as warnings, session logging uses `LogService.OpenSessionLog`/`WriteSessionConfig`, and teardown calls `LogService.CloseSessionLog`.*

#### 2.3 Synchronize `docs/architecture.md`
* ~~**File:** [`docs/architecture.md`](architecture.md)~~ — *Done. Updated the `DownloaderApp` cancellation/logging fields, documented the grouped `UIWidgets` structure, and synchronized `HistoryService.AppendAll` plus its callback-based UIManager boundary.*

---

### Category 3 — Roadmap Text & Errata Corrections (Documentation)

Minor stale references and signature mismatches within [`refactor_roadmap.md`](refactor_roadmap.md) itself:

#### 3.1 Correct file name for HistoryService
* ~~**Done:** Changed `HistoryService`'s source reference from `history.go` to `history_service.go`.~~

#### 3.2 Correct `HistoryService.AppendAll` signature
* ~~**Done:** Updated the documented signature to `AppendAll(rec DownloadRecord) error`.~~

#### 3.3 Correct post-processing block thresholds location
* ~~**Done:** Updated the threshold location to `ui_manager.go` within `showPostProcessing()`.~~

#### 3.4 Correct section count and import claims for `helpers.go`
* ~~**Done:** Corrected the helpers section count, removed the stale `loadConfigFile` claim, retained the `os/exec` import note, and pointed default-format logic to `ui_manager.go`.~~

---

## Second Post-Refactor Audit (2026-10-03)

A second pass over the full codebase against this roadmap. `go build`, `go vet`, and `go test ./...` all pass. However, **`go test -race ./...` fails**: 42 data races are reported and 4 tests in `download_test.go` fail under the race detector. CI does not run with `-race`, which is why this has not surfaced. All items below are open. The categories are ordered by priority.

### Recommended order

1. **Category 4** (correctness) first. The races and behavioural bugs are real defects, and fixing them makes `-race` usable in CI.
2. **Category 5** (missed or incomplete items from the original roadmap). These are small and finish the work the roadmap already claims is done.
3. **Category 6** (new refactoring targets). Start with 6.1–6.3, the three longest remaining functions.
4. **Category 7** (dead code, stale comments, and tooling). Can be done any time, ideally batched into one cleanup commit.
5. **Category 8** (roadmap errata). Update the text once the code changes above have landed.

---

### Category 4 — Correctness & Concurrency (Code)

#### ~~4.1 Fix data races on progress state~~
* *Done (`7ae8bf2`). Every `DownloadStats` field is now guarded by a mutex and accessed only through methods (`setTarget`, `takeTarget`, `recordSize`, `reset`). `runProgressSmoother` in `helpers.go` tracks the displayed value itself, never reads the widget, and only calls `progress.SetValue` inside `fyne.Do`. CI runs `go test -race ./...` (see 7.6), and the suite passes under the race detector. A related race in the status-dot pulse was fixed in `aab615e`.*
* **Files:** [`download.go`](../download.go#L99-L125), [`helpers.go`](../helpers.go#L181-L198), [`types.go`](../types.go#L163-L168)
* **Issue:** The progress-smoother goroutine in `startDownload` reads `app.ui.download.progress.Value` outside `fyne.Do` and reads `app.stats.targetPct` without synchronization. Meanwhile `setProgress` (called from the yt-dlp output goroutine via `OnProgress`) and `setProgressNow` write `targetPct`. `updateProgress` also writes `lastSize`/`downloadedRaw`/`unit` from the engine goroutine, and `runYtDlp` reads them. This violates coding guidelines §2.3 and §2.5.
* **Fix:** Store `targetPct` as an atomic value, or guard `DownloadStats` with a mutex. Have the smoother track its own `current` value instead of reading the widget, and only call `progress.SetValue` inside `fyne.Do`. Then add `go test -race ./...` to CI (see 7.6).

#### ~~4.2 Fix the `LogService.bufferLimit` race~~
* *Done (`cbbca79`). `SetBufferLimit` and `BufferLimit` now take `svc.mutex`.*
* **File:** [`log_service.go`](../log_service.go#L143-L150)
* **Issue:** `SetBufferLimit` and `BufferLimit` access `bufferLimit` without the mutex. `WriteToFile` reads it under `svc.mutex` from background goroutines. Tests don't exercise this path, but it is a real race.
* **Fix:** Take `svc.mutex` in both accessors, or make the field an `atomic.Int64`.

#### ~~4.3 Reset `DownloadStats` between downloads~~
* *Done (`5b5d18d`). `DownloadStats.reset()` clears the size, downloaded amount, unit, and target, and requests a snap to 0. It is called at session start and before each batch item.*
* **Files:** [`download.go`](../download.go#L66-L69), [`download.go`](../download.go#L174-L178)
* **Issue:** Only `targetPct` is reset at session start and between batch items. `lastSize`, `downloadedRaw`, and `unit` carry over. If a later URL fails before reporting progress, its ABORTED/COMPLETE summary shows the previous download's size and average speed.
* **Fix:** Add a `DownloadStats.reset()` method (or assign a fresh `DownloadStats{}`) at both reset points.

#### ~~4.4 Report every post-processing failure through `OnFailure`~~
* *Done (`9138d3c`). The terminal failure paths in `runJob` go through a shared `failJob(job, cb, msg, output)` helper, which calls `cb.OnFailure()`, logs the output, and removes the temp file.*
* **File:** [`pp_engine.go`](../pp_engine.go#L254-L300)
* **Issue:** In `runJob`, the non-streaming fallback path (`CombinedOutput` error) and the CPU-job `cmd.Start()` failure both log and return without calling `cb.OnFailure()`. The streaming `Wait()` failure path does call it. So these two failure paths never turn the Download button into "Retry".
* **Fix:** Call `cb.OnFailure()` on every terminal failure path. Better, merge the failure handling into a single `failJob(job, cb, err, output)` helper so the three paths cannot drift apart again.

#### ~~4.5 Make the `-update` CLI path use the bundled yt-dlp~~
* *Done (`eee8dda`). `UpdateYtDlpCLI` became `(svc *DependencyService) UpdateCLI() error`, which runs `svc.Resolve("yt-dlp")`. `main()` calls `NewDependencyService().UpdateCLI()`.*
* **File:** [`dependency_service.go`](../dependency_service.go#L145-L158)
* **Issue:** `UpdateYtDlpCLI` runs the bare `"yt-dlp"` from PATH. The in-app `RunUpdate` uses `svc.Resolve("yt-dlp")`. For a packaged release, which ships `bin/yt-dlp.exe`, `GoVid.exe -update` therefore updates the wrong binary or fails.
* **Fix:** Construct a `DependencyService` in the CLI path and run `Resolve("yt-dlp")`. Simplest is to make it a method: `(svc *DependencyService) UpdateCLI() error`.

#### ~~4.6 Restore `smoothMotionFPS` when the Post-Processing window opens~~
* *Done (`f8b7c96`, superseded by 6.4). `showPostProcessing` now reloads the whole post-processing group through `applyPostProcessPrefs`, which includes `smoothMotionFPS`.*
* **File:** [`ui_manager.go`](../ui_manager.go#L527-L546)
* **Issue:** `showPostProcessing` re-applies every persisted post-processing preference to its widget except `SmoothFPS`. This is the same class of bug as the sharpen-intensity fix in `b26f422`.
* **Fix:** Add `ui.postProcess.smoothMotionFPS.SetValue(prefs.SmoothFPS)`. Ideally this is resolved by 6.4, which removes the duplicated reload list entirely.

#### ~~4.7 Write history atomically~~
* *Done (`43bcea9`). `AppendAll` writes to a temp file in the same directory and then `os.Rename`s it over `download_history.json`, so the "single atomic write" comment now holds.*
* **File:** [`history_service.go`](../history_service.go#L79-L94)
* **Issue:** The `AppendAll` doc comment says "single atomic write", but `os.WriteFile` truncates the file and then writes it. A crash or power loss mid-write corrupts `download_history.json`, and from then on every `Load` fails.
* **Fix:** Write to `download_history.json.tmp` and then `os.Rename` it over the original. Alternatively, correct the comment.

---

### Category 5 — Missed or Incomplete Roadmap Items (Code)

#### ~~5.1 `main.go` still reads the theme preference directly~~
* *Done (`b930ffa`). `main()` now applies the startup theme with `applyTheme(mainApp, dlApp.prefSvc.Load().ThemeMode)`. The only remaining direct store access is the `NewPreferenceService(fyne.CurrentApp().Preferences())` construction in `newDownloaderApp`.*
* **File:** [`main.go`](../main.go#L108-L116)
* **Issue:** This was missed by Category 1. `main()` calls `mainApp.Preferences().StringWithFallback(prefThemeMode, defaultThemeMode)` directly instead of going through `PreferenceService`.
* **Fix:** Construct the `PreferenceService` before `newDownloaderApp` (or have `newDownloaderApp` return the loaded prefs) and use `prefs.ThemeMode`. Combine this with 6.6 (`applyTheme`).

#### ~~5.2 `DownloadEngine` still imports Fyne~~
* *Done (`625059f`). Plain output lines are reported with a `nil` colour, and `UIManager.appendLogLine` substitutes the theme foreground. Neither `logscanner.go` nor `download_engine.go` imports Fyne.*
* **File:** [`logscanner.go`](../logscanner.go#L22)
* **Issue:** The roadmap and the `ProcessCallbacks` doc both say the engine reports events "without importing Fyne". But `watchOutput` imports `fyne.io/fyne/v2/theme` and calls `theme.ForegroundColor()`, a deprecated API, for plain output lines.
* **Fix:** Give these lines a named palette colour (e.g. reuse `colOutputLine`), or pass `nil` and let `appendLogLine` substitute the theme foreground. That puts the theme lookup on the UI side and removes the Fyne import from the engine.

#### ~~5.3 `runYtDlp` is not yet a thin wrapper~~
* *Done (`dd7ca23`). `runYtDlp` is now ~40 lines: it builds the request, calls `engine.Run`, then calls `recordHistory` and `reportDownloadResult`. The summary is built by `logDownloadSummary` and the pure `describeOutputFormat`, which has a table-driven test.*
* **File:** [`download.go`](../download.go#L239-L379)
* **Issue:** The roadmap describes `runYtDlp` as "a thin wrapper", but it is still ~140 lines. About 70 of those build the COMPLETE/ABORTED summary and the "WEBM+M4A → MP4 (remuxed)" format line inline inside `fyne.Do`.
* **Fix:** Extract a pure, table-testable `describeOutputFormat(extension string, scan scanResult) string` and a `logDownloadSummary(...)` helper. Then `runYtDlp` is just: build the request, call `engine.Run`, record history, report the result.

#### ~~5.4 Services still depend on `*UIWidgets`~~
* *Done (`c354052`). `newSessionConfig`, `newPostProcessSettings`, `applyPreferencesToWidgets`, and the new `snapshotPreferences` now live in `ui_snapshot.go`. No service or engine file references `*UIWidgets`.*
* **Files:** [`log_service.go`](../log_service.go#L181-L207), [`postprocess.go`](../postprocess.go#L79-L101)
* **Issue:** `newSessionConfig` and `newPostProcessSettings` are UI→value translators, but they live in service and engine files. As a result, `log_service.go` and `postprocess.go` can't be reasoned about, or moved into their own package later, without the widget bag.
* **Fix:** Move both translators (and `applyPreferencesToWidgets` from `helpers.go`) into one UI-side file, e.g. `ui_snapshot.go`, alongside `UIManager.savePreferences`, which already does the same widget→struct job for `AppPreferences`.

#### ~~5.5 Remaining hard-coded preference defaults~~
* *Done (`d240597`). `LogService` uses a shared `defaultLogBufferLimit` constant for both `NewLogService` and `ParseBufferLimit`. Restore Defaults is now `UIManager.restoreDefaults`, which calls `Reset()` and then applies `onLoadPreferences()`, so no default value is repeated in UI code.*
* **Files:** [`ui_manager.go`](../ui_manager.go#L418-L431), [`ui_manager.go`](../ui_manager.go#L505-L508), [`log_service.go`](../log_service.go#L35-L38), [`log_service.go`](../log_service.go#L262-L271)
* **Issue:** "Restore Defaults" hard-codes `"Dark"` and `"200"`, `resetPreferences` hard-codes `200`, and `NewLogService`/`ParseBufferLimit` each hard-code `200`. These duplicate `defaultThemeMode` and `defaultLogLimit` in `preference_service.go`.
* **Fix:** Use `defaultThemeMode` and `defaultLogLimit`, and add a `defaultLogBufferLimit = 200` int constant shared by `LogService`. Better still, have the reset handler call `applyPreferencesToWidgets(ui, manager.onLoadPreferences())` after `Reset()` so no defaults are repeated at all.

---

### Category 6 — New Refactoring Targets (Code)

#### ~~6.1 Split `startDownload`~~
* *Done (`71972a9`). `startDownload` is now a ~25-line composition. It calls `readSession`, which uses the pure, tested `collectURLs`, then `resetSession`, `openSessionLog`, `runProgressSmoother`, and `runSession`. `runSession` drives `runQueue`/`downloadItem`, `runPostProcessing`, and `notifyCompletion`, whose notification text comes from the pure `completionNotification`.*
* **File:** [`download.go`](../download.go#L26-L230)
* **Issue:** At ~200 lines, `startDownload` is now the longest function in the codebase. It handles URL collection, validation, UI reset, session-log setup, the progress smoother goroutine, the batch loop with per-item contexts, post-processing, and three notification variants.
* **Fix:** Extract:
  * `collectURLs(text string, batch bool) ([]string, error)`: pure and testable.
  * `resetSessionUI()`
  * `startProgressSmoother(ctx)`
  * `runQueue(ctx, urls, ...) []string`
  * `runPostProcessing(ctx, paths, filters)`
  * `notifyCompletion(...)`

  `startDownload` should become a short composition, the same way `createUI` became one in Phase 6.

#### ~~6.2 Split `showPostProcessing`~~
* *Done (`d11220c`). `showPostProcessing` now composes `applyPostProcessPrefs` (in place of `loadPostProcessState`; see 6.4), `wirePostProcessHandlers`, `buildLoadIndicator`, `buildPostProcessForm`, and `buildPostProcessFooter`. `sectionHeader` and `sectionDivider` moved to `ui.go`. The block thresholds are the package-level `loadBlockThresholds`, next to the `loadThreshold*` constants in `postprocess.go`.*
* **File:** [`ui_manager.go`](../ui_manager.go#L519-L782)
* **Issue:** At ~265 lines, this is the largest UI function left. It mixes widget state reload, enable/disable wiring for 13 controls, the load-indicator construction, form layout, and window setup.
* **Fix:** Apply the same treatment `createUI` got:
  * `loadPostProcessState(prefs)`
  * `wirePostProcessHandlers(refresh func())`
  * `buildLoadIndicator() (fyne.CanvasObject, func())`
  * `buildPostProcessForm()`
  * `buildPostProcessFooter()`

  Move the inline `sectionHeader`/`sectionDivider` closures next to `roundedCard`/`accentBar` in `ui.go`. Name the `blockThresholds` slice as a package-level constant next to the `loadThreshold*` constants.

#### ~~6.3 Split `showPreferences` and extract history formatting~~
* *Done (`c09910c`). `showPreferences` now uses `buildCookiesRow`, `confirmRestoreDefaults`/`restoreDefaults`, and `loadConfigFile`, which is the `onLoadConfig` flow. History text comes from the pure `formatHistoryEntries`, which has a table-driven test in `ui_manager_test.go`.*
* **File:** [`ui_manager.go`](../ui_manager.go#L217-L285), [`ui_manager.go`](../ui_manager.go#L349-L461)
* **Issue:** `showPreferences` (~110 lines) inlines the form, the cookie picker, Restore Defaults, and the whole "Load from Config" flow. `showHistory` builds its display text inline. That text is pure logic, and it was flagged in [audit_review.md](audit_review.md) ("String formatting in history display") but never actioned.
* **Fix:** Extract `buildCookiesRow`, `onRestoreDefaults`, and `onLoadConfig` from `showPreferences`. Add a pure `formatHistoryEntries([]DownloadHistoryEntry) string` with a table-driven test.

#### ~~6.4 Single source of truth for preference→widget application~~
* *Done (`d03ea61`). The format and quality `Options` are set in `NewDownloadControls`. `PreferenceService.Load()` resolves the platform defaults for save path and format. `applyPreferencesToWidgets` is the only writer: it composes `applyMainWindowPrefs`, `applyGeneralPrefs`, and `applyPostProcessPrefs`, and the two dialogs call their own group when they open.*
* **Files:** [`helpers.go`](../helpers.go#L204-L243), [`ui_manager.go`](../ui_manager.go#L357-L392), [`ui_manager.go`](../ui_manager.go#L527-L546), [`ui_manager.go`](../ui_manager.go#L893-L919)
* **Issue:** Preferences are written to widgets in four places with overlapping subsets: `applyPreferencesToWidgets`, the top of `showPreferences`, the top of `showPostProcessing`, and `loadMainWindowState`. The subsets have already drifted apart (see 4.6). At startup, `format`/`quality` are applied before their `Options` exist, so `newDownloaderApp` silently drops them and `loadMainWindowState` sets them a second time.
* **Fix:** Set the `Options` in the widget constructors (`NewDownloadControls`). Make `applyPreferencesToWidgets` the only writer, with platform defaults resolved in `PreferenceService.Load()` or a `resolveDefaults` step. Have the dialogs call it, or a per-group variant such as `applyPostProcessPrefs`.

#### ~~6.5 Centralize option lists and enum-like strings~~
* *Done (`338b92b`). The new `options.go` defines named constants and ordered option slices (`formatOptions`, `qualityOptions`, theme, log-limit, and post-processing mode labels). The widget constructors, preference defaults, the yt-dlp and FFmpeg argument builders, and the `showConfigHelp` text all reference them.*
* **Files:** [`ui_manager.go`](../ui_manager.go#L904-L917), [`download_engine.go`](../download_engine.go#L71-L107), [`history_service.go`](../history_service.go#L138), [`types.go`](../types.go#L91-L94), [`icons.go`](../icons.go#L79), [`postprocess.go`](../postprocess.go#L109-L182)
* **Issue:** Many string literals are repeated across files:
  * Format names (`"MP4"`, `"MP3"`…) and quality names (`"Best Quality"`, `"1080p"`…)
  * Theme names (`"Dark"`/`"Light"`)
  * Log-limit options
  * Post-processing mode labels (`"Fast"`, `"Balanced"`, `"NLMeans (HQ, slow)"`, `"4K (2160p)"`…). These are matched by string equality in `buildPostProcessFilters`, `computeProcessingLoad`, and the radio/select constructors.

  The format and quality lists are also restated in the `showConfigHelp` text. If a UI label is renamed in one place, filter selection silently falls through to the `default` branch.
* **Fix:** Define named constants plus option slices (e.g. `formatOptions`, `qualityOptions`, `themeDark`/`themeLight`, `smoothModeFast`…) in one place, and reference them from the constructors, the engines, and the help text.

#### ~~6.6 Deduplicate theme application~~
* *Done (`b930ffa`, together with 5.1). `applyTheme(app fyne.App, mode string)` in `theme.go` is the only `SetTheme` caller. It is used by `main()`, `submitPreferences`, and `restoreDefaults`.*
* **Files:** [`main.go`](../main.go#L108-L116), [`ui_manager.go`](../ui_manager.go#L406-L413), [`ui_manager.go`](../ui_manager.go#L510-L515)
* **Issue:** The `switch mode { case "Light": SetTheme(&lightTheme{}) default: SetTheme(&darkTheme{}) }` block appears three times.
* **Fix:** Add `applyTheme(app fyne.App, mode string)` in `theme.go`.

#### ~~6.7 Type the status-indicator states and deduplicate the pulse goroutine~~
* *Done (`36601e7`). `type StatusState int` with `StatusIdle`, `StatusActive`, `StatusProcessing`, `StatusSuccess`, `StatusFailed`, and `StatusCanceled`. Both pulsing states share `startStatusPulse(base color.RGBA)`, and `onSetStatusIndicator` now takes a `StatusState`.*
* **File:** [`helpers.go`](../helpers.go#L107-L177)
* **Issue:** `setStatusIndicator` takes free-form strings (`"active"`, `"processing"`, …) passed from `download.go` and `ui_manager.go`. Its `"active"` and `"processing"` branches are two copies of the same ~20-line pulse goroutine that differ only in colour.
* **Fix:** Add `type StatusState int` with named constants (or string constants), and extract `startPulse(base color.RGBA)`. Update the `onSetStatusIndicator` callback type to match.

#### ~~6.8 Name the auto-crop sentinel and the audio-only check~~
* *Done (`072b90b`). `const autoCropSentinel` lives in `postprocess.go`. `isAudioOnlyExt(ext)` in `options.go` is used by both `DownloadEngine.BuildArgs` and `PPEngine.ApplyFilters`.*
* **Files:** [`postprocess.go`](../postprocess.go#L166), [`pp_engine.go`](../pp_engine.go#L169-L199), [`pp_engine.go`](../pp_engine.go#L577-L578), [`download_engine.go`](../download_engine.go#L146)
* **Issue:** The `"__autocrop__"` magic string appears in three places across two files. The "mp3 or m4a means audio-only" rule is written out separately in `DownloadEngine.BuildArgs` and `PPEngine.ApplyFilters`.
* **Fix:** Add `const autoCropSentinel = "__autocrop__"` and `func isAudioOnlyExt(ext string) bool`.

#### ~~6.9 Thread count passed directly instead of patched~~
* *Done (`168525d`). The per-job thread count is computed before the jobs are built and passed into `buildFFmpegArgs`/`buildFFmpegArgsForBackend`. `patchThreadCount` was deleted.*
* **File:** [`pp_engine.go`](../pp_engine.go#L524-L562), [`pp_engine.go`](../pp_engine.go#L645-L649)
* **Issue:** `buildFFmpegArgs` emits a `-threads 0` placeholder that `patchThreadCount` rewrites later, both in `ApplyFilters` and in `retryWithCPU`. That is two passes over the argument slice and a hidden coupling between the builder and the patcher.
* **Fix:** Compute `threadsPerJob` before building jobs and pass it to `buildFFmpegArgsForBackend`. Then delete `patchThreadCount`.

#### ~~6.10 Move single-owner types next to their owners~~
* *Done (`5c8806a`). `AppConfig` moved to `preference_service.go` and `PostProcessJob` to `pp_engine.go`.*
* **File:** [`types.go`](../types.go#L193-L213)
* **Issue:** `AppConfig` belongs to `preference_service.go` and `PostProcessJob` belongs to `pp_engine.go`, but both live in `types.go`. Meanwhile every newer type (`DownloadRequest`, `SessionConfig`, `DownloadRecord`, …) is defined in its owner's file.
* **Fix:** Move both types. `types.go` then contains only the shared app/widget types.

#### ~~6.11 Rename `ppFailed`~~
* *Done (`2a688d4`). Renamed to `sessionFailed` and changed to an `atomic.Bool`.*
* **Files:** [`types.go`](../types.go#L187-L189), [`download.go`](../download.go#L328)
* **Issue:** `ppFailed` is also set when a *download* fails (`runYtDlp` stores 1 on yt-dlp failure). It means "any job in this session failed", so the name misleads.
* **Fix:** Rename it to `sessionFailed` (or `jobFailed`) and change it from `atomic.Int32` to `atomic.Bool`, since it is only ever 0/1.

---

### Category 7 — Dead Code, Stale Comments & Tooling

#### ~~7.1 Remove dead code~~
* *Done (`5e291ba`). Deleted the unused `DownloaderApp` delegates (only `clearTerminalOutput` remains), `ExitOK`/`ExitUnexpected`, the test-only `checkPostProcessingEnabled`, `LogService.IsActive`, and `GPUCapabilityService.Capability`, and the `atadenoise` case in `filterShortName`.*
* **Unused `DownloaderApp` delegates in [`ui.go`](../ui.go#L18-L49):** `showHistory`, `showPreferences`, `showConfigHelp`, `showAbout`, `showPostProcessing`, and `getPostProcessingButton` have no production callers. `getPostProcessingButton` is the only caller of `showPostProcessing`. Only `clearTerminalOutput` is used, by `download.go`. Roadmap Phase 6 describes these as "thin delegates", but they can simply be deleted.
* **`ExitOK`/`ExitUnexpected`** in [`types.go`](../types.go#L18-L25) are never referenced.
* **Test-only production code:** `checkPostProcessingEnabled` ([`postprocess.go`](../postprocess.go#L343-L351)), `LogService.IsActive` ([`log_service.go`](../log_service.go#L87-L92)), and `GPUCapabilityService.Capability` ([`gpu_capability.go`](../gpu_capability.go#L121-L132)) are only called from tests. Either wire each one in where it was intended (e.g. `hasPostProcess` in `startDownload` could use `checkPostProcessingEnabled`) or delete it.
* **`atadenoise`** case in `filterShortName` ([`postprocess.go`](../postprocess.go#L322)): no code path generates this filter any more.

#### ~~7.2 Fix stale file and doc comments~~
* *Done (`59a6daa`). Every comment in the table now matches the code. For the GPU diagnostics, the About-window claim was dropped; the diagnostics go to the session log only. The `history_service.go` comment is accurate since 4.7, and `coding_guidelines.md` §2.5 now names `LogService`.*
| Location | Stale claim | Reality |
|---|---|---|
| [`download.go`](../download.go#L1-L9) header | Builds yt-dlp args and streams/parses output | Both now live in `DownloadEngine` / `logscanner.go` |
| [`ui_manager.go`](../ui_manager.go#L41) | "owns references to all (non-main, for now) windows" | It owns the main window too |
| [`ui_manager.go`](../ui_manager.go#L50-L51) | `prefsWindow`/`ppWindow` "opened by DownloaderApp" | Opened by `UIManager` |
| [`types.go`](../types.go#L180) | `uiManager` "Owns secondary window state" | Owns main window layout too |
| [`types.go`](../types.go#L110) | denoise "ATADenoise = Fast" | Options are NLMeans / hqdn3d |
| [`postprocess.go`](../postprocess.go#L47) | "block indicator in ui.go" | `ui_manager.go` (`showPostProcessing`) |
| [`pp_engine.go`](../pp_engine.go#L35-L37) | GPU fields zero-value "until the 'Add a user setting' roadmap item…" | Wired in `applyFFmpegFilters` |
| [`gpu_capability.go`](../gpu_capability.go#L9-L11) | "No command-builder integration, user-facing settings, or fallback logic yet" | All three exist |
| [`gpu_capability.go`](../gpu_capability.go#L358-L360) | Diagnostics shown in "the About window's GPU Acceleration section" | `showAbout` has no GPU section — either add it or drop the claim |
| [`ui.go`](../ui.go#L38) | `// showPostProcessingButton …` | Function is `getPostProcessingButton` (and dead, see 7.1) |
| [`history_service.go`](../history_service.go#L79-L81) | "single atomic write" | See 4.7 |
| [`docs/coding_guidelines.md`](coding_guidelines.md) §2.5 | "`LogManager` uses a `sync.Mutex`" | `LogService` |

#### ~~7.3 Replace deprecated Fyne APIs~~
* *Done. `theme.PrimaryColor()` in the About window was replaced in `f3f998d`, and the `theme.ForegroundColor()` calls went away with 5.2. No deprecated Fyne colour accessors remain.*
* `theme.PrimaryColor()` in [`ui_manager.go`](../ui_manager.go#L186-L187), which already has a `//TODO`. Use `theme.Color(theme.ColorNamePrimary)` or `accentCyan`.
* `theme.ForegroundColor()` in [`logscanner.go`](../logscanner.go#L69) and [`logscanner.go`](../logscanner.go#L104). Removed as part of 5.2.

#### ~~7.4 Document or migrate the legacy `prefSmoothMotion = "upscale"` key~~
* *Done (`a19bb96`). Smooth Motion is now stored under `"smoothMotion"`. `PreferenceService.migrateLegacyKeys` copies a stored `"upscale"` value over and removes the legacy key, which remains documented as `legacyPrefSmoothMotion`.*
* **File:** [`preference_service.go`](../preference_service.go#L40)
* **Issue:** The Smooth Motion toggle is stored under the key `"upscale"`, a holdover from before the separate Upscale feature existed. Upscale itself uses `"upscaleVideo"`. This trips up anyone inspecting the preference store.
* **Fix:** At minimum, add a comment explaining the legacy name. Optionally, add a one-time migration in `Load()` that copies `"upscale"` → `"smoothMotion"`.

#### ~~7.5 Normalize line endings~~
* *Done (`39c06c8`). A `.gitattributes` pins `*.go` to `eol=lf`, the sources were renormalized, `gofmt -l .` is clean, and the CI formatting check now fails the build.*
* **Issue:** `dependency_service.go`, `ui_manager.go`, `entry_mode_test.go`, and `preference_service_test.go` use CRLF, so `gofmt -l .` flags all four (CI only emits a warning). There is no `.gitattributes`.
* **Fix:** Add a `.gitattributes` with `*.go text eol=lf`, then renormalize (`git add --renormalize .`). Once `gofmt -l .` is clean, consider making the CI formatting check fail the build.

#### ~~7.6 Strengthen CI~~
* *Done. CI runs `go test -race ./...` (`1877598`) and `staticcheck` pinned to v0.8.1 (`50fba89`).*
* **File:** [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)
* **Fix:** After 4.1/4.2 land, change the test step to `go test -race ./...`, at least on the Ubuntu runner where cgo is already available. Optionally add `staticcheck` (the locally installed copy was built with Go 1.24 and refuses to analyse this Go 1.26 module, so reinstall it with `go install honnef.co/go/tools/cmd/staticcheck@latest`).

#### ~~7.7 Minor consistency items~~
* *Done. Commits: `3aa48eb` made every `UpdateCallbacks` field required (no nil checks), matching `ProcessCallbacks`/`PPCallbacks`. `433056e` made `Check` report a missing `ffprobe`. `fd86c02` moved `fpsInterval` next to `runProgressSmoother`, and `3deee43` moved `configFileName` to `preference_service.go`. `785e693` made `startDownload` call `app.uiManager.savePreferences` directly, with the delegate removed.*
* `DependencyService.RunUpdate` nil-checks every callback, but no other callback struct consumer does (`ProcessCallbacks`, `PPCallbacks`). Pick one convention.
* `DependencyService.Check` verifies `yt-dlp` and `ffmpeg` but not `ffprobe`, which `PPEngine` needs for frame counts and duration.
* `fpsInterval` is declared in `main.go` but used only by the progress smoother in `download.go`. Move it next to its user, or into the 6.1 `startProgressSmoother` helper.
* `configFileName` is declared in `helpers.go` but used only for preference loading. Move it to `preference_service.go`.
* `DownloaderApp.savePreferences` lives in `preference_service.go` but is a UI delegate. Move it to `ui.go`, or call `app.uiManager.savePreferences` directly from `startDownload`.

---

### Category 8 — Roadmap Text Errata (Documentation)

Stale descriptions within this file that no longer match the code:

* ~~**High Priority / Phase 6:** "`ui.go` is left with only thin `DownloaderApp` delegates". Most of those delegates are dead code (7.1).~~ — *Done. The High Priority item, Phase 5 step 4, and the UIManager "Done" paragraph now say that only `clearTerminalOutput` remains and the other delegates were deleted in 7.1.*
* ~~**DownloadEngine "Done" paragraph:** "`ProcessCallbacks` bridges log, status, and progress events back to the UI without Fyne imports". `logscanner.go` still imports Fyne (5.2).~~ — *Done. 5.2 removed the Fyne import, so the claim now holds. The paragraph also explains how: plain lines carry a `nil` colour and `appendLogLine` substitutes the theme foreground.*
* ~~**DownloadEngine / High Priority "Split download.go":** "`runYtDlp` is now a thin wrapper". See 5.3.~~ — *Done. 5.3 made the claim true. Both places now name the helpers that took over the inline work: `recordHistory`, `reportDownloadResult`, `logDownloadSummary`, and `describeOutputFormat`.*
* ~~**PreferenceService "Done" paragraph:**~~ — *Done. The paragraph now describes the current code. `applyPreferencesToWidgets(ui *UIWidgets, p AppPreferences)` lives in `ui_snapshot.go`, alongside `snapshotPreferences`. `UIManager.savePreferences` is the only `savePreferences`, since the delegate was removed in 7.7. `resetPreferences`/`rebuildUI` became `UIManager.restoreDefaults`. The UIManager "Done" paragraph was updated to match.*
  * ~~`applyPreferencesToWidgets(AppPreferences)` is now `applyPreferencesToWidgets(ui *UIWidgets, p AppPreferences)`.~~
  * ~~"`savePreferences` has moved to `preference_service.go`": the real implementation is now `UIManager.savePreferences`, and only a delegate remains in `preference_service.go`.~~
  * ~~"`resetPreferences()` lives in `helpers.go`": it is now in `ui_manager.go`.~~
* ~~**PreferenceService step 2:** says the Load-from-Config button calls "`prefSvc.Save` directly". It calls `onSavePreferences`.~~ — *Done. Step 2 now names `UIManager.loadConfigFile` and says that it persists through `onSavePreferences`.*
* ~~**GPUCapabilityService:** "Its *consumers* still have pending work: see PPEngine step 4". PPEngine step 4 is done. Also, "`UIWidgets` gained a `gpuBackend` field" should now read `UIWidgets.postProcess.gpuBackend`.~~ — *Done. The "No open next steps" note now says the consumers are done too. Every `gpuBackend` reference now uses `UIWidgets.postProcess.gpuBackend`, including the ones in UIManager step 5 and the coupling note. The "Done" paragraph also no longer mentions the deleted `Capability` accessor (7.1) or an About-window GPU section (7.2).*
* ~~**PPEngine "Done" paragraph:** the shared-helper list omits `lastLine`, which `postprocess.go` exports to both `pp_engine.go` and `gpu_capability.go`.~~ — *Done. `lastLine` was added to the shared-helper list, along with its use in `gpu_capability.go`. The same paragraph was brought up to date for 4.4 (`failJob`), 5.4 (`newPostProcessSettings` in `ui_snapshot.go`), 6.9 (`patchThreadCount` deleted), and 7.1 (`checkPostProcessingEnabled` deleted).*
