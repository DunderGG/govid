# GoVid — Next Ten Priorities

The next ten items to work on, in order. They follow the first ten in [priorities.md](priorities.md), which are all done. Each item was checked against the code as it was on 2026-10-06, after commit `50d7081`.

How the order was chosen:

1. **Fix what the last round left behind (1–2).** The playlist work added a second yt-dlp extraction to every download. Also, the filename's quality label is wrong in two common cases.
2. **Then a quick high-priority win (3).** "Load from file" is the last open High Priority item in Batch Downloading.
3. **Then the features users notice most (4–8).** Several reuse the URL probe (`DownloadEngine.Probe`) from the last round, which already returns the video's title, size, and formats. #7 is the base that #8 builds on.
4. **Then release infrastructure (9–10).** #9 makes the local release script publish a checksum next to the zip. #10 needs that checksum before it can safely replace the running app. #9 does not depend on anything else, so it can be done earlier if that suits.

Source references point to the matching section of [roadmap.md](roadmap.md). Tick items off here and in the roadmap as they land.

---

## 1. ✅ Download from the probe's answer instead of extracting twice

**Roadmap:** Follow-up to Playlist Support and Disk Space Pre-check. Not listed on its own; the cost was noted when #7 of the first round was done.

**Status: Done.**
- **Loading the probe's JSON.** For a single video, `Probe` now keeps the JSON it read (`MediaInfo.raw`) and when it read it (`probedAt`). `downloadItem` passes it as `DownloadRequest.InfoJSON`. `DownloadEngine.Run` writes it to a temporary `govid-*.info.json` and `BuildArgs` passes `--load-info-json <file>` *instead of* the URL, because yt-dlp would download a URL given alongside it as well. The file goes in the temp folder rather than the save folder, so `FinalizeFiles` cannot mistake it for a download, and it is removed when `Run` returns. The JSON is dropped from memory once used.
- **Playlist entries.** `checkItem` probes an item with no fresh answer right before it downloads, under the item's own context, so Cancel/Skip stops the probe too. It uses the new `ProbeVideo` (`--no-playlist`), so a "Only this video" link is probed as that video, not as its playlist. `checkDiskSpace` now runs after it, so playlist entries get a size check.
- **Fallback.** `scanResult.hadExpiredLinkErr` is set by an `ERROR:` line with HTTP 403 or 410. A run from loaded info that fails this way is repeated once from the URL, after its partial files are removed. An answer older than `probeMaxAge` (30 minutes) is probed again when its turn comes.
- **One deviation:** a URL whose first probe *failed* is not probed again before downloading (`queueItem.probeFailed`). A second probe would most likely fail the same way, and it would add requests, which this item is meant to reduce. The download still reports the problem.

Tests: `TestStartDownloadExtractsSingleVideoOnce` and `TestStartDownloadExtractsEachPlaylistEntryOnce` (the fake yt-dlp now counts extractions, and refuses a URL given together with `--load-info-json`), `TestRunRetriesFromURLWhenLoadedLinksExpire`, `TestRunDoesNotRetryOtherFailuresFromURL`, `TestRunLoadsInfoJSONAndRemovesIt`, `TestBuildArgsLoadsInfoJSONInsteadOfURL`, `TestStartDownloadPlaylistEntryChecksDiskSpace`, and `TestQueueItemNeedsProbe`.

**Problem.** Every single-video download now makes yt-dlp extract the video twice. Videos picked from a playlist get no size check and no title.

**What we found**
- `checkURLs` ([playlist.go:48](../playlist.go#L48)) runs `Probe` on every URL. For a single video that is a full extraction ([probe.go](../probe.go)). `runYtDlp` then passes the URL to yt-dlp again, which repeats the extraction. Each extraction makes several requests to the site. That adds a few seconds per video and doubles the request volume that sets off YouTube's "Sign in to confirm you're not a bot" check.
- Playlist entries become `queueItem{url}` with `info == nil`. `checkDiskSpace` therefore skips them ("size unknown"), and nothing about them is known before the download.

**Proposed solution**
1. Have `Probe` also return the raw JSON it read. Write it to a temporary `<downloadID>.info.json` and pass `--load-info-json <file>` to yt-dlp instead of the URL. The `-f` selector, cookies, `--download-sections`, and the embed flags all still apply, because yt-dlp runs format selection again on the loaded info. Delete the file when the run ends.
2. For items with `info == nil` (playlist entries, failed probes), probe right before downloading, inside `downloadItem`, using the item's own context so Cancel/Skip stops the probe too. Then run `checkDiskSpace` with the result. Playlist entries then get a size check, and the rest of this list gets their title and resolution.
3. Fallback: format URLs expire (on YouTube after about six hours) and can be tied to the IP address that probed them. If a run from `--load-info-json` fails with HTTP 403/410, retry once with the URL. Probe again if the saved info is more than ~30 minutes old when the item's turn comes, which can happen in long batches.

**Done when**
- In tests, the fake yt-dlp counts exactly one extraction per downloaded video, whether it came from a single URL or a playlist.
- A test where the loaded info returns 403 shows the retry with the URL.
- A playlist entry that doesn't fit on the disk triggers the disk-space prompt.

---

## 2. Correct quality labels and the "Smart Downscale" notice

**Roadmap:** Technical Improvements → Automatic "Best-Fit" Quality ("Show a 'Smart Downscale' notification if the requested resolution isn't available").

**Problem.** The quality label in the filename is often wrong, and the user is never told when they get a different resolution from the one they asked for.

**What we found**
- **Audio files get a video label.** `formatSelection` ([download_engine.go:75](../download_engine.go#L75)) works out `height` before it returns early for MP3/M4A. `BuildArgs` ([download_engine.go:123](../download_engine.go#L123)) then adds `"_" + req.Quality` whenever `height != ""`. With Quality left at 1080p, an MP3 is saved as `GoVid_<title>_1080p.mp3`. Nothing resets Quality when an audio format is chosen.
- **The label shows the cap, not the result.** A video whose best version is 720p, downloaded with the 1080p setting, is still named `_1080p`.
- **The cap isn't firm.** Every capped selector ends in a plain `/best`. On sites that only offer combined formats above the cap, yt-dlp falls back to `best` and downloads a *higher* resolution than the user asked for, without saying so.

**Proposed solution**
1. Return an empty `height` from `formatSelection` for audio formats, so audio files get no quality label.
2. Build the label from what was actually downloaded: `_%(height)sp` in the output template instead of `_<req.Quality>`. Keep a capped label only when the user picked a cap, as today.
3. Add `Height int json:"height"` to `MediaInfo`. For a merged download, yt-dlp sets the top-level `height` from the chosen video format. After the probe (and the per-item probe from #1):
   - **Lower than asked:** log and show "1080p isn't available for this video; downloading 720p (the best there is)."
   - **Higher than asked** (the `/best` fallback): warn "No version at or below 480p; downloading 1080p."
4. Add table tests for `formatSelection` and the template, covering audio, capped, and Best.

**Done when**
- An MP3 has no `_1080p` in its name.
- A 720p-only video downloaded with the 1080p setting is named `_720p` and shows the downscale notice.

---

## 3. Faster ways to add URLs: load from file, paste, drop

**Roadmap:** High Priority → Batch Downloading ("Load from file" button). Also Low Priority → Clipboard Paste Button, and UI & UX → Drag-and-Drop Support.

**Problem.** The only way to add URLs is to type or paste them into the field. "Load from file" is the last open High Priority batch item.

**Proposed solution**
1. **Load from file.** Add a button next to the URL field that opens `dialog.ShowFileOpen` filtered to `.txt`. Read the file one line at a time, skip blank lines and lines starting with `#`, append the URLs to the batch field without duplicates, and switch on batch mode. Validate the lines with the same rules `collectURLs` ([download.go](../download.go)) uses, and log how many lines were skipped and why.
2. **Paste button.** Add a small clipboard icon next to the field. It reads `fyne.CurrentApp().Clipboard().Content()` and accepts the text only if every non-blank line looks like a URL. Several lines switch on batch mode.
3. **Drop files.** Use `window.SetOnDropped`. Fyne 2.7 has it, but it only delivers *files*. A dropped `.txt` is loaded as in step 1. A dropped `.url` file (a Windows internet shortcut, an INI file with a `URL=` line) is added as a URL.
4. **Links dragged from a browser** probably won't arrive, because the GLFW layer only accepts file drops. Run a short spike to confirm before promising it. If it doesn't work, the guide should say "drag the link to the desktop first, then drop the shortcut", or the roadmap item should be closed as not possible.

**Done when**
- Loading a 50-line file with blanks and comments fills the batch field correctly.
- The paste button refuses non-URL text.
- Dropping a `.txt` file or a `.url` shortcut onto the window adds its URLs.

---

## 4. Subtitle support

**Roadmap:** Medium Priority → Subtitle Support.

**Problem.** There is no way to download subtitles. Post-processing already keeps subtitle streams (since #10 of the first round), so only the download side is missing.

**Proposed solution**
1. In Preferences, add **Subtitles: Off / Embed / Save as .srt / Both**, a **Languages** field (yt-dlp `--sub-langs` syntax, default `en.*`), and an **Include auto-generated** checkbox (off by default: auto-captions are noisy and add more requests). Save them in `AppPreferences` and `govid.json`, and include them in the `SessionConfig` log.
2. In `BuildArgs`, add `--write-subs [--write-auto-subs] --sub-langs <langs> --convert-subs srt`, plus `--embed-subs` for Embed. yt-dlp converts embedded subtitles per container (mov_text for MP4, any format for MKV, WebVTT only for WebM). Skip all of this for MP3/M4A and log that it was skipped.
3. **Check the "Both" case.** yt-dlp deletes the subtitle files after embedding them unless told to keep them. Confirm which flag keeps the `.srt` file next to the video before offering "Both".
4. **Keep sidecars out of the media list.** Sidecar files carry the `GOVID<id>` token, so `FinalizeFiles` renames them along with the video and returns them as final paths. `ApplyFilters` ([pp_engine.go:964](../pp_engine.go#L964)) and `recordHistory` would then treat an `.srt` file as a media file. Filter sidecars out of the list given to post-processing and history.
5. Read `subtitles` and `automatic_captions` from the probe's JSON. Log which languages exist, and warn when none of the requested ones do.
6. **Failures.** Find out whether a failed subtitle download (YouTube often answers 429) fails the whole download. If it does, retry once without subtitles and say so.
7. **Trimming.** Subtitles are not cut to a trim range. For trimmed downloads, either skip sidecar files or note in the guide that they cover the whole video.

**Done when**
- An MP4 with Embed has a selectable subtitle track.
- "Save as .srt" leaves a correctly named `.srt` next to the video, and post-processing doesn't touch it.
- A video without the requested language downloads normally, with a warning.

---

## 5. Better download history

**Roadmap:** Low Priority → Download History (real source title, warn on duplicates, keep-history toggle). Also Technical Improvements → UX Improvements ("a button to each history entry to quickly re-add to URL field").

**Problem.** History stores a title guessed from the filename and is shown as one block of disabled text, so it can't do anything useful.

**What we found**
- `recordHistory` ([download.go](../download.go)) passes no title. `HistoryService.buildEntries` guesses one with `inferOriginalTitle(filename)`.
- `showHistory` ([ui_manager.go](../ui_manager.go)) shows all entries as text in a disabled `MultiLineEntry`. The "Clear History" button and its confirmation already exist.

**Proposed solution**
1. **Real titles.** Put the probe's `Title` (or the playlist entry's title) on the `queueItem`, carry it into `DownloadRecord.Title`, and keep `inferOriginalTitle` only as a fallback.
2. **Duplicate warning.** Read `id` and `extractor_key` from the probe's JSON and store them in new `videoID`/`extractor` fields on `DownloadHistoryEntry`. Matching on these catches the same video under different URL forms (`youtu.be/…`, `watch?v=…&t=…`). Old entries without them fall back to an exact URL match. In `checkURLs`, ask "Already downloaded on <date> as <file>. Download again / Skip". In a batch, add "Skip all duplicates".
3. **Keep-history toggle.** Add a "Keep download history" preference, on by default. When it's off, `recordHistory` does nothing. Turning it off asks whether to clear the existing history too.
4. **History window.** Replace the text block with a `widget.Table`/`List`: date, title, format/quality, plus row actions **Re-add** (appends the URL to the field, switching on batch mode if the field isn't empty), **Show in folder** (`explorer /select,<file>`), and **Copy URL**. Add a search field. Grey out entries whose file no longer exists.

**Done when**
- New entries show the real title.
- Pasting a `youtu.be` link to a video already downloaded via `watch?v=` shows the duplicate prompt.
- Re-add puts the URL back in the field.

---

## 6. Queue panel

**Roadmap:** Medium Priority → Queue Manager (per-item status, cancel, retry, reordering). Pause/resume and saving the queue across restarts are left for later; see below.

**Problem.** Playlists now turn into dozens of queue items, but the queue can only be followed through `── URL i of N ──` lines in the log. Nothing in the queue can be changed once it starts.

**What we found**
- `runQueue`/`downloadItem` ([download.go](../download.go)) walk `session.items` by index. The per-item cancel context in `downloadItem` already supports "skip this one".

**Proposed solution**
1. Add a `QueueModel`: a mutex-guarded list of items, each with a title (from the probe), a status (Waiting / Checking / Downloading x% / Post-processing / Done / Failed / Skipped), and an `OnChanged` callback. Send its UI refreshes through `latestValueThrottle`, as for the status label.
2. Change `runQueue` to take the next *Waiting* item from the model instead of indexing `session.items`. Items can then be removed or reordered while the queue runs.
3. Show it in a collapsible "Queue" card (a `widget.List`) above the log, visible when there is more than one item. Row actions:
   - **Waiting:** Remove, Move up, Move down.
   - **Downloading:** Skip (the existing per-item cancel).
   - **Failed:** Retry (puts the item back at the end of the queue).
   Show an overall counter, such as "7 of 20 done, 1 failed".
4. Later: **pause/resume** conflicts with the current `--no-part --no-continue` flags. It would need yt-dlp's `.part` files and `--continue`, plus partial-file cleanup that understands them. **Saving the queue across restarts** comes after that.

**Done when**
- During a 20-item playlist download, the user can remove a waiting item, move one to the front, skip the active one, and retry a failed one, and each row shows the right status.

---

## 7. Make the config file cover every setting

**Roadmap:** Low Priority → Config File Support ("Override all the other preferences as well from the file").

**Problem.** `govid.json` covers only seven settings, and there is no way to save the current settings to a file. Presets (#8) and Portable Mode both need settings as a file, so this comes first.

**What we found**
- `AppConfig` ([preference_service.go:313](../preference_service.go#L313)) has format, quality, path, max speed, and the three embed toggles. `AppPreferences` has 35 fields: theme, cookies, log limit, notify, auto-retry, every post-processing filter and its settings, GPU backend, and more.

**Proposed solution**
1. Give `AppConfig` a field for every `AppPreferences` field, using pointers (`*bool`, `*string`, `*float64`) so a field left out of the file changes nothing. This is the pattern the embed toggles already use.
2. Validate every option field against the lists centralised in [options.go](../options.go). Range-check numbers such as FPS and sharpen amount. Report all errors together, as `MergeConfig` does today.
3. Add **Tools → Export settings…**, which writes the current preferences as a complete `govid.json`.
4. Add a test that every `AppPreferences` field has a config key, so a new preference can't be forgotten again. Use reflection over the two structs.
5. Update the guide's config section and `govid.json` in the repo with the full key list.

**Done when**
- Exporting, changing every setting, then loading the exported file restores identical `AppPreferences` (tested as a round trip).
- The field-coverage test fails when a preference is added without a config key.

---

## 8. Presets

**Roadmap:** Medium Priority → Presets / Profiles.

**Problem.** Users who switch between setups (audio-only, 1080p MP4, archive) have to change several settings by hand each time.

**Proposed solution**
1. A preset is a name plus a *partial* `AppConfig` from #7, holding only the settings the user chose to include. Applying one is `MergeConfig` onto the current preferences followed by `applyPreferencesToWidgets`, so no new apply logic is needed.
2. Store the presets as a JSON list in the preferences store. Add a **Preset** dropdown next to Format/Quality in the input card, plus **Save current as preset…** (with checkboxes for which settings to include) and **Manage presets** (rename, delete).
3. Ship three starter presets: *Audio (MP3, metadata + cover)*, *1080p MP4*, and *Archive (MKV, Best, subtitles, chapters)*. Archive needs #4.
4. Import/export: one JSON file holding a list of presets. Use the same reader as `govid.json`, so validation errors are reported the same way.
5. Show "(modified)" next to the preset name once the user changes a setting after applying it.

**Done when**
- Choosing a preset sets exactly the settings it contains and leaves the others alone.
- Exported presets import on another machine.

---

## 9. Checksums and a safer release script

**Roadmap:** Follow-up to Windows Distribution, and the prerequisite for Self-Updating GoVid's "update in place" item.

**Problem.** Self-update (#10) can't verify a downloaded release, because releases don't publish a checksum. The release script can also produce a broken zip without stopping.

Releases stay manual: you tag a commit, run `package.ps1`, and upload the result yourself. Nothing here publishes anything automatically.

**What we found**
- [package.ps1](../package.ps1) is intentionally kept out of git (`*.ps1` is in [.gitignore](../.gitignore)). It builds, bundles `external/yt-dlp.exe` and `external/ffmpeg.exe`, and zips the result.
- It writes no checksum, and nothing records which yt-dlp and ffmpeg builds went into a release.
- If `yt-dlp.exe` or `ffmpeg.exe` is missing from `external/`, the script only warns and still creates the zip, so a release could ship without them.

**Proposed solution** (all in the local `package.ps1`)
1. After zipping, write `SHA256SUMS` next to the zip with `Get-FileHash -Algorithm SHA256`, in the standard `<hash>  <file name>` format. Upload it to the GitHub release together with the zip.
2. Stop with an error instead of a warning when `yt-dlp.exe` or `ffmpeg.exe` is missing.
3. Print the bundled tool versions (`yt-dlp --version`, the first line of `ffmpeg -version`) and add them to `SHA256SUMS` as comment lines, or to a small `VERSIONS.txt` inside the zip. Each release then records what it shipped.
4. Print a short checklist at the end: upload the zip and `SHA256SUMS` to the release for tag `<version>`. #10 relies on both being there.

**Done when**
- Running the script on a tagged commit produces `GoVid_<tag>_Ready.zip` plus a `SHA256SUMS` whose hash matches `Get-FileHash` of the zip.
- Running it with `ffmpeg.exe` missing from `external/` fails without creating a zip.

---

## 10. Self-update in place

**Roadmap:** High Priority → Self-Updating GoVid ("Update in place").

**Problem.** GoVid already tells users about a new release (first round's #9), but they still have to download and unpack it by hand.

**Proposed solution** (depends on #9)
1. In the release dialog ([release_dialog.go](../release_dialog.go)), add an **Update now** button next to "Open download page". Show it only for release builds (`buildType == "release"`), on Windows, when no download is running.
2. Find the `GoVid_<tag>_Ready.zip` and `SHA256SUMS` assets in the `assets` that `ReleaseService` already parses. If either is missing (for example, in a release made before #9), show only "Open download page". Otherwise, download them to a temporary folder with progress shown in the status bar, and verify the zip's SHA-256. On a mismatch, stop and keep the downloaded file for inspection.
3. Extract only `GoVid.exe`, as `GoVid.exe.new` next to the running exe. yt-dlp updates itself, and the bundled ffmpeg rarely changes. Check first that the folder is writable, reusing the check from the yt-dlp update.
4. Swap: rename the running `GoVid.exe` to `GoVid.exe.old` (Windows allows renaming a running exe, though not overwriting it), move `GoVid.exe.new` into place, start the new exe, and quit through `DownloaderApp.Shutdown`. If any step fails, rename `.old` back.
5. On startup, delete a leftover `GoVid.exe.old`.
6. Note in the guide that antivirus or SmartScreen may scan the new exe on its first start.

**Done when**
- A release build tagged one version behind updates itself to the latest test release and restarts on the new version.
- A corrupted download (wrong checksum) leaves the installed version untouched.

---

## Next after these

- **Format Browser** (Medium): the probe's JSON already holds `formats`, so a table of resolution, codec, bitrate, and container can be built without another yt-dlp call.
- **Copy Diagnostics** (UI Performance & Stability → Observability): GoVid/yt-dlp/ffmpeg versions, OS, GPU detection results, current settings, and the last ~200 log lines, copied to the clipboard for bug reports.
- **Default to the OS theme** (Dark / Light Mode Toggle): today `applyTheme` ([theme.go:175](../theme.go#L175)) falls back to dark for any value except Light. Add a "System" option that follows the OS.
- **Portable Mode**: builds on #7. Read and write `settings.json` beside the exe instead of `%AppData%\fyne\com.govid.downloader`.
- **Named constants for dialog window sizes** (still literals in `ui_manager.go`).

## Still open from the first round

These were done in code but still need the hand checks noted in [priorities.md](priorities.md):
- **#5:** compare a real HDR YouTube video, after tone mapping, with the browser's SDR rendering.
- **#8:** the nearly-full USB stick test.
- **#10:** an MP3 download showing title, artist, date, and cover in a music player.

The roadmap cleanup listed at the end of [priorities.md](priorities.md) is also still pending. [roadmap.md](roadmap.md) still shows the Window Management, Named Constants (thresholds and costs), Split Long Functions, and status-animation items as open, although the code has them done.
