package main

import (
	"image/color"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

// logCollector records every callback the scanner makes. watchOutput reads
// stdout and stderr concurrently, so all access is guarded by a mutex.
type logCollector struct {
	mu       sync.Mutex
	lines    []string
	colors   map[string]color.Color
	progress []float64
	sizes    []string
	phases   []string
}

func newLogCollector() *logCollector {
	return &logCollector{colors: map[string]color.Color{}}
}

func (c *logCollector) callbacks() ProcessCallbacks {
	return ProcessCallbacks{
		OnLog: func(line string, col color.Color) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.lines = append(c.lines, line)
			c.colors[line] = col
		},
		OnStatus: func(string) {},
		OnProgress: func(pct float64, size string) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.progress = append(c.progress, pct)
			c.sizes = append(c.sizes, size)
		},
		OnPhase: func(phase string) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.phases = append(c.phases, phase)
		},
	}
}

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func TestWatchOutputSuccessfulMerge(t *testing.T) {
	_ = test.NewApp()
	engine := &DownloadEngine{}
	collector := newLogCollector()

	result := engine.watchOutput(
		openFixture(t, "ytdlp_stdout.log"),
		openFixture(t, "ytdlp_stderr_merge.log"),
		collector.callbacks(),
	)

	if want := []string{"webm", "m4a"}; !reflect.DeepEqual(result.sourceExts, want) {
		t.Errorf("sourceExts = %v, want %v", result.sourceExts, want)
	}
	if !result.wasConverted {
		t.Error("wasConverted = false, want true after [Merger] line")
	}
	if result.hadTransientErr {
		t.Error("hadTransientErr = true, want false")
	}

	// Every line from both streams is forwarded to the log.
	if got, want := len(collector.lines), 11+5; got != want {
		t.Errorf("logged %d lines, want %d", got, want)
	}

	wantProgress := []float64{0, 0.125, 0.5, 1, 1, 1}
	if !reflect.DeepEqual(collector.progress, wantProgress) {
		t.Errorf("progress = %v, want %v", collector.progress, wantProgress)
	}
	if collector.sizes[1] != "45.20MiB" {
		t.Errorf("sizes[1] = %q, want %q", collector.sizes[1], "45.20MiB")
	}
	if last := collector.sizes[len(collector.sizes)-1]; last != "3.10MiB" {
		t.Errorf("last size = %q, want %q", last, "3.10MiB")
	}
}

func TestWatchOutputColorsStderrBySeverity(t *testing.T) {
	_ = test.NewApp()
	engine := &DownloadEngine{}
	collector := newLogCollector()

	stderr := strings.Join([]string{
		"ERROR: Unsupported URL: https://example.com",
		"WARNING: [youtube] Falling back to generic n function search",
		"[debug] Encodings: locale cp1252",
		"[Merger] Merging formats",
	}, "\n")
	engine.watchOutput(strings.NewReader(""), strings.NewReader(stderr), collector.callbacks())

	tests := []struct {
		prefix string
		want   color.Color
	}{
		{"ERROR:", colError},
		{"WARNING:", colWarning},
		{"[debug]", colDebug},
	}
	for _, tt := range tests {
		for line, col := range collector.colors {
			if strings.HasPrefix(line, tt.prefix) && col != tt.want {
				t.Errorf("color for %q = %v, want %v", line, col, tt.want)
			}
		}
	}
	for line, col := range collector.colors {
		if strings.HasPrefix(line, "[Merger]") && col != nil {
			t.Errorf("color for %q = %v, want nil (default foreground)", line, col)
		}
	}
}

func TestWatchOutputTransientErrors(t *testing.T) {
	_ = test.NewApp()
	engine := &DownloadEngine{}

	tests := []struct {
		name    string
		fixture string
		want    bool
	}{
		{"rate limited", "ytdlp_stderr_transient.log", true},
		{"unsupported URL is permanent", "ytdlp_stderr_fatal.log", false},
		{"merge output has no errors", "ytdlp_stderr_merge.log", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.watchOutput(strings.NewReader(""), openFixture(t, tt.fixture), newLogCollector().callbacks())
			if result.hadTransientErr != tt.want {
				t.Errorf("hadTransientErr = %v, want %v", result.hadTransientErr, tt.want)
			}
		})
	}
}

func TestWatchOutputDetectsEveryTransientPattern(t *testing.T) {
	_ = test.NewApp()
	engine := &DownloadEngine{}

	for _, pattern := range transientErrPatterns {
		t.Run(pattern, func(t *testing.T) {
			stderr := strings.NewReader("ERROR: something went wrong: " + pattern)
			result := engine.watchOutput(strings.NewReader(""), stderr, newLogCollector().callbacks())
			if !result.hadTransientErr {
				t.Errorf("pattern %q not detected as transient", pattern)
			}
		})
	}
}

func TestWatchOutputIgnoresDestinationWithoutExtension(t *testing.T) {
	_ = test.NewApp()
	engine := &DownloadEngine{}

	stdout := strings.NewReader("[download] Destination: C:\\Downloads\\no_extension\n")
	result := engine.watchOutput(stdout, strings.NewReader(""), newLogCollector().callbacks())

	if len(result.sourceExts) != 0 {
		t.Errorf("sourceExts = %v, want empty", result.sourceExts)
	}
}

func TestParseProgress(t *testing.T) {
	engine := &DownloadEngine{}

	tests := []struct {
		name     string
		line     string
		wantCall bool
		wantPct  float64
		wantSize string
	}{
		{"standard progress line", "[download]  42.0% of   10.00MiB at 1.00MiB/s ETA 00:06", true, 0.42, "10.00MiB"},
		{"completion line", "[download] 100% of   45.20MiB in 00:00:07 at 6.21MiB/s", true, 1, "45.20MiB"},
		{"too few fields for size", "[download] 7.5%", true, 0.075, ""},
		{"no percent sign", "[youtube] dQw4w9WgXcQ: Downloading webpage", false, 0, ""},
		{"percent not at end of a field", "[debug] -o GoVid_%(title)s.%(ext)s", false, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			var gotPct float64
			var gotSize string
			engine.parseProgress(tt.line, ProcessCallbacks{
				OnProgress: func(pct float64, size string) {
					called = true
					gotPct, gotSize = pct, size
				},
			})

			if called != tt.wantCall {
				t.Fatalf("OnProgress called = %v, want %v", called, tt.wantCall)
			}
			if !called {
				return
			}
			if diff := gotPct - tt.wantPct; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("pct = %v, want %v", gotPct, tt.wantPct)
			}
			if gotSize != tt.wantSize {
				t.Errorf("size = %q, want %q", gotSize, tt.wantSize)
			}
		})
	}
}

func TestDetectPhase(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{`[Merger] Merging formats into "GoVid_Clip.mp4"`, phaseMerging},
		{`[VideoConvertor] Converting video from webm to mp4; Destination: GoVid_Clip.mp4`, phaseConverting},
		{`[ExtractAudio] Destination: GoVid_Clip.mp3`, phaseConverting},
		{`[download]  42.0% of 10.00MiB`, ""},
		{`[debug] ffmpeg command line: ffmpeg -i "[Merger].mp4"`, ""},
	}
	for _, tt := range tests {
		if got := detectPhase(tt.line); got != tt.want {
			t.Errorf("detectPhase(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestWatchOutputReportsPhasesFromEitherStream(t *testing.T) {
	collector := newLogCollector()
	stdout := strings.NewReader("[download] 100% of 10.00MiB\n[Merger] Merging formats into \"x.mp4\"\n")
	stderr := strings.NewReader("[ExtractAudio] Destination: x.mp3\n")

	result := NewDownloadEngine("", "").watchOutput(stdout, stderr, collector.callbacks())

	slices.Sort(collector.phases)
	if want := []string{phaseConverting, phaseMerging}; !slices.Equal(collector.phases, want) {
		t.Errorf("phases = %q, want %q", collector.phases, want)
	}
	if !result.wasConverted {
		t.Error("wasConverted = false, want true for a [Merger] line on stdout")
	}
}

func TestWatchOutputDetectsExtractorErrors(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"bot check", "ERROR: [youtube] abc: Sign in to confirm you're not a bot.", true},
		{"extraction", "ERROR: [vimeo] 123: Unable to extract info section", true},
		{"forbidden", "ERROR: unable to download video data: HTTP Error 403: Forbidden", true},
		{"warning only", "WARNING: [youtube] Unable to extract yt initial data; retrying", false},
		{"other error", "ERROR: Unsupported URL: https://example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewDownloadEngine("", "").watchOutput(strings.NewReader(""), strings.NewReader(tt.stderr+"\n"), newLogCollector().callbacks())
			if result.hadExtractorErr != tt.want {
				t.Errorf("hadExtractorErr = %v, want %v", result.hadExtractorErr, tt.want)
			}
		})
	}
}

func TestIsProgressLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"[download]  42.3% of   10.00MiB at    1.20MiB/s ETA 00:07", true},
		{"[download] 100% of   10.00MiB in 00:00:02 at 4.81MiB/s", true},
		{"[download] Destination: GoVid_Clip.f137.mp4", false},
		{"[download] GoVid_Clip.mp4 has already been downloaded", false},
		{"[Merger] Merging formats into \"GoVid_Clip.mp4\"", false},
	}
	for _, tt := range tests {
		if got := IsProgressLine(tt.line); got != tt.want {
			t.Errorf("IsProgressLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestWatchOutputDetectsAMissingJSRuntime(t *testing.T) {
	// The warning the bundled yt-dlp 2026.03.17 prints on every YouTube
	// extraction when it has no JavaScript runtime.
	warning := "WARNING: [youtube] No supported JavaScript runtime could be found. Only deno is enabled by default; to use another runtime add  --js-runtimes RUNTIME[:PATH]  to your command/config. YouTube extraction without a JS runtime has been deprecated, and some formats may be missing."
	for _, tt := range []struct {
		stderr string
		want   bool
	}{
		{warning, true},
		{"[debug] JS runtimes: deno-2.9.7", false},
	} {
		result := NewDownloadEngine("", "").watchOutput(strings.NewReader(""), strings.NewReader(tt.stderr+"\n"), newLogCollector().callbacks())
		if result.hadNoJSRuntime != tt.want {
			t.Errorf("hadNoJSRuntime = %v for %q, want %v", result.hadNoJSRuntime, tt.stderr, tt.want)
		}
	}
}

func TestWatchOutputKeepsReadingPastLongLines(t *testing.T) {
	tests := []struct {
		name      string
		lineLen   int
		wantAfter bool // the lines after the long one are read, not drained
	}{
		{"200 KiB line", 200 << 10, true},
		{"line over the limit", 2 * maxOutputLine, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := strings.Repeat("x", tt.lineLen) + "\nafter the long line\n"
			stdoutR, stdoutW := io.Pipe()
			stderrR, stderrW := io.Pipe()
			// Unblock the writers if the test fails, like a killed process.
			t.Cleanup(func() { stdoutR.Close(); stderrR.Close() })
			var writers sync.WaitGroup
			for _, w := range []*io.PipeWriter{stdoutW, stderrW} {
				writers.Go(func() {
					_, err := io.WriteString(w, output)
					w.CloseWithError(err)
				})
			}
			collector := newLogCollector()

			done := make(chan struct{})
			go func() {
				NewDownloadEngine("", "").watchOutput(stdoutR, stderrR, collector.callbacks())
				writers.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("the output was not read to the end; the process would block on a full pipe")
			}

			joined := strings.Join(collector.lines, "\n")
			if got := strings.Count(joined, "after the long line"); tt.wantAfter && got != 2 {
				t.Errorf("the line after the long one was logged %d times, want 2 (stdout and stderr)", got)
			}
			if got := strings.Count(joined, "read error"); !tt.wantAfter && got != 2 {
				t.Errorf("read errors logged %d times, want 2 (stdout and stderr)", got)
			}
		})
	}
}
