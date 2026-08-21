package main

import (
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
			args := engine.buildFFmpegArgsForBackend("in.mp4", "in_pp.mp4", []string{"eq=contrast=1.1"}, nil, tt.backend)
			got := strings.Join(args, " ")
			if !strings.Contains(got, tt.want) {
				t.Errorf("buildFFmpegArgsForBackend(...) = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestBuildFFmpegArgsForBackendNoFilters(t *testing.T) {
	engine := &PPEngine{}
	args := engine.buildFFmpegArgsForBackend("in.mp4", "in_pp.mp4", nil, nil, BackendNVIDIA)
	got := strings.Join(args, " ")
	if !strings.Contains(got, "-c:v copy") {
		t.Errorf("buildFFmpegArgsForBackend(...) with no filters = %q, want stream copy", got)
	}
}

func TestNewPPEngineGPUSemCapacity(t *testing.T) {
	engine := NewPPEngine("ffmpeg", "ffprobe")
	if cap(engine.gpuSem) != maxConcurrentGPUJobs {
		t.Errorf("gpuSem capacity = %d, want %d", cap(engine.gpuSem), maxConcurrentGPUJobs)
	}
}
