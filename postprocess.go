// postprocess.go — Post-download FFmpeg processing pipeline.
//
// Responsibilities:
//   - Building the video/audio filter lists from the current UI state.
//   - Renaming temp files to their clean, conflict-free final names.
//   - Thin applyFFmpegFilters wrapper: collects binary paths and wires
//     PPCallbacks before delegating to PPEngine.ApplyFilters.
//   - Shared helpers called by pp_engine.go (same package): formatFFmpegProgress,
//     formatBytes, formatDuration, filterShortName, scanCRLF.
package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
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

// buildPostProcessFilters reads the post-processing checkbox state from the UI
// and returns the video filter (vfFilters) and audio filter (afFilters) slices
// to be passed to applyFFmpegFilters.
func (app *DownloaderApp) buildPostProcessFilters() (vfFilters, afFilters []string) {
	if app.ui.smoothMotion.Checked {
		fps := int(app.ui.smoothMotionFPS.Value)
		switch app.ui.smoothMotionMode.Selected {
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
	if app.ui.sharpen.Checked {
		amount := app.ui.sharpenAmount.Value
		// CAS (Contrast Adaptive Sharpening) adaptively sharpens edges while
		// leaving smooth areas untouched, avoiding the haloing and noise
		// amplification that unsharp mask produces.
		// AMD recommends 0.3–0.5 for typical content; cap at 0.5 to prevent
		// over-sharpening artifacts and excessive encoder bitrate.
		// Maps slider 0–2 → CAS strength 0.0–0.5.
		strength := amount * 0.35
		vfFilters = append(vfFilters, fmt.Sprintf("cas=strength=%.2f", strength))
	}
	if app.ui.vividMode.Checked {
		// contrast=1.15 adds a subtle pop; saturation=1.25 enriches colours.
		// gamma_b=1.08 lifts the blue channel in midtones/highlights, counteracting
		// the warm/yellow cast that boosted saturation introduces in white areas.
		vfFilters = append(vfFilters, "eq=contrast=1.15:saturation=1.25:gamma_b=1.08")
	}
	if app.ui.deband.Checked {
		vfFilters = append(vfFilters, "deband")
	}
	if app.ui.hdrToSdr.Checked {
		// Multi-step HDR-to-SDR pipeline: linearise → tonemap (Hable) → convert to BT.709.
		vfFilters = append(vfFilters, "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:min=gbr:r=tv,format=yuv420p")
	}
	if app.ui.denoise.Checked {
		switch app.ui.denoiseMode.Selected {
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
	if app.ui.deinterlace.Checked {
		vfFilters = append(vfFilters, "bwdif")
	}
	if app.ui.stabilize.Checked {
		vfFilters = append(vfFilters, "deshake")
	}
	if app.ui.autoCrop.Checked {
		// The actual crop parameters are determined per-file in applyFFmpegFilters.
		vfFilters = append(vfFilters, "__autocrop__")
	}
	if app.ui.upscaleVideo.Checked {
		// Use FFmpeg's if() expression to skip rescaling when the video is already
		// at or above the target height, avoiding a pointless re-encode.
		// -2 keeps width proportional and divisible by 2.
		// if(gte(ih,TARGET),ih,TARGET) → keep original height when input >= target.
		switch app.ui.upscaleTarget.Selected {
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
	if app.ui.normalizeAudio.Checked {
		afFilters = append(afFilters, "loudnorm")
	}
	if app.ui.nightMode.Checked {
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
	engine.ApplyFilters(ctx, filePaths, vfFilters, afFilters, PPCallbacks{
		OnLog:     app.appendOutput,
		OnStatus:  app.updateStatus,
		OnFailure: func() { app.ppFailed.Store(1) },
	})
}

// finalizeDownloadedFiles finds all files written by yt-dlp under the given
// downloadID token, strips the token from their names, and renames them to
// their final conflict-free paths using uniquePath. It returns the list of
// final paths so callers can apply further post-processing.
func (app *DownloaderApp) finalizeDownloadedFiles(savePath, downloadID string) []string {
	pattern := filepath.Join(savePath, "*"+downloadID+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	var finalPaths []string
	for _, tmpPath := range matches {
		cleanBase := strings.Replace(filepath.Base(tmpPath), "_"+downloadID, "", 1)
		cleanPath := filepath.Join(savePath, cleanBase)
		finalPath := uniquePath(cleanPath)
		if finalPath != cleanPath {
			app.appendOutput(
				fmt.Sprintf("[SYSTEM] File already exists — saving as: %s", filepath.Base(finalPath)),
				colSystem,
			)
		}
		if err := os.Rename(tmpPath, finalPath); err != nil {
			app.appendOutput(
				fmt.Sprintf("[SYSTEM] Failed to rename file: %v", err),
				colErrorSoft,
			)
		}
		finalPaths = append(finalPaths, finalPath)
	}
	return finalPaths
}

// uniquePath returns path unchanged when no file exists at that location.
// If the path is already taken, it appends an incrementing numeric suffix
// to the base (e.g. "Video.mp4" → "Video 1.mp4" → "Video 2.mp4") until it
// finds a name that does not conflict with an existing file.
func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)

	// Loop until we find a filename that doesn't exist.
	// Theoretically this could run indefinitely if there are always conflicting files,
	// but in practice it's unlikely anyone will have dozens of duplicates in the same folder.
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s %d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
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
func (app *DownloaderApp) checkPostProcessingEnabled() bool {
	ui := app.ui
	return ui.smoothMotion.Checked || ui.sharpen.Checked || ui.normalizeAudio.Checked ||
		ui.vividMode.Checked || ui.denoise.Checked || ui.hdrToSdr.Checked ||
		ui.deband.Checked || ui.autoCrop.Checked || ui.stabilize.Checked ||
		ui.deinterlace.Checked || ui.nightMode.Checked || ui.upscaleVideo.Checked
}

// computeProcessingLoad returns a raw cost score and a human-readable
// description based on the currently selected filters. The score is unbounded
// so callers can show it as-is rather than normalising to 0–1.
func (app *DownloaderApp) computeProcessingLoad() (int, string) {
	ui := app.ui
	cost := 0

	if ui.smoothMotion.Checked {
		switch ui.smoothMotionMode.Selected {
		case "Fast":
			cost += costSmoothMotionFast
		case "Balanced":
			cost += costSmoothMotionBalanced
		default: // "Precise (slow)"
			cost += costSmoothMotionPrecise
		}
	}
	if ui.denoise.Checked {
		switch ui.denoiseMode.Selected {
		case "NLMeans (HQ, slow)":
			cost += costDenoiseNLMeans
		default: // hqdn3d (Balanced)
			cost += costDenoiseHQDN3D
		}
	}
	if ui.hdrToSdr.Checked {
		cost += costHDRToSDR
	}
	if ui.upscaleVideo.Checked {
		switch ui.upscaleTarget.Selected {
		case "4K (2160p)":
			cost += costUpscale4K
		default:
			cost += costUpscaleDefault
		}
	}
	if ui.stabilize.Checked {
		cost += costStabilize
	}
	if ui.autoCrop.Checked {
		cost += costAutoCrop
	}
	if ui.deinterlace.Checked {
		cost += costDeinterlace
	}
	if ui.sharpen.Checked {
		cost += costSharpen
	}
	if ui.deband.Checked {
		cost += costDeband
	}
	if ui.vividMode.Checked {
		cost += costVividMode
	}
	if ui.normalizeAudio.Checked {
		cost += costNormalizeAudio
	}
	if ui.nightMode.Checked {
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
