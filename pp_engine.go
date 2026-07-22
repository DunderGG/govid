// pp_engine.go — FFmpeg post-processing engine.
//
// Responsibilities:
//   - PPEngine: typed component holding FFmpeg tool paths, with methods for
//     crop detection, filter resolution, and concurrent post-processing jobs.
//   - Probe methods: probeFrameCount, probeDuration, computeOutputFrameCount,
//     parseRationalFPS — ffprobe wrappers and FPS/duration maths.
//   - Argument builders: buildFFmpegArgs, patchThreadCount — pure helpers that
//     construct and patch the FFmpeg command-line for each post-processing job.
//   - PPCallbacks: bridge that lets the engine report events to the UI layer.
package main

import (
	"bufio"
	"context"
	"fmt"
	"image/color"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PPEngine holds the resolved paths to the FFmpeg tools it drives.
// Construct one with NewPPEngine and call ApplyFilters to post-process files.
type PPEngine struct {
	FFmpegPath  string // absolute path to ffmpeg binary
	FFprobePath string // absolute path to ffprobe binary
}

// NewPPEngine returns a PPEngine configured with the given binary paths.
func NewPPEngine(ffmpegPath, ffprobePath string) *PPEngine {
	return &PPEngine{
		FFmpegPath:  ffmpegPath,
		FFprobePath: ffprobePath,
	}
}

// PPCallbacks lets PPEngine report events back to the UI layer.
type PPCallbacks struct {
	// OnLog is called for every message the engine wants to show in the log view.
	OnLog func(line string, col color.Color)
	// OnStatus is called to update the short status label.
	OnStatus func(msg string)
	// OnFailure is called when a post-processing job fails, e.g. to mark the
	// retry button and surface the failure state in the parent session.
	OnFailure func()
}

// detectCropFilter runs a quick FFmpeg cropdetect pass on the first 60 seconds
// of the file and returns a "crop=W:H:X:Y" filter string, or "" if no bars were
// found or detection failed.
func (engine *PPEngine) detectCropFilter(ctx context.Context, inputPath string, cb PPCallbacks) string {
	// Run FFmpeg with cropdetect on the first 60 seconds of the input file.
	cmd := exec.CommandContext(ctx, engine.FFmpegPath,
		"-t", "60", "-i", inputPath,
		"-vf", "cropdetect=limit=24:round=16:reset=0",
		"-f", "null", "-",
	)

	// Hide the FFmpeg console window on Windows to avoid flashing a black box.
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		cb.OnLog(
			fmt.Sprintf("[SYSTEM] Auto-Crop: cropdetect failed, skipping (%v)", err),
			colWarning,
		)
		return ""
	}

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
		cb.OnLog("[SYSTEM] Auto-Crop: no black bars detected, skipping.", colSystem)
	} else {
		cb.OnLog(fmt.Sprintf("[SYSTEM] Auto-Crop: detected %s", lastCrop), colSystem)
	}
	return lastCrop
}

// resolveAutoCrop replaces the "__autocrop__" sentinel in a vfFilters slice with
// the actual crop filter detected from the specific file. If detection fails the
// sentinel is silently dropped so the rest of the filter chain still runs.
func (engine *PPEngine) resolveAutoCrop(ctx context.Context, inputPath string, filters []string, cb PPCallbacks) []string {
	hasSentinel := false
	for _, filter := range filters {
		if filter == "__autocrop__" {
			hasSentinel = true
			break
		}
	}

	// If no sentinel is present, return the original filters unchanged.
	if !hasSentinel {
		return filters
	}

	// Run cropdetect on the file to get the actual crop filter.
	cropFilter := engine.detectCropFilter(ctx, inputPath, cb)
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

// runJob executes a single PostProcessJob and streams FFmpeg's stderr to the UI
// in real-time. Progress stats update the status bar; all other lines are
// forwarded to the log. The original file is replaced only if FFmpeg succeeds.
func (engine *PPEngine) runJob(ctx context.Context, job PostProcessJob, cb PPCallbacks) {
	cb.OnLog(
		fmt.Sprintf("[SYSTEM] Post-processing: %s", filepath.Base(job.inputPath)),
		colSystem,
	)

	// Warn when re-encoding WebM: VP9 is significantly slower than H.264.
	if strings.ToLower(filepath.Ext(job.inputPath)) == ".webm" && strings.Contains(job.encodeMode, "libvpx-vp9") {
		cb.OnLog(
			"[SYSTEM] ⚠ WebM re-encodes use VP9 which is much slower than H.264. Consider downloading as MKV for faster post-processing.",
			colCaution,
		)
	}

	// sizeBefore is used to compute the delta in file size after post-processing.
	var sizeBefore int64
	if info, err := os.Stat(job.inputPath); err == nil {
		sizeBefore = info.Size()
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, engine.FFmpegPath, job.ffmpegArgs...)
	hideWindow(cmd)

	stderrPipe, pipeErr := cmd.StderrPipe()
	if pipeErr != nil {
		// Fallback: run without streaming.
		out, err := cmd.CombinedOutput()

		// If FFmpeg fails, log the error and the captured output.
		if err != nil {
			cb.OnLog(fmt.Sprintf("[ERROR] Post-processing failed: %v", err), colError)
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if line != "" {
					cb.OnLog(line, colDebug)
				}
			}
			if removeErr := os.Remove(job.tmpOutput); removeErr != nil {
				cb.OnLog(
					fmt.Sprintf("[SYSTEM] Warning: could not remove temp file: %v", removeErr),
					colWarning,
				)
			}
			return
		}

		// If FFmpeg succeeded, still need to promote the temp file to its final name.
		if renameErr := os.Rename(job.tmpOutput, job.finalPath); renameErr != nil {
			cb.OnLog(
				fmt.Sprintf("[SYSTEM] Failed to rename output file: %v", renameErr),
				colErrorSoft,
			)
			cb.OnFailure()
		}
		return
	}

	if err := cmd.Start(); err != nil {
		cb.OnLog(fmt.Sprintf("[ERROR] Could not start FFmpeg: %v", err), colError)
		return
	}

	// Stream FFmpeg's stderr in real-time to the log and status bar.
	var errLines []string
	scanner := bufio.NewScanner(stderrPipe)
	scanner.Split(scanCRLF)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "frame=") {
			cb.OnStatus("Post-Processing: " + formatFFmpegProgress(line, job.totalFrames))
		} else {
			errLines = append(errLines, line)
			cb.OnLog(line, colVerbose)
		}
	}
	if err := scanner.Err(); err != nil {
		cb.OnLog(fmt.Sprintf("[SYSTEM] FFmpeg output read error: %v", err), colWarning)
	}

	err := cmd.Wait()
	duration := time.Since(start)

	if err != nil {
		cb.OnFailure()
		cb.OnLog(
			fmt.Sprintf("[ERROR] Post-processing failed: %v", err),
			colError,
		)
		if removeErr := os.Remove(job.tmpOutput); removeErr != nil {
			cb.OnLog(
				fmt.Sprintf("[SYSTEM] Warning: could not remove temp file: %v", removeErr),
				colWarning,
			)
		}
		return
	}

	// sizeAfter is used to compute the delta in file size after post-processing.
	var sizeAfter int64
	if info, err := os.Stat(job.tmpOutput); err == nil {
		sizeAfter = info.Size()
	}

	// Promote the temp file to its final name, replacing the original.
	if err := os.Rename(job.tmpOutput, job.finalPath); err != nil {
		cb.OnLog(
			fmt.Sprintf("[SYSTEM] Failed to rename output file: %v", err),
			colErrorSoft,
		)
		cb.OnFailure()
		return
	}

	sizeDelta := ""
	if sizeBefore > 0 && sizeAfter > 0 {
		deltaPct := (float64(sizeAfter) - float64(sizeBefore)) / float64(sizeBefore) * 100
		sign := "+"
		if deltaPct < 0 {
			sign = ""
		}
		sizeDelta = fmt.Sprintf("%s → %s (%s%.1f%%)",
			formatBytes(sizeBefore), formatBytes(sizeAfter), sign, deltaPct)
	}

	var filterNames []string
	for _, vfFilter := range job.vfFilters {
		filterNames = append(filterNames, filterShortName(vfFilter))
	}
	for _, afFilter := range job.afFilters {
		filterNames = append(filterNames, filterShortName(afFilter))
	}

	successColor := colSuccess
	cb.OnLog("────────────────────────────────────────", colPPBorder)
	cb.OnLog(fmt.Sprintf("POST-PROCESSING COMPLETE: %s", filepath.Base(job.finalPath)), successColor)
	cb.OnLog(fmt.Sprintf("   ├─ Duration:   %s", formatDuration(duration)), successColor)
	cb.OnLog(fmt.Sprintf("   ├─ Size Delta: %s", sizeDelta), successColor)
	cb.OnLog(fmt.Sprintf("   ├─ Encoder:    %s", job.encodeMode), successColor)
	cb.OnLog(fmt.Sprintf("   ├─ Threads:    %d", job.threads), successColor)
	cb.OnLog(fmt.Sprintf("   └─ Filters:    %s", strings.Join(filterNames, ", ")), successColor)
	cb.OnLog("────────────────────────────────────────", colPPBorder)
}

// ── Probe helpers ────────────────────────────────────────────────────────────

// probeFrameCount uses ffprobe to count the exact number of video packets in
// the file index. For MP4 this reads the moov atom (instant); for MKV/WebM it
// reads the cue points. Neither approach decodes any video data.
// Both nb_frames metadata and avg_frame_rate×duration are unreliable for VFR
// content — muxers often write nb_frames from declared fps×duration rather
// than actual packet count, causing estimates to be 2–3× too high.
func (engine *PPEngine) probeFrameCount(ctx context.Context, inputPath string) int64 {
	cmd := exec.CommandContext(ctx, engine.FFprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-count_packets",
		"-show_entries", "stream=nb_read_packets",
		"-of", "csv=p=0",
		inputPath,
	)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err == nil && len(out) > 0 {
		if n, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	// Fallback: duration × avg_frame_rate (less reliable but always available).
	cmd2 := exec.CommandContext(ctx, engine.FFprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=duration,avg_frame_rate",
		"-of", "csv=p=0",
		inputPath,
	)
	hideWindow(cmd2)
	out2, err := cmd2.Output()
	if err != nil {
		return 0
	}
	fields := strings.SplitN(strings.TrimSpace(string(out2)), ",", 2)
	if len(fields) == 2 {
		dur, dErr := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
		fps := engine.parseRationalFPS(strings.TrimSpace(fields[1]))
		if dErr == nil && dur > 0 && fps > 0 {
			return int64(math.Round(dur * fps))
		}
	}
	return 0
}

// computeOutputFrameCount adjusts the probed input frame count to account for
// filters that change the output frame rate or total frame count.
//   - minterpolate=fps=N: outputs at a fixed target fps → duration × N
//   - bwdif: send_field mode (default) outputs one frame per field → inputFrames × 2
func (engine *PPEngine) computeOutputFrameCount(ctx context.Context, inputPath string, inputFrames int64, vfFilters []string) int64 {
	// minterpolate takes priority — its target fps determines the final count.
	for _, f := range vfFilters {
		if strings.HasPrefix(f, "minterpolate=fps=") {
			rest := strings.TrimPrefix(f, "minterpolate=fps=")
			if i := strings.IndexAny(rest, ":,"); i != -1 {
				rest = rest[:i]
			}
			if targetFps, err := strconv.ParseFloat(rest, 64); err == nil && targetFps > 0 {
				if dur := engine.probeDuration(ctx, inputPath); dur > 0 {
					return int64(math.Round(dur * targetFps))
				}
			}
			return inputFrames
		}
	}
	// bwdif send_field (default) outputs one frame per interlaced field.
	for _, f := range vfFilters {
		if f == "bwdif" {
			return inputFrames * 2
		}
	}
	return inputFrames
}

// probeDuration returns the container duration of the file in seconds.
func (engine *PPEngine) probeDuration(ctx context.Context, inputPath string) float64 {
	cmd := exec.CommandContext(ctx, engine.FFprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		inputPath,
	)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	dur, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || dur <= 0 {
		return 0
	}
	return dur
}

// parseRationalFPS parses a "num/den" rational string (e.g. "30/1", "30000/1001")
// as returned by ffprobe and returns the floating-point FPS value.
func (engine *PPEngine) parseRationalFPS(s string) float64 {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
		return 0
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0
	}
	return num / den
}

// ── Argument builders ────────────────────────────────────────────────────────

// buildFFmpegArgs constructs the FFmpeg argument list for a single post-processing
// job. The -threads placeholder is set to "0" and patched to the real count later.
func (engine *PPEngine) buildFFmpegArgs(inputPath, tmpOutput string, vfFilters, afFilters []string) []string {
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
func (engine *PPEngine) patchThreadCount(args []string, count string) []string {
	for argIdx, arg := range args {
		if arg == "-threads" && argIdx+1 < len(args) {
			args[argIdx+1] = count
			return args
		}
	}
	return args
}

// ApplyFilters runs a concurrent worker pool to post-process each of the given
// files with the provided video/audio filters. Workers are bounded to
// runtime.NumCPU() so that total thread load never exceeds available cores.
// Each worker receives an evenly divided thread budget so concurrent FFmpeg
// processes do not compete for CPU. Audio-only files skip video filters.
func (engine *PPEngine) ApplyFilters(ctx context.Context, filePaths, vfFilters, afFilters []string, cb PPCallbacks) {
	if len(filePaths) == 0 || (len(vfFilters) == 0 && len(afFilters) == 0) {
		return
	}

	// Build one job per file, skipping files that need no processing.
	var jobs []PostProcessJob
	for _, inputPath := range filePaths {
		ext := strings.ToLower(filepath.Ext(inputPath))
		isAudioOnly := ext == ".mp3" || ext == ".m4a"

		activeVF := vfFilters
		if isAudioOnly {
			activeVF = nil // video filters do not apply to audio-only files
		} else {
			activeVF = engine.resolveAutoCrop(ctx, inputPath, activeVF, cb)
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
		jobs = append(jobs, PostProcessJob{
			inputPath:   inputPath,
			tmpOutput:   tmpOutput,
			finalPath:   finalPath,
			ffmpegArgs:  engine.buildFFmpegArgs(inputPath, tmpOutput, activeVF, afFilters),
			vfFilters:   activeVF,
			afFilters:   afFilters,
			encodeMode:  encodeMode,
			totalFrames: engine.computeOutputFrameCount(ctx, inputPath, engine.probeFrameCount(ctx, inputPath), activeVF),
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
	cb.OnLog(
		fmt.Sprintf("[SYSTEM] Starting post-processing (%s)", strings.Join(filterSummary, " | ")),
		colSystem,
	)

	// Cap workers at the number of logical CPU cores and at the number of jobs.
	numWorkers := runtime.NumCPU()
	if numWorkers > len(jobs) {
		numWorkers = len(jobs)
	}

	// Divide available cores evenly so concurrent FFmpeg processes do not
	// fight each other for threads. Minimum 1 thread per process.
	threadsPerJob := runtime.NumCPU() / numWorkers
	if threadsPerJob < 1 {
		threadsPerJob = 1
	}

	// Assign the thread budget to each job and patch each job's FFmpeg thread arg accordingly.
	for i := range jobs {
		jobs[i].threads = threadsPerJob
		jobs[i].ffmpegArgs = engine.patchThreadCount(jobs[i].ffmpegArgs, fmt.Sprintf("%d", threadsPerJob))
	}

	// Create a channel to distribute jobs to workers and close it after all jobs are sent.
	jobCh := make(chan PostProcessJob, len(jobs))
	for _, job := range jobs {
		jobCh <- job
	}
	close(jobCh)

	// Launch a worker pool to process jobs concurrently. Each worker reads from the job channel
	// until it is closed, then exits. The WaitGroup ensures we wait for all workers to finish.
	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobCh {
				engine.runJob(ctx, job, cb)
			}
		}()
	}
	wg.Wait()
}
