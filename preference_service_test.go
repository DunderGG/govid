package main

import (
	"math"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestSharpenSliderStepConfigured(t *testing.T) {
	_ = test.NewApp()

	ctrls := NewPostProcessControls()
	if ctrls.sharpenAmount.Step != 0.1 {
		t.Errorf("sharpenAmount.Step = %v, want 0.1", ctrls.sharpenAmount.Step)
	}
	if ctrls.smoothMotionFPS.Step != 1.0 {
		t.Errorf("smoothMotionFPS.Step = %v, want 1.0", ctrls.smoothMotionFPS.Step)
	}
}

func TestApplyPreferencesPreservesSharpenAmount(t *testing.T) {
	_ = test.NewApp()

	ui := NewUIWidgets()
	prefs := AppPreferences{
		SavePrefs:     true,
		Sharpen:       true,
		SharpenAmount: 1.5,
	}

	applyPreferencesToWidgets(ui, prefs)

	if math.Abs(ui.postProcess.sharpenAmount.Value-1.5) > 0.001 {
		t.Errorf("ui.postProcess.sharpenAmount.Value = %v, want 1.5", ui.postProcess.sharpenAmount.Value)
	}
}

func TestPreferenceServiceSharpenAmountRoundtrip(t *testing.T) {
	a := test.NewApp()
	store := a.Preferences()
	prefSvc := NewPreferenceService(store)

	p := prefSvc.Load()
	p.SavePrefs = true
	p.Sharpen = true
	p.SharpenAmount = 1.5

	prefSvc.Save(p)

	loaded := prefSvc.Load()
	if math.Abs(loaded.SharpenAmount-1.5) > 0.001 {
		t.Errorf("loaded.SharpenAmount = %v, want 1.5", loaded.SharpenAmount)
	}
}

func TestNewPreferenceServiceMigratesLegacySmoothMotionKey(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		store := test.NewApp().Preferences()
		store.SetBool(prefSmoothMotion, !legacy)
		store.SetBool(legacyPrefSmoothMotion, legacy)

		if got := NewPreferenceService(store).Load().SmoothMotion; got != legacy {
			t.Errorf("legacy %v: SmoothMotion = %v, want %v", legacy, got, legacy)
		}
		if got := store.BoolWithFallback(legacyPrefSmoothMotion, !legacy); got != !legacy {
			t.Errorf("legacy %v: legacy key still stored after migration", legacy)
		}
	}

	store := test.NewApp().Preferences()
	store.SetBool(prefSmoothMotion, true)
	if !NewPreferenceService(store).Load().SmoothMotion {
		t.Error("SmoothMotion = false with no legacy key, want the stored true kept")
	}
}

func TestLoadResolvesRuntimeDefaults(t *testing.T) {
	tests := []struct {
		name  string
		setup func(store fyne.Preferences)
		want  AppPreferences
	}{
		{"unset", func(fyne.Preferences) {},
			AppPreferences{SavedPath: defaultSavePath(), Format: defaultFormat(), Quality: defaultQuality}},
		{"stored empty", func(store fyne.Preferences) {
			store.SetString(prefSavedPath, "")
			store.SetString(prefFormat, "")
			store.SetString(prefQuality, "")
		}, AppPreferences{SavedPath: defaultSavePath(), Format: defaultFormat(), Quality: defaultQuality}},
		{"stored values kept", func(store fyne.Preferences) {
			store.SetString(prefSavedPath, "/videos")
			store.SetString(prefFormat, "WebM")
			store.SetString(prefQuality, "720p")
		}, AppPreferences{SavedPath: "/videos", Format: "WebM", Quality: "720p"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := test.NewApp().Preferences()
			tt.setup(store)

			got := NewPreferenceService(store).Load()

			if got.SavedPath != tt.want.SavedPath || got.Format != tt.want.Format || got.Quality != tt.want.Quality {
				t.Errorf("Load() path/format/quality = %q/%q/%q, want %q/%q/%q",
					got.SavedPath, got.Format, got.Quality, tt.want.SavedPath, tt.want.Format, tt.want.Quality)
			}
		})
	}
}

func TestMergeConfigEmbedToggles(t *testing.T) {
	cfg, err := parseAppConfig([]byte(`{"embedThumbnail": false, "embedChapters": true}`))
	if err != nil {
		t.Fatal(err)
	}
	base := AppPreferences{EmbedMetadata: true, EmbedThumbnail: true}

	merged, errs := (&PreferenceService{}).MergeConfig(cfg, base, formatOptions, qualityOptions)

	if len(errs) != 0 {
		t.Errorf("errs = %q, want none", errs)
	}
	if !merged.EmbedMetadata || merged.EmbedThumbnail || !merged.EmbedChapters {
		t.Errorf("merged = metadata %v, thumbnail %v, chapters %v; want true, false, true (an absent field keeps its value)",
			merged.EmbedMetadata, merged.EmbedThumbnail, merged.EmbedChapters)
	}
}

func TestEmbedPreferenceDefaults(t *testing.T) {
	_ = test.NewApp()
	prefs := NewPreferenceService(test.NewTempApp(t).Preferences()).Load()
	if !prefs.EmbedMetadata || !prefs.EmbedThumbnail || prefs.EmbedChapters {
		t.Errorf("defaults = metadata %v, thumbnail %v, chapters %v; want true, true, false",
			prefs.EmbedMetadata, prefs.EmbedThumbnail, prefs.EmbedChapters)
	}
}

func TestMergeConfigSubtitles(t *testing.T) {
	cfg, err := parseAppConfig([]byte(`{"subtitles": "Both", "subtitleLangs": "en,de", "autoSubtitles": true}`))
	if err != nil {
		t.Fatal(err)
	}

	merged, errs := (&PreferenceService{}).MergeConfig(cfg, AppPreferences{Subtitles: subtitlesOff}, formatOptions, qualityOptions)

	if len(errs) != 0 || merged.Subtitles != subtitlesBoth || merged.SubtitleLangs != "en,de" || !merged.AutoSubtitles {
		t.Errorf("merged = %q/%q/%v, errs %q; want Both/en,de/true", merged.Subtitles, merged.SubtitleLangs, merged.AutoSubtitles, errs)
	}

	bad, _ := parseAppConfig([]byte(`{"subtitles": "Sometimes"}`))
	merged, errs = (&PreferenceService{}).MergeConfig(bad, AppPreferences{Subtitles: subtitlesOff}, formatOptions, qualityOptions)
	if merged.Subtitles != subtitlesOff || len(errs) != 1 {
		t.Errorf("invalid mode: merged %q, errs %q; want it skipped with one error", merged.Subtitles, errs)
	}
}

func TestSubtitlePreferenceDefaults(t *testing.T) {
	_ = test.NewApp()
	prefs := NewPreferenceService(test.NewTempApp(t).Preferences()).Load()
	if prefs.Subtitles != subtitlesOff || prefs.SubtitleLangs != "en.*" || prefs.AutoSubtitles {
		t.Errorf("defaults = %q/%q/%v, want Off/en.*/false", prefs.Subtitles, prefs.SubtitleLangs, prefs.AutoSubtitles)
	}
}
