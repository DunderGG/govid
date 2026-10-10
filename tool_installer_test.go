package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

// exeName returns file as it is named on this platform: with ".exe" on
// Windows.
func exeName(file string) string {
	if runtime.GOOS == "windows" {
		return file + ".exe"
	}
	return file
}

// denoSums is Deno's published checksum file for hash, as it really looks:
// PowerShell's Get-FileHash list output, with CRLF line ends.
func denoSums(hash string) string {
	return "\r\nAlgorithm : SHA256\r\nHash      : " + strings.ToUpper(hash) +
		"\r\nPath      : C:\\a\\deno\\deno\\target\\release\\deno-x86_64-pc-windows-msvc.zip\r\n\r\n"
}

func TestChecksumFormats(t *testing.T) {
	hash := strings.Repeat("a0c3", 16)
	utf16Sums := func(text string) string {
		data := []byte{0xFF, 0xFE}
		for _, unit := range utf16.Encode([]rune(text)) {
			data = append(data, byte(unit), byte(unit>>8))
		}
		return string(data)
	}
	tests := []struct {
		name   string
		parse  checksumParser
		text   string
		asset  string
		want   string
		wantOK bool
	}{
		{"yt-dlp SHA2-256SUMS", parseSHA256Sums,
			strings.Repeat("1f", 32) + "  yt-dlp\n" + hash + "  yt-dlp.exe\n" + strings.Repeat("07", 32) + "  yt-dlp.tar.gz\n",
			"yt-dlp.exe", hash, true},
		{"gyan.dev bare hash", parseBareHash, hash, "ffmpeg-9.0.2-essentials_build.zip", hash, true},
		{"gyan.dev bare hash, upper case with a newline", parseBareHash, strings.ToUpper(hash) + "\r\n", "x.zip", hash, true},
		{"gyan.dev page that is not a hash", parseBareHash, "<html>303 See Other</html>", "x.zip", "", false},
		{"Deno Get-FileHash", parseGetFileHash, denoSums(hash), denoAsset, hash, true},
		{"Deno Get-FileHash in UTF-16", parseGetFileHash, utf16Sums(denoSums(hash)), denoAsset, hash, true},
		{"Deno Get-FileHash for another file", parseGetFileHash, denoSums(hash), "deno-aarch64-apple-darwin.zip", "", false},
		{"Get-FileHash with another algorithm", parseGetFileHash, strings.Replace(denoSums(hash), "SHA256", "MD5", 1), denoAsset, "", false},
		{"Get-FileHash with a short hash", parseGetFileHash, denoSums("abcd"), denoAsset, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.parse(tt.text, tt.asset)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("got %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// toolServer serves the three tool sources from one test server: GitHub's
// API and release downloads (/repos/…, /<owner>/<repo>/releases/latest/…)
// and gyan.dev (/ffmpeg/…). Each file's checksum is its real hash unless
// set in badHash.
type toolServer struct {
	server     *httptest.Server
	mux        *http.ServeMux
	rateLimit  bool              // GitHub's API answers 403
	badHash    map[string]bool   // asset names whose published checksum is wrong
	files      map[string][]byte // asset name → content
	ffmpegName string
}

// newToolServer serves yt-dlp 2026.09.26, Deno v2.9.7, and FFmpeg 9.0.2.
// FFmpeg's ZIP keeps the tools in a versioned bin/ folder, as gyan.dev's
// does. The ZIPs' tools are named as on this platform, as the installer
// looks for them under their bin/ names.
func newToolServer(t *testing.T) *toolServer {
	t.Helper()
	ts := &toolServer{badHash: map[string]bool{}, files: map[string][]byte{}, ffmpegName: "ffmpeg-9.0.2-essentials_build.zip"}
	ts.files["yt-dlp.exe"] = []byte("new yt-dlp")
	ts.files[denoAsset] = zipBytes(t, map[string]string{exeName("deno"): "new deno"})
	ts.files[ts.ffmpegName] = zipBytes(t, map[string]string{
		"ffmpeg-9.0.2-essentials_build/bin/" + exeName("ffmpeg"):  "new ffmpeg",
		"ffmpeg-9.0.2-essentials_build/bin/" + exeName("ffprobe"): "new ffprobe",
		"ffmpeg-9.0.2-essentials_build/bin/" + exeName("ffplay"):  "ffplay",
		"ffmpeg-9.0.2-essentials_build/LICENSE":                   "license",
	})

	ts.mux = http.NewServeMux()
	ts.server = httptest.NewServer(ts.mux)
	t.Cleanup(ts.server.Close)

	ts.serveGitHub("yt-dlp", "yt-dlp", "2026.09.26", "yt-dlp.exe", "SHA2-256SUMS", func(asset string) string {
		return ts.hash(asset) + "  " + asset + "\n"
	})
	ts.serveGitHub("denoland", "deno", "v2.9.7", denoAsset, denoAsset+".sha256sum", func(asset string) string {
		return denoSums(ts.hash(asset))
	})
	ts.mux.HandleFunc("/ffmpeg/release-version", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("9.0.2"))
	})
	ts.mux.HandleFunc("/ffmpeg/packages/"+ts.ffmpegName, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(ts.files[ts.ffmpegName])
	})
	ts.mux.HandleFunc("/ffmpeg/packages/"+ts.ffmpegName+".sha256", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(ts.hash(ts.ffmpegName)))
	})
	return ts
}

// serveGitHub serves owner/repo's latest release: the API answer, the
// release's download links, and /releases/latest, which redirects to the
// release's tag as github.com does.
func (ts *toolServer) serveGitHub(owner, repo, tag, asset, sumsName string, sums func(asset string) string) {
	base := "/" + owner + "/" + repo + "/releases/latest"
	ts.mux.HandleFunc("/repos/"+owner+"/"+repo+"/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		if ts.rateLimit {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fmt.Fprintf(w, `{"tag_name": %q, "assets": [{"name": %q, "browser_download_url": %q}, {"name": %q, "browser_download_url": %q}]}`,
			tag, asset, ts.server.URL+base+"/download/"+asset, sumsName, ts.server.URL+base+"/download/"+sumsName)
	})
	ts.mux.HandleFunc(base+"/download/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(ts.files[asset])
	})
	ts.mux.HandleFunc(base+"/download/"+sumsName, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(sums(asset)))
	})
	ts.mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/"+owner+"/"+repo+"/releases/tag/"+tag, http.StatusFound)
	})
}

// hash returns the checksum published for asset.
func (ts *toolServer) hash(asset string) string {
	if ts.badHash[asset] {
		return strings.Repeat("0", 64)
	}
	return sha256Hex(ts.files[asset])
}

// zipBytes returns a ZIP holding entries.
func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.zip")
	writeTestZip(t, path, entries)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// newTestInstaller returns an installer whose bin/ is a temp folder and
// whose sources are ts.
func newTestInstaller(t *testing.T, ts *toolServer) *ToolInstaller {
	t.Helper()
	deps := &DependencyService{binDir: filepath.Join(t.TempDir(), "bin")}
	releases := NewReleaseService(memoryCache{}, "GoVid/test")
	releases.baseURL = ts.server.URL
	installer := NewToolInstaller(deps, releases, "GoVid/test")
	installer.githubURL = ts.server.URL
	installer.gyanURL = ts.server.URL + "/ffmpeg/"
	installer.tempDir = t.TempDir()
	return installer
}

// binFile returns the content of file (without ".exe") in installer's bin/,
// or "<missing>".
func binFile(installer *ToolInstaller, file string) string {
	return fileContent(installer.localPath(file))
}

// writeBinFile puts content in installer's bin/ as file (without ".exe").
func writeBinFile(t *testing.T, installer *ToolInstaller, file, content string) {
	t.Helper()
	path := installer.localPath(file)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

// leftovers lists the .new and .old files in installer's bin/.
func leftovers(t *testing.T, installer *ToolInstaller) []string {
	t.Helper()
	entries, _ := os.ReadDir(installer.deps.binDir)
	var names []string
	for _, entry := range entries {
		if ext := filepath.Ext(entry.Name()); ext == ".new" || ext == ".old" {
			names = append(names, entry.Name())
		}
	}
	return names
}

func TestToolInstallerInstallsEachTool(t *testing.T) {
	tests := []struct {
		tool        string
		wantVersion string
		want        map[string]string // bin/ file → content
	}{
		{toolYtDlp, "2026.09.26", map[string]string{"yt-dlp": "new yt-dlp"}},
		{toolDeno, "v2.9.7", map[string]string{"deno": "new deno"}},
		{toolFFmpeg, "9.0.2", map[string]string{"ffmpeg": "new ffmpeg", "ffprobe": "new ffprobe", "ffplay": "<missing>"}},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			ts := newToolServer(t)
			installer := newTestInstaller(t, ts)
			writeBinFile(t, installer, tt.tool, "old")
			tool, _ := findTool(tt.tool)
			var lastDone, lastTotal int64

			version, err := installer.Install(context.Background(), tool, func(done, total int64) { lastDone, lastTotal = done, total })

			if err != nil {
				t.Fatalf("Install() error = %v", err)
			}
			if version != tt.wantVersion {
				t.Errorf("version = %q, want %q", version, tt.wantVersion)
			}
			for file, want := range tt.want {
				if got := binFile(installer, file); got != want {
					t.Errorf("bin/%s = %q, want %q", file, got, want)
				}
			}
			if lastTotal <= 0 || lastDone != lastTotal {
				t.Errorf("progress ended at %d of %d bytes", lastDone, lastTotal)
			}
			if left := leftovers(t, installer); len(left) > 0 {
				t.Errorf("left behind %q", left)
			}
		})
	}
}

func TestToolInstallerRejectsAWrongHash(t *testing.T) {
	ts := newToolServer(t)
	ts.badHash[denoAsset] = true
	installer := newTestInstaller(t, ts)
	writeBinFile(t, installer, "deno", "old deno")
	tool, _ := findTool(toolDeno)

	_, err := installer.Install(context.Background(), tool, nil)

	if !errors.Is(err, errChecksumMismatch) {
		t.Fatalf("Install() error = %v, want errChecksumMismatch", err)
	}
	if got := binFile(installer, "deno"); got != "old deno" {
		t.Errorf("bin/deno = %q, want the installed one untouched", got)
	}
	if left := leftovers(t, installer); len(left) > 0 {
		t.Errorf("left behind %q", left)
	}
}

func TestToolInstallerPutsTheOldToolBackWhenAMoveFails(t *testing.T) {
	tests := []struct {
		name         string
		oldFFprobe   bool
		failRenameTo string // the rename to this bin/ file (with suffix) fails
	}{
		{"moving the new ffprobe in fails", true, exeName("ffprobe")},
		{"moving the old ffprobe aside fails", true, exeName("ffprobe") + ".old"},
		{"moving a first ffprobe in fails", false, exeName("ffprobe")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newToolServer(t)
			installer := newTestInstaller(t, ts)
			writeBinFile(t, installer, "ffmpeg", "old ffmpeg")
			if tt.oldFFprobe {
				writeBinFile(t, installer, "ffprobe", "old ffprobe")
			}
			installer.rename = func(from, to string) error {
				if filepath.Base(to) == tt.failRenameTo {
					return errors.New("access denied")
				}
				return os.Rename(from, to)
			}
			tool, _ := findTool(toolFFmpeg)

			_, err := installer.Install(context.Background(), tool, nil)

			if err == nil || !strings.Contains(err.Error(), "access denied") {
				t.Fatalf("Install() error = %v, want the failed move", err)
			}
			if got := binFile(installer, "ffmpeg"); got != "old ffmpeg" {
				t.Errorf("bin/ffmpeg = %q, want the old one back", got)
			}
			wantFFprobe := "<missing>"
			if tt.oldFFprobe {
				wantFFprobe = "old ffprobe"
			}
			if got := binFile(installer, "ffprobe"); got != wantFFprobe {
				t.Errorf("bin/ffprobe = %q, want %q", got, wantFFprobe)
			}
			if left := leftovers(t, installer); len(left) > 0 {
				t.Errorf("left behind %q", left)
			}
		})
	}
}

func TestToolInstallerUsesDownloadLinksWhenGitHubRateLimits(t *testing.T) {
	ts := newToolServer(t)
	ts.rateLimit = true
	installer := newTestInstaller(t, ts)

	for _, name := range []string{toolYtDlp, toolDeno} {
		tool, _ := findTool(name)
		if latest, err := installer.Latest(context.Background(), tool); err != nil || latest == "" {
			t.Errorf("Latest(%s) = %q, %v; want the tag /releases/latest redirects to", name, latest, err)
		}
		if _, err := installer.Install(context.Background(), tool, nil); err != nil {
			t.Fatalf("Install(%s) error = %v", name, err)
		}
	}
	if binFile(installer, "yt-dlp") != "new yt-dlp" || binFile(installer, "deno") != "new deno" {
		t.Errorf("bin/ holds yt-dlp %q and deno %q", binFile(installer, "yt-dlp"), binFile(installer, "deno"))
	}
}

func TestToolInstallerRefusesABinFolderItCannotWrite(t *testing.T) {
	ts := newToolServer(t)
	installer := newTestInstaller(t, ts)
	installer.writable = func(string) bool { return false }
	tool, _ := findTool(toolDeno)

	_, err := installer.Install(context.Background(), tool, nil)

	if err == nil || !strings.Contains(err.Error(), "cannot write to") {
		t.Errorf("Install() error = %v, want an unwritable bin folder", err)
	}
	if got := binFile(installer, "deno"); got != "<missing>" {
		t.Errorf("bin/deno = %q, want nothing installed", got)
	}
}

func TestRemoveOldTools(t *testing.T) {
	deps := &DependencyService{binDir: t.TempDir()}
	for _, name := range []string{"ffmpeg", "deno"} {
		for _, suffix := range []string{"", ".old", ".new"} {
			os.WriteFile(deps.LocalPath(name)+suffix, []byte("x"), 0644)
		}
	}

	restored := removeOldTools(deps)

	for _, name := range []string{"ffmpeg", "deno"} {
		if !fileExists(deps.LocalPath(name)) {
			t.Errorf("%s itself was removed", name)
		}
		for _, suffix := range []string{".old", ".new"} {
			if fileExists(deps.LocalPath(name) + suffix) {
				t.Errorf("%s%s was left", name, suffix)
			}
		}
	}
	if len(restored) > 0 {
		t.Errorf("restored %q, want nothing", restored)
	}
}

// GoVid stopped between swap's two renames: the tool exists only as .old,
// beside the staged .new.
func TestRemoveOldToolsRestoresAToolLeftOnlyAsOld(t *testing.T) {
	deps := &DependencyService{binDir: t.TempDir()}
	final := deps.LocalPath("ffprobe")
	os.WriteFile(final+".old", []byte("old ffprobe"), 0644)
	os.WriteFile(final+".new", []byte("new ffprobe"), 0644)

	restored := removeOldTools(deps)

	if got, _ := os.ReadFile(final); string(got) != "old ffprobe" {
		t.Errorf("ffprobe = %q, want the .old copy moved back", got)
	}
	if fileExists(final+".old") || fileExists(final+".new") {
		t.Error("a .old or .new file was left")
	}
	if len(restored) != 1 || restored[0] != final {
		t.Errorf("restored = %q, want [%q]", restored, final)
	}
}

// sha256Hex returns data's SHA-256 in lower case hex.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
