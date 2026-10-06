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

	var dlg *dialog.CustomDialog
	downloadBtn := widget.NewButton("Download", func() {
		positions, err := parsePlaylistSelection(rangeEntry.Text, count)
		if err != nil {
			rangeEntry.SetValidationError(err)
			return
		}
		answer(playlistDecision{positions: positions})
		dlg.Hide()
	})
	cancelBtn := widget.NewButton("Cancel", func() {
		dlg.Hide()
	})
	buttons := []fyne.CanvasObject{cancelBtn}

	// For watch?v=…&list=… links the user most likely meant the one video,
	// so that is the highlighted choice.
	if prompt.hasVideo {
		onlyBtn := widget.NewButton("Only this video", func() {
			answer(playlistDecision{onlyVideo: true})
			dlg.Hide()
		})
		onlyBtn.Importance = widget.HighImportance
		buttons = append(buttons, downloadBtn, onlyBtn)
	} else {
		downloadBtn.Importance = widget.HighImportance
		buttons = append(buttons, downloadBtn)
	}

	content := container.NewVBox(
		title,
		details,
		widget.NewLabel("Videos to download:"),
		rangeEntry,
	)
	dlg = dialog.NewCustomWithoutButtons("Playlist detected", content, manager.mainWindow)
	dlg.SetButtons(buttons)
	dlg.SetOnClosed(func() { answer(playlistDecision{}) })
	dlg.Resize(fyne.NewSize(480, 0))
	dlg.Show()
	return dlg
}
