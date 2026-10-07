package main

// fake_tool_test.go — Lets the test binary stand in for external tools.
//
// When GOVID_FAKE_TOOL is set, TestMain runs the requested fake behaviour
// instead of the test suite and exits. Tests point a service at the test
// binary (directly, or via installFakeTool under the expected tool name) and
// select the behaviour with useFakeTool, so no real yt-dlp or FFmpeg is needed.

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// fakeExtractionsEnv optionally names a file the fake yt-dlp appends to
// each time it extracts a single video: a probe that answers with one, or a
// download from a URL rather than from --load-info-json. Real yt-dlp sends
// several requests to the site for each extraction.
const fakeExtractionsEnv = "GOVID_FAKE_TOOL_EXTRACTIONS"

// recordFakeExtraction appends a line to the fakeExtractionsEnv file, if set.
func recordFakeExtraction() {
	path := os.Getenv(fakeExtractionsEnv)
	if path == "" {
		return
	}
	if file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		fmt.Fprintln(file, "extract")
		file.Close()
	}
}

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
	if mode := os.Getenv(fakeToolEnv); mode != "" || fakeRuntimeName() != "" {
		os.Exit(runFakeTool(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeRuntimeVersionEnv optionally sets the version a fake JavaScript
// runtime reports, e.g. to make one too old for yt-dlp.
const fakeRuntimeVersionEnv = "GOVID_FAKE_RUNTIME_VERSION"

// fakeRuntimeName returns "deno", "node", or "bun" when the test binary was
// installed under one of those names (see installFakeTool), and "" else.
// Such a copy always acts as that runtime, whatever fakeToolEnv says, since
// GoVid asks every runtime it finds for its version.
func fakeRuntimeName() string {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	if slices.Contains([]string{"deno", "node", "bun"}, name) {
		return name
	}
	return ""
}

// fakeRuntime mimics "<runtime> --version" as deno, node, and bun print it.
func fakeRuntime(name string, args []string) int {
	if !slices.Contains(args, "--version") {
		fmt.Fprintf(os.Stderr, "fake %s: expected --version, got %q\n", name, args)
		return 4
	}
	version := os.Getenv(fakeRuntimeVersionEnv)
	switch name {
	case "deno":
		version = cmp.Or(version, "2.9.7")
		fmt.Printf("deno %s (stable, release, x86_64-pc-windows-msvc)\nv8 14.1.146.11-rusty\ntypescript 5.9.2\n", version)
	case "node":
		fmt.Printf("v%s\n", cmp.Or(version, "24.16.0"))
	default:
		fmt.Println(cmp.Or(version, "1.3.0"))
	}
	return 0
}

// fakeArgsEnv optionally names a file the fake tool appends each run's
// arguments to, one run per line, separated by fakeArgsSep.
const fakeArgsEnv = "GOVID_FAKE_TOOL_ARGS"

// fakeArgsSep separates the arguments of one run in the fakeArgsEnv file.
const fakeArgsSep = "\x1f"

// recordFakeArgs appends args to the fakeArgsEnv file, if set.
func recordFakeArgs(args []string) {
	path := os.Getenv(fakeArgsEnv)
	if path == "" {
		return
	}
	if file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		fmt.Fprintln(file, strings.Join(args, fakeArgsSep))
		file.Close()
	}
}

// useFakeArgs points the fake tool at a fresh argument log and returns a
// function listing the arguments of every run since.
func useFakeArgs(t *testing.T) func() [][]string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake_tool_args.txt")
	t.Setenv(fakeArgsEnv, path)
	return func() [][]string {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var runs [][]string
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			runs = append(runs, strings.Split(line, fakeArgsSep))
		}
		return runs
	}
}

// runFakeTool implements the fake tool modes and returns the exit code.
func runFakeTool(mode string, args []string) int {
	if name := fakeRuntimeName(); name != "" {
		return fakeRuntime(name, args)
	}
	recordFakeArgs(args)
	// yt-dlp probes (-J) are answered first and not counted, so tests that
	// count downloads are not affected by the probe before each one.
	if slices.Contains(args, "-J") {
		return fakeYtDlpProbe(mode, args[len(args)-1], slices.Contains(args, "--no-playlist"))
	}
	if infoPath := argAfter(args, "--load-info-json"); infoPath != "" {
		if code := checkFakeInfoJSON(infoPath, args); code != 0 {
			return code
		}
	} else if argAfter(args, "-o") != "" {
		recordFakeExtraction() // a download from a URL
	}

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
	case "ytdlp-download", "ytdlp-playlist", "ytdlp-probe-fail":
		return fakeYtDlpDownload(args)
	case "ytdlp-mixed":
		return fakeYtDlpMixed(args)
	case "ytdlp-cookies-locked", "ytdlp-bot-check":
		fmt.Fprintln(os.Stderr, fakeAccessErrors[mode])
		return 1
	case "ytdlp-concurrent":
		return fakeYtDlpConcurrent(args)
	case "ytdlp-rate-limited":
		fmt.Fprintln(os.Stderr, "ERROR: [youtube] fake: Unable to download webpage: HTTP Error 429: Too Many Requests")
		return 1
	case "ytdlp-formats":
		return fakeFixtureDownload(args)
	case "ytdlp-resumable":
		return fakeYtDlpResumable(args)
	case "ytdlp-live":
		return fakeYtDlpLive(args)
	case "ytdlp-upcoming":
		return fakeYtDlpUpcoming(args)
	case "ytdlp-no-js-runtime":
		fmt.Fprintln(os.Stderr, "WARNING: [youtube] No supported JavaScript runtime could be found. Only deno is enabled by default; to use another runtime add  --js-runtimes RUNTIME[:PATH]  to your command/config. YouTube extraction without a JS runtime has been deprecated, and some formats may be missing.")
		return fakeYtDlpDownload(args)
	case "ytdlp-subtitles-429":
		if slices.Contains(args, "--write-subs") {
			fmt.Fprintln(os.Stderr, "ERROR: Unable to download video subtitles for 'en': HTTP Error 429: Too Many Requests")
			return 1
		}
		return fakeYtDlpDownload(args)
	case "ytdlp-info-expired":
		if slices.Contains(args, "--load-info-json") {
			return fakeYtDlpExpiredLinks(args)
		}
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
	return fakeWriteSubtitles(args, strings.TrimSuffix(path, "."+ext))
}

// fakeSubtitleLangs are the subtitle languages the fake single video has.
var fakeSubtitleLangs = []string{"en", "de"}

// fakeWriteSubtitles mimics yt-dlp's subtitle handling, as checked against
// the real one: with --write-subs, each fakeSubtitleLangs entry that
// --sub-langs selects is written as <base>.<lang>.<--convert-subs ext>,
// and is kept after --embed-subs unless "--compat-options no-keep-subs" is
// given.
func fakeWriteSubtitles(args []string, base string) int {
	if !slices.Contains(args, "--write-subs") {
		return 0
	}
	if slices.Contains(args, "--embed-subs") && argAfter(args, "--compat-options") == "no-keep-subs" {
		return 0
	}
	for _, lang := range matchSubLangs(argAfter(args, "--sub-langs"), fakeSubtitleLangs) {
		path := base + "." + lang + "." + argAfter(args, "--convert-subs")
		if err := os.WriteFile(path, []byte("1\n00:00:00,000 --> 00:00:01,000\nHello\n"), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: %v\n", err)
			return 2
		}
	}
	return 0
}

// fakeYtDlpTransient mimics yt-dlp failing with a retryable rate-limit error.
func fakeYtDlpTransient() int {
	fmt.Fprintln(os.Stderr, "ERROR: [youtube] fake: Unable to download webpage: HTTP Error 429: Too Many Requests")
	return 1
}

// fakePlaylistSize is the number of videos the "ytdlp-playlist" mode's
// playlist holds.
const fakePlaylistSize = 20

// fakeVideoSize is the size, in bytes, the probe reports for a single video:
// 10 MiB, matching the progress lines of fakeYtDlpDownload.
const fakeVideoSize = 10 * 1024 * 1024

// fakeVideoHeight is the height of the fake single video: the probe reports
// it, and the download names its file with it.
const fakeVideoHeight = 720

// fakeYtDlpExpiredLinks mimics a download from saved info whose format URLs
// have expired: it writes a partial file and fails with HTTP 403.
func fakeYtDlpExpiredLinks(args []string) int {
	if path, _ := fakeOutputPath(args); path != "" {
		os.WriteFile(path, []byte("partial"), 0644)
	}
	fmt.Fprintln(os.Stderr, "ERROR: unable to download video data: HTTP Error 403: Forbidden")
	return 1
}

// checkFakeInfoJSON checks the arguments of a download from saved info, as
// real yt-dlp would treat them: the file must hold a video's JSON, and no
// URL may be given as well, since yt-dlp would download that too.
func checkFakeInfoJSON(path string, args []string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: %v\n", err)
		return 2
	}
	if !strings.Contains(string(data), `"title"`) {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: %s holds no video info\n", path)
		return 2
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "http") {
			fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: URL %q given with --load-info-json\n", arg)
			return 2
		}
	}
	return 0
}

// fakeYtDlpProbe mimics "yt-dlp -J --flat-playlist": the "ytdlp-playlist"
// mode reports a playlist of fakePlaylistSize videos at
// https://example.com/v/<n> (unless noPlaylist is set), "ytdlp-probe-fail"
// fails, and every other mode reports a single 10 MiB, 720p video.
func fakeYtDlpProbe(mode, url string, noPlaylist bool) int {
	if mode == "ytdlp-formats" {
		return fakeFixtureProbe()
	}
	if line, ok := fakeAccessErrors[mode]; ok {
		fmt.Fprintln(os.Stderr, line)
		return 1
	}
	switch {
	case mode == "ytdlp-playlist" && !noPlaylist:
		var entries []string
		for n := 1; n <= fakePlaylistSize; n++ {
			entries = append(entries, fmt.Sprintf(`{"_type": "url", "ie_key": "Fake", "id": "v%d", "url": "https://example.com/v/%d", "title": "Video %d", "duration": 90}`, n, n, n))
		}
		fmt.Printf(`{"_type": "playlist", "title": "Fake Playlist", "entries": [%s]}`+"\n", strings.Join(entries, ", "))
		return 0
	case mode == "ytdlp-probe-fail":
		fmt.Fprintln(os.Stderr, "ERROR: [generic] fake: Unable to download webpage")
		return 1
	case mode == "ytdlp-live":
		fmt.Printf(`{"_type": "video", "id": "fakelive", "extractor_key": "Youtube", "webpage_url": %q, "title": "Fake Live", "live_status": "is_live", "is_live": true}`+"\n", url)
		return 0
	case mode == "ytdlp-upcoming":
		fmt.Printf(`{"_type": "video", "id": "fakepremiere", "extractor_key": "Youtube", "webpage_url": %q, "title": "Fake Premiere", "live_status": "is_upcoming", "release_timestamp": %d}`+"\n", url, time.Now().Add(2*time.Hour).Unix())
		return 0
	}
	recordFakeExtraction()
	fmt.Printf(`{"_type": "video", "id": "fakevid", "extractor_key": "Fake", "webpage_url": %q, "title": "Fake Video", "format_id": "fake-v+fake-a", "duration": 10, "height": %d, "subtitles": {"en": [], "de": []}, "automatic_captions": {"en": [], "fr": []}, "requested_formats": [{"filesize": %d}, {"filesize_approx": %d}]}`+"\n",
		url, fakeVideoHeight, fakeVideoSize-1024*1024, 1024*1024)
	return 0
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
	name := strings.NewReplacer("%(title)s", "Fake Video", heightLabel, fmt.Sprintf("_%dp", fakeVideoHeight), "%(ext)s", ext).Replace(template)
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

// useFakeExtractions points the fake yt-dlp at a fresh extraction log and
// returns a function reporting how many single-video extractions it has made
// since (see fakeExtractionsEnv).
func useFakeExtractions(t *testing.T) func() int {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake_tool_extractions.txt")
	t.Setenv(fakeExtractionsEnv, path)
	return func() int {
		data, err := os.ReadFile(path)
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

// fakeYtDlpMixed lets one session mix outcomes: a video whose URL contains
// "/hang" stalls like "ytdlp-hang", one containing "/fail" fails, and any
// other downloads. The URL is the last argument, or the webpage_url of the
// --load-info-json file.
func fakeYtDlpMixed(args []string) int {
	url := args[len(args)-1]
	if infoPath := argAfter(args, "--load-info-json"); infoPath != "" {
		var info struct {
			WebpageURL string `json:"webpage_url"`
		}
		data, _ := os.ReadFile(infoPath)
		json.Unmarshal(data, &info)
		url = info.WebpageURL
	}
	switch {
	case strings.Contains(url, "/hang"):
		return fakeYtDlpHang(args)
	case strings.Contains(url, "/fail"):
		fmt.Fprintln(os.Stderr, "ERROR: fake tool failure")
		return 1
	default:
		return fakeYtDlpDownload(args)
	}
}

// fakeYtDlpLive mimics recording a live stream: it writes the output file
// and keeps adding to it, as yt-dlp's ffmpeg does, until it is killed (or a
// minute passes).
func fakeYtDlpLive(args []string) int {
	path, _ := fakeOutputPath(args)
	if path == "" {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: missing -P or -o in %q\n", args)
		return 2
	}
	file, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: %v\n", err)
		return 2
	}
	defer file.Close()
	fmt.Println("[download] Destination: " + path)
	chunk := make([]byte, 64*1024)
	for range 600 {
		file.Write(chunk)
		time.Sleep(100 * time.Millisecond)
	}
	return 0
}

// fakeYtDlpUpcoming mimics a scheduled stream: without --wait-for-video it
// fails as yt-dlp does; with it, yt-dlp waits, and then this fake records a
// short stream that ends by itself.
func fakeYtDlpUpcoming(args []string) int {
	if argAfter(args, "--wait-for-video") == "" {
		fmt.Fprintln(os.Stderr, "ERROR: [youtube] fakepremiere: This live event will begin in 2 hours.")
		return 1
	}
	fmt.Println("[wait] Waiting for 00:05:00 - Press Ctrl+C to try now")
	time.Sleep(1500 * time.Millisecond)
	path, _ := fakeOutputPath(args)
	if err := os.WriteFile(path, []byte("recorded premiere"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: %v\n", err)
		return 2
	}
	fmt.Println("[download] Destination: " + path)
	return 0
}

// fakeAccessErrors are the errors the "ytdlp-cookies-locked" and
// "ytdlp-bot-check" modes fail with, probe and download alike, as the
// bundled yt-dlp printed them.
var fakeAccessErrors = map[string]string{
	"ytdlp-cookies-locked": "ERROR: Could not copy Chrome cookie database. See  https://github.com/yt-dlp/yt-dlp/issues/7271  for more info",
	"ytdlp-bot-check":      "ERROR: [youtube] jNQXAC9IVRw: Sign in to confirm you’re not a bot. Use --cookies-from-browser or --cookies for the authentication. See  https://github.com/yt-dlp/yt-dlp/wiki/FAQ#how-do-i-pass-cookies-to-yt-dlp  for how to manually pass cookies.",
}

// fakeResumableChunks is how many 1 MiB chunks the "ytdlp-resumable" mode
// downloads in all.
const fakeResumableChunks = 10

// fakeYtDlpResumable mimics yt-dlp downloading with .part files: it needs
// --continue, writes <name>.part a chunk at a time with progress lines,
// continues an existing .part file from its size (saying so, as yt-dlp
// does), and renames it when complete.
func fakeYtDlpResumable(args []string) int {
	if !slices.Contains(args, "--continue") {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: expected --continue, got %q\n", args)
		return 2
	}
	path, _ := fakeOutputPath(args)
	part := path + ".part"
	const chunk = 1024 * 1024
	done := int64(0)
	if info, err := os.Stat(part); err == nil {
		done = info.Size()
		fmt.Printf("[download] Resuming download at byte %d\n", done)
	}
	fmt.Println("[download] Destination: " + path)
	file, err := os.OpenFile(part, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: %v\n", err)
		return 2
	}
	for done < fakeResumableChunks*chunk {
		file.Write(make([]byte, chunk))
		done += chunk
		fmt.Printf("[download] %5.1f%% of   10.00MiB at    5.00MiB/s ETA 00:01\n", float64(done)*100/(fakeResumableChunks*chunk))
		time.Sleep(60 * time.Millisecond)
	}
	file.Close()
	if err := os.Rename(part, path); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: %v\n", err)
		return 2
	}
	return 0
}

// fakeConcurrencyEnv names the folder the "ytdlp-concurrent" mode marks its
// running downloads in.
const fakeConcurrencyEnv = "GOVID_FAKE_CONCURRENCY_DIR"

// fakeYtDlpConcurrent mimics a download that takes a while, and records how
// many downloads were running at once: each run leaves a file named after
// its process ID in the fakeConcurrencyEnv folder while it runs, and
// appends the number of such files it saw to "peaks" there.
func fakeYtDlpConcurrent(args []string) int {
	dir := os.Getenv(fakeConcurrencyEnv)
	marker := filepath.Join(dir, fmt.Sprintf("run-%d", os.Getpid()))
	os.WriteFile(marker, nil, 0644)
	defer os.Remove(marker)
	for range 8 {
		running, _ := filepath.Glob(filepath.Join(dir, "run-*"))
		if file, err := os.OpenFile(filepath.Join(dir, "peaks"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			fmt.Fprintln(file, len(running))
			file.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fakeYtDlpDownload(args)
}

// useFakeConcurrency points the "ytdlp-concurrent" mode at a fresh folder
// and returns a function reporting the most downloads it saw at once.
func useFakeConcurrency(t *testing.T) func() int {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(fakeConcurrencyEnv, dir)
	return func() int {
		data, _ := os.ReadFile(filepath.Join(dir, "peaks"))
		peak := 0
		for _, line := range strings.Fields(string(data)) {
			var n int
			fmt.Sscan(line, &n)
			peak = max(peak, n)
		}
		return peak
	}
}

// fakeFixtureProbe answers a probe with the real (sanitised) YouTube info
// in testdata/ytdlp_info_formats.json, whose selector chose 401+251.
func fakeFixtureProbe() int {
	data, err := os.ReadFile(filepath.Join("testdata", "ytdlp_info_formats.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: fake yt-dlp: %v\n", err)
		return 2
	}
	os.Stdout.Write(data)
	return 0
}

// fakeFixtureDownload prints the line real yt-dlp gives before a download,
// naming the formats it fetches: the -f value when it picks formats by ID,
// else the fixture's own choice, and then downloads.
func fakeFixtureDownload(args []string) int {
	formats := "401+251"
	if pick := argAfter(args, "-f"); regexp.MustCompile(`^[0-9]+(\+[0-9]+)?$`).MatchString(pick) {
		formats = pick
	}
	fmt.Printf("[info] dQw4w9WgXcQ: Downloading 1 format(s): %s\n", formats)
	return fakeYtDlpDownload(args)
}
