// parallel.go — Downloading several queue items at once, and the per-item
// controls that need.
//
// Responsibilities:
//   - itemControls: the Skip and Pause of each item being downloaded, so
//     the Queue panel's row buttons act on their own item even when
//     several download at once (registerSkip, registerPause, skipItem,
//     pauseItem, requestPause).
//   - queueMode / runParallel: with "Simultaneous downloads" above 1, the
//     queue runs that many workers, each taking the next waiting item.
//     Workers start workerStagger apart, and after an HTTP 429 or a bot
//     check (backOff) only one goes on.
//   - itemRun: one item's place in the queue and its own size statistics;
//     its log lines get an "[n/total]" prefix and the main progress bar
//     shows the whole queue's progress.
//   - reserveSpace: the disk space a download in progress is expected to
//     need, which checkDiskSpace leaves for it.
package main

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// itemControls are the actions that stop one item being downloaded.
type itemControls struct {
	skip  func() // stops it and removes its partial files (for a recording: stops and keeps it)
	pause func() // stops it and keeps its partial files; nil while it cannot be paused
}

// control returns the controls of the item with id, creating them. Call
// it with cancelMu held.
func (app *DownloaderApp) control(id int) *itemControls {
	if app.controls == nil {
		app.controls = map[int]*itemControls{}
	}
	controls := app.controls[id]
	if controls == nil {
		controls = &itemControls{}
		app.controls[id] = controls
	}
	return controls
}

// registerSkip sets what Skip does for the item with id.
func (app *DownloaderApp) registerSkip(id int, skip func()) {
	app.cancelMu.Lock()
	app.control(id).skip = skip
	app.cancelMu.Unlock()
}

// registerPause sets what Pause does for the item with id, and enables the
// Pause button.
func (app *DownloaderApp) registerPause(id int, pause func()) {
	app.cancelMu.Lock()
	app.control(id).pause = pause
	app.cancelMu.Unlock()
	app.refreshPauseControl()
}

// unregister forgets the controls of the item with id, once it is no
// longer downloading.
func (app *DownloaderApp) unregister(id int) {
	app.cancelMu.Lock()
	delete(app.controls, id)
	app.cancelMu.Unlock()
	app.refreshPauseControl()
}

// takePauses removes and returns the pause functions of the items ids
// names, or of every item when ids is empty.
func (app *DownloaderApp) takePauses(ids ...int) []func() {
	app.cancelMu.Lock()
	defer app.cancelMu.Unlock()
	var pauses []func()
	for id, controls := range app.controls {
		if controls.pause == nil || (len(ids) > 0 && id != ids[0]) {
			continue
		}
		pauses = append(pauses, controls.pause)
		controls.pause = nil
	}
	return pauses
}

// requestPause pauses every download that can be paused, and reports
// whether there was one.
func (app *DownloaderApp) requestPause() bool {
	pauses := app.takePauses()
	for _, pause := range pauses {
		pause()
	}
	return len(pauses) > 0
}

// pauseItem pauses the item with id (the Queue panel's Pause).
func (app *DownloaderApp) pauseItem(id int) {
	for _, pause := range app.takePauses(id) {
		pause()
	}
}

// skipItem skips the item with id (the Queue panel's Skip): it stops the
// download and removes its partial files, or stops a recording and keeps
// it. An item without its own Skip falls back to the Cancel button's.
func (app *DownloaderApp) skipItem(id int) bool {
	app.cancelMu.Lock()
	var skip func()
	if controls := app.controls[id]; controls != nil {
		skip, controls.skip = controls.skip, nil
	}
	app.cancelMu.Unlock()
	if skip == nil {
		return app.RequestCancel()
	}
	skip()
	return true
}

// refreshPauseControl enables the Pause button while a download can be
// paused, except while the queue waits for a resume (then it reads
// Resume; see waitForResume).
func (app *DownloaderApp) refreshPauseControl() {
	if app.awaitingResume.Load() {
		return
	}
	app.cancelMu.Lock()
	pausable := false
	for _, controls := range app.controls {
		pausable = pausable || controls.pause != nil
	}
	app.cancelMu.Unlock()
	app.setPauseControl("Pause", pausable)
}

// ── Running several at once ──────────────────────────────────────────────────

// workerStagger is how long apart the workers of simultaneous downloads
// start, so the site does not see a burst of requests; a variable so tests
// can shorten it.
var workerStagger = 3 * time.Second

// simultaneousDownloads parses the Simultaneous Downloads setting: 1 to 3,
// 1 for anything else.
func simultaneousDownloads(setting string) int {
	n, err := strconv.Atoi(setting)
	if err != nil || n < 1 || n > 3 {
		return 1
	}
	return n
}

// queueMode is how a session's queue runs.
type queueMode struct {
	batch    bool          // the queue started with several items: each has its own Skip
	parallel bool          // several items may download at once
	limit    *atomic.Int32 // with parallel, how many workers may still take items
}

// itemRun is one queue item's download: where it is in the queue, whether
// others download at the same time, and its own statistics.
type itemRun struct {
	id              int
	position, total int
	parallel        bool
	stats           *DownloadStats // the item's sizes; app.stats when it downloads alone
}

// prefix returns the "[2/5] " that marks the item's log lines when several
// download at once, or "".
func (run itemRun) prefix() string {
	if !run.parallel {
		return ""
	}
	return fmt.Sprintf("[%d/%d] ", run.position, run.total)
}

// runParallel downloads the queue with workers workers, each taking the
// next waiting item until none waits, and returns the finalized paths of
// every successful download. A worker that finds only paused items left,
// with nothing else downloading, waits for one to be resumed.
func (app *DownloaderApp) runParallel(queueCtx context.Context, session downloadSession, queue *QueueModel, workers int) []string {
	mode := queueMode{batch: true, parallel: true, limit: &atomic.Int32{}}
	mode.limit.Store(int32(workers))
	app.appendOutput(fmt.Sprintf("[SYSTEM] Downloading up to %d videos at a time.", workers), colInfo)

	var (
		mu               sync.Mutex
		finalPaths       []string
		stopped          atomic.Bool
		continueLowSpace bool // guarded by promptMu, like every disk space prompt
		wg               sync.WaitGroup
	)
	for worker := range workers {
		wg.Go(func() {
			name := fmt.Sprintf("download worker %d", worker+1)
			markLoop(name, "started")
			defer markLoop(name, "stopped")
			if !sleepCtx(queueCtx, time.Duration(worker)*workerStagger) {
				return
			}
			for queueCtx.Err() == nil && !stopped.Load() && int32(worker) < mode.limit.Load() {
				id, item, ok := queue.Next()
				if !ok {
					if queue.HasPaused() && queue.ActiveCount() == 0 {
						app.waitForResume(queueCtx, queue)
						continue
					}
					return
				}
				paths, stop := app.downloadItem(queueCtx, session, queue, id, item, mode, &continueLowSpace)
				mu.Lock()
				finalPaths = append(finalPaths, paths...)
				mu.Unlock()
				if stop {
					stopped.Store(true)
				}
				app.showParallelProgress(queue)
			}
		})
	}
	wg.Wait()

	// The last item's own result was not shown (see reportDownloadResult).
	if queueCtx.Err() != nil {
		app.updateStatus("Status: Canceled.")
		app.setStatusIndicator(StatusCanceled)
		return finalPaths
	}
	app.setProgressNow(queue.OverallProgress())
	app.updateStatus("Status: " + queue.Summary() + ".")
	if app.sessionFailed.Load() {
		app.setStatusIndicator(StatusFailed)
	} else {
		app.setStatusIndicator(StatusSuccess)
	}
	return finalPaths
}

// sleepCtx waits for d, and reports false if ctx ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// showParallelProgress shows the whole queue's progress in the progress
// bar and how many videos are downloading in the status label.
func (app *DownloaderApp) showParallelProgress(queue *QueueModel) {
	app.setProgress(queue.OverallProgress())
	switch active := queue.ActiveCount(); active {
	case 0:
	case 1:
		app.updateStatus("Status: Downloading 1 video…")
	default:
		app.updateStatus(fmt.Sprintf("Status: Downloading %d videos…", active))
	}
}

// backOff lets only one worker go on taking items, when the site rate
// limits (HTTP 429) or asks for a bot check, which more requests at once
// make worse. It logs the first time.
func (app *DownloaderApp) backOff(mode queueMode, scan scanResult) {
	if !mode.parallel || mode.limit == nil {
		return
	}
	var reason string
	switch {
	case scan.hadRateLimit:
		reason = "The site answered HTTP 429 (too many requests)"
	case scan.accessProblem == accessBotCheck:
		reason = "YouTube asked to confirm you are not a bot"
	default:
		return
	}
	if mode.limit.Swap(1) > 1 {
		app.appendOutput("[SYSTEM] "+reason+"; downloading one video at a time for the rest of this session.", colWarning)
	}
}

// reserveSpace sets aside needed bytes for a download in progress, so
// checkDiskSpace leaves them for it, and returns the function that gives
// them back once it has finished.
func (app *DownloaderApp) reserveSpace(needed uint64) (release func()) {
	app.reservedBytes.Add(int64(needed))
	return func() { app.reservedBytes.Add(-int64(needed)) }
}
