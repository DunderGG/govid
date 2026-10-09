// playlist_dialog.go — The prompt shown when a URL turns out to be a playlist.
//
// UIManager.askPlaylist shows the playlist's title, length, and video count,
// lets the user pick a range of videos, and waits for the answer. It is
// called from the session goroutine, which it blocks until the user answers
// or the session is cancelled.
package main

import (
	"context"
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// askPlaylist shows the playlist prompt and waits for the answer. Closing
// the prompt, or cancelling ctx, skips the playlist. Call it off the UI
// thread.
func (manager *UIManager) askPlaylist(ctx context.Context, prompt playlistPrompt) playlistDecision {
	answer := make(chan playlistDecision, 1)
	var shown dialog.Dialog // only touched on the UI thread
	fyne.Do(func() {
		shown = manager.showPlaylistDialog(prompt, func(decision playlistDecision) {
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
		return playlistDecision{}
	}
}

// showPlaylistDialog builds and shows the playlist prompt. onAnswer is called
// exactly once, on the UI thread: with the chosen positions for Download,
// with onlyVideo for "Only this video", or with an empty decision when the
// prompt is cancelled or closed. Must be called on the UI thread.
func (manager *UIManager) showPlaylistDialog(prompt playlistPrompt, onAnswer func(playlistDecision)) dialog.Dialog {
	var once sync.Once
	answer := func(decision playlistDecision) {
		once.Do(func() { onAnswer(decision) })
	}
	count := len(prompt.entries)

	title := widget.NewLabelWithStyle(prompt.title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord
	details := widget.NewLabel(fmt.Sprintf("%d videos · Total length: %s · Total size: unknown",
		count, playlistDuration(prompt.entries)))
	details.Wrapping = fyne.TextWrapWord

	rangeEntry := widget.NewEntry()
	rangeEntry.SetPlaceHolder(fmt.Sprintf("e.g. 1-10 or 3,5,8 (blank downloads all %d)", count))
	rangeEntry.Validator = func(text string) error {
		_, err := parsePlaylistSelection(text, count)
		return err
	}

	content := container.NewVBox(
		title,
		details,
		widget.NewLabel("Videos to download:"),
		rangeEntry,
	)
	dlg := dialog.NewCustomWithoutButtons("Playlist detected", content, manager.mainWindow)
	dlg.SetButtons(playlistButtons(prompt.hasVideo, rangeEntry, count, answer, dlg.Hide))
	dlg.SetOnClosed(func() { answer(playlistDecision{}) })
	dlg.Resize(fyne.NewSize(480, 0))
	dlg.Show()
	return dlg
}

// playlistButtons returns the playlist prompt's buttons. Cancel only hides
// the prompt (hide), which answers that nothing is chosen. Download answers
// with the positions rangeEntry selects out of count, or shows on the entry
// why the selection does not parse. For a link to one video of the
// playlist (hasVideo), "Only this video" is added and highlighted.
func playlistButtons(hasVideo bool, rangeEntry *widget.Entry, count int, answer func(playlistDecision), hide func()) []fyne.CanvasObject {
	downloadBtn := widget.NewButton("Download", func() {
		positions, err := parsePlaylistSelection(rangeEntry.Text, count)
		if err != nil {
			rangeEntry.SetValidationError(err)
			return
		}
		answer(playlistDecision{positions: positions})
		hide()
	})
	cancelBtn := widget.NewButton("Cancel", hide)
	buttons := []fyne.CanvasObject{cancelBtn}

	// For watch?v=…&list=… links the user most likely meant the one video,
	// so that is the highlighted choice.
	if hasVideo {
		onlyBtn := widget.NewButton("Only this video", func() {
			answer(playlistDecision{onlyVideo: true})
			hide()
		})
		onlyBtn.Importance = widget.HighImportance
		return append(buttons, downloadBtn, onlyBtn)
	}
	downloadBtn.Importance = widget.HighImportance
	return append(buttons, downloadBtn)
}
