package main

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestPresetGroupsNameConfigFields(t *testing.T) {
	configType := reflect.TypeFor[AppConfig]()
	seen := map[string]string{}
	for _, group := range presetGroups {
		for _, name := range group.fields {
			if _, ok := configType.FieldByName(name); !ok {
				t.Errorf("group %q names %s, which AppConfig does not have", group.label, name)
			}
			if other, dup := seen[name]; dup {
				t.Errorf("%s is in both %q and %q", name, other, group.label)
			}
			seen[name] = group.label
		}
	}
}

func TestStarterPresetsAreValid(t *testing.T) {
	for _, preset := range starterPresets() {
		if _, errs := ValidateConfig(preset.Settings); len(errs) != 0 {
			t.Errorf("starter preset %q: %q", preset.Name, errs)
		}
	}
}

func TestPresetListEdits(t *testing.T) {
	presets := []Preset{{Name: "A"}, {Name: "B"}}

	presets = upsertPreset(presets, Preset{Name: "B", Settings: AppConfig{Format: ptr(formatMKV)}})
	presets = upsertPreset(presets, Preset{Name: "C"})
	if names := presetNames(presets); !slices.Equal(names, []string{"A", "B", "C"}) || presets[1].Settings.Format == nil {
		t.Errorf("after upserts: %q (B replaced in place)", names)
	}
	if _, err := renamePreset(presets, "A", "C"); err == nil {
		t.Error("renaming onto an existing name succeeded")
	}
	if _, err := renamePreset(presets, "A", "  "); err == nil {
		t.Error("renaming to a blank name succeeded")
	}
	renamed, err := renamePreset(presets, "A", " Z ")
	if err != nil || !slices.Equal(presetNames(renamed), []string{"Z", "B", "C"}) {
		t.Errorf("rename = %q, %v", presetNames(renamed), err)
	}
	if names := presetNames(deletePreset(renamed, "B")); !slices.Equal(names, []string{"Z", "C"}) {
		t.Errorf("delete = %q", names)
	}
}

func TestPresetsStartWithStartersAndPersist(t *testing.T) {
	_ = test.NewApp()
	svc := NewPreferenceService(test.NewTempApp(t).Preferences())
	if got := presetNames(svc.LoadPresets()); !slices.Equal(got, presetNames(starterPresets())) {
		t.Errorf("first LoadPresets = %q, want the starters", got)
	}

	svc.SavePresets([]Preset{{Name: "Mine", Settings: AppConfig{Quality: ptr(quality720p)}}})
	if got := svc.LoadPresets(); len(got) != 1 || got[0].Name != "Mine" || *got[0].Settings.Quality != quality720p {
		t.Errorf("LoadPresets after saving = %+v", got)
	}

	svc.SavePresets(nil)
	if got := svc.LoadPresets(); len(got) != 0 {
		t.Errorf("LoadPresets after deleting all = %q, want none (no starters again)", presetNames(got))
	}
}

func TestApplyPresetSetsOnlyItsSettings(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	ui := h.app.ui
	ui.download.format.SetSelected(formatWebM)
	ui.download.quality.SetSelected(quality480p)
	ui.prefs.embedChapters.SetChecked(true)
	ui.prefs.subtitles.SetSelected(subtitlesSRT)
	ui.download.notify.SetChecked(true)
	before := snapshotPreferences(ui, ui.download.path.Text)

	h.app.uiManager.applyPreset("1080p MP4")

	after := snapshotPreferences(ui, ui.download.path.Text)
	if after.Format != formatMP4 || after.Quality != quality1080p {
		t.Errorf("format/quality = %s/%s, want the preset's MP4/1080p", after.Format, after.Quality)
	}
	after.Format, after.Quality = before.Format, before.Quality
	if !reflect.DeepEqual(after, before) {
		t.Errorf("the preset changed settings it does not hold:\n before %+v\n after  %+v", before, after)
	}
	if state := h.app.uiManager.presetState.Text; state != "" {
		t.Errorf("state right after applying = %q, want none", state)
	}

	ui.download.notify.SetChecked(false) // not a setting of the preset
	if state := h.app.uiManager.presetState.Text; state != "" {
		t.Errorf("state after changing another setting = %q, want none", state)
	}
	ui.download.quality.SetSelected(quality720p)
	if state := h.app.uiManager.presetState.Text; state != "(modified)" {
		t.Errorf("state after changing the preset's quality = %q, want (modified)", state)
	}
}

func TestSavePresetStoresTheChosenGroups(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	ui := h.app.ui
	ui.download.format.SetSelected(formatMKV)
	ui.download.quality.SetSelected(quality720p)
	ui.prefs.embedChapters.SetChecked(true)

	if err := h.app.uiManager.savePresetAs("  Mine ", []presetGroup{presetGroups[0], presetGroups[3]}); err != nil {
		t.Fatal(err)
	}

	stored, ok := findPreset(h.app.prefSvc.LoadPresets(), "Mine")
	if !ok {
		t.Fatalf("preset not stored; presets = %q", presetNames(h.app.prefSvc.LoadPresets()))
	}
	cfg := stored.Settings
	if cfg.Format == nil || *cfg.Format != formatMKV || cfg.Quality == nil || *cfg.Quality != quality720p || cfg.EmbedChapters == nil || !*cfg.EmbedChapters {
		t.Errorf("preset settings = %+v, want the format, quality, and embed values", cfg)
	}
	if cfg.SavedPath != nil || cfg.Subtitles != nil || cfg.SmoothFPS != nil {
		t.Errorf("preset holds settings from groups that were not chosen: %+v", cfg)
	}
	if h.app.uiManager.presetSelect.Selected != "Mine" {
		t.Errorf("dropdown shows %q, want the new preset", h.app.uiManager.presetSelect.Selected)
	}
	if err := h.app.uiManager.savePresetAs("", presetGroups[:1]); err == nil {
		t.Error("a preset without a name was saved")
	}
}

func TestExportedPresetsImportOnAnotherMachine(t *testing.T) {
	_ = test.NewApp()
	source := NewPreferenceService(test.NewTempApp(t).Preferences())
	folder := t.TempDir()
	presets := append(starterPresets(), Preset{Name: "Here", Settings: AppConfig{SavedPath: ptr(folder), MaxSpeed: ptr("3M")}})
	path := filepath.Join(t.TempDir(), presetFileName)
	if err := source.WritePresetFile(path, presets); err != nil {
		t.Fatal(err)
	}
	// The other machine has no such folder.
	if err := os.Remove(folder); err != nil {
		t.Fatal(err)
	}

	h := newDownloadHarness(t, "ytdlp-download")
	h.app.prefSvc.SavePresets([]Preset{{Name: "1080p MP4"}, {Name: "Theirs"}})
	h.app.uiManager.presets = h.app.prefSvc.LoadPresets()
	h.app.uiManager.importPresets(path)

	got := h.app.prefSvc.LoadPresets()
	wantNames := []string{"1080p MP4", "Theirs", "Audio (MP3, metadata + cover)", "Archive (MKV, Best, subtitles, chapters)", "Here"}
	if !slices.Equal(presetNames(got), wantNames) {
		t.Errorf("presets = %q, want %q", presetNames(got), wantNames)
	}
	if imported, _ := findPreset(got, "1080p MP4"); imported.Settings.Quality == nil {
		t.Error("an imported preset did not replace the one of the same name")
	}
	if here, _ := findPreset(got, "Here"); here.Settings.SavedPath != nil || here.Settings.MaxSpeed == nil {
		t.Errorf("preset Here = %+v, want the missing folder dropped and the speed kept", here.Settings)
	}

	_, problems, err := h.app.prefSvc.ReadPresetFile(path)
	if err != nil || len(problems) != 1 || !strings.HasPrefix(problems[0], "Here: invalid path") {
		t.Errorf("ReadPresetFile problems = %q, %v; want the missing folder reported", problems, err)
	}
}

func TestPresetDialogsSaveAndDelete(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.window.Resize(fyne.NewSize(900, 800))
	manager := h.app.uiManager

	manager.showSavePreset()
	overlay := h.window.Canvas().Overlays().Top()
	findEntry(t, overlay).SetText("From dialog")
	test.Tap(findButton(t, overlay, "Save"))
	if _, ok := findPreset(manager.presets, "From dialog"); !ok {
		t.Fatalf("Save did not add the preset; presets = %q", presetNames(manager.presets))
	}

	manager.showManagePresets()
	overlay = h.window.Canvas().Overlays().Top()
	var deleteButtons []*widget.Button
	walkObjects(overlay, func(obj fyne.CanvasObject) {
		if button, ok := obj.(*widget.Button); ok && button.Text == "Delete" {
			deleteButtons = append(deleteButtons, button)
		}
	})
	if len(deleteButtons) != len(manager.presets) {
		t.Fatalf("Manage Presets shows %d Delete buttons for %d presets", len(deleteButtons), len(manager.presets))
	}
	test.Tap(deleteButtons[len(deleteButtons)-1])
	test.Tap(findButton(t, h.window.Canvas().Overlays().Top(), "Yes"))
	if _, ok := findPreset(manager.presets, "From dialog"); ok {
		t.Error("Delete did not remove the preset")
	}
}
