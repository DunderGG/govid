// components_window.go — Tools → Components.
//
// Responsibilities:
//   - UIManager.showComponents: a small window with one row per tool
//     (yt-dlp, FFmpeg with ffprobe, Deno): the installed version and where
//     it was found (bin/ or PATH), the latest version, and one button,
//     Install, Update, or Reinstall (see componentStatus.action). The rows
//     are filled in off the UI thread, and again after each action.
//
// On platforms where GoVid does not install tools (see canInstallTools),
// the window lists the versions and says to use the package manager.
package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// componentsWindowWidth is the Components window's width.
const componentsWindowWidth = 640

// showComponents opens the Components window. It is a singleton: if it is
// already open, it is focused instead.
func (manager *UIManager) showComponents() {
	if focusOrCreate(&manager.compWindow) {
		return
	}

	intro := widget.NewLabel("GoVid keeps its tools in the bin folder beside it, where they take precedence over copies on PATH. " +
		"Every download is checked against the SHA-256 its source publishes: yt-dlp and Deno from their GitHub releases, " +
		"FFmpeg (the essentials build, with ffprobe) from gyan.dev.")
	intro.Wrapping = fyne.TextWrapWord
	note := widget.NewLabel("yt-dlp's Update runs \"yt-dlp -U\"; Reinstall downloads a fresh copy, which repairs a broken one.")
	if !canInstallTools() {
		note.SetText("These downloads are Windows builds. Install yt-dlp, FFmpeg, and Deno with your package manager.")
	}
	note.Wrapping = fyne.TextWrapWord
	note.TextStyle = fyne.TextStyle{Italic: true}

	rows := container.NewVBox(widget.NewLabel("Checking the installed tools and the latest releases…"))
	window := fyne.CurrentApp().NewWindow("Components")
	manager.compWindow = window
	window.SetContent(container.NewPadded(container.NewVBox(intro, widget.NewSeparator(), rows, widget.NewSeparator(), note)))
	window.Resize(fyne.NewSize(componentsWindowWidth, 0))
	window.SetOnClosed(onWindowClosed(&manager.compWindow))
	window.Show()
	manager.loadComponents(window, rows)
}

// loadComponents fills rows with the tools' status, found off the UI
// thread, unless window has been closed meanwhile.
func (manager *UIManager) loadComponents(window fyne.Window, rows *fyne.Container) {
	go func() {
		statuses := manager.onComponents()
		fyne.Do(func() {
			if manager.compWindow != window {
				return
			}
			manager.renderComponents(window, rows, statuses)
		})
	}()
}

// renderComponents lays out one row per tool. Must be called on the UI
// thread.
func (manager *UIManager) renderComponents(window fyne.Window, rows *fyne.Container, statuses []componentStatus) {
	bold := fyne.TextStyle{Bold: true}
	grid := container.NewGridWithColumns(4,
		widget.NewLabelWithStyle("Tool", fyne.TextAlignLeading, bold),
		widget.NewLabelWithStyle("Installed", fyne.TextAlignLeading, bold),
		widget.NewLabelWithStyle("Latest", fyne.TextAlignLeading, bold),
		widget.NewLabel(""),
	)
	var buttons []*widget.Button
	for _, status := range statuses {
		grid.Add(widget.NewLabel(status.label))
		grid.Add(widget.NewLabel(status.installedText()))
		grid.Add(widget.NewLabel(status.latestText()))
		if !canInstallTools() {
			grid.Add(widget.NewLabel(""))
			continue
		}
		button := widget.NewButton(status.action(), nil)
		button.OnTapped = func() {
			for _, other := range buttons {
				other.Disable()
			}
			button.SetText("Working…")
			manager.onComponentAction(status.name, status.action(), func(err error) {
				manager.componentActionDone(window, rows, err)
			})
		}
		buttons = append(buttons, button)
		grid.Add(button)
	}
	rows.Objects = []fyne.CanvasObject{grid}
	rows.Refresh()
}

// componentActionDone reports a failed action and checks the tools again,
// if the window is still open. Must be called on the UI thread.
func (manager *UIManager) componentActionDone(window fyne.Window, rows *fyne.Container, err error) {
	open := manager.compWindow == window
	if err != nil {
		parent := manager.mainWindow
		if open {
			parent = window
		}
		dialog.ShowError(fmt.Errorf("the tool was not changed: %w", err), parent)
	}
	if !open {
		return
	}
	rows.Objects = []fyne.CanvasObject{widget.NewLabel("Checking the installed tools and the latest releases…")}
	rows.Refresh()
	manager.loadComponents(window, rows)
}
