package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
)

// assertDistinct fails if any two options map to the same result, which
// would mean one of them silently falls through to another's code path.
func assertDistinct(t *testing.T, options []string, result func(option string) string) {
	t.Helper()
	seen := map[string]string{}
	for _, option := range options {
		got := result(option)
		if other, ok := seen[got]; ok {
			t.Errorf("options %q and %q both produce %q", other, option, got)
		}
		seen[got] = option
	}
}

func TestEveryOptionSelectsItsOwnCodePath(t *testing.T) {
	engine := NewDownloadEngine("yt-dlp", "")

	t.Run("format", func(t *testing.T) {
		assertDistinct(t, formatOptions, func(format string) string {
			return engine.BuildArgs(DownloadRequest{Format: format, Quality: qualityBest}).Extension
		})
	})
	t.Run("quality", func(t *testing.T) {
		assertDistinct(t, qualityOptions, func(quality string) string {
			args := engine.BuildArgs(DownloadRequest{Format: formatMP4, Quality: quality}).Args
			return args[slices.Index(args, "-f")+1]
		})
	})
	t.Run("smooth motion mode", func(t *testing.T) {
		assertDistinct(t, smoothModeOptions, func(mode string) string {
			settings := PostProcessSettings{SmoothMotion: true, SmoothMotionMode: mode, SmoothMotionFPS: 60}
			vf, _ := buildPostProcessFilters(settings)
			cost, _ := computeProcessingLoad(settings)
			return strings.Join(vf, ",") + " cost=" + strconv.Itoa(cost)
		})
	})
	t.Run("denoise mode", func(t *testing.T) {
		assertDistinct(t, denoiseModeOptions, func(mode string) string {
			settings := PostProcessSettings{Denoise: true, DenoiseMode: mode}
			vf, _ := buildPostProcessFilters(settings)
			cost, _ := computeProcessingLoad(settings)
			return strings.Join(vf, ",") + " cost=" + strconv.Itoa(cost)
		})
	})
	t.Run("upscale target", func(t *testing.T) {
		assertDistinct(t, upscaleTargetOptions, func(target string) string {
			vf, _ := buildPostProcessFilters(PostProcessSettings{UpscaleVideo: true, UpscaleTarget: target})
			return strings.Join(vf, ",")
		})
	})
	t.Run("theme", func(t *testing.T) {
		app := test.NewApp()
		assertDistinct(t, themeOptions, func(mode string) string {
			applyTheme(app, mode)
			return fmt.Sprintf("%T", app.Settings().Theme())
		})
	})
}

func TestLogLimitOptionsParse(t *testing.T) {
	for _, option := range logLimitOptions {
		want := math.MaxInt32
		if option != logLimitUnlimited {
			n, err := strconv.Atoi(option)
			if err != nil {
				t.Fatalf("log limit option %q is neither %q nor a number", option, logLimitUnlimited)
			}
			want = n
		}
		if got := ParseBufferLimit(option); got != want {
			t.Errorf("ParseBufferLimit(%q) = %d, want %d", option, got, want)
		}
	}
}

func TestDefaultsAreValidOptions(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		options []string
	}{
		{"format", defaultFormat(), formatOptions},
		{"quality", defaultQuality, qualityOptions},
		{"theme", defaultThemeMode, themeOptions},
		{"log limit", defaultLogLimit, logLimitOptions},
		{"smooth motion mode", defaultSmoothMotionMode, smoothModeOptions},
		{"denoise mode", defaultDenoiseMode, denoiseModeOptions},
		{"upscale target", defaultUpscaleTarget, upscaleTargetOptions},
		{"GPU backend", defaultGPUBackend, GPUBackendOptions()},
	}
	for _, tt := range tests {
		if !slices.Contains(tt.options, tt.value) {
			t.Errorf("default %s %q is not one of %q", tt.name, tt.value, tt.options)
		}
	}
}
