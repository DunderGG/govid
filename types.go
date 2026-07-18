package main

import (
	"context"
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

// UIWidgets holds the graphical components of the application.
type UIWidgets struct {
	entry             *widget.Entry       // URL input field
	path              *widget.Entry       // Save directory input field
	output            *container.Scroll   // Scrollable container for logs
	logList           *fyne.Container     // Vertical box containing individual log lines
	progress          *widget.ProgressBar // Visual progress indicator
	status            *widget.Label       // Short status message (e.g. "Downloading...")
	format            *widget.Select      // File format selector (MP4, MP3, etc.)
	quality           *widget.Select      // Maximum resolution selector
	saveLog           *widget.Check       // Option to persist output to a .txt file
	notify            *widget.Check       // Option to send a system notification on completion
	autoRetry         *widget.Check       // Option to automatically retry on transient errors
	enablePostProcess *widget.Check       // Master toggle to enable/disable all post-processing
	downloadBtn       *widget.Button      // Start button for downloads
	cancelBtn         *widget.Button      // Stop button for active downloads
	statusDot         *canvas.Circle      // Animated state indicator dot next to the status label
	trimStart         *widget.Entry       // Optional start time for video trimming (HH:MM:SS)
	trimEnd           *widget.Entry       // Optional end time for video trimming (HH:MM:SS)
	maxSpeed          *widget.Entry       // Download speed limit (e.g. 5M)
	themeMode         *widget.RadioGroup  // Theme mode selector (Dark / Light)
	cookies           *widget.Entry       // Path to a Mozilla/Netscape-format cookies file
	savePrefs         *widget.Check       // Option to persist preferences between sessions
	logLimit          *widget.Select      // Max lines kept in the graphical log view
	batchMode         *widget.Check       // Option to switch URL input to multi-line batch mode
	smoothMotion      *widget.Check       // Post-processing: Smooth to custom fps
	smoothMotionMode  *widget.RadioGroup  // Quality mode for Smooth Motion
	smoothMotionFPS   *widget.Slider      // Target framerate for motion smoothing
	sharpen           *widget.Check       // Post-processing: Apply unsharp mask
	sharpenAmount     *widget.Slider      // Post-processing: Sharpening intensity
	normalizeAudio    *widget.Check       // Post-processing: Normalize audio loudness
	vividMode         *widget.Check       // Post-processing: Color/saturation enhancement
	denoise           *widget.Check       // Post-processing: Noise reduction
	denoiseMode       *widget.RadioGroup  // Denoise method (NLMeans = HQ, ATADenoise = Fast)
	hdrToSdr          *widget.Check       // Post-processing: HDR to SDR tone mapping
	deband            *widget.Check       // Post-processing: Fix gradient banding
	autoCrop          *widget.Check       // Post-processing: Auto-crop black bars
	stabilize         *widget.Check       // Post-processing: Video stabilization (deshake)
	deinterlace       *widget.Check       // Post-processing: Deinterlace (bwdif)
	nightMode         *widget.Check       // Post-processing: Dynamic audio compression
	upscaleVideo      *widget.Check       // Post-processing: Resolution upscaling
	upscaleTarget     *widget.Select      // Target resolution for upscaling
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
	window     fyne.Window        // The primary application window
	ui         *UIWidgets         // The graphical interface components
	stats      *DownloadStats     // Statistics tracked during a session
	logSvc     *LogService        // Session log, error log, and buffer-limit management
	cancelFn   context.CancelFunc // Function used to signal yt-dlp to stop
	stopPulse  chan struct{}       // Closed to stop the status dot pulse goroutine
	uiManager  *UIManager         // Owns secondary window state (About, Help, History, Prefs, PP)
	prefSvc    *PreferenceService // Centralised preference loading and persistence
	historySvc *HistoryService    // Download history persistence
	depSvc     *DependencyService // Binary path resolution, dependency checks, and yt-dlp updater

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
}
