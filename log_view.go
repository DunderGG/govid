// log_view.go — The main window's Terminal Output log view.
//
// Responsibilities:
//   - appendLogLine, flushLog: batched rendering of log lines, at most every
//     logFlushInterval, capped at maxScreenLogLines and following new lines
//     only while the view is scrolled to the bottom.
//   - clearTerminalOutput: empties the view, including lines not yet shown.
//
// Writing lines to the session log file is LogService's job (log_service.go).
package main

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// logFlushInterval is how long appendLogLine collects lines before they are
// rendered together. Verbose yt-dlp and FFmpeg output can produce hundreds of
// lines a second; rendering them in batches keeps the UI thread responsive.
const logFlushInterval = 100 * time.Millisecond

// maxScreenLogLines caps the lines kept in the log view whatever the Log
// Buffer Limit preference says, since every refresh lays out every line.
// "Unlimited" therefore applies only to the lines kept for the log file.
const maxScreenLogLines = 5000

// followTolerance is how close to the bottom, in pixels, the log view must be
// for new lines to keep it scrolled to the bottom.
const followTolerance = 8

// pendingLogLine is a log line waiting for the next flush.
type pendingLogLine struct {
	text string
	col  color.Color
}

// screenLogLimit returns the number of lines the log view keeps for the
// given Log Buffer Limit.
func screenLogLimit(bufferLimit int) int {
	return min(bufferLimit, maxScreenLogLines)
}

// appendLogLine queues one line for the graphical log view. It is registered
// on DownloaderApp as the onLogLine callback so appendOutput never touches
// widgets directly. It is safe to call from any goroutine: the first queued
// line arms a timer that renders every line queued by then in a single UI
// update (see flushLog), so an idle app does no work.
func (manager *UIManager) appendLogLine(line string, col color.Color) {
	manager.logMu.Lock()
	defer manager.logMu.Unlock()
	manager.pendingLog = append(manager.pendingLog, pendingLogLine{text: line, col: col})
	if !manager.logFlushArmed {
		manager.logFlushArmed = true
		manager.afterFunc(logFlushInterval, manager.flushLog)
	}
}

// takePendingLog removes and returns the queued log lines.
func (manager *UIManager) takePendingLog() []pendingLogLine {
	manager.logMu.Lock()
	defer manager.logMu.Unlock()
	lines := manager.pendingLog
	manager.pendingLog = nil
	manager.logFlushArmed = false
	return lines
}

// flushLog renders the queued log lines now, in one UI update. The timer
// armed by appendLogLine calls it; call it directly to show queued lines
// without waiting, e.g. so a session's summary appears as soon as it ends.
func (manager *UIManager) flushLog() {
	lines := manager.takePendingLog()
	if len(lines) == 0 {
		return
	}
	fyne.Do(func() { manager.renderLogLines(lines) })
}

// renderLogLines appends lines to the log view, trims it once to its limit,
// and refreshes it once. A yt-dlp progress line replaces the one before it
// instead of adding a line. The view follows new lines only if it was
// already at (or within followTolerance of) the bottom, so a user who
// scrolled up to read something is not pulled back down. A nil colour means
// "default text colour" and is resolved to the current theme's foreground
// here, on the UI side. Must be called on the UI thread.
func (manager *UIManager) renderLogLines(lines []pendingLogLine) {
	logList := manager.ui.download.logList
	output := manager.ui.download.output
	follow := isScrolledToBottom(output)

	for _, line := range lines {
		col := line.col
		if col == nil {
			col = theme.Color(theme.ColorNameForeground)
		}
		// A file's yt-dlp progress lines share one line of the view, which
		// shows the latest; the log file keeps every one. With several
		// downloads at once, each item (by its line prefix) has its own,
		// until another line of that item, such as the next file's
		// destination, follows it.
		item := lineItem(line.text)
		if !IsProgressLine(line.text) {
			delete(manager.progressLines, item)
		} else if shown := manager.progressLines[item]; shown != nil {
			shown.Text, shown.Color = line.text, col
			shown.Refresh()
			continue
		}
		label := canvas.NewText(line.text, col)
		label.TextSize = theme.TextSize()
		logList.Objects = append(logList.Objects, label)
		if IsProgressLine(line.text) {
			manager.progressLines[item] = label
		}
	}

	if limit := screenLogLimit(manager.onLogBufferLimit()); len(logList.Objects) > limit {
		logList.Objects = logList.Objects[len(logList.Objects)-limit:]
		// A progress line may have been trimmed away; start new ones.
		clear(manager.progressLines)
	}

	logList.Refresh()
	if follow {
		// Resize the content now, as the scroll's next layout pass would:
		// ScrollToBottom clamps the offset to the content's current size,
		// which is stale until then.
		output.Content.Resize(output.Content.MinSize().Max(output.Size()))
		output.ScrollToBottom()
	}
}

// isScrolledToBottom reports whether scroll shows the end of its content,
// within followTolerance pixels. Content shorter than the view counts as
// scrolled to the bottom.
func isScrolledToBottom(scroll *container.Scroll) bool {
	// MinSize, like Scroll.ScrollToBottom, is current even before the next
	// layout pass has resized the content.
	hidden := scroll.Content.MinSize().Height - scroll.Size().Height
	return scroll.Offset.Y >= hidden-followTolerance
}

// clearTerminalOutput empties the terminal output window and resets its
// scroll position. Lines still queued for the view are dropped too, since
// they were logged before the clear.
func (manager *UIManager) clearTerminalOutput() {
	ui := manager.ui
	if ui == nil || ui.download.logList == nil {
		return
	}
	manager.takePendingLog()
	fyne.Do(func() {
		ui.download.logList.Objects = nil
		ui.download.logList.Refresh()
		clear(manager.progressLines)
		if ui.download.output != nil {
			ui.download.output.ScrollToTop()
		}
	})
}

// pendingLogLines returns how many log lines wait for the next flush.
func (manager *UIManager) pendingLogLines() int {
	manager.logMu.Lock()
	defer manager.logMu.Unlock()
	return len(manager.pendingLog)
}
