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
	savePath := strings.TrimSpace(app.ui.download.path.Text)
	trimStart := strings.TrimSpace(app.ui.download.trimStart.Text)
	trimEnd := strings.TrimSpace(app.ui.download.trimEnd.Text)

	// Collect the URL(s) to download.
	var urls []string
	if app.ui.download.batchMode.Checked {
		for _, line := range strings.Split(app.ui.download.entry.Text, "\n") {
			if url := strings.TrimSpace(line); url != "" {
				urls = append(urls, url)
			}
		}
		if len(urls) == 0 {
			dialog.ShowError(fmt.Errorf("no URLs entered"), app.window)
			return
		}
	} else {
		rawURL := strings.TrimSpace(app.ui.download.entry.Text)
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
	app.stats.reset()
	app.clearTerminalOutput()
	app.ui.download.cancelBtn.Enable()
	app.ui.download.downloadBtn.Disable()
	app.ui.download.downloadBtn.SetText("Download Now!")
	app.setStatusIndicator("active")
	app.ppFailed.Store(0)
	app.isRunning.Store(true)

	// Initialize logging to file if the option is checked.
	if app.ui.download.saveLog.Checked {
		if logPath, err := app.logSvc.OpenSessionLog(savePath); err == nil {
			app.appendOutput(fmt.Sprintf("[SYSTEM] Logging to: %s", logPath), colSystem)
			cfg := newSessionConfig(app.ui, urls, savePath, trimStart, trimEnd)
			app.logSvc.WriteSessionConfig(cfg, app.appendOutput)
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
	app.SetCancelFunc(stopQueue)

	// Launch the smoothing goroutine, which owns the progress bar until the
	// session ends.
	go app.runProgressSmoother(queueCtx)

	if len(urls) > 1 {
		app.appendOutput(fmt.Sprintf("[SYSTEM] Batch mode: %d URLs queued.", len(urls)), colInfo)
	}

	go func() {
		// Always stop the smoother and re-enable the download button when the
		// batch finishes, regardless of how it ends.
		defer stopQueue()
		defer app.SetCancelFunc(nil)
		defer app.isRunning.Store(false)
		defer fyne.Do(func() {
			if app.ppFailed.Load() > 0 {
				app.ui.download.downloadBtn.SetText("Retry")
			}
			app.ui.download.downloadBtn.Enable()
		})

		// Build filters once — they come from UI state and are the same for every URL.
		// Skipped entirely when the master post-processing toggle is off.
		var vfFilters, afFilters []string
		if app.ui.postProcess.enablePostProcess.Checked {
			vfFilters, afFilters = buildPostProcessFilters(newPostProcessSettings(app.ui))
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
				app.SetCancelFunc(skipItem)
			}

			if len(urls) > 1 {
				app.appendOutput(fmt.Sprintf("[SYSTEM] ── URL %d of %d ──", index+1, len(urls)), colInfo)
			}
			if index > 0 {
				// Reset progress UI and stats between URLs.
				app.stats.reset()
				fyne.Do(func() { app.ui.download.cancelBtn.Enable() })
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
			app.SetCancelFunc(stopQueue)
			fyne.Do(func() { app.ui.download.cancelBtn.Enable() })
			app.updateStatus("Status: Post-processing...")
			app.setStatusIndicator("processing")
			app.applyFFmpegFilters(queueCtx, allFinalPaths, vfFilters, afFilters)
			fyne.Do(func() { app.ui.download.cancelBtn.Disable() })
			if queueCtx.Err() == context.Canceled {
				app.updateStatus("Status: Canceled.")
				app.setStatusIndicator("canceled")
				app.appendOutput("Post-processing canceled by user.", colWarning)
			} else {
				app.updateStatus("Status: Done.")
				app.setStatusIndicator("success")
				if app.ui.download.notify.Checked {
					fyne.CurrentApp().SendNotification(&fyne.Notification{
						Title:   "GoVid — All Done",
						Content: fmt.Sprintf("%d file(s) downloaded and processed.", len(allFinalPaths)),
					})
				}
			}
		} else if queueCtx.Err() == nil && len(allFinalPaths) > 0 && app.ui.download.notify.Checked {
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

// runYtDlp gathers UI state into a DownloadRequest, delegates the full
// download lifecycle to engine.Run, and then handles app-specific side
// effects: history recording, the completion/failure report in the log,
// and system notifications. It returns the list of finalized output file
// paths on success, or nil on failure or cancellation. Post-processing is
// the caller's responsibility. index and total indicate the position within
// a batch (both 1 for single downloads).
func (app *DownloaderApp) runYtDlp(ctx context.Context, rawURL string, savePath string, trimStart string, trimEnd string, index, total int) []string {
	startTime := time.Now()

	// Resolve speed limit: prefer current UI value, fall back to saved preference.
	limit := strings.TrimSpace(app.ui.prefs.maxSpeed.Text)
	if limit == "" {
		limit = app.prefSvc.Load().MaxSpeed
	}

	engine := NewDownloadEngine(
		app.depSvc.Resolve("yt-dlp"),
		app.depSvc.Resolve("ffmpeg"),
	)

	req := DownloadRequest{
		URL:         rawURL,
		SavePath:    savePath,
		Format:      app.ui.download.format.Selected,
		Quality:     app.ui.download.quality.Selected,
		TrimStart:   trimStart,
		TrimEnd:     trimEnd,
		MaxSpeed:    limit,
		CookiesPath: strings.TrimSpace(app.ui.prefs.cookies.Text),
	}
	dl := engine.Run(ctx, req, DownloadOptions{
		AutoRetry: app.ui.download.autoRetry.Checked,
		Index:     index,
		Total:     total,
	}, ProcessCallbacks{
		OnLog:      app.appendOutput,
		OnStatus:   app.updateStatus,
		OnProgress: app.updateProgress,
	})

	if dl.Err == nil {
		app.recordHistory(req, dl.FinalPaths)
	}
	app.reportDownloadResult(ctx, dl, time.Since(startTime))
	return dl.FinalPaths
}

// recordHistory appends one history entry per finalized output file, logging
// a warning (rather than failing the download) if the history write fails.
func (app *DownloaderApp) recordHistory(req DownloadRequest, finalPaths []string) {
	rec := DownloadRecord{
		URL:           req.URL,
		FinalPaths:    finalPaths,
		SavePath:      req.SavePath,
		Format:        req.Format,
		Quality:       req.Quality,
		PostProcessed: app.ui.postProcess.enablePostProcess.Checked,
	}
	if err := app.historySvc.AppendAll(rec); err != nil {
		app.appendOutput(
			fmt.Sprintf("[SYSTEM] Warning: failed to record history: %v", err),
			colWarning,
		)
	}
}

// reportDownloadResult writes the COMPLETE/ABORTED summary to the log and
// updates the status label, status dot, progress bar, and (on failure) the
// session-failed flag and notification. It blocks until the UI updates are
// committed, so the summary is fully rendered before the caller starts
// post-processing and overwrites the status.
func (app *DownloaderApp) reportDownloadResult(ctx context.Context, dl DownloadResult, elapsed time.Duration) {
	lastSize, downloadedRaw, unit := app.stats.sizeSnapshot()
	elapsedStr := fmt.Sprintf("%.2fs", elapsed.Seconds())
	avgSpeed := averageSpeed(downloadedRaw, elapsed.Seconds(), unit)

	uiDone := make(chan struct{})
	fyne.Do(func() {
		defer close(uiDone)
		app.ui.download.cancelBtn.Disable()

		switch {
		case dl.Err == nil:
			app.logDownloadSummary("DOWNLOAD COMPLETE", []summaryRow{
				{"Duration", elapsedStr},
				{"Avg Speed", avgSpeed},
				{"Downloaded", lastSize},
				{"Format", describeOutputFormat(dl.Extension, dl.Scan)},
			}, colSuccess, colSuccessBorder)
			app.updateStatus("Status: Success!")
			app.setProgressNow(1)
			app.setStatusIndicator("success")
		case ctx.Err() == context.Canceled:
			app.logDownloadSummary("DOWNLOAD ABORTED", []summaryRow{
				{"Runtime", elapsedStr},
				{"Avg Speed", avgSpeed},
				{"Downloaded", lastSize},
			}, colWarning, colAbortedBorder)
			app.updateStatus("Status: Canceled.")
			app.setStatusIndicator("canceled")
		default:
			app.updateStatus("Status: Failed. Check output below.")
			app.setStatusIndicator("failed")
			app.ppFailed.Store(1)
			if app.ui.download.notify.Checked {
				fyne.CurrentApp().SendNotification(&fyne.Notification{
					Title:   "GoVid — Download Failed",
					Content: "The download encountered an error. Check the log for details.",
				})
			}
		}
	})
	<-uiDone
}

// summaryBorder frames the post-download summary block in the log.
const summaryBorder = "────────────────────────────────────────"

// summaryRow is one labelled value in a post-download summary block.
type summaryRow struct {
	label string
	value string
}

// logDownloadSummary writes a bordered summary block: a title line followed
// by the rows rendered as a tree (see summaryLines).
func (app *DownloaderApp) logDownloadSummary(title string, rows []summaryRow, textCol, borderCol color.Color) {
	app.appendOutput(summaryBorder, borderCol)
	app.appendOutput(title, textCol)
	for _, line := range summaryLines(rows) {
		app.appendOutput(line, textCol)
	}
	app.appendOutput(summaryBorder, borderCol)
}

// summaryLines renders rows as aligned tree branches, e.g.
// "   ├─ Avg Speed:  1.20MiB/s", with "└─" on the last row.
func summaryLines(rows []summaryRow) []string {
	lines := make([]string, len(rows))
	for i, row := range rows {
		branch := "├─"
		if i == len(rows)-1 {
			branch = "└─"
		}
		lines[i] = fmt.Sprintf("   %s %-11s %s", branch, row.label+":", row.value)
	}
	return lines
}

// averageSpeed formats downloaded/seconds as e.g. "1.25MiB/s", or "N/A" when
// either value is zero (nothing downloaded, or no measurable elapsed time).
func averageSpeed(downloaded, seconds float64, unit string) string {
	if seconds <= 0 || downloaded <= 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.2f%s/s", downloaded/seconds, unit)
}

// describeOutputFormat builds the human-readable format line for the
// download summary, e.g. "WEBM+M4A → MP4 (remuxed)". extension is the final
// output extension; scan supplies the source extensions yt-dlp reported and
// whether ffmpeg re-encoded them. With no source extensions it returns just
// the upper-cased output extension.
func describeOutputFormat(extension string, scan scanResult) string {
	outExt := strings.ToUpper(extension)
	if len(scan.sourceExts) == 0 {
		return outExt
	}

	seen := map[string]bool{}
	var unique []string
	for _, ext := range scan.sourceExts {
		ext = strings.ToUpper(ext)
		if !seen[ext] {
			seen[ext] = true
			unique = append(unique, ext)
		}
	}
	srcStr := strings.Join(unique, "+")

	switch {
	case scan.wasConverted:
		return fmt.Sprintf("%s → %s (converted)", srcStr, outExt)
	case srcStr != outExt:
		return fmt.Sprintf("%s → %s (remuxed)", srcStr, outExt)
	default:
		return fmt.Sprintf("%s (original)", outExt)
	}
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
