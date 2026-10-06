package main

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
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
	ui.prefs.showDebug.SetChecked(true)
	app.showDebug.Store(true)
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
	if ui.prefs.showDebug.Checked || app.showDebug.Load() {
		t.Error("debug output still shown after restoring defaults")
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

// ── Log view ─────────────────────────────────────────────────────────────────

// newLogTestManager returns a UIManager with its main window built and sized
// so the log view has a real viewport.
func newLogTestManager(t *testing.T) *UIManager {
	t.Helper()
	_ = test.NewApp()
	window := test.NewWindow(nil)
	app := newDownloaderApp(window)
	app.uiManager.createUI()
	window.Resize(fyne.NewSize(windowWidth, 1000))
	return app.uiManager
}

// logTexts returns the text of every line in the log view.
func logTexts(mgr *UIManager) []string {
	var texts []string
	for _, obj := range mgr.ui.download.logList.Objects {
		texts = append(texts, obj.(*canvas.Text).Text)
	}
	return texts
}

func TestAppendLogLineBatchesUntilFlush(t *testing.T) {
	mgr := newLogTestManager(t)
	var delays []time.Duration
	var scheduled []func()
	mgr.afterFunc = func(delay time.Duration, flush func()) *time.Timer {
		delays = append(delays, delay)
		scheduled = append(scheduled, flush)
		return nil
	}

	for i := range 3 {
		mgr.appendLogLine(fmt.Sprintf("line %d", i), nil)
	}

	if len(scheduled) != 1 || delays[0] != logFlushInterval {
		t.Fatalf("flushes scheduled = %d with delays %v, want one after %v", len(scheduled), delays, logFlushInterval)
	}
	if n := len(mgr.ui.download.logList.Objects); n != 0 {
		t.Errorf("log view has %d lines before the flush, want 0", n)
	}

	scheduled[0]()

	if got := logTexts(mgr); !slices.Equal(got, []string{"line 0", "line 1", "line 2"}) {
		t.Errorf("log view = %q, want the lines in order", got)
	}

	// The next line arms a new flush.
	mgr.appendLogLine("line 3", nil)
	if len(scheduled) != 2 {
		t.Errorf("flushes scheduled = %d, want a second one after the first ran", len(scheduled))
	}
}

func TestLogViewCapsLinesEvenWhenUnlimited(t *testing.T) {
	mgr := newLogTestManager(t)
	mgr.onLogBufferLimit = func() int { return ParseBufferLimit(logLimitUnlimited) }

	for i := range maxScreenLogLines + 1000 {
		mgr.appendLogLine(fmt.Sprintf("line %d", i), nil)
	}
	mgr.flushLog()

	texts := logTexts(mgr)
	if len(texts) != maxScreenLogLines {
		t.Fatalf("log view has %d lines, want the %d-line cap", len(texts), maxScreenLogLines)
	}
	if want := fmt.Sprintf("line %d", maxScreenLogLines+999); texts[len(texts)-1] != want {
		t.Errorf("last line = %q, want %q (the oldest lines are trimmed)", texts[len(texts)-1], want)
	}
}

func TestScreenLogLimit(t *testing.T) {
	tests := []struct{ bufferLimit, want int }{
		{200, 200},
		{maxScreenLogLines, maxScreenLogLines},
		{ParseBufferLimit(logLimitUnlimited), maxScreenLogLines},
	}
	for _, tt := range tests {
		if got := screenLogLimit(tt.bufferLimit); got != tt.want {
			t.Errorf("screenLogLimit(%d) = %d, want %d", tt.bufferLimit, got, tt.want)
		}
	}
}

func TestLogViewFollowsOnlyWhenAtBottom(t *testing.T) {
	mgr := newLogTestManager(t)
	output := mgr.ui.download.output

	addLines := func(n int) {
		for i := range n {
			mgr.appendLogLine(fmt.Sprintf("line %d", i), nil)
		}
		mgr.flushLog()
	}

	addLines(200)
	if !isScrolledToBottom(output) {
		t.Fatalf("view not at the bottom after lines were added while following (offset %v)", output.Offset)
	}

	// The user scrolls up to read something; new lines must not move the view.
	output.ScrollToTop()
	addLines(50)
	if output.Offset.Y != 0 {
		t.Errorf("view moved to offset %v while scrolled up, want it left at the top", output.Offset)
	}

	// Back at the bottom, the view follows new lines again.
	output.ScrollToBottom()
	addLines(50)
	if !isScrolledToBottom(output) {
		t.Errorf("view stopped following new lines after returning to the bottom (offset %v)", output.Offset)
	}
}

func TestNoticeActionRunsAndDismisses(t *testing.T) {
	mgr := newLogTestManager(t)
	ran := 0

	mgr.showNotice(notice{id: "a", text: "first", actionLabel: "Do it", action: func() { ran++ }})
	mgr.showNotice(notice{id: "a", text: "replaced", actionLabel: "Do it", action: func() { ran++ }})
	mgr.showNotice(notice{id: "b", text: "second"})

	if len(mgr.notices) != 2 || mgr.notices[0].text != "replaced" || len(mgr.noticeBox.Objects) != 2 {
		t.Fatalf("notices = %+v (%d shown), want a (replaced) and b", mgr.notices, len(mgr.noticeBox.Objects))
	}

	test.Tap(findButton(t, mgr.noticeBox, "Do it"))

	if ran != 1 {
		t.Errorf("action ran %d times, want 1", ran)
	}
	if len(mgr.notices) != 1 || mgr.notices[0].id != "b" {
		t.Errorf("notices = %+v, want only b after acting on a", mgr.notices)
	}

	// Rebuilding the window keeps the remaining notice.
	mgr.createUI()
	if len(mgr.noticeBox.Objects) != 1 {
		t.Errorf("notice area shows %d notices after createUI, want 1", len(mgr.noticeBox.Objects))
	}

	mgr.dismissNotice("b")
	if len(mgr.notices) != 0 || len(mgr.noticeBox.Objects) != 0 {
		t.Errorf("notices = %+v, want none after dismissing b", mgr.notices)
	}
}

// walkObjects calls visit for obj and everything inside it, including the
// parts widgets such as dialogs are rendered from.
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

func TestClearTerminalOutputDropsQueuedLines(t *testing.T) {
	mgr := newLogTestManager(t)

	mgr.appendLogLine("queued before the clear", nil)
	mgr.clearTerminalOutput()
	mgr.flushLog()

	if got := logTexts(mgr); len(got) != 0 {
		t.Errorf("log view = %q, want lines queued before the clear dropped", got)
	}
}

func TestLogViewShowsLatestProgressLineInPlace(t *testing.T) {
	mgr := newLogTestManager(t)

	for _, line := range []string{
		"[download] Destination: GoVid_Clip.f137.mp4",
		"[download]  10.0% of   10.00MiB at    1.00MiB/s ETA 00:09",
		"[download]  20.0% of   10.00MiB at    1.00MiB/s ETA 00:08",
	} {
		mgr.appendLogLine(line, nil)
	}
	mgr.flushLog()
	// A later flush still updates the same line.
	mgr.appendLogLine("[download] 100% of   10.00MiB in 00:00:10 at 1.00MiB/s", nil)
	mgr.appendLogLine("[download] Destination: GoVid_Clip.f140.m4a", nil)
	mgr.appendLogLine("[download]  50.0% of    2.00MiB at    1.00MiB/s ETA 00:01", nil)
	mgr.flushLog()

	want := []string{
		"[download] Destination: GoVid_Clip.f137.mp4",
		"[download] 100% of   10.00MiB in 00:00:10 at 1.00MiB/s",
		"[download] Destination: GoVid_Clip.f140.m4a",
		"[download]  50.0% of    2.00MiB at    1.00MiB/s ETA 00:01",
	}
	if got := logTexts(mgr); !slices.Equal(got, want) {
		t.Errorf("log view = %q, want %q", got, want)
	}
}
