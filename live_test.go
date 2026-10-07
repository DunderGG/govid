package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

func TestMediaInfoLiveStatus(t *testing.T) {
	info := MediaInfo{LiveStatus: liveStatusUpcoming, ReleaseTimestamp: 1791400000, ExtractorKey: "Youtube"}
	if !info.IsUpcoming() || info.IsLive() || info.IsPostLive() {
		t.Errorf("upcoming info reads as live=%v upcoming=%v postLive=%v", info.IsLive(), info.IsUpcoming(), info.IsPostLive())
	}
	if got := info.releaseTime(); !got.Equal(time.Unix(1791400000, 0)) {
		t.Errorf("releaseTime() = %v", got)
	}
	for key, want := range map[string]bool{"Youtube": true, "TwitchStream": true, "Generic": false, "Vimeo": false} {
		if got := (MediaInfo{ExtractorKey: key}).canRecordFromStart(); got != want {
			t.Errorf("canRecordFromStart(%s) = %v, want %v", key, got, want)
		}
	}
	if !(MediaInfo{}).releaseTime().IsZero() {
		t.Error("an unknown start time is not zero")
	}
}

func TestBuildArgsForLiveStreams(t *testing.T) {
	engine := NewDownloadEngine("yt-dlp", "")
	base := DownloadRequest{URL: "u", SavePath: "s", Format: formatMP4, Quality: qualityBest}
	tests := []struct {
		name    string
		req     func(DownloadRequest) DownloadRequest
		want    []string
		wantNot []string
	}{
		{"not live", func(r DownloadRequest) DownloadRequest { return r }, nil, []string{"--hls-use-mpegts", "--live-from-start", "--wait-for-video"}},
		{"from now", func(r DownloadRequest) DownloadRequest { r.Live = true; return r }, []string{"--hls-use-mpegts"}, []string{"--live-from-start", "--wait-for-video"}},
		{"from the start", func(r DownloadRequest) DownloadRequest { r.Live, r.LiveFromStart = true, true; return r }, []string{"--hls-use-mpegts", "--live-from-start"}, []string{"--wait-for-video"}},
		{"scheduled", func(r DownloadRequest) DownloadRequest { r.Live, r.WaitForVideo = true, true; return r }, []string{"--hls-use-mpegts", "--wait-for-video"}, []string{"--live-from-start"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := engine.BuildArgs(tt.req(base)).Args
			for _, flag := range tt.want {
				if !slices.Contains(args, flag) {
					t.Errorf("args missing %s: %q", flag, args)
				}
			}
			for _, flag := range tt.wantNot {
				if slices.Contains(args, flag) {
					t.Errorf("args unexpectedly contain %s: %q", flag, args)
				}
			}
			if slices.Contains(args, "--wait-for-video") && argAfter(args, "--wait-for-video") != "60-300" {
				t.Errorf("--wait-for-video = %q, want 60-300", argAfter(args, "--wait-for-video"))
			}
		})
	}
}

func TestProbeIgnoresMissingFormats(t *testing.T) {
	// A scheduled stream has no formats yet; without this flag the probe fails.
	args := NewDownloadEngine("yt-dlp", "").probeArgs(DownloadRequest{URL: "u", Format: formatMP4}, true)
	if !slices.Contains(args, "--ignore-no-formats-error") {
		t.Errorf("probe args = %q, want --ignore-no-formats-error", args)
	}
}

func TestLiveStatusText(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		started bool
		elapsed time.Duration
		size    int64
		release time.Time
		want    string
	}{
		{true, 12*time.Minute + 34*time.Second, 410 << 20, time.Time{}, "Status: Recording 00:12:34 · 410.0 MiB"},
		{false, 0, 0, now.Add(2*time.Hour + 10*time.Minute), "Status: Stream starts in 02:10:00; waiting to record…"},
		{false, 0, 0, now.Add(-time.Minute), "Status: Waiting for the stream to start…"},
		{false, 0, 0, time.Time{}, "Status: Waiting for the stream to start…"},
	}
	for _, tt := range tests {
		if got := liveStatusText(tt.started, tt.elapsed, tt.size, tt.release, now); got != tt.want {
			t.Errorf("liveStatusText() = %q, want %q", got, tt.want)
		}
	}
}

func TestStartsInText(t *testing.T) {
	for startsIn, want := range map[time.Duration]string{
		2*time.Hour + 10*time.Minute: "Starts in 2 h 10 min.",
		25 * time.Minute:             "Starts in 25 min.",
		20 * time.Second:             "Starts in less than a minute.",
	} {
		if got := startsInText(startsIn); got != want {
			t.Errorf("startsInText(%v) = %q, want %q", startsIn, got, want)
		}
	}
	if got := startsInText(0); !strings.Contains(got, "does not say") {
		t.Errorf("startsInText(0) = %q", got)
	}
}

func TestRecordingRemuxPlan(t *testing.T) {
	if got := recordingTarget(filepath.Join("d", "GoVid_Stream.f299.mp4"), "mp4"); got != filepath.Join("d", "GoVid_Stream.mp4") {
		t.Errorf("recordingTarget() = %q", got)
	}
	if got := recordingTarget(filepath.Join("d", "GoVid_Stream.mp4"), "mkv"); got != filepath.Join("d", "GoVid_Stream.mkv") {
		t.Errorf("recordingTarget() = %q", got)
	}
	for ext, want := range map[string][]string{"mp4": {"mp4", "mkv"}, "webm": {"webm", "mkv"}, "mkv": {"mkv"}, "mp3": {"mp3"}} {
		if got := recordingContainers(ext); !slices.Equal(got, want) {
			t.Errorf("recordingContainers(%s) = %q, want %q", ext, got, want)
		}
	}

	merge := recordingRemuxArgs([]string{"v.mp4", "a.m4a"}, "out.mp4", "mp4")
	want := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", "v.mp4", "-i", "a.m4a", "-map", "0:v?", "-map", "0:a?", "-map", "1:v?", "-map", "1:a?", "-c", "copy", "out.mp4"}
	if !slices.Equal(merge, want) {
		t.Errorf("merge args = %q, want %q", merge, want)
	}
	mp3 := recordingRemuxArgs([]string{"rec.ts"}, "out.mp3", "mp3")
	if slices.Contains(mp3, "0:v?") || argAfter(mp3, "-c:a") != "libmp3lame" {
		t.Errorf("MP3 args = %q, want audio re-encoded with LAME", mp3)
	}
}

func TestDownloadContextCauseWhenTheSessionStops(t *testing.T) {
	tests := []struct {
		name     string
		live     bool
		quitting bool
		want     error
	}{
		{"a recording is kept", true, false, errStopKeep},
		{"a download is cancelled", false, false, context.Canceled},
		{"quitting pauses a download", false, true, errPaused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &DownloaderApp{}
			app.quitting.Store(tt.quitting)
			session, stopSession := context.WithCancel(context.Background())
			ctx, _, release := app.downloadContext(session, tt.live)
			defer release()

			stopSession()

			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("the download did not stop with the session")
			}
			if !errors.Is(context.Cause(ctx), tt.want) {
				t.Errorf("cause = %v, want %v", context.Cause(ctx), tt.want)
			}
		})
	}
}

func TestRunStoppedRecordingKeepsAndFinalizesIt(t *testing.T) {
	_ = test.NewApp()
	isolatePath(t) // no ffmpeg: the recording is kept as it was
	useFakeTool(t, "ytdlp-live")
	saveDir := t.TempDir()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	rec := &engineRecorder{}
	callbacks := rec.callbacks()
	var sawStart bool
	callbacks.OnRecording = func(started bool, elapsed time.Duration, size int64) {
		if started && size > 0 {
			sawStart = true
			cancel(errStopKeep)
		}
	}

	result := NewDownloadEngine(fakeToolPath(t), "").Run(ctx, DownloadRequest{
		URL: "https://example.com/live", SavePath: saveDir, Format: formatMP4, Live: true,
	}, DownloadOptions{Index: 1, Total: 1}, callbacks)

	if !sawStart {
		t.Error("OnRecording never reported the recording")
	}
	if result.Err != nil || !result.Stopped {
		t.Fatalf("Run() = err %v, stopped %v; want a kept recording", result.Err, result.Stopped)
	}
	if len(result.FinalPaths) != 1 || strings.Contains(result.FinalPaths[0], "GOVID") {
		t.Fatalf("FinalPaths = %q, want one finalized file", result.FinalPaths)
	}
	if info, err := os.Stat(result.FinalPaths[0]); err != nil || info.Size() == 0 {
		t.Errorf("the recording is missing or empty: %v", err)
	}
	if logs := rec.joinedLogs(); !strings.Contains(logs, "keeping what was recorded") || strings.Contains(logs, "Removed partial file") {
		t.Errorf("log:\n%s", logs)
	}
}

// liveHarness is a download harness whose fake yt-dlp records a live stream
// and whose live prompt answers decision. It returns the prompts shown.
func liveHarness(t *testing.T, mode string, decision liveDecision) (*downloadHarness, *[]livePrompt) {
	t.Helper()
	h := newDownloadHarness(t, mode)
	prompts := &[]livePrompt{}
	h.app.askLive = func(_ context.Context, prompt livePrompt) liveDecision {
		*prompts = append(*prompts, prompt)
		return decision
	}
	h.app.ui.download.entry.SetText("https://www.youtube.com/watch?v=live")
	return h, prompts
}

// stopOnceRecording calls stop shortly after the fake yt-dlp starts writing.
func stopOnceRecording(h *downloadHarness, stop func()) {
	h.setHook(func(line string) {
		if strings.HasPrefix(line, "[download] Destination:") {
			go func() {
				time.Sleep(300 * time.Millisecond)
				stop()
			}()
		}
	})
}

// checkRecordingSaved checks that one finalized recording was saved and
// recorded in the history.
func checkRecordingSaved(t *testing.T, h *downloadHarness) {
	t.Helper()
	files := h.savedFiles(t)
	if len(files) != 1 || strings.Contains(files[0], "GOVID") {
		t.Errorf("save folder = %q, want one finalized recording", files)
	}
	if entries := h.history(t); len(entries) != 1 {
		t.Errorf("history has %d entries, want the recording", len(entries))
	}
	if !strings.Contains(h.joinedLogs(), "RECORDING SAVED") {
		t.Errorf("log does not report the saved recording:\n%s", h.joinedLogs())
	}
	if h.app.recording.Load() || h.app.ui.download.cancelBtn.Text != "Cancel" || h.app.ui.download.progressLive.Visible() {
		t.Error("the recording view was not switched back")
	}
}

func TestStopRecordingKeepsTheLiveStream(t *testing.T) {
	h, prompts := liveHarness(t, "ytdlp-live", liveRecordNow)
	runs := useFakeArgs(t)
	stopOnceRecording(h, func() { h.app.RequestCancel() })

	h.startAndWait(t)

	if len(*prompts) != 1 || (*prompts)[0].upcoming || !(*prompts)[0].fromStart {
		t.Errorf("prompts = %+v, want one for a live YouTube stream offering Record from the start", *prompts)
	}
	checkRecordingSaved(t, h)
	download := runs()[len(runs())-1]
	if !slices.Contains(download, "--hls-use-mpegts") || slices.Contains(download, "--load-info-json") || slices.Contains(download, "--live-from-start") {
		t.Errorf("download args = %q", download)
	}
	if strings.Contains(h.joinedLogs(), "Download canceled by user") {
		t.Error("stopping the recording was reported as a cancel")
	}
}

func TestQuittingWhileRecordingKeepsTheRecording(t *testing.T) {
	h, _ := liveHarness(t, "ytdlp-live", liveRecordFromStart)
	runs := useFakeArgs(t)
	stopOnceRecording(h, h.app.StopSession)

	h.startAndWait(t)

	checkRecordingSaved(t, h)
	if download := runs()[len(runs())-1]; !slices.Contains(download, "--live-from-start") {
		t.Errorf("download args = %q, want --live-from-start", download)
	}
}

func TestRecordingStopsWhenTheDriveIsNearlyFull(t *testing.T) {
	h, _ := liveHarness(t, "ytdlp-live", liveRecordNow)
	h.app.freeBytes = func(string) (uint64, error) { return 100 << 20, nil }

	h.startAndWait(t)

	checkRecordingSaved(t, h)
	if !strings.Contains(h.joinedLogs(), "Only 100.0 MiB is free") {
		t.Errorf("log does not explain the stop:\n%s", h.joinedLogs())
	}
	if !slices.Contains(noticeIDs(h.app), lowSpaceNoticeID) {
		t.Error("no low-space notice")
	}
}

func TestScheduledStreamWaitsAndThenRecords(t *testing.T) {
	h, prompts := liveHarness(t, "ytdlp-upcoming", liveWait)
	runs := useFakeArgs(t)

	h.startAndWait(t)

	if len(*prompts) != 1 || !(*prompts)[0].upcoming {
		t.Fatalf("prompts = %+v, want one for a scheduled stream", *prompts)
	}
	if startsIn := (*prompts)[0].startsIn; startsIn < 119*time.Minute || startsIn > 2*time.Hour {
		t.Errorf("startsIn = %v, want about 2 h", startsIn)
	}
	download := runs()[len(runs())-1]
	if argAfter(download, "--wait-for-video") != "60-300" || slices.Contains(download, "--load-info-json") {
		t.Errorf("download args = %q, want --wait-for-video 60-300 from the URL", download)
	}
	if files := h.savedFiles(t); len(files) != 1 || len(h.history(t)) != 1 {
		t.Errorf("save folder = %q, history %d entries; want the recorded premiere", files, len(h.history(t)))
	}
}

func TestSkippingALiveStreamDownloadsNothing(t *testing.T) {
	h, _ := liveHarness(t, "ytdlp-live", liveSkip)
	runs := useFakeArgs(t)

	h.startAndWait(t)

	if got := len(runs()); got != 1 {
		t.Errorf("yt-dlp ran %d times, want only the probe", got)
	}
	if files := h.savedFiles(t); len(files) != 0 {
		t.Errorf("save folder = %q, want nothing", files)
	}
	if !strings.Contains(h.joinedLogs(), "Skipped the live stream") {
		t.Errorf("log:\n%s", h.joinedLogs())
	}
}
