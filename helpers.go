// helpers.go — Utility functions that support the rest of the application.
//
// Sections:
//   - File I/O: save-folder launcher.
//   - UI updates: status label, log output, status dot animation, progress bar.
//   - Preference management: applyPreferencesToWidgets.
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
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
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
	fyne.Do(func() {
		label := canvas.NewText(line, col)
		label.TextSize = theme.TextSize()

		app.ui.download.logList.Add(label)

		if len(app.ui.download.logList.Objects) > app.logSvc.BufferLimit() {
			app.ui.download.logList.Objects = app.ui.download.logList.Objects[len(app.ui.download.logList.Objects)-app.logSvc.BufferLimit():]
		}

		app.ui.download.logList.Refresh()
		app.ui.download.output.ScrollToBottom()
	})

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
		app.stats.lastSize = size
		fmt.Sscanf(size, "%f%s", &app.stats.downloadedRaw, &app.stats.unit)
	}
}

// setStatusIndicator updates the status dot color to reflect the current
// download state. It also manages the pulse goroutine:
//   - "active"   → starts or continues the pulsing animation (cyan)
//   - "idle"     → stops pulsing, shows grey
//   - "success"  → stops pulsing, shows green
//   - "failed"   → stops pulsing, shows red
//   - "canceled" → stops pulsing, shows orange
func (app *DownloaderApp) setStatusIndicator(state string) {
	fyne.Do(func() {
		// Stop any existing pulse goroutine.
		if app.stopPulse != nil {
			close(app.stopPulse)
			app.stopPulse = nil
		}

		switch state {
		case "active":
			app.stopPulse = make(chan struct{})
			stopCh := app.stopPulse
			go func() {
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
							app.ui.download.statusDot.FillColor = color.RGBA{R: accentCyan.R, G: accentCyan.G, B: accentCyan.B, A: alpha}
							app.ui.download.statusDot.Refresh()
						})
					}
				}
			}()
		case "processing":
			// Pulsing purple to distinguish post-processing from active download.
			app.stopPulse = make(chan struct{})
			stopCh := app.stopPulse
			go func() {
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
							app.ui.download.statusDot.FillColor = color.RGBA{R: colDotProcessing.R, G: colDotProcessing.G, B: colDotProcessing.B, A: alpha}
							app.ui.download.statusDot.Refresh()
						})
					}
				}
			}()
		case "success":
			app.ui.download.statusDot.FillColor = colDotSuccess
		case "failed":
			app.ui.download.statusDot.FillColor = colDotFailed
		case "canceled":
			app.ui.download.statusDot.FillColor = colDotCanceled
		default: // "idle"
			app.ui.download.statusDot.FillColor = colDotIdle
		}
		app.ui.download.statusDot.Refresh()
	})
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
	app.stats.targetPct = pct
}

// setProgressNow immediately sets the progress bar to the given value,
// bypassing the smooth interpolation. Use for resets or completion snaps.
func (app *DownloaderApp) setProgressNow(pct float64) {
	fyne.Do(func() {
		app.ui.download.progress.SetValue(pct)
	})
	app.stats.targetPct = pct
}

// ── Preference management ────────────────────────────────────────────────────

// applyPreferencesToWidgets writes the values from an AppPreferences struct
// into the corresponding UI widgets. Called at startup and after a reset.
func applyPreferencesToWidgets(ui *UIWidgets, p AppPreferences) {
	if p.Format != "" {
		ui.download.format.SetSelected(p.Format)
	}
	if p.Quality != "" {
		ui.download.quality.SetSelected(p.Quality)
	}
	if p.SavedPath != "" {
		ui.download.path.SetText(p.SavedPath)
	}
	ui.prefs.themeMode.SetSelected(p.ThemeMode)
	ui.prefs.savePrefs.SetChecked(p.SavePrefs)
	ui.postProcess.smoothMotion.SetChecked(p.SmoothMotion)
	ui.postProcess.smoothMotionMode.SetSelected(p.SmoothMotionMode)
	ui.postProcess.smoothMotionFPS.SetValue(p.SmoothFPS)
	ui.postProcess.sharpen.SetChecked(p.Sharpen)
	ui.postProcess.sharpenAmount.SetValue(p.SharpenAmount)
	ui.postProcess.normalizeAudio.SetChecked(p.NormalizeAudio)
	ui.postProcess.vividMode.SetChecked(p.VividMode)
	ui.postProcess.denoise.SetChecked(p.Denoise)
	ui.postProcess.denoiseMode.SetSelected(p.DenoiseMode)
	ui.postProcess.hdrToSdr.SetChecked(p.HDRToSDR)
	ui.postProcess.deband.SetChecked(p.Deband)
	ui.postProcess.autoCrop.SetChecked(p.AutoCrop)
	ui.postProcess.stabilize.SetChecked(p.Stabilize)
	ui.postProcess.deinterlace.SetChecked(p.Deinterlace)
	ui.postProcess.nightMode.SetChecked(p.NightMode)
	ui.postProcess.upscaleVideo.SetChecked(p.UpscaleVideo)
	ui.postProcess.upscaleTarget.SetSelected(p.UpscaleTarget)
	ui.postProcess.gpuBackend.SetSelected(p.GPUBackend)
	ui.prefs.cookies.SetText(p.CookiesPath)
	ui.download.batchMode.SetChecked(p.BatchMode)
	ui.download.saveLog.SetChecked(p.SaveLog)
	ui.download.notify.SetChecked(p.Notify)
	ui.download.autoRetry.SetChecked(p.AutoRetry)
	ui.postProcess.enablePostProcess.SetChecked(p.EnablePostProcess)
	ui.prefs.logLimit.SetSelected(p.LogLimit)
	ui.prefs.maxSpeed.SetText(p.MaxSpeed)
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
