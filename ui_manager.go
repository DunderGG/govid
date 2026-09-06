// ui_manager.go — Main window layout and singleton secondary window management.
//
// Responsibilities:
//   - UIManager: typed component that owns the primary window reference and
//     the secondary window references (About, Help, History, Preferences,
//     Post-Processing), ensuring at most one instance of each is open at a time.
//   - createUI: builds the main window layout (header, input tools, status,
//     logs, and footer) and wires its widget event handlers.
//   - createMainMenu: builds the main window's menu bar.
//   - showAbout, showHistory, showConfigHelp, showPreferences,
//     showPostProcessing: self-contained window construction, built from
//     UIManager's own widget field and injected service callbacks.
//   - savePreferences, resetPreferences, rebuildUI: preference persistence
//     and UI-rebuild helpers used by showPreferences.
//   - checkDependencies, runUpdateInUI: thin delegates to the injected
//     dependency-service callbacks for the startup tool check and the
//     "Update yt-dlp" menu action.
package main

import (
	"fmt"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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
// each is a singleton — at most one window instance open at a time. It holds
// no service-type references directly; all service access is bridged in via
// callbacks so UIManager stays decoupled from the service implementations.
type UIManager struct {
	mainWindow    fyne.Window // primary application window; content is built by createUI
	aboutWindow   fyne.Window
	helpWindow    fyne.Window
	historyWindow fyne.Window
	prefsWindow   fyne.Window // owned here for singleton tracking; opened by DownloaderApp
	ppWindow      fyne.Window // owned here for singleton tracking; opened by DownloaderApp
	ui            *UIWidgets  // shared widget bag; set by newDownloaderApp after construction

	// Callbacks bridging DownloaderApp actions and services into the main
	// window and secondary windows; all set by newDownloaderApp after
	// construction.
	onLog                func(line string, col color.Color)                                                                          // appends a line to the terminal output panel
	onStatus             func(msg string)                                                                                            // updates the short status label
	onSetStatusIndicator func(state string)                                                                                          // updates the status dot color
	onStartDownload      func()                                                                                                      // begins a download/batch run
	onOpenFolder         func()                                                                                                      // opens the save destination in the system file manager
	onRequestCancel      func() bool                                                                                                 // cancels the active download or post-process job
	onLoadHistory        func() ([]DownloadHistoryEntry, error)                                                                      // HistoryService.Load
	onClearHistory       func() error                                                                                                // HistoryService.Clear
	onCheckDependencies  func(onWarning func(msg string))                                                                            // DependencyService.Check
	onRunUpdate          func(cb UpdateCallbacks)                                                                                    // DependencyService.RunUpdate
	onLoadPreferences    func() AppPreferences                                                                                       // PreferenceService.Load
	onSavePreferences    func(AppPreferences)                                                                                        // PreferenceService.Save
	onResetPreferences   func()                                                                                                      // PreferenceService.Reset
	onLoadConfigFile     func(path string) (*AppConfig, error)                                                                       // PreferenceService.LoadFromFile
	onMergeConfig        func(cfg *AppConfig, base AppPreferences, validFormats, validQualities []string) (AppPreferences, []string) // PreferenceService.MergeConfig
	onSetLogBufferLimit  func(limit int)                                                                                             // LogService.SetBufferLimit
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
	manager.onCheckDependencies(func(msg string) {
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
	manager.onRunUpdate(UpdateCallbacks{
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
	entries, err := manager.onLoadHistory()
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
				if err := manager.onClearHistory(); err != nil {
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
	prefs := manager.onLoadPreferences()

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
			manager.onSetLogBufferLimit(ParseBufferLimit(ui.logLimit.Selected))
			manager.savePreferences(ui.path.Text)

			// Apply theme change and rebuild the UI so canvas.Rectangle colors
			// (which are snapshotted at construction time) get fresh theme values.
			switch ui.themeMode.Selected {
			case "Light":
				fyne.CurrentApp().Settings().SetTheme(&lightTheme{})
			default:
				fyne.CurrentApp().Settings().SetTheme(&darkTheme{})
			}
			manager.createUI()
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
		config, err := manager.onLoadConfigFile(configFileName)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to load govid.json: %v", err), manager.prefsWindow)
			return
		}
		merged, errs := manager.onMergeConfig(config, manager.onLoadPreferences(), ui.format.Options, ui.quality.Options)
		applyPreferencesToWidgets(ui, merged)
		manager.onSavePreferences(merged)
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
	manager.onSavePreferences(AppPreferences{
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
	manager.onResetPreferences()
	manager.onSetLogBufferLimit(200)
}

// rebuildUI applies the default dark theme and recreates the main window
// layout. Called after resetPreferences to complete a full application reset.
func (manager *UIManager) rebuildUI() {
	fyne.CurrentApp().Settings().SetTheme(&darkTheme{})
	manager.createUI()
}

// showPostProcessing opens a window for specialized hardware/software filters.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showPostProcessing() {
	if focusOrCreate(&manager.ppWindow) {
		return
	}

	ui := manager.ui
	prefs := manager.onLoadPreferences()

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
		line.SetMinSize(fyne.NewSize(500, 1))
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
			{Text: "Encoder Backend", Widget: container.New(layout.NewGridWrapLayout(fyne.NewSize(200, ui.gpuBackend.MinSize().Height)), ui.gpuBackend), HintText: "GPU-accelerated re-encoding; falls back to CPU if unavailable"},
			{Text: "", Widget: sectionDivider()},
			// ── MOTION ─────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("MOTION ENHANCEMENT")},
			{Text: "Smooth Motion", Widget: ui.smoothMotion, HintText: "Interpolate frames for fluid playback (slow)"},
			{Text: "Smoothing Mode", Widget: ui.smoothMotionMode, HintText: "Precise/Balanced use motion vectors, Fast uses blending"},
			{Text: "Target FPS", Widget: container.NewHBox(container.New(layout.NewGridWrapLayout(fyne.NewSize(200, ui.smoothMotionFPS.MinSize().Height)), ui.smoothMotionFPS), fpsLabel), HintText: "Standard is 60, cinematic is 24, high-refresh is 120"},
			{Text: "", Widget: sectionDivider()},
			// ── VIDEO ──────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("VIDEO ENHANCEMENT")},
			{Text: "Vivid Mode", Widget: ui.vividMode, HintText: "Boost brightness, contrast, and saturation"},
			{Text: "Sharpen Video", Widget: ui.sharpen, HintText: "CAS (Contrast Adaptive Sharpening) — sharpens edges without haloing or noise amplification"},
			{Text: "Sharpen Intensity", Widget: container.NewHBox(container.New(layout.NewGridWrapLayout(fyne.NewSize(200, ui.sharpenAmount.MinSize().Height)), ui.sharpenAmount), sharpenLabel), HintText: "1.0x is gentle, 1.5x is moderate, 2.0x is strong"},
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
			{Text: "Target Resolution", Widget: container.New(layout.NewGridWrapLayout(fyne.NewSize(200, ui.upscaleTarget.MinSize().Height)), ui.upscaleTarget), HintText: "2× doubles both dimensions; fixed targets set a specific height"},
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

// ── Main window ────────────────────────────────────────────────────────────────

// createUI constructs the graphical user interface by organizing widgets into
// cards and containers. It sets up the layout (header, input tools, status,
// logs, and footer) and attaches event handlers to buttons.
func (manager *UIManager) createUI() {
	ui := manager.ui

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
		manager.createUI()
	}
	ui.saveLog.OnChanged = func(_ bool) {
		manager.savePreferences(ui.path.Text)
	}
	ui.notify.OnChanged = func(_ bool) {
		manager.savePreferences(ui.path.Text)
	}
	ui.autoRetry.OnChanged = func(_ bool) {
		manager.savePreferences(ui.path.Text)
	}
	ui.enablePostProcess.OnChanged = func(_ bool) {
		manager.savePreferences(ui.path.Text)
	}
	ui.path.SetPlaceHolder("Download folder...")
	ui.path.OnChanged = func(text string) {
		if ui.savePrefs.Checked {
			fyne.CurrentApp().Preferences().SetString(prefSavedPath, strings.TrimSpace(text))
		}
	}

	// Load previously saved path from preferences.
	prefs := manager.onLoadPreferences()
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
		}, manager.mainWindow)
	})

	ui.downloadBtn.Icon = themedIcon(IconDownload)
	ui.downloadBtn.Text = "Download Now!"
	ui.downloadBtn.OnTapped = func() {
		manager.onStartDownload()
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
		manager.onOpenFolder()
	})

	ui.cancelBtn.Icon = themedIcon(IconCancel)
	ui.cancelBtn.Text = "Cancel"
	ui.cancelBtn.OnTapped = func() {
		if manager.onRequestCancel() {
			manager.onLog("Download canceled by user.", colWarning)
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
	manager.mainWindow.SetContent(container.NewPadded(content))
}
