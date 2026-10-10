# GoVid — Design Decisions

> **Audience:** contributors and maintainers.
> **Read the entry before you change the code it names.** Each one explains a choice that looks odd, roundabout, or missing until you know the reason.

This document records *why* GoVid works the way it does, when the code alone doesn't tell you. Most entries are one of three kinds:

- **A workaround** for how yt-dlp, FFmpeg, Fyne, GitHub, or Windows behaves. These were found by testing, so the entry gives the version and the date.
- **A trade-off**, where the obvious approach was tried or considered and rejected.
- **Something left out on purpose**, so that nobody adds it without knowing why it was left out.

Other things go elsewhere. How the code is put together is in [architecture.md](architecture.md), and what is done or planned is in [roadmap.md](../roadmap.md). A reason that matters inside one function only belongs in a comment in that function. Some reasons already sit next to what they explain: the GPU choices (backends, the encoder session cap, the stall watchdog, WebM on the CPU) are in [gpu-acceleration.md](gpu-acceleration.md), and the concurrency rules are in [architecture.md §7](architecture.md#7-concurrency-model). They are not repeated here.

**External tools change.** If a newer yt-dlp, FFmpeg, or Fyne behaves differently, test it again. Then update the entry with the new version and date, or delete the entry together with its workaround. An entry is no reason to keep a workaround that the tools no longer need.

**Numbers.** Each entry has a number, such as DD-12, for referring to it from commits, code comments, and other documents. The number belongs to the entry, not to its position: a new entry takes the next unused number, wherever it goes in the document, and the number of a deleted entry is never used again.

**Adding an entry.** Put it under the matching section, add it to the contents, and use this form:

```markdown
### DD-NN: Short name

**Decision.** What GoVid does, or does not do, in one or two sentences.

**Why.** What goes wrong otherwise, or what testing showed. For an external
tool, give the version and the date.

Code: `file.go` (`symbol`) · Commit: `abc1234`
```

The commit named in an entry has the full context. The first entries were collected from the priority lists worked through on 2026-10-06 and 2026-10-07.

## Contents

- [Processes and files](#processes-and-files)
  - [DD-01: Killing a tool's process tree](#dd-01-killing-a-tools-process-tree)
  - [DD-02: Finding a download's files by its token](#dd-02-finding-a-downloads-files-by-its-token)
  - [DD-03: Never globbing the save folder](#dd-03-never-globbing-the-save-folder)
  - [DD-04: Retrying renames and deletes](#dd-04-retrying-renames-and-deletes)
  - [DD-05: Partial files and resuming](#dd-05-partial-files-and-resuming)
  - [DD-06: Each queue item keeps its own settings](#dd-06-each-queue-item-keeps-its-own-settings)
  - [DD-07: Saving the queue when GoVid quits](#dd-07-saving-the-queue-when-govid-quits)
  - [DD-08: Checking free space](#dd-08-checking-free-space)
  - [DD-09: Writing GoVid's own files atomically](#dd-09-writing-govids-own-files-atomically)
- [yt-dlp](#yt-dlp)
  - [DD-10: Extracting each video once](#dd-10-extracting-each-video-once)
  - [DD-11: Playlists become separate queue items](#dd-11-playlists-become-separate-queue-items)
  - [DD-12: Reading both of yt-dlp's output streams](#dd-12-reading-both-of-yt-dlps-output-streams)
  - [DD-13: Keeping `--verbose`](#dd-13-keeping---verbose)
  - [DD-14: Measuring download sizes on disk](#dd-14-measuring-download-sizes-on-disk)
  - [DD-15: The quality label comes from the downloaded height](#dd-15-the-quality-label-comes-from-the-downloaded-height)
  - [DD-16: Subtitle flags](#dd-16-subtitle-flags)
  - [DD-17: A JavaScript runtime for YouTube](#dd-17-a-javascript-runtime-for-youtube)
  - [DD-18: Stopping a live recording keeps it](#dd-18-stopping-a-live-recording-keeps-it)
  - [DD-19: Cookies from the browser](#dd-19-cookies-from-the-browser)
  - [DD-20: At most three downloads at once](#dd-20-at-most-three-downloads-at-once)
  - [DD-21: Preferred codec instead of favourite formats](#dd-21-preferred-codec-instead-of-favourite-formats)
- [FFmpeg and post-processing](#ffmpeg-and-post-processing)
  - [DD-22: HDR tone mapping](#dd-22-hdr-tone-mapping)
  - [DD-23: Explicit stream mapping, and Matroska covers](#dd-23-explicit-stream-mapping-and-matroska-covers)
  - [DD-24: Covers as JPEG, and none in WebM](#dd-24-covers-as-jpeg-and-none-in-webm)
- [Updates, tools, and releases](#updates-tools-and-releases)
  - [DD-25: Comparing versions as numbers, not semver](#dd-25-comparing-versions-as-numbers-not-semver)
  - [DD-26: GitHub rate limits](#dd-26-github-rate-limits)
  - [DD-27: FFmpeg from its versioned folder](#dd-27-ffmpeg-from-its-versioned-folder)
  - [DD-28: Three checksum formats](#dd-28-three-checksum-formats)
  - [DD-29: Replacing a file that may be running](#dd-29-replacing-a-file-that-may-be-running)
  - [DD-30: No installs or updates during a download](#dd-30-no-installs-or-updates-during-a-download)
  - [DD-31: Automatic update checks for yt-dlp only](#dd-31-automatic-update-checks-for-yt-dlp-only)
  - [DD-32: One source for the version: the git tag](#dd-32-one-source-for-the-version-the-git-tag)
  - [DD-33: The release script](#dd-33-the-release-script)
- [Settings](#settings)
  - [DD-34: One config key for every preference](#dd-34-one-config-key-for-every-preference)
  - [DD-35: Settings apply for the session with "Save preferences" off](#dd-35-settings-apply-for-the-session-with-save-preferences-off)
  - [DD-36: Option labels are defined once, and stored as they are](#dd-36-option-labels-are-defined-once-and-stored-as-they-are)
  - [DD-37: The default format depends on the platform](#dd-37-the-default-format-depends-on-the-platform)
  - [DD-38: Renamed preference keys are migrated](#dd-38-renamed-preference-keys-are-migrated)
  - [DD-39: Presets](#dd-39-presets)
  - [DD-40: Portable Mode is a marker file](#dd-40-portable-mode-is-a-marker-file)
- [User interface](#user-interface)
  - [DD-41: The log view's cap](#dd-41-the-log-views-cap)
  - [DD-42: Dropping links from a browser](#dd-42-dropping-links-from-a-browser)
  - [DD-43: F1 and Esc outside text fields](#dd-43-f1-and-esc-outside-text-fields)
  - [DD-44: Rebuilding the window when the system theme changes](#dd-44-rebuilding-the-window-when-the-system-theme-changes)
  - [DD-45: Fyne dialog and scrolling quirks](#dd-45-fyne-dialog-and-scrolling-quirks)
  - [DD-46: What the Queue panel lets you change](#dd-46-what-the-queue-panel-lets-you-change)
  - [DD-47: What the diagnostics report hides](#dd-47-what-the-diagnostics-report-hides)
- [Tests](#tests)
  - [DD-48: The Fyne test driver runs `fyne.Do` inline](#dd-48-the-fyne-test-driver-runs-fynedo-inline)

---

## Processes and files

### DD-01: Killing a tool's process tree

**Decision.** Cancel kills the whole yt-dlp or FFmpeg process tree: on Windows with `taskkill /T /F`, not a Job Object, and on Unix through the process group. `cmd.WaitDelay` is 3 s.

**Why.** yt-dlp starts its own ffmpeg to merge and trim, and the Windows `yt-dlp.exe` (a PyInstaller bundle) runs as more than one process. Killing only the direct child left processes that used CPU and kept the output file locked. A Job Object would have to be attached after `Start`, which callers that use `Output` or `CombinedOutput` cannot do. `WaitDelay` stops `Wait` from hanging on pipes that a surviving child still holds open.

Start every new tool command through `newToolCommand`, and read its output with `newOutputScanner`. The time limits and the long-line handling that go with them are explained in [architecture.md](architecture.md) §4.9 and §7.

Code: `process.go` (`newToolCommand`), `sys_windows.go` (`killProcessTree`) · Commit: `4709ce8`

### DD-02: Finding a download's files by its token

**Decision.** yt-dlp writes every file of a download with a `GOVID<id>` token in its name. Cleanup, finalizing, subtitle splitting, and resuming all find the files by that token, and `FinalizeFiles` removes it from the final name. A custom filename template cannot leave the token out, and cannot contain `/` or `\`.

**Why.** The token is the only reliable way to tell which files in the save folder belong to this download, including leftovers from earlier runs and files that yt-dlp's own post-processors create. A template with subfolders would need `FinalizeFiles` to search them, and it doesn't.

Code: `download_engine.go` (`filesWithID`, `FinalizeFiles`, `RemovePartialFiles`), `filename_template.go` (`outputTemplate`) · Commits: `4709ce8`, `23c151f`

### DD-03: Never globbing the save folder

**Decision.** Code that looks for a download's files lists the folder and compares the names (`filesWithID`). It never uses `filepath.Glob` on a path that includes the save folder.

**Why.** `filepath.Glob` reads `[` and `]` in a folder's name as part of the pattern. With a save folder such as `Videos [HD]`, every glob matched nothing: finished downloads were never renamed, and partial files were never removed. The same goes for any other folder the user chooses.

Code: `download_engine.go` (`filesWithID`) · Commit: `e43b8c3`

### DD-04: Retrying renames and deletes

**Decision.** Removing a download's partial files and renaming its finished ones are each tried up to 10 times, 200 ms apart. A finished file whose rename still fails keeps its temporary name, and the log says so. It is never cleaned up with the partial files.

**Why.** On Windows, a killed process can keep its files locked for a moment after it exits. An antivirus scanner, the search indexer, or Explorer's thumbnails can also hold a new file open briefly. Before this change, a rename that failed for one of these reasons led the cleanup to delete the finished download.

Code: `download_engine.go` (`renameWithRetry`, `RemovePartialFiles`, `RemoveLeftoverPartials`), `live.go` (`finishRecording`) · Commit: `e80ba6c`

### DD-05: Partial files and resuming

**Decision.** Downloads use yt-dlp's `.part` files and `--continue`, and each queue item keeps one download ID for its whole life. `FinalizeFiles` skips `.part`, `.part-Frag*`, `.ytdl`, and `.temp` files. Live recordings are the exception: they keep `--no-part`.

**Why.** GoVid used to pass `--no-part --no-continue` and make a new ID on every run. A retry, an auto-retry, or a resume could then never find the earlier partial file, and every interrupted download started from zero. A live recording cannot be resumed, and with `.part` files a stopped recording would be left as a `.part` file. Checked with yt-dlp 2026.03.17 against a local server on 2026-10-07: paused at 2,096,128 bytes, the next run logged `Resuming download at byte 2096128` and asked for `Range: bytes=2096128-`.

Code: `download_engine.go` (`BuildArgs`, `isPartialFile`, `newDownloadID`) · Commit: `7ed7660`

### DD-06: Each queue item keeps its own settings

**Decision.** The settings are read once, when the session starts, and copied onto each queue item. Changing Format during a batch therefore doesn't change the items that are still waiting.

**Why.** A retry, an auto-retry, a resume, or an item restored from `queue.json` has to write the same file names and use the same format as its first run. Otherwise `--continue` has nothing to continue.

Code: `download.go` (`downloadSession.request`, `queueItem.withRequest`) · Commit: `7ed7660`

### DD-07: Saving the queue when GoVid quits

**Decision.** Quitting pauses the running download instead of cancelling it, and the waiting and paused items are written to `queue.json`. The file holds no cookies and no probe JSON. It is written only when GoVid quits.

**Why.** The probe JSON is left out because its format links expire (see [DD-10](#dd-10-extracting-each-video-once)), so every restored item is probed again. Writing the file only on quit is a known gap, not a requirement: after a crash the queue is lost, although its partial files stay in the save folder. Writing the file whenever the queue changes would close the gap.

Code: `queue_store.go`, `pause_resume.go` · Commit: `7ed7660`

### DD-08: Checking free space

**Decision.** The disk check measures the nearest folder that exists. It asks for the size estimate plus 10%, doubled when post-processing is on, and it subtracts the space reserved by downloads already running. A live recording skips the check. Instead, it checks every 30 s while recording and stops, keeping the file, below 1 GiB.

**Why.** yt-dlp creates the save folder itself, so the folder may not exist yet. Post-processing writes a temporary output next to the source file. Without the reservation, parallel downloads would each see the same free space. A live stream has no size to check in advance.

Code: `disk_space.go` (`checkDiskSpace`), `parallel.go` (`reserveSpace`), `live.go` · Commits: `c689a53`, `53ed3b6`, `dbf66ee`, `0d7bd9b`

### DD-09: Writing GoVid's own files atomically

**Decision.** Every file GoVid writes for itself goes through `writeFileAtomic`: the download history, `queue.json`, Portable Mode's `settings.json`, and exported settings and presets. It writes a temporary file in the same folder, syncs it to disk, and renames it over the old file.

**Why.** `os.WriteFile` empties the file before it writes the new content. A crash or power cut in between left `download_history.json` corrupt, and every later load of it failed. A rename within one folder replaces the file in one step.

Code: `history_service.go` (`writeFileAtomic`) · Commit: `43bcea9`

---

## yt-dlp

### DD-10: Extracting each video once

**Decision.** The URL check's probe keeps the JSON it read. The download writes it to a temporary file and passes `--load-info-json <file>` *instead of* the URL. The file goes in the temp folder and is removed when the run ends. Several exceptions apply:
- An answer older than 30 minutes is probed again.
- A run from a loaded answer that fails with HTTP 403 or 410 runs once more from the URL.
- A URL whose first probe failed is not probed again.

**Why.**
- Without this, every download extracted the video twice. That added a few seconds per video and doubled the requests that set off YouTube's "Sign in to confirm you're not a bot".
- yt-dlp also downloads a URL given alongside `--load-info-json`, so the URL has to be left out.
- In the save folder, `FinalizeFiles` could take the JSON file for part of the download.
- Format links expire (on YouTube after about six hours) and can be tied to the IP address that probed them. yt-dlp falls back to the URL by itself for some failures, such as a subtitle error, but not for a failed format download (checked with yt-dlp 2026.03.17 on 2026-10-06). GoVid therefore needs its own retry.
- A second probe of a URL that failed would most likely fail the same way, and would only add requests.

Code: `probe.go` (`probeMaxAge`), `download_engine.go` (`Run`, `BuildArgs`), `logscanner.go` (`hadExpiredLinkErr`), `playlist.go` (`probeFailed`) · Commit: `3070e2c`

### DD-11: Playlists become separate queue items

**Decision.** The URL check probes with `--flat-playlist`. The videos the user picks are queued as separate URLs, each downloaded with `--no-playlist`. GoVid never passes `--yes-playlist`. The playlist's total size is shown as unknown.

**Why.** For a `playlist?list=` URL, yt-dlp ignores `--no-playlist` and downloads every video inside one queue item. That item would have one progress bar and one Cancel for all of them, and every file would be under one download ID. As separate items, each video gets its own progress row, Skip, retry, history entry, and duplicate-name handling, with no extra queue code. Knowing the size would need a full extraction of every video, which is too slow to do before the user has chosen.

Code: `playlist.go` (`checkURLs`), `probe.go` · Commit: `4d623e9`

### DD-12: Reading both of yt-dlp's output streams

**Decision.** `watchOutput` looks for phase markers and errors on stdout and stderr alike.

**Why.** yt-dlp prints `[Merger]` on stdout, but GoVid used to look for it only on stderr, so a merge was never noticed. Found with the bundled yt-dlp on 2026-10-06.

Code: `logscanner.go` (`watchOutput`, `detectPhase`) · Commit: `863f465`

### DD-13: Keeping `--verbose`

**Decision.** yt-dlp always runs with `--verbose`. Its `[debug]` lines go to the log file, but stay out of the log view unless Debug Output is on.

**Why.** Turning off `--verbose` would remove the noise from the view, but it would also leave the log file too thin for a bug report. Filtering the view keeps both. One cost: `--verbose` prints yt-dlp's command line, including the cookies file's path, so every log line has to be masked (see [DD-19](#dd-19-cookies-from-the-browser)).

Code: `log_service.go` (`IsDebugLine`), `download_engine.go` (`BuildArgs`) · Commit: `701623f`

### DD-14: Measuring download sizes on disk

**Decision.** The sizes in the download summary come from the files on disk (`DownloadResult.Bytes`), measured when the run ends, not from yt-dlp's progress lines. A resumed download's average speed counts only the bytes this run wrote.

**Why.** The size in `[download]  42.3% of   45.20MiB` is the total size, not the amount downloaded so far. For a merged download, each stream's lines replace the previous stream's, so the summary described only the last stream, usually the audio. Fragmented downloads may also print `of ~  45.20MiB`, which a split on spaces reads as `~`.

Code: `download_engine.go` (`Run`, `DownloadResult`), `download.go` · Commit: `de184e0`

### DD-15: The quality label comes from the downloaded height

**Decision.** A download with a quality cap gets `%(height&_{}p|)s` in its file name, which yt-dlp fills in from the height it actually downloaded. Audio formats get no label and no height cap.

**Why.** The label used to show the cap, so a 720p-only video downloaded with the 1080p setting was still named `_1080p`. An MP3 with Quality left at 1080p was named `_1080p.mp3`. The `&…|` form writes nothing when the height is unknown, where `%(height)s` would write `_NAp`. Checked with yt-dlp 2026.03.17 on 2026-10-06.

Code: `download_engine.go` (`formatSelection`, `heightLabel`) · Commit: `0f09628`

### DD-16: Subtitle flags

**Decision.**
- Every subtitle mode passes `--write-subs` (plus `--write-auto-subs` when auto-generated captions are wanted), `--sub-langs <langs>`, and `--convert-subs srt`. Subtitles embedded in WebM use `vtt` instead of `srt`.
- **Embed** adds `--embed-subs --compat-options no-keep-subs`. **Both** adds only `--embed-subs`.
- A download whose subtitles fail is repeated once without them.
- Subtitles are not cut to a trim range.

**Why.** Tested on 2026-10-06 with yt-dlp 2026.03.17, through `--load-info-json`, against a local HTTP server that served a video, WebVTT subtitles, and a subtitle URL answering 429:

| Flags | Result |
| --- | --- |
| `--embed-subs` | embedded, `.srt` deleted |
| `--write-subs --embed-subs` | embedded, `.srt` kept |
| `--write-subs --embed-subs --compat-options no-keep-subs` | embedded, `.srt` deleted |
| `--write-auto-subs --embed-subs` | only the auto captions embedded; the manual ones ignored |
| a subtitle URL answering 429 | the whole download fails (exit 1) |

`--write-subs` is always passed because `--write-auto-subs` on its own ignores the manual subtitles. WebM can only hold WebVTT. YouTube often answers subtitle requests with 429, and losing the whole video for that is worse than downloading it without subtitles.

Code: `download_engine.go` (`subtitleArgs`, `splitSubtitleFiles`), `logscanner.go` (`hadSubtitleErr`) · Commit: `310d8c0`

### DD-17: A JavaScript runtime for YouTube

**Decision.** GoVid looks for `bin/deno` first, then for deno, node, and bun on `PATH`. It passes the first one it finds to both the probe and the download as `--js-runtimes name:path`, always with the full path. The minimum versions are Deno 2.3.0, Node 22.0.0, and Bun 1.2.11 up to 1.3.14. Deno is not in the release ZIP; Tools → Components installs it on demand.

**Why.** Since late 2025, yt-dlp needs a JavaScript runtime to solve YouTube's player challenges. Without one, it falls back to a deprecated client that may miss formats and could stop working at any time. yt-dlp enables only deno by default, and looks for it only on `PATH`, which `bin/` is not on. The minimum versions come from yt-dlp's EJS wiki page, which marks Bun as deprecated. yt-dlp also supports QuickJS, but GoVid doesn't look for it: it is rarely installed, and older versions can take minutes per challenge. Checked on 2026-10-07: the bundled yt-dlp 2026.03.17 reported `JS runtimes: none` and the deprecation warning. With the Deno v2.9.7 that GoVid installed, it reported `JS runtimes: deno-2.9.7`.

Code: `dependency_service.go` (`JSRuntime`), `tool_installer.go` · Commit: `d36b463`

### DD-18: Stopping a live recording keeps it

**Decision.**
- **Stop recording** cancels the download with the cause `errStopKeep`. The files are then finalized instead of removed.
- `finishRecording` remuxes the recording with ffmpeg, without re-encoding, into the chosen container. If that container can't hold the streams, it uses MKV instead (WebM can't hold H.264). If the remux fails, the file is kept as `.ts`.
- Recordings use the normal format selector, not `-f b`.
- A recording runs under its own context, so stopping the session or quitting keeps it.
- The probe passes `--ignore-no-formats-error`.

**Why.**
- yt-dlp hands live HLS to ffmpeg with `--hls-use-mpegts`, and fixes the container only when a stream ends normally.
- Killed after 30 s, the recording was 30.8 s of valid MPEG-TS under an `.mp4` name. Forcing a merged selector gave the same result. So the fix is a remux afterwards, not a different selector.
- The remux keeps only video and audio, because HLS streams also carry ID3 data streams, which MP4 cannot hold.
- A scheduled stream has no formats yet, so the probe failed on it without `--ignore-no-formats-error`.
- Before this change, Cancel deleted the whole recording.

Tested on 2026-10-07 with yt-dlp 2026.03.17 against a public live HLS test stream (`demo.unified-streaming.com`, through yt-dlp's generic extractor), because YouTube asked this machine to sign in. A real YouTube live stream is still a hand check in the roadmap.

Code: `live.go` (`finishRecording`), `download_engine.go` (`errStopKeep`), `pause_resume.go` (`downloadContext`), `probe.go` · Commit: `dbf66ee`

### DD-19: Cookies from the browser

**Decision.** Preferences → Cookies lists Firefox first. GoVid recognises the errors yt-dlp prints for Chrome and Edge, and says what to do instead. The session log and the diagnostics report name only the cookie source ("Cookies: Firefox", "file set"), never the file path or the profile name. Settings saved before the Cookies choice existed are read as **From file** when a cookies file was set.

**Why.** On Windows, Chrome and Edge lock their cookie database while they are open, and encrypt cookies with app-bound encryption, which yt-dlp cannot decrypt. Firefox works. Recorded with yt-dlp 2026.03.17 on 2026-10-07, against a local URL so that no cookie left the machine:

| Case | yt-dlp's line |
| --- | --- |
| Chrome open (database locked) | `ERROR: Could not copy Chrome cookie database. See  https://github.com/yt-dlp/yt-dlp/issues/7271  for more info` |
| Chrome or Edge closed (app-bound encryption) | `ERROR: Failed to decrypt with DPAPI. See  https://github.com/yt-dlp/yt-dlp/issues/10927  for more info` |
| A Firefox profile that doesn't exist | `ERROR: could not find firefox cookies database in '…'` |

Cookies are the user's login, and a path or profile name can identify the user. yt-dlp runs with `--verbose`, and its `[debug] Command-line config` line names the `--cookies` file. `cookiesPathMask` therefore replaces that path in every log line, and it remembers every path passed during the run, because the preference can change while a download that uses the old file is still logging. Reading old settings as From file means nobody's cookies stopped working after the update.

Code: `cookies.go` (`cookieArgs`, `classifyAccessError`, `cookieLabel`), `preference_service.go` (`resolveDefaults`) · Commits: `5089e18`, `6fdfd11`

### DD-20: At most three downloads at once

**Decision.** Simultaneous Downloads is 1 to 3, and 1 by default. Workers start 3 s apart. After an HTTP 429 or a bot check, only one worker runs for the rest of the session.

**Why.** More requests at the same time make YouTube's bot check more likely, and the setting's hint says so. A 429 or a bot check shows that the site is already limiting GoVid, so GoVid stops adding parallel requests.

Code: `parallel.go` (`runParallel`, `workerStagger`, `backOff`) · Commit: `53ed3b6`

### DD-21: Preferred codec instead of favourite formats

**Decision.** The roadmap's "pin or favourite preferred formats" item is a **Preferred Video Codec** setting, which adds `-S vcodec:…`, not a list of pinned format IDs. In the Format Browser, a format with no codec listed counts as having one.

**Why.**
- Format IDs differ from video to video, so a pinned ID means nothing on the next video.
- A codec in `-S` sorts before resolution. H.264 therefore picks 1080p even when 4K VP9 is available (checked with yt-dlp 2026.03.17 on the test fixture, 2026-10-07), and the guide says so.
- Counting a missing codec as present keeps YouTube's HLS audio formats 233 and 234 in the list, as `yt-dlp -F` does (it shows them as "unknown").
- For the fixture in `testdata/`, yt-dlp needs each format's `url`, plus `manifest_url`, `available_at`, and `http_headers`, to size and order HLS formats. The fixture keeps those fields, with every URL replaced by `https://example.invalid/…`.

Code: `formats.go`, `formats_window.go`, `testdata/ytdlp_info_formats.json` · Commit: `0209323`

---

## FFmpeg and post-processing

### DD-22: HDR tone mapping

**Decision.**
- Each file's colour tags are probed, and only PQ (`smpte2084`) and HLG (`arib-std-b67`) sources are tone mapped.
- The filter chain states the input's transfer, matrix, and primaries, and the output is tagged BT.709.
- A BT.2020 file with no transfer tag is treated as PQ.
- Without ffprobe, the tags are read from the stream summary that `ffmpeg -i` prints.

**Why.**
- When a frame's colour tags are missing, `zscale` assumes BT.709, which gives the washed-out picture. Tags often go missing when yt-dlp merges VP9 or AV1 streams.
- VP9 and AV1 bitstreams carry the matrix but not the transfer, so the transfer tag is the one a merge loses.
- Running the filter on SDR sources changed ordinary videos too.
- ffprobe is not in the release ZIP. Without the fallback, release users would never get tone mapping.

Checked on 2026-10-06 with the bundled ffmpeg 8.1 and `testdata/hdr_pq_sample.mkv`. With the transfer tag removed, the old chain produced no output, and the new chain produced the same frames as for the tagged clip.

Code: `pp_engine.go` (`resolveToneMap`, `probeColorInfo`), `postprocess.go` (`toneMapFilter`) · Commit: `cc2ab7e`

### DD-23: Explicit stream mapping, and Matroska covers

**Decision.** Post-processing maps every stream explicitly:
- the main video, which is filtered;
- each attached picture, copied with `-disposition attached_pic`;
- all audio, subtitle, and attachment streams;
- plus `-map_metadata 0 -map_chapters 0`.

For MKV output, the covers are extracted to temporary images and added back with `-attach`.

**Why.** FFmpeg's default stream selection dropped the cover and the subtitles: before this change, a post-processed MP4 lost both. FFmpeg cannot write a mapped cover back to Matroska as an attachment, even with `-map 0 -c copy`; it becomes a stray video track instead.

Code: `pp_engine.go` (`probeStreamLayout`, `buildFFmpegArgs`) · Commit: `50d7081`

### DD-24: Covers as JPEG, and none in WebM

**Decision.** `--embed-thumbnail` is always paired with `--convert-thumbnails jpg`, and is skipped for WebM.

**Why.** Many players cannot show WebP covers in MP3 or MP4, and WebM cannot hold cover art at all.

Code: `download_engine.go` (`embedArgs`) · Commit: `50d7081`

---

## Updates, tools, and releases

### DD-25: Comparing versions as numbers, not semver

**Decision.** `compareVersions` compares versions part by part, as numbers, for both yt-dlp and GoVid.

**Why.** Both use date versions (`2025.09.26`, `2026.04.11`). The leading zeros make them invalid semver, so `golang.org/x/mod/semver` would reject them. Comparing numbers also handles yt-dlp's nightly versions, and avoids a dependency.

Code: `release_service.go` (`compareVersions`) · Commits: `d7d6789`, `773b52c`

### DD-26: GitHub rate limits

**Decision.** An HTTP 403 or 429 from the GitHub API means "unknown", not an error, and is cached like an answer. When the API refuses, the tool installer uses the `/releases/latest/download/<file>` links instead, and reads the version from where `/releases/latest` redirects.

**Why.** GitHub rate-limited the development machine while both features were being built. A rate-limited check repeated at every start would never succeed, and would only add requests. The download links are not rate limited.

Code: `release_service.go` (`errReleaseUnknown`), `tool_installer.go` · Commits: `d7d6789`, `d36b463`

### DD-27: FFmpeg from its versioned folder

**Decision.** The installer reads gyan.dev's `release-version`, then downloads the ZIP and its `.sha256` from that version's `packages/` folder.

**Why.** The unversioned links are redirects. They could move to a new release between the two requests, which would give a hash that doesn't belong to the ZIP.

Code: `tool_installer.go` · Commit: `d36b463`

### DD-28: Three checksum formats

**Decision.** The installer reads three checksum formats: `<hash>  <name>` lines (also with `*name`), a bare hash, and the output of PowerShell's `Get-FileHash`, including CRLF line ends and a UTF-16 copy.

**Why.** That is what the three sources publish (checked on 2026-10-07): yt-dlp's `SHA2-256SUMS` has `<hash>  <name>` lines, gyan.dev's `.sha256` holds just the hash, and Deno's `.sha256sum` is `Get-FileHash` output (`Hash : A0C3…`, upper case).

Code: `self_update.go` (`parseSHA256Sums`), `tool_installer.go` (`parseBareHash`, `parseGetFileHash`) · Commit: `d36b463`

### DD-29: Replacing a file that may be running

**Decision.**
- Self-update and the tool installer write the new file as `.new`, rename the old one to `.old`, and move the new one in. If any step fails, the old file is put back.
- Self-update extracts only `GoVid.exe` from the release ZIP.
- At the next start, GoVid deletes `GoVid.exe.old`, retrying for up to 15 s.
- A tool found only as `.old` at startup is moved back instead of deleted.
- **Update now** is offered only for release builds (`main.buildType == "release"`) on Windows, and only when the release has both its ZIP and `SHA256SUMS`.

**Why.**
- Windows lets a running `.exe` be renamed, but not overwritten or deleted.
- The previous GoVid may still be closing when the new one starts, because Shutdown waits up to 5 s. A test confirmed that `.old` couldn't be deleted ("Access is denied") until the old process had exited.
- A tool left only as `.old` means GoVid stopped between the two renames, so that file is the only copy.
- yt-dlp updates itself, and the bundled FFmpeg rarely changes, so self-update replaces only GoVid.
- A development build is not the file a release would replace, and a release without `SHA256SUMS` cannot be verified.

Code: `self_update.go` (`Install`, `cleanUpAfterUpdate`, `canSelfUpdate`), `tool_installer.go` (`removeOldTools`) · Commits: `158ea36`, `d36b463`, `980dac7`

### DD-30: No installs or updates during a download

**Decision.** Installing a tool, updating yt-dlp, and a download session never overlap. An install or update refuses to start while a session runs. While one runs, it holds `installing` and keeps the Download button disabled. This applies to every way of starting a yt-dlp update: Tools → Update yt-dlp, the out-of-date notice, and Components.

**Why.** Windows will not replace a running `yt-dlp.exe`. An update during a download therefore fails, or the download's next retry cannot find its binary. The other way round, a download could start while the binary is being replaced.

Code: `components.go` (`installComponent`, `updateYtDlp`) · Commits: `d36b463`, `232704a`

### DD-31: Automatic update checks for yt-dlp only

**Decision.** At startup, GoVid checks for new yt-dlp and GoVid releases, but not for new FFmpeg or Deno releases. The Components window shows their latest versions whenever it opens.

**Why.** yt-dlp is the tool that breaks when sites change. FFmpeg and Deno rarely need an update for GoVid to keep working, so they don't need a startup check.

Code: `update_check.go`, `components_window.go` · Commits: `d7d6789`, `d36b463`

### DD-32: One source for the version: the git tag

**Decision.** `build.bat`, `build.sh`, and `package.ps1` all take the version from `git describe --tags --exact-match`. The build scripts fall back to `dev`, and `package.ps1` refuses to package an untagged commit.

**Why.** `package.ps1` used to have its own hand-edited version, which could disagree with the `-X main.version` value. The update check compares that version with the latest release tag, so the two must match.

Code: `build.bat`, `build.sh`, `package.ps1` · Commit: `773b52c`

### DD-33: The release script

**Decision.**
- `package.ps1` is kept out of git (`*.ps1` is in `.gitignore`), so changes to it don't appear in any commit.
- It writes `SHA256SUMS` next to the ZIP, in the `sha256sum` format: lower-case hex, two spaces, LF line ends, no BOM.
- It records the bundled tool versions in `VERSIONS.txt` inside the ZIP, not as comment lines in `SHA256SUMS`.
- It captures each tool's whole output before it takes the first line.

**Why.**
- In that format, both `sha256sum -c` and GoVid can check the file.
- Some checkers reject comment lines.
- `Select-Object -First 1` stops the pipeline early, which made `$LASTEXITCODE` unreliable.
- A related finding: Windows PowerShell's `Compress-Archive` stores entry names with backslashes (`bin\ffmpeg.exe`), so the updater accepts either separator.

Code: `package.ps1` (local only), `self_update.go` · Commit: `cad4f26` (documentation only)

---

## Settings

### DD-34: One config key for every preference

**Decision.** `AppConfig` has a pointer field for every `AppPreferences` field, with the same name. `applyConfig` copies the fields that are set using reflection, and `configRules` validates them. A test fails when a preference has no config key. For a choice or for `path`, `""` leaves the setting unchanged. `"maxSpeed": ""` is the exception: it means unlimited.

**Why.**
- `govid.json` once covered only seven of 35 settings, because each new setting had to be added to it by hand.
- With pointers, a missing key changes nothing. Presets rely on this, since a preset is a partial `AppConfig`.
- With reflection, merging cannot forget a field.
- Older files with `"format": ""` still load.
- An exported "unlimited" speed has to load back as unlimited.
- Unknown keys are ignored, so a file written by a newer GoVid still loads in an older one.

Code: `config_file.go` (`AppConfig`, `applyConfig`, `configRules`, `parseAppConfig`) · Commit: `3e9bc11`

### DD-35: Settings apply for the session with "Save preferences" off

**Decision.** `PreferenceService` keeps the last saved settings in memory, and `Load` returns that copy. With "Save preferences" off, only the toggle itself is written to the store, but the settings still apply until GoVid quits. Code that needs the settings in use calls `Load` rather than reading the store.

**Why.** The widgets hold the live settings, and several windows reload them when they open, to discard edits that weren't saved. With persistence off, the store is out of date, so reloading from it threw away changes the user had saved and applied. For example: tick Sharpen in Post-Processing, press Apply & Close, open the window again, and Sharpen was unticked. A Portable Mode switch carried the same out-of-date copy.

Code: `preference_service.go` (`Load`, `Save`), `portable.go` (`copySettings`) · Commit: `8eee93a`

### DD-36: Option labels are defined once, and stored as they are

**Decision.** Every selector's labels are named constants, each with an ordered list, in `options.go`. The widgets, defaults, argument builders, config rules, and help text all use those names. The label text itself ("MP4", "Best Quality") is what the settings store, presets, and `govid.json` hold.

**Why.** The labels used to be string literals repeated across files, so renaming one in one place silently sent that choice down another code path's default branch. Because the label is also the stored value, renaming one means that saved settings, presets, and config files holding the old label no longer match any option. A rename therefore needs a migration of the stored values, as a renamed key does ([DD-38](#dd-38-renamed-preference-keys-are-migrated)).

Code: `options.go` · Commit: `338b92b`

### DD-37: The default format depends on the platform

**Decision.** A new install defaults to MP4 on Windows and macOS, and to MKV elsewhere (`defaultFormat`). A test that needs a setting to differ from its default uses a value that is never the default on any platform, such as WebM for the format.

**Why.** MP4 plays natively on Windows and macOS. A test that changed Format to MKV passed on Windows but failed on the Ubuntu CI runner, where MKV already is the default.

Code: `preference_service.go` (`defaultFormat`), `config_file_test.go` (`changedPreferences`) · Commits: `d03ea61`, `927deb7`

### DD-38: Renamed preference keys are migrated

**Decision.** When a preference's storage key changes, `migrateLegacyKeys` moves a value stored under the old key to the new one and removes the old key. The old key stays defined as a `legacyPref…` constant.

**Why.** Without the move, everyone's saved value silently falls back to the default. Smooth Motion used to be stored under `upscale`, a name from before the separate Upscale feature existed, and was moved to `smoothMotion` this way.

Code: `preference_service.go` (`migrateLegacyKeys`, `legacyPrefSmoothMotion`) · Commit: `a19bb96`

### DD-39: Presets

**Decision.**
- Presets are saved even when "Save preferences" is off, and Restore Defaults keeps them.
- Deleting every preset stores an empty list.
- App-wide settings (theme, log limit, update checks, history) can't go in a preset.

**Why.** Presets only change when the user changes them on purpose. The empty list stops the starter presets from coming back. App-wide settings are not part of a download setup.

Code: `presets.go` (`LoadPresets`), `preset_ui.go` · Commit: `ff85390`

### DD-40: Portable Mode is a marker file

**Decision.** GoVid is in Portable Mode when a `GoVid.portable` file exists beside the executable. The toggle acts when it is ticked, not on Save, and it is not in `AppConfig`. The Fyne store is opened only when it is used.

**Why.** The choice has to be known before any setting is read, so it can't be a setting itself. Because the Fyne store is never opened in Portable Mode, a portable copy never touches `%AppData%`, and a test checks this.

Code: `portable.go` (`chooseSettingsStore`, `fileStore`) · Commit: `4932439`

---

## User interface

### DD-41: The log view's cap

**Decision.** The log view holds at most 5,000 lines, even when the buffer is set to "Unlimited", which applies only to the log file. New lines are added every 100 ms, in one `fyne.Do`. The view still uses a plain VBox, not a virtualized list.

**Why.** Each line used to make its own `fyne.Do` and lay out the whole view again. With verbose output that is hundreds of layouts per second, the most likely cause of the reported freezes. With batching, one flush costs one layout, so the VBox is fast enough and a virtualized list wasn't needed.

Code: `log_view.go` (`maxScreenLogLines`) · Commit: `5642bbc`

### DD-42: Dropping links from a browser

**Decision.** Dropping a `.txt` list or an internet shortcut (`.url`, `.desktop`) onto the window adds its URLs. Dropping a link straight from a browser is not supported on Windows; the guide says to drag the link to the desktop first.

**Why.** From the GLFW source that Fyne 2.7.3 builds: on Windows it only calls `DragAcceptFiles` and handles `WM_DROPFILES`, which carries file lists (CF_HDROP). Browsers offer a dragged link as text plus a virtual `.url` file, not as CF_HDROP, so nothing reaches the app. On X11, GLFW accepts `text/uri-list` and only strips `file://`, so an `https://` link arrives as a "path". `handleDrop` accepts that too, but it is untested.

Code: `url_input.go` (`handleDrop`) · Commit: `1e88ce6`

### DD-43: F1 and Esc outside text fields

**Decision.** Shortcuts with Ctrl work everywhere. F1 and Esc work only when no text field has the cursor, and the guide says so.

**Why.** From Fyne 2.7's GLFW driver: a main-menu item's shortcut runs before the focused widget sees the keys. A key without a modifier, though, is never a shortcut. It goes to the focused widget, and Fyne's Entry ignores it. Only when nothing has the focus does the key reach the window.

Code: `shortcuts.go` · Commit: `85865b2`

### DD-44: Rebuilding the window when the system theme changes

**Decision.** With the System theme, `followSystemTheme` rebuilds the main window when Windows switches between light and dark.

**Why.** Fyne repaints its own widgets, but the window's own colours and icons are chosen when the window is built.

Code: `ui_manager.go` (`followSystemTheme`), `theme.go` (`systemTheme`) · Commit: `85865b2`

### DD-45: Fyne dialog and scrolling quirks

**Decision.** Two behaviours of Fyne 2.7 are worked around where they occur:
- A dialog button that may lead to an error dialog hides its own dialog first, and shows the error after.
- The log view resizes its content itself before it calls `ScrollToBottom`.

**Why.**
- Hiding a dialog also removes every overlay shown after it, so an error shown before the hide disappears with the dialog.
- `ScrollToBottom` clamps the scroll offset to the content's current size, which stays out of date until the scroll container's next layout. Without the resize, newly added lines were not scrolled into view.

Code: `release_dialog.go` (`showGoVidRelease`), `log_view.go` (`renderLogLines`) · Commits: `d7d1cd1`, `5642bbc`

### DD-46: What the Queue panel lets you change

**Decision.** Move up and Move down only swap neighbouring waiting items. Retry is offered only while the session runs.

**Why.** Moving an item above the running one wouldn't change anything. Only a running session takes items from the queue. After the session, the main **Retry** button runs the URLs again.

Code: `queue_model.go` (`Move`), `queue_panel.go` · Commit: `dda1c22`

### DD-47: What the diagnostics report hides

**Decision.** The report replaces the user profile folder (with either slash, and with the doubled backslashes yt-dlp prints), the user name, matched only as a whole word, and the cookies file's path.

**Why.** yt-dlp's verbose command-line line shows the cookies path. Replacing the user name inside other words would also change links such as `DunderGG/govid`.

Code: `diagnostics.go` (`anonymizer`) · Commit: `9e3e538`

---

## Tests

### DD-48: The Fyne test driver runs `fyne.Do` inline

**Decision.** The session test harness replaces the timed redraws of the Queue panel and the Pause button with no-ops. Panel tests redraw explicitly.

**Why.** The Fyne test driver runs `fyne.Do` on the calling goroutine instead of a UI thread. A throttle's timer goroutine would therefore race with the session's own UI updates under `-race`. In the app, `fyne.Do` runs on the UI thread, so the race only exists in tests.

Code: `download_test.go`, `formats_test.go`, `throttle.go` · Commit: `dda1c22`
