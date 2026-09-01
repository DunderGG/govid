// ui.go — Builds and manages every visual element of the GoVid window.
//
// Responsibilities:
//   - Main window layout: URL input, format/quality selectors, trim fields,
//     speed limit, checkboxes, progress bar, and scrollable log view.
//   - Thin delegates to UIManager for secondary windows (History, About,
//     Preferences, Post-Processing, GoVid Guide) and the main menu bar.
package main

import (
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
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

// createUI constructs the graphical user interface by organizing widgets into
// cards and containers. It sets up the layout (header, input tools, status,
// logs, and footer) and attaches event handlers to buttons.
func (app *DownloaderApp) createUI() {
	ui := app.ui

	// Load the logo from the bundled resources.
	image := canvas.NewImageFromResource(resourceAppiconPng)
	image.FillMode = canvas.ImageFillContain
	image.SetMinSize(fyne.NewSize(128, 128))
	brandLogo := image

	// Configure the URL entry for single or batch mode.
	if ui.batchMode.Checked {
		ui.entry.MultiLine = true
		ui.entry.SetMinRowsVisible(4)
		ui.entry.SetPlaceHolder("One URL per line...\nhttps://...\nhttps://...")
	} else {
		ui.entry.MultiLine = false
		ui.entry.SetMinRowsVisible(1)
		ui.entry.SetPlaceHolder("https://www.youtube.com/watch?v=...")
	}
	ui.batchMode.OnChanged = func(checked bool) {
		fyne.CurrentApp().Preferences().SetBool("batchMode", checked)
		if !checked {
			// Switching back to single mode: keep only the first non-empty URL.
			first := ""
			for _, line := range strings.Split(ui.entry.Text, "\n") {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					first = trimmed
					break
				}
			}
			ui.entry.SetText(first)
		}
		app.createUI()
	}
	ui.saveLog.OnChanged = func(_ bool) {
		app.savePreferences(app.ui.path.Text)
	}
	ui.notify.OnChanged = func(_ bool) {
		app.savePreferences(app.ui.path.Text)
	}
	ui.autoRetry.OnChanged = func(_ bool) {
		app.savePreferences(app.ui.path.Text)
	}
	ui.enablePostProcess.OnChanged = func(_ bool) {
		app.savePreferences(app.ui.path.Text)
	}
	ui.path.SetPlaceHolder("Download folder...")
	ui.path.OnChanged = func(text string) {
		if app.ui.savePrefs.Checked {
			fyne.CurrentApp().Preferences().SetString(prefSavedPath, strings.TrimSpace(text))
		}
	}

	// Load previously saved path from preferences.
	prefs := app.prefSvc.Load()
	savedPath := prefs.SavedPath
	if savedPath != "" {
		ui.path.SetText(savedPath)
	} else {
		exePath, err := os.Executable()
		if err == nil {
			ui.path.SetText(filepath.Dir(exePath))
		} else {
			if cwd, err := os.Getwd(); err == nil {
				ui.path.SetText(cwd)
			}
		}
	}

	browseBtn := widget.NewButtonWithIcon("", themedIcon(IconFolderOpen), func() {
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			ui.path.SetText(filepath.FromSlash(list.Path()))
		}, app.window)
	})

	ui.downloadBtn.Icon = themedIcon(IconDownload)
	ui.downloadBtn.Text = "Download Now!"
	ui.downloadBtn.OnTapped = func() {
		app.startDownload()
	}
	ui.downloadBtn.Importance = widget.HighImportance
	ui.downloadBtn.Refresh()

	ui.format.Options = []string{"MP4", "MKV", "WebM", "MP3", "M4A"}

	savedFormat := prefs.Format
	savedQuality := prefs.Quality

	if savedFormat != "" {
		ui.format.SetSelected(savedFormat)
	} else if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		ui.format.SetSelected("MP4")
	} else {
		ui.format.SetSelected("MKV")
	}

	ui.quality.Options = []string{"Best Quality", "1080p", "720p", "480p", "360p"}

	if savedQuality != "" {
		ui.quality.SetSelected(savedQuality)
	} else {
		ui.quality.SetSelected("Best Quality")
	}

	openFolderBtn := widget.NewButtonWithIcon("Open Folder", themedIcon(IconFolder), func() {
		app.openDownloadFolder()
	})

	ui.cancelBtn.Icon = themedIcon(IconCancel)
	ui.cancelBtn.Text = "Cancel"
	ui.cancelBtn.OnTapped = func() {
		if app.RequestCancel() {
			app.appendOutput("Download canceled by user.", colWarning)
		}
	}

	titleText := canvas.NewText("GoVid", accentCyan)
	titleText.TextSize = 38
	titleText.TextStyle = fyne.TextStyle{Bold: true}

	subtitleText := canvas.NewText("Video Downloader", theme.Color(theme.ColorNameDisabled))
	subtitleText.TextSize = 23
	subtitleText.TextStyle = fyne.TextStyle{Italic: true}

	headerLeft := container.NewVBox(titleText, subtitleText)
	header := container.NewHBox(headerLeft, layout.NewSpacer(), brandLogo)

	ui.trimStart.SetPlaceHolder("e.g. 00:01:30  (optional)")
	ui.trimEnd.SetPlaceHolder("e.g. 00:05:00  (optional)")
	ui.trimStart.Validator = validateTimestamp
	ui.trimEnd.Validator = validateTimestamp

	// accentBar returns a 4px wide rectangle in the theme's primary colour,
	// used as a decorative left-edge bar on cards.
	accentBar := func() *canvas.Rectangle {
		bar := canvas.NewRectangle(accentCyan)
		bar.SetMinSize(fyne.NewSize(4, 0))
		return bar
	}

	inputCard := roundedCard("Specify the source and destination",
		container.NewVBox(
			container.NewHBox(
				widget.NewLabelWithStyle("Video URL:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				layout.NewSpacer(),
				ui.batchMode,
			),
			container.NewBorder(nil, nil, nil, widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
				ui.entry.SetText("")
			}), ui.entry),
			widget.NewLabelWithStyle("Save Destination:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewBorder(nil, nil, nil, browseBtn, ui.path),
			container.NewGridWithColumns(2,
				container.NewVBox(
					widget.NewLabelWithStyle("Output Format:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					ui.format,
				),
				container.NewVBox(
					widget.NewLabelWithStyle("Max Quality:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					ui.quality,
				),
			),
			container.NewGridWithColumns(2,
				container.NewVBox(
					widget.NewLabelWithStyle("Trim Start: (optional)", fyne.TextAlignLeading, fyne.TextStyle{}),
					ui.trimStart,
				),
				container.NewVBox(
					widget.NewLabelWithStyle("Trim End: (optional)", fyne.TextAlignLeading, fyne.TextStyle{}),
					ui.trimEnd,
				),
			),
			container.NewHBox(ui.saveLog, ui.notify, ui.autoRetry, ui.enablePostProcess),
			container.NewGridWithColumns(3, ui.downloadBtn, openFolderBtn, ui.cancelBtn),
		),
	)
	inputCardAccented := container.NewBorder(nil, nil, accentBar(), nil, inputCard)

	// Wrap the status dot in a fixed-size container so the circle renders at 18×18.
	dotContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(18, 18)), ui.statusDot)
	statusCard := roundedCard("",
		container.NewVBox(
			ui.progress,
			container.NewHBox(dotContainer, ui.status),
		),
	)
	statusCardAccented := container.NewBorder(nil, nil, accentBar(), nil, statusCard)

	ui.logList = container.NewVBox()
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(0, 10))
	ui.output = container.NewScroll(container.NewVBox(ui.logList, spacer))
	ui.output.SetMinSize(fyne.NewSize(0, 200))

	copyright := canvas.NewText("GoVid • By David Bennehag (dunder.gg) • Built with ❤️, 🤖 and ☕", theme.Color(theme.ColorNameDisabled))
	copyright.TextSize = 14
	copyright.Alignment = fyne.TextAlignCenter
	footer := container.NewCenter(copyright)

	topContent := container.NewVBox(
		header,
		inputCardAccented,
		statusCardAccented,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Terminal Output:", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
	)

	content := container.NewBorder(topContent, footer, nil, nil, ui.output)
	app.window.SetContent(container.NewPadded(content))
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
