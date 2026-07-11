// download_engine.go — yt-dlp execution engine.
//
// Responsibilities:
//   - DownloadEngine: typed component holding tool paths, with methods for
//     building yt-dlp arguments and executing downloads with retry logic.
//   - DownloadRequest: typed value object holding per-download inputs.
//   - DownloadArgs: typed value object holding the resolved argument list
//     and derived metadata (extension, downloadID, trim display strings).
//   - ProcessCallbacks: bridge that lets the engine report events to the UI.
package main

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
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
	Format      string // e.g. "MP4", "MKV", "WebM", "MP3", "M4A"
	Quality     string // e.g. "Best Quality", "1080p", "720p"
	TrimStart   string // HH:MM:SS or empty
	TrimEnd     string // HH:MM:SS or empty
	MaxSpeed    string // e.g. "5M" or empty
	CookiesPath string // path to cookies.txt or empty
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
}

// BuildArgs derives the full yt-dlp argument list from a DownloadRequest.
// FFmpegPath comes from the engine rather than the request, since it is
// configured once at engine construction and shared across all downloads.
func (engine *DownloadEngine) BuildArgs(req DownloadRequest) DownloadArgs {
	formatFlag := "bestvideo+bestaudio/best"
	extension := "mp4"

	height := ""
	switch req.Quality {
	case "1080p":
		height = "1080"
	case "720p":
		height = "720"
	case "480p":
		height = "480"
	case "360p":
		height = "360"
	}

	selection := req.Format
	if strings.Contains(selection, "MP3") {
		formatFlag = "bestaudio/best"
		extension = "mp3"
	} else if strings.Contains(selection, "M4A") {
		formatFlag = "bestaudio[ext=m4a]/bestaudio/best"
		extension = "m4a"
	} else {
		if height != "" {
			formatFlag = fmt.Sprintf("bestvideo[height<=%s]+bestaudio/best[height<=%s]/best", height, height)
		}
		if strings.Contains(selection, "WebM") {
			extension = "webm"
			if height != "" {
				formatFlag = fmt.Sprintf(
					"bestvideo[vcodec^=vp9][height<=%s]+bestaudio[acodec=opus]/bestvideo[vcodec^=av01][height<=%s]+bestaudio[acodec=opus]/bestvideo[height<=%s]+bestaudio/best",
					height, height, height,
				)
			} else {
				formatFlag = "bestvideo[vcodec^=vp9]+bestaudio[acodec=opus]/bestvideo[vcodec^=av01]+bestaudio[acodec=opus]/bestvideo+bestaudio/best"
			}
		} else if strings.Contains(selection, "MKV") {
			extension = "mkv"
		}
	}

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

	if extension == "mp3" || extension == "m4a" {
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

	args = append(args, req.URL)

	return DownloadArgs{
		Args:             args,
		Extension:        extension,
		DownloadID:       downloadID,
		HasTrim:          hasTrim,
		TrimDisplayStart: trimDisplayStart,
		TrimDisplayEnd:   trimDisplayEnd,
	}
}

// ProcessCallbacks lets the engine report events to the UI layer without
// importing Fyne. The caller wires these to its own log/status/watch methods.
type ProcessCallbacks struct {
	// OnLog is called for every message the engine wants to show in the log view.
	OnLog func(line string, col color.Color)
	// OnStatus is called to update the short status label.
	OnStatus func(msg string)
	// WatchOutput reads stdout and stderr from a running yt-dlp process,
	// forwarding lines to the UI and returning collected scan metadata.
	WatchOutput func(stdout, stderr io.Reader) scanResult
}

// Execute runs yt-dlp with the given args, retrying on transient errors
// when autoRetry is true (up to 3 attempts with 1 s / 5 s / 30 s backoff).
// It is free of UI or Fyne references; all event reporting goes through cb.
// Returns the scan metadata and the final process error (nil on success).
func (engine *DownloadEngine) Execute(ctx context.Context, args []string, autoRetry bool, index, total int, cb ProcessCallbacks) (scanResult, error) {
	retryDelays := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}
	var result scanResult
	var cmdErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if !autoRetry || !result.hadTransientErr {
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

		cmd := exec.CommandContext(ctx, engine.YtDlpPath, args...)
		hideWindow(cmd)

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

		if total > 1 {
			cb.OnStatus(fmt.Sprintf("Status: Downloading (%d of %d)...", index, total))
		} else {
			cb.OnStatus("Status: Downloading...")
		}

		result = cb.WatchOutput(stdout, stderr)
		cmdErr = cmd.Wait()
		if cmdErr == nil {
			break
		}
	}

	return result, cmdErr
}
