package main

import (
	"context"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ExitCode represents process exit statuses for CLI execution paths.
type ExitCode int

const (
	// ExitOK indicates successful completion.
	ExitOK ExitCode = 0
	// ExitUnexpected indicates a non-specific failure.
	ExitUnexpected ExitCode = 1
	// ExitUpdateFailed indicates yt-dlp update failed without a specific subprocess code.
	ExitUpdateFailed ExitCode = 10
)

// UIWidgets holds the graphical components of the application, grouped into
// feature-specific sub-structs. NewUIWidgets constructs all of them.
type UIWidgets struct {
	download    *DownloadControls    // Main window: URL/path input, format/quality, progress, and log view
	prefs       *PreferenceControls  // Preferences dialog: app-wide settings
	postProcess *PostProcessControls // Post-Processing dialog: FFmpeg filter toggles
}

// DownloadControls holds the widgets that drive a single download or batch
// run and display its progress and log output.
type DownloadControls struct {
	entry       *widget.Entry       // URL input field
	path        *widget.Entry       // Save directory input field
	output      *container.Scroll   // Scrollable container for logs
	logList     *fyne.Container     // Vertical box containing individual log lines
	progress    *widget.ProgressBar // Visual progress indicator
	status      *widget.Label       // Short status message (e.g. "Downloading...")
	format      *widget.Select      // File format selector (MP4, MP3, etc.)
	quality     *widget.Select      // Maximum resolution selector
	saveLog     *widget.Check       // Option to persist output to a .txt file
	notify      *widget.Check       // Option to send a system notification on completion
	autoRetry   *widget.Check       // Option to automatically retry on transient errors
	downloadBtn *widget.Button      // Start button for downloads
	cancelBtn   *widget.Button      // Stop button for active downloads
	statusDot   *canvas.Circle      // Animated state indicator dot next to the status label
	trimStart   *widget.Entry       // Optional start time for video trimming (HH:MM:SS)
	trimEnd     *widget.Entry       // Optional end time for video trimming (HH:MM:SS)
	batchMode   *widget.Check       // Option to switch URL input to multi-line batch mode
}

// NewDownloadControls constructs the main window's download-related widgets.
func NewDownloadControls() *DownloadControls {
	return &DownloadControls{
		entry:       widget.NewEntry(),
		path:        widget.NewEntry(),
		format:      widget.NewSelect(nil, nil),
		quality:     widget.NewSelect(nil, nil),
		saveLog:     widget.NewCheck("Save output to log file", nil),
		notify:      widget.NewCheck("Notify on Completion", nil),
		autoRetry:   widget.NewCheck("Auto-retry", nil),
		downloadBtn: widget.NewButtonWithIcon("Download Now!", nil, nil),
		cancelBtn:   widget.NewButton("", nil),
		statusDot:   canvas.NewCircle(colDotIdle),
		progress:    widget.NewProgressBar(),
		status:      widget.NewLabel("Status: Idle"),
		trimStart:   widget.NewEntry(),
		trimEnd:     widget.NewEntry(),
		batchMode:   widget.NewCheck("Batch Mode", nil),
	}
}

// PreferenceControls holds the widgets shown in the Preferences dialog.
type PreferenceControls struct {
	maxSpeed  *widget.Entry      // Download speed limit (e.g. 5M)
	themeMode *widget.RadioGroup // Theme mode selector (Dark / Light)
	cookies   *widget.Entry      // Path to a Mozilla/Netscape-format cookies file
	savePrefs *widget.Check      // Option to persist preferences between sessions
	logLimit  *widget.Select     // Max lines kept in the graphical log view
}

// NewPreferenceControls constructs the Preferences dialog's widgets.
func NewPreferenceControls() *PreferenceControls {
	return &PreferenceControls{
		maxSpeed:  widget.NewEntry(),
		themeMode: widget.NewRadioGroup([]string{"Dark", "Light"}, nil),
		cookies:   widget.NewEntry(),
		savePrefs: widget.NewCheck("Save preferences between sessions", nil),
		logLimit:  widget.NewSelect([]string{"100", "200", "500", "1000", "5000", "Unlimited"}, nil),
	}
}

// PostProcessControls holds the widgets shown in the Post-Processing dialog,
// plus the master enable toggle rendered on the main window.
type PostProcessControls struct {
	enablePostProcess *widget.Check      // Master toggle to enable/disable all post-processing
	smoothMotion      *widget.Check      // Post-processing: Smooth to custom fps
	smoothMotionMode  *widget.RadioGroup // Quality mode for Smooth Motion
	smoothMotionFPS   *widget.Slider     // Target framerate for motion smoothing
	sharpen           *widget.Check      // Post-processing: Apply unsharp mask
	sharpenAmount     *widget.Slider     // Post-processing: Sharpening intensity
	normalizeAudio    *widget.Check      // Post-processing: Normalize audio loudness
	vividMode         *widget.Check      // Post-processing: Color/saturation enhancement
	denoise           *widget.Check      // Post-processing: Noise reduction
	denoiseMode       *widget.RadioGroup // Denoise method (NLMeans = HQ, ATADenoise = Fast)
	hdrToSdr          *widget.Check      // Post-processing: HDR to SDR tone mapping
	deband            *widget.Check      // Post-processing: Fix gradient banding
	autoCrop          *widget.Check      // Post-processing: Auto-crop black bars
	stabilize         *widget.Check      // Post-processing: Video stabilization (deshake)
	deinterlace       *widget.Check      // Post-processing: Deinterlace (bwdif)
	nightMode         *widget.Check      // Post-processing: Dynamic audio compression
	upscaleVideo      *widget.Check      // Post-processing: Resolution upscaling
	upscaleTarget     *widget.Select     // Target resolution for upscaling
	gpuBackend        *widget.Select     // GPU acceleration backend for the final encode step
}

// NewPostProcessControls constructs the Post-Processing dialog's widgets.
func NewPostProcessControls() *PostProcessControls {
	smoothFPSSlider := widget.NewSlider(24, 120)
	smoothFPSSlider.Step = 1

	sharpenSlider := widget.NewSlider(0, 2)
	sharpenSlider.Step = 0.1

	return &PostProcessControls{
		enablePostProcess: widget.NewCheck("Post-Processing", nil),
		smoothMotion:      widget.NewCheck("Enabled", nil),
		smoothMotionMode:  widget.NewRadioGroup([]string{"Precise (slow)", "Balanced", "Fast"}, nil),
		smoothMotionFPS:   smoothFPSSlider,
		sharpen:           widget.NewCheck("Sharpen Video", nil),
		sharpenAmount:     sharpenSlider,
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
		gpuBackend:        widget.NewSelect(GPUBackendOptions(), nil),
	}
}

// NewUIWidgets constructs the full widget bag for the application.
func NewUIWidgets() *UIWidgets {
	return &UIWidgets{
		download:    NewDownloadControls(),
		prefs:       NewPreferenceControls(),
		postProcess: NewPostProcessControls(),
	}
}

// DownloadStats tracks the real-time metrics of a download session.
type DownloadStats struct {
	lastSize      string  // Last size reported by yt-dlp e.g., "15.2MiB"
	downloadedRaw float64 // Numeric value for calculations
	unit          string  // e.g., "MiB"
	targetPct     float64 // The target percentage to aim for, for smoothing logic
}

// DownloaderApp acts as a coordinator, holding pointers to the specialized
// sub-structs and handling application lifecycle.
type DownloaderApp struct {
	window     fyne.Window           // The primary application window
	ui         *UIWidgets            // The graphical interface components
	stats      *DownloadStats        // Statistics tracked during a session
	logSvc     *LogService           // Session log, error log, and buffer-limit management
	cancelMu   sync.Mutex            // Guards cancelFn updates and reads
	cancelFn   context.CancelFunc    // Function used to signal yt-dlp to stop
	stopPulse  chan struct{}         // Closed to stop the status dot pulse goroutine
	uiManager  *UIManager            // Owns secondary window state (About, Help, History, Prefs, PP)
	prefSvc    *PreferenceService    // Centralised preference loading and persistence
	historySvc *HistoryService       // Download history persistence
	depSvc     *DependencyService    // Binary path resolution, dependency checks, and yt-dlp updater
	gpuSvc     *GPUCapabilityService // GPU backend capability detection and cache

	// Track processing failures across concurrent workers so we can adjust the
	// Retry button text at the end of the batch.
	ppFailed  atomic.Int32
	isRunning atomic.Bool // true while a download or post-processing session is active
}

// AppConfig represents the JSON configuration file structure.
type AppConfig struct {
	Format   string `json:"format"`
	Quality  string `json:"quality"`
	Path     string `json:"path"`
	MaxSpeed string `json:"maxSpeed"`
}

// PostProcessJob holds the inputs for a single file's FFmpeg post-processing pass.
type PostProcessJob struct {
	inputPath   string
	tmpOutput   string
	finalPath   string // destination after FFmpeg succeeds; may differ from inputPath (e.g. .webm → .mkv)
	ffmpegArgs  []string
	vfFilters   []string // active video filters, for summary logging
	afFilters   []string // active audio filters, for summary logging
	threads     int      // thread count assigned to this job
	encodeMode  string   // human-readable encode strategy, for summary logging
	totalFrames int64    // total video frames, for progress percentage (0 = unknown)
	usedGPU     bool     // true if ffmpegArgs uses a GPU encoder; enables one CPU retry on failure
}
