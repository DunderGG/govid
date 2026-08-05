// logscanner.go — Parses yt-dlp stdout/stderr and reports events via callbacks.
//
// Responsibilities:
//   - Reads stdout and stderr from an active yt-dlp process concurrently.
//   - Reports each line to the caller (via ProcessCallbacks.OnLog) with
//     appropriate colouring.
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
	"strings"
	"sync"

	"fyne.io/fyne/v2/theme"
)

// scanResult holds metadata collected while reading a yt-dlp process's output.
type scanResult struct {
	sourceExts      []string // file extensions seen in "[download] Destination:" lines
	wasConverted    bool     // true when [Merger] or [VideoConvertor] appeared in stderr
	hadTransientErr bool     // true when a recoverable network/rate-limit error was seen in stderr
}

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

// watchOutput reads stdout and stderr from a running yt-dlp process concurrently,
// forwarding every line to the UI log (via cb.OnLog) and collecting format
// metadata. It blocks until both streams reach EOF. The engine owns no mutable
// UI state itself — progress updates are reported through cb.OnProgress.
func (engine *DownloadEngine) watchOutput(stdout, stderr io.Reader, cb ProcessCallbacks) scanResult {
	var (
		result    scanResult
		waitGroup sync.WaitGroup
	)

	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			engine.parseProgress(line, cb)
			// Capture the extension of each file yt-dlp writes to disk.
			if dest, found := strings.CutPrefix(line, "[download] Destination: "); found {
				if ext := strings.TrimPrefix(filepath.Ext(dest), "."); ext != "" {
					result.sourceExts = append(result.sourceExts, ext)
				}
			}
			cb.OnLog(line, theme.ForegroundColor())
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
			// Detect ffmpeg post-processing (merge or re-encode).
			if strings.HasPrefix(line, "[Merger]") || strings.HasPrefix(line, "[VideoConvertor]") {
				result.wasConverted = true
			}
			// Detect transient network / rate-limit errors so the caller can retry.
			if !result.hadTransientErr {
				for _, pattern := range transientErrPatterns {
					if strings.Contains(line, pattern) {
						result.hadTransientErr = true
						break
					}
				}
			}
			var logColor color.Color
			switch {
			case strings.Contains(line, "ERROR:"):
				logColor = colError
			case strings.Contains(line, "WARNING:"):
				logColor = colWarning
			case strings.Contains(line, "[debug]"):
				logColor = colDebug
			default:
				logColor = theme.ForegroundColor()
			}
			cb.OnLog(line, logColor)
		}
		if err := scanner.Err(); err != nil {
			cb.OnLog(fmt.Sprintf("[SYSTEM] stderr read error: %v", err), colWarning)
		}
	}()

	waitGroup.Wait()
	return result
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
