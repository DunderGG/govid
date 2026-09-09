package main

import (
	"math"
	"testing"

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

