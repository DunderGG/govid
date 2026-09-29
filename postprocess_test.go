package main

import (
	"bufio"
	"reflect"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

// allFiltersSettings enables every post-processing option with its default mode.
func allFiltersSettings() PostProcessSettings {
	return PostProcessSettings{
		SmoothMotion:     true,
		SmoothMotionMode: "Precise (slow)",
		SmoothMotionFPS:  60,
		Sharpen:          true,
		SharpenAmount:    1.0,
		VividMode:        true,
		Deband:           true,
		HDRToSDR:         true,
		Denoise:          true,
		DenoiseMode:      "hqdn3d (Balanced)",
		Deinterlace:      true,
		Stabilize:        true,
		AutoCrop:         true,
		UpscaleVideo:     true,
		UpscaleTarget:    "1080p",
		NormalizeAudio:   true,
		NightMode:        true,
	}
}

func TestBuildPostProcessFiltersNoneEnabled(t *testing.T) {
	vf, af := buildPostProcessFilters(PostProcessSettings{
		SmoothMotionMode: "Fast",
		SharpenAmount:    2,
		DenoiseMode:      "NLMeans (HQ, slow)",
		UpscaleTarget:    "4K (2160p)",
	})
	if len(vf) != 0 || len(af) != 0 {
		t.Errorf("buildPostProcessFilters(disabled) = %v, %v; want no filters", vf, af)
	}
}

func TestBuildPostProcessFiltersSingleOption(t *testing.T) {
	tests := []struct {
		name     string
		settings PostProcessSettings
		wantVF   []string
		wantAF   []string
	}{
		{"smooth motion fast", PostProcessSettings{SmoothMotion: true, SmoothMotionMode: "Fast", SmoothMotionFPS: 60},
			[]string{"minterpolate=fps=60:mi_mode=blend"}, nil},
		{"smooth motion balanced", PostProcessSettings{SmoothMotion: true, SmoothMotionMode: "Balanced", SmoothMotionFPS: 48},
			[]string{"minterpolate=fps=48:mi_mode=mci:vsbmc=0:mc_mode=obmc"}, nil},
		{"smooth motion precise", PostProcessSettings{SmoothMotion: true, SmoothMotionMode: "Precise (slow)", SmoothMotionFPS: 120},
			[]string{"minterpolate=fps=120:mi_mode=mci"}, nil},
		{"smooth motion fractional fps truncates", PostProcessSettings{SmoothMotion: true, SmoothMotionMode: "Fast", SmoothMotionFPS: 59.94},
			[]string{"minterpolate=fps=59:mi_mode=blend"}, nil},
		{"sharpen midpoint", PostProcessSettings{Sharpen: true, SharpenAmount: 1.0},
			[]string{"cas=strength=0.40"}, nil},
		{"sharpen max", PostProcessSettings{Sharpen: true, SharpenAmount: 2.0},
			[]string{"cas=strength=0.80"}, nil},
		{"vivid mode", PostProcessSettings{VividMode: true},
			[]string{"eq=contrast=1.30:brightness=0.02:saturation=1.50:gamma_b=1.1"}, nil},
		{"deband", PostProcessSettings{Deband: true}, []string{"deband"}, nil},
		{"denoise nlmeans", PostProcessSettings{Denoise: true, DenoiseMode: "NLMeans (HQ, slow)"},
			[]string{"nlmeans=2.0:7:5:15:9"}, nil},
		{"denoise hqdn3d", PostProcessSettings{Denoise: true, DenoiseMode: "hqdn3d (Balanced)"},
			[]string{"hqdn3d=4:3:6:4.5"}, nil},
		{"deinterlace", PostProcessSettings{Deinterlace: true}, []string{"bwdif"}, nil},
		{"stabilize", PostProcessSettings{Stabilize: true}, []string{"deshake"}, nil},
		{"auto-crop placeholder", PostProcessSettings{AutoCrop: true}, []string{"__autocrop__"}, nil},
		{"upscale 1080p", PostProcessSettings{UpscaleVideo: true, UpscaleTarget: "1080p"},
			[]string{`scale=-2:if(gte(ih\,1080)\,ih\,1080):flags=lanczos`}, nil},
		{"upscale 1440p", PostProcessSettings{UpscaleVideo: true, UpscaleTarget: "1440p"},
			[]string{`scale=-2:if(gte(ih\,1440)\,ih\,1440):flags=lanczos`}, nil},
		{"upscale 4K", PostProcessSettings{UpscaleVideo: true, UpscaleTarget: "4K (2160p)"},
			[]string{`scale=-2:if(gte(ih\,2160)\,ih\,2160):flags=lanczos`}, nil},
		{"upscale double", PostProcessSettings{UpscaleVideo: true, UpscaleTarget: "2× (Double)"},
			[]string{"scale=iw*2:ih*2:flags=lanczos"}, nil},
		{"normalize audio", PostProcessSettings{NormalizeAudio: true}, nil, []string{"loudnorm"}},
		{"night mode", PostProcessSettings{NightMode: true}, nil, []string{"dynaudnorm=f=300:g=5:p=0.95"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vf, af := buildPostProcessFilters(tt.settings)
			if !reflect.DeepEqual(vf, tt.wantVF) {
				t.Errorf("vf = %q, want %q", vf, tt.wantVF)
			}
			if !reflect.DeepEqual(af, tt.wantAF) {
				t.Errorf("af = %q, want %q", af, tt.wantAF)
			}
		})
	}
}

func TestBuildPostProcessFiltersHDRToSDR(t *testing.T) {
	vf, _ := buildPostProcessFilters(PostProcessSettings{HDRToSDR: true})
	if len(vf) != 1 {
		t.Fatalf("vf = %q, want one filter chain", vf)
	}
	for _, step := range []string{"zscale=t=linear", "tonemap=tonemap=hable", "format=yuv420p"} {
		if !strings.Contains(vf[0], step) {
			t.Errorf("HDR chain %q missing %q", vf[0], step)
		}
	}
}

func TestBuildPostProcessFiltersAllEnabledOrder(t *testing.T) {
	vf, af := buildPostProcessFilters(allFiltersSettings())

	wantPrefixes := []string{
		"minterpolate", "cas=", "eq=", "deband", "zscale=t=linear",
		"hqdn3d", "bwdif", "deshake", "__autocrop__", "scale=",
	}
	if len(vf) != len(wantPrefixes) {
		t.Fatalf("vf has %d filters, want %d: %q", len(vf), len(wantPrefixes), vf)
	}
	for i, prefix := range wantPrefixes {
		if !strings.HasPrefix(vf[i], prefix) {
			t.Errorf("vf[%d] = %q, want prefix %q", i, vf[i], prefix)
		}
	}
	if want := []string{"loudnorm", "dynaudnorm=f=300:g=5:p=0.95"}; !reflect.DeepEqual(af, want) {
		t.Errorf("af = %q, want %q", af, want)
	}
}

func TestBuildPostProcessFiltersUnknownModesFallBack(t *testing.T) {
	vf, _ := buildPostProcessFilters(PostProcessSettings{
		SmoothMotion:     true,
		SmoothMotionMode: "",
		SmoothMotionFPS:  60,
		Denoise:          true,
		DenoiseMode:      "ATADenoise (retired)",
		UpscaleVideo:     true,
		UpscaleTarget:    "8K",
	})

	want := []string{
		"minterpolate=fps=60:mi_mode=mci",
		"hqdn3d=4:3:6:4.5",
		"scale=iw*2:ih*2:flags=lanczos",
	}
	if !reflect.DeepEqual(vf, want) {
		t.Errorf("vf = %q, want defaults %q", vf, want)
	}
}

func TestEveryBuiltFilterHasShortName(t *testing.T) {
	settings := []PostProcessSettings{allFiltersSettings()}
	alt := allFiltersSettings()
	alt.SmoothMotionMode = "Fast"
	alt.DenoiseMode = "NLMeans (HQ, slow)"
	settings = append(settings, alt)
	alt.SmoothMotionMode = "Balanced"
	settings = append(settings, alt)

	for _, s := range settings {
		vf, af := buildPostProcessFilters(s)
		for _, filter := range append(vf, af...) {
			if filter == "__autocrop__" {
				continue // replaced by a concrete crop= filter before use
			}
			if got := filterShortName(filter); got == filter {
				t.Errorf("filterShortName(%q) has no friendly label", filter)
			}
		}
	}
}

func TestFilterShortName(t *testing.T) {
	tests := []struct {
		filter string
		want   string
	}{
		{"minterpolate=fps=60:mi_mode=blend", "Smooth Motion (Fast)"},
		{"minterpolate=fps=60:mi_mode=mci:vsbmc=0:mc_mode=obmc", "Smooth Motion (Balanced)"},
		{"minterpolate=fps=60:mi_mode=mci", "Smooth Motion (Precise)"},
		{"crop=1920:800:0:140", "Auto-Crop"},
		{"atadenoise", "Denoise (ATADenoise)"},
		{"unknownfilter=1", "unknownfilter=1"},
	}
	for _, tt := range tests {
		if got := filterShortName(tt.filter); got != tt.want {
			t.Errorf("filterShortName(%q) = %q, want %q", tt.filter, got, tt.want)
		}
	}
}

func TestCheckPostProcessingEnabled(t *testing.T) {
	if checkPostProcessingEnabled(PostProcessSettings{SmoothMotionMode: "Fast", SharpenAmount: 2}) {
		t.Error("checkPostProcessingEnabled(no toggles) = true, want false")
	}

	toggles := map[string]func(*PostProcessSettings){
		"SmoothMotion":   func(s *PostProcessSettings) { s.SmoothMotion = true },
		"Sharpen":        func(s *PostProcessSettings) { s.Sharpen = true },
		"NormalizeAudio": func(s *PostProcessSettings) { s.NormalizeAudio = true },
		"VividMode":      func(s *PostProcessSettings) { s.VividMode = true },
		"Denoise":        func(s *PostProcessSettings) { s.Denoise = true },
		"HDRToSDR":       func(s *PostProcessSettings) { s.HDRToSDR = true },
		"Deband":         func(s *PostProcessSettings) { s.Deband = true },
		"AutoCrop":       func(s *PostProcessSettings) { s.AutoCrop = true },
		"Stabilize":      func(s *PostProcessSettings) { s.Stabilize = true },
		"Deinterlace":    func(s *PostProcessSettings) { s.Deinterlace = true },
		"NightMode":      func(s *PostProcessSettings) { s.NightMode = true },
		"UpscaleVideo":   func(s *PostProcessSettings) { s.UpscaleVideo = true },
	}
	for name, enable := range toggles {
		var s PostProcessSettings
		enable(&s)
		if !checkPostProcessingEnabled(s) {
			t.Errorf("checkPostProcessingEnabled with only %s = false, want true", name)
		}
	}
}

func TestComputeProcessingLoad(t *testing.T) {
	tests := []struct {
		name     string
		settings PostProcessSettings
		wantCost int
		wantDesc string
	}{
		{"nothing", PostProcessSettings{}, 0, "No post-processing active"},
		{"light", PostProcessSettings{Sharpen: true, VividMode: true}, costSharpen + costVividMode, "Light — minimal overhead"},
		{"moderate at threshold", PostProcessSettings{Denoise: true, DenoiseMode: "hqdn3d (Balanced)"}, costDenoiseHQDN3D, "Moderate — noticeable extra time"},
		{"heavy", PostProcessSettings{SmoothMotion: true, SmoothMotionMode: "Balanced"}, costSmoothMotionBalanced, "Heavy — significant re-encode time"},
		{"very heavy", PostProcessSettings{SmoothMotion: true, SmoothMotionMode: "Precise (slow)", HDRToSDR: true}, costSmoothMotionPrecise + costHDRToSDR, "Very Heavy — expect long processing"},
		{"upscale 4K costs more", PostProcessSettings{UpscaleVideo: true, UpscaleTarget: "4K (2160p)"}, costUpscale4K, "Moderate — noticeable extra time"},
		{"unknown modes use defaults", PostProcessSettings{SmoothMotion: true, SmoothMotionMode: "bogus", Denoise: true, DenoiseMode: "bogus"}, costSmoothMotionPrecise + costDenoiseHQDN3D, "Very Heavy — expect long processing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, desc := computeProcessingLoad(tt.settings)
			if cost != tt.wantCost || desc != tt.wantDesc {
				t.Errorf("computeProcessingLoad = %d, %q; want %d, %q", cost, desc, tt.wantCost, tt.wantDesc)
			}
		})
	}

	cost, desc := computeProcessingLoad(allFiltersSettings())
	if cost < loadThresholdVeryHeavy || desc != "Intensive — expect very long processing" {
		t.Errorf("computeProcessingLoad(all) = %d, %q; want intensive", cost, desc)
	}
}

func TestNewPostProcessSettingsSnapshotsWidgets(t *testing.T) {
	_ = test.NewApp()
	ui := NewUIWidgets()
	ui.postProcess.smoothMotion.SetChecked(true)
	ui.postProcess.smoothMotionMode.SetSelected("Balanced")
	ui.postProcess.smoothMotionFPS.SetValue(90)
	ui.postProcess.denoise.SetChecked(true)
	ui.postProcess.denoiseMode.SetSelected("NLMeans (HQ, slow)")
	ui.postProcess.upscaleVideo.SetChecked(true)
	ui.postProcess.upscaleTarget.SetSelected("1440p")
	ui.postProcess.nightMode.SetChecked(true)

	got := newPostProcessSettings(ui)
	want := PostProcessSettings{
		SmoothMotion:     true,
		SmoothMotionMode: "Balanced",
		SmoothMotionFPS:  90,
		SharpenAmount:    ui.postProcess.sharpenAmount.Value,
		Denoise:          true,
		DenoiseMode:      "NLMeans (HQ, slow)",
		UpscaleVideo:     true,
		UpscaleTarget:    "1440p",
		NightMode:        true,
	}
	if got != want {
		t.Errorf("newPostProcessSettings = %+v, want %+v", got, want)
	}
}

func TestFormatFFmpegProgress(t *testing.T) {
	const statsLine = "frame=  240 fps= 60 q=28.0 size=    1024KiB time=00:00:04.00 bitrate=2097.2kbits/s speed=2.01x"

	tests := []struct {
		name        string
		line        string
		totalFrames int64
		want        string
	}{
		{"with frame total", statsLine, 300, "80% | 60 fps | speed 2.01x"},
		{"unknown frame total", statsLine, 0, "frame 240 | 60 fps | time 00:00:04.00 | speed 2.01x"},
		{"clamped to 100%", statsLine, 200, "100% | 60 fps | speed 2.01x"},
		{"zero fps omitted", "frame=0 fps=0 time=00:00:00.00 speed=N/A", 0, "frame 0 | time 00:00:00.00 | speed N/A"},
		{"non-stats line passes through", "[out#0/mp4 @ 000001] video:1000KiB", 0, "[out#0/mp4 @ 000001] video:1000KiB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatFFmpegProgress(tt.line, tt.totalFrames); got != tt.want {
				t.Errorf("formatFFmpegProgress = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScanCRLFSplitsFFmpegProgress(t *testing.T) {
	scanner := bufio.NewScanner(openFixture(t, "ffmpeg_progress.log"))
	scanner.Split(scanCRLF)

	var lines []string
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// 2 header lines, 3 CR-separated progress updates, 1 summary line.
	if len(lines) != 6 {
		t.Fatalf("got %d non-empty lines, want 6: %q", len(lines), lines)
	}

	var progress []string
	for _, line := range lines {
		if strings.HasPrefix(line, "frame=") {
			progress = append(progress, formatFFmpegProgress(line, 300))
		}
	}
	wantPrefixes := []string{"20% |", "50% |", "80% |"}
	if len(progress) != len(wantPrefixes) {
		t.Fatalf("progress = %q, want %d updates", progress, len(wantPrefixes))
	}
	for i, prefix := range wantPrefixes {
		if !strings.HasPrefix(progress[i], prefix) {
			t.Errorf("progress[%d] = %q, want prefix %q", i, progress[i], prefix)
		}
	}
}

func TestScanCRLFFinalTokenWithoutTerminator(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("a\rb\nc"))
	scanner.Split(scanCRLF)

	var got []string
	for scanner.Scan() {
		got = append(got, scanner.Text())
	}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tokens = %q, want %q", got, want)
	}
}

func TestLastLine(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"single", "single"},
		{"first\nsecond\n", "second"},
		{"  first\n  last line  \n\n", "last line"},
	}
	for _, tt := range tests {
		if got := lastLine(tt.in); got != tt.want {
			t.Errorf("lastLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{45*1024*1024 + 200*1024, "45.2 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
		{5 * 1024 * 1024 * 1024 * 1024, "5.0 TiB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.in); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0.000 seconds"},
		{1500 * time.Millisecond, "1.500 seconds"},
		{59999 * time.Millisecond, "59.999 seconds"},
		{time.Minute, "1 minutes and 0.000 seconds"},
		{2*time.Minute + 3250*time.Millisecond, "2 minutes and 3.250 seconds"},
	}
	for _, tt := range tests {
		if got := formatDuration(tt.in); got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
