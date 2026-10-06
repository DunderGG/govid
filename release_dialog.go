// release_dialog.go — Telling the user about a newer GoVid release.
//
// Responsibilities:
//   - checkForGoVidUpdates: the Tools → "Check for GoVid updates" action.
//   - showGoVidRelease: a dialog with a release's notes, a button that opens
//     its download page, and, where GoVid can replace itself (see
//     self_update.go), an "Update now" button.
//   - showUpdateFailure: why an "Update now" failed.
package main

import (
	"errors"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// checkForGoVidUpdates asks GitHub for the latest GoVid release, off the UI
// thread, and shows the answer: the release notes when a newer release
// exists, otherwise a short message.
func (manager *UIManager) checkForGoVidUpdates() {
	go func() {
		release, newer, err := manager.onCheckGoVidRelease()
		fyne.Do(func() {
			switch {
			case errors.Is(err, errReleaseUnknown):
				dialog.ShowInformation("Check for GoVid Updates",
					"GitHub did not say which release is the latest (it limits how often it can be asked). Please try again later.",
					manager.mainWindow)
			case err != nil:
				dialog.ShowError(fmt.Errorf("could not check for GoVid updates: %w", err), manager.mainWindow)
			case newer:
				manager.showGoVidRelease(release)
			case version == devVersion:
				dialog.ShowInformation("Check for GoVid Updates",
					fmt.Sprintf("This is a development build, so it is not compared with releases. The latest release is %s.", release.TagName),
					manager.mainWindow)
			default:
				dialog.ShowInformation("Check for GoVid Updates",
					fmt.Sprintf("GoVid %s is the latest version.", version),
					manager.mainWindow)
			}
		})
	}()
}

// showGoVidRelease shows a release's notes, with a button that opens its
// download page in the browser. Must be called on the UI thread.
func (manager *UIManager) showGoVidRelease(release Release) {
	notes := release.Body
	if notes == "" {
		notes = "_This release has no release notes._"
	}
	body := widget.NewRichTextFromMarkdown(notes)
	body.Wrapping = fyne.TextWrapWord
	scroll := container.NewVScroll(body)
	scroll.SetMinSize(fyne.NewSize(480, 280))

	heading := widget.NewLabel(fmt.Sprintf("GoVid %s is available. You have %s.", release.TagName, version))
	content := container.NewBorder(heading, nil, nil, nil, scroll)

	var dlg *dialog.CustomDialog
	openBtn := widget.NewButton("Open download page", func() {
		if err := fyne.CurrentApp().OpenURL(parseURL(release.HTMLURL)); err != nil {
			dialog.ShowError(fmt.Errorf("could not open %s: %w", release.HTMLURL, err), manager.mainWindow)
		}
		dlg.Hide()
	})
	openBtn.Importance = widget.HighImportance
	closeBtn := widget.NewButton("Close", func() { dlg.Hide() })
	buttons := []fyne.CanvasObject{closeBtn, openBtn}

	// "Update now" replaces GoVid in place; it is offered only when that
	// can work (see DownloaderApp.canSelfUpdate).
	if manager.onCanSelfUpdate(release) {
		openBtn.Importance = widget.MediumImportance
		updateBtn := widget.NewButton("Update now", func() {
			dlg.Hide()
			manager.onSelfUpdate(release)
		})
		updateBtn.Importance = widget.HighImportance
		buttons = append(buttons, updateBtn)
	}

	dlg = dialog.NewCustomWithoutButtons("GoVid Update", content, manager.mainWindow)
	dlg.SetButtons(buttons)
	dlg.Show()
}

// showUpdateFailure explains why updating GoVid in place failed and offers
// the download page instead. Must be called on the UI thread.
func (manager *UIManager) showUpdateFailure(err error) {
	message := widget.NewLabel(fmt.Sprintf("GoVid could not update itself:\n\n%v\n\nThe installed GoVid has not been changed. You can download the new version from the release page instead.", err))
	message.Wrapping = fyne.TextWrapWord
	dlg := dialog.NewCustom("GoVid Update Failed", "OK", message, manager.mainWindow)
	dlg.Resize(fyne.NewSize(480, 0))
	dlg.Show()
}
