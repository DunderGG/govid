// probe.go — Asking yt-dlp what a URL points at before downloading it.
//
// Responsibilities:
//   - DownloadEngine.Probe: runs "yt-dlp -J --flat-playlist" for one URL and
//     parses the JSON into a MediaInfo. --flat-playlist keeps playlists fast
//     (their entries are listed, not extracted), while a single video is
//     still extracted in full. ProbeVideo does the same with --no-playlist,
//     for a URL known to name one video.
//   - MediaInfo / PlaylistEntry: the parts of yt-dlp's info JSON GoVid uses.
//     A single video's MediaInfo also keeps the JSON itself, which the
//     download hands back to yt-dlp (--load-info-json) so the video is not
//     extracted a second time.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// probeMaxAge is how old a probe's answer may be when its video's turn in
// the queue comes. The format URLs in it expire (on YouTube after about six
// hours), so an older answer is replaced by a fresh probe.
const probeMaxAge = 30 * time.Minute

// MediaInfo is what yt-dlp reports about a URL before downloading it.
type MediaInfo struct {
	Type     string          `json:"_type"` // "playlist" for playlists; "video" or "" for a single video
	Title    string          `json:"title"`
	Duration float64         `json:"duration"` // seconds; 0 when unknown
	Entries  []PlaylistEntry `json:"entries"`  // playlist items, in playlist order

	// The site's ID for the video and yt-dlp's name for the site, which
	// together recognise the same video under different URLs.
	ID           string `json:"id"`
	ExtractorKey string `json:"extractor_key"`

	// LiveStatus is yt-dlp's live_status: "is_live", "is_upcoming" (a
	// scheduled stream or premiere), "post_live", "was_live", or
	// "not_live"; "" when the site does not say. ReleaseTimestamp is when
	// a scheduled stream starts, in Unix seconds; 0 when unknown.
	// FormatID is the format(s) the -f selector picked, e.g. "137+251".
	FormatID string `json:"format_id"`

	LiveStatus       string  `json:"live_status"`
	ReleaseTimestamp float64 `json:"release_timestamp"`

	// Height is the height of the video the -f selector picked (for merged
	// streams, the video stream's); 0 when unknown or audio only.
	Height int `json:"height"`

	// The subtitle languages the site offers, by language code: written by
	// people, and generated automatically. Only the codes are used.
	Subtitles         map[string]json.RawMessage `json:"subtitles"`
	AutomaticCaptions map[string]json.RawMessage `json:"automatic_captions"`

	// The size of the format(s) the -f selector picked: one set of sizes
	// for a single format, or one per stream in RequestedFormats when a
	// video and an audio stream are merged.
	formatSize
	RequestedFormats []FormatInfo `json:"requested_formats"`

	// Formats is every format the site offers, worst first, as "yt-dlp -F"
	// lists them (see formatRows).
	Formats []FormatInfo `json:"formats"`

	// raw is the JSON yt-dlp printed for a single video, and probedAt is
	// when. The download loads raw instead of extracting the video again.
	raw      []byte
	probedAt time.Time
}

// formatSize is the size yt-dlp reports for one format: exact when the site
// says, approximate (from the bitrate) otherwise. yt-dlp may write either as
// a float, hence the float64 fields.
type formatSize struct {
	FileSize       float64 `json:"filesize"`
	FileSizeApprox float64 `json:"filesize_approx"`
}

// bytes returns the format's size, exact if known, and whether it is known.
func (size formatSize) bytes() (float64, bool) {
	switch {
	case size.FileSize > 0:
		return size.FileSize, true
	case size.FileSizeApprox > 0:
		return size.FileSizeApprox, true
	default:
		return 0, false
	}
}

// IsPlaylist reports whether the URL points at a playlist.
func (info MediaInfo) IsPlaylist() bool {
	return info.Type == "playlist"
}

// isFresh reports whether, at now, the probe's answer is recent enough for
// the download to use the format URLs in it (see probeMaxAge).
func (info MediaInfo) isFresh(now time.Time) bool {
	return len(info.raw) > 0 && now.Sub(info.probedAt) < probeMaxAge
}

// EstimatedSize returns the size of the download in bytes, and whether it is
// known: the sum of the requested formats when streams are merged, otherwise
// the selected format's own size. If any merged stream's size is unknown,
// so is the total.
func (info MediaInfo) EstimatedSize() (uint64, bool) {
	if len(info.RequestedFormats) == 0 {
		size, known := info.bytes()
		return uint64(size), known
	}
	var total float64
	for _, format := range info.RequestedFormats {
		size, known := format.bytes()
		if !known {
			return 0, false
		}
		total += size
	}
	return uint64(total), true
}

// PlaylistEntry is one item of a playlist, as --flat-playlist lists it.
type PlaylistEntry struct {
	URL        string  `json:"url"`
	WebpageURL string  `json:"webpage_url"`
	Title      string  `json:"title"`
	Duration   float64 `json:"duration"` // seconds; 0 when unknown
	ID         string  `json:"id"`
	IEKey      string  `json:"ie_key"` // the extractor key a full probe reports as extractor_key
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
	return engine.probe(ctx, engine.probeArgs(req, false))
}

// ProbeVideo is Probe for a URL that names one video, such as a playlist
// entry, or a watch?v=…&list=… link the user chose "Only this video" for. Like
// the download, it passes --no-playlist.
func (engine *DownloadEngine) ProbeVideo(ctx context.Context, req DownloadRequest) (MediaInfo, error) {
	return engine.probe(ctx, engine.probeArgs(req, true))
}

// probe runs yt-dlp with args and parses its answer.
func (engine *DownloadEngine) probe(ctx context.Context, args []string) (MediaInfo, error) {
	cmd := newToolCommand(ctx, engine.YtDlpPath, args...)
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
	if !info.IsPlaylist() {
		info.raw = out
		info.probedAt = time.Now()
	}
	return info, nil
}

// probeArgs builds the yt-dlp arguments a probe runs with. Without
// singleVideo it does not pass --no-playlist, so a playlist is reported as
// one.
func (engine *DownloadEngine) probeArgs(req DownloadRequest, singleVideo bool) []string {
	// A scheduled stream has no formats yet, which yt-dlp would report as
	// an error; --ignore-no-formats-error returns its details (live_status,
	// release_timestamp) instead, so GoVid can offer to wait for it.
	args := append([]string{"-J", "--flat-playlist", "--no-warnings", "--ignore-no-formats-error"}, formatArgs(req)...)
	if singleVideo {
		args = append(args, "--no-playlist")
	}
	args = append(args, engine.jsRuntimeArgs()...)
	args = append(args, cookieArgs(req)...)
	return append(args, req.URL)
}
