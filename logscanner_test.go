package main

import (
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

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
