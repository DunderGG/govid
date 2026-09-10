// GoVid: A high-performance, cross-platform video downloader.
//
// This application provides a graphical interface for the powerful 'yt-dlp'
// command-line tool. It supports multiple formats (MP4, MKV, MP3, etc.),
// real-time progress tracking, and post-processing via FFMPEG.
//
// Built with Go and the Fyne toolkit.
// Author: David Bennehag (dunder.gg)
package main

import (
	"flag"
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
)

const (
	// Default window size for the application.
	windowWidth  = 750
	windowHeight = 550

	// UI formatting constants.
	fpsInterval = 20 // Progress smoothing interval (in milliseconds).
)

var (
	// version is injected at release build time via:
	//	-X main.version=1.0.0
	// Falls back to "dev" for local builds.
	version = "dev"
)

// newDownloaderApp constructs and fully initialises a DownloaderApp.
func newDownloaderApp(window fyne.Window) *DownloaderApp {
	depSvc := NewDependencyService()
	dlApp := &DownloaderApp{
		window:    window,
		uiManager: NewUIManager(window),
		ui:        NewUIWidgets(),
		stats:     &DownloadStats{},
		logSvc:    NewLogService(),
		depSvc:    depSvc,
		gpuSvc:    NewGPUCapabilityService(depSvc.Resolve("ffmpeg")),
	}

	// Load saved preferences and apply them to all widgets.
	dlApp.prefSvc = NewPreferenceService(fyne.CurrentApp().Preferences())
	prefs := dlApp.prefSvc.Load()
	applyPreferencesToWidgets(dlApp.ui, prefs)
	dlApp.logSvc.SetBufferLimit(ParseBufferLimit(prefs.LogLimit))

	// Wire history service to both the app and the UIManager's callbacks.
	dlApp.historySvc = NewHistoryService()
	dlApp.uiManager.onLoadHistory = dlApp.historySvc.Load
	dlApp.uiManager.onClearHistory = dlApp.historySvc.Clear

	// Wire the widgets and preference/log-service callbacks showPreferences
	// and createUI need.
	dlApp.uiManager.ui = dlApp.ui
	dlApp.uiManager.onLoadPreferences = dlApp.prefSvc.Load
	dlApp.uiManager.onSavePreferences = dlApp.prefSvc.Save
	dlApp.uiManager.onResetPreferences = dlApp.prefSvc.Reset
	dlApp.uiManager.onLoadConfigFile = dlApp.prefSvc.LoadFromFile
	dlApp.uiManager.onMergeConfig = dlApp.prefSvc.MergeConfig
	dlApp.uiManager.onSetLogBufferLimit = dlApp.logSvc.SetBufferLimit
	dlApp.uiManager.onLogBufferLimit = dlApp.logSvc.BufferLimit

	// Wire the dependency-service callbacks and log/status callbacks for
	// checkDependencies and the "Update yt-dlp" menu action.
	dlApp.uiManager.onCheckDependencies = depSvc.Check
	dlApp.uiManager.onRunUpdate = depSvc.RunUpdate
	dlApp.uiManager.onLog = dlApp.appendOutput
	dlApp.uiManager.onStatus = dlApp.updateStatus
	dlApp.uiManager.onSetStatusIndicator = dlApp.setStatusIndicator

	// appendOutput delegates the widget-mutation half of logging to UIManager,
	// which owns the log widgets' lifecycle.
	dlApp.onLogLine = dlApp.uiManager.appendLogLine

	// Wire the main window's action callbacks (download, open folder, cancel).
	dlApp.uiManager.onStartDownload = dlApp.startDownload
	dlApp.uiManager.onOpenFolder = dlApp.openDownloadFolder
	dlApp.uiManager.onRequestCancel = dlApp.RequestCancel
	return dlApp
}

// main is the entry point of the application. It initializes the Fyne app,
// sets up the DownloaderApp coordinator, starts the UI creation process,
// and shows the main window.
func main() {
	updateFlag := flag.Bool("update", false, "Update yt-dlp to the latest version")
	flag.Parse()

	if *updateFlag {
		if err := UpdateYtDlpCLI(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(int(exitCodeFromError(err)))
		}
		return
	}

	mainApp := app.NewWithID("com.govid.downloader")

	// Apply the user's preferred theme or the custom GoVid theme.
	themePref := mainApp.Preferences().StringWithFallback(prefThemeMode, defaultThemeMode)
	switch themePref {
	case "Light":
		mainApp.Settings().SetTheme(&lightTheme{})
	default:
		// Force the custom theme to use Dark variant for its base
		mainApp.Settings().SetTheme(&darkTheme{})
	}

	// Set the custom brand icon using the bundled resource.
	mainApp.SetIcon(resourceAppiconPng)

	mainWindow := mainApp.NewWindow("GoVid")
	mainWindow.Resize(fyne.NewSize(windowWidth, windowHeight))
	mainWindow.SetIcon(resourceAppiconPng)

	dlApp := newDownloaderApp(mainWindow)
	dlApp.uiManager.createMainMenu()
	dlApp.uiManager.createUI()
	dlApp.uiManager.checkDependencies()
	dlApp.startGPUDetection()

	// Show a confirmation dialog if a download or post-processing job is active.
	mainWindow.SetCloseIntercept(func() {
		if dlApp.isRunning.Load() {
			dialog.ShowConfirm(
				"Job in Progress",
				"A download or post-processing job is currently running.\nAre you sure you want to quit?",
				func(confirmed bool) {
					if confirmed {
						dlApp.RequestCancel()
						mainApp.Quit()
					}
				},
				mainWindow,
			)
			return
		}
		mainApp.Quit()
	})

	mainWindow.ShowAndRun()
}
