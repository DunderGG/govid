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
