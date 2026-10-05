// ui.go — Shared UI helpers.
//
// Responsibilities:
//   - clearTerminalOutput: the DownloaderApp delegate download.go uses to
//     reset the log view. The main window layout (createUI), the main menu
//     bar, and every secondary window live in ui_manager.go.
//   - Shared layout helpers used by UIManager: roundedCard, accentBar,
//     sectionHeader, sectionDivider, and fixedWidth.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// clearTerminalOutput delegates to UIManager which owns the log container.
func (app *DownloaderApp) clearTerminalOutput() {
	app.uiManager.clearTerminalOutput()
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

// accentBar returns a 4px wide rectangle in the theme's primary colour, used
// as a decorative left-edge bar on cards.
func accentBar() *canvas.Rectangle {
	bar := canvas.NewRectangle(accentCyan)
	bar.SetMinSize(fyne.NewSize(4, 0))
	return bar
}

// sectionHeader creates a small bold accent-coloured title for a section of
// a form.
func sectionHeader(text string) fyne.CanvasObject {
	label := canvas.NewText(text, accentCyan)
	label.TextStyle = fyne.TextStyle{Bold: true}
	label.TextSize = 12
	return label
}

// sectionDivider creates a thin, centred accent line with extra vertical
// padding, used between form sections.
func sectionDivider() fyne.CanvasObject {
	line := canvas.NewRectangle(accentCyan)
	line.SetMinSize(fyne.NewSize(500, 1))
	return container.NewPadded(container.NewCenter(line))
}

// fixedWidth wraps obj so it is laid out at the given width (and its own
// minimum height) instead of stretching to fill its container.
func fixedWidth(obj fyne.CanvasObject, width float32) fyne.CanvasObject {
	size := fyne.NewSize(width, obj.MinSize().Height)
	return container.New(layout.NewGridWrapLayout(size), obj)
}
