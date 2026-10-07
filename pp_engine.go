// pp_engine.go — FFmpeg post-processing engine.
//
// Responsibilities:
//   - PPEngine: typed component holding FFmpeg tool paths, with methods for
//     crop detection, filter resolution, and concurrent post-processing jobs.
//   - Probe methods: probeFrameCount, probeDuration, probeColorInfo,
//     computeOutputFrameCount, parseRationalFPS — ffprobe wrappers (with an
//     ffmpeg fallback for colour information) and FPS/duration maths.
//   - Filter resolution: resolveAutoCrop and resolveToneMap replace the
//     per-file sentinels in the session's filter chain.
//   - Argument builders: buildFFmpegArgs, buildFFmpegArgsForBackend — pure
//     helpers that construct the FFmpeg command-line for each post-processing job.
//   - PostProcessJob: the inputs for one file's FFmpeg pass.
//   - PPCallbacks: bridge that lets the engine report events to the UI layer.
package main

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
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

	// GPUBackend and GPUCapabilities are set by applyFFmpegFilters from the
	// user's GPU Acceleration setting and GPUCapabilityService.Detect. Left at
	// their zero values, PlanEncoder falls back to the CPU encoder.
	GPUBackend      GPUBackend
	GPUCapabilities map[GPUBackend]BackendCapability

	// gpuSem caps how many GPU-encoded jobs run concurrently, since consumer
	// GPUs (NVENC in particular) enforce a low concurrent hardware encoder
	// session limit; exceeding it fails jobs that would otherwise succeed.
	gpuSem chan struct{}
}

// maxConcurrentGPUJobs bounds simultaneous hardware encoder sessions across
// a single ApplyFilters batch, independent of the CPU worker pool size.
const maxConcurrentGPUJobs = 2

// gpuStallTimeout is how long a GPU-encoded job may produce no FFmpeg output
// before it is treated as a hung driver, killed, and retried on the CPU.
const gpuStallTimeout = 30 * time.Second

// NewPPEngine returns a PPEngine configured with the given binary paths.
func NewPPEngine(ffmpegPath, ffprobePath string) *PPEngine {
	return &PPEngine{
		FFmpegPath:  ffmpegPath,
		FFprobePath: ffprobePath,
		gpuSem:      make(chan struct{}, maxConcurrentGPUJobs),
	}
}

// gpuJobGuard owns the resource lifecycle a GPU-encoded job needs on top of a
// plain ffmpeg run: a slot in engine.gpuSem and a stall watchdog. It is a
// no-op for CPU jobs so runJob can use it unconditionally.
type gpuJobGuard struct {
	engine   *PPEngine
	active   bool // true if this job uses the GPU semaphore/watchdog
	acquired bool // true once the semaphore slot has been taken
	released bool // idempotency guard for release()
	watchdog *time.Timer
}

// newGPUJobGuard acquires a gpuSem slot when usedGPU is true, blocking until
// one is free.
func newGPUJobGuard(engine *PPEngine, usedGPU bool) *gpuJobGuard {
	guard := &gpuJobGuard{engine: engine, active: usedGPU}
	if usedGPU {
		engine.gpuSem <- struct{}{}
		guard.acquired = true
	}
	return guard
}

// arm starts the stall watchdog, calling onStall if no output is seen within
// gpuStallTimeout. No-op for non-GPU jobs.
func (guard *gpuJobGuard) arm(onStall func()) {
	if !guard.active {
		return
	}
	guard.watchdog = time.AfterFunc(gpuStallTimeout, onStall)
}

// pet resets the stall watchdog; call on every line of ffmpeg output received.
func (guard *gpuJobGuard) pet() {
	if guard.watchdog != nil {
		guard.watchdog.Reset(gpuStallTimeout)
	}
}

// release stops the watchdog and frees the gpuSem slot. Safe to call more
// than once — only the first call has any effect.
func (guard *gpuJobGuard) release() {
	if guard.released {
		return
	}
	guard.released = true
	if guard.watchdog != nil {
		guard.watchdog.Stop()
	}
	if guard.acquired {
		<-guard.engine.gpuSem
	}
}

// PostProcessJob holds the inputs for a single file's FFmpeg post-processing pass.
type PostProcessJob struct {
	inputPath   string
	tmpOutput   string
	finalPath   string // destination after FFmpeg succeeds; may differ from inputPath (e.g. .webm → .mkv)
	ffmpegArgs  []string
	vfFilters   []string      // active video filters, for summary logging
	afFilters   []string      // active audio filters, for summary logging
	threads     int           // thread count assigned to this job
	encodeMode  string        // human-readable encode strategy, for summary logging
	totalFrames int64         // total video frames, for progress percentage (0 = unknown)
	usedGPU     bool          // true if ffmpegArgs uses a GPU encoder; enables one CPU retry on failure
	layout      *streamLayout // the input's streams, for explicit mapping; nil to let ffmpeg pick
}

// PPCallbacks lets PPEngine report events back to the UI layer. Every field
// must be set.
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
	cmd := newToolCommand(ctx, engine.FFmpegPath,
		"-t", "60", "-i", inputPath,
		"-vf", "cropdetect=limit=24:round=16:reset=0",
		"-f", "null", "-",
	)

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

// resolveAutoCrop replaces autoCropSentinel in a vfFilters slice with the
// actual crop filter detected from the specific file. If detection fails the
// sentinel is silently dropped so the rest of the filter chain still runs.
func (engine *PPEngine) resolveAutoCrop(ctx context.Context, inputPath string, filters []string, cb PPCallbacks) []string {
	// If no sentinel is present, return the original filters unchanged.
	if !slices.Contains(filters, autoCropSentinel) {
		return filters
	}

	// Run cropdetect on the file to get the actual crop filter.
	cropFilter := engine.detectCropFilter(ctx, inputPath, cb)
	return replaceSentinel(filters, autoCropSentinel, cropFilter)
}

// replaceSentinel returns filters with every sentinel replaced by
// replacement, or removed when replacement is "".
func replaceSentinel(filters []string, sentinel, replacement string) []string {
	var resolved []string
	for _, filter := range filters {
		switch {
		case filter != sentinel:
			resolved = append(resolved, filter)
		case replacement != "":
			resolved = append(resolved, replacement)
		}
	}
	return resolved
}

// resolveToneMap replaces toneMapSentinel in a vfFilters slice with the
// HDR-to-SDR chain for this file's transfer function. SDR files, and files
// whose colour information cannot be read, keep the rest of the chain but
// are not tone mapped, since tone mapping an SDR picture washes it out.
func (engine *PPEngine) resolveToneMap(ctx context.Context, inputPath string, filters []string, cb PPCallbacks) []string {
	if !slices.Contains(filters, toneMapSentinel) {
		return filters
	}

	info, err := engine.probeColorInfo(ctx, inputPath)
	if err != nil {
		cb.OnLog(fmt.Sprintf("[SYSTEM] HDR to SDR: could not read the colour information (%v), skipping tone mapping.", err), colWarning)
		return replaceSentinel(filters, toneMapSentinel, "")
	}
	transfer, assumed := info.hdrTransfer()
	toneMap := toneMapFilter(transfer)
	switch {
	case toneMap == "":
		cb.OnLog(fmt.Sprintf("[SYSTEM] HDR to SDR: source is SDR (transfer: %s), skipping tone mapping.", info.describeTransfer()), colSystem)
	case assumed:
		cb.OnLog("[SYSTEM] HDR to SDR: BT.2020 source without a transfer tag, assuming HDR10 (PQ); tone mapping to BT.709.", colSystem)
	default:
		cb.OnLog(fmt.Sprintf("[SYSTEM] HDR to SDR: %s source, tone mapping to BT.709.", info.describeTransfer()), colSystem)
	}
	return replaceSentinel(filters, toneMapSentinel, toneMap)
}

// retryWithCPU rebuilds job's ffmpeg args using the CPU encoder and runs it
// once more. Used when a GPU-accelerated encode fails at runtime despite
// passing the earlier capability probe (docs/gpu-acceleration.md §8 — strict
// fallback behavior).
func (engine *PPEngine) retryWithCPU(ctx context.Context, job PostProcessJob, cb PPCallbacks, reason string) {
	cb.OnLog(fmt.Sprintf("[SYSTEM] GPU encode failed (%s) — retrying with CPU.", reason), colWarning)

	job.ffmpegArgs = engine.buildFFmpegArgsForBackend(job, BackendOff)
	job.encodeMode = PlanEncoder(BackendOff, nil, filepath.Ext(job.tmpOutput)).Label
	job.usedGPU = false
	engine.runJob(ctx, job, cb)
}

// failJob reports a terminal FFmpeg failure for job: it marks the session as
// failed via OnFailure, logs msg and any captured output, and removes the
// partial temp file. Every path that gives up on a job must go through here
// so the Retry button and the log stay consistent.
func failJob(job PostProcessJob, cb PPCallbacks, msg string, output []string) {
	cb.OnFailure()
	cb.OnLog("[ERROR] "+msg, colError)
	for _, line := range output {
		if line = strings.TrimSpace(line); line != "" {
			cb.OnLog(line, colDebug)
		}
	}
	if removeErr := os.Remove(job.tmpOutput); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		cb.OnLog(
			fmt.Sprintf("[SYSTEM] Warning: could not remove temp file: %v", removeErr),
			colWarning,
		)
	}
}

// runJob executes a single PostProcessJob and streams FFmpeg's stderr to the UI
// in real-time. Progress stats update the status bar; all other lines are
// forwarded to the log. The original file is replaced only if FFmpeg succeeds.
//
// Phases: log/warn -> size-before -> acquire gpuJobGuard -> exec+stream ->
// wait (retry on CPU if a GPU job failed) -> rename+report.
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

	// guard releases its gpuSem slot explicitly before every CPU retry so the
	// slot frees up immediately instead of staying held for the retry's
	// duration; release() is idempotent so the deferred call on every other
	// exit path is a safe no-op if already released.
	guard := newGPUJobGuard(engine, job.usedGPU)
	defer guard.release()

	start := time.Now()
	cmd := newToolCommand(ctx, engine.FFmpegPath, job.ffmpegArgs...)

	stderrPipe, pipeErr := cmd.StderrPipe()
	if pipeErr != nil {
		// Fallback: run without streaming.
		out, err := cmd.CombinedOutput()

		// If FFmpeg fails, log the error and the captured output.
		if err != nil {
			if job.usedGPU {
				guard.release()
				engine.retryWithCPU(ctx, job, cb, lastLine(string(out)))
				return
			}
			failJob(job, cb, fmt.Sprintf("Post-processing failed: %v", err), strings.Split(string(out), "\n"))
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
		if job.usedGPU {
			guard.release()
			engine.retryWithCPU(ctx, job, cb, err.Error())
			return
		}
		failJob(job, cb, fmt.Sprintf("Could not start FFmpeg: %v", err), nil)
		return
	}

	// GPU jobs get a stall watchdog: if a hung driver stops producing any
	// output, kill the process so the existing CPU retry can take over
	// instead of the batch hanging indefinitely.
	guard.arm(func() {
		cb.OnLog(
			fmt.Sprintf("[SYSTEM] GPU encode produced no output for %s, assuming a hung driver; terminating.", gpuStallTimeout),
			colWarning,
		)
		_ = cmd.Process.Kill()
	})

	toolDone := trackTool(toolFFmpeg)
	defer toolDone()
	markLoop("ffmpeg progress reader ("+filepath.Base(job.finalPath)+")", "started")
	// Stream FFmpeg's stderr in real-time to the log and status bar.
	var errLines []string
	scanner := bufio.NewScanner(stderrPipe)
	scanner.Split(scanCRLF)
	for scanner.Scan() {
		guard.pet()
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
	markLoop("ffmpeg progress reader ("+filepath.Base(job.finalPath)+")", "stopped")
	duration := time.Since(start)

	if err != nil {
		if job.usedGPU {
			reason := err.Error()
			if len(errLines) > 0 {
				reason = errLines[len(errLines)-1]
			}
			guard.release()
			engine.retryWithCPU(ctx, job, cb, reason)
			return
		}
		// The output was already streamed to the log line by line.
		failJob(job, cb, fmt.Sprintf("Post-processing failed: %v", err), nil)
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
	cmd := newToolCommand(ctx, engine.FFprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-count_packets",
		"-show_entries", "stream=nb_read_packets",
		"-of", "csv=p=0",
		inputPath,
	)
	out, err := cmd.Output()
	if err == nil && len(out) > 0 {
		if n, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	// Fallback: duration × avg_frame_rate (less reliable but always available).
	cmd2 := newToolCommand(ctx, engine.FFprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=duration,avg_frame_rate",
		"-of", "csv=p=0",
		inputPath,
	)
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

// colorInfo is the colour description of a file's first video stream, as
// ffprobe names it (e.g. "smpte2084", "bt2020", "bt2020nc"). Empty fields
// are unknown.
type colorInfo struct {
	Transfer  string
	Primaries string
	Space     string
}

// hdrTransfer returns the HDR transfer function to tone map from, or "" for
// an SDR source. A source tagged with BT.2020 primaries or matrix but no
// transfer function is taken to be HDR10 (PQ), and assumed is true: the VP9
// and AV1 bitstreams carry the matrix but not the transfer, so the transfer
// tag is the one that goes missing when yt-dlp merges streams, and YouTube
// serves BT.2020 video only for HDR.
func (info colorInfo) hdrTransfer() (transfer string, assumed bool) {
	switch {
	case info.Transfer == transferPQ || info.Transfer == transferHLG:
		return info.Transfer, false
	case info.Transfer == "" && (info.Primaries == "bt2020" || strings.HasPrefix(info.Space, "bt2020")):
		return transferPQ, true
	default:
		return "", false
	}
}

// describeTransfer names the transfer function for log messages.
func (info colorInfo) describeTransfer() string {
	switch info.Transfer {
	case transferPQ:
		return "HDR10 (PQ)"
	case transferHLG:
		return "HLG"
	case "":
		return "unknown"
	default:
		return info.Transfer
	}
}

// probeColorInfo reads the colour description of the file's first video
// stream with ffprobe. ffprobe is optional and not bundled, so when it cannot
// be run the same tags are read from ffmpeg's stream summary instead.
func (engine *PPEngine) probeColorInfo(ctx context.Context, inputPath string) (colorInfo, error) {
	out, err := newToolCommand(ctx, engine.FFprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=color_transfer,color_primaries,color_space",
		"-of", "default=noprint_wrappers=1",
		inputPath,
	).Output()
	if err == nil {
		return parseFFprobeColorInfo(string(out)), nil
	}

	// ffmpeg exits with an error when given no output file, but it has
	// printed the input's stream summary by then.
	out, _ = newToolCommand(ctx, engine.FFmpegPath, "-hide_banner", "-i", inputPath).CombinedOutput()
	info, ok := parseFFmpegColorInfo(string(out))
	if !ok {
		return colorInfo{}, errors.New("no video stream found")
	}
	return info, nil
}

// parseFFprobeColorInfo parses ffprobe's "key=value" output for the
// color_transfer, color_primaries, and color_space entries. ffprobe reports
// "unknown" for untagged streams, which is treated as empty.
func parseFFprobeColorInfo(out string) colorInfo {
	var info colorInfo
	for _, line := range strings.Split(out, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found || value == "unknown" {
			continue
		}
		switch key {
		case "color_transfer":
			info.Transfer = value
		case "color_primaries":
			info.Primaries = value
		case "color_space":
			info.Space = value
		}
	}
	return info
}

// ffmpegColorPattern matches the colour description in a video stream line
// of ffmpeg's input summary: "(tv, bt2020nc/bt2020/smpte2084" names the
// matrix, primaries, and transfer, and "(tv, bt709" names one value for all
// three.
var ffmpegColorPattern = regexp.MustCompile(`\((?:tv|pc), ([a-z0-9-]+)(?:/([a-z0-9-]+)/([a-z0-9-]+))?`)

// parseFFmpegColorInfo finds the first video stream in ffmpeg's input
// summary, e.g.
//
//	Stream #0:0: Video: vp9 (Profile 2), yuv420p10le(tv, bt2020nc/bt2020/smpte2084), 3840x2160
//
// and returns its colour description; untagged values ("unknown") are left
// empty. ok is false when the summary lists no video stream.
func parseFFmpegColorInfo(out string) (info colorInfo, ok bool) {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "Stream #") || !strings.Contains(line, "Video:") {
			continue
		}
		match := ffmpegColorPattern.FindStringSubmatch(line)
		switch {
		case match == nil:
		case match[2] == "" && isFieldOrder(match[1]):
			// "(tv, progressive)": no colour description at all.
		case match[2] == "":
			info = colorInfo{Space: match[1], Primaries: match[1], Transfer: match[1]}
		default:
			info = colorInfo{Space: match[1], Primaries: match[2], Transfer: match[3]}
		}
		return info.withoutUnknown(), true
	}
	return colorInfo{}, false
}

// isFieldOrder reports whether word starts the field order ffmpeg prints
// after the colour description ("progressive", "top first", "bottom first"),
// which takes its place when the stream has no colour tags.
func isFieldOrder(word string) bool {
	return word == "progressive" || word == "top" || word == "bottom"
}

// withoutUnknown clears the fields ffmpeg reports as "unknown".
func (info colorInfo) withoutUnknown() colorInfo {
	for _, field := range []*string{&info.Transfer, &info.Primaries, &info.Space} {
		if *field == "unknown" {
			*field = ""
		}
	}
	return info
}

// computeOutputFrameCount adjusts the probed input frame count to account for
// filters that change the output frame rate or total frame count.
//   - minterpolate=fps=N: outputs at a fixed target fps → duration × N
//   - bwdif: send_field mode (default) outputs one frame per field → inputFrames × 2
func (engine *PPEngine) computeOutputFrameCount(ctx context.Context, inputPath string, inputFrames int64, vfFilters []string) int64 {
	// minterpolate takes priority — its target fps determines the final count.
	for _, filter := range vfFilters {
		if strings.HasPrefix(filter, "minterpolate=fps=") {
			rest := strings.TrimPrefix(filter, "minterpolate=fps=")
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
	for _, filter := range vfFilters {
		if filter == "bwdif" {
			return inputFrames * 2
		}
	}
	return inputFrames
}

// probeDuration returns the container duration of the file in seconds.
func (engine *PPEngine) probeDuration(ctx context.Context, inputPath string) float64 {
	cmd := newToolCommand(ctx, engine.FFprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		inputPath,
	)
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

// buildFFmpegArgs constructs the FFmpeg argument list for a single
// post-processing job, using the engine's GPU backend and the job's share
// of the CPU threads.
func (engine *PPEngine) buildFFmpegArgs(job PostProcessJob) []string {
	return engine.buildFFmpegArgsForBackend(job, engine.GPUBackend)
}

// buildFFmpegArgsForBackend is buildFFmpegArgs with an explicit backend
// override, used by runJob's strict CPU fallback to rebuild CPU-only args
// when a GPU-accelerated job fails at runtime.
//
// When the job's stream layout is known, every stream is mapped explicitly
// (see streamLayout.mapArgs) and the video filters apply to the main video
// stream only, so cover art, subtitles, attachments, metadata, and chapters
// survive the re-encode. Without a layout, ffmpeg's default stream selection
// is used, which keeps one stream of each kind.
func (engine *PPEngine) buildFFmpegArgsForBackend(job PostProcessJob, backend GPUBackend) []string {
	// Note: -stats_period was added in FFmpeg 4.4; omitting it keeps progress
	// reporting working on older builds (FFmpeg defaults to 0.5 s anyway).
	args := []string{"-y", "-threads", strconv.Itoa(job.threads), "-i", job.inputPath}
	filterVideo := len(job.vfFilters) > 0
	if job.layout != nil {
		args = append(args, job.layout.mapArgs(filterVideo)...)
	}

	switch {
	case filterVideo && job.layout != nil:
		args = append(args, "-filter:v:0", strings.Join(job.vfFilters, ","))
		plan := PlanEncoder(backend, engine.GPUCapabilities, filepath.Ext(job.tmpOutput))
		args = append(args, encodeFirstVideoOnly(plan.Args)...)
		args = append(args, job.layout.coverArgs(true)...)
	case filterVideo:
		args = append(args, "-vf", strings.Join(job.vfFilters, ","))
		plan := PlanEncoder(backend, engine.GPUCapabilities, filepath.Ext(job.tmpOutput))
		args = append(args, plan.Args...)
	default:
		args = append(args, "-c:v", "copy")
		if job.layout != nil {
			args = append(args, job.layout.coverArgs(false)...)
		}
	}
	if hasToneMap(job.vfFilters) {
		// Without these tags players may still treat the output as BT.2020.
		args = append(args, "-color_primaries:v:0", "bt709", "-color_trc:v:0", "bt709", "-colorspace:v:0", "bt709")
	}
	if job.layout != nil {
		args = append(args, "-c:s", "copy")
	}

	if len(job.afFilters) > 0 {
		args = append(args, "-af", strings.Join(job.afFilters, ","))
	} else {
		args = append(args, "-c:a", "copy")
	}
	return append(args, job.tmpOutput)
}

// encodeFirstVideoOnly scopes an encoder plan's "-c:v <encoder>" to the first
// output video stream ("-c:v:0"), so cover art mapped after it can be
// stream-copied. The plan's other options only affect encoded streams.
func encodeFirstVideoOnly(planArgs []string) []string {
	scoped := slices.Clone(planArgs)
	for i, arg := range scoped {
		if arg == "-c:v" {
			scoped[i] = "-c:v:0"
		}
	}
	return scoped
}

// ── Stream layout ────────────────────────────────────────────────────────────

// streamLayout lists the input streams that post-processing maps by index:
// the main video stream and any cover art. yt-dlp's --embed-thumbnail stores
// cover art as an attached picture, which ffmpeg reports as a video stream
// (in MP4 and MP3, and also for a Matroska cover attachment).
type streamLayout struct {
	mainVideo   int        // index of the first video stream that is not cover art; -1 when there is none
	videoCount  int        // number of video streams that are not cover art
	covers      []coverArt // the attached pictures
	attachments int        // other attachment streams (such as fonts), copied by -map 0:t?

	// coverFiles holds the covers extracted to image files for a Matroska
	// output (see PPEngine.extractCovers). ffmpeg writes a mapped cover back
	// into Matroska as an ordinary video track, so there the covers are
	// re-attached from these files with -attach instead.
	coverFiles []string
}

// coverArt is one attached picture.
type coverArt struct {
	index    int    // input stream index
	filename string // Matroska attachment file name, e.g. "cover.jpg"; may be empty
	mimetype string // Matroska attachment MIME type, e.g. "image/jpeg"; may be empty
}

// mapArgs maps every stream worth keeping, plus the global metadata and the
// chapters. When the video is filtered, the main video stream is mapped
// first (output stream v:0); otherwise every video stream that is not cover
// art is copied. Cover art is mapped after it, unless it is re-attached from
// files. Data streams, such as MP4's chapter text track, are left out:
// -map_chapters recreates the chapters.
func (layout streamLayout) mapArgs(filterVideo bool) []string {
	var args []string
	if filterVideo {
		args = append(args, "-map", fmt.Sprintf("0:%d", layout.mainVideo))
	} else {
		args = append(args, "-map", "0:V?")
	}
	if layout.coverFiles == nil {
		for _, cover := range layout.covers {
			args = append(args, "-map", fmt.Sprintf("0:%d", cover.index))
		}
	}
	return append(args,
		"-map", "0:a?", "-map", "0:s?", "-map", "0:t?",
		"-map_metadata", "0", "-map_chapters", "0",
	)
}

// coverArgs keeps the cover art: each mapped cover is stream-copied and
// marked as an attached picture, or, for a Matroska output, each extracted
// cover file is attached with its file name and MIME type.
func (layout streamLayout) coverArgs(filterVideo bool) []string {
	var args []string
	for i, path := range layout.coverFiles {
		cover := layout.covers[i]
		stream := fmt.Sprintf("-metadata:s:t:%d", layout.attachments+i)
		args = append(args, "-attach", path,
			stream, "mimetype="+cmp.Or(cover.mimetype, "image/jpeg"),
			stream, "filename="+cmp.Or(cover.filename, "cover"+filepath.Ext(path)),
		)
	}
	if layout.coverFiles != nil {
		return args
	}

	// Mapped covers follow the video streams mapped before them. Without
	// video filters, "-c:v copy" already copies them.
	first := layout.videoCount
	if filterVideo {
		first = 1
	}
	for i := range layout.covers {
		if filterVideo {
			args = append(args, fmt.Sprintf("-c:v:%d", first+i), "copy")
		}
		args = append(args, fmt.Sprintf("-disposition:v:%d", first+i), "attached_pic")
	}
	return args
}

// ffmpegStreamPattern matches a stream line of ffmpeg's input summary, e.g.
// "  Stream #0:4[0x0](eng): Video: mjpeg (Baseline), … (attached pic)",
// capturing the stream index and its type.
var ffmpegStreamPattern = regexp.MustCompile(`Stream #0:(\d+)\S*: (\w+):`)

// ffmpegStreamTagPattern matches the attachment tags ffmpeg lists under a
// stream, e.g. "      mimetype        : image/jpeg".
var ffmpegStreamTagPattern = regexp.MustCompile(`^\s+(filename|mimetype)\s*:\s*(.+?)\s*$`)

// parseStreamLayout finds the main video stream, the cover art, and the
// other attachments in ffmpeg's input summary. ok is false when the summary
// lists no streams.
func parseStreamLayout(out string) (layout streamLayout, ok bool) {
	layout.mainVideo = -1
	var lastCover *coverArt // the cover whose tags follow, if any
	for _, line := range strings.Split(out, "\n") {
		if tag := ffmpegStreamTagPattern.FindStringSubmatch(line); tag != nil && lastCover != nil {
			if tag[1] == "filename" {
				lastCover.filename = tag[2]
			} else {
				lastCover.mimetype = tag[2]
			}
			continue
		}
		match := ffmpegStreamPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		ok = true
		lastCover = nil
		index, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		switch {
		case match[2] == "Attachment":
			layout.attachments++
		case match[2] != "Video":
		case strings.Contains(line, "(attached pic)"):
			layout.covers = append(layout.covers, coverArt{index: index})
			lastCover = &layout.covers[len(layout.covers)-1]
		default:
			layout.videoCount++
			if layout.mainVideo < 0 {
				layout.mainVideo = index
			}
		}
	}
	return layout, ok
}

// probeStreamLayout reads the file's stream layout from ffmpeg's input
// summary. It returns nil when the layout cannot be read, or when the video
// is to be filtered but there is no video stream to filter; the job then
// falls back to ffmpeg's default stream selection.
func (engine *PPEngine) probeStreamLayout(ctx context.Context, inputPath string, filterVideo bool) *streamLayout {
	// ffmpeg exits with an error when given no output file, but it has
	// printed the input summary by then.
	out, _ := newToolCommand(ctx, engine.FFmpegPath, "-hide_banner", "-i", inputPath).CombinedOutput()
	layout, ok := parseStreamLayout(string(out))
	if !ok || (filterVideo && layout.mainVideo < 0) {
		return nil
	}
	return &layout
}

// isMatroska reports whether a file extension names a Matroska container.
func isMatroska(ext string) bool {
	ext = strings.ToLower(ext)
	return ext == ".mkv" || ext == ".mka"
}

// extractCovers saves each cover of a Matroska job's input to an image file
// beside its temp output and records the files in job.layout.coverFiles, so
// buildFFmpegArgs can re-attach them. If a cover cannot be extracted, the
// covers are left out of the output and the log says so. The caller removes
// the files once the job has run.
func (engine *PPEngine) extractCovers(ctx context.Context, job *PostProcessJob, cb PPCallbacks) {
	layout := job.layout
	if layout == nil || len(layout.covers) == 0 || !isMatroska(filepath.Ext(job.tmpOutput)) {
		return
	}
	base := strings.TrimSuffix(job.tmpOutput, filepath.Ext(job.tmpOutput))
	files := []string{}
	for i, cover := range layout.covers {
		path := fmt.Sprintf("%s_cover%d%s", base, i+1, cmp.Or(filepath.Ext(cover.filename), ".jpg"))
		err := newToolCommand(ctx, engine.FFmpegPath,
			"-y", "-i", job.inputPath,
			"-map", fmt.Sprintf("0:%d", cover.index), "-c", "copy", "-frames:v", "1", "-f", "image2", path,
		).Run()
		if err != nil {
			removeFiles(files)
			cb.OnLog(fmt.Sprintf("[SYSTEM] Could not keep the cover art of %s (%v).", filepath.Base(job.inputPath), err), colWarning)
			layout.covers = nil
			return
		}
		files = append(files, path)
	}
	layout.coverFiles = files
}

// removeFiles removes paths, ignoring files that are already gone.
func removeFiles(paths []string) {
	for _, path := range paths {
		os.Remove(path)
	}
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

	// Plan one job per file, skipping files that need no processing. The
	// FFmpeg args are built below, once the thread budget is known.
	var jobs []PostProcessJob
	for _, inputPath := range filePaths {
		ext := strings.ToLower(filepath.Ext(inputPath))

		activeVF := vfFilters
		if isAudioOnlyExt(ext) {
			activeVF = nil // video filters do not apply to audio-only files
		} else {
			activeVF = engine.resolveAutoCrop(ctx, inputPath, activeVF, cb)
			activeVF = engine.resolveToneMap(ctx, inputPath, activeVF, cb)
		}
		if len(activeVF) == 0 && len(afFilters) == 0 {
			continue
		}

		tmpOutput := strings.TrimSuffix(inputPath, ext) + "_pp" + ext
		finalPath := inputPath
		encodeMode := "Stream copy"
		usedGPU := false
		if len(activeVF) > 0 {
			plan := PlanEncoder(engine.GPUBackend, engine.GPUCapabilities, ext)
			encodeMode = plan.Label
			usedGPU = plan.UsedGPU
		}
		frameCount := engine.probeFrameCount(ctx, inputPath)
		totalFrames := engine.computeOutputFrameCount(ctx, inputPath, frameCount, activeVF)
		jobs = append(jobs, PostProcessJob{
			inputPath:   inputPath,
			tmpOutput:   tmpOutput,
			finalPath:   finalPath,
			vfFilters:   activeVF,
			afFilters:   afFilters,
			encodeMode:  encodeMode,
			usedGPU:     usedGPU,
			totalFrames: totalFrames,
			layout:      engine.probeStreamLayout(ctx, inputPath, len(activeVF) > 0),
		})
		engine.extractCovers(ctx, &jobs[len(jobs)-1], cb)
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

	for i := range jobs {
		job := &jobs[i]
		job.threads = threadsPerJob
		job.ffmpegArgs = engine.buildFFmpegArgs(*job)
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

	for _, job := range jobs {
		if job.layout != nil {
			removeFiles(job.layout.coverFiles)
		}
	}
}
