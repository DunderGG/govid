package main

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestComponentStatusAction(t *testing.T) {
	inBin := InstalledTool{Path: "bin/x", InBin: true, Version: "8.1"}
	tests := []struct {
		name   string
		status componentStatus
		want   string
	}{
		{"missing", componentStatus{latest: "9.0.2"}, componentInstall},
		{"only on PATH", componentStatus{installed: InstalledTool{Path: "/usr/bin/x", Version: "9.0.2"}, latest: "9.0.2"}, componentInstall},
		{"older in bin", componentStatus{installed: inBin, latest: "9.0.2"}, componentUpdate},
		{"current in bin", componentStatus{installed: InstalledTool{Path: "bin/x", InBin: true, Version: "2.9.7"}, latest: "v2.9.7"}, componentReinstall},
		{"broken in bin", componentStatus{installed: InstalledTool{Path: "bin/x", InBin: true}, latest: "9.0.2"}, componentReinstall},
		{"latest unknown", componentStatus{installed: inBin}, componentReinstall},
	}
	for _, tt := range tests {
		if got := tt.status.action(); got != tt.want {
			t.Errorf("%s: action() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestComponentStatusText(t *testing.T) {
	tests := []struct {
		status componentStatus
		want   string
	}{
		{componentStatus{}, "not installed"},
		{componentStatus{installed: InstalledTool{Path: "bin/ffmpeg", InBin: true, Version: "8.1"}, noFFprobe: true}, "8.1 (bin/), without ffprobe"},
		{componentStatus{installed: InstalledTool{Path: "/usr/bin/node", Version: "24.16.0"}}, "24.16.0 (PATH)"},
		{componentStatus{installed: InstalledTool{Path: "bin/yt-dlp", InBin: true}}, "does not run (bin/)"},
	}
	for _, tt := range tests {
		if got := tt.status.installedText(); got != tt.want {
			t.Errorf("installedText() = %q, want %q", got, tt.want)
		}
	}
}

func TestMissingFilters(t *testing.T) {
	// Lines of "ffmpeg -hide_banner -filters" from the bundled 8.1 build.
	filters := "Filters:\n" +
		"  T.. = Timeline support\n" +
		" ... tonemap           V->V       Conversion to/from different dynamic ranges.\n" +
		" TSC zscale            V->V       Apply resizing, colorspace and bit depth conversion.\n"
	if missing := missingFilters(filters, hdrToneMapFilters); len(missing) != 0 {
		t.Errorf("missingFilters() = %q, want none", missing)
	}
	withoutZscale := strings.Replace(filters, "zscale", "scale", 1)
	if missing := missingFilters(withoutZscale, hdrToneMapFilters); !slices.Equal(missing, []string{"zscale"}) {
		t.Errorf("missingFilters() = %q, want zscale", missing)
	}
}

func TestCheckToolsShowsANoticeForEachMissingTool(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download") // bin/ holds yt-dlp only, and PATH is empty

	h.app.checkToolsNow()

	ids := noticeIDs(h.app)
	want := []string{missingToolNoticeID(toolFFmpeg), jsRuntimeNoticeID}
	if !slices.Equal(ids, want) {
		t.Fatalf("notices = %q, want %q", ids, want)
	}
	for _, n := range h.app.uiManager.notices {
		if canInstallTools() != (n.actionLabel == componentInstall) {
			t.Errorf("notice %q offers %q on %s", n.id, n.actionLabel, "this platform")
		}
		if !canInstallTools() && !strings.Contains(n.text, "package manager") {
			t.Errorf("notice %q does not say to use the package manager: %q", n.id, n.text)
		}
	}
	if !strings.Contains(h.joinedLogs(), "No JavaScript runtime") {
		t.Error("the missing runtime was not logged")
	}
}

func TestCheckToolsIsQuietWhenEverythingIsThere(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	installFakeTool(t, h.app.depSvc.binDir, "ffmpeg")
	installFakeTool(t, h.app.depSvc.binDir, "deno")

	h.app.checkToolsNow()

	if ids := noticeIDs(h.app); len(ids) != 0 {
		t.Errorf("notices = %q, want none", ids)
	}
}

func TestDownloadWarningShowsTheRuntimeNotice(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-no-js-runtime")
	h.app.ui.download.entry.SetText("https://example.com/watch?v=1")

	h.startAndWait(t)

	if ids := noticeIDs(h.app); !slices.Contains(ids, jsRuntimeNoticeID) {
		t.Errorf("notices = %q, want the JavaScript runtime notice", ids)
	}
}

// waitInstall runs installComponent and waits for it to report.
func waitInstall(t *testing.T, app *DownloaderApp, name, action string) error {
	t.Helper()
	done := make(chan error, 1)
	app.installComponent(name, action, func(err error) { done <- err })
	select {
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		t.Fatal("installComponent did not finish")
		return nil
	}
}

func TestInstallComponentInstallsAndLooksForTheRuntimeAgain(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	ts := newToolServer(t)
	h.app.toolInstaller = newTestInstaller(t, ts)
	h.app.toolInstaller.deps = h.app.depSvc
	if _, ok := h.app.depSvc.JSRuntime(); ok {
		t.Fatal("found a runtime before installing one")
	}

	if err := waitInstall(t, h.app, toolDeno, componentInstall); err != nil {
		t.Fatalf("installComponent() error = %v", err)
	}

	if got := binFile(h.app.toolInstaller, "deno"); got != "new deno" {
		t.Errorf("bin/deno = %q, want the downloaded one", got)
	}
	// The test's "deno" is not a real program, so the new search finds it
	// and reports that it does not run: proof that it searched again.
	if notes := strings.Join(h.app.depSvc.JSRuntimeNotes(), "\n"); !strings.Contains(notes, "could not be run") {
		t.Errorf("JSRuntimeNotes() = %q, want the new Deno tried", notes)
	}
	if !strings.Contains(h.joinedLogs(), "Installed Deno (JavaScript runtime) v2.9.7 in bin/.") {
		t.Errorf("log does not report the install:\n%s", h.joinedLogs())
	}
	if h.app.installing.Load() || h.app.ui.download.downloadBtn.Disabled() {
		t.Error("the install did not finish cleanly")
	}
}

func TestInstallComponentRefusesWhileBusy(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.toolInstaller = newTestInstaller(t, newToolServer(t))

	h.app.isRunning.Store(true)
	if err := waitInstall(t, h.app, toolDeno, componentInstall); !errors.Is(err, errToolsInUse) {
		t.Errorf("during a session: error = %v, want errToolsInUse", err)
	}
	h.app.isRunning.Store(false)

	h.app.installing.Store(true)
	if err := waitInstall(t, h.app, toolDeno, componentInstall); !errors.Is(err, errAlreadyInstall) {
		t.Errorf("during another install: error = %v, want errAlreadyInstall", err)
	}
	if got := binFile(h.app.toolInstaller, "deno"); got != "<missing>" {
		t.Errorf("bin/deno = %q, want nothing installed", got)
	}
}

// stubYtDlpUpdate replaces "yt-dlp -U" with a stub, so a test decides when
// the update ends. It returns the number of updates started and the
// callbacks of the last one.
func stubYtDlpUpdate(app *DownloaderApp) (started *int, last *UpdateCallbacks) {
	started, last = new(int), new(UpdateCallbacks)
	app.uiManager.onRunUpdate = func(cb UpdateCallbacks) {
		*started++
		*last = cb
	}
	return started, last
}

func TestYtDlpUpdateRefusesWhileBusy(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	started, _ := stubYtDlpUpdate(h.app)

	h.app.isRunning.Store(true)
	if err := waitInstall(t, h.app, toolYtDlp, componentUpdate); !errors.Is(err, errToolsInUse) {
		t.Errorf("Components during a session: error = %v, want errToolsInUse", err)
	}
	// The Tools menu and the out-of-date notice share runUpdateInUI.
	h.app.uiManager.runUpdateInUI()
	h.app.isRunning.Store(false)

	h.app.installing.Store(true)
	if err := waitInstall(t, h.app, toolYtDlp, componentUpdate); !errors.Is(err, errAlreadyInstall) {
		t.Errorf("Components during an install: error = %v, want errAlreadyInstall", err)
	}
	h.app.uiManager.runUpdateInUI()
	h.app.installing.Store(false)

	if *started != 0 {
		t.Errorf("yt-dlp -U ran %d times while busy, want 0", *started)
	}
}

func TestYtDlpUpdateBlocksDownloadsUntilItEnds(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	started, last := stubYtDlpUpdate(h.app)
	h.app.ui.download.entry.SetText("https://example.com/watch?v=1")

	h.app.uiManager.runUpdateInUI()

	if *started != 1 {
		t.Fatalf("yt-dlp -U ran %d times, want 1", *started)
	}
	if !h.app.installing.Load() || !h.app.ui.download.downloadBtn.Disabled() {
		t.Error("the update does not hold installing and the Download button")
	}
	h.app.startDownload()
	if h.app.isRunning.Load() {
		t.Error("a download started while yt-dlp was being updated")
	}
	if err := waitInstall(t, h.app, toolYtDlp, componentUpdate); !errors.Is(err, errAlreadyInstall) {
		t.Errorf("a second update: error = %v, want errAlreadyInstall", err)
	}

	last.OnSuccess()

	if h.app.installing.Load() || h.app.ui.download.downloadBtn.Disabled() {
		t.Error("the update did not finish cleanly")
	}
}

func TestComponentsRowsOfferEachToolsAction(t *testing.T) {
	_ = test.NewApp()
	manager := NewUIManager(test.NewWindow(nil))
	var actions []string
	manager.onComponentAction = func(name, action string, onDone func(error)) {
		actions = append(actions, name+":"+action)
	}
	manager.onComponents = func() []componentStatus { return nil }
	window := test.NewWindow(nil)
	manager.compWindow = window
	rows := container.NewVBox()

	manager.renderComponents(window, rows, []componentStatus{
		{name: toolYtDlp, label: "yt-dlp", installed: InstalledTool{Path: "bin/yt-dlp", InBin: true, Version: "2026.03.17"}, latest: "2026.09.26"},
		{name: toolDeno, label: "Deno", latest: "v2.9.7"},
	})

	var buttons []*widget.Button
	walkObjects(rows, func(obj fyne.CanvasObject) {
		if button, ok := obj.(*widget.Button); ok {
			buttons = append(buttons, button)
		}
	})
	if !canInstallTools() {
		if len(buttons) != 0 {
			t.Errorf("offered %d buttons where GoVid cannot install tools", len(buttons))
		}
		return
	}
	if len(buttons) != 2 || buttons[0].Text != componentUpdate || buttons[1].Text != componentInstall {
		t.Fatalf("buttons = %v, want Update and Install", buttons)
	}
	test.Tap(buttons[1])
	if !slices.Equal(actions, []string{"deno:Install"}) || !buttons[0].Disabled() {
		t.Errorf("tapping Install ran %q; other buttons disabled = %v", actions, buttons[0].Disabled())
	}
}
