// live_dialog.go — The prompt shown before recording a live or scheduled
// stream.
//
// UIManager.askLive offers Record from now / Record from the start / Skip
// for a stream that is live, and Wait and record / Skip for one that has
// not started. It is called from the session goroutine, which it blocks
// until the user answers or the session is cancelled.
package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// livePrompt is what the live prompt shows.
type livePrompt struct {
	title     string
	upcoming  bool          // the stream has not started yet
	startsIn  time.Duration // how long until an upcoming stream starts; 0 when unknown
	fromStart bool          // "Record from the start" is offered (YouTube, Twitch)
}

// liveDecision is the user's answer to the live prompt.
type liveDecision int

const (
	liveSkip liveDecision = iota
	liveRecordNow
	liveRecordFromStart
	liveWait
)

// askLive shows the live prompt and waits for the answer. Closing the
// prompt, or cancelling ctx, skips the stream. Call it off the UI thread.
func (manager *UIManager) askLive(ctx context.Context, prompt livePrompt) liveDecision {
	answer := make(chan liveDecision, 1)
	var shown dialog.Dialog // only touched on the UI thread
	fyne.Do(func() {
		shown = manager.showLiveDialog(prompt, func(decision liveDecision) {
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
		return liveSkip
	}
}

// startsInText describes when an upcoming stream starts, e.g. "Starts in 2 h
// 10 min".
func startsInText(startsIn time.Duration) string {
	if startsIn <= 0 {
		return "It has not started yet, and the site does not say when it will."
	}
	minutes := int(startsIn.Round(time.Minute).Minutes())
	switch {
	case minutes < 1:
		return "Starts in less than a minute."
	case minutes < 60:
		return fmt.Sprintf("Starts in %d min.", minutes)
	default:
		return fmt.Sprintf("Starts in %d h %d min.", minutes/60, minutes%60)
	}
}

// showLiveDialog builds and shows the live prompt. onAnswer is called
// exactly once, on the UI thread. Must be called on the UI thread.
func (manager *UIManager) showLiveDialog(prompt livePrompt, onAnswer func(liveDecision)) dialog.Dialog {
	var once sync.Once
	var dlg *dialog.CustomDialog
	answer := func(decision liveDecision) {
		once.Do(func() { onAnswer(decision) })
		dlg.Hide()
	}
	button := func(label string, decision liveDecision) *widget.Button {
		return widget.NewButton(label, func() { answer(decision) })
	}

	title := widget.NewLabelWithStyle(prompt.title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Wrapping = fyne.TextWrapWord
	var message string
	var buttons []fyne.CanvasObject
	if prompt.upcoming {
		message = startsInText(prompt.startsIn) + "\n\nGoVid can wait for it, checking every few minutes, and record it once it starts."
		wait := button("Wait and record", liveWait)
		wait.Importance = widget.HighImportance
		buttons = []fyne.CanvasObject{button("Skip", liveSkip), wait}
	} else {
		message = "This stream is live. It is recorded until you press Stop recording (or it ends); what was recorded is kept."
		now := button("Record from now", liveRecordNow)
		now.Importance = widget.HighImportance
		buttons = []fyne.CanvasObject{button("Skip", liveSkip)}
		if prompt.fromStart {
			buttons = append(buttons, button("Record from the start", liveRecordFromStart))
		}
		buttons = append(buttons, now)
	}
	body := widget.NewLabel(message)
	body.Wrapping = fyne.TextWrapWord

	heading := "Live stream"
	if prompt.upcoming {
		heading = "Scheduled stream"
	}
	dlg = dialog.NewCustomWithoutButtons(heading, container.NewVBox(title, body), manager.mainWindow)
	dlg.SetButtons(buttons)
	dlg.SetOnClosed(func() { once.Do(func() { onAnswer(liveSkip) }) })
	dlg.Resize(fyne.NewSize(480, 0))
	dlg.Show()
	return dlg
}
