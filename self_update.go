// self_update.go — Replacing the running GoVid with a newer release.
//
// Responsibilities:
//   - SelfUpdater: downloads a release's ZIP and its SHA256SUMS, refuses a
//     ZIP whose hash does not match, extracts GoVid.exe beside the running
//     executable, swaps the two (Windows lets a running .exe be renamed, but
//     not overwritten), and starts the new one. Any failed step puts the old
//     executable back. It has no UI dependency.
//   - findUpdateAssets / parseSHA256Sums: the release assets and checksum
//     file format package.ps1 produces.
//   - removeOldExecutable: deletes the GoVid.exe.old a previous update left.
//   - DownloaderApp.canSelfUpdate / runSelfUpdate: when "Update now" is
//     offered, and running it with progress in the status label.
package main

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
)

// releaseBuildType is main.buildType in a build made by package.ps1.
const releaseBuildType = "release"

// sumsAssetName is the checksum file package.ps1 writes beside the ZIP.
const sumsAssetName = "SHA256SUMS"

// updateDownloadTimeout bounds the whole download of a release ZIP.
const updateDownloadTimeout = 15 * time.Minute

// errChecksumMismatch is returned when a downloaded ZIP does not match its
// published SHA-256. The ZIP is kept for inspection.
var errChecksumMismatch = errors.New("the download does not match its published SHA-256")

// zipAssetName returns the name of a release's ZIP, as package.ps1 names it.
func zipAssetName(tag string) string {
	return "GoVid_" + tag + "_Ready.zip"
}

// updateAssets are the two files an update needs.
type updateAssets struct {
	zip  ReleaseAsset
	sums ReleaseAsset
}

// findUpdateAssets returns release's ZIP and SHA256SUMS. ok is false when
// either is missing, e.g. for a release made before checksums were
// published.
func findUpdateAssets(release Release) (assets updateAssets, ok bool) {
	var hasZip, hasSums bool
	for _, asset := range release.Assets {
		switch asset.Name {
		case zipAssetName(release.TagName):
			assets.zip, hasZip = asset, true
		case sumsAssetName:
			assets.sums, hasSums = asset, true
		}
	}
	return assets, hasZip && hasSums
}

// parseSHA256Sums returns the hash sums lists for the file called name, in
// lower case. It accepts the "<hash>  <name>" lines sha256sum writes (and
// its "<hash> *<name>" binary form), and ignores blank and # comment lines.
func parseSHA256Sums(sums, name string) (string, bool) {
	scanner := bufio.NewScanner(strings.NewReader(sums))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		hash, file, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		file = strings.TrimPrefix(strings.TrimLeft(file, " "), "*")
		if file == name && len(hash) == sha256.Size*2 {
			return strings.ToLower(hash), true
		}
	}
	return "", false
}

// SelfUpdater replaces the running GoVid with a release's GoVid.exe.
type SelfUpdater struct {
	client    *http.Client
	userAgent string
	exePath   string                  // the running executable
	tempDir   string                  // where downloads go; "" for the system temp folder
	start     func(path string) error // starts the new executable; replaced in tests
	writable  func(dir string) bool   // dirWritable; replaced in tests
}

// NewSelfUpdater returns an updater for the running executable.
func NewSelfUpdater(userAgent string) (*SelfUpdater, error) {
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding GoVid's own file: %w", err)
	}
	return &SelfUpdater{
		client:    &http.Client{Timeout: updateDownloadTimeout},
		userAgent: userAgent,
		exePath:   exePath,
		start:     startDetached,
		writable:  dirWritable,
	}, nil
}

// startDetached starts the executable at path in its own folder, without
// waiting for it.
func startDetached(path string) error {
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Download fetches release's SHA256SUMS and ZIP into a new temporary folder,
// reporting the ZIP's progress through onProgress (total is -1 when the
// server does not say), and checks the ZIP against its listed hash. It
// returns the ZIP's path; on a mismatch the error wraps errChecksumMismatch
// and the ZIP is kept at that path for inspection.
func (updater *SelfUpdater) Download(ctx context.Context, release Release, onProgress func(done, total int64)) (string, error) {
	assets, ok := findUpdateAssets(release)
	if !ok {
		return "", fmt.Errorf("release %s does not have both %s and %s", release.TagName, zipAssetName(release.TagName), sumsAssetName)
	}
	sums, err := updater.fetchText(ctx, assets.sums.DownloadURL)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", sumsAssetName, err)
	}
	want, ok := parseSHA256Sums(sums, assets.zip.Name)
	if !ok {
		return "", fmt.Errorf("%s does not list %s", sumsAssetName, assets.zip.Name)
	}

	dir, err := os.MkdirTemp(updater.tempDir, "govid-update-*")
	if err != nil {
		return "", err
	}
	zipPath := filepath.Join(dir, assets.zip.Name)
	got, err := updater.fetchFile(ctx, assets.zip.DownloadURL, zipPath, onProgress)
	if err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("downloading %s: %w", assets.zip.Name, err)
	}
	if got != want {
		return zipPath, fmt.Errorf("%w (expected %s, got %s)", errChecksumMismatch, want, got)
	}
	return zipPath, nil
}

// get starts a GET request for url and returns the response, which the
// caller closes, after checking its status.
func (updater *SelfUpdater) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", updater.userAgent)
	resp, err := updater.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("server answered %s", resp.Status)
	}
	return resp, nil
}

// fetchText downloads a small text file.
func (updater *SelfUpdater) fetchText(ctx context.Context, url string) (string, error) {
	resp, err := updater.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(data), err
}

// fetchFile downloads url to path and returns the file's SHA-256 in lower
// case hex.
func (updater *SelfUpdater) fetchFile(ctx context.Context, url, path string, onProgress func(done, total int64)) (string, error) {
	resp, err := updater.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	progress := &progressWriter{total: resp.ContentLength, onProgress: onProgress}
	if _, err := io.Copy(io.MultiWriter(file, hash, progress), resp.Body); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// progressWriter counts the bytes written through it and reports them.
type progressWriter struct {
	done, total int64
	onProgress  func(done, total int64)
}

func (writer *progressWriter) Write(p []byte) (int, error) {
	writer.done += int64(len(p))
	if writer.onProgress != nil {
		writer.onProgress(writer.done, writer.total)
	}
	return len(p), nil
}

// Install puts the GoVid.exe from zipPath in place of the running
// executable and starts it. The caller should then quit. Windows lets a
// running .exe be renamed but not replaced, so the running one is renamed
// to GoVid.exe.old (deleted at the next start, see removeOldExecutable)
// and the new one moved into its place. If any step fails, the original
// executable is put back.
func (updater *SelfUpdater) Install(zipPath string) error {
	dir := filepath.Dir(updater.exePath)
	if !updater.writable(dir) {
		return fmt.Errorf("GoVid cannot write to %s. Run GoVid as administrator once to update it, or move GoVid to a folder you can write to", dir)
	}
	newPath := updater.exePath + ".new"
	oldPath := updater.exePath + ".old"
	if err := extractExecutable(zipPath, filepath.Base(updater.exePath), newPath); err != nil {
		return err
	}

	// A GoVid.exe.old a failed cleanup left would block the rename.
	if err := os.Remove(oldPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.Remove(newPath)
		return fmt.Errorf("removing the old %s: %w", filepath.Base(oldPath), err)
	}
	if err := os.Rename(updater.exePath, oldPath); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("moving the running GoVid aside: %w", err)
	}
	if err := os.Rename(newPath, updater.exePath); err != nil {
		os.Rename(oldPath, updater.exePath)
		os.Remove(newPath)
		return fmt.Errorf("putting the new GoVid in place: %w", err)
	}
	if err := updater.start(updater.exePath); err != nil {
		os.Remove(updater.exePath)
		os.Rename(oldPath, updater.exePath)
		return fmt.Errorf("starting the new GoVid: %w", err)
	}
	return nil
}

// extractExecutable writes the ZIP's top-level entry called name (GoVid.exe)
// to dest. Entry names may use either slash: Windows PowerShell's
// Compress-Archive writes backslashes.
func extractExecutable(zipPath, name, dest string) error {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening the update: %w", err)
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if !strings.EqualFold(strings.ReplaceAll(entry.Name, `\`, "/"), name) {
			continue
		}
		return extractEntry(entry, dest)
	}
	return fmt.Errorf("the update does not contain %s", name)
}

// extractEntry writes one ZIP entry to dest.
func extractEntry(entry *zip.File, dest string) error {
	src, err := entry.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		os.Remove(dest)
		return fmt.Errorf("extracting %s: %w", entry.Name, err)
	}
	return out.Close()
}

// removeOldExecutable deletes the executable an update moved aside
// (GoVid.exe.old) and any half-extracted GoVid.exe.new. It is called at
// startup, once the old process has exited.
func removeOldExecutable(exePath string) error {
	var errs []error
	for _, leftover := range []string{exePath + ".old", exePath + ".new"} {
		if err := os.Remove(leftover); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ── App ──────────────────────────────────────────────────────────────────────

// canSelfUpdate reports whether "Update now" can be offered for release: a
// release build, on Windows, with the release's ZIP and SHA256SUMS
// published, and no download or update running.
func (app *DownloaderApp) canSelfUpdate(release Release) bool {
	if buildType != releaseBuildType || runtime.GOOS != "windows" || app.selfUpdater == nil {
		return false
	}
	if app.isRunning.Load() || app.updating.Load() {
		return false
	}
	_, ok := findUpdateAssets(release)
	return ok
}

// runSelfUpdate downloads, checks, and installs release in the background,
// with progress in the status label, and quits once the new GoVid has
// started. On failure it says why and leaves the installed GoVid as it was.
func (app *DownloaderApp) runSelfUpdate(release Release) {
	if !app.updating.CompareAndSwap(false, true) {
		return
	}
	fyne.Do(func() { app.ui.download.downloadBtn.Disable() })
	app.setStatusIndicator(StatusActive)
	app.appendOutput(fmt.Sprintf("[SYSTEM] Updating GoVid to %s…", release.TagName), colSystem)

	go func() {
		err := app.selfUpdate(release)
		if err == nil {
			app.appendOutput(fmt.Sprintf("[SYSTEM] GoVid %s started; closing this one.", release.TagName), colSuccess)
			app.updateStatus("Status: Restarting…")
			app.statusThrottle.Flush()
			app.Shutdown(fyne.CurrentApp().Quit)
			return
		}
		app.updating.Store(false)
		app.appendOutput(fmt.Sprintf("[ERROR] GoVid update failed: %v", err), colError)
		app.updateStatus("Status: GoVid update failed.")
		app.setStatusIndicator(StatusFailed)
		fyne.Do(func() {
			app.ui.download.downloadBtn.Enable()
			app.uiManager.showUpdateFailure(err)
		})
	}()
}

// selfUpdate downloads and installs release, reporting the download's
// progress in the status label.
func (app *DownloaderApp) selfUpdate(release Release) error {
	onProgress := func(done, total int64) {
		if total > 0 {
			app.updateStatus(fmt.Sprintf("Status: Downloading GoVid %s… %d%% of %s", release.TagName, done*100/total, formatBytes(total)))
		} else {
			app.updateStatus(fmt.Sprintf("Status: Downloading GoVid %s… %s", release.TagName, formatBytes(done)))
		}
	}
	zipPath, err := app.selfUpdater.Download(context.Background(), release, onProgress)
	if err != nil {
		if errors.Is(err, errChecksumMismatch) {
			return fmt.Errorf("%w. GoVid was not changed; the download is kept at %s for inspection", err, zipPath)
		}
		return err
	}
	app.updateStatus("Status: Installing GoVid " + release.TagName + "…")
	if err := app.selfUpdater.Install(zipPath); err != nil {
		return err
	}
	os.RemoveAll(filepath.Dir(zipPath))
	return nil
}

// oldExecutableCleanupAttempts and oldExecutableCleanupDelay bound how long
// startup keeps trying to delete GoVid.exe.old: the previous GoVid may still
// be closing (Shutdown waits up to shutdownTimeout), and Windows does not let
// a running executable be deleted.
const (
	oldExecutableCleanupAttempts = 15
	oldExecutableCleanupDelay    = time.Second
)

// cleanUpAfterUpdate deletes, in the background, the executable a previous
// update moved aside, logging to the log file if it cannot.
func (app *DownloaderApp) cleanUpAfterUpdate() {
	if app.selfUpdater == nil {
		return
	}
	go func() {
		var err error
		for attempt := 0; attempt < oldExecutableCleanupAttempts; attempt++ {
			if attempt > 0 {
				time.Sleep(oldExecutableCleanupDelay)
			}
			if err = removeOldExecutable(app.selfUpdater.exePath); err == nil {
				return
			}
		}
		app.logSvc.WriteToFile(fmt.Sprintf("[SYSTEM] Could not remove the previous GoVid after updating: %v", err))
	}()
}
