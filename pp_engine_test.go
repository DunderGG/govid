package main

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestBuildFFmpegArgsForBackend(t *testing.T) {
	engine := &PPEngine{
		GPUCapabilities: map[GPUBackend]BackendCapability{
			BackendNVIDIA: {Backend: BackendNVIDIA, Available: true},
		},
	}

	tests := []struct {
		name    string
		backend GPUBackend
		want    string
	}{
		{"nvidia available uses NVENC", BackendNVIDIA, "-c:v h264_nvenc"},
		{"off always falls back to CPU", BackendOff, "-c:v libx264"},
		{"unavailable backend falls back to CPU", BackendIntel, "-c:v libx264"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := engine.buildFFmpegArgsForBackend("in.mp4", "in_pp.mp4", []string{"eq=contrast=1.1"}, nil, tt.backend, 4)
			got := strings.Join(args, " ")
			if !strings.Contains(got, tt.want) {
				t.Errorf("buildFFmpegArgsForBackend(...) = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestBuildFFmpegArgsForBackendNoFilters(t *testing.T) {
	engine := &PPEngine{}
	args := engine.buildFFmpegArgsForBackend("in.mp4", "in_pp.mp4", nil, nil, BackendNVIDIA, 4)
	got := strings.Join(args, " ")
	if !strings.Contains(got, "-c:v copy") {
		t.Errorf("buildFFmpegArgsForBackend(...) with no filters = %q, want stream copy", got)
	}
}

func TestBuildFFmpegArgsUsesThreadCount(t *testing.T) {
	engine := &PPEngine{}
	for _, threads := range []int{1, 6} {
		args := engine.buildFFmpegArgs("in.mp4", "in_pp.mp4", []string{"deband"}, []string{"loudnorm"}, threads)
		want := []string{"-y", "-threads", strconv.Itoa(threads), "-i", "in.mp4"}
		if !slices.Equal(args[:len(want)], want) {
			t.Errorf("buildFFmpegArgs(..., %d) starts %q, want %q", threads, args[:len(want)], want)
		}
	}
}

func TestNewPPEngineGPUSemCapacity(t *testing.T) {
	engine := NewPPEngine("ffmpeg", "ffprobe")
	if cap(engine.gpuSem) != maxConcurrentGPUJobs {
		t.Errorf("gpuSem capacity = %d, want %d", cap(engine.gpuSem), maxConcurrentGPUJobs)
	}
}

// recordingPPCallbacks returns callbacks that count OnFailure calls and
// collect logged lines.
func recordingPPCallbacks() (cb PPCallbacks, failures *int, logs *[]string) {
	failures, logs = new(int), new([]string)
	cb = PPCallbacks{
		OnLog:     func(line string, _ color.Color) { *logs = append(*logs, line) },
		OnStatus:  func(string) {},
		OnFailure: func() { *failures++ },
	}
	return cb, failures, logs
}

func TestRunJobStartFailureReportsFailure(t *testing.T) {
	dir := t.TempDir()
	engine := NewPPEngine(filepath.Join(dir, "no-such-ffmpeg"), "ffprobe")
	job := PostProcessJob{
		inputPath: filepath.Join(dir, "in.mp4"),
		tmpOutput: filepath.Join(dir, "in_pp.mp4"),
		finalPath: filepath.Join(dir, "in.mp4"),
	}
	cb, failures, logs := recordingPPCallbacks()

	engine.runJob(context.Background(), job, cb)

	if *failures != 1 {
		t.Errorf("OnFailure called %d times, want 1", *failures)
	}
	joined := strings.Join(*logs, "\n")
	if !strings.Contains(joined, "Could not start FFmpeg") {
		t.Errorf("log missing start failure, got:\n%s", joined)
	}
	if strings.Contains(joined, "could not remove temp file") {
		t.Errorf("missing temp file should not be reported, got:\n%s", joined)
	}
}

func TestRunJobFFmpegErrorReportsFailureAndRemovesTemp(t *testing.T) {
	useFakeTool(t, "fail")
	dir := t.TempDir()
	engine := NewPPEngine(fakeToolPath(t), "ffprobe")
	job := PostProcessJob{
		inputPath: filepath.Join(dir, "in.mp4"),
		tmpOutput: filepath.Join(dir, "in_pp.mp4"),
		finalPath: filepath.Join(dir, "in.mp4"),
	}
	if err := os.WriteFile(job.tmpOutput, []byte("partial"), 0644); err != nil {
		t.Fatal(err)
	}
	cb, failures, logs := recordingPPCallbacks()

	engine.runJob(context.Background(), job, cb)

	if *failures != 1 {
		t.Errorf("OnFailure called %d times, want 1", *failures)
	}
	if joined := strings.Join(*logs, "\n"); !strings.Contains(joined, "Post-processing failed") {
		t.Errorf("log missing failure, got:\n%s", joined)
	}
	if _, err := os.Stat(job.tmpOutput); !os.IsNotExist(err) {
		t.Errorf("temp output still exists (stat err = %v)", err)
	}
}

// ── HDR to SDR ───────────────────────────────────────────────────────────────

func TestParseFFprobeColorInfo(t *testing.T) {
	out := "color_space=bt2020nc\ncolor_transfer=smpte2084\ncolor_primaries=bt2020\n"
	want := colorInfo{Transfer: "smpte2084", Primaries: "bt2020", Space: "bt2020nc"}
	if got := parseFFprobeColorInfo(out); got != want {
		t.Errorf("parseFFprobeColorInfo() = %+v, want %+v", got, want)
	}
	if got := parseFFprobeColorInfo("color_transfer=unknown\r\n"); got != (colorInfo{}) {
		t.Errorf("parseFFprobeColorInfo(unknown) = %+v, want empty", got)
	}
}

func TestParseFFmpegColorInfo(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		want   colorInfo
		wantOK bool
	}{
		{"PQ", "  Stream #0:0: Video: vp9 (Profile 2), yuv420p10le(tv, bt2020nc/bt2020/smpte2084), 3840x2160",
			colorInfo{Transfer: transferPQ, Primaries: "bt2020", Space: "bt2020nc"}, true},
		{"HLG", "  Stream #0:0(eng): Video: hevc (Main 10), yuv420p10le(tv, bt2020nc/bt2020/arib-std-b67), 1920x1080",
			colorInfo{Transfer: transferHLG, Primaries: "bt2020", Space: "bt2020nc"}, true},
		{"SDR, one name for all three", "  Stream #0:0: Video: h264 (High), yuv420p(tv, bt709, progressive), 1920x1080",
			colorInfo{Transfer: "bt709", Primaries: "bt709", Space: "bt709"}, true},
		{"transfer missing", "  Stream #0:0: Video: vp9 (Profile 2), yuv420p10le(tv, bt2020nc/bt2020/unknown), 3840x2160",
			colorInfo{Primaries: "bt2020", Space: "bt2020nc"}, true},
		{"untagged", "  Stream #0:0: Video: hevc (Main 10), yuv420p10le(tv, progressive), 320x180", colorInfo{}, true},
		{"audio before video", "  Stream #0:0: Audio: opus, 48000 Hz\n  Stream #0:1: Video: av1, yuv420p10le(tv, bt2020nc/bt2020/smpte2084)",
			colorInfo{Transfer: transferPQ, Primaries: "bt2020", Space: "bt2020nc"}, true},
		{"no video", "  Stream #0:0: Audio: mp3, 44100 Hz", colorInfo{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := parseFFmpegColorInfo(tt.out)
			if info != tt.want || ok != tt.wantOK {
				t.Errorf("parseFFmpegColorInfo() = %+v, %v; want %+v, %v", info, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestColorInfoHDRTransfer(t *testing.T) {
	tests := []struct {
		name        string
		info        colorInfo
		want        string
		wantAssumed bool
	}{
		{"PQ", colorInfo{Transfer: transferPQ, Primaries: "bt2020", Space: "bt2020nc"}, transferPQ, false},
		{"HLG", colorInfo{Transfer: transferHLG}, transferHLG, false},
		{"BT.2020 without a transfer tag", colorInfo{Primaries: "bt2020", Space: "bt2020nc"}, transferPQ, true},
		{"BT.2020 matrix only", colorInfo{Space: "bt2020nc"}, transferPQ, true},
		{"SDR BT.709", colorInfo{Transfer: "bt709", Primaries: "bt709", Space: "bt709"}, "", false},
		{"BT.2020 tagged as SDR", colorInfo{Transfer: "bt2020-10", Primaries: "bt2020", Space: "bt2020nc"}, "", false},
		{"untagged", colorInfo{}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, assumed := tt.info.hdrTransfer()
			if got != tt.want || assumed != tt.wantAssumed {
				t.Errorf("hdrTransfer() = %q, %v; want %q, %v", got, assumed, tt.want, tt.wantAssumed)
			}
		})
	}
}

func TestResolveToneMap(t *testing.T) {
	filters := []string{"deband", toneMapSentinel, "deshake"}
	missingProbe := filepath.Join(t.TempDir(), "no-such-ffprobe.exe")

	tests := []struct {
		name    string
		mode    string // fake tool mode for the tool that answers
		useFF   bool   // ffprobe is missing, so ffmpeg's summary answers
		color   string // transfer,primaries,space
		want    []string
		wantLog string
	}{
		{"PQ via ffprobe", "ffprobe-color", false, "smpte2084,bt2020,bt2020nc",
			[]string{"deband", toneMapFilter(transferPQ), "deshake"}, "HDR10 (PQ) source, tone mapping"},
		{"SDR via ffprobe", "ffprobe-color", false, "bt709,bt709,bt709",
			[]string{"deband", "deshake"}, "source is SDR (transfer: bt709), skipping"},
		{"HLG via ffmpeg fallback", "ffmpeg-summary", true, "arib-std-b67,bt2020,bt2020nc",
			[]string{"deband", toneMapFilter(transferHLG), "deshake"}, "HLG source, tone mapping"},
		{"BT.2020 without a transfer tag", "ffprobe-color", false, ",bt2020,bt2020nc",
			[]string{"deband", toneMapFilter(transferPQ), "deshake"}, "without a transfer tag, assuming HDR10 (PQ)"},
		{"untagged via ffmpeg fallback", "ffmpeg-summary", true, "",
			[]string{"deband", "deshake"}, "source is SDR (transfer: unknown)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useFakeTool(t, tt.mode)
			t.Setenv(fakeColorEnv, tt.color)
			engine := NewPPEngine(fakeToolPath(t), fakeToolPath(t))
			if tt.useFF {
				engine.FFprobePath = missingProbe
			}
			var logs []string
			cb := PPCallbacks{
				OnLog:     func(line string, _ color.Color) { logs = append(logs, line) },
				OnStatus:  func(string) {},
				OnFailure: func() {},
			}

			got := engine.resolveToneMap(context.Background(), "in.mkv", filters, cb)

			if !slices.Equal(got, tt.want) {
				t.Errorf("resolveToneMap() = %q, want %q", got, tt.want)
			}
			if joined := strings.Join(logs, "\n"); !strings.Contains(joined, tt.wantLog) {
				t.Errorf("log = %q, want it to mention %q", joined, tt.wantLog)
			}
		})
	}
}

func TestResolveToneMapWithoutSentinelRunsNoProbe(t *testing.T) {
	engine := NewPPEngine(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "missing"))
	filters := []string{"deband"}
	got := engine.resolveToneMap(context.Background(), "in.mkv", filters, PPCallbacks{
		OnLog: func(line string, _ color.Color) { t.Errorf("unexpected log %q", line) },
	})
	if !slices.Equal(got, filters) {
		t.Errorf("resolveToneMap() = %q, want filters unchanged", got)
	}
}

func TestBuildFFmpegArgsTagsToneMappedOutputAsBT709(t *testing.T) {
	engine := &PPEngine{}
	tags := "-color_primaries bt709 -color_trc bt709 -colorspace bt709"

	mapped := strings.Join(engine.buildFFmpegArgsForBackend("in.mkv", "in_pp.mkv", []string{toneMapFilter(transferPQ)}, nil, BackendOff, 4), " ")
	if !strings.Contains(mapped, tags) {
		t.Errorf("tone-mapped args = %q, want the BT.709 tags", mapped)
	}
	plain := strings.Join(engine.buildFFmpegArgsForBackend("in.mkv", "in_pp.mkv", []string{"deband"}, nil, BackendOff, 4), " ")
	if strings.Contains(plain, "-color_trc") {
		t.Errorf("args without tone mapping = %q, want no colour tags", plain)
	}
}
