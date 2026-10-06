// preset_ui.go — The Preset selector and its dialogs.
//
// Responsibilities:
//   - buildPresetRow: the Preset dropdown in the input card, its "(modified)"
//     marker, and a menu button for Save current as preset…, Manage
//     presets…, Import presets…, and Export presets….
//   - applyPreset: MergeConfig of a preset's settings onto the current ones,
//     then the same apply-and-save path as importing a settings file.
//   - refreshPresetState: shows "(modified)" once a setting the applied
//     preset sets no longer has the preset's value.
//
// The Preset type, the starter presets, and storage are in presets.go.
package main

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// presetFileName is the default name Export presets suggests.
const presetFileName = "govid-presets.json"

// buildPresetRow returns the Preset dropdown, its "(modified)" marker, and
// its menu button. It is rebuilt with the rest of the input card.
func (manager *UIManager) buildPresetRow() fyne.CanvasObject {
	manager.presets = manager.onLoadPresets()
	manager.presetSelect = widget.NewSelect(presetNames(manager.presets), nil)
	manager.presetSelect.PlaceHolder = "(none)"
	if manager.appliedPreset != nil {
		manager.presetSelect.Selected = manager.appliedPreset.Name
	}
	manager.presetSelect.OnChanged = manager.applyPreset
	manager.presetState = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Italic: true})

	var menuBtn *widget.Button
	menuBtn = widget.NewButtonWithIcon("", theme.MoreVerticalIcon(), func() {
		menu := fyne.NewMenu("",
			fyne.NewMenuItem("Save current as preset…", manager.showSavePreset),
			fyne.NewMenuItem("Manage presets…", manager.showManagePresets),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Import presets…", manager.showImportPresets),
			fyne.NewMenuItem("Export presets…", manager.showExportPresets),
		)
		position := fyne.NewPos(0, menuBtn.Size().Height)
		widget.ShowPopUpMenuAtRelativePosition(menu, manager.mainWindow.Canvas(), position, menuBtn)
	})

	manager.refreshPresetState()
	return container.NewBorder(nil, nil, nil, container.NewHBox(manager.presetState, menuBtn), manager.presetSelect)
}

// applyPreset applies the preset called name: its settings are merged onto
// the current ones, so the settings it does not hold keep their values. A
// value that is no longer valid, such as a save folder that has since been
// deleted, is skipped and logged.
func (manager *UIManager) applyPreset(name string) {
	preset, ok := findPreset(manager.presets, name)
	if !ok {
		return
	}
	current := snapshotPreferences(manager.ui, manager.ui.download.path.Text)
	valid, problems := ValidateConfig(preset.Settings)
	merged := applyConfig(valid, current)

	manager.appliedPreset = &preset
	applyPreferencesToWidgets(manager.ui, merged)
	manager.applyRuntimePrefs(merged)
	manager.onSavePreferences(merged)
	if merged.ThemeMode != current.ThemeMode {
		applyTheme(fyne.CurrentApp(), merged.ThemeMode)
		manager.createUI()
	}
	manager.onLog(fmt.Sprintf("[SYSTEM] Preset %q applied.", name), colSystem)
	for _, problem := range problems {
		manager.onLog(fmt.Sprintf("[SYSTEM] Preset %q: skipped %s", name, problem), colWarning)
	}
	manager.refreshPresetState()
}

// refreshPresetState shows "(modified)" beside the preset dropdown when a
// setting the applied preset sets has been changed since. Must be called
// on the UI thread.
func (manager *UIManager) refreshPresetState() {
	if manager.presetState == nil {
		return
	}
	text := ""
	if manager.appliedPreset != nil && !configMatches(manager.appliedPreset.Settings, snapshotPreferences(manager.ui, manager.ui.download.path.Text)) {
		text = "(modified)"
	}
	manager.presetState.SetText(text)
}

// setPresets stores presets and updates the dropdown. The applied preset
// stays selected if it still exists under the same name.
func (manager *UIManager) setPresets(presets []Preset) {
	manager.presets = presets
	manager.onSavePresets(presets)
	if manager.appliedPreset != nil {
		if preset, ok := findPreset(presets, manager.appliedPreset.Name); ok {
			manager.appliedPreset = &preset
		} else {
			manager.appliedPreset = nil
		}
	}
	if manager.presetSelect == nil {
		return
	}
	manager.presetSelect.Options = presetNames(presets)
	manager.presetSelect.OnChanged = nil // the selection below is not a choice to apply
	if manager.appliedPreset != nil {
		manager.presetSelect.SetSelected(manager.appliedPreset.Name)
	} else {
		manager.presetSelect.ClearSelected()
	}
	manager.presetSelect.OnChanged = manager.applyPreset
	manager.presetSelect.Refresh()
	manager.refreshPresetState()
}

// savePresetAs stores the current values of the settings in groups as the
// preset called name, replacing any preset of that name, and marks it as
// applied, since the current settings match it.
func (manager *UIManager) savePresetAs(name string, groups []presetGroup) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("a preset needs a name")
	}
	var fields []string
	for _, group := range groups {
		fields = append(fields, group.fields...)
	}
	if len(fields) == 0 {
		return fmt.Errorf("choose at least one group of settings to include")
	}
	current := snapshotPreferences(manager.ui, manager.ui.download.path.Text)
	preset := Preset{Name: name, Settings: configFields(configFromPreferences(current), fields)}
	manager.appliedPreset = &preset
	manager.setPresets(upsertPreset(manager.presets, preset))
	return nil
}

// showSavePreset asks for a name and which groups of settings to include,
// then saves the current settings as a preset.
func (manager *UIManager) showSavePreset() {
	name := widget.NewEntry()
	name.SetPlaceHolder("Preset name")
	if manager.appliedPreset != nil {
		name.SetText(manager.appliedPreset.Name)
	}
	checks := make([]*widget.Check, len(presetGroups))
	include := container.NewVBox()
	for i, group := range presetGroups {
		checks[i] = widget.NewCheck(group.label, nil)
		checks[i].SetChecked(group.checked)
		include.Add(checks[i])
	}

	items := []*widget.FormItem{
		widget.NewFormItem("Name", name),
		widget.NewFormItem("Include", include),
	}
	form := dialog.NewForm("Save Current as Preset", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		var groups []presetGroup
		for i, check := range checks {
			if check.Checked {
				groups = append(groups, presetGroups[i])
			}
		}
		if err := manager.savePresetAs(name.Text, groups); err != nil {
			dialog.ShowError(err, manager.mainWindow)
		}
	}, manager.mainWindow)
	form.Resize(fyne.NewSize(460, 0))
	form.Show()
}

// showManagePresets lists the presets with Rename and Delete buttons.
func (manager *UIManager) showManagePresets() {
	rows := container.NewVBox()
	var dlg dialog.Dialog
	var render func()
	render = func() {
		rows.Objects = nil
		if len(manager.presets) == 0 {
			rows.Add(widget.NewLabel("No presets. Use Save current as preset… to make one."))
		}
		for _, preset := range manager.presets {
			name := preset.Name
			rename := widget.NewButtonWithIcon("Rename", theme.DocumentCreateIcon(), func() {
				manager.showRenamePreset(name, render)
			})
			remove := widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), func() {
				dialog.ShowConfirm("Delete Preset", fmt.Sprintf("Delete the preset %q?", name), func(ok bool) {
					if ok {
						manager.setPresets(deletePreset(manager.presets, name))
						render()
					}
				}, manager.mainWindow)
			})
			label := widget.NewLabel(name)
			label.Truncation = fyne.TextTruncateEllipsis
			rows.Add(container.NewBorder(nil, nil, nil, container.NewHBox(rename, remove), label))
		}
		rows.Refresh()
	}
	render()

	scroll := container.NewVScroll(rows)
	scroll.SetMinSize(fyne.NewSize(460, 220))
	dlg = dialog.NewCustom("Manage Presets", "Close", scroll, manager.mainWindow)
	dlg.Show()
}

// showRenamePreset asks for a new name for the preset called from, then
// calls done.
func (manager *UIManager) showRenamePreset(from string, done func()) {
	entry := widget.NewEntry()
	entry.SetText(from)
	dialog.ShowForm("Rename Preset", "Rename", "Cancel", []*widget.FormItem{widget.NewFormItem("Name", entry)}, func(ok bool) {
		if !ok {
			return
		}
		presets, err := renamePreset(manager.presets, from, entry.Text)
		if err != nil {
			dialog.ShowError(err, manager.mainWindow)
			return
		}
		if manager.appliedPreset != nil && manager.appliedPreset.Name == from {
			manager.appliedPreset.Name = strings.TrimSpace(entry.Text)
		}
		manager.setPresets(presets)
		done()
	}, manager.mainWindow)
}

// importPresets adds the presets in the file at path, replacing presets of
// the same name, and reports what it imported and skipped.
func (manager *UIManager) importPresets(path string) {
	imported, problems, err := manager.onReadPresets(path)
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to import presets: %w", err), manager.mainWindow)
		return
	}
	presets := manager.presets
	for _, preset := range imported {
		presets = upsertPreset(presets, preset)
	}
	manager.setPresets(presets)

	message := fmt.Sprintf("Imported %s.", plural(len(imported), "preset", "presets"))
	if len(problems) > 0 {
		message += fmt.Sprintf("\n\nSome settings were skipped:\n- %s", strings.Join(problems, "\n- "))
	}
	dialog.ShowInformation("Presets Imported", message, manager.mainWindow)
}

// showImportPresets picks a presets file and imports it.
func (manager *UIManager) showImportPresets() {
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
		manager.importPresets(path)
	}, manager.mainWindow)
	picker.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
	picker.Show()
}

// showExportPresets asks where to save the presets, then writes them all.
func (manager *UIManager) showExportPresets() {
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
		if err := manager.onWritePresets(path, manager.presets); err != nil {
			dialog.ShowError(fmt.Errorf("failed to export presets: %w", err), manager.mainWindow)
			return
		}
		dialog.ShowInformation("Presets Exported",
			fmt.Sprintf("Saved %s to %s.", plural(len(manager.presets), "preset", "presets"), path), manager.mainWindow)
	}, manager.mainWindow)
	picker.SetFileName(presetFileName)
	picker.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
	picker.Show()
}
