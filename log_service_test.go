package main

import (
	"image/color"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestDailyLogPaths(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		got     string
		pattern string
	}{
		{"session log", SessionLogPath(dir), `^GoVid_log_\d{4}-\d{2}-\d{2}\.txt$`},
		{"error log", ErrorLogPath(dir), `^GoVid_errors_\d{4}-\d{2}-\d{2}\.txt$`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if filepath.Dir(tt.got) != dir {
				t.Errorf("dir = %q, want %q", filepath.Dir(tt.got), dir)
			}
			if !regexp.MustCompile(tt.pattern).MatchString(filepath.Base(tt.got)) {
				t.Errorf("base name %q does not match %s", filepath.Base(tt.got), tt.pattern)
			}
		})
	}
}

func TestSessionLogLifecycle(t *testing.T) {
	dir := t.TempDir()
	svc := NewLogService()

	if svc.IsActive() {
		t.Fatal("IsActive() = true before OpenSessionLog")
	}

	path, err := svc.OpenSessionLog(dir)
	if err != nil {
		t.Fatalf("OpenSessionLog: %v", err)
	}
	if path != SessionLogPath(dir) {
		t.Errorf("path = %q, want %q", path, SessionLogPath(dir))
	}
	if !svc.IsActive() {
		t.Error("IsActive() = false after OpenSessionLog")
	}

	svc.WriteToFile("hello session")
	svc.CloseSessionLog()

	if svc.IsActive() {
		t.Error("IsActive() = true after CloseSessionLog")
	}

	content := readFile(t, path)
	if !regexp.MustCompile(`(?m)^\[\d{2}:\d{2}:\d{2}\] hello session$`).MatchString(strings.ReplaceAll(content, "\r", "")) {
		t.Errorf("log missing timestamped line, got:\n%s", content)
	}
	if !strings.Contains(content, "[SYSTEM] Log file closed.") {
		t.Errorf("log missing closing marker, got:\n%s", content)
	}

	// Closing again is a no-op and must not panic.
	svc.CloseSessionLog()
}

func TestSessionLogAppendsAcrossSessions(t *testing.T) {
	dir := t.TempDir()
	svc := NewLogService()

	for _, line := range []string{"first session", "second session"} {
		if _, err := svc.OpenSessionLog(dir); err != nil {
			t.Fatalf("OpenSessionLog: %v", err)
		}
		svc.WriteToFile(line)
		svc.CloseSessionLog()
	}

	content := readFile(t, SessionLogPath(dir))
	first := strings.Index(content, "first session")
	second := strings.Index(content, "second session")
	if first == -1 || second == -1 || first > second {
		t.Errorf("expected both sessions in order, got:\n%s", content)
	}
}

func TestPreSessionLinesAreFlushedOnOpen(t *testing.T) {
	dir := t.TempDir()
	svc := NewLogService()

	svc.WriteToFile("startup check")
	if _, err := svc.OpenSessionLog(dir); err != nil {
		t.Fatalf("OpenSessionLog: %v", err)
	}
	svc.WriteToFile("during session")
	svc.CloseSessionLog()

	content := readFile(t, SessionLogPath(dir))
	startup := strings.Index(content, "startup check")
	during := strings.Index(content, "during session")
	if startup == -1 || during == -1 || startup > during {
		t.Errorf("expected buffered line before session line, got:\n%s", content)
	}

	// The buffer is drained once flushed, so a second session starts clean.
	if _, err := svc.OpenSessionLog(dir); err != nil {
		t.Fatalf("second OpenSessionLog: %v", err)
	}
	svc.CloseSessionLog()
	if n := strings.Count(readFile(t, SessionLogPath(dir)), "startup check"); n != 1 {
		t.Errorf("buffered line written %d times, want 1", n)
	}
}

func TestPreSessionBufferIsCappedToBufferLimit(t *testing.T) {
	dir := t.TempDir()
	svc := NewLogService()
	svc.SetBufferLimit(3)

	for _, line := range []string{"line-1", "line-2", "line-3", "line-4", "line-5"} {
		svc.WriteToFile(line)
	}
	if _, err := svc.OpenSessionLog(dir); err != nil {
		t.Fatalf("OpenSessionLog: %v", err)
	}
	svc.CloseSessionLog()

	content := readFile(t, SessionLogPath(dir))
	for _, dropped := range []string{"line-1", "line-2"} {
		if strings.Contains(content, dropped) {
			t.Errorf("oldest line %q should have been dropped, got:\n%s", dropped, content)
		}
	}
	for _, kept := range []string{"line-3", "line-4", "line-5"} {
		if !strings.Contains(content, kept) {
			t.Errorf("newest line %q missing, got:\n%s", kept, content)
		}
	}
}

func TestWriteToErrorLogUsesSessionDir(t *testing.T) {
	dir := t.TempDir()
	svc := NewLogService()

	if _, err := svc.OpenSessionLog(dir); err != nil {
		t.Fatalf("OpenSessionLog: %v", err)
	}
	svc.WriteToErrorLog("ERROR: first failure")
	svc.WriteToErrorLog("ERROR: second failure")
	svc.CloseSessionLog()

	content := readFile(t, ErrorLogPath(dir))
	for _, want := range []string{"ERROR: first failure", "ERROR: second failure"} {
		if !strings.Contains(content, want) {
			t.Errorf("error log missing %q, got:\n%s", want, content)
		}
	}
	if strings.Contains(readFile(t, SessionLogPath(dir)), "first failure") {
		t.Error("WriteToErrorLog should not write to the session log")
	}
}

func TestBufferLimitAccessors(t *testing.T) {
	svc := NewLogService()
	if got := svc.BufferLimit(); got != 200 {
		t.Errorf("default BufferLimit() = %d, want 200", got)
	}
	svc.SetBufferLimit(500)
	if got := svc.BufferLimit(); got != 500 {
		t.Errorf("BufferLimit() = %d, want 500", got)
	}
}

// Run with -race: the preferences window changes the limit on the UI thread
// while background goroutines buffer pre-session lines.
func TestBufferLimitConcurrentWithWriteToFile(t *testing.T) {
	svc := NewLogService()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 100 {
			svc.SetBufferLimit(50 + i)
			_ = svc.BufferLimit()
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			svc.WriteToFile("line")
		}
	}()
	wg.Wait()
}

func TestIsErrorLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"ERROR: Unsupported URL", true},
		{"[ffmpeg] Conversion failed!", true},
		{"Post-processing FAILED for clip.mp4", true},
		{"some error happened", true},
		{"[debug] Encodings: error utf-8 (No ANSI)", false},
		{"[download] 50.0% of 10.00MiB", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsErrorLine(tt.line); got != tt.want {
			t.Errorf("IsErrorLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestParseBufferLimit(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"100", 100},
		{"5000", 5000},
		{"Unlimited", math.MaxInt32},
		{"", 200},
		{"abc", 200},
		{"0", 200},
		{"-10", 200},
	}

	for _, tt := range tests {
		if got := ParseBufferLimit(tt.in); got != tt.want {
			t.Errorf("ParseBufferLimit(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestWriteSessionConfig(t *testing.T) {
	svc := NewLogService()
	var lines []string
	collect := func(line string, _ color.Color) { lines = append(lines, line) }

	svc.WriteSessionConfig(SessionConfig{
		URLs:      []string{"https://example.com/a", "https://example.com/b"},
		SavePath:  "/downloads",
		BatchMode: true,
		Format:    "MP4",
		Quality:   "1080p",
	}, collect)

	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"[SYSTEM] Save path: /downloads",
		"[SYSTEM] Mode: batch=true, url_count=2",
		"[SYSTEM] Format/quality: MP4 / 1080p",
		"[SYSTEM] Max speed: (none)",
		"[SYSTEM] Cookies file: (none)",
		`[SYSTEM] URL field (raw): "(empty)"`,
		"[SYSTEM] URL[1]: https://example.com/a",
		"[SYSTEM] URL[2]: https://example.com/b",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("session config missing %q, got:\n%s", want, joined)
		}
	}
	if !strings.HasPrefix(lines[0], "[SYSTEM] =====") || !strings.HasPrefix(lines[len(lines)-1], "[SYSTEM] =====") {
		t.Errorf("session config should be wrapped in banner lines, got first=%q last=%q", lines[0], lines[len(lines)-1])
	}
}
