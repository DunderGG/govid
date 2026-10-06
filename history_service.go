// history_service.go — Owns download history persistence.
//
// Responsibilities:
//   - HistoryService: typed service that owns the history file path and
//     exposes Load, AppendAll, and Clear operations.
//   - DownloadHistoryEntry: the JSON record type written once per output file.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const historyFileName = "download_history.json"

// DownloadHistoryEntry is a single record persisted to download_history.json.
type DownloadHistoryEntry struct {
	URL           string `json:"url"`
	OriginalTitle string `json:"originalTitle"`
	FinalFilename string `json:"finalFilename"`
	SavedPath     string `json:"savedPath"`
	Format        string `json:"format"`
	Quality       string `json:"quality"`
	DownloadedAt  string `json:"downloadedAt"`
	PostProcessed bool   `json:"postProcessed"`
}

// DownloadRecord carries the inputs needed to record a completed download.
// It is passed to HistoryService.AppendAll so callers do not need to supply
// a long positional argument list.
type DownloadRecord struct {
	URL           string
	FinalPaths    []string
	SavePath      string
	Format        string
	Quality       string
	PostProcessed bool
}

// HistoryService owns the download history file path and exposes Load,
// AppendAll, and Clear. It has no UI dependency.
type HistoryService struct {
	filePath string
}

// NewHistoryService returns a HistoryService that persists to
// download_history.json beside the running executable.
func NewHistoryService() *HistoryService {
	path := historyFileName
	if exePath, err := os.Executable(); err == nil {
		path = filepath.Join(filepath.Dir(exePath), historyFileName)
	}
	return &HistoryService{filePath: path}
}

// Load reads all entries from disk in chronological order.
// Returns nil with no error if the file does not yet exist.
func (svc *HistoryService) Load() ([]DownloadHistoryEntry, error) {
	data, err := os.ReadFile(svc.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var entries []DownloadHistoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// AppendAll builds one DownloadHistoryEntry per path in rec.FinalPaths and
// appends them all to the history file in a single atomic write. When
// FinalPaths is empty a placeholder entry is written so the URL is still recorded.
func (svc *HistoryService) AppendAll(rec DownloadRecord) error {
	entries, err := svc.Load()
	if err != nil {
		return err
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	entries = append(entries, svc.buildEntries(rec, now)...)
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(svc.filePath, data)
}

// Clear overwrites the history file with an empty JSON array,
// effectively removing all recorded entries.
func (svc *HistoryService) Clear() error {
	return writeFileAtomic(svc.filePath, []byte("[]"))
}

// writeFileAtomic replaces path with data so that a crash or power loss
// leaves either the old or the new contents, never a truncated file. It
// writes a temp file in the same directory, flushes it to disk, and renames
// it over path.
func writeFileAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// buildEntries constructs one DownloadHistoryEntry per path in rec.FinalPaths,
// or a single placeholder entry when FinalPaths is empty.
func (svc *HistoryService) buildEntries(rec DownloadRecord, timestamp string) []DownloadHistoryEntry {
	if len(rec.FinalPaths) == 0 {
		return []DownloadHistoryEntry{{
			URL:           rec.URL,
			SavedPath:     rec.SavePath,
			Format:        rec.Format,
			Quality:       rec.Quality,
			DownloadedAt:  timestamp,
			PostProcessed: rec.PostProcessed,
		}}
	}
	result := make([]DownloadHistoryEntry, 0, len(rec.FinalPaths))
	for _, p := range rec.FinalPaths {
		base := filepath.Base(p)
		result = append(result, DownloadHistoryEntry{
			URL:           rec.URL,
			OriginalTitle: inferOriginalTitle(base, rec.Quality),
			FinalFilename: base,
			SavedPath:     filepath.Dir(p),
			Format:        rec.Format,
			Quality:       rec.Quality,
			DownloadedAt:  timestamp,
			PostProcessed: rec.PostProcessed,
		})
	}
	return result
}

// heightSuffixPattern matches the height label at the end of a capped
// video's name, e.g. "_720p" (see heightLabel).
var heightSuffixPattern = regexp.MustCompile(`_\d+p$`)

// inferOriginalTitle derives a human-readable title from a saved filename by
// stripping the GoVid_ prefix, height label, and file extension. Only a
// capped video download has a height label, which names the height
// downloaded rather than the quality chosen.
func inferOriginalTitle(filename, quality string) string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	base = strings.TrimPrefix(base, "GoVid_")
	base = strings.TrimSuffix(base, "_TRIM")
	if quality != "" && quality != qualityBest && !isAudioOnlyExt(ext) {
		base = heightSuffixPattern.ReplaceAllString(base, "")
	}
	return base
}
