package main

import (
	"context"
	"fmt"
	"image/color"
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
		format:      widget.NewSelect(formatOptions, nil),
		quality:     widget.NewSelect(qualityOptions, nil),
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
	maxSpeed       *widget.Entry      // Download speed limit (e.g. 5M)
	themeMode      *widget.RadioGroup // Theme mode selector (Dark / Light)
	cookies        *widget.Entry      // Path to a Mozilla/Netscape-format cookies file
	savePrefs      *widget.Check      // Option to persist preferences between sessions
	logLimit       *widget.Select     // Max lines kept in the graphical log view
	showDebug      *widget.Check      // Option to show yt-dlp [debug] lines in the log view
	checkUpdates   *widget.Check      // Option to check for newer yt-dlp and GoVid releases on startup
	embedMetadata  *widget.Check      // Option to write metadata tags into downloaded files
	embedThumbnail *widget.Check      // Option to write the thumbnail into downloaded files as cover art
	embedChapters  *widget.Check      // Option to write chapter markers into downloaded files
	subtitles      *widget.Select     // Subtitle mode: Off, Embed, Save as .srt, or Both
	subtitleLangs  *widget.Entry      // Subtitle languages, in yt-dlp --sub-langs syntax
	autoSubtitles  *widget.Check      // Option to also take auto-generated captions
	keepHistory    *widget.Check      // Option to record downloads in the download history
}

// NewPreferenceControls constructs the Preferences dialog's widgets.
func NewPreferenceControls() *PreferenceControls {
	maxSpeed := widget.NewEntry()
	maxSpeed.SetPlaceHolder("e.g. 5M (Unlimited if blank)")

	themeMode := widget.NewRadioGroup(themeOptions, nil)
	themeMode.Horizontal = true

	cookies := widget.NewEntry()
	cookies.SetPlaceHolder("Path to cookies.txt (optional)")

	subtitleLangs := widget.NewEntry()
	subtitleLangs.SetPlaceHolder(defaultSubtitleLangs + " (e.g. en.*,de,ja)")

	return &PreferenceControls{
		maxSpeed:       maxSpeed,
		themeMode:      themeMode,
		cookies:        cookies,
		savePrefs:      widget.NewCheck("Save preferences between sessions", nil),
		logLimit:       widget.NewSelect(logLimitOptions, nil),
		showDebug:      widget.NewCheck("Show yt-dlp debug output", nil),
		checkUpdates:   widget.NewCheck("Check for updates on startup", nil),
		embedMetadata:  widget.NewCheck("Metadata", nil),
		embedThumbnail: widget.NewCheck("Thumbnail", nil),
		embedChapters:  widget.NewCheck("Chapters", nil),
		subtitles:      widget.NewSelect(subtitleModeOptions, nil),
		subtitleLangs:  subtitleLangs,
		autoSubtitles:  widget.NewCheck("Include auto-generated", nil),
		keepHistory:    widget.NewCheck("Keep download history", nil),
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
	denoiseMode       *widget.RadioGroup // Denoise method (NLMeans = HQ, hqdn3d = Fast)
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

	smoothModeRadio := widget.NewRadioGroup(smoothModeOptions, nil)
	smoothModeRadio.Horizontal = true

	denoiseModeRadio := widget.NewRadioGroup(denoiseModeOptions, nil)
	denoiseModeRadio.Horizontal = true

	return &PostProcessControls{
		enablePostProcess: widget.NewCheck("Post-Processing", nil),
		smoothMotion:      widget.NewCheck("Enabled", nil),
		smoothMotionMode:  smoothModeRadio,
		smoothMotionFPS:   smoothFPSSlider,
		sharpen:           widget.NewCheck("Sharpen Video", nil),
		sharpenAmount:     sharpenSlider,
		normalizeAudio:    widget.NewCheck("Normalize Audio", nil),
		vividMode:         widget.NewCheck("Vivid Mode", nil),
		denoise:           widget.NewCheck("Denoise", nil),
		denoiseMode:       denoiseModeRadio,
		hdrToSdr:          widget.NewCheck("HDR to SDR", nil),
		deband:            widget.NewCheck("Fix Banding", nil),
		autoCrop:          widget.NewCheck("Auto-Crop", nil),
		stabilize:         widget.NewCheck("Stabilize", nil),
		deinterlace:       widget.NewCheck("Deinterlace", nil),
		nightMode:         widget.NewCheck("Night Mode", nil),
		upscaleVideo:      widget.NewCheck("Upscale Video", nil),
		upscaleTarget:     widget.NewSelect(upscaleTargetOptions, nil),
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

// DownloadStats tracks the real-time metrics of a download session. It is
// written from the yt-dlp output goroutine and read by the progress smoother
// and runYtDlp, so every field is guarded by mu.
type DownloadStats struct {
	mu            sync.Mutex
	lastSize      string  // Last size reported by yt-dlp e.g., "15.2MiB"
	downloadedRaw float64 // Numeric value for calculations
	unit          string  // e.g., "MiB"
	targetPct     float64 // The target percentage to aim for, for smoothing logic
	snapPending   bool    // true when the smoother should jump straight to targetPct
}

// setTarget records a new progress target. When snap is true the smoother
// jumps to it on its next tick instead of easing towards it.
func (s *DownloadStats) setTarget(pct float64, snap bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targetPct = pct
	s.snapPending = snap
}

// takeTarget returns the current progress target and whether a snap was
// requested, clearing the snap request.
func (s *DownloadStats) takeTarget() (pct float64, snap bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap = s.snapPending
	s.snapPending = false
	return s.targetPct, snap
}

// reset clears the metrics of the previous download and requests a snap of
// the progress bar back to 0, so a download that fails before reporting
// progress does not inherit the previous one's size and speed.
func (s *DownloadStats) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSize = ""
	s.downloadedRaw = 0
	s.unit = ""
	s.targetPct = 0
	s.snapPending = true
}

// recordSize stores the latest downloaded size reported by yt-dlp, e.g. "15.2MiB".
func (s *DownloadStats) recordSize(size string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSize = size
	fmt.Sscanf(size, "%f%s", &s.downloadedRaw, &s.unit)
}

// sizeSnapshot returns the latest downloaded size and its parsed parts.
func (s *DownloadStats) sizeSnapshot() (lastSize string, downloadedRaw float64, unit string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSize, s.downloadedRaw, s.unit
}

// DownloaderApp acts as a coordinator, holding pointers to the specialized
// sub-structs and handling application lifecycle.
type DownloaderApp struct {
	window      fyne.Window           // The primary application window
	ui          *UIWidgets            // The graphical interface components
	stats       *DownloadStats        // Statistics tracked during a session
	logSvc      *LogService           // Session log, error log, and buffer-limit management
	cancelMu    sync.Mutex            // Guards cancelFn and stopFn updates and reads
	cancelFn    context.CancelFunc    // Function used to signal yt-dlp to stop; in batch mode it skips only the current item
	stopFn      context.CancelFunc    // Stops the whole session, including the rest of a batch queue
	sessions    sync.WaitGroup        // Counts running sessions so Shutdown can wait for them to finish
	stopPulse   chan struct{}         // Closed to stop the status dot pulse goroutine
	pulseDone   chan struct{}         // Closed by the pulse goroutine when it exits
	uiManager   *UIManager            // Owns the main window layout and all secondary windows
	prefSvc     *PreferenceService    // Centralised preference loading and persistence
	historySvc  *HistoryService       // Download history persistence
	depSvc      *DependencyService    // Binary path resolution, dependency checks, and yt-dlp updater
	gpuSvc      *GPUCapabilityService // GPU backend capability detection and cache
	releaseSvc  *ReleaseService       // Latest-release lookups on GitHub for update checks
	selfUpdater *SelfUpdater          // Replaces the running GoVid with a release; nil when its own path is unknown

	// askPlaylist shows the playlist prompt and waits for the answer; set to
	// uiManager.askPlaylist, replaced in tests.
	askPlaylist func(ctx context.Context, prompt playlistPrompt) playlistDecision

	// askDuplicate asks whether to download a video again; set to
	// uiManager.askDuplicate, replaced in tests.
	askDuplicate func(ctx context.Context, prompt duplicatePrompt) duplicateDecision

	// freeBytes returns the free space on the volume holding a folder, and
	// askDiskSpace asks what to do when a download will not fit; set to
	// freeDiskBytes and uiManager.askDiskSpace, replaced in tests.
	freeBytes    func(path string) (uint64, error)
	askDiskSpace func(ctx context.Context, prompt diskSpacePrompt) diskSpaceDecision
	onLogLine    func(line string, col color.Color) // Renders a log line in the UI; set to uiManager.appendLogLine

	// statusThrottle rate-limits and de-duplicates status label updates;
	// see updateStatus.
	statusThrottle *latestValueThrottle[string]

	// sessionFailed is set when any download or post-processing job in the
	// session fails, so the download button offers "Retry" when it ends.
	// Post-processing workers set it concurrently.
	sessionFailed atomic.Bool
	isRunning     atomic.Bool // true while a download or post-processing session is active
	updating      atomic.Bool // true while a GoVid self-update downloads or installs
	showDebug     atomic.Bool // true to show yt-dlp [debug] lines in the log view; see appendOutput
	keepHistory   atomic.Bool // true to record downloads in the history and warn about repeats; see recordHistory

	// queue is the running (or last) session's download queue, shown in the
	// Queue panel; yt-dlp's output readers report download progress to it.
	queue atomic.Pointer[QueueModel]
}
