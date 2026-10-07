// download_engine.go — yt-dlp execution engine.
//
// Responsibilities:
//   - DownloadEngine: typed component holding tool paths, with methods for
//     building yt-dlp arguments, executing downloads with retry logic, and
//     finalizing output filenames once a download completes (or removing
//     the partial files of one that failed or was cancelled).
//   - DownloadRequest: typed value object holding per-download inputs.
//   - DownloadArgs: typed value object holding the resolved argument list
//     and derived metadata (extension, downloadID, trim display strings).
//   - ProcessCallbacks: bridge that lets the engine report events to the UI.
package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DownloadEngine holds the resolved paths to the tools it drives.
// Construct one with NewDownloadEngine and call its methods to build
// argument lists and execute downloads.
type DownloadEngine struct {
	YtDlpPath  string // absolute path to yt-dlp binary
	FFmpegPath string // absolute path to ffmpeg binary (empty → omit flag)

	// JSRuntime is the JavaScript runtime yt-dlp solves YouTube's player
	// challenges with; the zero value passes none.
	JSRuntime JSRuntime
}

// jsRuntimeArgs returns the --js-runtimes option naming the engine's
// runtime, or nothing when it has none. Both the probe and the download
// extract the video, so both pass it.
func (engine *DownloadEngine) jsRuntimeArgs() []string {
	if engine.JSRuntime.Path == "" {
		return nil
	}
	return []string{"--js-runtimes", engine.JSRuntime.Arg()}
}

// NewDownloadEngine returns a DownloadEngine configured with the given binary paths.
func NewDownloadEngine(ytDlpPath, ffmpegPath string) *DownloadEngine {
	return &DownloadEngine{
		YtDlpPath:  ytDlpPath,
		FFmpegPath: ffmpegPath,
	}
}

// DownloadRequest holds all per-download inputs needed to build a yt-dlp command.
// All fields are plain values; no UI or Fyne types are referenced.
type DownloadRequest struct {
	URL         string
	SavePath    string
	Format      string // one of formatOptions
	Quality     string // one of qualityOptions
	TrimStart   string // HH:MM:SS or empty
	TrimEnd     string // HH:MM:SS or empty
	MaxSpeed    string // e.g. "5M" or empty
	CookiesPath string // path to cookies.txt or empty
	// FormatPick is the formats the user chose in the Format Browser, as a
	// -f value ("247+251"); "" uses Format and Quality (and PreferredCodec,
	// one of preferredCodecOptions). The container still follows Format.
	FormatPick     string
	PreferredCodec string

	// CookiesFromBrowser is a --cookies-from-browser value, e.g. "firefox" or
	// "firefox:work"; when set, it is used instead of CookiesPath.
	CookiesFromBrowser string

	// Written into the downloaded file by yt-dlp (with ffmpeg).
	EmbedMetadata  bool // title, artist, upload date, … tags
	EmbedThumbnail bool // the thumbnail as cover art (converted to JPEG)
	EmbedChapters  bool // chapter markers

	// Subtitles is one of subtitleModeOptions ("" means Off). SubtitleLangs
	// uses yt-dlp's --sub-langs syntax ("" means defaultSubtitleLangs), and
	// AutoSubtitles also takes auto-generated captions.
	Subtitles     string
	SubtitleLangs string
	AutoSubtitles bool

	// DownloadID is the token in the names of the files yt-dlp writes (see
	// newDownloadID); "" makes BuildArgs create one. A queued item keeps
	// its own, so its partial files can be continued.
	DownloadID string

	// Live records a live stream: it has no end, so Run reports its
	// progress through ProcessCallbacks.OnRecording, and a stop (see
	// errStopKeep) or a failure keeps what was recorded. LiveFromStart
	// records it from its beginning (YouTube and Twitch). WaitForVideo
	// waits for a scheduled stream to start, which ReleaseTime says when
	// (zero when unknown).
	Live          bool
	LiveFromStart bool
	WaitForVideo  bool
	ReleaseTime   time.Time

	// InfoJSON is the probe's JSON for URL (see MediaInfo), or nil. When set,
	// Run has yt-dlp load it instead of extracting the video again.
	InfoJSON []byte
	// infoJSONPath is the file Run saved InfoJSON to; BuildArgs passes it to
	// yt-dlp in place of URL.
	infoJSONPath string
}

// DownloadArgs is the resolved output of buildYtDlpArgs. It carries the
// argument slice plus metadata the caller needs to log and identify files.
type DownloadArgs struct {
	Args             []string
	Extension        string // e.g. "mp4", "mkv", "mp3"
	DownloadID       string // unique token embedded in the temp filename
	HasTrim          bool
	TrimDisplayStart string // human-readable trim start ("start" if omitted)
	TrimDisplayEnd   string // human-readable trim end ("end" if omitted)
	ThumbnailSkipped bool   // EmbedThumbnail was set, but the container cannot hold cover art
	HasSubtitles     bool   // the args fetch subtitles
	SubtitlesSkipped bool   // subtitles were asked for, but the format is audio only
}

// formatSelection returns the yt-dlp -f selector and the output extension
// for a format and quality choice, plus the height cap the quality sets
// ("" for none). Audio formats have no picture, so the quality sets no cap
// for them. The download and the probe share it, so the probe reports the
// formats the download will fetch.
func formatSelection(format, quality string) (formatFlag, extension, height string) {
	switch {
	case strings.Contains(format, formatMP3):
		return "bestaudio/best", "mp3", ""
	case strings.Contains(format, formatM4A):
		return "bestaudio[ext=m4a]/bestaudio/best", "m4a", ""
	}

	formatFlag = "bestvideo+bestaudio/best"
	extension = "mp4"

	switch quality {
	case quality1080p:
		height = "1080"
	case quality720p:
		height = "720"
	case quality480p:
		height = "480"
	case quality360p:
		height = "360"
	}

	if height != "" {
		formatFlag = fmt.Sprintf("bestvideo[height<=%s]+bestaudio/best[height<=%s]/best", height, height)
	}
	switch {
	case strings.Contains(format, formatWebM):
		extension = "webm"
		if height != "" {
			formatFlag = fmt.Sprintf(
				"bestvideo[vcodec^=vp9][height<=%s]+bestaudio[acodec=opus]/bestvideo[vcodec^=av01][height<=%s]+bestaudio[acodec=opus]/bestvideo[height<=%s]+bestaudio/best",
				height, height, height,
			)
		} else {
			formatFlag = "bestvideo[vcodec^=vp9]+bestaudio[acodec=opus]/bestvideo[vcodec^=av01]+bestaudio[acodec=opus]/bestvideo+bestaudio/best"
		}
	case strings.Contains(format, formatMKV):
		extension = "mkv"
	}
	return formatFlag, extension, height
}

// heightLabel is the output-template field that labels a file with the
// height of the video downloaded, e.g. "_720p". yt-dlp replaces "{}" with
// the height, and writes nothing when the height is unknown, rather than
// "_NAp".
const heightLabel = "%(height&_{}p|)s"

// BuildArgs derives the full yt-dlp argument list from a DownloadRequest.
// FFmpegPath comes from the engine rather than the request, since it is
// configured once at engine construction and shared across all downloads.
func (engine *DownloadEngine) BuildArgs(req DownloadRequest) DownloadArgs {
	_, extension, height := formatSelection(req.Format, req.Quality)

	// A capped download is labelled with the height yt-dlp actually picked,
	// which can be lower than the cap (or, through the selector's final
	// "/best", higher); see heightLabel.
	qualitySuffix := ""
	if height != "" {
		qualitySuffix = heightLabel
	}

	// Embed a unique token into the filename while yt-dlp is running so it never
	// conflicts with existing files mid-download. Stripped on finalization.
	// A queued item keeps its own (req.DownloadID), so a retry or a resume
	// writes the same names and yt-dlp can continue its partial files.
	downloadID := req.DownloadID
	if downloadID == "" {
		downloadID = newDownloadID()
	}

	outputTemplate := "GoVid_%(title)s" + qualitySuffix + "_" + downloadID + ".%(ext)s"
	hasTrim := req.TrimStart != "" || req.TrimEnd != ""
	if hasTrim {
		outputTemplate = "GoVid_%(title)s" + qualitySuffix + "_TRIM_" + downloadID + ".%(ext)s"
	}

	// yt-dlp writes .part files and continues them, so an interrupted
	// download resumes where it stopped. A live recording cannot be
	// resumed, and must be written under its final name so that stopping it
	// leaves a file to keep (see finishRecording).
	partFlag := "--continue"
	if req.Live {
		partFlag = "--no-part"
	}
	args := []string{
		"--newline", "--progress", "--verbose", partFlag, "--no-playlist",
		"-P", req.SavePath, "-o", outputTemplate,
	}
	args = append(args, formatArgs(req)...)

	// Use bundled ffmpeg if available.
	if engine.FFmpegPath != "" {
		if _, err := os.Stat(engine.FFmpegPath); err == nil {
			args = append(args, "--ffmpeg-location", engine.FFmpegPath)
		}
	}

	args = append(args, engine.jsRuntimeArgs()...)

	if req.MaxSpeed != "" {
		args = append(args, "--limit-rate", req.MaxSpeed)
	}

	args = append(args, cookieArgs(req)...)

	if isAudioOnlyExt(extension) {
		args = append(args, "--extract-audio", "--audio-format", extension, "--audio-quality", "0")
	} else if extension != "" {
		args = append(args, "--merge-output-format", extension)
		args = append(args, "--remux-video", extension, "--recode-video", extension)
	}

	// Trim arguments.
	trimDisplayStart, trimDisplayEnd := req.TrimStart, req.TrimEnd
	if hasTrim {
		start := req.TrimStart
		if start == "" {
			start = "0"
			trimDisplayStart = "start"
		}
		end := req.TrimEnd
		if end == "" {
			end = "inf"
			trimDisplayEnd = "end"
		}
		args = append(args, "--download-sections", fmt.Sprintf("*%s-%s", start, end))
		args = append(args, "--force-keyframes-at-cuts")
	}

	args = append(args, liveArgs(req)...)

	embedFlags, thumbnailSkipped := embedArgs(req, extension)
	args = append(args, embedFlags...)
	subtitleFlags, subtitlesSkipped := subtitleArgs(req, extension)
	args = append(args, subtitleFlags...)

	// yt-dlp downloads every URL it is given as well as a loaded info file,
	// so the URL must be left out when the info file stands in for it.
	if req.infoJSONPath != "" {
		args = append(args, "--load-info-json", req.infoJSONPath)
	} else {
		args = append(args, req.URL)
	}

	return DownloadArgs{
		Args:             args,
		Extension:        extension,
		DownloadID:       downloadID,
		HasTrim:          hasTrim,
		TrimDisplayStart: trimDisplayStart,
		TrimDisplayEnd:   trimDisplayEnd,
		ThumbnailSkipped: thumbnailSkipped,
		HasSubtitles:     len(subtitleFlags) > 0,
		SubtitlesSkipped: subtitlesSkipped,
	}
}

// waitForVideoRange is how long yt-dlp waits between checks of a scheduled
// stream, in seconds: it waits until the announced start, but at least a
// minute and at most five, and then asks the site again.
const waitForVideoRange = "60-300"

// liveArgs returns the yt-dlp flags for recording a live or scheduled
// stream. Live HLS is written as MPEG-TS, which stays readable when the
// recording is stopped by killing yt-dlp; see finishRecording.
func liveArgs(req DownloadRequest) []string {
	var args []string
	if req.Live || req.WaitForVideo {
		args = append(args, "--hls-use-mpegts")
	}
	if req.LiveFromStart {
		args = append(args, "--live-from-start")
	}
	if req.WaitForVideo {
		args = append(args, "--wait-for-video", waitForVideoRange)
	}
	return args
}

// subtitleArgs returns the yt-dlp flags that fetch subtitles as req asks:
// written next to the video as .srt, embedded in it, or both. Audio files
// cannot hold subtitles, so for them nothing is fetched and skipped is true.
//
// --write-subs is always passed, because --write-auto-subs alone would take
// only auto-generated captions. With it, yt-dlp keeps the subtitle files
// after embedding them, which "Both" wants; "Embed" undoes that with the
// no-keep-subs compatibility option. WebM can only hold WebVTT subtitles,
// so subtitles embedded in WebM are kept in that format.
func subtitleArgs(req DownloadRequest, extension string) (args []string, skipped bool) {
	if req.Subtitles == "" || req.Subtitles == subtitlesOff {
		return nil, false
	}
	if isAudioOnlyExt(extension) {
		return nil, true
	}
	langs := strings.TrimSpace(req.SubtitleLangs)
	if langs == "" {
		langs = defaultSubtitleLangs
	}
	embed := req.Subtitles == subtitlesEmbed || req.Subtitles == subtitlesBoth
	subtitleFormat := "srt"
	if embed && extension == "webm" {
		subtitleFormat = "vtt"
	}

	args = []string{"--write-subs"}
	if req.AutoSubtitles {
		args = append(args, "--write-auto-subs")
	}
	args = append(args, "--sub-langs", langs, "--convert-subs", subtitleFormat)
	switch req.Subtitles {
	case subtitlesEmbed:
		args = append(args, "--embed-subs", "--compat-options", "no-keep-subs")
	case subtitlesBoth:
		args = append(args, "--embed-subs")
	}
	return args, false
}

// embedArgs returns the yt-dlp flags that write metadata, the thumbnail, and
// chapters into the downloaded file, as req asks. The thumbnail is
// converted to JPEG because many players cannot show WebP covers in MP3 or
// MP4. A WebM file cannot hold cover art, so for WebM the thumbnail is
// skipped and thumbnailSkipped is true.
func embedArgs(req DownloadRequest, extension string) (args []string, thumbnailSkipped bool) {
	if req.EmbedMetadata {
		args = append(args, "--embed-metadata")
	}
	if req.EmbedThumbnail {
		if extension == "webm" {
			thumbnailSkipped = true
		} else {
			args = append(args, "--embed-thumbnail", "--convert-thumbnails", "jpg")
		}
	}
	if req.EmbedChapters {
		args = append(args, "--embed-chapters")
	}
	return args, thumbnailSkipped
}

// ProcessCallbacks lets the engine report events to the UI layer without
// importing Fyne. The caller wires these to its own log/status/progress methods;
// every field must be set.
type ProcessCallbacks struct {
	// OnLog is called for every message the engine wants to show in the log view.
	// col is nil for plain output lines that should use the theme foreground.
	OnLog func(line string, col color.Color)
	// OnStatus is called to update the short status label.
	OnStatus func(msg string)
	// OnProgress is called whenever a progress percentage is parsed from yt-dlp
	// output. size is the last reported downloaded-size token (e.g. "15.2MiB"),
	// or empty when the output line did not include one.
	OnProgress func(pct float64, size string)
	// OnPhase is called when yt-dlp moves on from downloading to a step it
	// runs with ffmpeg once the download is complete: phaseMerging or
	// phaseConverting.
	OnPhase func(phase string)
	// OnRecording, which may be nil, is called about once a second while a
	// live stream (DownloadRequest.Live or WaitForVideo) is recorded:
	// started is false until its first file appears (while yt-dlp waits
	// for the stream), elapsed counts from then, and size is the bytes
	// recorded so far.
	OnRecording func(started bool, elapsed time.Duration, size int64)
}

// DownloadOptions bundles the runtime options shared by Run and Execute:
// the retry policy and this URL's position within a batch.
type DownloadOptions struct {
	AutoRetry bool
	Index     int // 1-based position within a batch (1 for single downloads)
	Total     int // total number of URLs in the batch (1 for single downloads)
}

// Execute runs yt-dlp with the given args, retrying on transient errors
// when opts.AutoRetry is true (up to 3 attempts with 1 s / 5 s / 30 s backoff).
// It is free of UI or Fyne references; all event reporting goes through cb.
// Returns the scan metadata and the final process error (nil on success).
func (engine *DownloadEngine) Execute(ctx context.Context, args []string, opts DownloadOptions, cb ProcessCallbacks) (scanResult, error) {
	retryDelays := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}
	var result scanResult
	var cmdErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if !opts.AutoRetry || !result.hadTransientErr {
				break
			}
			delay := retryDelays[attempt-1]
			cb.OnLog(
				fmt.Sprintf("[SYSTEM] Transient error detected — retrying in %v (attempt %d/3)...", delay, attempt+1),
				colWarning,
			)
			select {
			case <-ctx.Done():
				return result, cmdErr
			case <-time.After(delay):
			}
		}

		cmd := newToolCommand(ctx, engine.YtDlpPath, args...)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			cb.OnStatus(fmt.Sprintf("Failed to create stdout pipe: %v", err))
			return result, err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			cb.OnStatus(fmt.Sprintf("Failed to create stderr pipe: %v", err))
			return result, err
		}
		if err := cmd.Start(); err != nil {
			cb.OnStatus(fmt.Sprintf("Failed to launch yt-dlp: %v", err))
			return result, err
		}
		toolDone := trackTool(toolYtDlp)

		if opts.Total > 1 {
			cb.OnStatus(fmt.Sprintf("Status: Downloading (%d of %d)...", opts.Index, opts.Total))
		} else {
			cb.OnStatus("Status: Downloading...")
		}

		result = engine.watchOutput(stdout, stderr, cb)
		cmdErr = cmd.Wait()
		toolDone()
		if cmdErr == nil {
			break
		}
	}

	return result, cmdErr
}

// DownloadResult is the outcome of a single Run call: everything the caller
// needs to report status and record history, with no intermediate UI state.
type DownloadResult struct {
	FinalPaths    []string // the downloaded media files
	SubtitlePaths []string // subtitle files saved next to them
	Extension     string   // e.g. "mp4", "mkv", "mp3"
	Scan          scanResult
	Err           error
	// Stopped is set when yt-dlp was stopped (see errStopKeep), or a live
	// recording ended with an error, and what it wrote was kept and
	// finalized anyway. Err is then nil.
	Stopped bool
	// Paused is set when the download was paused (see errPaused): its
	// partial files are kept for a later run to continue. Err is then nil.
	Paused bool
}

// errStopKeep is the cause a download's context is cancelled with to stop
// it but keep what it wrote, as "Stop recording" does for a live stream:
// Run then finalizes the files instead of removing them (see
// context.WithCancelCause).
var errStopKeep = errors.New("stopped; keeping what was downloaded")

// errPaused is the cause a download's context is cancelled with to pause
// it: Run neither removes nor finalizes its partial files, so a later run
// with the same DownloadID continues them (DownloadResult.Paused).
var errPaused = errors.New("paused")

// lastDownloadID is the number in the newest download ID; see
// newDownloadID.
var lastDownloadID atomic.Int64

// newDownloadID returns a token for the names of a download's files,
// "GOVID" and a number: the time in nanoseconds, made larger than every
// earlier ID, so items queued in the same instant still differ.
func newDownloadID() string {
	for {
		previous := lastDownloadID.Load()
		next := max(time.Now().UnixNano(), previous+1)
		if lastDownloadID.CompareAndSwap(previous, next) {
			return fmt.Sprintf("GOVID%d", next)
		}
	}
}

// isPartialFile reports whether path is one of the files yt-dlp keeps while
// a download is unfinished: a .part file, a fragment (.part-Frag3), its
// fragment state (.ytdl), or a merge in progress (.temp.mp4). They are
// left for yt-dlp to continue, and never taken for a finished download.
func isPartialFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(name, ".part") || strings.Contains(name, ".part-frag") ||
		strings.HasSuffix(name, ".ytdl") || strings.HasSuffix(name, ".temp") || strings.Contains(name, ".temp.")
}

// Run composes BuildArgs, Execute, and FinalizeFiles into the full lifecycle
// of a single URL: build the yt-dlp command, run it with retry handling, and
// (on success) rename the output files to their final, conflict-free names.
// It reads no UI state — all inputs come from req and opts — and reports
// every event through cb.
//
// When req.InfoJSON is set, yt-dlp loads it instead of extracting the video
// again. Its format URLs may have expired, or be tied to another IP address,
// so a run that fails with HTTP 403 or 410 is repeated once from the URL.
func (engine *DownloadEngine) Run(ctx context.Context, req DownloadRequest, opts DownloadOptions, cb ProcessCallbacks) DownloadResult {
	infoPath, removeInfo := saveInfoJSON(req.InfoJSON, cb.OnLog)
	defer removeInfo()
	req.infoJSONPath = infoPath

	built := engine.BuildArgs(req)
	if built.HasTrim {
		cb.OnLog(
			fmt.Sprintf("[SYSTEM] Trimming: %s → %s", built.TrimDisplayStart, built.TrimDisplayEnd),
			colSystem,
		)
	}
	if built.ThumbnailSkipped {
		cb.OnLog("[SYSTEM] Not embedding the thumbnail: WebM files cannot hold cover art.", colSystem)
	}
	if built.SubtitlesSkipped {
		cb.OnLog("[SYSTEM] Not downloading subtitles: audio files cannot hold them.", colSystem)
	}
	if built.HasSubtitles && built.HasTrim {
		cb.OnLog("[SYSTEM] Subtitles are not trimmed: they cover the whole video.", colSystem)
	}

	result := engine.runArgs(ctx, req, built, opts, cb)
	if req.infoJSONPath != "" && result.Err != nil && ctx.Err() == nil && result.Scan.hadExpiredLinkErr {
		cb.OnLog("[SYSTEM] The video's download links were refused (expired?); asking the site for new ones.", colWarning)
		req.infoJSONPath = ""
		result = engine.runArgs(ctx, req, engine.BuildArgs(req), opts, cb)
	}
	// A subtitle that cannot be fetched fails the whole download, so try
	// once more without subtitles rather than lose the video.
	if built.HasSubtitles && result.Err != nil && ctx.Err() == nil && result.Scan.hadSubtitleErr {
		cb.OnLog("[SYSTEM] The subtitles could not be downloaded; downloading the video without them.", colWarning)
		req.Subtitles = subtitlesOff
		result = engine.runArgs(ctx, req, engine.BuildArgs(req), opts, cb)
	}
	return result
}

// runArgs runs yt-dlp with built's arguments, then finalizes the files it
// wrote, or removes them if it failed or was cancelled.
//
// A run stopped with errStopKeep, and a live recording that ends with an
// error after recording something, keep their files: they are finalized as
// for a finished download, and a live recording is then made playable in
// the chosen container (finishRecording).
func (engine *DownloadEngine) runArgs(ctx context.Context, req DownloadRequest, built DownloadArgs, opts DownloadOptions, cb ProcessCallbacks) DownloadResult {
	stopMonitor := engine.monitorRecording(req, built.DownloadID, cb.OnRecording)
	scan, cmdErr := engine.Execute(ctx, built.Args, opts, cb)
	stopMonitor()

	result := DownloadResult{Extension: built.Extension, Scan: scan, Err: cmdErr}
	if cmdErr != nil && errors.Is(context.Cause(ctx), errPaused) {
		cb.OnLog("[SYSTEM] Paused; the partial download is kept and continues when resumed.", colSystem)
		result.Err, result.Paused = nil, true
		return result
	}
	stopped := errors.Is(context.Cause(ctx), errStopKeep)
	keep := cmdErr != nil && (stopped || req.Live) && engine.hasFiles(req.SavePath, built.DownloadID)
	if cmdErr != nil && !keep {
		// Execute returns only once the process tree is dead, so nothing is
		// still writing to these files.
		engine.RemovePartialFiles(req.SavePath, built.DownloadID, cb.OnLog)
		return result
	}

	if keep {
		if stopped {
			cb.OnLog("[SYSTEM] Stopped; keeping what was recorded.", colSystem)
		} else {
			cb.OnLog(fmt.Sprintf("[SYSTEM] The recording ended with an error (%v); keeping what was recorded.", cmdErr), colWarning)
		}
		result.Err, result.Stopped = nil, true
	}
	finalPaths, subtitlePaths := splitSubtitleFiles(engine.FinalizeFiles(req.SavePath, built.DownloadID, cb.OnLog))
	// What FinalizeFiles left are partial files yt-dlp no longer needs,
	// such as a format an earlier, interrupted run had started.
	engine.RemovePartialFiles(req.SavePath, built.DownloadID, cb.OnLog)
	if keep && req.Live {
		finalPaths = engine.finishRecording(finalPaths, built.Extension, cb.OnLog)
	}
	for _, path := range subtitlePaths {
		cb.OnLog(fmt.Sprintf("[SYSTEM] Saved subtitles: %s", filepath.Base(path)), colSystem)
	}
	result.FinalPaths, result.SubtitlePaths = finalPaths, subtitlePaths
	return result
}

// hasFiles reports whether yt-dlp wrote any file under downloadID.
func (engine *DownloadEngine) hasFiles(savePath, downloadID string) bool {
	matches, err := filepath.Glob(filepath.Join(savePath, "*"+downloadID+"*"))
	return err == nil && len(matches) > 0
}

// subtitleExts are the extensions of the subtitle files yt-dlp writes next
// to a video.
var subtitleExts = []string{".srt", ".vtt", ".ass", ".ssa", ".lrc", ".ttml", ".srv1", ".srv2", ".srv3", ".json3"}

// splitSubtitleFiles separates subtitle files (e.g. "Video.en.srt") from the
// media files among paths, so post-processing and history see only media.
func splitSubtitleFiles(paths []string) (media, subtitles []string) {
	for _, path := range paths {
		if slices.Contains(subtitleExts, strings.ToLower(filepath.Ext(path))) {
			subtitles = append(subtitles, path)
		} else {
			media = append(media, path)
		}
	}
	return media, subtitles
}

// saveInfoJSON writes a probe's JSON to a temporary file for yt-dlp's
// --load-info-json, and returns its path and a function that removes it.
// The file goes to the temp folder, not the save folder, where FinalizeFiles
// would take it for a download. With no JSON, or when it cannot be written
// (which is logged), the path is "" and yt-dlp extracts the video itself.
func saveInfoJSON(infoJSON []byte, onLog func(line string, col color.Color)) (path string, remove func()) {
	if len(infoJSON) == 0 {
		return "", func() {}
	}
	file, err := os.CreateTemp("", "govid-*.info.json")
	if err != nil {
		onLog(fmt.Sprintf("[SYSTEM] Could not save the video's info (%v); yt-dlp will look it up again.", err), colWarning)
		return "", func() {}
	}
	remove = func() { os.Remove(file.Name()) }
	_, writeErr := file.Write(infoJSON)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		remove()
		onLog(fmt.Sprintf("[SYSTEM] Could not save the video's info (%v); yt-dlp will look it up again.", err), colWarning)
		return "", func() {}
	}
	return file.Name(), remove
}

// FinalizeFiles finds all finished files written by yt-dlp under the given
// downloadID token (not its partial files, see isPartialFile), strips the
// token from their names, and renames them to their final
// conflict-free paths using uniquePath. It returns the list of final paths so
// callers can apply further post-processing.
func (engine *DownloadEngine) FinalizeFiles(savePath, downloadID string, onLog func(line string, col color.Color)) []string {
	pattern := filepath.Join(savePath, "*"+downloadID+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	// Picking a free name and taking it must not interleave with another
	// download finishing at the same time (Simultaneous Downloads).
	renameMu.Lock()
	defer renameMu.Unlock()
	var finalPaths []string
	for _, tmpPath := range matches {
		if isPartialFile(tmpPath) {
			continue
		}
		cleanBase := strings.Replace(filepath.Base(tmpPath), "_"+downloadID, "", 1)
		cleanPath := filepath.Join(savePath, cleanBase)
		finalPath := uniquePath(cleanPath)
		if finalPath != cleanPath {
			onLog(
				fmt.Sprintf("[SYSTEM] File already exists — saving as: %s", filepath.Base(finalPath)),
				colSystem,
			)
		}
		if err := os.Rename(tmpPath, finalPath); err != nil {
			onLog(
				fmt.Sprintf("[SYSTEM] Failed to rename file: %v", err),
				colErrorSoft,
			)
		}
		finalPaths = append(finalPaths, finalPath)
	}
	return finalPaths
}

// renameMu is held while a finished file is given its final name, from
// uniquePath to the rename, so two downloads finishing at once with the
// same title cannot both pick the same free name.
var renameMu sync.Mutex

// partialRemoveAttempts and partialRemoveRetryDelay bound how long
// RemovePartialFiles keeps retrying a file that is still locked. On Windows a
// killed process can keep its files locked for a moment after it exits.
const (
	partialRemoveAttempts   = 10
	partialRemoveRetryDelay = 200 * time.Millisecond
)

// RemovePartialFiles deletes every file yt-dlp wrote under the downloadID
// token, for a download that failed or was cancelled: its .part and
// fragment files, and any media it finished before it stopped. Each
// removal, and each file that could not be removed, is logged.
func (engine *DownloadEngine) RemovePartialFiles(savePath, downloadID string, onLog func(line string, col color.Color)) {
	matches, err := filepath.Glob(filepath.Join(savePath, "*"+downloadID+"*"))
	if err != nil {
		return
	}
	for _, path := range matches {
		if err := removeWithRetry(path); err != nil {
			onLog(fmt.Sprintf("[SYSTEM] Could not remove partial file: %v", err), colWarning)
			continue
		}
		onLog(fmt.Sprintf("[SYSTEM] Removed partial file: %s", filepath.Base(path)), colSystem)
	}
}

// removeWithRetry removes path, retrying for a short while if it fails.
func removeWithRetry(path string) error {
	var err error
	for attempt := 0; attempt < partialRemoveAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(partialRemoveRetryDelay)
		}
		err = os.Remove(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	return err
}

// uniquePath returns path unchanged when no file exists at that location.
// If the path is already taken, it appends an incrementing numeric suffix
// to the base (e.g. "Video.mp4" → "Video 1.mp4" → "Video 2.mp4") until it
// finds a name that does not conflict with an existing file.
func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)

	// Loop until we find a filename that doesn't exist.
	// Theoretically this could run indefinitely if there are always conflicting files,
	// but in practice it's unlikely anyone will have dozens of duplicates in the same folder.
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s %d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
