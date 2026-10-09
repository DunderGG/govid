package main

import (
	"context"
	"errors"
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
	for _, flag := range []string{"--newline", "--continue", "--no-playlist"} {
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
	for _, flag := range []string{"--ffmpeg-location", "--limit-rate", "--cookies", "--download-sections", "--extract-audio", "--js-runtimes"} {
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

func TestFormatSelectionHeight(t *testing.T) {
	tests := []struct {
		format, quality string
		wantHeight      string
	}{
		{formatMP4, qualityBest, ""},
		{formatMP4, quality1080p, "1080"},
		{formatMKV, quality720p, "720"},
		{formatWebM, quality480p, "480"},
		{formatMP4, quality360p, "360"},
		{formatMP3, quality1080p, ""}, // audio has no picture to cap
		{formatM4A, quality720p, ""},
	}
	for _, tt := range tests {
		if _, _, got := formatSelection(tt.format, tt.quality); got != tt.wantHeight {
			t.Errorf("formatSelection(%q, %q) height = %q, want %q", tt.format, tt.quality, got, tt.wantHeight)
		}
	}
}

func TestBuildArgsQualityLabelInTemplate(t *testing.T) {
	tests := []struct {
		format, quality string
		wantLabel       string
	}{
		{formatMP4, quality720p, heightLabel}, // the height downloaded, not the cap
		{formatMKV, quality1080p, heightLabel},
		{formatMP4, qualityBest, ""},
		{formatMP3, quality1080p, ""},
		{formatM4A, quality360p, ""},
	}
	engine := NewDownloadEngine("yt-dlp", "")
	for _, tt := range tests {
		built := engine.BuildArgs(DownloadRequest{URL: "u", SavePath: "s", Format: tt.format, Quality: tt.quality})
		if got, want := argAfter(built.Args, "-o"), "GoVid_%(title)s"+tt.wantLabel+"_"+built.DownloadID+".%(ext)s"; got != want {
			t.Errorf("%s/%s: -o = %q, want %q", tt.format, tt.quality, got, want)
		}
	}
}

func TestQualityFit(t *testing.T) {
	tests := []struct {
		format, quality string
		height          int
		wantMessage     string
		wantAbove       bool
	}{
		{formatMP4, quality1080p, 720, "1080p isn't available for this video; downloading 720p (the best there is).", false},
		{formatMP4, quality480p, 1080, "No version at or below 480p; downloading 1080p.", true},
		{formatMP4, quality720p, 720, "", false},
		{formatMP4, qualityBest, 360, "", false},
		{formatMP4, quality1080p, 0, "", false}, // height unknown
		{formatMP3, quality1080p, 720, "", false},
	}
	for _, tt := range tests {
		message, above := qualityFit(tt.format, tt.quality, tt.height)
		if message != tt.wantMessage || above != tt.wantAbove {
			t.Errorf("qualityFit(%q, %q, %d) = %q, %v; want %q, %v", tt.format, tt.quality, tt.height, message, above, tt.wantMessage, tt.wantAbove)
		}
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
			if got, want := argAfter(built.Args, "-o"), "GoVid_%(title)s"+heightLabel+"_TRIM_"+built.DownloadID+".%(ext)s"; got != want {
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
		OnProgress: func(pct float64) {
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
	if want := int64(len("partial")); result.Bytes != want {
		t.Errorf("Bytes = %d, want %d: the partial file's size before it was removed", result.Bytes, want)
	}
}

// Run measures what the download wrote, and how much of it an earlier,
// paused run had written, for the summary: yt-dlp's progress lines give
// only each stream's total (CR-11).
func TestRunMeasuresTheBytesDownloaded(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-resumable")
	saveDir := t.TempDir()
	const resumed = 3 * 1024 * 1024
	part := filepath.Join(saveDir, "GoVid_Fake Video_GOVID123.mp4.part")
	if err := os.WriteFile(part, make([]byte, resumed), 0644); err != nil {
		t.Fatal(err)
	}
	rec := &engineRecorder{}

	result := NewDownloadEngine(fakeToolPath(t), "").Run(context.Background(), DownloadRequest{
		URL: "https://example.com/v", SavePath: saveDir, Format: formatMP4, Quality: qualityBest, DownloadID: "GOVID123",
	}, DownloadOptions{Index: 1, Total: 1}, rec.callbacks())

	if result.Err != nil {
		t.Fatalf("Run() error = %v, log:\n%s", result.Err, rec.joinedLogs())
	}
	if want := int64(fakeResumableChunks * 1024 * 1024); result.Bytes != want || result.ResumedBytes != resumed {
		t.Errorf("Bytes, ResumedBytes = %d, %d; want %d, %d", result.Bytes, result.ResumedBytes, want, resumed)
	}
}

func TestRemovePartialFilesKeepsOtherFiles(t *testing.T) {
	forEachSaveDir(t, func(t *testing.T, dir string) {
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
	})
}

// forEachSaveDir runs fn in a plain save folder and in one whose name
// has the brackets a glob pattern would misread.
func forEachSaveDir(t *testing.T, fn func(t *testing.T, dir string)) {
	for _, name := range []string{"Videos", "Videos [HD]"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			if err := os.Mkdir(dir, 0755); err != nil {
				t.Fatal(err)
			}
			fn(t, dir)
		})
	}
}

func TestFilesWithIDFindsFilesInBracketedFolder(t *testing.T) {
	forEachSaveDir(t, func(t *testing.T, dir string) {
		const id = "GOVID77"
		touch(t, filepath.Join(dir, "GoVid_A_"+id+".mp4.part"))
		touch(t, filepath.Join(dir, "GoVid_B.mp4"))
		if err := os.Mkdir(filepath.Join(dir, "Folder_"+id), 0755); err != nil {
			t.Fatal(err)
		}

		want := []string{filepath.Join(dir, "GoVid_A_"+id+".mp4.part")}
		if got, err := filesWithID(dir, id); err != nil || !slices.Equal(got, want) {
			t.Errorf("filesWithID() = %q, %v; want %q", got, err, want)
		}
		if !NewDownloadEngine("", "").hasFiles(dir, id) {
			t.Error("hasFiles() = false, want true")
		}
		if size, found := downloadedBytes(dir, id); !found || size != 1 {
			t.Errorf("downloadedBytes() = %d, %v; want 1, true", size, found)
		}
	})
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

func TestBuildArgsLoadsInfoJSONInsteadOfURL(t *testing.T) {
	built := NewDownloadEngine("yt-dlp", "").BuildArgs(DownloadRequest{
		URL: "https://example.com/v", SavePath: "s", Format: "MP4", infoJSONPath: "info.json",
	})

	if got := argAfter(built.Args, "--load-info-json"); got != "info.json" {
		t.Errorf("--load-info-json = %q, want info.json", got)
	}
	if slices.Contains(built.Args, "https://example.com/v") {
		t.Errorf("args %q also pass the URL, which yt-dlp would download a second time", built.Args)
	}
	if got := argAfter(built.Args, "-f"); got == "" {
		t.Error("args lack -f; yt-dlp selects formats again from the loaded info")
	}
}

// fakeInfoJSON is a probe answer for the fake yt-dlp's single video.
var fakeInfoJSON = []byte(`{"_type": "video", "title": "Fake Video"}`)

func TestRunLoadsInfoJSONAndRemovesIt(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-download")
	extractions := useFakeExtractions(t)
	tempDir := t.TempDir()
	t.Setenv("TMP", tempDir) // os.TempDir on Windows
	t.Setenv("TMPDIR", tempDir)
	rec := &engineRecorder{}

	result := NewDownloadEngine(fakeToolPath(t), "").Run(context.Background(), DownloadRequest{
		URL: "https://example.com/v", SavePath: t.TempDir(), Format: "MP4", InfoJSON: fakeInfoJSON,
	}, DownloadOptions{Index: 1, Total: 1}, rec.callbacks())

	if result.Err != nil {
		t.Fatalf("Run() error = %v, log:\n%s", result.Err, rec.joinedLogs())
	}
	if n := extractions(); n != 0 {
		t.Errorf("extractions = %d, want 0 when the info is loaded", n)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(tempDir, "govid-*.info.json")); len(leftovers) != 0 {
		t.Errorf("info files left behind: %q", leftovers)
	}
}

func TestRunRetriesFromURLWhenLoadedLinksExpire(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-info-expired")
	runs := useFakeToolState(t)
	extractions := useFakeExtractions(t)
	saveDir := t.TempDir()
	rec := &engineRecorder{}

	result := NewDownloadEngine(fakeToolPath(t), "").Run(context.Background(), DownloadRequest{
		URL: "https://example.com/v", SavePath: saveDir, Format: "MP4", InfoJSON: fakeInfoJSON,
	}, DownloadOptions{Index: 1, Total: 1}, rec.callbacks())

	if result.Err != nil {
		t.Fatalf("Run() error = %v, want the retry from the URL to succeed; log:\n%s", result.Err, rec.joinedLogs())
	}
	if runs() != 2 || extractions() != 1 {
		t.Errorf("yt-dlp runs = %d with %d extractions, want 2 runs, the second from the URL", runs(), extractions())
	}
	logs := rec.joinedLogs()
	for _, want := range []string{"HTTP Error 403", "asking the site for new ones", "Removed partial file"} {
		if !strings.Contains(logs, want) {
			t.Errorf("log missing %q:\n%s", want, logs)
		}
	}
	if want := filepath.Join(saveDir, "GoVid_Fake Video.mp4"); !slices.Equal(result.FinalPaths, []string{want}) {
		t.Errorf("FinalPaths = %q, want %q", result.FinalPaths, want)
	}
}

func TestRunDoesNotRetryOtherFailuresFromURL(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "fail")
	runs := useFakeToolState(t)

	result := NewDownloadEngine(fakeToolPath(t), "").Run(context.Background(), DownloadRequest{
		URL: "https://example.com/v", SavePath: t.TempDir(), Format: "MP4", InfoJSON: fakeInfoJSON,
	}, DownloadOptions{Index: 1, Total: 1}, (&engineRecorder{}).callbacks())

	if result.Err == nil || runs() != 1 {
		t.Errorf("Run() error = %v after %d runs, want one failed run", result.Err, runs())
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
	forEachSaveDir(t, func(t *testing.T, dir string) {
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
	})
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

// errLocked stands in for a rename refused because another process holds
// the file open.
var errLocked = errors.New("the file is in use by another process")

func TestRunKeepsFileWhoseRenameFails(t *testing.T) {
	_ = test.NewApp()
	useFakeTool(t, "ytdlp-download")
	saveDir := t.TempDir()
	rec := &engineRecorder{}
	engine := NewDownloadEngine(fakeToolPath(t), "")
	var attempts int
	engine.rename = func(string, string) error { attempts++; return errLocked }

	result := engine.Run(context.Background(), DownloadRequest{
		URL: "https://example.com/v", SavePath: saveDir, Format: "MKV", Quality: "720p",
	}, DownloadOptions{Index: 1, Total: 1}, rec.callbacks())

	if result.Err != nil {
		t.Fatalf("Run() error = %v, log:\n%s", result.Err, rec.joinedLogs())
	}
	if attempts != partialRemoveAttempts {
		t.Errorf("rename attempts = %d, want %d", attempts, partialRemoveAttempts)
	}
	if len(result.FinalPaths) != 1 {
		t.Fatalf("FinalPaths = %q, want the file under its temporary name", result.FinalPaths)
	}
	path := result.FinalPaths[0]
	if !strings.Contains(filepath.Base(path), "_GOVID") {
		t.Errorf("FinalPaths[0] = %q, want the temporary name", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the finished download was removed: %v", err)
	}
	logs := rec.joinedLogs()
	if !strings.Contains(logs, "it keeps its temporary name") {
		t.Errorf("log missing the rename failure:\n%s", logs)
	}
	if strings.Contains(logs, "Removed partial file") {
		t.Errorf("log reports a removal:\n%s", logs)
	}
}

func TestFinalizeFilesRetriesLockedRename(t *testing.T) {
	dir := t.TempDir()
	const id = "GOVID321"
	touch(t, filepath.Join(dir, "GoVid_Clip_"+id+".mp4"))
	engine := NewDownloadEngine("", "")
	var attempts int
	engine.rename = func(from, to string) error {
		if attempts++; attempts < 3 {
			return errLocked
		}
		return os.Rename(from, to)
	}

	paths := engine.FinalizeFiles(dir, id, func(string, color.Color) {})

	want := filepath.Join(dir, "GoVid_Clip.mp4")
	if !slices.Equal(paths, []string{want}) {
		t.Errorf("FinalizeFiles() = %q, want %q", paths, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("renamed file missing: %v", err)
	}
}

func TestRemoveLeftoverPartialsKeepsFinishedFiles(t *testing.T) {
	forEachSaveDir(t, func(t *testing.T, dir string) {
		const id = "GOVID43"
		touch(t, filepath.Join(dir, "GoVid_A_"+id+".mp4"))
		touch(t, filepath.Join(dir, "GoVid_A_"+id+".f251.webm.part"))
		touch(t, filepath.Join(dir, "GoVid_A_"+id+".f251.webm.ytdl"))

		NewDownloadEngine("", "").RemoveLeftoverPartials(dir, id, func(string, color.Color) {})

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "GoVid_A_"+id+".mp4" {
			t.Errorf("remaining files = %v, want only the finished file", entries)
		}
	})
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
	check := func(label, path, want string) {
		t.Helper()
		if got, err := uniquePath(path); got != want || err != nil {
			t.Errorf("uniquePath(%s) = %q, %v; want %q", label, got, err, want)
		}
	}

	check("free", video, video)

	touch(t, video)
	check("taken", video, filepath.Join(dir, "Video 1.mp4"))

	touch(t, filepath.Join(dir, "Video 1.mp4"))
	touch(t, filepath.Join(dir, "Video 2.mp4"))
	check("3 taken", video, filepath.Join(dir, "Video 3.mp4"))

	noExt := filepath.Join(dir, "README")
	touch(t, noExt)
	check("no extension", noExt, filepath.Join(dir, "README 1"))
}

// A Stat error other than "not found", such as access denied, used to make
// every name taken, and uniquePath searched forever while holding renameMu.
// Here a NUL byte, which os.Stat rejects as invalid on every system.
func TestUniquePathTreatsAStatErrorAsFree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Video\x00.mp4")
	if _, err := os.Stat(path); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.Stat() error = %v, want one other than not found", err)
	}
	done := make(chan struct{})
	var got string
	var err error

	go func() {
		defer close(done)
		got, err = uniquePath(path)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("uniquePath() did not return")
	}
	if got != path || err != nil {
		t.Errorf("uniquePath() = %q, %v; want the path itself, for the rename to report", got, err)
	}
}

func TestUniquePathGivesUpAfterItsBound(t *testing.T) {
	checks := 0
	allTaken := func(string) bool { checks++; return true }

	got, err := freePath("Video.mp4", allTaken)

	if !errors.Is(err, errNoFreeName) || got != "" {
		t.Errorf("freePath() = %q, %v; want errNoFreeName and no path, so nothing is replaced", got, err)
	}
	if checks != maxUniquePathTries+1 {
		t.Errorf("checked %d names, want %d", checks, maxUniquePathTries+1)
	}
}

func TestBuildArgsEmbedding(t *testing.T) {
	tests := []struct {
		name        string
		format      string
		metadata    bool
		thumbnail   bool
		chapters    bool
		wantFlags   []string
		wantSkipped bool
	}{
		{"all on, MP3", formatMP3, true, true, true, []string{"--embed-metadata", "--embed-thumbnail", "--convert-thumbnails", "jpg", "--embed-chapters"}, false},
		{"defaults, MP4", formatMP4, true, true, false, []string{"--embed-metadata", "--embed-thumbnail", "--convert-thumbnails", "jpg"}, false},
		{"WebM cannot hold a cover", formatWebM, true, true, true, []string{"--embed-metadata", "--embed-chapters"}, true},
		{"all off", formatMKV, false, false, false, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			built := NewDownloadEngine("yt-dlp", "").BuildArgs(DownloadRequest{
				URL: "u", SavePath: "s", Format: tt.format, Quality: qualityBest,
				EmbedMetadata: tt.metadata, EmbedThumbnail: tt.thumbnail, EmbedChapters: tt.chapters,
			})
			var got []string
			for i, arg := range built.Args {
				if strings.HasPrefix(arg, "--embed-") || arg == "--convert-thumbnails" || (i > 0 && built.Args[i-1] == "--convert-thumbnails") {
					got = append(got, arg)
				}
			}
			if !slices.Equal(got, tt.wantFlags) {
				t.Errorf("embed flags = %q, want %q", got, tt.wantFlags)
			}
			if built.ThumbnailSkipped != tt.wantSkipped {
				t.Errorf("ThumbnailSkipped = %v, want %v", built.ThumbnailSkipped, tt.wantSkipped)
			}
			if last := built.Args[len(built.Args)-1]; last != "u" {
				t.Errorf("last arg = %q, want the URL", last)
			}
		})
	}
}

func TestBuildArgsAndProbePassTheJSRuntime(t *testing.T) {
	engine := NewDownloadEngine("yt-dlp", "")
	engine.JSRuntime = JSRuntime{Name: "deno", Path: filepath.Join("C:", "GoVid", "bin", "deno.exe"), Version: "2.9.7", InBin: true}
	want := "deno:" + engine.JSRuntime.Path
	req := DownloadRequest{URL: "https://example.com/v", SavePath: "s", Format: formatMP4, Quality: qualityBest}

	if got := argAfter(engine.BuildArgs(req).Args, "--js-runtimes"); got != want {
		t.Errorf("BuildArgs --js-runtimes = %q, want %q", got, want)
	}
	for _, singleVideo := range []bool{false, true} {
		if got := argAfter(engine.probeArgs(req, singleVideo), "--js-runtimes"); got != want {
			t.Errorf("probeArgs(singleVideo=%v) --js-runtimes = %q, want %q", singleVideo, got, want)
		}
	}
}

func TestStartDownloadPassesTheRuntimeItFinds(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	denoPath := installFakeTool(t, h.app.depSvc.binDir, "deno")
	runs := useFakeArgs(t)
	h.app.ui.download.entry.SetText("https://example.com/watch?v=1")

	h.startAndWait(t)

	got := runs()
	if len(got) != 2 {
		t.Fatalf("yt-dlp ran %d times, want a probe and a download: %q", len(got), got)
	}
	for _, args := range got {
		if value := argAfter(args, "--js-runtimes"); value != "deno:"+denoPath {
			t.Errorf("--js-runtimes = %q in %q, want deno:%s", value, args, denoPath)
		}
	}
}
