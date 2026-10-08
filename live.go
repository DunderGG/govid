// live.go — Recording live streams and scheduled streams (premieres).
//
// Responsibilities:
//   - Live status: what the probe's live_status says about a video
//     (MediaInfo.IsLive, IsUpcoming, IsPostLive).
//   - DownloadEngine.monitorRecording: while a live stream is recorded,
//     reports the time recorded and the bytes written so far through
//     ProcessCallbacks.OnRecording.
//   - DownloadEngine.finishRecording: makes the files a stopped recording
//     left into one playable file in the chosen container. yt-dlp writes
//     live HLS as MPEG-TS, which stays readable when it is killed but does
//     not match the file's extension; yt-dlp fixes that itself only when the
//     stream ends normally.
//   - DownloaderApp.prepareLive: asks how to record a live or scheduled
//     stream (askLive), and DownloaderApp.recordingContext /
//     recordingCallback: the "Stop recording" context, the recording view
//     (indeterminate progress, "Recording 00:12:34 · 410 MB"), and the free
//     space watch that stops a recording before the drive is full.
//
// The prompt itself is in live_dialog.go.
package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
)

// The live_status values yt-dlp reports.
const (
	liveStatusLive     = "is_live"
	liveStatusUpcoming = "is_upcoming"
	liveStatusPostLive = "post_live" // ended, but the site is still processing it
)

// IsLive reports whether the video is a stream that is live now.
func (info MediaInfo) IsLive() bool {
	return info.LiveStatus == liveStatusLive
}

// IsUpcoming reports whether the video is a scheduled stream or premiere
// that has not started yet.
func (info MediaInfo) IsUpcoming() bool {
	return info.LiveStatus == liveStatusUpcoming
}

// IsPostLive reports whether the video is a stream that has just ended and
// that the site is still processing, so only part of it may be available.
func (info MediaInfo) IsPostLive() bool {
	return info.LiveStatus == liveStatusPostLive
}

// releaseTime returns when a scheduled stream starts, or the zero time
// when the site does not say.
func (info MediaInfo) releaseTime() time.Time {
	if info.ReleaseTimestamp <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(info.ReleaseTimestamp), 0)
}

// canRecordFromStart reports whether yt-dlp can record the stream from its
// beginning (--live-from-start), which it supports for YouTube and Twitch.
func (info MediaInfo) canRecordFromStart() bool {
	return info.ExtractorKey == "Youtube" || strings.HasPrefix(info.ExtractorKey, "Twitch")
}

// ── Engine ───────────────────────────────────────────────────────────────────

// recordingTick is how often monitorRecording reports.
const recordingTick = time.Second

// monitorRecording reports, through onRecording, how long a recording has
// run and how much it has written, until the returned function is called.
// It does nothing (and returns a no-op) unless req records a live or
// scheduled stream and onRecording is set.
func (engine *DownloadEngine) monitorRecording(req DownloadRequest, downloadID string, onRecording func(started bool, elapsed time.Duration, size int64)) (stop func()) {
	if onRecording == nil || !(req.Live || req.WaitForVideo) {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		markLoop("recording monitor", "started")
		defer markLoop("recording monitor", "stopped")
		defer close(finished)
		ticker := time.NewTicker(recordingTick)
		defer ticker.Stop()
		var startedAt time.Time
		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				size, found := downloadedBytes(req.SavePath, downloadID)
				if found && startedAt.IsZero() {
					startedAt = now
				}
				if startedAt.IsZero() {
					onRecording(false, 0, 0)
				} else {
					onRecording(true, now.Sub(startedAt), size)
				}
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

// downloadedBytes adds up the size of the files yt-dlp has written under
// downloadID, and reports whether there are any.
func downloadedBytes(savePath, downloadID string) (int64, bool) {
	matches, err := filesWithID(savePath, downloadID)
	if err != nil || len(matches) == 0 {
		return 0, false
	}
	var total int64
	for _, path := range matches {
		if info, err := os.Stat(path); err == nil {
			total += info.Size()
		}
	}
	return total, true
}

// remuxTimeout bounds how long finishing a stopped recording may take.
const remuxTimeout = 30 * time.Minute

// formatIDSuffix matches the ".f<format id>" yt-dlp adds to the files of a
// download it merges, e.g. "Stream.f299.mp4".
var formatIDSuffix = regexp.MustCompile(`\.f[0-9A-Za-z_-]+$`)

// recordingTarget returns the name a stopped recording's files become, in
// container ext: the first file's name without a format ID suffix.
func recordingTarget(path, ext string) string {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	return formatIDSuffix.ReplaceAllString(base, "") + "." + ext
}

// recordingContainers returns the containers to try for a stopped
// recording, in order: the chosen one, then (for video) MKV, which holds
// any codec.
func recordingContainers(ext string) []string {
	if ext == "" {
		ext = "mkv"
	}
	if isAudioOnlyExt(ext) || ext == "mkv" {
		return []string{ext}
	}
	return []string{ext, "mkv"}
}

// recordingRemuxArgs returns the ffmpeg arguments that put the video and
// audio of inputs into one file, out, without re-encoding, except into
// MP3, which needs it. Only video and audio are kept: HLS streams also
// carry ID3 data streams that MP4 cannot hold.
func recordingRemuxArgs(inputs []string, out, container string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	for _, in := range inputs {
		args = append(args, "-i", in)
	}
	for i := range inputs {
		if !isAudioOnlyExt(container) {
			args = append(args, "-map", strconv.Itoa(i)+":v?")
		}
		args = append(args, "-map", strconv.Itoa(i)+":a?")
	}
	switch container {
	case "mp3":
		args = append(args, "-c:a", "libmp3lame", "-q:a", "0")
	default:
		args = append(args, "-c", "copy")
	}
	return append(args, out)
}

// finishRecording makes the files a stopped live recording left (one
// MPEG-TS file, or a video and an audio file when recorded from the start)
// into one file in the chosen container ext, and returns its path. A
// container that cannot hold the stream's codecs (WebM for H.264) falls
// back to MKV; when ffmpeg cannot write either, a single file is kept as
// MPEG-TS (.ts). When ffmpeg cannot be run at all, the files are kept as
// they are. Every outcome is logged.
func (engine *DownloadEngine) finishRecording(paths []string, ext string, onLog func(line string, col color.Color)) []string {
	if len(paths) == 0 {
		return paths
	}
	ctx, cancel := context.WithTimeout(context.Background(), remuxTimeout)
	defer cancel()

	for _, container := range recordingContainers(ext) {
		target := recordingTarget(paths[0], container)
		tmp := strings.TrimSuffix(target, "."+container) + ".remuxing." + container
		cmd := newToolCommand(ctx, engine.ffmpeg(), recordingRemuxArgs(paths, tmp, container)...)
		toolDone := trackTool(toolFFmpeg)
		output, err := cmd.CombinedOutput()
		toolDone()
		if err != nil {
			os.Remove(tmp)
			if _, ran := errors.AsType[*exec.ExitError](err); !ran {
				onLog(fmt.Sprintf("[SYSTEM] Could not run ffmpeg to finish the recording (%v); it is kept as it was.", err), colWarning)
				return paths
			}
			onLog(fmt.Sprintf("[SYSTEM] The recording could not be put in %s: %s", strings.ToUpper(container), lastLine(string(output))), colWarning)
			continue
		}
		for _, path := range paths {
			if err := removeWithRetry(path); err != nil {
				onLog(fmt.Sprintf("[SYSTEM] Could not remove %s: %v", filepath.Base(path), err), colWarning)
			}
		}
		renameMu.Lock()
		final := uniquePath(target)
		err = engine.renameFile(tmp, final)
		renameMu.Unlock()
		if err != nil {
			onLog(fmt.Sprintf("[SYSTEM] Failed to rename the recording: %v", err), colErrorSoft)
			return []string{tmp}
		}
		onLog(fmt.Sprintf("[SYSTEM] Saved the recording as %s.", filepath.Base(final)), colSystem)
		return []string{final}
	}

	if len(paths) > 1 {
		onLog("[SYSTEM] The recording's video and audio are kept as separate files.", colWarning)
		return paths
	}
	renameMu.Lock()
	ts := uniquePath(strings.TrimSuffix(paths[0], filepath.Ext(paths[0])) + ".ts")
	err := engine.renameFile(paths[0], ts)
	renameMu.Unlock()
	if err != nil {
		return paths
	}
	onLog(fmt.Sprintf("[SYSTEM] Kept the recording as MPEG-TS: %s", filepath.Base(ts)), colSystem)
	return []string{ts}
}

// ffmpeg returns the ffmpeg to run: the configured one, or the one on PATH.
func (engine *DownloadEngine) ffmpeg() string {
	if engine.FFmpegPath != "" {
		return engine.FFmpegPath
	}
	return "ffmpeg"
}

// ── App ──────────────────────────────────────────────────────────────────────

// liveMinFreeBytes is the free space below which a recording is stopped,
// keeping what it has, rather than fill the drive.
const liveMinFreeBytes = 1 << 30

// liveSpaceCheckInterval is how often free space is checked while
// recording; a variable so tests can shorten it.
var liveSpaceCheckInterval = 30 * time.Second

// prepareLive handles a live or scheduled stream before it downloads: it
// asks how to record it and sets req to match, and reports whether to
// download it at all. A stream that has just ended (post_live) is only
// warned about.
func (app *DownloaderApp) prepareLive(ctx context.Context, item queueItem, req *DownloadRequest) bool {
	info := item.info
	if info == nil {
		return true
	}
	if info.IsPostLive() {
		app.appendOutput(fmt.Sprintf("[SYSTEM] %q has just ended and the site is still processing it; only part of it may be available until that is done.", item.displayName()), colWarning)
		return true
	}
	if !info.IsLive() && !info.IsUpcoming() {
		return true
	}

	prompt := livePrompt{title: item.displayName(), upcoming: info.IsUpcoming(), fromStart: info.canRecordFromStart()}
	if release := info.releaseTime(); prompt.upcoming && !release.IsZero() {
		prompt.startsIn = time.Until(release)
	}
	decision := app.askLive(ctx, prompt)
	switch decision {
	case liveRecordNow, liveRecordFromStart:
		req.Live = true
		req.LiveFromStart = decision == liveRecordFromStart
		from := ""
		if req.LiveFromStart {
			from = " from its start"
		}
		app.appendOutput(fmt.Sprintf("[SYSTEM] Recording the live stream %q%s. Press Stop recording to end it; what was recorded is kept.",
			item.displayName(), from), colInfo)
	case liveWait:
		req.Live, req.WaitForVideo = true, true
		req.ReleaseTime = info.releaseTime()
		app.appendOutput(fmt.Sprintf("[SYSTEM] Waiting for %q to start, then recording it.", item.displayName()), colInfo)
	default:
		app.appendOutput(fmt.Sprintf("[SYSTEM] Skipped the live stream %q.", item.displayName()), colInfo)
		return false
	}
	app.appendOutput("[SYSTEM] A live stream has no known size, so the disk space check is skipped; GoVid checks free space every 30 s while recording instead.", colSystem)
	return true
}

// liveStatusText is the status label while a stream is recorded or waited
// for: "Recording 00:12:34 · 410.0 MiB", "Stream starts in 01:59:58;
// waiting to record…", or, past its start (or with none announced),
// "Waiting for the stream to start…".
func liveStatusText(started bool, elapsed time.Duration, size int64, release, now time.Time) string {
	switch {
	case started:
		return fmt.Sprintf("Status: Recording %s · %s", clock(elapsed), formatBytes(size))
	case !release.IsZero() && release.After(now):
		return fmt.Sprintf("Status: Stream starts in %s; waiting to record…", clock(release.Sub(now)))
	default:
		return "Status: Waiting for the stream to start…"
	}
}

// clock formats d as HH:MM:SS.
func clock(d time.Duration) string {
	seconds := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}

// recordingCallback returns the ProcessCallbacks.OnRecording handler for a
// recording of req: it shows the recording's time and size (or the
// countdown to a scheduled stream) in the status label, keeps the session
// stats' size current, and every liveSpaceCheckInterval checks the free
// space, stopping the recording with stop when less than liveMinFreeBytes
// is left. Alongside other downloads (run.parallel) it leaves the status label
// to the whole queue.
func (app *DownloaderApp) recordingCallback(req DownloadRequest, stop func(), run itemRun) func(started bool, elapsed time.Duration, size int64) {
	var lastCheck time.Time
	stopped := false
	return func(started bool, elapsed time.Duration, size int64) {
		now := time.Now()
		if !run.parallel {
			app.updateStatus(liveStatusText(started, elapsed, size, req.ReleaseTime, now))
		}
		if !started || stopped {
			return
		}
		run.stats.recordSize(strings.ReplaceAll(formatBytes(size), " ", ""))
		if now.Sub(lastCheck) < liveSpaceCheckInterval {
			return
		}
		lastCheck = now
		free, err := app.freeBytes(existingDir(req.SavePath))
		if err != nil || free >= liveMinFreeBytes {
			return
		}
		stopped = true
		message := fmt.Sprintf("Only %s is free on the save folder's drive, so the recording was stopped; what was recorded is kept.", formatBytes(int64(free)))
		app.appendOutput("[WARNING] "+message, colWarning)
		app.uiManager.showNotice(notice{id: lowSpaceNoticeID, text: message})
		stop()
	}
}

// lowSpaceNoticeID identifies the notice that a recording was stopped for
// lack of disk space.
const lowSpaceNoticeID = "recording-low-space"

// setRecordingView switches the status card between the recording view
// (an indeterminate progress bar, and "Stop recording" on the Cancel
// button) and the normal one. It is safe to call from any goroutine.
func (app *DownloaderApp) setRecordingView(recording bool) {
	app.recording.Store(recording)
	fyne.Do(func() {
		controls := app.ui.download
		if recording {
			controls.progressBox.Hide()
			controls.progressLive.Show()
			controls.progressLive.Start()
			controls.cancelBtn.SetText("Stop recording")
			return
		}
		controls.progressLive.Stop()
		controls.progressLive.Hide()
		controls.progressBox.Show()
		controls.cancelBtn.SetText("Cancel")
	})
}
