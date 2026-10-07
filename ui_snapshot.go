// ui_snapshot.go — Translators between the UIWidgets bag and plain value structs.
//
// Everything that reads widget state into a value struct, or writes a value
// struct back into widgets, lives here. Services and engines only ever see the
// resulting values (AppPreferences, PostProcessSettings, SessionConfig), so
// they never depend on *UIWidgets.
//
// Widget → value:
//   - snapshotPreferences:    AppPreferences for PreferenceService.Save.
//   - newPostProcessSettings: PostProcessSettings for the filter builders.
//   - newSessionConfig:       SessionConfig for LogService.WriteSessionConfig.
//
// Value → widget:
//   - applyPreferencesToWidgets: called at startup, after loading govid.json,
//     and by Restore Defaults. It is composed of applyMainWindowPrefs,
//     applyGeneralPrefs, and applyPostProcessPrefs; the Preferences and
//     Post-Processing dialogs call their own group when they open.
package main

import "strings"

// snapshotPreferences collects the current widget state into an
// AppPreferences value. savePath is passed explicitly because callers may
// persist a path that has not yet been written to the path entry.
func snapshotPreferences(ui *UIWidgets, savePath string) AppPreferences {
	return AppPreferences{
		SavePrefs:         ui.prefs.savePrefs.Checked,
		SavedPath:         savePath,
		Format:            ui.download.format.Selected,
		Quality:           ui.download.quality.Selected,
		MaxSpeed:          strings.TrimSpace(ui.prefs.maxSpeed.Text),
		ThemeMode:         ui.prefs.themeMode.Selected,
		CookiesPath:       strings.TrimSpace(ui.prefs.cookies.Text),
		CookieSource:      ui.prefs.cookieSource.Selected,
		CookieBrowser:     ui.prefs.cookieBrowser.Selected,
		CookieProfile:     strings.TrimSpace(ui.prefs.cookieProfile.Text),
		Simultaneous:      ui.prefs.simultaneous.Selected,
		LogLimit:          ui.prefs.logLimit.Selected,
		ShowDebug:         ui.prefs.showDebug.Checked,
		CheckUpdates:      ui.prefs.checkUpdates.Checked,
		EmbedMetadata:     ui.prefs.embedMetadata.Checked,
		EmbedThumbnail:    ui.prefs.embedThumbnail.Checked,
		EmbedChapters:     ui.prefs.embedChapters.Checked,
		Subtitles:         ui.prefs.subtitles.Selected,
		SubtitleLangs:     strings.TrimSpace(ui.prefs.subtitleLangs.Text),
		AutoSubtitles:     ui.prefs.autoSubtitles.Checked,
		KeepHistory:       ui.prefs.keepHistory.Checked,
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
	}
}

// newPostProcessSettings snapshots the post-processing widgets into a
// PostProcessSettings value.
func newPostProcessSettings(ui *UIWidgets) PostProcessSettings {
	return PostProcessSettings{
		SmoothMotion:     ui.postProcess.smoothMotion.Checked,
		SmoothMotionMode: ui.postProcess.smoothMotionMode.Selected,
		SmoothMotionFPS:  ui.postProcess.smoothMotionFPS.Value,
		Sharpen:          ui.postProcess.sharpen.Checked,
		SharpenAmount:    ui.postProcess.sharpenAmount.Value,
		VividMode:        ui.postProcess.vividMode.Checked,
		Deband:           ui.postProcess.deband.Checked,
		HDRToSDR:         ui.postProcess.hdrToSdr.Checked,
		Denoise:          ui.postProcess.denoise.Checked,
		DenoiseMode:      ui.postProcess.denoiseMode.Selected,
		Deinterlace:      ui.postProcess.deinterlace.Checked,
		Stabilize:        ui.postProcess.stabilize.Checked,
		AutoCrop:         ui.postProcess.autoCrop.Checked,
		UpscaleVideo:     ui.postProcess.upscaleVideo.Checked,
		UpscaleTarget:    ui.postProcess.upscaleTarget.Selected,
		NormalizeAudio:   ui.postProcess.normalizeAudio.Checked,
		NightMode:        ui.postProcess.nightMode.Checked,
	}
}

// newSessionConfig snapshots the widgets relevant to a download session,
// mirroring newPostProcessSettings.
func newSessionConfig(ui *UIWidgets, urls []string, savePath, trimStart, trimEnd string) SessionConfig {
	return SessionConfig{
		URLs:        urls,
		RawURLField: ui.download.entry.Text,
		SavePath:    savePath,
		BatchMode:   ui.download.batchMode.Checked,
		Format:      ui.download.format.Selected,
		Quality:     ui.download.quality.Selected,
		TrimStart:   trimStart,
		TrimEnd:     trimEnd,
		MaxSpeed:    strings.TrimSpace(ui.prefs.maxSpeed.Text),
		Cookies:     cookieLabel(ui.prefs.cookieSource.Selected, ui.prefs.cookieBrowser.Selected, ui.prefs.cookieProfile.Text, strings.TrimSpace(ui.prefs.cookies.Text)),

		SaveLog:            ui.download.saveLog.Checked,
		Notify:             ui.download.notify.Checked,
		AutoRetry:          ui.download.autoRetry.Checked,
		PostProcessEnabled: ui.postProcess.enablePostProcess.Checked,

		SavePrefs: ui.prefs.savePrefs.Checked,
		LogLimit:  ui.prefs.logLimit.Selected,
		ShowDebug: ui.prefs.showDebug.Checked,

		EmbedMetadata:  ui.prefs.embedMetadata.Checked,
		EmbedThumbnail: ui.prefs.embedThumbnail.Checked,
		EmbedChapters:  ui.prefs.embedChapters.Checked,
		Subtitles:      ui.prefs.subtitles.Selected,
		SubtitleLangs:  strings.TrimSpace(ui.prefs.subtitleLangs.Text),
		AutoSubtitles:  ui.prefs.autoSubtitles.Checked,
		ThemeMode:      ui.prefs.themeMode.Selected,

		PP: newPostProcessSettings(ui),
	}
}

// applyPreferencesToWidgets writes every value from an AppPreferences struct
// into the corresponding UI widgets. It is the only writer of preference
// values into widgets: the Preferences and Post-Processing dialogs reload
// their own group through applyGeneralPrefs and applyPostProcessPrefs.
func applyPreferencesToWidgets(ui *UIWidgets, p AppPreferences) {
	applyMainWindowPrefs(ui, p)
	applyGeneralPrefs(ui, p)
	applyPostProcessPrefs(ui, p)
}

// applyMainWindowPrefs writes the preferences shown on the main window. An
// empty save path, format, or quality leaves the widget unchanged; Load
// already resolves those to their platform defaults.
func applyMainWindowPrefs(ui *UIWidgets, p AppPreferences) {
	if p.SavedPath != "" {
		ui.download.path.SetText(p.SavedPath)
	}
	if p.Format != "" {
		ui.download.format.SetSelected(p.Format)
	}
	if p.Quality != "" {
		ui.download.quality.SetSelected(p.Quality)
	}
	ui.download.batchMode.SetChecked(p.BatchMode)
	ui.download.saveLog.SetChecked(p.SaveLog)
	ui.download.notify.SetChecked(p.Notify)
	ui.download.autoRetry.SetChecked(p.AutoRetry)
	ui.postProcess.enablePostProcess.SetChecked(p.EnablePostProcess)
}

// applyGeneralPrefs writes the preferences shown in the Preferences dialog.
func applyGeneralPrefs(ui *UIWidgets, p AppPreferences) {
	ui.prefs.savePrefs.SetChecked(p.SavePrefs)
	ui.prefs.logLimit.SetSelected(p.LogLimit)
	ui.prefs.showDebug.SetChecked(p.ShowDebug)
	ui.prefs.checkUpdates.SetChecked(p.CheckUpdates)
	ui.prefs.embedMetadata.SetChecked(p.EmbedMetadata)
	ui.prefs.embedThumbnail.SetChecked(p.EmbedThumbnail)
	ui.prefs.embedChapters.SetChecked(p.EmbedChapters)
	ui.prefs.subtitles.SetSelected(p.Subtitles)
	ui.prefs.subtitleLangs.SetText(p.SubtitleLangs)
	ui.prefs.autoSubtitles.SetChecked(p.AutoSubtitles)
	ui.prefs.keepHistory.SetChecked(p.KeepHistory)
	ui.prefs.maxSpeed.SetText(p.MaxSpeed)
	ui.prefs.themeMode.SetSelected(p.ThemeMode)
	ui.prefs.cookies.SetText(p.CookiesPath)
	ui.prefs.cookieSource.SetSelected(p.CookieSource)
	ui.prefs.cookieBrowser.SetSelected(p.CookieBrowser)
	ui.prefs.cookieProfile.SetText(p.CookieProfile)
	ui.prefs.simultaneous.SetSelected(p.Simultaneous)
}

// applyPostProcessPrefs writes the preferences shown in the Post-Processing
// dialog. The master enable toggle lives on the main window and is written
// by applyMainWindowPrefs instead.
func applyPostProcessPrefs(ui *UIWidgets, p AppPreferences) {
	ui.postProcess.gpuBackend.SetSelected(p.GPUBackend)
	ui.postProcess.smoothMotion.SetChecked(p.SmoothMotion)
	ui.postProcess.smoothMotionMode.SetSelected(p.SmoothMotionMode)
	ui.postProcess.smoothMotionFPS.SetValue(p.SmoothFPS)
	ui.postProcess.vividMode.SetChecked(p.VividMode)
	ui.postProcess.sharpen.SetChecked(p.Sharpen)
	ui.postProcess.sharpenAmount.SetValue(p.SharpenAmount)
	ui.postProcess.deband.SetChecked(p.Deband)
	ui.postProcess.hdrToSdr.SetChecked(p.HDRToSDR)
	ui.postProcess.denoise.SetChecked(p.Denoise)
	ui.postProcess.denoiseMode.SetSelected(p.DenoiseMode)
	ui.postProcess.deinterlace.SetChecked(p.Deinterlace)
	ui.postProcess.stabilize.SetChecked(p.Stabilize)
	ui.postProcess.autoCrop.SetChecked(p.AutoCrop)
	ui.postProcess.upscaleVideo.SetChecked(p.UpscaleVideo)
	ui.postProcess.upscaleTarget.SetSelected(p.UpscaleTarget)
	ui.postProcess.normalizeAudio.SetChecked(p.NormalizeAudio)
	ui.postProcess.nightMode.SetChecked(p.NightMode)
}
