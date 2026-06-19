package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const historyFileName = "download_history.json"

type DownloadHistoryEntry struct {
	URL          string `json:"url"`
	DownloadedAt string `json:"downloadedAt"`
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

func appendDownloadHistory(url string) error {
	entries, err := loadDownloadHistory()
	if err != nil {
		return err
	}
	entries = append(entries, DownloadHistoryEntry{
		URL:          url,
		DownloadedAt: time.Now().Format("2006-01-02 15:04:05"),
	})

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(historyFilePath(), data, 0644)
}
