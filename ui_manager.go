// ui_manager.go — Singleton window management.
//
// Responsibilities:
//   - UIManager: typed component that owns the secondary window references
//     (About, Help, History, Preferences, Post-Processing) and ensures at
//     most one window instance open at a time.
//   - createMainMenu: builds the main window's menu bar.
//   - showAbout, showHistory, showConfigHelp, showPreferences,
//     showPostProcessing: self-contained window construction, built from
//     UIManager's own widget and service fields.
//   - savePreferences, resetPreferences, rebuildUI: preference persistence
//     and UI-rebuild helpers used by showPreferences.
//   - checkDependencies, runUpdateInUI: thin delegates to DependencyService
//     for the startup tool check and the "Update yt-dlp" menu action.
//
// createUI still lives on DownloaderApp (see docs/refactor_roadmap.md
// Phase 5 for the plan to move it here).
package main

import (
	"fmt"
	"image/color"
	"net/url"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
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
	prefsWindow   fyne.Window        // owned here for singleton tracking; opened by DownloaderApp
	ppWindow      fyne.Window        // owned here for singleton tracking; opened by DownloaderApp
	historySvc    *HistoryService    // history persistence; set by newDownloaderApp after construction
	depSvc        *DependencyService // dependency checks and yt-dlp updater; set by newDownloaderApp after construction
	ui            *UIWidgets         // shared widget bag; set by newDownloaderApp after construction
	prefSvc       *PreferenceService // preference load/save/reset; set by newDownloaderApp after construction
	logSvc        *LogService        // log buffer-limit updates; set by newDownloaderApp after construction
	onCreateUI    func()             // rebuilds the main window; temporary until createUI itself moves here

	// Callbacks bridging DependencyService progress into the main window; all
	// set by newDownloaderApp after construction.
	onLog                func(line string, col color.Color) // appends a line to the terminal output panel
	onStatus             func(msg string)                   // updates the short status label
	onSetStatusIndicator func(state string)                 // updates the status dot color
}

// NewUIManager returns a UIManager bound to the given primary window.
func NewUIManager(mainWindow fyne.Window) *UIManager {
	return &UIManager{mainWindow: mainWindow}
}

// ── Singleton window helpers ──────────────────────────────────────────────────

// focusOrCreate returns true when window is already open, focusing it so the
// caller can return immediately without building a new window. Usage:
//
//	if focusOrCreate(&manager.aboutWindow) { return }
func focusOrCreate(window *fyne.Window) bool {
	if *window != nil {
		(*window).RequestFocus()
		return true
	}
	return false
}

// onWindowClosed returns a closure that nils the window field when the window
// is closed. Assign it directly to SetOnClosed:
//
//	w.SetOnClosed(onWindowClosed(&manager.aboutWindow))
func onWindowClosed(window *fyne.Window) func() {
	return func() { *window = nil }
}

// parseURL is a small helper to safely parse a URL string for use in hyperlinks.
func parseURL(rawURL string) *url.URL {
	parsed, _ := url.Parse(rawURL)
	return parsed
}

// ── Main menu ──────────────────────────────────────────────────────────────────

// createMainMenu builds the application's top-level menu bar.
func (manager *UIManager) createMainMenu() {
	historyMenu := fyne.NewMenuItem("History", func() {
		manager.showHistory()
	})

	updateMenu := fyne.NewMenuItem("Update yt-dlp", func() {
		dialog.ShowConfirm("Update yt-dlp", "This will run 'yt-dlp -U' to update the tool. Continue?", func(ok bool) {
			if ok {
				manager.runUpdateInUI()
			}
		}, manager.mainWindow)
	})

	prefsMenu := fyne.NewMenuItem("Preferences", func() {
		manager.showPreferences()
	})

	configHelpMenu := fyne.NewMenuItem("GoVid Guide", func() {
		manager.showConfigHelp()
	})

	aboutMenu := fyne.NewMenuItem("About GoVid", func() {
		manager.showAbout()
	})

	mainMenu := fyne.NewMainMenu(
		fyne.NewMenu("File", historyMenu),
		fyne.NewMenu("Tools", updateMenu, prefsMenu, fyne.NewMenuItem("Post-Processing", func() {
			manager.showPostProcessing()
		})),
		fyne.NewMenu("Help", configHelpMenu, fyne.NewMenuItemSeparator(), aboutMenu),
	)
	manager.mainWindow.SetMainMenu(mainMenu)
}

// checkDependencies verifies that the required external tools — yt-dlp and
// ffmpeg — are available either in the 'bin' folder beside the executable or
// in the system PATH. Warnings are printed to the log panel.
func (manager *UIManager) checkDependencies() {
	manager.depSvc.Check(func(msg string) {
		manager.onLog(msg, colWarning)
	})
}

// runUpdateInUI sets the initial UI state for an update and delegates
// execution to DependencyService, which runs yt-dlp -U in a background
// goroutine and reports progress via UpdateCallbacks.
func (manager *UIManager) runUpdateInUI() {
	manager.onLog("[SYSTEM] Starting yt-dlp update...", colSystem)
	manager.onSetStatusIndicator("active")
	manager.onStatus("Status: Updating yt-dlp...")
	manager.depSvc.RunUpdate(UpdateCallbacks{
		OnLog:     manager.onLog,
		OnStatus:  manager.onStatus,
		OnSuccess: func() { manager.onSetStatusIndicator("success") },
		OnFailure: func() { manager.onSetStatusIndicator("failed") },
	})
}

// showAbout opens a small window with information about the creator and the app.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showAbout() {
	if focusOrCreate(&manager.aboutWindow) {
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
	manager.aboutWindow.SetOnClosed(onWindowClosed(&manager.aboutWindow))
	manager.aboutWindow.Show()
}

// showHistory opens a window listing previously downloaded URLs from disk.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showHistory() {
	if focusOrCreate(&manager.historyWindow) {
		return
	}

	// Load the download history from disk. If it fails, show an error dialog and abort.
	entries, err := manager.historySvc.Load()
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
				if err := manager.historySvc.Clear(); err != nil {
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
	manager.historyWindow.SetOnClosed(onWindowClosed(&manager.historyWindow))
	manager.historyWindow.Show()
}

// showConfigHelp opens a scrollable window explaining all configuration options.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showConfigHelp() {
	if focusOrCreate(&manager.helpWindow) {
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
	manager.helpWindow.SetOnClosed(onWindowClosed(&manager.helpWindow))
	manager.helpWindow.Show()
}

// showPreferences opens a window for general application settings.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showPreferences() {
	if focusOrCreate(&manager.prefsWindow) {
		return
	}

	ui := manager.ui
	prefs := manager.prefSvc.Load()

	// Log Buffer Limit
	ui.logLimit.SetSelected(prefs.LogLimit)

	// Speed Limit field
	ui.maxSpeed.SetPlaceHolder("e.g. 5M (Unlimited if blank)")
	ui.maxSpeed.SetText(prefs.MaxSpeed)

	// Theme Mode field — horizontal radio group for a simple two-option toggle.
	ui.themeMode.Horizontal = true
	ui.themeMode.SetSelected(prefs.ThemeMode)

	// Cookies field
	ui.cookies.SetPlaceHolder("Path to cookies.txt (optional)")
	ui.cookies.SetText(prefs.CookiesPath)

	cookiesBrowse := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			ui.cookies.SetText(reader.URI().Path())
			reader.Close()
		}, manager.prefsWindow)
		// Filter for common cookie file extensions
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".txt", ".cookies", ".dat"}))
		fileDialog.Show()
	})
	cookiesClear := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		ui.cookies.SetText("")
	})
	cookiesRow := container.NewBorder(nil, nil, nil, container.NewHBox(cookiesBrowse, cookiesClear), ui.cookies)

	// Save Preferences toggle.
	ui.savePrefs.SetChecked(prefs.SavePrefs)

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Save Preferences", Widget: ui.savePrefs, HintText: "Remember format, quality, path, speed, and theme between sessions"},
			{Text: "Log Buffer Limit", Widget: ui.logLimit, HintText: "Max lines kept in the log view; older entries are removed from the top"},
			{Text: "Max Download Speed", Widget: ui.maxSpeed, HintText: "Limits download rate (e.g. 50K, 5M, 10G)"},
			{Text: "Application Theme", Widget: ui.themeMode, HintText: "Restart may be required for some changes"},
			{Text: "Cookies File", Widget: cookiesRow, HintText: "Path to a Mozilla/Netscape-format cookies.txt file"},
		},
		OnSubmit: func() {
			manager.logSvc.SetBufferLimit(ParseBufferLimit(ui.logLimit.Selected))
			manager.savePreferences(ui.path.Text)

			// Apply theme change and rebuild the UI so canvas.Rectangle colors
			// (which are snapshotted at construction time) get fresh theme values.
			switch ui.themeMode.Selected {
			case "Light":
				fyne.CurrentApp().Settings().SetTheme(&lightTheme{})
			default:
				fyne.CurrentApp().Settings().SetTheme(&darkTheme{})
			}
			manager.onCreateUI()
		},
	}

	resetBtn := widget.NewButton("Restore Defaults", func() {
		dialog.ShowConfirm("Restore Defaults", "Reset all preferences to their default values?", func(ok bool) {
			if !ok {
				return
			}
			manager.resetPreferences()
			manager.rebuildUI()
			ui.savePrefs.SetChecked(true)
			ui.maxSpeed.SetText("")
			ui.cookies.SetText("")
			ui.themeMode.SetSelected("Dark")
			ui.logLimit.SetSelected("200")
		}, manager.prefsWindow)
	})
	resetBtn.Importance = widget.DangerImportance

	loadConfigBtn := widget.NewButtonWithIcon("Load from Config (govid.json)", theme.SettingsIcon(), func() {
		config, err := manager.prefSvc.LoadFromFile(configFileName)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to load govid.json: %v", err), manager.prefsWindow)
			return
		}
		merged, errs := manager.prefSvc.MergeConfig(config, manager.prefSvc.Load(), ui.format.Options, ui.quality.Options)
		applyPreferencesToWidgets(ui, merged)
		manager.prefSvc.Save(merged)
		if len(errs) > 0 {
			dialog.ShowCustom("Config Loaded with Warnings", "OK",
				widget.NewLabel(fmt.Sprintf("some settings were skipped:\n- %s", strings.Join(errs, "\n- "))),
				manager.prefsWindow)
		} else {
			dialog.ShowInformation("Config Loaded", "Preferences updated from govid.json", manager.prefsWindow)
		}
	})

	manager.prefsWindow = fyne.CurrentApp().NewWindow("Preferences")
	manager.prefsWindow.SetContent(container.NewPadded(container.NewVBox(
		form,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, loadConfigBtn, resetBtn),
	)))
	manager.prefsWindow.Resize(fyne.NewSize(500, 360))
	manager.prefsWindow.SetOnClosed(onWindowClosed(&manager.prefsWindow))
	manager.prefsWindow.Show()
}

// savePreferences collects the current widget state into an AppPreferences
// struct and delegates persistence to PreferenceService.Save.
func (manager *UIManager) savePreferences(savePath string) {
	ui := manager.ui
	manager.prefSvc.Save(AppPreferences{
		SavePrefs:         ui.savePrefs.Checked,
		SavedPath:         savePath,
		Format:            ui.format.Selected,
		Quality:           ui.quality.Selected,
		MaxSpeed:          strings.TrimSpace(ui.maxSpeed.Text),
		ThemeMode:         ui.themeMode.Selected,
		CookiesPath:       strings.TrimSpace(ui.cookies.Text),
		LogLimit:          ui.logLimit.Selected,
		BatchMode:         ui.batchMode.Checked,
		SaveLog:           ui.saveLog.Checked,
		Notify:            ui.notify.Checked,
		AutoRetry:         ui.autoRetry.Checked,
		EnablePostProcess: ui.enablePostProcess.Checked,
		SmoothMotion:      ui.smoothMotion.Checked,
		SmoothMotionMode:  ui.smoothMotionMode.Selected,
		SmoothFPS:         ui.smoothMotionFPS.Value,
		Sharpen:           ui.sharpen.Checked,
		SharpenAmount:     ui.sharpenAmount.Value,
		NormalizeAudio:    ui.normalizeAudio.Checked,
		VividMode:         ui.vividMode.Checked,
		Denoise:           ui.denoise.Checked,
		DenoiseMode:       ui.denoiseMode.Selected,
		HDRToSDR:          ui.hdrToSdr.Checked,
		Deband:            ui.deband.Checked,
		AutoCrop:          ui.autoCrop.Checked,
		Stabilize:         ui.stabilize.Checked,
		Deinterlace:       ui.deinterlace.Checked,
		NightMode:         ui.nightMode.Checked,
		UpscaleVideo:      ui.upscaleVideo.Checked,
		UpscaleTarget:     ui.upscaleTarget.Selected,
		GPUBackend:        ui.gpuBackend.Selected,
	})
}

// resetPreferences clears the stored preference data and resets the log
// buffer to its default limit. Call rebuildUI afterwards to complete the
// visual reset.
func (manager *UIManager) resetPreferences() {
	manager.prefSvc.Reset()
	manager.logSvc.SetBufferLimit(200)
}

// rebuildUI applies the default dark theme and recreates the main window
// layout. Called after resetPreferences to complete a full application reset.
func (manager *UIManager) rebuildUI() {
	fyne.CurrentApp().Settings().SetTheme(&darkTheme{})
	manager.onCreateUI()
}

// showPostProcessing opens a window for specialized hardware/software filters.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showPostProcessing() {
	if focusOrCreate(&manager.ppWindow) {
		return
	}

	ui := manager.ui
	prefs := manager.prefSvc.Load()

	// Reload all post-processing prefs so the window always shows persisted state.
	ui.smoothMotion.SetChecked(prefs.SmoothMotion)
	ui.smoothMotionMode.Horizontal = true
	ui.smoothMotionMode.SetSelected(prefs.SmoothMotionMode)
	ui.sharpen.SetChecked(prefs.Sharpen)
	ui.sharpenAmount.SetValue(prefs.SharpenAmount)
	ui.vividMode.SetChecked(prefs.VividMode)
	ui.deband.SetChecked(prefs.Deband)
	ui.hdrToSdr.SetChecked(prefs.HDRToSDR)
	ui.denoise.SetChecked(prefs.Denoise)
	ui.denoiseMode.Horizontal = true
	ui.denoiseMode.SetSelected(prefs.DenoiseMode)
	ui.deinterlace.SetChecked(prefs.Deinterlace)
	ui.stabilize.SetChecked(prefs.Stabilize)
	ui.autoCrop.SetChecked(prefs.AutoCrop)
	ui.upscaleVideo.SetChecked(prefs.UpscaleVideo)
	ui.upscaleTarget.SetSelected(prefs.UpscaleTarget)
	ui.normalizeAudio.SetChecked(prefs.NormalizeAudio)
	ui.nightMode.SetChecked(prefs.NightMode)
	ui.gpuBackend.SetSelected(prefs.GPUBackend)

	// FPS slider for smooth motion — use a bound float so the label updates live.
	fpsBinding := binding.NewFloat()
	fpsBinding.Set(ui.smoothMotionFPS.Value)
	fpsLabel := widget.NewLabelWithData(binding.FloatToStringWithFormat(fpsBinding, "%.0f FPS"))
	ui.smoothMotionFPS.Step = 1
	ui.smoothMotionFPS.OnChanged = func(v float64) {
		fpsBinding.Set(v)
	}
	if !ui.smoothMotion.Checked {
		ui.smoothMotionMode.Disable()
		ui.smoothMotionFPS.Disable()
	}

	// Sharpening slider — bind to float for live label updates.
	sharpenBinding := binding.NewFloat()
	sharpenBinding.Set(ui.sharpenAmount.Value)
	sharpenLabel := widget.NewLabelWithData(binding.FloatToStringWithFormat(sharpenBinding, "%.1fx"))
	ui.sharpenAmount.Step = 0.1
	ui.sharpenAmount.OnChanged = func(v float64) {
		sharpenBinding.Set(v)
	}
	if !ui.sharpen.Checked {
		ui.sharpenAmount.Disable()
	}

	// Live processing-load indicator — 5 colored blocks, each lighting up at a
	// cost threshold. The thresholds are arbitrary and based on testing with a variety of videos and filter combinations,
	// but they should give a rough relative indication of how intensive the current settings are.
	blockEmpty := colLoadEmpty
	blockColors := colLoadPalette
	// Cost thresholds that light up each successive block. These are spaced
	// to give a useful visual spread across the loadThreshold* scale in postprocess.go.
	blockThresholds := []int{15, 35, 65, 100, 130}

	blocks := make([]*canvas.Rectangle, 5)
	for i := range blocks {
		block := canvas.NewRectangle(blockEmpty)
		block.SetMinSize(fyne.NewSize(0, 14))
		block.CornerRadius = 3
		blocks[i] = block
	}

	loadDesc := binding.NewString()
	loadLabel := widget.NewLabelWithData(loadDesc)
	loadLabel.Alignment = fyne.TextAlignCenter

	sizeWarn := binding.NewString()
	sizeWarnLabel := widget.NewLabelWithData(sizeWarn)
	sizeWarnLabel.Alignment = fyne.TextAlignCenter
	sizeWarnLabel.TextStyle = fyne.TextStyle{Italic: true}
	sizeWarnLabel.Wrapping = fyne.TextWrapWord

	refreshLoad := func() {
		cost, desc := computeProcessingLoad(newPostProcessSettings(manager.ui))
		loadDesc.Set(desc)
		for idx, block := range blocks {
			if cost > blockThresholds[idx] {
				block.FillColor = blockColors[idx]
			} else {
				block.FillColor = blockEmpty
			}
			block.Refresh()
		}
		upscale := ui.upscaleVideo.Checked
		smooth := ui.smoothMotion.Checked
		switch {
		case upscale && smooth:
			sizeWarn.Set("⚠ Upscaling + Smooth Motion will greatly increase file size")
		case upscale:
			sizeWarn.Set("⚠ Upscaling significantly increases file size (bigger frames)")
		case smooth:
			sizeWarn.Set("⚠ Smooth Motion increases file size (more frames)")
		default:
			sizeWarn.Set("")
		}
	}

	blockBar := container.NewGridWithColumns(5,
		blocks[0], blocks[1], blocks[2], blocks[3], blocks[4],
	)

	ui.smoothMotion.OnChanged = func(checked bool) {
		if checked {
			ui.smoothMotionMode.Enable()
			ui.smoothMotionFPS.Enable()
		} else {
			ui.smoothMotionMode.Disable()
			ui.smoothMotionFPS.Disable()
		}
		refreshLoad()
	}
	ui.smoothMotionMode.OnChanged = func(_ string) { refreshLoad() }

	// Denoise mode is only relevant when denoise is enabled.
	if !ui.denoise.Checked {
		ui.denoiseMode.Disable()
	}
	ui.denoise.OnChanged = func(checked bool) {
		if checked {
			ui.denoiseMode.Enable()
		} else {
			ui.denoiseMode.Disable()
		}
		refreshLoad()
	}
	ui.denoiseMode.OnChanged = func(_ string) { refreshLoad() }

	ui.sharpen.OnChanged = func(checked bool) {
		if checked {
			ui.sharpenAmount.Enable()
		} else {
			ui.sharpenAmount.Disable()
		}
		refreshLoad()
	}
	ui.sharpenAmount.OnChanged = func(v float64) {
		sharpenBinding.Set(v)
		refreshLoad()
	}

	// Upscale target is only relevant when upscale is enabled.
	if !ui.upscaleVideo.Checked {
		ui.upscaleTarget.Disable()
	}
	ui.upscaleVideo.OnChanged = func(checked bool) {
		if checked {
			ui.upscaleTarget.Enable()
		} else {
			ui.upscaleTarget.Disable()
		}
		refreshLoad()
	}
	ui.upscaleTarget.OnChanged = func(_ string) { refreshLoad() }

	// Simple toggles — just refresh the load indicator.
	ui.vividMode.OnChanged = func(_ bool) { refreshLoad() }
	ui.deband.OnChanged = func(_ bool) { refreshLoad() }
	ui.hdrToSdr.OnChanged = func(_ bool) { refreshLoad() }
	ui.deinterlace.OnChanged = func(_ bool) { refreshLoad() }
	ui.stabilize.OnChanged = func(_ bool) { refreshLoad() }
	ui.autoCrop.OnChanged = func(_ bool) { refreshLoad() }
	ui.normalizeAudio.OnChanged = func(_ bool) { refreshLoad() }
	ui.nightMode.OnChanged = func(_ bool) { refreshLoad() }

	refreshLoad() // seed with the current state

	// sectionDivider creates a very thin, subtle line with extra vertical padding.
	sectionDivider := func() fyne.CanvasObject {
		line := canvas.NewRectangle(accentCyan)
		line.SetMinSize(fyne.NewSize(100, 1))
		return container.NewPadded(container.NewCenter(line))
	}

	// sectionHeader creates a small bold label used as an inline section title.
	sectionHeader := func(text string) fyne.CanvasObject {
		label := canvas.NewText(text, accentCyan)
		label.TextStyle = fyne.TextStyle{Bold: true}
		label.TextSize = 12
		return label
	}

	form := &widget.Form{
		Items: []*widget.FormItem{
			// ── GPU ACCELERATION ─────────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("GPU ACCELERATION")},
			{Text: "Encoder Backend", Widget: ui.gpuBackend, HintText: "Accelerates the final re-encode step (only applies when at least one video filter above is enabled); falls back to CPU automatically if unavailable"},
			{Text: "", Widget: sectionDivider()},
			// ── MOTION ─────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("MOTION ENHANCEMENT")},
			{Text: "Smooth Motion", Widget: ui.smoothMotion, HintText: "Interpolate frames for fluid playback (slow)"},
			{Text: "Smoothing Mode", Widget: ui.smoothMotionMode, HintText: "Precise/Balanced use motion vectors, Fast uses blending"},
			{Text: "Target FPS", Widget: container.NewBorder(nil, nil, nil, fpsLabel, ui.smoothMotionFPS), HintText: "Standard is 60, cinematic is 24, high-refresh is 120"},
			{Text: "", Widget: sectionDivider()},
			// ── VIDEO ──────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("VIDEO ENHANCEMENT")},
			{Text: "Vivid Mode", Widget: ui.vividMode, HintText: "Boost brightness, contrast, and saturation"},
			{Text: "Sharpen Video", Widget: ui.sharpen, HintText: "CAS (Contrast Adaptive Sharpening) — sharpens edges without haloing or noise amplification"},
			{Text: "Sharpen Intensity", Widget: container.NewBorder(nil, nil, nil, sharpenLabel, ui.sharpenAmount), HintText: "1.0x is gentle, 1.5x is moderate, 2.0x is strong"},
			{Text: "Fix Banding", Widget: ui.deband, HintText: "Remove gradient banding steps in skies and dark scenes (deband)"},
			{Text: "HDR to SDR", Widget: ui.hdrToSdr, HintText: "Tone-map 4K HDR content for standard monitors (zscale + Hable tonemap)"},
			{Text: "", Widget: sectionDivider()},
			// ── NOISE & ARTIFACTS ───────────────────────────────────────────
			{Text: "", Widget: sectionHeader("NOISE & ARTIFACTS")},
			{Text: "Denoise", Widget: ui.denoise, HintText: "HQ noise reduction for low-quality or grainy footage"},
			{Text: "Denoise Mode", Widget: ui.denoiseMode, HintText: "NLMeans: highest quality, very slow | hqdn3d: spatial + temporal denoising, fast and effective"},
			{Text: "Deinterlace", Widget: ui.deinterlace, HintText: "Remove combing artifacts from archival or TV-rip content (bwdif)"},
			{Text: "Stabilize", Widget: ui.stabilize, HintText: "Smooth out shaky handheld footage (deshake)"},
			{Text: "Auto-Crop", Widget: ui.autoCrop, HintText: "Detect and remove black letterbox/pillarbox bars automatically"},
			{Text: "", Widget: sectionDivider()},
			// ── UPSCALING ────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("UPSCALING")},
			{Text: "Upscale Video", Widget: ui.upscaleVideo, HintText: "Enlarge the video using a high-quality Lanczos resampler"},
			{Text: "Target Resolution", Widget: ui.upscaleTarget, HintText: "2× doubles both dimensions; fixed targets set a specific height"},
			{Text: "", Widget: sectionDivider()},
			// ── AUDIO ──────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("AUDIO ENHANCEMENT")},
			{Text: "Normalize Audio", Widget: ui.normalizeAudio, HintText: "Loudness normalization via the loudnorm filter"},
			{Text: "Night Mode", Widget: ui.nightMode, HintText: "Dynamic compression to balance quiet dialogue and loud effects (dynaudnorm)"},
		},
	}

	applyBtn := widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), func() {
		manager.savePreferences(ui.path.Text)
	})

	applyCloseBtn := widget.NewButtonWithIcon("Apply & Close", theme.ConfirmIcon(), func() {
		manager.savePreferences(ui.path.Text)
		manager.ppWindow.Close()
	})
	applyCloseBtn.Importance = widget.HighImportance

	buttons := container.NewGridWithColumns(2, applyBtn, applyCloseBtn)

	notice := widget.NewLabelWithStyle("⚠️ Most filters require FFmpeg and trigger a full re-encode.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})

	// Live processing-load indicator.
	loadSection := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Estimated Processing Load", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		blockBar,
		loadLabel,
		sizeWarnLabel,
	)

	title := widget.NewLabelWithStyle("Post-Processing Filters", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	footer := container.NewVBox(loadSection, widget.NewSeparator(), buttons, notice)

	scroll := container.NewScroll(form)
	// Border layout: title pinned top, footer pinned bottom, scroll fills the rest.
	content := container.NewBorder(title, footer, nil, nil, scroll)

	manager.ppWindow = fyne.CurrentApp().NewWindow("Post-Processing Settings")
	manager.ppWindow.SetContent(container.NewPadded(content))
	manager.ppWindow.Resize(fyne.NewSize(680, 580))
	manager.ppWindow.SetFixedSize(false)
	manager.ppWindow.SetOnClosed(onWindowClosed(&manager.ppWindow))
	manager.ppWindow.Show()
}
