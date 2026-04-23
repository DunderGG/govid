package main

import (
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// createMainMenu builds the application's top-level menu bar.
func (app *DownloaderApp) createMainMenu() {
	updateMenu := fyne.NewMenuItem("Update yt-dlp", func() {
		dialog.ShowConfirm("Update yt-dlp", "This will run 'yt-dlp -U' to update the tool. Continue?", func(ok bool) {
			if ok {
				app.runUpdateInUI()
			}
		}, app.window)
	})

	configHelpMenu := fyne.NewMenuItem("Configuration Guide", func() {
		app.showConfigHelp()
	})

	aboutMenu := fyne.NewMenuItem("About GoVid", func() {
		app.showAbout()
	})

	mainMenu := fyne.NewMainMenu(
		fyne.NewMenu("Tools", updateMenu),
		fyne.NewMenu("Help", configHelpMenu, fyne.NewMenuItemSeparator(), aboutMenu),
	)
	app.window.SetMainMenu(mainMenu)
}

// showConfigHelp opens a scrollable window explaining all configuration options.
func (app *DownloaderApp) showConfigHelp() {
	type helpItem struct {
		label string
		desc  string
	}

	items := []helpItem{
		{"Video URL", "Paste any URL supported by yt-dlp, such as a YouTube, Vimeo, or Twitter/X link."},
		{"Save Destination", "The folder where the downloaded file will be saved. GoVid remembers this between sessions."},
		{"Output Format", "The container format for the downloaded file:\n  • MP4 – widely compatible, recommended for most uses\n  • MKV – flexible container, ideal for high-quality archiving\n  • WebM – open format, good for web use\n  • MP3 – audio only, compressed\n  • M4A – audio only, Apple/iTunes compatible"},
		{"Max Quality", "Sets the maximum resolution yt-dlp will request:\n  • Best Quality – downloads the highest resolution available\n  • 1080p / 720p / 480p / 360p – caps the resolution to save space or bandwidth"},
		{"Trim Start / Trim End", "Download only a segment of the video. Leave both blank to download the full video.\nAccepted formats:\n  • HH:MM:SS  (e.g. 01:30:00)\n  • MM:SS      (e.g. 01:30)\n  • Seconds    (e.g. 90)\nBoth fields must be filled or both left empty."},
		{"Allow Duplicate Downloads", "When checked, a timestamp is added to the filename so re-downloading the same video does not overwrite the previous file."},
		{"Save output to log file", "When checked, everything printed in the Terminal Output panel is also saved to a GoVid_log.txt file in your save destination folder."},
		{"Cancel", "Stops the active download immediately. Partially downloaded files are discarded (due to --no-part mode)."},
		{"Open Folder", "Opens your chosen save destination in the system file manager."},
	}

	content := container.NewVBox()
	for _, item := range items {
		title := widget.NewRichTextFromMarkdown("### " + item.label)
		title.Wrapping = fyne.TextWrapOff

		body := widget.NewLabel(item.desc)
		body.Wrapping = fyne.TextWrapWord

		content.Add(title)
		content.Add(body)
		content.Add(widget.NewSeparator())
	}

	scroll := container.NewScroll(content)
	scroll.SetMinSize(fyne.NewSize(520, 420))

	w := fyne.CurrentApp().NewWindow("Configuration Guide")
	w.SetContent(container.NewPadded(scroll))
	w.Resize(fyne.NewSize(540, 460))
	w.Show()
}

// showAbout opens a small window with information about the creator and the app.
func (app *DownloaderApp) showAbout() {
	logo := canvas.NewImageFromResource(resourceAppiconPng)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(80, 80))

	appName := canvas.NewText("GoVid", theme.PrimaryColor())
	appName.TextSize = 24
	appName.TextStyle = fyne.TextStyle{Bold: true}
	appName.Alignment = fyne.TextAlignCenter

	tagline := widget.NewLabelWithStyle("A high-performance video downloader\nbuilt with Go and Fyne.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})

	author := widget.NewLabelWithStyle("Created by David Bennehag", fyne.TextAlignCenter, fyne.TextStyle{})
	website := widget.NewHyperlink("dunder.gg", parseURL("https://dunder.gg"))
	github := widget.NewHyperlink("github.com/DunderGG/govid", parseURL("https://github.com/DunderGG/govid"))

	links := container.NewHBox(layout.NewSpacer(), website, widget.NewLabel("•"), github, layout.NewSpacer())

	content := container.NewVBox(
		container.NewCenter(logo),
		container.NewCenter(appName),
		container.NewCenter(tagline),
		widget.NewSeparator(),
		container.NewCenter(author),
		links,
	)

	w := fyne.CurrentApp().NewWindow("About GoVid")
	w.SetContent(container.NewPadded(content))
	w.Resize(fyne.NewSize(360, 280))
	w.SetFixedSize(true)
	w.Show()
}

// parseURL is a small helper to safely parse a URL string for use in hyperlinks.
func parseURL(rawURL string) *url.URL {
	u, _ := url.Parse(rawURL)
	return u
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

	ui.entry.SetPlaceHolder("https://www.youtube.com/watch?v=...")
	ui.path.SetPlaceHolder("Download folder...")

	// Load previously saved path from preferences.
	prefs := fyne.CurrentApp().Preferences()
	savedPath := prefs.String("savedPath")
	if savedPath != "" {
		ui.path.SetText(savedPath)
	} else {
		exePath, err := os.Executable()
		if err == nil {
			ui.path.SetText(filepath.Dir(exePath))
		} else {
			cwd, _ := os.Getwd()
			ui.path.SetText(cwd)
		}
	}

	browseBtn := widget.NewButtonWithIcon("", iconFolderOpen, func() {
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			ui.path.SetText(filepath.FromSlash(list.Path()))
		}, app.window)
	})

	downloadBtn := widget.NewButtonWithIcon("Download Now!", iconDownload, func() {
		app.startDownload()
	})
	downloadBtn.Importance = widget.HighImportance

	ui.format.Options = []string{"MP4", "MKV", "WebM", "MP3 (Audio Only)", "M4A (Apple Audio)"}

	savedFormat := prefs.String("format")
	savedQuality := prefs.String("quality")

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

	openFolderBtn := widget.NewButtonWithIcon("Open Folder", iconFolder, func() {
		app.openDownloadFolder()
	})

	ui.cancelBtn.Icon = iconCancel
	ui.cancelBtn.Text = "Cancel"
	ui.cancelBtn.OnTapped = func() {
		if app.cancelFn != nil {
			app.cancelFn()
			app.appendOutput("Download canceled by user.", color.RGBA{R: 255, G: 165, B: 0, A: 255})
		}
	}

	headerText := widget.NewRichTextFromMarkdown("# Media Selection")
	headerText.Wrapping = fyne.TextWrapOff
	header := container.NewHBox(headerText, layout.NewSpacer(), brandLogo)

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
			widget.NewLabelWithStyle("Video URL:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			ui.entry,
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
					widget.NewLabelWithStyle("Trim Start: (leave blank for full video)", fyne.TextAlignLeading, fyne.TextStyle{}),
					ui.trimStart,
				),
				container.NewVBox(
					widget.NewLabelWithStyle("Trim End:", fyne.TextAlignLeading, fyne.TextStyle{}),
					ui.trimEnd,
				),
			),
			container.NewHBox(ui.duplicates, ui.saveLog),
			container.NewGridWithColumns(3, downloadBtn, openFolderBtn, ui.cancelBtn),
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
// a dark background with a subtle 1px border and 10px corner radius, then
// layers an optional italic subtitle and the provided content on top.
func roundedCard(subtitle string, content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.RGBA{R: 26, G: 26, B: 36, A: 255})
	bg.CornerRadius = 10
	bg.StrokeColor = color.RGBA{R: 45, G: 45, B: 60, A: 255}
	bg.StrokeWidth = 1

	sub := widget.NewLabelWithStyle(subtitle, fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	inner := container.NewVBox(sub, content)
	return container.NewStack(bg, container.NewPadded(inner))
}
