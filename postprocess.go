// postprocess.go — Post-download FFmpeg processing pipeline.
//
// Responsibilities:
//   - PostProcessSettings: a plain-value snapshot of the post-processing UI
//     state, and the pure functions that operate on it (buildPostProcessFilters,
//     computeProcessingLoad, checkPostProcessingEnabled).
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
// human-readable label. The visual block indicator in ui.go uses its own
// (slightly different) thresholds aligned to the five block positions.
const (
	loadThresholdLight     = 20
	loadThresholdModerate  = 50
	loadThresholdHeavy     = 80
	loadThresholdVeryHeavy = 120
)

// PostProcessSettings is a plain-value snapshot of the post-processing UI
// state, decoupling buildPostProcessFilters, computeProcessingLoad, and
// checkPostProcessingEnabled from *UIWidgets.
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

// newPostProcessSettings snapshots the post-processing widgets into a
// PostProcessSettings value.
func newPostProcessSettings(ui *UIWidgets) PostProcessSettings {
	return PostProcessSettings{
		SmoothMotion:     ui.smoothMotion.Checked,
		SmoothMotionMode: ui.smoothMotionMode.Selected,
		SmoothMotionFPS:  ui.smoothMotionFPS.Value,
		Sharpen:          ui.sharpen.Checked,
		SharpenAmount:    ui.sharpenAmount.Value,
		VividMode:        ui.vividMode.Checked,
		Deband:           ui.deband.Checked,
		HDRToSDR:         ui.hdrToSdr.Checked,
		Denoise:          ui.denoise.Checked,
		DenoiseMode:      ui.denoiseMode.Selected,
		Deinterlace:      ui.deinterlace.Checked,
		Stabilize:        ui.stabilize.Checked,
		AutoCrop:         ui.autoCrop.Checked,
		UpscaleVideo:     ui.upscaleVideo.Checked,
		UpscaleTarget:    ui.upscaleTarget.Selected,
		NormalizeAudio:   ui.normalizeAudio.Checked,
		NightMode:        ui.nightMode.Checked,
	}
}

// buildPostProcessFilters returns the video filter (vfFilters) and audio
// filter (afFilters) slices to be passed to applyFFmpegFilters for the given
// settings.
func buildPostProcessFilters(s PostProcessSettings) (vfFilters, afFilters []string) {
	if s.SmoothMotion {
		fps := int(s.SmoothMotionFPS)
		switch s.SmoothMotionMode {
		case "Fast":
			// Frame blending — multi-threaded, much faster, slightly less precise.
			vfFilters = append(vfFilters, fmt.Sprintf("minterpolate=fps=%d:mi_mode=blend", fps))
		case "Balanced":
			// MCI without variant-size blocks — ~40% faster than Precise, similar quality.
			vfFilters = append(vfFilters, fmt.Sprintf("minterpolate=fps=%d:mi_mode=mci:vsbmc=0:mc_mode=obmc", fps))
		default: // "Precise (slow)"
			vfFilters = append(vfFilters, fmt.Sprintf("minterpolate=fps=%d:mi_mode=mci", fps))
		}
	}
	if s.Sharpen {
		amount := s.SharpenAmount
		// CAS (Contrast Adaptive Sharpening) adaptively sharpens edges while
		// leaving smooth areas untouched, avoiding the haloing and noise
		// amplification that unsharp mask produces.
		// AMD recommends 0.3–0.5 for typical content; cap at 0.5 to prevent
		// over-sharpening artifacts and excessive encoder bitrate.
		// Maps slider 0–2 → CAS strength 0.0–0.5.
		strength := amount * 0.35
		vfFilters = append(vfFilters, fmt.Sprintf("cas=strength=%.2f", strength))
	}
	if s.VividMode {
		// contrast=1.30 and saturation=1.50 give a strong "vivid" pop; brightness=0.02
		// and gamma=1.05 lift overall midtones slightly to keep shadows from crushing.
		// gamma_b=1.1 lifts the blue channel in midtones/highlights, counteracting
		// the warm/yellow cast that boosted saturation introduces in white areas.
		vfFilters = append(vfFilters, "eq=contrast=1.30:brightness=0.02:saturation=1.50:gamma=1.05:gamma_b=1.1")
	}
	if s.Deband {
		vfFilters = append(vfFilters, "deband")
	}
	if s.HDRToSDR {
		// Multi-step HDR-to-SDR pipeline: linearise → tonemap (Hable) → convert to BT.709.
		vfFilters = append(vfFilters, "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:min=gbr:r=tv,format=yuv420p")
	}
	if s.Denoise {
		switch s.DenoiseMode {
		case "NLMeans (HQ, slow)":
			// s=2.0 is noticeably more effective on compressed web video than the
			// default s=1.0. Research size (15) must always exceed patch size (7).
			vfFilters = append(vfFilters, "nlmeans=2.0:7:5:15:9")
		default: // hqdn3d (Balanced)
			// hqdn3d applies both spatial and temporal denoising in one pass.
			// luma_spatial=4, chroma_spatial=3, luma_tmp=6, chroma_tmp=4.5
			vfFilters = append(vfFilters, "hqdn3d=4:3:6:4.5")
		}
	}
	if s.Deinterlace {
		vfFilters = append(vfFilters, "bwdif")
	}
	if s.Stabilize {
		vfFilters = append(vfFilters, "deshake")
	}
	if s.AutoCrop {
		// The actual crop parameters are determined per-file in applyFFmpegFilters.
		vfFilters = append(vfFilters, "__autocrop__")
	}
	if s.UpscaleVideo {
		// Use FFmpeg's if() expression to skip rescaling when the video is already
		// at or above the target height, avoiding a pointless re-encode.
		// -2 keeps width proportional and divisible by 2.
		// if(gte(ih,TARGET),ih,TARGET) → keep original height when input >= target.
		switch s.UpscaleTarget {
		case "1080p":
			vfFilters = append(vfFilters, "scale=-2:if(gte(ih\\,1080)\\,ih\\,1080):flags=lanczos")
		case "1440p":
			vfFilters = append(vfFilters, "scale=-2:if(gte(ih\\,1440)\\,ih\\,1440):flags=lanczos")
		case "4K (2160p)":
			vfFilters = append(vfFilters, "scale=-2:if(gte(ih\\,2160)\\,ih\\,2160):flags=lanczos")
		default: // "2× (Double)" — no meaningful ceiling; always doubles
			vfFilters = append(vfFilters, "scale=iw*2:ih*2:flags=lanczos")
		}
	}
	if s.NormalizeAudio {
		afFilters = append(afFilters, "loudnorm")
	}
	if s.NightMode {
		afFilters = append(afFilters, "dynaudnorm=f=300:g=5:p=0.95")
	}
	return
}

// All PPEngine methods and helpers (detectCropFilter, resolveAutoCrop, runJob,
// buildFFmpegArgs, patchThreadCount, and the probe functions) live in pp_engine.go.

// applyFFmpegFilters creates a PPEngine from resolved binary paths and delegates
// to PPEngine.ApplyFilters, wiring the app's log/status/failure callbacks.
func (app *DownloaderApp) applyFFmpegFilters(ctx context.Context, filePaths, vfFilters, afFilters []string) {
	engine := NewPPEngine(app.depSvc.Resolve("ffmpeg"), app.depSvc.Resolve("ffprobe"))
	engine.GPUBackend = GPUBackendFromLabel(app.ui.gpuBackend.Selected)
	engine.GPUCapabilities = app.gpuSvc.Detect(ctx)
	engine.ApplyFilters(ctx, filePaths, vfFilters, afFilters, PPCallbacks{
		OnLog:     app.appendOutput,
		OnStatus:  app.updateStatus,
		OnFailure: func() { app.ppFailed.Store(1) },
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
func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.LastIndex(s, "\n"); idx != -1 {
		return strings.TrimSpace(s[idx+1:])
	}
	return s
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
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.3f seconds", d.Seconds())
	}
	minutes := int(d.Minutes())
	seconds := d.Seconds() - float64(minutes)*60
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
	case strings.HasPrefix(filterStr, "atadenoise"):
		return "Denoise (ATADenoise)"
	case strings.HasPrefix(filterStr, "zscale=t=linear"):
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

// checkPostProcessingEnabled reports whether any post-processing filter is
// currently selected. Used by callers to skip the FFmpeg pass entirely when
// no filters are active.
func checkPostProcessingEnabled(s PostProcessSettings) bool {
	return s.SmoothMotion || s.Sharpen || s.NormalizeAudio ||
		s.VividMode || s.Denoise || s.HDRToSDR ||
		s.Deband || s.AutoCrop || s.Stabilize ||
		s.Deinterlace || s.NightMode || s.UpscaleVideo
}

// computeProcessingLoad returns a raw cost score and a human-readable
// description based on the currently selected filters. The score is unbounded
// so callers can show it as-is rather than normalising to 0–1.
func computeProcessingLoad(ppSettings PostProcessSettings) (int, string) {
	cost := 0

	if ppSettings.SmoothMotion {
		switch ppSettings.SmoothMotionMode {
		case "Fast":
			cost += costSmoothMotionFast
		case "Balanced":
			cost += costSmoothMotionBalanced
		default: // "Precise (slow)"
			cost += costSmoothMotionPrecise
		}
	}
	if ppSettings.Denoise {
		switch ppSettings.DenoiseMode {
		case "NLMeans (HQ, slow)":
			cost += costDenoiseNLMeans
		default: // hqdn3d (Balanced)
			cost += costDenoiseHQDN3D
		}
	}
	if ppSettings.HDRToSDR {
		cost += costHDRToSDR
	}
	if ppSettings.UpscaleVideo {
		switch ppSettings.UpscaleTarget {
		case "4K (2160p)":
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

	switch {
	case cost == 0:
		return 0, "No post-processing active"
	case cost < loadThresholdLight:
		return cost, "Light — minimal overhead"
	case cost < loadThresholdModerate:
		return cost, "Moderate — noticeable extra time"
	case cost < loadThresholdHeavy:
		return cost, "Heavy — significant re-encode time"
	case cost < loadThresholdVeryHeavy:
		return cost, "Very Heavy — expect long processing"
	default:
		return cost, "Intensive — expect very long processing"
	}
}
