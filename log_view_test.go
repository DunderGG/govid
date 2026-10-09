package main

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

// ── Log view ─────────────────────────────────────────────────────────────────

// newLogTestManager returns a UIManager with its main window built and sized
// so the log view has a real viewport.
func newLogTestManager(t *testing.T) *UIManager {
	t.Helper()
	_ = test.NewApp()
	window := test.NewWindow(nil)
	app := newDownloaderApp(window)
	app.uiManager.createUI()
	window.Resize(fyne.NewSize(windowWidth, 1000))
	return app.uiManager
}

// logTexts returns the text of every line in the log view.
func logTexts(mgr *UIManager) []string {
	var texts []string
	for _, obj := range mgr.ui.download.logList.Objects {
		texts = append(texts, obj.(*canvas.Text).Text)
	}
	return texts
}

func TestAppendLogLineBatchesUntilFlush(t *testing.T) {
	mgr := newLogTestManager(t)
	var delays []time.Duration
	var scheduled []func()
	mgr.afterFunc = func(delay time.Duration, flush func()) *time.Timer {
		delays = append(delays, delay)
		scheduled = append(scheduled, flush)
		return nil
	}

	for i := range 3 {
		mgr.appendLogLine(fmt.Sprintf("line %d", i), nil)
	}

	if len(scheduled) != 1 || delays[0] != logFlushInterval {
		t.Fatalf("flushes scheduled = %d with delays %v, want one after %v", len(scheduled), delays, logFlushInterval)
	}
	if n := len(mgr.ui.download.logList.Objects); n != 0 {
		t.Errorf("log view has %d lines before the flush, want 0", n)
	}

	scheduled[0]()

	if got := logTexts(mgr); !slices.Equal(got, []string{"line 0", "line 1", "line 2"}) {
		t.Errorf("log view = %q, want the lines in order", got)
	}

	// The next line arms a new flush.
	mgr.appendLogLine("line 3", nil)
	if len(scheduled) != 2 {
		t.Errorf("flushes scheduled = %d, want a second one after the first ran", len(scheduled))
	}
}

func TestLogViewCapsLinesEvenWhenUnlimited(t *testing.T) {
	mgr := newLogTestManager(t)
	mgr.onLogBufferLimit = func() int { return ParseBufferLimit(logLimitUnlimited) }

	for i := range maxScreenLogLines + 1000 {
		mgr.appendLogLine(fmt.Sprintf("line %d", i), nil)
	}
	mgr.flushLog()

	texts := logTexts(mgr)
	if len(texts) != maxScreenLogLines {
		t.Fatalf("log view has %d lines, want the %d-line cap", len(texts), maxScreenLogLines)
	}
	if want := fmt.Sprintf("line %d", maxScreenLogLines+999); texts[len(texts)-1] != want {
		t.Errorf("last line = %q, want %q (the oldest lines are trimmed)", texts[len(texts)-1], want)
	}
}

func TestScreenLogLimit(t *testing.T) {
	tests := []struct{ bufferLimit, want int }{
		{200, 200},
		{maxScreenLogLines, maxScreenLogLines},
		{ParseBufferLimit(logLimitUnlimited), maxScreenLogLines},
	}
	for _, tt := range tests {
		if got := screenLogLimit(tt.bufferLimit); got != tt.want {
			t.Errorf("screenLogLimit(%d) = %d, want %d", tt.bufferLimit, got, tt.want)
		}
	}
}

func TestLogViewFollowsOnlyWhenAtBottom(t *testing.T) {
	mgr := newLogTestManager(t)
	output := mgr.ui.download.output

	addLines := func(n int) {
		for i := range n {
			mgr.appendLogLine(fmt.Sprintf("line %d", i), nil)
		}
		mgr.flushLog()
	}

	addLines(200)
	if !isScrolledToBottom(output) {
		t.Fatalf("view not at the bottom after lines were added while following (offset %v)", output.Offset)
	}

	// The user scrolls up to read something; new lines must not move the view.
	output.ScrollToTop()
	addLines(50)
	if output.Offset.Y != 0 {
		t.Errorf("view moved to offset %v while scrolled up, want it left at the top", output.Offset)
	}

	// Back at the bottom, the view follows new lines again.
	output.ScrollToBottom()
	addLines(50)
	if !isScrolledToBottom(output) {
		t.Errorf("view stopped following new lines after returning to the bottom (offset %v)", output.Offset)
	}
}

func TestClearTerminalOutputDropsQueuedLines(t *testing.T) {
	mgr := newLogTestManager(t)

	mgr.appendLogLine("queued before the clear", nil)
	mgr.clearTerminalOutput()
	mgr.flushLog()

	if got := logTexts(mgr); len(got) != 0 {
		t.Errorf("log view = %q, want lines queued before the clear dropped", got)
	}
}

func TestLogViewShowsLatestProgressLineInPlace(t *testing.T) {
	mgr := newLogTestManager(t)

	for _, line := range []string{
		"[download] Destination: GoVid_Clip.f137.mp4",
		"[download]  10.0% of   10.00MiB at    1.00MiB/s ETA 00:09",
		"[download]  20.0% of   10.00MiB at    1.00MiB/s ETA 00:08",
	} {
		mgr.appendLogLine(line, nil)
	}
	mgr.flushLog()
	// A later flush still updates the same line.
	mgr.appendLogLine("[download] 100% of   10.00MiB in 00:00:10 at 1.00MiB/s", nil)
	mgr.appendLogLine("[download] Destination: GoVid_Clip.f140.m4a", nil)
	mgr.appendLogLine("[download]  50.0% of    2.00MiB at    1.00MiB/s ETA 00:01", nil)
	mgr.flushLog()

	want := []string{
		"[download] Destination: GoVid_Clip.f137.mp4",
		"[download] 100% of   10.00MiB in 00:00:10 at 1.00MiB/s",
		"[download] Destination: GoVid_Clip.f140.m4a",
		"[download]  50.0% of    2.00MiB at    1.00MiB/s ETA 00:01",
	}
	if got := logTexts(mgr); !slices.Equal(got, want) {
		t.Errorf("log view = %q, want %q", got, want)
	}
}
