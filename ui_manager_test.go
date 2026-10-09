package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

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

// walkObjects calls visit for obj and everything inside it, including the
// parts widgets such as dialogs are rendered from.
func TestMustParseURL(t *testing.T) {
	if got := mustParseURL("https://dunder.gg").Host; got != "dunder.gg" {
		t.Errorf("mustParseURL(https://dunder.gg).Host = %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("mustParseURL did not panic on a URL that does not parse")
		}
	}()
	mustParseURL("https://dunder.gg/%zz")
}

func walkObjects(obj fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	visit(obj)
	switch o := obj.(type) {
	case *fyne.Container:
		for _, child := range o.Objects {
			walkObjects(child, visit)
		}
	case fyne.Widget:
		for _, child := range test.WidgetRenderer(o).Objects() {
			walkObjects(child, visit)
		}
	}
}

// findButton returns the button labelled text inside root.
func findButton(t *testing.T, root fyne.CanvasObject, text string) *widget.Button {
	t.Helper()
	var found *widget.Button
	walkObjects(root, func(obj fyne.CanvasObject) {
		if btn, ok := obj.(*widget.Button); ok && btn.Text == text {
			found = btn
		}
	})
	if found == nil {
		t.Fatalf("no %q button found", text)
	}
	return found
}
