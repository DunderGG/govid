package main

import "testing"

func TestIsEncoderCompiled(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		encoder string
		want    bool
	}{
		{
			name:    "encoder present",
			output:  " V....D h264_nvenc           NVIDIA NVENC H.264 encoder (codec h264)",
			encoder: "h264_nvenc",
			want:    true,
		},
		{
			name:    "encoder absent",
			output:  " V....D libx264              libx264 H.264 (codec h264)",
			encoder: "h264_nvenc",
			want:    false,
		},
		{
			name:    "partial name is not a match",
			output:  " V....D h264_nvenc_foo       Not the real encoder (codec h264)",
			encoder: "h264_nvenc",
			want:    false,
		},
		{
			name:    "empty output",
			output:  "",
			encoder: "h264_nvenc",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEncoderCompiled(tt.output, tt.encoder)
			if got != tt.want {
				t.Errorf("isEncoderCompiled(%q, %q) = %v, want %v", tt.output, tt.encoder, got, tt.want)
			}
		})
	}
}

func TestPlanEncoder(t *testing.T) {
	available := func(backend GPUBackend) map[GPUBackend]BackendCapability {
		return map[GPUBackend]BackendCapability{backend: {Backend: backend, Available: true}}
	}

	tests := []struct {
		name         string
		requested    GPUBackend
		capabilities map[GPUBackend]BackendCapability
		containerExt string
		wantUsedGPU  bool
		wantBackend  GPUBackend
	}{
		{
			name:         "webm always stays CPU regardless of backend",
			requested:    BackendNVIDIA,
			capabilities: available(BackendNVIDIA),
			containerExt: ".webm",
			wantUsedGPU:  false,
		},
		{
			name:         "off falls back to CPU",
			requested:    BackendOff,
			capabilities: available(BackendNVIDIA),
			containerExt: ".mp4",
			wantUsedGPU:  false,
		},
		{
			name:         "empty backend falls back to CPU",
			requested:    "",
			capabilities: nil,
			containerExt: ".mp4",
			wantUsedGPU:  false,
		},
		{
			name:         "auto with nothing available falls back to CPU",
			requested:    BackendAuto,
			capabilities: map[GPUBackend]BackendCapability{},
			containerExt: ".mp4",
			wantUsedGPU:  false,
		},
		{
			name:         "auto picks first available by priority",
			requested:    BackendAuto,
			capabilities: available(BackendAMD),
			containerExt: ".mkv",
			wantUsedGPU:  true,
			wantBackend:  BackendAMD,
		},
		{
			name:         "explicit backend requested but unavailable falls back to CPU",
			requested:    BackendIntel,
			capabilities: map[GPUBackend]BackendCapability{BackendIntel: {Backend: BackendIntel, Available: false}},
			containerExt: ".mp4",
			wantUsedGPU:  false,
		},
		{
			name:         "explicit nvidia backend available",
			requested:    BackendNVIDIA,
			capabilities: available(BackendNVIDIA),
			containerExt: ".mp4",
			wantUsedGPU:  true,
			wantBackend:  BackendNVIDIA,
		},
		{
			name:         "explicit vaapi backend available",
			requested:    BackendVAAPI,
			capabilities: available(BackendVAAPI),
			containerExt: ".mkv",
			wantUsedGPU:  true,
			wantBackend:  BackendVAAPI,
		},
		{
			name:         "explicit videotoolbox backend available",
			requested:    BackendVideoToolbox,
			capabilities: available(BackendVideoToolbox),
			containerExt: ".mp4",
			wantUsedGPU:  true,
			wantBackend:  BackendVideoToolbox,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := PlanEncoder(tt.requested, tt.capabilities, tt.containerExt)
			if plan.UsedGPU != tt.wantUsedGPU {
				t.Errorf("PlanEncoder(...).UsedGPU = %v, want %v", plan.UsedGPU, tt.wantUsedGPU)
			}
			if tt.wantUsedGPU && plan.Backend != tt.wantBackend {
				t.Errorf("PlanEncoder(...).Backend = %v, want %v", plan.Backend, tt.wantBackend)
			}
			if len(plan.Args) == 0 || plan.Label == "" {
				t.Errorf("PlanEncoder(...) returned an incomplete plan: %+v", plan)
			}
		})
	}
}
