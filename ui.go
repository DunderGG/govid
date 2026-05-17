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
		fyne.NewMenu("Tools", updateMenu, prefsMenu, fyne.NewMenuItem("Post-Processing", func() {
			app.showPostProcessing()
		})),
		fyne.NewMenu("Help", configHelpMenu, fyne.NewMenuItemSeparator(), aboutMenu),
	)
	app.window.SetMainMenu(mainMenu)
}

// showPostProcessing opens a window for specialized hardware/software filters.
func (app *DownloaderApp) showPostProcessing() {
	if app.ppWindow != nil {
		app.ppWindow.RequestFocus()
		return
	}

	ui := app.ui
	prefs := fyne.CurrentApp().Preferences()

	// Reload all post-processing prefs so the window always shows persisted state.
	ui.smoothMotion.SetChecked(prefs.Bool("upscale"))
	ui.smoothMotionMode.Horizontal = true
	ui.smoothMotionMode.SetSelected(prefs.StringWithFallback("smoothMotionMode", "Balanced"))
	ui.sharpen.SetChecked(prefs.Bool("sharpen"))
	ui.sharpenAmount.SetValue(prefs.FloatWithFallback("sharpenAmount", 1.0))
	ui.vividMode.SetChecked(prefs.Bool("vividMode"))
	ui.deband.SetChecked(prefs.Bool("deband"))
	ui.hdrToSdr.SetChecked(prefs.Bool("hdrToSdr"))
	ui.denoise.SetChecked(prefs.Bool("denoise"))
	ui.denoiseMode.Horizontal = true
	ui.denoiseMode.SetSelected(prefs.StringWithFallback("denoiseMode", "hqdn3d (Balanced)"))
	ui.deinterlace.SetChecked(prefs.Bool("deinterlace"))
	ui.stabilize.SetChecked(prefs.Bool("stabilize"))
	ui.autoCrop.SetChecked(prefs.Bool("autoCrop"))
	ui.upscaleVideo.SetChecked(prefs.Bool("upscaleVideo"))
	ui.upscaleTarget.SetSelected(prefs.StringWithFallback("upscaleTarget", "2× (Double)"))
	ui.normalizeAudio.SetChecked(prefs.Bool("normalize"))
	ui.nightMode.SetChecked(prefs.Bool("nightMode"))

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
			{Text: "Sharpen Video", Widget: ui.sharpen, HintText: "Apply unsharp mask to restore edge detail"},
			{Text: "Sharpen Intensity", Widget: container.NewBorder(nil, nil, nil, sharpenLabel, ui.sharpenAmount), HintText: "0.5x is subtle, 1.0x is default, 2.0x is aggressive"},
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
		app.ppWindow.Close()
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

	app.ppWindow = fyne.CurrentApp().NewWindow("Post-Processing Settings")
	app.ppWindow.SetContent(container.NewPadded(content))
	app.ppWindow.Resize(fyne.NewSize(600, 580))
	app.ppWindow.SetFixedSize(false)
	app.ppWindow.SetOnClosed(func() {
		app.ppWindow = nil
	})
	app.ppWindow.Show()
}

// showPreferences opens a window for general application settings.
func (app *DownloaderApp) showPreferences() {
	if app.prefsWindow != nil {
		app.prefsWindow.RequestFocus()
		return
	}

	ui := app.ui
	prefs := fyne.CurrentApp().Preferences()

	// Speed Limit field
	ui.maxSpeed.SetPlaceHolder("e.g. 5M (Unlimited if blank)")
	ui.maxSpeed.SetText(prefs.String("maxSpeed"))

	// Theme Mode field — horizontal radio group for a simple two-option toggle.
	ui.themeMode.Horizontal = true
	ui.themeMode.SetSelected(prefs.StringWithFallback("themeMode", "Dark"))

	// Cookies field
	ui.cookies.SetPlaceHolder("Path to cookies.txt (optional)")
	ui.cookies.SetText(prefs.String("cookiesPath"))

	cookiesBrowse := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			ui.cookies.SetText(reader.URI().Path())
			reader.Close()
		}, app.prefsWindow)
		// Filter for common cookie file extensions
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".txt", ".cookies", ".dat"}))
		fileDialog.Show()
	})
	cookiesClear := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		ui.cookies.SetText("")
	})
	cookiesRow := container.NewBorder(nil, nil, nil, container.NewHBox(cookiesBrowse, cookiesClear), ui.cookies)

	// Save Preferences toggle.
	ui.savePrefs.SetChecked(prefs.BoolWithFallback("savePrefs", true))

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Save Preferences", Widget: ui.savePrefs, HintText: "Remember format, quality, path, speed, and theme between sessions"},
			{Text: "Max Download Speed", Widget: ui.maxSpeed, HintText: "Limits download rate (e.g. 50K, 5M, 10G)"},
			{Text: "Application Theme", Widget: ui.themeMode, HintText: "Restart may be required for some changes"},
			{Text: "Cookies File", Widget: cookiesRow, HintText: "Path to a Mozilla/Netscape-format cookies.txt file"},
		},
		OnSubmit: func() {
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
		}, app.prefsWindow)
	})
	resetBtn.Importance = widget.DangerImportance

	loadConfigBtn := widget.NewButtonWithIcon("Load from Config (govid.json)", theme.SettingsIcon(), func() {
		config, err := app.loadConfigFromFile()
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to load govid.json: %v", err), app.prefsWindow)
			return
		}
		err = app.applyConfig(config)
		if err != nil {
			dialog.ShowCustom("Config Loaded with Warnings", "OK", widget.NewLabel(err.Error()), app.prefsWindow)
		} else {
			dialog.ShowInformation("Config Loaded", "Preferences updated from govid.json", app.prefsWindow)
		}
	})

	app.prefsWindow = fyne.CurrentApp().NewWindow("Preferences")
	app.prefsWindow.SetContent(container.NewPadded(container.NewVBox(
		form,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, loadConfigBtn, resetBtn),
	)))
	app.prefsWindow.Resize(fyne.NewSize(500, 360))
	app.prefsWindow.SetOnClosed(func() {
		app.prefsWindow = nil
	})
	app.prefsWindow.Show()
}

// showConfigHelp opens a scrollable window explaining all configuration options.
func (app *DownloaderApp) showConfigHelp() {
	if app.helpWindow != nil {
		app.helpWindow.RequestFocus()
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
		{"Allow Duplicate Downloads", "When checked, a timestamp is added to the filename so re-downloading the same video does not overwrite the previous file."},
		{"Save output to log file", "When checked, everything printed in the Terminal Output panel is also saved to a **GoVid_log.txt** file in your save destination folder."},
		{"Notify on Completion", "When checked, a system notification is sent when a download finishes (success or failure), but not when cancelled."},
		{"Save Preferences", "Found in **Tools → Preferences**. When checked, GoVid remembers your format, quality, save path, speed limit, and theme between sessions. The toggle itself is always remembered so the choice survives a restart."},
		{"Max Download Speed", "Found in **Tools → Preferences**. Limits the bandwidth used by GoVid to prevent network saturation. Examples:\n  * `50K` – Very slow (dial-up speed)\n  * `5M` – Moderate (standard HD streaming speed)\n  * `10G` – Virtually unlimited\n\nLeave blank to use full available bandwidth."},
		{"Cookies File", "Found in **Tools → Preferences**. Path to a `cookies.txt` file in Mozilla/Netscape format. Required for access to restricted, private, or age-gated videos.\n\n⚠️ **Security Warning**: Cookie files contain sensitive session data. Never share this file or let it fall into unauthorized hands. Use a trusted browser extension (like 'Get cookies.txt LOCALLY') to export your active session."},
		{"Post-Processing", "Found in **Tools → Preferences**. Enhance your downloads using FFmpeg:\n  * **Smooth Motion** – interpolates frames to 60fps for smoother playback\n  * **Sharpen Video** – applies an unsharp mask to restore edge detail in compressed videos\n  * **Normalize Audio** – balances volume levels using the `loudnorm` filter\n\n---\n\nThe **Smooth Motion Mode** controls which interpolation method is used:\n\n  * **Precise (slow)** – Full motion-compensated interpolation (`mi_mode=mci`). Analyses motion vectors between every pair of frames to synthesize new ones. Produces the smoothest and most accurate result, but is almost entirely single-threaded — expect one CPU core pegged at 100% for the full duration. Best for short clips or when quality is the priority.\n\n  * **Balanced** – A faster variant of MCI that disables variant-size block motion compensation (`vsbmc=0`) and uses overlapped block motion compensation (`mc_mode=obmc`). Roughly 40% faster than Precise with very similar visual quality. A good default choice for most videos.\n\n  * **Fast** – Frame blending (`mi_mode=blend`). Instead of computing motion vectors, it cross-fades adjacent frames to generate the in-between frame. Much faster and fully multi-threaded, so it will use all available CPU cores. The result is slightly softer on fast-motion content, but imperceptible on most videos."},
		{"Cancel", "Stops the active download immediately. In batch mode, it skips the current URL and moves on to the next one."},
		{"Open Folder", "Opens your chosen save destination in the system file manager."},
		{"JSON Configuration", "For advanced users, GoVid supports loading settings from a `govid.json` file located in the application folder.\n\n**Format Example:**\n```json\n{\n  \"path\": \"C:\\\\Downloads\",\n  \"format\": \"MP4\",\n  \"quality\": \"1080p\",\n  \"maxSpeed\": \"5M\"\n}\n```\nTo apply changes made to this file, go to **Tools → Preferences** and click **Load from Config (govid.json)**. Note that standard JSON does not support comments; adding them will cause a loading error.\n\n**Supported Values:**\n* **format**: `MP4`, `MKV`, `WebM`, `MP3`, `M4A`\n* **quality**: `Best Quality`, `1080p`, `720p`, `480p`, `360p`\n* **path**: Any valid absolute folder path\n* **maxSpeed**: Numeric value with unit, e.g., `50K`, `5M`, `1G` (or blank for unlimited)"},
	}

	content := container.NewVBox()
	for _, item := range items {
		title := widget.NewRichTextFromMarkdown("### " + item.label)
		title.Wrapping = fyne.TextWrapOff

		body := widget.NewRichTextFromMarkdown(item.desc)
		for segIdx := range body.Segments {
			if segment, ok := body.Segments[segIdx].(*widget.TextSegment); ok {
				// Make bold text use the theme's primary color for better visual hierarchy
				if segment.Style.TextStyle.Bold {
					segment.Style.ColorName = theme.ColorNamePrimary
				}
				// Ensure code segments have slightly more visibility
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

	app.helpWindow = fyne.CurrentApp().NewWindow("GoVid Guide")
	app.helpWindow.SetContent(container.NewPadded(scroll))
	app.helpWindow.Resize(fyne.NewSize(550, 500))
	app.helpWindow.SetOnClosed(func() {
		app.helpWindow = nil
	})
	app.helpWindow.Show()
}

// showPostProcessingButton adds a button to the main UI to open the PP window.
func (app *DownloaderApp) getPostProcessingButton() *widget.Button {
	return widget.NewButtonWithIcon("Post-Processing", theme.SettingsIcon(), func() {
		app.showPostProcessing()
	})
}

// showAbout opens a small window with information about the creator and the app.
func (app *DownloaderApp) showAbout() {
	if app.aboutWindow != nil {
		app.aboutWindow.RequestFocus()
		return
	}

	logo := canvas.NewImageFromResource(resourceAppiconPng)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(80, 80))

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

	app.aboutWindow = fyne.CurrentApp().NewWindow("About GoVid")
	app.aboutWindow.SetContent(container.NewPadded(content))
	app.aboutWindow.Resize(fyne.NewSize(360, 280))
	app.aboutWindow.SetFixedSize(true)
	app.aboutWindow.SetOnClosed(func() {
		app.aboutWindow = nil
	})
	app.aboutWindow.Show()
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
		fyne.CurrentApp().Preferences().SetBool("notify", checked)
	}
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
			container.NewHBox(ui.saveLog, ui.notify),
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
