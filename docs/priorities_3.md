# GoVid — Third Round of Priorities

The next ten items to work on, in order. They follow [priorities.md](priorities.md) and [priorities_2.md](priorities_2.md), which are both done. Each item was checked against the code as it was on 2026-10-07, after commit `7909dd6`.

How the order was chosen:

1. **Keep downloads working (1).** The bundled yt-dlp has no JavaScript runtime, so YouTube downloads currently rely on a deprecated fallback. This is not on the roadmap; we found it by running the bundled yt-dlp against a real video. The fix adds an installer that also covers yt-dlp and FFmpeg.
2. **Stop losing recordings and progress (2, 4).** Cancelling a live recording deletes it, and any interrupted download starts over from zero.
3. **Get past login and bot checks (3).** Today the only way to pass cookies is a hand-exported `cookies.txt` file.
4. **Then the features users notice most (5–10).** These are parallel downloads, the Format Browser, diagnostics for bug reports, Portable Mode, theme and keyboard polish, and filename templates.

Some items touch the same code, so the order also groups them:
- #1 and #3 both add yt-dlp options to `Probe` and `BuildArgs`.
- #2 and #4 both change what happens to partial files when a download is stopped. #2's "keep the file" path should be written so #4 can reuse it.
- #4 and #5 both rework `runQueue` and how download IDs are created.
- #7 does not depend on anything else and is small. It can be done earlier if bug reports start coming in after #1 or #3.

Source references point to the matching section of [roadmap.md](roadmap.md). Items #1, #2, #3, and #5 were not on the roadmap before this round and have been added to it. Tick items off here and in the roadmap as they land.

---

## 1. ✅ Give yt-dlp a JavaScript runtime, and let GoVid install its own tools

**Roadmap:** High Priority → YouTube JavaScript Runtime (added this round). Also Low Priority → FFmpeg On-Demand, which this item covers.

**Status: Done in code** (new files [tool_installer.go](../tool_installer.go), [components.go](../components.go), [components_window.go](../components_window.go), and [http_fetch.go](../http_fetch.go)); the hand check through the GUI is still to do (see the roadmap's Testing section).
- **Runtime.** `DependencyService.JSRuntime` looks for `bin/deno` first, then `deno`, `node`, and `bun` on `PATH`, runs each with `--version`, and takes the first whose version the EJS wiki supports: Deno 2.3.0+, Node 22.0.0+, Bun 1.2.11 up to 1.3.14 (Bun is deprecated there). A skipped runtime is noted with why ("node 20.11.0 in PATH is too old…"). The search is cached and reset after Deno is installed. `BuildArgs` and `Probe` both pass `--js-runtimes name:path`. The runtime is in About, in the session configuration (written from the session goroutine, since the search may run the runtime), and in the log file at startup.
- **Installer.** `ToolInstaller` describes each tool once (`toolSpec`: files for `bin/`, how to find its `toolRelease`). yt-dlp (`yt-dlp.exe` + `SHA2-256SUMS`) and Deno (`deno-x86_64-pc-windows-msvc.zip` + `.sha256sum`) come from their latest GitHub release through `ReleaseService`. GitHub's API was rate-limited from the development machine while this was built, so when it refuses, the installer uses the `/releases/latest/download/` links, which are not limited, and reads the version from where `/releases/latest` redirects. FFmpeg reads `release-version` and then takes the ZIP and its `.sha256` from that version's `packages/` folder, so the hash always belongs to the ZIP (the unversioned links are redirects that could change between the two requests). The download, hash, and unzip code was moved out of `SelfUpdater` into `httpFetcher` and is shared. Files are written as `.new`, the old file is renamed to `.old`, the new one moved in, and the `.old` deleted; a failed rename puts every old file back, and leftovers are deleted at startup. Installs refuse while a session or another install runs, and the Download button is disabled during one.
- **Components window.** Tools → Components lists yt-dlp, FFmpeg (with ffprobe), and Deno with the installed version and source, the latest version (looked up whenever it opens), and one button. Install is offered for a missing tool or one only on `PATH`, Update when the latest is newer, Reinstall otherwise. yt-dlp's Update runs `yt-dlp -U`. On Linux the window lists the versions and says to use the package manager. The Update yt-dlp menu item stays.
- **After installing FFmpeg**, GPU detection runs again (`GPUCapabilityService.Reset`) and `ffmpeg -filters` is checked for `zscale` and `tonemap`, with a warning and a notice if either is missing.
- **Notices.** The startup check shows a notice with Install for a missing yt-dlp, FFmpeg, or runtime. A download whose output has "No supported JavaScript runtime" shows the runtime notice.
- **Checked against the real sources** (outside the GUI, through the installer itself, on 2026-10-07): it installed yt-dlp 2026.08.19, Deno v2.9.7, and FFmpeg 9.0.2 with ffprobe, each hash verified. The bundled yt-dlp then reported `[debug] JS runtimes: deno-2.9.7` for a YouTube video, without the runtime warning; with Node passed instead it reported `node-24.16.0`. FFmpeg 9.0.2 has the same hardware encoders and `-hwaccels` as 8.1, and `zscale` and `tonemap`; [gpu-acceleration.md](gpu-acceleration.md#5-current-bundled-build-inventory) records this.
- **Release ZIP** unchanged, as decided. No automatic update checks were added for FFmpeg or Deno.

Tests: `TestChecksumFormats` (the three formats, including Deno's real CRLF layout and a UTF-16 copy), `TestToolInstallerInstallsEachTool`, `TestToolInstallerRejectsAWrongHash`, `TestToolInstallerPutsTheOldToolBackWhenAMoveFails` (three failure points), `TestToolInstallerUsesDownloadLinksWhenGitHubRateLimits`, `TestToolInstallerRefusesABinFolderItCannotWrite`, `TestRemoveOldTools`, `TestJSRuntimePrefersTheDenoInBin`, `TestJSRuntimeFindsNodeOnPath`, `TestJSRuntimeSkipsVersionsYtDlpDoesNotSupport`, `TestJSRuntimeIsCachedUntilReset`, `TestBuildArgsAndProbePassTheJSRuntime`, `TestStartDownloadPassesTheRuntimeItFinds` (the fake yt-dlp records its arguments; the fake tool acts as deno, node, or bun when installed under that name), `TestWatchOutputDetectsAMissingJSRuntime`, `TestDownloadWarningShowsTheRuntimeNotice`, `TestCheckToolsShowsANoticeForEachMissingTool`, `TestInstallComponentInstallsAndLooksForTheRuntimeAgain`, `TestInstallComponentRefusesWhileBusy`, `TestComponentStatusAction`, `TestComponentsRowsOfferEachToolsAction`, `TestMissingFilters`, and `TestParseToolVersion`.

**Problem.** Since late 2025, yt-dlp needs an external JavaScript runtime to solve YouTube's player challenges. Without one, it falls back to a deprecated YouTube client. That fallback still works, but formats may be missing, and when YouTube closes it, every YouTube download in GoVid stops at once.

The runtime has to come from somewhere, and the same problem applies to GoVid's other tools. A missing or broken yt-dlp or FFmpeg is only reported in the log ("Please install it"), and the user has to find and download the right file by hand.

**Decision:** GoVid downloads the runtime on demand from a menu, instead of bundling it in the release ZIP. The same installer also covers yt-dlp and FFmpeg.

**What we found**
- **No runtime.** `yt-dlp -v` with the bundled 2026.03.17 build reports `[debug] JS runtimes: none`. The exe already includes the challenge scripts (`yt_dlp_ejs-0.8.0` under "Optional libraries"); only the runtime is missing.
- **The warning.** Every YouTube extraction logs:

  `WARNING: [youtube] No supported JavaScript runtime could be found. Only deno is enabled by default; to use another runtime add --js-runtimes RUNTIME[:PATH] to your command/config. YouTube extraction without a JS runtime has been deprecated, and some formats may be missing.`

  yt-dlp then uses only the "android vr" client. On 2026-10-07 that client still listed formats up to 2160p for the test video, so downloads work today.
- **With a runtime.** With `--js-runtimes node` (Node is installed on this machine), the warning is gone and yt-dlp fetches and solves the web player ("Downloading player 1b3be681-main").
- **Runtime support.** yt-dlp supports deno, node, quickjs, and bun, in that order of priority. Only deno is enabled by default, and yt-dlp finds it only on `PATH`. GoVid never passes `--js-runtimes`, and `package.ps1` bundles only yt-dlp and ffmpeg.
- **Missing tools.** `DependencyService.Check` ([dependency_service.go:69](../dependency_service.go#L69)) only logs a warning for a missing yt-dlp or ffmpeg. Tools → **Update yt-dlp** runs `yt-dlp -U`, so it needs a working yt-dlp to start with.
- **ffprobe is not shipped.** Without it, post-processing shows no percentage. The HDR check falls back to parsing `ffmpeg -i`, the fallback added in round 1, #5.
- **Every source publishes a checksum,** each in its own format (checked on 2026-10-07):

  | Tool | Download | Checksum file | Format |
  | --- | --- | --- | --- |
  | yt-dlp | `yt-dlp.exe` from the latest GitHub release of `yt-dlp/yt-dlp` | `SHA2-256SUMS` | `<hash>  <name>` lines, which `parseSHA256Sums` from round two already reads |
  | FFmpeg | `ffmpeg-release-essentials.zip` from gyan.dev, the same build family as the bundled 8.1 "essentials_build"; the current version is in `release-version` (9.0.2 today) | `ffmpeg-release-essentials.zip.sha256` | just the hash |
  | Deno | `deno-x86_64-pc-windows-msvc.zip` from the latest GitHub release of `denoland/deno` | `….zip.sha256sum` | PowerShell `Get-FileHash` output (`Hash : A0C3…`, upper case) |
- **FFmpeg's ZIP includes ffprobe.** gyan.dev's essentials ZIP has `ffmpeg.exe`, `ffprobe.exe`, and `ffplay.exe` in a versioned `bin/` folder. Installing FFmpeg from it would therefore give users ffprobe too.

**Proposed solution**
1. **Find a runtime.** In `DependencyService`, look for `bin/deno.exe` first, then deno, node, and bun on `PATH`, and read each one's version. Check the minimum versions listed on yt-dlp's EJS wiki page, and skip a runtime that is too old (Node especially).
2. **Pass it.** Add `--js-runtimes <name>:<path>` to both `Probe` and `BuildArgs`, because both extract. Always pass the explicit path, even for deno, since `bin/` is not on `PATH`.
3. **One installer for every tool** (`tool_installer.go`). Each tool is described once:
   - where to download it, and where its checksum file is;
   - how to read that checksum file (the three formats above);
   - which files to take from a ZIP;
   - how to read the installed version.

   Install downloads into a temporary folder, with progress in the status label, and verifies the hash. It then moves the files into `bin/` through a `.new` file and a rename, so a failed install leaves the old tool working. Reuse the download, hash, and unzip code from `SelfUpdater`, the `dirWritable` check, and `ReleaseService` for the GitHub sources. Refuse to install while a session is running, because the tools would be in use.
4. **Tools → Components…** A small window with one row per tool: yt-dlp, FFmpeg (with ffprobe), and Deno. Each row shows the installed version and where it comes from (`bin/` or `PATH`), the latest version, and one button: **Install**, **Update**, or **Reinstall**.
   - A tool found only on `PATH` can still be installed into `bin/`, which then takes precedence.
   - yt-dlp's **Update** keeps using `yt-dlp -U`, and **Reinstall** downloads a fresh `yt-dlp.exe`, which repairs a broken one.
   - The existing **Update yt-dlp** menu item stays as a shortcut.
   - On Linux, the window lists the versions and says to use the package manager, because these downloads are Windows builds.
5. **FFmpeg version check.** After installing FFmpeg, re-run GPU capability detection, and check that `zscale` and `tonemap` are still present (`ffmpeg -filters`), because HDR tone mapping needs them. If either is missing, say so. Replacing 8.1 with 9.x also changes what [gpu-acceleration.md](gpu-acceleration.md#5-current-bundled-build-inventory) records, so the inventory needs re-running.
6. **Tell the user.**
   - The startup check shows a notice with **Install** for a missing yt-dlp, FFmpeg, or runtime, instead of only a log line.
   - `logscanner` recognises "No supported JavaScript runtime" and shows the same notice for Deno.
   - The runtime and its version appear in About and in the session configuration log.
7. **Release ZIP.** It stays as it is: yt-dlp and FFmpeg bundled, with Deno and ffprobe installed on demand. Adding `ffprobe.exe` to `package.ps1` is a small, separate choice. It would make post-processing percentages work out of the box.
8. **No automatic update checks** for FFmpeg and Deno. The yt-dlp check from round one stays, because yt-dlp is the tool that breaks when sites change. The Components window shows the latest versions whenever it is opened.

**Done when**
- A YouTube download's verbose log shows `JS runtimes: deno-…` (or node) and no JS-runtime warning.
- With no runtime, the notice appears, and installing Deno from it fixes the next download.
- With `bin/ffmpeg.exe` deleted, Components → FFmpeg → **Install** restores it, adds `ffprobe.exe`, and post-processing shows a percentage again.
- Tests:
  - `BuildArgs` and `Probe` pass the runtime found by a fake discovery.
  - Each of the three checksum formats is parsed.
  - An installer download with the wrong hash is rejected and leaves the installed tool untouched.
  - A failed move puts the old tool back.

---

## 2. ✅ Live streams and premieres

**Roadmap:** Medium Priority → Live Streams (added this round; from the "Implement live stream downloads" line in `notes.txt`).

**Status: Done in code** (new files [live.go](../live.go) and [live_dialog.go](../live_dialog.go)); a hand check with a real YouTube stream is still to do (see the roadmap's Testing section).
- **The spike.** YouTube answered "Sign in to confirm you're not a bot" from the development machine (a VPN address), so the spike used a public live HLS stream (`demo.unified-streaming.com`, which yt-dlp's generic extractor reports as `is_live`), with GoVid's exact arguments and the same `taskkill /T /F` Cancel uses. After 30 s the kept file held 30.8 s of **MPEG-TS** that ffprobe reads, under the `.mp4` name: yt-dlp hands live HLS to ffmpeg with `--hls-use-mpegts`, the default for live, and only fixes the container (`FixupM3u8`) when a stream ends normally. Forcing a merged selector gave the same. YouTube live without `--live-from-start` normally lists only muxed HLS formats as well, so the selector should fall back to `best` there the same way, but that could not be checked from this machine. So a stopped recording is remuxed rather than recorded with `-f b`, and the remux also merges separate video and audio files in case a site leaves them.
- **Stop keeps the file.** A download whose context is cancelled with the cause `errStopKeep` is finalized instead of cleaned up (`DownloadResult.Stopped`, `Err` nil), so history and post-processing run as for a finished one. A live recording that fails after writing something is kept too. `finishRecording` then remuxes it with the bundled ffmpeg without re-encoding: into the chosen container, falling back to MKV (WebM cannot hold H.264), or kept as `.ts`. Video and audio left by `--live-from-start` are merged. Checked on the spike's file with the real ffmpeg: MP4, WebM → MKV, MP3, and M4A all came out playable, and a 12-second recording through `DownloadEngine.Run` with the real yt-dlp, stopped by the cause, ended as a real MP4. #4's Pause can use the same cause mechanism with its own cause.
- **The prompt.** After `checkItem`, `prepareLive` asks through `askLive`: **Record from now** / **Record from the start** (`--live-from-start`, offered for the `Youtube` and `Twitch*` extractors) / **Skip**, or for a scheduled stream "Starts in 2 h 10 min." with **Wait and record** (`--wait-for-video 60-300`) / **Skip**. A scheduled stream has no formats, so a probe failed on it; the probe now passes `--ignore-no-formats-error` and reads `live_status` and `release_timestamp`. A recording downloads from the URL, not the probe's JSON. `post_live` logs a warning.
- **Recording view.** An indeterminate bar replaces the progress bar, the status reads "Recording 00:12:34 · 410.0 MiB" (or "Stream starts in 01:59:58; waiting to record…"), and the Cancel button and the Queue panel's row read **Stop recording**; the row's status reads Recording. The engine's `monitorRecording` measures the files under the download ID once a second and reports through the new optional `ProcessCallbacks.OnRecording`.
- **Stopping never loses a recording.** A recording runs under its own context (`recordingContext`, which #4 generalised into `downloadContext`), derived with `context.WithoutCancel` and cancelled with `errStopKeep` by Stop, or, through `context.AfterFunc`, when the item is skipped or the session stops, including on quit.
- **Free space.** The up-front check is skipped with a log line saying why; every 30 s the free space is checked, and below 1 GiB the recording stops, keeping it, with a warning and a notice.

Tests: `TestRunStoppedRecordingKeepsAndFinalizesIt` and the existing `TestRunCancelRemovesPartialFiles` (a stopped live item keeps and finalizes its file; a cancelled normal item still removes it), `TestStopRecordingKeepsTheLiveStream`, `TestQuittingWhileRecordingKeepsTheRecording`, `TestRecordingStopsWhenTheDriveIsNearlyFull`, `TestScheduledStreamWaitsAndThenRecords`, `TestSkippingALiveStreamDownloadsNothing` (new fake modes "ytdlp-live", which writes until it is killed, and "ytdlp-upcoming", which fails without `--wait-for-video`), `TestRecordingContextKeepsTheRecordingWhenTheSessionStops`, `TestBuildArgsForLiveStreams`, `TestProbeIgnoresMissingFormats`, `TestMediaInfoLiveStatus`, `TestLiveStatusText`, `TestStartsInText`, and `TestRecordingRemuxPlan`.

**Problem.** GoVid treats a live stream as a normal video. A live recording has no end, so the user's only way to stop it is Cancel, and Cancel deletes everything recorded so far. A scheduled stream or premiere fails straight away.

**What we found**
- `MediaInfo` ([probe.go](../probe.go)) does not read `live_status`. yt-dlp sets it to `is_live`, `is_upcoming`, `was_live`, `post_live`, or `not_live`.
- Cancel makes `Run` call `RemovePartialFiles` ([download_engine.go:456](../download_engine.go#L456)), which deletes every `*GOVID<id>*` file. For a live recording, that is the whole recording.
- Even if the file were kept, the process-tree kill (`taskkill /T /F`) gives ffmpeg no chance to finish writing it, so an MP4 may be unplayable. yt-dlp's help says it writes live HLS as MPEG-TS by default (`--hls-use-mpegts`) to reduce corruption when interrupted. Which downloader handles GoVid's merged `bv+ba` selector on a live stream still needs a spike.
- A live stream has no total size or duration. The progress bar has nothing to show, and `checkDiskSpace` skips the item with "size unknown".
- yt-dlp has `--live-from-start` (experimental; YouTube, Twitch, and TVer only) and `--wait-for-video MIN[-MAX]` for scheduled streams. GoVid passes neither.

**Proposed solution**
1. Read `live_status` and `release_timestamp` into `MediaInfo`.
2. **Ask before recording.**
   - For a live item: **Record from now** / **Record from the start** (`--live-from-start`, offered only for YouTube and Twitch) / **Skip**.
   - For an upcoming item: "Starts in 2 h 10 min. **Wait and record** / **Skip**", which passes `--wait-for-video 60-300`. The status shows the countdown while it waits.
3. **Recording view.** The progress bar becomes indeterminate. The status shows "Recording 00:12:34 · 410 MB". The Cancel/Skip button reads **Stop recording**.
4. **Stop keeps the file.** Give `Run` a "stopped by the user, keep the output" result. That path skips `RemovePartialFiles` and runs `FinalizeFiles`, history, and post-processing as for a finished download. #4's Pause should reuse this path.
5. **Make the kept file playable.** Spike first: record a YouTube live stream for 30 s with the current selector, kill it, and check whether the file plays. If it does not, record live items with a single muxed format (`-f b`, which on YouTube live is an HLS format with both video and audio) as `.ts`. After Stop, remux it to the chosen container with the bundled ffmpeg.
6. **Watch free space while recording.** Skip the up-front disk check and log why. Instead, check free space every 30 s while recording, and stop (keeping the file) with a warning when it drops below 1 GB.
7. For `post_live` (the stream ended but YouTube hasn't processed it yet), warn that only part of it may be available until processing finishes.

**Done when**
- Recording a live stream and pressing **Stop recording** leaves a playable file, with a history entry.
- An upcoming stream waits, then records when it starts.
- A fake yt-dlp test: a "live" item stopped by the user keeps its file and runs `FinalizeFiles`, while a cancelled normal item still removes its partial files.

---

## 3. ✅ Cookies from the browser

**Roadmap:** Medium Priority → Authentication Support (new item added this round).

**Status: Done in code** (new file [cookies.go](../cookies.go)); the hand check with a signed-in Firefox is still to do (see the roadmap's Testing section).
- **The setting.** Preferences → **Cookies** is None / From file (the existing file field) / From browser (a browser dropdown with Firefox first, and an optional profile name); the controls of the chosen source show below it. `cookieSource`, `cookieBrowser`, and `cookieProfile` are in `AppConfig` (validated against `cookieSourceOptions` and `cookieBrowserOptions`), `govid.json`, and a new "Cookies" presets group, added last so the existing groups keep their order. Settings saved before have no source; `resolveDefaults` makes it From file when a cookies file was set, so nobody's cookies stop working. `cookieArgs` gives both the probe and the download `--cookies-from-browser <browser>[:<profile>]` or `--cookies <file>`.
- **The recorded lines.** Run with the bundled yt-dlp 2026.03.17 on 2026-10-07, against a local URL (nothing listening) so the cookies were only read and never sent: Chrome with its database held open without sharing, as a running Chrome holds it, gave `ERROR: Could not copy Chrome cookie database. See  https://github.com/yt-dlp/yt-dlp/issues/7271  for more info`. Chrome and Edge, closed, gave `ERROR: Failed to decrypt with DPAPI. See  https://github.com/yt-dlp/yt-dlp/issues/10927  for more info` (app-bound encryption). A Firefox profile that does not exist gave `ERROR: could not find firefox cookies database in '…'`. `classifyAccessError` matches these, and `accessHint` says plainly: close the browser, use Firefox, or export a `cookies.txt`.
- **Better hints.** The bot check ("Sign in to confirm you’re not a bot", as YouTube answered this machine), age-restricted, members-only, and private errors now suggest cookies and name **Tools → Preferences → Cookies**. The advice depends on what was passed: with no cookies, choose From browser; with a browser, check that you are signed in there; with a file, export it again. The bot-check hint still suggests updating yt-dlp; other site-change errors keep the update hint.
- **Safety.** The guide says cookies are your login, that GoVid only passes them to yt-dlp, and that the log names only the source. The session configuration's "Cookies file: <path>" line is now "Cookies: Firefox" / "file set" (`cookieLabel`), never the path or the profile name; #7's diagnostics will use the same label.

Tests: `TestCookieArgsForEachSource` (download and probe), `TestClassifyAccessError` (the recorded lines and YouTube's sign-in messages), `TestAccessHintsSayWhatToDo`, `TestBrowserCookiesFailureExplainsWhatToDo` and `TestBotCheckSuggestsCookies` (new fake modes failing with the recorded lines), `TestCookieLabelNeverShowsThePath`, `TestSessionLogNamesOnlyTheCookieSource`, `TestLoadKeepsAnExistingCookiesFile`, `TestCookiesRowShowsTheChosenSource`, `TestCookiesFromBrowserAndItsLabel`, and the config round-trip tests, which now cover the three new keys.

**Problem.** Age-restricted, members-only, and private videos need the user's login, and so does getting past YouTube's "Sign in to confirm you're not a bot" check. GoVid accepts only a `cookies.txt` file. Users have to export it with a browser extension, and it goes stale.

**What we found**
- `BuildArgs` passes `--cookies <file>` when the file exists ([download_engine.go:181](../download_engine.go#L181)), and `Probe` does the same. There is no other cookie option.
- yt-dlp's `--cookies-from-browser BROWSER[:PROFILE]` supports brave, chrome, chromium, edge, firefox, opera, safari, vivaldi, and whale.
- **Windows limitation:** Chrome and Edge now encrypt cookies with app-bound encryption, which yt-dlp cannot decrypt. Chromium browsers also lock the cookie database while they are open. Firefox works.
- The extractor-error hint from the first round suggests updating yt-dlp for "Sign in to confirm", but it never mentions cookies.

**Proposed solution**
1. In Preferences, replace the cookies file field with a **Cookies** choice:
   - None
   - From file (the existing field)
   - From browser: a browser dropdown with Firefox first, plus an optional profile name

   Pass `--cookies-from-browser <browser>[:<profile>]` to both the probe and the download. Add `cookieSource`/`cookieBrowser`/`cookieProfile` to `AppConfig` (the config coverage test will insist on it) and to the presets' groups.
2. **Explain failures.** Run the bundled yt-dlp against a locked Chrome database and an app-bound-encrypted one, and record the exact error lines. Match those lines in `logscanner` and log plainly: close the browser, use Firefox, or export a `cookies.txt`.
3. **Better hints.** For "Sign in to confirm you're not a bot", "age-restricted", and "members-only" errors, suggest cookies, and say which setting to use.
4. **Safety.** The guide says that cookies are your login, and that GoVid only passes them to yt-dlp. The session log and diagnostics (#7) show "cookies: Firefox" or "cookies: file set", never the file path or any contents.

**Done when**
- With Firefox signed in to YouTube, an age-restricted video downloads.
- With Chrome selected, the failure explains what to do instead of showing only yt-dlp's raw error.
- Tests: the args for each cookie source, and the error matching on the recorded lines.

---

## 4. ✅ Resumable downloads, with pause and resume

**Roadmap:** Medium Priority → Queue Manager (the pause/resume and keep-the-queue-after-restart items).

**Status: Done** (new files [pause_resume.go](../pause_resume.go) and [queue_store.go](../queue_store.go)).
- **Partial files.** `BuildArgs` passes `--continue` instead of `--no-part --no-continue`. A live recording keeps `--no-part`: it cannot be resumed, and without it a stopped recording would be a `.part` file. `FinalizeFiles` skips `.part`, `.part-Frag*`, `.ytdl`, and `.temp` files (`isPartialFile`), and a successful run removes any it left, such as an earlier run's format. Checked with the bundled yt-dlp against a local server, through `DownloadEngine.Run`: paused after 2 s at 2,096,128 bytes, the next run logged `Resuming download at byte 2096128` and asked for `Range: bytes=2096128-`.
- **Stable IDs and settings.** `NewQueueModel` gives each item a download ID once (`newDownloadID`, strictly increasing so items queued in the same instant differ), carried in `DownloadRequest.DownloadID`. Each item also gets its own copy of the settings, read from the widgets once when the session starts (`downloadSession.request`, `queueItem.withRequest`), so retries, auto-retries, resumes, and restored items write the same names. A side effect: changing the format during a batch no longer changes the items not yet downloaded.
- **Pause.** A **Pause** button beside Cancel, and on the downloading row, cancels the download's context with `errPaused` (`downloadContext`, which #2's recording context became): `runArgs` neither removes nor finalizes the files and returns `DownloadResult.Paused`, and the item becomes **Paused**. The queue moves on; once only paused items are left it waits (`waitForResume`), the button reads **Resume**, and Cancel discards them. A paused row offers Resume, which puts it first among the waiting items, and Remove, which deletes its partial files. Skip and Cancel still delete them. A paused item keeps its probe JSON, so resuming within 30 minutes does not extract it again. Live recordings cannot be paused.
- **Keeping the queue.** `Shutdown` sets `quitting`, so the running download is paused rather than cancelled, and `finishQueue` saves the waiting and paused items to `queue.json` beside the history: URL, title, video and download IDs, format ID, status, and the settings, without cookies or the probe JSON. The next start asks "Resume N downloads?" (Resume / Discard; Discard deletes their partial files). A resumed session skips the URL check; each item is probed again, and when the probe's `format_id` differs from the saved one the log says that stream starts over. **One deviation:** the queue is written when GoVid quits, so a crash still loses it (its partial files stay in the save folder).

Tests: `TestPauseAndResumeContinuesTheDownload` (a new "ytdlp-resumable" fake writes `.part` files, needs `--continue`, and resumes from the file's size: paused at 50%, it resumed at about half, with the same output name and no second probe), `TestCancellingAPausedDownloadRemovesItsFiles`, `TestShutdownPausesTheBatchAndSavesTheQueue` (replaces the test that quitting removes the partial file), `TestRestoredQueueResumesAtTheNextStart` (with the format-change line), `TestDiscardingTheRestoredQueueRemovesItsFiles`, `TestQueueStoreRoundTrip`, `TestRunPausedKeepsPartialFilesWithoutFinalizing`, `TestDownloadContextCauseWhenTheSessionStops`, `TestQueueModelPauseAndResume`, `TestFinalizeFilesLeavesPartialFiles`, `TestIsPartialFile`, `TestBuildArgsResumesPartialFiles`, and `TestNewDownloadIDIsUniqueAndIncreasing`.

**Problem.** An interrupted download always starts again from zero, whether a network drop, Cancel, a crash, or quitting interrupted it. For a 4K video that can mean gigabytes downloaded twice. There is no way to pause.

**What we found**
- `BuildArgs` passes `--no-part --no-continue` ([download_engine.go:166](../download_engine.go#L166)). yt-dlp writes straight to the final-looking name and never resumes.
- The download ID is created fresh in every `BuildArgs` call ([download_engine.go:157](../download_engine.go#L157)). A rerun of the same item therefore writes different names and could not find the earlier partial file, even with `--continue`.
- Auto-retry after a transient error also restarts from zero.
- The queue lives only in memory, so quitting loses it.

**Proposed solution**
1. **Keep partial files.** Drop `--no-part --no-continue`, so yt-dlp writes `.part` files and resumes them. Fragmented (DASH/HLS) downloads also keep `.part-Frag*` and `.ytdl` state files, which yt-dlp reuses.
2. **Stable download ID per item.** Create the ID once when the item is queued, keep it on `queueItem`, and pass it in `DownloadRequest`. A retry, an auto-retry, or a resume then writes the same names. Keep the per-call ID only for one-off uses such as tests.
3. **Pause.** Add a Pause button on the downloading row and beside Cancel.
   - It stops the process tree but keeps the partial files, using #2's "keep the output" path without finalizing.
   - The item gets a new **Paused** status. Resume puts it back to Waiting, at the front of the queue.
   - Skip and Cancel still delete the partial files.
4. **Finalize only finished files.** `RemovePartialFiles` already globs `*<id>*`, so it catches the new leftovers. `FinalizeFiles` must now skip `.part`, `.part-Frag*`, `.ytdl`, and `.temp` files.
5. **Keep the queue after quitting.** If the queue holds waiting or paused items when the app quits, save them to `queue.json` beside the history: URL, title, video ID, download ID, status, save folder, and the request settings. Don't save the probe JSON, because it expires. On the next start, ask "Resume 7 queued downloads?". A resumed item is probed again (`probeMaxAge` already handles that).
6. **Mind format changes.** If the new probe picks a different format, yt-dlp's `.part` name (`.f137.mp4.part`) won't match, and that stream simply starts fresh. This is acceptable; log it.

**Done when**
- Pausing at about 50% and resuming finishes from about 50%. The fake tool checks that `--continue` is passed and that the output name is the same.
- Quitting with a waiting queue offers to restore it on the next start.
- Skip still leaves no files behind.

---

## 5. ✅ Parallel downloads

**Roadmap:** Medium Priority → Queue Manager (new item added this round; from "Concurrency & Goroutines" in `notes.txt`).

**Status: Done** (new file [parallel.go](../parallel.go)).
- **Setting.** Preferences → **Simultaneous Downloads** (1–3, default 1), with the bot-check warning as its hint; `simultaneousDownloads` in `govid.json`. It is read once when the session starts. With 1, the queue runs exactly as before.
- **Workers.** `runParallel` starts that many workers, `workerStagger` (3 s) apart, each taking `queue.Next()`. A worker that finds only paused items, with nothing else active, waits for a resume (#4).
- **Progress.** Each item has an `itemRun` with its own `DownloadStats`. Its progress goes to its Queue row (`QueueModel.SetProgress(id)`, replacing the "first downloading item" lookup in parallel mode). The bar shows `OverallProgress()` (finished items plus active fractions, over all), and the status reads "Downloading 3 videos…". Each line from yt-dlp gets an "[n/total]" prefix; `renderLogLines` keeps one in-place progress line per prefix, so interleaved progress does not flood the view. Each item's summary block is prefixed too, and the final status ("3 of 3 done") is set when all workers end.
- **Controls.** Single-item controls became per-item (`itemControls`): each row's Skip and Pause act on that item, the main Pause pauses every download, and Cancel stops the session. A live recording in parallel mode keeps the normal progress view, and its row's Skip stops and keeps it.
- **Shared state.** Prompts take turns (`promptMu`, which also guards "continue for all" on low space). `renameMu` covers `uniquePath` plus the rename in `FinalizeFiles` and `finishRecording`. `HistoryService` has a mutex around `AppendAll` and `Clear`. The disk check subtracts `reservedBytes`, the estimates of downloads in progress (`reserveSpace`).
- **Back off.** `scanResult.hadRateLimit` (HTTP 429 / "Too Many Requests") or a bot check makes `backOff` lower the worker limit to 1 for the rest of the session, logged once; the other workers stop taking items after their current one.

Tests: `TestThreeDownloadsRunAtOnce` (a new "ytdlp-concurrent" fake marks itself running in a folder and records the peak: 3; the three same-titled videos get three names; three history entries; the prefixes), `TestOneDownloadAtATimeByDefault`, `TestRateLimitBacksOffToOneWorker`, `TestBackOffNeedsRateLimitOrBotCheck`, `TestDiskCheckLeavesSpaceForRunningDownloads`, `TestProgressLinesStayInPlacePerItem`, `TestQueueModelOverallProgress`, and `TestSimultaneousDownloads`. `go test -race ./...` passes.

**Problem.** The queue downloads one video at a time. A playlist of many short videos is slow, because each item spends several seconds starting yt-dlp and extracting before any data moves.

**What we found**
- `runQueue` ([download.go:230](../download.go#L230)) takes one item, downloads it, and only then takes the next.
- There is one shared progress bar, one status label, and one `stats` value. The disk-space, duplicate, and quality prompts are each shown per item.
- `uniquePath` ([download_engine.go:587](../download_engine.go#L587)) checks whether a name exists and then renames. Two parallel items with the same title could both pick the same name.
- `HistoryService` has no mutex. Its append reads, changes, and rewrites the JSON file, so two parallel writes could lose an entry.

**Proposed solution**
1. Add a **Simultaneous downloads** preference: 1–3, default 1. Setting it higher than 1 makes YouTube's bot check more likely, and the hint text says so.
2. `runQueue` starts N workers, each of which calls `queue.Next()`. The per-item cancel contexts already exist.
3. **Progress.**
   - Per-item percentages are already in the queue panel.
   - The main bar shows overall progress: (done items + the sum of active items' fractions) / total.
   - The status reads "Downloading 3 videos…".
   - Each log line from yt-dlp gets an `[n/total]` prefix, so interleaved output stays readable.
4. **Serialize what is shared.**
   - Show prompts one at a time.
   - Put a mutex around `uniquePath` plus the rename in `FinalizeFiles`.
   - Add a mutex to `HistoryService`.
   - Make the disk check subtract the estimates of items already downloading.
5. **Back off.** Start workers a few seconds apart. When an item hits HTTP 429 or the bot check, drop to one worker for the rest of the session and log it.

**Done when**
- In a fake yt-dlp test with N = 3, three downloads run at the same time (the fake tool counts how many are running at once).
- Two items with the same title get different names.
- No history entries are lost.
- `go test -race` passes.

---

## 6. Format Browser

**Roadmap:** Medium Priority → Format Browser.

**Problem.** Users choose a format and quality cap, and yt-dlp picks the actual streams. Users can't see what is available, what will be picked, or choose a particular codec.

**What we found**
- Each probe already keeps the video's full JSON (`MediaInfo.raw`). Its `formats` list has `format_id`, `ext`, `width`/`height`, `fps`, `vcodec`, `acodec`, `tbr`/`abr`, `filesize`/`filesize_approx`, `dynamic_range`, and `format_note`. The probe also records which formats the current Format/Quality picks (`requested_formats`).
- Since round two, the download runs format selection again on the loaded JSON. A different `-f` therefore needs no new extraction.

**Proposed solution**
1. Add a **Formats…** button next to the URL field (for a single URL) and on each queue row. It probes the URL if needed and opens a table with these columns: resolution, fps, HDR, video codec, audio codec, bitrate, container, and size. The table can be filtered by video-only, audio-only, or combined formats. Storyboards are hidden.
2. **Preview of the final choice.** Highlight the rows the current settings pick. Also log a line before each download, for example "Will download: 1080p AV1 + Opus → MP4 (~45 MB)". This covers the roadmap's "preview of the final format choice".
3. **Pick a format.** Choosing a video row and an audio row sets a per-item override (`-f 247+251`) on the `queueItem`. `BuildArgs` uses it instead of `formatSelection`. The container still follows the Format setting, with the usual remux and recode rules.
4. **Favourites.** Pinning format IDs doesn't work, because the IDs differ from video to video. Instead, add a **Preferred video codec** preference (Any / H.264 / VP9 / AV1), mapped to yt-dlp's format sorting (`-S vcodec:…`). This covers the roadmap's "pin or favorite preferred formats" item in a way that works for every video.

**Done when**
- For a JSON fixture in `testdata/`, the table lists the same formats as `yt-dlp -F`.
- Picking 720p VP9 plus Opus makes the fake tool receive `-f 247+251`.
- The "Will download" line matches what yt-dlp then downloads.

---

## 7. Copy Diagnostics and freeze diagnostics

**Roadmap:** UI Performance & Stability → Observability for Freeze Reports, and Goroutine Lifecycle Hygiene ("lifecycle diagnostics").

**Problem.** A bug report needs versions, settings, and the log, and users have to collect them by hand. A UI freeze leaves nothing in the logs to explain it.

**What we found**
- All the pieces exist but nothing gathers them:
  - tool versions (`DependencyService.Version`, shown in About);
  - GPU detection (`formatGPUDiagnostics`);
  - every setting (`ExportConfig`, from round two);
  - the session configuration (`WriteSessionConfig`);
  - the session and error log files.
- The UI has no heartbeat. Nothing records how long `fyne.Do` calls wait, or how many goroutines and tool processes are running.

**Proposed solution**
1. **Copy diagnostics.** Add Help → **Copy diagnostics**. It builds one text report and copies it to the clipboard, with a **Save as file…** option. The report holds:
   - the GoVid version and build type;
   - the OS;
   - the yt-dlp, ffmpeg, and JS runtime (#1) versions;
   - the GPU detection summary;
   - all settings, with the user profile in paths replaced by `%USERPROFILE%` and cookies shown only as their source (#3);
   - the queue summary;
   - the goroutine count;
   - the last 200 log lines.
2. **Heartbeat.** When Debug Output is on, write one line to the session log every 10 s with:
   - the `fyne.Do` round-trip time (a frozen UI shows up as a large number);
   - the goroutine count;
   - the number of running yt-dlp and ffmpeg processes;
   - the number of queued log lines.
3. **Loop markers.** In debug mode, log start and stop markers for the status pulse, the progress smoother, the ffmpeg progress readers, and the throttles. Then do the roadmap's ticker-loop audit as a review: record each loop's owner and stop path in [architecture.md](architecture.md).

**Done when**
- The report contains every listed section, and a test checks that it contains no cookie path and no user name.
- In debug mode, a test that blocks the UI thread for 2 s sees the heartbeat report the delay.

---

## 8. Portable Mode

**Roadmap:** Technical Improvements → Portable Mode.

**Problem.** GoVid ships as a ZIP that runs from any folder, but its settings live in `%AppData%`. A copy on a USB stick doesn't take its settings along, and two copies on one machine share the same settings.

**What we found**
- Preferences go through a `fyne.Preferences` store ([preference_service.go:146](../preference_service.go#L146)). Fyne saves it under `%AppData%\fyne\com.govid.downloader` (`app.NewWithID` in [main.go:134](../main.go#L134)). Presets and the release-check cache are kept in that store too.
- Download history is already beside the exe (`download_history.json`, [history_service.go](../history_service.go)). Today GoVid's files are split between two places.
- `PreferenceService` takes the store as an interface, so a different store needs no other code changes.

**Proposed solution**
1. GoVid runs in portable mode when a `GoVid.portable` marker file exists beside the exe. A marker is needed because the choice has to be known before any settings are read.
2. Add `fileStore`, a `fyne.Preferences` implementation (about 30 small methods) backed by `settings.json` beside the exe. It writes atomically, like `WriteConfigFile`, and debounces its saves.
3. Preferences → **Portable mode** creates or removes the marker, copies the current settings into or out of `settings.json`, and asks for a restart.
4. If the exe's folder is not writable (checked with `dirWritable`), fall back to `%AppData%` and say so.
5. The guide explains the two modes and lists which files live where.

**Done when**
- With the marker, GoVid reads and writes only `settings.json` beside the exe. A test checks that the Fyne store is never touched.
- Copying the folder to another machine keeps the settings, presets, and history.

---

## 9. Follow the OS theme, and keyboard shortcuts

**Roadmap:** UI & UX → Dark / Light Mode Toggle ("Default to the OS system theme"), and Technical Improvements → UX Improvements ("hotkeys").

**Problem.** GoVid always starts dark, even on a light Windows desktop. Nothing in the app can be done from the keyboard.

**What we found**
- `themeOptions` holds only Dark and Light ([options.go:54](../options.go#L54)). `applyTheme` ([theme.go:175](../theme.go#L175)) falls back to dark for any other value, and both themes ignore the variant Fyne passes in.
- `themedIcon` and the header colours read the saved theme name directly, so a third option has to be handled there too.
- No shortcuts are registered anywhere: there is no `AddShortcut` or `SetOnTypedKey` in the code.

**Proposed solution**
1. **System theme.**
   - Add **System** to `themeOptions` and make it the default for new installs. Existing users keep their saved choice.
   - `systemTheme` passes each `Color(name, variant)` call on to the dark or light theme, depending on the variant Fyne passes in. On Windows, Fyne takes that variant from the "apps use light theme" setting, so the theme follows the OS without extra code.
   - Resolve System to the actual variant in `themedIcon` and in the header.
   - `configRules` picks up the new option from `options.go` automatically.
2. **Shortcuts** (`desktop.CustomShortcut` with `KeyModifierShortcutDefault`):

   | Keys | Action |
   | --- | --- |
   | Ctrl+Enter | Start the download |
   | Ctrl+O | Open the save folder |
   | Ctrl+L | Load URLs from a file |
   | Ctrl+Shift+V | Paste URLs (the paste button) |
   | Ctrl+H | History |
   | Ctrl+, | Preferences |
   | F1 | The guide |
   | Esc | Close About, Help, Preferences, Post-Processing, or History |

   Show the shortcuts in the menus (`MenuItem.Shortcut`) and list them in the guide.

**Done when**
- With Windows in light mode and the theme set to System, GoVid starts light, and switching Windows to dark switches GoVid (a hand check).
- Tests trigger each shortcut through the Fyne test driver.

---

## 10. Custom filename template

**Roadmap:** Low Priority → Custom Output Filename Template. It is listed here because the fixed name, starting with `GoVid_`, is something many users rename by hand after every download. It is also the step between downloading a file and filing it in a media library.

**What we found**
- The template is fixed: `"GoVid_%(title)s" + qualitySuffix + "_" + downloadID + ".%(ext)s"`, with a `_TRIM_` variant ([download_engine.go:159](../download_engine.go#L159)).
- The `GOVID<id>` token has to stay in the name until `FinalizeFiles` strips it, because the finalize, cleanup, and subtitle-split steps all find files by that token.
- `inferOriginalTitle`, the history fallback, only knows the `GoVid_` pattern. Since round two, history gets the real title from the probe, so the fallback is rarely used.

**Proposed solution**
1. Add a **Filename template** preference in yt-dlp's own syntax, with a **Reset** button. The default reproduces today's names: `GoVid_%(title)s{quality}`. `{quality}` is a GoVid placeholder for the existing height label, which appears only for capped downloads. GoVid itself still appends `_<downloadID>` (and `_TRIM`) before `.%(ext)s`. `FinalizeFiles` strips the token as it does today.
2. **Validation.**
   - Reject `/` and `\`. Subfolders such as `%(uploader)s/%(title)s` would need `FinalizeFiles` to look in subfolders, which can come later.
   - Reject an empty template.
   - Warn when the template has neither `%(title)s` nor `%(id)s`, because every file would then get the same name plus a number.
3. **Live preview.** Below the field, show the name the template produces for a sample video, filling the common fields (`title`, `id`, `uploader`, `upload_date`, `height`, `ext`) in Go. Show fields it doesn't know as-is, with a note.
4. The template goes into `govid.json` and the presets automatically, because the config coverage test requires a key for every preference.

**Done when**
- `%(uploader)s - %(title)s` produces `Rick Astley - Never Gonna Give You Up.mp4`, with no ID token left in the name.
- An invalid template is refused with a message.
- Trimmed downloads, subtitle sidecars, and duplicate names still work (the existing tests, run with a non-default template).

---

## Next after these

- **Tray integration**: `desktop.App.SetSystemTrayMenu` exists in Fyne 2.7. It would add Minimize to Tray plus Pause/Resume (once #4 is done), Open Folder, and Exit.
- **Log view**:
  - Wrap or shorten long lines. Each line is a `canvas.Text` inside a scroll container ([ui_manager.go:1293](../ui_manager.go#L1293)), so a long title scrolls sideways.
  - Add a search/filter bar.
- **Queue extras**: an ETA for each item, and the playlist's total size and time. Once items are probed one by one, the total can fill in as the queue runs.
- **Audio controls**: a bitrate selector and "Strip Audio".
- **Post-download command** with a file-path placeholder.
- **Code quality**:
  - Move the guide's long strings (`showConfigHelp` is a 117-line function) into an embedded Markdown file.
  - Turn the dialog sizes into named constants. They are literals in [ui_manager.go](../ui_manager.go) (lines 294, 416, 468, 698), [history_window.go](../history_window.go#L233), and the 460/480-wide prompts in `disk_space.go`, `duplicates.go`, `playlist_dialog.go`, `preset_ui.go`, and `release_dialog.go`.
- **UI thread-safety checklist and freeze regression tests**: #7's heartbeat makes these measurable.
- **GPU**: benchmark against the CPU encoders, and run the capability inventory again on a Linux build.
- **Linux and macOS**: run `build.sh` on Ubuntu, check `xdg-open` on the common desktops, and investigate macOS.

## Testing

The hand checks left over from rounds one and two, and the other testing we should do, are in the **Testing** section of [roadmap.md](roadmap.md#-testing).
