// playlist.go — Checking each URL before a session downloads it.
//
// Responsibilities:
//   - checkURLs: probes every URL of a session (DownloadEngine.Probe) before
//     anything is downloaded. A playlist is shown to the user through
//     askPlaylist, and the videos they choose replace it in the queue as
//     separate items, so each gets its own progress row, cancel, retry,
//     history entry, and duplicate-name handling.
//   - checkItem: probes a queued video that has no fresh probe answer (a
//     playlist entry, or one whose answer is too old) right before it is
//     downloaded.
//   - parsePlaylistSelection, playlistDuration, namesSingleVideo: the pure
//     helpers behind the playlist prompt (see playlist_dialog.go).
package main

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// queueItem is one URL in a session's download queue.
type queueItem struct {
	url  string
	info *MediaInfo // what the probe reported about this single video; nil when unknown
	// probeFailed is set when the URL could not be probed. Its download then
	// goes ahead from the URL, without probing it again first.
	probeFailed bool

	// What the probe or the playlist says the video is, for its history
	// entry and the duplicate check; "" when unknown.
	title     string
	videoID   string
	extractor string
}

// withInfo returns the item with the probe's answer about it.
func (item queueItem) withInfo(info *MediaInfo) queueItem {
	item.info = info
	item.probeFailed = false
	if title := strings.TrimSpace(info.Title); title != "" {
		item.title = title
	}
	if info.ID != "" {
		item.videoID, item.extractor = info.ID, info.ExtractorKey
	}
	return item
}

// displayName is how the item is named to the user: its title, or its URL.
func (item queueItem) displayName() string {
	if item.title != "" {
		return item.title
	}
	return item.url
}

// needsProbe reports whether the item must be probed (again) before it is
// downloaded at now: a playlist entry has not been probed yet, and an answer
// older than probeMaxAge holds format URLs that may have expired.
func (item queueItem) needsProbe(now time.Time) bool {
	if item.info == nil {
		return !item.probeFailed
	}
	return !item.info.isFresh(now)
}

// playlistPrompt is what the playlist prompt shows.
type playlistPrompt struct {
	url      string
	title    string
	entries  []PlaylistEntry
	hasVideo bool // the URL also names one video of the playlist (watch?v=…&list=…)
}

// playlistDecision is the user's answer to the playlist prompt. With neither
// positions nor onlyVideo set, the URL is skipped.
type playlistDecision struct {
	positions []int // 1-based playlist positions to download, in order
	onlyVideo bool  // download only the video the URL names
}

// checkURLs probes each of the session's URLs and returns the download
// queue: single videos as they are, and each playlist replaced by the videos
// the user chose. A URL that cannot be probed is queued as it is, so the
// download reports the problem. Playlist entries are probed later, just
// before each is downloaded (see checkItem). It returns nil when ctx is
// cancelled.
func (app *DownloaderApp) checkURLs(ctx context.Context, session downloadSession) []queueItem {
	engine := app.newDownloadEngine()
	var items []queueItem
	for i, rawURL := range session.urls {
		if len(session.urls) > 1 {
			app.updateStatus(fmt.Sprintf("Status: Checking URL %d of %d…", i+1, len(session.urls)))
		} else {
			app.updateStatus("Status: Checking URL…")
		}

		info, err := engine.Probe(ctx, app.newDownloadRequest(rawURL, session.savePath, session.trimStart, session.trimEnd))
		switch {
		case ctx.Err() != nil:
			return nil
		case err != nil:
			app.appendOutput(fmt.Sprintf("[SYSTEM] Could not check %s (%v); downloading it as a single video.", rawURL, err), colWarning)
			items = append(items, queueItem{url: rawURL, probeFailed: true})
		case info.IsPlaylist():
			items = append(items, app.expandPlaylist(ctx, rawURL, info)...)
		default:
			items = append(items, queueItem{url: rawURL}.withInfo(&info))
		}
	}
	return items
}

// expandPlaylist asks the user which videos of a playlist to download and
// returns them as queue items.
func (app *DownloaderApp) expandPlaylist(ctx context.Context, rawURL string, info MediaInfo) []queueItem {
	title := playlistTitle(info)
	decision := app.askPlaylist(ctx, playlistPrompt{
		url:      rawURL,
		title:    title,
		entries:  info.Entries,
		hasVideo: namesSingleVideo(rawURL),
	})

	switch {
	case decision.onlyVideo:
		app.appendOutput(fmt.Sprintf("[SYSTEM] Playlist %q: downloading only the linked video.", title), colInfo)
		return []queueItem{{url: rawURL}}
	case len(decision.positions) == 0:
		app.appendOutput(fmt.Sprintf("[SYSTEM] Playlist %q skipped.", title), colInfo)
		return nil
	}

	var items []queueItem
	missing := 0
	for _, position := range decision.positions {
		entry := info.Entries[position-1]
		entryURL := entry.DownloadURL()
		if entryURL == "" {
			missing++
			continue
		}
		items = append(items, queueItem{url: entryURL, title: strings.TrimSpace(entry.Title), videoID: entry.ID, extractor: entry.IEKey})
	}
	app.appendOutput(fmt.Sprintf("[SYSTEM] Playlist %q: queued %d of %d videos.", title, len(items), len(info.Entries)), colInfo)
	if missing > 0 {
		app.appendOutput(fmt.Sprintf("[SYSTEM] Playlist %q: %d chosen videos have no URL and were skipped.", title, missing), colWarning)
	}
	return items
}

// checkItem returns item, first probing it when needsProbe says so. The
// download then has its
// size, title, and the info JSON it loads instead of extracting the video
// again. A probe that fails is logged, and the item is downloaded from its
// URL without a size check. ctx is the item's own context, so Cancel stops
// the probe too; the item is returned unchanged when ctx is cancelled.
// position and total place the item in the queue, for the status label.
func (app *DownloaderApp) checkItem(ctx context.Context, session downloadSession, item queueItem, position, total int) queueItem {
	if !item.needsProbe(time.Now()) {
		return item
	}
	if item.info != nil {
		app.appendOutput(fmt.Sprintf("[SYSTEM] The info for %s is over %v old; checking it again.", item.url, probeMaxAge), colSystem)
	}
	if total > 1 {
		app.updateStatus(fmt.Sprintf("Status: Checking URL %d of %d…", position, total))
	} else {
		app.updateStatus("Status: Checking URL…")
	}

	req := app.newDownloadRequest(item.url, session.savePath, session.trimStart, session.trimEnd)
	info, err := app.newDownloadEngine().ProbeVideo(ctx, req)
	switch {
	case ctx.Err() != nil:
		return item
	case err != nil:
		app.appendOutput(fmt.Sprintf("[SYSTEM] Could not check %s (%v); downloading it without a size check.", item.url, err), colWarning)
		item.info, item.probeFailed = nil, true
	case info.IsPlaylist():
		// --no-playlist should rule this out; download the URL as it is.
		item.info, item.probeFailed = nil, true
	default:
		item = item.withInfo(&info)
	}
	return item
}

// playlistTitle returns the playlist's title, or a placeholder.
func playlistTitle(info MediaInfo) string {
	if title := strings.TrimSpace(info.Title); title != "" {
		return title
	}
	return "Untitled playlist"
}

// namesSingleVideo reports whether a playlist URL also names one of its
// videos, as YouTube's watch?v=…&list=… links do. For those, "only this
// video" is offered and is the default.
func namesSingleVideo(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	switch {
	case host == "youtu.be":
		return strings.Trim(parsed.Path, "/") != ""
	case strings.HasSuffix(host, "youtube.com"):
		return parsed.Query().Get("v") != ""
	default:
		return false
	}
}

// parsePlaylistSelection parses the playlist prompt's range field into
// 1-based playlist positions, in the order given and without repeats. The
// field takes comma-separated positions and ranges, e.g. "1-10" or "3,5,8";
// "5-" runs to the end of the playlist, and a blank field selects every
// video. Each position must be between 1 and count.
func parsePlaylistSelection(text string, count int) ([]int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		positions := make([]int, count)
		for i := range positions {
			positions[i] = i + 1
		}
		return positions, nil
	}

	var positions []int
	for _, part := range strings.Split(text, ",") {
		first, last, err := parsePlaylistRange(strings.TrimSpace(part), count)
		if err != nil {
			return nil, err
		}
		for position := first; position <= last; position++ {
			if !slices.Contains(positions, position) {
				positions = append(positions, position)
			}
		}
	}
	return positions, nil
}

// parsePlaylistRange parses one part of the range field: "7", "3-9", or
// "5-" (to the end).
func parsePlaylistRange(part string, count int) (first, last int, err error) {
	if part == "" {
		return 0, 0, fmt.Errorf("empty entry in the list")
	}
	from, to, isRange := strings.Cut(part, "-")
	first, err = parsePlaylistPosition(from, count)
	if err != nil {
		return 0, 0, err
	}
	if !isRange {
		return first, first, nil
	}
	last = count
	if strings.TrimSpace(to) != "" {
		if last, err = parsePlaylistPosition(to, count); err != nil {
			return 0, 0, err
		}
	}
	if last < first {
		return 0, 0, fmt.Errorf("%q counts backwards", part)
	}
	return first, last, nil
}

// parsePlaylistPosition parses one playlist position, between 1 and count.
func parsePlaylistPosition(text string, count int) (int, error) {
	position, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", strings.TrimSpace(text))
	}
	if position < 1 || position > count {
		return 0, fmt.Errorf("%d is outside the playlist (1-%d)", position, count)
	}
	return position, nil
}

// playlistDuration describes the total length of entries, e.g. "1h 23m",
// noting how many entries have no length listed. It returns "unknown" when
// none has one.
func playlistDuration(entries []PlaylistEntry) string {
	var total float64
	unknown := 0
	for _, entry := range entries {
		if entry.Duration > 0 {
			total += entry.Duration
		} else {
			unknown++
		}
	}
	switch {
	case unknown == len(entries):
		return "unknown"
	case unknown > 0:
		return fmt.Sprintf("at least %s (%d without a listed length)", formatPlaylistLength(total), unknown)
	default:
		return formatPlaylistLength(total)
	}
}

// formatPlaylistLength formats a length in seconds as "1h 23m", "12m 5s", or "45s".
func formatPlaylistLength(seconds float64) string {
	length := time.Duration(seconds) * time.Second
	hours := int(length.Hours())
	minutes := int(length.Minutes()) % 60
	secs := int(length.Seconds()) % 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}
