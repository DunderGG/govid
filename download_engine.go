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

	rename func(from, to string) error // os.Rename when nil; replaced in tests
}

// renameFile renames from to to, retrying while the file is locked (see
// renameWithRetry).
func (engine *DownloadEngine) renameFile(from, to string) error {
	rename := engine.rename
	if rename == nil {
		rename = os.Rename
	}
	return renameWithRetry(rename, from, to)
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

	// FilenameTemplate names the downloaded files, in yt-dlp's output
	// template syntax plus {quality} (see outputTemplate); "" for
	// defaultFilenameTemplate.
	FilenameTemplate string

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

	// Embed a unique token into the filename while yt-dlp is running so it never
	// conflicts with existing files mid-download. Stripped on finalization.
	// A queued item keeps its own (req.DownloadID), so a retry or a resume
	// writes the same names and yt-dlp can continue its partial files.
	downloadID := req.DownloadID
	if downloadID == "" {
		downloadID = newDownloadID()
	}

	hasTrim := req.TrimStart != "" || req.TrimEnd != ""
	args := outputArgs(req, height, downloadID, hasTrim)
	args = append(args, formatArgs(req)...)
	args = append(args, engine.ffmpegLocationArgs()...)
	args = append(args, engine.jsRuntimeArgs()...)

	if req.MaxSpeed != "" {
		args = append(args, "--limit-rate", req.MaxSpeed)
	}

	args = append(args, cookieArgs(req)...)
	args = append(args, containerArgs(extension)...)
	trimFlags, trimDisplayStart, trimDisplayEnd := trimArgs(req)
	args = append(args, trimFlags...)
	args = append(args, liveArgs(req)...)

	embedFlags, thumbnailSkipped := embedArgs(req, extension)
	args = append(args, embedFlags...)
	subtitleFlags, subtitlesSkipped := subtitleArgs(req, extension)
	args = append(args, subtitleFlags...)
	args = append(args, inputArgs(req)...)

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

// outputArgs returns the flags every download starts with: progress on
// lines of its own, verbose output for the log file, no playlist, and the
// save folder and -o template, which carries downloadID. height is the
// quality cap, "" for none; hasTrim marks a trimmed download.
func outputArgs(req DownloadRequest, height, downloadID string, hasTrim bool) []string {
	// A capped download is labelled with the height yt-dlp actually picked,
	// which can be lower than the cap (or, through the selector's final
	// "/best", higher); see heightLabel.
	qualitySuffix := ""
	if height != "" {
		qualitySuffix = heightLabel
	}
	template := outputTemplate(req.FilenameTemplate, qualitySuffix, downloadID, hasTrim)

	// yt-dlp writes .part files and continues them, so an interrupted
	// download resumes where it stopped. A live recording cannot be
	// resumed, and must be written under its final name so that stopping it
	// leaves a file to keep (see finishRecording).
	partFlag := "--continue"
	if req.Live {
		partFlag = "--no-part"
	}
	return []string{
		"--newline", "--progress", "--verbose", partFlag, "--no-playlist",
		"-P", req.SavePath, "-o", template,
	}
}

// ffmpegLocationArgs points yt-dlp at the bundled ffmpeg, if there is one.
func (engine *DownloadEngine) ffmpegLocationArgs() []string {
	if engine.FFmpegPath == "" {
		return nil
	}
	if _, err := os.Stat(engine.FFmpegPath); err != nil {
		return nil
	}
	return []string{"--ffmpeg-location", engine.FFmpegPath}
}

// inputArgs returns what yt-dlp downloads: the loaded info file when req
// has one, else req's URL. yt-dlp downloads every URL it is given as well
// as a loaded info file, so the URL must be left out when the info file
// stands in for it.
func inputArgs(req DownloadRequest) []string {
	if req.infoJSONPath != "" {
		return []string{"--load-info-json", req.infoJSONPath}
	}
	return []string{req.URL}
}

// containerArgs returns the yt-dlp flags that give the download the
// container extension: for an audio format the audio is extracted and
// converted to it, and a video's streams are merged and remuxed, or
// recoded, into it.
func containerArgs(extension string) []string {
	switch {
	case isAudioOnlyExt(extension):
		return []string{"--extract-audio", "--audio-format", extension, "--audio-quality", "0"}
	case extension != "":
		return []string{"--merge-output-format", extension, "--remux-video", extension, "--recode-video", extension}
	default:
		return nil
	}
}

// trimArgs returns the yt-dlp flags that download only req's trim range,
// none when req is not trimmed, and the range's bounds as the log shows
// them: a bound left empty is "start" or "end".
func trimArgs(req DownloadRequest) (args []string, displayStart, displayEnd string) {
	displayStart, displayEnd = req.TrimStart, req.TrimEnd
	if req.TrimStart == "" && req.TrimEnd == "" {
		return nil, displayStart, displayEnd
	}
	start := req.TrimStart
	if start == "" {
		start = "0"
		displayStart = "start"
	}
	end := req.TrimEnd
	if end == "" {
		end = "inf"
		displayEnd = "end"
	}
	args = []string{"--download-sections", fmt.Sprintf("*%s-%s", start, end), "--force-keyframes-at-cuts"}
	return args, displayStart, displayEnd
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
	// OnProgress is called whenever a progress percentage (0..1) is parsed
	// from yt-dlp output.
	OnProgress func(pct float64)
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
	// Bytes is the size on disk of what the download wrote: its finished
	// files or, when it was paused, failed, or cancelled, its partial files
	// (measured before they were removed). ResumedBytes is how much of it
	// an earlier, paused run had already written when this one started.
	// The summary reports these rather than the sizes in yt-dlp's progress
	// lines, which give each stream's total, not what has been downloaded.
	Bytes, ResumedBytes int64
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
	resumedBytes, _ := downloadedBytes(req.SavePath, built.DownloadID)
	stopMonitor := engine.monitorRecording(req, built.DownloadID, cb.OnRecording)
	scan, cmdErr := engine.Execute(ctx, built.Args, opts, cb)
	stopMonitor()

	result := DownloadResult{Extension: built.Extension, Scan: scan, Err: cmdErr, ResumedBytes: resumedBytes}
	if cmdErr != nil && errors.Is(context.Cause(ctx), errPaused) {
		cb.OnLog("[SYSTEM] Paused; the partial download is kept and continues when resumed.", colSystem)
		result.Err, result.Paused = nil, true
		result.Bytes, _ = downloadedBytes(req.SavePath, built.DownloadID)
		return result
	}
	stopped := errors.Is(context.Cause(ctx), errStopKeep)
	keep := cmdErr != nil && (stopped || req.Live) && engine.hasFiles(req.SavePath, built.DownloadID)
	if cmdErr != nil && !keep {
		// Execute returns only once the process tree is dead, so nothing is
		// still writing to these files.
		result.Bytes, _ = downloadedBytes(req.SavePath, built.DownloadID)
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
	// The partial files FinalizeFiles left are ones yt-dlp no longer needs,
	// such as a format an earlier, interrupted run had started. A finished
	// file whose rename failed still carries the download ID, so only
	// partial files are removed.
	engine.RemoveLeftoverPartials(req.SavePath, built.DownloadID, cb.OnLog)
	if keep && req.Live {
		finalPaths = engine.finishRecording(finalPaths, built.Extension, cb.OnLog)
	}
	for _, path := range subtitlePaths {
		cb.OnLog(fmt.Sprintf("[SYSTEM] Saved subtitles: %s", filepath.Base(path)), colSystem)
	}
	result.FinalPaths, result.SubtitlePaths = finalPaths, subtitlePaths
	result.Bytes = totalSize(finalPaths) + totalSize(subtitlePaths)
	return result
}

// hasFiles reports whether yt-dlp wrote any file under downloadID.
func (engine *DownloadEngine) hasFiles(savePath, downloadID string) bool {
	matches, err := filesWithID(savePath, downloadID)
	return err == nil && len(matches) > 0
}

// filesWithID returns the files in dir whose names contain downloadID,
// sorted by name. It lists the folder rather than using filepath.Glob,
// which would read brackets in the folder's name (e.g. "Videos [HD]") as a
// pattern and match nothing.
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
// callers can apply further post-processing. A file that cannot be renamed
// (see renameWithRetry) keeps its temporary name, and that name is returned.
func (engine *DownloadEngine) FinalizeFiles(savePath, downloadID string, onLog func(line string, col color.Color)) []string {
	matches, err := filesWithID(savePath, downloadID)
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
		finalPath, err := uniquePath(cleanPath)
		if err == nil && finalPath != cleanPath {
			onLog(
				fmt.Sprintf("[SYSTEM] File already exists — saving as: %s", filepath.Base(finalPath)),
				colSystem,
			)
		}
		if err == nil {
			err = engine.renameFile(tmpPath, finalPath)
		}
		if err != nil {
			onLog(
				fmt.Sprintf("[SYSTEM] Failed to rename file (%v); it keeps its temporary name: %s", err, filepath.Base(tmpPath)),
				colErrorSoft,
			)
			finalPath = tmpPath
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
// RemovePartialFiles keeps retrying a file that is still locked, and
// FinalizeFiles a rename. On Windows a killed process can keep its files
// locked for a moment after it exits, and an antivirus scanner, the search
// indexer, or Explorer's thumbnails can briefly hold a new file open.
const (
	partialRemoveAttempts   = 10
	partialRemoveRetryDelay = 200 * time.Millisecond
)

// RemovePartialFiles deletes every file yt-dlp wrote under the downloadID
// token, for a download that failed or was cancelled: its .part and
// fragment files, and any media it finished before it stopped. Each
// removal, and each file that could not be removed, is logged.
func (engine *DownloadEngine) RemovePartialFiles(savePath, downloadID string, onLog func(line string, col color.Color)) {
	removeFilesWithID(savePath, downloadID, false, onLog)
}

// RemoveLeftoverPartials deletes only the partial files (see isPartialFile)
// under the downloadID token, for a download that finished: any finished
// file that still carries the token, because its rename failed, is kept.
func (engine *DownloadEngine) RemoveLeftoverPartials(savePath, downloadID string, onLog func(line string, col color.Color)) {
	removeFilesWithID(savePath, downloadID, true, onLog)
}

// removeFilesWithID deletes the files under the downloadID token, or only
// the partial ones when partialOnly is set, and logs each outcome.
func removeFilesWithID(savePath, downloadID string, partialOnly bool, onLog func(line string, col color.Color)) {
	matches, err := filesWithID(savePath, downloadID)
	if err != nil {
		return
	}
	for _, path := range matches {
		if partialOnly && !isPartialFile(path) {
			continue
		}
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

// renameWithRetry renames from to to with rename, retrying for a short
// while if it fails, as removeWithRetry does. A missing source is not
// retried.
func renameWithRetry(rename func(from, to string) error, from, to string) error {
	var err error
	for attempt := 0; attempt < partialRemoveAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(partialRemoveRetryDelay)
		}
		err = rename(from, to)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return err
}

// maxUniquePathTries bounds how many numbered names uniquePath tries. It
// runs with renameMu held, so an endless search would block every other
// download from finishing.
const maxUniquePathTries = 10000

// errNoFreeName is returned by uniquePath when every name it tried is taken.
var errNoFreeName = errors.New("no free file name")

// uniquePath returns path unchanged when no file exists at that location.
// If the path is already taken, it appends an incrementing numeric suffix
// to the base (e.g. "Video.mp4" → "Video 1.mp4" → "Video 2.mp4") until it
// finds a name that does not conflict with an existing file. Only a file
// os.Stat finds counts as taken: any other error (access denied, a name too
// long) leaves the name to the rename, which then fails and is reported.
// After maxUniquePathTries numbered names it returns errNoFreeName rather
// than path, because a rename onto path would replace the file there.
func uniquePath(path string) (string, error) {
	return freePath(path, fileExists)
}

// freePath is uniquePath with taken deciding whether a name is in use.
func freePath(path string, taken func(string) bool) (string, error) {
	if !taken(path) {
		return path, nil
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 1; i <= maxUniquePathTries; i++ {
		candidate := fmt.Sprintf("%s %d%s", base, i, ext)
		if !taken(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w for %s: it and %d numbered names are taken", errNoFreeName, filepath.Base(path), maxUniquePathTries)
}
