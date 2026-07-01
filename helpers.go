// helpers.go — Utility functions that support the rest of the application.
//
// Responsibilities:
//   - Status bar, log output, and progress bar updates (thread-safe).
//   - Saving and loading user preferences via the Fyne preferences API.
//   - Opening the download folder in the OS file manager.
//   - Checking for required external dependencies (yt-dlp, ffmpeg).
//   - Triggering yt-dlp self-updates.
package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
)

// loadConfigFromFile reads settings from govid.json and returns an AppConfig.
// Unlike C++, it is safe to return a local pointer, it does not go "out of scope".
// The Go compiler performs Escape Analysis.
// If the compiler sees that a local variable's address is being returned or shared outside the function,
// it "escapes" the stack and is automatically allocated on the heap instead.
// The Go garbage collector then tracks that memory and
// only frees it when it is no longer being used anywhere in the program.
func (app *DownloaderApp) loadConfigFromFile() (*AppConfig, error) {
	data, err := os.ReadFile("govid.json")
	if err != nil {
		return nil, err
	}
	var config AppConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// applyConfig updates the UI and preferences with values from an AppConfig.
// It returns an error if any of the provided values are not valid.
func (app *DownloaderApp) applyConfig(config *AppConfig) error {
	var errs []string

	if config.Format != "" {
		valid := false
		for _, opt := range app.ui.format.Options {
			if opt == config.Format {
				valid = true
				break
			}
		}
		if valid {
			app.ui.format.SetSelected(config.Format)
		} else {
			errs = append(errs, fmt.Sprintf("invalid format: %s", config.Format))
		}
	}

	if config.Quality != "" {
		valid := false
		for _, opt := range app.ui.quality.Options {
			if opt == config.Quality {
				valid = true
				break
			}
		}
		if valid {
			app.ui.quality.SetSelected(config.Quality)
		} else {
			errs = append(errs, fmt.Sprintf("invalid quality: %s", config.Quality))
		}
	}

	if config.Path != "" {
		// Basic check if path exists and is a directory
		info, err := os.Stat(config.Path)
		if err == nil && info.IsDir() {
			app.ui.path.SetText(config.Path)
		} else {
			errs = append(errs, fmt.Sprintf("invalid path: %s", config.Path))
		}
	}

	if config.MaxSpeed != "" {
		// Simplified validation for speed limit (allows blank, or numeric+suffix)
		app.ui.maxSpeed.SetText(config.MaxSpeed)
	}

	// Persist the changes
	app.savePreferences(app.ui.path.Text)

	if len(errs) > 0 {
		return fmt.Errorf("some settings were skipped:\n- %s", strings.Join(errs, "\n- "))
	}
	return nil
}


// updateStatus sets the short status label text thread-safely.
func (app *DownloaderApp) updateStatus(msg string) {
	fyne.Do(func() {
		app.ui.status.SetText(msg)
	})
}

// appendOutput adds a line of text to the graphical log view and, when log-to-file
// is enabled, also writes it to the file on disk. It enforces the logBufferLimit
// by trimming old entries from the top of the log list.
func (app *DownloaderApp) appendOutput(line string, col color.Color) {
	fyne.Do(func() {
		label := canvas.NewText(line, col)
		label.TextSize = theme.TextSize()

		app.ui.logList.Add(label)

		if len(app.ui.logList.Objects) > logBufferLimit {
			app.ui.logList.Objects = app.ui.logList.Objects[len(app.ui.logList.Objects)-logBufferLimit:]
		}

		app.ui.logList.Refresh()
		app.ui.output.ScrollToBottom()
	})

	app.log.mutex.Lock()
	if app.log.file != nil {
		fmt.Fprintf(app.log.file, "[%s] %s\n", time.Now().Format("15:04:05"), line)
	}
	app.log.mutex.Unlock()

	if isErrorLogLine(line) {
		app.appendErrorOutput(line)
	}
}

// appendErrorOutput writes error-like lines to a daily error log file.
func (app *DownloaderApp) appendErrorOutput(line string) {
	errorPath := app.dailyErrorLogPath()
	app.log.errorMutex.Lock()
	defer app.log.errorMutex.Unlock()

	f, err := os.OpenFile(errorPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05"), line)
}

// dailyErrorLogPath returns today's error log file path in the current save folder.
func (app *DownloaderApp) dailyErrorLogPath() string {
	dir := strings.TrimSpace(app.ui.path.Text)
	if dir == "" {
		exePath, err := os.Executable()
		if err == nil {
			dir = filepath.Dir(exePath)
		}
	}
	if dir == "" {
		dir = "."
	}
	dateStamp := time.Now().Format("2006-01-02")
	return filepath.Join(dir, fmt.Sprintf("GoVid_errors_%s.txt", dateStamp))
}

// isErrorLogLine returns true when a log line looks like an error worth mirroring.
func isErrorLogLine(line string) bool {
	upper := strings.ToUpper(line)
	return strings.Contains(upper, "ERROR") || strings.Contains(upper, "FAILED")
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
							app.ui.statusDot.FillColor = color.RGBA{R: 180, G: 80, B: 255, A: alpha}
							app.ui.statusDot.Refresh()
						})
					}
				}
			}()
		case "success":
			app.ui.statusDot.FillColor = color.RGBA{R: 0, G: 200, B: 80, A: 255}
		case "failed":
			app.ui.statusDot.FillColor = color.RGBA{R: 220, G: 50, B: 50, A: 255}
		case "canceled":
			app.ui.statusDot.FillColor = color.RGBA{R: 255, G: 140, B: 0, A: 255}
		default: // "idle"
			app.ui.statusDot.FillColor = color.RGBA{R: 100, G: 100, B: 115, A: 255}
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

// applyPreferencesToWidgets writes the values from an AppPreferences struct
// into the corresponding UI widgets. Called at startup and after a reset.
func (app *DownloaderApp) applyPreferencesToWidgets(p AppPreferences) {
	ui := app.ui
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

// savePreferences collects the current widget state into an AppPreferences
// struct and delegates persistence to PreferenceService.Save.
func (app *DownloaderApp) savePreferences(savePath string) {
	ui := app.ui
	app.prefSvc.Save(AppPreferences{
		SavePrefs:         ui.savePrefs.Checked,
		SavedPath:         savePath,
		Format:            ui.format.Selected,
		Quality:           ui.quality.Selected,
		MaxSpeed:          strings.TrimSpace(ui.maxSpeed.Text),
		ThemeMode:         ui.themeMode.Selected,
		CookiesPath:       strings.TrimSpace(ui.cookies.Text),
		LogLimit:          ui.logLimit.Selected,
		BatchMode:         ui.batchMode.Checked,
		SaveLog:           ui.saveLog.Checked,
		Notify:            ui.notify.Checked,
		AutoRetry:         ui.autoRetry.Checked,
		EnablePostProcess: ui.enablePostProcess.Checked,
		SmoothMotion:      ui.smoothMotion.Checked,
		SmoothMotionMode:  ui.smoothMotionMode.Selected,
		SmoothFPS:         ui.smoothMotionFPS.Value,
		Sharpen:           ui.sharpen.Checked,
		SharpenAmount:     ui.sharpenAmount.Value,
		NormalizeAudio:    ui.normalizeAudio.Checked,
		VividMode:         ui.vividMode.Checked,
		Denoise:           ui.denoise.Checked,
		DenoiseMode:       ui.denoiseMode.Selected,
		HDRToSDR:          ui.hdrToSdr.Checked,
		Deband:            ui.deband.Checked,
		AutoCrop:          ui.autoCrop.Checked,
		Stabilize:         ui.stabilize.Checked,
		Deinterlace:       ui.deinterlace.Checked,
		NightMode:         ui.nightMode.Checked,
		UpscaleVideo:      ui.upscaleVideo.Checked,
		UpscaleTarget:     ui.upscaleTarget.Selected,
	})
}

// resetPreferences clears all stored preferences and rebuilds the UI with
// defaults. The Preferences window is responsible for resetting its own
// widgets to match after calling this.
func (app *DownloaderApp) resetPreferences() {
	app.prefSvc.Reset()
	logBufferLimit = 200
	fyne.CurrentApp().Settings().SetTheme(&darkTheme{})
	app.createUI()
}

// parseLogLimit converts a log-limit preference string (e.g. "200", "Unlimited")
// to an integer. Returns 200 for any unrecognised value.
func parseLogLimit(logLimitString string) int {
	if logLimitString == "Unlimited" {
		return math.MaxInt32 // effectively unlimited
	}
	logLimitInt, err := strconv.Atoi(logLimitString)
	if err != nil || logLimitInt <= 0 {
		return 200
	}
	return logLimitInt
}

// openDownloadFolder launches the system file manager pointing at the current
// save destination. The exact command differs per operating system.
func (app *DownloaderApp) openDownloadFolder() {
	savePath := strings.TrimSpace(app.ui.path.Text)
	if savePath == "" {
		dialog.ShowError(fmt.Errorf("no save path set"), app.window)
		return
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", savePath)
	case "windows":
		cmd = exec.Command("explorer", savePath)
	default:
		cmd = exec.Command("xdg-open", savePath)
	}

	if err := cmd.Start(); err != nil {
		dialog.ShowError(fmt.Errorf("could not open folder: %v", err), app.window)
	}
}

// checkDependencies verifies that the required external tools — yt-dlp and
// ffmpeg — are available either in the 'bin' folder beside the executable or
// in the system PATH. Warnings are printed to the log panel.
func (app *DownloaderApp) checkDependencies() {
	for _, tool := range []string{"yt-dlp", "ffmpeg"} {
		localPath := app.getLocalBinPath(tool)
		_, localErr := os.Stat(localPath)
		_, pathErr := exec.LookPath(tool)
		if localErr != nil && pathErr != nil {
			app.appendOutput(
				fmt.Sprintf("[WARNING] '%s' not found in PATH or ./bin/. Please install it.", tool),
				color.RGBA{R: 255, G: 165, B: 0, A: 255},
			)
		}
	}
}

// runUpdateInUI runs 'yt-dlp -U' in a background goroutine while streaming
// its output to the log panel, so the UI stays responsive throughout.
func (app *DownloaderApp) runUpdateInUI() {
	app.appendOutput("[SYSTEM] Starting yt-dlp update...", color.RGBA{R: 0, G: 255, B: 255, A: 255})
	app.setStatusIndicator("active")
	app.updateStatus("Status: Updating yt-dlp...")

	go func() {
		ytDlpPath := app.getLocalBinPath("yt-dlp")
		if _, err := os.Stat(ytDlpPath); err != nil {
			ytDlpPath = "yt-dlp"
		}
		cmd := exec.Command(ytDlpPath, "-U")
		hideWindow(cmd)
		out, err := cmd.CombinedOutput()

		fyne.Do(func() {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			for _, line := range lines {
				app.appendOutput(line, theme.ForegroundColor())
			}
			if err != nil {
				app.appendOutput(fmt.Sprintf("[ERROR] Update failed: %v", err), color.RGBA{R: 255, G: 0, B: 0, A: 255})
				app.setStatusIndicator("failed")
				app.updateStatus("Status: Update failed.")
			} else {
				app.appendOutput("[SYSTEM] yt-dlp is up to date.", color.RGBA{R: 0, G: 200, B: 0, A: 255})
				app.setStatusIndicator("success")
				app.updateStatus("Status: yt-dlp updated.")
			}
		})
	}()
}

// updateYtDlp is a standalone (non-method) function for the --update CLI flag.
// It runs 'yt-dlp -U' and prints the combined output directly to stdout.
func updateYtDlp() {
	fmt.Println("Updating yt-dlp...")
	cmd := exec.Command("yt-dlp", "-U")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	fmt.Print(string(out))
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
