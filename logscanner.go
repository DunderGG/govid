// logscanner.go — Parses yt-dlp stdout/stderr and reports events via callbacks.
//
// Responsibilities:
//   - Reads stdout and stderr from an active yt-dlp process concurrently.
//   - Reports each line to the caller (via ProcessCallbacks.OnLog) with
//     appropriate colouring. Plain lines carry a nil colour so the UI picks
//     the theme foreground, keeping this file free of Fyne imports.
//   - Extracts file-format metadata (source extensions, conversion flag)
//     for display in the post-download summary.
//   - Parses progress percentages, reporting them via
//     ProcessCallbacks.OnProgress for the animated progress bar.
package main

import (
	"fmt"
	"image/color"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// scanResult holds metadata collected while reading a yt-dlp process's output.
type scanResult struct {
	sourceExts        []string      // file extensions seen in "[download] Destination:" lines
	wasConverted      bool          // true when [Merger] or [VideoConvertor] appeared in the output
	hadTransientErr   bool          // true when a recoverable network/rate-limit error was seen in stderr
	hadExtractorErr   bool          // true when stderr showed an error typical of a site change that a newer yt-dlp may fix
	hadExpiredLinkErr bool          // true when stderr showed an HTTP 403 or 410 error, which expired format URLs give
	hadSubtitleErr    bool          // true when stderr said subtitles could not be downloaded, which fails the whole download
	hadNoJSRuntime    bool          // true when yt-dlp warned that it found no JavaScript runtime for YouTube
	accessProblem     accessProblem // the first error that cookies caused or would fix (see classifyAccessError)
	hadRateLimit      bool          // true when the site answered HTTP 429 (too many requests)
}

// expiredLinkErrPatterns are substrings of the errors a site gives for a
// format URL that has expired or was issued to another IP address.
var expiredLinkErrPatterns = []string{
	"HTTP Error 403",
	"HTTP Error 410",
}

// extractorErrPatterns are substrings of yt-dlp errors that usually mean the
// site changed in a way an outdated yt-dlp cannot handle.
var extractorErrPatterns = []string{
	"Unable to extract",
	"Sign in to confirm",
	"HTTP Error 403",
}

// subtitleErrPattern starts the error yt-dlp gives when a subtitle file
// cannot be downloaded (YouTube often answers 429). It fails the download.
const subtitleErrPattern = "Unable to download video subtitles"

// noJSRuntimePattern starts the warning yt-dlp gives on every YouTube
// extraction when it has no JavaScript runtime to solve the player
// challenges with. It then uses a deprecated client that may miss formats.
const noJSRuntimePattern = "No supported JavaScript runtime"

// transientErrPatterns are substrings that indicate a temporary failure worth retrying.
var transientErrPatterns = []string{
	"HTTP Error 429",
	"Too Many Requests",
	"Read timed out",
	"urlopen error",
	"Connection reset by peer",
	"RemoteDisconnected",
	"IncompleteRead",
	"Connection refused",
	"Network is unreachable",
	"socket.timeout",
}

// Post-download steps yt-dlp runs with ffmpeg, reported via
// ProcessCallbacks.OnPhase.
const (
	phaseMerging    = "Merging"    // [Merger]: joining the video and audio streams
	phaseConverting = "Converting" // [VideoConvertor], [ExtractAudio]: re-encoding to the target format
)

// detectPhase returns the post-download step a yt-dlp output line starts,
// or "" when the line does not start one.
func detectPhase(line string) string {
	switch {
	case strings.HasPrefix(line, "[Merger]"):
		return phaseMerging
	case strings.HasPrefix(line, "[VideoConvertor]"), strings.HasPrefix(line, "[ExtractAudio]"):
		return phaseConverting
	default:
		return ""
	}
}

// containsAny reports whether line contains any of patterns.
func containsAny(line string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(line, pattern) {
			return true
		}
	}
	return false
}

// isConversionLine reports whether a yt-dlp output line shows ffmpeg merging
// or re-encoding the download, for the summary's format line.
func isConversionLine(line string) bool {
	return strings.HasPrefix(line, "[Merger]") || strings.HasPrefix(line, "[VideoConvertor]")
}

// watchOutput reads stdout and stderr from a running yt-dlp process concurrently,
// forwarding every line to the UI log (via cb.OnLog) and collecting format
// metadata. It blocks until both streams reach EOF. The engine owns no mutable
// UI state itself — progress updates are reported through cb.OnProgress.
func (engine *DownloadEngine) watchOutput(stdout, stderr io.Reader, cb ProcessCallbacks) scanResult {
	// Each stream is scanned into its own result, so the goroutines never
	// write the same field, and the two are combined once both are done.
	var (
		stdoutResult, stderrResult scanResult
		waitGroup                  sync.WaitGroup
	)
	waitGroup.Go(func() { stdoutResult = engine.scanStdout(stdout, cb) })
	waitGroup.Go(func() { stderrResult = scanStderr(stderr, cb) })
	waitGroup.Wait()

	result := stderrResult
	result.sourceExts = stdoutResult.sourceExts
	// yt-dlp prints [Merger] to stdout, but other steps may report on stderr.
	result.wasConverted = stdoutResult.wasConverted || stderrResult.wasConverted
	return result
}

// scanStdout reads yt-dlp's stdout until EOF: it reports each line's
// progress and post-download step, logs the line, and returns the source
// extensions it saw and whether a line showed a conversion.
func (engine *DownloadEngine) scanStdout(stdout io.Reader, cb ProcessCallbacks) scanResult {
	var result scanResult
	scanner := newOutputScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		engine.parseProgress(line, cb)
		result.wasConverted = result.wasConverted || isConversionLine(line)
		if phase := detectPhase(line); phase != "" {
			cb.OnPhase(phase)
		}
		// Capture the extension of each file yt-dlp writes to disk.
		if dest, found := strings.CutPrefix(line, "[download] Destination: "); found {
			if ext := strings.TrimPrefix(filepath.Ext(dest), "."); ext != "" {
				result.sourceExts = append(result.sourceExts, ext)
			}
		}
		cb.OnLog(line, nil) // nil = default foreground, resolved by the UI
	}
	if err := scanner.Err(); err != nil {
		cb.OnLog(fmt.Sprintf("[SYSTEM] stdout read error: %v; the rest of yt-dlp's output is not shown.", err), colWarning)
		drainOutput(stdout, nil)
	}
	return result
}

// scanStderr reads yt-dlp's stderr until EOF: it reports each line's
// post-download step, logs the line coloured by its kind, and returns what
// the lines showed (see classifyStderrLine).
func scanStderr(stderr io.Reader, cb ProcessCallbacks) scanResult {
	var result scanResult
	scanner := newOutputScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if phase := detectPhase(line); phase != "" {
			cb.OnPhase(phase)
		}
		classifyStderrLine(line, &result)
		cb.OnLog(line, stderrColor(line))
	}
	if err := scanner.Err(); err != nil {
		cb.OnLog(fmt.Sprintf("[SYSTEM] stderr read error: %v; the rest of yt-dlp's output is not shown.", err), colWarning)
		drainOutput(stderr, nil)
	}
	return result
}

// classifyStderrLine records in result what a line of yt-dlp's stderr
// shows: a conversion, and the errors the caller retries on, explains, or
// works around.
func classifyStderrLine(line string, result *scanResult) {
	result.wasConverted = result.wasConverted || isConversionLine(line)
	// Detect transient network / rate-limit errors so the caller can retry.
	result.hadTransientErr = result.hadTransientErr || containsAny(line, transientErrPatterns)
	result.hadRateLimit = result.hadRateLimit || strings.Contains(line, "HTTP Error 429") || strings.Contains(line, "Too Many Requests")
	// Detect errors a newer yt-dlp may fix, so the caller can say so.
	isError := strings.Contains(line, "ERROR:")
	result.hadExtractorErr = result.hadExtractorErr || (isError && containsAny(line, extractorErrPatterns))
	result.hadExpiredLinkErr = result.hadExpiredLinkErr || (isError && containsAny(line, expiredLinkErrPatterns))
	// yt-dlp reports this as an ERROR, or inside the WARNING it gives when
	// it falls back from loaded info to the URL.
	result.hadSubtitleErr = result.hadSubtitleErr || strings.Contains(line, subtitleErrPattern)
	result.hadNoJSRuntime = result.hadNoJSRuntime || strings.Contains(line, noJSRuntimePattern)
	if result.accessProblem == accessOK {
		result.accessProblem = classifyAccessError(line)
	}
}

// stderrColor returns the log colour of a line of yt-dlp's stderr: errors,
// warnings, and debug lines have their own, and any other line is nil, the
// default foreground, resolved by the UI.
func stderrColor(line string) color.Color {
	switch {
	case strings.Contains(line, "ERROR:"):
		return colError
	case strings.Contains(line, "WARNING:"):
		return colWarning
	case strings.Contains(line, "[debug]"):
		return colDebug
	default:
		return nil
	}
}

// progressLinePattern matches yt-dlp's per-update progress lines, e.g.
// "[download]  42.3% of   10.00MiB at    1.20MiB/s ETA 00:07", with the
// "[2/5] " item prefix simultaneous downloads add (see itemPrefix).
var progressLinePattern = regexp.MustCompile(`^(\[\d+/\d+\] )?\[download\]\s+\d+(\.\d+)?%`)

// itemPrefixPattern matches the "[2/5] " prefix that marks which queue item
// a line comes from when several download at once.
var itemPrefixPattern = regexp.MustCompile(`^\[\d+/\d+\] `)

// IsProgressLine reports whether line is one of yt-dlp's progress lines.
// With --newline yt-dlp prints hundreds of them per file, so the log view
// shows only the latest one in place (see UIManager.renderLogLines); the log
// file keeps them all.
func IsProgressLine(line string) bool {
	return progressLinePattern.MatchString(line)
}

// lineItem returns the item prefix of line ("[2/5] "), or "" when it has
// none.
func lineItem(line string) string {
	return itemPrefixPattern.FindString(line)
}

// parseProgress reports the first percentage in a line of yt-dlp output,
// e.g. the 42.3 of "[download]  42.3% of   10.00MiB …", to cb.OnProgress for
// the caller to apply to its progress bar. A field such as "abc%" is not a
// percentage and is skipped. The sizes in the line are not used: they give
// the current stream's total, not what has been downloaded, so the summary
// measures the files instead (DownloadResult.Bytes).
func (engine *DownloadEngine) parseProgress(line string, cb ProcessCallbacks) {
	if !strings.Contains(line, "%") {
		return
	}
	for _, field := range strings.Fields(line) {
		number, found := strings.CutSuffix(field, "%")
		if !found {
			continue
		}
		pct, err := strconv.ParseFloat(number, 64)
		if err != nil {
			continue
		}
		cb.OnProgress(pct / 100)
		return
	}
}
