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
	"image/color"
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
	// logBufferLimit is the maximum number of lines kept in the graphical log view.
	// Older entries are trimmed from the top. Configurable via Preferences.
	logBufferLimit = 200
	
	// version is injected at release build time via:
	//	-X main.version=1.0.0
	// Falls back to "dev" for local builds.
	version = "dev"
)

// newDownloaderApp constructs and fully initialises a DownloaderApp.
func newDownloaderApp(window fyne.Window) *DownloaderApp {
	app := &DownloaderApp{
		window:    window,
		uiManager: NewUIManager(window),
		ui: &UIWidgets{
			entry:      widget.NewEntry(),
			path:       widget.NewEntry(),
			format:     widget.NewSelect(nil, nil),
			quality:    widget.NewSelect(nil, nil),
			saveLog:    widget.NewCheck("Save output to log file", nil),
			notify:     widget.NewCheck("Notify on Completion", nil),
			autoRetry:  widget.NewCheck("Auto-retry", nil),
			enablePostProcess: widget.NewCheck("Post-Processing", nil),
			downloadBtn: widget.NewButtonWithIcon("Download Now!", nil, nil),
			cancelBtn:  widget.NewButton("", nil),
			statusDot:  canvas.NewCircle(color.RGBA{R: 100, G: 100, B: 115, A: 255}),
			progress:   widget.NewProgressBar(),
			status:     widget.NewLabel("Status: Idle"),
			trimStart:  widget.NewEntry(),
			trimEnd:    widget.NewEntry(),
			maxSpeed:   widget.NewEntry(),
			themeMode:  widget.NewRadioGroup([]string{"Dark", "Light"}, nil),
			cookies:    widget.NewEntry(),
			savePrefs:  widget.NewCheck("Save preferences between sessions", nil),
			logLimit:   widget.NewSelect([]string{"100", "200", "500", "1000", "5000", "Unlimited"}, nil),
			batchMode:  widget.NewCheck("Batch Mode", nil),
			smoothMotion:     widget.NewCheck("Enabled", nil),
			smoothMotionMode: widget.NewRadioGroup([]string{"Precise (slow)", "Balanced", "Fast"}, nil),
			smoothMotionFPS:  widget.NewSlider(24, 120),
			sharpen:          widget.NewCheck("Sharpen Video", nil),
			sharpenAmount:    widget.NewSlider(0, 2),
			normalizeAudio:   widget.NewCheck("Normalize Audio", nil),
			vividMode:        widget.NewCheck("Vivid Mode", nil),
			denoise:          widget.NewCheck("Denoise", nil),
			denoiseMode:      widget.NewRadioGroup([]string{"NLMeans (HQ, slow)", "hqdn3d (Balanced)"}, nil),
			hdrToSdr:         widget.NewCheck("HDR to SDR", nil),
			deband:           widget.NewCheck("Fix Banding", nil),
			autoCrop:         widget.NewCheck("Auto-Crop", nil),
			stabilize:        widget.NewCheck("Stabilize", nil),
			deinterlace:      widget.NewCheck("Deinterlace", nil),
			nightMode:        widget.NewCheck("Night Mode", nil),
			upscaleVideo:     widget.NewCheck("Upscale Video", nil),
			upscaleTarget:    widget.NewSelect([]string{"2× (Double)", "1080p", "1440p", "4K (2160p)"}, nil),
		},
		stats: &DownloadStats{},
		log:   &LogManager{},
	}

	// Load saved preferences from the Fyne preferences system.
	prefs := fyne.CurrentApp().Preferences()
	// Ensure the theme selector always has a valid value so savePreferences never
	// writes an empty string, which would suppress the "Dark" fallback on next launch.
	savedTheme := prefs.StringWithFallback("themeMode", "Dark")
	app.ui.themeMode.SetSelected(savedTheme)
	// Set the savePrefs toggle first since it gates whether the other preferences are loaded.
	app.ui.savePrefs.SetChecked(prefs.BoolWithFallback("savePrefs", true))
	// Load post-processing state so buildPostProcessFilters reads correct values
	// even if the Preferences window has never been opened this session.
	app.ui.smoothMotion.SetChecked(prefs.Bool("upscale"))
	app.ui.smoothMotionMode.SetSelected(prefs.StringWithFallback("smoothMotionMode", "Balanced"))
	
	fps := prefs.FloatWithFallback("smoothFPS", 60)
	app.ui.smoothMotionFPS.SetValue(fps)
	
	app.ui.sharpen.SetChecked(prefs.Bool("sharpen"))
	app.ui.sharpenAmount.SetValue(prefs.FloatWithFallback("sharpenAmount", 1.0))
	app.ui.normalizeAudio.SetChecked(prefs.Bool("normalize"))
	app.ui.vividMode.SetChecked(prefs.Bool("vividMode"))
	app.ui.denoise.SetChecked(prefs.Bool("denoise"))
	app.ui.denoiseMode.SetSelected(prefs.StringWithFallback("denoiseMode", "hqdn3d (Balanced)"))
	app.ui.hdrToSdr.SetChecked(prefs.Bool("hdrToSdr"))
	app.ui.deband.SetChecked(prefs.Bool("deband"))
	app.ui.autoCrop.SetChecked(prefs.Bool("autoCrop"))
	app.ui.stabilize.SetChecked(prefs.Bool("stabilize"))
	app.ui.deinterlace.SetChecked(prefs.Bool("deinterlace"))
	app.ui.nightMode.SetChecked(prefs.Bool("nightMode"))
	app.ui.upscaleVideo.SetChecked(prefs.Bool("upscaleVideo"))
	app.ui.upscaleTarget.SetSelected(prefs.StringWithFallback("upscaleTarget", "2× (Double)"))
	app.ui.cookies.SetText(prefs.String("cookiesPath"))
	app.ui.batchMode.SetChecked(prefs.Bool("batchMode"))
	app.ui.saveLog.SetChecked(prefs.Bool("saveLog"))
	app.ui.notify.SetChecked(prefs.Bool("notify"))
	app.ui.autoRetry.SetChecked(prefs.Bool("autoRetry"))
	app.ui.enablePostProcess.SetChecked(prefs.BoolWithFallback("enablePostProcess", true))
	app.ui.logLimit.SetSelected(prefs.StringWithFallback("logLimit", "200"))
	logBufferLimit = parseLogLimit(prefs.StringWithFallback("logLimit", "200"))
	return app
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

	myApp := app.NewWithID("com.govid.downloader")

	// Apply the user's preferred theme or the custom GoVid theme.
	themePref := myApp.Preferences().StringWithFallback("themeMode", "Dark")
	switch themePref {
	case "Light":
		myApp.Settings().SetTheme(&lightTheme{})
	default:
		// Force the custom theme to use Dark variant for its base
		myApp.Settings().SetTheme(&darkTheme{})
	}

	// Set the custom brand icon using the bundled resource.
	myApp.SetIcon(resourceAppiconPng)

	myWindow := myApp.NewWindow("GoVid")
	myWindow.Resize(fyne.NewSize(windowWidth, windowHeight))
	myWindow.SetIcon(resourceAppiconPng)

	downloader := newDownloaderApp(myWindow)
	downloader.createMainMenu()
	downloader.createUI()
	downloader.checkDependencies()

	// Show a confirmation dialog if a download or post-processing job is active.
	myWindow.SetCloseIntercept(func() {
		if downloader.isRunning.Load() {
			dialog.ShowConfirm(
				"Job in Progress",
				"A download or post-processing job is currently running.\nAre you sure you want to quit?",
				func(confirmed bool) {
					if confirmed {
						if downloader.cancelFn != nil {
							downloader.cancelFn()
						}
						myApp.Quit()
					}
				},
				myWindow,
			)
			return
		}
		myApp.Quit()
	})

	myWindow.ShowAndRun()
}
