package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestParsePlaylistSelection(t *testing.T) {
	tests := []struct {
		text    string
		want    []int
		wantErr string
	}{
		{"", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, ""},
		{"  ", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, ""},
		{"5-8", []int{5, 6, 7, 8}, ""},
		{"3,5,8", []int{3, 5, 8}, ""},
		{" 8 , 3-4 ", []int{8, 3, 4}, ""},
		{"2-4,3-5", []int{2, 3, 4, 5}, ""},
		{"9-", []int{9, 10}, ""},
		{"7", []int{7}, ""},
		{"0", nil, "outside the playlist"},
		{"11", nil, "outside the playlist"},
		{"3-12", nil, "outside the playlist"},
		{"8-5", nil, "counts backwards"},
		{"a-3", nil, "not a number"},
		{"1,,2", nil, "empty entry"},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got, err := parsePlaylistSelection(tt.text, 10)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("parsePlaylistSelection(%q) error = %v, want %q", tt.text, err, tt.wantErr)
				}
				return
			}
			if err != nil || !slices.Equal(got, tt.want) {
				t.Errorf("parsePlaylistSelection(%q) = %v, %v; want %v", tt.text, got, err, tt.want)
			}
		})
	}
}

func TestNamesSingleVideo(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://www.youtube.com/watch?v=abc&list=PL1", true},
		{"https://music.youtube.com/watch?v=abc&list=PL1", true},
		{"https://youtu.be/abc?list=PL1", true},
		{"https://www.youtube.com/playlist?list=PL1", false},
		{"https://vimeo.com/showcase/123", false},
		{"not a url", false},
	}
	for _, tt := range tests {
		if got := namesSingleVideo(tt.url); got != tt.want {
			t.Errorf("namesSingleVideo(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestPlaylistDuration(t *testing.T) {
	tests := []struct {
		name    string
		lengths []float64
		want    string
	}{
		{"all known", []float64{3600, 1380}, "1h 23m"},
		{"short", []float64{45, 20}, "1m 5s"},
		{"some unknown", []float64{60, 0, 0}, "at least 1m 0s (2 without a listed length)"},
		{"none known", []float64{0, 0}, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entries []PlaylistEntry
			for _, length := range tt.lengths {
				entries = append(entries, PlaylistEntry{Duration: length})
			}
			if got := playlistDuration(entries); got != tt.want {
				t.Errorf("playlistDuration() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlaylistEntryDownloadURL(t *testing.T) {
	tests := []struct {
		entry PlaylistEntry
		want  string
	}{
		{PlaylistEntry{URL: "https://www.youtube.com/watch?v=a"}, "https://www.youtube.com/watch?v=a"},
		{PlaylistEntry{URL: "a", WebpageURL: "https://example.com/a"}, "https://example.com/a"},
		{PlaylistEntry{URL: "a"}, ""},
	}
	for _, tt := range tests {
		if got := tt.entry.DownloadURL(); got != tt.want {
			t.Errorf("DownloadURL(%+v) = %q, want %q", tt.entry, got, tt.want)
		}
	}
}

func TestProbeArgs(t *testing.T) {
	args := NewDownloadEngine("yt-dlp", "").probeArgs(DownloadRequest{URL: "https://example.com/v", Format: formatMP4, Quality: quality720p}, false)
	for _, flag := range []string{"-J", "--flat-playlist"} {
		if !slices.Contains(args, flag) {
			t.Errorf("probe args %q missing %s", args, flag)
		}
	}
	if slices.Contains(args, "--no-playlist") {
		t.Errorf("probe args %q must not hide playlists", args)
	}
	if got, want := argAfter(args, "-f"), "bestvideo[height<=720]+bestaudio/best[height<=720]/best"; got != want {
		t.Errorf("-f = %q, want the download's selector %q", got, want)
	}
	if args[len(args)-1] != "https://example.com/v" {
		t.Errorf("last arg = %q, want the URL", args[len(args)-1])
	}
}

func TestProbe(t *testing.T) {
	tests := []struct {
		mode         string
		wantPlaylist bool
		wantEntries  int
		wantErr      bool
	}{
		{"ytdlp-download", false, 0, false},
		{"ytdlp-playlist", true, fakePlaylistSize, false},
		{"ytdlp-probe-fail", false, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			useFakeTool(t, tt.mode)
			info, err := NewDownloadEngine(fakeToolPath(t), "").Probe(context.Background(), DownloadRequest{URL: "https://example.com/v"})

			if (err != nil) != tt.wantErr {
				t.Fatalf("Probe() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if !strings.Contains(err.Error(), "Unable to download webpage") {
					t.Errorf("Probe() error = %v, want yt-dlp's reason", err)
				}
				return
			}
			if info.IsPlaylist() != tt.wantPlaylist || len(info.Entries) != tt.wantEntries {
				t.Errorf("Probe() = playlist %v with %d entries, want %v with %d", info.IsPlaylist(), len(info.Entries), tt.wantPlaylist, tt.wantEntries)
			}
			if tt.wantPlaylist && info.Entries[4].DownloadURL() != "https://example.com/v/5" {
				t.Errorf("entry 5 URL = %q", info.Entries[4].DownloadURL())
			}
		})
	}
}

// ── Sessions with playlists ──────────────────────────────────────────────────

// answerPlaylist makes the harness answer the playlist prompt with decide,
// and returns the prompts it was shown.
func answerPlaylist(h *downloadHarness, decide func(prompt playlistPrompt) playlistDecision) func() []playlistPrompt {
	var mu sync.Mutex
	var prompts []playlistPrompt
	h.app.askPlaylist = func(_ context.Context, prompt playlistPrompt) playlistDecision {
		mu.Lock()
		prompts = append(prompts, prompt)
		mu.Unlock()
		return decide(prompt)
	}
	return func() []playlistPrompt {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(prompts)
	}
}

func TestStartDownloadPlaylistRange(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-playlist")
	h.app.ui.download.entry.SetText("https://www.youtube.com/playlist?list=PL1")
	prompts := answerPlaylist(h, func(prompt playlistPrompt) playlistDecision {
		positions, err := parsePlaylistSelection("5-8", len(prompt.entries))
		if err != nil {
			t.Error(err)
		}
		return playlistDecision{positions: positions}
	})

	h.startAndWait(t)

	shown := prompts()
	if len(shown) != 1 || shown[0].title != "Fake Playlist" || len(shown[0].entries) != fakePlaylistSize || shown[0].hasVideo {
		t.Fatalf("prompts = %+v, want one for the 20-video playlist without a linked video", shown)
	}
	if h.runs() != 4 {
		t.Errorf("yt-dlp downloads = %d, want 4", h.runs())
	}
	if files := h.savedFiles(t); len(files) != 4 {
		t.Errorf("saved files = %q, want 4", files)
	}
	var urls []string
	for _, entry := range h.history(t) {
		urls = append(urls, entry.URL)
	}
	want := []string{"https://example.com/v/5", "https://example.com/v/6", "https://example.com/v/7", "https://example.com/v/8"}
	if !slices.Equal(urls, want) {
		t.Errorf("history URLs = %q, want %q", urls, want)
	}
	logs := h.joinedLogs()
	for _, line := range []string{`Playlist "Fake Playlist": queued 4 of 20 videos.`, "URL 1 of 4", "URL 4 of 4"} {
		if !strings.Contains(logs, line) {
			t.Errorf("log missing %q:\n%s", line, logs)
		}
	}
}

func TestStartDownloadPlaylistOnlyThisVideo(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-playlist")
	const url = "https://www.youtube.com/watch?v=abc&list=PL1"
	h.app.ui.download.entry.SetText(url)
	prompts := answerPlaylist(h, func(playlistPrompt) playlistDecision {
		return playlistDecision{onlyVideo: true}
	})

	h.startAndWait(t)

	if shown := prompts(); len(shown) != 1 || !shown[0].hasVideo {
		t.Errorf("prompts = %+v, want one offering the linked video", shown)
	}
	if h.runs() != 1 {
		t.Errorf("yt-dlp downloads = %d, want 1", h.runs())
	}
	if entries := h.history(t); len(entries) != 1 || entries[0].URL != url {
		t.Errorf("history = %+v, want only the linked video", entries)
	}
}

func TestStartDownloadPlaylistSkipped(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-playlist")
	h.app.ui.download.entry.SetText("https://www.youtube.com/playlist?list=PL1")
	answerPlaylist(h, func(playlistPrompt) playlistDecision { return playlistDecision{} })

	h.startAndWait(t)

	if h.runs() != 0 {
		t.Errorf("yt-dlp downloads = %d, want 0", h.runs())
	}
	if got := h.app.ui.download.status.Text; got != "Status: Nothing to download." {
		t.Errorf("status = %q, want Status: Nothing to download.", got)
	}
}

func TestStartDownloadProbeFailureStillDownloads(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-probe-fail")
	h.app.ui.download.entry.SetText("https://example.com/v")

	h.startAndWait(t)

	if !strings.Contains(h.joinedLogs(), "Could not check https://example.com/v") {
		t.Errorf("log missing the probe failure:\n%s", h.joinedLogs())
	}
	if files := h.savedFiles(t); !slices.Equal(files, []string{"GoVid_Fake Video.mp4"}) {
		t.Errorf("saved files = %q, want the video downloaded anyway", files)
	}
}

// ── One extraction per video ─────────────────────────────────────────────────

func TestStartDownloadExtractsSingleVideoOnce(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	extractions := useFakeExtractions(t)
	h.app.ui.download.entry.SetText("https://example.com/v")

	h.startAndWait(t)

	if files := h.savedFiles(t); !slices.Equal(files, []string{"GoVid_Fake Video.mp4"}) {
		t.Fatalf("saved files = %q; log:\n%s", files, h.joinedLogs())
	}
	if n := extractions(); n != 1 {
		t.Errorf("extractions = %d, want 1 (the download loads the probe's answer)", n)
	}
}

func TestStartDownloadExtractsEachPlaylistEntryOnce(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-playlist")
	extractions := useFakeExtractions(t)
	h.app.ui.download.entry.SetText("https://www.youtube.com/playlist?list=PL1")
	answerPlaylist(h, func(playlistPrompt) playlistDecision {
		return playlistDecision{positions: []int{2, 3, 4}}
	})

	h.startAndWait(t)

	if files := h.savedFiles(t); len(files) != 3 {
		t.Fatalf("saved files = %q, want 3; log:\n%s", files, h.joinedLogs())
	}
	if n := extractions(); n != 3 {
		t.Errorf("extractions = %d, want 3 (one probe per entry, none by the downloads)", n)
	}
}

func TestStartDownloadPlaylistEntryChecksDiskSpace(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-playlist")
	h.app.ui.download.entry.SetText("https://www.youtube.com/playlist?list=PL1")
	answerPlaylist(h, func(playlistPrompt) playlistDecision {
		return playlistDecision{positions: []int{1, 2}}
	})
	prompts := lowSpace(h, fakeVideoSize/2, spaceSkip)

	h.startAndWait(t)

	shown := prompts()
	if len(shown) != 2 || shown[0].url != "https://example.com/v/1" || shown[1].url != "https://example.com/v/2" {
		t.Errorf("prompts = %+v, want one per playlist entry", shown)
	}
	if h.runs() != 0 {
		t.Errorf("yt-dlp downloads = %d, want 0 after skipping both", h.runs())
	}
}

func TestQueueItemNeedsProbe(t *testing.T) {
	now := time.Now()
	fresh := &MediaInfo{raw: []byte(`{}`), probedAt: now.Add(-time.Minute)}
	stale := &MediaInfo{raw: []byte(`{}`), probedAt: now.Add(-probeMaxAge - time.Minute)}
	used := &MediaInfo{probedAt: now} // its JSON was dropped after a download
	tests := []struct {
		name string
		item queueItem
		want bool
	}{
		{"playlist entry", queueItem{url: "u"}, true},
		{"probe failed", queueItem{url: "u", probeFailed: true}, false},
		{"fresh answer", queueItem{url: "u", info: fresh}, false},
		{"stale answer", queueItem{url: "u", info: stale}, true},
		{"answer already used", queueItem{url: "u", info: used}, true},
	}
	for _, tt := range tests {
		if got := tt.item.needsProbe(now); got != tt.want {
			t.Errorf("%s: needsProbe() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// ── Playlist prompt ──────────────────────────────────────────────────────────

func newPlaylistPrompt(hasVideo bool) playlistPrompt {
	prompt := playlistPrompt{url: "https://example.com/list", title: "Fake Playlist", hasVideo: hasVideo}
	for n := 1; n <= fakePlaylistSize; n++ {
		prompt.entries = append(prompt.entries, PlaylistEntry{URL: fmt.Sprintf("https://example.com/v/%d", n), Duration: 90})
	}
	return prompt
}

// showTestPlaylistDialog shows the prompt in a test window and returns the
// window and a function reporting the answers it gave.
func showTestPlaylistDialog(t *testing.T, prompt playlistPrompt) (fyne.Window, func() []playlistDecision) {
	t.Helper()
	_ = test.NewApp()
	window := test.NewWindow(nil)
	window.Resize(fyne.NewSize(800, 600))
	mgr := NewUIManager(window)
	var answers []playlistDecision
	mgr.showPlaylistDialog(prompt, func(decision playlistDecision) { answers = append(answers, decision) })
	return window, func() []playlistDecision { return answers }
}

// findEntry returns the first entry inside root.
func findEntry(t *testing.T, root fyne.CanvasObject) *widget.Entry {
	t.Helper()
	var found *widget.Entry
	walkObjects(root, func(obj fyne.CanvasObject) {
		if entry, ok := obj.(*widget.Entry); ok && found == nil {
			found = entry
		}
	})
	if found == nil {
		t.Fatal("no entry found")
	}
	return found
}

func TestPlaylistDialogDownloadsRange(t *testing.T) {
	window, answers := showTestPlaylistDialog(t, newPlaylistPrompt(false))
	overlay := window.Canvas().Overlays().Top()

	findEntry(t, overlay).SetText("5-8")
	test.Tap(findButton(t, overlay, "Download"))

	got := answers()
	if len(got) != 1 || !slices.Equal(got[0].positions, []int{5, 6, 7, 8}) || got[0].onlyVideo {
		t.Errorf("answers = %+v, want positions 5-8 once", got)
	}
	if window.Canvas().Overlays().Top() != nil {
		t.Error("prompt still open after Download")
	}
}

func TestPlaylistDialogRejectsBadRange(t *testing.T) {
	window, answers := showTestPlaylistDialog(t, newPlaylistPrompt(false))
	overlay := window.Canvas().Overlays().Top()

	findEntry(t, overlay).SetText("30")
	test.Tap(findButton(t, overlay, "Download"))

	if got := answers(); len(got) != 0 {
		t.Errorf("answers = %+v, want none for a range outside the playlist", got)
	}
	if window.Canvas().Overlays().Top() == nil {
		t.Error("prompt closed despite the invalid range")
	}
}

func TestPlaylistDialogOnlyThisVideoAndCancel(t *testing.T) {
	window, answers := showTestPlaylistDialog(t, newPlaylistPrompt(true))
	test.Tap(findButton(t, window.Canvas().Overlays().Top(), "Only this video"))
	if got := answers(); len(got) != 1 || !got[0].onlyVideo {
		t.Errorf("answers = %+v, want only-this-video", got)
	}

	window, answers = showTestPlaylistDialog(t, newPlaylistPrompt(false))
	overlay := window.Canvas().Overlays().Top()
	walkObjects(overlay, func(obj fyne.CanvasObject) {
		if btn, ok := obj.(*widget.Button); ok && btn.Text == "Only this video" {
			t.Error("Only this video offered for a URL that names no video")
		}
	})
	test.Tap(findButton(t, overlay, "Cancel"))
	if got := answers(); len(got) != 1 || got[0].onlyVideo || len(got[0].positions) != 0 {
		t.Errorf("answers = %+v, want one empty (skip) answer", got)
	}
}
