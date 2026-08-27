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

1. **UIManager step 1** — Move `showPreferences` to `UIManager` (requires `PreferenceService`).
2. **UIManager step 2** — Move `showPostProcessing` to `UIManager` (requires `PreferenceService` + `PPEngine`).
3. **UIManager step 3** — Move `createMainMenu` to `UIManager`; inline `DependencyService` step 1 to remove the `checkDependencies` / `runUpdateInUI` wrappers from `DownloaderApp`.
4. **UIManager step 4** — Move `createUI` to `UIManager`. Largest single step; do last.
5. **UIManager step 5** — Replace all direct service fields on `UIManager` with injected callbacks; redesign the `UIManager` constructor so it holds no service-type references.

### Phase 6 — Final cleanup (after Phase 5)

- **High Priority: Group UIWidgets** — Break the 40-field flat struct into feature-specific sub-structs now that `createUI` lives in `UIManager`.
- **High Priority: Refactor ui.go** — Split the remaining window construction into focused helpers for menus, dialogs, and layout.
- **LogService step 3** — Replace the direct widget mutation in `appendOutput` with an `OnLogLine func(line string, col color.Color)` callback, now that `UIManager` owns widget lifecycle.
- **Update documentation** — `architecture.md` is already current (§4.11 documents `GPUCapabilityService`); `classes.puml` and the sequence diagrams still have no GPU-related entries and need `GPUCapabilityService`, `GPUBackend`, `BackendCapability`, and `EncoderPlan` added, alongside the rest of the fully extracted architecture.

---

## High Priority

- [ ] Refactor ui.go into smaller helpers — Split the large window construction into helpers for menus, dialogs, history, and preferences so the file is easier to scan and change. *(ui.go is 709 lines; `showAbout`, `showHistory`, `showConfigHelp` have moved to UIManager but `createUI`, `createMainMenu`, `showPreferences`, `showPostProcessing` remain. Blocked until more services are extracted.)*
- [ ] Split download.go into phases — Separate yt-dlp argument building, process startup, output parsing, and retry handling into smaller functions. *(`BuildArgs`, the retry loop, `FinalizeFiles`, and their composition are all in `DownloadEngine` now ✓ (`engine.Run`). `download.go`'s `runYtDlp` is now a thin wrapper: build `DownloadRequest`/`DownloadOptions` from UI state, call `engine.Run`, handle history + UI report. `Execute()`'s argument count is also resolved ✓ (`DownloadOptions`). DownloadEngine has no further open steps.)*
- [x] Break postprocess.go into smaller pipelines — Move FFmpeg option building, UI state handling, and feature-specific logic into smaller functions or separate files. *(`PPEngine` owns filter execution. Probe functions and `buildFFmpegArgs`/`patchThreadCount` moved to `pp_engine.go` ✓. `buildPostProcessFilters`, `computeProcessingLoad`, and `checkPostProcessingEnabled` decoupled from `*UIWidgets` via the `PostProcessSettings` value struct ✓. `postprocess.go` is now settings + pure functions + a thin `applyFFmpegFilters` wrapper + shared format helpers.)*
- [x] Use context.Context consistently for cancellation — Pass context through the download pipeline so stopping a job does not leave background work running. *(Context flows correctly through `startDownload` → `runYtDlp` → `DownloadEngine.Execute` → `PPEngine.ApplyFilters`. Resolved as a side-effect of the service extractions.)*
- [ ] Group UIWidgets into smaller structs — Break the large UIWidgets type into smaller feature-specific structs like download controls and preferences controls. *(Still one flat 40-field struct. Best tackled alongside the ui.go refactor.)*
- [x] Keep main.go thin — Use main.go as a bootstrapper only, and move app-specific setup into smaller constructors or services. *(`main()` is already a clean bootstrapper. `newDownloaderApp()` still initialises every widget inline (~50 lines), but this resolves naturally as a side-effect of "Group UIWidgets into smaller structs" — once that work produces typed sub-constructors, the initialization collapses to a single `NewUIWidgets()` call. No standalone action needed now.)*

## Medium Priority

- [x] Centralize preference loading — Move preference reads and default values into a small settings-loading layer so UI code stays focused on layout and event wiring. *(`PreferenceService` done.)*
- [x] Extract shared window-focus logic — Create one helper for the repeated focus-or-create pattern so every dialog and tool window behaves consistently. *(Added `focusOrCreate(win *fyne.Window) bool` and `onWindowClosed(win *fyne.Window) func()` to `ui_manager.go`; applied across all 5 singleton show methods in `ui_manager.go` and `ui.go`.)*
- [x] Replace hard-coded post-processing thresholds with constants — Name the thresholds and cost values so the code self-documents what each value means and is easier to tune later. *(Added 16 `cost*` constants and 4 `loadThreshold*` constants to `postprocess.go`; all magic numbers in `computeProcessingLoad` replaced. Block thresholds in `ui.go` left inline with a cross-reference comment to the load scale.)*
- [x] Keep LogManager focused on one job — Separate file appending and log persistence from mutex and error-handling details if the type grows further. *(`LogService` extracted; `LogManager` removed.)*
- [x] Move history handling behind a service boundary — Keep storage and schema changes away from the UI so history can evolve without touching the main window code. *(`HistoryService` done.)*
- [x] Keep log parsing tolerant — Treat yt-dlp output parsing as best-effort so small wording changes do not break downloads. *(Already satisfied: all parsing uses `strings.Contains` / `strings.CutPrefix` / `strings.Fields` with silent fallbacks. No parse failure can interrupt a download — worst case is a wrong progress value or incorrect format label in the summary.)*

## Low Priority

- [x] Organize helpers.go by purpose — Split helpers into groups like parsing, filesystem, UI, and formatting so the file does not become a dumping ground. *(Six named sections added: Config file, Thread-safe UI updates, Progress bar, Preference management, Filesystem, Dependency / update wrappers.)*
- [x] Make helper functions narrowly named and testable — Use descriptive helper names and prefer deterministic helpers for time, byte, and formatting logic so they are easy to test. *(Extracted pure `parseAppConfig([]byte)` and `isValidOption(string, []string)` from the DownloaderApp methods; made `loadConfigFile(path string)` a package-level function accepting an explicit path; added `configFileName` constant.)*
- [x] Keep theme code isolated and reusable — Keep theme colors and helpers separate from UI construction, and use named constants or helpers for repeated colors. *(Added 12 named colour vars to theme.go (`colSystem`, `colInfo`, `colError`, `colWarning`, `colSuccess`, `colSuccessBorder`, `colDebug`, `colDotIdle`, `colDotSuccess`, `colDotFailed`, `colDotCanceled`, `colDotProcessing`); replaced ~70 inline `color.RGBA{...}` literals across 8 files; normalised the stray `{255,160,0}` to `colWarning`; removed unused `image/color` import from main.go.)*
- [x] Isolate icon and embedded asset code — Keep generated or embedded asset files separate from application logic so they stay predictable and easier to update. *(`icons.go` and `embedded_icon.go` were already well-isolated. Fixed the raw `"themeMode"` string in `themedIcon()` to use the `prefThemeMode` constant; added a comment linking `svgFillLight` to `accentCyan` in `theme.go` to prevent them drifting.)*
- [x] Preserve platform-specific wrappers — Keep Windows and non-Windows process handling in dedicated build-tag files so the rest of the app can stay cross-platform and simple. *(Extracted `openFolderCommand(path string) *exec.Cmd` into `sys_windows.go` (Explorer) and `sys_others.go` (open/xdg-open); `openDownloadFolder` in helpers.go is now a 3-line wrapper; `runtime` and `os/exec` imports removed from helpers.go. The `.exe` suffix check in `dependency_service.go` and the default-format UI logic in `ui.go` were left inline — both are policy/string logic, not process handling.)*

---

## Component Status

Breaking down the `DownloaderApp` "God Object" into specialized components:

- [ ] **DownloadEngine** — yt-dlp execution, retries, cancellation, and progress parsing.
- [ ] **PPEngine** — FFmpeg filter composition, crop detection, worker pool orchestration, and post-process execution.
- [ ] **UIManager** — secondary window lifecycle (About, Help, History, Prefs, PP).
- [ ] **PreferenceService** — preference load/save/reset logic and defaults.
- [ ] **HistoryService** — download history persistence, schema evolution, and lookup helpers.
- [ ] **LogService** — session log/error log routing, rotation policy, and structured log helpers.
- [ ] **DependencyService** — binary discovery, dependency checks, and updater command execution.
- [ ] **GPUCapabilityService** — FFmpeg GPU backend detection, capability caching, and encoder-plan resolution (see [gpu-acceleration.md](gpu-acceleration.md)).
- [ ] **Update documentation** — architecture.md, classes.puml, and sequence diagrams fully reflect the extracted architecture.

See the sections below for per-component details and open next steps.


## DownloadEngine

**Done:** `DownloadEngine` struct introduced in `download_engine.go`. It owns the yt-dlp and ffmpeg binary paths and exposes four methods: `BuildArgs(DownloadRequest) DownloadArgs` (pure argument construction, no I/O), `Execute(ctx, args, opts DownloadOptions, ProcessCallbacks) (scanResult, error)` (retry loop with exponential backoff), `FinalizeFiles(savePath, downloadID string, onLog func(string, color.Color)) []string` (globs, strips the temp token, and renames each output to its final conflict-free path via the private `uniquePath` helper), and `Run(ctx, req, opts DownloadOptions, ProcessCallbacks) DownloadResult` (composes the three above into the full lifecycle of a single URL, reading no UI state). `DownloadOptions{AutoRetry bool; Index, Total int}` bundles the retry policy and this URL's position within a batch — the three runtime options `Execute` and `Run` share. `ProcessCallbacks` bridges log, status, and progress events back to the UI without Fyne imports. Its private `watchOutput`/`parseProgress` methods (moved from `DownloaderApp`, now living in `logscanner.go`) own all yt-dlp output scanning and report progress via `OnProgress(pct float64, size string)` instead of writing to `app.stats` directly. `download.go`'s `runYtDlp` is now a thin wrapper: it builds a `DownloadRequest` and `DownloadOptions` from UI state, calls `engine.Run`, and handles app-specific side effects (history recording, the completion/failure report, notifications) from the returned `DownloadResult`.

**Next steps:**

1. ~~**Move `watchOutput` / `parseProgress` out of `DownloaderApp`**~~ — *Done. Both are now private methods on `DownloadEngine` in `logscanner.go`, taking a `ProcessCallbacks` parameter. `ProcessCallbacks.WatchOutput` was removed; `Execute` calls `engine.watchOutput` directly, and a new `OnProgress(pct float64, size string)` field lets the caller update its own progress bar and `DownloadStats` without the engine touching UI state.*

2. ~~**Move `finalizeDownloadedFiles` to `DownloadEngine`**~~ — *Done. `FinalizeFiles(savePath, downloadID string, onLog func(string, color.Color)) []string` and the `uniquePath` helper it depends on moved from `postprocess.go` to `download_engine.go`. `runYtDlp` now calls `engine.FinalizeFiles(savePath, downloadID, app.appendOutput)` instead of `app.finalizeDownloadedFiles(...)`.*

3. ~~**Move `runYtDlp` to `DownloadEngine`**~~ — *Done. `Run(ctx, req DownloadRequest, autoRetry bool, index, total int, ProcessCallbacks) DownloadResult` added, composing `BuildArgs` + `Execute` + `FinalizeFiles` with no remaining UI state reads. `DownloaderApp.runYtDlp` is now a thin wrapper around it.*

4. ~~**Refactor `execute`**~~ — *Done. `DownloadOptions{AutoRetry bool; Index, Total int}` bundles the three loose params `Execute` and `Run` shared. Both methods now take `(ctx, ..., opts DownloadOptions, cb ProcessCallbacks)` — down from 6 params each to 4.*

## PPEngine

**Done:** `PPEngine` struct introduced in `pp_engine.go`. It owns the ffmpeg and ffprobe binary paths and exposes `ApplyFilters(ctx, filePaths, vfFilters, afFilters, PPCallbacks)`. `PPCallbacks` bridges log, status, and failure events to the UI. Private methods `detectCropFilter`, `resolveAutoCrop`, and `runJob` are fully engine-owned. Probe helpers (`probeFrameCount`, `probeDuration`, `computeOutputFrameCount`, `parseRationalFPS`) and argument builders (`buildFFmpegArgs`, `patchThreadCount`) moved from `postprocess.go` to `pp_engine.go` as private methods, dropping their explicit `ffprobePath` parameters. `postprocess.go` is now a thin layer containing the `PostProcessSettings` value struct plus its `newPostProcessSettings(ui)` translator, the free functions `buildPostProcessFilters`, `computeProcessingLoad`, and `checkPostProcessingEnabled` (all take `PostProcessSettings`, no `*UIWidgets` reads), `applyFFmpegFilters` (5-line wrapper), and shared format/scan helpers (`formatFFmpegProgress`, `formatBytes`, `formatDuration`, `filterShortName`, `scanCRLF`) used by `runJob`. GPU acceleration for the final encode step has since been layered on: `PPEngine.GPUBackend`/`GPUCapabilities` feed `PlanEncoder` inside `buildFFmpegArgsForBackend`, and `runJob` delegates its GPU semaphore/watchdog bookkeeping to a dedicated `gpuJobGuard` type (`newGPUJobGuard`, `arm`, `pet`, `release`), falling back to `retryWithCPU` on failure for GPU-encoded jobs (see [gpu-acceleration.md](gpu-acceleration.md) §6/§10).

**Next steps:**

1. ~~**Move `buildPostProcessFilters` out of `DownloaderApp`**~~ — *Done. `PostProcessSettings` (plain fields, no Fyne references) added to `postprocess.go`, along with `newPostProcessSettings(ui *UIWidgets) PostProcessSettings`. `buildPostProcessFilters`, `computeProcessingLoad`, and `checkPostProcessingEnabled` are now free functions taking `PostProcessSettings` — the `*DownloaderApp` receiver was dropped from all three since none of them need app state. `download.go` and `ui.go` call sites build the settings via `newPostProcessSettings(app.ui)` before calling in.*

2. ~~**Move probe functions to `PPEngine`**~~ — *Done. `probeFrameCount`, `probeDuration`, `computeOutputFrameCount`, and `parseRationalFPS` are now private methods on `PPEngine` in `pp_engine.go`. The explicit `ffprobePath` parameter was replaced by `engine.FFprobePath` throughout.*

3. ~~**Move `buildFFmpegArgs` and `patchThreadCount` to `PPEngine`**~~ — *Done. Both are now private methods on `PPEngine` in `pp_engine.go`. Call sites in `ApplyFilters` updated to use the `engine.` receiver.*

4. ~~**Refactor `runJob()`**~~ — *Done. The GPU semaphore/watchdog bookkeeping (previously an inline `releaseGPU` closure plus a raw `*time.Timer`) moved into a dedicated `gpuJobGuard` type (`newGPUJobGuard`, `arm`, `pet`, `release`) defined next to `maxConcurrentGPUJobs`/`gpuStallTimeout`. `runJob` now only builds/starts/streams/waits on the ffmpeg process and decides whether to hand off to `retryWithCPU`; `retryWithCPU` itself is unchanged and stays a separate method (business-logic fallback, not resource lifecycle). Also fixed: a GPU job's `cmd.Start()` failure previously logged-and-returned instead of retrying on CPU like the other two failure paths — it now calls `guard.release()` + `engine.retryWithCPU(...)` for consistency. A short phase-list doc comment was added to `runJob` covering the "document it better" half of this step.*

5. ~~**One operation per line**~~ — *Done. The nested `engine.computeOutputFrameCount(ctx, inputPath, engine.probeFrameCount(ctx, inputPath), activeVF)` call in `ApplyFilters`'s job-build loop split into `frameCount := engine.probeFrameCount(...)` followed by `totalFrames := engine.computeOutputFrameCount(..., frameCount, ...)`.*

## UIManager

**Done:** `UIManager` struct introduced in `ui_manager.go`. It owns the five singleton window fields (`aboutWindow`, `helpWindow`, `historyWindow`, `prefsWindow`, `ppWindow`) previously scattered on `DownloaderApp`. The three self-contained show methods (`showAbout`, `showHistory`, `showConfigHelp`) have moved to `UIManager`; their counterparts on `DownloaderApp` are now one-line delegates.

**Next steps — blocked until other services are extracted:**

1. **Move `showPreferences` to UIManager** — currently calls `savePreferences`, `resetPreferences`, `loadConfigFromFile`, `applyConfig`, `createUI`. Once `PreferenceService` owns those, `showPreferences` needs only a `PreferenceService` reference and an `onThemeChange` callback, which is manageable.

2. **Move `showPostProcessing` to UIManager** — currently calls `computeProcessingLoad`, `buildPostProcessFilters`, `savePreferences`, and (via `applyFFmpegFilters`) reads `app.ui.gpuBackend.Selected` and calls `app.gpuSvc.Detect`. Once `PreferenceService`, `PPEngine`, and `GPUCapabilityService` are the named dependencies, the callback surface shrinks to three objects.

3. **Move `createMainMenu` to UIManager** — menu item callbacks (`startDownload`, `runUpdateInUI`, `showPostProcessing`, etc.) become `UIManager` callback fields, wired at construction time. Depends on `DependencyService` for the updater action.

4. **Move `createUI` to UIManager** — the largest step. The main window layout reads from `*UIWidgets` and calls back into almost every service. This should be last, after all other services exist, so callbacks are typed references rather than raw closures over `DownloaderApp`.

5. **Remove direct service references from UIManager** — `UIManager` currently holds `historySvc *HistoryService` directly, creating dual ownership (both `DownloaderApp` and `UIManager` own the same instance). As each `show*` method migrates here, it will add more service fields, tightening coupling further — once step 2 above lands, `gpuSvc *GPUCapabilityService` will join `historySvc` as a second directly-held service reference. The clean solution is for `UIManager` to hold **no service references** — instead, inject callbacks at construction time (e.g. `OnLoadHistory func() ([]DownloadHistoryEntry, error)`, `OnClearHistory func() error`, `OnDetectGPU func(ctx context.Context) map[GPUBackend]BackendCapability`). `DownloaderApp` wires those callbacks to its services at startup, so `UIManager` stays decoupled from service types entirely. This step should be done once all `show*` methods have moved here, so the full callback surface is known before the constructor is redesigned.

## HistoryService

**Done:** `HistoryService` struct introduced in `history.go`. It owns the path to `download_history.json` and exposes three methods: `Load() ([]DownloadHistoryEntry, error)` (reads all entries, tolerant of missing file), `AppendAll(url, finalPaths, savePath, format, quality, postProcessed) error` (builds and persists one entry per output file in a single write), and `Clear() error` (resets to an empty array). The private `buildEntries` helper and `inferOriginalTitle` moved onto the service. All previous free functions (`historyFilePath`, `loadDownloadHistory`, `appendDownloadHistory`, `clearDownloadHistory`, `buildDownloadHistoryEntries`) have been removed. `DownloaderApp` holds `historySvc *HistoryService`; `UIManager` receives a reference at startup so `showHistory` and its Clear button never touch file paths directly. `download.go` now calls `app.historySvc.AppendAll(...)` instead of a for-loop over individual `appendDownloadHistory` calls.

**No open next steps** — `HistoryService` is fully extracted. Future work would be covered by the medium-priority roadmap item "Move history handling behind a service boundary", which is now complete.

> **Coupling note:** `UIManager` currently holds a direct `historySvc *HistoryService` reference, meaning both `DownloaderApp` and `UIManager` own the same instance. This is a temporary compromise. See UIManager step 5 above for the plan to replace all service fields on `UIManager` with injected callbacks.

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

`appendOutput()` remains on `DownloaderApp` because it is tightly coupled to `UIWidgets` (it reads widget state and mutates the log list); it delegates all file I/O to `logSvc`. `logSessionConfiguration()` has been removed — its formatting logic now lives in `LogService.WriteSessionConfig`, driven by the plain `SessionConfig` struct.

**Next steps:**

1. ~~**Cache the active session dir on `LogService`**~~ — *Done. `LogService` now stores `sessionDir` (set at `OpenSessionLog`, cleared at `CloseSessionLog`). `WriteToErrorLog` resolves the directory internally; `appendOutput` no longer reads `app.ui.path.Text` outside `fyne.Do`.*

2. ~~**Extract `logSessionConfiguration` into a `SessionConfig` value struct**~~ — *Done. See Phase 4 above.*

3. **`appendOutput` UI part will need a callback when `UIManager` absorbs `createUI`** — the `fyne.Do` block in `appendOutput` directly mutates `app.ui.logList` and `app.ui.output`. When `UIManager` eventually takes ownership of widget lifecycle and rendering (UIManager step 4), the UI side of `appendOutput` will need to become a registered `OnLogLine func(line string, col color.Color)` callback — similar to how `PPCallbacks.OnLog` and `ProcessCallbacks.OnLog` already decouple the engines from the UI.

## DependencyService

**Done:** `DependencyService` struct introduced in `dependency_service.go`. It owns `binDir` (resolved once at construction from the executable location) and exposes four members:

- `LocalPath(toolName string) string` — constructs the path inside `binDir`, appending `.exe` on Windows. Extracted from `getLocalBinPath` in `download.go`.
- `Resolve(toolName string) string` — returns the bundled path when it exists, otherwise the bare name for PATH lookup. Extracted from `resolvedBinPath` in `download.go`.
- `Check(onWarning func(string))` — verifies `yt-dlp` and `ffmpeg` are reachable and calls `onWarning` for each missing tool. Extracted from the body of `checkDependencies` in `helpers.go`.
- `RunUpdate(cb UpdateCallbacks)` — runs `yt-dlp -U` in a background goroutine. Extracted from the goroutine body of `runUpdateInUI` in `helpers.go`.

`UpdateCallbacks` is a bridge struct (`OnLog`, `OnStatus`, `OnSuccess`, `OnFailure`) with no Fyne dependency, following the same pattern as `PPCallbacks` and `ProcessCallbacks`.

The package-level `UpdateYtDlpCLI()` replaces the old `updateYtDlp()` free function used by the `--update` CLI flag.

`getLocalBinPath` and `resolvedBinPath` have been removed from `download.go` along with their `os`, `filepath`, and `runtime` imports. All callers (`runYtDlp`, `applyFFmpegFilters`) now use `app.depSvc.Resolve(...)`. Thin wrappers `checkDependencies()` and `runUpdateInUI()` remain on `DownloaderApp` in `helpers.go` to minimise call-site changes.

**Next steps:**

1. **Move `checkDependencies` and `runUpdateInUI` wrappers to `UIManager`** — both are currently thin one-call methods on `DownloaderApp`. Once `createMainMenu` migrates to `UIManager`, the update menu item callback and the startup dependency check can wire directly to `depSvc`, removing the wrappers entirely. `UIManager` would hold `depSvc *DependencyService` the same way it currently holds `historySvc`.

2. **Expose a `Version(toolName string) (string, error)` method** — needed when the "yt-dlp Auto-Update" roadmap feature is implemented (showing the installed version alongside the latest available). The method would run `yt-dlp --version` and return the trimmed output.

## PreferenceService

**Done:** `PreferenceService` struct introduced in `preference_service.go`. It owns all preference key constants (`prefSavedPath`, `prefFormat`, etc.) and default value constants (`defaultThemeMode`, `defaultSmoothFPS`, etc.) that were previously scattered as inline string/numeric literals. `AppPreferences` is a plain value struct with no Fyne widget references — safe to construct and pass anywhere. `PreferenceService.Load()` reads the Fyne store and returns a fully-defaulted `AppPreferences`; `Save(AppPreferences)` writes it back with the savePrefs gate preserved; `Reset()` removes all managed keys in one call. `LoadFromFile(path)` reads and parses a `govid.json` override file; `MergeConfig(cfg, base, validFormats, validQualities)` validates and merges config fields onto `base` without touching any widget, returning the merged struct and any validation error strings. `DownloaderApp.prefSvc` is initialised in `newDownloaderApp`. `applyPreferencesToWidgets(AppPreferences)` in `helpers.go` is the single translator from struct → widget state. `savePreferences` has moved to `preference_service.go`, co-located with the service it delegates to. `resetPreferences()` lives in `helpers.go` and now only handles data and log-buffer reset; UI reconstruction is split into a separate `rebuildUI()` function. All raw `fyne.CurrentApp().Preferences()` reads and the four direct `SetBool` writes in `OnChanged` handlers (`saveLog`, `notify`, `autoRetry`, `enablePostProcess`) have been removed from `ui.go`; all now route through `savePreferences`.

**Next steps:**

1. **Move `showPreferences` to UIManager** — now that `PreferenceService` owns all persistence, `showPreferences` only needs `app.prefSvc`, an `onThemeChange` callback, and `applyPreferencesToWidgets`. The dependency surface is small enough to pass through a constructor.

2. ~~**Move `loadConfigFromFile` / `applyConfig` to `PreferenceService`**~~ — *Done. `LoadFromFile(path) (*AppConfig, error)` and `MergeConfig(cfg, base, validFormats, validQualities) (AppPreferences, []string)` added to `preference_service.go`. `loadConfigFile` and `applyConfig` removed from `helpers.go`. The "Load from Config" button in `ui.go` now calls `prefSvc.MergeConfig`, `applyPreferencesToWidgets`, and `prefSvc.Save` directly. `applyPreferencesToWidgets` gained guarded writes for `Format`, `Quality`, and `SavedPath` (skipped when empty to preserve platform-specific defaults at startup).*

3. ~~**Remove the three inline `fyne.CurrentApp().Preferences().SetBool(...)` onChanged handlers**~~ — *Done. The `OnChanged` callbacks for `notify`, `autoRetry`, and `enablePostProcess` now call `app.savePreferences(app.ui.path.Text)`. A fourth handler (`saveLog`) that used a raw `"saveLog"` string rather than `prefSaveLog` was brought in line at the same time.*

## GPUCapabilityService

**Done:** `GPUCapabilityService` struct introduced in `gpu_capability.go`, following the same plain-struct-plus-cache pattern as the other extracted services. It owns `ffmpegPath` and a mutex-guarded cache keyed by `GPUBackend`, populated once per app run by `Detect(ctx) map[GPUBackend]BackendCapability` (checks `ffmpeg -encoders` output, then runs a short synthetic-frame probe per applicable backend) and read back via `Capability(backend) (BackendCapability, bool)`. `PlanEncoder(requested, capabilities, containerExt) EncoderPlan` is a pure function that always resolves to a runnable `-c:v` argument set, falling back to the CPU encoder when the requested/auto backend is unavailable. `GPUBackendOptions()`/`GPUBackendFromLabel()` map the UI selector's OS-filtered labels to `GPUBackend` values, and `FormatGPUDiagnostics()` renders per-backend availability lines for the session log and About window. `DownloaderApp` holds `gpuSvc *GPUCapabilityService`, constructed in `main.go`; `PreferenceService` owns the `gpuBackend` preference key/default; `UIWidgets` gained a `gpuBackend *widget.Select` field for the Post-Processing window's "Encoder Backend" selector. See [gpu-acceleration.md](gpu-acceleration.md) for the full design.

**No open next steps** — the service itself is fully self-contained and requires no further extraction. Its *consumers* still have pending work: see PPEngine step 4 (the `runJob` GPU-lifecycle extraction) and UIManager step 2 (moving `showPostProcessing`, which will pull in `gpuSvc` as a named dependency) above.

> **Coupling note:** Once UIManager step 2 lands, `UIManager` will hold a direct `gpuSvc *GPUCapabilityService` reference alongside `historySvc`, the same temporary dual-ownership compromise described under HistoryService above. See UIManager step 5 for the plan to replace all service fields on `UIManager` with injected callbacks.

## main.go

- [x] Potential race/coupling around cancellation function access — *Done. Direct reads/invocations of `cancelFn` were removed from UI/close handlers; callers now use `RequestCancel()`, and cancel callback access is synchronized behind a mutex on `DownloaderApp`.*

- [x] Non-idiomatic os.Exit(0) in main normal flow — *Done. The `-update` success path exits via `return`; `os.Exit(...)` remains only for non-zero error exit code propagation.*

- [x] Open question: strict single-cancel semantics — *Addressed. `RequestCancel()` atomically takes-and-clears the active cancel callback before invocation, preventing repeated concurrent invocations of the same cancel function from multiple UI paths.*