package main

import (
	"bufio"
	"context"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
)

// startDownload prepares the application for a new download session. It validates
// inputs, resets metrics/visuals, initializes log files if requested, and
// launches the background goroutines for progress interpolation and yt-dlp execution.
func (app *DownloaderApp) startDownload() {
	rawURL := strings.TrimSpace(app.ui.entry.Text)
	savePath := strings.TrimSpace(app.ui.path.Text)
	trimStart := strings.TrimSpace(app.ui.trimStart.Text)
	trimEnd := strings.TrimSpace(app.ui.trimEnd.Text)

	if rawURL == "" {
		dialog.ShowError(fmt.Errorf("URL cannot be empty"), app.window)
		return
	}
	if savePath == "" {
		dialog.ShowError(fmt.Errorf("save path cannot be empty"), app.window)
		return
	}

	// Trim validation: both fields must be provided together, or both left empty.
	if (trimStart == "") != (trimEnd == "") {
		dialog.ShowError(fmt.Errorf("both Trim Start and Trim End must be filled in, or both left empty"), app.window)
		return
	}
	if validateTimestamp(trimStart) != nil || validateTimestamp(trimEnd) != nil {
		dialog.ShowError(fmt.Errorf("invalid trim time format — use HH:MM:SS, MM:SS, or plain seconds"), app.window)
		return
	}

	app.savePreferences(savePath)

	// Reset UI and stats for new session.
	app.updateStatus("Status: Initializing...")
	app.setProgressNow(0)
	app.stats.targetPct = 0
	app.ui.logList.Objects = nil
	app.ui.logList.Refresh()
	app.ui.cancelBtn.Enable()
	app.setStatusIndicator("active")

	// Initialize logging to file if the option is checked.
	if app.ui.saveLog.Checked {
		logPath := filepath.Join(savePath, "GoVid_log.txt")
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			app.log.file = f
			app.appendOutput(fmt.Sprintf("[SYSTEM] Logging to: %s", logPath), color.RGBA{R: 0, G: 255, B: 255, A: 255})
		} else {
			app.appendOutput(fmt.Sprintf("[ERROR] Failed to create log file: %v", err), color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	app.cancelFn = cancel

	// Launch smoothing goroutine: interpolates progress bar towards the target
	// percentage using an easing step, giving a smooth visual effect.
	go func() {
		ticker := time.NewTicker(time.Duration(fpsInterval) * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current := app.ui.progress.Value
				target := app.stats.targetPct
				if current < target {
					step := (target - current) * 0.05
					if step < 0.001 {
						step = 0.001
					}
					newVal := current + step
					if newVal > target {
						newVal = target
					}
					fyne.Do(func() {
						app.ui.progress.SetValue(newVal)
					})
				}
			}
		}
	}()

	go app.runYtDlp(ctx, rawURL, savePath, trimStart, trimEnd)
}

// runYtDlp manages the external lifecycle of the yt-dlp process. It builds
// the command arguments based on UI selections (quality, format), executes
// the tool, and pipes its output/errors back to the UI in real-time.
func (app *DownloaderApp) runYtDlp(ctx context.Context, rawURL string, savePath string, trimStart string, trimEnd string) {
	startTime := time.Now()
	formatFlag := "bestvideo+bestaudio/best"
	extension := "mp4"

	quality := app.ui.quality.Selected
	height := ""
	switch quality {
	case "1080p":
		height = "1080"
	case "720p":
		height = "720"
	case "480p":
		height = "480"
	case "360p":
		height = "360"
	}

	selection := app.ui.format.Selected
	if strings.Contains(selection, "MP3") {
		formatFlag = "bestaudio/best"
		extension = "mp3"
	} else if strings.Contains(selection, "M4A") {
		formatFlag = "bestaudio[ext=m4a]/bestaudio/best"
		extension = "m4a"
	} else {
		if height != "" {
			formatFlag = fmt.Sprintf("bestvideo[height<=%s]+bestaudio/best[height<=%s]/best", height, height)
		}
		if strings.Contains(selection, "WebM") {
			extension = "webm"
		} else if strings.Contains(selection, "MKV") {
			extension = "mkv"
		}
	}

	qualitySuffix := ""
	if height != "" {
		qualitySuffix = "_" + quality
	}

	outputTemplate := "GoVid_%(title)s" + qualitySuffix + "." + extension
	if app.ui.duplicates.Checked {
		outputTemplate = "GoVid_%(title)s" + qualitySuffix + "_%(epoch)s." + extension
	}

	args := []string{
		"--newline", "--progress", "--verbose", "--no-part", "--no-continue",
		"-f", formatFlag, "-P", savePath, "-o", outputTemplate,
	}

	// Use bundled ffmpeg if available.
	ffmpegPath := app.getLocalBinPath("ffmpeg")
	if _, err := os.Stat(ffmpegPath); err == nil {
		args = append(args, "--ffmpeg-location", ffmpegPath)
	}

	if extension == "mp3" || extension == "m4a" {
		args = append(args, "--extract-audio", "--audio-format", extension)
	} else if extension != "" {
		args = append(args, "--recode-video", extension)
	}

	// Trim: pass start/end to yt-dlp with keyframe-accurate cuts.
	if trimStart != "" && trimEnd != "" {
		args = append(args, "--download-sections", fmt.Sprintf("*%s-%s", trimStart, trimEnd))
		args = append(args, "--force-keyframes-at-cuts")
		app.appendOutput(fmt.Sprintf("[SYSTEM] Trimming: %s → %s", trimStart, trimEnd), color.RGBA{R: 0, G: 255, B: 255, A: 255})
	}

	args = append(args, rawURL)

	ytDlpPath := app.getLocalBinPath("yt-dlp")
	if _, err := os.Stat(ytDlpPath); err != nil {
		ytDlpPath = "yt-dlp"
	}

	cmd := exec.CommandContext(ctx, ytDlpPath, args...)
	hideWindow(cmd)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		app.updateStatus(fmt.Sprintf("Failed to launch yt-dlp: %v", err))
		return
	}
	app.updateStatus("Status: Downloading...")

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			app.parseProgress(line)
			app.appendOutput(line, theme.ForegroundColor())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			var logColor color.Color
			switch {
			case strings.Contains(line, "ERROR:"):
				logColor = color.RGBA{R: 255, G: 0, B: 0, A: 255}
			case strings.Contains(line, "WARNING:"):
				logColor = color.RGBA{R: 255, G: 165, B: 0, A: 255}
			case strings.Contains(line, "[debug]"):
				logColor = color.RGBA{R: 180, G: 180, B: 180, A: 255}
			default:
				logColor = theme.ForegroundColor()
			}
			app.appendOutput(line, logColor)
		}
	}()

	err := cmd.Wait()
	durationTotal := time.Since(startTime).Seconds()
	durationFormatted := fmt.Sprintf("%.2fs", durationTotal)

	var avgSpeed string
	if durationTotal > 0 && app.stats.downloadedRaw > 0 {
		avg := app.stats.downloadedRaw / durationTotal
		avgSpeed = fmt.Sprintf("%.2f%s/s", avg, app.stats.unit)
	} else {
		avgSpeed = "N/A"
	}

	fyne.Do(func() {
		app.ui.cancelBtn.Disable()
		if err != nil {
			if ctx.Err() == context.Canceled {
				summary := fmt.Sprintf("────────────────────────────────────────\nDOWNLOAD ABORTED\n   ├─ Runtime:    %s\n   ├─ Avg Speed:  %s\n   ├─ Downloaded: %s\n────────────────────────────────────────", durationFormatted, avgSpeed, app.stats.lastSize)
				app.appendOutput(summary, color.RGBA{R: 255, G: 165, B: 0, A: 255})
				app.updateStatus("Status: Canceled.")
				app.setStatusIndicator("canceled")
			} else {
				app.updateStatus("Status: Failed. Check logs above.")
				app.setStatusIndicator("failed")
			}
		} else {
			summary := fmt.Sprintf("────────────────────────────────────────\nDOWNLOAD COMPLETE\n   ├─ Duration:   %s\n   ├─ Avg Speed:  %s\n   ├─ Downloaded: %s\n────────────────────────────────────────", durationFormatted, avgSpeed, app.stats.lastSize)
			app.appendOutput(summary, color.RGBA{R: 0, G: 200, B: 0, A: 255})
			app.updateStatus("Status: Success!")
			app.setProgressNow(1)
			app.setStatusIndicator("success")
		}

		app.log.mutex.Lock()
		if app.log.file != nil {
			fmt.Fprintf(app.log.file, "[%s] [SYSTEM] Log file closed.\n", time.Now().Format("15:04:05"))
			app.log.file.Close()
			app.log.file = nil
		}
		app.log.mutex.Unlock()
	})
}

// parseProgress scans a line of yt-dlp output for percentage markers and size
// information, updating the progress bar target and session statistics.
func (app *DownloaderApp) parseProgress(line string) {
	if strings.Contains(line, "%") {
		fields := strings.Fields(line)
		for _, field := range fields {
			if strings.HasSuffix(field, "%") {
				var val float64
				fmt.Sscanf(field, "%f%%", &val)
				app.setProgress(val / 100.0)
				if len(fields) >= 4 {
					app.stats.lastSize = fields[3]
					fmt.Sscanf(app.stats.lastSize, "%f%s", &app.stats.downloadedRaw, &app.stats.unit)
				}
				break
			}
		}
	}
}

// validateTimestamp checks that a trim time entry is either empty (meaning no trim)
// or a valid timestamp in one of the formats yt-dlp accepts:
//   - HH:MM:SS  (e.g. 01:30:00)
//   - MM:SS     (e.g. 01:30)
//   - plain seconds, optionally with decimals (e.g. 90 or 90.5)
func validateTimestamp(s string) error {
	if s == "" {
		return nil
	}
	matched, _ := regexp.MatchString(`^\d+:\d{2}:\d{2}$|^\d+:\d{2}$|^\d+(\.\d+)?$`, s)
	if !matched {
		return fmt.Errorf("use HH:MM:SS, MM:SS or seconds (e.g. 90)")
	}
	return nil
}

// getLocalBinPath returns the absolute path to a tool in the 'bin' folder
// next to the current GoVid executable, falling back to the bare tool name
// for system PATH lookups when the local file is not found.
func (app *DownloaderApp) getLocalBinPath(toolName string) string {
	exePath, err := os.Executable()
	if err != nil {
		return toolName
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(toolName, ".exe") {
		toolName += ".exe"
	}
	return filepath.Join(filepath.Dir(exePath), "bin", toolName)
}
