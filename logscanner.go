// logscanner.go — Parses and routes yt-dlp stdout/stderr to the UI.
//
// Responsibilities:
//   - Reads stdout and stderr from an active yt-dlp process concurrently.
//   - Routes each line to the UI with appropriate colouring.
//   - Extracts file-format metadata (source extensions, conversion flag)
//     for display in the post-download summary.
//   - Parses percentage and size tokens for the animated progress bar.
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
// forwarding every line to the UI log and collecting format metadata.
// It blocks until both streams reach EOF.
func (app *DownloaderApp) watchOutput(stdout, stderr io.Reader) scanResult {
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
			app.parseProgress(line)
			// Capture the extension of each file yt-dlp writes to disk.
			if dest, found := strings.CutPrefix(line, "[download] Destination: "); found {
				if ext := strings.TrimPrefix(filepath.Ext(dest), "."); ext != "" {
					result.sourceExts = append(result.sourceExts, ext)
				}
			}
			app.appendOutput(line, theme.ForegroundColor())
		}
		if err := scanner.Err(); err != nil {
			app.appendOutput(fmt.Sprintf("[SYSTEM] stdout read error: %v", err), colWarning)
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
			app.appendOutput(line, logColor)
		}
		if err := scanner.Err(); err != nil {
			app.appendOutput(fmt.Sprintf("[SYSTEM] stderr read error: %v", err), colWarning)
		}
	}()

	waitGroup.Wait()
	return result
}

// parseProgress scans a line of yt-dlp output for percentage markers and size
// information, updating the progress bar target and session statistics.
func (app *DownloaderApp) parseProgress(line string) {
	if strings.Contains(line, "%") {
		fields := strings.Fields(line)
		for _, field := range fields {
			if strings.HasSuffix(field, "%") {
				var val float64
				fmt.Sscanf(field, "%f%%", &val)
				app.setProgress(val / 100.0)
				if len(fields) >= 4 {
					app.stats.lastSize = fields[3]
					fmt.Sscanf(app.stats.lastSize, "%f%s", &app.stats.downloadedRaw, &app.stats.unit)
				}
				break
			}
		}
	}
}
