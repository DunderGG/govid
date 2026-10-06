package main

// fake_tool_test.go — Lets the test binary stand in for external tools.
//
// When GOVID_FAKE_TOOL is set, TestMain runs the requested fake behaviour
// instead of the test suite and exits. Tests point a service at the test
// binary (directly, or via installFakeTool under the expected tool name) and
// select the behaviour with useFakeTool, so no real yt-dlp or FFmpeg is needed.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeToolEnv selects the fake tool behaviour in the child process.
const fakeToolEnv = "GOVID_FAKE_TOOL"

// fakeToolStateEnv optionally names a file the fake tool appends to on every
// run, so tests can count invocations across retries.
const fakeToolStateEnv = "GOVID_FAKE_TOOL_STATE"

// fakeColorEnv holds the "transfer,primaries,space" colour tags reported by
// the "ffprobe-color" and "ffmpeg-summary" modes.
const fakeColorEnv = "GOVID_FAKE_COLOR"

// fakeFFprobeColor mimics ffprobe printing a stream's colour entries.
func fakeFFprobeColor() int {
	transfer, rest, _ := strings.Cut(os.Getenv(fakeColorEnv), ",")
	primaries, space, _ := strings.Cut(rest, ",")
	fmt.Printf("color_space=%s\ncolor_transfer=%s\ncolor_primaries=%s\n", space, transfer, primaries)
	return 0
}

// fakeFFmpegSummary mimics "ffmpeg -i in.mkv" with no output file: it prints
// the input summary, colour tags included, and fails.
func fakeFFmpegSummary() int {
	transfer, rest, _ := strings.Cut(os.Getenv(fakeColorEnv), ",")
	primaries, space, _ := strings.Cut(rest, ",")
	// ffmpeg names the matrix, primaries, and transfer, in that order.
	tags := fmt.Sprintf("%s/%s/%s, ", space, primaries, transfer)
	if transfer == "" && primaries == "" && space == "" {
		tags = ""
	}
	fmt.Fprintln(os.Stderr, "Input #0, matroska,webm, from 'in.mkv':")
	fmt.Fprintf(os.Stderr, "  Stream #0:0: Video: vp9 (Profile 2), yuv420p10le(tv, %sprogressive), 3840x2160\n", tags)
	fmt.Fprintln(os.Stderr, "At least one output file must be specified")
	return 1
}

// fakeToolVersion is the version string the "version" mode reports.
const fakeToolVersion = "2026.09.01"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeToolEnv); mode != "" {
		os.Exit(runFakeTool(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// runFakeTool implements the fake tool modes and returns the exit code.
func runFakeTool(mode string, args []string) int {
	// Each invocation appends a line to the state file (when one is set), so
	// tests can count attempts and modes can behave differently on retries.
	previousRuns := 0
	if statePath := os.Getenv(fakeToolStateEnv); statePath != "" {
		if data, err := os.ReadFile(statePath); err == nil {
			previousRuns = strings.Count(string(data), "\n")
		}
		if file, err := os.OpenFile(statePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			fmt.Fprintln(file, mode)
			file.Close()
		}
	}

	switch mode {
	case "ytdlp-download":
		return fakeYtDlpDownload(args)
	case "ytdlp-transient":
		return fakeYtDlpTransient()
	case "ytdlp-transient-once":
		if previousRuns == 0 {
			return fakeYtDlpTransient()
		}
		return fakeYtDlpDownload(args)
	case "ytdlp-hang":
		return fakeYtDlpHang(args)
	case "ytdlp-hang-once":
		if previousRuns == 0 {
			return fakeYtDlpHang(args)
		}
		return fakeYtDlpDownload(args)
	case "ytdlp-spawn-child":
		return fakeYtDlpSpawnChild()
	case "tick":
		return fakeTick()
	case "ffprobe-color":
		return fakeFFprobeColor()
	case "ffmpeg-summary":
		return fakeFFmpegSummary()
	case "version":
		if !slices.Contains(args, "--version") {
			fmt.Fprintf(os.Stderr, "fake tool: expected --version, got %q\n", args)
			return 4
		}
		fmt.Printf("  %s  \n", fakeToolVersion)
		return 0
	case "update":
		if !slices.Contains(args, "-U") {
			fmt.Fprintf(os.Stderr, "fake tool: expected -U, got %q\n", args)
			return 4
		}
		fmt.Println("Current version: stable@" + fakeToolVersion)
		fmt.Println("yt-dlp is up to date (stable@" + fakeToolVersion + ")")
		return 0
	case "fail":
		fmt.Fprintln(os.Stderr, "ERROR: fake tool failure")
		return 1
	case "ytdlp-extractor-error":
		fmt.Fprintln(os.Stderr, "ERROR: [youtube] fake: Unable to extract initial player response")
		return 1
	}
	fmt.Fprintf(os.Stderr, "fake tool: unknown mode %q\n", mode)
	return 3
}

// fakeYtDlpDownload mimics a successful yt-dlp run: it writes an output file
// named from the -P directory and -o template, and prints the usual
// destination, progress, and merge lines.
func fakeYtDlpDownload(args []string) int {
	path, ext := fakeOutputPath(args)
	if path == "" {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: missing -P or -o in %q\n", args)
		return 2
	}
	if err := os.WriteFile(path, []byte("fake media"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: %v\n", err)
		return 2
	}

	fmt.Println("[download] Destination: " + strings.TrimSuffix(path, "."+ext) + ".f248.webm")
	fmt.Println("[download]  50.0% of   10.00MiB at    5.00MiB/s ETA 00:01")
	fmt.Println("[download] 100.0% of   10.00MiB at    5.00MiB/s ETA 00:00")
	fmt.Fprintln(os.Stderr, "[debug] Command-line config: fake")
	// Real yt-dlp prints post-processor messages such as [Merger] to stdout.
	fmt.Printf("[Merger] Merging formats into %q\n", path)
	return 0
}

// fakeYtDlpTransient mimics yt-dlp failing with a retryable rate-limit error.
func fakeYtDlpTransient() int {
	fmt.Fprintln(os.Stderr, "ERROR: [youtube] fake: Unable to download webpage: HTTP Error 429: Too Many Requests")
	return 1
}

// fakeOutputPath returns the file a yt-dlp run with args would write, named
// from the -P directory and -o template, and its extension. path is "" when
// args lack -P or -o.
func fakeOutputPath(args []string) (path, ext string) {
	dir, template := argAfter(args, "-P"), argAfter(args, "-o")
	if dir == "" || template == "" {
		return "", ""
	}
	ext = argAfter(args, "--merge-output-format")
	if ext == "" {
		ext = argAfter(args, "--audio-format")
	}
	name := strings.NewReplacer("%(title)s", "Fake Video", "%(ext)s", ext).Replace(template)
	return filepath.Join(dir, name), ext
}

// fakeYtDlpHang mimics a stalled download: it writes a partial output file
// (when args name one), reports some progress, and then blocks until the
// test cancels it (the process is killed on cancel).
func fakeYtDlpHang(args []string) int {
	if path, _ := fakeOutputPath(args); path != "" {
		if err := os.WriteFile(path, []byte("partial"), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: %v\n", err)
			return 2
		}
	}
	fmt.Println("[download]   1.0% of   10.00MiB at    1.00MiB/s ETA 00:10")
	time.Sleep(time.Minute)
	return 0
}

// fakeToolTickEnv names the file the "tick" mode appends to while it runs.
const fakeToolTickEnv = "GOVID_FAKE_TOOL_TICK"

// fakeYtDlpSpawnChild mimics yt-dlp running ffmpeg: it starts a long-running
// child process that shares its stdout, reports some progress, and blocks.
// Cancelling must kill the child too, or the child would keep running and
// keep the output pipe open.
func fakeYtDlpSpawnChild() int {
	child := exec.Command(os.Args[0])
	// Clear the state file so the child is not counted as a tool run.
	child.Env = append(os.Environ(), fakeToolEnv+"=tick", fakeToolStateEnv+"=")
	child.Stdout = os.Stdout
	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: start child: %v\n", err)
		return 2
	}
	fmt.Println("[download]   1.0% of   10.00MiB at    1.00MiB/s ETA 00:10")
	time.Sleep(time.Minute)
	return 0
}

// fakeTick appends a byte to the fakeToolTickEnv file every 20 ms for a
// minute, so a test can tell whether the process is still alive.
func fakeTick() int {
	path := os.Getenv(fakeToolTickEnv)
	for range 3000 {
		if file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			file.Write([]byte("."))
			file.Close()
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0
}

// argAfter returns the value following flag in args, or "" when absent.
func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

// useFakeToolState points the fake tool at a fresh state file and returns a
// function reporting how many times the tool has been started since.
func useFakeToolState(t *testing.T) func() int {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "fake_tool_runs.txt")
	t.Setenv(fakeToolStateEnv, statePath)
	return func() int {
		data, err := os.ReadFile(statePath)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "\n")
	}
}

// useFakeTool makes child processes started by this test run the given fake
// mode. It uses t.Setenv, so tests calling it cannot run in parallel.
func useFakeTool(t *testing.T, mode string) {
	t.Helper()
	t.Setenv(fakeToolEnv, mode)
}

// fakeToolPath returns the path of the running test binary, which acts as
// the fake tool when started with fakeToolEnv set.
func fakeToolPath(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	return exe
}

// installFakeTool places a copy of the test binary in dir under toolName
// (with .exe appended on Windows) and returns its path.
func installFakeTool(t *testing.T, dir, toolName string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		toolName += ".exe"
	}
	dst := filepath.Join(dir, toolName)

	// Copy rather than hard-link: on Windows a link to the running test
	// binary shares its lock and cannot be removed by t.TempDir cleanup.
	in, err := os.Open(fakeToolPath(t))
	if err != nil {
		t.Fatalf("open test binary: %v", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		t.Fatalf("create fake tool: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatalf("copy fake tool: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close fake tool: %v", err)
	}
	return dst
}
