// postprocess.go — Post-download FFmpeg processing pipeline.
//
// Responsibilities:
//   - Building the video/audio filter lists from the current UI state.
//   - Renaming temp files to their clean, conflict-free final names.
//   - Running a dedicated FFmpeg pass to apply post-processing filters.
package main

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// buildPostProcessFilters reads the post-processing checkbox state from the UI
// and returns the video filter (vfFilters) and audio filter (afFilters) slices
// to be passed to applyFFmpegFilters.
func (app *DownloaderApp) buildPostProcessFilters() (vfFilters, afFilters []string) {
	if app.ui.smoothMotion.Checked {
		switch app.ui.smoothMotionMode.Selected {
		case "Fast":
			// Frame blending — multi-threaded, much faster, slightly less precise.
			vfFilters = append(vfFilters, "minterpolate=fps=60:mi_mode=blend")
		case "Balanced":
			// MCI without variant-size blocks — ~40% faster than Precise, similar quality.
			vfFilters = append(vfFilters, "minterpolate=fps=60:mi_mode=mci:vsbmc=0:mc_mode=obmc")
		default: // "Precise (slow)"
			vfFilters = append(vfFilters, "minterpolate=fps=60:mi_mode=mci")
		}
	}
	if app.ui.sharpen.Checked {
		vfFilters = append(vfFilters, "unsharp=3:3:1.5:3:3:0.5")
	}
	if app.ui.normalizeAudio.Checked {
		afFilters = append(afFilters, "loudnorm")
	}
	return
}

// finalizeDownloadedFiles finds all files written by yt-dlp under the given
// downloadID token, strips the token from their names, and renames them to
// their final conflict-free paths using uniquePath. It returns the list of
// final paths so callers can apply further post-processing.
func (app *DownloaderApp) finalizeDownloadedFiles(savePath, downloadID string) []string {
	pattern := filepath.Join(savePath, "*"+downloadID+"*")
	matches, _ := filepath.Glob(pattern)
	var finalPaths []string
	for _, tmpPath := range matches {
		cleanBase := strings.Replace(filepath.Base(tmpPath), "_"+downloadID, "", 1)
		cleanPath := filepath.Join(savePath, cleanBase)
		finalPath := uniquePath(cleanPath)
		if finalPath != cleanPath {
			app.appendOutput(
				fmt.Sprintf("[SYSTEM] File already exists — saving as: %s", filepath.Base(finalPath)),
				color.RGBA{R: 0, G: 255, B: 255, A: 255},
			)
		}
		os.Rename(tmpPath, finalPath)
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

// applyFFmpegFilters runs a dedicated FFmpeg pass on each of the given files,
// applying the video filters (vfFilters) and audio filters (afFilters).
// Video filters are skipped for audio-only files. The original file is replaced
// with the filtered output only if FFmpeg succeeds.
func (app *DownloaderApp) applyFFmpegFilters(ctx context.Context, filePaths, vfFilters, afFilters []string) {
	if len(filePaths) == 0 || (len(vfFilters) == 0 && len(afFilters) == 0) {
		return
	}

	ffmpegPath := app.getLocalBinPath("ffmpeg")
	if _, err := os.Stat(ffmpegPath); err != nil {
		ffmpegPath = "ffmpeg"
	}

	for _, inputPath := range filePaths {
		ext := strings.ToLower(filepath.Ext(inputPath))
		isAudioOnly := ext == ".mp3" || ext == ".m4a"

		activeVF := vfFilters
		if isAudioOnly {
			activeVF = nil // video filters don't apply to audio files
		}
		if len(activeVF) == 0 && len(afFilters) == 0 {
			continue
		}

		tmpOutput := strings.TrimSuffix(inputPath, ext) + "_pp" + ext
		args := []string{"-y", "-threads", "0", "-i", inputPath}

		if len(activeVF) > 0 {
			// Re-encode with high quality settings to prevent the output from being
			// smaller/worse than the original. CRF 18 is visually near-lossless for
			// H.264. The slower preset squeezes more quality out at the same CRF and
			// keeps all threads saturated for the full encode.
			args = append(args, "-vf", strings.Join(activeVF, ","))
			args = append(args, "-c:v", "libx264", "-crf", "18", "-preset", "slower")
		} else {
			args = append(args, "-c:v", "copy")
		}
		if len(afFilters) > 0 {
			args = append(args, "-af", strings.Join(afFilters, ","))
		} else {
			args = append(args, "-c:a", "copy")
		}
		args = append(args, tmpOutput)

		app.appendOutput(
			fmt.Sprintf("[SYSTEM] Applying post-processing to: %s", filepath.Base(inputPath)),
			color.RGBA{R: 0, G: 255, B: 255, A: 255},
		)

		// Run FFmpeg with the constructed arguments. 
		// The context allows this process to be killed if the user cancels the download while post-processing is underway.
		cmd := exec.CommandContext(ctx, ffmpegPath, args...)
		// Hide the FFmpeg window on Windows to prevent it from popping up.
		hideWindow(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			app.appendOutput(
				fmt.Sprintf("[ERROR] Post-processing failed: %v", err),
				color.RGBA{R: 255, G: 0, B: 0, A: 255},
			)
			// Log FFmpeg's output to help with diagnosis.
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if line != "" {
					app.appendOutput(line, color.RGBA{R: 180, G: 180, B: 180, A: 255})
				}
			}
			os.Remove(tmpOutput)
			continue
		}

		os.Remove(inputPath)
		os.Rename(tmpOutput, inputPath)
		app.appendOutput("[SYSTEM] Post-processing complete.", color.RGBA{R: 0, G: 200, B: 0, A: 255})
	}
}

// checkPostProcessingEnabled reports whether any post-processing filter is
// currently selected. Used by callers to skip the FFmpeg pass entirely when
// no filters are active.
func (app *DownloaderApp) checkPostProcessingEnabled() bool {
	return app.ui.smoothMotion.Checked || app.ui.sharpen.Checked || app.ui.normalizeAudio.Checked
}
