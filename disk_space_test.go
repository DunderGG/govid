package main

import (
	"context"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestDiskSpaceNeeded(t *testing.T) {
	tests := []struct {
		name        string
		estimate    uint64
		postProcess bool
		want        uint64
	}{
		{"margin", 1000, false, 1100},
		{"post-processing doubles", 1000, true, 2200},
		{"nothing", 0, true, 0},
	}
	for _, tt := range tests {
		if got := diskSpaceNeeded(tt.estimate, tt.postProcess); got != tt.want {
			t.Errorf("%s: diskSpaceNeeded(%d, %v) = %d, want %d", tt.name, tt.estimate, tt.postProcess, got, tt.want)
		}
	}
}

func TestTrimFraction(t *testing.T) {
	tests := []struct {
		start, end string
		want       float64
	}{
		{"", "", 1},
		{"00:00:30", "", 0.75},
		{"", "1:00", 0.5},
		{"30", "90", 0.5},
		{"1:00", "5:00", 0.5}, // the end is past the video's end
		{"90", "30", 1},       // backwards: give up rather than guess
		{"x", "", 1},
	}
	for _, tt := range tests {
		if got := trimFraction(120, tt.start, tt.end); math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("trimFraction(120, %q, %q) = %v, want %v", tt.start, tt.end, got, tt.want)
		}
	}
	if got := trimFraction(0, "30", ""); got != 1 {
		t.Errorf("trimFraction with unknown duration = %v, want 1", got)
	}
}

func TestMediaInfoEstimatedSize(t *testing.T) {
	tests := []struct {
		name      string
		info      MediaInfo
		want      uint64
		wantKnown bool
	}{
		{"merged streams", MediaInfo{RequestedFormats: []formatSize{{FileSize: 1000}, {FileSizeApprox: 500}}}, 1500, true},
		{"merged stream of unknown size", MediaInfo{RequestedFormats: []formatSize{{FileSize: 1000}, {}}}, 0, false},
		{"single format, exact", MediaInfo{formatSize: formatSize{FileSize: 700, FileSizeApprox: 900}}, 700, true},
		{"single format, approximate", MediaInfo{formatSize: formatSize{FileSizeApprox: 900.7}}, 900, true},
		{"unknown", MediaInfo{}, 0, false},
	}
	for _, tt := range tests {
		got, known := tt.info.EstimatedSize()
		if got != tt.want || known != tt.wantKnown {
			t.Errorf("%s: EstimatedSize() = %d, %v; want %d, %v", tt.name, got, known, tt.want, tt.wantKnown)
		}
	}
}

func TestFreeDiskBytes(t *testing.T) {
	free, err := freeDiskBytes(t.TempDir())
	if err != nil || free == 0 {
		t.Errorf("freeDiskBytes(temp dir) = %d, %v; want some free space", free, err)
	}
}

func TestExistingDir(t *testing.T) {
	dir := t.TempDir()
	if got := existingDir(filepath.Join(dir, "not", "made", "yet")); got != dir {
		t.Errorf("existingDir(missing subfolder) = %q, want %q", got, dir)
	}
	if got := existingDir(dir); got != dir {
		t.Errorf("existingDir(existing) = %q, want it unchanged", got)
	}
}

// ── Sessions ─────────────────────────────────────────────────────────────────

// lowSpace makes the harness report free bytes and answer the low-space
// prompt with decision, and returns the prompts it was shown.
func lowSpace(h *downloadHarness, free uint64, decision diskSpaceDecision) func() []diskSpacePrompt {
	var mu sync.Mutex
	var prompts []diskSpacePrompt
	h.app.freeBytes = func(string) (uint64, error) { return free, nil }
	h.app.askDiskSpace = func(_ context.Context, prompt diskSpacePrompt) diskSpaceDecision {
		mu.Lock()
		defer mu.Unlock()
		prompts = append(prompts, prompt)
		return decision
	}
	return func() []diskSpacePrompt {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(prompts)
	}
}

func TestStartDownloadWithEnoughSpaceDoesNotAsk(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.entry.SetText("https://example.com/v")
	prompts := lowSpace(h, 100*fakeVideoSize, spaceStop)

	h.startAndWait(t)

	if n := len(prompts()); n != 0 {
		t.Errorf("asked %d times, want 0 with enough space", n)
	}
	if h.runs() != 1 {
		t.Errorf("yt-dlp downloads = %d, want 1", h.runs())
	}
}

func TestStartDownloadLowSpaceCancelled(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.entry.SetText("https://example.com/v")
	prompts := lowSpace(h, fakeVideoSize/2, spaceStop)

	h.startAndWait(t)

	shown := prompts()
	want := diskSpacePrompt{url: "https://example.com/v", needed: diskSpaceNeeded(fakeVideoSize, false), free: fakeVideoSize / 2, batch: false}
	if len(shown) != 1 || shown[0] != want {
		t.Fatalf("prompts = %+v, want %+v", shown, want)
	}
	if h.runs() != 0 {
		t.Errorf("yt-dlp downloads = %d, want 0 after Cancel", h.runs())
	}
	if got := h.app.ui.download.status.Text; got != "Status: Stopped (not enough disk space)." {
		t.Errorf("status = %q", got)
	}
}

func TestStartDownloadLowSpaceContinueAnyway(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.entry.SetText("https://example.com/v")
	lowSpace(h, fakeVideoSize/2, spaceProceed)

	h.startAndWait(t)

	if h.runs() != 1 {
		t.Errorf("yt-dlp downloads = %d, want 1 after Continue anyway", h.runs())
	}
}

func TestStartDownloadBatchLowSpaceContinueAsksOnce(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.batchMode.SetChecked(true)
	h.app.ui.download.entry.SetText("https://example.com/a\nhttps://example.com/b\nhttps://example.com/c")
	prompts := lowSpace(h, fakeVideoSize/2, spaceContinueAll)

	h.startAndWait(t)

	if shown := prompts(); len(shown) != 1 || !shown[0].batch {
		t.Errorf("prompts = %+v, want one batch prompt", shown)
	}
	if h.runs() != 3 {
		t.Errorf("yt-dlp downloads = %d, want 3", h.runs())
	}
}

func TestStartDownloadBatchLowSpaceSkip(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.batchMode.SetChecked(true)
	h.app.ui.download.entry.SetText("https://example.com/a\nhttps://example.com/b")
	prompts := lowSpace(h, fakeVideoSize/2, spaceSkip)

	h.startAndWait(t)

	shown := prompts()
	if len(shown) != 2 || !shown[0].batch || shown[1].batch {
		t.Errorf("prompts = %+v, want one per item, the last not a batch prompt", shown)
	}
	if h.runs() != 0 {
		t.Errorf("yt-dlp downloads = %d, want 0 after skipping both", h.runs())
	}
	if !strings.Contains(h.joinedLogs(), "Skipped https://example.com/a: not enough disk space.") {
		t.Errorf("log missing the skip:\n%s", h.joinedLogs())
	}
}

func TestStartDownloadLowSpaceCountsPostProcessing(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.download.entry.SetText("https://example.com/v")
	h.app.ui.postProcess.enablePostProcess.SetChecked(true)
	h.app.ui.postProcess.deband.SetChecked(true)
	prompts := lowSpace(h, fakeVideoSize, spaceStop)

	h.startAndWait(t)

	if shown := prompts(); len(shown) != 1 || shown[0].needed != diskSpaceNeeded(fakeVideoSize, true) {
		t.Errorf("prompts = %+v, want one needing room for the processed copy too", shown)
	}
}

func TestStartDownloadUnknownSizeSkipsCheck(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-probe-fail")
	h.app.ui.download.entry.SetText("https://example.com/v")
	prompts := lowSpace(h, 0, spaceStop)

	h.startAndWait(t)

	if n := len(prompts()); n != 0 {
		t.Errorf("asked %d times, want 0 for an unknown size", n)
	}
	if !strings.Contains(h.joinedLogs(), "Download size unknown; skipping the disk space check.") {
		t.Errorf("log missing the skipped check:\n%s", h.joinedLogs())
	}
	if h.runs() != 1 {
		t.Errorf("yt-dlp downloads = %d, want 1", h.runs())
	}
}

// ── Prompt ───────────────────────────────────────────────────────────────────

func TestDiskSpaceDialogButtons(t *testing.T) {
	tests := []struct {
		batch bool
		tap   string
		want  diskSpaceDecision
	}{
		{false, "Continue anyway", spaceProceed},
		{false, "Cancel", spaceStop},
		{true, "Continue", spaceContinueAll},
		{true, "Skip", spaceSkip},
		{true, "Stop", spaceStop},
	}
	for _, tt := range tests {
		t.Run(tt.tap, func(t *testing.T) {
			_ = test.NewApp()
			window := test.NewWindow(nil)
			window.Resize(fyne.NewSize(800, 600))
			var answers []diskSpaceDecision
			NewUIManager(window).showDiskSpaceDialog(diskSpacePrompt{url: "u", needed: 2 << 30, free: 1 << 30, batch: tt.batch},
				func(decision diskSpaceDecision) { answers = append(answers, decision) })

			test.Tap(findButton(t, window.Canvas().Overlays().Top(), tt.tap))

			if !slices.Equal(answers, []diskSpaceDecision{tt.want}) {
				t.Errorf("answers = %v, want [%v]", answers, tt.want)
			}
		})
	}
}
