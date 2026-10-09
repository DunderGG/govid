package main

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// fixtureInfo reads testdata/ytdlp_info_formats.json: a real YouTube
// video's info, as "yt-dlp -J -f bestvideo+bestaudio/best" gave it on
// 2026-10-07, with the URLs replaced.
func fixtureInfo(t *testing.T) MediaInfo {
	t.Helper()
	data, err := os.ReadFile("testdata/ytdlp_info_formats.json")
	if err != nil {
		t.Fatal(err)
	}
	var info MediaInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	return info
}

// listedFormat is one row of "yt-dlp -F".
type listedFormat struct {
	id, ext, resolution string
	storyboard          bool
}

// ytDlpListing reads testdata/ytdlp_list_formats.log, which is what
// "yt-dlp -F --load-info-json testdata/ytdlp_info_formats.json" printed.
func ytDlpListing(t *testing.T) []listedFormat {
	t.Helper()
	data, err := os.ReadFile("testdata/ytdlp_list_formats.log")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n")
	var formats []listedFormat
	started := false
	for _, line := range lines {
		if strings.HasPrefix(line, "-----") {
			started = true
			continue
		}
		fields := strings.Fields(line)
		if !started || len(fields) < 3 {
			continue
		}
		resolution := fields[2]
		if resolution == "audio" {
			resolution = "audio only"
		}
		formats = append(formats, listedFormat{id: fields[0], ext: fields[1], resolution: resolution, storyboard: strings.HasSuffix(line, "storyboard")})
	}
	return formats
}

func TestFormatRowsListWhatYtDlpLists(t *testing.T) {
	info := fixtureInfo(t)
	rows := formatRows(info, filterAll)

	var want []listedFormat
	for _, format := range ytDlpListing(t) {
		if !format.storyboard {
			want = append(want, format)
		}
	}
	if len(rows) != len(want) || len(want) != 45 {
		t.Fatalf("%d rows, yt-dlp -F lists %d formats besides storyboards (want 45)", len(rows), len(want))
	}
	for i, row := range rows {
		if row.cells[0] != want[i].id || row.cells[7] != want[i].ext || row.cells[1] != want[i].resolution {
			t.Errorf("row %d = %q, yt-dlp lists %+v", i, row.cells, want[i])
		}
	}
}

func TestFormatRowsFilter(t *testing.T) {
	info := fixtureInfo(t)
	counts := map[formatFilter]int{}
	for _, filter := range []formatFilter{filterVideo, filterAudio, filterCombined} {
		counts[filter] = len(formatRows(info, filter))
	}
	if counts[filterVideo] != 37 || counts[filterAudio] != 7 || counts[filterCombined] != 1 {
		t.Errorf("video %d, audio %d, combined %d; want 37, 7, 1", counts[filterVideo], counts[filterAudio], counts[filterCombined])
	}
	vp9, _ := info.findFormat("247")
	row := formatRows(MediaInfo{Formats: []FormatInfo{vp9}, Duration: info.Duration}, filterAll)[0]
	if want := []string{"247", "1280x720", "25", "", "VP9", "", "664k", "webm", "16.9 MiB"}; !slices.Equal(row.cells, want) {
		t.Errorf("row of 247 = %q, want %q", row.cells, want)
	}
}

func TestDescribeDownload(t *testing.T) {
	info := fixtureInfo(t)
	tests := []struct {
		pick, extension string
		wantIDs, want   string
	}{
		{"", "mp4", "401+251", "2160p AV1 + Opus → MP4 (~232.5 MiB)"},
		{"247+251", "mp4", "247+251", "720p VP9 + Opus → MP4 (~20.1 MiB)"},
		{"18", "mkv", "18", "360p H.264 + AAC → MKV (~11.3 MiB)"},
		{"251", "mp3", "251", "Opus → MP3 (~3.3 MiB)"},
		{"999", "mp4", "", ""},
	}
	for _, tt := range tests {
		if got := describeDownload(info, tt.pick, tt.extension); got != tt.want {
			t.Errorf("describeDownload(%q) = %q, want %q", tt.pick, got, tt.want)
		}
		if got := selectedIDs(info, tt.pick); got != tt.wantIDs {
			t.Errorf("selectedIDs(%q) = %q, want %q", tt.pick, got, tt.wantIDs)
		}
	}
}

func TestFormatChoice(t *testing.T) {
	info := fixtureInfo(t)
	format := func(id string) FormatInfo {
		f, _ := info.findFormat(id)
		return f
	}
	choice := choiceFor(info, "")
	if choice.pick() != "401+251" {
		t.Errorf("initial choice = %q, want the probe's 401+251", choice.pick())
	}
	choice.choose(format("247"))
	if choice.pick() != "247+251" {
		t.Errorf("after a video row: %q", choice.pick())
	}
	choice.choose(format("18"))
	if choice.pick() != "18" {
		t.Errorf("after a combined row: %q", choice.pick())
	}
	choice.choose(format("140"))
	if choice.pick() != "140" || choice.chosen(format("18")) {
		t.Errorf("an audio row after a combined one: %q", choice.pick())
	}
	choice.choose(format("137"))
	if choice.pick() != "137+140" {
		t.Errorf("then a video row: %q", choice.pick())
	}
}

func TestFormatArgs(t *testing.T) {
	tests := []struct {
		req  DownloadRequest
		want []string
	}{
		{DownloadRequest{Format: formatMP4, Quality: qualityBest}, []string{"-f", "bestvideo+bestaudio/best"}},
		{DownloadRequest{Format: formatMP4, Quality: qualityBest, PreferredCodec: codecH264}, []string{"-f", "bestvideo+bestaudio/best", "-S", "vcodec:avc"}},
		{DownloadRequest{Format: formatMKV, Quality: quality720p, PreferredCodec: codecVP9}, []string{"-f", "bestvideo[height<=720]+bestaudio/best[height<=720]/best", "-S", "vcodec:vp9"}},
		{DownloadRequest{Format: formatMP3, PreferredCodec: codecAV1}, []string{"-f", "bestaudio/best"}},
		{DownloadRequest{Format: formatMP4, FormatPick: "247+251", PreferredCodec: codecH264}, []string{"-f", "247+251"}},
	}
	for _, tt := range tests {
		if got := formatArgs(tt.req); !slices.Equal(got, tt.want) {
			t.Errorf("formatArgs(%+v) = %q, want %q", tt.req, got, tt.want)
		}
	}
	// The download's container still follows Format with a pick.
	built := NewDownloadEngine("yt-dlp", "").BuildArgs(DownloadRequest{URL: "u", SavePath: "s", Format: formatMKV, FormatPick: "247+251"})
	if argAfter(built.Args, "-f") != "247+251" || argAfter(built.Args, "--merge-output-format") != "mkv" {
		t.Errorf("args = %q", built.Args)
	}
}

func TestFormatWindowPicksAVideoAndAnAudioRow(t *testing.T) {
	_ = test.NewApp()
	window := test.NewWindow(nil)
	window.Resize(fyne.NewSize(900, 700))
	manager := NewUIManager(window)
	info := fixtureInfo(t)
	var picked []string

	manager.showFormatWindow(info, "", "mp4", func(pick string) { picked = append(picked, pick) })

	overlay := window.Canvas().Overlays().Top()
	var list *widget.List
	var use *widget.Button
	walkObjects(overlay, func(obj fyne.CanvasObject) {
		switch o := obj.(type) {
		case *widget.List:
			list = o
		case *widget.Button:
			if o.Text == "Use these formats" {
				use = o
			}
		}
	})
	if list == nil || use == nil {
		t.Fatal("the window has no format list or Use button")
	}
	rows := formatRows(info, filterAll)
	for i, row := range rows {
		if row.format.ID == "247" || row.format.ID == "251" {
			list.Select(i)
		}
	}
	test.Tap(use)

	if !slices.Equal(picked, []string{"247+251"}) {
		t.Errorf("picked %q, want 247+251", picked)
	}
}

func TestPickedFormatsReachYtDlp(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-formats")
	runs := useFakeArgs(t)
	url := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	h.app.ui.download.entry.SetText(url)
	h.app.formatPicks.set(url, "247+251")

	h.startAndWait(t)

	for _, args := range runs() {
		if got := argAfter(args, "-f"); got != "247+251" {
			t.Errorf("-f = %q in %q, want 247+251", got, args)
		}
	}
	logs := h.joinedLogs()
	if !strings.Contains(logs, "Will download 247+251: 720p VP9 + Opus → MP4 (~20.1 MiB)") {
		t.Errorf("no Will download line for the pick:\n%s", logs)
	}
	if h.app.formatPicks.get(url) != "" {
		t.Error("the pick was kept after the download queued it")
	}
}

func TestWillDownloadMatchesWhatYtDlpDownloads(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-formats")
	h.app.ui.download.entry.SetText("https://www.youtube.com/watch?v=dQw4w9WgXcQ")

	h.startAndWait(t)

	logs := h.joinedLogs()
	will := regexp.MustCompile(`Will download (\S+): `).FindStringSubmatch(logs)
	got := regexp.MustCompile(`Downloading 1 format\(s\): (\S+)`).FindStringSubmatch(logs)
	if will == nil || got == nil || will[1] != got[1] {
		t.Errorf("Will download %v, yt-dlp downloaded %v:\n%s", will, got, logs)
	}
}

// probeFormatsAndWait runs probeFormatsForURL on the URL field's URL and
// returns the status label right after it starts and once it has shown the
// formats.
func probeFormatsAndWait(t *testing.T, h *downloadHarness) (during, after string) {
	t.Helper()
	url := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	req := h.app.newDownloadRequest(url, h.saveDir, "", "")

	shown := h.app.probeFormatsForURL(url, req)
	h.app.statusThrottle.Flush()
	during = h.app.ui.download.status.Text
	select {
	case <-shown:
	case <-time.After(30 * time.Second):
		t.Fatal("the formats were not shown")
	}
	h.app.statusThrottle.Flush()
	return during, h.app.ui.download.status.Text
}

func TestFormatsShowsItsStatusOutsideASession(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-formats")

	during, after := probeFormatsAndWait(t, h)

	if during != checkingFormatsStatus || after != "Status: Idle" {
		t.Errorf("status = %q during the probe and %q after, want %q and Status: Idle", during, after, checkingFormatsStatus)
	}
}

func TestFormatsKeepsARunningSessionsStatus(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-formats")
	const sessionStatus = "Status: Downloading 1 of 2…"
	h.app.isRunning.Store(true)
	h.app.updateStatus(sessionStatus)
	h.app.statusThrottle.Flush()

	during, after := probeFormatsAndWait(t, h)

	if during != sessionStatus || after != sessionStatus {
		t.Errorf("status = %q during the probe and %q after, want the session's %q", during, after, sessionStatus)
	}
}
