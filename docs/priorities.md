# GoVid — Top Ten Priorities

The ten roadmap items to work on next, in the order they should be done. Each was checked against the current code. The "What we found" notes describe the code as it was on 2026-10-06.

How the order was chosen:

1. **Stability and data hygiene first (1–4).** Cancelling or quitting can leave background processes running and leftover files on disk. Busy downloads can also flood the UI thread. These problems affect every user on every download.
2. **Then a shipped feature that is broken (5).**
3. **Then the features users notice most (6–10).** Items that build on each other are next to each other: #6 adds the GitHub release client that #9 reuses, and #7 adds the URL probe that #8 reuses.

Source references point to the matching section of [roadmap.md](roadmap.md). Tick items off here and in the roadmap as they land.

---

## 1. ✅ Clean cancellation and safe shutdown

**Roadmap:** UI Performance & Stability → Goroutine Lifecycle Hygiene ("safe shutdown path"). The zombie-process risk is also listed in `notes.txt`.

**Status: Done.** yt-dlp, ffmpeg, and ffprobe are started through `newToolCommand` ([process.go](../process.go)), whose `cmd.Cancel` kills the whole process tree and whose `cmd.WaitDelay` is 3 s. On Windows this uses the `taskkill /T /F` fallback rather than a Job Object, because the Job Object would have to be attached after `Start`, which `Output`/`CombinedOutput` callers cannot do. `DownloadEngine.Run` calls the new `RemovePartialFiles` on failure or cancel. Quitting calls `DownloaderApp.Shutdown`, which stops the whole session (not just the current batch item), waits up to 5 s, closes the session log, and then quits. Tests: `TestExecuteCancelKillsChildProcesses`, `TestRunCancelRemovesPartialFiles`, and `TestShutdownStopsWholeBatchAndQuits`.

**Problem.** Cancelling a download, or quitting while one is running, can leave processes and files behind.

**What we found**
- **Only the direct child process is killed.** yt-dlp is started with `exec.CommandContext` ([download_engine.go:231](../download_engine.go#L231)), so cancelling kills only the yt-dlp PID. yt-dlp starts its own ffmpeg child for merging and for trimming (`--force-keyframes-at-cuts` re-encodes). The Windows `yt-dlp.exe` is a PyInstaller bundle that also runs as more than one process. These child processes keep running after cancel. They use CPU and keep the output file locked.
- **Partial files are left behind.** `--no-part` makes yt-dlp write directly to `GoVid_<title>_GOVID<id>.<ext>`. [download_engine.go:291](../download_engine.go#L291) calls `FinalizeFiles` only on success. A cancelled or failed download therefore leaves `*GOVID<id>*` files in the user's folder, and nothing ever removes them.
- **Quitting does not wait.** The close handler ([main.go:124](../main.go#L124)) calls `RequestCancel()` and then `mainApp.Quit()` right away. It does not wait for workers to stop or for the session log to close.

**Proposed solution**
1. Add a per-OS `killProcessTree(cmd)` in [sys_windows.go](../sys_windows.go) and [sys_others.go](../sys_others.go):
   - **Windows:** after `Start`, put the process in a Job Object created with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` (`golang.org/x/sys/windows`: `CreateJobObject`, `SetInformationJobObject`, `AssignProcessToJobObject`). Cancel with `TerminateJobObject`. A simpler fallback is `taskkill /T /F /PID <pid>`.
   - **Unix:** set `SysProcAttr.Setpgid = true` and kill the whole process group with `syscall.Kill(-pid, SIGKILL)`.
2. Connect it through `cmd.Cancel` so `CommandContext` uses it. Set `cmd.WaitDelay` (for example 3 s) so `Wait` cannot hang on pipes that a child process still holds open. Use this for yt-dlp and for the ffmpeg/ffprobe calls in [pp_engine.go](../pp_engine.go).
3. In `DownloadEngine.Run`, when the download fails or is cancelled, remove every file matching `*<downloadID>*` once the process tree is dead, and log what was removed.
4. Give `DownloaderApp` a `sync.WaitGroup` (or a done channel) for active work. On quit: cancel, show "Stopping…", wait off the UI thread with a timeout (about 5 s), close the session log, then call `Quit` from inside `fyne.Do`.

**Done when**
- Cancelling or quitting during the `[Merger]` phase leaves no `yt-dlp`/`ffmpeg` processes in Task Manager and no `GOVID` files in the save folder.
- A test passes in which the fake tool from [fake_tool_test.go](../fake_tool_test.go) starts a long-sleeping child process, and the test checks that the child is gone after cancel.

---

## 2. ✅ Batch the log rendering

**Roadmap:** UI Performance & Stability → Log Rendering Efficiency.

**Status: Done.** `UIManager.appendLogLine` queues lines under a mutex, and the first queued line arms a 100 ms timer. `flushLog` then adds every queued line, trims once, and refreshes once inside a single `fyne.Do`. The session end calls `flushLog` directly so the summary appears immediately. The view holds at most `maxScreenLogLines` (5000) lines even when "Unlimited" is selected; the Preferences hint and the guide say so. The view follows new lines only when it was already within 8 px of the bottom. Step 4 (a virtualized `widget.List`) was not needed: a flush now costs one layout instead of one per line. Tests: `TestAppendLogLineBatchesUntilFlush`, `TestLogViewCapsLinesEvenWhenUnlimited`, `TestLogViewFollowsOnlyWhenAtBottom`, and `TestClearTerminalOutputDropsQueuedLines`.

**Problem.** Every log line goes through its own UI-thread round trip. Verbose yt-dlp and ffmpeg output can produce hundreds of lines per second. This is the most likely cause of the freezes the roadmap's freeze-report items describe.

**What we found**
- `appendLogLine` ([ui_manager.go:960](../ui_manager.go#L960)) makes one `fyne.Do` call per line. Each call creates a `canvas.Text`, refreshes the whole VBox, and calls `ScrollToBottom()`.
- The "Unlimited" buffer setting maps to `math.MaxInt32` ([log_service.go:238](../log_service.go#L238)). The on-screen VBox can therefore grow without limit, and each refresh lays out every line, so the cost grows with the length of the log.
- Every new line forces a scroll to the bottom. A user who scrolls up to read an error is pulled back down.

**Proposed solution**
1. Collect incoming lines in a slice guarded by a mutex. Flush them every ~100 ms with one `fyne.Do` that adds all pending lines, trims once, and refreshes once. Arm the flush timer on the first pending line so an idle app does no work. Flush right away when a session ends so the summary appears immediately.
2. Add a hard on-screen cap (for example 5,000 lines) that applies even when "Unlimited" is selected. "Unlimited" then applies only to the file log. Update the Preferences label and the help text to say so.
3. Follow mode: call `ScrollToBottom()` only if the view was already at, or within a few pixels of, the bottom before the flush.
4. If the VBox is still slow after steps 1–3, replace it with a virtualized `widget.List` backed by a slice. Only the visible rows are then rendered.

**Done when**
- A long, verbose batch run keeps the window responsive.
- A user can scroll up during a download and the view stays where they left it.

---

## 3. ✅ Throttle status and progress updates

**Roadmap:** UI Performance & Stability → Event Queue Backpressure. Also UI & UX → Download Controls ("Hold the progress bar at ~95% during the `[Merger]` phase").

**Status: Done.** `latestValueThrottle` ([throttle.go](../throttle.go)) applies a value at once after a quiet period and otherwise coalesces values to the newest one at most every 150 ms, skipping repeats. `updateStatus` goes through it, and the throttle is flushed when a download result is reported and when a session ends. The smoother now ticks every 33 ms and only sends changes of at least 0.002. `ProcessCallbacks.OnPhase` reports `[Merger]` ("Merging") and `[VideoConvertor]`/`[ExtractAudio]` ("Converting"); `showDownloadPhase` snaps the bar to 95% and sets the status. While doing this we found that real yt-dlp prints `[Merger]` to **stdout**, but `wasConverted` was only detected on stderr; both streams are now checked. Tests: `throttle_test.go`, `TestDetectPhase`, `TestWatchOutputReportsPhasesFromEitherStream`, and `TestShowDownloadPhaseHoldsProgressBar`.

**Problem.** Status text and progress updates are sent to the UI thread with no rate limit and no de-duplication.

**What we found**
- `updateStatus` ([helpers.go:74](../helpers.go#L74)) makes one `fyne.Do` call for every message.
- During post-processing, [pp_engine.go:340](../pp_engine.go#L340) calls `OnStatus` for every ffmpeg progress line, in every worker of the pool. Concurrent jobs therefore each send updates to the same label.
- `runProgressSmoother` ([helpers.go:221](../helpers.go#L221)) sends a frame every 20 ms (50/s) while the bar is moving, even when the change is too small to see.
- [logscanner.go:82](../logscanner.go#L82) already detects `[Merger]` and `[VideoConvertor]`, but only sets a flag for the final summary. The UI is not told when the merge phase starts.

**Proposed solution**
1. Add a small `latestValueThrottle` helper. It stores the newest value under a mutex, schedules at most one `fyne.Do` per interval (~150 ms) that applies that value, and skips the update when the value has not changed. Route `updateStatus` through it.
2. In the smoother, skip the frame when the change is below ~0.002, and lower the tick rate to ~30 fps (33 ms).
3. Add an `OnPhase(phase string)` callback to `ProcessCallbacks`. On `[Merger]`/`[VideoConvertor]`, set the progress target to 0.95 and the status to "Merging…". On completion, snap to 100% as before.

**Done when**
- Status updates reach the UI at no more than ~7 per second during post-processing.
- The bar shows "Merging…" at 95% instead of sitting at 100% (or dropping back) during the merge.

---

## 4. ✅ Cut log noise

**Roadmap:** Technical Improvements → General improvements ("limit how much we are logging").

**Status: Done.** `appendOutput` classifies each line with `IsDebugLine` and hides `[debug]` lines from the view unless the new **Debug Output** preference (`prefShowDebug`, off by default) is on. The log file always gets every line, and the setting is logged in the session configuration. `--verbose` is kept. For progress, we took the "update a single line in place" option: `renderLogLines` replaces a yt-dlp progress line (`IsProgressLine`) with the next one, so each downloaded stream takes one line. FFmpeg's per-frame stats lines were already sent only to the status label (now throttled by #3), so step 4 needed no change. Tests: `TestAppendOutputKeepsDebugLinesOutOfTheView`, `TestLogViewShowsLatestProgressLineInPlace`, `TestIsDebugLine`, and `TestIsProgressLine`.

**Problem.** The on-screen log is full of debug output, which hides the lines users care about. It also adds to the UI load addressed in #2.

**What we found**
- `BuildArgs` always passes `--verbose` ([download_engine.go:125](../download_engine.go#L125)). Every `[debug]` line goes to the screen; [logscanner.go](../logscanner.go) only colours them grey.
- `--newline` makes yt-dlp print a new `[download]  x.x%` line for every progress update. A single file produces hundreds of these lines.

**Proposed solution**
1. Add a level to the log path (`OnLog(line, col)` becomes `OnLog(line, col, level)`, or the logscanner classifies each line). `[debug]` lines go only to the session log file, unless a new "Show debug output" preference (off by default) is enabled.
2. Keep `--verbose` so the file log stays useful for bug reports. If it turns out to add nothing, pass it only when the debug preference is on.
3. Show fewer progress lines on screen. Either show one line per 10% step, or update a single "current progress" line in place instead of appending a new one.
4. Apply the same rule to ffmpeg's per-frame stats lines in post-processing. The status label (#3) already shows that progress.

**Done when**
- A normal single download shows roughly 20–40 lines on screen.
- The session log file still contains the full verbose output.

---

## 5. ✅ Fix HDR → SDR tone mapping

**Roadmap:** Technical Improvements → Post-Processing Features ("HDR to SDR Tone Mapping … Does not seem to work properly").

**Status: Done.** `buildPostProcessFilters` now emits a `__tonemap__` sentinel. `PPEngine.resolveToneMap` replaces it per file with `toneMapFilter(transfer)` (the proposed chain, with `tin=smpte2084` or `tin=arib-std-b67`), or drops it and logs "Source is SDR, skipping tone mapping". Tone-mapped output is tagged `-color_primaries bt709 -color_trc bt709 -colorspace bt709`. Two additions beyond the plan:
- `probeColorInfo` falls back to parsing `ffmpeg -i`'s stream summary, because ffprobe is not bundled. Without the fallback, release users would never get tone mapping.
- A BT.2020 file with no transfer tag is treated as PQ. VP9/AV1 bitstreams carry the matrix but not the transfer, so the transfer tag is the one lost in merges.

The bundled ffmpeg 8.1 has `zscale` and `tonemap` (now recorded in [gpu-acceleration.md](gpu-acceleration.md#5-current-bundled-build-inventory)). `testdata/hdr_pq_sample.mkv` is a 16 KiB PQ clip for manual checks. We ran it through `ApplyFilters` with the bundled ffmpeg and no ffprobe:
- The tagged PQ clip is tone mapped.
- An SDR clip is left byte-for-byte unchanged.
- With the transfer tag removed, the old chain produced no output at all, while the new chain produced the same frames as for the tagged clip.

Comparing against a browser's rendering of a real HDR YouTube video still needs a manual check. Tests: `TestToneMapFilter`, `TestResolveToneMap`, `TestColorInfoHDRTransfer`, `TestParseFFmpegColorInfo`, `TestParseFFprobeColorInfo`, and `TestBuildFFmpegArgsTagsToneMappedOutputAsBT709`.

**Problem.** This feature has already shipped, but it produces washed-out output.

**What we found** ([postprocess.go:131](../postprocess.go#L131))
```
zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:min=gbr:r=tv,format=yuv420p
```
- The first `zscale` never states the input colour space (`tin=`, `pin=`, `min=`). zscale then relies on the colour tags in each frame. When those tags are missing, it assumes BT.709, and the result is the washed-out picture described in the roadmap. Tags are often missing after yt-dlp merges VP9/AV1 streams into mkv/mp4, and for HLG sources.
- The filter runs even on SDR sources, so ordinary videos are changed too.
- The output file is not tagged as BT.709, so players may still treat it as BT.2020.

**Proposed solution**
1. Add `probeColorInfo` next to `probeFrameCount` in [pp_engine.go](../pp_engine.go). It runs `ffprobe -select_streams v:0 -show_entries stream=color_transfer,color_primaries,color_space`.
2. Only tone map when the transfer is `smpte2084` (PQ) or `arib-std-b67` (HLG). For any other source, log "Source is SDR, skipping tone mapping" and leave it unchanged.
3. Declare the input colour space explicitly:
   `zscale=tin=smpte2084:min=bt2020nc:pin=bt2020:t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p`
   (use `tin=arib-std-b67` for HLG).
4. Tag the output: `-color_primaries bt709 -color_trc bt709 -colorspace bt709`.
5. Confirm that the bundled ffmpeg includes `zscale` (libzimg) using the inventory in [gpu-acceleration.md](gpu-acceleration.md#5-current-bundled-build-inventory).
6. Add filter-string tests for PQ, HLG, and SDR inputs. Store a short generated PQ clip in `testdata/` for a manual check.

**Done when**
- A known HDR YouTube video, after processing, matches the browser's SDR rendering when compared side by side.
- An SDR input is left unchanged.

---

## 6. ✅ yt-dlp version check

**Roadmap:** Medium Priority → yt-dlp Auto-Update. (The one-click "Update yt-dlp" menu item is already done; the version check is what remains.)

**Status: Done.**
- `ReleaseService.Latest(ctx, owner, repo, maxAge)` ([release_service.go](../release_service.go)) caches each answer with its check time in the preferences store. HTTP 403/429 returns `errReleaseUnknown` and is cached as well, so a rate-limited check isn't repeated on every start.
- `compareVersions` compares versions part by part, numerically, which covers yt-dlp's date versions including nightlies.
- `startUpdateChecks` ([update_check.go](../update_check.go)) runs after `checkDependencies`. An outdated yt-dlp is logged once, and `UIManager.showNotice` shows a new non-blocking notice bar above the input card with **Update now** (→ `runUpdateInUI`). A successful update dismisses it.
- New "Check for updates on startup" preference, on by default.
- The Update yt-dlp dialog and About show the installed and latest versions.
- Failed downloads with "Unable to extract" / "Sign in to confirm" / "HTTP Error 403" errors log a hint to update yt-dlp.
- A failed `yt-dlp -U` checks whether the yt-dlp folder is writable and, if not, explains how to fix it (in both the GUI and `--update`).

While testing, GitHub returned 403 (rate limit) to this machine, which is exactly the "unknown" case. Tests: `release_service_test.go`, `update_check_test.go`, `TestNoticeActionRunsAndDismisses`, `TestDependencyRunUpdateExplainsUnwritableFolder`, `TestWatchOutputDetectsExtractorErrors`, and `TestRunYtDlpExtractorErrorSuggestsUpdate`.

**Problem.** An outdated yt-dlp is the most common reason a downloader suddenly stops working. Sites, YouTube above all, often change in ways that break older yt-dlp versions within weeks. Users currently get no warning.

**What we found**
- Tools → Update yt-dlp runs `yt-dlp -U` ([dependency_service.go:117](../dependency_service.go#L117)), and `DependencyService.Version` can read the installed version.
- Nothing compares the installed version with the latest release.

**Proposed solution**
1. Add a `ReleaseService` (`release_service.go`) with `Latest(ctx, owner, repo) (Release, error)`. It calls `https://api.github.com/repos/<owner>/<repo>/releases/latest` and parses `tag_name`, `html_url`, `body`, and `assets`. It uses a 5 s timeout, caches the result with a timestamp in preferences (at most one check per day), and treats HTTP 403 (rate limit) as "unknown" rather than as an error. **#9 reuses this service.**
2. yt-dlp versions are dates (`2025.09.26`), so compare them as date strings after normalising.
3. Check in the background after `checkDependencies()` at startup. If the installed version is out of date, log one line and show a non-blocking notice with an "Update now" button that calls the existing `runUpdateInUI`. Add a "Check for updates on startup" preference.
4. Show the installed and latest versions in the Update dialog and in About.
5. When a download fails with an extractor error (`Unable to extract`, `Sign in to confirm`, `HTTP Error 403`), add a hint suggesting the user update yt-dlp.
6. If `yt-dlp -U` fails because `bin/` is not writable (for example under Program Files), show the error clearly.

**Done when**
- Putting an old `yt-dlp.exe` in `bin/` triggers the notice at startup, and "Update now" brings it up to date.

---

## 7. Playlist support

**Roadmap:** High Priority → Playlist Support.

**Problem.** Playlist URLs are not handled in a way users would expect.

**What we found**
- `--no-playlist` is hard-coded ([download_engine.go:125](../download_engine.go#L125)). For a `watch?v=X&list=Y` URL, the playlist is silently ignored.
- For a pure `playlist?list=Y` URL, yt-dlp ignores `--no-playlist` and downloads the whole playlist inside a single queue item. That item has one progress bar and one cancel button covering every video, and `FinalizeFiles` picks up all of the files under one download ID.

**Proposed solution**
1. Before queueing, probe each URL off the UI thread with `yt-dlp --flat-playlist -J --no-warnings <url>`. The status shows "Checking URL…" and the probe can be cancelled. `--flat-playlist` is fast because it does not extract each video.
2. If `_type == "playlist"`, show a dialog with the playlist title, the item count, the total duration (when `entries[].duration` is available), and a range field ("1-10", "3,5,8"). The options are **Download all / Only this video / Cancel**. For `watch?v=…&list=…` URLs, "Only this video" is the default.
3. Expand the chosen entries (`entries[].url`) into separate URLs and feed them into the existing batch queue (`runQueue` in [download.go](../download.go)). Do not pass `--yes-playlist`. Each video then gets its own progress row, per-item cancel, retry, history entry, and duplicate-name handling with no new queue code.
4. Keep `--no-playlist` on each expanded item.
5. Show size as "unknown" for playlists. A full per-video probe is too slow to run by default; #8 handles size where it matters.

**Done when**
- Pasting a 20-video playlist opens the prompt.
- Choosing range `5-8` downloads four files, each with its own progress and cancel.

---

## 8. Disk space pre-check

**Roadmap:** Medium Priority → Disk Space Pre-check.

**Problem.** Nothing checks free space before a download starts. A full drive causes a failure partway through the download or during post-processing. Post-processing needs extra room because it writes a temporary output next to the source file.

**Proposed solution**
1. Add `freeBytes(path) (uint64, error)` in [sys_windows.go](../sys_windows.go) (`windows.GetDiskFreeSpaceEx`) and [sys_others.go](../sys_others.go) (`unix.Statfs`: `Bavail * Bsize`). Inject it as a function so tests can fake it.
2. Get the size estimate from the #7 probe. For single videos, run `-J` without `--flat-playlist` and use the same `-f` format string as `BuildArgs`. Sum `requested_formats[].filesize` or `filesize_approx`. If the size is unknown, skip the check and log that it was skipped.
3. Check before each item, not just once at the start of the session, because earlier items in a batch use up space. The required space is estimate × 1.1, doubled when post-processing is enabled.
4. If there is not enough space, show: "Needs ~X GB, Y GB free. Continue anyway?" In a batch, show the warning once with Skip / Continue / Stop options. Use the existing `formatBytes` helper for the numbers.

**Done when**
- Unit tests with a fake `freeBytes` cover the cases: enough space, not enough space, and unknown size.
- A manual test on a nearly full USB stick shows the warning before the download starts.

---

## 9. GoVid self-update check

**Roadmap:** High Priority → Self-Updating GoVid. Also Technical Improvements → Proper Version String ("Use the version string when querying the GitHub Releases API").

**Problem.** Users have no way to find out that a newer GoVid release exists.

**What we found**
- The version is injected through `-ldflags "-X main.version=…"` ([main.go:28](../main.go#L28)) and defaults to `"dev"`. It is shown in About.
- [package.ps1](../package.ps1) has its own hand-edited `$Version`. The two can get out of sync.
- Releases are published at `github.com/DunderGG/govid`.

**Proposed solution**
1. Reuse `ReleaseService` from #6 against `DunderGG/govid`. Compare versions as semver (`golang.org/x/mod/semver`). Never prompt when `version == "dev"`.
2. Add a Tools → "Check for GoVid updates" menu item. Also check at startup, sharing #6's daily cache and preference.
3. **Phase 1 (do this now):** show a dialog with the release notes (`body`) and an "Open download page" button (`fyne.CurrentApp().OpenURL(html_url)`).
4. **Phase 2 (later, optional):** update in place. Download the release zip, check its SHA-256 against a checksums file published by `package.ps1`, and extract to `GoVid.exe.new`. Windows does not allow overwriting a running `.exe`, but it does allow renaming one. So rename the running file to `GoVid.exe.old`, move the new file into place, relaunch, and delete `.old` on the next start.
5. Use one source for the version number: derive both `package.ps1`'s `$Version` and the `-X main.version` value from the git tag.

**Done when**
- A build with `-X main.version=0.0.1` shows the update prompt.
- A `dev` build never shows it.

---

## 10. Metadata, thumbnail, and chapter embedding

**Roadmap:** Medium Priority → Metadata & Thumbnail Embedding.

**Problem.** Downloaded files carry no tags, cover art, or chapters. This is the most visible quality gap for audio users: MP3/M4A files show up untitled and without artwork in music players.

**What we found**
- `BuildArgs` passes no `--embed-*` flags.
- Post-processing ([pp_engine.go:541](../pp_engine.go#L541)) relies on ffmpeg's default stream selection (no `-map`). An embedded cover image, and any subtitle streams, would therefore be dropped when a file is post-processed.

**Proposed solution**
1. Add three options: "Embed metadata" (on by default), "Embed thumbnail" (on by default), and "Embed chapters". Save them in `AppPreferences` and the config file, and include them in the `SessionConfig` log.
2. In `BuildArgs`, add `--embed-metadata`, `--embed-thumbnail --convert-thumbnails jpg`, and `--embed-chapters`. Many players cannot show WebP covers in MP3/MP4, hence the conversion to JPG. yt-dlp runs these steps with the bundled ffmpeg (`--ffmpeg-location` is already passed), which covers the roadmap's "via FFmpeg" item.
3. Skip `--embed-thumbnail` for containers that do not support covers (for example `webm`) and log that it was skipped.
4. In the post-processing ffmpeg args, map streams explicitly so embedding still works after post-processing: the first video stream for filtering, all audio and subtitle streams, any attached picture copied with `disposition attached_pic`, plus `-map_metadata 0 -map_chapters 0`.

**Done when**
- An MP3 download shows its title, artist, date, and cover in a music player.
- An MP4 with post-processing enabled keeps its cover and chapters.

---

## Not in the top ten

### Quick wins

Small items that can be done between the ones above:
- **Load URLs from a .txt file** (Batch Downloading): a button that calls `dialog.ShowFileOpen` and appends the file's lines to the batch field. This is the last open item in that section.
- **Named constants for dialog window sizes** (Named Constants): the literals at [ui_manager.go:212](../ui_manager.go#L212), [263](../ui_manager.go#L263), [361](../ui_manager.go#L361), [406](../ui_manager.go#L406), and [537](../ui_manager.go#L537).
- **Default to the OS theme** (Dark / Light Mode Toggle).

### Roadmap cleanup

These items are still unchecked in [roadmap.md](roadmap.md), but the code shows they are done (see [refactor_roadmap.md](refactor_roadmap.md)). They should be ticked off so they don't pad the backlog:
- **Window Management Boilerplate.** Done: `focusOrCreate` and `onWindowClosed` are in [ui_manager.go:89](../ui_manager.go#L89) and are used by all five dialogs.
- **Named Constants:** the post-processing load thresholds and per-filter cost constants. Done: [postprocess.go](../postprocess.go). The values are now 20/50 rather than the 15/35 the roadmap mentions.
- **Split Long Functions.** Done: `runYtDlp` is a ~40-line wrapper, `startDownload` delegates to `readSession`/`runSession`, and `createUI` is made up of nine `build*` helpers.
- **Deduplicate Status Indicator Animation.** Done: `startStatusPulse(base color.RGBA)` handles both states.
