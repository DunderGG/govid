// helpers.go — Utility functions that support the rest of the application.
//
// Sections:
//   - File I/O: save-folder launcher.
//   - UI updates: status label, log output, status dot animation, progress bar.
//   - Preference management: applyPreferencesToWidgets, resetPreferences, rebuildUI.
//   - External tools: thin delegates to DependencyService.
package main

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"os"
	"os/exec"
	"path/filepath"
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

// ── File I/O ─────────────────────────────────────────────────────────────────

// configFileName is the optional JSON override file read by "Load from Config".
const configFileName = "govid.json"

// openDownloadFolder launches the system file manager pointing at the current
// save destination. The platform-specific command is provided by openFolderCommand.
func (app *DownloaderApp) openDownloadFolder() {
	savePath := strings.TrimSpace(app.ui.path.Text)
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
		app.ui.status.SetText(msg)
	})
}

// appendOutput adds a line of text to the graphical log view and, when log-to-file
// is enabled, also writes it to the session log on disk. Error-like lines are
// additionally mirrored to the daily error log via LogService.
func (app *DownloaderApp) appendOutput(line string, col color.Color) {
	fyne.Do(func() {
		label := canvas.NewText(line, col)
		label.TextSize = theme.TextSize()

		app.ui.logList.Add(label)

		if len(app.ui.logList.Objects) > app.logSvc.BufferLimit() {
			app.ui.logList.Objects = app.ui.logList.Objects[len(app.ui.logList.Objects)-app.logSvc.BufferLimit():]
		}

		app.ui.logList.Refresh()
		app.ui.output.ScrollToBottom()
	})

	app.logSvc.WriteToFile(line)

	if IsErrorLine(line) {
		dir := strings.TrimSpace(app.ui.path.Text)
		if dir == "" {
			if exePath, err := os.Executable(); err == nil {
				dir = filepath.Dir(exePath)
			}
		}
		if dir == "" {
			dir = "."
		}
		app.logSvc.WriteToErrorLog(line, dir)
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
							app.ui.statusDot.FillColor = color.RGBA{R: accentCyan.R, G: accentCyan.G, B: accentCyan.B, A: alpha}
							app.ui.statusDot.Refresh()
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
							app.ui.statusDot.FillColor = color.RGBA{R: colDotProcessing.R, G: colDotProcessing.G, B: colDotProcessing.B, A: alpha}
							app.ui.statusDot.Refresh()
						})
					}
				}
			}()
		case "success":
			app.ui.statusDot.FillColor = colDotSuccess
		case "failed":
			app.ui.statusDot.FillColor = colDotFailed
		case "canceled":
			app.ui.statusDot.FillColor = colDotCanceled
		default: // "idle"
			app.ui.statusDot.FillColor = colDotIdle
		}
		app.ui.statusDot.Refresh()
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
		app.ui.progress.SetValue(pct)
	})
	app.stats.targetPct = pct
}

// ── Preference management ────────────────────────────────────────────────────

// applyPreferencesToWidgets writes the values from an AppPreferences struct
// into the corresponding UI widgets. Called at startup and after a reset.
func (app *DownloaderApp) applyPreferencesToWidgets(p AppPreferences) {
	ui := app.ui
	if p.Format != "" {
		ui.format.SetSelected(p.Format)
	}
	if p.Quality != "" {
		ui.quality.SetSelected(p.Quality)
	}
	if p.SavedPath != "" {
		ui.path.SetText(p.SavedPath)
	}
	ui.themeMode.SetSelected(p.ThemeMode)
	ui.savePrefs.SetChecked(p.SavePrefs)
	ui.smoothMotion.SetChecked(p.SmoothMotion)
	ui.smoothMotionMode.SetSelected(p.SmoothMotionMode)
	ui.smoothMotionFPS.SetValue(p.SmoothFPS)
	ui.sharpen.SetChecked(p.Sharpen)
	ui.sharpenAmount.SetValue(p.SharpenAmount)
	ui.normalizeAudio.SetChecked(p.NormalizeAudio)
	ui.vividMode.SetChecked(p.VividMode)
	ui.denoise.SetChecked(p.Denoise)
	ui.denoiseMode.SetSelected(p.DenoiseMode)
	ui.hdrToSdr.SetChecked(p.HDRToSDR)
	ui.deband.SetChecked(p.Deband)
	ui.autoCrop.SetChecked(p.AutoCrop)
	ui.stabilize.SetChecked(p.Stabilize)
	ui.deinterlace.SetChecked(p.Deinterlace)
	ui.nightMode.SetChecked(p.NightMode)
	ui.upscaleVideo.SetChecked(p.UpscaleVideo)
	ui.upscaleTarget.SetSelected(p.UpscaleTarget)
	ui.cookies.SetText(p.CookiesPath)
	ui.batchMode.SetChecked(p.BatchMode)
	ui.saveLog.SetChecked(p.SaveLog)
	ui.notify.SetChecked(p.Notify)
	ui.autoRetry.SetChecked(p.AutoRetry)
	ui.enablePostProcess.SetChecked(p.EnablePostProcess)
	ui.logLimit.SetSelected(p.LogLimit)
	ui.maxSpeed.SetText(p.MaxSpeed)
}
// resetPreferences clears the stored preference data and resets the log
// buffer to its default limit. Call rebuildUI afterwards to complete the
// visual reset.
func (app *DownloaderApp) resetPreferences() {
	app.prefSvc.Reset()
	app.logSvc.SetBufferLimit(200)
}

// rebuildUI applies the default dark theme and recreates the main window
// layout. Called after resetPreferences to complete a full application reset.
func (app *DownloaderApp) rebuildUI() {
	fyne.CurrentApp().Settings().SetTheme(&darkTheme{})
	app.createUI()
}
// ── External tools ───────────────────────────────────────────────────────────

// checkDependencies verifies that the required external tools — yt-dlp and
// ffmpeg — are available either in the 'bin' folder beside the executable or
// in the system PATH. Warnings are printed to the log panel.
func (app *DownloaderApp) checkDependencies() {
	app.depSvc.Check(func(msg string) {
		app.appendOutput(msg, colWarning)
	})
}

// runUpdateInUI sets the initial UI state for an update and delegates
// execution to DependencyService, which runs yt-dlp -U in a background
// goroutine and reports progress via UpdateCallbacks.
func (app *DownloaderApp) runUpdateInUI() {
	app.appendOutput("[SYSTEM] Starting yt-dlp update...", colSystem)
	app.setStatusIndicator("active")
	app.updateStatus("Status: Updating yt-dlp...")
	app.depSvc.RunUpdate(UpdateCallbacks{
		OnLog:     app.appendOutput,
		OnStatus:  app.updateStatus,
		OnSuccess: func() { app.setStatusIndicator("success") },
		OnFailure: func() { app.setStatusIndicator("failed") },
	})
}
