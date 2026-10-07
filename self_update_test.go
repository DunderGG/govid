package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestFindUpdateAssets(t *testing.T) {
	both := Release{TagName: "2026.10.07", Assets: []ReleaseAsset{
		{Name: "GoVid_2026.10.07_Ready.zip", DownloadURL: "z"}, {Name: "SHA256SUMS", DownloadURL: "s"}, {Name: "notes.txt"},
	}}
	if assets, ok := findUpdateAssets(both); !ok || assets.zip.DownloadURL != "z" || assets.sums.DownloadURL != "s" {
		t.Errorf("findUpdateAssets() = %+v, %v", assets, ok)
	}
	older := Release{TagName: "2026.09.01", Assets: []ReleaseAsset{{Name: "GoVid_2026.09.01_Ready.zip"}}}
	if _, ok := findUpdateAssets(older); ok {
		t.Error("a release without SHA256SUMS was accepted")
	}
	otherTag := Release{TagName: "2026.10.07", Assets: []ReleaseAsset{{Name: "GoVid_2026.10.06_Ready.zip"}, {Name: "SHA256SUMS"}}}
	if _, ok := findUpdateAssets(otherTag); ok {
		t.Error("a ZIP for another tag was accepted")
	}
}

func TestParseSHA256Sums(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	sums := "# GoVid release\n\n" + strings.ToUpper(hash) + "  GoVid_x_Ready.zip\n" +
		strings.Repeat("cd", 32) + " *other.zip\n" + "short  bad.zip\n"
	tests := []struct {
		name   string
		want   string
		wantOK bool
	}{
		{"GoVid_x_Ready.zip", hash, true},
		{"other.zip", strings.Repeat("cd", 32), true},
		{"bad.zip", "", false},
		{"missing.zip", "", false},
	}
	for _, tt := range tests {
		if got, ok := parseSHA256Sums(sums, tt.name); got != tt.want || ok != tt.wantOK {
			t.Errorf("parseSHA256Sums(%q) = %q, %v; want %q, %v", tt.name, got, ok, tt.want, tt.wantOK)
		}
	}
}

// writeTestZip writes a ZIP with the given entries to path and returns its
// SHA-256.
func writeTestZip(t *testing.T, path string, entries map[string]string) string {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, content := range entries {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		writer.Write([]byte(content))
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()
	data, _ := os.ReadFile(path)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// releaseServer serves a release's ZIP (built from entries) and a
// SHA256SUMS listing sumsHash for it ("" for the ZIP's real hash), and
// returns the release.
func releaseServer(t *testing.T, entries map[string]string, sumsHash string) Release {
	t.Helper()
	const tag = "2026.10.07"
	zipPath := filepath.Join(t.TempDir(), zipAssetName(tag))
	realHash := writeTestZip(t, zipPath, entries)
	if sumsHash == "" {
		sumsHash = realHash
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/sums", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(sumsHash + "  " + zipAssetName(tag) + "\n"))
	})
	mux.HandleFunc("/zip", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, zipPath)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return Release{TagName: tag, Assets: []ReleaseAsset{
		{Name: zipAssetName(tag), DownloadURL: server.URL + "/zip"},
		{Name: sumsAssetName, DownloadURL: server.URL + "/sums"},
	}}
}

// newTestUpdater returns an updater for a fake installed GoVid.exe holding
// "old", whose start function records what it started.
func newTestUpdater(t *testing.T) (updater *SelfUpdater, started *[]string) {
	t.Helper()
	dir := t.TempDir()
	exePath := filepath.Join(dir, "GoVid.exe")
	if err := os.WriteFile(exePath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	started = new([]string)
	updater = &SelfUpdater{
		httpFetcher: httpFetcher{client: http.DefaultClient},
		exePath:     exePath,
		tempDir:     t.TempDir(),
		start:       func(path string) error { *started = append(*started, path); return nil },
		writable:    func(string) bool { return true },
	}
	return updater, started
}

// fileContent returns path's content, or "<missing>".
func fileContent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(data)
}

func TestSelfUpdaterDownloadAndInstall(t *testing.T) {
	// Windows PowerShell's Compress-Archive writes backslashes in entry names.
	release := releaseServer(t, map[string]string{"GoVid.exe": "new", `bin\yt-dlp.exe`: "tool", "VERSIONS.txt": "v"}, "")
	updater, started := newTestUpdater(t)
	var lastDone, lastTotal int64

	zipPath, err := updater.Download(context.Background(), release, func(done, total int64) { lastDone, lastTotal = done, total })
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if lastTotal <= 0 || lastDone != lastTotal {
		t.Errorf("progress ended at %d of %d bytes", lastDone, lastTotal)
	}
	if err := updater.Install(zipPath); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	if got := fileContent(updater.exePath); got != "new" {
		t.Errorf("GoVid.exe = %q, want the new one", got)
	}
	if got := fileContent(updater.exePath + ".old"); got != "old" {
		t.Errorf("GoVid.exe.old = %q, want the old one moved aside", got)
	}
	if len(*started) != 1 || (*started)[0] != updater.exePath {
		t.Errorf("started %q, want the new GoVid.exe", *started)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(updater.exePath), "bin")); err == nil {
		t.Error("the bundled tools were extracted too; only GoVid.exe should be")
	}

	if err := removeOldExecutable(updater.exePath); err != nil {
		t.Fatalf("removeOldExecutable() error = %v", err)
	}
	if got := fileContent(updater.exePath + ".old"); got != "<missing>" {
		t.Error("GoVid.exe.old still there after the cleanup")
	}
}

func TestSelfUpdaterRejectsAChecksumMismatch(t *testing.T) {
	release := releaseServer(t, map[string]string{"GoVid.exe": "tampered"}, strings.Repeat("0", 64))
	updater, started := newTestUpdater(t)

	zipPath, err := updater.Download(context.Background(), release, nil)

	if !errors.Is(err, errChecksumMismatch) {
		t.Fatalf("Download() error = %v, want errChecksumMismatch", err)
	}
	if _, statErr := os.Stat(zipPath); statErr != nil {
		t.Errorf("the mismatched download was not kept for inspection: %v", statErr)
	}
	if fileContent(updater.exePath) != "old" || len(*started) != 0 {
		t.Error("the installed GoVid was touched")
	}
}

func TestSelfUpdaterInstallPutsTheOldVersionBackOnFailure(t *testing.T) {
	tests := []struct {
		name    string
		entries map[string]string
		setup   func(updater *SelfUpdater)
		wantErr string
	}{
		{"new GoVid does not start", map[string]string{"GoVid.exe": "new"},
			func(updater *SelfUpdater) { updater.start = func(string) error { return errors.New("blocked") } }, "starting the new GoVid"},
		{"ZIP without GoVid.exe", map[string]string{"bin/ffmpeg.exe": "x"}, nil, "does not contain GoVid.exe"},
		{"folder not writable", map[string]string{"GoVid.exe": "new"},
			func(updater *SelfUpdater) { updater.writable = func(string) bool { return false } }, "cannot write to"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updater, _ := newTestUpdater(t)
			if tt.setup != nil {
				tt.setup(updater)
			}
			zipPath := filepath.Join(t.TempDir(), "update.zip")
			writeTestZip(t, zipPath, tt.entries)

			err := updater.Install(zipPath)

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Install() error = %v, want %q", err, tt.wantErr)
			}
			if got := fileContent(updater.exePath); got != "old" {
				t.Errorf("GoVid.exe = %q, want the old one back", got)
			}
			for _, leftover := range []string{".old", ".new"} {
				if got := fileContent(updater.exePath + leftover); got != "<missing>" {
					t.Errorf("GoVid.exe%s left behind", leftover)
				}
			}
		})
	}
}

func TestCorruptUpdateLeavesTheInstalledVersion(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	updater, started := newTestUpdater(t)
	h.app.selfUpdater = updater
	release := releaseServer(t, map[string]string{"GoVid.exe": "tampered"}, strings.Repeat("0", 64))

	err := h.app.selfUpdate(release)

	if !errors.Is(err, errChecksumMismatch) || !strings.Contains(err.Error(), "kept at") {
		t.Errorf("selfUpdate() error = %v, want a checksum mismatch naming the kept file", err)
	}
	if fileContent(updater.exePath) != "old" || len(*started) != 0 {
		t.Error("the installed GoVid was replaced or restarted")
	}
}

func TestCanSelfUpdate(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.selfUpdater, _ = newTestUpdater(t)
	release := Release{TagName: "2026.10.07", Assets: []ReleaseAsset{{Name: "GoVid_2026.10.07_Ready.zip"}, {Name: "SHA256SUMS"}}}

	if h.app.canSelfUpdate(release) {
		t.Error("offered for a build that is not a release build")
	}
	saved := buildType
	buildType = releaseBuildType
	defer func() { buildType = saved }()

	if got, want := h.app.canSelfUpdate(release), runtime.GOOS == "windows"; got != want {
		t.Errorf("canSelfUpdate() = %v on %s, want %v", got, runtime.GOOS, want)
	}
	h.app.isRunning.Store(true)
	if h.app.canSelfUpdate(release) {
		t.Error("offered while a download is running")
	}
	h.app.isRunning.Store(false)
	if h.app.canSelfUpdate(Release{TagName: "2026.10.07"}) {
		t.Error("offered for a release without the ZIP and SHA256SUMS")
	}
}

func TestReleaseDialogOffersUpdateNowOnlyWhenPossible(t *testing.T) {
	for _, possible := range []bool{false, true} {
		h := newDownloadHarness(t, "ytdlp-download")
		h.window.Resize(fyne.NewSize(900, 700))
		var updated []string
		h.app.uiManager.onCanSelfUpdate = func(Release) bool { return possible }
		h.app.uiManager.onSelfUpdate = func(release Release) { updated = append(updated, release.TagName) }

		h.app.uiManager.showGoVidRelease(Release{TagName: "2026.10.07", Body: "notes"})

		overlay := h.window.Canvas().Overlays().Top()
		var updateBtn *widget.Button
		walkObjects(overlay, func(obj fyne.CanvasObject) {
			if button, ok := obj.(*widget.Button); ok && button.Text == "Update now" {
				updateBtn = button
			}
		})
		if (updateBtn != nil) != possible {
			t.Errorf("possible=%v: Update now shown = %v", possible, updateBtn != nil)
			continue
		}
		if updateBtn != nil {
			test.Tap(updateBtn)
			if len(updated) != 1 || updated[0] != "2026.10.07" {
				t.Errorf("Update now started %q", updated)
			}
		}
	}
}
