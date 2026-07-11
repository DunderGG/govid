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
	"regexp"
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
	app.ui.downloadBtn.Disable()
	app.ui.downloadBtn.SetText("Download Now!")
	app.setStatusIndicator("active")
	app.ppFailed.Store(0)
	app.isRunning.Store(true)

	// Initialize logging to file if the option is checked.
	if app.ui.saveLog.Checked {
		if logPath, err := app.logSvc.OpenSessionLog(savePath); err == nil {
			app.appendOutput(fmt.Sprintf("[SYSTEM] Logging to: %s", logPath), colSystem)
			app.logSessionConfiguration(urls, savePath, trimStart, trimEnd)
		} else {
			app.appendOutput(fmt.Sprintf("[ERROR] Failed to create log file: %v", err), colError)
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
		app.appendOutput(fmt.Sprintf("[SYSTEM] Batch mode: %d URLs queued.", len(urls)), colInfo)
	}

	go func() {
		// Always stop the smoother and re-enable the download button when the
		// batch finishes, regardless of how it ends.
		defer stopQueue()
		defer app.isRunning.Store(false)
		defer fyne.Do(func() {
			if app.ppFailed.Load() > 0 {
				app.ui.downloadBtn.SetText("Retry")
			}
			app.ui.downloadBtn.Enable()
		})

		// Build filters once — they come from UI state and are the same for every URL.
		// Skipped entirely when the master post-processing toggle is off.
		var vfFilters, afFilters []string
		if app.ui.enablePostProcess.Checked {
			vfFilters, afFilters = app.buildPostProcessFilters()
		}
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
				app.appendOutput(fmt.Sprintf("[SYSTEM] ── URL %d of %d ──", index+1, len(urls)), colInfo)
			}
			if index > 0 {
				// Reset progress UI between URLs.
				app.setProgressNow(0)
				app.stats.targetPct = 0
				fyne.Do(func() { app.ui.cancelBtn.Enable() })
			}

			paths := app.runYtDlp(runCtx, url, savePath, trimStart, trimEnd, index+1, len(urls))
			allFinalPaths = append(allFinalPaths, paths...)

			if skipItem != nil {
				skipItem() // release the per-item context whether it was cancelled or not
			}
		}

		// Run post-processing over all collected files in one pass so the worker
		// pool can saturate available CPU cores across multiple concurrent jobs.
		if hasPostProcess && len(allFinalPaths) > 0 && queueCtx.Err() == nil {
			// Re-enable cancel and point it at the queue context so the user can
			// abort all running FFmpeg jobs at once.
			app.cancelFn = stopQueue
			fyne.Do(func() { app.ui.cancelBtn.Enable() })
			app.updateStatus("Status: Post-processing...")
			app.setStatusIndicator("processing")
			app.applyFFmpegFilters(queueCtx, allFinalPaths, vfFilters, afFilters)
			fyne.Do(func() { app.ui.cancelBtn.Disable() })
			if queueCtx.Err() == context.Canceled {
				app.updateStatus("Status: Canceled.")
				app.setStatusIndicator("canceled")
				app.appendOutput("Post-processing canceled by user.", colWarning)
			} else {
				app.updateStatus("Status: Done.")
				app.setStatusIndicator("success")
				if app.ui.notify.Checked {
					fyne.CurrentApp().SendNotification(&fyne.Notification{
						Title:   "GoVid — All Done",
						Content: fmt.Sprintf("%d file(s) downloaded and processed.", len(allFinalPaths)),
					})
				}
			}
		} else if queueCtx.Err() == nil && len(allFinalPaths) > 0 && app.ui.notify.Checked {
			// No post-processing — notify now that all downloads are finished.
			count := len(urls)
			msg := "Your download is ready."
			if count > 1 {
				msg = fmt.Sprintf("%d downloads complete.", count)
			}
			fyne.CurrentApp().SendNotification(&fyne.Notification{
				Title:   "GoVid — Download Complete",
				Content: msg,
			})
		}

		// Close the log file here, after post-processing, so FFmpeg output is captured.
		app.logSvc.CloseSessionLog()
	}()
}

// runYtDlp manages the external lifecycle of the yt-dlp process. It builds
// the command arguments based on UI selections (quality, format), executes
// the tool, and pipes its output/errors back to the UI in real-time.
// It returns the list of finalized output file paths on success, or nil on
// failure or cancellation. Post-processing is the caller's responsibility.
// index and total indicate the position within a batch (both 1 for single downloads).
func (app *DownloaderApp) runYtDlp(ctx context.Context, rawURL string, savePath string, trimStart string, trimEnd string, index, total int) []string {
	startTime := time.Now()

	// Resolve speed limit: prefer current UI value, fall back to saved preference.
	limit := strings.TrimSpace(app.ui.maxSpeed.Text)
	if limit == "" {
		limit = fyne.CurrentApp().Preferences().String("maxSpeed")
	}

	engine := NewDownloadEngine(
		app.depSvc.Resolve("yt-dlp"),
		app.depSvc.Resolve("ffmpeg"),
	)

	built := engine.BuildArgs(DownloadRequest{
		URL:         rawURL,
		SavePath:    savePath,
		Format:      app.ui.format.Selected,
		Quality:     app.ui.quality.Selected,
		TrimStart:   trimStart,
		TrimEnd:     trimEnd,
		MaxSpeed:    limit,
		CookiesPath: strings.TrimSpace(app.ui.cookies.Text),
	})

	args := built.Args
	extension := built.Extension
	downloadID := built.DownloadID
	selection := app.ui.format.Selected
	quality := app.ui.quality.Selected

	if built.HasTrim {
		app.appendOutput(
			fmt.Sprintf("[SYSTEM] Trimming: %s → %s", built.TrimDisplayStart, built.TrimDisplayEnd),
			colSystem,
		)
	}

	result, cmdErr := engine.Execute(ctx, args, app.ui.autoRetry.Checked, index, total, ProcessCallbacks{
		OnLog:       app.appendOutput,
		OnStatus:    app.updateStatus,
		WatchOutput: app.watchOutput,
	})

	// Rename temp files to their clean, conflict-free names.
	var finalPaths []string
	if cmdErr == nil && downloadID != "" {
		finalPaths = app.finalizeDownloadedFiles(savePath, downloadID)
		postProcessed := app.ui.enablePostProcess.Checked
		rec := DownloadRecord{
			URL:           rawURL,
			FinalPaths:    finalPaths,
			SavePath:      savePath,
			Format:        selection,
			Quality:       quality,
			PostProcessed: postProcessed,
		}
		if historyErr := app.historySvc.AppendAll(rec); historyErr != nil {
			app.appendOutput(
				fmt.Sprintf("[SYSTEM] Warning: failed to record history: %v", historyErr),
				colWarning,
			)
		}
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
		if cmdErr != nil {
			if ctx.Err() == context.Canceled {
				app.appendOutput("────────────────────────────────────────", colAbortedBorder)
				app.appendOutput("DOWNLOAD ABORTED", colWarning)
				app.appendOutput(fmt.Sprintf("   ├─ Runtime:    %s", durationFormatted), colWarning)
				app.appendOutput(fmt.Sprintf("   ├─ Avg Speed:  %s", avgSpeed), colWarning)
				app.appendOutput(fmt.Sprintf("   └─ Downloaded: %s", app.stats.lastSize), colWarning)
				app.appendOutput("────────────────────────────────────────", colAbortedBorder)
				app.updateStatus("Status: Canceled.")
				app.setStatusIndicator("canceled")
			} else {
				app.updateStatus("Status: Failed. Check output below.")
				app.setStatusIndicator("failed")
				app.ppFailed.Store(1)
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

			app.appendOutput("────────────────────────────────────────", colSuccessBorder)
			app.appendOutput("DOWNLOAD COMPLETE", colSuccess)
			app.appendOutput(fmt.Sprintf("   ├─ Duration:   %s", durationFormatted), colSuccess)
			app.appendOutput(fmt.Sprintf("   ├─ Avg Speed:  %s", avgSpeed), colSuccess)
			app.appendOutput(fmt.Sprintf("   ├─ Downloaded: %s", app.stats.lastSize), colSuccess)
			app.appendOutput(fmt.Sprintf("   └─ Format:     %s", formatLine), colSuccess)
			app.appendOutput("────────────────────────────────────────", colSuccessBorder)
			app.updateStatus("Status: Success!")
			app.setProgressNow(1)
			app.setStatusIndicator("success")
		}

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
func validateTimestamp(timestamp string) error {
	if timestamp == "" {
		return nil
	}
	matched, _ := regexp.MatchString(`^\d+:\d{2}:\d{2}$|^\d+:\d{2}$|^\d+(\.\d+)?$`, timestamp)
	if !matched {
		return fmt.Errorf("use HH:MM:SS, MM:SS or seconds (e.g. 90)")
	}
	return nil
}

// logSessionConfiguration writes the current run configuration to the log output
// at the start of a session, including the raw URL field and parsed URL list.
func (app *DownloaderApp) logSessionConfiguration(urls []string, savePath, trimStart, trimEnd string) {
	maxSpeed := strings.TrimSpace(app.ui.maxSpeed.Text)
	if maxSpeed == "" {
		maxSpeed = "(none)"
	}
	cookiesPath := strings.TrimSpace(app.ui.cookies.Text)
	if cookiesPath == "" {
		cookiesPath = "(none)"
	}
	rawURLField := app.ui.entry.Text
	if strings.TrimSpace(rawURLField) == "" {
		rawURLField = "(empty)"
	}

	app.appendOutput("[SYSTEM] ===== Session Configuration =====", colSystem)
	app.appendOutput(fmt.Sprintf("[SYSTEM] Save path: %s", savePath), colSystem)
	app.appendOutput(fmt.Sprintf("[SYSTEM] Mode: batch=%t, url_count=%d", app.ui.batchMode.Checked, len(urls)), colSystem)
	app.appendOutput(fmt.Sprintf("[SYSTEM] Format/quality: %s / %s", app.ui.format.Selected, app.ui.quality.Selected), colSystem)
	app.appendOutput(fmt.Sprintf("[SYSTEM] Trim: start=%q, end=%q", trimStart, trimEnd), colSystem)
	app.appendOutput(fmt.Sprintf("[SYSTEM] Max speed: %s", maxSpeed), colSystem)
	app.appendOutput(fmt.Sprintf("[SYSTEM] Cookies file: %s", cookiesPath), colSystem)
	app.appendOutput(fmt.Sprintf("[SYSTEM] Runtime toggles: saveLog=%t, notify=%t, autoRetry=%t, postProcess=%t", app.ui.saveLog.Checked, app.ui.notify.Checked, app.ui.autoRetry.Checked, app.ui.enablePostProcess.Checked), colSystem)
	app.appendOutput(fmt.Sprintf("[SYSTEM] Preferences: savePrefs=%t, logLimit=%s, theme=%s", app.ui.savePrefs.Checked, app.ui.logLimit.Selected, app.ui.themeMode.Selected), colSystem)

	app.appendOutput(fmt.Sprintf("[SYSTEM] URL field (raw): %q", rawURLField), colSystem)
	for i, url := range urls {
		app.appendOutput(fmt.Sprintf("[SYSTEM] URL[%d]: %s", i+1, url), colSystem)
	}

	app.appendOutput(fmt.Sprintf("[SYSTEM] Post-process toggles: smoothMotion=%t, sharpen=%t, normalizeAudio=%t, vividMode=%t, denoise=%t, hdrToSdr=%t, deband=%t, autoCrop=%t, stabilize=%t, deinterlace=%t, nightMode=%t, upscaleVideo=%t", app.ui.smoothMotion.Checked, app.ui.sharpen.Checked, app.ui.normalizeAudio.Checked, app.ui.vividMode.Checked, app.ui.denoise.Checked, app.ui.hdrToSdr.Checked, app.ui.deband.Checked, app.ui.autoCrop.Checked, app.ui.stabilize.Checked, app.ui.deinterlace.Checked, app.ui.nightMode.Checked, app.ui.upscaleVideo.Checked), colSystem)
	app.appendOutput(fmt.Sprintf("[SYSTEM] Post-process values: smoothMotionMode=%s, smoothFPS=%.0f, sharpenAmount=%.1f, denoiseMode=%s, upscaleTarget=%s", app.ui.smoothMotionMode.Selected, app.ui.smoothMotionFPS.Value, app.ui.sharpenAmount.Value, app.ui.denoiseMode.Selected, app.ui.upscaleTarget.Selected), colSystem)
	app.appendOutput("[SYSTEM] =================================", colSystem)
}
