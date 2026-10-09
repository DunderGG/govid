// notices.go — Non-blocking notices above the main window's input card.
//
// Responsibilities:
//   - notice: a message such as "a newer yt-dlp is available", with an
//     optional action button.
//   - showNotice, dismissNotice: safe from any goroutine; notices survive
//     createUI rebuilding the window.
package main

import (
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// notice is a message shown in a bar above the main window's input card
// until the user acts on it or dismisses it, such as "a newer yt-dlp is
// available". Unlike a dialog it does not block the window.
type notice struct {
	id          string // a notice replaces any shown notice with the same id
	text        string
	actionLabel string // label of the action button; "" for no button
	action      func() // run on the UI thread when the action button is tapped
}

// showNotice shows n in the notice area, replacing any notice with the same
// id. It is safe to call from any goroutine. Notices survive createUI
// rebuilding the window.
func (manager *UIManager) showNotice(n notice) {
	fyne.Do(func() {
		manager.notices = slices.DeleteFunc(manager.notices, func(shown notice) bool { return shown.id == n.id })
		manager.notices = append(manager.notices, n)
		manager.renderNotices()
	})
}

// dismissNotice removes the notice with the given id, if shown. It is safe
// to call from any goroutine.
func (manager *UIManager) dismissNotice(id string) {
	fyne.Do(func() {
		manager.notices = slices.DeleteFunc(manager.notices, func(shown notice) bool { return shown.id == id })
		manager.renderNotices()
	})
}

// renderNotices rebuilds the notice area from manager.notices. Must be
// called on the UI thread.
func (manager *UIManager) renderNotices() {
	if manager.noticeBox == nil {
		return
	}
	manager.noticeBox.Objects = nil
	for _, n := range manager.notices {
		manager.noticeBox.Add(manager.buildNotice(n))
	}
	manager.noticeBox.Refresh()
}

// buildNotice lays out one notice: its text, its action button (which also
// dismisses it), and a dismiss button.
func (manager *UIManager) buildNotice(n notice) fyne.CanvasObject {
	text := widget.NewLabel(n.text)
	text.Wrapping = fyne.TextWrapWord

	buttons := container.NewHBox()
	if n.actionLabel != "" {
		actionBtn := widget.NewButton(n.actionLabel, func() {
			manager.dismissNotice(n.id)
			n.action()
		})
		actionBtn.Importance = widget.HighImportance
		buttons.Add(actionBtn)
	}
	buttons.Add(widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		manager.dismissNotice(n.id)
	}))

	card := roundedCard("", container.NewBorder(nil, nil, nil, container.NewCenter(buttons), text))
	return container.NewBorder(nil, nil, accentBar(), nil, card)
}
