// postprocess.go — Post-download FFmpeg processing pipeline.
//
// Responsibilities:
//   - Building the video/audio filter lists from the current UI state.
//   - Renaming temp files to their clean, conflict-free final names.
//   - Running a dedicated FFmpeg pass to apply post-processing filters.
package main

import (
	"bufio"
	"context"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

// detectCropFilter runs a quick FFmpeg cropdetect pass on the first 60 seconds of
// the file and returns a "crop=W:H:X:Y" filter string, or "" if no bars were found.
func (app *DownloaderApp) detectCropFilter(ctx context.Context, ffmpegPath, inputPath string) string {
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-t", "60", "-i", inputPath,
		"-vf", "cropdetect=limit=24:round=16:reset=0",
		"-f", "null", "-",
	)
	hideWindow(cmd)
	out, _ := cmd.CombinedOutput()

	// The last "crop=" line contains the tightest detected crop.
	lastCrop := ""
	for _, line := range strings.Split(string(out), "\n") {
		if idx := strings.Index(line, "crop="); idx != -1 {
			fields := strings.Fields(line[idx:])
			if len(fields) > 0 {
				lastCrop = fields[0]
			}
		}
	}
	if lastCrop == "" {
		app.appendOutput("[SYSTEM] Auto-Crop: no black bars detected, skipping.", color.RGBA{R: 0, G: 255, B: 255, A: 255})
	} else {
		app.appendOutput(fmt.Sprintf("[SYSTEM] Auto-Crop: detected %s", lastCrop), color.RGBA{R: 0, G: 255, B: 255, A: 255})
	}
	return lastCrop
}

// resolveAutoCrop replaces the "__autocrop__" sentinel in a vfFilters slice with
// the actual crop filter detected from the specific file. If detection fails the
// sentinel is silently dropped so the rest of the filter chain still runs.
func (app *DownloaderApp) resolveAutoCrop(ctx context.Context, ffmpegPath, inputPath string, filters []string) []string {
	hasSentinel := false
	for _, filter := range filters {
		if filter == "__autocrop__" {
			hasSentinel = true
			break
		}
	}
	if !hasSentinel {
		return filters
	}
	cropFilter := app.detectCropFilter(ctx, ffmpegPath, inputPath)
	var resolved []string
	for _, filter := range filters {
		if filter == "__autocrop__" {
			if cropFilter != "" {
				resolved = append(resolved, cropFilter)
			}
		} else {
			resolved = append(resolved, filter)
		}
	}
	return resolved
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
	finalPath   string   // destination after FFmpeg succeeds; may differ from inputPath (e.g. .webm → .mkv)
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
		} else {
			// Resolve the __autocrop__ sentinel to an actual crop filter for this specific file.
			activeVF = app.resolveAutoCrop(ctx, ffmpegPath, inputPath, activeVF)
		}
		if len(activeVF) == 0 && len(afFilters) == 0 {
			continue
		}

		tmpOutput := strings.TrimSuffix(inputPath, ext) + "_pp" + ext
		finalPath := inputPath
		encodeMode := "Stream copy"
		if len(activeVF) > 0 {
			if ext == ".webm" {
				encodeMode = "Re-encode (libvpx-vp9, CRF 31)"
			} else {
				encodeMode = "Re-encode (libx264, CRF 18, slower)"
			}
		}
		jobs = append(jobs, ppJob{
			inputPath:  inputPath,
			tmpOutput:  tmpOutput,
			finalPath:  finalPath,
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
	// Note: -stats_period was added in FFmpeg 4.4; omitting it keeps progress
	// reporting working on older builds (FFmpeg defaults to 0.5 s anyway).
	args := []string{"-y", "-threads", "0", "-i", inputPath}
	if len(vfFilters) > 0 {
		args = append(args, "-vf", strings.Join(vfFilters, ","))
		// Choose encoder based on output container.
		// libx264 cannot be muxed into WebM; use libvpx-vp9 instead.
		// VP9 CRF 31 with -b:v 0 (constant-quality mode) is roughly equivalent
		// in perceived quality to H.264 CRF 18.
		if strings.ToLower(filepath.Ext(tmpOutput)) == ".webm" {
			args = append(args, "-c:v", "libvpx-vp9", "-crf", "31", "-b:v", "0", "-deadline", "good", "-cpu-used", "2")
		} else {
			// CRF 18 is visually near-lossless for H.264. The slower preset
			// squeezes more quality out at the same CRF.
			args = append(args, "-c:v", "libx264", "-crf", "18", "-preset", "slower")
		}
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
	for argIdx, arg := range args {
		if arg == "-threads" && argIdx+1 < len(args) {
			args[argIdx+1] = count
			return args
		}
	}
	return args
}

// runFFmpegJob executes a single ppJob and streams FFmpeg's stderr to the UI
// in real-time. Progress stats (frame, fps, speed) update the status bar;
// all other lines are collected for error reporting if the job fails.
func (app *DownloaderApp) runFFmpegJob(ctx context.Context, ffmpegPath string, job ppJob) {
	app.appendOutput(
		fmt.Sprintf("[SYSTEM] Post-processing: %s", filepath.Base(job.inputPath)),
		color.RGBA{R: 0, G: 255, B: 255, A: 255},
	)

	// Warn when re-encoding WebM: VP9 is significantly slower than H.264.
	// Suggest MKV as a faster alternative.
	if strings.ToLower(filepath.Ext(job.inputPath)) == ".webm" && strings.Contains(job.encodeMode, "libvpx-vp9") {
		app.appendOutput(
			"[SYSTEM] ⚠ WebM re-encodes use VP9 which is much slower than H.264. Consider downloading as MKV for faster post-processing.",
			color.RGBA{R: 255, G: 200, B: 0, A: 255},
		)
	}

	// Capture input file size before processing.
	var sizeBefore int64
	if info, err := os.Stat(job.inputPath); err == nil {
		sizeBefore = info.Size()
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, ffmpegPath, job.ffmpegArgs...)
	hideWindow(cmd)

	stderrPipe, pipeErr := cmd.StderrPipe()
	if pipeErr != nil {
		// Fallback: run without streaming.
		out, err := cmd.CombinedOutput()
		if err != nil {
			app.appendOutput(fmt.Sprintf("[ERROR] Post-processing failed: %v", err), color.RGBA{R: 255, A: 255})
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if line != "" {
					app.appendOutput(line, color.RGBA{R: 180, G: 180, B: 180, A: 255})
				}
			}
			os.Remove(job.tmpOutput)
		}
		return
	}

	if err := cmd.Start(); err != nil {
		app.appendOutput(fmt.Sprintf("[ERROR] Could not start FFmpeg: %v", err), color.RGBA{R: 255, A: 255})
		return
	}

	// Stream stderr: progress lines update the status bar and are discarded;
	// all other lines are printed to the log live and kept in errLines so the
	// error path can reference them if needed.
	var errLines []string
	scanner := bufio.NewScanner(stderrPipe)
	scanner.Split(scanCRLF)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "frame=") {
			app.ui.status.SetText("Post-Processing: " + formatFFmpegProgress(line))
		} else {
			errLines = append(errLines, line)
			app.appendOutput(line, color.RGBA{R: 160, G: 160, B: 160, A: 255})
		}
	}

	err := cmd.Wait()
	duration := time.Since(start)

	// Restore status bar regardless of outcome.
	app.ui.status.SetText("Status: Idle")

	if err != nil {
		atomic.StoreInt32(&app.ppFailed, 1)
		app.appendOutput(
			fmt.Sprintf("[ERROR] Post-processing failed: %v", err),
			color.RGBA{R: 255, G: 0, B: 0, A: 255},
		)
		os.Remove(job.tmpOutput)
		return
	}

	// Capture output size before the rename so we can compute the delta.
	var sizeAfter int64
	if info, err := os.Stat(job.tmpOutput); err == nil {
		sizeAfter = info.Size()
	}

	os.Remove(job.inputPath)
	os.Rename(job.tmpOutput, job.finalPath)

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
	for _, vfFilter := range job.vfFilters {
		filterNames = append(filterNames, filterShortName(vfFilter))
	}
	for _, afFilter := range job.afFilters {
		filterNames = append(filterNames, filterShortName(afFilter))
	}

	successColor := color.RGBA{R: 0, G: 200, B: 0, A: 255}
	app.appendOutput("────────────────────────────────────────", color.RGBA{R: 0, G: 160, B: 0, A: 255})
	app.appendOutput(fmt.Sprintf("POST-PROCESSING COMPLETE: %s", filepath.Base(job.finalPath)), successColor)
	app.appendOutput(fmt.Sprintf("   ├─ Duration:  %s", formatDuration(duration)), successColor)
	app.appendOutput(fmt.Sprintf("   ├─ File size: %s", sizeDelta), successColor)
	app.appendOutput(fmt.Sprintf("   ├─ Encoder:   %s", job.encodeMode), successColor)
	app.appendOutput(fmt.Sprintf("   ├─ Threads:   %d", job.threads), successColor)
	app.appendOutput(fmt.Sprintf("   └─ Filters:   %s", strings.Join(filterNames, ", ")), successColor)
	app.appendOutput("────────────────────────────────────────", color.RGBA{R: 0, G: 160, B: 0, A: 255})
}

// formatFFmpegProgress parses a FFmpeg stats line ("frame=X fps=X ... time=HH:MM:SS speed=Xx")
// and returns a compact human-readable string for the status bar.
func formatFFmpegProgress(line string) string {
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
	if val := get("frame"); val != "" { parts = append(parts, "frame "+val) }
	if val := get("fps"); val != "" && val != "0" { parts = append(parts, val+" fps") }
	if val := get("time"); val != "" { parts = append(parts, "time "+val) }
	if val := get("speed"); val != "" { parts = append(parts, "speed "+val) }
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
			cost += 30
		case "Balanced":
			cost += 55
		default: // "Precise (slow)"
			cost += 70
		}
	}
	if ui.denoise.Checked {
		switch ui.denoiseMode.Selected {
		case "NLMeans (HQ, slow)":
			cost += 40
		default: // hqdn3d (Balanced)
			cost += 20
		}
	}
	if ui.hdrToSdr.Checked     { cost += 25 }
	if ui.upscaleVideo.Checked {
		switch ui.upscaleTarget.Selected {
		case "4K (2160p)":
			cost += 35
		default:
			cost += 20
		}
	}
	if ui.stabilize.Checked    { cost += 20 }
	if ui.autoCrop.Checked     { cost += 15 }
	if ui.deinterlace.Checked  { cost += 12 }
	if ui.sharpen.Checked      { cost += 10 }
	if ui.deband.Checked       { cost += 8  }
	if ui.vividMode.Checked    { cost += 5  }
	if ui.normalizeAudio.Checked { cost += 5 }
	if ui.nightMode.Checked      { cost += 5 }

	switch {
	case cost == 0:
		return 0, "No post-processing active"
	case cost < 20:
		return cost, "Light — minimal overhead"
	case cost < 50:
		return cost, "Moderate — noticeable extra time"
	case cost < 80:
		return cost, "Heavy — significant re-encode time"
	case cost < 120:
		return cost, "Very Heavy — expect long processing"
	default:
		return cost, "Intensive — expect very long processing"
	}
}
