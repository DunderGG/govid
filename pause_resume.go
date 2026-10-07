// pause_resume.go — Pausing and resuming downloads, and resuming the queue
// after a restart.
//
// Responsibilities:
//   - queueItem.withRequest / downloadRequest: each item downloads with the
//     settings it was queued with (or saved with), under its own download
//     ID, so a retry or a resume continues its partial files.
//   - downloadContext: the context one download runs under. Pause stops it
//     with errPaused (its partial files are kept); Cancel or Skip stops it
//     plainly (they are removed); quitting pauses it.
//   - pauseOrResume / waitForResume: the Pause / Resume button, and the
//     queue waiting while only paused items are left.
//   - UIManager.askRestoreQueue: the "Resume downloads?" prompt.
//   - finishQueue: when the session ends, paused items are discarded
//     (their partial files removed), unless GoVid is quitting: then the
//     waiting and paused items are saved to queue.json (QueueStore).
//   - offerQueueRestore / resumeQueue: "Resume 7 queued downloads?" at the
//     next start.
package main

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// withRequest returns the item with the settings it downloads with: a copy
// of template (the session's settings) for an item queued now, or its own
// saved settings, with template's cookies, for one restored from
// queue.json.
func (item queueItem) withRequest(template DownloadRequest) queueItem {
	req := template
	if item.request != nil {
		req = *item.request
		req.CookiesPath, req.CookiesFromBrowser = template.CookiesPath, template.CookiesFromBrowser
	}
	item.request = &req
	return item
}

// downloadRequest returns the request to probe or download the item with.
func (item queueItem) downloadRequest() DownloadRequest {
	var req DownloadRequest
	if item.request != nil {
		req = *item.request
	}
	req.URL, req.DownloadID = item.url, item.downloadID
	req.FormatPick = item.formatPick
	return req
}

// downloadContext returns the context one download runs under, a function
// that stops it with a cause (errPaused to pause, errStopKeep to stop and
// keep a recording), and release, to call once it has finished. When
// parent ends (the item is skipped or the session stopped), the download
// stops too: a live recording is kept, a download is paused when GoVid is
// quitting (so it can resume at the next start), and otherwise cancelled,
// which removes its partial files.
func (app *DownloaderApp) downloadContext(parent context.Context, live bool) (ctx context.Context, stop context.CancelCauseFunc, release func()) {
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	unhook := context.AfterFunc(parent, func() {
		switch {
		case live:
			cancel(errStopKeep)
		case app.quitting.Load():
			cancel(errPaused)
		default:
			cancel(context.Canceled)
		}
	})
	return ctx, cancel, func() {
		unhook()
		cancel(nil)
	}
}

// pauseOrResume is the main window's Pause / Resume button: it pauses the
// running download, or, when the queue only has paused items left, resumes
// them all.
func (app *DownloaderApp) pauseOrResume() {
	if app.requestPause() {
		return
	}
	if queue := app.queue.Load(); queue != nil && app.awaitingResume.Load() {
		queue.ResumeAll()
	}
}

// pauseControl is the state of the Pause / Resume button.
type pauseControl struct {
	label   string
	enabled bool
}

// setPauseControl sets the Pause / Resume button's label and whether it can
// be pressed. It is safe to call from any goroutine: the change goes
// through pauseThrottle, so updates from several downloads at once are
// applied one at a time, newest last.
func (app *DownloaderApp) setPauseControl(label string, enabled bool) {
	app.pauseThrottle.Set(pauseControl{label: label, enabled: enabled})
}

// showPauseControl is pauseThrottle's apply function.
func (app *DownloaderApp) showPauseControl(state pauseControl) {
	fyne.Do(func() {
		button := app.ui.download.pauseBtn
		button.SetText(state.label)
		if state.enabled {
			button.Enable()
		} else {
			button.Disable()
		}
	})
}

// waitForResume waits, while the queue has only paused items left, until
// one is resumed or the session is stopped. Meanwhile Resume resumes them
// all and Cancel stops the session, which discards them.
func (app *DownloaderApp) waitForResume(queueCtx context.Context, queue *QueueModel) {
	app.updateStatus("Status: Paused. Press Resume to continue, or Cancel to discard the partial downloads.")
	app.setStatusIndicator(StatusCanceled)
	app.setPauseControl("Resume", true)
	app.SetCancelFunc(app.StopSession)
	fyne.Do(func() { app.ui.download.cancelBtn.Enable() })
	app.awaitingResume.Store(true)
	select {
	case <-queueCtx.Done():
	case <-queue.Changed():
	}
	app.awaitingResume.Store(false)
	app.setPauseControl("Pause", false)
}

// finishQueue settles what is left in the queue when a session ends. When
// GoVid is quitting, the waiting and paused items are saved to queue.json
// for the next start; otherwise waiting items are marked skipped and
// paused ones discarded, their partial files removed.
func (app *DownloaderApp) finishQueue(queue *QueueModel) {
	if app.quitting.Load() {
		app.saveQueue(queue)
		return
	}
	queue.MarkAll(queueWaiting, queueSkipped)
	engine := app.newDownloadEngine()
	for _, entry := range queue.Snapshot() {
		if entry.status != queuePaused {
			continue
		}
		req := entry.item.downloadRequest()
		engine.RemovePartialFiles(req.SavePath, req.DownloadID, app.appendOutput)
		queue.SetStatus(entry.id, queueSkipped)
	}
}

// saveQueue saves the queue's waiting and paused items to queue.json, so
// the next start can resume them, and logs how many.
func (app *DownloaderApp) saveQueue(queue *QueueModel) {
	var items []savedQueueItem
	for _, entry := range queue.Snapshot() {
		switch entry.status {
		case queueWaiting, queueChecking:
			items = append(items, saveItem(entry.item, savedWaiting))
		case queuePaused, queueDownloading:
			items = append(items, saveItem(entry.item, savedPaused))
		}
	}
	if err := app.queueStore.Save(items); err != nil {
		app.logSvc.WriteToFile(fmt.Sprintf("[SYSTEM] Could not save the queue: %v", err))
		return
	}
	if len(items) > 0 {
		app.logSvc.WriteToFile(fmt.Sprintf("[SYSTEM] Saved %d queued download(s) to resume at the next start.", len(items)))
	}
}

// discardPausedItem removes a paused item from the queue and deletes its
// partial files (the Queue panel's Remove on a paused row).
func (app *DownloaderApp) discardPausedItem(id int) {
	queue := app.queue.Load()
	if queue == nil {
		return
	}
	item, ok := queue.RemovePaused(id)
	if !ok {
		return
	}
	req := item.downloadRequest()
	go app.newDownloadEngine().RemovePartialFiles(req.SavePath, req.DownloadID, app.appendOutput)
}

// offerQueueRestore asks, when queue.json holds items from the last run,
// whether to resume them: Resume starts a session with them, and Discard
// deletes their partial files. Must be called on the UI thread.
func (app *DownloaderApp) offerQueueRestore() {
	saved, err := app.queueStore.Load()
	if err != nil {
		app.appendOutput(fmt.Sprintf("[SYSTEM] Could not read the saved queue (%v); it was discarded.", err), colWarning)
		app.queueStore.Clear()
		return
	}
	if len(saved) == 0 {
		return
	}
	app.askRestoreQueue(len(saved), func(resume bool) {
		app.queueStore.Clear()
		if resume {
			app.resumeQueue(saved)
			return
		}
		go app.discardSaved(saved)
	})
}

// discardSaved deletes the partial files of saved items the user chose not
// to resume.
func (app *DownloaderApp) discardSaved(saved []savedQueueItem) {
	engine := app.newDownloadEngine()
	for _, item := range saved {
		engine.RemovePartialFiles(item.Request.SavePath, item.DownloadID, app.appendOutput)
	}
	app.appendOutput(fmt.Sprintf("[SYSTEM] Discarded %d saved download(s).", len(saved)), colSystem)
}

// resumeQueue starts a session with the saved items, each with its own
// settings and download ID, so yt-dlp continues their partial files. They
// are probed again first. Must be called on the UI thread.
func (app *DownloaderApp) resumeQueue(saved []savedQueueItem) {
	if app.isRunning.Load() || app.installing.Load() {
		dialog.ShowError(fmt.Errorf("finish the running job first; the saved downloads were kept for the next start"), app.window)
		app.saveSavedAgain(saved)
		return
	}
	session := downloadSession{savePath: saved[0].Request.SavePath, restored: true}
	for _, item := range saved {
		session.urls = append(session.urls, item.URL)
		session.items = append(session.items, item.queueItem())
	}
	session.request = app.newDownloadRequest("", session.savePath, "", "")
	session.workers = simultaneousDownloads(app.ui.prefs.simultaneous.Selected)
	if app.ui.postProcess.enablePostProcess.Checked {
		session.vfFilters, session.afFilters = buildPostProcessFilters(newPostProcessSettings(app.ui))
	}
	app.appendOutput(fmt.Sprintf("[SYSTEM] Resuming %d saved download(s).", len(saved)), colInfo)
	app.startSession(session)
}

// saveSavedAgain puts saved items back in queue.json.
func (app *DownloaderApp) saveSavedAgain(saved []savedQueueItem) {
	if err := app.queueStore.Save(saved); err != nil {
		app.logSvc.WriteToFile(fmt.Sprintf("[SYSTEM] Could not save the queue: %v", err))
	}
}

// askRestoreQueue asks whether to resume the count downloads GoVid saved
// when it last quit, and calls answer on the UI thread with the choice:
// Resume, or Discard, which deletes their partial files. Must be called on
// the UI thread.
func (manager *UIManager) askRestoreQueue(count int, answer func(resume bool)) {
	confirm := dialog.NewConfirm("Resume Downloads",
		fmt.Sprintf("GoVid was closed with %d download(s) still queued or paused. Resume them now?\n\nThey continue where they stopped. Discard deletes their partly downloaded files.", count),
		answer, manager.mainWindow)
	confirm.SetConfirmText("Resume")
	confirm.SetDismissText("Discard")
	confirm.Show()
}
