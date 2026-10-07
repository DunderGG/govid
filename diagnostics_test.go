package main

import (
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAnonymizer(t *testing.T) {
	anon := newAnonymizer(`C:\Users\Jane`, "Jane", `C:\Users\Jane\secret\yt cookies.txt`)
	tests := []struct{ in, want string }{
		{`Save path: C:\Users\Jane\Videos`, `Save path: %USERPROFILE%\Videos`},
		{`path: "c:/users/jane/Videos"`, `path: "%USERPROFILE%/Videos"`},
		{`[debug] Command-line config: ['--cookies', 'C:\\Users\\Jane\\secret\\yt cookies.txt', 'https://x']`, `[debug] Command-line config: ['--cookies', '<cookies file>', 'https://x']`},
		{`signed in as Jane.`, `signed in as %USERNAME%.`},
		{`JaneDoe and github.com/JaneGG stay`, `JaneDoe and github.com/JaneGG stay`},
	}
	for _, tt := range tests {
		if got := anon.apply(tt.in); got != tt.want {
			t.Errorf("apply(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTrackTool(t *testing.T) {
	before := runningTools()
	done := trackTool(toolFFmpeg)
	if runningToolCounts[toolFFmpeg].Load() < 1 {
		t.Error("the ffmpeg process was not counted")
	}
	done()
	done() // a second call changes nothing
	if after := runningTools(); after != before {
		t.Errorf("runningTools() = %q after the process ended, want %q", after, before)
	}
}

func TestDiagnosticsReportHasEverySectionAndNoPrivateData(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	home, _ := os.UserHomeDir()
	username := os.Getenv("USERNAME")
	if current, err := user.Current(); err == nil {
		username = filepath.Base(strings.ReplaceAll(current.Username, `\`, "/"))
	}
	cookies := filepath.Join(t.TempDir(), "my cookies.txt")
	touch(t, cookies)
	prefs := snapshotPreferences(h.app.ui, h.saveDir)
	prefs.CookieSource, prefs.CookiesPath = cookieSourceFile, cookies
	// Lines as they reach the log: yt-dlp prints its arguments with
	// doubled backslashes.
	h.app.logSvc.WriteToFile("[debug] Command-line config: ['--cookies', '" + strings.ReplaceAll(cookies, `\`, `\\`) + "']")
	h.app.logSvc.WriteToFile("[SYSTEM] Save path: " + filepath.Join(home, "Videos"))
	h.app.logSvc.WriteToFile("[SYSTEM] user " + username + " started a download")

	report := h.app.diagnosticsReport(prefs)

	for _, section := range []string{"GoVid diagnostics", "== GoVid ==", "version: ", "build: ", "== System ==", "OS: ", "== Tools ==",
		"yt-dlp: ", "ffmpeg: ", "JavaScript runtime: ", "== GPU ==", "== Settings ==", `"format":`, "cookies: file set",
		"== Queue ==", "== Runtime ==", "goroutines: ", "tools running: ", "== Log (last 200 lines) ==", "started a download"} {
		if !strings.Contains(report, section) {
			t.Errorf("report has no %q:\n%s", section, report)
		}
	}
	lower := strings.ToLower(report)
	for _, private := range []string{cookies, strings.ReplaceAll(cookies, `\`, `\\`), "my cookies.txt", home} {
		if strings.Contains(lower, strings.ToLower(private)) {
			t.Errorf("report holds %q", private)
		}
	}
	if username != "" && regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(username)+`\b`).MatchString(report) {
		t.Errorf("report holds the user name %q", username)
	}
}

func TestHeartbeatReportsAFrozenUIThread(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	savedInterval := heartbeatInterval
	heartbeatInterval = 300 * time.Millisecond
	t.Cleanup(func() { heartbeatInterval = savedInterval })
	// A UI thread that is busy for 2 s before it runs anything.
	h.app.runOnUI = func(fn func()) {
		go func() {
			time.Sleep(2 * time.Second)
			fn()
		}()
	}

	h.app.setDebug(true)
	defer h.app.setDebug(false)
	deadline := time.Now().Add(15 * time.Second)
	var beat string
	for beat == "" && time.Now().Before(deadline) {
		for _, line := range h.app.logSvc.Recent(diagnosticsLogLines) {
			if strings.Contains(line, "Heartbeat: UI round trip") {
				beat = line
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	match := regexp.MustCompile(`UI round trip ([0-9.]+)s`).FindStringSubmatch(beat)
	if match == nil || parseSeconds(match[1]) < 2 {
		t.Fatalf("heartbeat = %q, want a round trip of 2 s or more", beat)
	}
	if !strings.Contains(beat, "goroutines") || !strings.Contains(beat, "tools running: yt-dlp") || !strings.Contains(beat, "log lines waiting") {
		t.Errorf("heartbeat = %q", beat)
	}
	stalled := false
	for _, line := range h.app.logSvc.Recent(diagnosticsLogLines) {
		stalled = stalled || strings.Contains(line, "the UI thread has not answered")
	}
	if !stalled {
		t.Error("the stalled UI thread was not reported while it stalled")
	}
}

func TestLoopMarkersOnlyInDebugMode(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	count := func() int {
		n := 0
		for _, line := range h.app.logSvc.Recent(diagnosticsLogLines) {
			if strings.Contains(line, "Loop started: test loop") {
				n++
			}
		}
		return n
	}

	markLoop("test loop", "started")
	h.app.setDebug(true)
	markLoop("test loop", "started")
	h.app.setDebug(false)
	markLoop("test loop", "started")

	if got := count(); got != 1 {
		t.Errorf("%d markers logged, want only the one made in debug mode", got)
	}
}

// parseSeconds parses a number of seconds, -1 when it is not one.
func parseSeconds(text string) float64 {
	seconds, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return -1
	}
	return seconds
}
