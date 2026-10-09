package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSizeWarning(t *testing.T) {
	tests := []struct {
		upscale, smooth bool
		want            string
	}{
		{false, false, ""},
		{true, false, "⚠ Upscaling significantly increases file size (bigger frames)"},
		{false, true, "⚠ Smooth Motion increases file size (more frames)"},
		{true, true, "⚠ Upscaling + Smooth Motion will greatly increase file size"},
	}
	for _, tt := range tests {
		if got := sizeWarning(tt.upscale, tt.smooth); got != tt.want {
			t.Errorf("sizeWarning(%v, %v) = %q, want %q", tt.upscale, tt.smooth, got, tt.want)
		}
	}
}

func TestPostProcessingWindowEnablesSubControls(t *testing.T) {
	_ = test.NewApp()
	app := newDownloaderApp(test.NewWindow(nil))
	app.uiManager.showPostProcessing()
	pp := app.ui.postProcess

	tests := []struct {
		name       string
		toggle     *widget.Check
		dependents []fyne.Disableable
	}{
		{"smooth motion", pp.smoothMotion, []fyne.Disableable{pp.smoothMotionMode, pp.smoothMotionFPS}},
		{"sharpen", pp.sharpen, []fyne.Disableable{pp.sharpenAmount}},
		{"denoise", pp.denoise, []fyne.Disableable{pp.denoiseMode}},
		{"upscale", pp.upscaleVideo, []fyne.Disableable{pp.upscaleTarget}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Every option defaults to off, so its sub-controls start disabled.
			for _, checked := range []bool{false, true, false} {
				if checked != tt.toggle.Checked {
					tt.toggle.SetChecked(checked)
				}
				for i, dependent := range tt.dependents {
					if dependent.Disabled() == checked {
						t.Errorf("checked=%v: dependent %d Disabled() = %v", checked, i, dependent.Disabled())
					}
				}
			}
		})
	}
}
