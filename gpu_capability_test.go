package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestGPUBackendFromLabel(t *testing.T) {
	tests := []struct {
		label string
		want  GPUBackend
	}{
		{"Auto (Recommended)", BackendAuto},
		{"Off", BackendOff},
		{"NVIDIA", BackendNVIDIA},
		{"Intel", BackendIntel},
		{"AMD", BackendAMD},
		{"VAAPI", BackendVAAPI},
		{"VideoToolbox", BackendVideoToolbox},
		{"unrecognized label", BackendAuto},
		{"", BackendAuto},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			if got := GPUBackendFromLabel(tt.label); got != tt.want {
				t.Errorf("GPUBackendFromLabel(%q) = %v, want %v", tt.label, got, tt.want)
			}
		})
	}
}

func TestGPUBackendOptions(t *testing.T) {
	options := GPUBackendOptions()
	if len(options) < 2 || options[0] != "Auto (Recommended)" || options[1] != "Off" {
		t.Errorf("GPUBackendOptions() = %v, want it to start with [Auto (Recommended) Off]", options)
	}
	for _, label := range options {
		if GPUBackendFromLabel(label) == BackendAuto && label != "Auto (Recommended)" {
			t.Errorf("GPUBackendOptions() included unrecognized label %q", label)
		}
	}
}

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

// firstApplicableBackendDef returns the first backendDef targeting the
// current runtime.GOOS, so diagnostics tests stay valid across platforms.
func firstApplicableBackendDef(t *testing.T) backendDef {
	t.Helper()
	for _, def := range backendDefs {
		if osIn(def.OSes, runtime.GOOS) {
			return def
		}
	}
	t.Fatalf("no backendDef targets runtime.GOOS %q", runtime.GOOS)
	return backendDef{}
}

func TestFormatGPUDiagnostics(t *testing.T) {
	def := firstApplicableBackendDef(t)

	t.Run("available backend reports its encoder", func(t *testing.T) {
		caps := map[GPUBackend]BackendCapability{
			def.Backend: {Backend: def.Backend, Available: true, Encoder: def.Encoder},
		}
		lines := FormatGPUDiagnostics(caps)
		found := false
		for _, line := range lines {
			if strings.Contains(line, "available ("+def.Encoder+")") {
				found = true
			}
		}
		if !found {
			t.Errorf("FormatGPUDiagnostics(...) = %v, want a line reporting %s available (%s)", lines, def.Backend, def.Encoder)
		}
	})

	t.Run("unavailable backend reports its reason", func(t *testing.T) {
		caps := map[GPUBackend]BackendCapability{
			def.Backend: {Backend: def.Backend, Available: false, Reason: "driver initialization failed"},
		}
		lines := FormatGPUDiagnostics(caps)
		found := false
		for _, line := range lines {
			if strings.Contains(line, "unavailable — driver initialization failed") {
				found = true
			}
		}
		if !found {
			t.Errorf("FormatGPUDiagnostics(...) = %v, want a line reporting the unavailable reason", lines)
		}
	})

	t.Run("missing capability reports not detected", func(t *testing.T) {
		lines := FormatGPUDiagnostics(map[GPUBackend]BackendCapability{})
		found := false
		for _, line := range lines {
			if strings.Contains(line, "not detected") {
				found = true
			}
		}
		if !found {
			t.Errorf("FormatGPUDiagnostics(...) = %v, want a line reporting not detected", lines)
		}
	})

	t.Run("no applicable backends falls back to a CPU-only message", func(t *testing.T) {
		original := backendDefs
		backendDefs = nil
		defer func() { backendDefs = original }()

		lines := FormatGPUDiagnostics(map[GPUBackend]BackendCapability{})
		if len(lines) != 1 || !strings.Contains(lines[0], "No GPU acceleration backend") {
			t.Errorf("FormatGPUDiagnostics(...) = %v, want a single CPU-only fallback message", lines)
		}
	})
}
