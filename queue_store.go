// queue_store.go — Keeping the download queue when GoVid quits.
//
// Responsibilities:
//   - QueueStore: queue.json beside the executable (next to the download
//     history). When GoVid quits with items waiting or paused, they are
//     saved there, and the next start offers to resume them.
//   - savedQueueItem / savedRequest: what is kept of each item: its URL,
//     title, video ID, download ID (so yt-dlp continues its partial files),
//     status, and the settings it downloads with. The probe's JSON is not
//     kept: its format URLs expire, so a resumed item is probed again.
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// queueFileName is the file the queue is saved to.
const queueFileName = "queue.json"

// The statuses an item is saved with.
const (
	savedWaiting = "waiting"
	savedPaused  = "paused"
)

// savedRequest is the part of a DownloadRequest an item is saved with.
// Cookies are not saved: the cookie settings in use when it resumes apply.
type savedRequest struct {
	SavePath       string `json:"savePath"`
	Format         string `json:"format"`
	Quality        string `json:"quality"`
	TrimStart      string `json:"trimStart,omitempty"`
	TrimEnd        string `json:"trimEnd,omitempty"`
	MaxSpeed       string `json:"maxSpeed,omitempty"`
	EmbedMetadata  bool   `json:"embedMetadata"`
	EmbedThumbnail bool   `json:"embedThumbnail"`
	EmbedChapters  bool   `json:"embedChapters"`
	Subtitles      string `json:"subtitles,omitempty"`
	SubtitleLangs  string `json:"subtitleLangs,omitempty"`
	AutoSubtitles  bool   `json:"autoSubtitles"`
}

// savedQueueItem is one queue item in queue.json.
type savedQueueItem struct {
	URL        string       `json:"url"`
	Title      string       `json:"title,omitempty"`
	VideoID    string       `json:"videoId,omitempty"`
	Extractor  string       `json:"extractor,omitempty"`
	DownloadID string       `json:"downloadId"`
	FormatID   string       `json:"formatId,omitempty"`
	Status     string       `json:"status"`
	Request    savedRequest `json:"request"`
}

// savedQueue is queue.json.
type savedQueue struct {
	Items []savedQueueItem `json:"items"`
}

// saveItem returns item as it is saved, with status savedWaiting or
// savedPaused.
func saveItem(item queueItem, status string) savedQueueItem {
	saved := savedQueueItem{
		URL: item.url, Title: item.title, VideoID: item.videoID, Extractor: item.extractor,
		DownloadID: item.downloadID, FormatID: item.formatID, Status: status,
	}
	if req := item.request; req != nil {
		saved.Request = savedRequest{
			SavePath: req.SavePath, Format: req.Format, Quality: req.Quality,
			TrimStart: req.TrimStart, TrimEnd: req.TrimEnd, MaxSpeed: req.MaxSpeed,
			EmbedMetadata: req.EmbedMetadata, EmbedThumbnail: req.EmbedThumbnail, EmbedChapters: req.EmbedChapters,
			Subtitles: req.Subtitles, SubtitleLangs: req.SubtitleLangs, AutoSubtitles: req.AutoSubtitles,
		}
	}
	return saved
}

// queueItem returns the saved item as a queue item, with its settings and
// download ID but no probe answer, so it is probed again before it
// downloads.
func (saved savedQueueItem) queueItem() queueItem {
	settings := saved.Request
	return queueItem{
		url: saved.URL, title: saved.Title, videoID: saved.VideoID, extractor: saved.Extractor,
		downloadID: saved.DownloadID, formatID: saved.FormatID,
		request: &DownloadRequest{
			SavePath: settings.SavePath, Format: settings.Format, Quality: settings.Quality,
			TrimStart: settings.TrimStart, TrimEnd: settings.TrimEnd, MaxSpeed: settings.MaxSpeed,
			EmbedMetadata: settings.EmbedMetadata, EmbedThumbnail: settings.EmbedThumbnail, EmbedChapters: settings.EmbedChapters,
			Subtitles: settings.Subtitles, SubtitleLangs: settings.SubtitleLangs, AutoSubtitles: settings.AutoSubtitles,
		},
	}
}

// QueueStore reads and writes queue.json. It has no UI dependency.
type QueueStore struct {
	filePath string
}

// NewQueueStore returns a QueueStore for queue.json beside the running
// executable.
func NewQueueStore() *QueueStore {
	path := queueFileName
	if exePath, err := os.Executable(); err == nil {
		path = filepath.Join(filepath.Dir(exePath), queueFileName)
	}
	return &QueueStore{filePath: path}
}

// Load returns the saved items, or none when nothing is saved.
func (store *QueueStore) Load() ([]savedQueueItem, error) {
	data, err := os.ReadFile(store.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var queue savedQueue
	if err := json.Unmarshal(data, &queue); err != nil {
		return nil, err
	}
	return queue.Items, nil
}

// Save replaces the saved items with items; with none, it deletes the file.
func (store *QueueStore) Save(items []savedQueueItem) error {
	if len(items) == 0 {
		return store.Clear()
	}
	data, err := json.MarshalIndent(savedQueue{Items: items}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(store.filePath, append(data, '\n'))
}

// Clear deletes the saved items.
func (store *QueueStore) Clear() error {
	if err := os.Remove(store.filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
