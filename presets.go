// presets.go — Named sets of settings.
//
// Responsibilities:
//   - Preset: a name plus a partial AppConfig, holding only the settings the
//     user chose to include. Applying one is MergeConfig onto the current
//     settings, so presets need no apply logic of their own.
//   - starterPresets: the presets a new install starts with.
//   - presetGroups: the groups of settings "Save current as preset" offers.
//   - upsertPreset, renamePreset, deletePreset, findPreset: list edits.
//   - PreferenceService.LoadPresets / SavePresets (the preferences store) and
//     ReadPresetFile / WritePresetFile (a file to move presets between
//     machines, validated like govid.json).
//
// The preset menu, dialogs, and "(modified)" marker are in preset_ui.go.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// prefPresets is the preferences key the presets are stored under, as JSON.
// It is not one of the AppPreferences keys: Restore Defaults keeps presets.
const prefPresets = "presets"

// Preset is a named set of some settings.
type Preset struct {
	Name     string    `json:"name"`
	Settings AppConfig `json:"settings"`
}

// presetFile is the file Export presets writes and Import presets reads.
type presetFile struct {
	Presets []Preset `json:"presets"`
}

// ptr returns a pointer to a copy of value, for building AppConfig values.
func ptr[T any](value T) *T {
	return &value
}

// starterPresets returns the presets GoVid starts with, before the user
// has saved or deleted any.
func starterPresets() []Preset {
	return []Preset{
		{Name: "Audio (MP3, metadata + cover)", Settings: AppConfig{
			Format: ptr(formatMP3), EmbedMetadata: ptr(true), EmbedThumbnail: ptr(true),
		}},
		{Name: "1080p MP4", Settings: AppConfig{
			Format: ptr(formatMP4), Quality: ptr(quality1080p),
		}},
		{Name: "Archive (MKV, Best, subtitles, chapters)", Settings: AppConfig{
			Format: ptr(formatMKV), Quality: ptr(qualityBest),
			Subtitles: ptr(subtitlesEmbed), EmbedChapters: ptr(true),
			EmbedMetadata: ptr(true), EmbedThumbnail: ptr(true),
		}},
	}
}

// presetGroup is one group of settings a new preset can include.
type presetGroup struct {
	label   string
	fields  []string // AppConfig field names
	checked bool     // included unless the user unticks it
}

// presetGroups lists the settings a preset can hold, grouped as the "Save
// current as preset" dialog offers them. App-wide settings such as the
// theme or the log limit are left out: they are not part of a download
// setup.
var presetGroups = []presetGroup{
	{label: "Format, quality, and preferred codec", fields: []string{"Format", "Quality", "PreferredCodec"}, checked: true},
	{label: "Save folder", fields: []string{"SavedPath"}},
	{label: "Speed limit", fields: []string{"MaxSpeed"}},
	{label: "Embed in file (metadata, thumbnail, chapters)", fields: []string{"EmbedMetadata", "EmbedThumbnail", "EmbedChapters"}, checked: true},
	{label: "Subtitles", fields: []string{"Subtitles", "SubtitleLangs", "AutoSubtitles"}, checked: true},
	{label: "Log file, notification, and auto-retry toggles", fields: []string{"SaveLog", "Notify", "AutoRetry"}},
	{label: "Post-processing", fields: []string{
		"EnablePostProcess", "SmoothMotion", "SmoothMotionMode", "SmoothFPS", "Sharpen", "SharpenAmount",
		"NormalizeAudio", "VividMode", "Denoise", "DenoiseMode", "HDRToSDR", "Deband", "AutoCrop",
		"Stabilize", "Deinterlace", "NightMode", "UpscaleVideo", "UpscaleTarget", "GPUBackend",
	}},
	{label: "Filename template", fields: []string{"FilenameTemplate"}},
	{label: "Cookies (source, browser, and profile)", fields: []string{"CookieSource", "CookieBrowser", "CookieProfile"}},
}

// findPreset returns the preset called name.
func findPreset(presets []Preset, name string) (Preset, bool) {
	index := slices.IndexFunc(presets, func(preset Preset) bool { return preset.Name == name })
	if index < 0 {
		return Preset{}, false
	}
	return presets[index], true
}

// upsertPreset returns presets with preset added, replacing any preset of
// the same name in its place.
func upsertPreset(presets []Preset, preset Preset) []Preset {
	presets = slices.Clone(presets)
	if index := slices.IndexFunc(presets, func(p Preset) bool { return p.Name == preset.Name }); index >= 0 {
		presets[index] = preset
		return presets
	}
	return append(presets, preset)
}

// renamePreset returns presets with the preset called from renamed to to.
func renamePreset(presets []Preset, from, to string) ([]Preset, error) {
	to = strings.TrimSpace(to)
	switch {
	case to == "":
		return nil, errors.New("a preset needs a name")
	case to != from && slices.ContainsFunc(presets, func(p Preset) bool { return p.Name == to }):
		return nil, fmt.Errorf("there is already a preset called %q", to)
	}
	presets = slices.Clone(presets)
	for i := range presets {
		if presets[i].Name == from {
			presets[i].Name = to
		}
	}
	return presets, nil
}

// deletePreset returns presets without the preset called name.
func deletePreset(presets []Preset, name string) []Preset {
	return slices.DeleteFunc(slices.Clone(presets), func(p Preset) bool { return p.Name == name })
}

// presetNames returns the presets' names, in order.
func presetNames(presets []Preset) []string {
	names := make([]string, len(presets))
	for i, preset := range presets {
		names[i] = preset.Name
	}
	return names
}

// LoadPresets returns the stored presets, or the starter presets when none
// have been stored yet. Unreadable stored presets are replaced by the
// starters too, rather than leaving the user with nothing.
func (prefSvc *PreferenceService) LoadPresets() []Preset {
	raw := prefSvc.store.String(prefPresets)
	if raw == "" {
		return starterPresets()
	}
	var presets []Preset
	if err := json.Unmarshal([]byte(raw), &presets); err != nil {
		return starterPresets()
	}
	return presets
}

// SavePresets stores presets. Unlike the other preferences they are saved
// even when "Save preferences" is off: they are only ever changed on
// purpose.
func (prefSvc *PreferenceService) SavePresets(presets []Preset) {
	if presets == nil {
		presets = []Preset{} // stored as [], so the starters do not come back
	}
	raw, err := json.Marshal(presets)
	if err != nil {
		return
	}
	prefSvc.store.SetString(prefPresets, string(raw))
}

// WritePresetFile writes presets to path as indented JSON.
func (prefSvc *PreferenceService) WritePresetFile(path string, presets []Preset) error {
	data, err := json.MarshalIndent(presetFile{Presets: presets}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// ReadPresetFile reads a file written by WritePresetFile. Each preset's
// settings are checked like govid.json's (see ValidateConfig): an invalid
// value, such as a save folder that does not exist on this machine, is
// dropped from its preset and reported. Presets without a name are skipped.
func (prefSvc *PreferenceService) ReadPresetFile(path string) ([]Preset, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var file presetFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, nil, err
	}
	var presets []Preset
	var problems []string
	for _, preset := range file.Presets {
		preset.Name = strings.TrimSpace(preset.Name)
		if preset.Name == "" {
			problems = append(problems, "skipped a preset without a name")
			continue
		}
		valid, errs := ValidateConfig(preset.Settings)
		for _, err := range errs {
			problems = append(problems, fmt.Sprintf("%s: %s", preset.Name, err))
		}
		preset.Settings = valid
		presets = append(presets, preset)
	}
	return presets, problems, nil
}
