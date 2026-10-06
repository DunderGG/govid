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
	"strings"
	"time"
)

// DownloadEngine holds the resolved paths to the tools it drives.
// Construct one with NewDownloadEngine and call its methods to build
// argument lists and execute downloads.
type DownloadEngine struct {
	YtDlpPath  string // absolute path to yt-dlp binary
	FFmpegPath string // absolute path to ffmpeg binary (empty → omit flag)
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

	// Written into the downloaded file by yt-dlp (with ffmpeg).
	EmbedMetadata  bool // title, artist, upload date, … tags
	EmbedThumbnail bool // the thumbnail as cover art (converted to JPEG)
	EmbedChapters  bool // chapter markers

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
}

// formatSelection returns the yt-dlp -f selector and the output extension
// for a format and quality choice, plus the height cap the quality sets
// ("" for none). The download and the probe share it, so the probe reports
// the formats the download will fetch.
func formatSelection(format, quality string) (formatFlag, extension, height string) {
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

	switch {
	case strings.Contains(format, formatMP3):
		return "bestaudio/best", "mp3", height
	case strings.Contains(format, formatM4A):
		return "bestaudio[ext=m4a]/bestaudio/best", "m4a", height
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

// BuildArgs derives the full yt-dlp argument list from a DownloadRequest.
// FFmpegPath comes from the engine rather than the request, since it is
// configured once at engine construction and shared across all downloads.
func (engine *DownloadEngine) BuildArgs(req DownloadRequest) DownloadArgs {
	formatFlag, extension, height := formatSelection(req.Format, req.Quality)

	qualitySuffix := ""
	if height != "" {
		qualitySuffix = "_" + req.Quality
	}

	// Embed a unique token into the filename while yt-dlp is running so it never
	// conflicts with existing files mid-download. Stripped on finalization.
	downloadID := fmt.Sprintf("GOVID%d", time.Now().UnixNano())

	outputTemplate := "GoVid_%(title)s" + qualitySuffix + "_" + downloadID + ".%(ext)s"
	hasTrim := req.TrimStart != "" || req.TrimEnd != ""
	if hasTrim {
		outputTemplate = "GoVid_%(title)s" + qualitySuffix + "_TRIM_" + downloadID + ".%(ext)s"
	}

	args := []string{
		"--newline", "--progress", "--verbose", "--no-part", "--no-continue", "--no-playlist",
		"-f", formatFlag, "-P", req.SavePath, "-o", outputTemplate,
	}

	// Use bundled ffmpeg if available.
	if engine.FFmpegPath != "" {
		if _, err := os.Stat(engine.FFmpegPath); err == nil {
			args = append(args, "--ffmpeg-location", engine.FFmpegPath)
		}
	}

	if req.MaxSpeed != "" {
		args = append(args, "--limit-rate", req.MaxSpeed)
	}

	if req.CookiesPath != "" {
		if _, err := os.Stat(req.CookiesPath); err == nil {
			args = append(args, "--cookies", req.CookiesPath)
		}
	}

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

	embedFlags, thumbnailSkipped := embedArgs(req, extension)
	args = append(args, embedFlags...)

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
	}
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

		if opts.Total > 1 {
			cb.OnStatus(fmt.Sprintf("Status: Downloading (%d of %d)...", opts.Index, opts.Total))
		} else {
			cb.OnStatus("Status: Downloading...")
		}

		result = engine.watchOutput(stdout, stderr, cb)
		cmdErr = cmd.Wait()
		if cmdErr == nil {
			break
		}
	}

	return result, cmdErr
}

// DownloadResult is the outcome of a single Run call: everything the caller
// needs to report status and record history, with no intermediate UI state.
type DownloadResult struct {
	FinalPaths []string
	Extension  string // e.g. "mp4", "mkv", "mp3"
	Scan       scanResult
	Err        error
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

	result := engine.runArgs(ctx, req.SavePath, built, opts, cb)
	if req.infoJSONPath != "" && result.Err != nil && ctx.Err() == nil && result.Scan.hadExpiredLinkErr {
		cb.OnLog("[SYSTEM] The video's download links were refused (expired?); asking the site for new ones.", colWarning)
		req.infoJSONPath = ""
		result = engine.runArgs(ctx, req.SavePath, engine.BuildArgs(req), opts, cb)
	}
	return result
}

// runArgs runs yt-dlp with built's arguments, then finalizes the files it
// wrote, or removes them if it failed or was cancelled.
func (engine *DownloadEngine) runArgs(ctx context.Context, savePath string, built DownloadArgs, opts DownloadOptions, cb ProcessCallbacks) DownloadResult {
	scan, cmdErr := engine.Execute(ctx, built.Args, opts, cb)

	var finalPaths []string
	if cmdErr == nil {
		finalPaths = engine.FinalizeFiles(savePath, built.DownloadID, cb.OnLog)
	} else {
		// Execute returns only once the process tree is dead, so nothing is
		// still writing to these files.
		engine.RemovePartialFiles(savePath, built.DownloadID, cb.OnLog)
	}

	return DownloadResult{
		FinalPaths: finalPaths,
		Extension:  built.Extension,
		Scan:       scan,
		Err:        cmdErr,
	}
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

// FinalizeFiles finds all files written by yt-dlp under the given downloadID
// token, strips the token from their names, and renames them to their final
// conflict-free paths using uniquePath. It returns the list of final paths so
// callers can apply further post-processing.
func (engine *DownloadEngine) FinalizeFiles(savePath, downloadID string, onLog func(line string, col color.Color)) []string {
	pattern := filepath.Join(savePath, "*"+downloadID+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	var finalPaths []string
	for _, tmpPath := range matches {
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

// partialRemoveAttempts and partialRemoveRetryDelay bound how long
// RemovePartialFiles keeps retrying a file that is still locked. On Windows a
// killed process can keep its files locked for a moment after it exits.
const (
	partialRemoveAttempts   = 10
	partialRemoveRetryDelay = 200 * time.Millisecond
)

// RemovePartialFiles deletes every file yt-dlp wrote under the downloadID
// token, for a download that failed or was cancelled. Because yt-dlp runs
// with --no-part, these are incomplete media files under their final-looking
// names. Each removal, and each file that could not be removed, is logged.
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
