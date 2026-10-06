package main

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

// ── BuildArgs ────────────────────────────────────────────────────────────────

func TestBuildArgsDefaults(t *testing.T) {
	engine := NewDownloadEngine("yt-dlp", "")
	built := engine.BuildArgs(DownloadRequest{
		URL:      "https://example.com/watch?v=1",
		SavePath: "downloads",
		Format:   "MP4",
		Quality:  "Best Quality",
	})
	args := built.Args

	if built.Extension != "mp4" {
		t.Errorf("Extension = %q, want mp4", built.Extension)
	}
	if built.HasTrim {
		t.Error("HasTrim = true, want false")
	}
	if !regexp.MustCompile(`^GOVID\d+$`).MatchString(built.DownloadID) {
		t.Errorf("DownloadID = %q, want GOVID<nanos>", built.DownloadID)
	}
	if got := argAfter(args, "-f"); got != "bestvideo+bestaudio/best" {
		t.Errorf("-f = %q", got)
	}
	if got := argAfter(args, "-P"); got != "downloads" {
		t.Errorf("-P = %q, want downloads", got)
	}
	if got, want := argAfter(args, "-o"), "GoVid_%(title)s_"+built.DownloadID+".%(ext)s"; got != want {
		t.Errorf("-o = %q, want %q", got, want)
	}
	for _, flag := range []string{"--newline", "--no-part", "--no-continue", "--no-playlist"} {
		if !slices.Contains(args, flag) {
			t.Errorf("args missing %s: %q", flag, args)
		}
	}
	for flag, want := range map[string]string{
		"--merge-output-format": "mp4",
		"--remux-video":         "mp4",
		"--recode-video":        "mp4",
	} {
		if got := argAfter(args, flag); got != want {
			t.Errorf("%s = %q, want %q", flag, got, want)
		}
	}
	for _, flag := range []string{"--ffmpeg-location", "--limit-rate", "--cookies", "--download-sections", "--extract-audio"} {
		if slices.Contains(args, flag) {
			t.Errorf("args unexpectedly contain %s: %q", flag, args)
		}
	}
	if last := args[len(args)-1]; last != "https://example.com/watch?v=1" {
		t.Errorf("last arg = %q, want the URL", last)
	}
}

func TestBuildArgsFormatAndQuality(t *testing.T) {
	tests := []struct {
		format, quality string
		wantFormat      string
		wantExt         string
	}{
		{"MP4", "1080p", "bestvideo[height<=1080]+bestaudio/best[height<=1080]/best", "mp4"},
		{"MP4", "360p", "bestvideo[height<=360]+bestaudio/best[height<=360]/best", "mp4"},
		{"MKV", "Best Quality", "bestvideo+bestaudio/best", "mkv"},
		{"MKV", "720p", "bestvideo[height<=720]+bestaudio/best[height<=720]/best", "mkv"},
		{"WebM", "Best Quality", "bestvideo[vcodec^=vp9]+bestaudio[acodec=opus]/bestvideo[vcodec^=av01]+bestaudio[acodec=opus]/bestvideo+bestaudio/best", "webm"},
		{"WebM", "480p", "bestvideo[vcodec^=vp9][height<=480]+bestaudio[acodec=opus]/bestvideo[vcodec^=av01][height<=480]+bestaudio[acodec=opus]/bestvideo[height<=480]+bestaudio/best", "webm"},
		{"MP3", "Best Quality", "bestaudio/best", "mp3"},
		{"M4A", "Best Quality", "bestaudio[ext=m4a]/bestaudio/best", "m4a"},
		{"MP4", "Unknown", "bestvideo+bestaudio/best", "mp4"},
		{"", "", "bestvideo+bestaudio/best", "mp4"},
	}

	engine := NewDownloadEngine("yt-dlp", "")
	for _, tt := range tests {
		t.Run(tt.format+"/"+tt.quality, func(t *testing.T) {
			built := engine.BuildArgs(DownloadRequest{URL: "u", SavePath: "s", Format: tt.format, Quality: tt.quality})
			if got := argAfter(built.Args, "-f"); got != tt.wantFormat {
				t.Errorf("-f = %q, want %q", got, tt.wantFormat)
			}
			if built.Extension != tt.wantExt {
				t.Errorf("Extension = %q, want %q", built.Extension, tt.wantExt)
			}
		})
	}
}

func TestBuildArgsAudioExtraction(t *testing.T) {
	engine := NewDownloadEngine("yt-dlp", "")
	for _, format := range []string{"MP3", "M4A"} {
		t.Run(format, func(t *testing.T) {
			built := engine.BuildArgs(DownloadRequest{URL: "u", SavePath: "s", Format: format, Quality: "Best Quality"})
			args := built.Args
			if !slices.Contains(args, "--extract-audio") {
				t.Errorf("args missing --extract-audio: %q", args)
			}
			if got := argAfter(args, "--audio-format"); got != strings.ToLower(format) {
				t.Errorf("--audio-format = %q, want %q", got, strings.ToLower(format))
			}
			if got := argAfter(args, "--audio-quality"); got != "0" {
				t.Errorf("--audio-quality = %q, want 0", got)
			}
			if slices.Contains(args, "--merge-output-format") || slices.Contains(args, "--recode-video") {
				t.Errorf("audio download should not remux/recode video: %q", args)
			}
		})
	}
}

func TestBuildArgsQualitySuffixInTemplate(t *testing.T) {
	built := NewDownloadEngine("yt-dlp", "").BuildArgs(DownloadRequest{URL: "u", SavePath: "s", Format: "MP4", Quality: "720p"})
	if got, want := argAfter(built.Args, "-o"), "GoVid_%(title)s_720p_"+built.DownloadID+".%(ext)s"; got != want {
		t.Errorf("-o = %q, want %q", got, want)
	}
}

func TestBuildArgsTrim(t *testing.T) {
	tests := []struct {
		name             string
		start, end       string
		wantSection      string
		wantDisplayStart string
		wantDisplayEnd   string
	}{
		{"both bounds", "00:01:00", "00:02:30", "*00:01:00-00:02:30", "00:01:00", "00:02:30"},
		{"start only", "90", "", "*90-inf", "90", "end"},
		{"end only", "", "1:30", "*0-1:30", "start", "1:30"},
	}

	engine := NewDownloadEngine("yt-dlp", "")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			built := engine.BuildArgs(DownloadRequest{URL: "u", SavePath: "s", Format: "MP4", Quality: "1080p", TrimStart: tt.start, TrimEnd: tt.end})

			if !built.HasTrim {
				t.Fatal("HasTrim = false, want true")
			}
			if got := argAfter(built.Args, "--download-sections"); got != tt.wantSection {
				t.Errorf("--download-sections = %q, want %q", got, tt.wantSection)
			}
			if !slices.Contains(built.Args, "--force-keyframes-at-cuts") {
				t.Error("args missing --force-keyframes-at-cuts")
			}
			if built.TrimDisplayStart != tt.wantDisplayStart || built.TrimDisplayEnd != tt.wantDisplayEnd {
				t.Errorf("trim display = %q → %q, want %q → %q", built.TrimDisplayStart, built.TrimDisplayEnd, tt.wantDisplayStart, tt.wantDisplayEnd)
			}
			if got, want := argAfter(built.Args, "-o"), "GoVid_%(title)s_1080p_TRIM_"+built.DownloadID+".%(ext)s"; got != want {
				t.Errorf("-o = %q, want %q", got, want)
			}
			if last := built.Args[len(built.Args)-1]; last != "u" {
				t.Errorf("last arg = %q, want the URL", last)
			}
		})
	}
}

func TestBuildArgsOptionalFlags(t *testing.T) {
	dir := t.TempDir()
	ffmpegPath := filepath.Join(dir, "ffmpeg.exe")
	cookiesPath := filepath.Join(dir, "cookies.txt")
	for _, path := range []string{ffmpegPath, cookiesPath} {
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	built := NewDownloadEngine("yt-dlp", ffmpegPath).BuildArgs(DownloadRequest{
		URL: "u", SavePath: "s", Format: "MP4", MaxSpeed: "5M", CookiesPath: cookiesPath,
	})
	for flag, want := range map[string]string{
		"--ffmpeg-location": ffmpegPath,
		"--limit-rate":      "5M",
		"--cookies":         cookiesPath,
	} {
		if got := argAfter(built.Args, flag); got != want {
			t.Errorf("%s = %q, want %q", flag, got, want)
		}
	}

	// Paths that do not exist are silently dropped rather than passed to yt-dlp.
	missing := NewDownloadEngine("yt-dlp", filepath.Join(dir, "no-ffmpeg")).BuildArgs(DownloadRequest{
		URL: "u", SavePath: "s", Format: "MP4", CookiesPath: filepath.Join(dir, "no-cookies.txt"),
	})
	for _, flag := range []string{"--ffmpeg-location", "--cookies"} {
		if slices.Contains(missing.Args, flag) {
			t.Errorf("args contain %s for a missing file: %q", flag, missing.Args)
		}
	}
}

// ── Execute / Run ────────────────────────────────────────────────────────────

// engineRecorder captures engine callbacks; the scanner calls OnLog from two
// goroutines, so access is guarded by a mutex.
type engineRecorder struct {
	mu       sync.Mutex
	logs     []string
	statuses []string
	progress []float64
	phases   []string
	onLog    func(line string) // optional hook run for each log line
}

func (r *engineRecorder) callbacks() ProcessCallbacks {
	return ProcessCallbacks{
		OnLog: func(line string, _ color.Color) {
			r.mu.Lock()
			r.logs = append(r.logs, line)
			hook := r.onLog
			r.mu.Unlock()
			if hook != nil {
				hook(line)
			}
		},
		OnStatus: func(msg string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.statuses = append(r.statuses, msg)
		},
		OnProgress: func(pct float64, _ string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.progress = append(r.progress, pct)
		},
		OnPhase: func(phase string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.phases = append(r.phases, phase)
		},
	}
}

func (r *engineRecorder) joinedLogs() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.logs, "\n")
}

// fakeDownloadArgs builds real yt-dlp arguments targeting a temp directory.
func fakeDownloadArgs(t *testing.T) (DownloadArgs, string) {
	t.Helper()
	saveDir := t.TempDir()
	built := NewDownloadEngine("yt-dlp", "").BuildArgs(DownloadRequest{
		URL: "https://example.com/v", SavePath: saveDir, Format: "MP4", Quality: "Best Quality",
	})
	return built, saveDir
}

func TestExecuteSuccess(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-download")
	runs := useFakeToolState(t)
	built, _ := fakeDownloadArgs(t)
	engine := NewDownloadEngine(fakeToolPath(t), "")
	rec := &engineRecorder{}

	scan, err := engine.Execute(context.Background(), built.Args, DownloadOptions{Index: 1, Total: 1}, rec.callbacks())

	if err != nil {
		t.Fatalf("Execute() error = %v, log:\n%s", err, rec.joinedLogs())
	}
	if runs() != 1 {
		t.Errorf("yt-dlp started %d times, want 1", runs())
	}
	if !slices.Contains(rec.statuses, "Status: Downloading...") {
		t.Errorf("statuses = %q, want Status: Downloading...", rec.statuses)
	}
	if !scan.wasConverted || !slices.Equal(scan.sourceExts, []string{"webm"}) {
		t.Errorf("scan = %+v, want merged webm source", scan)
	}
	if !slices.Equal(rec.phases, []string{phaseMerging}) {
		t.Errorf("phases = %q, want the merge reported once", rec.phases)
	}
	if !slices.Contains(rec.progress, 1.0) {
		t.Errorf("progress = %v, want to reach 1.0", rec.progress)
	}
}

func TestExecuteReportsBatchPosition(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-download")
	built, _ := fakeDownloadArgs(t)
	rec := &engineRecorder{}

	_, err := NewDownloadEngine(fakeToolPath(t), "").Execute(context.Background(), built.Args, DownloadOptions{Index: 2, Total: 3}, rec.callbacks())

	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !slices.Contains(rec.statuses, "Status: Downloading (2 of 3)...") {
		t.Errorf("statuses = %q, want batch position", rec.statuses)
	}
}

func TestExecuteRetryPolicy(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		autoRetry bool
		wantErr   bool
		wantRuns  int
	}{
		{"permanent error is not retried", "fail", true, true, 1},
		{"transient error without auto-retry", "ytdlp-transient", false, true, 1},
		{"transient error retried until success", "ytdlp-transient-once", true, false, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = test.NewApp()
			useFakeTool(t, tt.mode)
			runs := useFakeToolState(t)
			built, _ := fakeDownloadArgs(t)
			rec := &engineRecorder{}

			_, err := NewDownloadEngine(fakeToolPath(t), "").Execute(context.Background(), built.Args, DownloadOptions{AutoRetry: tt.autoRetry, Index: 1, Total: 1}, rec.callbacks())

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if runs() != tt.wantRuns {
				t.Errorf("yt-dlp started %d times, want %d", runs(), tt.wantRuns)
			}
			retried := strings.Contains(rec.joinedLogs(), "Transient error detected")
			if retried != (tt.wantRuns > 1) {
				t.Errorf("retry message logged = %v, want %v; log:\n%s", retried, tt.wantRuns > 1, rec.joinedLogs())
			}
		})
	}
}

func TestExecuteCancelDuringRetryBackoff(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-transient")
	runs := useFakeToolState(t)
	built, _ := fakeDownloadArgs(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &engineRecorder{onLog: func(line string) {
		if strings.Contains(line, "Transient error detected") {
			cancel()
		}
	}}

	start := time.Now()
	_, err := NewDownloadEngine(fakeToolPath(t), "").Execute(ctx, built.Args, DownloadOptions{AutoRetry: true, Index: 1, Total: 1}, rec.callbacks())

	if err == nil {
		t.Error("Execute() error = nil, want the failed attempt's error")
	}
	if runs() != 1 {
		t.Errorf("yt-dlp started %d times, want 1 (no retry after cancel)", runs())
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("Execute() took %v; cancellation should skip the backoff delay", elapsed)
	}
}

func TestExecuteCancelWhileRunning(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-hang")
	built, _ := fakeDownloadArgs(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &engineRecorder{onLog: func(line string) {
		if strings.Contains(line, "%") {
			cancel()
		}
	}}

	start := time.Now()
	_, err := NewDownloadEngine(fakeToolPath(t), "").Execute(ctx, built.Args, DownloadOptions{AutoRetry: true, Index: 1, Total: 1}, rec.callbacks())

	if err == nil {
		t.Error("Execute() error = nil, want an error for the killed process")
	}
	if ctx.Err() != context.Canceled {
		t.Errorf("ctx.Err() = %v, want context.Canceled", ctx.Err())
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("Execute() took %v; the process should be killed on cancel", elapsed)
	}
}

// fileSize returns the size of the file at path, or 0 if it does not exist.
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func TestExecuteCancelKillsChildProcesses(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-spawn-child")
	tickPath := filepath.Join(t.TempDir(), "ticks")
	t.Setenv(fakeToolTickEnv, tickPath)
	built, _ := fakeDownloadArgs(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := NewDownloadEngine(fakeToolPath(t), "").Execute(ctx, built.Args, DownloadOptions{Index: 1, Total: 1}, (&engineRecorder{}).callbacks())
		done <- err
	}()

	// Wait until the child process is running.
	deadline := time.Now().Add(30 * time.Second)
	for fileSize(tickPath) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("child process never started ticking")
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Execute did not return after cancel; a child process is holding its output pipe")
	}

	// The child ticks every 20 ms while alive, so its file must stop growing.
	before := fileSize(tickPath)
	time.Sleep(500 * time.Millisecond)
	if after := fileSize(tickPath); after != before {
		t.Errorf("child process still running after cancel (tick file grew from %d to %d bytes)", before, after)
	}
}

func TestRunCancelRemovesPartialFiles(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-hang")
	saveDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec := &engineRecorder{onLog: func(line string) {
		if strings.Contains(line, "%") {
			cancel()
		}
	}}

	result := NewDownloadEngine(fakeToolPath(t), "").Run(ctx, DownloadRequest{
		URL: "https://example.com/v", SavePath: saveDir, Format: "MP4",
	}, DownloadOptions{Index: 1, Total: 1}, rec.callbacks())

	if result.Err == nil {
		t.Fatal("Run() error = nil, want the cancelled process's error")
	}
	entries, err := os.ReadDir(saveDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("save folder still holds %d file(s), want the partial file removed", len(entries))
	}
	if !strings.Contains(rec.joinedLogs(), "[SYSTEM] Removed partial file: GoVid_Fake Video_GOVID") {
		t.Errorf("log missing the removal message:\n%s", rec.joinedLogs())
	}
}

func TestRemovePartialFilesKeepsOtherFiles(t *testing.T) {
	dir := t.TempDir()
	const id = "GOVID42"
	touch(t, filepath.Join(dir, "GoVid_A_"+id+".mp4"))
	touch(t, filepath.Join(dir, "GoVid_A_"+id+".f248.webm"))
	touch(t, filepath.Join(dir, "GoVid_B.mp4"))

	NewDownloadEngine("", "").RemovePartialFiles(dir, id, func(string, color.Color) {})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "GoVid_B.mp4" {
		t.Errorf("remaining files = %v, want only GoVid_B.mp4", entries)
	}
}

func TestExecuteLaunchFailure(t *testing.T) {
	built, _ := fakeDownloadArgs(t)
	rec := &engineRecorder{}
	missing := filepath.Join(t.TempDir(), "no-such-yt-dlp.exe")

	_, err := NewDownloadEngine(missing, "").Execute(context.Background(), built.Args, DownloadOptions{AutoRetry: true, Index: 1, Total: 1}, rec.callbacks())

	if err == nil {
		t.Fatal("Execute() error = nil for a missing binary")
	}
	if len(rec.statuses) != 1 || !strings.HasPrefix(rec.statuses[0], "Failed to launch yt-dlp") {
		t.Errorf("statuses = %q, want launch failure", rec.statuses)
	}
}

func TestRunSuccessFinalizesFiles(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-download")
	saveDir := t.TempDir()
	rec := &engineRecorder{}

	result := NewDownloadEngine(fakeToolPath(t), "").Run(context.Background(), DownloadRequest{
		URL: "https://example.com/v", SavePath: saveDir, Format: "MKV", Quality: "720p", TrimEnd: "30",
	}, DownloadOptions{Index: 1, Total: 1}, rec.callbacks())

	if result.Err != nil {
		t.Fatalf("Run() error = %v, log:\n%s", result.Err, rec.joinedLogs())
	}
	if result.Extension != "mkv" {
		t.Errorf("Extension = %q, want mkv", result.Extension)
	}
	want := filepath.Join(saveDir, "GoVid_Fake Video_720p_TRIM.mkv")
	if !slices.Equal(result.FinalPaths, []string{want}) {
		t.Fatalf("FinalPaths = %q, want %q", result.FinalPaths, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("finalized file missing: %v", err)
	}
	if !strings.Contains(rec.joinedLogs(), "[SYSTEM] Trimming: start → 30") {
		t.Errorf("log missing trim message:\n%s", rec.joinedLogs())
	}
}

func TestRunFailureSkipsFinalize(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "fail")
	saveDir := t.TempDir()

	result := NewDownloadEngine(fakeToolPath(t), "").Run(context.Background(), DownloadRequest{
		URL: "https://example.com/v", SavePath: saveDir, Format: "MP4",
	}, DownloadOptions{Index: 1, Total: 1}, (&engineRecorder{}).callbacks())

	if result.Err == nil {
		t.Error("Run() error = nil, want failure")
	}
	if result.FinalPaths != nil {
		t.Errorf("FinalPaths = %q, want nil", result.FinalPaths)
	}
}

// ── FinalizeFiles / uniquePath ───────────────────────────────────────────────

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeFilesStripsDownloadID(t *testing.T) {
	dir := t.TempDir()
	const id = "GOVID123"
	touch(t, filepath.Join(dir, "GoVid_Clip_"+id+".mp4"))
	touch(t, filepath.Join(dir, "GoVid_Clip_"+id+".en.vtt"))
	touch(t, filepath.Join(dir, "Unrelated.mp4"))

	var logs []string
	paths := NewDownloadEngine("", "").FinalizeFiles(dir, id, func(line string, _ color.Color) { logs = append(logs, line) })

	slices.Sort(paths)
	want := []string{filepath.Join(dir, "GoVid_Clip.en.vtt"), filepath.Join(dir, "GoVid_Clip.mp4")}
	if !slices.Equal(paths, want) {
		t.Errorf("FinalizeFiles() = %q, want %q", paths, want)
	}
	for _, path := range append(want, filepath.Join(dir, "Unrelated.mp4")) {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", filepath.Base(path), err)
		}
	}
	if len(logs) != 0 {
		t.Errorf("unexpected logs: %q", logs)
	}
}

func TestFinalizeFilesAvoidsOverwritingExisting(t *testing.T) {
	dir := t.TempDir()
	const id = "GOVID456"
	touch(t, filepath.Join(dir, "GoVid_Clip.mp4"))
	touch(t, filepath.Join(dir, "GoVid_Clip_"+id+".mp4"))

	var logs []string
	paths := NewDownloadEngine("", "").FinalizeFiles(dir, id, func(line string, _ color.Color) { logs = append(logs, line) })

	want := filepath.Join(dir, "GoVid_Clip 1.mp4")
	if !slices.Equal(paths, []string{want}) {
		t.Errorf("FinalizeFiles() = %q, want %q", paths, want)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "saving as: GoVid_Clip 1.mp4") {
		t.Errorf("logs = %q, want rename notice", logs)
	}
}

func TestFinalizeFilesNoMatches(t *testing.T) {
	paths := NewDownloadEngine("", "").FinalizeFiles(t.TempDir(), "GOVID789", func(string, color.Color) {})
	if paths != nil {
		t.Errorf("FinalizeFiles() = %q, want nil", paths)
	}
}

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Video.mp4")

	if got := uniquePath(video); got != video {
		t.Errorf("uniquePath(free) = %q, want unchanged", got)
	}

	touch(t, video)
	if got, want := uniquePath(video), filepath.Join(dir, "Video 1.mp4"); got != want {
		t.Errorf("uniquePath(taken) = %q, want %q", got, want)
	}

	touch(t, filepath.Join(dir, "Video 1.mp4"))
	touch(t, filepath.Join(dir, "Video 2.mp4"))
	if got, want := uniquePath(video), filepath.Join(dir, "Video 3.mp4"); got != want {
		t.Errorf("uniquePath(3 taken) = %q, want %q", got, want)
	}

	noExt := filepath.Join(dir, "README")
	touch(t, noExt)
	if got, want := uniquePath(noExt), filepath.Join(dir, "README 1"); got != want {
		t.Errorf("uniquePath(no extension) = %q, want %q", got, want)
	}
}
