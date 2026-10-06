// ui_manager.go — Main window layout and singleton secondary window management.
//
// Responsibilities:
//   - UIManager: typed component that owns the primary window reference and
//     the secondary window references (About, Help, History, Preferences,
//     Post-Processing), ensuring at most one instance of each is open at a time.
//   - createUI: composes the main window layout (header, input card, status
//     card, log pane, footer) from focused builder/wiring helpers below it.
//   - createMainMenu: builds the main window's menu bar.
//   - showAbout, showHistory, showConfigHelp, showPreferences,
//     showPostProcessing: self-contained window construction, built from
//     UIManager's own widget field and injected service callbacks.
//   - savePreferences, restoreDefaults: preference persistence and the
//     "Restore Defaults" reset used by showPreferences.
//   - checkDependencies, confirmYtDlpUpdate, runUpdateInUI: thin delegates to the injected
//     dependency-service callbacks for the startup tool check and the
//     "Update yt-dlp" menu action.
//   - appendLogLine, flushLog: batched rendering of the Terminal Output log
//     view, capped at maxScreenLogLines and following new lines only while
//     the view is scrolled to the bottom.
//   - showNotice, dismissNotice: non-blocking notices above the input card,
//     such as "a newer yt-dlp is available".
package main

import (
	"fmt"
	"image/color"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// UIManager owns the main window and every secondary window, ensuring each
// secondary window is a singleton — at most one instance open at a time. It holds
// no service-type references directly; all service access is bridged in via
// callbacks so UIManager stays decoupled from the service implementations.
type UIManager struct {
	mainWindow    fyne.Window // primary application window; content is built by createUI
	aboutWindow   fyne.Window
	helpWindow    fyne.Window
	historyWindow fyne.Window
	prefsWindow   fyne.Window
	ppWindow      fyne.Window
	ui            *UIWidgets // shared widget bag; set by newDownloaderApp after construction

	// restoringDefaults suppresses savePreferences while restoreDefaults is
	// writing default values into the widgets. Only touched on the UI goroutine.
	restoringDefaults bool

	// Log lines queued by appendLogLine and not yet rendered; see flushLog.
	logMu         sync.Mutex
	pendingLog    []pendingLogLine
	logFlushArmed bool                                    // true while a flush timer is pending
	afterFunc     func(time.Duration, func()) *time.Timer // schedules the flush; time.AfterFunc, replaced in tests

	// Notices shown above the input card; see showNotice. Only touched on
	// the UI thread.
	notices   []notice
	noticeBox *fyne.Container

	// Callbacks bridging DownloaderApp actions and services into the main
	// window and secondary windows; all set by newDownloaderApp after
	// construction.
	onLog                func(line string, col color.Color)                                                                          // appends a line to the terminal output panel
	onStatus             func(msg string)                                                                                            // updates the short status label
	onSetStatusIndicator func(state StatusState)                                                                                     // updates the status dot color
	onStartDownload      func()                                                                                                      // begins a download/batch run
	onOpenFolder         func()                                                                                                      // opens the save destination in the system file manager
	onRequestCancel      func() bool                                                                                                 // cancels the active download or post-process job
	onLoadHistory        func() ([]DownloadHistoryEntry, error)                                                                      // HistoryService.Load
	onClearHistory       func() error                                                                                                // HistoryService.Clear
	onCheckDependencies  func(onWarning func(msg string))                                                                            // DependencyService.Check
	onRunUpdate          func(cb UpdateCallbacks)                                                                                    // DependencyService.RunUpdate
	onYtDlpVersions      func() (installed, latest string)                                                                           // DownloaderApp.ytDlpVersions
	onCheckGoVidRelease  func() (release Release, newer bool, err error)                                                             // DownloaderApp.checkGoVidRelease
	onLoadPreferences    func() AppPreferences                                                                                       // PreferenceService.Load
	onSavePreferences    func(AppPreferences)                                                                                        // PreferenceService.Save
	onResetPreferences   func()                                                                                                      // PreferenceService.Reset
	onLoadConfigFile     func(path string) (*AppConfig, error)                                                                       // PreferenceService.LoadFromFile
	onMergeConfig        func(cfg *AppConfig, base AppPreferences, validFormats, validQualities []string) (AppPreferences, []string) // PreferenceService.MergeConfig
	onSetLogBufferLimit  func(limit int)                                                                                             // LogService.SetBufferLimit
	onLogBufferLimit     func() int                                                                                                  // LogService.BufferLimit
	onSetShowDebug       func(show bool)                                                                                             // DownloaderApp.showDebug.Store
}

// NewUIManager returns a UIManager bound to the given primary window.
func NewUIManager(mainWindow fyne.Window) *UIManager {
	return &UIManager{mainWindow: mainWindow, afterFunc: time.AfterFunc}
}

// ── Singleton window helpers ──────────────────────────────────────────────────

// focusOrCreate returns true when window is already open, focusing it so the
// caller can return immediately without building a new window. Usage:
//
//	if focusOrCreate(&manager.aboutWindow) { return }
func focusOrCreate(window *fyne.Window) bool {
	if *window != nil {
		(*window).RequestFocus()
		return true
	}
	return false
}

// onWindowClosed returns a closure that nils the window field when the window
// is closed. Assign it directly to SetOnClosed:
//
//	w.SetOnClosed(onWindowClosed(&manager.aboutWindow))
func onWindowClosed(window *fyne.Window) func() {
	return func() { *window = nil }
}

// parseURL is a small helper to safely parse a URL string for use in hyperlinks.
func parseURL(rawURL string) *url.URL {
	parsed, _ := url.Parse(rawURL)
	return parsed
}

// ── Main menu ──────────────────────────────────────────────────────────────────

// createMainMenu builds the application's top-level menu bar.
func (manager *UIManager) createMainMenu() {
	historyMenu := fyne.NewMenuItem("History", func() {
		manager.showHistory()
	})

	clearLogMenu := fyne.NewMenuItem("Clear Terminal Output", func() {
		manager.clearTerminalOutput()
	})

	updateMenu := fyne.NewMenuItem("Update yt-dlp", manager.confirmYtDlpUpdate)

	prefsMenu := fyne.NewMenuItem("Preferences", func() {
		manager.showPreferences()
	})

	configHelpMenu := fyne.NewMenuItem("GoVid Guide", func() {
		manager.showConfigHelp()
	})

	aboutMenu := fyne.NewMenuItem("About GoVid", func() {
		manager.showAbout()
	})

	mainMenu := fyne.NewMainMenu(
		fyne.NewMenu("File", historyMenu, fyne.NewMenuItemSeparator(), clearLogMenu),
		fyne.NewMenu("Tools",
			updateMenu,
			fyne.NewMenuItem("Check for GoVid updates", manager.checkForGoVidUpdates),
			fyne.NewMenuItemSeparator(),
			prefsMenu,
			fyne.NewMenuItem("Post-Processing", func() {
				manager.showPostProcessing()
			}),
		),
		fyne.NewMenu("Help", configHelpMenu, fyne.NewMenuItemSeparator(), aboutMenu),
	)
	manager.mainWindow.SetMainMenu(mainMenu)
}

// checkDependencies verifies that the external tools — yt-dlp, ffmpeg, and
// the optional ffprobe — are available either in the 'bin' folder beside the executable or
// in the system PATH. Warnings are printed to the log panel.
func (manager *UIManager) checkDependencies() {
	manager.onCheckDependencies(func(msg string) {
		manager.onLog(msg, colWarning)
	})
}

// confirmYtDlpUpdate shows the installed and latest yt-dlp versions and asks
// whether to update. Finding the versions runs yt-dlp and may ask GitHub,
// so it happens off the UI thread before the dialog opens.
func (manager *UIManager) confirmYtDlpUpdate() {
	go func() {
		installed, latest := manager.onYtDlpVersions()
		message := fmt.Sprintf("Installed version: %s\nLatest version: %s\n\nThis will run 'yt-dlp -U' to update the tool. Continue?", installed, latest)
		fyne.Do(func() {
			dialog.ShowConfirm("Update yt-dlp", message, func(ok bool) {
				if ok {
					manager.runUpdateInUI()
				}
			}, manager.mainWindow)
		})
	}()
}

// runUpdateInUI sets the initial UI state for an update and delegates
// execution to DependencyService, which runs yt-dlp -U in a background
// goroutine and reports progress via UpdateCallbacks. A successful update
// dismisses the "yt-dlp is out of date" notice.
func (manager *UIManager) runUpdateInUI() {
	manager.onLog("[SYSTEM] Starting yt-dlp update...", colSystem)
	manager.onSetStatusIndicator(StatusActive)
	manager.onStatus("Status: Updating yt-dlp...")
	manager.onRunUpdate(UpdateCallbacks{
		OnLog:    manager.onLog,
		OnStatus: manager.onStatus,
		OnSuccess: func() {
			manager.onSetStatusIndicator(StatusSuccess)
			manager.dismissNotice(ytDlpNoticeID)
		},
		OnFailure: func() { manager.onSetStatusIndicator(StatusFailed) },
	})
}

// showAbout opens a small window with information about the creator and the app.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showAbout() {
	if focusOrCreate(&manager.aboutWindow) {
		return
	}

	logo := canvas.NewImageFromResource(resourceAppiconPng)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(80, 80))

	appName := canvas.NewText("GoVid", accentCyan)
	appName.TextSize = 24
	appName.TextStyle = fyne.TextStyle{Bold: true}
	appName.Alignment = fyne.TextAlignCenter

	versionLabel := widget.NewLabelWithStyle("v"+version, fyne.TextAlignCenter, fyne.TextStyle{Monospace: true})
	ytDlpLabel := widget.NewLabelWithStyle("yt-dlp: checking…", fyne.TextAlignCenter, fyne.TextStyle{Monospace: true})
	go func() {
		installed, latest := manager.onYtDlpVersions()
		fyne.Do(func() {
			ytDlpLabel.SetText(fmt.Sprintf("yt-dlp %s (latest: %s)", installed, latest))
		})
	}()
	tagline := widget.NewLabelWithStyle("A high-performance video downloader\nbuilt with Go and Fyne.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	author := widget.NewLabelWithStyle("Created by David Bennehag", fyne.TextAlignCenter, fyne.TextStyle{})
	website := widget.NewHyperlink("dunder.gg", parseURL("https://dunder.gg"))
	github := widget.NewHyperlink("github.com/DunderGG/govid", parseURL("https://github.com/DunderGG/govid"))
	links := container.NewHBox(layout.NewSpacer(), website, widget.NewLabel("•"), github, layout.NewSpacer())

	content := container.NewVBox(
		container.NewCenter(logo),
		container.NewCenter(appName),
		container.NewCenter(versionLabel),
		container.NewCenter(ytDlpLabel),
		container.NewCenter(tagline),
		widget.NewSeparator(),
		container.NewCenter(author),
		links,
	)

	manager.aboutWindow = fyne.CurrentApp().NewWindow("About GoVid")
	manager.aboutWindow.SetContent(container.NewPadded(content))
	manager.aboutWindow.Resize(fyne.NewSize(360, 310))
	manager.aboutWindow.SetFixedSize(true)
	manager.aboutWindow.SetOnClosed(onWindowClosed(&manager.aboutWindow))
	manager.aboutWindow.Show()
}

// showHistory opens a window listing previously downloaded URLs from disk.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showHistory() {
	if focusOrCreate(&manager.historyWindow) {
		return
	}

	// Load the download history from disk. If it fails, show an error dialog and abort.
	entries, err := manager.onLoadHistory()
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to load download history: %v", err), manager.mainWindow)
		return
	}

	text := widget.NewMultiLineEntry()
	text.SetPlaceHolder("No download history yet.")
	text.SetText(formatHistoryEntries(entries))
	text.Disable()

	scroll := container.NewScroll(text)
	scroll.SetMinSize(fyne.NewSize(760, 420))

	clearBtn := widget.NewButton("Clear History", func() {
		dialog.ShowConfirm(
			"Clear Download History",
			"Are you sure you want to clear all download history? This cannot be undone.",
			func(ok bool) {
				if !ok {
					return
				}
				if err := manager.onClearHistory(); err != nil {
					dialog.ShowError(fmt.Errorf("failed to clear history: %v", err), manager.historyWindow)
					return
				}
				text.SetText("")
			},
			manager.historyWindow,
		)
	})

	bottomBar := container.NewHBox(layout.NewSpacer(), clearBtn)
	content := container.NewBorder(nil, bottomBar, nil, nil, scroll)

	manager.historyWindow = fyne.CurrentApp().NewWindow("Download History")
	manager.historyWindow.SetContent(container.NewPadded(content))
	manager.historyWindow.Resize(fyne.NewSize(800, 500))
	manager.historyWindow.SetOnClosed(onWindowClosed(&manager.historyWindow))
	manager.historyWindow.Show()
}

// formatHistoryEntries renders the download history for the History window,
// newest first. Each entry is a title line followed by indented details and
// a blank separator line. The title falls back to the file name, then the URL.
func formatHistoryEntries(entries []DownloadHistoryEntry) string {
	var lines []string
	for _, entry := range slices.Backward(entries) {
		title := entry.OriginalTitle
		if title == "" {
			title = entry.FinalFilename
		}
		if title == "" {
			title = entry.URL
		}
		lines = append(lines,
			fmt.Sprintf("%s | %s", entry.DownloadedAt, title),
			fmt.Sprintf("  URL: %s", entry.URL),
			fmt.Sprintf("  Saved As: %s", entry.FinalFilename),
			fmt.Sprintf("  Path: %s", entry.SavedPath),
			fmt.Sprintf("  Format/Quality: %s / %s", entry.Format, entry.Quality),
			fmt.Sprintf("  Post-Processed: %t", entry.PostProcessed),
			"",
		)
	}
	return strings.Join(lines, "\n")
}

// showConfigHelp opens a scrollable window explaining all configuration options.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showConfigHelp() {
	if focusOrCreate(&manager.helpWindow) {
		return
	}

	type helpItem struct {
		label string
		desc  string
	}

	items := []helpItem{
		{"Video URL", "Paste any URL supported by yt-dlp, such as a **YouTube**, **Vimeo**, or **Twitter/X** link.\n\nOther ways to add URLs (each switches on **Batch Mode** when the field then holds more than one, and skips URLs already in it):\n" +
			"  * **Load from file…** – reads a `.txt` file with one URL per line. Blank lines and lines starting with `#` are skipped, as are lines that are not links; the log says how many and which\n" +
			"  * **Paste button** (next to the field) – adds the links on the clipboard, one per line. It refuses the clipboard if anything in it is not a link\n" +
			"  * **Drop onto the window** – a `.txt` list, or an internet shortcut (`.url`). A link dragged straight from a browser is not received on Windows; drag it to the desktop first, then drop the shortcut that makes\n\n" +
			"In Batch Mode, lines starting with `#` are comments and are not downloaded."},
		{"Playlists", "Before downloading, GoVid checks each URL. When one is a playlist, it shows the playlist's title, number of videos, and total length, and asks which videos to download:\n  * Leave the range blank and click **Download** to get them all\n  * Enter a range such as `1-10`, `5-`, or `3,5,8` to get only those\n  * For a link to one video inside a playlist (`watch?v=…&list=…`), **Only this video** is the default\n\nEach chosen video becomes its own item in the queue, with its own progress, Cancel, and history entry."},
		{"Save Destination", "The folder where the downloaded file will be saved. GoVid remembers this between sessions.\n\nBefore each download, GoVid checks that the folder's drive has room for it (with a 10% margin, and twice that with post-processing, which writes a second copy). If it does not, you can continue anyway or cancel; in a batch you can also skip that video. This includes each video picked from a playlist, which GoVid checks just before downloading it. When the site does not say how big a video is, the check is skipped."},
		{"Output Format", "The container format for the downloaded file:\n" +
			"  * **" + formatMP4 + "** – widely compatible, recommended for most uses\n" +
			"  * **" + formatMKV + "** – flexible container, ideal for high-quality archiving\n" +
			"  * **" + formatWebM + "** – open format, good for web use\n" +
			"  * **" + formatMP3 + "** – audio only, compressed\n" +
			"  * **" + formatM4A + "** – audio only, Apple/iTunes compatible"},
		{"Max Quality", "Sets the maximum resolution yt-dlp will request:\n" +
			"  * **" + qualityBest + "** – downloads the highest resolution available\n" +
			"  * **" + strings.Join(qualityOptions[1:], "** / **") + "** – caps the resolution to save space or bandwidth\n\n" +
			"A capped download is named after the resolution it actually got, e.g. `_720p`. When a video is not available at the cap, GoVid says so in the log and in a notice above the input card: it downloads the best version below the cap, or, when the site has no version at or below it, the best version there is, with a warning. Audio formats ignore this setting and get no label."},
		{"Trim Start / Trim End", "Download only a segment of the video. Leave both blank to download the full video.\n\nAccepted formats:\n  * `HH:MM:SS` (e.g. 01:30:00)\n  * `MM:SS` (e.g. 01:30)\n  * `Seconds` (e.g. 90)\n\nEither field can be used alone:\n  * **Trim Start only** → downloads from that point to the end\n  * **Trim End only** → downloads from the start to that point"},
		{"Save output to log file", "When checked, everything printed in the Terminal Output panel is also saved to a **GoVid_log_YYYY-MM-DD.txt** file in your save destination folder. Errors are also mirrored to a separate **GoVid_errors_YYYY-MM-DD.txt** file."},
		{"Notify on Completion", "When checked, a system notification is sent when a download finishes (success or failure), but not when cancelled."},
		{"Log Buffer Limit", "Found in **Tools → Preferences**. The number of lines kept in the Terminal Output panel; older lines are removed from the top. The panel never shows more than the latest **5000** lines, so choosing **Unlimited** only affects the lines kept for the log file. The log file itself is never trimmed. If you scroll up while a download is running, the panel stays where you left it; scroll back to the bottom to follow new lines again."},
		{"Embed in File", "Found in **Tools → Preferences**. What yt-dlp writes into each downloaded file:\n  * **Metadata** (on by default) – title, artist, upload date, and description tags, so music players show more than a file name\n  * **Thumbnail** (on by default) – the video's thumbnail as cover art, converted to JPEG. WebM files cannot hold cover art, so it is skipped for them\n  * **Chapters** – the video's chapter markers\n\nPost-processing keeps the cover art, chapters, subtitles, and tags of the files it re-encodes."},
		{"Subtitles", "Found in **Tools → Preferences**. Downloads the video's subtitles:\n" +
			"  * **" + subtitlesOff + "** (default) – no subtitles\n" +
			"  * **" + subtitlesEmbed + "** – as a subtitle track inside the video, which players let you turn on and off\n" +
			"  * **" + subtitlesSRT + "** – as `.srt` files next to the video, named like it with the language added, e.g. `GoVid_Title.en.srt`\n" +
			"  * **" + subtitlesBoth + "** – both of the above\n\n" +
			"**Subtitle Languages** takes language codes or patterns separated by commas, as yt-dlp's `--sub-langs` does: `en.*` (the default) matches `en`, `en-US`, `en-GB`, and so on; `all,-live_chat` takes every language except live chat. Before each download, the log lists the languages the video has and warns when none matches; the video then downloads without subtitles.\n\n" +
			"**Include auto-generated** also takes captions the site generated automatically, for languages that have no subtitles written by people. They are often inaccurate, and YouTube offers them in over 150 languages.\n\n" +
			"Audio formats cannot hold subtitles, so none are downloaded for them. WebM can only hold WebVTT subtitles, so those embedded in WebM stay in that format. For a trimmed download, subtitles still cover the whole video. If the subtitles cannot be downloaded (YouTube sometimes refuses with \"Too Many Requests\"), GoVid downloads the video again without them and says so."},
		{"Debug Output", "Found in **Tools → Preferences**. GoVid runs yt-dlp in verbose mode so the log file has everything needed for a bug report, but the **[debug]** lines are hidden from the Terminal Output panel unless this is checked. The panel also shows only the latest download progress line for each file; the log file keeps them all."},
		{"Updates", "Found in **Tools → Preferences**. When **Check for updates on startup** is checked, GoVid asks GitHub (at most once a day) whether a newer yt-dlp is available and, if so, shows a notice with an **Update now** button. Sites change often, and an outdated yt-dlp is the most common reason downloads stop working. **Tools → Update yt-dlp** shows the installed and latest versions and updates on demand.\n\nGoVid also tells you when a newer GoVid release is available; **What's new** shows its release notes and a link to the download page. **Tools → Check for GoVid updates** checks right away.\n\nIf the update fails because GoVid's folder cannot be written to (for example under `Program Files`), run GoVid as administrator once, or move it to a folder you own."},
		{"Save Preferences", "Found in **Tools → Preferences**. When checked, GoVid remembers your format, quality, save path, speed limit, and theme between sessions. The toggle itself is always remembered so the choice survives a restart."},
		{"Max Download Speed", "Found in **Tools → Preferences**. Limits the bandwidth used by GoVid to prevent network saturation. Examples:\n  * `50K` – Very slow\n  * `5M` – Moderate (standard HD streaming speed)\n  * `10G` – Virtually unlimited\n\nLeave blank to use full available bandwidth."},
		{"Cookies File", "Found in **Tools → Preferences**. Path to a `cookies.txt` file in Mozilla/Netscape format. Required for access to restricted, private, or age-gated videos.\n\n⚠️ **Security Warning**: Cookie files contain sensitive session data. Never share this file."},
		{"Post-Processing", "Found in **Tools → Post-Processing**. Enhance your downloads using FFmpeg. Most filters trigger a full re-encode.\n\n⚠️ **WebM files** use VP9 encoding which is significantly slower than H.264 — use MKV for faster post-processing."},
		{"Cancel", "Stops the active download immediately and deletes its partly downloaded files. In batch mode, it skips the current URL and moves on to the next one."},
		{"Open Folder", "Opens your chosen save destination in the system file manager."},
		{"JSON Configuration", "For advanced users, GoVid supports loading settings from a `govid.json` file located in the application folder.\n\n**Supported Values:**\n" +
			"* **format**: " + codeList(formatOptions) + "\n" +
			"* **quality**: " + codeList(qualityOptions) + "\n" +
			"* **path**: Any valid absolute folder path\n* **maxSpeed**: Numeric value with unit, e.g., `50K`, `5M`, `1G` (or blank for unlimited)\n" +
			"* **embedMetadata**, **embedThumbnail**, **embedChapters**: `true` or `false`\n" +
			"* **subtitles**: " + codeList(subtitleModeOptions) + "\n" +
			"* **subtitleLangs**: yt-dlp `--sub-langs` syntax, e.g. `en.*,de`\n" +
			"* **autoSubtitles**: `true` or `false`"},
	}

	content := container.NewVBox()
	for _, item := range items {
		title := widget.NewRichTextFromMarkdown("### " + item.label)
		title.Wrapping = fyne.TextWrapOff

		body := widget.NewRichTextFromMarkdown(item.desc)
		for segIdx := range body.Segments {
			if segment, ok := body.Segments[segIdx].(*widget.TextSegment); ok {
				if segment.Style.TextStyle.Bold {
					segment.Style.ColorName = theme.ColorNamePrimary
				}
				if segment.Style.TextStyle.Monospace {
					segment.Style.ColorName = theme.ColorNameWarning
				}
			}
		}
		body.Wrapping = fyne.TextWrapWord

		content.Add(title)
		content.Add(body)
		content.Add(widget.NewSeparator())
	}

	scroll := container.NewScroll(content)
	scroll.SetMinSize(fyne.NewSize(520, 420))

	manager.helpWindow = fyne.CurrentApp().NewWindow("GoVid Guide")
	manager.helpWindow.SetContent(container.NewPadded(scroll))
	manager.helpWindow.Resize(fyne.NewSize(550, 500))
	manager.helpWindow.SetOnClosed(onWindowClosed(&manager.helpWindow))
	manager.helpWindow.Show()
}

// codeList renders options as a comma-separated list of Markdown code spans,
// e.g. "`MP4`, `MKV`".
func codeList(options []string) string {
	return "`" + strings.Join(options, "`, `") + "`"
}

// showPreferences opens a window for general application settings.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showPreferences() {
	if focusOrCreate(&manager.prefsWindow) {
		return
	}

	ui := manager.ui
	// Reload the persisted values so the window never shows edits that were
	// discarded by closing it without saving.
	applyGeneralPrefs(ui, manager.onLoadPreferences())

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Save Preferences", Widget: ui.prefs.savePrefs, HintText: "Remember format, quality, path, speed, and theme between sessions"},
			{Text: "Log Buffer Limit", Widget: ui.prefs.logLimit, HintText: "Max lines kept in the log view (never more than 5000); older entries are removed from the top"},
			{Text: "Debug Output", Widget: ui.prefs.showDebug, HintText: "Show yt-dlp's [debug] lines in the log view; the log file always has them"},
			{Text: "Updates", Widget: ui.prefs.checkUpdates, HintText: "Check GitHub once a day for newer yt-dlp and GoVid releases"},
			{Text: "Embed in File", Widget: container.NewHBox(ui.prefs.embedMetadata, ui.prefs.embedThumbnail, ui.prefs.embedChapters), HintText: "Write tags (title, artist, date), cover art, and chapter markers into downloads"},
			{Text: "Subtitles", Widget: container.NewHBox(ui.prefs.subtitles, ui.prefs.autoSubtitles), HintText: "Embed subtitles in videos, save them as .srt files beside them, or both"},
			{Text: "Subtitle Languages", Widget: ui.prefs.subtitleLangs, HintText: "Comma-separated language codes or patterns, e.g. en.*,de (yt-dlp --sub-langs)"},
			{Text: "Max Download Speed", Widget: ui.prefs.maxSpeed, HintText: "Limits download rate (e.g. 50K, 5M, 10G)"},
			{Text: "Application Theme", Widget: ui.prefs.themeMode, HintText: "Restart may be required for some changes"},
			{Text: "Cookies File", Widget: manager.buildCookiesRow(), HintText: "Path to a Mozilla/Netscape-format cookies.txt file"},
		},
		OnSubmit: manager.submitPreferences,
	}

	resetBtn := widget.NewButton("Restore Defaults", manager.confirmRestoreDefaults)
	resetBtn.Importance = widget.DangerImportance

	loadConfigBtn := widget.NewButtonWithIcon("Load from Config (govid.json)", theme.SettingsIcon(), manager.loadConfigFile)

	manager.prefsWindow = fyne.CurrentApp().NewWindow("Preferences")
	manager.prefsWindow.SetContent(container.NewPadded(container.NewVBox(
		form,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, loadConfigBtn, resetBtn),
	)))
	manager.prefsWindow.Resize(fyne.NewSize(520, 560))
	manager.prefsWindow.SetOnClosed(onWindowClosed(&manager.prefsWindow))
	manager.prefsWindow.Show()
}

// buildCookiesRow lays out the cookies-file entry with a browse button that
// picks a cookie file and a button that clears the entry.
func (manager *UIManager) buildCookiesRow() fyne.CanvasObject {
	cookies := manager.ui.prefs.cookies

	browseBtn := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			cookies.SetText(reader.URI().Path())
			reader.Close()
		}, manager.prefsWindow)
		// Filter for common cookie file extensions
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".txt", ".cookies", ".dat"}))
		fileDialog.Show()
	})
	clearBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		cookies.SetText("")
	})
	return container.NewBorder(nil, nil, nil, container.NewHBox(browseBtn, clearBtn), cookies)
}

// submitPreferences handles the Preferences form's Submit: it applies the
// log buffer limit, saves every preference, and applies the selected theme.
func (manager *UIManager) submitPreferences() {
	ui := manager.ui
	manager.onSetLogBufferLimit(ParseBufferLimit(ui.prefs.logLimit.Selected))
	manager.onSetShowDebug(ui.prefs.showDebug.Checked)
	manager.savePreferences(ui.download.path.Text)

	// Apply theme change and rebuild the UI so canvas.Rectangle colors
	// (which are snapshotted at construction time) get fresh theme values.
	applyTheme(fyne.CurrentApp(), ui.prefs.themeMode.Selected)
	manager.createUI()
}

// confirmRestoreDefaults asks the user to confirm, then runs restoreDefaults.
func (manager *UIManager) confirmRestoreDefaults() {
	dialog.ShowConfirm("Restore Defaults", "Reset all preferences to their default values?", func(ok bool) {
		if ok {
			manager.restoreDefaults()
		}
	}, manager.prefsWindow)
}

// loadConfigFile merges govid.json into the stored preferences, applies the
// result to the widgets and saves it, then reports any skipped settings.
func (manager *UIManager) loadConfigFile() {
	ui := manager.ui
	config, err := manager.onLoadConfigFile(configFileName)
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to load govid.json: %v", err), manager.prefsWindow)
		return
	}

	merged, errs := manager.onMergeConfig(config, manager.onLoadPreferences(), ui.download.format.Options, ui.download.quality.Options)
	applyPreferencesToWidgets(ui, merged)
	manager.onSavePreferences(merged)

	if len(errs) > 0 {
		dialog.ShowCustom("Config Loaded with Warnings", "OK",
			widget.NewLabel(fmt.Sprintf("some settings were skipped:\n- %s", strings.Join(errs, "\n- "))),
			manager.prefsWindow)
		return
	}
	dialog.ShowInformation("Config Loaded", "Preferences updated from govid.json", manager.prefsWindow)
}

// savePreferences snapshots the current widget state (see snapshotPreferences)
// and delegates persistence to PreferenceService.Save. It does nothing while
// restoreDefaults is running.
func (manager *UIManager) savePreferences(savePath string) {
	if manager.restoringDefaults {
		return
	}
	manager.onSavePreferences(snapshotPreferences(manager.ui, savePath))
}

// restoreDefaults clears every stored preference and returns the whole UI to
// its default state: every preference-backed widget, the log buffer limit, the
// theme, and the main window layout. The defaults come from loading the
// freshly cleared store, so no default value is repeated here.
//
// Applying the defaults fires the widgets' save-on-change handlers. Saving is
// suppressed meanwhile so those handlers cannot write a half-updated snapshot
// back into the store, which is left empty as PreferenceService.Reset intends.
func (manager *UIManager) restoreDefaults() {
	manager.restoringDefaults = true
	defer func() { manager.restoringDefaults = false }()

	manager.onResetPreferences()
	defaults := manager.onLoadPreferences()

	applyPreferencesToWidgets(manager.ui, defaults)
	manager.onSetLogBufferLimit(ParseBufferLimit(defaults.LogLimit))
	manager.onSetShowDebug(defaults.ShowDebug)
	applyTheme(fyne.CurrentApp(), defaults.ThemeMode)
	manager.createUI()
}

// showPostProcessing opens a window for specialized hardware/software filters.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showPostProcessing() {
	if focusOrCreate(&manager.ppWindow) {
		return
	}

	// Reload the persisted values so the window never shows edits that were
	// discarded by closing it without applying.
	applyPostProcessPrefs(manager.ui, manager.onLoadPreferences())

	// Live readouts of the two sliders' values, shown beside them.
	fpsValue := binding.NewFloat()
	sharpenValue := binding.NewFloat()

	loadIndicator, refreshLoad := manager.buildLoadIndicator()
	manager.wirePostProcessHandlers(refreshLoad, fpsValue, sharpenValue)
	refreshLoad() // seed with the current state

	title := widget.NewLabelWithStyle("Post-Processing Filters", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	scroll := container.NewScroll(manager.buildPostProcessForm(fpsValue, sharpenValue))
	footer := manager.buildPostProcessFooter(loadIndicator)
	// Border layout: title pinned top, footer pinned bottom, scroll fills the rest.
	content := container.NewBorder(title, footer, nil, nil, scroll)

	manager.ppWindow = fyne.CurrentApp().NewWindow("Post-Processing Settings")
	manager.ppWindow.SetContent(container.NewPadded(content))
	manager.ppWindow.Resize(fyne.NewSize(680, 580))
	manager.ppWindow.SetFixedSize(false)
	manager.ppWindow.SetOnClosed(onWindowClosed(&manager.ppWindow))
	manager.ppWindow.Show()
}

// wirePostProcessHandlers attaches the Post-Processing window's OnChanged
// handlers: every control calls refresh so the load indicator stays live,
// the slider readouts track their sliders, and each option's sub-controls
// are enabled only while the option is checked.
func (manager *UIManager) wirePostProcessHandlers(refresh func(), fpsValue, sharpenValue binding.Float) {
	pp := manager.ui.postProcess

	bindDependents(pp.smoothMotion, refresh, pp.smoothMotionMode, pp.smoothMotionFPS)
	bindDependents(pp.sharpen, refresh, pp.sharpenAmount)
	bindDependents(pp.denoise, refresh, pp.denoiseMode)
	bindDependents(pp.upscaleVideo, refresh, pp.upscaleTarget)

	fpsValue.Set(pp.smoothMotionFPS.Value)
	pp.smoothMotionFPS.OnChanged = func(v float64) {
		fpsValue.Set(v)
	}
	sharpenValue.Set(pp.sharpenAmount.Value)
	pp.sharpenAmount.OnChanged = func(v float64) {
		sharpenValue.Set(v)
		refresh()
	}

	onSelect := func(_ string) { refresh() }
	pp.smoothMotionMode.OnChanged = onSelect
	pp.denoiseMode.OnChanged = onSelect
	pp.upscaleTarget.OnChanged = onSelect

	for _, toggle := range []*widget.Check{
		pp.vividMode, pp.deband, pp.hdrToSdr, pp.deinterlace,
		pp.stabilize, pp.autoCrop, pp.normalizeAudio, pp.nightMode,
	} {
		toggle.OnChanged = func(_ bool) { refresh() }
	}
}

// bindDependents enables dependents only while toggle is checked, applying
// the toggle's current state immediately, and calls refresh after every
// change of the toggle.
func bindDependents(toggle *widget.Check, refresh func(), dependents ...fyne.Disableable) {
	setEnabled := func(enabled bool) {
		for _, dependent := range dependents {
			if enabled {
				dependent.Enable()
			} else {
				dependent.Disable()
			}
		}
	}
	setEnabled(toggle.Checked)
	toggle.OnChanged = func(checked bool) {
		setEnabled(checked)
		refresh()
	}
}

// buildLoadIndicator builds the live "Estimated Processing Load" section:
// a row of coloured blocks that light up as the cost of the selected filters
// passes each of loadBlockThresholds, a description of the load, and a file
// size warning. The returned refresh function recomputes all three from the
// current widget state.
func (manager *UIManager) buildLoadIndicator() (fyne.CanvasObject, func()) {
	blocks := make([]*canvas.Rectangle, len(loadBlockThresholds))
	blockBar := container.NewGridWithColumns(len(blocks))
	for i := range blocks {
		block := canvas.NewRectangle(colLoadEmpty)
		block.SetMinSize(fyne.NewSize(0, 14))
		block.CornerRadius = 3
		blocks[i] = block
		blockBar.Add(block)
	}

	loadDesc := binding.NewString()
	loadLabel := widget.NewLabelWithData(loadDesc)
	loadLabel.Alignment = fyne.TextAlignCenter

	sizeWarn := binding.NewString()
	sizeWarnLabel := widget.NewLabelWithData(sizeWarn)
	sizeWarnLabel.Alignment = fyne.TextAlignCenter
	sizeWarnLabel.TextStyle = fyne.TextStyle{Italic: true}
	sizeWarnLabel.Wrapping = fyne.TextWrapWord

	pp := manager.ui.postProcess
	refresh := func() {
		cost, desc := computeProcessingLoad(newPostProcessSettings(manager.ui))
		loadDesc.Set(desc)
		for i, block := range blocks {
			if cost > loadBlockThresholds[i] {
				block.FillColor = colLoadPalette[i]
			} else {
				block.FillColor = colLoadEmpty
			}
			block.Refresh()
		}
		sizeWarn.Set(sizeWarning(pp.upscaleVideo.Checked, pp.smoothMotion.Checked))
	}

	indicator := container.NewVBox(
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Estimated Processing Load", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		blockBar,
		loadLabel,
		sizeWarnLabel,
	)
	return indicator, refresh
}

// sizeWarning returns the file-size warning for the filters that add pixels
// (upscale) or frames (smooth motion), or "" when neither is selected.
func sizeWarning(upscale, smooth bool) string {
	switch {
	case upscale && smooth:
		return "⚠ Upscaling + Smooth Motion will greatly increase file size"
	case upscale:
		return "⚠ Upscaling significantly increases file size (bigger frames)"
	case smooth:
		return "⚠ Smooth Motion increases file size (more frames)"
	default:
		return ""
	}
}

// buildPostProcessForm lays out the Post-Processing window's filter controls
// in titled sections. fpsValue and sharpenValue back the slider readouts.
func (manager *UIManager) buildPostProcessForm(fpsValue, sharpenValue binding.Float) *widget.Form {
	pp := manager.ui.postProcess

	fpsLabel := widget.NewLabelWithData(binding.FloatToStringWithFormat(fpsValue, "%.0f FPS"))
	sharpenLabel := widget.NewLabelWithData(binding.FloatToStringWithFormat(sharpenValue, "%.1fx"))

	return &widget.Form{
		Items: []*widget.FormItem{
			// ── GPU ACCELERATION ─────────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("GPU ACCELERATION")},
			{Text: "Encoder Backend", Widget: fixedWidth(pp.gpuBackend, 200), HintText: "GPU-accelerated re-encoding; falls back to CPU if unavailable"},
			{Text: "", Widget: sectionDivider()},
			// ── MOTION ─────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("MOTION ENHANCEMENT")},
			{Text: "Smooth Motion", Widget: pp.smoothMotion, HintText: "Interpolate frames for fluid playback (slow)"},
			{Text: "Smoothing Mode", Widget: pp.smoothMotionMode, HintText: "Precise/Balanced use motion vectors, Fast uses blending"},
			{Text: "Target FPS", Widget: container.NewHBox(fixedWidth(pp.smoothMotionFPS, 200), fpsLabel), HintText: "Standard is 60, cinematic is 24, high-refresh is 120"},
			{Text: "", Widget: sectionDivider()},
			// ── VIDEO ──────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("VIDEO ENHANCEMENT")},
			{Text: "Vivid Mode", Widget: pp.vividMode, HintText: "Boost brightness, contrast, and saturation"},
			{Text: "Sharpen Video", Widget: pp.sharpen, HintText: "CAS (Contrast Adaptive Sharpening) — sharpens edges without haloing or noise amplification"},
			{Text: "Sharpen Intensity", Widget: container.NewHBox(fixedWidth(pp.sharpenAmount, 200), sharpenLabel), HintText: "1.0x is gentle, 1.5x is moderate, 2.0x is strong"},
			{Text: "Fix Banding", Widget: pp.deband, HintText: "Remove gradient banding steps in skies and dark scenes (deband)"},
			{Text: "HDR to SDR", Widget: pp.hdrToSdr, HintText: "Tone-map HDR (PQ/HLG) videos for standard monitors; SDR videos are left unchanged"},
			{Text: "", Widget: sectionDivider()},
			// ── NOISE & ARTIFACTS ───────────────────────────────────────────
			{Text: "", Widget: sectionHeader("NOISE & ARTIFACTS")},
			{Text: "Denoise", Widget: pp.denoise, HintText: "HQ noise reduction for low-quality or grainy footage"},
			{Text: "Denoise Mode", Widget: pp.denoiseMode, HintText: "NLMeans: highest quality, very slow | hqdn3d: spatial + temporal denoising, fast and effective"},
			{Text: "Deinterlace", Widget: pp.deinterlace, HintText: "Remove combing artifacts from archival or TV-rip content (bwdif)"},
			{Text: "Stabilize", Widget: pp.stabilize, HintText: "Smooth out shaky handheld footage (deshake)"},
			{Text: "Auto-Crop", Widget: pp.autoCrop, HintText: "Detect and remove black letterbox/pillarbox bars automatically"},
			{Text: "", Widget: sectionDivider()},
			// ── UPSCALING ────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("UPSCALING")},
			{Text: "Upscale Video", Widget: pp.upscaleVideo, HintText: "Enlarge the video using a high-quality Lanczos resampler"},
			{Text: "Target Resolution", Widget: fixedWidth(pp.upscaleTarget, 200), HintText: "2× doubles both dimensions; fixed targets set a specific height"},
			{Text: "", Widget: sectionDivider()},
			// ── AUDIO ──────────────────────────────────────────────────
			{Text: "", Widget: sectionHeader("AUDIO ENHANCEMENT")},
			{Text: "Normalize Audio", Widget: pp.normalizeAudio, HintText: "Loudness normalization via the loudnorm filter"},
			{Text: "Night Mode", Widget: pp.nightMode, HintText: "Dynamic compression to balance quiet dialogue and loud effects (dynaudnorm)"},
		},
	}
}

// buildPostProcessFooter assembles the area pinned below the filter form:
// the load indicator, the Apply / Apply & Close buttons, and the re-encode
// notice.
func (manager *UIManager) buildPostProcessFooter(loadIndicator fyne.CanvasObject) fyne.CanvasObject {
	applyBtn := widget.NewButtonWithIcon("Apply", theme.ConfirmIcon(), func() {
		manager.savePreferences(manager.ui.download.path.Text)
	})

	applyCloseBtn := widget.NewButtonWithIcon("Apply & Close", theme.ConfirmIcon(), func() {
		manager.savePreferences(manager.ui.download.path.Text)
		manager.ppWindow.Close()
	})
	applyCloseBtn.Importance = widget.HighImportance

	buttons := container.NewGridWithColumns(2, applyBtn, applyCloseBtn)
	notice := widget.NewLabelWithStyle("⚠️ Most filters require FFmpeg and trigger a full re-encode.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})

	return container.NewVBox(loadIndicator, widget.NewSeparator(), buttons, notice)
}

// ── Main window ────────────────────────────────────────────────────────────────

// createUI constructs the graphical user interface by organizing widgets into
// cards and containers. It sets up the layout (header, input tools, status,
// logs, and footer) and attaches event handlers to buttons.
func (manager *UIManager) createUI() {
	prefs := manager.onLoadPreferences()

	manager.configureEntryMode()
	manager.wireToggleHandlers()
	manager.wireActionButtons(prefs.ThemeMode)

	header := buildHeader()
	inputCard := manager.buildInputCard(prefs.ThemeMode)
	statusCard := manager.buildStatusCard()
	logPane := manager.buildLogPane()
	footer := buildFooter()

	manager.noticeBox = container.NewVBox()
	manager.renderNotices()

	topContent := container.NewVBox(
		header,
		manager.noticeBox,
		inputCard,
		statusCard,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Terminal Output:", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
	)

	content := container.NewBorder(topContent, footer, nil, nil, logPane)
	manager.mainWindow.SetContent(container.NewPadded(content))
	manager.mainWindow.SetOnDropped(func(_ fyne.Position, items []fyne.URI) {
		manager.handleDrop(items)
	})
}

// notice is a message shown in a bar above the main window's input card
// until the user acts on it or dismisses it, such as "a newer yt-dlp is
// available". Unlike a dialog it does not block the window.
type notice struct {
	id          string // a notice replaces any shown notice with the same id
	text        string
	actionLabel string // label of the action button; "" for no button
	action      func() // run on the UI thread when the action button is tapped
}

// showNotice shows n in the notice area, replacing any notice with the same
// id. It is safe to call from any goroutine. Notices survive createUI
// rebuilding the window.
func (manager *UIManager) showNotice(n notice) {
	fyne.Do(func() {
		manager.notices = slices.DeleteFunc(manager.notices, func(shown notice) bool { return shown.id == n.id })
		manager.notices = append(manager.notices, n)
		manager.renderNotices()
	})
}

// dismissNotice removes the notice with the given id, if shown. It is safe
// to call from any goroutine.
func (manager *UIManager) dismissNotice(id string) {
	fyne.Do(func() {
		manager.notices = slices.DeleteFunc(manager.notices, func(shown notice) bool { return shown.id == id })
		manager.renderNotices()
	})
}

// renderNotices rebuilds the notice area from manager.notices. Must be
// called on the UI thread.
func (manager *UIManager) renderNotices() {
	if manager.noticeBox == nil {
		return
	}
	manager.noticeBox.Objects = nil
	for _, n := range manager.notices {
		manager.noticeBox.Add(manager.buildNotice(n))
	}
	manager.noticeBox.Refresh()
}

// buildNotice lays out one notice: its text, its action button (which also
// dismisses it), and a dismiss button.
func (manager *UIManager) buildNotice(n notice) fyne.CanvasObject {
	text := widget.NewLabel(n.text)
	text.Wrapping = fyne.TextWrapWord

	buttons := container.NewHBox()
	if n.actionLabel != "" {
		actionBtn := widget.NewButton(n.actionLabel, func() {
			manager.dismissNotice(n.id)
			n.action()
		})
		actionBtn.Importance = widget.HighImportance
		buttons.Add(actionBtn)
	}
	buttons.Add(widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		manager.dismissNotice(n.id)
	}))

	card := roundedCard("", container.NewBorder(nil, nil, nil, container.NewCenter(buttons), text))
	return container.NewBorder(nil, nil, accentBar(), nil, card)
}

// buildHeader constructs the app logo/title header shown atop the main window.
func buildHeader() fyne.CanvasObject {
	logo := canvas.NewImageFromResource(resourceAppiconPng)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(128, 128))

	titleText := canvas.NewText("GoVid", accentCyan)
	titleText.TextSize = 38
	titleText.TextStyle = fyne.TextStyle{Bold: true}

	subtitleText := canvas.NewText("Video Downloader", theme.Color(theme.ColorNameDisabled))
	subtitleText.TextSize = 23
	subtitleText.TextStyle = fyne.TextStyle{Italic: true}

	headerLeft := container.NewVBox(titleText, subtitleText)
	return container.NewHBox(headerLeft, layout.NewSpacer(), logo)
}

// configureEntryMode switches the URL entry between single-line and
// multi-line batch mode, and wires the batch-mode toggle's OnChanged handler.
func (manager *UIManager) configureEntryMode() {
	ui := manager.ui

	setMode := func(checked bool) {
		if checked {
			ui.download.entry.MultiLine = true
			ui.download.entry.SetMinRowsVisible(4)
			ui.download.entry.SetPlaceHolder("One URL per line...\nhttps://...\nhttps://...")
		} else {
			// Switching back to single mode: keep only the first non-empty URL.
			first := ""
			for _, line := range strings.Split(ui.download.entry.Text, "\n") {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					first = trimmed
					break
				}
			}
			ui.download.entry.SetText(first)
			ui.download.entry.MultiLine = false
			ui.download.entry.SetMinRowsVisible(1)
			ui.download.entry.SetPlaceHolder("https://www.youtube.com/watch?v=...")
		}
		ui.download.entry.Refresh()
	}

	setMode(ui.download.batchMode.Checked)

	ui.download.batchMode.OnChanged = func(checked bool) {
		manager.savePreferences(ui.download.path.Text)
		setMode(checked)
	}
}

// wireToggleHandlers attaches the OnChanged handlers for the main window's
// checkboxes and path field; all of them simply persist the current preferences.
func (manager *UIManager) wireToggleHandlers() {
	ui := manager.ui

	ui.download.saveLog.OnChanged = func(_ bool) {
		manager.savePreferences(ui.download.path.Text)
	}
	ui.download.notify.OnChanged = func(_ bool) {
		manager.savePreferences(ui.download.path.Text)
	}
	ui.download.autoRetry.OnChanged = func(_ bool) {
		manager.savePreferences(ui.download.path.Text)
	}
	ui.postProcess.enablePostProcess.OnChanged = func(_ bool) {
		manager.savePreferences(ui.download.path.Text)
	}
	ui.download.path.SetPlaceHolder("Download folder...")
	ui.download.path.OnChanged = func(text string) {
		manager.savePreferences(text)
	}
}

// wireActionButtons configures the download and cancel buttons' icons, text,
// and tap handlers.
func (manager *UIManager) wireActionButtons(themeMode string) {
	ui := manager.ui

	ui.download.downloadBtn.Icon = themedIcon(IconDownload, themeMode)
	ui.download.downloadBtn.Text = "Download Now!"
	ui.download.downloadBtn.OnTapped = func() {
		manager.onStartDownload()
	}
	ui.download.downloadBtn.Importance = widget.HighImportance
	ui.download.downloadBtn.Refresh()

	ui.download.cancelBtn.Icon = themedIcon(IconCancel, themeMode)
	ui.download.cancelBtn.Text = "Cancel"
	ui.download.cancelBtn.OnTapped = func() {
		if manager.onRequestCancel() {
			manager.onLog("Download canceled by user.", colWarning)
		}
	}
}

// buildInputCard assembles the "Specify the source and destination" card:
// URL/path entry, format/quality selectors, trim range, and the toggle/action
// button rows. Returns the card wrapped with its decorative accent bar.
func (manager *UIManager) buildInputCard(themeMode string) fyne.CanvasObject {
	ui := manager.ui

	browseBtn := widget.NewButtonWithIcon("", themedIcon(IconFolderOpen, themeMode), func() {
		dialog.ShowFolderOpen(func(list fyne.ListableURI, err error) {
			if err != nil || list == nil {
				return
			}
			ui.download.path.SetText(filepath.FromSlash(list.Path()))
		}, manager.mainWindow)
	})

	openFolderBtn := widget.NewButtonWithIcon("Open Folder", themedIcon(IconFolder, themeMode), func() {
		manager.onOpenFolder()
	})

	ui.download.trimStart.SetPlaceHolder("e.g. 00:01:30  (optional)")
	ui.download.trimEnd.SetPlaceHolder("e.g. 00:05:00  (optional)")
	ui.download.trimStart.Validator = validateTimestamp
	ui.download.trimEnd.Validator = validateTimestamp

	loadFileBtn := widget.NewButtonWithIcon("Load from file…", theme.FileTextIcon(), manager.showLoadURLFile)
	pasteBtn := widget.NewButtonWithIcon("", theme.ContentPasteIcon(), manager.pasteURLs)
	clearBtn := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		ui.download.entry.SetText("")
	})

	inputCard := roundedCard("Specify the source and destination",
		container.NewVBox(
			container.NewHBox(
				widget.NewLabelWithStyle("Video URL:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				layout.NewSpacer(),
				loadFileBtn,
				ui.download.batchMode,
			),
			container.NewBorder(nil, nil, nil, container.NewHBox(pasteBtn, clearBtn), ui.download.entry),
			widget.NewLabelWithStyle("Save Destination:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewBorder(nil, nil, nil, browseBtn, ui.download.path),
			container.NewGridWithColumns(2,
				container.NewVBox(
					widget.NewLabelWithStyle("Output Format:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					ui.download.format,
				),
				container.NewVBox(
					widget.NewLabelWithStyle("Max Quality:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					ui.download.quality,
				),
			),
			container.NewGridWithColumns(2,
				container.NewVBox(
					widget.NewLabelWithStyle("Trim Start: (optional)", fyne.TextAlignLeading, fyne.TextStyle{}),
					ui.download.trimStart,
				),
				container.NewVBox(
					widget.NewLabelWithStyle("Trim End: (optional)", fyne.TextAlignLeading, fyne.TextStyle{}),
					ui.download.trimEnd,
				),
			),
			container.NewHBox(ui.download.saveLog, ui.download.notify, ui.download.autoRetry, ui.postProcess.enablePostProcess),
			container.NewGridWithColumns(3, ui.download.downloadBtn, openFolderBtn, ui.download.cancelBtn),
		),
	)
	return container.NewBorder(nil, nil, accentBar(), nil, inputCard)
}

// buildStatusCard assembles the progress bar and status-dot indicator card.
func (manager *UIManager) buildStatusCard() fyne.CanvasObject {
	ui := manager.ui

	// Wrap the status dot in a fixed-size container so the circle renders at 18×18.
	dotContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(18, 18)), ui.download.statusDot)
	statusCard := roundedCard("",
		container.NewVBox(
			ui.download.progress,
			container.NewHBox(dotContainer, ui.download.status),
		),
	)
	return container.NewBorder(nil, nil, accentBar(), nil, statusCard)
}

// buildLogPane creates the scrollable log container and stores it on the
// widget bag so appendLogLine can append to it later. Existing log entries
// are preserved if logList is already initialized.
func (manager *UIManager) buildLogPane() *container.Scroll {
	ui := manager.ui

	if ui.download.logList == nil {
		ui.download.logList = container.NewVBox()
	}
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(0, 10))
	ui.download.output = container.NewScroll(container.NewVBox(ui.download.logList, spacer))
	ui.download.output.SetMinSize(fyne.NewSize(0, 200))
	return ui.download.output
}

// logFlushInterval is how long appendLogLine collects lines before they are
// rendered together. Verbose yt-dlp and FFmpeg output can produce hundreds of
// lines a second; rendering them in batches keeps the UI thread responsive.
const logFlushInterval = 100 * time.Millisecond

// maxScreenLogLines caps the lines kept in the log view whatever the Log
// Buffer Limit preference says, since every refresh lays out every line.
// "Unlimited" therefore applies only to the lines kept for the log file.
const maxScreenLogLines = 5000

// followTolerance is how close to the bottom, in pixels, the log view must be
// for new lines to keep it scrolled to the bottom.
const followTolerance = 8

// pendingLogLine is a log line waiting for the next flush.
type pendingLogLine struct {
	text string
	col  color.Color
}

// screenLogLimit returns the number of lines the log view keeps for the
// given Log Buffer Limit.
func screenLogLimit(bufferLimit int) int {
	return min(bufferLimit, maxScreenLogLines)
}

// appendLogLine queues one line for the graphical log view. It is registered
// on DownloaderApp as the onLogLine callback so appendOutput never touches
// widgets directly. It is safe to call from any goroutine: the first queued
// line arms a timer that renders every line queued by then in a single UI
// update (see flushLog), so an idle app does no work.
func (manager *UIManager) appendLogLine(line string, col color.Color) {
	manager.logMu.Lock()
	defer manager.logMu.Unlock()
	manager.pendingLog = append(manager.pendingLog, pendingLogLine{text: line, col: col})
	if !manager.logFlushArmed {
		manager.logFlushArmed = true
		manager.afterFunc(logFlushInterval, manager.flushLog)
	}
}

// takePendingLog removes and returns the queued log lines.
func (manager *UIManager) takePendingLog() []pendingLogLine {
	manager.logMu.Lock()
	defer manager.logMu.Unlock()
	lines := manager.pendingLog
	manager.pendingLog = nil
	manager.logFlushArmed = false
	return lines
}

// flushLog renders the queued log lines now, in one UI update. The timer
// armed by appendLogLine calls it; call it directly to show queued lines
// without waiting, e.g. so a session's summary appears as soon as it ends.
func (manager *UIManager) flushLog() {
	lines := manager.takePendingLog()
	if len(lines) == 0 {
		return
	}
	fyne.Do(func() { manager.renderLogLines(lines) })
}

// renderLogLines appends lines to the log view, trims it once to its limit,
// and refreshes it once. A yt-dlp progress line replaces the one before it
// instead of adding a line. The view follows new lines only if it was
// already at (or within followTolerance of) the bottom, so a user who
// scrolled up to read something is not pulled back down. A nil colour means
// "default text colour" and is resolved to the current theme's foreground
// here, on the UI side. Must be called on the UI thread.
func (manager *UIManager) renderLogLines(lines []pendingLogLine) {
	logList := manager.ui.download.logList
	output := manager.ui.download.output
	follow := isScrolledToBottom(output)

	for _, line := range lines {
		col := line.col
		if col == nil {
			col = theme.Color(theme.ColorNameForeground)
		}
		// Consecutive yt-dlp progress lines share one line of the view,
		// which shows the latest; the log file keeps every one.
		if IsProgressLine(line.text) {
			if last := lastLogText(logList); last != nil && IsProgressLine(last.Text) {
				last.Text, last.Color = line.text, col
				last.Refresh()
				continue
			}
		}
		label := canvas.NewText(line.text, col)
		label.TextSize = theme.TextSize()
		logList.Objects = append(logList.Objects, label)
	}

	if limit := screenLogLimit(manager.onLogBufferLimit()); len(logList.Objects) > limit {
		logList.Objects = logList.Objects[len(logList.Objects)-limit:]
	}

	logList.Refresh()
	if follow {
		// Resize the content now, as the scroll's next layout pass would:
		// ScrollToBottom clamps the offset to the content's current size,
		// which is stale until then.
		output.Content.Resize(output.Content.MinSize().Max(output.Size()))
		output.ScrollToBottom()
	}
}

// lastLogText returns the last line of the log view, or nil when it is empty.
func lastLogText(logList *fyne.Container) *canvas.Text {
	if len(logList.Objects) == 0 {
		return nil
	}
	text, _ := logList.Objects[len(logList.Objects)-1].(*canvas.Text)
	return text
}

// isScrolledToBottom reports whether scroll shows the end of its content,
// within followTolerance pixels. Content shorter than the view counts as
// scrolled to the bottom.
func isScrolledToBottom(scroll *container.Scroll) bool {
	// MinSize, like Scroll.ScrollToBottom, is current even before the next
	// layout pass has resized the content.
	hidden := scroll.Content.MinSize().Height - scroll.Size().Height
	return scroll.Offset.Y >= hidden-followTolerance
}

// clearTerminalOutput empties the terminal output window and resets its
// scroll position. Lines still queued for the view are dropped too, since
// they were logged before the clear.
func (manager *UIManager) clearTerminalOutput() {
	ui := manager.ui
	if ui == nil || ui.download.logList == nil {
		return
	}
	manager.takePendingLog()
	fyne.Do(func() {
		ui.download.logList.Objects = nil
		ui.download.logList.Refresh()
		if ui.download.output != nil {
			ui.download.output.ScrollToTop()
		}
	})
}

// buildFooter constructs the copyright line shown at the bottom of the main window.
func buildFooter() fyne.CanvasObject {
	copyright := canvas.NewText("GoVid • By David Bennehag (dunder.gg) • Built with ❤️, 🤖 and ☕", theme.Color(theme.ColorNameDisabled))
	copyright.TextSize = 14
	copyright.Alignment = fyne.TextAlignCenter
	return container.NewCenter(copyright)
}
