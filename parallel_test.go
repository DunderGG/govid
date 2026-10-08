package main

import (
	"context"
	"image/color"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

func TestSimultaneousDownloads(t *testing.T) {
	for setting, want := range map[string]int{"1": 1, "2": 2, "3": 3, "": 1, "4": 1, "x": 1} {
		if got := simultaneousDownloads(setting); got != want {
			t.Errorf("simultaneousDownloads(%q) = %d, want %d", setting, got, want)
		}
	}
}

func TestQueueModelOverallProgress(t *testing.T) {
	queue := NewQueueModel([]queueItem{{url: "a"}, {url: "b"}, {url: "c"}, {url: "d"}})
	a, _, _ := queue.Next()
	queue.SetStatus(a, queueDone)
	b, _, _ := queue.Next()
	queue.SetStatus(b, queueDownloading)
	queue.SetProgress(b, 0.5)
	c, _, _ := queue.Next()
	queue.SetStatus(c, queueDownloading)

	if got := queue.OverallProgress(); got != 1.5/4 {
		t.Errorf("OverallProgress() = %v, want 1.5 of 4", got)
	}
	if got := queue.ActiveCount(); got != 2 {
		t.Errorf("ActiveCount() = %d, want 2", got)
	}
	if entries := queue.Snapshot(); entries[1].progress != 0.5 || entries[2].progress != 0 {
		t.Errorf("SetProgress set %v and %v, want only b's", entries[1].progress, entries[2].progress)
	}
}

// parallelHarness is a download harness with Simultaneous Downloads set to
// workers, workers starting at once, and urls queued in batch mode.
func parallelHarness(t *testing.T, mode string, workers string, urls ...string) *downloadHarness {
	t.Helper()
	h := newDownloadHarness(t, mode)
	saved := workerStagger
	workerStagger = 0
	t.Cleanup(func() { workerStagger = saved })
	h.app.ui.prefs.simultaneous.SetSelected(workers)
	h.app.ui.download.batchMode.SetChecked(true)
	h.app.ui.download.entry.SetText(strings.Join(urls, "\n"))
	return h
}

func TestThreeDownloadsRunAtOnce(t *testing.T) {
	h := parallelHarness(t, "ytdlp-concurrent", "3", "https://example.com/a", "https://example.com/b", "https://example.com/c")
	peak := useFakeConcurrency(t)

	h.startAndWait(t)

	if got := peak(); got != 3 {
		t.Errorf("at most %d downloads ran at once, want 3", got)
	}
	// All three are called "Fake Video": each still gets a name of its own.
	files := h.savedFiles(t)
	if want := []string{"GoVid_Fake Video 1.mp4", "GoVid_Fake Video 2.mp4", "GoVid_Fake Video.mp4"}; !slices.Equal(files, want) {
		t.Errorf("saved files = %q, want %q", files, want)
	}
	if entries := h.history(t); len(entries) != 3 {
		t.Errorf("history has %d entries, want all 3", len(entries))
	}
	logs := h.joinedLogs()
	for _, prefix := range []string{"[1/3] ", "[2/3] ", "[3/3] "} {
		if !strings.Contains(logs, prefix+"[download] Destination:") {
			t.Errorf("log has no lines marked %q", prefix)
		}
	}
	if queue := h.app.queue.Load(); queue.OverallProgress() != 1 || queue.Summary() != "3 of 3 done" {
		t.Errorf("queue ended at %v: %s", queue.OverallProgress(), queue.Summary())
	}
}

func TestOneDownloadAtATimeByDefault(t *testing.T) {
	h := parallelHarness(t, "ytdlp-concurrent", "1", "https://example.com/a", "https://example.com/b")
	peak := useFakeConcurrency(t)

	h.startAndWait(t)

	if got := peak(); got != 1 {
		t.Errorf("at most %d downloads ran at once, want 1", got)
	}
	if strings.Contains(h.joinedLogs(), "[1/2] ") {
		t.Error("log lines are marked with their item when only one downloads at a time")
	}
}

func TestRateLimitBacksOffToOneWorker(t *testing.T) {
	h := parallelHarness(t, "ytdlp-rate-limited", "3", "https://example.com/a", "https://example.com/b", "https://example.com/c", "https://example.com/d")

	h.startAndWait(t)

	if logs := h.joinedLogs(); strings.Count(logs, "downloading one video at a time for the rest of this session") != 1 {
		t.Errorf("log does not report backing off exactly once:\n%s", logs)
	}
	if got := h.app.queue.Load().Summary(); got != "0 of 4 done, 4 failed" {
		t.Errorf("queue = %s, want every item tried", got)
	}
}

func TestBackOffNeedsRateLimitOrBotCheck(t *testing.T) {
	app := &DownloaderApp{onLogLine: func(string, color.Color) {}, logSvc: NewLogService()}
	limit := &atomic.Int32{}
	limit.Store(3)
	mode := queueMode{parallel: true, limit: limit}

	app.backOff(mode, scanResult{hadTransientErr: true})
	if limit.Load() != 3 {
		t.Errorf("a network error reduced the workers to %d", limit.Load())
	}
	app.backOff(mode, scanResult{accessProblem: accessBotCheck})
	if limit.Load() != 1 {
		t.Errorf("a bot check left %d workers, want 1", limit.Load())
	}
}

// A row can still show Skip just after its item finished and its controls
// were unregistered. The click must not reach the Cancel button's
// function, which by then stops the session or skips the next item.
func TestSkipOfFinishedItemDoesNotCancel(t *testing.T) {
	app := &DownloaderApp{}
	app.SetCancelFunc(func() { t.Error("Skip of a finished item called the Cancel function") })
	skips := 0

	if app.skipItem(1) {
		t.Error("skipItem() of a finished item = true, want false")
	}
	app.registerSkip(2, func() { skips++ })
	if !app.skipItem(2) || app.skipItem(2) {
		t.Error("skipItem() of a running item twice = want true, then false")
	}
	if skips != 1 {
		t.Errorf("item's Skip called %d times, want 1", skips)
	}
}

func TestDiskCheckLeavesSpaceForRunningDownloads(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.freeBytes = func(string) (uint64, error) { return 30 << 20, nil }
	var asked bool
	h.app.askDiskSpace = func(ctx context.Context, prompt diskSpacePrompt) diskSpaceDecision {
		asked = true
		return spaceProceed
	}
	item := queueItem{url: "u", info: &MediaInfo{formatSize: formatSize{FileSize: 10 << 20}}}
	req := DownloadRequest{SavePath: h.saveDir}
	continueAll := false

	h.app.checkDiskSpace(context.Background(), req, false, item, false, &continueAll)
	if asked {
		t.Fatal("asked with room to spare")
	}
	release := h.app.reserveSpace(25 << 20)
	h.app.checkDiskSpace(context.Background(), req, false, item, false, &continueAll)
	release()
	if !asked {
		t.Error("did not leave room for the download already running")
	}
}

func TestProgressLinesStayInPlacePerItem(t *testing.T) {
	_ = test.NewApp()
	manager := NewUIManager(test.NewWindow(nil))
	manager.ui = NewUIWidgets()
	manager.ui.download.logList = container.NewVBox()
	manager.ui.download.output = container.NewScroll(manager.ui.download.logList)
	manager.onLogBufferLimit = func() int { return 200 }

	manager.renderLogLines([]pendingLogLine{
		{text: "[1/2] [download] Destination: a.mp4"},
		{text: "[2/2] [download] Destination: b.mp4"},
		{text: "[1/2] [download]  10.0% of 10.00MiB"},
		{text: "[2/2] [download]  20.0% of 10.00MiB"},
		{text: "[1/2] [download]  30.0% of 10.00MiB"},
		{text: "[2/2] [download]  40.0% of 10.00MiB"},
	})

	var lines []string
	for _, obj := range manager.ui.download.logList.Objects {
		lines = append(lines, obj.(*canvas.Text).Text)
	}
	want := []string{
		"[1/2] [download] Destination: a.mp4",
		"[2/2] [download] Destination: b.mp4",
		"[1/2] [download]  30.0% of 10.00MiB",
		"[2/2] [download]  40.0% of 10.00MiB",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("log view = %q, want %q", lines, want)
	}
}
