package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"math"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

func TestExitCodeFromError(t *testing.T) {
	// Re-running the test binary with an unknown flag makes the flag package
	// exit with status 2, which gives us a genuine *exec.ExitError.
	cmd := exec.Command(os.Args[0], "-test.no-such-flag")
	exitErr := cmd.Run()
	if exitErr == nil {
		t.Fatal("expected re-run of test binary with bad flag to fail")
	}

	tests := []struct {
		name string
		err  error
		want ExitCode
	}{
		{"exec exit error keeps process code", exitErr, ExitCode(2)},
		{"wrapped exec exit error keeps process code", fmt.Errorf("update: %w", exitErr), ExitCode(2)},
		{"plain error maps to update failed", errors.New("boom"), ExitUpdateFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCodeFromError(tt.err); got != tt.want {
				t.Errorf("exitCodeFromError(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestRequestCancel(t *testing.T) {
	app := &DownloaderApp{}

	if app.RequestCancel() {
		t.Error("RequestCancel() with no cancel func = true, want false")
	}

	calls := 0
	app.SetCancelFunc(func() { calls++ })

	if !app.RequestCancel() {
		t.Error("RequestCancel() with cancel func = false, want true")
	}
	if app.RequestCancel() {
		t.Error("second RequestCancel() = true, want false (cancel func is consumed)")
	}
	if calls != 1 {
		t.Errorf("cancel func called %d times, want 1", calls)
	}
}

func TestSetCancelFuncNilClears(t *testing.T) {
	app := &DownloaderApp{}
	app.SetCancelFunc(func() { t.Error("cleared cancel func was called") })
	app.SetCancelFunc(nil)

	if app.RequestCancel() {
		t.Error("RequestCancel() after SetCancelFunc(nil) = true, want false")
	}
}

func TestSetProgressClamps(t *testing.T) {
	tests := []struct {
		in   float64
		want float64
	}{
		{-0.5, 0},
		{0, 0},
		{0.42, 0.42},
		{1, 1},
		{1.7, 1},
	}

	for _, tt := range tests {
		app := &DownloaderApp{stats: &DownloadStats{}}
		app.setProgress(tt.in)
		if app.stats.targetPct != tt.want {
			t.Errorf("setProgress(%v): targetPct = %v, want %v", tt.in, app.stats.targetPct, tt.want)
		}
	}
}

func TestUpdateProgressRecordsSize(t *testing.T) {
	app := &DownloaderApp{stats: &DownloadStats{}}

	app.updateProgress(0.25, "15.2MiB")

	if app.stats.targetPct != 0.25 {
		t.Errorf("targetPct = %v, want 0.25", app.stats.targetPct)
	}
	if app.stats.lastSize != "15.2MiB" {
		t.Errorf("lastSize = %q, want %q", app.stats.lastSize, "15.2MiB")
	}
	if math.Abs(app.stats.downloadedRaw-15.2) > 0.001 {
		t.Errorf("downloadedRaw = %v, want 15.2", app.stats.downloadedRaw)
	}
	if app.stats.unit != "MiB" {
		t.Errorf("unit = %q, want %q", app.stats.unit, "MiB")
	}
}

func TestUpdateProgressEmptySizeKeepsStats(t *testing.T) {
	app := &DownloaderApp{stats: &DownloadStats{lastSize: "3.0GiB", downloadedRaw: 3, unit: "GiB"}}

	app.updateProgress(0.5, "")

	if app.stats.targetPct != 0.5 {
		t.Errorf("targetPct = %v, want 0.5", app.stats.targetPct)
	}
	if app.stats.lastSize != "3.0GiB" || app.stats.downloadedRaw != 3 || app.stats.unit != "GiB" {
		t.Errorf("stats changed on empty size: lastSize=%q downloadedRaw=%v unit=%q",
			app.stats.lastSize, app.stats.downloadedRaw, app.stats.unit)
	}
}

func TestSetProgressNowRequestsSnap(t *testing.T) {
	app := &DownloaderApp{stats: &DownloadStats{targetPct: 0.9}}

	app.setProgressNow(0.3)

	pct, snap := app.stats.takeTarget()
	if pct != 0.3 || !snap {
		t.Errorf("takeTarget() = (%v, %v), want (0.3, true)", pct, snap)
	}
	if _, snap := app.stats.takeTarget(); snap {
		t.Error("takeTarget() still reports a snap after it was taken")
	}
}

func TestDownloadStatsResetClearsPreviousDownload(t *testing.T) {
	stats := &DownloadStats{}
	stats.recordSize("15.2MiB")
	stats.setTarget(0.8, false)

	stats.reset()

	if size, raw, unit := stats.sizeSnapshot(); size != "" || raw != 0 || unit != "" {
		t.Errorf("sizeSnapshot() = (%q, %v, %q), want zero values", size, raw, unit)
	}
	if pct, snap := stats.takeTarget(); pct != 0 || !snap {
		t.Errorf("takeTarget() = (%v, %v), want (0, true)", pct, snap)
	}
}

// runSmootherFor runs the progress smoother for d and returns once it has
// exited. The test driver runs fyne.Do synchronously on the smoother's
// goroutine, so the widget may only be read after this returns.
func runSmootherFor(app *DownloaderApp, d time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	app.runProgressSmoother(ctx)
}

func TestProgressSmootherEasesTowardsTarget(t *testing.T) {
	_ = test.NewApp()
	app := &DownloaderApp{ui: NewUIWidgets(), stats: &DownloadStats{}}

	app.setProgress(0.5)
	runSmootherFor(app, 10*fpsInterval)

	if got := app.ui.download.progress.Value; got <= 0 || got > 0.5 {
		t.Errorf("progress.Value = %v, want in (0, 0.5]", got)
	}
}

func TestProgressSmootherAppliesPendingSnapOnShutdown(t *testing.T) {
	_ = test.NewApp()
	app := &DownloaderApp{ui: NewUIWidgets(), stats: &DownloadStats{}}

	app.setProgressNow(1)
	runSmootherFor(app, 0)

	if got := app.ui.download.progress.Value; got != 1 {
		t.Errorf("progress.Value = %v, want 1", got)
	}
}

func TestApplyPreferencesKeepsWidgetsForEmptyValues(t *testing.T) {
	_ = test.NewApp()
	ui := NewUIWidgets()
	ui.download.format.Options = []string{"MP4", "MKV"}
	ui.download.format.SetSelected("MKV")
	ui.download.quality.Options = []string{"Best Quality", "720p"}
	ui.download.quality.SetSelected("720p")
	ui.download.path.SetText("/existing/path")

	applyPreferencesToWidgets(ui, AppPreferences{})

	if got := ui.download.format.Selected; got != "MKV" {
		t.Errorf("format.Selected = %q, want %q", got, "MKV")
	}
	if got := ui.download.quality.Selected; got != "720p" {
		t.Errorf("quality.Selected = %q, want %q", got, "720p")
	}
	if got := ui.download.path.Text; got != "/existing/path" {
		t.Errorf("path.Text = %q, want %q", got, "/existing/path")
	}
}

func TestShowDownloadPhaseHoldsProgressBar(t *testing.T) {
	_ = test.NewApp()
	app := newDownloaderApp(test.NewWindow(nil))
	app.uiManager.createUI()
	app.setProgress(1)

	app.showDownloadPhase(phaseMerging)
	app.statusThrottle.Flush()

	if pct, snap := app.stats.takeTarget(); pct != phaseProgress || !snap {
		t.Errorf("progress target = %v (snap %v), want a snap to %v", pct, snap, phaseProgress)
	}
	if got := app.ui.download.status.Text; got != "Status: Merging…" {
		t.Errorf("status = %q, want Status: Merging…", got)
	}
}

func TestAppendOutputKeepsDebugLinesOutOfTheView(t *testing.T) {
	_ = test.NewApp()
	app := newDownloaderApp(test.NewWindow(nil))
	var shown []string
	app.onLogLine = func(line string, _ color.Color) { shown = append(shown, line) }
	logDir := t.TempDir()
	if _, err := app.logSvc.OpenSessionLog(logDir); err != nil {
		t.Fatal(err)
	}

	app.appendOutput("[debug] Encodings: locale cp1252", nil)
	app.appendOutput("[youtube] Extracting URL", nil)
	app.showDebug.Store(true)
	app.appendOutput("[debug] yt-dlp version 2026.09.26", nil)
	app.logSvc.CloseSessionLog()

	want := []string{"[youtube] Extracting URL", "[debug] yt-dlp version 2026.09.26"}
	if !slices.Equal(shown, want) {
		t.Errorf("shown = %q, want %q", shown, want)
	}
	data, err := os.ReadFile(SessionLogPath(logDir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[debug] Encodings: locale cp1252") {
		t.Errorf("log file is missing the hidden debug line:\n%s", data)
	}
}
