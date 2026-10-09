// components.go — Checking and installing GoVid's tools from the app.
//
// Responsibilities:
//   - componentStatus / componentStatuses: each installable tool's installed
//     version and where it was found, and its latest version, for
//     Tools → Components.
//   - installComponent: the Components window's Install / Update /
//     Reinstall buttons and the notices' Install button. It runs
//     ToolInstaller (or "yt-dlp -U" for yt-dlp's Update) with progress in
//     the status label, then puts the new tool to use (afterInstall).
//   - checkTools: the startup check, which shows a notice with Install for
//     a missing yt-dlp, FFmpeg, or JavaScript runtime.
//   - checkFFmpegFilters / missingFilters: whether an FFmpeg build has the
//     filters HDR to SDR tone mapping needs.
//
// The Components window itself is in components_window.go.
package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// The action a Components row offers.
const (
	componentInstall   = "Install"
	componentUpdate    = "Update"
	componentReinstall = "Reinstall"
)

// componentsCheckTimeout bounds how long the Components window waits for
// the tools' versions and the latest releases.
const componentsCheckTimeout = 30 * time.Second

// Errors installComponent and updateYtDlp report instead of starting.
var (
	errToolsInUse     = errors.New("a download is running and is using the tools; try again when it has finished")
	errAlreadyInstall = errors.New("another tool is being installed or updated; wait for it to finish")
)

// canInstallTools reports whether the Components window can install tools
// here: the downloads are Windows builds, so elsewhere the package manager
// does it.
func canInstallTools() bool {
	return runtime.GOOS == "windows"
}

// componentStatus is what the Components window shows about one tool.
type componentStatus struct {
	name, label string
	installed   InstalledTool
	noFFprobe   bool   // FFmpeg only: ffprobe was not found
	latest      string // "" when unknown
}

// action returns the button the tool's row offers. A tool that is missing,
// or found only on PATH, is installed into bin/, which takes precedence. A
// tool in bin/ older than the latest release is updated; otherwise it can
// be reinstalled, which repairs a broken copy.
func (status componentStatus) action() string {
	switch {
	case !status.installed.InBin:
		return componentInstall
	case status.installed.Version != "" && status.latest != "" && isOlderVersion(status.installed.Version, status.latest):
		return componentUpdate
	default:
		return componentReinstall
	}
}

// installedText describes the installed tool, e.g. "2026.03.17 (bin/)".
func (status componentStatus) installedText() string {
	if !status.installed.Found() {
		return "not installed"
	}
	version := status.installed.Version
	if version == "" {
		version = "does not run"
	}
	text := fmt.Sprintf("%s (%s)", version, status.installed.Source())
	if status.noFFprobe {
		text += ", without ffprobe"
	}
	return text
}

// latestText describes the latest release.
func (status componentStatus) latestText() string {
	if status.latest == "" {
		return unknownVersion
	}
	return status.latest
}

// componentStatuses finds every installable tool and its latest release,
// all at once. It runs the tools and asks the network, so call it off the
// UI thread.
func (app *DownloaderApp) componentStatuses() []componentStatus {
	ctx, cancel := context.WithTimeout(context.Background(), componentsCheckTimeout)
	defer cancel()

	statuses := make([]componentStatus, len(installableTools))
	var wg sync.WaitGroup
	for i, tool := range installableTools {
		wg.Go(func() {
			status := componentStatus{name: tool.name, label: tool.label, installed: app.depSvc.Installed(tool.name)}
			if tool.name == toolFFmpeg {
				_, _, hasFFprobe := app.depSvc.locate("ffprobe")
				status.noFFprobe = status.installed.Found() && !hasFFprobe
			}
			if latest, err := app.toolInstaller.Latest(ctx, tool); err == nil {
				status.latest = latest
			} else {
				app.logSvc.WriteToFile(fmt.Sprintf("[SYSTEM] Could not find the latest %s: %v", tool.label, err))
			}
			statuses[i] = status
		})
	}
	wg.Wait()
	return statuses
}

// installComponent runs a Components action for the tool called name:
// yt-dlp's Update runs "yt-dlp -U" through updateYtDlp, and everything else
// downloads the tool's latest release into bin/. It refuses while a
// download session or another install runs, since the tools would be in
// use. onDone is called on the UI thread when the action has finished, with
// nil on success. Must be called on the UI thread.
func (app *DownloaderApp) installComponent(name, action string, onDone func(err error)) {
	finish := func(err error) { fyne.Do(func() { onDone(err) }) }
	tool, ok := findTool(name)
	switch {
	case !ok:
		finish(fmt.Errorf("unknown tool %q", name))
		return
	case name == toolYtDlp && action == componentUpdate:
		app.updateYtDlp(onDone)
		return
	case app.isRunning.Load():
		finish(errToolsInUse)
		return
	case !app.installing.CompareAndSwap(false, true):
		finish(errAlreadyInstall)
		return
	}

	app.ui.download.downloadBtn.Disable()
	app.setStatusIndicator(StatusActive)
	app.appendOutput(fmt.Sprintf("[SYSTEM] Installing %s into bin/…", tool.label), colSystem)
	go func() {
		version, err := app.toolInstaller.Install(context.Background(), tool, app.toolProgress(tool))
		app.installing.Store(false)
		fyne.Do(func() { app.ui.download.downloadBtn.Enable() })
		if err != nil {
			app.appendOutput(fmt.Sprintf("[ERROR] Installing %s failed: %v", tool.label, err), colError)
			app.updateStatus(fmt.Sprintf("Status: Installing %s failed.", tool.label))
			app.setStatusIndicator(StatusFailed)
			finish(err)
			return
		}
		if version == "" {
			version = "the latest version"
		}
		app.appendOutput(fmt.Sprintf("[SYSTEM] Installed %s %s in bin/.", tool.label, version), colSuccess)
		app.updateStatus(fmt.Sprintf("Status: %s installed.", tool.label))
		app.setStatusIndicator(StatusSuccess)
		app.afterInstall(tool)
		finish(nil)
	}()
}

// updateYtDlp runs "yt-dlp -U". Every Update yt-dlp action goes through it:
// the Tools menu, the out-of-date notice (both via UIManager.runUpdateInUI),
// and Components. Like an install, it refuses while a download session or
// another install runs, and holds installing, with the Download button
// disabled, until the update has finished, since Windows cannot replace a
// yt-dlp.exe that is running. onDone is called on the UI thread when the
// update has finished, with an error only when it refused to start; the
// update's own outcome is in the log and the status label. Must be called
// on the UI thread.
func (app *DownloaderApp) updateYtDlp(onDone func(err error)) {
	finish := func(err error) { fyne.Do(func() { onDone(err) }) }
	switch {
	case app.isRunning.Load():
		finish(errToolsInUse)
		return
	case !app.installing.CompareAndSwap(false, true):
		finish(errAlreadyInstall)
		return
	}

	app.ui.download.downloadBtn.Disable()
	app.uiManager.runUpdateThen(func(bool) {
		app.installing.Store(false)
		fyne.Do(func() { app.ui.download.downloadBtn.Enable() })
		finish(nil)
	})
}

// toolProgress returns a progress handler that shows a tool download's
// progress in the status label.
func (app *DownloaderApp) toolProgress(tool toolSpec) func(done, total int64) {
	return func(done, total int64) {
		if total > 0 {
			app.updateStatus(fmt.Sprintf("Status: Downloading %s… %d%% of %s", tool.label, done*100/total, formatBytes(total)))
		} else {
			app.updateStatus(fmt.Sprintf("Status: Downloading %s… %s", tool.label, formatBytes(done)))
		}
	}
}

// afterInstall puts a newly installed tool to use: the notices about it go,
// a new runtime is looked for, and for a new FFmpeg the GPU encoders are
// detected again and the HDR filters checked.
func (app *DownloaderApp) afterInstall(tool toolSpec) {
	app.uiManager.dismissNotice(missingToolNoticeID(tool.name))
	switch tool.name {
	case toolYtDlp:
		app.uiManager.dismissNotice(ytDlpNoticeID)
	case toolFFmpeg:
		app.gpuSvc.Reset(app.depSvc.Resolve(toolFFmpeg))
		app.startGPUDetection()
		app.checkFFmpegFilters()
	case toolDeno:
		app.depSvc.ResetJSRuntime()
		if rt, ok := app.depSvc.JSRuntime(); ok {
			app.appendOutput("[SYSTEM] YouTube downloads now use the JavaScript runtime "+rt.Label()+".", colSystem)
			app.uiManager.dismissNotice(jsRuntimeNoticeID)
			return
		}
		for _, note := range app.depSvc.JSRuntimeNotes() {
			app.appendOutput("[WARNING] JavaScript runtime: "+note, colWarning)
		}
	}
}

// hdrToneMapFilters are the ffmpeg filters HDR to SDR tone mapping uses.
var hdrToneMapFilters = []string{"zscale", "tonemap"}

// ffmpegFiltersNoticeID identifies the notice that the installed FFmpeg
// cannot tone map HDR.
const ffmpegFiltersNoticeID = "ffmpeg-filters"

// checkFFmpegFilters checks that the installed ffmpeg still has the filters
// HDR to SDR needs, and says so in the log and a notice when it does not.
func (app *DownloaderApp) checkFFmpegFilters() {
	ctx, cancel := context.WithTimeout(context.Background(), toolCommandTimeout)
	defer cancel()
	out, err := newToolCommand(ctx, app.depSvc.Resolve(toolFFmpeg), "-hide_banner", "-filters").Output()
	if err != nil {
		err = commandError(ctx, err, toolCommandTimeout)
		app.appendOutput(fmt.Sprintf("[WARNING] Could not list FFmpeg's filters: %v", err), colWarning)
		return
	}
	missing := missingFilters(string(out), hdrToneMapFilters)
	if len(missing) == 0 {
		app.logSvc.WriteToFile("[SYSTEM] FFmpeg has the zscale and tonemap filters HDR to SDR needs.")
		return
	}
	message := fmt.Sprintf("The installed FFmpeg lacks the %s filter(s), so HDR to SDR post-processing will fail.", strings.Join(missing, " and "))
	app.appendOutput("[WARNING] "+message, colWarning)
	app.uiManager.showNotice(notice{id: ffmpegFiltersNoticeID, text: message})
}

// missingFilters returns the names among wanted that the output of
// "ffmpeg -filters" does not list. Each filter is a line such as
// " TSC zscale            V->V       Apply resizing, …".
func missingFilters(filtersOutput string, wanted []string) []string {
	listed := map[string]bool{}
	for _, line := range strings.Split(filtersOutput, "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 {
			listed[fields[1]] = true
		}
	}
	var missing []string
	for _, name := range wanted {
		if !listed[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// ── Startup check and notices ────────────────────────────────────────────────

// jsRuntimeNoticeID identifies the "no JavaScript runtime" notice.
const jsRuntimeNoticeID = "js-runtime"

// missingToolNoticeID identifies the notice about a missing tool.
func missingToolNoticeID(name string) string {
	return "missing-" + name
}

// checkTools looks, in the background, for yt-dlp, FFmpeg, and a
// JavaScript runtime, and shows a notice with Install for each one that is
// missing. Which runtime YouTube downloads will use goes to the log file.
func (app *DownloaderApp) checkTools() {
	go app.checkToolsNow()
}

// checkToolsNow is checkTools, run on the calling goroutine.
func (app *DownloaderApp) checkToolsNow() {
	if !app.depSvc.available(toolYtDlp) {
		app.showToolNotice(missingToolNoticeID(toolYtDlp), "yt-dlp was not found. GoVid needs it to download anything.", toolYtDlp)
	}
	if !app.depSvc.available(toolFFmpeg) {
		app.showToolNotice(missingToolNoticeID(toolFFmpeg), "FFmpeg was not found. GoVid needs it to merge video and audio, convert formats, and post-process.", toolFFmpeg)
	}

	for _, note := range app.depSvc.JSRuntimeNotes() {
		app.logSvc.WriteToFile("[SYSTEM] JavaScript runtime: " + note)
	}
	if rt, ok := app.depSvc.JSRuntime(); ok {
		app.logSvc.WriteToFile("[SYSTEM] JavaScript runtime for YouTube: " + rt.Label())
		return
	}
	app.appendOutput("[WARNING] No JavaScript runtime (Deno, Node, or Bun) found. YouTube downloads may miss formats.", colWarning)
	app.showJSRuntimeNotice()
}

// showJSRuntimeNotice shows the notice offering to install Deno, because
// yt-dlp has no JavaScript runtime for YouTube's player challenges.
func (app *DownloaderApp) showJSRuntimeNotice() {
	app.showToolNotice(jsRuntimeNoticeID,
		"No JavaScript runtime was found. YouTube needs one (such as Deno) to list every format; without it some formats are missing, and YouTube downloads will stop working when YouTube removes the fallback yt-dlp uses.",
		toolDeno)
}

// showToolNotice shows a notice about the tool called name. Where GoVid
// can install tools, its button installs it; elsewhere the notice says to
// use the package manager.
func (app *DownloaderApp) showToolNotice(id, text, name string) {
	n := notice{id: id, text: text}
	if canInstallTools() {
		n.actionLabel = componentInstall
		n.action = func() { app.installComponent(name, componentInstall, app.reportInstallError) }
	} else {
		n.text += " Install it with your package manager."
	}
	app.uiManager.showNotice(n)
}

// reportInstallError shows why an install from a notice failed. Must be
// called on the UI thread.
func (app *DownloaderApp) reportInstallError(err error) {
	if err != nil {
		dialog.ShowError(err, app.window)
	}
}

// jsRuntimeLabel describes the JavaScript runtime downloads use, for the
// session log and the About window, e.g. "deno 2.9.7 (bin/)". The first
// call may run the candidates, so call it off the UI thread.
func (app *DownloaderApp) jsRuntimeLabel() string {
	if rt, ok := app.depSvc.JSRuntime(); ok {
		return rt.Label()
	}
	return "none (YouTube may miss formats)"
}
