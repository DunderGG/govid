package main

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestReleaseDialogReportsAPageURLThatDoesNotParse(t *testing.T) {
	_ = test.NewApp()
	window := test.NewWindow(nil)
	window.Resize(fyne.NewSize(900, 700))
	manager := NewUIManager(window)
	manager.onCanSelfUpdate = func(Release) bool { return false }
	// "%zz" is not a valid escape, so url.Parse fails on it.
	release := Release{TagName: "2026.10.07", Body: "notes", HTMLURL: "https://github.com/DunderGG/govid/%zz"}

	manager.showGoVidRelease(release)
	test.Tap(findButton(t, window.Canvas().Overlays().Top(), "Open download page"))

	var shown []string
	for _, overlay := range window.Canvas().Overlays().List() {
		walkObjects(overlay, func(obj fyne.CanvasObject) {
			if label, ok := obj.(*widget.Label); ok {
				shown = append(shown, label.Text)
			}
		})
	}
	// The error dialog capitalises the error's first letter.
	want := "Could not open " + release.HTMLURL
	for _, text := range shown {
		if strings.HasPrefix(text, want) {
			return
		}
	}
	t.Errorf("labels shown = %q, want an error starting %q", shown, want)
}
