package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// newTempHistoryService returns a HistoryService backed by a file in a fresh
// temp directory. When fixture is non-empty, it is copied in as the starting
// history file.
func newTempHistoryService(t *testing.T, fixture string) *HistoryService {
	t.Helper()
	path := filepath.Join(t.TempDir(), historyFileName)
	if fixture != "" {
		data, err := os.ReadFile(filepath.Join("testdata", fixture))
		if err != nil {
			t.Fatalf("read fixture %s: %v", fixture, err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("write history file: %v", err)
		}
	}
	return &HistoryService{filePath: path}
}

func TestHistoryLoadMissingFile(t *testing.T) {
	svc := newTempHistoryService(t, "")

	entries, err := svc.Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if entries != nil {
		t.Errorf("Load() = %v, want nil", entries)
	}
}

func TestHistoryLoadEmptyFile(t *testing.T) {
	svc := newTempHistoryService(t, "")
	if err := os.WriteFile(svc.filePath, nil, 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := svc.Load()
	if err != nil || entries != nil {
		t.Errorf("Load() = %v, %v; want nil, nil", entries, err)
	}
}

func TestHistoryLoadFixture(t *testing.T) {
	svc := newTempHistoryService(t, "history_valid.json")

	entries, err := svc.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Load() returned %d entries, want 2", len(entries))
	}

	want := DownloadHistoryEntry{
		URL:           "https://example.com/podcast",
		OriginalTitle: "Podcast Episode",
		FinalFilename: "GoVid_Podcast Episode.mp3",
		SavedPath:     `C:\Downloads`,
		Format:        "MP3",
		Quality:       "Best Quality",
		DownloadedAt:  "2026-01-16 08:00:00",
		PostProcessed: true,
	}
	if entries[1] != want {
		t.Errorf("entries[1] = %+v, want %+v", entries[1], want)
	}
}

func TestHistoryLoadCorruptedFile(t *testing.T) {
	svc := newTempHistoryService(t, "history_corrupted.json")

	if _, err := svc.Load(); err == nil {
		t.Error("Load() on corrupted file returned nil error")
	}
}

func TestHistoryAppendAllDoesNotOverwriteCorruptedFile(t *testing.T) {
	svc := newTempHistoryService(t, "history_corrupted.json")
	before, _ := os.ReadFile(svc.filePath)

	err := svc.AppendAll(DownloadRecord{URL: "https://example.com/new"})
	if err == nil {
		t.Fatal("AppendAll() on corrupted file returned nil error")
	}

	after, _ := os.ReadFile(svc.filePath)
	if string(after) != string(before) {
		t.Error("AppendAll() modified a corrupted history file instead of leaving it for recovery")
	}
}

func TestHistoryAppendAllRoundTrip(t *testing.T) {
	svc := newTempHistoryService(t, "history_valid.json")
	saveDir := filepath.Join("downloads", "videos")

	err := svc.AppendAll(DownloadRecord{
		URL: "https://example.com/new",
		FinalPaths: []string{
			filepath.Join(saveDir, "GoVid_New Video_720p.mp4"),
			filepath.Join(saveDir, "GoVid_New Video_720p.en.vtt"),
		},
		SavePath:      saveDir,
		Format:        "MP4",
		Quality:       "720p",
		PostProcessed: true,
	})
	if err != nil {
		t.Fatalf("AppendAll() error = %v", err)
	}

	entries, err := svc.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("Load() returned %d entries, want 4 (2 existing + 2 new)", len(entries))
	}

	got := entries[2]
	if got.URL != "https://example.com/new" ||
		got.FinalFilename != "GoVid_New Video_720p.mp4" ||
		got.OriginalTitle != "New Video" ||
		got.SavedPath != saveDir ||
		got.Format != "MP4" || got.Quality != "720p" || !got.PostProcessed {
		t.Errorf("appended entry = %+v", got)
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`).MatchString(got.DownloadedAt) {
		t.Errorf("DownloadedAt = %q, want YYYY-MM-DD HH:MM:SS", got.DownloadedAt)
	}
	if entries[3].FinalFilename != "GoVid_New Video_720p.en.vtt" {
		t.Errorf("entries[3].FinalFilename = %q", entries[3].FinalFilename)
	}
	if entries[0].URL != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Errorf("existing entries were not preserved: entries[0] = %+v", entries[0])
	}
}

func TestHistoryAppendAllCreatesMissingFile(t *testing.T) {
	svc := newTempHistoryService(t, "")

	if err := svc.AppendAll(DownloadRecord{URL: "https://example.com/first"}); err != nil {
		t.Fatalf("AppendAll() error = %v", err)
	}

	entries, err := svc.Load()
	if err != nil || len(entries) != 1 {
		t.Fatalf("Load() = %v, %v; want 1 entry", entries, err)
	}
}

func TestHistoryAppendAllWritesPlaceholderWithoutPaths(t *testing.T) {
	svc := newTempHistoryService(t, "")

	err := svc.AppendAll(DownloadRecord{
		URL:      "https://example.com/no-files",
		SavePath: "downloads",
		Format:   "MKV",
		Quality:  "Best Quality",
	})
	if err != nil {
		t.Fatalf("AppendAll() error = %v", err)
	}

	entries, _ := svc.Load()
	if len(entries) != 1 {
		t.Fatalf("Load() returned %d entries, want 1", len(entries))
	}
	got := entries[0]
	if got.URL != "https://example.com/no-files" || got.SavedPath != "downloads" ||
		got.FinalFilename != "" || got.OriginalTitle != "" {
		t.Errorf("placeholder entry = %+v", got)
	}
}

func TestHistoryClear(t *testing.T) {
	svc := newTempHistoryService(t, "history_valid.json")

	if err := svc.Clear(); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	entries, err := svc.Load()
	if err != nil {
		t.Fatalf("Load() after Clear() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Load() after Clear() returned %d entries, want 0", len(entries))
	}

	// Clearing also recovers from a corrupted file.
	corrupted := newTempHistoryService(t, "history_corrupted.json")
	if err := corrupted.Clear(); err != nil {
		t.Fatalf("Clear() on corrupted file error = %v", err)
	}
	if _, err := corrupted.Load(); err != nil {
		t.Errorf("Load() after clearing corrupted file error = %v", err)
	}
}

func TestInferOriginalTitle(t *testing.T) {
	tests := []struct {
		filename string
		quality  string
		want     string
	}{
		{"GoVid_My Video.mp4", "Best Quality", "My Video"},
		{"GoVid_My Video_1080p.mp4", "1080p", "My Video"},
		{"GoVid_My Video_1080p_TRIM.mkv", "1080p", "My Video"},
		{"GoVid_My Video_TRIM.webm", "", "My Video"},
		{"GoVid_Talk_720p.mp3", "1080p", "Talk_720p"},
		{"No Prefix.mp4", "Best Quality", "No Prefix"},
		{"GoVid_Dots.In.Title.mp4", "", "Dots.In.Title"},
	}

	for _, tt := range tests {
		if got := inferOriginalTitle(tt.filename, tt.quality); got != tt.want {
			t.Errorf("inferOriginalTitle(%q, %q) = %q, want %q", tt.filename, tt.quality, got, tt.want)
		}
	}
}
