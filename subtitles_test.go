package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestSubtitleArgs(t *testing.T) {
	tests := []struct {
		name        string
		req         DownloadRequest
		extension   string
		want        []string
		wantSkipped bool
	}{
		{"off", DownloadRequest{Subtitles: subtitlesOff}, "mp4", nil, false},
		{"unset", DownloadRequest{}, "mp4", nil, false},
		{"save as srt", DownloadRequest{Subtitles: subtitlesSRT, SubtitleLangs: "en.*,de"}, "mp4",
			[]string{"--write-subs", "--sub-langs", "en.*,de", "--convert-subs", "srt"}, false},
		{"embed drops the files", DownloadRequest{Subtitles: subtitlesEmbed, SubtitleLangs: "en"}, "mp4",
			[]string{"--write-subs", "--sub-langs", "en", "--convert-subs", "srt", "--embed-subs", "--compat-options", "no-keep-subs"}, false},
		{"both keeps the files", DownloadRequest{Subtitles: subtitlesBoth, SubtitleLangs: "en"}, "mkv",
			[]string{"--write-subs", "--sub-langs", "en", "--convert-subs", "srt", "--embed-subs"}, false},
		{"auto-generated", DownloadRequest{Subtitles: subtitlesSRT, SubtitleLangs: "en", AutoSubtitles: true}, "mp4",
			[]string{"--write-subs", "--write-auto-subs", "--sub-langs", "en", "--convert-subs", "srt"}, false},
		{"default languages", DownloadRequest{Subtitles: subtitlesSRT, SubtitleLangs: "  "}, "mp4",
			[]string{"--write-subs", "--sub-langs", defaultSubtitleLangs, "--convert-subs", "srt"}, false},
		{"webm embeds webvtt", DownloadRequest{Subtitles: subtitlesEmbed, SubtitleLangs: "en"}, "webm",
			[]string{"--write-subs", "--sub-langs", "en", "--convert-subs", "vtt", "--embed-subs", "--compat-options", "no-keep-subs"}, false},
		{"webm sidecar stays srt", DownloadRequest{Subtitles: subtitlesSRT, SubtitleLangs: "en"}, "webm",
			[]string{"--write-subs", "--sub-langs", "en", "--convert-subs", "srt"}, false},
		{"audio skipped", DownloadRequest{Subtitles: subtitlesBoth, SubtitleLangs: "en"}, "mp3", nil, true},
	}
	for _, tt := range tests {
		got, skipped := subtitleArgs(tt.req, tt.extension)
		if !slices.Equal(got, tt.want) || skipped != tt.wantSkipped {
			t.Errorf("%s: subtitleArgs() = %q, %v; want %q, %v", tt.name, got, skipped, tt.want, tt.wantSkipped)
		}
	}
}

func TestMatchSubLangs(t *testing.T) {
	available := []string{"de", "en", "en-GB", "en-US", "fr", "live_chat"}
	tests := []struct {
		spec string
		want []string
	}{
		{"en.*", []string{"en", "en-GB", "en-US"}},
		{"en", []string{"en"}},
		{"de,en", []string{"de", "en"}},
		{"en.*,en", []string{"en", "en-GB", "en-US"}},
		{"all,-live_chat", []string{"de", "en", "en-GB", "en-US", "fr"}},
		{"en.*,-en-GB", []string{"en", "en-US"}},
		{"ja", nil},
		{"(", nil}, // not a valid expression
		{"", nil},
	}
	for _, tt := range tests {
		if got := matchSubLangs(tt.spec, available); !slices.Equal(got, tt.want) {
			t.Errorf("matchSubLangs(%q) = %q, want %q", tt.spec, got, tt.want)
		}
	}
}

func TestSplitSubtitleFiles(t *testing.T) {
	media, subtitles := splitSubtitleFiles([]string{"V.mp4", "V.en.srt", "V.de.VTT", "V.mkv"})
	if !slices.Equal(media, []string{"V.mp4", "V.mkv"}) || !slices.Equal(subtitles, []string{"V.en.srt", "V.de.VTT"}) {
		t.Errorf("splitSubtitleFiles() = %q, %q", media, subtitles)
	}
}

func TestRunSavesSubtitlesBesideTheVideo(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-download")
	saveDir := t.TempDir()
	rec := &engineRecorder{}

	result := NewDownloadEngine(fakeToolPath(t), "").Run(context.Background(), DownloadRequest{
		URL: "https://example.com/v", SavePath: saveDir, Format: formatMP4,
		Subtitles: subtitlesSRT, SubtitleLangs: "en",
	}, DownloadOptions{Index: 1, Total: 1}, rec.callbacks())

	if result.Err != nil {
		t.Fatalf("Run() error = %v; log:\n%s", result.Err, rec.joinedLogs())
	}
	if len(result.FinalPaths) != 1 || !strings.HasSuffix(result.FinalPaths[0], "GoVid_Fake Video.mp4") {
		t.Errorf("FinalPaths = %q, want only the video", result.FinalPaths)
	}
	if len(result.SubtitlePaths) != 1 || !strings.HasSuffix(result.SubtitlePaths[0], "GoVid_Fake Video.en.srt") {
		t.Errorf("SubtitlePaths = %q, want GoVid_Fake Video.en.srt", result.SubtitlePaths)
	}
	if !strings.Contains(rec.joinedLogs(), "[SYSTEM] Saved subtitles: GoVid_Fake Video.en.srt") {
		t.Errorf("log does not name the subtitle file:\n%s", rec.joinedLogs())
	}
}

func TestRunRetriesWithoutSubtitlesWhenTheyFail(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-subtitles-429")
	runs := useFakeToolState(t)
	rec := &engineRecorder{}

	result := NewDownloadEngine(fakeToolPath(t), "").Run(context.Background(), DownloadRequest{
		URL: "https://example.com/v", SavePath: t.TempDir(), Format: formatMP4,
		Subtitles: subtitlesEmbed, SubtitleLangs: "en",
	}, DownloadOptions{Index: 1, Total: 1}, rec.callbacks())

	if result.Err != nil || len(result.FinalPaths) != 1 {
		t.Fatalf("Run() = %q, %v; want the video downloaded without subtitles; log:\n%s", result.FinalPaths, result.Err, rec.joinedLogs())
	}
	if runs() != 2 {
		t.Errorf("yt-dlp runs = %d, want 2", runs())
	}
	if !strings.Contains(rec.joinedLogs(), "The subtitles could not be downloaded; downloading the video without them.") {
		t.Errorf("log does not explain the retry:\n%s", rec.joinedLogs())
	}
}

func TestRunNotesUntrimmedSubtitlesAndSkipsAudio(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-download")
	engine := NewDownloadEngine(fakeToolPath(t), "")

	trimmed := &engineRecorder{}
	engine.Run(context.Background(), DownloadRequest{URL: "https://example.com/v", SavePath: t.TempDir(), Format: formatMKV,
		TrimEnd: "30", Subtitles: subtitlesSRT}, DownloadOptions{Index: 1, Total: 1}, trimmed.callbacks())
	if !strings.Contains(trimmed.joinedLogs(), "Subtitles are not trimmed") {
		t.Errorf("trimmed download log lacks the note:\n%s", trimmed.joinedLogs())
	}

	audio := &engineRecorder{}
	result := engine.Run(context.Background(), DownloadRequest{URL: "https://example.com/v", SavePath: t.TempDir(), Format: formatMP3,
		Subtitles: subtitlesBoth, SubtitleLangs: "en"}, DownloadOptions{Index: 1, Total: 1}, audio.callbacks())
	if !strings.Contains(audio.joinedLogs(), "Not downloading subtitles: audio files cannot hold them.") || len(result.SubtitlePaths) != 0 {
		t.Errorf("audio download: subtitles %q; log:\n%s", result.SubtitlePaths, audio.joinedLogs())
	}
}

// ── Sessions ─────────────────────────────────────────────────────────────────

func TestStartDownloadKeepsSubtitlesOutOfHistory(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.prefs.subtitles.SetSelected(subtitlesBoth)
	h.app.ui.prefs.subtitleLangs.SetText("en.*")
	h.app.ui.download.entry.SetText("https://example.com/v")

	h.startAndWait(t)

	if files := h.savedFiles(t); !slices.Equal(files, []string{"GoVid_Fake Video.en.srt", "GoVid_Fake Video.mp4"}) {
		t.Errorf("saved files = %q, want the video and its .srt", files)
	}
	if entries := h.history(t); len(entries) != 1 || entries[0].FinalFilename != "GoVid_Fake Video.mp4" {
		t.Errorf("history = %+v, want only the video", entries)
	}
	logs := h.joinedLogs()
	for _, want := range []string{"[SYSTEM] Subtitles: de, en (auto-generated captions are off).", "[SYSTEM] Downloading subtitles: en."} {
		if !strings.Contains(logs, want) {
			t.Errorf("log missing %q:\n%s", want, logs)
		}
	}
}

func TestStartDownloadWarnsWhenNoSubtitleLanguageMatches(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.prefs.subtitles.SetSelected(subtitlesSRT)
	h.app.ui.prefs.subtitleLangs.SetText("ja")
	h.app.ui.prefs.autoSubtitles.SetChecked(true)
	h.app.ui.download.entry.SetText("https://example.com/v")

	h.startAndWait(t)

	if files := h.savedFiles(t); !slices.Equal(files, []string{"GoVid_Fake Video.mp4"}) {
		t.Errorf("saved files = %q, want the video alone", files)
	}
	logs := h.joinedLogs()
	for _, want := range []string{"[SYSTEM] Subtitles: de, en; auto-generated: en, fr.", `No subtitles match "ja" for this video; it will download without them.`} {
		if !strings.Contains(logs, want) {
			t.Errorf("log missing %q:\n%s", want, logs)
		}
	}
}
