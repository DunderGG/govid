package main

import (
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestCookieArgsForEachSource(t *testing.T) {
	cookies := filepath.Join(t.TempDir(), "cookies.txt")
	touch(t, cookies)
	engine := NewDownloadEngine("yt-dlp", "")
	tests := []struct {
		name string
		req  DownloadRequest
		want []string
	}{
		{"none", DownloadRequest{}, nil},
		{"file", DownloadRequest{CookiesPath: cookies}, []string{"--cookies", cookies}},
		{"missing file", DownloadRequest{CookiesPath: filepath.Join(t.TempDir(), "gone.txt")}, nil},
		{"browser", DownloadRequest{CookiesFromBrowser: "firefox"}, []string{"--cookies-from-browser", "firefox"}},
		{"browser and profile", DownloadRequest{CookiesFromBrowser: "firefox:work"}, []string{"--cookies-from-browser", "firefox:work"}},
		{"browser wins over a file", DownloadRequest{CookiesPath: cookies, CookiesFromBrowser: "edge"}, []string{"--cookies-from-browser", "edge"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.req.URL, tt.req.SavePath, tt.req.Format = "https://example.com/v", "s", formatMP4
			for name, args := range map[string][]string{
				"download": engine.BuildArgs(tt.req).Args,
				"probe":    engine.probeArgs(tt.req, false),
			} {
				got := cookieFlags(args)
				if !slices.Equal(got, tt.want) {
					t.Errorf("%s cookie args = %q, want %q", name, got, tt.want)
				}
			}
		})
	}
}

// cookieFlags returns the cookie options in args, with their values.
func cookieFlags(args []string) []string {
	var flags []string
	for _, flag := range []string{"--cookies", "--cookies-from-browser"} {
		if i := slices.Index(args, flag); i >= 0 {
			flags = append(flags, flag, args[i+1])
		}
	}
	return flags
}

func TestCookiesFromBrowserAndItsLabel(t *testing.T) {
	if got := cookiesFromBrowser(browserFirefox, "  work "); got != "firefox:work" {
		t.Errorf("cookiesFromBrowser() = %q", got)
	}
	if got := cookiesFromBrowser(browserEdge, ""); got != "edge" {
		t.Errorf("cookiesFromBrowser() = %q", got)
	}
	if got := browserLabel("firefox:work"); got != browserFirefox {
		t.Errorf("browserLabel() = %q", got)
	}
}

func TestCookieLabelNeverShowsThePath(t *testing.T) {
	home := t.TempDir()
	cookies := filepath.Join(home, "jane.doe", "youtube_cookies.txt")
	os.MkdirAll(filepath.Dir(cookies), 0755)
	touch(t, cookies)
	tests := []struct {
		source, browser, profile, path string
		want                           string
	}{
		{cookieSourceNone, browserFirefox, "", cookies, "none"},
		{cookieSourceFile, browserFirefox, "", cookies, "file set"},
		{cookieSourceFile, browserFirefox, "", filepath.Join(home, "gone.txt"), "file set, but missing"},
		{cookieSourceBrowser, browserFirefox, "", cookies, "Firefox"},
		{cookieSourceBrowser, browserChrome, "jane", "", "Chrome, profile set"},
	}
	for _, tt := range tests {
		got := cookieLabel(tt.source, tt.browser, tt.profile, tt.path)
		if got != tt.want {
			t.Errorf("cookieLabel(%s, %s) = %q, want %q", tt.source, tt.browser, got, tt.want)
		}
		if strings.Contains(got, "jane") || strings.Contains(got, home) {
			t.Errorf("cookieLabel() = %q leaks the path or profile", got)
		}
	}
}

func TestClassifyAccessError(t *testing.T) {
	tests := []struct {
		line string
		want accessProblem
	}{
		// Recorded from the bundled yt-dlp 2026.03.17 on Windows.
		{"ERROR: Could not copy Chrome cookie database. See  https://github.com/yt-dlp/yt-dlp/issues/7271  for more info", cookiesLocked},
		{"ERROR: Failed to decrypt with DPAPI. See  https://github.com/yt-dlp/yt-dlp/issues/10927  for more info", cookiesAppBound},
		{`ERROR: could not find firefox cookies database in 'C:\\Users\\x\\AppData\\Roaming\\Mozilla\\Firefox\\Profiles\\work'`, cookiesNoProfile},
		{"ERROR: [youtube] jNQXAC9IVRw: Sign in to confirm you’re not a bot. Use --cookies-from-browser or --cookies for the authentication.", accessBotCheck},
		// yt-dlp's other YouTube messages.
		{"ERROR: [youtube] abc: Sign in to confirm your age. This video may be inappropriate for some users.", accessAgeRestricted},
		{"ERROR: [youtube] abc: Join this channel to get access to members-only content like this video, and other exclusive perks.", accessMembersOnly},
		{"ERROR: [youtube] abc: Private video. Sign in if you've been granted access to this video", accessPrivate},
		{"ERROR: Unsupported URL: https://example.com", accessOK},
		{"WARNING: [youtube] Sign in to confirm you’re not a bot", accessOK},
	}
	for _, tt := range tests {
		if got := classifyAccessError(tt.line); got != tt.want {
			t.Errorf("classifyAccessError(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestAccessHintsSayWhatToDo(t *testing.T) {
	chrome := DownloadRequest{CookiesFromBrowser: "chrome"}
	file := DownloadRequest{CookiesPath: "cookies.txt"}
	none := DownloadRequest{}
	tests := []struct {
		name    string
		problem accessProblem
		req     DownloadRequest
		want    []string
	}{
		{"locked", cookiesLocked, chrome, []string{"Close Chrome", "Firefox", "cookies.txt", cookiesSetting}},
		{"app-bound", cookiesAppBound, chrome, []string{"Chrome encrypts", "Firefox", "cookies.txt", cookiesSetting}},
		{"no profile", cookiesNoProfile, DownloadRequest{CookiesFromBrowser: "firefox:work"}, []string{"no Firefox cookies", "profile name"}},
		{"bot check, no cookies", accessBotCheck, none, []string{"not a bot", "From browser", cookiesSetting, "Update yt-dlp"}},
		{"bot check, browser cookies", accessBotCheck, chrome, []string{"passed Chrome's cookies", "signed in"}},
		{"age, old file", accessAgeRestricted, file, []string{"age-restricted", "cookies file", "Export it again"}},
		{"members", accessMembersOnly, none, []string{"members", "a member's account"}},
		{"private", accessPrivate, none, []string{"private", "shared"}},
	}
	for _, tt := range tests {
		hint := accessHint(tt.problem, tt.req)
		for _, want := range tt.want {
			if !strings.Contains(hint, want) {
				t.Errorf("%s: hint %q does not mention %q", tt.name, hint, want)
			}
		}
	}
	if hint := accessHint(accessOK, none); hint != "" {
		t.Errorf("accessHint(accessOK) = %q, want none", hint)
	}
}

func TestLoadKeepsAnExistingCookiesFile(t *testing.T) {
	for _, tt := range []struct {
		path string
		want string
	}{
		{"", cookieSourceNone},
		{"C:/cookies.txt", cookieSourceFile},
	} {
		store := test.NewApp().Preferences()
		store.SetString(prefCookiesPath, tt.path)

		got := NewPreferenceService(store).Load()

		if got.CookieSource != tt.want || got.CookieBrowser != browserFirefox {
			t.Errorf("cookiesPath %q: source %q, browser %q; want %q, Firefox", tt.path, got.CookieSource, got.CookieBrowser, tt.want)
		}
	}
}

func TestBrowserCookiesFailureExplainsWhatToDo(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-cookies-locked")
	runs := useFakeArgs(t)
	h.app.ui.prefs.cookieSource.SetSelected(cookieSourceBrowser)
	h.app.ui.prefs.cookieBrowser.SetSelected(browserChrome)
	h.app.ui.download.entry.SetText("https://www.youtube.com/watch?v=x")

	h.startAndWait(t)

	for _, args := range runs() {
		if got := argAfter(args, "--cookies-from-browser"); got != "chrome" {
			t.Errorf("--cookies-from-browser = %q in %q, want chrome", got, args)
		}
	}
	logs := h.joinedLogs()
	if !strings.Contains(logs, "Close Chrome completely") || strings.Contains(logs, ytDlpUpdateHint) {
		t.Errorf("log does not explain the locked cookies:\n%s", logs)
	}
}

func TestBotCheckSuggestsCookies(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-bot-check")
	h.app.ui.prefs.cookieSource.SetSelected(cookieSourceNone)
	h.app.ui.download.entry.SetText("https://www.youtube.com/watch?v=x")

	h.startAndWait(t)

	if logs := h.joinedLogs(); !strings.Contains(logs, "not a bot") || !strings.Contains(logs, "Choose From browser") {
		t.Errorf("log does not suggest cookies:\n%s", logs)
	}
}

func TestSessionLogNamesOnlyTheCookieSource(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	cookies := filepath.Join(t.TempDir(), "private", "cookies.txt")
	os.MkdirAll(filepath.Dir(cookies), 0755)
	touch(t, cookies)
	h.app.ui.prefs.cookies.SetText(cookies)
	h.app.ui.prefs.cookieSource.SetSelected(cookieSourceFile)

	cfg := newSessionConfig(h.app.ui, []string{"u"}, h.saveDir, "", "")
	var lines []string
	h.app.logSvc.WriteSessionConfig(cfg, func(line string, _ color.Color) { lines = append(lines, line) })

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[SYSTEM] Cookies: file set") || strings.Contains(joined, cookies) {
		t.Errorf("session config:\n%s", joined)
	}
}

// yt-dlp runs with --verbose, and its "[debug] Command-line config" line
// named the cookies file in the session log, which the help says it never
// does (CR-16).
func TestSessionLogHidesTheCookiesFilePath(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	runs := useFakeArgs(t)
	h.app.showDebug.Store(true) // the [debug] line also reaches the view
	folder := filepath.Join(t.TempDir(), "PrivateCookieFolder")
	cookies := filepath.Join(folder, "cookies.txt")
	os.MkdirAll(folder, 0755)
	touch(t, cookies)
	h.app.ui.prefs.cookies.SetText(cookies)
	h.app.ui.prefs.cookieSource.SetSelected(cookieSourceFile)
	h.app.ui.download.saveLog.SetChecked(true)
	h.app.ui.download.entry.SetText("https://example.com/v")

	h.startAndWait(t)

	passed := false
	for _, args := range runs() {
		passed = passed || argAfter(args, "--cookies") == cookies
	}
	if !passed {
		t.Fatalf("yt-dlp was not passed the cookies file: %q", runs())
	}
	for name, text := range map[string]string{
		"session log": readFile(t, SessionLogPath(h.saveDir)),
		"log view":    h.joinedLogs(),
	} {
		if !strings.Contains(text, "'--cookies', '<cookies file>'") {
			t.Errorf("%s does not show the hidden cookies file:\n%s", name, text)
		}
		if strings.Contains(strings.ToLower(text), "privatecookiefolder") {
			t.Errorf("%s names the cookies file's folder:\n%s", name, text)
		}
	}
}

func TestCookiesPathMask(t *testing.T) {
	var mask cookiesPathMask
	line := `[debug] Command-line config: ['--cookies', 'C:\\Jane\\old.txt', '--cookies', 'C:\\Jane\\new.txt']`
	if got := mask.apply(line); got != line {
		t.Errorf("an empty mask changed the line to %q", got)
	}

	mask.add(`C:\Jane\old.txt`)
	mask.add(`C:\Jane\new.txt`)
	mask.add(`C:\Jane\new.txt`)
	mask.add("  ")

	want := `[debug] Command-line config: ['--cookies', '<cookies file>', '--cookies', '<cookies file>']`
	if got := mask.apply(line); got != want {
		t.Errorf("apply() = %q, want %q", got, want)
	}
	if got := mask.apply(`ERROR: c:/jane/OLD.txt is not a Netscape cookies file`); got != "ERROR: <cookies file> is not a Netscape cookies file" {
		t.Errorf("apply() = %q, want the forward-slash path hidden in any case", got)
	}
	if len(mask.paths) != 2 {
		t.Errorf("mask keeps %q, want the two paths once each", mask.paths)
	}
}

func TestCookiesRowShowsTheChosenSource(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	row := h.app.uiManager.buildCookiesRow().(*fyne.Container)
	fileRow, browserRow := row.Objects[1], row.Objects[2]

	for _, tt := range []struct {
		source        string
		file, browser bool
	}{
		{cookieSourceNone, false, false},
		{cookieSourceFile, true, false},
		{cookieSourceBrowser, false, true},
	} {
		h.app.ui.prefs.cookieSource.SetSelected(tt.source)
		if fileRow.Visible() != tt.file || browserRow.Visible() != tt.browser {
			t.Errorf("%s: file row shown %v, browser row shown %v; want %v, %v", tt.source, fileRow.Visible(), browserRow.Visible(), tt.file, tt.browser)
		}
	}
}
