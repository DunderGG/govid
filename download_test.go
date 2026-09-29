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
	deadline := time.Now().Add(60 * time.Second)
	for h.app.isRunning.Load() {
		if time.Now().After(deadline) {
			t.Fatalf("download session did not finish; log:\n%s", h.joinedLogs())
		}
		time.Sleep(20 * time.Millisecond)
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

	paths := h.app.runYtDlp(context.Background(), url, h.saveDir, "", "", 1, 1)

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
	if h.app.ppFailed.Load() != 0 {
		t.Error("ppFailed set after a successful download")
	}

	entries := h.history(t)
	if len(entries) != 1 || entries[0].URL != url || entries[0].FinalFilename != "GoVid_Fake Video.mp4" || entries[0].Format != "MP4" {
		t.Errorf("history = %+v, want one entry for the download", entries)
	}
}

func TestRunYtDlpFailure(t *testing.T) {
	h := newDownloadHarness(t, "fail")

	paths := h.app.runYtDlp(context.Background(), "https://example.com/v", h.saveDir, "", "", 1, 1)

	if paths != nil {
		t.Errorf("runYtDlp() = %q, want nil", paths)
	}
	if got := h.app.ui.download.status.Text; got != "Status: Failed. Check output below." {
		t.Errorf("status = %q", got)
	}
	if h.app.ppFailed.Load() != 1 {
		t.Error("ppFailed not set after a failed download")
	}
	if !strings.Contains(h.joinedLogs(), "ERROR: fake tool failure") {
		t.Errorf("yt-dlp error not forwarded to the log:\n%s", h.joinedLogs())
	}
	if entries := h.history(t); len(entries) != 0 {
		t.Errorf("history = %+v, want no entries for a failed download", entries)
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

	paths := h.app.runYtDlp(ctx, "https://example.com/v", h.saveDir, "", "", 1, 1)

	if paths != nil {
		t.Errorf("runYtDlp() = %q, want nil", paths)
	}
	if got := h.app.ui.download.status.Text; got != "Status: Canceled." {
		t.Errorf("status = %q, want Status: Canceled.", got)
	}
	if !strings.Contains(h.joinedLogs(), "DOWNLOAD ABORTED") {
		t.Errorf("log missing abort summary:\n%s", h.joinedLogs())
	}
	if h.app.ppFailed.Load() != 0 {
		t.Error("ppFailed set after a user cancellation")
	}
	if entries := h.history(t); len(entries) != 0 {
		t.Errorf("history = %+v, want no entries for a canceled download", entries)
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
