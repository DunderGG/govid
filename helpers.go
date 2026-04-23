package main

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
)

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

// savePreferences persists the format, quality, and save-path selections
// so they are restored on the next launch.
func (app *DownloaderApp) savePreferences(savePath string) {
	prefs := fyne.CurrentApp().Preferences()
	prefs.SetString("savedPath", savePath)
	prefs.SetString("format", app.ui.format.Selected)
	prefs.SetString("quality", app.ui.quality.Selected)
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

// runUpdateInUI in helpers.go; the standalone updateYtDlp is kept here to satisfy
// the -update flag path in main, which calls os.Exit after this returns.
