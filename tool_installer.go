// tool_installer.go — Installing yt-dlp, FFmpeg, and Deno into bin/.
//
// Responsibilities:
//   - toolSpec / installableTools: how each tool is installed: which files
//     go into bin/, and where its newest release, its download, and its
//     published checksum are (toolRelease).
//   - ToolInstaller: downloads a tool's release into a temporary folder,
//     with progress, checks it against its published SHA-256, and moves its
//     files into bin/ through a ".new" file and a rename. A failed step puts
//     the old files back, so a failed install leaves the old tool working.
//   - parseBareHash, parseGetFileHash: the checksum formats of gyan.dev and
//     Deno (yt-dlp's sha256sum format is parseSHA256Sums, in
//     self_update.go).
//
// The downloads are Windows builds; the Components window does not offer
// them elsewhere. It has no UI dependency.
package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

// The tools the Components window installs, by the name DependencyService
// resolves them under.
const (
	toolYtDlp  = "yt-dlp"
	toolFFmpeg = "ffmpeg"
	toolDeno   = "deno"
)

// toolDownloadTimeout bounds a whole tool download; the FFmpeg ZIP is over
// 100 MB.
const toolDownloadTimeout = 15 * time.Minute

// Where the downloads come from.
const (
	githubURL = "https://github.com"
	// gyanURL publishes the "essentials" FFmpeg builds, the family the
	// release ZIP bundles. release-version names the current version, and
	// packages/ holds each version's ZIP and its .sha256.
	gyanURL = "https://www.gyan.dev/ffmpeg/builds/"
)

// denoAsset is the Windows build of Deno on its GitHub releases.
const denoAsset = "deno-x86_64-pc-windows-msvc.zip"

// toolSpec describes how one tool is installed.
type toolSpec struct {
	name  string   // as DependencyService resolves it
	label string   // as the Components window shows it
	files []string // the executables put in bin/, without ".exe"; files[0] is the tool itself
	// latest finds the tool's newest release and where to download it.
	latest func(installer *ToolInstaller, ctx context.Context) (toolRelease, error)
}

// installableTools lists the tools the Components window offers, in its order.
var installableTools = []toolSpec{
	{name: toolYtDlp, label: "yt-dlp", files: []string{"yt-dlp"}, latest: (*ToolInstaller).latestYtDlp},
	{name: toolFFmpeg, label: "FFmpeg (with ffprobe)", files: []string{"ffmpeg", "ffprobe"}, latest: (*ToolInstaller).latestFFmpeg},
	{name: toolDeno, label: "Deno (JavaScript runtime)", files: []string{"deno"}, latest: (*ToolInstaller).latestDeno},
}

// findTool returns the installable tool called name.
func findTool(name string) (toolSpec, bool) {
	for _, tool := range installableTools {
		if tool.name == name {
			return tool, true
		}
	}
	return toolSpec{}, false
}

// checksumParser reads a published checksum file and returns the SHA-256 it
// lists for assetName, in lower case.
type checksumParser func(text, assetName string) (string, bool)

// toolRelease is a tool's newest release and where to download it.
type toolRelease struct {
	version     string // "" when unknown
	assetName   string // the file downloaded, e.g. "yt-dlp.exe"
	url         string
	checksumURL string
	parse       checksumParser
	zipped      bool // the download is a ZIP holding the tool's files
}

// ToolInstaller installs tools into DependencyService's bin/ folder.
type ToolInstaller struct {
	httpFetcher
	deps      *DependencyService
	releases  *ReleaseService
	githubURL string                      // githubURL; replaced in tests
	gyanURL   string                      // gyanURL; replaced in tests
	tempDir   string                      // where downloads go; "" for the system temp folder
	writable  func(dir string) bool       // dirWritable; replaced in tests
	rename    func(from, to string) error // os.Rename; replaced in tests
}

// NewToolInstaller returns an installer that puts tools in deps' bin/
// folder, asking releases for GitHub releases.
func NewToolInstaller(deps *DependencyService, releases *ReleaseService, userAgent string) *ToolInstaller {
	return &ToolInstaller{
		httpFetcher: httpFetcher{client: &http.Client{Timeout: toolDownloadTimeout}, userAgent: userAgent},
		deps:        deps,
		releases:    releases,
		githubURL:   githubURL,
		gyanURL:     gyanURL,
		writable:    dirWritable,
		rename:      os.Rename,
	}
}

// Latest returns the version of tool's newest release, "" when unknown.
func (installer *ToolInstaller) Latest(ctx context.Context, tool toolSpec) (string, error) {
	release, err := tool.latest(installer, ctx)
	return release.version, err
}

// ── Sources ──────────────────────────────────────────────────────────────────

// latestYtDlp is yt-dlp.exe from yt-dlp's latest GitHub release.
func (installer *ToolInstaller) latestYtDlp(ctx context.Context) (toolRelease, error) {
	return installer.githubRelease(ctx, ytDlpOwner, ytDlpRepo, toolRelease{
		assetName: "yt-dlp.exe",
		parse:     parseSHA256Sums,
	}, "SHA2-256SUMS")
}

// latestDeno is the Windows ZIP from Deno's latest GitHub release.
func (installer *ToolInstaller) latestDeno(ctx context.Context) (toolRelease, error) {
	return installer.githubRelease(ctx, "denoland", "deno", toolRelease{
		assetName: denoAsset,
		parse:     parseGetFileHash,
		zipped:    true,
	}, denoAsset+".sha256sum")
}

// latestFFmpeg is gyan.dev's current "essentials" build. The ZIP and its
// checksum are taken from the versioned packages/ folder, so both always
// belong to the version release-version named.
func (installer *ToolInstaller) latestFFmpeg(ctx context.Context) (toolRelease, error) {
	text, err := installer.fetchText(ctx, installer.gyanURL+"release-version")
	if err != nil {
		return toolRelease{}, fmt.Errorf("asking gyan.dev for the FFmpeg version: %w", err)
	}
	version := strings.TrimSpace(text)
	if versionPattern.FindString(version) != version {
		return toolRelease{}, fmt.Errorf("gyan.dev named an unexpected FFmpeg version %q", version)
	}
	name := "ffmpeg-" + version + "-essentials_build.zip"
	zipURL := installer.gyanURL + "packages/" + name
	return toolRelease{
		version:     version,
		assetName:   name,
		url:         zipURL,
		checksumURL: zipURL + ".sha256",
		parse:       parseBareHash,
		zipped:      true,
	}, nil
}

// githubRelease completes release with the download and checksum links of
// owner/repo's latest release, whose assets are called release.assetName
// and sumsName. GitHub's API allows only a few requests an hour without an
// account; when it refuses, the release's /latest/download/ links (which
// it does not limit) are used, and the version comes from where
// /releases/latest redirects to.
func (installer *ToolInstaller) githubRelease(ctx context.Context, owner, repo string, release toolRelease, sumsName string) (toolRelease, error) {
	latest, err := installer.releases.Latest(ctx, owner, repo, 0)
	switch {
	case errors.Is(err, errReleaseUnknown):
		base := fmt.Sprintf("%s/%s/%s/releases/latest", installer.githubURL, owner, repo)
		release.url = base + "/download/" + release.assetName
		release.checksumURL = base + "/download/" + sumsName
		release.version = installer.latestTag(ctx, base)
		return release, nil
	case err != nil:
		return toolRelease{}, err
	}

	release.version = latest.TagName
	for _, asset := range latest.Assets {
		switch asset.Name {
		case release.assetName:
			release.url = asset.DownloadURL
		case sumsName:
			release.checksumURL = asset.DownloadURL
		}
	}
	if release.url == "" || release.checksumURL == "" {
		return toolRelease{}, fmt.Errorf("%s/%s %s does not have both %s and %s", owner, repo, latest.TagName, release.assetName, sumsName)
	}
	return release, nil
}

// latestTag returns the tag GitHub's /releases/latest page redirects to,
// or "" when it cannot tell.
func (installer *ToolInstaller) latestTag(ctx context.Context, latestURL string) string {
	client := *installer.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, latestURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", installer.userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	_, tag, found := strings.Cut(resp.Header.Get("Location"), "/releases/tag/")
	if !found {
		return ""
	}
	if unescaped, err := url.PathUnescape(tag); err == nil {
		tag = unescaped
	}
	return tag
}

// ── Checksum formats ─────────────────────────────────────────────────────────

// cleanChecksumText removes what can surround a checksum file's text: a
// byte order mark, UTF-16 encoding (Windows PowerShell's default), and
// carriage returns.
func cleanChecksumText(text string) string {
	data := []byte(text)
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
		units := make([]uint16, 0, len(data)/2)
		for i := 2; i+1 < len(data); i += 2 {
			units = append(units, uint16(data[i])|uint16(data[i+1])<<8)
		}
		text = string(utf16.Decode(units))
	}
	text = strings.TrimPrefix(text, "\uFEFF")
	return strings.ReplaceAll(text, "\r", "")
}

// isSHA256Hex reports whether text is a SHA-256 in hex.
func isSHA256Hex(text string) bool {
	decoded, err := hex.DecodeString(text)
	return err == nil && len(decoded) == sha256.Size
}

// parseBareHash reads a checksum file that holds only the hash, as
// gyan.dev's .sha256 files do. The file is for one download, so assetName
// is not checked.
func parseBareHash(text, _ string) (string, bool) {
	fields := strings.Fields(cleanChecksumText(text))
	if len(fields) == 0 || !isSHA256Hex(fields[0]) {
		return "", false
	}
	return strings.ToLower(fields[0]), true
}

// parseGetFileHash reads the list output of PowerShell's Get-FileHash, as
// Deno publishes it:
//
//	Algorithm : SHA256
//	Hash      : A0C3101B4158D1DF…
//	Path      : C:\a\deno\deno\target\release\deno-x86_64-pc-windows-msvc.zip
//
// The path is where Deno's build machine had the file, so only its file
// name is compared with assetName.
func parseGetFileHash(text, assetName string) (string, bool) {
	var algorithm, hash, path string
	for _, line := range strings.Split(cleanChecksumText(text), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Algorithm":
			algorithm = value
		case "Hash":
			hash = value
		case "Path":
			path = value
		}
	}
	if !strings.EqualFold(algorithm, "SHA256") || !isSHA256Hex(hash) {
		return "", false
	}
	if path != "" && !strings.EqualFold(windowsBase(path), assetName) {
		return "", false
	}
	return strings.ToLower(hash), true
}

// windowsBase returns the last element of a path written with either
// slash, whatever the platform.
func windowsBase(path string) string {
	return filepath.Base(filepath.FromSlash(strings.ReplaceAll(path, `\`, "/")))
}

// ── Installing ───────────────────────────────────────────────────────────────

// localPath returns where file (without ".exe") goes in bin/.
func (installer *ToolInstaller) localPath(file string) string {
	return installer.deps.LocalPath(file)
}

// Install downloads tool's newest release, checks it against its published
// SHA-256, and moves its files into bin/, reporting the download's progress
// through onProgress (total is -1 when the server does not say). It returns
// the version installed, "" when unknown. When any step fails, the files
// in bin/ are left as they were.
func (installer *ToolInstaller) Install(ctx context.Context, tool toolSpec, onProgress func(done, total int64)) (string, error) {
	binDir := filepath.Dir(installer.localPath(tool.files[0]))
	if err := os.MkdirAll(binDir, 0755); err != nil || !installer.writable(binDir) {
		return "", fmt.Errorf("GoVid cannot write to %s. Run GoVid as administrator once, or move GoVid to a folder you can write to", binDir)
	}
	release, err := tool.latest(installer, ctx)
	if err != nil {
		return "", fmt.Errorf("finding the latest %s: %w", tool.label, err)
	}

	sums, err := installer.fetchText(ctx, release.checksumURL)
	if err != nil {
		return "", fmt.Errorf("downloading the checksum of %s: %w", release.assetName, err)
	}
	want, ok := release.parse(sums, release.assetName)
	if !ok {
		return "", fmt.Errorf("the published checksum of %s could not be read", release.assetName)
	}

	dir, err := os.MkdirTemp(installer.tempDir, "govid-tool-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	download := filepath.Join(dir, release.assetName)
	got, err := installer.fetchFile(ctx, release.url, download, onProgress)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", release.assetName, err)
	}
	if got != want {
		return "", fmt.Errorf("%w (%s: expected %s, got %s); nothing was changed", errChecksumMismatch, release.assetName, want, got)
	}

	staged, err := installer.stage(tool, release, download)
	if err != nil {
		return "", err
	}
	if err := installer.swap(staged); err != nil {
		return "", fmt.Errorf("putting %s in place: %w; the previous files were kept", tool.label, err)
	}
	return release.version, nil
}

// stagedFile is a new file waiting beside the file it replaces.
type stagedFile struct {
	next   string // <final>.new
	final  string
	hadOld bool // final existed and was moved to <final>.old
}

// stage writes each of tool's files from the download to <file>.new in
// bin/. On failure it removes what it wrote.
func (installer *ToolInstaller) stage(tool toolSpec, release toolRelease, download string) ([]stagedFile, error) {
	var staged []stagedFile
	for _, file := range tool.files {
		final := installer.localPath(file)
		next := final + ".new"
		var err error
		if release.zipped {
			err = extractNamed(download, filepath.Base(final), next)
		} else {
			err = copyFile(download, next)
		}
		if err != nil {
			os.Remove(next)
			removeStaged(staged)
			return nil, err
		}
		staged = append(staged, stagedFile{next: next, final: final})
	}
	return staged, nil
}

// swap moves each staged file into place, moving the file it replaces to
// <final>.old first, and then deletes the .old files. If a move fails,
// every file already moved is put back. A .old file that cannot be deleted
// (Windows does not delete a running executable) is left for
// removeOldTools at the next start.
func (installer *ToolInstaller) swap(staged []stagedFile) error {
	for i := range staged {
		file := &staged[i]
		old := file.final + ".old"
		if fileExists(file.final) {
			os.Remove(old) // a leftover would block the rename on some systems
			if err := installer.rename(file.final, old); err != nil {
				restoreSwapped(staged[:i])
				removeStaged(staged)
				return err
			}
			file.hadOld = true
		}
		if err := installer.rename(file.next, file.final); err != nil {
			restoreSwapped(staged[:i+1])
			removeStaged(staged)
			return err
		}
	}
	for _, file := range staged {
		if file.hadOld {
			os.Remove(file.final + ".old")
		}
	}
	return nil
}

// restoreSwapped puts back the old files of swapped, newest first.
func restoreSwapped(swapped []stagedFile) {
	for i := len(swapped) - 1; i >= 0; i-- {
		file := swapped[i]
		if !file.hadOld {
			if !fileExists(file.next) {
				os.Remove(file.final) // the new file had been moved in
			}
			continue
		}
		os.Remove(file.final)
		os.Rename(file.final+".old", file.final)
	}
}

// removeStaged deletes the .new files that were not moved into place.
func removeStaged(staged []stagedFile) {
	for _, file := range staged {
		os.Remove(file.next)
	}
}

// removeOldTools deletes the .old and .new files an install left in bin/,
// for example when the old tool was still running. It is called at
// startup.
func removeOldTools(deps *DependencyService) {
	for _, tool := range installableTools {
		for _, file := range tool.files {
			final := deps.LocalPath(file)
			os.Remove(final + ".old")
			os.Remove(final + ".new")
		}
	}
}

// extractNamed writes the ZIP entry whose file name is name, in whatever
// folder of the ZIP, to dest. gyan.dev's ZIP keeps the tools in a
// versioned folder, e.g. "ffmpeg-9.0.2-essentials_build/bin/ffmpeg.exe".
func extractNamed(zipPath, name, dest string) error {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", filepath.Base(zipPath), err)
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() || !strings.EqualFold(windowsBase(entry.Name), name) {
			continue
		}
		return extractEntry(entry, dest)
	}
	return fmt.Errorf("%s does not contain %s", filepath.Base(zipPath), name)
}

// copyFile copies src to dest as an executable.
func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
