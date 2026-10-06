// duplicates.go — Warning before downloading a video again.
//
// Responsibilities:
//   - skipDownloaded: after the session's URLs are checked, finds queued
//     videos the download history already has (see findDownloaded) and asks
//     whether to download each again.
//   - UIManager.askDuplicate: the "Already downloaded" prompt.
package main

import (
	"context"
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// duplicateDecision is what to do about a queued video that was downloaded
// before.
type duplicateDecision int

const (
	// duplicateDownload downloads the video again.
	duplicateDownload duplicateDecision = iota
	// duplicateSkip leaves the video out of the queue.
	duplicateSkip
	// duplicateSkipAll leaves this video, and every later one that was
	// downloaded before, out of the queue without asking again.
	duplicateSkipAll
)

// duplicatePrompt is what the "Already downloaded" prompt shows.
type duplicatePrompt struct {
	title    string // the queued video's title, or its URL
	previous DownloadHistoryEntry
	batch    bool // more videos are queued, so "Skip all duplicates" applies
}

// skipDownloaded returns items without the videos the user chose not to
// download again. The history is read once; a video counts as downloaded
// before when findDownloaded finds it. The check is skipped when "Keep
// download history" is off, and when the history cannot be read (which is
// logged). It returns nil when ctx is cancelled.
func (app *DownloaderApp) skipDownloaded(ctx context.Context, items []queueItem) []queueItem {
	if !app.keepHistory.Load() || len(items) == 0 {
		return items
	}
	history, err := app.historySvc.Load()
	if err != nil {
		app.appendOutput(fmt.Sprintf("[SYSTEM] Could not read the download history (%v); not checking for repeat downloads.", err), colWarning)
		return items
	}

	var kept []queueItem
	skipAll := false
	for _, item := range items {
		previous, found := findDownloaded(history, item.url, item.videoID, item.extractor)
		if !found {
			kept = append(kept, item)
			continue
		}
		if !skipAll {
			decision := app.askDuplicate(ctx, duplicatePrompt{title: itemName(item), previous: previous, batch: len(items) > 1})
			if ctx.Err() != nil {
				return nil
			}
			if decision == duplicateDownload {
				kept = append(kept, item)
				continue
			}
			skipAll = decision == duplicateSkipAll
		}
		app.appendOutput(fmt.Sprintf("[SYSTEM] Skipped %s: already downloaded on %s as %s.", itemName(item), previous.DownloadedAt, previous.DisplayFile()), colInfo)
	}
	return kept
}

// itemName names a queued video for the user: its title, or its URL.
func itemName(item queueItem) string {
	if item.title != "" {
		return fmt.Sprintf("%q", item.title)
	}
	return item.url
}

// askDuplicate shows the "Already downloaded" prompt and waits for the
// answer. Closing the prompt skips the video; cancelling ctx hides it. Call
// it off the UI thread.
func (manager *UIManager) askDuplicate(ctx context.Context, prompt duplicatePrompt) duplicateDecision {
	answer := make(chan duplicateDecision, 1)
	var shown dialog.Dialog // only touched on the UI thread
	fyne.Do(func() {
		shown = manager.showDuplicateDialog(prompt, func(decision duplicateDecision) {
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
		return duplicateSkip
	}
}

// showDuplicateDialog builds and shows the "Already downloaded" prompt:
// Download again / Skip, plus "Skip all duplicates" in a batch. onAnswer is
// called exactly once, on the UI thread. Must be called on the UI thread.
func (manager *UIManager) showDuplicateDialog(prompt duplicatePrompt, onAnswer func(duplicateDecision)) dialog.Dialog {
	var once sync.Once
	var dlg *dialog.CustomDialog
	answer := func(decision duplicateDecision) {
		once.Do(func() { onAnswer(decision) })
		dlg.Hide()
	}
	button := func(label string, decision duplicateDecision) *widget.Button {
		return widget.NewButton(label, func() { answer(decision) })
	}

	message := widget.NewLabel(fmt.Sprintf("%s\n\nAlready downloaded on %s as %s.",
		prompt.title, prompt.previous.DownloadedAt, prompt.previous.DisplayFile()))
	message.Wrapping = fyne.TextWrapWord

	again := button("Download again", duplicateDownload)
	buttons := []fyne.CanvasObject{button("Skip", duplicateSkip), again}
	if prompt.batch {
		buttons = append([]fyne.CanvasObject{button("Skip all duplicates", duplicateSkipAll)}, buttons...)
	}

	dlg = dialog.NewCustomWithoutButtons("Already downloaded", message, manager.mainWindow)
	dlg.SetButtons(buttons)
	dlg.SetOnClosed(func() { once.Do(func() { onAnswer(duplicateSkip) }) })
	dlg.Resize(fyne.NewSize(460, 0))
	dlg.Show()
	return dlg
}
