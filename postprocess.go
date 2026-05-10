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
	"runtime"
	"strings"
	"sync"
	"time"
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

// ppJob holds the inputs for a single file's FFmpeg post-processing pass.
type ppJob struct {
	inputPath   string
	tmpOutput   string
	ffmpegArgs  []string
	vfFilters   []string // active video filters, for summary logging
	afFilters   []string // active audio filters, for summary logging
	threads     int      // thread count assigned to this job
	encodeMode  string   // human-readable encode strategy, for summary logging
}

// applyFFmpegFilters runs a concurrent worker pool to post-process each of
// the given files with the provided video/audio filters. Workers are bounded
// to runtime.NumCPU() so that the total thread load across all FFmpeg processes
// never exceeds the available core count. Each worker receives an evenly
// divided thread budget so concurrent jobs don't compete for CPU.
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

	// Build one job per file, skipping files that need no processing.
	var jobs []ppJob
	for _, inputPath := range filePaths {
		ext := strings.ToLower(filepath.Ext(inputPath))
		isAudioOnly := ext == ".mp3" || ext == ".m4a"

		activeVF := vfFilters
		if isAudioOnly {
			activeVF = nil // video filters don't apply to audio-only files
		}
		if len(activeVF) == 0 && len(afFilters) == 0 {
			continue
		}

		tmpOutput := strings.TrimSuffix(inputPath, ext) + "_pp" + ext
		encodeMode := "Stream copy"
		if len(activeVF) > 0 {
			encodeMode = "Re-encode (libx264, CRF 18, slower)"
		}
		jobs = append(jobs, ppJob{
			inputPath:  inputPath,
			tmpOutput:  tmpOutput,
			ffmpegArgs: buildFFmpegArgs(inputPath, tmpOutput, activeVF, afFilters),
			vfFilters:  activeVF,
			afFilters:  afFilters,
			encodeMode: encodeMode,
		})
	}

	if len(jobs) == 0 {
		return
	}

	// Log a summary of the active filters before starting any workers.
	var filterSummary []string
	filterSummary = append(filterSummary, fmt.Sprintf("files: %d", len(jobs)))
	if len(vfFilters) > 0 {
		filterSummary = append(filterSummary, "vf: "+strings.Join(vfFilters, ", "))
	}
	if len(afFilters) > 0 {
		filterSummary = append(filterSummary, "af: "+strings.Join(afFilters, ", "))
	}
	app.appendOutput(
		fmt.Sprintf("[SYSTEM] Starting post-processing (%s)", strings.Join(filterSummary, " | ")),
		color.RGBA{R: 0, G: 255, B: 255, A: 255},
	)

	// Cap workers at the number of logical CPU cores and at the number of jobs.
	numWorkers := runtime.NumCPU()
	if numWorkers > len(jobs) {
		numWorkers = len(jobs)
	}

	// Divide available cores evenly so concurrent FFmpeg processes don't
	// fight each other for threads. Minimum 1 thread per process.
	threadsPerJob := runtime.NumCPU() / numWorkers
	if threadsPerJob < 1 {
		threadsPerJob = 1
	}

	// Patch the -threads value in each job's arg list now that we know the count.
	for i := range jobs {
		jobs[i].threads = threadsPerJob
		jobs[i].ffmpegArgs = patchThreadCount(jobs[i].ffmpegArgs, fmt.Sprintf("%d", threadsPerJob))
	}

	// Push all jobs into a buffered channel and close it so workers stop when empty.
	jobCh := make(chan ppJob, len(jobs))
	for _, job := range jobs {
		jobCh <- job
	}
	close(jobCh)

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				app.runFFmpegJob(ctx, ffmpegPath, job)
			}
		}()
	}
	wg.Wait()
}

// buildFFmpegArgs constructs the FFmpeg argument list for a single post-processing
// job. The -threads placeholder is set to "0" and patched to the real count later.
func buildFFmpegArgs(inputPath, tmpOutput string, vfFilters, afFilters []string) []string {
	args := []string{"-y", "-threads", "0", "-i", inputPath}
	if len(vfFilters) > 0 {
		// Re-encode with high quality settings to prevent the output from being
		// smaller/worse than the original. CRF 18 is visually near-lossless for
		// H.264. The slower preset squeezes more quality out at the same CRF.
		args = append(args, "-vf", strings.Join(vfFilters, ","))
		args = append(args, "-c:v", "libx264", "-crf", "18", "-preset", "slower")
	} else {
		args = append(args, "-c:v", "copy")
	}
	if len(afFilters) > 0 {
		args = append(args, "-af", strings.Join(afFilters, ","))
	} else {
		args = append(args, "-c:a", "copy")
	}
	return append(args, tmpOutput)
}

// patchThreadCount replaces the value immediately after the "-threads" flag in
// an FFmpeg argument slice with the given count string.
func patchThreadCount(args []string, count string) []string {
	for i, arg := range args {
		if arg == "-threads" && i+1 < len(args) {
			args[i+1] = count
			return args
		}
	}
	return args
}

// runFFmpegJob executes a single ppJob and logs the outcome to the UI.
func (app *DownloaderApp) runFFmpegJob(ctx context.Context, ffmpegPath string, job ppJob) {
	app.appendOutput(
		fmt.Sprintf("[SYSTEM] Post-processing: %s", filepath.Base(job.inputPath)),
		color.RGBA{R: 0, G: 255, B: 255, A: 255},
	)

	// Capture input file size before processing.
	var sizeBefore int64
	if info, err := os.Stat(job.inputPath); err == nil {
		sizeBefore = info.Size()
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, ffmpegPath, job.ffmpegArgs...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	duration := time.Since(start)
	if err != nil {
		app.appendOutput(
			fmt.Sprintf("[ERROR] Post-processing failed: %v", err),
			color.RGBA{R: 255, G: 0, B: 0, A: 255},
		)
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line != "" {
				app.appendOutput(line, color.RGBA{R: 180, G: 180, B: 180, A: 255})
			}
		}
		os.Remove(job.tmpOutput)
		return
	}

	// Capture output size before the rename so we can compute the delta.
	var sizeAfter int64
	if info, err := os.Stat(job.tmpOutput); err == nil {
		sizeAfter = info.Size()
	}

	os.Remove(job.inputPath)
	os.Rename(job.tmpOutput, job.inputPath)

	// Build a human-readable size change string.
	sizeDelta := ""
	if sizeBefore > 0 && sizeAfter > 0 {
		deltaPct := (float64(sizeAfter)-float64(sizeBefore))/float64(sizeBefore)*100
		sign := "+"
		if deltaPct < 0 {
			sign = ""
		}
		sizeDelta = fmt.Sprintf("%s → %s (%s%.1f%%)",
			formatBytes(sizeBefore), formatBytes(sizeAfter), sign, deltaPct)
	}

	// Collect active filter names for the summary.
	var filterNames []string
	for _, f := range job.vfFilters {
		filterNames = append(filterNames, filterShortName(f))
	}
	for _, f := range job.afFilters {
		filterNames = append(filterNames, filterShortName(f))
	}

	c := color.RGBA{R: 0, G: 200, B: 0, A: 255}
	app.appendOutput("────────────────────────────────────────", color.RGBA{R: 0, G: 160, B: 0, A: 255})
	app.appendOutput(fmt.Sprintf("POST-PROCESSING COMPLETE: %s", filepath.Base(job.inputPath)), c)
	app.appendOutput(fmt.Sprintf("   ├─ Duration:  %s", formatDuration(duration)), c)
	app.appendOutput(fmt.Sprintf("   ├─ File size: %s", sizeDelta), c)
	app.appendOutput(fmt.Sprintf("   ├─ Encoder:   %s", job.encodeMode), c)
	app.appendOutput(fmt.Sprintf("   ├─ Threads:   %d", job.threads), c)
	app.appendOutput(fmt.Sprintf("   └─ Filters:   %s", strings.Join(filterNames, ", ")), c)
	app.appendOutput("────────────────────────────────────────", color.RGBA{R: 0, G: 160, B: 0, A: 255})
}

// formatBytes formats a byte count as a human-readable string (e.g. "45.2 MiB").
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// formatDuration formats a duration as a compact human-readable string,
// always showing three decimal places on the seconds component for precision.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.3f seconds", d.Seconds())
	}
	m := int(d.Minutes())
	s := d.Seconds() - float64(m)*60
	return fmt.Sprintf("%d minutes and %.3f seconds", m, s)
}

// filterShortName returns a short, readable label for a known FFmpeg filter string.
func filterShortName(f string) string {
	switch {
	case strings.HasPrefix(f, "minterpolate") && strings.Contains(f, "blend"):
		return "Smooth Motion (Fast)"
	case strings.HasPrefix(f, "minterpolate") && strings.Contains(f, "vsbmc"):
		return "Smooth Motion (Balanced)"
	case strings.HasPrefix(f, "minterpolate"):
		return "Smooth Motion (Precise)"
	case strings.HasPrefix(f, "unsharp"):
		return "Sharpen"
	case f == "loudnorm":
		return "Normalize Audio"
	default:
		return f
	}
}

// checkPostProcessingEnabled reports whether any post-processing filter is
// currently selected. Used by callers to skip the FFmpeg pass entirely when
// no filters are active.
func (app *DownloaderApp) checkPostProcessingEnabled() bool {
	return app.ui.smoothMotion.Checked || app.ui.sharpen.Checked || app.ui.normalizeAudio.Checked
}
