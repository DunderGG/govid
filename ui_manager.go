// ui_manager.go — Main window layout and singleton secondary window management.
//
// Responsibilities:
//   - UIManager: typed component that owns the primary window reference and
//     the secondary window references (About, Help, History, Preferences,
//     Post-Processing), ensuring at most one instance of each is open at a time.
//   - createUI: composes the main window layout (header, input card, status
//     card, log pane, footer) from focused builder/wiring helpers below it.
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
	onLogBufferLimit     func() int                                                                                                  // LogService.BufferLimit
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
	ui.prefs.logLimit.SetSelected(prefs.LogLimit)

	// Speed Limit field
	ui.prefs.maxSpeed.SetPlaceHolder("e.g. 5M (Unlimited if blank)")
	ui.prefs.maxSpeed.SetText(prefs.MaxSpeed)

	// Theme Mode field — horizontal radio group for a simple two-option toggle.
	ui.prefs.themeMode.Horizontal = true
	ui.prefs.themeMode.SetSelected(prefs.ThemeMode)

	// Cookies field
	ui.prefs.cookies.SetPlaceHolder("Path to cookies.txt (optional)")
	ui.prefs.cookies.SetText(prefs.CookiesPath)

	cookiesBrowse := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			ui.prefs.cookies.SetText(reader.URI().Path())
			reader.Close()
		}, manager.prefsWindow)
		// Filter for common cookie file extensions
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".txt", ".cookies", ".dat"}))
		fileDialog.Show()
	})
	cookiesClear := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		ui.prefs.cookies.SetText("")
	})
	cookiesRow := container.NewBorder(nil, nil, nil, container.NewHBox(cookiesBrowse, cookiesClear), ui.prefs.cookies)

	// Save Preferences toggle.
	ui.prefs.savePrefs.SetChecked(prefs.SavePrefs)

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Save Preferences", Widget: ui.prefs.savePrefs, HintText: "Remember format, quality, path, speed, and theme between sessions"},
			{Text: "Log Buffer Limit", Widget: ui.prefs.logLimit, HintText: "Max lines kept in the log view; older entries are removed from the top"},
			{Text: "Max Download Speed", Widget: ui.prefs.maxSpeed, HintText: "Limits download rate (e.g. 50K, 5M, 10G)"},
			{Text: "Application Theme", Widget: ui.prefs.themeMode, HintText: "Restart may be required for some changes"},
			{Text: "Cookies File", Widget: cookiesRow, HintText: "Path to a Mozilla/Netscape-format cookies.txt file"},
		},
		OnSubmit: func() {
			manager.onSetLogBufferLimit(ParseBufferLimit(ui.prefs.logLimit.Selected))
			manager.savePreferences(ui.download.path.Text)

			// Apply theme change and rebuild the UI so canvas.Rectangle colors
			// (which are snapshotted at construction time) get fresh theme values.
			switch ui.prefs.themeMode.Selected {
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
			ui.prefs.savePrefs.SetChecked(true)
			ui.prefs.maxSpeed.SetText("")
			ui.prefs.cookies.SetText("")
			ui.prefs.themeMode.SetSelected("Dark")
			ui.prefs.logLimit.SetSelected("200")
		}, manager.prefsWindow)
	})
	resetBtn.Importance = widget.DangerImportance

	loadConfigBtn := widget.NewButtonWithIcon("Load from Config (govid.json)", theme.SettingsIcon(), func() {
		config, err := manager.onLoadConfigFile(configFileName)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to load govid.json: %v", err), manager.prefsWindow)
			return
		}
		merged, errs := manager.onMergeConfig(config, manager.onLoadPreferences(), ui.download.format.Options, ui.download.quality.Options)
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
		SavePrefs:         ui.prefs.savePrefs.Checked,
		SavedPath:         savePath,
		Format:            ui.download.format.Selected,
		Quality:           ui.download.quality.Selected,
		MaxSpeed:          strings.TrimSpace(ui.prefs.maxSpeed.Text),
		ThemeMode:         ui.prefs.themeMode.Selected,
		CookiesPath:       strings.TrimSpace(ui.prefs.cookies.Text),
		LogLimit:          ui.prefs.logLimit.Selected,
		BatchMode:         ui.download.batchMode.Checked,
		SaveLog:           ui.download.saveLog.Checked,
		Notify:            ui.download.notify.Checked,
		AutoRetry:         ui.download.autoRetry.Checked,
		EnablePostProcess: ui.postProcess.enablePostProcess.Checked,
		SmoothMotion:      ui.postProcess.smoothMotion.Checked,
		SmoothMotionMode:  ui.postProcess.smoothMotionMode.Selected,
		SmoothFPS:         ui.postProcess.smoothMotionFPS.Value,
		Sharpen:           ui.postProcess.sharpen.Checked,
		SharpenAmount:     ui.postProcess.sharpenAmount.Value,
		NormalizeAudio:    ui.postProcess.normalizeAudio.Checked,
		VividMode:         ui.postProcess.vividMode.Checked,
		Denoise:           ui.postProcess.denoise.Checked,
		DenoiseMode:       ui.postProcess.denoiseMode.Selected,
		HDRToSDR:          ui.postProcess.hdrToSdr.Checked,
		Deband:            ui.postProcess.deband.Checked,
		AutoCrop:          ui.postProcess.autoCrop.Checked,
		Stabilize:         ui.postProcess.stabilize.Checked,
		Deinterlace:       ui.postProcess.deinterlace.Checked,
		NightMode:         ui.postProcess.nightMode.Checked,
		UpscaleVideo:      ui.postProcess.upscaleVideo.Checked,
		UpscaleTarget:     ui.postProcess.upscaleTarget.Selected,
		GPUBackend:        ui.postProcess.gpuBackend.Selected,
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
	ui.postProcess.smoothMotion.SetChecked(prefs.SmoothMotion)
	ui.postProcess.smoothMotionMode.Horizontal = true
	ui.postProcess.smoothMotionMode.SetSelected(prefs.SmoothMotionMode)
	ui.postProcess.sharpen.SetChecked(prefs.Sharpen)
	ui.postProcess.sharpenAmount.SetValue(prefs.SharpenAmount)
	ui.postProcess.vividMode.SetChecked(prefs.VividMode)
	ui.postProcess.deband.SetChecked(prefs.Deband)
	ui.postProcess.hdrToSdr.SetChecked(prefs.HDRToSDR)
	ui.postProcess.denoise.SetChecked(prefs.Denoise)
	ui.postProcess.denoiseMode.Horizontal = true
	ui.postProcess.denoiseMode.SetSelected(prefs.DenoiseMode)
	ui.postProcess.deinterlace.SetChecked(prefs.Deinterlace)
	ui.postProcess.stabilize.SetChecked(prefs.Stabilize)
	ui.postProcess.autoCrop.SetChecked(prefs.AutoCrop)
	ui.postProcess.upscaleVideo.SetChecked(prefs.UpscaleVideo)
	ui.postProcess.upscaleTarget.SetSelected(prefs.UpscaleTarget)
	ui.postProcess.normalizeAudio.SetChecked(prefs.NormalizeAudio)
	ui.postProcess.nightMode.SetChecked(prefs.NightMode)
	ui.postProcess.gpuBackend.SetSelected(prefs.GPUBackend)

	// FPS slider for smooth motion — use a bound float so the label updates live.
	fpsBinding := binding.NewFloat()
	fpsBinding.Set(ui.postProcess.smoothMotionFPS.Value)
	fpsLabel := widget.NewLabelWithData(binding.FloatToStringWithFormat(fpsBinding, "%.0f FPS"))
	ui.postProcess.smoothMotionFPS.Step = 1
	ui.postProcess.smoothMotionFPS.OnChanged = func(v float64) {
		fpsBinding.Set(v)
	}
	if !ui.postProcess.smoothMotion.Checked {
		ui.postProcess.smoothMotionMode.Disable()
		ui.postProcess.smoothMotionFPS.Disable()
	}

	// Sharpening slider — bind to float for live label updates.
	sharpenBinding := binding.NewFloat()
	sharpenBinding.Set(ui.postProcess.sharpenAmount.Value)
	sharpenLabel := widget.NewLabelWithData(binding.FloatToStringWithFormat(sharpenBinding, "%.1fx"))
	ui.postProcess.sharpenAmount.Step = 0.1
	if !ui.postProcess.sharpen.Checked {
		ui.postProcess.sharpenAmount.Disable()
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
		upscale := ui.postProcess.upscaleVideo.Checked
		smooth := ui.postProcess.smoothMotion.Checked
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

	ui.postProcess.smoothMotion.OnChanged = func(checked bool) {
		if checked {
			ui.postProcess.smoothMotionMode.Enable()
			ui.postProcess.smoothMotionFPS.Enable()
		} else {
			ui.postProcess.smoothMotionMode.Disable()
			ui.postProcess.smoothMotionFPS.Disable()
		}
		refreshLoad()
	}
	ui.postProcess.smoothMotionMode.OnChanged = func(_ string) { refreshLoad() }

	// Denoise mode is only relevant when denoise is enabled.
	if !ui.postProcess.denoise.Checked {
		ui.postProcess.denoiseMode.Disable()
	}
	ui.postProcess.denoise.OnChanged = func(checked bool) {
		if checked {
			ui.postProcess.denoiseMode.Enable()
		} else {
			ui.postProcess.denoiseMode.Disable()
		}
		refreshLoad()
	}
	ui.postProcess.denoiseMode.OnChanged = func(_ string) { refreshLoad() }

	ui.postProcess.sharpen.OnChanged = func(checked bool) {
		if checked {
			ui.postProcess.sharpenAmount.Enable()
		} else {
			ui.postProcess.sharpenAmount.Disable()
		}
		refreshLoad()
	}
	ui.postProcess.sharpenAmount.OnChanged = func(v float64) {
		sharpenBinding.Set(v)
		refreshLoad()
	}

	// Upscale target is only relevant when upscale is enabled.
	if !ui.postProcess.upscaleVideo.Checked {
		ui.postProcess.upscaleTarget.Disable()
	}
	ui.postProcess.upscaleVideo.OnChanged = func(checked bool) {
		if checked {
			ui.postProcess.upscaleTarget.Enable()
		} else {
			ui.postProcess.upscaleTarget.Disable()
		}
		refreshLoad()
	}
	ui.postProcess.upscaleTarget.OnChanged = func(_ string) { refreshLoad() }

	// Simple toggles — just refresh the load indicator.
	ui.postProcess.vividMode.OnChanged = func(_ bool) { refreshLoad() }
	ui.postProcess.deband.OnChanged = func(_ bool) { refreshLoad() }
	ui.postProcess.hdrToSdr.OnChanged = func(_ bool) { refreshLoad() }
	ui.postProcess.deinterlace.OnChanged = func(_ bool) { refreshLoad() }
	ui.postProcess.stabilize.OnChanged = func(_ bool) { refreshLoad() }
	ui.postProcess.autoCrop.OnChanged = func(_ bool) { refreshLoad() }
	ui.postProcess.normalizeAudio.OnChanged = func(_ bool) { refreshLoad() }
	ui.postProcess.nightMode.OnChanged = func(_ bool) { refreshLoad() }

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
			{Text: "Encoder Backend", Widget: container.New(layout.NewGridWrapLayout(fyne.NewSize(200, ui.postProcess.gpuBackend.MinSize().Height)), ui.postProcess.gpuBackend), HintText: "GPU-accelerated re-encoding; falls back to CPU if unavailable"},
			{Text: "", Widget: sectionDivider()},
			// ── MOTION ─────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("MOTION ENHANCEMENT")},
			{Text: "Smooth Motion", Widget: ui.postProcess.smoothMotion, HintText: "Interpolate frames for fluid playback (slow)"},
			{Text: "Smoothing Mode", Widget: ui.postProcess.smoothMotionMode, HintText: "Precise/Balanced use motion vectors, Fast uses blending"},
			{Text: "Target FPS", Widget: container.NewHBox(container.New(layout.NewGridWrapLayout(fyne.NewSize(200, ui.postProcess.smoothMotionFPS.MinSize().Height)), ui.postProcess.smoothMotionFPS), fpsLabel), HintText: "Standard is 60, cinematic is 24, high-refresh is 120"},
			{Text: "", Widget: sectionDivider()},
			// ── VIDEO ──────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("VIDEO ENHANCEMENT")},
			{Text: "Vivid Mode", Widget: ui.postProcess.vividMode, HintText: "Boost brightness, contrast, and saturation"},
			{Text: "Sharpen Video", Widget: ui.postProcess.sharpen, HintText: "CAS (Contrast Adaptive Sharpening) — sharpens edges without haloing or noise amplification"},
			{Text: "Sharpen Intensity", Widget: container.NewHBox(container.New(layout.NewGridWrapLayout(fyne.NewSize(200, ui.postProcess.sharpenAmount.MinSize().Height)), ui.postProcess.sharpenAmount), sharpenLabel), HintText: "1.0x is gentle, 1.5x is moderate, 2.0x is strong"},
			{Text: "Fix Banding", Widget: ui.postProcess.deband, HintText: "Remove gradient banding steps in skies and dark scenes (deband)"},
			{Text: "HDR to SDR", Widget: ui.postProcess.hdrToSdr, HintText: "Tone-map 4K HDR content for standard monitors (zscale + Hable tonemap)"},
			{Text: "", Widget: sectionDivider()},
			// ── NOISE & ARTIFACTS ───────────────────────────────────────────
			{Text: "", Widget: sectionHeader("NOISE & ARTIFACTS")},
			{Text: "Denoise", Widget: ui.postProcess.denoise, HintText: "HQ noise reduction for low-quality or grainy footage"},
			{Text: "Denoise Mode", Widget: ui.postProcess.denoiseMode, HintText: "NLMeans: highest quality, very slow | hqdn3d: spatial + temporal denoising, fast and effective"},
			{Text: "Deinterlace", Widget: ui.postProcess.deinterlace, HintText: "Remove combing artifacts from archival or TV-rip content (bwdif)"},
			{Text: "Stabilize", Widget: ui.postProcess.stabilize, HintText: "Smooth out shaky handheld footage (deshake)"},
			{Text: "Auto-Crop", Widget: ui.postProcess.autoCrop, HintText: "Detect and remove black letterbox/pillarbox bars automatically"},
			{Text: "", Widget: sectionDivider()},
			// ── UPSCALING ────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("UPSCALING")},
			{Text: "Upscale Video", Widget: ui.postProcess.upscaleVideo, HintText: "Enlarge the video using a high-quality Lanczos resampler"},
			{Text: "Target Resolution", Widget: container.New(layout.NewGridWrapLayout(fyne.NewSize(200, ui.postProcess.upscaleTarget.MinSize().Height)), ui.postProcess.upscaleTarget), HintText: "2× doubles both dimensions; fixed targets set a specific height"},
			{Text: "", Widget: sectionDivider()},
			// ── AUDIO ──────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("AUDIO ENHANCEMENT")},
			{Text: "Normalize Audio", Widget: ui.postProcess.normalizeAudio, HintText: "Loudness normalization via the loudnorm filter"},
			{Text: "Night Mode", Widget: ui.postProcess.nightMode, HintText: "Dynamic compression to balance quiet dialogue and loud effects (dynaudnorm)"},
		},
	}

	applyBtn := widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), func() {
		manager.savePreferences(ui.download.path.Text)
	})

	applyCloseBtn := widget.NewButtonWithIcon("Apply & Close", theme.ConfirmIcon(), func() {
		manager.savePreferences(ui.download.path.Text)
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
	prefs := manager.onLoadPreferences()

	manager.configureEntryMode()
	manager.wireToggleHandlers()
	manager.loadMainWindowState(prefs)
	manager.wireActionButtons()

	header := buildHeader()
	inputCard := manager.buildInputCard()
	statusCard := manager.buildStatusCard()
	logPane := manager.buildLogPane()
	footer := buildFooter()

	topContent := container.NewVBox(
		header,
		inputCard,
		statusCard,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Terminal Output:", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
	)

	content := container.NewBorder(topContent, footer, nil, nil, logPane)
	manager.mainWindow.SetContent(container.NewPadded(content))
}

// buildHeader constructs the app logo/title header shown atop the main window.
func buildHeader() fyne.CanvasObject {
	logo := canvas.NewImageFromResource(resourceAppiconPng)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(128, 128))

	titleText := canvas.NewText("GoVid", accentCyan)
	titleText.TextSize = 38
	titleText.TextStyle = fyne.TextStyle{Bold: true}

	subtitleText := canvas.NewText("Video Downloader", theme.Color(theme.ColorNameDisabled))
	subtitleText.TextSize = 23
	subtitleText.TextStyle = fyne.TextStyle{Italic: true}

	headerLeft := container.NewVBox(titleText, subtitleText)
	return container.NewHBox(headerLeft, layout.NewSpacer(), logo)
}

// configureEntryMode switches the URL entry between single-line and
// multi-line batch mode, and wires the batch-mode toggle's OnChanged handler.
func (manager *UIManager) configureEntryMode() {
	ui := manager.ui

	if ui.download.batchMode.Checked {
		ui.download.entry.MultiLine = true
		ui.download.entry.SetMinRowsVisible(4)
		ui.download.entry.SetPlaceHolder("One URL per line...\nhttps://...\nhttps://...")
	} else {
		ui.download.entry.MultiLine = false
		ui.download.entry.SetMinRowsVisible(1)
		ui.download.entry.SetPlaceHolder("https://www.youtube.com/watch?v=...")
	}
	ui.download.batchMode.OnChanged = func(checked bool) {
		fyne.CurrentApp().Preferences().SetBool("batchMode", checked)
		if !checked {
			// Switching back to single mode: keep only the first non-empty URL.
			first := ""
			for _, line := range strings.Split(ui.download.entry.Text, "\n") {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					first = trimmed
					break
				}
			}
			ui.download.entry.SetText(first)
		}
		manager.createUI()
	}
}

// wireToggleHandlers attaches the OnChanged handlers for the main window's
// checkboxes and path field; all of them simply persist the current preferences.
func (manager *UIManager) wireToggleHandlers() {
	ui := manager.ui

	ui.download.saveLog.OnChanged = func(_ bool) {
		manager.savePreferences(ui.download.path.Text)
	}
	ui.download.notify.OnChanged = func(_ bool) {
		manager.savePreferences(ui.download.path.Text)
	}
	ui.download.autoRetry.OnChanged = func(_ bool) {
		manager.savePreferences(ui.download.path.Text)
	}
	ui.postProcess.enablePostProcess.OnChanged = func(_ bool) {
		manager.savePreferences(ui.download.path.Text)
	}
	ui.download.path.SetPlaceHolder("Download folder...")
	ui.download.path.OnChanged = func(text string) {
		if ui.prefs.savePrefs.Checked {
			fyne.CurrentApp().Preferences().SetString(prefSavedPath, strings.TrimSpace(text))
		}
	}
}

// loadMainWindowState applies the saved path, format, and quality preferences
// to their widgets, falling back to platform/OS defaults when unset.
func (manager *UIManager) loadMainWindowState(prefs AppPreferences) {
	ui := manager.ui

	if prefs.SavedPath != "" {
		ui.download.path.SetText(prefs.SavedPath)
	} else if exePath, err := os.Executable(); err == nil {
		ui.download.path.SetText(filepath.Dir(exePath))
	} else if cwd, err := os.Getwd(); err == nil {
		ui.download.path.SetText(cwd)
	}

	ui.download.format.Options = []string{"MP4", "MKV", "WebM", "MP3", "M4A"}
	if prefs.Format != "" {
		ui.download.format.SetSelected(prefs.Format)
	} else if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		ui.download.format.SetSelected("MP4")
	} else {
		ui.download.format.SetSelected("MKV")
	}

	ui.download.quality.Options = []string{"Best Quality", "1080p", "720p", "480p", "360p"}
	if prefs.Quality != "" {
		ui.download.quality.SetSelected(prefs.Quality)
	} else {
		ui.download.quality.SetSelected("Best Quality")
	}
}

// wireActionButtons configures the download and cancel buttons' icons, text,
// and tap handlers.
func (manager *UIManager) wireActionButtons() {
	ui := manager.ui

	ui.download.downloadBtn.Icon = themedIcon(IconDownload)
	ui.download.downloadBtn.Text = "Download Now!"
	ui.download.downloadBtn.OnTapped = func() {
		manager.onStartDownload()
	}
	ui.download.downloadBtn.Importance = widget.HighImportance
	ui.download.downloadBtn.Refresh()

	ui.download.cancelBtn.Icon = themedIcon(IconCancel)
	ui.download.cancelBtn.Text = "Cancel"
	ui.download.cancelBtn.OnTapped = func() {
		if manager.onRequestCancel() {
			manager.onLog("Download canceled by user.", colWarning)
		}
	}
}

// buildInputCard assembles the "Specify the source and destination" card:
// URL/path entry, format/quality selectors, trim range, and the toggle/action
// button rows. Returns the card wrapped with its decorative accent bar.
func (manager *UIManager) buildInputCard() fyne.CanvasObject {
	ui := manager.ui

	browseBtn := widget.NewButtonWithIcon("", themedIcon(IconFolderOpen), func() {
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			ui.download.path.SetText(filepath.FromSlash(list.Path()))
		}, manager.mainWindow)
	})

	openFolderBtn := widget.NewButtonWithIcon("Open Folder", themedIcon(IconFolder), func() {
		manager.onOpenFolder()
	})

	ui.download.trimStart.SetPlaceHolder("e.g. 00:01:30  (optional)")
	ui.download.trimEnd.SetPlaceHolder("e.g. 00:05:00  (optional)")
	ui.download.trimStart.Validator = validateTimestamp
	ui.download.trimEnd.Validator = validateTimestamp

	inputCard := roundedCard("Specify the source and destination",
		container.NewVBox(
			container.NewHBox(
				widget.NewLabelWithStyle("Video URL:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				layout.NewSpacer(),
				ui.download.batchMode,
			),
			container.NewBorder(nil, nil, nil, widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
				ui.download.entry.SetText("")
			}), ui.download.entry),
			widget.NewLabelWithStyle("Save Destination:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewBorder(nil, nil, nil, browseBtn, ui.download.path),
			container.NewGridWithColumns(2,
				container.NewVBox(
					widget.NewLabelWithStyle("Output Format:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					ui.download.format,
				),
				container.NewVBox(
					widget.NewLabelWithStyle("Max Quality:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					ui.download.quality,
				),
			),
			container.NewGridWithColumns(2,
				container.NewVBox(
					widget.NewLabelWithStyle("Trim Start: (optional)", fyne.TextAlignLeading, fyne.TextStyle{}),
					ui.download.trimStart,
				),
				container.NewVBox(
					widget.NewLabelWithStyle("Trim End: (optional)", fyne.TextAlignLeading, fyne.TextStyle{}),
					ui.download.trimEnd,
				),
			),
			container.NewHBox(ui.download.saveLog, ui.download.notify, ui.download.autoRetry, ui.postProcess.enablePostProcess),
			container.NewGridWithColumns(3, ui.download.downloadBtn, openFolderBtn, ui.download.cancelBtn),
		),
	)
	return container.NewBorder(nil, nil, accentBar(), nil, inputCard)
}

// buildStatusCard assembles the progress bar and status-dot indicator card.
func (manager *UIManager) buildStatusCard() fyne.CanvasObject {
	ui := manager.ui

	// Wrap the status dot in a fixed-size container so the circle renders at 18×18.
	dotContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(18, 18)), ui.download.statusDot)
	statusCard := roundedCard("",
		container.NewVBox(
			ui.download.progress,
			container.NewHBox(dotContainer, ui.download.status),
		),
	)
	return container.NewBorder(nil, nil, accentBar(), nil, statusCard)
}

// buildLogPane creates the scrollable log container and stores it on the
// widget bag so appendLogLine can append to it later.
func (manager *UIManager) buildLogPane() *container.Scroll {
	ui := manager.ui

	ui.download.logList = container.NewVBox()
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(0, 10))
	ui.download.output = container.NewScroll(container.NewVBox(ui.download.logList, spacer))
	ui.download.output.SetMinSize(fyne.NewSize(0, 200))
	return ui.download.output
}

// appendLogLine renders one line in the graphical log view, trimming the
// oldest lines once the buffer limit is exceeded. It is registered on
// DownloaderApp as the onLogLine callback so appendOutput never touches
// widgets directly.
func (manager *UIManager) appendLogLine(line string, col color.Color) {
	ui := manager.ui
	fyne.Do(func() {
		label := canvas.NewText(line, col)
		label.TextSize = theme.TextSize()

		ui.download.logList.Add(label)

		if limit := manager.onLogBufferLimit(); len(ui.download.logList.Objects) > limit {
			ui.download.logList.Objects = ui.download.logList.Objects[len(ui.download.logList.Objects)-limit:]
		}

		ui.download.logList.Refresh()
		ui.download.output.ScrollToBottom()
	})
}

// buildFooter constructs the copyright line shown at the bottom of the main window.
func buildFooter() fyne.CanvasObject {
	copyright := canvas.NewText("GoVid • By David Bennehag (dunder.gg) • Built with ❤️, 🤖 and ☕", theme.Color(theme.ColorNameDisabled))
	copyright.TextSize = 14
	copyright.Alignment = fyne.TextAlignCenter
	return container.NewCenter(copyright)
}
