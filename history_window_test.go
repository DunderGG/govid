package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// historyFixture returns three entries, oldest first; only the newest one's
// file is on disk.
func historyFixture(t *testing.T) []DownloadHistoryEntry {
	t.Helper()
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "GoVid_Cats.mp4"))
	return []DownloadHistoryEntry{
		{URL: "https://example.com/dogs", OriginalTitle: "Dogs", FinalFilename: "GoVid_Dogs.mkv", SavedPath: dir, Format: "MKV", Quality: "720p", DownloadedAt: "2026-10-01 10:00:00"},
		{URL: "https://example.com/song", OriginalTitle: "Song", FinalFilename: "GoVid_Song.mp3", SavedPath: dir, Format: "MP3", Quality: "Best Quality", DownloadedAt: "2026-10-02 10:00:00"},
		{URL: "https://example.com/cats", OriginalTitle: "Cats", FinalFilename: "GoVid_Cats.mp4", SavedPath: dir, Format: "MP4", Quality: "1080p", DownloadedAt: "2026-10-03 10:00:00"},
	}
}

func TestHistoryViewOrderMissingAndSearch(t *testing.T) {
	view := newHistoryView(historyFixture(t), fileExists)

	var titles []string
	var missing []bool
	for id := range view.shown {
		entry, gone := view.shownEntry(id)
		titles = append(titles, entry.OriginalTitle)
		missing = append(missing, gone)
	}
	if !slices.Equal(titles, []string{"Cats", "Song", "Dogs"}) || !slices.Equal(missing, []bool{false, true, true}) {
		t.Errorf("view = %q missing %v, want newest first with only Cats on disk", titles, missing)
	}

	for query, want := range map[string]int{"": 3, "CAT": 1, "mp3": 1, "example.com/dogs": 1, "GoVid_": 3, "nothing": 0} {
		view.search(query)
		if len(view.shown) != want {
			t.Errorf("search(%q) shows %d entries, want %d", query, len(view.shown), want)
		}
	}
	view.search("cat")
	if got := view.count(); got != "1 of 3 downloads" {
		t.Errorf("count() = %q", got)
	}
}

func TestHistoryDetails(t *testing.T) {
	entry := DownloadHistoryEntry{DownloadedAt: "2026-10-03 10:00:00", Format: "MP4", Quality: "1080p", FinalFilename: "GoVid_Cats.mp4"}
	if got, want := historyDetails(entry, false), "2026-10-03 10:00:00  ·  MP4 / 1080p  ·  GoVid_Cats.mp4"; got != want {
		t.Errorf("historyDetails() = %q, want %q", got, want)
	}
	if got := historyDetails(entry, true); got != "2026-10-03 10:00:00  ·  MP4 / 1080p  ·  GoVid_Cats.mp4  (file not found)" {
		t.Errorf("historyDetails(missing) = %q", got)
	}
}

func TestHistoryRowActions(t *testing.T) {
	_ = test.NewApp()
	entries := historyFixture(t)
	var readded, copied, revealed []string
	actions := historyActions{
		readd:   func(url string) { readded = append(readded, url) },
		copyURL: func(url string) { copied = append(copied, url) },
		reveal:  func(path string) { revealed = append(revealed, path) },
	}
	row := newHistoryRow()

	row.show(entries[2], false, actions)
	test.Tap(row.readd)
	test.Tap(row.copyURL)
	test.Tap(row.reveal)
	if !slices.Equal(readded, []string{entries[2].URL}) || !slices.Equal(copied, []string{entries[2].URL}) ||
		!slices.Equal(revealed, []string{entries[2].FilePath()}) {
		t.Errorf("actions got readd %q, copy %q, reveal %q", readded, copied, revealed)
	}
	if row.title.Text != "Cats" || row.title.Importance != widget.MediumImportance {
		t.Errorf("title = %q (importance %v)", row.title.Text, row.title.Importance)
	}

	row.show(entries[0], true, actions)
	if !row.reveal.Disabled() || row.title.Importance != widget.LowImportance {
		t.Error("a row whose file is gone should be greyed out with Show in folder disabled")
	}
}

func TestReaddHistoryURLAddsToTheField(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.entry.SetText("https://example.com/first")

	h.app.uiManager.readdHistoryURL("https://example.com/again")

	if got := h.app.ui.download.entry.Text; got != "https://example.com/first\nhttps://example.com/again" {
		t.Errorf("field = %q", got)
	}
	if !h.app.ui.download.batchMode.Checked {
		t.Error("batch mode not switched on for a second URL")
	}
}

func TestShowHistoryListsEntries(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	data := historyFixture(t)
	for _, entry := range data {
		if err := h.app.historySvc.AppendAll(DownloadRecord{URL: entry.URL, FinalPaths: []string{entry.FilePath()}, Title: entry.OriginalTitle}); err != nil {
			t.Fatal(err)
		}
	}

	loaded := h.app.uiManager.openHistory()
	window := h.app.uiManager.historyWindow
	if window == nil {
		t.Fatal("History window not opened")
	}
	defer window.Close()
	waitLoaded(t, loaded)

	if list := historyListIn(t, window); list.Length() != 3 {
		t.Fatalf("history list has %d rows, want 3", list.Length())
	}
}

// The window opens before the history has been read and its files checked,
// which can take seconds on a disconnected drive or network share, so the
// UI thread does not wait for them (CR-10).
func TestShowHistoryOpensBeforeTheHistoryLoads(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	entries := historyFixture(t)
	release := make(chan struct{})
	h.app.uiManager.onLoadHistory = func() ([]DownloadHistoryEntry, error) {
		<-release
		return entries, nil
	}

	opened := make(chan (<-chan struct{}))
	go func() { opened <- h.app.uiManager.openHistory() }()
	var loaded <-chan struct{}
	select {
	case loaded = <-opened:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("opening the History window waited for the history to load")
	}
	window := h.app.uiManager.historyWindow
	defer window.Close()

	list := historyListIn(t, window)
	status := historyStatusIn(t, window)
	if list.Length() != 0 || !strings.HasPrefix(status.Text, "Loading") || !status.Visible() {
		t.Errorf("while loading: %d rows, status %q (visible %v)", list.Length(), status.Text, status.Visible())
	}
	if clearBtn := findButton(t, window.Content(), "Clear History"); !clearBtn.Disabled() {
		t.Error("Clear History is enabled before the history has loaded")
	}

	close(release)
	waitLoaded(t, loaded)
	if list.Length() != 3 || status.Visible() {
		t.Errorf("after loading: %d rows, status %q (visible %v)", list.Length(), status.Text, status.Visible())
	}
}

func TestHistoryWindowSaysWhyTheHistoryFailedToLoad(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.uiManager.onLoadHistory = func() ([]DownloadHistoryEntry, error) {
		return nil, errors.New("unexpected end of JSON input")
	}

	loaded := h.app.uiManager.openHistory()
	window := h.app.uiManager.historyWindow
	defer window.Close()
	waitLoaded(t, loaded)

	if status := historyStatusIn(t, window); !strings.Contains(status.Text, "unexpected end of JSON input") {
		t.Errorf("status = %q, want the load error", status.Text)
	}
	if findButton(t, window.Content(), "Clear History").Disabled() {
		t.Error("Clear History is disabled, so a damaged history cannot be cleared")
	}
}

func TestHistoryClosedWhileLoadingIsNotFilled(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	entries := historyFixture(t)
	release := make(chan struct{})
	h.app.uiManager.onLoadHistory = func() ([]DownloadHistoryEntry, error) {
		<-release
		return entries, nil
	}

	loaded := h.app.uiManager.openHistory()
	window := h.app.uiManager.historyWindow
	list := historyListIn(t, window)
	window.Close()
	close(release)
	waitLoaded(t, loaded)

	if list.Length() != 0 || h.app.uiManager.historyWindow != nil {
		t.Errorf("closed window filled with %d rows, historyWindow = %v", list.Length(), h.app.uiManager.historyWindow)
	}
}

// waitLoaded waits for openHistory's channel to close.
func waitLoaded(t *testing.T, loaded <-chan struct{}) {
	t.Helper()
	select {
	case <-loaded:
	case <-time.After(10 * time.Second):
		t.Fatal("the history did not load")
	}
}

// historyListIn returns the list in the History window.
func historyListIn(t *testing.T, window fyne.Window) *widget.List {
	t.Helper()
	var list *widget.List
	walkObjects(window.Content(), func(obj fyne.CanvasObject) {
		if found, ok := obj.(*widget.List); ok {
			list = found
		}
	})
	if list == nil {
		t.Fatal("no list in the History window")
	}
	return list
}

// historyStatusIn returns the label shown over the History window's list.
func historyStatusIn(t *testing.T, window fyne.Window) *widget.Label {
	t.Helper()
	var status *widget.Label
	walkObjects(window.Content(), func(obj fyne.CanvasObject) {
		if label, ok := obj.(*widget.Label); ok && label.TextStyle.Italic {
			status = label
		}
	})
	if status == nil {
		t.Fatal("no status label in the History window")
	}
	return status
}

func TestTurningHistoryOffOffersToClearIt(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	if err := h.app.historySvc.AppendAll(DownloadRecord{URL: "https://example.com/v"}); err != nil {
		t.Fatal(err)
	}
	h.app.uiManager.showPreferences()
	prefs := h.app.uiManager.prefsWindow
	defer prefs.Close()
	prefs.Resize(fyne.NewSize(600, 700))

	h.app.ui.prefs.keepHistory.SetChecked(false)
	test.Tap(findButton(t, prefs.Canvas().Overlays().Top(), "Delete history"))

	if entries := h.history(t); len(entries) != 0 {
		t.Errorf("history = %+v, want it deleted", entries)
	}
	if _, err := os.Stat(h.app.historySvc.filePath); err != nil {
		t.Errorf("history file: %v", err)
	}
}
