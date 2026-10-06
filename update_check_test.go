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

// useVersion sets main.version for the test, as -X main.version would.
func useVersion(t *testing.T, v string) {
	t.Helper()
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

// newGoVidUpdateApp returns an app whose release service asks a fake GitHub
// that says latestTag is the newest GoVid release.
func newGoVidUpdateApp(t *testing.T, latestTag string) (*DownloaderApp, *fakeGitHub, func() []string) {
	t.Helper()
	app, shown := newUpdateCheckApp(t, "2026.10.01")
	gh := newFakeGitHubRepos(t, map[string]fakeGitHubRelease{
		govidOwner + "/" + govidRepo: {http.StatusOK, `{"tag_name": "` + latestTag + `", "html_url": "https://github.com/DunderGG/govid/releases/tag/` + latestTag + `", "body": "- New things"}`},
	})
	now := time.Unix(1_800_000_000, 0)
	app.releaseSvc = newTestReleaseService(gh, &now)
	return app, gh, shown
}

func TestIsNewerRelease(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"0.0.1", "2026.09.17", true},
		{"2026.04.11", "2026.09.17", true},
		{"2026.09.17", "2026.09.17", false},
		{"2026.10.06", "2026.09.17", false},
		{"1.1.0", "v1.2.0", true},
		{devVersion, "2026.09.17", false},
	}
	for _, tt := range tests {
		if got := isNewerRelease(tt.current, tt.latest); got != tt.want {
			t.Errorf("isNewerRelease(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestCheckGoVidUpdateShowsNotice(t *testing.T) {
	useVersion(t, "0.0.1")
	app, _, shown := newGoVidUpdateApp(t, "2026.10.06")

	app.checkGoVidUpdate(context.Background())

	if ids := noticeIDs(app); !slices.Equal(ids, []string{govidNoticeID}) {
		t.Fatalf("notices = %q, want the GoVid update notice", ids)
	}
	if n := app.uiManager.notices[0]; !strings.Contains(n.text, "2026.10.06") || n.actionLabel != "What's new" {
		t.Errorf("notice = %+v, want the new version and a What's new button", n)
	}
	if want := "[SYSTEM] GoVid 2026.10.06 is available (this is 0.0.1)."; !slices.Contains(shown(), want) {
		t.Errorf("log = %q, want %q", shown(), want)
	}

	// "What's new" opens the release notes.
	app.uiManager.notices[0].action()
	overlay := app.window.Canvas().Overlays().Top()
	if overlay == nil {
		t.Fatal("no release dialog shown")
	}
	findButton(t, overlay, "Open download page")
}

func TestCheckGoVidUpdateNeverPromptsDevBuild(t *testing.T) {
	useVersion(t, devVersion)
	app, gh, _ := newGoVidUpdateApp(t, "2026.10.06")

	app.checkGoVidUpdate(context.Background())

	if ids := noticeIDs(app); len(ids) != 0 {
		t.Errorf("notices = %q, want none for a dev build", ids)
	}
	if n := gh.requests.Load(); n != 0 {
		t.Errorf("GitHub asked %d times, want 0 for a dev build", n)
	}
}

func TestCheckGoVidUpdateQuietWhenCurrent(t *testing.T) {
	useVersion(t, "2026.10.06")
	app, _, _ := newGoVidUpdateApp(t, "2026.10.06")

	app.checkGoVidUpdate(context.Background())

	if ids := noticeIDs(app); len(ids) != 0 {
		t.Errorf("notices = %q, want none when up to date", ids)
	}
}

func TestCheckGoVidReleaseIgnoresCache(t *testing.T) {
	useVersion(t, "0.0.1")
	app, gh, _ := newGoVidUpdateApp(t, "2026.10.06")

	for range 2 {
		release, newer, err := app.checkGoVidRelease()
		if err != nil || !newer || release.TagName != "2026.10.06" {
			t.Fatalf("checkGoVidRelease() = %+v, %v, %v", release, newer, err)
		}
	}
	if n := gh.requests.Load(); n != 2 {
		t.Errorf("GitHub asked %d times, want 2 (the menu check always asks)", n)
	}
}

func TestToolsMenuHasGoVidUpdateCheck(t *testing.T) {
	_ = test.NewApp()
	window := test.NewWindow(nil)
	app := newDownloaderApp(window)
	app.uiManager.createMainMenu()

	var labels []string
	for _, menu := range window.MainMenu().Items {
		if menu.Label == "Tools" {
			for _, item := range menu.Items {
				labels = append(labels, item.Label)
			}
		}
	}
	if !slices.Contains(labels, "Check for GoVid updates") {
		t.Errorf("Tools menu = %q, want Check for GoVid updates", labels)
	}
}

func TestYtDlpVersions(t *testing.T) {
	app, _ := newUpdateCheckApp(t, "2026.10.01")

	installed, latest := app.ytDlpVersions()

	if installed != fakeToolVersion || latest != "2026.10.01" {
		t.Errorf("ytDlpVersions() = %q, %q; want %q, 2026.10.01", installed, latest, fakeToolVersion)
	}
}
