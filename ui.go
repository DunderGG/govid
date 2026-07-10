// ui.go — Builds and manages every visual element of the GoVid window.
//
// Responsibilities:
//   - Main window layout: URL input, format/quality selectors, trim fields,
//     speed limit, checkboxes, progress bar, and scrollable log view.
//   - Menu bar: File, Tools, and Help menus with all their actions.
//   - Dialogs: Preferences window, About window, and the Config Guide.
package main

import (
	"fmt"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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

// createMainMenu builds the application's top-level menu bar.
func (app *DownloaderApp) createMainMenu() {
	historyMenu := fyne.NewMenuItem("History", func() {
		app.showHistory()
	})

	updateMenu := fyne.NewMenuItem("Update yt-dlp", func() {
		dialog.ShowConfirm("Update yt-dlp", "This will run 'yt-dlp -U' to update the tool. Continue?", func(ok bool) {
			if ok {
				app.runUpdateInUI()
			}
		}, app.window)
	})

	prefsMenu := fyne.NewMenuItem("Preferences", func() {
		app.showPreferences()
	})

	configHelpMenu := fyne.NewMenuItem("GoVid Guide", func() {
		app.showConfigHelp()
	})

	aboutMenu := fyne.NewMenuItem("About GoVid", func() {
		app.showAbout()
	})

	mainMenu := fyne.NewMainMenu(
		fyne.NewMenu("File", historyMenu),
		fyne.NewMenu("Tools", updateMenu, prefsMenu, fyne.NewMenuItem("Post-Processing", func() {
			app.showPostProcessing()
		})),
		fyne.NewMenu("Help", configHelpMenu, fyne.NewMenuItemSeparator(), aboutMenu),
	)
	app.window.SetMainMenu(mainMenu)
}

// showHistory delegates to UIManager which owns the window state.
func (app *DownloaderApp) showHistory() {
	app.uiManager.showHistory()
}

// showPostProcessing opens a window for specialized hardware/software filters.
func (app *DownloaderApp) showPostProcessing() {
	if app.uiManager.ppWindow != nil {
		app.uiManager.ppWindow.RequestFocus()
		return
	}

	ui := app.ui
	prefs := app.prefSvc.Load()

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
	blockEmpty := color.RGBA{R: 55, G: 55, B: 65, A: 255}
	blockColors := []color.RGBA{
		{R: 0, G: 210, B: 90, A: 255},  // green
		{R: 140, G: 210, B: 0, A: 255}, // yellow-green
		{R: 230, G: 185, B: 0, A: 255}, // yellow
		{R: 230, G: 100, B: 0, A: 255}, // orange
		{R: 220, G: 45, B: 45, A: 255}, // red
	}
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
		cost, desc := app.computeProcessingLoad()
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
	ui.vividMode.OnChanged    = func(_ bool) { refreshLoad() }
	ui.deband.OnChanged       = func(_ bool) { refreshLoad() }
	ui.hdrToSdr.OnChanged     = func(_ bool) { refreshLoad() }
	ui.deinterlace.OnChanged  = func(_ bool) { refreshLoad() }
	ui.stabilize.OnChanged    = func(_ bool) { refreshLoad() }
	ui.autoCrop.OnChanged     = func(_ bool) { refreshLoad() }
	ui.normalizeAudio.OnChanged = func(_ bool) { refreshLoad() }
	ui.nightMode.OnChanged    = func(_ bool) { refreshLoad() }

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
			// ── MOTION ────────────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("MOTION ENHANCEMENT")},
			{Text: "Smooth Motion", Widget: ui.smoothMotion, HintText: "Interpolate frames for fluid playback (slow)"},
			{Text: "Smoothing Mode", Widget: ui.smoothMotionMode, HintText: "Precise/Balanced use motion vectors, Fast uses blending"},
			{Text: "Target FPS", Widget: container.NewBorder(nil, nil, nil, fpsLabel, ui.smoothMotionFPS), HintText: "Standard is 60, cinematic is 24, high-refresh is 120"},
			{Text: "", Widget: sectionDivider()},
			// ── VIDEO ─────────────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("VIDEO ENHANCEMENT")},
			{Text: "Vivid Mode", Widget: ui.vividMode, HintText: "Boost brightness, contrast, and saturation"},
			{Text: "Sharpen Video", Widget: ui.sharpen, HintText: "CAS (Contrast Adaptive Sharpening) — sharpens edges without haloing or noise amplification"},
			{Text: "Sharpen Intensity", Widget: container.NewBorder(nil, nil, nil, sharpenLabel, ui.sharpenAmount), HintText: "1.0x is gentle, 1.5x is moderate, 2.0x is strong"},
			{Text: "Fix Banding", Widget: ui.deband, HintText: "Remove gradient banding steps in skies and dark scenes (deband)"},
			{Text: "HDR to SDR", Widget: ui.hdrToSdr, HintText: "Tone-map 4K HDR content for standard monitors (zscale + Hable tonemap)"},
			{Text: "", Widget: sectionDivider()},
			// ── NOISE & ARTIFACTS ─────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("NOISE & ARTIFACTS")},
			{Text: "Denoise", Widget: ui.denoise, HintText: "HQ noise reduction for low-quality or grainy footage"},
			{Text: "Denoise Mode", Widget: ui.denoiseMode, HintText: "NLMeans: highest quality, very slow | hqdn3d: spatial + temporal denoising, fast and effective"},
			{Text: "Deinterlace", Widget: ui.deinterlace, HintText: "Remove combing artifacts from archival or TV-rip content (bwdif)"},
			{Text: "Stabilize", Widget: ui.stabilize, HintText: "Smooth out shaky handheld footage (deshake)"},
			{Text: "Auto-Crop", Widget: ui.autoCrop, HintText: "Detect and remove black letterbox/pillarbox bars automatically"},
			{Text: "", Widget: sectionDivider()},
			// ── UPSCALING ─────────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("UPSCALING")},
			{Text: "Upscale Video", Widget: ui.upscaleVideo, HintText: "Enlarge the video using a high-quality Lanczos resampler"},
			{Text: "Target Resolution", Widget: ui.upscaleTarget, HintText: "2× doubles both dimensions; fixed targets set a specific height"},
			{Text: "", Widget: sectionDivider()},
			// ── AUDIO ─────────────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("AUDIO ENHANCEMENT")},
			{Text: "Normalize Audio", Widget: ui.normalizeAudio, HintText: "Loudness normalization via the loudnorm filter"},
			{Text: "Night Mode", Widget: ui.nightMode, HintText: "Dynamic compression to balance quiet dialogue and loud effects (dynaudnorm)"},
		},
	}

	applyBtn := widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), func() {
		app.savePreferences(ui.path.Text)
	})

	applyCloseBtn := widget.NewButtonWithIcon("Apply & Close", theme.ConfirmIcon(), func() {
		app.savePreferences(ui.path.Text)
		app.uiManager.ppWindow.Close()
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

	app.uiManager.ppWindow = fyne.CurrentApp().NewWindow("Post-Processing Settings")
	app.uiManager.ppWindow.SetContent(container.NewPadded(content))
	app.uiManager.ppWindow.Resize(fyne.NewSize(680, 580))
	app.uiManager.ppWindow.SetFixedSize(false)
	app.uiManager.ppWindow.SetOnClosed(func() {
		app.uiManager.ppWindow = nil
	})
	app.uiManager.ppWindow.Show()
}

// showPreferences opens a window for general application settings.
func (app *DownloaderApp) showPreferences() {
	if app.uiManager.prefsWindow != nil {
		app.uiManager.prefsWindow.RequestFocus()
		return
	}

	ui := app.ui
	prefs := app.prefSvc.Load()

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
		}, app.uiManager.prefsWindow)
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
			app.logSvc.SetBufferLimit(ParseBufferLimit(ui.logLimit.Selected))
			app.savePreferences(ui.path.Text)

			// Apply theme change and rebuild the UI so canvas.Rectangle colors
			// (which are snapshotted at construction time) get fresh theme values.
			switch ui.themeMode.Selected {
			case "Light":
				fyne.CurrentApp().Settings().SetTheme(&lightTheme{})
			default:
				fyne.CurrentApp().Settings().SetTheme(&darkTheme{})
			}
			app.createUI()
		},
	}

	resetBtn := widget.NewButton("Restore Defaults", func() {
		dialog.ShowConfirm("Restore Defaults", "Reset all preferences to their default values?", func(ok bool) {
			if !ok {
				return
			}
			app.resetPreferences()
			ui.savePrefs.SetChecked(true)
			ui.maxSpeed.SetText("")
			ui.cookies.SetText("")
			ui.themeMode.SetSelected("Dark")
			ui.logLimit.SetSelected("200")
		}, app.uiManager.prefsWindow)
	})
	resetBtn.Importance = widget.DangerImportance

	loadConfigBtn := widget.NewButtonWithIcon("Load from Config (govid.json)", theme.SettingsIcon(), func() {
		config, err := loadConfigFile(configFileName)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to load govid.json: %v", err), app.uiManager.prefsWindow)
			return
		}
		err = app.applyConfig(config)
		if err != nil {
			dialog.ShowCustom("Config Loaded with Warnings", "OK", widget.NewLabel(err.Error()), app.uiManager.prefsWindow)
		} else {
			dialog.ShowInformation("Config Loaded", "Preferences updated from govid.json", app.uiManager.prefsWindow)
		}
	})

	app.uiManager.prefsWindow = fyne.CurrentApp().NewWindow("Preferences")
	app.uiManager.prefsWindow.SetContent(container.NewPadded(container.NewVBox(
		form,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, loadConfigBtn, resetBtn),
	)))
	app.uiManager.prefsWindow.Resize(fyne.NewSize(500, 360))
	app.uiManager.prefsWindow.SetOnClosed(func() {
		app.uiManager.prefsWindow = nil
	})
	app.uiManager.prefsWindow.Show()
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

// parseURL is a small helper to safely parse a URL string for use in hyperlinks.
func parseURL(rawURL string) *url.URL {
	parsed, _ := url.Parse(rawURL)
	return parsed
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
	ui.saveLog.OnChanged = func(checked bool) {
		fyne.CurrentApp().Preferences().SetBool("saveLog", checked)
	}
	ui.notify.OnChanged = func(checked bool) {
		fyne.CurrentApp().Preferences().SetBool(prefNotify, checked)
	}
	ui.autoRetry.OnChanged = func(checked bool) {
		fyne.CurrentApp().Preferences().SetBool(prefAutoRetry, checked)
	}
	ui.enablePostProcess.OnChanged = func(checked bool) {
		fyne.CurrentApp().Preferences().SetBool(prefEnablePostProcess, checked)
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
		if app.cancelFn != nil {
			app.cancelFn()
			app.appendOutput("Download canceled by user.", color.RGBA{R: 255, G: 165, B: 0, A: 255})
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
