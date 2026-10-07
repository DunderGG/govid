// logscanner.go — Parses yt-dlp stdout/stderr and reports events via callbacks.
//
// Responsibilities:
//   - Reads stdout and stderr from an active yt-dlp process concurrently.
//   - Reports each line to the caller (via ProcessCallbacks.OnLog) with
//     appropriate colouring. Plain lines carry a nil colour so the UI picks
//     the theme foreground, keeping this file free of Fyne imports.
//   - Extracts file-format metadata (source extensions, conversion flag)
//     for display in the post-download summary.
//   - Parses percentage and size tokens, reporting them via
//     ProcessCallbacks.OnProgress for the animated progress bar.
package main

import (
	"bufio"
	"fmt"
	"image/color"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// scanResult holds metadata collected while reading a yt-dlp process's output.
type scanResult struct {
	sourceExts        []string // file extensions seen in "[download] Destination:" lines
	wasConverted      bool     // true when [Merger] or [VideoConvertor] appeared in the output
	hadTransientErr   bool     // true when a recoverable network/rate-limit error was seen in stderr
	hadExtractorErr   bool     // true when stderr showed an error typical of a site change that a newer yt-dlp may fix
	hadExpiredLinkErr bool     // true when stderr showed an HTTP 403 or 410 error, which expired format URLs give
	hadSubtitleErr    bool     // true when stderr said subtitles could not be downloaded, which fails the whole download
	hadNoJSRuntime    bool     // true when yt-dlp warned that it found no JavaScript runtime for YouTube
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
	var (
		result scanResult
		// Each stream records conversions separately so the goroutines never
		// write the same field; yt-dlp prints [Merger] to stdout, but other
		// steps may report on stderr.
		stdoutConverted, stderrConverted bool
		waitGroup                        sync.WaitGroup
	)

	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			engine.parseProgress(line, cb)
			stdoutConverted = stdoutConverted || isConversionLine(line)
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
			cb.OnLog(fmt.Sprintf("[SYSTEM] stdout read error: %v", err), colWarning)
		}
	}()

	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if phase := detectPhase(line); phase != "" {
				cb.OnPhase(phase)
			}
			stderrConverted = stderrConverted || isConversionLine(line)
			// Detect transient network / rate-limit errors so the caller can retry.
			result.hadTransientErr = result.hadTransientErr || containsAny(line, transientErrPatterns)
			// Detect errors a newer yt-dlp may fix, so the caller can say so.
			isError := strings.Contains(line, "ERROR:")
			result.hadExtractorErr = result.hadExtractorErr || (isError && containsAny(line, extractorErrPatterns))
			result.hadExpiredLinkErr = result.hadExpiredLinkErr || (isError && containsAny(line, expiredLinkErrPatterns))
			// yt-dlp reports this as an ERROR, or inside the WARNING it gives when
			// it falls back from loaded info to the URL.
			result.hadSubtitleErr = result.hadSubtitleErr || strings.Contains(line, subtitleErrPattern)
			result.hadNoJSRuntime = result.hadNoJSRuntime || strings.Contains(line, noJSRuntimePattern)
			var logColor color.Color // nil = default foreground, resolved by the UI
			switch {
			case strings.Contains(line, "ERROR:"):
				logColor = colError
			case strings.Contains(line, "WARNING:"):
				logColor = colWarning
			case strings.Contains(line, "[debug]"):
				logColor = colDebug
			}
			cb.OnLog(line, logColor)
		}
		if err := scanner.Err(); err != nil {
			cb.OnLog(fmt.Sprintf("[SYSTEM] stderr read error: %v", err), colWarning)
		}
	}()

	waitGroup.Wait()
	result.wasConverted = stdoutConverted || stderrConverted
	return result
}

// progressLinePattern matches yt-dlp's per-update progress lines, e.g.
// "[download]  42.3% of   10.00MiB at    1.20MiB/s ETA 00:07".
var progressLinePattern = regexp.MustCompile(`^\[download\]\s+\d+(\.\d+)?%`)

// IsProgressLine reports whether line is one of yt-dlp's progress lines.
// With --newline yt-dlp prints hundreds of them per file, so the log view
// shows only the latest one in place (see UIManager.renderLogLines); the log
// file keeps them all.
func IsProgressLine(line string) bool {
	return progressLinePattern.MatchString(line)
}

// parseProgress scans a line of yt-dlp output for percentage markers and size
// information, reporting them to cb.OnProgress for the caller to apply to
// its own progress bar and session statistics.
func (engine *DownloadEngine) parseProgress(line string, cb ProcessCallbacks) {
	if strings.Contains(line, "%") {
		fields := strings.Fields(line)
		for _, field := range fields {
			if strings.HasSuffix(field, "%") {
				var val float64
				fmt.Sscanf(field, "%f%%", &val)
				size := ""
				if len(fields) >= 4 {
					size = fields[3]
				}
				cb.OnProgress(val/100.0, size)
				break
			}
		}
	}
}
