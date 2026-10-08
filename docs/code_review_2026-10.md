# GoVid Code Review — October 2026

> **Audience:** contributors picking up fixes.
> **Reviewed:** commit `54f3d9a` (main), 2026-10-08, against [architecture.md](architecture.md), [coding_guidelines.md](coding_guidelines.md), and the [README](../README.md).

Each finding has an ID (`CR-nn`), a severity, every place it applies, what goes wrong, and a suggested fix. The items are independent unless they say otherwise. Line numbers refer to the reviewed commit, so check them against the current code before editing.

**Severity**

- **High:** a download is lost, or a core flow breaks.
- **Medium:** wrong behaviour users will meet, a data race, or a possible hang.
- **Low:** an edge case, robustness, or a misleading message.
- **Guideline:** maintainability, measured against [coding_guidelines.md](coding_guidelines.md).

**Baseline at review time.** `gofmt -l .`, `go vet ./...`, and `staticcheck` v0.8.1 (the CI version) report nothing. `go test -race ./...` passes; the run took about 215 s, and why it takes that long was not investigated. CR-01 was reproduced with a standalone program. The other findings come from reading the code.

---

## Summary

| ID | Severity | Finding | Main files |
|---|---|---|---|
| [CR-01](#cr-01-a-save-folder-with-brackets-in-its-name-breaks-every-download) | High | A save folder with brackets (`[` `]`) in its name breaks every download | download_engine.go, live.go |
| [CR-02](#cr-02-a-failed-rename-deletes-the-finished-download) | High | A failed rename deletes the finished download | download_engine.go |
| [CR-03](#cr-03-a-stale-skip-in-the-queue-panel-stops-the-session-or-skips-the-wrong-item) | Medium | A stale Skip in the Queue panel stops the session or skips the wrong item | parallel.go |
| [CR-04](#cr-04-with-save-preferences-off-opening-a-settings-window-reverts-the-sessions-settings) | Medium | With "Save preferences" off, opening a settings window reverts the session's settings | ui_manager.go, preference_service.go |
| [CR-05](#cr-05-widgets-are-read-from-background-goroutines) | Medium | Widgets are read from background goroutines | download.go, postprocess.go |
| [CR-06](#cr-06-cancelling-post-processing-is-reported-as-a-failure) | Medium | Cancelling post-processing is reported as a failure | pp_engine.go |
| [CR-07](#cr-07-yt-dlp-updates-are-not-guarded-against-running-downloads) | Medium | yt-dlp updates are not guarded against running downloads | components.go, update_check.go, ui_manager.go |
| [CR-08](#cr-08-output-readers-hang-on-a-line-longer-than-64-kib) | Medium | Output readers hang on a line longer than 64 KiB | logscanner.go, pp_engine.go |
| [CR-09](#cr-09-the-pre-session-log-buffer-is-unbounded-with-unlimited) | Medium | The pre-session log buffer is unbounded with "Unlimited" | log_service.go |
| [CR-10](#cr-10-blocking-file-io-on-the-ui-thread) | Medium | Blocking file I/O on the UI thread | history_window.go, others |
| [CR-11](#cr-11-the-download-summarys-sizes-are-wrong) | Low | The download summary's sizes are wrong | logscanner.go, types.go |
| [CR-12](#cr-12-portable-settings-can-be-saved-out-of-order) | Low | Portable settings can be saved out of order | portable.go |
| [CR-13](#cr-13-removeoldtools-can-delete-the-only-copy-of-a-tool) | Low | `removeOldTools` can delete the only copy of a tool | tool_installer.go |
| [CR-14](#cr-14-uniquepath-can-loop-forever-while-holding-renamemu) | Low | `uniquePath` can loop forever while holding `renameMu` | download_engine.go |
| [CR-15](#cr-15-the-disk-space-check-and-reservation-are-not-atomic) | Low | The disk-space check and reservation are not atomic | download.go, disk_space.go |
| [CR-16](#cr-16-the-cookies-privacy-claim-about-the-session-log-is-inaccurate) | Low | The cookies privacy claim about the session log is inaccurate | ui_manager.go, log_service.go |
| [CR-17](#cr-17-runsession-re-enables-the-ui-before-it-resets-session-state) | Low | `runSession` re-enables the UI before it resets session state | download.go |
| [CR-18](#cr-18-open-folder-leaves-a-zombie-process-on-linux) | Low | Open Folder leaves a zombie process on Linux | helpers.go |
| [CR-19](#cr-19-formats-overwrites-a-running-sessions-status) | Low | Formats… overwrites a running session's status | formats_window.go |
| [CR-20](#cr-20-external-commands-run-without-a-timeout) | Low | External commands run without a timeout | dependency_service.go, components.go |
| [CR-21](#cr-21-ui_managergo-has-too-many-responsibilities) | Guideline | `ui_manager.go` has too many responsibilities (§3.1) | ui_manager.go |
| [CR-22](#cr-22-functions-over-60-lines) | Guideline | Functions over 60 lines (§1.4) | 16 functions |
| [CR-23](#cr-23-unchecked-errors-and-regexes-compiled-per-call) | Guideline | Unchecked errors, and regexes compiled per call (§1.3) | 4 places |
| [CR-24](#cr-24-exported-symbols-without-doc-comments) | Guideline | Exported symbols without doc comments (§1.7) | gpu_capability.go, icons.go |
| [CR-25](#cr-25-a-misplaced-comment-in-downloaderapp) | Guideline | A misplaced comment in `DownloaderApp` | types.go |
| [CR-26](#cr-26-architecturemd-has-drifted-from-the-code) | Guideline | `architecture.md` has drifted from the code | docs/architecture.md |

---

## High

### CR-01: A save folder with brackets in its name breaks every download

**Where.** Every lookup of a download's files by its download ID uses `filepath.Glob(filepath.Join(savePath, "*"+downloadID+"*"))`:

- [download_engine.go:625](../download_engine.go#L625) `hasFiles`
- [download_engine.go:678](../download_engine.go#L678) `FinalizeFiles`
- [download_engine.go:729](../download_engine.go#L729) `RemovePartialFiles`
- [live.go:126](../live.go#L126) `downloadedBytes`

**Problem.** `Glob` reads the *whole* path as a pattern, the folder part included. A folder such as `Videos [HD]` is read as a character class, so it matches nothing. On Windows the brackets cannot be escaped either, because `\` is the path separator. Reproduced: a file `GoVid_Title_GOVID123.mp4` in `…\Videos [HD]\` makes `Glob` return no matches and no error. `*` and `?` cannot be in Windows folder names, but they can on Linux.

**Impact.** A download from such a folder:

- logs "DOWNLOAD COMPLETE", yet its Queue row says Failed (`FinalPaths` is empty);
- keeps its `_GOVID<id>` temporary name;
- gets a history entry with no file, and is not post-processed;
- leaves its partial files behind after a failure, a cancel, or a discarded pause;
- never starts the live recording's time and size display, which waits for a file to appear.

**Suggested fix.** Add one helper and use it in all four places:

```go
// filesWithID returns the files in dir whose names contain downloadID.
func filesWithID(dir, downloadID string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.Contains(entry.Name(), downloadID) {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	return paths, nil
}
```

**Test.** Run the existing `FinalizeFiles` and `RemovePartialFiles` tests with `filepath.Join(t.TempDir(), "Videos [HD]")` as the save folder.

---

### CR-02: A failed rename deletes the finished download

**Where.**
- [download_engine.go:700-706](../download_engine.go#L700-L706): `FinalizeFiles` adds `finalPath` to its result even when `os.Rename` fails.
- [download_engine.go:612](../download_engine.go#L612): after finalizing, `runArgs` calls `RemovePartialFiles`, which deletes *every* file still carrying the download ID, not only partial files.

**Problem.** If the rename fails, the finished media file still has its temporary name, which contains the download ID. The cleanup that follows deletes it. The caller also gets a path that does not exist, so history records it and post-processing tries to open it.

**Impact.** The download is lost even though yt-dlp succeeded. On Windows, an antivirus scanner, the search indexer, or Explorer's thumbnail generator briefly holding a new file open is enough to make a rename fail. `removeWithRetry` already exists for the same kind of lock.

**Suggested fix.**
1. Add `renameWithRetry`, with the same attempts and delay as `removeWithRetry`, and use it in `FinalizeFiles`.
2. If the rename still fails, put `tmpPath` (the file that exists) in the result and log that the file kept its temporary name.
3. Let the cleanup after a *successful* run delete only what `isPartialFile` matches. Either add a `partialOnly bool` to `RemovePartialFiles` or add a separate `RemoveLeftoverPartials`.

**Test.** Make the rename injectable, like `ToolInstaller.rename`. With a failing rename, check that the media file still exists and that `FinalPaths` names it.

---

## Medium

### CR-03: A stale Skip in the Queue panel stops the session or skips the wrong item

**Where.** [parallel.go:110-122](../parallel.go#L110-L122) `skipItem`, called from the row's Skip button at [queue_panel.go:197-201](../queue_panel.go#L197-L201).

**Problem.** When the item has no registered controls, `skipItem` falls back to `RequestCancel`. When an item finishes, `downloadItem`'s deferred `unregister(id)` runs at once. The row keeps showing Skip until the panel next redraws, which is throttled to at least 150 ms. A click in that window reaches the fallback:

- With Simultaneous Downloads above 1, `cancelFn` is the session's `stopQueue`, so the **whole session** is cancelled.
- With one download at a time in a batch, `cancelFn` is the **next** item's skip function, so the wrong video is skipped.

**Why the fallback can go.** The panel only appears for queues of two or more items. In those, `downloadItem` calls `registerSkip` for every item before it does anything else.

**Suggested fix.** Return `false` when no controls are registered for the id, and drop the `RequestCancel` fallback. Optionally also re-check the item's status in the button handler with `queue.Item(id)`.

**Test.** Call `skipItem` with an id that has no controls, and check that the function set with `SetCancelFunc` was not called.

---

### CR-04: With "Save preferences" off, opening a settings window reverts the session's settings

**Where.**
- [ui_manager.go:540](../ui_manager.go#L540): `showPreferences` calls `applyGeneralPrefs(ui, manager.onLoadPreferences())`.
- [ui_manager.go:828](../ui_manager.go#L828): `showPostProcessing` calls `applyPostProcessPrefs(ui, manager.onLoadPreferences())`.
- [ui_manager.go:718](../ui_manager.go#L718): `importConfig` merges onto `onLoadPreferences()`, not onto the current settings.
- [ui_manager.go:1049](../ui_manager.go#L1049): `createUI` takes the theme for its icons from the store, not from the theme widget.
- [portable.go:318](../portable.go#L318): `copySettings` copies the stored settings, so changes made this session do not go with a Portable Mode switch.
- Root cause: [preference_service.go:295-301](../preference_service.go#L295-L301). With `SavePrefs` false, `Save` writes only the toggle, so `Load` returns whatever was saved before persistence was turned off.

**Problem.** The widgets *are* the live settings: `newDownloadRequest` reads them. Reloading them from the store is meant to discard unsaved edits. When persistence is off, though, the store is out of date, so this reload throws away changes the user did save and apply.

**Repro.** Untick "Save preferences" and save. Open Post-Processing, tick Sharpen, then Apply & Close. Open Post-Processing again: Sharpen is unticked, and the next download is not sharpened.

**Suggested fix.** Have `PreferenceService` keep the last saved `AppPreferences` in memory:
- `Save` always updates the in-memory copy, and writes to the store only when `SavePrefs` is true.
- `Load` returns the in-memory copy when there is one, and reads the store otherwise.
- `Reset` clears the in-memory copy.

This fixes every caller above without touching them.

**Test.** In `preference_service_test.go`: `Save(p with SavePrefs=false, Sharpen=true)`, then `Load().Sharpen` must be true. A new `PreferenceService` on the same store must still return the old value.

---

### CR-05: Widgets are read from background goroutines

**Where.** Every widget read on the session goroutine that is not inside `fyne.Do`:

| Location | Read | Called from |
|---|---|---|
| [download.go:516](../download.go#L516) | `notify.Checked` | `notifyCompletion` ← `runSession` |
| [download.go:571](../download.go#L571) | `autoRetry.Checked` | `runYtDlp` ← `downloadItem` |
| [download.go:679](../download.go#L679) | `enablePostProcess.Checked` | `recordHistory` ← `runYtDlp` |
| [postprocess.go:230](../postprocess.go#L230) | `gpuBackend.Selected` | `applyFFmpegFilters` ← `runPostProcessing` |

The other widget accesses in background code are already inside `fyne.Do`. `newDownloadRequest` reads widgets too, but all three of its callers (`readSession`, `resumeQueue`, `showFormatsForURL`) run on the UI thread.

**Problem.** These are data races, which guideline §2.3 forbids. They also give wrong results. `PostProcessed` in the history records the toggle's state when the download *ends*, not what the session will do. It is also true when the toggle is on but no filter is selected, in which case nothing is processed.

**Suggested fix.**
1. Add `autoRetry`, `notify`, and `gpuBackend` to `downloadSession`. Set them in `readSession` ([download.go:98](../download.go#L98)) and in `resumeQueue` ([pause_resume.go:229](../pause_resume.go#L229)), next to `workers`.
2. Pass the session, or the values, to `runYtDlp`, `notifyCompletion`, and `applyFFmpegFilters`.
3. Record `PostProcessed: session.hasPostProcess()` in the history.

**Test.** A session test that changes the widgets after `startSession` and checks that the values the session read did not change.

---

### CR-06: Cancelling post-processing is reported as a failure

**Where.** In [pp_engine.go](../pp_engine.go), `runJob` reaches `failJob` or `retryWithCPU` after a cancel at:
- [:320-328](../pp_engine.go#L320-L328), the fallback without streaming;
- [:342-349](../pp_engine.go#L342-L349), when `cmd.Start` fails;
- [:391-403](../pp_engine.go#L391-L403), when `cmd.Wait` returns an error.

`retryWithCPU` itself is at [:250](../pp_engine.go#L250).

**Problem.** Cancelling post-processing kills ffmpeg, so `Wait` returns an error. For a CPU job that goes to `failJob`, which:
- calls `OnFailure`, so `sessionFailed` is set and the button turns into **Retry**;
- logs `[ERROR] Post-processing failed: exit status 1`.

A GPU job first logs "GPU encode failed — retrying with CPU". Its retry then fails at once with `context canceled`, which is logged as an error too.

**Suggested fix.** At each of the three points, check `ctx.Err()` before retrying or failing. When the context is done, remove `job.tmpOutput`, log the file as cancelled at `colWarning`, and return without calling `OnFailure`. A small `cancelJob(job, cb)` beside `failJob` keeps it in one place.

**Test.** Cancel the context while a job runs (`fake_tool_test.go` has a fake ffmpeg). Check that `OnFailure` is not called and that no CPU retry runs.

---

### CR-07: yt-dlp updates are not guarded against running downloads

**Where.** Every path that runs `yt-dlp -U`:
- [update_check.go:135](../update_check.go#L135): the "Update now" button on the out-of-date notice;
- [ui_manager.go:195](../ui_manager.go#L195) → [:265](../ui_manager.go#L265): Tools → Update yt-dlp (`runUpdateInUI`);
- [components.go:148](../components.go#L148): Components → Update for yt-dlp, which returns before the `installing` guard below it;
- [dependency_service.go:377](../dependency_service.go#L377): `RunUpdate`.

**Problem.** None of these checks `isRunning` or sets `installing`, while `installComponent`'s other actions do both. As a result:

- An update can run during a download. Windows will not replace a running `yt-dlp.exe`, so the update fails, or the download's next retry cannot find its binary.
- A download can start while the update is replacing the binary.

**Suggested fix.** Add `DownloaderApp.updateYtDlp(onDone)` and send all three entry points through it. It should:
1. refuse with `errToolsInUse` while `isRunning`;
2. `CompareAndSwap` `installing`, and disable the Download button;
3. clear both when the update finishes.

`startDownload` already refuses while `installing` is set.

---

### CR-08: Output readers hang on a line longer than 64 KiB

**Where.** Every `bufio.Scanner` that reads a running process:
- [logscanner.go:129](../logscanner.go#L129): yt-dlp's stdout;
- [logscanner.go:153](../logscanner.go#L153): yt-dlp's stderr;
- [pp_engine.go:368](../pp_engine.go#L368): ffmpeg's stderr.

The scanners at [self_update.go:79](../self_update.go#L79) and [url_input.go:128](../url_input.go#L128) read strings already in memory and are not affected.

**Problem.** `bufio.Scanner`'s default token limit is 64 KiB. On a longer line, `Scan` returns false with `ErrTooLong` and the goroutine stops reading. The process then blocks once the pipe buffer is full. `cmd.WaitDelay` only applies after the process has exited, so `Wait` never returns, and the session hangs until GoVid is killed. This is unlikely with today's output, since yt-dlp runs with `--verbose`, but if it happens the hang is total.

**Suggested fix.** At all three places:

```go
scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
for scanner.Scan() { … }
if err := scanner.Err(); err != nil {
	cb.OnLog(…)
	io.Copy(io.Discard, reader) // keep the pipe drained so the process can exit
}
```

**Test.** Feed `watchOutput` a 200 KiB line followed by normal lines through an `io.Pipe`, and check that it returns.

---

### CR-09: The pre-session log buffer is unbounded with "Unlimited"

**Where.** [log_service.go:110-113](../log_service.go#L110-L113). The cap is `bufferLimit`, and [ParseBufferLimit](../log_service.go#L263-L272) turns "Unlimited" into `math.MaxInt32`.

**Problem.** When no session log is open, `WriteToFile` appends every line to `preSession`. That is the default, since "Save output to log file" starts off. With `--newline`, yt-dlp prints hundreds of progress lines per file, so with Log Buffer Limit "Unlimited" memory grows for as long as GoVid runs. Even with the default of 200, the next session log starts with leftover lines from earlier, unrelated downloads.

**Suggested fix.**
- Cap `preSession` with its own constant, such as `preSessionMaxLines = 500`, instead of the UI limit. The buffer exists for startup diagnostics, not as a second scrollback.
- Optionally, once the first session has started, stop buffering, or clear the buffer in `CloseSessionLog`, so a session log holds only its own lines plus the startup lines.

---

### CR-10: Blocking file I/O on the UI thread

Guideline §2.2: anything that can block runs off the UI thread.

**Where.** The main case:
- [history_window.go:178-183](../history_window.go#L178-L183): `showHistory` reads `download_history.json`. Then `newHistoryView` ([:41-45](../history_window.go#L41-L45)) runs `os.Stat` on *every* entry's file. A file on a disconnected network share or USB drive can take seconds to time out, per entry, and the app is frozen meanwhile.

Smaller cases, each usually fast but on the UI thread all the same:
- [pause_resume.go:190](../pause_resume.go#L190): `discardPausedItem` → `newDownloadEngine` → `DependencyService.JSRuntime`. If the runtime search has not finished, this waits on `runtimeMu`, and the search runs each runtime with a 15 s timeout.
- [pause_resume.go:197](../pause_resume.go#L197): `offerQueueRestore` reads `queue.json`.
- [main.go:174](../main.go#L174): `removeOldTools` at startup.
- [download.go:176](../download.go#L176): `openSessionLog` opens the log file in the save folder, which may be a network share.
- [ui_manager.go:712](../ui_manager.go#L712): `importConfig` reads the settings file.
- [portable.go:339](../portable.go#L339) and [:356](../portable.go#L356): `setPortable` → `copySettings` → `fileStore.Flush`, a synchronous write with fsync.

**Suggested fix.**
- **History:** open the window at once with a "Loading…" label, load the history and check the files in a goroutine, then fill the list through `fyne.Do`.
- **`discardPausedItem`:** call `newDownloadEngine` inside the goroutine it already starts.
- The rest can stay as they are; move them only if they show up in the heartbeat log.

---

## Low

### CR-11: The download summary's sizes are wrong

**Where.**
- [logscanner.go:221-237](../logscanner.go#L221-L237) `parseProgress`;
- [types.go:262-267](../types.go#L262-L267) `recordSize`;
- the summary in `reportDownloadResult`, [download.go:702-743](../download.go#L702-L743).

**Problem.**
- `size` is `fields[3]` of `[download]  42.3% of   45.20MiB at …`, which is the **total** size, not the bytes downloaded so far.
- For a merged download, each stream's lines overwrite the last, so the summary's "Downloaded" and "Avg Speed" describe only the final stream, usually the audio.
- Unverified against real output: for fragmented and HLS downloads yt-dlp writes `of ~  45.20MiB`. If so, `fields[3]` is `~`, the summary shows `Downloaded: ~`, and the average speed is N/A.

**Suggested fix.** For the summary, add up the sizes of `DownloadResult.FinalPaths` with `os.Stat` once the download has finished; this is exact and needs no parsing. Keep `parseProgress` for the percentage only, or parse the size with a regex that allows `~`: `of\s+~?\s*([\d.]+\s*[KMGT]?i?B)`.

---

### CR-12: Portable settings can be saved out of order

**Where.** [portable.go:78-92](../portable.go#L78-L92) `fileStore.Flush`.

**Problem.** `Flush` marshals the values under `mu` but writes the file after releasing it. Two flushes can overlap: a timer flush whose snapshot is older, and the flush on quit, which has the latest change. If the older write finishes last, `settings.json` loses the latest change.

**Suggested fix.** Add a `writeMu sync.Mutex`, held from before the marshal until `writeFileAtomic` returns. Alternatively, keep a version number and skip a write whose snapshot is older than the one written last.

---

### CR-13: `removeOldTools` can delete the only copy of a tool

**Where.** [tool_installer.go:452-460](../tool_installer.go#L452-L460).

**Problem.** `swap` renames `final` → `final.old`, then `final.new` → `final`. If GoVid exits between the two, the tool exists only as `.old`, and the next start's `removeOldTools` deletes it along with `.new`. Closing the window during an install does not ask for confirmation, because the close intercept only checks `isRunning`.

**Suggested fix.**
- In `removeOldTools`, when `final` is missing and `final.old` exists, rename `.old` back instead of deleting it.
- Optionally, make the close intercept in [main.go:184](../main.go#L184) also confirm while `installing` or `updating` is set.

---

### CR-14: `uniquePath` can loop forever while holding `renameMu`

**Where.** [download_engine.go:761-777](../download_engine.go#L761-L777). It is called with `renameMu` held from `FinalizeFiles` ([:693](../download_engine.go#L693)) and `finishRecording` ([live.go:226](../live.go#L226), [:242](../live.go#L242)).

**Problem.** A candidate counts as free only when `os.Stat` returns `IsNotExist`. Any other error makes every candidate "taken", for example access denied or a path that is too long. The loop then never ends, and because it holds `renameMu`, every other download blocks when it tries to finish.

**Suggested fix.** Treat a candidate as taken only when `Stat` succeeds (`err == nil`), and stop after a bound such as 10 000 tries, returning the original path. The rename then fails and is reported, so it does not hang.

---

### CR-15: The disk-space check and reservation are not atomic

**Where.**
- [download.go:382-399](../download.go#L382-L399): the check runs under `promptMu`, but `reserveSpace` runs after it is released;
- [disk_space.go:131](../disk_space.go#L131): the check subtracts `reservedBytes`;
- [parallel.go:296](../parallel.go#L296) `reserveSpace`.

**Problem.** With Simultaneous Downloads, worker B can pass its check after worker A's check but before A has reserved its space. Both then count the same free bytes.

**Suggested fix.** Reserve inside the `promptMu` section: either have `checkDiskSpace` return the release function when it decides to proceed, or call `reserveSpace` before `promptMu.Unlock()`.

---

### CR-16: The cookies privacy claim about the session log is inaccurate

**Where.**
- The help text at [ui_manager.go:463](../ui_manager.go#L463): "The session log names only the source … never the file's path".
- The lines are written to the file by [log_service.go:98](../log_service.go#L98) `WriteToFile`.
- [diagnostics_test.go:19](../diagnostics_test.go#L19) depends on the same behaviour.

**Problem.** yt-dlp runs with `--verbose`, and its `[debug] Command-line config: [... '--cookies', 'C:\\Users\\…\\cookies.txt', ...]` line is written to the session log file. Copy diagnostics hides the path with its anonymizer; the log file does not. The *session configuration* block does log only `cookieLabel`, as documented.

**Suggested fix.** Choose one:
1. Hide the path in `appendOutput`, before the line reaches `WriteToFile`, by replacing the cookies path (all three slash forms, as `newAnonymizer.addPath` does) with `<cookies file>`. Keep the current path in an `atomic.Pointer[string]` that is updated whenever the preference is saved.
2. Correct the help text to say the log file may contain the path, but not the cookies.

---

### CR-17: `runSession` re-enables the UI before it resets session state

**Where.** [download.go:205-210](../download.go#L205-L210).

**Problem.** Deferred calls run last-in, first-out, so `finishSessionUI` runs **first**: it queues the Download button's re-enable. Only after it come `isRunning.Store(false)`, `SetCancelFunc(nil)`, `setStopFunc(nil)`, and `stopQueue`. If a new session starts in that gap, the old session's deferred calls clear the new session's cancel and stop functions and its running flag. Cancel then does nothing, and closing the window does not ask for confirmation. It is unlikely, because the re-enable is queued through `fyne.Do`, but nothing in the code prevents it.

**Suggested fix.** Reorder the defers so the state reset happens before the UI is re-enabled: defer `finishSessionUI` first, just after `sessions.Done`, and `isRunning.Store(false)` and the others after it, so they run before it.

---

### CR-18: Open Folder leaves a zombie process on Linux

**Where.** [helpers.go:128](../helpers.go#L128): `openDownloadFolder` calls `.Start()` and never waits on the process. [history_window.go:275](../history_window.go#L275) reaps its process with `go cmd.Wait()`; `startDetached` ([self_update.go:125-128](../self_update.go#L125-L128)) calls `Release`, which is correct there.

**Problem.** On Linux, each click leaves an `xdg-open` zombie until GoVid exits. On Windows, the process handle stays open.

**Suggested fix.** After a successful `Start`, add `go cmd.Wait()`, as `revealHistoryFile` does.

---

### CR-19: Formats… overwrites a running session's status

**Where.** [formats_window.go:278-281](../formats_window.go#L278-L281).

**Problem.** `showFormatsForURL` sets "Status: Checking the formats…" and, once the probe returns, "Status: Idle". The button can be pressed while a session runs, so the session's status label is overwritten until its next update.

**Suggested fix.** Change the status only when `!app.isRunning.Load()`, or show the progress in the Formats window instead of the status label.

---

### CR-20: External commands run without a timeout

**Where.**
- [dependency_service.go:107](../dependency_service.go#L107) `Version`: used by the startup update check, the About window, and the Update yt-dlp dialog;
- [dependency_service.go:380](../dependency_service.go#L380) `RunUpdate` (`yt-dlp -U`);
- [dependency_service.go:412](../dependency_service.go#L412) `UpdateCLI`, for `--update`; a terminal user can stop this one with Ctrl+C;
- [components.go:228](../components.go#L228) `checkFFmpegFilters`.

**Problem.** A hung network connection or a tool that never exits leaves the status reading "Updating yt-dlp…", or the About window reading "checking…", forever. `runVersion` ([dependency_service.go:168](../dependency_service.go#L168)) already uses `toolCommandTimeout` for the same kind of call.

**Suggested fix.** Use `exec.CommandContext` with a timeout: `toolCommandTimeout` for the version and filter queries, and a few minutes for `yt-dlp -U`. Better still, have `Version` reuse `runVersion`.

---

## Guideline

### CR-21: `ui_manager.go` has too many responsibilities

**Guideline:** §3.1, one responsibility per file.

**Where.** [ui_manager.go](../ui_manager.go) is 1,549 lines. It holds the main window layout, the menu, the About window, a 173-line help text, the Preferences window, the Post-Processing window, settings import and export, notices, and log rendering.

**Suggested split.** Move code only, with no behaviour change:

| New file | Moves |
|---|---|
| `help_window.go` | `showConfigHelp`, `codeList` (the help text could also become a `[]helpItem` package variable) |
| `preferences_window.go` | `showPreferences`, `buildCookiesRow`, `buildCookiesFileRow`, `setVisible`, `submitPreferences`, `onKeepHistoryChanged`, `applyRuntimePrefs`, `confirmRestoreDefaults`, `restoreDefaults`, `loadConfigFile`, `importConfig`, `applyAndSavePreferences`, `showImportSettings`, `showExportSettings`, `savePreferences` |
| `postprocess_window.go` | `showPostProcessing`, `wirePostProcessHandlers`, `bindDependents`, `buildLoadIndicator`, `sizeWarning`, `buildPostProcessForm`, `buildPostProcessFooter` |
| `log_view.go` | `logFlushInterval`, `maxScreenLogLines`, `followTolerance`, `pendingLogLine`, `screenLogLimit`, `appendLogLine`, `takePendingLog`, `flushLog`, `renderLogLines`, `isScrolledToBottom`, `clearTerminalOutput`, `pendingLogLines` |
| `notices.go` | `notice`, `showNotice`, `dismissNotice`, `renderNotices`, `buildNotice` |
| `ui_manager.go` (kept) | `UIManager`, window singletons, `createMainMenu`, `showAbout`, the yt-dlp update delegates, `createUI` and its builders, `followSystemTheme` |

Update the file map in architecture.md §3 at the same time.

---

### CR-22: Functions over 60 lines

**Guideline:** §1.4. About 60 lines is a smell threshold, not a hard limit. Every function in the package over 60 lines:

| Lines | Function | Suggestion |
|---|---|---|
| 173 | [ui_manager.go `showConfigHelp`](../ui_manager.go#L349) | Mostly data. Move the items to a package-level `helpItems` variable (see CR-21); the function is then about 25 lines. |
| 165 | [pp_engine.go `runJob`](../pp_engine.go#L285) | Split out `streamFFmpeg(cmd, job, guard, cb) (errLines, error)` and `reportJobDone(job, sizeBefore, duration, cb)`. The `StderrPipe` fallback ([:315-340](../pp_engine.go#L315-L340)) cannot really happen before `Start` and duplicates the rename and retry logic; remove it and treat a pipe error like a start error. Fold in CR-06 while splitting. |
| 117 | [download.go `downloadItem`](../download.go#L323) | Extract `prepareItem` (probe, live prompt, quality and subtitle reports, "Will download"), `checkItemSpace` (disk check and reservation; see CR-15), `registerItemControls` (the Skip/Pause/recording switch), and `itemStatus(dl, ctx) queueStatus`. |
| 110 | [pp_engine.go `ApplyFilters`](../pp_engine.go#L968) | Extract `planJob(ctx, path, vf, af, cb) (PostProcessJob, bool)` for the per-file loop body, and `runWorkerPool(ctx, jobs, cb)`. |
| 102 | [download_engine.go `BuildArgs`](../download_engine.go#L190) | Extract `trimArgs(req) (args, start, end)` and `containerArgs(extension)`, as `liveArgs`, `embedArgs`, and `subtitleArgs` already are. |
| 98 | [main.go `newDownloaderApp`](../main.go#L40) | Mostly wiring. Split into `wireUIManager(dlApp)` and `wirePrompts(dlApp)`, grouped as the comments already group it. |
| 86 | [postprocess.go `buildPostProcessFilters`](../postprocess.go#L135) | Extract `smoothMotionFilter`, `denoiseFilter`, and `upscaleFilter`, the three switches. |
| 79 | [formats_window.go `showFormatWindow`](../formats_window.go#L80) | Extract the filter select and the button bar. |
| 79 | [download.go `reportDownloadResult`](../download.go#L698) | Extract `summaryFor(dl, …) (title, rows, colours, status, state)` as a pure function, and test it table-driven (§4.1). |
| 77 | [logscanner.go `watchOutput`](../logscanner.go#L116) | Move each goroutine body into `scanStdout` and `scanStderr`, with stderr's classification in `classifyStderrLine(line, *scanResult)`. Fold in CR-08. |
| 71 | [postprocess.go `computeProcessingLoad`](../postprocess.go#L374) | Extract `processingCost(settings) int` and `describeLoad(cost) string`. |
| 69 | [ui_manager.go `buildInputCard`](../ui_manager.go#L1262) | Extract `buildURLRow` and `buildSelectorsRow`. |
| 64 | [history_window.go `showHistory`](../history_window.go#L173) | Extract `buildHistoryList(view, actions)`. Fold in CR-10. |
| 61 | [ui_manager.go `showPreferences`](../ui_manager.go#L532) | Extract `buildPreferencesForm()` and the Portable Mode toggle wiring. |
| 61 | [playlist_dialog.go `showPlaylistDialog`](../playlist_dialog.go#L49) | Borderline; extract the button list. |
| 61 | [diagnostics.go `diagnosticsReport`](../diagnostics.go#L284) | Borderline; extract `writeToolsSection(report)`. |

---

### CR-23: Unchecked errors, and regexes compiled per call

**Guideline:** §1.3, always check errors.

**Where.**
- [download.go:863](../download.go#L863) `validateTimestamp`: `matched, _ := regexp.MatchString(…)` ignores the error and compiles the regex on every call. It runs on every keystroke, as the validator of both trim fields. Make it a package-level `regexp.MustCompile` variable, as the rest of the code does (for example `progressLinePattern` in logscanner.go).
- [logscanner.go:227](../logscanner.go#L227) `fmt.Sscanf(field, "%f%%", &val)`: on failure `val` stays 0, and a progress of 0 % is reported. Use `strconv.ParseFloat(strings.TrimSuffix(field, "%"), 64)` and skip the field on error.
- [types.go:266](../types.go#L266) `fmt.Sscanf(size, "%f%s", …)`: on failure the previous `downloadedRaw`/`unit` survive next to the new `lastSize`. Parse first, and update all three only on success (see also CR-11).
- [ui_manager.go:176](../ui_manager.go#L176) `parseURL`: `url.Parse` errors are ignored. It is only called with constant URLs, so this is acceptable; say so in its doc comment, or panic on error as `MustCompile` would.

---

### CR-24: Exported symbols without doc comments

**Guideline:** §1.7, every exported symbol has a doc comment.

**Where.**
- [gpu_capability.go:33-39](../gpu_capability.go#L33-L39): `BackendAuto`, `BackendOff`, `BackendNVIDIA`, `BackendIntel`, `BackendAMD`, `BackendVAAPI`, `BackendVideoToolbox`.
- [icons.go:24-27](../icons.go#L24-L27): `IconDownload`, `IconFolderOpen`, `IconFolder`, `IconCancel`.

**Suggested fix.** Add a comment above each constant block, or a one-line comment on each constant.

Exported *methods* that implement an interface have no doc comments either:
- [portable.go:123-249](../portable.go#L123-L249), `fileStore`'s `fyne.Preferences` methods;
- [theme.go](../theme.go) lines 71, 124, 128, 132, 158, 162, 166, 170, 194, 198, 202, and 206, the `fyne.Theme` methods;
- [formats_window.go:184](../formats_window.go#L184) and [:188](../formats_window.go#L188), `fixedWidthLayout`'s `MinSize` and `Layout`;
- [http_fetch.go:88](../http_fetch.go#L88), `progressWriter.Write`.

These are optional. If the team wants them covered, one comment per group (for example "fyne.Preferences methods, backed by values") is enough.

---

### CR-25: A misplaced comment in `DownloaderApp`

**Where.** [types.go:352-357](../types.go#L352-L357).

**Problem.** The comment "The heartbeat (see startHeartbeat): heartbeatStop stops it … runOnUI replaces fyne.Do …" now sits above `settingsStore`, `portable`, and `settingsNote`, merged into their comment. The heartbeat fields it describes (`heartbeatMu`, `heartbeatStop`, `runOnUI`) are at lines 362-364. A later insertion probably split them apart.

**Suggested fix.** Move the heartbeat sentences down to `heartbeatMu`, and leave only the settings sentences above `settingsStore`.

---

### CR-26: `architecture.md` has drifted from the code

**Where.** In [architecture.md](architecture.md):

| Line | Says | Should say |
|---|---|---|
| [15](architecture.md#L15) | Language: Go 1.24+ | Go 1.26+ (go.mod has `go 1.26.1`; the README says 1.26+) |
| §3 file map | — | add `ui_snapshot.go`: snapshotPreferences, newPostProcessSettings, newSessionConfig, applyPreferencesToWidgets and its three groups |
| [89](architecture.md#L89) | helpers.go holds `applyPreferencesToWidgets` | it is in ui_snapshot.go |
| [173](architecture.md#L173) | "Beyond the five `show*` methods" | there are now more (`showComponents`, `showQueue`, `showNotice`, `showFormatWindow`, …); drop the count |
| [177](architecture.md#L177) | `savePreferences`, `resetPreferences`, `rebuildUI` | `savePreferences`, `restoreDefaults` (the other two were merged) |
| [251](architecture.md#L251) | `applyPreferencesToWidgets` in helpers.go; `DownloaderApp.savePreferences` delegate; `resetPreferences`/`rebuildUI` | ui_snapshot.go; the delegate was removed (`startDownload` calls `uiManager.savePreferences`); `restoreDefaults` |
| [360](architecture.md#L360) | "Updating in place … is not implemented." | delete the sentence; the next paragraph documents `self_update.go` |

**Suggested fix.** Make the edits above, and follow the checklist in architecture.md §10 when fixing the other items here. CR-01, CR-02, CR-05, and CR-21 each change what §4 describes.
