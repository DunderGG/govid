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
	statusDot  *canvas.Circle      // Animated state indicator dot next to the status label
	trimStart  *widget.Entry       // Optional start time for video trimming (HH:MM:SS)
	trimEnd    *widget.Entry       // Optional end time for video trimming (HH:MM:SS)
	maxSpeed   *widget.Entry       // Download speed limit (e.g. 5M)
	themeMode  *widget.Select      // Theme mode selector (System, Dark, Light)
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
			duplicates: widget.NewCheck("Allow Duplicate Downloads", nil),
			saveLog:    widget.NewCheck("Save output to log file", nil),
			cancelBtn:  widget.NewButton("", nil),
			statusDot:  canvas.NewCircle(color.RGBA{R: 100, G: 100, B: 115, A: 255}),
			progress:   widget.NewProgressBar(),
			status:     widget.NewLabel("Status: Idle"),
			trimStart:  widget.NewEntry(),
			trimEnd:    widget.NewEntry(),
			maxSpeed:   widget.NewEntry(),
			themeMode:  widget.NewSelect([]string{"Dark", "Light"}, nil),
		},
		stats: &DownloadStats{},
		log:   &LogManager{},
	}
	// Ensure the theme selector always has a valid value so savePreferences never
	// writes an empty string, which would suppress the "Dark" fallback on next launch.
	savedTheme := fyne.CurrentApp().Preferences().StringWithFallback("themeMode", "Dark")
	app.ui.themeMode.SetSelected(savedTheme)
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

	myWindow.ShowAndRun()
}
