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
			args := engine.buildFFmpegArgsForBackend(testJob([]string{"eq=contrast=1.1"}, nil, 4), tt.backend)
			got := strings.Join(args, " ")
			if !strings.Contains(got, tt.want) {
				t.Errorf("buildFFmpegArgsForBackend(...) = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestBuildFFmpegArgsForBackendNoFilters(t *testing.T) {
	engine := &PPEngine{}
	args := engine.buildFFmpegArgsForBackend(testJob(nil, nil, 4), BackendNVIDIA)
	got := strings.Join(args, " ")
	if !strings.Contains(got, "-c:v copy") {
		t.Errorf("buildFFmpegArgsForBackend(...) with no filters = %q, want stream copy", got)
	}
}

func TestBuildFFmpegArgsUsesThreadCount(t *testing.T) {
	engine := &PPEngine{}
	for _, threads := range []int{1, 6} {
		args := engine.buildFFmpegArgs(testJob([]string{"deband"}, []string{"loudnorm"}, threads))
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
	tags := "-color_primaries:v:0 bt709 -color_trc:v:0 bt709 -colorspace:v:0 bt709"

	mapped := strings.Join(engine.buildFFmpegArgsForBackend(testJob([]string{toneMapFilter(transferPQ)}, nil, 4), BackendOff), " ")
	if !strings.Contains(mapped, tags) {
		t.Errorf("tone-mapped args = %q, want the BT.709 tags", mapped)
	}
	plain := strings.Join(engine.buildFFmpegArgsForBackend(testJob([]string{"deband"}, nil, 4), BackendOff), " ")
	if strings.Contains(plain, "-color_trc") {
		t.Errorf("args without tone mapping = %q, want no colour tags", plain)
	}
}

// testJob returns a job for in.mp4 with the given filters and threads.
func testJob(vfFilters, afFilters []string, threads int) PostProcessJob {
	return PostProcessJob{inputPath: "in.mp4", tmpOutput: "in_pp.mp4", vfFilters: vfFilters, afFilters: afFilters, threads: threads}
}

// ── Stream mapping ───────────────────────────────────────────────────────────

// richMP4Summary is ffmpeg's input summary for an MP4 with chapters, a
// subtitle, a chapter text track, and cover art.
const richMP4Summary = `Input #0, mov,mp4,m4a,3gp,3g2,mj2, from 'rich.mp4':
  Chapters:
    Chapter #0:0: start 0.000000, end 1.000000
  Stream #0:0[0x1](und): Video: h264 (High) (avc1 / 0x31637661), yuv420p(progressive), 320x180, 24 fps (default)
  Stream #0:1[0x2](und): Audio: aac (LC) (mp4a / 0x6134706D), 44100 Hz, mono, fltp, 69 kb/s (default)
  Stream #0:2[0x3](und): Subtitle: mov_text (tx3g / 0x67337874), 0 kb/s (default)
  Stream #0:3[0x5](eng): Data: bin_data (text / 0x74786574), 0 kb/s
  Stream #0:4[0x0]: Video: mjpeg (Baseline), yuvj420p(pc, bt470bg/unknown/unknown), 320x180, 90k tbr, 90k tbn (attached pic)
At least one output file must be specified`

func TestParseStreamLayout(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		want   streamLayout
		wantOK bool
	}{
		{"mp4 with cover", richMP4Summary, streamLayout{mainVideo: 0, videoCount: 1, covers: []coverArt{{index: 4}}}, true},
		{"cover first", "  Stream #0:0: Video: mjpeg, yuvj420p (attached pic)\n  Stream #0:1: Video: vp9, yuv420p\n  Stream #0:2: Audio: opus", streamLayout{mainVideo: 1, videoCount: 1, covers: []coverArt{{index: 0}}}, true},
		{"mp3 with cover", "  Stream #0:0: Audio: mp3, 44100 Hz\n  Stream #0:1: Video: mjpeg (Baseline), 600x600 (attached pic)", streamLayout{mainVideo: -1, covers: []coverArt{{index: 1}}}, true},
		{"mkv with a font and a tagged cover", mkvSummary, streamLayout{mainVideo: 0, videoCount: 1, attachments: 1,
			covers: []coverArt{{index: 3, filename: "cover.jpg", mimetype: "image/jpeg"}}}, true},
		{"no streams", "in.mp4: No such file or directory", streamLayout{mainVideo: -1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseStreamLayout(tt.out)
			if got.mainVideo != tt.want.mainVideo || got.videoCount != tt.want.videoCount || got.attachments != tt.want.attachments || !slices.Equal(got.covers, tt.want.covers) || ok != tt.wantOK {
				t.Errorf("parseStreamLayout() = %+v, %v; want %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestBuildFFmpegArgsKeepsCoverChaptersAndSubtitles(t *testing.T) {
	engine := &PPEngine{}
	job := testJob([]string{"deband"}, nil, 4)
	job.layout = &streamLayout{mainVideo: 0, videoCount: 1, covers: []coverArt{{index: 4}}}

	got := strings.Join(engine.buildFFmpegArgsForBackend(job, BackendOff), " ")

	want := "-y -threads 4 -i in.mp4 " +
		"-map 0:0 -map 0:4 -map 0:a? -map 0:s? -map 0:t? -map_metadata 0 -map_chapters 0 " +
		"-filter:v:0 deband -c:v:0 libx264 -crf 18 -preset slower " +
		"-c:v:1 copy -disposition:v:1 attached_pic -c:s copy -c:a copy in_pp.mp4"
	if got != want {
		t.Errorf("args =\n%s\nwant\n%s", got, want)
	}
}

func TestBuildFFmpegArgsAudioOnlyFilteringCopiesAllVideo(t *testing.T) {
	engine := &PPEngine{}
	job := testJob(nil, []string{"loudnorm"}, 2)
	job.layout = &streamLayout{mainVideo: -1, covers: []coverArt{{index: 1}}}

	got := strings.Join(engine.buildFFmpegArgsForBackend(job, BackendOff), " ")

	for _, part := range []string{"-map 0:V? -map 0:1 -map 0:a?", "-map_chapters 0", "-c:v copy -disposition:v:0 attached_pic", "-af loudnorm"} {
		if !strings.Contains(got, part) {
			t.Errorf("args %q missing %q", got, part)
		}
	}
}

func TestEncodeFirstVideoOnly(t *testing.T) {
	plan := []string{"-c:v", "h264_nvenc", "-rc", "constqp"}
	if got := encodeFirstVideoOnly(plan); !slices.Equal(got, []string{"-c:v:0", "h264_nvenc", "-rc", "constqp"}) {
		t.Errorf("encodeFirstVideoOnly() = %q", got)
	}
	if plan[0] != "-c:v" {
		t.Error("encodeFirstVideoOnly modified its input")
	}
}

// mkvSummary is ffmpeg's input summary for a Matroska file with a font
// attachment and a cover attachment (which ffmpeg lists as a video stream).
const mkvSummary = `Input #0, matroska,webm, from 'rich.mkv':
  Stream #0:0: Video: h264 (High), yuv420p(progressive), 320x180 (default)
  Stream #0:1: Audio: aac (LC), 44100 Hz, mono, fltp (default)
  Stream #0:2: Attachment: ttf
    Metadata:
      filename        : font.ttf
      mimetype        : font/ttf
  Stream #0:3: Video: mjpeg (Baseline), yuvj420p(pc, bt470bg/unknown/unknown), 320x180, 90k tbr, 90k tbn (attached pic)
    Metadata:
      filename        : cover.jpg
      mimetype        : image/jpeg`

func TestBuildFFmpegArgsReattachesMatroskaCovers(t *testing.T) {
	engine := &PPEngine{}
	job := PostProcessJob{inputPath: "in.mkv", tmpOutput: "in_pp.mkv", vfFilters: []string{"deband"}, threads: 4}
	job.layout = &streamLayout{
		mainVideo: 0, videoCount: 1, attachments: 1,
		covers:     []coverArt{{index: 3, filename: "cover.jpg", mimetype: "image/jpeg"}},
		coverFiles: []string{"in_pp_cover1.jpg"},
	}

	got := strings.Join(engine.buildFFmpegArgsForBackend(job, BackendOff), " ")

	if strings.Contains(got, "-map 0:3") || strings.Contains(got, "attached_pic") {
		t.Errorf("args %q map the cover as a stream; Matroska would store it as a video track", got)
	}
	want := "-attach in_pp_cover1.jpg -metadata:s:t:1 mimetype=image/jpeg -metadata:s:t:1 filename=cover.jpg"
	if !strings.Contains(got, want) {
		t.Errorf("args %q missing %q", got, want)
	}
}
