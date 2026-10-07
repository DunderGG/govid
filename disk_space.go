// disk_space.go — Checking for free disk space before each download.
//
// Responsibilities:
//   - checkDiskSpace: before each queued item, compares the size the probe
//     estimated (see MediaInfo.EstimatedSize) with the free space in the
//     save folder, and asks the user what to do when it looks too small.
//     Checking per item matters because earlier items of a batch use space.
//   - diskSpaceNeeded, trimFraction: the pure arithmetic behind the check.
//   - UIManager.askDiskSpace: the "Low disk space" prompt.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// diskSpaceMargin is how much more than the estimated size a download is
// expected to need, since estimates (especially approximate ones) run low.
const diskSpaceMargin = 1.1

// diskSpaceDecision is what to do about one queued item before downloading it.
type diskSpaceDecision int

const (
	// spaceProceed downloads the item.
	spaceProceed diskSpaceDecision = iota
	// spaceSkip moves on to the next item without downloading this one.
	spaceSkip
	// spaceStop stops the queue.
	spaceStop
	// spaceContinueAll downloads this item and every later one without
	// asking again this session.
	spaceContinueAll
)

// diskSpacePrompt is what the low-disk-space prompt shows.
type diskSpacePrompt struct {
	url    string
	needed uint64 // bytes the download is expected to need
	free   uint64 // bytes free in the save folder
	batch  bool   // more items follow, so Skip and "continue for all" apply
}

// diskSpaceNeeded returns the free space a download of estimate bytes needs:
// estimate plus diskSpaceMargin, doubled when post-processing will write a
// processed copy next to the original.
func diskSpaceNeeded(estimate uint64, postProcess bool) uint64 {
	needed := uint64(float64(estimate) * diskSpaceMargin)
	if postProcess {
		needed *= 2
	}
	return needed
}

// trimFraction returns the share of a video of the given duration that a trim
// range keeps, or 1 when there is no trim or it cannot be worked out.
func trimFraction(duration float64, trimStart, trimEnd string) float64 {
	if duration <= 0 || (trimStart == "" && trimEnd == "") {
		return 1
	}
	start, startErr := parseTimestamp(trimStart, 0)
	end, endErr := parseTimestamp(trimEnd, duration)
	if startErr != nil || endErr != nil || end <= start {
		return 1
	}
	return min((min(end, duration)-start)/duration, 1)
}

// parseTimestamp converts a trim timestamp (HH:MM:SS, MM:SS, or seconds, as
// validateTimestamp accepts) to seconds, returning fallback for "".
func parseTimestamp(timestamp string, fallback float64) (float64, error) {
	if timestamp == "" {
		return fallback, nil
	}
	var seconds float64
	for _, part := range strings.Split(timestamp, ":") {
		value, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid timestamp %q", timestamp)
		}
		seconds = seconds*60 + value
	}
	return seconds, nil
}

// existingDir returns path, or its nearest existing parent folder when path
// does not exist yet (yt-dlp creates the save folder), so its free space can
// be measured.
func existingDir(path string) string {
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

// checkDiskSpace decides whether to download item, given the size the probe
// estimated (scaled to req's trim range, and doubled when postProcess will
// write a second copy) and the free space in req's save folder. moreQueued says whether
// other items wait after it, so Skip and "continue for all" apply. With too
// little space it asks the user, unless they already chose to continue for
// the whole session (*continueAll). An unknown size, or free space that
// cannot be measured, skips the check, and the log says so.
func (app *DownloaderApp) checkDiskSpace(ctx context.Context, req DownloadRequest, postProcess bool, item queueItem, moreQueued bool, continueAll *bool) diskSpaceDecision {
	needed, known := downloadNeeds(item, req, postProcess)
	if !known {
		app.appendOutput("[SYSTEM] Download size unknown; skipping the disk space check.", colSystem)
		return spaceProceed
	}

	free, err := app.freeBytes(existingDir(req.SavePath))
	if err != nil {
		app.appendOutput(fmt.Sprintf("[SYSTEM] Could not check free disk space (%v); skipping the check.", err), colWarning)
		return spaceProceed
	}
	// Downloads already running will take some of it.
	free = uint64(max(int64(free)-app.reservedBytes.Load(), 0))
	if free >= needed {
		return spaceProceed
	}

	app.appendOutput(fmt.Sprintf("[SYSTEM] Low disk space: this download needs ~%s and %s is free.", formatBytes(int64(needed)), formatBytes(int64(free))), colWarning)
	if *continueAll {
		return spaceProceed
	}
	decision := app.askDiskSpace(ctx, diskSpacePrompt{
		url:    item.url,
		needed: needed,
		free:   free,
		batch:  moreQueued,
	})
	if decision == spaceContinueAll {
		*continueAll = true
		return spaceProceed
	}
	return decision
}

// askDiskSpace shows the low-disk-space prompt and waits for the answer.
// Closing the prompt, or cancelling ctx, stops the queue. Call it off the UI
// thread.
func (manager *UIManager) askDiskSpace(ctx context.Context, prompt diskSpacePrompt) diskSpaceDecision {
	answer := make(chan diskSpaceDecision, 1)
	var shown dialog.Dialog // only touched on the UI thread
	fyne.Do(func() {
		shown = manager.showDiskSpaceDialog(prompt, func(decision diskSpaceDecision) {
			answer <- decision
		})
	})

	select {
	case decision := <-answer:
		return decision
	case <-ctx.Done():
		fyne.Do(func() {
			if shown != nil {
				shown.Hide()
			}
		})
		return spaceStop
	}
}

// showDiskSpaceDialog builds and shows the low-disk-space prompt: "Continue
// anyway" or "Cancel" for a single download, and Skip / Continue / Stop in
// a batch. onAnswer is called exactly once, on the UI thread. Must be called
// on the UI thread.
func (manager *UIManager) showDiskSpaceDialog(prompt diskSpacePrompt, onAnswer func(diskSpaceDecision)) dialog.Dialog {
	var once sync.Once
	var dlg *dialog.CustomDialog
	answer := func(decision diskSpaceDecision) {
		once.Do(func() { onAnswer(decision) })
		dlg.Hide()
	}
	button := func(label string, decision diskSpaceDecision) *widget.Button {
		return widget.NewButton(label, func() { answer(decision) })
	}

	message := widget.NewLabel(fmt.Sprintf("Needs ~%s, %s free. Continue anyway?\n\n%s",
		formatBytes(int64(prompt.needed)), formatBytes(int64(prompt.free)), prompt.url))
	message.Wrapping = fyne.TextWrapWord

	var buttons []fyne.CanvasObject
	if prompt.batch {
		buttons = []fyne.CanvasObject{
			button("Stop", spaceStop),
			button("Skip", spaceSkip),
			button("Continue", spaceContinueAll),
		}
	} else {
		buttons = []fyne.CanvasObject{
			button("Cancel", spaceStop),
			button("Continue anyway", spaceProceed),
		}
	}

	dlg = dialog.NewCustomWithoutButtons("Low disk space", message, manager.mainWindow)
	dlg.SetButtons(buttons)
	dlg.SetOnClosed(func() { once.Do(func() { onAnswer(spaceStop) }) })
	dlg.Resize(fyne.NewSize(460, 0))
	dlg.Show()
	return dlg
}

// downloadNeeds returns the free space a download of item with req is
// expected to need (see diskSpaceNeeded), from the probe's size scaled to
// req's trim range, and whether that is known.
func downloadNeeds(item queueItem, req DownloadRequest, postProcess bool) (uint64, bool) {
	if item.info == nil {
		return 0, false
	}
	estimate, known := item.info.EstimatedSize()
	if !known {
		return 0, false
	}
	if item.info.Duration > 0 {
		estimate = uint64(float64(estimate) * trimFraction(item.info.Duration, req.TrimStart, req.TrimEnd))
	}
	return diskSpaceNeeded(estimate, postProcess), true
}
