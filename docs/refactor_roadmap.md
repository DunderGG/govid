# GoVid Refactoring Roadmap — Priority Sorted

This version groups the audit items into priority buckets so you can tackle the biggest maintainability wins first. See the audit for details. [audit_review.md](audit_review.md)

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
- [ ] **Update documentation** — architecture.md, classes.puml, and sequence diagrams fully reflect the extracted architecture.

See the sections below for per-component details and open next steps.

---

## High Priority

- [ ] Refactor ui.go into smaller helpers — Split the large window construction into helpers for menus, dialogs, history, and preferences so the file is easier to scan and change. *(ui.go is 709 lines; `showAbout`, `showHistory`, `showConfigHelp` have moved to UIManager but `createUI`, `createMainMenu`, `showPreferences`, `showPostProcessing` remain. Blocked until more services are extracted.)*
- [ ] Split download.go into phases — Separate yt-dlp argument building, process startup, output parsing, and retry handling into smaller functions. *(`BuildArgs` and the retry loop are in `DownloadEngine`. Remaining: move `watchOutput`/`parseProgress`/`finalizeDownloadedFiles` then `runYtDlp` — see DownloadEngine next steps below.)*
- [ ] Break postprocess.go into smaller pipelines — Move FFmpeg option building, UI state handling, and feature-specific logic into smaller functions or separate files. *(`PPEngine` owns filter execution. Remaining: move probe functions, `buildFFmpegArgs`/`patchThreadCount`, and decouple `buildPostProcessFilters` from widgets — see PPEngine next steps below.)*
- [x] Use context.Context consistently for cancellation — Pass context through the download pipeline so stopping a job does not leave background work running. *(Context flows correctly through `startDownload` → `runYtDlp` → `DownloadEngine.Execute` → `PPEngine.ApplyFilters`. Resolved as a side-effect of the service extractions.)*
- [ ] Group UIWidgets into smaller structs — Break the large UIWidgets type into smaller feature-specific structs like download controls and preferences controls. *(Still one flat 40-field struct. Best tackled alongside the ui.go refactor.)*
- [ ] Keep main.go thin — Use main.go as a bootstrapper only, and move app-specific setup into smaller constructors or services. *(`main.go` is 159 lines; `newDownloaderApp` still initialises every widget inline. Acceptable for now.)*

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

5. **Remove direct service references from UIManager** — `UIManager` currently holds `historySvc *HistoryService` directly, creating dual ownership (both `DownloaderApp` and `UIManager` own the same instance). As each `show*` method migrates here, it will add more service fields, tightening coupling further. The clean solution is for `UIManager` to hold **no service references** — instead, inject callbacks at construction time (e.g. `OnLoadHistory func() ([]DownloadHistoryEntry, error)`, `OnClearHistory func() error`). `DownloaderApp` wires those callbacks to its services at startup, so `UIManager` stays decoupled from service types entirely. This step should be done once all `show*` methods have moved here, so the full callback surface is known before the constructor is redesigned.

## HistoryService

**Done:** `HistoryService` struct introduced in `history.go`. It owns the path to `download_history.json` and exposes three methods: `Load() ([]DownloadHistoryEntry, error)` (reads all entries, tolerant of missing file), `AppendAll(url, finalPaths, savePath, format, quality, postProcessed) error` (builds and persists one entry per output file in a single write), and `Clear() error` (resets to an empty array). The private `buildEntries` helper and `inferOriginalTitle` moved onto the service. All previous free functions (`historyFilePath`, `loadDownloadHistory`, `appendDownloadHistory`, `clearDownloadHistory`, `buildDownloadHistoryEntries`) have been removed. `DownloaderApp` holds `historySvc *HistoryService`; `UIManager` receives a reference at startup so `showHistory` and its Clear button never touch file paths directly. `download.go` now calls `app.historySvc.AppendAll(...)` instead of a for-loop over individual `appendDownloadHistory` calls.

**No open next steps** — `HistoryService` is fully extracted. Future work would be covered by the medium-priority roadmap item "Move history handling behind a service boundary", which is now complete.

> **Coupling note:** `UIManager` currently holds a direct `historySvc *HistoryService` reference, meaning both `DownloaderApp` and `UIManager` own the same instance. This is a temporary compromise. See UIManager step 5 above for the plan to replace all service fields on `UIManager` with injected callbacks.

## LogService

**Done:** `LogService` struct introduced in `log_service.go`. It owns the session log file handle, two mutexes, the daily rotation policy (daily `YYYY-MM-DD` filename scheme), and the UI buffer-limit value. `DownloaderApp` holds `logSvc *LogService` (previously `log *LogManager`).

Extracted from `helpers.go` and `download.go`:
- `OpenSessionLog(dir string) (string, error)` — replaces the inline `os.OpenFile` + `app.log.file = file` block in `startDownload`.
- `CloseSessionLog()` — replaces the inline mutex + write + close + nil block in `startDownload`.
- `WriteToFile(line string)` — replaces the `app.log.mutex.Lock` / `fmt.Fprintf` / `Unlock` block inside `appendOutput`.
- `WriteToErrorLog(line, dir string)` — replaces `appendErrorOutput` + `dailyErrorLogPath` in `helpers.go`.
- `SetBufferLimit(n int)` / `BufferLimit() int` — replace the `logBufferLimit` package-level global.
- `IsErrorLine(line string) bool` — replaces `isErrorLogLine` (package-level helper, no instance needed).
- `ParseBufferLimit(s string) int` — replaces `parseLogLimit` (package-level helper).
- `SessionLogPath(dir string)` / `ErrorLogPath(dir string)` — replaces the `dateStamp` + `filepath.Join` inline logic in both `startDownload` and `dailyErrorLogPath`.

`appendOutput()` and `logSessionConfiguration()` remain on `DownloaderApp` because they are tightly coupled to `UIWidgets` (they read widget state and mutate the log list). They now delegate all file I/O to `logSvc`.

**Next steps:**

1. **Cache the active session dir on `LogService`** — `OpenSessionLog(dir)` already knows the save directory at session start; storing it on the service would let `WriteToErrorLog` use the session dir automatically instead of requiring the caller to re-read `app.ui.path.Text` on every call. This also fixes a subtle correctness issue: `appendOutput` currently reads `app.ui.path.Text` from outside `fyne.Do`, meaning the widget could change mid-session and shift the error log to a different directory than the session log. Storing the dir at `OpenSessionLog` time would anchor both files to the same location for the lifetime of a session.

2. **Extract `logSessionConfiguration` into a `SessionConfig` value struct** — `logSessionConfiguration` reads directly from ~15 `app.ui.*` fields (format, quality, trim, toggles, post-process settings). When `DownloadEngine.runYtDlp` eventually becomes `engine.Run(ctx, req, callbacks)`, its caller will pass config as data rather than reading widgets inline. At that point, introduce a `SessionConfig` plain struct (parallel to `AppPreferences`) and a `logSvc.WriteSessionConfig(cfg SessionConfig, writeFn func(string, color.Color))` method — completing the "structured log helper" described in the original roadmap item.

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

**Done:** `PreferenceService` struct introduced in `preference_service.go`. It owns all preference key constants (`prefSavedPath`, `prefFormat`, etc.) and default value constants (`defaultThemeMode`, `defaultSmoothFPS`, etc.) that were previously scattered as inline string/numeric literals. `AppPreferences` is a plain value struct with no Fyne widget references — safe to construct and pass anywhere. `PreferenceService.Load()` reads the Fyne store and returns a fully-defaulted `AppPreferences`; `Save(AppPreferences)` writes it back with the savePrefs gate preserved; `Reset()` removes all managed keys in one call. `DownloaderApp.prefSvc` is initialised in `newDownloaderApp`. `applyPreferencesToWidgets(AppPreferences)` in `helpers.go` is the single place that translates a loaded struct into widget state. `savePreferences` now builds an `AppPreferences` from widget state and calls `prefSvc.Save`. `resetPreferences` calls `prefSvc.Reset()`. All raw `fyne.CurrentApp().Preferences()` reads have been removed from `ui.go` (`showPostProcessing`, `showPreferences`, `createUI`).

**Next steps:**

1. **Move `showPreferences` to UIManager** — now that `PreferenceService` owns all persistence, `showPreferences` only needs `app.prefSvc`, an `onThemeChange` callback, and `applyPreferencesToWidgets`. The dependency surface is small enough to pass through a constructor.

2. **Move `loadConfigFromFile` / `applyConfig` to `PreferenceService`** — these read `govid.json` and merge its values into preferences. They are currently methods on `DownloaderApp` but have no UI dependency; a `LoadFromFile(path string) (*AppConfig, error)` method and a `MergeConfig(cfg *AppConfig, ui *UIWidgets)` helper would remove the last preference-adjacent code from `DownloaderApp`.

3. **Remove the three inline `fyne.CurrentApp().Preferences().SetBool(...)` onChanged handlers** — `notify`, `autoRetry`, and `enablePostProcess` still write directly to the Fyne store on change. Replace each with a call to `app.savePreferences(app.ui.path.Text)` so every write goes through the service.
