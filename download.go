// download.go — Drives a download session from the UI.
//
// Responsibilities:
//   - Reads and validates the session inputs (URLs, save path, trim range)
//     from the widgets, and resets the UI for a new session.
//   - Runs the session: checks each URL and expands playlists (playlist.go),
//     downloads each queued URL through DownloadEngine (runYtDlp wires the
//     engine's ProcessCallbacks to the UI), then post-processes the
//     results. Building yt-dlp arguments and executing it live in
//     download_engine.go; parsing its output lives in logscanner.go.
//   - Records download history and logs a per-download summary.
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

// downloadSession holds the inputs of one download session. They are read
// from the widgets once, on the UI thread, when the session starts.
type downloadSession struct {
	urls      []string    // the URLs as entered
	items     []queueItem // the download queue: urls after checkURLs expanded any playlists
	savePath  string
	trimStart string
	trimEnd   string
	vfFilters []string // post-processing video filters; nil when post-processing is off
	afFilters []string // post-processing audio filters; nil when post-processing is off
}

// hasPostProcess reports whether any post-processing filter is active.
func (session downloadSession) hasPostProcess() bool {
	return len(session.vfFilters) > 0 || len(session.afFilters) > 0
}

// isBatch reports whether the session's queue holds more than one URL.
func (session downloadSession) isBatch() bool {
	return len(session.items) > 1
}

// startDownload validates the inputs of a new download session, resets the UI
// for it, and launches the progress smoother and the session goroutine.
func (app *DownloaderApp) startDownload() {
	session, err := app.readSession()
	if err != nil {
		dialog.ShowError(err, app.window)
		return
	}

	app.uiManager.savePreferences(session.savePath)
	app.resetSession()
	app.openSessionLog(session)

	// queueCtx never expires on its own; stopQueue (wired to Cancel) ends the
	// whole session. In batch mode each URL gets a child of queueCtx so Cancel
	// can skip one item without stopping the queue (see downloadItem).
	queueCtx, stopQueue := context.WithCancel(context.Background())
	app.SetCancelFunc(stopQueue)
	app.setStopFunc(stopQueue)

	// The smoother owns the progress bar until the session ends.
	go app.runProgressSmoother(queueCtx)
	app.sessions.Add(1)
	go app.runSession(queueCtx, stopQueue, session)
}

// readSession reads and validates the session inputs from the widgets. The
// returned error is suitable for showing to the user as-is.
func (app *DownloaderApp) readSession() (downloadSession, error) {
	urls, err := collectURLs(app.ui.download.entry.Text, app.ui.download.batchMode.Checked)
	if err != nil {
		return downloadSession{}, err
	}

	session := downloadSession{
		urls:      urls,
		savePath:  strings.TrimSpace(app.ui.download.path.Text),
		trimStart: strings.TrimSpace(app.ui.download.trimStart.Text),
		trimEnd:   strings.TrimSpace(app.ui.download.trimEnd.Text),
	}
	if session.savePath == "" {
		return downloadSession{}, fmt.Errorf("save path cannot be empty")
	}
	// Either trim bound may be used alone; each must be empty or valid.
	if validateTimestamp(session.trimStart) != nil || validateTimestamp(session.trimEnd) != nil {
		return downloadSession{}, fmt.Errorf("invalid trim time format — use HH:MM:SS, MM:SS, or plain seconds")
	}

	// Build the filters once: they are the same for every URL in the session.
	if app.ui.postProcess.enablePostProcess.Checked {
		session.vfFilters, session.afFilters = buildPostProcessFilters(newPostProcessSettings(app.ui))
	}
	return session, nil
}

// collectURLs extracts the URLs to download from the URL entry's text. In
// batch mode every non-blank line is a URL; otherwise the whole trimmed text
// is a single URL. It returns an error when no URL was entered.
func collectURLs(text string, batch bool) ([]string, error) {
	if !batch {
		rawURL := strings.TrimSpace(text)
		if rawURL == "" {
			return nil, fmt.Errorf("URL cannot be empty")
		}
		return []string{rawURL}, nil
	}

	var urls []string
	for _, line := range strings.Split(text, "\n") {
		if url := strings.TrimSpace(line); url != "" {
			urls = append(urls, url)
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("no URLs entered")
	}
	return urls, nil
}

// resetSession clears the previous session's log, stats, and failure flag,
// marks a session as running, and puts the buttons and status into their
// "downloading" state. Must be called on the UI thread.
func (app *DownloaderApp) resetSession() {
	app.updateStatus("Status: Initializing...")
	app.stats.reset()
	app.clearTerminalOutput()
	app.ui.download.cancelBtn.Enable()
	app.ui.download.downloadBtn.Disable()
	app.ui.download.downloadBtn.SetText("Download Now!")
	app.setStatusIndicator(StatusActive)
	app.sessionFailed.Store(false)
	app.isRunning.Store(true)
}

// openSessionLog starts the on-disk session log and writes the session
// configuration header to it, when "Save output to log file" is checked.
func (app *DownloaderApp) openSessionLog(session downloadSession) {
	if !app.ui.download.saveLog.Checked {
		return
	}
	logPath, err := app.logSvc.OpenSessionLog(session.savePath)
	if err != nil {
		app.appendOutput(fmt.Sprintf("[ERROR] Failed to create log file: %v", err), colError)
		return
	}
	app.appendOutput(fmt.Sprintf("[SYSTEM] Logging to: %s", logPath), colSystem)
	cfg := newSessionConfig(app.ui, session.urls, session.savePath, session.trimStart, session.trimEnd)
	app.logSvc.WriteSessionConfig(cfg, app.appendOutput)
}

// runSession checks every URL in the session (expanding playlists into the
// videos the user picks), downloads them, post-processes the results,
// sends the completion notification, and finally restores the idle UI. It
// runs on its own goroutine and owns queueCtx until it returns.
func (app *DownloaderApp) runSession(queueCtx context.Context, stopQueue context.CancelFunc, session downloadSession) {
	// Always stop the smoother and re-enable the download button when the
	// session finishes, regardless of how it ends. sessions.Done runs last so
	// Shutdown sees the session as finished only once everything is closed.
	defer app.sessions.Done()
	defer stopQueue()
	defer app.setStopFunc(nil)
	defer app.SetCancelFunc(nil)
	defer app.isRunning.Store(false)
	defer app.finishSessionUI()

	session.items = app.checkURLs(queueCtx, session)
	switch {
	case queueCtx.Err() != nil:
		app.updateStatus("Status: Canceled.")
		app.setStatusIndicator(StatusCanceled)
	case len(session.items) == 0:
		app.updateStatus("Status: Nothing to download.")
		app.setStatusIndicator(StatusIdle)
	}

	finalPaths := app.runQueue(queueCtx, session)

	switch {
	case queueCtx.Err() != nil || len(finalPaths) == 0:
		// Cancelled, or nothing downloaded: nothing to process or announce.
	case session.hasPostProcess():
		app.runPostProcessing(queueCtx, stopQueue, finalPaths, session)
		if queueCtx.Err() == nil {
			app.notifyCompletion(true, len(finalPaths), len(session.items))
		}
	default:
		app.notifyCompletion(false, len(finalPaths), len(session.items))
	}

	// Close the log file here, after post-processing, so FFmpeg output is captured.
	app.logSvc.CloseSessionLog()
}

// finishSessionUI shows any log lines and status still queued, so the
// session's summary appears at once, and re-enables the download button,
// relabelling it "Retry" if any job in the session failed.
func (app *DownloaderApp) finishSessionUI() {
	app.uiManager.flushLog()
	app.statusThrottle.Flush()
	fyne.Do(func() {
		if app.sessionFailed.Load() {
			app.ui.download.downloadBtn.SetText("Retry")
		}
		app.ui.download.downloadBtn.Enable()
	})
}

// runQueue downloads the session's queue one item after another, checking
// for free disk space before each (see checkDiskSpace), until it is
// cancelled, and returns the finalized paths of every successful download so
// post-processing can run over all of them at once.
func (app *DownloaderApp) runQueue(queueCtx context.Context, session downloadSession) []string {
	if session.isBatch() {
		app.appendOutput(fmt.Sprintf("[SYSTEM] Batch mode: %d URLs queued.", len(session.items)), colInfo)
	}

	var finalPaths []string
	continueLowSpace := false // the user chose to continue despite low disk space
	for index := range session.items {
		if queueCtx.Err() != nil {
			break
		}
		switch app.checkDiskSpace(queueCtx, session, index, &continueLowSpace) {
		case spaceSkip:
			app.appendOutput(fmt.Sprintf("[SYSTEM] Skipped %s: not enough disk space.", session.items[index].url), colWarning)
			continue
		case spaceStop:
			app.appendOutput("[SYSTEM] Queue stopped: not enough disk space.", colWarning)
			app.updateStatus("Status: Stopped (not enough disk space).")
			app.setStatusIndicator(StatusCanceled)
			return finalPaths
		}
		paths := app.downloadItem(queueCtx, session, index)
		finalPaths = append(finalPaths, paths...)
	}
	return finalPaths
}

// downloadItem downloads session.items[index] and returns its finalized paths.
func (app *DownloaderApp) downloadItem(queueCtx context.Context, session downloadSession, index int) []string {
	// In batch mode, give each URL its own child context so the Cancel
	// button skips only the active download without killing the queue.
	// In single-URL mode, runCtx == queueCtx and Cancel stops all.
	runCtx := queueCtx
	if session.isBatch() {
		var skipItem context.CancelFunc
		runCtx, skipItem = context.WithCancel(queueCtx)
		defer skipItem() // release the per-item context whether it was cancelled or not
		app.SetCancelFunc(skipItem)
		app.appendOutput(fmt.Sprintf("[SYSTEM] ── URL %d of %d ──", index+1, len(session.items)), colInfo)
	}
	if index > 0 {
		// Reset progress UI and stats between URLs.
		app.stats.reset()
		fyne.Do(func() { app.ui.download.cancelBtn.Enable() })
	}

	url := session.items[index].url
	return app.runYtDlp(runCtx, url, session.savePath, session.trimStart, session.trimEnd, index+1, len(session.items))
}

// runPostProcessing runs the session's filters over every downloaded file in
// one pass, so the worker pool can saturate the CPU across concurrent jobs,
// and reports the outcome in the status label and dot.
func (app *DownloaderApp) runPostProcessing(queueCtx context.Context, stopQueue context.CancelFunc, paths []string, session downloadSession) {
	// Re-enable cancel and point it at the queue context so the user can
	// abort all running FFmpeg jobs at once.
	app.SetCancelFunc(stopQueue)
	fyne.Do(func() { app.ui.download.cancelBtn.Enable() })
	app.updateStatus("Status: Post-processing...")
	app.setStatusIndicator(StatusProcessing)

	app.applyFFmpegFilters(queueCtx, paths, session.vfFilters, session.afFilters)

	fyne.Do(func() { app.ui.download.cancelBtn.Disable() })
	if queueCtx.Err() != nil {
		app.updateStatus("Status: Canceled.")
		app.setStatusIndicator(StatusCanceled)
		app.appendOutput("Post-processing canceled by user.", colWarning)
		return
	}
	app.updateStatus("Status: Done.")
	app.setStatusIndicator(StatusSuccess)
}

// notifyCompletion sends the end-of-session system notification when
// "Notify on Completion" is checked.
func (app *DownloaderApp) notifyCompletion(postProcessed bool, fileCount, urlCount int) {
	if !app.ui.download.notify.Checked {
		return
	}
	fyne.CurrentApp().SendNotification(completionNotification(postProcessed, fileCount, urlCount))
}

// completionNotification builds the end-of-session notification. After
// post-processing it counts the processed files; otherwise it counts the
// queued URLs.
func completionNotification(postProcessed bool, fileCount, urlCount int) *fyne.Notification {
	if postProcessed {
		return &fyne.Notification{
			Title:   "GoVid — All Done",
			Content: fmt.Sprintf("%d file(s) downloaded and processed.", fileCount),
		}
	}
	msg := "Your download is ready."
	if urlCount > 1 {
		msg = fmt.Sprintf("%d downloads complete.", urlCount)
	}
	return &fyne.Notification{
		Title:   "GoVid — Download Complete",
		Content: msg,
	}
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

	req := app.newDownloadRequest(rawURL, savePath, trimStart, trimEnd)
	dl := app.newDownloadEngine().Run(ctx, req, DownloadOptions{
		AutoRetry: app.ui.download.autoRetry.Checked,
		Index:     index,
		Total:     total,
	}, ProcessCallbacks{
		OnLog:      app.appendOutput,
		OnStatus:   app.updateStatus,
		OnProgress: app.updateProgress,
		OnPhase:    app.showDownloadPhase,
	})

	if dl.Err == nil {
		app.recordHistory(req, dl.FinalPaths)
	}
	app.reportDownloadResult(ctx, dl, time.Since(startTime))
	return dl.FinalPaths
}

// newDownloadEngine returns a DownloadEngine for the resolved yt-dlp and
// ffmpeg.
func (app *DownloaderApp) newDownloadEngine() *DownloadEngine {
	return NewDownloadEngine(app.depSvc.Resolve("yt-dlp"), app.depSvc.Resolve("ffmpeg"))
}

// newDownloadRequest gathers the widget state a download of rawURL needs
// into a DownloadRequest. The probe uses the same request, so it describes
// the formats the download will fetch.
func (app *DownloaderApp) newDownloadRequest(rawURL, savePath, trimStart, trimEnd string) DownloadRequest {
	// Resolve speed limit: prefer current UI value, fall back to saved preference.
	limit := strings.TrimSpace(app.ui.prefs.maxSpeed.Text)
	if limit == "" {
		limit = app.prefSvc.Load().MaxSpeed
	}

	return DownloadRequest{
		URL:         rawURL,
		SavePath:    savePath,
		Format:      app.ui.download.format.Selected,
		Quality:     app.ui.download.quality.Selected,
		TrimStart:   trimStart,
		TrimEnd:     trimEnd,
		MaxSpeed:    limit,
		CookiesPath: strings.TrimSpace(app.ui.prefs.cookies.Text),

		EmbedMetadata:  app.ui.prefs.embedMetadata.Checked,
		EmbedThumbnail: app.ui.prefs.embedThumbnail.Checked,
		EmbedChapters:  app.ui.prefs.embedChapters.Checked,
	}
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
// session-failed flag and notification. It blocks until those updates are
// committed and the new status is applied, so the result is shown before the
// caller starts post-processing and overwrites the status.
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
			app.setStatusIndicator(StatusSuccess)
		case ctx.Err() == context.Canceled:
			app.logDownloadSummary("DOWNLOAD ABORTED", []summaryRow{
				{"Runtime", elapsedStr},
				{"Avg Speed", avgSpeed},
				{"Downloaded", lastSize},
			}, colWarning, colAbortedBorder)
			app.updateStatus("Status: Canceled.")
			app.setStatusIndicator(StatusCanceled)
		default:
			app.updateStatus("Status: Failed. Check output below.")
			app.setStatusIndicator(StatusFailed)
			app.sessionFailed.Store(true)
			if dl.Scan.hadExtractorErr {
				app.appendOutput(ytDlpUpdateHint, colInfo)
			}
			if app.ui.download.notify.Checked {
				fyne.CurrentApp().SendNotification(&fyne.Notification{
					Title:   "GoVid — Download Failed",
					Content: "The download encountered an error. Check the log for details.",
				})
			}
		}
	})
	<-uiDone
	app.statusThrottle.Flush()
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
