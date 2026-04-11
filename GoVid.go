// GoVid: A high-performance, cross-platform video downloader.
//
// This application provides a graphical interface for the powerful 'yt-dlp'
// command-line tool. It supports multiple formats (MP4, MKV, MP3, etc.),
// real-time progress tracking, and professional-grade post-processing
// via FFMPEG.
//
// Built with Go and the Fyne toolkit.
// Author: David Bennehag (dunder.gg)
//
// Planned Roadmap & Future Features:
// - [ ] Batch Download: Ability to paste a list of URLs or load from a text file.
// - [ ] Playlist Support: Improved handling of YouTube/Vimeo playlist metadata.
// - [ ] Metadata Embedding: Automatic thumbnail and chapter injection via FFMPEG.
// - [ ] Watermark Support: Option to add custom image/text watermarks to videos during post-processing.
// - [ ] Dark/Light Mode Toggle: Manual override for the system theme.
//
// Current Constraints & Restrictions:
// - External Dependencies: Requires 'yt-dlp' and 'ffmpeg' to be in the system PATH.
// - Platform Specifics: macOS builds may require specific entitlements for sandboxing.
// - Resource Usage: High-resolution recoding (e.g. 4K to MP3) is CPU-intensive.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	// Default window size for the application
	windowWidth  = 750
	windowHeight = 550

	// UI formatting constants
	logBufferLimit = 200 // Max lines kept in the graphical log view
	fpsInterval    = 20  // Progress smoothing interval (in milliseconds)
)

// UIWidgets holds the graphical components of the application.
type UIWidgets struct {
	entry      *widget.Entry       // URL input field
	path       *widget.Entry       // Save directory input field
	output     *container.Scroll   // Scrollable container for logs
	logList    *fyne.Container     // Vertical box containing individual log lines
	progress   *widget.ProgressBar // Visual progress indicator
	status     *widget.Label       // Short status message (e.g. "Downloading...")
	format     *widget.Select      // File format selector (MP4, MP3, etc.)
	quality    *widget.Select      // Maximum resolution selector
	duplicates *widget.Check       // Option to force unique filenames
	saveLog    *widget.Check       // Option to persist output to a .txt file
	cancelBtn  *widget.Button      // Stop button for active downloads
}

// DownloadStats tracks the real-time metrics of a download session.
type DownloadStats struct {
	lastSize      string  // last size reported by yt-dlp e.g., "15.2MiB"
	downloadedRaw float64 // numeric value for calculations
	unit          string  // e.g., "MiB"
	targetPct     float64 // the target percentage to aim for, for smoothing logic
}

// LogManager handles file-based logging operations.
type LogManager struct {
	file  *os.File   // The persistent log file on disk
	mutex sync.Mutex // Prevents data races when writing from multiple goroutines
}

// DownloaderApp acts as a coordinator, holding pointers to the specialized sub-structs and handling application lifecycle.
type DownloaderApp struct {
	window   fyne.Window        // The primary application window
	ui       *UIWidgets         // The graphical interface components
	stats    *DownloadStats     // Statistics tracked during a session
	log      *LogManager        // Logging and persistence manager
	cancelFn context.CancelFunc // Function used to signal yt-dlp to stop
}

// newDownloaderApp constructs the complex DownloaderApp structure.
// We use this to ensure that everything is correctly allocated.
func newDownloaderApp(window fyne.Window) *DownloaderApp {
	return &DownloaderApp{
		window: window,
		ui: &UIWidgets{
			entry:      widget.NewEntry(),
			path:       widget.NewEntry(),
			format:     widget.NewSelect(nil, nil),
			quality:    widget.NewSelect(nil, nil),
			duplicates: widget.NewCheck("Allow Duplicate Downloads", nil),
			saveLog:    widget.NewCheck("Save output to log file", nil),
			cancelBtn:  widget.NewButton("", nil),
			progress:   widget.NewProgressBar(),
			status:     widget.NewLabel("Status: Idle"),
		},
		stats: &DownloadStats{},
		log:   &LogManager{},
	}
}

// main is the entry point of the application. It initializes the Fyne app,
// sets up the DownloaderApp coordinator, starts the UI creation process,
// and shows the main window.
func main() {
	updateFlag := flag.Bool("update", false, "Update yt-dlp to the latest version")
	flag.Parse()

	if *updateFlag {
		updateYtDlp()
		os.Exit(0)
	}

	myApp := app.NewWithID("com.govid.downloader") // Using a fixed app ID allows for consistent settings storage across sessions

	// Set the custom brand icon from the user provided file
	appIcon, err := fyne.LoadResourceFromPath("appicon.png")
	if err == nil {
		myApp.SetIcon(appIcon)
	}

	myWindow := myApp.NewWindow("GoVid")

	myWindow.Resize(fyne.NewSize(windowWidth, windowHeight))

	// Ensure the window icon matches
	if err == nil {
		myWindow.SetIcon(appIcon)
	}

	// Use our new constructor to initialize the app
	downloader := newDownloaderApp(myWindow)
	downloader.createMainMenu()
	downloader.createUI()
	downloader.checkDependencies()

	myWindow.ShowAndRun()
}

// createMainMenu builds the application's top-level menu bar.
func (app *DownloaderApp) createMainMenu() {
	updateMenu := fyne.NewMenuItem("Update yt-dlp", func() {
		dialog.ShowConfirm("Update yt-dlp", "This will run 'yt-dlp -U' to update the tool. Continue?", func(ok bool) {
			if ok {
				app.runUpdateInUI()
			}
		}, app.window)
	})

	mainMenu := fyne.NewMainMenu(
		fyne.NewMenu("Tools", updateMenu),
	)
	app.window.SetMainMenu(mainMenu)
}

// runUpdateInUI executes the update command in a background goroutine so it doesn't freeze the GUI.
func (app *DownloaderApp) runUpdateInUI() {
	app.appendOutput("[SYSTEM] Starting yt-dlp update...", color.RGBA{R: 0, G: 255, B: 255, A: 255})
	app.updateStatus("Status: Updating yt-dlp...")

	go func() {
		cmd := exec.Command("yt-dlp", "-U")
		hideWindow(cmd) // Hide the external console window on Windows
		output, err := cmd.CombinedOutput()

		fyne.Do(func() {
			outStr := string(output)
			app.appendOutput(outStr, theme.ForegroundColor())

			if err != nil {
				app.appendOutput(fmt.Sprintf("[ERROR] Update failed: %v", err), color.RGBA{R: 255, G: 0, B: 0, A: 255})
				app.updateStatus("Status: Update Failed.")
				dialog.ShowError(fmt.Errorf("Update failed: %v", err), app.window)
			} else {
				app.appendOutput("[SYSTEM] yt-dlp update complete.", color.RGBA{R: 0, G: 255, B: 0, A: 255})
				app.updateStatus("Status: Update Success.")
				dialog.ShowInformation("Update Complete", "yt-dlp has been updated successfully.", app.window)
			}
		})
	}()
}

// createUI constructs the graphical user interface by organizing widgets into
// cards and containers. It sets up the layout (header, input tools, status,
// logs, and footer) and attaches event handlers to buttons.
func (app *DownloaderApp) createUI() {
	ui := app.ui

	// 1. Branding - Load the logo to be used in the top row
	var brandLogo fyne.CanvasObject
	logo, err := fyne.LoadResourceFromPath("appicon.png")
	if err == nil {
		image := canvas.NewImageFromResource(logo)
		image.FillMode = canvas.ImageFillContain
		image.SetMinSize(fyne.NewSize(128, 128))
		brandLogo = image
	} else {
		brandLogo = layout.NewSpacer() // Placeholder if image fails
	}

	ui.entry.SetPlaceHolder("https://www.youtube.com/watch?v=...")
	ui.path.SetPlaceHolder("Download folder...")

	// Load previously saved path from preferences. If none is found, default to the current working directory.
	prefs := fyne.CurrentApp().Preferences()
	savedPath := prefs.String("savedPath")

	if savedPath != "" {
		ui.path.SetText(savedPath)
	} else {
		// Use the directory of the current executable as the default save path.
		// This is more intuitive for users when they move the app to a new folder.
		exePath, err := os.Executable()
		if err == nil {
			ui.path.SetText(filepath.Dir(exePath))
		} else {
			cwd, _ := os.Getwd()
			ui.path.SetText(cwd)
		}
	}

	browseBtn := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			ui.path.SetText(filepath.FromSlash(list.Path()))
		}, app.window)
	})

	downloadBtn := widget.NewButtonWithIcon("Download Now!", theme.DownloadIcon(), func() {
		app.startDownload()
	})
	downloadBtn.Importance = widget.HighImportance

	ui.format.Options = []string{
		"MP4",
		"MKV",
		"WebM",
		"MP3 (Audio Only)",
		"M4A (Apple Audio)",
	}

	// Load saved format and quality preferences.
	savedFormat := prefs.String("format")
	savedQuality := prefs.String("quality")

	if savedFormat != "" {
		ui.format.SetSelected(savedFormat)
	} else if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		ui.format.SetSelected("MP4")
	} else {
		ui.format.SetSelected("MKV")
	}

	ui.quality.Options = []string{
		"Best Quality",
		"1080p",
		"720p",
		"480p",
		"360p",
	}

	if savedQuality != "" {
		ui.quality.SetSelected(savedQuality)
	} else {
		ui.quality.SetSelected("Best Quality")
	}

	openFolderBtn := widget.NewButtonWithIcon("Open Folder", theme.FolderIcon(), func() {
		app.openDownloadFolder()
	})

	ui.cancelBtn.Icon = theme.CancelIcon()
	ui.cancelBtn.Text = "Cancel"
	ui.cancelBtn.OnTapped = func() {
		if app.cancelFn != nil {
			app.cancelFn()
			app.appendOutput("Download canceled by user.", color.RGBA{R: 255, G: 165, B: 0, A: 255})
		}
	}
	// Create header with "Media Selection" and logo
	headerText := canvas.NewText("Media Selection", theme.PrimaryColor())
	headerText.TextSize = 28
	headerText.TextStyle = fyne.TextStyle{Bold: true}

	header := container.NewHBox(
		headerText,
		layout.NewSpacer(),
		brandLogo,
	)

	inputCard := widget.NewCard("", "Specify the source and destination",
		container.NewVBox(
			widget.NewLabelWithStyle("Video URL:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			ui.entry,
			widget.NewLabelWithStyle("Save Destination:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewBorder(nil, nil, nil, browseBtn, ui.path),
			container.NewGridWithColumns(2,
				container.NewVBox(
					widget.NewLabelWithStyle("Output Format:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					ui.format,
				),
				container.NewVBox(
					widget.NewLabelWithStyle("Max Quality:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					ui.quality,
				),
			),
			container.NewHBox(ui.duplicates, ui.saveLog),
			container.NewGridWithColumns(3, downloadBtn, openFolderBtn, ui.cancelBtn),
		),
	)

	statusCard := widget.NewCard("Progress", "",
		container.NewVBox(
			ui.progress,
			container.NewHBox(widget.NewIcon(theme.InfoIcon()), ui.status),
		),
	)

	ui.logList = container.NewVBox()
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(0, 10))

	ui.output = container.NewScroll(container.NewVBox(ui.logList, spacer))
	ui.output.SetMinSize(fyne.NewSize(0, 200))

	copyright := canvas.NewText("GoVid • By David Bennehag (dunder.gg) • Built with ❤️, 🤖 and ☕", theme.Color(theme.ColorNameDisabled))
	copyright.TextSize = 14
	copyright.Alignment = fyne.TextAlignCenter
	footer := container.NewCenter(copyright)

	topContent := container.NewVBox(
		header,
		inputCard,
		statusCard,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Terminal Output:", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
	)

	content := container.NewBorder(
		topContent,
		footer, nil, nil,
		ui.output,
	)

	app.window.SetContent(container.NewPadded(content))
}

// startDownload prepares the application for a new download session. It validates
// inputs, resets metrics/visuals, initializes log files if requested, and
// launches the background goroutines for progress interpolation and yt-dlp execution.
func (app *DownloaderApp) startDownload() {
	url := strings.TrimSpace(app.ui.entry.Text)
	savePath := strings.TrimSpace(app.ui.path.Text)

	// Input validation: Ensure URL and save path are provided before starting the download.
	if url == "" {
		dialog.ShowError(fmt.Errorf("URL cannot be empty"), app.window)
		return
	}
	if savePath == "" {
		dialog.ShowError(fmt.Errorf("save path cannot be empty"), app.window)
		return
	}

	// Persist user preferences such as save destination and checkbox state.
	// Extracted into a separate function to allow for more scalable preference management.
	app.savePreferences(savePath)

	// Reset UI and stats for new session
	app.updateStatus("Status: Initializing...")
	app.setProgressNow(0)
	app.stats.targetPct = 0
	app.ui.logList.Objects = nil
	app.ui.logList.Refresh()
	app.ui.cancelBtn.Enable()

	// Initialize logging to file if the option is checked. This allows users to keep a record of their download sessions.
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

	// Create a cancellable context for the download process. This allows us to signal yt-dlp to stop if the user hits the Cancel button.
	ctx, cancel := context.WithCancel(context.Background())
	app.cancelFn = cancel

	// Launch smoothing goroutine: Interpolates progress for a smoother visual experience.
	// time.NewTicker is a powerful Go tool for periodic tasks (like a heartbeat).
	go func() {
		// The ticker will trigger every 'fpsInterval' milliseconds, allowing us to update the progress bar smoothly towards the target percentage set by the yt-dlp output.
		ticker := time.NewTicker(time.Duration(fpsInterval) * time.Millisecond)
		defer ticker.Stop() // 'defer' ensures cleanup happens even if the function exits early

		for {
			select {
			case <-ctx.Done(): // This "blocks" until the user hits Cancel OR the download finishes
				return
			case <-ticker.C: // Occurs every interval (e.g., 20ms)
				current := app.ui.progress.Value
				target := app.stats.targetPct
				if current < target {
					// Move part of the distance toward the target each tick for an "easing" effect.
					step := (target - current) * 0.05
					if step < 0.001 {
						step = 0.001
					}

					// Calculate the new progress value, ensuring it does not exceed the target.
					newVal := current + step
					if newVal > target {
						newVal = target
					}

					// Update the progress bar in the UI thread using fyne.Do to ensure thread safety.
					fyne.Do(func() {
						app.ui.progress.SetValue(newVal)
					})
				}
			}
		}
	}()

	// Run yt-dlp in a separate goroutine to avoid blocking the UI. This allows the application to remain responsive while the download is in progress.
	go app.runYtDlp(ctx, url, savePath)
}

// runYtDlp manages the external lifecycle of the yt-dlp process. It builds
// the command arguments based on UI selections (quality, format), executes
// the tool, and pipes its output/errors back to the UI in real-time.
// Parameters:
//   - ctx: context handle for process cancellation.
//   - url: the target media link.
//   - savePath: the local directory to save the file.
func (app *DownloaderApp) runYtDlp(ctx context.Context, url string, savePath string) {
	startTime := time.Now()
	formatFlag := "bestvideo+bestaudio/best"
	extension := "mp4"

	// Determine the format and quality flags based on user selections. This logic translates the UI options into yt-dlp command-line arguments.
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

	// Build the yt-dlp command arguments.
	// The output template uses the video title and optionally includes a timestamp if duplicates are allowed.
	// The format and quality flags are set based on user selections.
	outputTemplate := "GoVid_%(title)s" + qualitySuffix + "." + extension
	if app.ui.duplicates.Checked {
		outputTemplate = "GoVid_%(title)s" + qualitySuffix + "_%(epoch)s." + extension
	}

	// The args slice is constructed to include all necessary command-line options for yt-dlp.
	args := []string{
		"--newline", "--progress", "--verbose", "--no-part", "--no-continue",
		"-f", formatFlag, "-P", savePath, "-o", outputTemplate,
	}

	// Format-specific handling using FFMPEG post-processing if available
	if extension == "mp3" || extension == "m4a" {
		// Use audio extraction flags for audio-only formats
		args = append(args, "--extract-audio", "--audio-format", extension)
	} else if extension != "" {
		// For video, ensure the output container matches the user selection precisely
		args = append(args, "--recode-video", extension)
	}
	args = append(args, url)

	// exec.CommandContext is used to run yt-dlp with the ability to cancel it via the context.
	// This allows for graceful termination when the user hits the Cancel button.
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	hideWindow(cmd) // Hide the external terminal window on Windows platforms
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		app.updateStatus(fmt.Sprintf("Failed to launch yt-dlp: %v", err))
		return
	}

	// Goroutine to read standard output from yt-dlp.
	// It scans each line of output, updates the progress based on percentage markers,
	// and appends the output to the UI "terminal" with appropriate coloring.
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			app.parseProgress(line)
			app.appendOutput(line, theme.ForegroundColor())
		}
	}()

	// Goroutine to read standard error from yt-dlp.
	// It scans for error messages and logs them in red, while warnings are logged in orange.
	// This helps users quickly identify issues during the download process.
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			logColor := color.RGBA{R: 255, G: 0, B: 0, A: 255}
			if strings.Contains(line, "[debug]") {
				logColor = color.RGBA{R: 255, G: 255, B: 0, A: 255}
			} else if strings.Contains(line, "WARNING:") {
				logColor = color.RGBA{R: 255, G: 165, B: 0, A: 255}
			}
			app.appendOutput(line, logColor)
		}
	}()

	// Wait for the yt-dlp process to finish and calculate final statistics.
	// This will allow us to provide a summary of the download session, including duration and average speed.
	err := cmd.Wait()
	durationTotal := time.Since(startTime).Seconds()
	durationFormatted := fmt.Sprintf("%.2fs", durationTotal)

	// Calculate average speed if possible.
	// The result is formatted for display in the summary.
	var avgSpeed string
	if durationTotal > 0 && app.stats.downloadedRaw > 0 {
		avg := app.stats.downloadedRaw / durationTotal
		avgSpeed = fmt.Sprintf("%.2f%s/s", avg, app.stats.unit)
	} else {
		avgSpeed = "N/A"
	}

	// Final UI updates based on the success or failure of the download. This includes a summary of the session and cleanup of log files.
	fyne.Do(func() {
		app.ui.cancelBtn.Disable()
		if err != nil {
			if ctx.Err() == context.Canceled {
				summary := fmt.Sprintf("────────────────────────────────────────\nDOWNLOAD ABORTED\n   ├─ Runtime:    %s\n   ├─ Avg Speed:  %s\n   ├─ Downloaded: %s\n────────────────────────────────────────", durationFormatted, avgSpeed, app.stats.lastSize)
				app.appendOutput(summary, color.RGBA{R: 255, G: 165, B: 0, A: 255})
				app.updateStatus("Status: Canceled.")
			} else {
				app.updateStatus("Status: Failed. Check logs above.")
			}
		} else {
			summary := fmt.Sprintf("────────────────────────────────────────\nDOWNLOAD COMPLETE\n   ├─ Duration:   %s\n   ├─ Avg Speed:  %s\n   ├─ Downloaded: %s\n────────────────────────────────────────", durationFormatted, avgSpeed, app.stats.lastSize)
			app.appendOutput(summary, color.RGBA{R: 0, G: 200, B: 0, A: 255})
			app.updateStatus("Status: Success!")
			app.setProgressNow(1)
		}

		// Lock mutex, close the file and clean up log file pointer if it was created for this session.
		// This ensures that we don't leave open file handles after the download is complete.
		app.log.mutex.Lock()
		if app.log.file != nil {
			fmt.Fprintf(app.log.file, "[%s] [SYSTEM] Log file closed.\n", time.Now().Format("15:04:05"))
			app.log.file.Close()
			app.log.file = nil
		}
		app.log.mutex.Unlock()
	})
}

// parseProgress scans a line of output from the downloader for percentage
// markers and size information. It extracts numeric values to update the
// progress bar's target and session statistics.
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

// updateStatus performs a thread-safe update to the UI status label using
// the provided message string.
func (app *DownloaderApp) updateStatus(msg string) {
	fyne.Do(func() {
		app.ui.status.SetText(msg)
	})
}

// appendOutput handles thread-safe logging of information. It writes to the active
// log file on disk and updates the scrolling list in the UI. It also maintains
// a 200-line buffer limit for the graphical list.
// Parameters:
//   - line: content to log.
//   - color: desired text color in the UI.
func (app *DownloaderApp) appendOutput(inString string, color color.Color) {
	app.log.mutex.Lock()
	if app.log.file != nil {
		fmt.Fprintf(app.log.file, "[%s] %s\n", time.Now().Format("15:04:05"), inString)
	}
	app.log.mutex.Unlock()

	fyne.Do(func() {
		lines := strings.Split(inString, "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) == "" && len(line) == 0 {
				continue
			}
			if len(app.ui.logList.Objects) > logBufferLimit {
				app.ui.logList.Objects = app.ui.logList.Objects[1:]
			}
			text := canvas.NewText(line, color)
			text.TextSize = 10
			text.TextStyle = fyne.TextStyle{Monospace: true}
			app.ui.logList.Add(text)
		}
		app.ui.output.ScrollToBottom()
	})
}

// setProgress updates the target percentage for the progress smoothing goroutine.
func (app *DownloaderApp) setProgress(val float64) {
	app.stats.targetPct = val
}

// setProgressNow forces an immediate visual update to the progress bar in the UI.
func (app *DownloaderApp) setProgressNow(val float64) {
	fyne.Do(func() {
		app.ui.progress.SetValue(val)
	})
}

// savePreferences handles the persistence of user settings across application restarts.
func (app *DownloaderApp) savePreferences(savePath string) {
	prefs := fyne.CurrentApp().Preferences()
	prefs.SetString("savedPath", savePath)
	prefs.SetString("format", app.ui.format.Selected)
	prefs.SetString("quality", app.ui.quality.Selected)
}

// openDownloadFolder utilizes OS-specific shell commands (explorer, open, xdg-open)
// to open the user's selected download directory in their native file manager.
func (app *DownloaderApp) openDownloadFolder() {
	path := strings.TrimSpace(app.ui.path.Text)
	if path == "" {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		app.appendOutput(fmt.Sprintf("[ERR] Failed to open folder: %v", err), color.RGBA{R: 255, G: 0, B: 0, A: 255})
	}
}

// checkDependencies verifies that external requirements (yt-dlp, ffmpeg) are
// present in the system PATH. It logs warnings to the terminal output if any
// dependencies are missing.
func (app *DownloaderApp) checkDependencies() {
	deps := []string{"yt-dlp", "ffmpeg"}
	var missing []string
	for _, d := range deps {
		_, err := exec.LookPath(d)
		if err != nil {
			missing = append(missing, d)
		}
	}
	// If any dependencies are missing, log a warning and show an information dialog to the user.
	// This helps ensure that users are aware of the requirements for the application to function properly.
	if len(missing) > 0 {
		msg := fmt.Sprintf("Missing dependencies: %s\nPlease install them to use all features.", strings.Join(missing, ", "))
		app.appendOutput("[WARN] "+msg, color.RGBA{R: 255, G: 165, B: 0, A: 255})
		dialog.ShowInformation("Dependencies Missing", msg, app.window)
	}
}

// updateYtDlp runs the 'yt-dlp -U' command to ensure the tool is up to date.
// It redirects output to the terminal so users can see the progress of the update.
func updateYtDlp() {
	fmt.Println("Updating yt-dlp...")
	cmd := exec.Command("yt-dlp", "-U")
	hideWindow(cmd) // Don't show external console window if called from main.exe
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Error updating yt-dlp: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("yt-dlp update complete.")
}
