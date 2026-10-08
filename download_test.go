package main

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// downloadHarness is a fully wired DownloaderApp running against the Fyne
// test driver, a fake yt-dlp, and temp directories for downloads and history.
type downloadHarness struct {
	app     *DownloaderApp
	window  fyne.Window
	saveDir string
	runs    func() int // number of times the fake yt-dlp has been started

	mu    sync.Mutex
	logs  []string
	onLog func(line string) // optional hook run for each log line
}

// newDownloadHarness builds the app with the fake yt-dlp running in mode.
func newDownloadHarness(t *testing.T, mode string) *downloadHarness {
	t.Helper()
	_ = test.NewApp()
	isolatePath(t)
	useFakeTool(t, mode)

	window := test.NewWindow(nil)
	app := newDownloaderApp(window)
	app.uiManager.createUI()

	binDir := t.TempDir()
	installFakeTool(t, binDir, "yt-dlp")
	app.depSvc = &DependencyService{binDir: binDir}
	app.historySvc = &HistoryService{filePath: filepath.Join(t.TempDir(), historyFileName)}
	app.queueStore = &QueueStore{filePath: filepath.Join(t.TempDir(), queueFileName)}
	app.uiManager.onLoadHistory = app.historySvc.Load
	app.uiManager.onClearHistory = app.historySvc.Clear
	// The test driver runs fyne.Do on the calling goroutine instead of the
	// UI thread, so Queue panel redraws from the throttle's timer goroutine
	// would race with the session goroutine's own UI updates. Panel tests
	// redraw it explicitly (see refreshQueue).
	app.uiManager.queueThrottle = newLatestValueThrottle(statusThrottleInterval, func(int64) {})
	// The same goes for the Pause button's timed updates; tests read
	// awaitingResume instead, and TestShowPauseControl checks the button.
	app.pauseThrottle = newLatestValueThrottle(statusThrottleInterval, func(pauseControl) {})

	// Anchor the error log in a temp dir rather than beside the test binary.
	if _, err := app.logSvc.OpenSessionLog(t.TempDir()); err != nil {
		t.Fatalf("OpenSessionLog: %v", err)
	}
	t.Cleanup(app.logSvc.CloseSessionLog)

	h := &downloadHarness{app: app, window: window, saveDir: t.TempDir(), runs: useFakeToolState(t)}
	app.onLogLine = func(line string, _ color.Color) {
		h.mu.Lock()
		h.logs = append(h.logs, line)
		hook := h.onLog
		h.mu.Unlock()
		if hook != nil {
			hook(line)
		}
	}

	app.ui.download.path.SetText(h.saveDir)
	app.ui.download.format.SetSelected("MP4")
	app.ui.download.quality.SetSelected("Best Quality")
	return h
}

func (h *downloadHarness) setHook(hook func(line string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onLog = hook
}

func (h *downloadHarness) joinedLogs() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.logs, "\n")
}

func (h *downloadHarness) history(t *testing.T) []DownloadHistoryEntry {
	t.Helper()
	entries, err := h.app.historySvc.Load()
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	return entries
}

func (h *downloadHarness) savedFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(h.saveDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names
}

// startAndWait runs startDownload and blocks until the session has finished.
func (h *downloadHarness) startAndWait(t *testing.T) {
	t.Helper()
	h.app.startDownload()
	// Wait for sessions.Done, not isRunning: runSession clears isRunning
	// before it re-enables the Download button, and the next startDownload
	// must not run beside that.
	if !waitTimeout(&h.app.sessions, 60*time.Second) {
		t.Fatalf("download session did not finish; log:\n%s", h.joinedLogs())
	}
}

// ── Input validation ─────────────────────────────────────────────────────────

func TestValidateTimestamp(t *testing.T) {
	tests := []struct {
		in    string
		valid bool
	}{
		{"", true},
		{"90", true},
		{"90.5", true},
		{"1:30", true},
		{"01:30", true},
		{"1:02:03", true},
		{"123:00:00", true},
		{"1:3", false},
		{"1:30:5", false},
		{"1:30:00:00", false},
		{"90s", false},
		{"-5", false},
		{"1.2.3", false},
		{" 90", false},
		{"abc", false},
	}

	for _, tt := range tests {
		err := validateTimestamp(tt.in)
		if (err == nil) != tt.valid {
			t.Errorf("validateTimestamp(%q) error = %v, want valid=%v", tt.in, err, tt.valid)
		}
	}
}

func TestCollectURLs(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		batch   bool
		want    []string
		wantErr bool
	}{
		{"single trimmed", "  https://a  ", false, []string{"https://a"}, false},
		{"single keeps newlines as one URL", "https://a\nhttps://b", false, []string{"https://a\nhttps://b"}, false},
		{"single empty", " \t ", false, nil, true},
		{"batch skips blank lines", "https://a\n\n  https://b \n\t\n", true, []string{"https://a", "https://b"}, false},
		{"batch handles CRLF", "https://a\r\nhttps://b\r\n", true, []string{"https://a", "https://b"}, false},
		{"batch only blank lines", "\n  \n\t\n", true, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := collectURLs(tt.text, tt.batch)
			if (err != nil) != tt.wantErr {
				t.Fatalf("collectURLs(%q, %v) error = %v, wantErr %v", tt.text, tt.batch, err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("collectURLs(%q, %v) = %q, want %q", tt.text, tt.batch, got, tt.want)
			}
		})
	}
}

func TestCompletionNotification(t *testing.T) {
	tests := []struct {
		name          string
		postProcessed bool
		fileCount     int
		urlCount      int
		wantTitle     string
		wantContent   string
	}{
		{"single download", false, 1, 1, "GoVid — Download Complete", "Your download is ready."},
		{"batch counts URLs", false, 2, 3, "GoVid — Download Complete", "3 downloads complete."},
		{"post-processed counts files", true, 2, 3, "GoVid — All Done", "2 file(s) downloaded and processed."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := completionNotification(tt.postProcessed, tt.fileCount, tt.urlCount)
			if got.Title != tt.wantTitle || got.Content != tt.wantContent {
				t.Errorf("completionNotification(%v, %d, %d) = {%q, %q}, want {%q, %q}",
					tt.postProcessed, tt.fileCount, tt.urlCount, got.Title, got.Content, tt.wantTitle, tt.wantContent)
			}
		})
	}
}

// ── Download summary formatting ──────────────────────────────────────────────

func TestDescribeOutputFormat(t *testing.T) {
	tests := []struct {
		name      string
		extension string
		scan      scanResult
		want      string
	}{
		{"no source info", "mp4", scanResult{}, "MP4"},
		{"original", "mp4", scanResult{sourceExts: []string{"mp4"}}, "MP4 (original)"},
		{"remuxed merge", "mp4", scanResult{sourceExts: []string{"webm", "m4a"}}, "WEBM+M4A → MP4 (remuxed)"},
		{"converted", "mp4", scanResult{sourceExts: []string{"webm"}, wasConverted: true}, "WEBM → MP4 (converted)"},
		{"duplicate sources collapsed", "mkv", scanResult{sourceExts: []string{"webm", "WEBM", "m4a"}}, "WEBM+M4A → MKV (remuxed)"},
		{"same ext but converted", "mp4", scanResult{sourceExts: []string{"mp4"}, wasConverted: true}, "MP4 → MP4 (converted)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeOutputFormat(tt.extension, tt.scan); got != tt.want {
				t.Errorf("describeOutputFormat(%q, %+v) = %q, want %q", tt.extension, tt.scan, got, tt.want)
			}
		})
	}
}

func TestSummaryLines(t *testing.T) {
	got := summaryLines([]summaryRow{
		{"Runtime", "1.50s"},
		{"Avg Speed", "N/A"},
		{"Downloaded", "10.00MiB"},
	})
	want := []string{
		"   ├─ Runtime:    1.50s",
		"   ├─ Avg Speed:  N/A",
		"   └─ Downloaded: 10.00MiB",
	}
	if !slices.Equal(got, want) {
		t.Errorf("summaryLines() =\n%q\nwant\n%q", got, want)
	}
	if got := summaryLines(nil); len(got) != 0 {
		t.Errorf("summaryLines(nil) = %q, want empty", got)
	}
}

func TestAverageSpeed(t *testing.T) {
	tests := []struct {
		downloaded, seconds float64
		unit                string
		want                string
	}{
		{10, 4, "MiB", "2.50MiB/s"},
		{0, 4, "MiB", "N/A"},
		{10, 0, "MiB", "N/A"},
	}
	for _, tt := range tests {
		if got := averageSpeed(tt.downloaded, tt.seconds, tt.unit); got != tt.want {
			t.Errorf("averageSpeed(%v, %v, %q) = %q, want %q", tt.downloaded, tt.seconds, tt.unit, got, tt.want)
		}
	}
}

func TestStartDownloadRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		setup func(ui *UIWidgets)
	}{
		{"empty URL", func(ui *UIWidgets) { ui.download.entry.SetText("   ") }},
		{"batch with only blank lines", func(ui *UIWidgets) {
			ui.download.batchMode.SetChecked(true)
			ui.download.entry.SetText("\n  \n\t\n")
		}},
		{"empty save path", func(ui *UIWidgets) {
			ui.download.entry.SetText("https://example.com/v")
			ui.download.path.SetText("  ")
		}},
		{"invalid trim start", func(ui *UIWidgets) {
			ui.download.entry.SetText("https://example.com/v")
			ui.download.trimStart.SetText("1m30s")
		}},
		{"invalid trim end", func(ui *UIWidgets) {
			ui.download.entry.SetText("https://example.com/v")
			ui.download.trimEnd.SetText("1:3")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newDownloadHarness(t, "ytdlp-download")
			tt.setup(h.app.ui)

			h.app.startDownload()

			if h.app.isRunning.Load() {
				t.Error("isRunning = true, want the session never to start")
			}
			if h.app.ui.download.downloadBtn.Disabled() {
				t.Error("download button disabled after rejected input")
			}
			if h.window.Canvas().Overlays().Top() == nil {
				t.Error("no error dialog shown for rejected input")
			}
			if h.runs() != 0 {
				t.Errorf("yt-dlp started %d times, want 0", h.runs())
			}
		})
	}
}

// ── runYtDlp ─────────────────────────────────────────────────────────────────

func TestRunYtDlpSuccessRecordsHistory(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	const url = "https://example.com/v"

	paths := h.app.runYtDlp(context.Background(), h.app.newDownloadRequest(url, h.saveDir, "", ""), queueItem{url: url}, itemRun{position: 1, total: 1}, nil).FinalPaths

	want := filepath.Join(h.saveDir, "GoVid_Fake Video.mp4")
	if !slices.Equal(paths, []string{want}) {
		t.Fatalf("runYtDlp() = %q, want %q; log:\n%s", paths, want, h.joinedLogs())
	}
	if got := h.app.ui.download.status.Text; got != "Status: Success!" {
		t.Errorf("status = %q, want Status: Success!", got)
	}
	logs := h.joinedLogs()
	for _, line := range []string{"DOWNLOAD COMPLETE", "Downloaded: 10.00MiB", "Format:     WEBM → MP4 (converted)"} {
		if !strings.Contains(logs, line) {
			t.Errorf("log missing %q:\n%s", line, logs)
		}
	}
	if h.app.sessionFailed.Load() {
		t.Error("sessionFailed set after a successful download")
	}

	entries := h.history(t)
	if len(entries) != 1 || entries[0].URL != url || entries[0].FinalFilename != "GoVid_Fake Video.mp4" || entries[0].Format != "MP4" {
		t.Errorf("history = %+v, want one entry for the download", entries)
	}
}

func TestRunYtDlpFailure(t *testing.T) {
	h := newDownloadHarness(t, "fail")

	paths := h.app.runYtDlp(context.Background(), h.app.newDownloadRequest("https://example.com/v", h.saveDir, "", ""), queueItem{url: "https://example.com/v"}, itemRun{position: 1, total: 1}, nil).FinalPaths

	if paths != nil {
		t.Errorf("runYtDlp() = %q, want nil", paths)
	}
	if got := h.app.ui.download.status.Text; got != "Status: Failed. Check output below." {
		t.Errorf("status = %q", got)
	}
	if !h.app.sessionFailed.Load() {
		t.Error("sessionFailed not set after a failed download")
	}
	if !strings.Contains(h.joinedLogs(), "ERROR: fake tool failure") {
		t.Errorf("yt-dlp error not forwarded to the log:\n%s", h.joinedLogs())
	}
	if entries := h.history(t); len(entries) != 0 {
		t.Errorf("history = %+v, want no entries for a failed download", entries)
	}
}

func TestRunYtDlpExtractorErrorSuggestsUpdate(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-extractor-error")

	h.app.runYtDlp(context.Background(), h.app.newDownloadRequest("https://example.com/v", h.saveDir, "", ""), queueItem{url: "https://example.com/v"}, itemRun{position: 1, total: 1}, nil)

	if !strings.Contains(h.joinedLogs(), ytDlpUpdateHint) {
		t.Errorf("log missing the update hint:\n%s", h.joinedLogs())
	}
}

func TestRunYtDlpOtherFailureGivesNoUpdateHint(t *testing.T) {
	h := newDownloadHarness(t, "fail")

	h.app.runYtDlp(context.Background(), h.app.newDownloadRequest("https://example.com/v", h.saveDir, "", ""), queueItem{url: "https://example.com/v"}, itemRun{position: 1, total: 1}, nil)

	if strings.Contains(h.joinedLogs(), ytDlpUpdateHint) {
		t.Errorf("log has the update hint for an unrelated error:\n%s", h.joinedLogs())
	}
}

func TestRunYtDlpCancel(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-hang")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.setHook(func(line string) {
		if strings.Contains(line, "%") {
			cancel()
		}
	})

	paths := h.app.runYtDlp(ctx, h.app.newDownloadRequest("https://example.com/v", h.saveDir, "", ""), queueItem{url: "https://example.com/v"}, itemRun{position: 1, total: 1}, nil).FinalPaths

	if paths != nil {
		t.Errorf("runYtDlp() = %q, want nil", paths)
	}
	if got := h.app.ui.download.status.Text; got != "Status: Canceled." {
		t.Errorf("status = %q, want Status: Canceled.", got)
	}
	if !strings.Contains(h.joinedLogs(), "DOWNLOAD ABORTED") {
		t.Errorf("log missing abort summary:\n%s", h.joinedLogs())
	}
	if h.app.sessionFailed.Load() {
		t.Error("sessionFailed set after a user cancellation")
	}
	if entries := h.history(t); len(entries) != 0 {
		t.Errorf("history = %+v, want no entries for a canceled download", entries)
	}
	if files := h.savedFiles(t); len(files) != 0 {
		t.Errorf("saved files = %q, want the partial file removed", files)
	}
}

func TestShutdownPausesTheBatchAndSavesTheQueue(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-hang")
	h.app.ui.download.batchMode.SetChecked(true)
	h.app.ui.download.entry.SetText("https://example.com/a\nhttps://example.com/b")

	quit := make(chan struct{})
	var once sync.Once
	h.setHook(func(line string) {
		if strings.Contains(line, "1.0%") {
			once.Do(func() {
				h.app.Shutdown(func() { close(quit) })
			})
		}
	})

	h.app.startDownload()
	select {
	case <-quit:
	case <-time.After(30 * time.Second):
		t.Fatalf("Shutdown never called quit; log:\n%s", h.joinedLogs())
	}

	if h.app.isRunning.Load() {
		t.Error("session still running when quit was called")
	}
	if h.runs() != 1 {
		t.Errorf("yt-dlp started %d times, want 1 (the rest of the queue abandoned)", h.runs())
	}
	files := h.savedFiles(t)
	if len(files) != 1 {
		t.Fatalf("saved files = %q, want the partial file kept to resume", files)
	}
	saved, err := h.app.queueStore.Load()
	if err != nil || len(saved) != 2 {
		t.Fatalf("saved queue = %+v, %v; want both items", saved, err)
	}
	if saved[0].Status != savedPaused || saved[1].Status != savedWaiting || saved[1].URL != "https://example.com/b" {
		t.Errorf("saved queue = %+v, want a paused, b waiting", saved)
	}
	if !strings.Contains(files[0], saved[0].DownloadID) || saved[0].Request.Format != formatMP4 {
		t.Errorf("saved item %+v does not match the partial file %s", saved[0], files[0])
	}
}

// ── startDownload sessions ───────────────────────────────────────────────────

func TestStartDownloadSingleURL(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.entry.SetText("  https://example.com/v  ")

	h.startAndWait(t)

	if files := h.savedFiles(t); !slices.Equal(files, []string{"GoVid_Fake Video.mp4"}) {
		t.Errorf("saved files = %q", files)
	}
	if entries := h.history(t); len(entries) != 1 || entries[0].URL != "https://example.com/v" {
		t.Errorf("history = %+v, want the trimmed URL recorded once", entries)
	}
	btn := h.app.ui.download.downloadBtn
	if btn.Disabled() || btn.Text != "Download Now!" {
		t.Errorf("download button = %q (disabled=%v), want enabled Download Now!", btn.Text, btn.Disabled())
	}
	if h.app.RequestCancel() {
		t.Error("cancel func still registered after the session finished")
	}
}

func TestStartDownloadLabelsDownscaledVideoWithItsHeight(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download") // the fake video is 720p
	h.app.ui.download.quality.SetSelected(quality1080p)
	h.app.ui.download.entry.SetText("https://example.com/v")

	h.startAndWait(t)

	if files := h.savedFiles(t); !slices.Equal(files, []string{"GoVid_Fake Video_720p.mp4"}) {
		t.Errorf("saved files = %q, want the 720p video labelled _720p", files)
	}
	const message = `"Fake Video": 1080p isn't available for this video; downloading 720p (the best there is).`
	if !strings.Contains(h.joinedLogs(), message) {
		t.Errorf("log missing the downscale notice:\n%s", h.joinedLogs())
	}
	var shown []string
	fyne.DoAndWait(func() {
		for _, n := range h.app.uiManager.notices {
			shown = append(shown, n.text)
		}
	})
	if !slices.Equal(shown, []string{message}) {
		t.Errorf("notices = %q, want the downscale notice", shown)
	}
}

func TestStartDownloadAudioHasNoQualityLabel(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.format.SetSelected(formatMP3)
	h.app.ui.download.quality.SetSelected(quality1080p)
	h.app.ui.download.entry.SetText("https://example.com/v")

	h.startAndWait(t)

	if files := h.savedFiles(t); !slices.Equal(files, []string{"GoVid_Fake Video.mp3"}) {
		t.Errorf("saved files = %q, want an MP3 without a quality label", files)
	}
	if strings.Contains(h.joinedLogs(), "isn't available") {
		t.Errorf("audio download got a resolution notice:\n%s", h.joinedLogs())
	}
}

func TestStartDownloadFailureOffersRetry(t *testing.T) {
	h := newDownloadHarness(t, "fail")
	h.app.ui.download.entry.SetText("https://example.com/v")

	h.startAndWait(t)

	btn := h.app.ui.download.downloadBtn
	if btn.Disabled() || btn.Text != "Retry" {
		t.Errorf("download button = %q (disabled=%v), want enabled Retry", btn.Text, btn.Disabled())
	}
}

func TestStartDownloadBatchQueue(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.batchMode.SetChecked(true)
	h.app.ui.download.entry.SetText("https://example.com/a\n\n  https://example.com/b  \n")

	h.startAndWait(t)

	if h.runs() != 2 {
		t.Errorf("yt-dlp started %d times, want 2 (blank lines skipped)", h.runs())
	}
	// Both URLs resolve to the same title, so the second is renamed rather than overwriting.
	if files, want := h.savedFiles(t), []string{"GoVid_Fake Video 1.mp4", "GoVid_Fake Video.mp4"}; !slices.Equal(files, want) {
		t.Errorf("saved files = %q, want %q", files, want)
	}
	entries := h.history(t)
	if len(entries) != 2 || entries[0].URL != "https://example.com/a" || entries[1].URL != "https://example.com/b" {
		t.Errorf("history = %+v, want both URLs in queue order", entries)
	}
	logs := h.joinedLogs()
	for _, line := range []string{"[SYSTEM] Batch mode: 2 URLs queued.", "URL 1 of 2", "URL 2 of 2"} {
		if !strings.Contains(logs, line) {
			t.Errorf("log missing %q:\n%s", line, logs)
		}
	}
}

func TestStartDownloadBatchCancelSkipsCurrentItem(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-hang-once")
	h.app.ui.download.batchMode.SetChecked(true)
	h.app.ui.download.entry.SetText("https://example.com/stalled\nhttps://example.com/next")

	// Cancel the first (stalled) item as soon as it reports progress.
	var once sync.Once
	h.setHook(func(line string) {
		if strings.Contains(line, "1.0%") {
			once.Do(func() { h.app.RequestCancel() })
		}
	})

	h.startAndWait(t)

	if h.runs() != 2 {
		t.Errorf("yt-dlp started %d times, want 2 (queue continues after skip)", h.runs())
	}
	if files := h.savedFiles(t); !slices.Equal(files, []string{"GoVid_Fake Video.mp4"}) {
		t.Errorf("saved files = %q, want only the second item", files)
	}
	entries := h.history(t)
	if len(entries) != 1 || entries[0].URL != "https://example.com/next" {
		t.Errorf("history = %+v, want only the second URL", entries)
	}
	if !strings.Contains(h.joinedLogs(), "DOWNLOAD ABORTED") {
		t.Errorf("log missing abort summary for the skipped item:\n%s", h.joinedLogs())
	}
}
