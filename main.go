// GoVid: A high-performance, cross-platform video downloader.
//
// This application provides a graphical interface for the powerful 'yt-dlp'
// command-line tool. It supports multiple formats (MP4, MKV, MP3, etc.),
// real-time progress tracking, and post-processing via FFMPEG.
//
// Built with Go and the Fyne toolkit.
// Author: David Bennehag (dunder.gg)
package main

import (
	"flag"
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

const (
	// Default window size for the application.
	windowWidth  = 750
	windowHeight = 550

	// UI formatting constants.
	fpsInterval = 20 // Progress smoothing interval (in milliseconds).
)

var (
	// version is injected at release build time via:
	//	-X main.version=1.0.0
	// Falls back to "dev" for local builds.
	version = "dev"
)

// newDownloaderApp constructs and fully initialises a DownloaderApp.
func newDownloaderApp(window fyne.Window) *DownloaderApp {
	dlApp := &DownloaderApp{
		window:    window,
		uiManager: NewUIManager(window),
		ui: &UIWidgets{
			entry:             widget.NewEntry(),
			path:              widget.NewEntry(),
			format:            widget.NewSelect(nil, nil),
			quality:           widget.NewSelect(nil, nil),
			saveLog:           widget.NewCheck("Save output to log file", nil),
			notify:            widget.NewCheck("Notify on Completion", nil),
			autoRetry:         widget.NewCheck("Auto-retry", nil),
			enablePostProcess: widget.NewCheck("Post-Processing", nil),
			downloadBtn:       widget.NewButtonWithIcon("Download Now!", nil, nil),
			cancelBtn:         widget.NewButton("", nil),
			statusDot:         canvas.NewCircle(colDotIdle),
			progress:          widget.NewProgressBar(),
			status:            widget.NewLabel("Status: Idle"),
			trimStart:         widget.NewEntry(),
			trimEnd:           widget.NewEntry(),
			maxSpeed:          widget.NewEntry(),
			themeMode:         widget.NewRadioGroup([]string{"Dark", "Light"}, nil),
			cookies:           widget.NewEntry(),
			savePrefs:         widget.NewCheck("Save preferences between sessions", nil),
			logLimit:          widget.NewSelect([]string{"100", "200", "500", "1000", "5000", "Unlimited"}, nil),
			batchMode:         widget.NewCheck("Batch Mode", nil),
			smoothMotion:      widget.NewCheck("Enabled", nil),
			smoothMotionMode:  widget.NewRadioGroup([]string{"Precise (slow)", "Balanced", "Fast"}, nil),
			smoothMotionFPS:   widget.NewSlider(24, 120),
			sharpen:           widget.NewCheck("Sharpen Video", nil),
			sharpenAmount:     widget.NewSlider(0, 2),
			normalizeAudio:    widget.NewCheck("Normalize Audio", nil),
			vividMode:         widget.NewCheck("Vivid Mode", nil),
			denoise:           widget.NewCheck("Denoise", nil),
			denoiseMode:       widget.NewRadioGroup([]string{"NLMeans (HQ, slow)", "hqdn3d (Balanced)"}, nil),
			hdrToSdr:          widget.NewCheck("HDR to SDR", nil),
			deband:            widget.NewCheck("Fix Banding", nil),
			autoCrop:          widget.NewCheck("Auto-Crop", nil),
			stabilize:         widget.NewCheck("Stabilize", nil),
			deinterlace:       widget.NewCheck("Deinterlace", nil),
			nightMode:         widget.NewCheck("Night Mode", nil),
			upscaleVideo:      widget.NewCheck("Upscale Video", nil),
			upscaleTarget:     widget.NewSelect([]string{"2× (Double)", "1080p", "1440p", "4K (2160p)"}, nil),
		},
		stats:  &DownloadStats{},
		logSvc: NewLogService(),
		depSvc: NewDependencyService(),
	}

	// Load saved preferences and apply them to all widgets.
	dlApp.prefSvc = NewPreferenceService(fyne.CurrentApp().Preferences())
	prefs := dlApp.prefSvc.Load()
	dlApp.applyPreferencesToWidgets(prefs)
	dlApp.logSvc.SetBufferLimit(ParseBufferLimit(prefs.LogLimit))

	// Wire history service to both the app and the UIManager.
	dlApp.historySvc = NewHistoryService()
	dlApp.uiManager.historySvc = dlApp.historySvc
	return dlApp
}

// main is the entry point of the application. It initializes the Fyne app,
// sets up the DownloaderApp coordinator, starts the UI creation process,
// and shows the main window.
func main() {
	updateFlag := flag.Bool("update", false, "Update yt-dlp to the latest version")
	flag.Parse()

	if *updateFlag {
		if err := UpdateYtDlpCLI(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(int(exitCodeFromError(err)))
		}
		return
	}

	mainApp := app.NewWithID("com.govid.downloader")

	// Apply the user's preferred theme or the custom GoVid theme.
	themePref := mainApp.Preferences().StringWithFallback(prefThemeMode, defaultThemeMode)
	switch themePref {
	case "Light":
		mainApp.Settings().SetTheme(&lightTheme{})
	default:
		// Force the custom theme to use Dark variant for its base
		mainApp.Settings().SetTheme(&darkTheme{})
	}

	// Set the custom brand icon using the bundled resource.
	mainApp.SetIcon(resourceAppiconPng)

	mainWindow := mainApp.NewWindow("GoVid")
	mainWindow.Resize(fyne.NewSize(windowWidth, windowHeight))
	mainWindow.SetIcon(resourceAppiconPng)

	dlApp := newDownloaderApp(mainWindow)
	dlApp.createMainMenu()
	dlApp.createUI()
	dlApp.checkDependencies()

	// Show a confirmation dialog if a download or post-processing job is active.
	mainWindow.SetCloseIntercept(func() {
		if dlApp.isRunning.Load() {
			dialog.ShowConfirm(
				"Job in Progress",
				"A download or post-processing job is currently running.\nAre you sure you want to quit?",
				func(confirmed bool) {
					if confirmed {
						if dlApp.cancelFn != nil {
							dlApp.cancelFn()
						}
						mainApp.Quit()
					}
				},
				mainWindow,
			)
			return
		}
		mainApp.Quit()
	})

	mainWindow.ShowAndRun()
}
