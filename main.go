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
	"context"
	"flag"
	"image/color"
	"os"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const (
	// Default window size for the application.
	windowWidth  = 750
	windowHeight = 550

	// UI formatting constants.
	logBufferLimit = 200 // Max lines kept in the graphical log view.
	fpsInterval    = 20  // Progress smoothing interval (in milliseconds).
)

// version is injected at release build time via:
//	-X main.version=1.0.0
// Falls back to "dev" for local builds.
var version = "dev"

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
	saveLog    *widget.Check       // Option to persist output to a .txt file
	notify     *widget.Check       // Option to send a system notification on completion
	downloadBtn *widget.Button     // Start button for downloads
	cancelBtn  *widget.Button      // Stop button for active downloads
	statusDot  *canvas.Circle      // Animated state indicator dot next to the status label
	trimStart  *widget.Entry       // Optional start time for video trimming (HH:MM:SS)
	trimEnd    *widget.Entry       // Optional end time for video trimming (HH:MM:SS)
	maxSpeed   *widget.Entry       // Download speed limit (e.g. 5M)
	themeMode  *widget.RadioGroup   // Theme mode selector (Dark / Light)
	cookies    *widget.Entry        // Path to a Mozilla/Netscape-format cookies file
	savePrefs  *widget.Check        // Option to persist preferences between sessions
	batchMode  *widget.Check        // Option to switch URL input to multi-line batch mode
	smoothMotion     *widget.Check      // Post-processing: Smooth to custom fps
	smoothMotionMode *widget.RadioGroup // Quality mode for Smooth Motion
	smoothMotionFPS  *widget.Slider     // Target framerate for motion smoothing
	sharpen          *widget.Check      // Post-processing: Apply unsharp mask
	normalizeAudio   *widget.Check      // Post-processing: Normalize audio loudness
	vividMode        *widget.Check      // Post-processing: Color/saturation enhancement
	denoise          *widget.Check      // Post-processing: Noise reduction
	denoiseMode      *widget.RadioGroup // Denoise method (NLMeans = HQ, ATADenoise = Fast)
	hdrToSdr         *widget.Check      // Post-processing: HDR to SDR tone mapping
	deband           *widget.Check      // Post-processing: Fix gradient banding
	autoCrop         *widget.Check      // Post-processing: Auto-crop black bars
	stabilize        *widget.Check      // Post-processing: Video stabilization (deshake)
	deinterlace      *widget.Check      // Post-processing: Deinterlace (bwdif)
	nightMode        *widget.Check      // Post-processing: Dynamic audio compression
	upscaleVideo     *widget.Check      // Post-processing: Resolution upscaling
	upscaleTarget    *widget.Select     // Target resolution for upscaling
}

// DownloadStats tracks the real-time metrics of a download session.
type DownloadStats struct {
	lastSize      string  // Last size reported by yt-dlp e.g., "15.2MiB"
	downloadedRaw float64 // Numeric value for calculations
	unit          string  // e.g., "MiB"
	targetPct     float64 // The target percentage to aim for, for smoothing logic
}

// LogManager handles file-based logging operations.
type LogManager struct {
	file  *os.File   // The persistent log file on disk
	mutex sync.Mutex // Prevents data races when writing from multiple goroutines
}

// DownloaderApp acts as a coordinator, holding pointers to the specialized
// sub-structs and handling application lifecycle.
type DownloaderApp struct {
	window    fyne.Window        // The primary application window
	ui        *UIWidgets         // The graphical interface components
	stats     *DownloadStats     // Statistics tracked during a session
	log       *LogManager        // Logging and persistence manager
	cancelFn  context.CancelFunc // Function used to signal yt-dlp to stop
	stopPulse chan struct{}       // Closed to stop the status dot pulse goroutine
}

// newDownloaderApp constructs and fully initialises a DownloaderApp.
func newDownloaderApp(window fyne.Window) *DownloaderApp {
	app := &DownloaderApp{
		window: window,
		ui: &UIWidgets{
			entry:      widget.NewEntry(),
			path:       widget.NewEntry(),
			format:     widget.NewSelect(nil, nil),
			quality:    widget.NewSelect(nil, nil),
			saveLog:    widget.NewCheck("Save output to log file", nil),
			notify:     widget.NewCheck("Notify on Completion", nil),
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
			batchMode:  widget.NewCheck("Batch Mode", nil),
			smoothMotion:     widget.NewCheck("Enabled", nil),
			smoothMotionMode: widget.NewRadioGroup([]string{"Precise (slow)", "Balanced", "Fast"}, nil),
			smoothMotionFPS:  widget.NewSlider(24, 120),
			sharpen:          widget.NewCheck("Sharpen Video", nil),
			normalizeAudio:   widget.NewCheck("Normalize Audio", nil),
			vividMode:        widget.NewCheck("Vivid Mode", nil),
			denoise:          widget.NewCheck("Denoise", nil),
			denoiseMode:      widget.NewRadioGroup([]string{"NLMeans (HQ, slow)", "ATADenoise (Fast)"}, nil),
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
	app.ui.normalizeAudio.SetChecked(prefs.Bool("normalize"))
	app.ui.vividMode.SetChecked(prefs.Bool("vividMode"))
	app.ui.denoise.SetChecked(prefs.Bool("denoise"))
	app.ui.denoiseMode.SetSelected(prefs.StringWithFallback("denoiseMode", "ATADenoise (Fast)"))
	app.ui.hdrToSdr.SetChecked(prefs.Bool("hdrToSdr"))
	app.ui.deband.SetChecked(prefs.Bool("deband"))
	app.ui.autoCrop.SetChecked(prefs.Bool("autoCrop"))
	app.ui.stabilize.SetChecked(prefs.Bool("stabilize"))
	app.ui.deinterlace.SetChecked(prefs.Bool("deinterlace"))
	app.ui.nightMode.SetChecked(prefs.Bool("nightMode"))
	app.ui.upscaleVideo.SetChecked(prefs.Bool("upscaleVideo"))
	app.ui.upscaleTarget.SetSelected(prefs.StringWithFallback("upscaleTarget", "2× (Double)"))
	app.ui.batchMode.SetChecked(prefs.Bool("batchMode"))
	app.ui.saveLog.SetChecked(prefs.Bool("saveLog"))
	app.ui.notify.SetChecked(prefs.Bool("notify"))
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

	// Ensure the whole process exits when the main window is closed,
	// even if secondary windows (Preferences, About, etc.) are still open.
	myWindow.SetCloseIntercept(func() {
		myApp.Quit()
	})

	myWindow.ShowAndRun()
}
