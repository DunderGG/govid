// ui_manager.go — Main window layout and singleton secondary window management.
//
// Responsibilities:
//   - UIManager: typed component that owns the primary window reference and
//     the secondary window references (About, Help, History, Preferences,
//     Post-Processing), ensuring at most one instance of each is open at a time.
//   - createUI: composes the main window layout (header, input card, status
//     card, log pane, footer) from focused builder/wiring helpers below it.
//   - createMainMenu: builds the main window's menu bar.
//   - showAbout: the About window.
//   - checkDependencies, confirmYtDlpUpdate, runUpdateInUI: thin delegates to the injected
//     dependency-service callbacks for the startup tool check and the
//     "Update yt-dlp" menu action, which runs through onUpdateYtDlp.
//   - followSystemTheme: rebuilds the window when the system theme changes.
//
// The other windows and parts of the main window have their own files:
// help_window.go (showConfigHelp), preferences_window.go (showPreferences,
// savePreferences, restoreDefaults), postprocess_window.go
// (showPostProcessing), history_window.go (showHistory), log_view.go (the
// Terminal Output log view), and notices.go (showNotice, dismissNotice).
package main

import (
	"fmt"
	"image/color"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
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
	compWindow    fyne.Window // Tools → Components
	ui            *UIWidgets  // shared widget bag; set by newDownloaderApp after construction

	// restoringDefaults suppresses savePreferences while restoreDefaults is
	// writing default values into the widgets. Only touched on the UI goroutine.
	restoringDefaults bool

	// Log lines queued by appendLogLine and not yet rendered; see flushLog.
	logMu         sync.Mutex
	pendingLog    []pendingLogLine
	logFlushArmed bool                                    // true while a flush timer is pending
	afterFunc     func(time.Duration, func()) *time.Timer // schedules the flush; time.AfterFunc, replaced in tests
	// progressLines holds, by item prefix ("" outside simultaneous
	// downloads), the log line showing that item's latest progress; see
	// renderLogLines. Only touched on the UI thread.
	progressLines map[string]*canvas.Text

	// Notices shown above the input card; see showNotice. Only touched on
	// the UI thread.
	notices   []notice
	noticeBox *fyne.Container

	// The Queue panel; see queue_panel.go. queue, queueSnapshot, queueBox,
	// and queuePanel are only touched on the UI thread.
	queue         *QueueModel
	queueSnapshot []queueEntry
	queueBox      *fyne.Container
	queuePanel    *queuePanel
	queueThrottle *latestValueThrottle[int64] // coalesces redraws; the value is queueVersion
	queueVersion  atomic.Int64

	// Presets; see preset_ui.go. Only touched on the UI thread.
	presets       []Preset
	appliedPreset *Preset // the preset last applied or saved, for "(modified)"; nil for none
	presetSelect  *widget.Select
	presetState   *widget.Label

	// Callbacks bridging DownloaderApp actions and services into the main
	// window and secondary windows; all set by newDownloaderApp after
	// construction.
	onLog                func(line string, col color.Color)                                   // appends a line to the terminal output panel
	onStatus             func(msg string)                                                     // updates the short status label
	onSetStatusIndicator func(state StatusState)                                              // updates the status dot color
	onStartDownload      func()                                                               // begins a download/batch run
	onOpenFolder         func()                                                               // opens the save destination in the system file manager
	onRequestCancel      func() bool                                                          // cancels the active download or post-process job
	onRecording          func() bool                                                          // DownloaderApp.recording.Load: a live stream is being recorded
	onPauseResume        func()                                                               // DownloaderApp.pauseOrResume: the main window's Pause / Resume button
	onPauseItem          func(id int)                                                         // DownloaderApp.pauseItem: a queue row's Pause
	onSkipItem           func(id int) bool                                                    // DownloaderApp.skipItem: a queue row's Skip
	onShowFormats        func()                                                               // DownloaderApp.showFormatsForURL: the Formats… button
	onCopyDiagnostics    func()                                                               // DownloaderApp.copyDiagnostics: Help → Copy diagnostics
	onIsPortable         func() bool                                                          // DownloaderApp.portable: settings are kept beside GoVid
	onSetPortable        func(on bool, revert func())                                         // DownloaderApp.onPortableChanged: the Portable Mode toggle
	onItemFormats        func(id int)                                                         // DownloaderApp.showFormatsForItem: a waiting row's Formats…
	onDiscardPaused      func(id int)                                                         // DownloaderApp.discardPausedItem: a paused row's Remove
	onLoadHistory        func() ([]DownloadHistoryEntry, error)                               // HistoryService.Load
	onClearHistory       func() error                                                         // HistoryService.Clear
	onCheckDependencies  func(onWarning func(msg string))                                     // DependencyService.Check
	onRunUpdate          func(cb UpdateCallbacks)                                             // DependencyService.RunUpdate
	onUpdateYtDlp        func(onDone func(err error))                                         // DownloaderApp.updateYtDlp: guards runUpdateThen
	onYtDlpVersions      func() (installed, latest string)                                    // DownloaderApp.ytDlpVersions
	onJSRuntimeLabel     func() string                                                        // DownloaderApp.jsRuntimeLabel
	onComponents         func() []componentStatus                                             // DownloaderApp.componentStatuses
	onComponentAction    func(name, action string, onDone func(err error))                    // DownloaderApp.installComponent
	onCheckGoVidRelease  func() (release Release, newer bool, err error)                      // DownloaderApp.checkGoVidRelease
	onCanSelfUpdate      func(release Release) bool                                           // DownloaderApp.canSelfUpdate
	onSelfUpdate         func(release Release)                                                // DownloaderApp.runSelfUpdate
	onLoadPreferences    func() AppPreferences                                                // PreferenceService.Load
	onSavePreferences    func(AppPreferences)                                                 // PreferenceService.Save
	onResetPreferences   func()                                                               // PreferenceService.Reset
	onLoadConfigFile     func(path string) (*AppConfig, error)                                // PreferenceService.LoadFromFile
	onMergeConfig        func(cfg *AppConfig, base AppPreferences) (AppPreferences, []string) // PreferenceService.MergeConfig
	onExportConfig       func(path string, p AppPreferences) error                            // PreferenceService.ExportConfig + WriteConfigFile
	onLoadPresets        func() []Preset                                                      // PreferenceService.LoadPresets
	onSavePresets        func([]Preset)                                                       // PreferenceService.SavePresets
	onReadPresets        func(path string) ([]Preset, []string, error)                        // PreferenceService.ReadPresetFile
	onWritePresets       func(path string, presets []Preset) error                            // PreferenceService.WritePresetFile
	onSetLogBufferLimit  func(limit int)                                                      // LogService.SetBufferLimit
	onLogBufferLimit     func() int                                                           // LogService.BufferLimit
	onSetShowDebug       func(show bool)                                                      // DownloaderApp.setDebug
	onSetKeepHistory     func(keep bool)                                                      // DownloaderApp.keepHistory.Store
	onSessionRunning     func() bool                                                          // DownloaderApp.isRunning.Load
}

// NewUIManager returns a UIManager bound to the given primary window.
func NewUIManager(mainWindow fyne.Window) *UIManager {
	manager := &UIManager{mainWindow: mainWindow, afterFunc: time.AfterFunc, progressLines: map[string]*canvas.Text{}}
	manager.queueThrottle = newLatestValueThrottle(statusThrottleInterval, func(int64) {
		fyne.Do(manager.refreshQueue)
	})
	return manager
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

// mustParseURL parses rawURL for a hyperlink. It is for the constant URLs
// GoVid links to, which parse, so like regexp.MustCompile it panics if one
// does not; a URL from elsewhere goes through url.Parse and its error.
func mustParseURL(rawURL string) *url.URL {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		panic(fmt.Sprintf("mustParseURL(%q): %v", rawURL, err))
	}
	return parsed
}

// ── Main menu ──────────────────────────────────────────────────────────────────

// createMainMenu builds the application's top-level menu bar.
func (manager *UIManager) createMainMenu() {
	// withShortcut returns a menu item that shows shortcut beside its label.
	withShortcut := func(label string, shortcut fyne.Shortcut, action func()) *fyne.MenuItem {
		item := fyne.NewMenuItem(label, action)
		item.Shortcut = shortcut
		return item
	}

	clearLogMenu := fyne.NewMenuItem("Clear Terminal Output", func() {
		manager.clearTerminalOutput()
	})

	updateMenu := fyne.NewMenuItem("Update yt-dlp", manager.confirmYtDlpUpdate)

	aboutMenu := fyne.NewMenuItem("About GoVid", func() {
		manager.showAbout()
	})

	mainMenu := fyne.NewMainMenu(
		fyne.NewMenu("File",
			withShortcut("Download", shortcutDownload, manager.startDownloadFromKeyboard),
			withShortcut("Paste URLs", shortcutPasteURLs, manager.pasteURLs),
			withShortcut("Load URLs from file…", shortcutLoadFile, manager.showLoadURLFile),
			withShortcut("Open save folder", shortcutOpenFolder, func() { manager.onOpenFolder() }),
			fyne.NewMenuItemSeparator(),
			withShortcut("History", shortcutHistory, manager.showHistory),
			fyne.NewMenuItemSeparator(),
			clearLogMenu,
		),
		fyne.NewMenu("Tools",
			updateMenu,
			fyne.NewMenuItem("Components…", manager.showComponents),
			fyne.NewMenuItem("Check for GoVid updates", manager.checkForGoVidUpdates),
			fyne.NewMenuItemSeparator(),
			withShortcut("Preferences", shortcutPreferences, manager.showPreferences),
			fyne.NewMenuItem("Import settings…", manager.showImportSettings),
			fyne.NewMenuItem("Export settings…", manager.showExportSettings),
			fyne.NewMenuItem("Post-Processing", func() {
				manager.showPostProcessing()
			}),
		),
		fyne.NewMenu("Help",
			withShortcut("GoVid Guide", shortcutGuide, manager.showConfigHelp),
			fyne.NewMenuItem("Copy diagnostics", func() { manager.onCopyDiagnostics() }),
			fyne.NewMenuItemSeparator(),
			aboutMenu,
		),
	)
	manager.mainWindow.SetMainMenu(mainMenu)
	manager.registerShortcuts()
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

// runUpdateInUI updates yt-dlp for the Tools menu and the out-of-date
// notice. It goes through the injected onUpdateYtDlp, which refuses while
// a download or an install runs, and shows why when it does.
func (manager *UIManager) runUpdateInUI() {
	manager.onUpdateYtDlp(func(err error) {
		if err != nil {
			dialog.ShowError(fmt.Errorf("yt-dlp was not updated: %w", err), manager.mainWindow)
		}
	})
}

// runUpdateThen sets the initial UI state for an update and delegates
// execution to DependencyService, which runs yt-dlp -U in a background
// goroutine and reports progress via UpdateCallbacks. A successful update
// dismisses the "yt-dlp is out of date" notice. done (when not nil) is
// called from the update's goroutine once it has finished, with whether it
// succeeded. Callers go through DownloaderApp.updateYtDlp, which guards it.
func (manager *UIManager) runUpdateThen(done func(ok bool)) {
	if done == nil {
		done = func(bool) {}
	}
	manager.onLog("[SYSTEM] Starting yt-dlp update...", colSystem)
	manager.onSetStatusIndicator(StatusActive)
	manager.onStatus("Status: Updating yt-dlp...")
	manager.onRunUpdate(UpdateCallbacks{
		OnLog:    manager.onLog,
		OnStatus: manager.onStatus,
		OnSuccess: func() {
			manager.onSetStatusIndicator(StatusSuccess)
			manager.dismissNotice(ytDlpNoticeID)
			done(true)
		},
		OnFailure: func() {
			manager.onSetStatusIndicator(StatusFailed)
			done(false)
		},
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
	runtimeLabel := widget.NewLabelWithStyle("JS runtime: checking…", fyne.TextAlignCenter, fyne.TextStyle{Monospace: true})
	go func() {
		installed, latest := manager.onYtDlpVersions()
		jsRuntime := manager.onJSRuntimeLabel()
		fyne.Do(func() {
			ytDlpLabel.SetText(fmt.Sprintf("yt-dlp %s (latest: %s)", installed, latest))
			runtimeLabel.SetText("JS runtime: " + jsRuntime)
		})
	}()
	tagline := widget.NewLabelWithStyle("A high-performance video downloader\nbuilt with Go and Fyne.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	author := widget.NewLabelWithStyle("Created by David Bennehag", fyne.TextAlignCenter, fyne.TextStyle{})
	website := widget.NewHyperlink("dunder.gg", mustParseURL("https://dunder.gg"))
	github := widget.NewHyperlink("github.com/DunderGG/govid", mustParseURL("https://github.com/DunderGG/govid"))
	links := container.NewHBox(layout.NewSpacer(), website, widget.NewLabel("•"), github, layout.NewSpacer())

	content := container.NewVBox(
		container.NewCenter(logo),
		container.NewCenter(appName),
		container.NewCenter(versionLabel),
		container.NewCenter(ytDlpLabel),
		container.NewCenter(runtimeLabel),
		container.NewCenter(tagline),
		widget.NewSeparator(),
		container.NewCenter(author),
		links,
	)

	manager.aboutWindow = fyne.CurrentApp().NewWindow("About GoVid")
	manager.aboutWindow.SetContent(container.NewPadded(content))
	manager.aboutWindow.Resize(fyne.NewSize(360, 340))
	manager.aboutWindow.SetFixedSize(true)
	manager.aboutWindow.SetOnClosed(onWindowClosed(&manager.aboutWindow))
	closeOnEscape(manager.aboutWindow)
	manager.aboutWindow.Show()
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
	manager.queueBox = container.NewVBox()
	manager.renderQueuePanel()

	topContent := container.NewVBox(
		header,
		manager.noticeBox,
		inputCard,
		statusCard,
		manager.queueBox,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Terminal Output:", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
	)

	content := container.NewBorder(topContent, footer, nil, nil, logPane)
	manager.mainWindow.SetContent(container.NewPadded(content))
	manager.mainWindow.SetOnDropped(func(_ fyne.Position, items []fyne.URI) {
		manager.handleDrop(items)
	})
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
	// Format and quality are saved when a download starts, but a change
	// still marks the applied preset as modified.
	ui.download.format.OnChanged = func(string) { manager.refreshPresetState() }
	ui.download.quality.OnChanged = func(string) { manager.refreshPresetState() }
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
		// Stopping a recording keeps it, and the engine says so.
		recording := manager.onRecording()
		if manager.onRequestCancel() && !recording {
			manager.onLog("Download canceled by user.", colWarning)
		}
	}

	// The Pause button reads "Resume" while the queue holds only paused
	// downloads; DownloaderApp.setPauseControl keeps it current.
	ui.download.pauseBtn.Icon = theme.MediaPauseIcon()
	ui.download.pauseBtn.OnTapped = manager.onPauseResume
	ui.download.pauseBtn.Refresh()
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

	urlHeader, urlRow := manager.buildURLRows()
	inputCard := roundedCard("Specify the source and destination",
		container.NewVBox(
			urlHeader,
			urlRow,
			widget.NewLabelWithStyle("Save Destination:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			container.NewBorder(nil, nil, nil, browseBtn, ui.download.path),
			manager.buildSelectorsRow(),
			manager.buildTrimRow(),
			container.NewHBox(ui.download.saveLog, ui.download.notify, ui.download.autoRetry, ui.postProcess.enablePostProcess),
			container.NewGridWithColumns(4, ui.download.downloadBtn, openFolderBtn, ui.download.pauseBtn, ui.download.cancelBtn),
		),
	)
	return container.NewBorder(nil, nil, accentBar(), nil, inputCard)
}

// buildURLRows returns the input card's "Video URL:" header, with the
// Formats…, Load from file…, and Batch Mode controls, and the URL field's
// row, with its paste and clear buttons.
func (manager *UIManager) buildURLRows() (header, row fyne.CanvasObject) {
	ui := manager.ui
	loadFileBtn := widget.NewButtonWithIcon("Load from file…", theme.FileTextIcon(), manager.showLoadURLFile)
	formatsBtn := widget.NewButtonWithIcon("Formats…", theme.ListIcon(), manager.onShowFormats)
	pasteBtn := widget.NewButtonWithIcon("", theme.ContentPasteIcon(), manager.pasteURLs)
	clearBtn := widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() {
		ui.download.entry.SetText("")
	})

	header = container.NewHBox(
		widget.NewLabelWithStyle("Video URL:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		formatsBtn,
		loadFileBtn,
		ui.download.batchMode,
	)
	row = container.NewBorder(nil, nil, nil, container.NewHBox(pasteBtn, clearBtn), ui.download.entry)
	return header, row
}

// buildSelectorsRow lays out the Preset, Output Format, and Max Quality
// selectors side by side.
func (manager *UIManager) buildSelectorsRow() fyne.CanvasObject {
	ui := manager.ui
	return container.NewGridWithColumns(3,
		container.NewVBox(
			widget.NewLabelWithStyle("Preset:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			manager.buildPresetRow(),
		),
		container.NewVBox(
			widget.NewLabelWithStyle("Output Format:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			ui.download.format,
		),
		container.NewVBox(
			widget.NewLabelWithStyle("Max Quality:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			ui.download.quality,
		),
	)
}

// buildTrimRow lays out the Trim Start and Trim End fields side by side,
// each checked with validateTimestamp.
func (manager *UIManager) buildTrimRow() fyne.CanvasObject {
	ui := manager.ui
	ui.download.trimStart.SetPlaceHolder("e.g. 00:01:30  (optional)")
	ui.download.trimEnd.SetPlaceHolder("e.g. 00:05:00  (optional)")
	ui.download.trimStart.Validator = validateTimestamp
	ui.download.trimEnd.Validator = validateTimestamp

	return container.NewGridWithColumns(2,
		container.NewVBox(
			widget.NewLabelWithStyle("Trim Start: (optional)", fyne.TextAlignLeading, fyne.TextStyle{}),
			ui.download.trimStart,
		),
		container.NewVBox(
			widget.NewLabelWithStyle("Trim End: (optional)", fyne.TextAlignLeading, fyne.TextStyle{}),
			ui.download.trimEnd,
		),
	)
}

// buildStatusCard assembles the progress bar and status-dot indicator card.
func (manager *UIManager) buildStatusCard() fyne.CanvasObject {
	ui := manager.ui

	// The recording view hides progressBox, not the bar inside it, which
	// the progress smoother updates from its own goroutine.
	ui.download.progressBox = container.NewStack(ui.download.progress)
	if manager.onRecording != nil && manager.onRecording() {
		ui.download.progressBox.Hide()
	}

	// Wrap the status dot in a fixed-size container so the circle renders at 18×18.
	dotContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(18, 18)), ui.download.statusDot)
	statusCard := roundedCard("",
		container.NewVBox(
			container.NewStack(ui.download.progressBox, ui.download.progressLive),
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

// buildFooter constructs the copyright line shown at the bottom of the main window.
func buildFooter() fyne.CanvasObject {
	copyright := canvas.NewText("GoVid • By David Bennehag (dunder.gg) • Built with ❤️, 🤖 and ☕", theme.Color(theme.ColorNameDisabled))
	copyright.TextSize = 14
	copyright.Alignment = fyne.TextAlignCenter
	return container.NewCenter(copyright)
}

// followSystemTheme rebuilds the main window when the operating system
// switches between light and dark while the theme is System: Fyne repaints
// its widgets itself, but the window's own colours and icons are chosen
// when it is built.
func (manager *UIManager) followSystemTheme(settings fyne.Settings) {
	var mu sync.Mutex
	last := settings.ThemeVariant()
	settings.AddListener(func(changed fyne.Settings) {
		mu.Lock()
		variant := changed.ThemeVariant()
		switched := variant != last
		last = variant
		mu.Unlock()
		if switched && manager.onLoadPreferences().ThemeMode == themeSystem {
			fyne.Do(manager.createUI)
		}
	})
}
