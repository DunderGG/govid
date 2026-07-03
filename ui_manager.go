// ui_manager.go — Singleton window management.
//
// Responsibilities:
//   - UIManager: typed component that owns the secondary window references
//     (About, Help, History, Preferences, Post-Processing) and ensures at
//     most one window instance open at a time.
//   - showAbout, showHistory, showConfigHelp: moved here because they are
//     self-contained UI construction with no calls into business logic.
//   - showPreferences, showPostProcessing, createMainMenu, createUI remain on
//     DownloaderApp for now — they require a heavy callback surface that will
//     be reduced once PreferenceService and DependencyService are extracted.
package main

import (
	"fmt"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// UIManager owns references to all (non-main, for now) windows and ensures
// each is a singleton — at most one window instance open at a time.
type UIManager struct {
	mainWindow    fyne.Window // reference to the primary window for dialog parenting
	aboutWindow   fyne.Window
	helpWindow    fyne.Window
	historyWindow fyne.Window
	prefsWindow   fyne.Window // owned here for singleton tracking; opened by DownloaderApp
	ppWindow      fyne.Window // owned here for singleton tracking; opened by DownloaderApp
}

// NewUIManager returns a UIManager bound to the given primary window.
func NewUIManager(mainWindow fyne.Window) *UIManager {
	return &UIManager{mainWindow: mainWindow}
}

// showAbout opens a small window with information about the creator and the app.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showAbout() {
	if manager.aboutWindow != nil {
		manager.aboutWindow.RequestFocus()
		return
	}

	logo := canvas.NewImageFromResource(resourceAppiconPng)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(80, 80))

	//TODO: theme.PrimaryColor() is deprecated.
	appName := canvas.NewText("GoVid", theme.PrimaryColor())
	appName.TextSize = 24
	appName.TextStyle = fyne.TextStyle{Bold: true}
	appName.Alignment = fyne.TextAlignCenter

	versionLabel := widget.NewLabelWithStyle("v"+version, fyne.TextAlignCenter, fyne.TextStyle{Monospace: true})
	tagline := widget.NewLabelWithStyle("A high-performance video downloader\nbuilt with Go and Fyne.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	author := widget.NewLabelWithStyle("Created by David Bennehag", fyne.TextAlignCenter, fyne.TextStyle{})
	website := widget.NewHyperlink("dunder.gg", parseURL("https://dunder.gg"))
	github := widget.NewHyperlink("github.com/DunderGG/govid", parseURL("https://github.com/DunderGG/govid"))
	links := container.NewHBox(layout.NewSpacer(), website, widget.NewLabel("•"), github, layout.NewSpacer())

	content := container.NewVBox(
		container.NewCenter(logo),
		container.NewCenter(appName),
		container.NewCenter(versionLabel),
		container.NewCenter(tagline),
		widget.NewSeparator(),
		container.NewCenter(author),
		links,
	)

	manager.aboutWindow = fyne.CurrentApp().NewWindow("About GoVid")
	manager.aboutWindow.SetContent(container.NewPadded(content))
	manager.aboutWindow.Resize(fyne.NewSize(360, 280))
	manager.aboutWindow.SetFixedSize(true)
	manager.aboutWindow.SetOnClosed(func() { manager.aboutWindow = nil })
	manager.aboutWindow.Show()
}

// showHistory opens a window listing previously downloaded URLs from disk.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showHistory() {
	if manager.historyWindow != nil {
		manager.historyWindow.RequestFocus()
		return
	}

	// Load the download history from disk. If it fails, show an error dialog and abort.
	entries, err := loadDownloadHistory()
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to load download history: %v", err), manager.mainWindow)
		return
	}

	text := widget.NewMultiLineEntry()
	text.SetPlaceHolder("No download history yet.")
	var lines []string

	for _, entry := range slices.Backward(entries) {
		title := entry.OriginalTitle
		if title == "" {
			title = entry.FinalFilename
		}
		if title == "" {
			title = entry.URL
		}
		lines = append(lines,
			fmt.Sprintf("%s | %s", entry.DownloadedAt, title),
			fmt.Sprintf("  URL: %s", entry.URL),
			fmt.Sprintf("  Saved As: %s", entry.FinalFilename),
			fmt.Sprintf("  Path: %s", entry.SavedPath),
			fmt.Sprintf("  Format/Quality: %s / %s", entry.Format, entry.Quality),
			fmt.Sprintf("  Post-Processed: %t", entry.PostProcessed),
			"",
		)
	}
	text.SetText(strings.Join(lines, "\n"))
	text.Disable()

	scroll := container.NewScroll(text)
	scroll.SetMinSize(fyne.NewSize(760, 420))

	clearBtn := widget.NewButton("Clear History", func() {
		dialog.ShowConfirm(
			"Clear Download History",
			"Are you sure you want to clear all download history? This cannot be undone.",
			func(ok bool) {
				if !ok {
					return
				}
				if err := clearDownloadHistory(); err != nil {
					dialog.ShowError(fmt.Errorf("failed to clear history: %v", err), manager.historyWindow)
					return
				}
				text.SetText("")
			},
			manager.historyWindow,
		)
	})

	bottomBar := container.NewHBox(layout.NewSpacer(), clearBtn)
	content := container.NewBorder(nil, bottomBar, nil, nil, scroll)

	manager.historyWindow = fyne.CurrentApp().NewWindow("Download History")
	manager.historyWindow.SetContent(container.NewPadded(content))
	manager.historyWindow.Resize(fyne.NewSize(800, 500))
	manager.historyWindow.SetOnClosed(func() { manager.historyWindow = nil })
	manager.historyWindow.Show()
}

// showConfigHelp opens a scrollable window explaining all configuration options.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showConfigHelp() {
	if manager.helpWindow != nil {
		manager.helpWindow.RequestFocus()
		return
	}

	type helpItem struct {
		label string
		desc  string
	}

	items := []helpItem{
		{"Video URL", "Paste any URL supported by yt-dlp, such as a **YouTube**, **Vimeo**, or **Twitter/X** link."},
		{"Save Destination", "The folder where the downloaded file will be saved. GoVid remembers this between sessions."},
		{"Output Format", "The container format for the downloaded file:\n  * **MP4** – widely compatible, recommended for most uses\n  * **MKV** – flexible container, ideal for high-quality archiving\n  * **WebM** – open format, good for web use\n  * **MP3** – audio only, compressed\n  * **M4A** – audio only, Apple/iTunes compatible"},
		{"Max Quality", "Sets the maximum resolution yt-dlp will request:\n  * **Best Quality** – downloads the highest resolution available\n  * **1080p** / **720p** / **480p** / **360p** – caps the resolution to save space or bandwidth"},
		{"Trim Start / Trim End", "Download only a segment of the video. Leave both blank to download the full video.\n\nAccepted formats:\n  * `HH:MM:SS` (e.g. 01:30:00)\n  * `MM:SS` (e.g. 01:30)\n  * `Seconds` (e.g. 90)\n\nEither field can be used alone:\n  * **Trim Start only** → downloads from that point to the end\n  * **Trim End only** → downloads from the start to that point"},
		{"Save output to log file", "When checked, everything printed in the Terminal Output panel is also saved to a **GoVid_log_YYYY-MM-DD.txt** file in your save destination folder. Errors are also mirrored to a separate **GoVid_errors_YYYY-MM-DD.txt** file."},
		{"Notify on Completion", "When checked, a system notification is sent when a download finishes (success or failure), but not when cancelled."},
		{"Save Preferences", "Found in **Tools → Preferences**. When checked, GoVid remembers your format, quality, save path, speed limit, and theme between sessions. The toggle itself is always remembered so the choice survives a restart."},
		{"Max Download Speed", "Found in **Tools → Preferences**. Limits the bandwidth used by GoVid to prevent network saturation. Examples:\n  * `50K` – Very slow\n  * `5M` – Moderate (standard HD streaming speed)\n  * `10G` – Virtually unlimited\n\nLeave blank to use full available bandwidth."},
		{"Cookies File", "Found in **Tools → Preferences**. Path to a `cookies.txt` file in Mozilla/Netscape format. Required for access to restricted, private, or age-gated videos.\n\n⚠️ **Security Warning**: Cookie files contain sensitive session data. Never share this file."},
		{"Post-Processing", "Found in **Tools → Post-Processing**. Enhance your downloads using FFmpeg. Most filters trigger a full re-encode.\n\n⚠️ **WebM files** use VP9 encoding which is significantly slower than H.264 — use MKV for faster post-processing."},
		{"Cancel", "Stops the active download immediately. In batch mode, it skips the current URL and moves on to the next one."},
		{"Open Folder", "Opens your chosen save destination in the system file manager."},
		{"JSON Configuration", "For advanced users, GoVid supports loading settings from a `govid.json` file located in the application folder.\n\n**Supported Values:**\n* **format**: `MP4`, `MKV`, `WebM`, `MP3`, `M4A`\n* **quality**: `Best Quality`, `1080p`, `720p`, `480p`, `360p`\n* **path**: Any valid absolute folder path\n* **maxSpeed**: Numeric value with unit, e.g., `50K`, `5M`, `1G` (or blank for unlimited)"},
	}

	content := container.NewVBox()
	for _, item := range items {
		title := widget.NewRichTextFromMarkdown("### " + item.label)
		title.Wrapping = fyne.TextWrapOff

		body := widget.NewRichTextFromMarkdown(item.desc)
		for segIdx := range body.Segments {
			if segment, ok := body.Segments[segIdx].(*widget.TextSegment); ok {
				if segment.Style.TextStyle.Bold {
					segment.Style.ColorName = theme.ColorNamePrimary
				}
				if segment.Style.TextStyle.Monospace {
					segment.Style.ColorName = theme.ColorNameWarning
				}
			}
		}
		body.Wrapping = fyne.TextWrapWord

		content.Add(title)
		content.Add(body)
		content.Add(widget.NewSeparator())
	}

	scroll := container.NewScroll(content)
	scroll.SetMinSize(fyne.NewSize(520, 420))

	manager.helpWindow = fyne.CurrentApp().NewWindow("GoVid Guide")
	manager.helpWindow.SetContent(container.NewPadded(scroll))
	manager.helpWindow.Resize(fyne.NewSize(550, 500))
	manager.helpWindow.SetOnClosed(func() { manager.helpWindow = nil })
	manager.helpWindow.Show()
}
