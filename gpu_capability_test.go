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
