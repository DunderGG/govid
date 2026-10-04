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
