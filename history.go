package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const historyFileName = "download_history.json"

type DownloadHistoryEntry struct {
	URL             string `json:"url"`
	OriginalTitle   string `json:"originalTitle"`
	FinalFilename   string `json:"finalFilename"`
	SavedPath       string `json:"savedPath"`
	Format          string `json:"format"`
	Quality         string `json:"quality"`
	DownloadedAt    string `json:"downloadedAt"`
	PostProcessed   bool   `json:"postProcessed"`
}

func historyFilePath() string {
	exePath, err := os.Executable()
	if err != nil {
		return historyFileName
	}
	return filepath.Join(filepath.Dir(exePath), historyFileName)
}

func loadDownloadHistory() ([]DownloadHistoryEntry, error) {
	path := historyFilePath()
	data, err := os.ReadFile(path)
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

func appendDownloadHistory(entry DownloadHistoryEntry) error {
	entries, err := loadDownloadHistory()
	if err != nil {
		return err
	}
	if entry.DownloadedAt == "" {
		entry.DownloadedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	entries = append(entries, entry)

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(historyFilePath(), data, 0644)
}

func buildDownloadHistoryEntries(url string, finalPaths []string, savePath, format, quality string, postProcessed bool) []DownloadHistoryEntry {
	if len(finalPaths) == 0 {
		return []DownloadHistoryEntry{{
			URL:           url,
			OriginalTitle: "",
			FinalFilename: "",
			SavedPath:     savePath,
			Format:        format,
			Quality:       quality,
			PostProcessed: postProcessed,
		}}
	}

	entries := make([]DownloadHistoryEntry, 0, len(finalPaths))
	for _, finalPath := range finalPaths {
		base := filepath.Base(finalPath)
		entries = append(entries, DownloadHistoryEntry{
			URL:           url,
			OriginalTitle: inferOriginalTitle(base, quality),
			FinalFilename: base,
			SavedPath:     filepath.Dir(finalPath),
			Format:        format,
			Quality:       quality,
			PostProcessed: postProcessed,
		})
	}
	return entries
}

func inferOriginalTitle(filename, quality string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	base = strings.TrimPrefix(base, "GoVid_")
	base = strings.TrimSuffix(base, "_TRIM")
	if quality != "" && quality != "Best Quality" {
		base = strings.TrimSuffix(base, "_"+quality)
	}
	return base
}
