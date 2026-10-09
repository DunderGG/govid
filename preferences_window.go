// preferences_window.go — The Preferences window and settings persistence.
//
// Responsibilities:
//   - UIManager.showPreferences: the general settings window, with the
//     Cookies row (buildCookiesRow) and the Portable Mode toggle.
//   - submitPreferences, savePreferences, applyRuntimePrefs: applying and
//     saving the settings, including those that live outside the widgets.
//   - confirmRestoreDefaults, restoreDefaults: the "Restore Defaults" reset.
//   - loadConfigFile, importConfig, showImportSettings, showExportSettings:
//     reading settings from govid.json or another file, and writing them out.
package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// showPreferences opens a window for general application settings.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showPreferences() {
	if focusOrCreate(&manager.prefsWindow) {
		return
	}

	ui := manager.ui
	// Reload the saved values so the window never shows edits that were
	// discarded by closing it without saving.
	applyGeneralPrefs(ui, manager.onLoadPreferences())
	ui.prefs.keepHistory.OnChanged = manager.onKeepHistoryChanged
	manager.wirePortableToggle()

	resetBtn := widget.NewButton("Restore Defaults", manager.confirmRestoreDefaults)
	resetBtn.Importance = widget.DangerImportance

	loadConfigBtn := widget.NewButtonWithIcon("Load from Config (govid.json)", theme.SettingsIcon(), manager.loadConfigFile)

	manager.prefsWindow = fyne.CurrentApp().NewWindow("Preferences")
	manager.prefsWindow.SetContent(container.NewPadded(container.NewVBox(
		manager.buildPreferencesForm(),
		widget.NewSeparator(),
		container.NewGridWithColumns(2, loadConfigBtn, resetBtn),
	)))
	manager.prefsWindow.Resize(fyne.NewSize(520, 560))
	manager.prefsWindow.SetOnClosed(onWindowClosed(&manager.prefsWindow))
	closeOnEscape(manager.prefsWindow)
	manager.prefsWindow.Show()
}

// wirePortableToggle sets the Portable Mode toggle to whether GoVid runs
// portable and wires it. Portable Mode is not a stored preference but a
// marker file, so the toggle acts at once rather than on Save. A switch
// that fails puts it back without acting again.
func (manager *UIManager) wirePortableToggle() {
	portable := manager.ui.prefs.portable
	var onPortable func(on bool)
	setPortable := func(on bool) {
		portable.OnChanged = nil
		portable.SetChecked(on)
		portable.OnChanged = onPortable
	}
	onPortable = func(on bool) {
		manager.onSetPortable(on, func() { setPortable(!on) })
	}
	setPortable(manager.onIsPortable())
}

// buildPreferencesForm lays out the Preferences window's settings, each
// with its hint; Save submits them (submitPreferences).
func (manager *UIManager) buildPreferencesForm() *widget.Form {
	ui := manager.ui
	return &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Save Preferences", Widget: ui.prefs.savePrefs, HintText: "Remember format, quality, path, speed, and theme between sessions"},
			{Text: "Log Buffer Limit", Widget: ui.prefs.logLimit, HintText: "Max lines kept in the log view (never more than 5000); older entries are removed from the top"},
			{Text: "Debug Output", Widget: ui.prefs.showDebug, HintText: "Show yt-dlp's [debug] lines in the log view; the log file always has them"},
			{Text: "Updates", Widget: ui.prefs.checkUpdates, HintText: "Check GitHub once a day for newer yt-dlp and GoVid releases"},
			{Text: "Download History", Widget: ui.prefs.keepHistory, HintText: "Record downloads in File → History, and warn before downloading a video again"},
			{Text: "Embed in File", Widget: container.NewHBox(ui.prefs.embedMetadata, ui.prefs.embedThumbnail, ui.prefs.embedChapters), HintText: "Write tags (title, artist, date), cover art, and chapter markers into downloads"},
			{Text: "Subtitles", Widget: container.NewHBox(ui.prefs.subtitles, ui.prefs.autoSubtitles), HintText: "Embed subtitles in videos, save them as .srt files beside them, or both"},
			{Text: "Subtitle Languages", Widget: ui.prefs.subtitleLangs, HintText: "Comma-separated language codes or patterns, e.g. en.*,de (yt-dlp --sub-langs)"},
			{Text: "Portable Mode", Widget: ui.prefs.portable, HintText: "Settings and presets travel with the GoVid folder (e.g. on a USB stick); takes effect after a restart"},
			{Text: "Filename Template", Widget: manager.buildFilenameTemplateRow(), HintText: "yt-dlp's output template, e.g. %(uploader)s - %(title)s; {quality} adds the height of capped downloads"},
			{Text: "Preferred Video Codec", Widget: fixedWidth(ui.prefs.preferredCodec, 120), HintText: "Pick this codec when a video offers it, even over a higher resolution in another (H.264 plays everywhere)"},
			{Text: "Simultaneous Downloads", Widget: fixedWidth(ui.prefs.simultaneous, 80), HintText: "Videos downloaded at once. More than 1 makes YouTube's \"confirm you're not a bot\" check more likely"},
			{Text: "Max Download Speed", Widget: ui.prefs.maxSpeed, HintText: "Limits download rate (e.g. 50K, 5M, 10G)"},
			{Text: "Application Theme", Widget: ui.prefs.themeMode, HintText: "System follows your computer's light or dark setting"},
			{Text: "Cookies", Widget: manager.buildCookiesRow(), HintText: "Your login, for age-restricted, members-only, and private videos and YouTube's bot check. On Windows, Firefox works best"},
		},
		OnSubmit: manager.submitPreferences,
	}
}

// buildCookiesRow lays out the Cookies choice (None / From file / From
// browser) with the controls of the chosen source below it: the cookies
// file entry, with a browse and a clear button, or the browser and an
// optional profile name.
func (manager *UIManager) buildCookiesRow() fyne.CanvasObject {
	prefs := manager.ui.prefs
	browserRow := container.NewBorder(nil, nil, fixedWidth(prefs.cookieBrowser, 140), nil, prefs.cookieProfile)
	fileRow := manager.buildCookiesFileRow()
	showSource := func(source string) {
		setVisible(fileRow, source == cookieSourceFile)
		setVisible(browserRow, source == cookieSourceBrowser)
	}
	showSource(prefs.cookieSource.Selected)
	prefs.cookieSource.OnChanged = showSource
	return container.NewVBox(prefs.cookieSource, fileRow, browserRow)
}

// setVisible shows or hides obj.
func setVisible(obj fyne.CanvasObject, visible bool) {
	if visible {
		obj.Show()
	} else {
		obj.Hide()
	}
}

// buildCookiesFileRow lays out the cookies-file entry with a browse button
// that picks a cookie file and a button that clears the entry.
func (manager *UIManager) buildCookiesFileRow() fyne.CanvasObject {
	cookies := manager.ui.prefs.cookies

	browseBtn := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			cookies.SetText(reader.URI().Path())
			reader.Close()
		}, manager.prefsWindow)
		// Filter for common cookie file extensions
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".txt", ".cookies", ".dat"}))
		fileDialog.Show()
	})
	clearBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		cookies.SetText("")
	})
	return container.NewBorder(nil, nil, nil, container.NewHBox(browseBtn, clearBtn), cookies)
}

// submitPreferences handles the Preferences form's Submit: it applies the
// log buffer limit, saves every preference, and applies the selected theme.
func (manager *UIManager) submitPreferences() {
	ui := manager.ui
	manager.applyRuntimePrefs(snapshotPreferences(ui, ui.download.path.Text))
	manager.savePreferences(ui.download.path.Text)

	// Apply theme change and rebuild the UI so canvas.Rectangle colors
	// (which are snapshotted at construction time) get fresh theme values.
	applyTheme(fyne.CurrentApp(), ui.prefs.themeMode.Selected)
	manager.createUI()
}

// onKeepHistoryChanged handles the "Keep download history" toggle: turning
// it off offers to delete the history kept so far as well. Restore Defaults
// never turns it off, so it never asks then.
func (manager *UIManager) onKeepHistoryChanged(keep bool) {
	if keep || manager.restoringDefaults {
		return
	}
	parent := manager.prefsWindow
	if parent == nil {
		parent = manager.mainWindow
	}
	confirm := dialog.NewConfirm("Stop Keeping History",
		"Once you save, GoVid stops recording downloads and no longer warns before downloading a video again.\n\nDelete the history recorded so far as well?",
		func(deleteIt bool) {
			if !deleteIt {
				return
			}
			if err := manager.onClearHistory(); err != nil {
				dialog.ShowError(fmt.Errorf("failed to clear history: %v", err), parent)
			}
		}, parent)
	confirm.SetConfirmText("Delete history")
	confirm.SetDismissText("Keep it")
	confirm.Show()
}

// applyRuntimePrefs applies the preferences that live outside the widgets:
// the log buffer limit, whether debug lines are shown, and whether history
// is kept.
func (manager *UIManager) applyRuntimePrefs(p AppPreferences) {
	manager.onSetLogBufferLimit(ParseBufferLimit(p.LogLimit))
	manager.onSetShowDebug(p.ShowDebug)
	manager.onSetKeepHistory(p.KeepHistory)
}

// confirmRestoreDefaults asks the user to confirm, then runs restoreDefaults.
func (manager *UIManager) confirmRestoreDefaults() {
	dialog.ShowConfirm("Restore Defaults", "Reset all preferences to their default values?", func(ok bool) {
		if ok {
			manager.restoreDefaults()
		}
	}, manager.prefsWindow)
}

// loadConfigFile merges govid.json into the stored preferences, applies the
// result to the widgets and saves it, then reports any skipped settings.
func (manager *UIManager) loadConfigFile() {
	manager.importConfig(configFileName, manager.prefsWindow)
}

// importConfig merges the settings file at path into the stored preferences,
// applies and saves the result (see applyAndSavePreferences), and then
// reports, over parent, any settings it skipped.
func (manager *UIManager) importConfig(path string, parent fyne.Window) {
	name := filepath.Base(path)
	config, err := manager.onLoadConfigFile(path)
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to load %s: %v", name, err), parent)
		return
	}

	merged, errs := manager.onMergeConfig(config, manager.onLoadPreferences())
	manager.applyAndSavePreferences(merged)

	if len(errs) > 0 {
		dialog.ShowCustom("Settings Loaded with Warnings", "OK",
			widget.NewLabel(fmt.Sprintf("Some settings in %s were skipped:\n- %s", name, strings.Join(errs, "\n- "))),
			parent)
		return
	}
	dialog.ShowInformation("Settings Loaded", fmt.Sprintf("Preferences updated from %s.", name), parent)
}

// applyAndSavePreferences puts p into effect: the widgets, the runtime
// preferences, the store, and the theme, rebuilding the main window so its
// colours follow the theme.
func (manager *UIManager) applyAndSavePreferences(p AppPreferences) {
	applyPreferencesToWidgets(manager.ui, p)
	manager.applyRuntimePrefs(p)
	manager.onSavePreferences(p)
	applyTheme(fyne.CurrentApp(), p.ThemeMode)
	manager.createUI()
}

// showImportSettings picks a settings file (an exported one, or any
// govid.json) and imports it.
func (manager *UIManager) showImportSettings() {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, manager.mainWindow)
			return
		}
		if reader == nil {
			return // cancelled
		}
		path := reader.URI().Path()
		reader.Close()
		manager.importConfig(path, manager.mainWindow)
	}, manager.mainWindow)
	picker.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
	picker.Show()
}

// showExportSettings asks where to save the current settings, then writes
// every one of them there as a govid.json-style file.
func (manager *UIManager) showExportSettings() {
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, manager.mainWindow)
			return
		}
		if writer == nil {
			return // cancelled
		}
		path := writer.URI().Path()
		writer.Close()
		prefs := snapshotPreferences(manager.ui, manager.ui.download.path.Text)
		if err := manager.onExportConfig(path, prefs); err != nil {
			dialog.ShowError(fmt.Errorf("failed to export settings: %w", err), manager.mainWindow)
			return
		}
		dialog.ShowInformation("Settings Exported",
			fmt.Sprintf("Saved every setting to %s.\n\nLoad it with Tools → Import settings…, or name it %s, put it beside GoVid, and use Load from Config in Preferences.", path, configFileName),
			manager.mainWindow)
	}, manager.mainWindow)
	picker.SetFileName(configFileName)
	picker.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
	picker.Show()
}

// savePreferences snapshots the current widget state (see snapshotPreferences)
// and delegates persistence to PreferenceService.Save. It does nothing while
// restoreDefaults is running.
func (manager *UIManager) savePreferences(savePath string) {
	if manager.restoringDefaults {
		return
	}
	manager.onSavePreferences(snapshotPreferences(manager.ui, savePath))
	manager.refreshPresetState()
}

// restoreDefaults clears every stored preference and returns the whole UI to
// its default state: every preference-backed widget, the log buffer limit, the
// theme, and the main window layout. The defaults come from loading the
// freshly cleared store, so no default value is repeated here.
//
// Applying the defaults fires the widgets' save-on-change handlers. Saving is
// suppressed meanwhile so those handlers cannot write a half-updated snapshot
// back into the store, which is left empty as PreferenceService.Reset intends.
func (manager *UIManager) restoreDefaults() {
	manager.restoringDefaults = true
	defer func() { manager.restoringDefaults = false }()

	manager.onResetPreferences()
	defaults := manager.onLoadPreferences()

	applyPreferencesToWidgets(manager.ui, defaults)
	manager.applyRuntimePrefs(defaults)
	applyTheme(fyne.CurrentApp(), defaults.ThemeMode)
	manager.createUI()
}
