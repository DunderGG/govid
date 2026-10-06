// history_service.go — Owns download history persistence.
//
// Responsibilities:
//   - HistoryService: typed service that owns the history file path and
//     exposes Load, AppendAll, and Clear operations.
//   - findDownloaded: finds an earlier download of the same video, for the
//     duplicate warning.
//   - DownloadHistoryEntry: the JSON record type written once per output file.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

	// The site's ID for the video and yt-dlp's extractor key (e.g.
	// "Youtube"), from the probe. Together they recognise the same video
	// under another URL. Entries written before they were recorded have
	// neither.
	VideoID   string `json:"videoId,omitempty"`
	Extractor string `json:"extractor,omitempty"`
}

// FilePath returns the path of the entry's file, or "" when it has none.
func (entry DownloadHistoryEntry) FilePath() string {
	if entry.FinalFilename == "" {
		return ""
	}
	return filepath.Join(entry.SavedPath, entry.FinalFilename)
}

// DisplayFile returns the entry's file name, or a placeholder when it was
// recorded without one.
func (entry DownloadHistoryEntry) DisplayFile() string {
	if entry.FinalFilename == "" {
		return "(file name not recorded)"
	}
	return entry.FinalFilename
}

// DisplayTitle returns the entry's title, falling back to its file name and
// then its URL.
func (entry DownloadHistoryEntry) DisplayTitle() string {
	for _, title := range []string{entry.OriginalTitle, entry.FinalFilename} {
		if strings.TrimSpace(title) != "" {
			return title
		}
	}
	return entry.URL
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

	// Title is the source's real title, from the probe or the playlist; ""
	// when unknown, in which case it is guessed from the file name.
	Title     string
	VideoID   string
	Extractor string
}

// findDownloaded returns the newest entry for the same video as the given
// URL, video ID, and extractor key. A video is the same when both sides have
// an ID and the IDs and extractors match (so youtu.be/x and watch?v=x are
// one video), or when the URLs are identical (for entries recorded without
// an ID, or a source that could not be probed).
func findDownloaded(entries []DownloadHistoryEntry, url, videoID, extractor string) (DownloadHistoryEntry, bool) {
	for _, entry := range slices.Backward(entries) {
		sameID := videoID != "" && entry.VideoID == videoID && strings.EqualFold(entry.Extractor, extractor)
		if sameID || entry.URL == url {
			return entry, true
		}
	}
	return DownloadHistoryEntry{}, false
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
			OriginalTitle: rec.Title,
			SavedPath:     rec.SavePath,
			Format:        rec.Format,
			Quality:       rec.Quality,
			DownloadedAt:  timestamp,
			PostProcessed: rec.PostProcessed,
			VideoID:       rec.VideoID,
			Extractor:     rec.Extractor,
		}}
	}
	result := make([]DownloadHistoryEntry, 0, len(rec.FinalPaths))
	for _, p := range rec.FinalPaths {
		base := filepath.Base(p)
		title := rec.Title
		if title == "" {
			title = inferOriginalTitle(base, rec.Quality)
		}
		result = append(result, DownloadHistoryEntry{
			URL:           rec.URL,
			OriginalTitle: title,
			FinalFilename: base,
			SavedPath:     filepath.Dir(p),
			Format:        rec.Format,
			Quality:       rec.Quality,
			DownloadedAt:  timestamp,
			PostProcessed: rec.PostProcessed,
			VideoID:       rec.VideoID,
			Extractor:     rec.Extractor,
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
