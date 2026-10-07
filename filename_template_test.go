package main

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestValidateFilenameTemplate(t *testing.T) {
	for template, valid := range map[string]bool{
		defaultFilenameTemplate:     true,
		"%(uploader)s - %(title)s":  true,
		"":                          false,
		"   ":                       false,
		"%(uploader)s/%(title)s":    false,
		`%(uploader)s\%(title)s`:    false,
		"%(upload_date>%Y)s %(id)s": true,
	} {
		if err := validateFilenameTemplate(template); (err == nil) != valid {
			t.Errorf("validateFilenameTemplate(%q) = %v, want valid %v", template, err, valid)
		}
	}
	if filenameTemplateWarning("%(uploader)s video") == "" {
		t.Error("no warning for a template that names every video alike")
	}
	for _, template := range []string{"%(title).40s", "[%(id)s]", defaultFilenameTemplate} {
		if warning := filenameTemplateWarning(template); warning != "" {
			t.Errorf("filenameTemplateWarning(%q) = %q", template, warning)
		}
	}
}

func TestOutputTemplate(t *testing.T) {
	tests := []struct {
		template, suffix string
		trimmed          bool
		want             string
	}{
		{defaultFilenameTemplate, "", false, "GoVid_%(title)s_GOVID1.%(ext)s"},
		{defaultFilenameTemplate, heightLabel, false, "GoVid_%(title)s" + heightLabel + "_GOVID1.%(ext)s"},
		{defaultFilenameTemplate, heightLabel, true, "GoVid_%(title)s" + heightLabel + "_TRIM_GOVID1.%(ext)s"},
		{"%(uploader)s - %(title)s", heightLabel, false, "%(uploader)s - %(title)s_GOVID1.%(ext)s"},
		{"a/b", "", false, "GoVid_%(title)s_GOVID1.%(ext)s"},
		{"", "", false, "GoVid_%(title)s_GOVID1.%(ext)s"},
	}
	for _, tt := range tests {
		if got := outputTemplate(tt.template, tt.suffix, "GOVID1", tt.trimmed); got != tt.want {
			t.Errorf("outputTemplate(%q, %q, %v) = %q, want %q", tt.template, tt.suffix, tt.trimmed, got, tt.want)
		}
	}
}

func TestPreviewFilename(t *testing.T) {
	tests := []struct {
		template, format, quality string
		want                      string
		unknown                   []string
	}{
		{"%(uploader)s - %(title)s", formatMP4, qualityBest, "Rick Astley - Never Gonna Give You Up.mp4", nil},
		{defaultFilenameTemplate, formatMKV, quality720p, "GoVid_Never Gonna Give You Up_1080p.mkv", nil},
		{defaultFilenameTemplate, formatMP3, quality720p, "GoVid_Never Gonna Give You Up.mp3", nil},
		// As yt-dlp writes them (checked against the bundled yt-dlp).
		{"%(upload_date>%Y-%m-%d)s %(title).10s [%(id)s]", formatMP4, qualityBest, "2009-10-25 Never Gonn [dQw4w9WgXcQ].mp4", nil},
		{"%(height)05d %(channel|Unknown)s", formatWebM, qualityBest, "01080 Unknown.webm", nil},
		{"%(channel)s - %(title)s", formatMP4, qualityBest, "%(channel)s - Never Gonna Give You Up.mp4", []string{"channel"}},
	}
	for _, tt := range tests {
		got, unknown := previewFilename(tt.template, tt.format, tt.quality)
		if got != tt.want || !slices.Equal(unknown, tt.unknown) {
			t.Errorf("previewFilename(%q) = %q, unknown %q; want %q, %q", tt.template, got, unknown, tt.want, tt.unknown)
		}
	}
}

func TestTemplatePreviewText(t *testing.T) {
	if text := templatePreviewText("a/b", formatMP4, qualityBest); !strings.HasPrefix(text, "Not saved:") {
		t.Errorf("an invalid template previews as %q", text)
	}
	text := templatePreviewText("%(channel)s", formatMP4, qualityBest)
	if !strings.Contains(text, "⚠") || !strings.Contains(text, "Shown as written: channel") {
		t.Errorf("preview = %q, want the warning and the unknown field", text)
	}
}

func TestConfigRefusesAnInvalidTemplate(t *testing.T) {
	bad := "%(uploader)s/%(title)s"
	valid, errs := ValidateConfig(AppConfig{FilenameTemplate: &bad})
	if valid.FilenameTemplate != nil || len(errs) != 1 || !strings.Contains(errs[0], "filenameTemplate") {
		t.Errorf("ValidateConfig kept %v, errors %q", valid.FilenameTemplate, errs)
	}
	entry := newTemplateEntry()
	entry.SetText(bad)
	if entry.Validate() == nil {
		t.Error("the Preferences entry accepts a template with a slash")
	}
}

// templateHarness is a download harness that names files with
// "%(uploader)s - %(title)s".
func templateHarness(t *testing.T) *downloadHarness {
	t.Helper()
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.ui.prefs.filenameTemplate.SetText("%(uploader)s - %(title)s")
	h.app.ui.download.entry.SetText("https://example.com/v")
	return h
}

func TestCustomTemplateNamesTheDownload(t *testing.T) {
	h := templateHarness(t)
	h.app.askDuplicate = func(context.Context, duplicatePrompt) duplicateDecision { return duplicateDownload }

	h.startAndWait(t)
	h.startAndWait(t) // the same name again

	want := []string{"Fake Uploader - Fake Video 1.mp4", "Fake Uploader - Fake Video.mp4"}
	if files := h.savedFiles(t); !slices.Equal(files, want) {
		t.Errorf("saved files = %q, want %q", files, want)
	}
	if entries := h.history(t); len(entries) != 2 || entries[0].FinalFilename != "Fake Uploader - Fake Video.mp4" {
		t.Errorf("history = %+v", entries)
	}
}

func TestCustomTemplateWithTrimAndSubtitles(t *testing.T) {
	h := templateHarness(t)
	h.app.ui.download.trimStart.SetText("00:00:01")
	h.app.ui.prefs.subtitles.SetSelected(subtitlesBoth)
	h.app.ui.prefs.subtitleLangs.SetText("en.*")

	h.startAndWait(t)

	want := []string{"Fake Uploader - Fake Video_TRIM.en.srt", "Fake Uploader - Fake Video_TRIM.mp4"}
	if files := h.savedFiles(t); !slices.Equal(files, want) {
		t.Errorf("saved files = %q, want %q", files, want)
	}
	if entries := h.history(t); len(entries) != 1 || entries[0].FinalFilename != "Fake Uploader - Fake Video_TRIM.mp4" {
		t.Errorf("history = %+v, want only the video", entries)
	}
}
