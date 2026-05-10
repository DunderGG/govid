// download.go — Drives the yt-dlp download pipeline.
//
// Responsibilities:
//   - Validates user inputs (URL, timestamps, speed limit).
//   - Builds the yt-dlp argument list from the current UI state
//     (format, quality, trim range, speed cap, output template).
//   - Streams yt-dlp stdout/stderr line-by-line, parses progress
//     percentages, and updates the UI in real time.
//   - Sends system notifications on completion or failure (when opted in).
package main

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// startDownload prepares the application for a new download session. It validates
// inputs, resets metrics/visuals, initializes log files if requested, and
// launches the background goroutines for progress interpolation and yt-dlp execution.
func (app *DownloaderApp) startDownload() {
	savePath := strings.TrimSpace(app.ui.path.Text)
	trimStart := strings.TrimSpace(app.ui.trimStart.Text)
	trimEnd := strings.TrimSpace(app.ui.trimEnd.Text)

	// Collect the URL(s) to download.
	var urls []string
	if app.ui.batchMode.Checked {
		for _, line := range strings.Split(app.ui.entry.Text, "\n") {
			if url := strings.TrimSpace(line); url != "" {
				urls = append(urls, url)
			}
		}
		if len(urls) == 0 {
			dialog.ShowError(fmt.Errorf("no URLs entered"), app.window)
			return
		}
	} else {
		rawURL := strings.TrimSpace(app.ui.entry.Text)
		if rawURL == "" {
			dialog.ShowError(fmt.Errorf("URL cannot be empty"), app.window)
			return
		}
		urls = []string{rawURL}
	}

	if savePath == "" {
		dialog.ShowError(fmt.Errorf("save path cannot be empty"), app.window)
		return
	}

	// Trim validation: at least one of trimStart/trimEnd must be provided for trimming,
	// but either can be used alone.
	if validateTimestamp(trimStart) != nil || validateTimestamp(trimEnd) != nil {
		dialog.ShowError(fmt.Errorf("invalid trim time format — use HH:MM:SS, MM:SS, or plain seconds"), app.window)
		return
	}

	app.savePreferences(savePath)

	// Reset UI and stats for new session.
	app.updateStatus("Status: Initializing...")
	app.setProgressNow(0)
	app.stats.targetPct = 0
	app.ui.logList.Objects = nil
	app.ui.logList.Refresh()
	app.ui.cancelBtn.Enable()
	app.setStatusIndicator("active")

	// Initialize logging to file if the option is checked.
	if app.ui.saveLog.Checked {
		logPath := filepath.Join(savePath, "GoVid_log.txt")
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			app.log.file = f
			app.appendOutput(fmt.Sprintf("[SYSTEM] Logging to: %s", logPath), color.RGBA{R: 0, G: 255, B: 255, A: 255})
		} else {
			app.appendOutput(fmt.Sprintf("[ERROR] Failed to create log file: %v", err), color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}

	// queueCtx is a child of context.Background(), a context that never expires on its own.
	// GoRoutines can watch queueCtx.Done() to know when to stop.
	// Calling stopQueue() marks queueCtx as done, which closes the queueCtx.Done() channel.
	// In the batch case, each URL's runCtx is a child of queueCtx via a second context.WithCancel(queueCtx).
	// Cancelling a child only affects that child
	queueCtx, stopQueue := context.WithCancel(context.Background())
	app.cancelFn = stopQueue

	// Launch smoothing goroutine: interpolates progress bar towards the target
	// percentage using an easing step, giving a smooth visual effect.
	go func() {
		ticker := time.NewTicker(time.Duration(fpsInterval) * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-queueCtx.Done():
				return
			case <-ticker.C:
				current := app.ui.progress.Value
				target := app.stats.targetPct
				if current < target {
					step := (target - current) * 0.05
					if step < 0.001 {
						step = 0.001
					}
					newVal := current + step
					if newVal > target {
						newVal = target
					}
					fyne.Do(func() {
						app.ui.progress.SetValue(newVal)
					})
				}
			}
		}
	}()

	if len(urls) > 1 {
		app.appendOutput(fmt.Sprintf("[SYSTEM] Batch mode: %d URLs queued.", len(urls)), color.RGBA{R: 0, G: 200, B: 200, A: 255})
	}

	go func() {
		// Always stop the smoother when the batch finishes, regardless of how it ends.
		defer stopQueue()

		// Build filters once — they come from UI state and are the same for every URL.
		vfFilters, afFilters := app.buildPostProcessFilters()
		hasPostProcess := len(vfFilters) > 0 || len(afFilters) > 0

		// Collect finalized paths from every successful download so post-processing
		// can run over all of them concurrently at the end of the batch.
		var allFinalPaths []string

		for index, url := range urls {
			if queueCtx.Err() != nil {
				break
			}

			// In batch mode, give each URL its own child context so the Cancel
			// button skips only the active download without killing the queue.
			// In single-URL mode, runCtx == queueCtx and Cancel stops all.
			runCtx := queueCtx
			var skipItem context.CancelFunc
			if len(urls) > 1 {
				runCtx, skipItem = context.WithCancel(queueCtx)
				app.cancelFn = skipItem
			}

			if len(urls) > 1 {
				app.appendOutput(fmt.Sprintf("[SYSTEM] ── URL %d of %d ──", index+1, len(urls)), color.RGBA{R: 0, G: 200, B: 200, A: 255})
			}
			if index > 0 {
				// Reset state between URLs: runYtDlp closes the log file and disables the cancel button.
				app.setProgressNow(0)
				app.stats.targetPct = 0
				fyne.Do(func() { app.ui.cancelBtn.Enable() })
				if app.ui.saveLog.Checked {
					logPath := filepath.Join(savePath, "GoVid_log.txt")
					if file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
						app.log.file = file
					}
				}
			}

			paths := app.runYtDlp(runCtx, url, savePath, trimStart, trimEnd)
			allFinalPaths = append(allFinalPaths, paths...)

			if skipItem != nil {
				skipItem() // release the per-item context whether it was cancelled or not
			}
		}

		// Run post-processing over all collected files in one pass so the worker
		// pool can saturate available CPU cores across multiple concurrent jobs.
		if hasPostProcess && len(allFinalPaths) > 0 && queueCtx.Err() == nil {
			app.updateStatus("Status: Post-processing...")
			app.setStatusIndicator("processing")
			app.applyFFmpegFilters(queueCtx, allFinalPaths, vfFilters, afFilters)
			app.updateStatus("Status: Done.")
			app.setStatusIndicator("success")
		}
	}()
}

// runYtDlp manages the external lifecycle of the yt-dlp process. It builds
// the command arguments based on UI selections (quality, format), executes
// the tool, and pipes its output/errors back to the UI in real-time.
// It returns the list of finalized output file paths on success, or nil on
// failure or cancellation. Post-processing is the caller's responsibility.
func (app *DownloaderApp) runYtDlp(ctx context.Context, rawURL string, savePath string, trimStart string, trimEnd string) []string {
	startTime := time.Now()
	formatFlag := "bestvideo+bestaudio/best"
	extension := "mp4"

	quality := app.ui.quality.Selected
	height := ""
	switch quality {
	case "1080p":
		height = "1080"
	case "720p":
		height = "720"
	case "480p":
		height = "480"
	case "360p":
		height = "360"
	}

	selection := app.ui.format.Selected
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
			// Prefer VP9/AV1 + Opus — the native WebM codecs — so the streams
			// remux losslessly without re-encoding. Fall back to any best stream.
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
		qualitySuffix = "_" + quality
	}

	// Embed a unique token into the filename while yt-dlp is running so it never
	// conflicts with existing files mid-download. After a successful download the
	// token is stripped and the file is renamed; a numeric suffix is appended only
	// if a file with the desired clean name already exists.
	downloadID := fmt.Sprintf("GOVID%d", time.Now().UnixNano())

	outputTemplate := "GoVid_%(title)s" + qualitySuffix + "_" + downloadID + ".%(ext)s"
	if app.ui.duplicates.Checked {
		// Epoch names are already globally unique — no post-rename needed.
		outputTemplate = "GoVid_%(title)s" + qualitySuffix + "_%(epoch)s.%(ext)s"
		downloadID = ""
	} else if trimStart != "" || trimEnd != "" {
		outputTemplate = "GoVid_%(title)s" + qualitySuffix + "_TRIM_" + downloadID + ".%(ext)s"
	}

	// Build the full argument list for yt-dlp.
	args := []string{
		"--newline", "--progress", "--verbose", "--no-part", "--no-continue",
		"-f", formatFlag, "-P", savePath, "-o", outputTemplate,
	}

	// Use bundled ffmpeg if available.
	ffmpegPath := app.getLocalBinPath("ffmpeg")
	if _, err := os.Stat(ffmpegPath); err == nil {
		args = append(args, "--ffmpeg-location", ffmpegPath)
	}

	// Apply speed limit if set.
	limit := strings.TrimSpace(app.ui.maxSpeed.Text)
	// If the user didn't enter a limit this time, check if there's a saved preference from before.
	if limit == "" {
		limit = fyne.CurrentApp().Preferences().String("maxSpeed")
	}
	// Append the limit argument if we found a value after checking both the current input and saved preferences.
	if limit != "" {
		args = append(args, "--limit-rate", limit)
	}

	// Apply cookies file if set.
	cookiesPath := strings.TrimSpace(app.ui.cookies.Text)
	if cookiesPath != "" {
		if _, err := os.Stat(cookiesPath); err == nil {
			args = append(args, "--cookies", cookiesPath)
		}
	}

	if extension == "mp3" || extension == "m4a" {
		// Default --audio-quality is 5 (medium) for most formats, but we want 0 (best) for the audio-focused formats.
		args = append(args, "--extract-audio", "--audio-format", extension, "--audio-quality", "0")
	} else if extension != "" {
		// Tell yt-dlp which container to use when merging separate video+audio
		// streams (avoids mismatches like .webm streams merged into .mp4).
		args = append(args, "--merge-output-format", extension)
		// Prefer lossless container remux; fall back to re-encode only if the
		// source codec is incompatible with the target container.
		args = append(args, "--remux-video", extension, "--recode-video", extension)
	}

	// Trim: pass start/end to yt-dlp with keyframe-accurate cuts.
	if trimStart != "" || trimEnd != "" {
		// Start downloading from 0 or the specified time.
		start, displayStart := trimStart, trimStart
		if start == "" {
			start = "0"
			displayStart = "start"
		}
		// Stop downloading at the specified time or the end of the video.
		end, displayEnd := trimEnd, trimEnd
		if end == "" {
			end  = "inf"
			displayEnd = "end"
		}

		args = append(args, "--download-sections", fmt.Sprintf("*%s-%s", start, end))
		args = append(args, "--force-keyframes-at-cuts")
		app.appendOutput(fmt.Sprintf("[SYSTEM] Trimming: %s → %s", displayStart, displayEnd), color.RGBA{R: 0, G: 255, B: 255, A: 255})
	}

	args = append(args, rawURL)

	ytDlpPath := app.getLocalBinPath("yt-dlp")
	if _, err := os.Stat(ytDlpPath); err != nil {
		ytDlpPath = "yt-dlp"
	}

	cmd := exec.CommandContext(ctx, ytDlpPath, args...)
	hideWindow(cmd)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	// If there is an error starting the process, report it and 
	// return nil so the caller knows not to proceed with post-processing.
	if err := cmd.Start(); err != nil {
		app.updateStatus(fmt.Sprintf("Failed to launch yt-dlp: %v", err))
		return nil
	}
	app.updateStatus("Status: Downloading...")

	result := app.watchOutput(stdout, stderr)
	err := cmd.Wait()

	// Rename temp files to their clean, conflict-free names.
	var finalPaths []string
	if err == nil && downloadID != "" {
		finalPaths = app.finalizeDownloadedFiles(savePath, downloadID)
	}

	durationTotal := time.Since(startTime).Seconds()
	durationFormatted := fmt.Sprintf("%.2fs", durationTotal)

	var avgSpeed string
	if durationTotal > 0 && app.stats.downloadedRaw > 0 {
		avg := app.stats.downloadedRaw / durationTotal
		avgSpeed = fmt.Sprintf("%.2f%s/s", avg, app.stats.unit)
	} else {
		avgSpeed = "N/A"
	}

	// uiDone is closed inside fyne.Do once all UI updates for this download are
	// committed. Waiting on it ensures the "DOWNLOAD COMPLETE" block is fully
	// rendered before the caller starts post-processing and overwrites the status.
	uiDone := make(chan struct{})
	fyne.Do(func() {
		app.ui.cancelBtn.Disable()
		if err != nil {
			if ctx.Err() == context.Canceled {
				app.appendOutput("────────────────────────────────────────", color.RGBA{R: 255, G: 165, B: 255, A: 255})
				app.appendOutput("DOWNLOAD ABORTED", color.RGBA{R: 255, G: 165, B: 0, A: 255})
				app.appendOutput(fmt.Sprintf("   ├─ Runtime:    %s", durationFormatted), color.RGBA{R: 255, G: 165, B: 0, A: 255})
				app.appendOutput(fmt.Sprintf("   ├─ Avg Speed:  %s", avgSpeed), color.RGBA{R: 255, G: 165, B: 0, A: 255})
				app.appendOutput(fmt.Sprintf("   └─ Downloaded: %s", app.stats.lastSize), color.RGBA{R: 255, G: 165, B: 0, A: 255})
				app.appendOutput("────────────────────────────────────────", color.RGBA{R: 255, G: 165, B: 255, A: 255})
				app.updateStatus("Status: Canceled.")
				app.setStatusIndicator("canceled")
			} else {
				app.updateStatus("Status: Failed. Check output below.")
				app.setStatusIndicator("failed")
				if app.ui.notify.Checked {
					fyne.CurrentApp().SendNotification(&fyne.Notification{
						Title:   "GoVid — Download Failed",
						Content: "The download encountered an error. Check the log for details.",
					})
				}
			}
		} else {
		// Build a human-readable format line, e.g. "WEBM+M4A → MP4 (remuxed)".
		outExt := strings.ToUpper(extension)
		formatLine := outExt
		if len(result.sourceExts) > 0 {
			seen := map[string]bool{}
			var unique []string
			for _, e := range result.sourceExts {
				exts := strings.ToUpper(e)
				if !seen[exts] {
					seen[exts] = true
					unique = append(unique, exts)
				}
			}
			srcStr := strings.Join(unique, "+")
			switch {
			case result.wasConverted:
				formatLine = fmt.Sprintf("%s → %s (converted)", srcStr, outExt)
			case srcStr != outExt:
				formatLine = fmt.Sprintf("%s → %s (remuxed)", srcStr, outExt)
			default:
				formatLine = fmt.Sprintf("%s (original)", outExt)
			}
		}

		app.appendOutput("────────────────────────────────────────", color.RGBA{R: 0, G: 255, B: 0, A: 255})
		app.appendOutput("DOWNLOAD COMPLETE", color.RGBA{R: 0, G: 200, B: 0, A: 255})
		app.appendOutput(fmt.Sprintf("   ├─ Duration:   %s", durationFormatted), color.RGBA{R: 0, G: 200, B: 0, A: 255})
		app.appendOutput(fmt.Sprintf("   ├─ Avg Speed:  %s", avgSpeed), color.RGBA{R: 0, G: 200, B: 0, A: 255})
		app.appendOutput(fmt.Sprintf("   ├─ Downloaded: %s", app.stats.lastSize), color.RGBA{R: 0, G: 200, B: 0, A: 255})
		app.appendOutput(fmt.Sprintf("   └─ Format:     %s", formatLine), color.RGBA{R: 0, G: 200, B: 0, A: 255})
			app.appendOutput("────────────────────────────────────────", color.RGBA{R: 0, G: 255, B: 0, A: 255})
			app.updateStatus("Status: Success!")
			app.setProgressNow(1)
			app.setStatusIndicator("success")
			if app.ui.notify.Checked {
				fyne.CurrentApp().SendNotification(&fyne.Notification{
					Title:   "GoVid — Download Complete",
					Content: fmt.Sprintf("Duration: %s  •  Downloaded: %s", durationFormatted, app.stats.lastSize),
				})
			}
		}

		app.log.mutex.Lock()
		if app.log.file != nil {
			fmt.Fprintf(app.log.file, "[%s] [SYSTEM] Log file closed.\n", time.Now().Format("15:04:05"))
			app.log.file.Close()
			app.log.file = nil
		}
		app.log.mutex.Unlock()
		close(uiDone)
	})
	// Wait for the UI updates to commit before returning, 
	// ensuring the final status is visible before any post-processing starts.
	<-uiDone
	return finalPaths
}

// validateTimestamp checks that a trim time entry is either empty (meaning no trim)
// or a valid timestamp in one of the formats yt-dlp accepts:
//   - HH:MM:SS  (e.g. 01:30:00)
//   - MM:SS     (e.g. 01:30)
//   - plain seconds, optionally with decimals (e.g. 90 or 90.5)
func validateTimestamp(s string) error {
	if s == "" {
		return nil
	}
	matched, _ := regexp.MatchString(`^\d+:\d{2}:\d{2}$|^\d+:\d{2}$|^\d+(\.\d+)?$`, s)
	if !matched {
		return fmt.Errorf("use HH:MM:SS, MM:SS or seconds (e.g. 90)")
	}
	return nil
}

// getLocalBinPath returns the absolute path to a tool in the 'bin' folder
// next to the current GoVid executable, falling back to the bare tool name
// for system PATH lookups when the local file is not found.
func (app *DownloaderApp) getLocalBinPath(toolName string) string {
	exePath, err := os.Executable()
	if err != nil {
		return toolName
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(toolName, ".exe") {
		toolName += ".exe"
	}
	return filepath.Join(filepath.Dir(exePath), "bin", toolName)
}
