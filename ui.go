// ui.go — Thin DownloaderApp delegates plus shared UI helpers.
//
// Responsibilities:
//   - Thin delegates to UIManager for secondary windows (History, About,
//     Preferences, Post-Processing, GoVid Guide). The main window layout
//     itself (createUI) and the main menu bar live in ui_manager.go.
//   - roundedCard: shared card-style container helper used by UIManager.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// showHistory delegates to UIManager which owns the window state.
func (app *DownloaderApp) showHistory() {
	app.uiManager.showHistory()
}

// showPostProcessing delegates to UIManager which owns the window state.
func (app *DownloaderApp) showPostProcessing() {
	app.uiManager.showPostProcessing()
}

// showPreferences delegates to UIManager which owns the window state.
func (app *DownloaderApp) showPreferences() {
	app.uiManager.showPreferences()
}

// showConfigHelp delegates to UIManager which owns the window state.
func (app *DownloaderApp) showConfigHelp() {
	app.uiManager.showConfigHelp()
}

// showPostProcessingButton adds a button to the main UI to open the PP window.

func (app *DownloaderApp) getPostProcessingButton() *widget.Button {
	return widget.NewButtonWithIcon("Post-Processing", theme.SettingsIcon(), func() {
		app.showPostProcessing()
	})
}

// showAbout delegates to UIManager which owns the window state.
func (app *DownloaderApp) showAbout() {
	app.uiManager.showAbout()
}

// roundedCard wraps content in a rounded-rectangle background panel, giving
// cards a softer, more modern look than the default widget.Card. It renders
// a themed background with a subtle 1px border and 10px corner radius, then
// layers an optional italic subtitle and the provided content on top.
// Colors are sourced from the active theme so they work in both dark and light modes.
func roundedCard(subtitle string, content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	bg.CornerRadius = 10
	bg.StrokeColor = theme.Color(theme.ColorNameSeparator)
	bg.StrokeWidth = 1

	sub := widget.NewLabelWithStyle(subtitle, fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	inner := container.NewVBox(sub, content)
	return container.NewStack(bg, container.NewPadded(inner))
}
