package main

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

func TestNewDownloadIDIsUniqueAndIncreasing(t *testing.T) {
	previous := int64(0)
	seen := map[string]bool{}
	for range 1000 {
		id := newDownloadID()
		if !regexp.MustCompile(`^GOVID\d+$`).MatchString(id) || seen[id] {
			t.Fatalf("newDownloadID() = %q (seen before: %v)", id, seen[id])
		}
		seen[id] = true
		number, _ := strconv.ParseInt(strings.TrimPrefix(id, "GOVID"), 10, 64)
		if number <= previous {
			t.Fatalf("%q is not larger than the one before", id)
		}
		previous = number
	}
}

func TestBuildArgsResumesPartialFiles(t *testing.T) {
	engine := NewDownloadEngine("yt-dlp", "")
	req := DownloadRequest{URL: "u", SavePath: "s", Format: formatMP4, DownloadID: "GOVID42"}

	args := engine.BuildArgs(req)
	if args.DownloadID != "GOVID42" || !strings.Contains(argAfter(args.Args, "-o"), "GOVID42") {
		t.Errorf("BuildArgs ignored the request's download ID: %q, %q", args.DownloadID, argAfter(args.Args, "-o"))
	}
	if !slices.Contains(args.Args, "--continue") || slices.Contains(args.Args, "--no-part") || slices.Contains(args.Args, "--no-continue") {
		t.Errorf("args = %q, want --continue and .part files", args.Args)
	}

	req.Live = true
	live := engine.BuildArgs(req).Args
	if !slices.Contains(live, "--no-part") || slices.Contains(live, "--continue") {
		t.Errorf("live args = %q, want --no-part: a stopped recording must not be left as a .part file", live)
	}
}

func TestIsPartialFile(t *testing.T) {
	for name, want := range map[string]bool{
		"GoVid_A_GOVID1.f137.mp4.part":        true,
		"GoVid_A_GOVID1.f137.mp4.part-Frag12": true,
		"GoVid_A_GOVID1.f137.mp4.ytdl":        true,
		"GoVid_A_GOVID1.temp.mp4":             true,
		"GoVid_A_GOVID1.mp4":                  false,
		"GoVid_A_GOVID1.en.srt":               false,
		"GoVid_Particle physics_GOVID1.mp4":   false,
		"GoVid_A temperature log_GOVID1.webm": false,
	} {
		if got := isPartialFile(name); got != want {
			t.Errorf("isPartialFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestFinalizeFilesLeavesPartialFiles(t *testing.T) {
	dir := t.TempDir()
	const id = "GOVID7"
	for _, name := range []string{"GoVid_A_" + id + ".mp4", "GoVid_A_" + id + ".f251.webm.part", "GoVid_A_" + id + ".f251.webm.ytdl"} {
		touch(t, filepath.Join(dir, name))
	}

	paths := NewDownloadEngine("", "").FinalizeFiles(dir, id, func(string, color.Color) {})

	if len(paths) != 1 || filepath.Base(paths[0]) != "GoVid_A.mp4" {
		t.Errorf("FinalizeFiles() = %q, want only the finished file", paths)
	}
}

func TestRunPausedKeepsPartialFilesWithoutFinalizing(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-resumable")
	saveDir := t.TempDir()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	var once sync.Once
	rec := &engineRecorder{onLog: func(line string) {
		if strings.Contains(line, "30.0%") {
			once.Do(func() { cancel(errPaused) })
		}
	}}

	result := NewDownloadEngine(fakeToolPath(t), "").Run(ctx, DownloadRequest{
		URL: "https://example.com/v", SavePath: saveDir, Format: formatMP4, DownloadID: "GOVID99",
	}, DownloadOptions{Index: 1, Total: 1}, rec.callbacks())

	if !result.Paused || result.Err != nil || len(result.FinalPaths) != 0 {
		t.Fatalf("Run() = paused %v, err %v, paths %q; want a pause", result.Paused, result.Err, result.FinalPaths)
	}
	files, _ := os.ReadDir(saveDir)
	if len(files) != 1 || !strings.HasSuffix(files[0].Name(), "_GOVID99.mp4.part") {
		t.Errorf("save folder = %v, want the .part file kept under its token", files)
	}
}

func TestQueueModelPauseAndResume(t *testing.T) {
	queue := NewQueueModel([]queueItem{{url: "a"}, {url: "b"}, {url: "c"}})
	first, _, _ := queue.Next()
	queue.SetStatus(first, queuePaused)
	second, _, _ := queue.Next()
	queue.SetStatus(second, queuePaused)

	if !queue.HasPaused() || queue.Summary() != "0 of 3 done, 2 paused" {
		t.Errorf("summary = %q", queue.Summary())
	}
	if !queue.ResumeAll() {
		t.Fatal("ResumeAll() found nothing to resume")
	}
	// Resumed items go before the item that was still waiting, in order.
	var order []string
	for {
		_, item, ok := queue.Next()
		if !ok {
			break
		}
		order = append(order, item.url)
	}
	if !slices.Equal(order, []string{"a", "b", "c"}) {
		t.Errorf("order after resuming = %q, want a, b, c", order)
	}

	for _, entry := range queue.Snapshot() {
		if entry.item.downloadID == "" {
			t.Errorf("%s has no download ID", entry.item.url)
		}
	}
	queue.SetStatus(first, queuePaused)
	if item, ok := queue.RemovePaused(first); !ok || item.url != "a" || queue.Len() != 2 {
		t.Errorf("RemovePaused() = %+v, %v; %d left", item, ok, queue.Len())
	}
	if _, ok := queue.RemovePaused(second); ok {
		t.Error("removed an item that was not paused")
	}
}

// pauseOnceAt pauses the harness's download the first time a progress line
// reaches percent.
func pauseOnceAt(h *downloadHarness, percent string) {
	var once sync.Once
	h.setHook(func(line string) {
		if strings.Contains(line, percent+"%") {
			once.Do(func() { h.app.requestPause() })
		}
	})
}

// waitForPaused waits until the queue has a paused item and nothing else
// running.
func waitForPaused(t *testing.T, h *downloadHarness) *QueueModel {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if queue := h.app.queue.Load(); queue != nil && h.app.awaitingResume.Load() {
			return queue
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the download was never paused; log:\n%s", h.joinedLogs())
	return nil
}

// waitForSessionEnd waits until the harness's session has finished.
func waitForSessionEnd(t *testing.T, h *downloadHarness) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for h.app.isRunning.Load() {
		if time.Now().After(deadline) {
			t.Fatalf("the session did not finish; log:\n%s", h.joinedLogs())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// resumedFrom returns the byte the fake yt-dlp said it resumed at, or -1.
func resumedFrom(logs string) int {
	match := regexp.MustCompile(`Resuming download at byte (\d+)`).FindStringSubmatch(logs)
	if match == nil {
		return -1
	}
	n, _ := strconv.Atoi(match[1])
	return n
}

func TestPauseAndResumeContinuesTheDownload(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-resumable")
	runs := useFakeArgs(t)
	h.app.ui.download.entry.SetText("https://example.com/v")
	pauseOnceAt(h, "50.0")

	h.app.startDownload()
	waitForPaused(t, h)
	h.app.pauseOrResume()
	waitForSessionEnd(t, h)

	if at := resumedFrom(h.joinedLogs()); at < 4*1024*1024 {
		t.Errorf("resumed at byte %d, want about half of the 10 MiB", at)
	}
	var downloads [][]string
	for _, run := range runs() {
		if argAfter(run, "-o") != "" {
			downloads = append(downloads, run)
		}
	}
	if len(runs()) != 3 {
		t.Errorf("yt-dlp ran %d times, want one probe and two downloads: the resume reuses the probe", len(runs()))
	}
	if len(downloads) != 2 || argAfter(downloads[0], "-o") != argAfter(downloads[1], "-o") {
		t.Errorf("downloads = %q, want two runs writing the same names", downloads)
	}
	if files := h.savedFiles(t); !slices.Equal(files, []string{"GoVid_Fake Video.mp4"}) {
		t.Errorf("saved files = %q, want the finished download only", files)
	}
	if entries := h.history(t); len(entries) != 1 {
		t.Errorf("history has %d entries, want 1", len(entries))
	}
}

func TestCancellingAPausedDownloadRemovesItsFiles(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-resumable")
	h.app.ui.download.entry.SetText("https://example.com/v")
	pauseOnceAt(h, "30.0")

	h.app.startDownload()
	waitForPaused(t, h)
	if files := h.savedFiles(t); len(files) != 1 || !strings.HasSuffix(files[0], ".part") {
		t.Fatalf("saved files while paused = %q, want the .part file", files)
	}
	h.app.RequestCancel()
	waitForSessionEnd(t, h)

	if files := h.savedFiles(t); len(files) != 0 {
		t.Errorf("saved files = %q, want the partial download removed", files)
	}
	if saved, _ := h.app.queueStore.Load(); len(saved) != 0 {
		t.Errorf("queue.json holds %+v after a cancel", saved)
	}
}

// Removing a paused row runs on the UI thread, which must not wait for a
// JavaScript runtime search that is still running: it can take seconds
// (CR-10).
func TestDiscardPausedItemDoesNotWaitForTheRuntimeSearch(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	part := filepath.Join(h.saveDir, "GoVid_Fake Video_GOVID123.mp4.part")
	if err := os.WriteFile(part, []byte("partial"), 0644); err != nil {
		t.Fatal(err)
	}
	queue := NewQueueModel([]queueItem{{url: "https://example.com/v", downloadID: "GOVID123", request: &DownloadRequest{SavePath: h.saveDir}}})
	id := queue.Snapshot()[0].id
	queue.SetStatus(id, queuePaused)
	h.app.queue.Store(queue)

	h.app.depSvc.runtimeMu.Lock() // as while the runtime search runs
	returned := make(chan struct{})
	go func() {
		h.app.discardPausedItem(id)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		h.app.depSvc.runtimeMu.Unlock()
		t.Fatal("discardPausedItem waited for the runtime search")
	}
	h.app.depSvc.runtimeMu.Unlock()

	deadline := time.Now().Add(10 * time.Second)
	for len(h.savedFiles(t)) > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if files := h.savedFiles(t); len(files) != 0 {
		t.Errorf("saved files = %q, want the partial download removed", files)
	}
}

func TestQueueStoreRoundTrip(t *testing.T) {
	store := &QueueStore{filePath: filepath.Join(t.TempDir(), queueFileName)}
	if saved, err := store.Load(); err != nil || saved != nil {
		t.Fatalf("Load() with no file = %v, %v", saved, err)
	}
	item := queueItem{url: "https://example.com/v", title: "Video", videoID: "v1", extractor: "Fake", downloadID: "GOVID5", formatID: "137+251",
		request: &DownloadRequest{SavePath: "D:/Videos", Format: formatMKV, Quality: quality720p, TrimStart: "10", CookiesPath: "secret.txt", Subtitles: subtitlesEmbed}}

	if err := store.Save([]savedQueueItem{saveItem(item, savedPaused)}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.filePath)
	if strings.Contains(string(data), "secret.txt") {
		t.Error("queue.json holds the cookies file")
	}
	saved, err := store.Load()
	if err != nil || len(saved) != 1 || saved[0].Status != savedPaused {
		t.Fatalf("Load() = %+v, %v", saved, err)
	}
	restored := saved[0].queueItem()
	want := *item.request
	want.CookiesPath = ""
	if restored.url != item.url || restored.downloadID != "GOVID5" || restored.formatID != "137+251" || !reflect.DeepEqual(*restored.request, want) {
		t.Errorf("restored %+v (request %+v), want %+v", restored, *restored.request, item)
	}

	if err := store.Save(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.filePath); !os.IsNotExist(err) {
		t.Error("saving an empty queue left the file")
	}
}

// savedPartial saves one paused item for the harness, with its .part file
// half written, as an earlier GoVid would have left it.
func savedPartial(t *testing.T, h *downloadHarness, formatID string) savedQueueItem {
	t.Helper()
	item := queueItem{url: "https://example.com/v", title: "Fake Video", downloadID: "GOVID123", formatID: formatID,
		request: &DownloadRequest{SavePath: h.saveDir, Format: formatMP4, Quality: qualityBest}}
	part := filepath.Join(h.saveDir, "GoVid_Fake Video_GOVID123.mp4.part")
	if err := os.WriteFile(part, make([]byte, 5*1024*1024), 0644); err != nil {
		t.Fatal(err)
	}
	saved := saveItem(item, savedPaused)
	if err := h.app.queueStore.Save([]savedQueueItem{saved}); err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestRestoredQueueResumesAtTheNextStart(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-resumable")
	savedPartial(t, h, "old-v+old-a")
	var asked int
	h.app.askRestoreQueue = func(count int, answer func(bool)) {
		asked = count
		answer(true)
	}

	h.app.offerQueueRestore()
	waitForSessionEnd(t, h)

	if asked != 1 {
		t.Errorf("asked about %d downloads, want 1", asked)
	}
	logs := h.joinedLogs()
	if at := resumedFrom(logs); at != 5*1024*1024 {
		t.Errorf("resumed at byte %d, want the saved 5 MiB", at)
	}
	if !strings.Contains(logs, "now picks other formats (fake-v+fake-a, was old-v+old-a)") {
		t.Errorf("the format change was not logged:\n%s", logs)
	}
	if files := h.savedFiles(t); !slices.Equal(files, []string{"GoVid_Fake Video.mp4"}) {
		t.Errorf("saved files = %q", files)
	}
	if saved, _ := h.app.queueStore.Load(); len(saved) != 0 {
		t.Errorf("queue.json still holds %+v", saved)
	}
}

func TestDiscardingTheRestoredQueueRemovesItsFiles(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-resumable")
	savedPartial(t, h, "")
	h.app.askRestoreQueue = func(_ int, answer func(bool)) { answer(false) }

	h.app.offerQueueRestore()
	deadline := time.Now().Add(10 * time.Second)
	for len(h.savedFiles(t)) > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if files := h.savedFiles(t); len(files) != 0 {
		t.Errorf("saved files = %q, want the partial download removed", files)
	}
	if saved, _ := h.app.queueStore.Load(); len(saved) != 0 {
		t.Errorf("queue.json still holds %+v", saved)
	}
}

func TestShowPauseControl(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	button := h.app.ui.download.pauseBtn
	if !button.Disabled() || button.Text != "Pause" {
		t.Fatalf("Pause button starts as %q, disabled %v", button.Text, button.Disabled())
	}

	h.app.showPauseControl(pauseControl{label: "Resume", enabled: true})
	if button.Disabled() || button.Text != "Resume" {
		t.Errorf("button = %q, disabled %v; want Resume, enabled", button.Text, button.Disabled())
	}
	h.app.showPauseControl(pauseControl{label: "Pause"})
	if !button.Disabled() || button.Text != "Pause" {
		t.Errorf("button = %q, disabled %v; want Pause, disabled", button.Text, button.Disabled())
	}
}
