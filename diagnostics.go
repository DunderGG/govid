// diagnostics.go — Information for bug reports, and what a frozen UI
// leaves in the log.
//
// Responsibilities:
//   - diagnosticsReport: Help → Copy diagnostics: one text report with the
//     GoVid version and build, the OS, the tools and JavaScript runtime,
//     GPU detection, every setting, the queue, the goroutines and tool
//     processes running, and the last log lines. The user profile folder
//     and user name are replaced (%USERPROFILE%, %USERNAME%), and cookies
//     appear only as their source.
//   - trackTool / runningTools: how many yt-dlp and ffmpeg processes run.
//   - Heartbeat (startHeartbeat): with Debug Output on, one line every
//     heartbeatInterval in the session log: how long a round trip through
//     the UI thread took (a frozen UI shows up as a large number), the
//     goroutine count, the tools running, and the log lines waiting to be
//     shown.
//   - markLoop: with Debug Output on, start and stop markers of the
//     background loops (see the ticker-loop table in docs/architecture.md).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ── Tool processes ───────────────────────────────────────────────────────────

// runningToolCounts counts the yt-dlp and ffmpeg processes running.
var runningToolCounts = map[string]*atomic.Int32{
	toolYtDlp:  {},
	toolFFmpeg: {},
}

// trackTool counts a process of tool (toolYtDlp or toolFFmpeg) as running
// until the returned function is called.
func trackTool(tool string) (done func()) {
	counter := runningToolCounts[tool]
	counter.Add(1)
	var once sync.Once
	return func() { once.Do(func() { counter.Add(-1) }) }
}

// runningTools describes the tool processes running, e.g. "yt-dlp 2,
// ffmpeg 0".
func runningTools() string {
	return fmt.Sprintf("yt-dlp %d, ffmpeg %d", runningToolCounts[toolYtDlp].Load(), runningToolCounts[toolFFmpeg].Load())
}

// ── Loop markers ─────────────────────────────────────────────────────────────

// loopLogger receives loop markers while Debug Output is on; nil otherwise.
var loopLogger atomic.Pointer[func(line string)]

// markLoop records that a background loop (name) started or stopped
// (event), when Debug Output is on.
func markLoop(name, event string) {
	if logger := loopLogger.Load(); logger != nil {
		(*logger)(fmt.Sprintf("[DEBUG] Loop %s: %s (%d goroutines)", event, name, runtime.NumGoroutine()))
	}
}

// ── Heartbeat ────────────────────────────────────────────────────────────────

// heartbeatInterval is how often the heartbeat writes its line; a variable
// so tests can shorten it.
var heartbeatInterval = 10 * time.Second

// setDebug turns Debug Output on or off: yt-dlp's [debug] lines in the log
// view, the heartbeat, and the loop markers.
func (app *DownloaderApp) setDebug(on bool) {
	app.showDebug.Store(on)
	if on {
		logger := app.logSvc.WriteToFile
		loopLogger.Store(&logger)
		app.startHeartbeat()
		return
	}
	loopLogger.Store(nil)
	app.stopHeartbeat()
}

// startHeartbeat starts the heartbeat, unless it runs already.
func (app *DownloaderApp) startHeartbeat() {
	app.heartbeatMu.Lock()
	defer app.heartbeatMu.Unlock()
	if app.heartbeatStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	app.heartbeatStop = func() {
		cancel()
		<-done
	}
	interval := heartbeatInterval
	go func() {
		defer close(done)
		app.runHeartbeat(ctx, interval)
	}()
}

// stopHeartbeat stops the heartbeat, if it runs, and waits for it to end.
func (app *DownloaderApp) stopHeartbeat() {
	app.heartbeatMu.Lock()
	defer app.heartbeatMu.Unlock()
	if app.heartbeatStop != nil {
		app.heartbeatStop()
		app.heartbeatStop = nil
	}
}

// runHeartbeat writes a heartbeat line every interval until ctx
// ends. A UI thread that does not answer within an interval is reported
// as such, and the round trip once it does.
func (app *DownloaderApp) runHeartbeat(ctx context.Context, interval time.Duration) {
	markLoop("heartbeat", "started")
	defer markLoop("heartbeat", "stopped")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		roundTrip, ok := app.uiRoundTrip(ctx, interval)
		if !ok {
			return
		}
		app.logSvc.WriteToFile(fmt.Sprintf("[DEBUG] Heartbeat: UI round trip %v, %d goroutines, tools running: %s, log lines waiting %d",
			roundTrip.Round(time.Millisecond), runtime.NumGoroutine(), runningTools(), app.uiManager.pendingLogLines()))
	}
}

// uiRoundTrip measures how long a function sent to the UI thread takes to
// run. While it has not run, a warning is written every interval.
// ok is false when ctx ended first.
func (app *DownloaderApp) uiRoundTrip(ctx context.Context, interval time.Duration) (time.Duration, bool) {
	start := time.Now()
	answered := make(chan struct{})
	app.doOnUI(func() { close(answered) })
	for {
		select {
		case <-answered:
			return time.Since(start), true
		case <-ctx.Done():
			return 0, false
		case <-time.After(interval):
			app.logSvc.WriteToFile(fmt.Sprintf("[DEBUG] Heartbeat: the UI thread has not answered for %v", time.Since(start).Round(time.Second)))
		}
	}
}

// doOnUI runs fn on the UI thread: fyne.Do, or runOnUI when set (tests).
func (app *DownloaderApp) doOnUI(fn func()) {
	if app.runOnUI != nil {
		app.runOnUI(fn)
		return
	}
	fyne.Do(fn)
}

// ── Report ───────────────────────────────────────────────────────────────────

// diagnosticsLogLines is how many of the latest log lines the report holds.
const diagnosticsLogLines = 200

// anonymizer replaces what names the user in the report: the user profile
// folder, the user name, and the cookies file.
type anonymizer struct {
	replacements []replacement // longest first
}

// replacement is one thing the anonymizer replaces.
type replacement struct {
	old, new  string
	wholeWord bool // only where no letter or digit adjoins it (the user name)
}

// newAnonymizer returns an anonymizer for the user with home folder home
// and name username, which also hides cookiesPath.
func newAnonymizer(home, username, cookiesPath string) anonymizer {
	var replacements []replacement
	addPath := func(old, with string) {
		if strings.TrimSpace(old) == "" {
			return
		}
		// Paths also appear with the other slash, and with doubled
		// backslashes (as yt-dlp prints its command-line config).
		for _, form := range []string{old, strings.ReplaceAll(old, `\`, `\\`), strings.ReplaceAll(old, `\`, "/")} {
			replacements = append(replacements, replacement{old: form, new: with})
		}
	}
	addPath(cookiesPath, "<cookies file>")
	addPath(home, "%USERPROFILE%")
	if strings.TrimSpace(username) != "" {
		replacements = append(replacements, replacement{old: username, new: "%USERNAME%", wholeWord: true})
	}
	sort.SliceStable(replacements, func(i, j int) bool { return len(replacements[i].old) > len(replacements[j].old) })
	return anonymizer{replacements: replacements}
}

// currentAnonymizer returns the anonymizer for the user running GoVid.
func currentAnonymizer(cookiesPath string) anonymizer {
	home, _ := os.UserHomeDir()
	username := os.Getenv("USERNAME")
	if current, err := user.Current(); err == nil {
		username = filepath.Base(strings.ReplaceAll(current.Username, `\`, "/")) // DOMAIN\name on Windows
	}
	return newAnonymizer(home, username, cookiesPath)
}

// apply returns text with every replacement made, ignoring case, as
// Windows paths do.
func (anon anonymizer) apply(text string) string {
	for _, r := range anon.replacements {
		text = replaceFold(text, r)
	}
	return text
}

// replaceFold makes r in text, matching r.old in any case.
func replaceFold(text string, r replacement) string {
	lowerOld := strings.ToLower(r.old)
	var out strings.Builder
	for {
		index := strings.Index(strings.ToLower(text), lowerOld)
		if index < 0 {
			out.WriteString(text)
			return out.String()
		}
		end := index + len(r.old)
		if r.wholeWord && (adjoinsWord(text, index-1) || adjoinsWord(text, end)) {
			out.WriteString(text[:end])
			text = text[end:]
			continue
		}
		out.WriteString(text[:index])
		out.WriteString(r.new)
		text = text[end:]
	}
}

// adjoinsWord reports whether text has a letter or digit at index.
func adjoinsWord(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return false
	}
	c := text[index]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// diagnosticsSettings returns p as govid.json would hold it, with cookies
// shown only as their source.
func diagnosticsSettings(p AppPreferences) string {
	cookies := cookieLabel(p.CookieSource, p.CookieBrowser, p.CookieProfile, p.CookiesPath)
	p.CookiesPath = ""
	if p.CookieProfile != "" {
		p.CookieProfile = "(set)"
	}
	data, err := jsonIndent(configFromPreferences(p))
	if err != nil {
		return fmt.Sprintf("(could not list the settings: %v)", err)
	}
	return data + "\ncookies: " + cookies
}

// diagnosticsReport builds the Copy diagnostics report. It runs the tools
// for their versions, so call it off the UI thread.
func (app *DownloaderApp) diagnosticsReport(prefs AppPreferences) string {
	var report strings.Builder
	section := func(title string) { fmt.Fprintf(&report, "\n== %s ==\n", title) }

	fmt.Fprintf(&report, "GoVid diagnostics, %s\n", time.Now().Format("2006-01-02 15:04:05"))
	section("GoVid")
	buildTypeText := buildType
	if buildTypeText == "" {
		buildTypeText = "development"
	}
	fmt.Fprintf(&report, "version: %s\nbuild: %s\nGo: %s\n", version, buildTypeText, runtime.Version())

	section("System")
	fmt.Fprintf(&report, "OS: %s/%s, %d CPUs\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())

	section("Tools")
	for _, name := range []string{toolYtDlp, toolFFmpeg, "ffprobe"} {
		tool := app.depSvc.Installed(name)
		switch {
		case !tool.Found():
			fmt.Fprintf(&report, "%s: not found\n", name)
		case tool.Version == "":
			fmt.Fprintf(&report, "%s: does not run (%s)\n", name, tool.Source())
		default:
			fmt.Fprintf(&report, "%s: %s (%s)\n", name, tool.Version, tool.Source())
		}
	}
	fmt.Fprintf(&report, "JavaScript runtime: %s\n", app.jsRuntimeLabel())
	for _, note := range app.depSvc.JSRuntimeNotes() {
		fmt.Fprintf(&report, "  %s\n", note)
	}

	section("GPU")
	for _, line := range FormatGPUDiagnostics(app.gpuSvc.Detect(context.Background())) {
		fmt.Fprintln(&report, line)
	}

	section("Settings")
	fmt.Fprintln(&report, diagnosticsSettings(prefs))

	section("Queue")
	if queue := app.queue.Load(); queue != nil {
		running := "finished"
		if app.isRunning.Load() {
			running = "running"
		}
		fmt.Fprintf(&report, "%s (session %s)\n", queue.Summary(), running)
	} else {
		fmt.Fprintln(&report, "no session yet")
	}

	section("Runtime")
	fmt.Fprintf(&report, "goroutines: %d\ntools running: %s\nlog lines waiting to be shown: %d\n",
		runtime.NumGoroutine(), runningTools(), app.uiManager.pendingLogLines())

	section(fmt.Sprintf("Log (last %d lines)", diagnosticsLogLines))
	for _, line := range app.logSvc.Recent(diagnosticsLogLines) {
		fmt.Fprintln(&report, line)
	}

	return currentAnonymizer(prefs.CookiesPath).apply(report.String())
}

// jsonIndent returns value as indented JSON.
func jsonIndent(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	return string(data), err
}

// copyDiagnostics builds the diagnostics report off the UI thread, copies
// it to the clipboard, and offers to save it as a file. Must be called on
// the UI thread.
func (app *DownloaderApp) copyDiagnostics() {
	prefs := snapshotPreferences(app.ui, app.ui.download.path.Text)
	app.updateStatus("Status: Gathering diagnostics…")
	go func() {
		report := app.diagnosticsReport(prefs)
		app.updateStatus("Status: Diagnostics copied.")
		fyne.Do(func() {
			fyne.CurrentApp().Clipboard().SetContent(report)
			app.uiManager.showDiagnosticsCopied(report)
		})
	}()
}

// showDiagnosticsCopied says the report was copied and offers to save it.
// Must be called on the UI thread.
func (manager *UIManager) showDiagnosticsCopied(report string) {
	lines := strings.Count(report, "\n")
	message := widget.NewLabel(fmt.Sprintf("The diagnostics report (%d lines) is on the clipboard; paste it into your bug report.\n\n"+
		"It holds GoVid's and the tools' versions, your settings, the queue, and the last %d log lines. "+
		"Your user folder and user name are replaced, and cookies are shown only as their source.", lines, diagnosticsLogLines))
	message.Wrapping = fyne.TextWrapWord
	var dlg *dialog.CustomDialog
	saveBtn := widget.NewButton("Save as file…", func() {
		dlg.Hide()
		manager.saveDiagnostics(report)
	})
	dlg = dialog.NewCustomWithoutButtons("Diagnostics Copied", message, manager.mainWindow)
	dlg.SetButtons([]fyne.CanvasObject{saveBtn, widget.NewButton("OK", func() { dlg.Hide() })})
	dlg.Resize(fyne.NewSize(480, 0))
	dlg.Show()
}

// saveDiagnostics asks where to save report and writes it there.
func (manager *UIManager) saveDiagnostics(report string) {
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, manager.mainWindow)
			return
		}
		if writer == nil {
			return // cancelled
		}
		defer writer.Close()
		if _, err := writer.Write([]byte(report)); err != nil {
			dialog.ShowError(fmt.Errorf("could not save the diagnostics: %w", err), manager.mainWindow)
		}
	}, manager.mainWindow)
	picker.SetFileName(fmt.Sprintf("GoVid_diagnostics_%s.txt", time.Now().Format("2006-01-02")))
	picker.Show()
}
