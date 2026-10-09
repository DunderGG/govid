// postprocess_window.go — The Post-Processing window.
//
// Responsibilities:
//   - UIManager.showPostProcessing: the post-processing settings window,
//     composed from the form (buildPostProcessForm), the processing load
//     indicator (buildLoadIndicator), and the footer
//     (buildPostProcessFooter).
//   - wirePostProcessHandlers, bindDependents: enabling each filter's
//     sub-controls with its toggle and refreshing the load indicator.
//   - sizeWarning: the output size warning for upscaling and smooth motion.
//
// The settings themselves, and the filters built from them, are in
// postprocess.go.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// showPostProcessing opens a window for specialized hardware/software filters.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showPostProcessing() {
	if focusOrCreate(&manager.ppWindow) {
		return
	}

	// Reload the saved values so the window never shows edits that were
	// discarded by closing it without applying.
	applyPostProcessPrefs(manager.ui, manager.onLoadPreferences())

	// Live readouts of the two sliders' values, shown beside them.
	fpsValue := binding.NewFloat()
	sharpenValue := binding.NewFloat()

	loadIndicator, refreshLoad := manager.buildLoadIndicator()
	manager.wirePostProcessHandlers(refreshLoad, fpsValue, sharpenValue)
	refreshLoad() // seed with the current state

	title := widget.NewLabelWithStyle("Post-Processing Filters", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	scroll := container.NewScroll(manager.buildPostProcessForm(fpsValue, sharpenValue))
	footer := manager.buildPostProcessFooter(loadIndicator)
	// Border layout: title pinned top, footer pinned bottom, scroll fills the rest.
	content := container.NewBorder(title, footer, nil, nil, scroll)

	manager.ppWindow = fyne.CurrentApp().NewWindow("Post-Processing Settings")
	manager.ppWindow.SetContent(container.NewPadded(content))
	manager.ppWindow.Resize(fyne.NewSize(680, 580))
	manager.ppWindow.SetFixedSize(false)
	manager.ppWindow.SetOnClosed(onWindowClosed(&manager.ppWindow))
	closeOnEscape(manager.ppWindow)
	manager.ppWindow.Show()
}

// wirePostProcessHandlers attaches the Post-Processing window's OnChanged
// handlers: every control calls refresh so the load indicator stays live,
// the slider readouts track their sliders, and each option's sub-controls
// are enabled only while the option is checked.
func (manager *UIManager) wirePostProcessHandlers(refresh func(), fpsValue, sharpenValue binding.Float) {
	pp := manager.ui.postProcess

	bindDependents(pp.smoothMotion, refresh, pp.smoothMotionMode, pp.smoothMotionFPS)
	bindDependents(pp.sharpen, refresh, pp.sharpenAmount)
	bindDependents(pp.denoise, refresh, pp.denoiseMode)
	bindDependents(pp.upscaleVideo, refresh, pp.upscaleTarget)

	fpsValue.Set(pp.smoothMotionFPS.Value)
	pp.smoothMotionFPS.OnChanged = func(v float64) {
		fpsValue.Set(v)
	}
	sharpenValue.Set(pp.sharpenAmount.Value)
	pp.sharpenAmount.OnChanged = func(v float64) {
		sharpenValue.Set(v)
		refresh()
	}

	onSelect := func(_ string) { refresh() }
	pp.smoothMotionMode.OnChanged = onSelect
	pp.denoiseMode.OnChanged = onSelect
	pp.upscaleTarget.OnChanged = onSelect

	for _, toggle := range []*widget.Check{
		pp.vividMode, pp.deband, pp.hdrToSdr, pp.deinterlace,
		pp.stabilize, pp.autoCrop, pp.normalizeAudio, pp.nightMode,
	} {
		toggle.OnChanged = func(_ bool) { refresh() }
	}
}

// bindDependents enables dependents only while toggle is checked, applying
// the toggle's current state immediately, and calls refresh after every
// change of the toggle.
func bindDependents(toggle *widget.Check, refresh func(), dependents ...fyne.Disableable) {
	setEnabled := func(enabled bool) {
		for _, dependent := range dependents {
			if enabled {
				dependent.Enable()
			} else {
				dependent.Disable()
			}
		}
	}
	setEnabled(toggle.Checked)
	toggle.OnChanged = func(checked bool) {
		setEnabled(checked)
		refresh()
	}
}

// buildLoadIndicator builds the live "Estimated Processing Load" section:
// a row of coloured blocks that light up as the cost of the selected filters
// passes each of loadBlockThresholds, a description of the load, and a file
// size warning. The returned refresh function recomputes all three from the
// current widget state.
func (manager *UIManager) buildLoadIndicator() (fyne.CanvasObject, func()) {
	blocks := make([]*canvas.Rectangle, len(loadBlockThresholds))
	blockBar := container.NewGridWithColumns(len(blocks))
	for i := range blocks {
		block := canvas.NewRectangle(colLoadEmpty)
		block.SetMinSize(fyne.NewSize(0, 14))
		block.CornerRadius = 3
		blocks[i] = block
		blockBar.Add(block)
	}

	loadDesc := binding.NewString()
	loadLabel := widget.NewLabelWithData(loadDesc)
	loadLabel.Alignment = fyne.TextAlignCenter

	sizeWarn := binding.NewString()
	sizeWarnLabel := widget.NewLabelWithData(sizeWarn)
	sizeWarnLabel.Alignment = fyne.TextAlignCenter
	sizeWarnLabel.TextStyle = fyne.TextStyle{Italic: true}
	sizeWarnLabel.Wrapping = fyne.TextWrapWord

	pp := manager.ui.postProcess
	refresh := func() {
		cost, desc := computeProcessingLoad(newPostProcessSettings(manager.ui))
		loadDesc.Set(desc)
		for i, block := range blocks {
			if cost > loadBlockThresholds[i] {
				block.FillColor = colLoadPalette[i]
			} else {
				block.FillColor = colLoadEmpty
			}
			block.Refresh()
		}
		sizeWarn.Set(sizeWarning(pp.upscaleVideo.Checked, pp.smoothMotion.Checked))
	}

	indicator := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Estimated Processing Load", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		blockBar,
		loadLabel,
		sizeWarnLabel,
	)
	return indicator, refresh
}

// sizeWarning returns the file-size warning for the filters that add pixels
// (upscale) or frames (smooth motion), or "" when neither is selected.
func sizeWarning(upscale, smooth bool) string {
	switch {
	case upscale && smooth:
		return "⚠ Upscaling + Smooth Motion will greatly increase file size"
	case upscale:
		return "⚠ Upscaling significantly increases file size (bigger frames)"
	case smooth:
		return "⚠ Smooth Motion increases file size (more frames)"
	default:
		return ""
	}
}

// buildPostProcessForm lays out the Post-Processing window's filter controls
// in titled sections. fpsValue and sharpenValue back the slider readouts.
func (manager *UIManager) buildPostProcessForm(fpsValue, sharpenValue binding.Float) *widget.Form {
	pp := manager.ui.postProcess

	fpsLabel := widget.NewLabelWithData(binding.FloatToStringWithFormat(fpsValue, "%.0f FPS"))
	sharpenLabel := widget.NewLabelWithData(binding.FloatToStringWithFormat(sharpenValue, "%.1fx"))

	return &widget.Form{
		Items: []*widget.FormItem{
			// ── GPU ACCELERATION ─────────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("GPU ACCELERATION")},
			{Text: "Encoder Backend", Widget: fixedWidth(pp.gpuBackend, 200), HintText: "GPU-accelerated re-encoding; falls back to CPU if unavailable"},
			{Text: "", Widget: sectionDivider()},
			// ── MOTION ─────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("MOTION ENHANCEMENT")},
			{Text: "Smooth Motion", Widget: pp.smoothMotion, HintText: "Interpolate frames for fluid playback (slow)"},
			{Text: "Smoothing Mode", Widget: pp.smoothMotionMode, HintText: "Precise/Balanced use motion vectors, Fast uses blending"},
			{Text: "Target FPS", Widget: container.NewHBox(fixedWidth(pp.smoothMotionFPS, 200), fpsLabel), HintText: "Standard is 60, cinematic is 24, high-refresh is 120"},
			{Text: "", Widget: sectionDivider()},
			// ── VIDEO ──────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("VIDEO ENHANCEMENT")},
			{Text: "Vivid Mode", Widget: pp.vividMode, HintText: "Boost brightness, contrast, and saturation"},
			{Text: "Sharpen Video", Widget: pp.sharpen, HintText: "CAS (Contrast Adaptive Sharpening) — sharpens edges without haloing or noise amplification"},
			{Text: "Sharpen Intensity", Widget: container.NewHBox(fixedWidth(pp.sharpenAmount, 200), sharpenLabel), HintText: "1.0x is gentle, 1.5x is moderate, 2.0x is strong"},
			{Text: "Fix Banding", Widget: pp.deband, HintText: "Remove gradient banding steps in skies and dark scenes (deband)"},
			{Text: "HDR to SDR", Widget: pp.hdrToSdr, HintText: "Tone-map HDR (PQ/HLG) videos for standard monitors; SDR videos are left unchanged"},
			{Text: "", Widget: sectionDivider()},
			// ── NOISE & ARTIFACTS ───────────────────────────────────────────
			{Text: "", Widget: sectionHeader("NOISE & ARTIFACTS")},
			{Text: "Denoise", Widget: pp.denoise, HintText: "HQ noise reduction for low-quality or grainy footage"},
			{Text: "Denoise Mode", Widget: pp.denoiseMode, HintText: "NLMeans: highest quality, very slow | hqdn3d: spatial + temporal denoising, fast and effective"},
			{Text: "Deinterlace", Widget: pp.deinterlace, HintText: "Remove combing artifacts from archival or TV-rip content (bwdif)"},
			{Text: "Stabilize", Widget: pp.stabilize, HintText: "Smooth out shaky handheld footage (deshake)"},
			{Text: "Auto-Crop", Widget: pp.autoCrop, HintText: "Detect and remove black letterbox/pillarbox bars automatically"},
			{Text: "", Widget: sectionDivider()},
			// ── UPSCALING ────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("UPSCALING")},
			{Text: "Upscale Video", Widget: pp.upscaleVideo, HintText: "Enlarge the video using a high-quality Lanczos resampler"},
			{Text: "Target Resolution", Widget: fixedWidth(pp.upscaleTarget, 200), HintText: "2× doubles both dimensions; fixed targets set a specific height"},
			{Text: "", Widget: sectionDivider()},
			// ── AUDIO ──────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("AUDIO ENHANCEMENT")},
			{Text: "Normalize Audio", Widget: pp.normalizeAudio, HintText: "Loudness normalization via the loudnorm filter"},
			{Text: "Night Mode", Widget: pp.nightMode, HintText: "Dynamic compression to balance quiet dialogue and loud effects (dynaudnorm)"},
		},
	}
}

// buildPostProcessFooter assembles the area pinned below the filter form:
// the load indicator, the Apply / Apply & Close buttons, and the re-encode
// notice.
func (manager *UIManager) buildPostProcessFooter(loadIndicator fyne.CanvasObject) fyne.CanvasObject {
	applyBtn := widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), func() {
		manager.savePreferences(manager.ui.download.path.Text)
	})

	applyCloseBtn := widget.NewButtonWithIcon("Apply & Close", theme.ConfirmIcon(), func() {
		manager.savePreferences(manager.ui.download.path.Text)
		manager.ppWindow.Close()
	})
	applyCloseBtn.Importance = widget.HighImportance

	buttons := container.NewGridWithColumns(2, applyBtn, applyCloseBtn)
	notice := widget.NewLabelWithStyle("⚠️ Most filters require FFmpeg and trigger a full re-encode.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})

	return container.NewVBox(loadIndicator, widget.NewSeparator(), buttons, notice)
}
