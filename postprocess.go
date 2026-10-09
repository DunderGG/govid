// postprocess.go — Post-download FFmpeg processing pipeline.
//
// Responsibilities:
//   - PostProcessSettings: a plain-value snapshot of the post-processing UI
//     state, and the pure functions that operate on it (buildPostProcessFilters,
//     computeProcessingLoad).
//   - Thin applyFFmpegFilters wrapper: collects binary paths and wires
//     PPCallbacks before delegating to PPEngine.ApplyFilters.
//   - Shared helpers called by pp_engine.go (same package): formatFFmpegProgress,
//     formatBytes, formatDuration, filterShortName, scanCRLF, lastLine.
package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ── Processing-load cost constants ───────────────────────────────────────────
// Each constant is the cost contribution of one active filter to the overall
// load score returned by computeProcessingLoad. Higher = more CPU-intensive.
// Tuned by feel based on observed encode times; adjust here to recalibrate.
const (
	costSmoothMotionFast     = 30
	costSmoothMotionBalanced = 55
	costSmoothMotionPrecise  = 70
	costDenoiseNLMeans       = 40
	costDenoiseHQDN3D        = 20
	costHDRToSDR             = 25
	costUpscale4K            = 35
	costUpscaleDefault       = 20
	costStabilize            = 20
	costAutoCrop             = 15
	costDeinterlace          = 12
	costSharpen              = 10
	costDeband               = 8
	costVividMode            = 5
	costNormalizeAudio       = 5
	costNightMode            = 5
)

// ── Processing-load description thresholds ───────────────────────────────────
// Boundaries used by computeProcessingLoad to map a raw cost score to a
// human-readable label. The visual block indicator uses loadBlockThresholds
// instead (see below).
const (
	loadThresholdLight     = 20
	loadThresholdModerate  = 50
	loadThresholdHeavy     = 80
	loadThresholdVeryHeavy = 120
)

// loadBlockThresholds are the costs above which each successive block of the
// Post-Processing window's load indicator lights up, one per colour in
// colLoadPalette. They are spaced for a useful visual spread across the
// loadThreshold* scale above, and tuned by feel like the cost constants.
var loadBlockThresholds = [...]int{15, 35, 65, 100, 130}

// autoCropSentinel stands in for the crop filter in the filter chain built by
// buildPostProcessFilters. The crop depends on each file's black bars, so
// PPEngine.resolveAutoCrop replaces it per file once cropdetect has run.
const autoCropSentinel = "__autocrop__"

// toneMapSentinel stands in for the HDR-to-SDR chain in the filter chain
// built by buildPostProcessFilters. PPEngine.resolveToneMap replaces it per
// file with toneMapFilter for that file's transfer function, or drops it for
// SDR sources.
const toneMapSentinel = "__tonemap__"

// HDR transfer functions, as ffprobe and ffmpeg name them.
const (
	transferPQ  = "smpte2084"    // HDR10 / Dolby Vision base layer
	transferHLG = "arib-std-b67" // Hybrid Log-Gamma
)

// toneMapFilterPrefix starts every chain toneMapFilter returns.
const toneMapFilterPrefix = "zscale=tin="

// toneMapFilter returns the HDR-to-SDR chain for a source with the given
// transfer function, or "" when the source is not HDR (PQ or HLG). The
// input transfer, matrix, and primaries are stated explicitly rather than
// read from the frame tags, which are often missing after yt-dlp merges
// VP9/AV1 streams; zscale would otherwise assume BT.709 and wash the
// picture out. The chain linearises the BT.2020 input, tone maps it with
// Hable, and converts it to 8-bit BT.709.
func toneMapFilter(transfer string) string {
	if transfer != transferPQ && transfer != transferHLG {
		return ""
	}
	return toneMapFilterPrefix + transfer + ":min=bt2020nc:pin=bt2020:t=linear:npl=100," +
		"format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0," +
		"zscale=t=bt709:m=bt709:r=tv,format=yuv420p"
}

// hasToneMap reports whether a resolved filter chain tone maps to SDR, in
// which case the output must be tagged as BT.709.
func hasToneMap(vfFilters []string) bool {
	for _, filter := range vfFilters {
		if strings.HasPrefix(filter, toneMapFilterPrefix) {
			return true
		}
	}
	return false
}

// PostProcessSettings is a plain-value snapshot of the post-processing UI
// state, decoupling buildPostProcessFilters and computeProcessingLoad from
// *UIWidgets.
type PostProcessSettings struct {
	SmoothMotion     bool
	SmoothMotionMode string
	SmoothMotionFPS  float64
	Sharpen          bool
	SharpenAmount    float64
	VividMode        bool
	Deband           bool
	HDRToSDR         bool
	Denoise          bool
	DenoiseMode      string
	Deinterlace      bool
	Stabilize        bool
	AutoCrop         bool
	UpscaleVideo     bool
	UpscaleTarget    string
	NormalizeAudio   bool
	NightMode        bool
}

// buildPostProcessFilters returns the video filter (vfFilters) and audio
// filter (afFilters) slices to be passed to applyFFmpegFilters for the given
// settings.
func buildPostProcessFilters(ppSetting PostProcessSettings) (vfFilters, afFilters []string) {
	if ppSetting.SmoothMotion {
		vfFilters = append(vfFilters, smoothMotionFilter(ppSetting.SmoothMotionMode, int(ppSetting.SmoothMotionFPS)))
	}
	if ppSetting.Sharpen {
		amount := ppSetting.SharpenAmount
		// CAS (Contrast Adaptive Sharpening) adaptively sharpens edges while
		// leaving smooth areas untouched, avoiding the haloing and noise
		// amplification that unsharp mask produces.
		// AMD recommends 0.3–0.5 for typical content; slider midpoint (1.0)
		// lands at 0.4 for a visible but clean result, while full slider
		// (2.0) reaches 0.8 — strong sharpening without the ringing
		// artifacts that CAS produces near its maximum (1.0).
		// Maps slider 0–2 → CAS strength 0.0–0.8.
		strength := amount * 0.40
		vfFilters = append(vfFilters, fmt.Sprintf("cas=strength=%.2f", strength))
	}
	if ppSetting.VividMode {
		// contrast=1.30 and saturation=1.50 give a strong "vivid" pop; brightness=0.02.
		// gamma_b=1.1 lifts the blue channel in midtones/highlights, counteracting
		// the warm/yellow cast that boosted saturation introduces in white areas.
		vfFilters = append(vfFilters, "eq=contrast=1.30:brightness=0.02:saturation=1.50:gamma_b=1.1")
	}
	if ppSetting.Deband {
		vfFilters = append(vfFilters, "deband")
	}
	if ppSetting.HDRToSDR {
		// The chain depends on each file's transfer function (and SDR files
		// must not be touched), so the engine resolves it per file.
		vfFilters = append(vfFilters, toneMapSentinel)
	}
	if ppSetting.Denoise {
		vfFilters = append(vfFilters, denoiseFilter(ppSetting.DenoiseMode))
	}
	if ppSetting.Deinterlace {
		vfFilters = append(vfFilters, "bwdif")
	}
	if ppSetting.Stabilize {
		vfFilters = append(vfFilters, "deshake")
	}
	if ppSetting.AutoCrop {
		// The actual crop parameters are determined per file by the engine.
		vfFilters = append(vfFilters, autoCropSentinel)
	}
	if ppSetting.UpscaleVideo {
		vfFilters = append(vfFilters, upscaleFilter(ppSetting.UpscaleTarget))
	}
	if ppSetting.NormalizeAudio {
		afFilters = append(afFilters, "loudnorm")
	}
	if ppSetting.NightMode {
		afFilters = append(afFilters, "dynaudnorm=f=300:g=5:p=0.95")
	}
	return
}

// smoothMotionFilter returns the minterpolate filter that raises the frame
// rate to fps in the Smooth Motion mode given.
func smoothMotionFilter(mode string, fps int) string {
	switch mode {
	case smoothModeFast:
		// Frame blending — multi-threaded, much faster, slightly less precise.
		return fmt.Sprintf("minterpolate=fps=%d:mi_mode=blend", fps)
	case smoothModeBalanced:
		// MCI without variant-size blocks — ~40% faster than Precise, similar quality.
		return fmt.Sprintf("minterpolate=fps=%d:mi_mode=mci:vsbmc=0:mc_mode=obmc", fps)
	default: // smoothModePrecise
		return fmt.Sprintf("minterpolate=fps=%d:mi_mode=mci", fps)
	}
}

// denoiseFilter returns the filter of the Denoise mode given.
func denoiseFilter(mode string) string {
	switch mode {
	case denoiseModeNLMeans:
		// s=2.0 is noticeably more effective on compressed web video than the
		// default s=1.0. Research size (15) must always exceed patch size (7).
		return "nlmeans=2.0:7:5:15:9"
	default: // denoiseModeHQDN3D
		// hqdn3d applies both spatial and temporal denoising in one pass.
		// luma_spatial=4, chroma_spatial=3, luma_tmp=6, chroma_tmp=4.5
		return "hqdn3d=4:3:6:4.5"
	}
}

// upscaleFilter returns the scale filter that upscales to target.
//
// It uses FFmpeg's if() expression to skip rescaling when the video is
// already at or above the target height, avoiding a pointless re-encode.
// -2 keeps width proportional and divisible by 2.
// if(gte(ih,TARGET),ih,TARGET) → keep original height when input >= target.
func upscaleFilter(target string) string {
	switch target {
	case upscale1080p:
		return "scale=-2:if(gte(ih\\,1080)\\,ih\\,1080):flags=lanczos"
	case upscale1440p:
		return "scale=-2:if(gte(ih\\,1440)\\,ih\\,1440):flags=lanczos"
	case upscale4K:
		return "scale=-2:if(gte(ih\\,2160)\\,ih\\,2160):flags=lanczos"
	default: // upscaleDouble — no meaningful ceiling; always doubles
		return "scale=iw*2:ih*2:flags=lanczos"
	}
}

// All PPEngine methods and helpers (detectCropFilter, resolveAutoCrop, runJob,
// buildFFmpegArgs, and the probe functions) live in pp_engine.go.

// applyFFmpegFilters creates a PPEngine from resolved binary paths and delegates
// to PPEngine.ApplyFilters, wiring the app's log/status/failure callbacks.
// backend is the Encoder Backend the session started with.
func (app *DownloaderApp) applyFFmpegFilters(ctx context.Context, filePaths, vfFilters, afFilters []string, backend GPUBackend) {
	engine := NewPPEngine(app.depSvc.Resolve("ffmpeg"), app.depSvc.Resolve("ffprobe"))
	engine.GPUBackend = backend
	engine.GPUCapabilities = app.gpuSvc.Detect(ctx)
	engine.ApplyFilters(ctx, filePaths, vfFilters, afFilters, PPCallbacks{
		OnLog:     app.appendOutput,
		OnStatus:  app.updateStatus,
		OnFailure: func() { app.sessionFailed.Store(true) },
	})
}

// formatFFmpegProgress parses a FFmpeg stats line ("frame=X fps=X ... time=HH:MM:SS speed=Xx")
// and returns a compact human-readable string for the status bar.
// When totalFrames > 0 the current frame is converted to a percentage.
func formatFFmpegProgress(line string, totalFrames int64) string {
	get := func(key string) string {
		idx := strings.Index(line, key+"=")
		if idx == -1 {
			return ""
		}
		rest := strings.TrimSpace(line[idx+len(key)+1:])
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return ""
		}
		return fields[0]
	}
	parts := []string{}
	frameStr := get("frame")
	if frameStr != "" {
		if totalFrames > 0 {
			if current, err := strconv.ParseInt(frameStr, 10, 64); err == nil {
				pct := math.Min(float64(current)/float64(totalFrames)*100, 100)
				parts = append(parts, fmt.Sprintf("%.0f%%", pct))
			}
		} else {
			parts = append(parts, "frame "+frameStr)
		}
	}
	if val := get("fps"); val != "" && val != "0" {
		parts = append(parts, val+" fps")
	}
	// Show elapsed time only when we have no percentage (keeps the bar compact).
	if totalFrames == 0 {
		if val := get("time"); val != "" {
			parts = append(parts, "time "+val)
		}
	}
	if val := get("speed"); val != "" {
		parts = append(parts, "speed "+val)
	}
	if len(parts) == 0 {
		return line
	}
	return strings.Join(parts, " | ")
}

// scanCRLF is a bufio.SplitFunc that splits on either \r or \n, handling the
// carriage-return-only line endings FFmpeg uses for its progress output.
func scanCRLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for pos, byteVal := range data {
		if byteVal == '\r' || byteVal == '\n' {
			return pos + 1, data[:pos], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// lastLine returns the last non-empty, trimmed line of s, or "" if s is empty.
func lastLine(line string) string {
	line = strings.TrimSpace(line)
	if idx := strings.LastIndex(line, "\n"); idx != -1 {
		return strings.TrimSpace(line[idx+1:])
	}
	return line
}

// formatBytes formats a byte count as a human-readable string (e.g. "45.2 MiB").
func formatBytes(byteCount int64) string {
	const unit = 1024
	if byteCount < unit {
		return fmt.Sprintf("%d B", byteCount)
	}
	div, exp := int64(unit), 0
	for remaining := byteCount / unit; remaining >= unit; remaining /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(byteCount)/float64(div), "KMGTPE"[exp])
}

// formatDuration formats a duration as a compact human-readable string,
// always showing three decimal places on the seconds component for precision.
func formatDuration(duration time.Duration) string {
	if duration < time.Minute {
		return fmt.Sprintf("%.3f seconds", duration.Seconds())
	}
	minutes := int(duration.Minutes())
	seconds := duration.Seconds() - float64(minutes)*60
	return fmt.Sprintf("%d minutes and %.3f seconds", minutes, seconds)
}

// filterShortName returns a short, readable label for a known FFmpeg filter string.
func filterShortName(filterStr string) string {
	switch {
	case strings.HasPrefix(filterStr, "minterpolate") && strings.Contains(filterStr, "blend"):
		return "Smooth Motion (Fast)"
	case strings.HasPrefix(filterStr, "minterpolate") && strings.Contains(filterStr, "vsbmc"):
		return "Smooth Motion (Balanced)"
	case strings.HasPrefix(filterStr, "minterpolate"):
		return "Smooth Motion (Precise)"
	case strings.HasPrefix(filterStr, "cas"):
		return "Sharpen (CAS)"
	case filterStr == "loudnorm":
		return "Normalize Audio"
	case strings.HasPrefix(filterStr, "eq="):
		return "Vivid Mode"
	case strings.HasPrefix(filterStr, "nlmeans"):
		return "Denoise (NLMeans)"
	case strings.HasPrefix(filterStr, "hqdn3d"):
		return "Denoise (hqdn3d)"
	case strings.HasPrefix(filterStr, toneMapFilterPrefix):
		return "HDR to SDR"
	case filterStr == "deband":
		return "Deband"
	case strings.HasPrefix(filterStr, "crop="):
		return "Auto-Crop"
	case filterStr == "deshake":
		return "Stabilize"
	case filterStr == "bwdif":
		return "Deinterlace"
	case strings.HasPrefix(filterStr, "dynaudnorm"):
		return "Night Mode"
	case strings.HasPrefix(filterStr, "scale="):
		return "Upscale"
	default:
		return filterStr
	}
}

// computeProcessingLoad returns a raw cost score and a human-readable
// description based on the currently selected filters. The score is unbounded
// so callers can show it as-is rather than normalising to 0–1.
func computeProcessingLoad(ppSettings PostProcessSettings) (int, string) {
	cost := processingCost(ppSettings)
	return cost, describeLoad(cost)
}

// processingCost returns the summed cost score of the filters ppSettings
// turns on (the cost* constants).
func processingCost(ppSettings PostProcessSettings) int {
	cost := 0

	if ppSettings.SmoothMotion {
		switch ppSettings.SmoothMotionMode {
		case smoothModeFast:
			cost += costSmoothMotionFast
		case smoothModeBalanced:
			cost += costSmoothMotionBalanced
		default: // smoothModePrecise
			cost += costSmoothMotionPrecise
		}
	}
	if ppSettings.Denoise {
		switch ppSettings.DenoiseMode {
		case denoiseModeNLMeans:
			cost += costDenoiseNLMeans
		default: // denoiseModeHQDN3D
			cost += costDenoiseHQDN3D
		}
	}
	if ppSettings.HDRToSDR {
		cost += costHDRToSDR
	}
	if ppSettings.UpscaleVideo {
		switch ppSettings.UpscaleTarget {
		case upscale4K:
			cost += costUpscale4K
		default:
			cost += costUpscaleDefault
		}
	}
	if ppSettings.Stabilize {
		cost += costStabilize
	}
	if ppSettings.AutoCrop {
		cost += costAutoCrop
	}
	if ppSettings.Deinterlace {
		cost += costDeinterlace
	}
	if ppSettings.Sharpen {
		cost += costSharpen
	}
	if ppSettings.Deband {
		cost += costDeband
	}
	if ppSettings.VividMode {
		cost += costVividMode
	}
	if ppSettings.NormalizeAudio {
		cost += costNormalizeAudio
	}
	if ppSettings.NightMode {
		cost += costNightMode
	}
	return cost
}

// describeLoad returns the human-readable description of a cost score, by
// the loadThreshold* constants.
func describeLoad(cost int) string {
	switch {
	case cost == 0:
		return "No post-processing active"
	case cost < loadThresholdLight:
		return "Light — minimal overhead"
	case cost < loadThresholdModerate:
		return "Moderate — noticeable extra time"
	case cost < loadThresholdHeavy:
		return "Heavy — significant re-encode time"
	case cost < loadThresholdVeryHeavy:
		return "Very Heavy — expect long processing"
	default:
		return "Intensive — expect very long processing"
	}
}
