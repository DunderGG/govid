package main

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestFindDownloaded(t *testing.T) {
	history := []DownloadHistoryEntry{
		{URL: "https://www.youtube.com/watch?v=abc", VideoID: "abc", Extractor: "Youtube", DownloadedAt: "1"},
		{URL: "https://old.example/v", DownloadedAt: "2"}, // recorded before IDs were kept
		{URL: "https://youtu.be/abc", VideoID: "abc", Extractor: "Youtube", DownloadedAt: "3"},
	}
	tests := []struct {
		name               string
		url, id, extractor string
		wantFound          bool
		wantAt             string
	}{
		{"same video, other URL form", "https://www.youtube.com/watch?v=abc&t=42", "abc", "Youtube", true, "3"},
		{"extractor case ignored", "https://m.youtube.com/watch?v=abc", "abc", "youtube", true, "3"},
		{"old entry by exact URL", "https://old.example/v", "", "", true, "2"},
		{"same ID on another site", "https://vimeo.com/abc", "abc", "Vimeo", false, ""},
		{"new video", "https://www.youtube.com/watch?v=xyz", "xyz", "Youtube", false, ""},
	}
	for _, tt := range tests {
		entry, found := findDownloaded(history, tt.url, tt.id, tt.extractor)
		if found != tt.wantFound || entry.DownloadedAt != tt.wantAt {
			t.Errorf("%s: findDownloaded() = %q, %v; want %q, %v", tt.name, entry.DownloadedAt, found, tt.wantAt, tt.wantFound)
		}
	}
}

func TestBuildEntriesPrefersTheRealTitle(t *testing.T) {
	svc := &HistoryService{}
	rec := DownloadRecord{URL: "u", FinalPaths: []string{"/v/GoVid_What_ Is This_.mp4"}, Title: "What? Is This?", VideoID: "abc", Extractor: "Youtube"}

	entries := svc.buildEntries(rec, "now")
	if len(entries) != 1 || entries[0].OriginalTitle != "What? Is This?" || entries[0].VideoID != "abc" || entries[0].Extractor != "Youtube" {
		t.Errorf("buildEntries() = %+v, want the real title, ID, and extractor", entries)
	}

	rec.Title = ""
	if entries := svc.buildEntries(rec, "now"); entries[0].OriginalTitle != "What_ Is This_" {
		t.Errorf("without a title, OriginalTitle = %q, want the one guessed from the file name", entries[0].OriginalTitle)
	}
}

// ── Sessions ─────────────────────────────────────────────────────────────────

// seedHistory records an earlier download of the fake yt-dlp's video, under
// a different URL form.
func seedHistory(t *testing.T, h *downloadHarness) {
	t.Helper()
	err := h.app.historySvc.AppendAll(DownloadRecord{
		URL: "https://www.example.com/watch?v=fakevid", FinalPaths: []string{h.saveDir + "/GoVid_Fake Video.mp4"},
		Format: formatMP4, Title: "Fake Video", VideoID: "fakevid", Extractor: "Fake",
	})
	if err != nil {
		t.Fatal(err)
	}
}

// answerDuplicates makes the harness answer the "Already downloaded" prompt
// with decision, and returns the prompts it was shown.
func answerDuplicates(h *downloadHarness, decision duplicateDecision) func() []duplicatePrompt {
	var mu sync.Mutex
	var prompts []duplicatePrompt
	h.app.askDuplicate = func(_ context.Context, prompt duplicatePrompt) duplicateDecision {
		mu.Lock()
		defer mu.Unlock()
		prompts = append(prompts, prompt)
		return decision
	}
	return func() []duplicatePrompt {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(prompts)
	}
}

func TestStartDownloadRecordsTitleAndVideoID(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.entry.SetText("https://example.com/v")

	h.startAndWait(t)

	entries := h.history(t)
	if len(entries) != 1 || entries[0].OriginalTitle != "Fake Video" || entries[0].VideoID != "fakevid" || entries[0].Extractor != "Fake" {
		t.Errorf("history = %+v, want the probe's title, ID, and extractor", entries)
	}
}

func TestStartDownloadAsksBeforeDownloadingAgain(t *testing.T) {
	for _, tt := range []struct {
		decision duplicateDecision
		wantRuns int
	}{
		{duplicateSkip, 0},
		{duplicateDownload, 1},
	} {
		h := newDownloadHarness(t, "ytdlp-download")
		seedHistory(t, h)
		prompts := answerDuplicates(h, tt.decision)
		h.app.ui.download.entry.SetText("https://example.com/fakevid") // another URL form

		h.startAndWait(t)

		shown := prompts()
		if len(shown) != 1 || shown[0].title != `"Fake Video"` || shown[0].batch || shown[0].previous.VideoID != "fakevid" {
			t.Errorf("decision %v: prompts = %+v, want one for the earlier download", tt.decision, shown)
		}
		if h.runs() != tt.wantRuns {
			t.Errorf("decision %v: yt-dlp downloads = %d, want %d", tt.decision, h.runs(), tt.wantRuns)
		}
	}
}

func TestStartDownloadBatchSkipAllDuplicates(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	seedHistory(t, h)
	prompts := answerDuplicates(h, duplicateSkipAll)
	h.app.ui.download.batchMode.SetChecked(true)
	h.app.ui.download.entry.SetText("https://example.com/a\nhttps://example.com/b\nhttps://example.com/c")

	h.startAndWait(t)

	if shown := prompts(); len(shown) != 1 || !shown[0].batch {
		t.Errorf("prompts = %+v, want one batch prompt", shown)
	}
	if h.runs() != 0 {
		t.Errorf("yt-dlp downloads = %d, want 0", h.runs())
	}
	if n := strings.Count(h.joinedLogs(), "already downloaded on"); n != 3 {
		t.Errorf("log names %d skipped duplicates, want 3:\n%s", n, h.joinedLogs())
	}
}

func TestStartDownloadWithoutHistoryRecordsNothingAndDoesNotAsk(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	seedHistory(t, h)
	prompts := answerDuplicates(h, duplicateSkip)
	h.app.keepHistory.Store(false)
	h.app.ui.download.entry.SetText("https://example.com/fakevid")

	h.startAndWait(t)

	if n := len(prompts()); n != 0 {
		t.Errorf("asked %d times, want 0 with history off", n)
	}
	if entries := h.history(t); len(entries) != 1 {
		t.Errorf("history has %d entries, want only the seeded one", len(entries))
	}
}

func TestDuplicateDialogButtons(t *testing.T) {
	tests := []struct {
		batch bool
		tap   string
		want  duplicateDecision
	}{
		{false, "Download again", duplicateDownload},
		{false, "Skip", duplicateSkip},
		{true, "Skip all duplicates", duplicateSkipAll},
	}
	for _, tt := range tests {
		_ = test.NewApp()
		window := test.NewWindow(nil)
		window.Resize(fyne.NewSize(800, 600))
		var answers []duplicateDecision
		NewUIManager(window).showDuplicateDialog(duplicatePrompt{title: "V", previous: DownloadHistoryEntry{DownloadedAt: "d"}, batch: tt.batch},
			func(decision duplicateDecision) { answers = append(answers, decision) })

		test.Tap(findButton(t, window.Canvas().Overlays().Top(), tt.tap))

		if !slices.Equal(answers, []duplicateDecision{tt.want}) {
			t.Errorf("%s: answers = %v, want [%v]", tt.tap, answers, tt.want)
		}
	}
}
