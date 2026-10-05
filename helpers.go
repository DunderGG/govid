// helpers.go — Utility functions that support the rest of the application.
//
// Sections:
//   - File I/O: save-folder launcher.
//   - UI updates: status label, log output, status dot animation, progress bar.
//   - External tools: background GPU capability detection.
package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"math"
	"os/exec"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// ── General ──────────────────────────────────────────────────────────────────

// exitCodeFromError maps an error to a process exit code.
// It preserves the original process exit code for exec failures when available.
func exitCodeFromError(err error) ExitCode {
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return ExitCode(exitErr.ExitCode())
	}
	return ExitUpdateFailed
}

// SetCancelFunc replaces the active cancellation callback for the current job.
func (app *DownloaderApp) SetCancelFunc(cancel context.CancelFunc) {
	app.cancelMu.Lock()
	defer app.cancelMu.Unlock()
	app.cancelFn = cancel
}

// RequestCancel safely invokes the active cancellation callback once.
// It returns true if a callback was present and called.
func (app *DownloaderApp) RequestCancel() bool {
	app.cancelMu.Lock()
	cancel := app.cancelFn
	app.cancelFn = nil
	app.cancelMu.Unlock()

	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// ── File I/O ─────────────────────────────────────────────────────────────────

// configFileName is the optional JSON override file read by "Load from Config".
const configFileName = "govid.json"

// openDownloadFolder launches the system file manager pointing at the current
// save destination. The platform-specific command is provided by openFolderCommand.
func (app *DownloaderApp) openDownloadFolder() {
	savePath := strings.TrimSpace(app.ui.download.path.Text)
	if savePath == "" {
		dialog.ShowError(fmt.Errorf("no save path set"), app.window)
		return
	}
	if err := openFolderCommand(savePath).Start(); err != nil {
		dialog.ShowError(fmt.Errorf("could not open folder: %v", err), app.window)
	}
}

// ── UI updates ───────────────────────────────────────────────────────────────

// updateStatus sets the short status label text thread-safely.
func (app *DownloaderApp) updateStatus(msg string) {
	fyne.Do(func() {
		app.ui.download.status.SetText(msg)
	})
}

// appendOutput adds a line of text to the graphical log view and, when log-to-file
// is enabled, also writes it to the session log on disk. Error-like lines are
// additionally mirrored to the daily error log via LogService.
func (app *DownloaderApp) appendOutput(line string, col color.Color) {
	app.onLogLine(line, col)

	app.logSvc.WriteToFile(line)

	if IsErrorLine(line) {
		app.logSvc.WriteToErrorLog(line)
	}
}

// updateProgress is a ProcessCallbacks.OnProgress handler: it advances the
// progress bar and, when size is non-empty, records it in the session stats.
func (app *DownloaderApp) updateProgress(pct float64, size string) {
	app.setProgress(pct)
	if size != "" {
		app.stats.recordSize(size)
	}
}

// StatusState is the state shown by the status dot next to the status label.
type StatusState int

const (
	// StatusIdle shows a grey dot: nothing is running.
	StatusIdle StatusState = iota
	// StatusActive pulses cyan while a download or yt-dlp update runs.
	StatusActive
	// StatusProcessing pulses purple while post-processing runs, to
	// distinguish it from downloading.
	StatusProcessing
	// StatusSuccess shows a green dot.
	StatusSuccess
	// StatusFailed shows a red dot.
	StatusFailed
	// StatusCanceled shows an orange dot.
	StatusCanceled
)

// setStatusIndicator updates the status dot to reflect state, starting the
// pulse animation for the running states and stopping it for the others.
func (app *DownloaderApp) setStatusIndicator(state StatusState) {
	fyne.Do(func() {
		app.stopStatusPulse()

		switch state {
		case StatusActive:
			app.startStatusPulse(accentCyan)
		case StatusProcessing:
			app.startStatusPulse(colDotProcessing)
		case StatusSuccess:
			app.ui.download.statusDot.FillColor = colDotSuccess
		case StatusFailed:
			app.ui.download.statusDot.FillColor = colDotFailed
		case StatusCanceled:
			app.ui.download.statusDot.FillColor = colDotCanceled
		default: // StatusIdle
			app.ui.download.statusDot.FillColor = colDotIdle
		}
		app.ui.download.statusDot.Refresh()
	})
}

// startStatusPulse launches a goroutine that pulses the status dot's alpha in
// the given base colour until stopStatusPulse is called. Must be called on
// the UI thread.
func (app *DownloaderApp) startStatusPulse(base color.RGBA) {
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	app.stopPulse, app.pulseDone = stopCh, doneCh

	go func() {
		defer close(doneCh)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		t := 0.0
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				t += 0.1
				alpha := uint8(128 + 127*math.Sin(t))
				fyne.Do(func() {
					// A frame queued just before the pulse was stopped must
					// not overwrite the colour of the next state.
					select {
					case <-stopCh:
						return
					default:
					}
					app.ui.download.statusDot.FillColor = color.RGBA{R: base.R, G: base.G, B: base.B, A: alpha}
					app.ui.download.statusDot.Refresh()
				})
			}
		}
	}()
}

// stopStatusPulse stops the pulse goroutine, if any, and waits for it to exit
// so none of its writes to the status dot can overlap the caller's. Must be
// called on the UI thread. Waiting is safe there because fyne.Do never blocks
// the pulse goroutine on the UI thread.
func (app *DownloaderApp) stopStatusPulse() {
	if app.stopPulse == nil {
		return
	}
	close(app.stopPulse)
	<-app.pulseDone
	app.stopPulse, app.pulseDone = nil, nil
}

// setProgress queues a new target percentage for the smooth progress interpolation
// goroutine. Clamped to [0, 1].
func (app *DownloaderApp) setProgress(pct float64) {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	app.stats.setTarget(pct, false)
}

// setProgressNow makes the progress bar jump to the given value, bypassing the
// smooth interpolation. Use for resets or completion snaps. The jump is applied
// by runProgressSmoother, which is the only writer of the progress bar during
// a session.
func (app *DownloaderApp) setProgressNow(pct float64) {
	app.stats.setTarget(pct, true)
}

// fpsInterval is how often runProgressSmoother moves the progress bar.
const fpsInterval = 20 * time.Millisecond

// runProgressSmoother eases the progress bar towards the target percentage
// until ctx is cancelled, giving a smooth visual effect. It tracks the
// displayed value itself rather than reading the widget, so the widget is
// only ever touched inside fyne.Do.
func (app *DownloaderApp) runProgressSmoother(ctx context.Context) {
	ticker := time.NewTicker(fpsInterval)
	defer ticker.Stop()

	current := 0.0
	show := func(pct float64) {
		current = pct
		fyne.Do(func() {
			app.ui.download.progress.SetValue(pct)
		})
	}

	for {
		select {
		case <-ctx.Done():
			// Apply a pending snap (e.g. the final 100%) so it is not lost
			// when the session ends between ticks.
			if target, snap := app.stats.takeTarget(); snap {
				show(target)
			}
			return
		case <-ticker.C:
			target, snap := app.stats.takeTarget()
			switch {
			case snap:
				show(target)
			case current < target:
				step := (target - current) * 0.05
				if step < 0.001 {
					step = 0.001
				}
				show(min(current+step, target))
			}
		}
	}
}

// ── External tools ───────────────────────────────────────────────────────────

// startGPUDetection runs GPU backend capability detection in the background
// so results are cached before post-processing needs them, logging a summary
// once detection completes so it's captured in the session log for bug reports.
func (app *DownloaderApp) startGPUDetection() {
	go func() {
		capabilities := app.gpuSvc.Detect(context.Background())
		for _, line := range FormatGPUDiagnostics(capabilities) {
			app.appendOutput("[SYSTEM] GPU: "+line, colSystem)
		}
	}()
}
