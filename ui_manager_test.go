package main

import (
	"math"
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestRestoreDefaultsResetsWidgetsAndStore(t *testing.T) {
	_ = test.NewApp()
	app := newDownloaderApp(test.NewWindow(nil))
	mgr := app.uiManager
	mgr.createUI()

	// Move a spread of main-window, Preferences, and Post-Processing widgets
	// away from their defaults. The main-window toggles persist on change.
	ui := app.ui
	ui.download.path.SetText(t.TempDir())
	ui.prefs.maxSpeed.SetText("5M")
	ui.prefs.cookies.SetText("cookies.txt")
	ui.prefs.themeMode.SetSelected("Light")
	ui.prefs.logLimit.SetSelected("1000")
	ui.postProcess.smoothMotion.SetChecked(true)
	ui.postProcess.sharpenAmount.SetValue(1.7)
	ui.download.notify.SetChecked(true)
	ui.download.autoRetry.SetChecked(true)
	ui.postProcess.enablePostProcess.SetChecked(false)
	mgr.onSetLogBufferLimit(1000)

	if !app.prefSvc.Load().Notify {
		t.Fatal("precondition: notify toggle was not persisted")
	}

	mgr.restoreDefaults()

	// Widgets show the defaults.
	if got := ui.prefs.maxSpeed.Text; got != "" {
		t.Errorf("maxSpeed = %q, want empty", got)
	}
	if got := ui.prefs.cookies.Text; got != "" {
		t.Errorf("cookies = %q, want empty", got)
	}
	if got := ui.prefs.themeMode.Selected; got != defaultThemeMode {
		t.Errorf("themeMode = %q, want %q", got, defaultThemeMode)
	}
	if got := ui.prefs.logLimit.Selected; got != defaultLogLimit {
		t.Errorf("logLimit = %q, want %q", got, defaultLogLimit)
	}
	if ui.postProcess.smoothMotion.Checked {
		t.Error("smoothMotion still checked")
	}
	if got := ui.postProcess.sharpenAmount.Value; math.Abs(got-defaultSharpenAmount) > 1e-9 {
		t.Errorf("sharpenAmount = %v, want %v", got, defaultSharpenAmount)
	}
	if ui.download.notify.Checked || ui.download.autoRetry.Checked {
		t.Error("notify/autoRetry still checked")
	}
	if ui.postProcess.enablePostProcess.Checked != defaultEnablePostProcess {
		t.Errorf("enablePostProcess = %v, want %v", ui.postProcess.enablePostProcess.Checked, defaultEnablePostProcess)
	}
	if got := app.logSvc.BufferLimit(); got != defaultLogBufferLimit {
		t.Errorf("BufferLimit() = %d, want %d", got, defaultLogBufferLimit)
	}

	// The save-on-change handlers fired while defaults were applied must not
	// have written the old values back into the cleared store.
	stored := app.prefSvc.Load()
	if stored.Notify || stored.AutoRetry || stored.MaxSpeed != "" || stored.LogLimit != defaultLogLimit {
		t.Errorf("store not reset: Notify=%v AutoRetry=%v MaxSpeed=%q LogLimit=%q",
			stored.Notify, stored.AutoRetry, stored.MaxSpeed, stored.LogLimit)
	}
	if raw := app.prefSvc.store.String(prefSavedPath); raw != "" {
		t.Errorf("stored savedPath = %q, want empty", raw)
	}
	if got := ui.download.path.Text; got != defaultSavePath() {
		t.Errorf("path = %q, want default %q", got, defaultSavePath())
	}

	// Saving resumes once the reset is complete.
	ui.download.notify.SetChecked(true)
	if !app.prefSvc.Load().Notify {
		t.Error("notify toggle not persisted after restoreDefaults")
	}
}

func TestStartupAppliesSavedFormatAndQuality(t *testing.T) {
	a := test.NewApp()
	a.Preferences().SetString(prefFormat, "WebM")
	a.Preferences().SetString(prefQuality, "720p")

	app := newDownloaderApp(test.NewWindow(nil))

	// Checked before createUI: newDownloaderApp alone must apply them.
	if got := app.ui.download.format.Selected; got != "WebM" {
		t.Errorf("format = %q, want %q", got, "WebM")
	}
	if got := app.ui.download.quality.Selected; got != "720p" {
		t.Errorf("quality = %q, want %q", got, "720p")
	}
}

func TestRebuildingMainWindowKeepsUnsavedSelections(t *testing.T) {
	_ = test.NewApp()
	app := newDownloaderApp(test.NewWindow(nil))
	mgr := app.uiManager
	mgr.createUI()

	// With saving off, these selections exist only in the widgets.
	app.ui.prefs.savePrefs.SetChecked(false)
	mgr.savePreferences(app.ui.download.path.Text)
	app.ui.download.format.SetSelected("M4A")
	app.ui.download.quality.SetSelected("360p")

	// A theme change rebuilds the main window.
	mgr.createUI()

	if got := app.ui.download.format.Selected; got != "M4A" {
		t.Errorf("format = %q after rebuild, want %q", got, "M4A")
	}
	if got := app.ui.download.quality.Selected; got != "360p" {
		t.Errorf("quality = %q after rebuild, want %q", got, "360p")
	}
}
