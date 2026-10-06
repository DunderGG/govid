package main

import (
	"context"
	"image/color"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

// newUpdateCheckApp returns an app whose yt-dlp is the fake tool reporting
// fakeToolVersion, and whose release service asks a fake GitHub that says
// latestTag is the newest yt-dlp. It also returns a function listing the
// lines shown in the log view.
func newUpdateCheckApp(t *testing.T, latestTag string) (*DownloaderApp, func() []string) {
	t.Helper()
	_ = test.NewApp()
	app := newDownloaderApp(test.NewWindow(nil))
	app.uiManager.createUI()

	binDir := t.TempDir()
	installFakeTool(t, binDir, "yt-dlp")
	useFakeTool(t, "version")
	app.depSvc = &DependencyService{binDir: binDir}

	gh := newFakeGitHub(t, http.StatusOK, `{"tag_name": "`+latestTag+`"}`)
	now := time.Unix(1_800_000_000, 0)
	app.releaseSvc = newTestReleaseService(gh, &now)

	var mu sync.Mutex
	var shown []string
	app.onLogLine = func(line string, _ color.Color) {
		mu.Lock()
		defer mu.Unlock()
		shown = append(shown, line)
	}
	return app, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(shown)
	}
}

// noticeIDs lists the ids of the notices the app shows.
func noticeIDs(app *DownloaderApp) []string {
	var ids []string
	for _, n := range app.uiManager.notices {
		ids = append(ids, n.id)
	}
	return ids
}

func TestCheckYtDlpUpdateShowsNoticeWhenOutdated(t *testing.T) {
	app, shown := newUpdateCheckApp(t, "2026.10.01")

	app.checkYtDlpUpdate(context.Background())

	if ids := noticeIDs(app); !slices.Equal(ids, []string{ytDlpNoticeID}) {
		t.Fatalf("notices = %q, want the yt-dlp update notice", ids)
	}
	n := app.uiManager.notices[0]
	if n.actionLabel != "Update now" || !strings.Contains(n.text, fakeToolVersion) || !strings.Contains(n.text, "2026.10.01") {
		t.Errorf("notice = %+v, want both versions and an Update now button", n)
	}
	want := "[SYSTEM] yt-dlp " + fakeToolVersion + " is out of date; the latest version is 2026.10.01."
	if !slices.Contains(shown(), want) {
		t.Errorf("log = %q, want %q", shown(), want)
	}
}

func TestCheckYtDlpUpdateQuietWhenCurrent(t *testing.T) {
	app, shown := newUpdateCheckApp(t, fakeToolVersion)

	app.checkYtDlpUpdate(context.Background())

	if ids := noticeIDs(app); len(ids) != 0 {
		t.Errorf("notices = %q, want none for an up-to-date yt-dlp", ids)
	}
	if lines := shown(); len(lines) != 0 {
		t.Errorf("log view = %q, want nothing for an up-to-date yt-dlp", lines)
	}
}

func TestYtDlpVersions(t *testing.T) {
	app, _ := newUpdateCheckApp(t, "2026.10.01")

	installed, latest := app.ytDlpVersions()

	if installed != fakeToolVersion || latest != "2026.10.01" {
		t.Errorf("ytDlpVersions() = %q, %q; want %q, 2026.10.01", installed, latest, fakeToolVersion)
	}
}
