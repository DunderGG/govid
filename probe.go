// probe.go — Asking yt-dlp what a URL points at before downloading it.
//
// Responsibilities:
//   - DownloadEngine.Probe: runs "yt-dlp -J --flat-playlist" for one URL and
//     parses the JSON into a MediaInfo. --flat-playlist keeps playlists fast
//     (their entries are listed, not extracted), while a single video is
//     still extracted in full.
//   - MediaInfo / PlaylistEntry: the parts of yt-dlp's info JSON GoVid uses.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// MediaInfo is what yt-dlp reports about a URL before downloading it.
type MediaInfo struct {
	Type     string          `json:"_type"` // "playlist" for playlists; "video" or "" for a single video
	Title    string          `json:"title"`
	Duration float64         `json:"duration"` // seconds; 0 when unknown
	Entries  []PlaylistEntry `json:"entries"`  // playlist items, in playlist order
}

// IsPlaylist reports whether the URL points at a playlist.
func (info MediaInfo) IsPlaylist() bool {
	return info.Type == "playlist"
}

// PlaylistEntry is one item of a playlist, as --flat-playlist lists it.
type PlaylistEntry struct {
	URL        string  `json:"url"`
	WebpageURL string  `json:"webpage_url"`
	Title      string  `json:"title"`
	Duration   float64 `json:"duration"` // seconds; 0 when unknown
}

// DownloadURL returns the URL to download the entry from, or "" when the
// playlist did not list one.
func (entry PlaylistEntry) DownloadURL() string {
	for _, url := range []string{entry.URL, entry.WebpageURL} {
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			return url
		}
	}
	return ""
}

// Probe asks yt-dlp what req.URL points at, without downloading anything.
// It passes the same format selection and cookies as the download, so a
// single video's info describes the formats the download would fetch.
func (engine *DownloadEngine) Probe(ctx context.Context, req DownloadRequest) (MediaInfo, error) {
	cmd := newToolCommand(ctx, engine.YtDlpPath, probeArgs(req)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if reason := lastLine(stderr.String()); reason != "" {
			return MediaInfo{}, fmt.Errorf("%w: %s", err, reason)
		}
		return MediaInfo{}, err
	}

	var info MediaInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return MediaInfo{}, fmt.Errorf("reading yt-dlp's answer: %w", err)
	}
	return info, nil
}

// probeArgs builds the yt-dlp arguments Probe runs with. Unlike the
// download, it does not pass --no-playlist, so a playlist is reported as
// one.
func probeArgs(req DownloadRequest) []string {
	formatFlag, _, _ := formatSelection(req.Format, req.Quality)
	args := []string{"-J", "--flat-playlist", "--no-warnings", "-f", formatFlag}
	if req.CookiesPath != "" {
		if _, err := os.Stat(req.CookiesPath); err == nil {
			args = append(args, "--cookies", req.CookiesPath)
		}
	}
	return append(args, req.URL)
}
