package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// newViewTestManager returns a UIManager with its main menu and main window
// built, the window sized taller than it needs to be.
func newViewTestManager(t *testing.T) (*DownloaderApp, fyne.Window) {
	t.Helper()
	_ = test.NewApp()
	window := test.NewWindow(nil)
	app := newDownloaderApp(window)
	app.uiManager.createMainMenu()
	app.uiManager.createUI()
	window.Resize(fyne.NewSize(windowWidth, window.Content().MinSize().Height+150))
	return app, window
}

func TestLoadViewShowsEverythingByDefault(t *testing.T) {
	_ = test.NewApp()
	prefSvc := NewPreferenceService(fyne.CurrentApp().Preferences())
	if !prefSvc.LoadView().ShowTerminal {
		t.Error("the terminal output is hidden by default")
	}
	prefSvc.SaveView(ViewSettings{ShowTerminal: false})
	if prefSvc.LoadView().ShowTerminal {
		t.Error("the terminal output is shown after saving it hidden")
	}
}

func TestHidingTheTerminalShrinksTheWindowAndShowingItGivesTheHeightBack(t *testing.T) {
	app, window := newViewTestManager(t)
	mgr := app.uiManager
	before := window.Canvas().Size()
	terminalHeight := mgr.terminal.Size().Height

	mgr.setTerminalShown(false)
	if mgr.terminal.Visible() {
		t.Error("the terminal output is still shown")
	}
	if mgr.terminalMenuItem.Checked {
		t.Error("View → Terminal Output is still checked")
	}
	if app.prefSvc.LoadView().ShowTerminal {
		t.Error("hiding the terminal output was not saved")
	}
	if got, want := window.Canvas().Size().Height, before.Height-terminalHeight; got != want {
		t.Errorf("window height = %v after hiding, want %v", got, want)
	}
	if got := window.Canvas().Size().Width; got != before.Width {
		t.Errorf("window width = %v after hiding, want it unchanged at %v", got, before.Width)
	}

	mgr.setTerminalShown(true)
	if !mgr.terminal.Visible() || !mgr.terminalMenuItem.Checked || !app.prefSvc.LoadView().ShowTerminal {
		t.Error("the terminal output is not shown, checked, and saved after showing it")
	}
	if got := window.Canvas().Size(); got != before {
		t.Errorf("window size = %v after showing again, want %v", got, before)
	}
}

func TestHiddenTerminalStaysHiddenWhenTheWindowIsRebuilt(t *testing.T) {
	app, _ := newViewTestManager(t)
	mgr := app.uiManager
	mgr.setTerminalShown(false)

	mgr.createUI() // as a theme change or Restore Defaults does
	if mgr.terminal.Visible() {
		t.Error("the rebuilt window shows the terminal output")
	}
}

func TestStartingWithTheTerminalHiddenLeavesItOutOfTheMinimumSize(t *testing.T) {
	_ = test.NewApp()
	fyne.CurrentApp().Preferences().SetBool(prefShowTerminal, false)
	window := test.NewWindow(nil)
	app := newDownloaderApp(window)
	app.uiManager.createMainMenu()
	app.uiManager.createUI()
	if app.uiManager.terminalMenuItem.Checked {
		t.Error("View → Terminal Output is checked while it is hidden")
	}
	hiddenMin := window.Content().MinSize().Height

	app.uiManager.setTerminalShown(true)
	if shownMin := window.Content().MinSize().Height; shownMin <= hiddenMin {
		t.Errorf("minimum height %v with the terminal output shown, want more than %v without it", shownMin, hiddenMin)
	}
	if got, min := window.Canvas().Size().Height, window.Content().MinSize().Height; got < min {
		t.Errorf("window height = %v after showing the terminal output, below its minimum %v", got, min)
	}
}

func TestAMaximizedWindowKeepsItsSize(t *testing.T) {
	app, window := newViewTestManager(t)
	mgr := app.uiManager
	mgr.windowMaximized = func(fyne.Window) bool { return true }
	before := window.Canvas().Size()

	mgr.setTerminalShown(false)
	if mgr.terminal.Visible() {
		t.Error("the terminal output is still shown")
	}
	if got := window.Canvas().Size(); got != before {
		t.Errorf("maximized window resized to %v when hiding, want %v", got, before)
	}
	mgr.setTerminalShown(true)
	if got := window.Canvas().Size(); got != before {
		t.Errorf("maximized window resized to %v when showing, want %v", got, before)
	}
}

func TestShowingTheTerminalUsesRoomTheWindowAlreadyHas(t *testing.T) {
	app, window := newViewTestManager(t)
	mgr := app.uiManager
	normal := window.Canvas().Size()

	// Hidden while maximized, then restored to the size it had before.
	mgr.windowMaximized = func(fyne.Window) bool { return true }
	window.Resize(fyne.NewSize(normal.Width, normal.Height+400))
	mgr.setTerminalShown(false)
	mgr.windowMaximized = func(fyne.Window) bool { return false }
	window.Resize(normal)

	mgr.setTerminalShown(true)
	if got := window.Canvas().Size(); got != normal {
		t.Errorf("window size = %v after showing, want it unchanged at %v", got, normal)
	}
}

func TestHintShowsWhenAWarningIsLoggedWhileTheTerminalIsHidden(t *testing.T) {
	app, _ := newViewTestManager(t)
	mgr := app.uiManager
	mgr.setTerminalShown(false)

	mgr.renderLogLines([]pendingLogLine{{text: "[download] Destination: a.mp4"}})
	if mgr.terminalHint.Visible() {
		t.Error("the hint shows for a plain line")
	}
	mgr.renderLogLines([]pendingLogLine{{text: "[WARNING] something", col: colWarning}})
	if !mgr.terminalHint.Visible() {
		t.Fatal("the hint does not show for a warning")
	}

	mgr.createUI() // as a theme change does
	if !mgr.terminalHint.Visible() {
		t.Error("the rebuilt window lost the hint")
	}

	test.Tap(mgr.terminalHint)
	if !mgr.terminal.Visible() || mgr.terminalHint.Visible() {
		t.Error("tapping the hint did not show the terminal output and hide the hint")
	}

	mgr.renderLogLines([]pendingLogLine{{text: "[ERROR] shown", col: colError}})
	if mgr.terminalHint.Visible() {
		t.Error("the hint shows for an error logged while the terminal output is shown")
	}
}

func TestClearingTheTerminalHidesTheHint(t *testing.T) {
	app, _ := newViewTestManager(t)
	mgr := app.uiManager
	mgr.setTerminalShown(false)
	mgr.renderLogLines([]pendingLogLine{{text: "[ERROR] failed", col: colError}})

	mgr.clearTerminalOutput()
	if mgr.terminalHint.Visible() {
		t.Error("the hint still shows after clearing the terminal output")
	}
}

func TestCtrlTTogglesTheTerminal(t *testing.T) {
	app, window := newViewTestManager(t)
	press := func() { window.Canvas().(fyne.Shortcutable).TypedShortcut(shortcutTerminal) }

	press()
	if app.uiManager.terminal.Visible() {
		t.Error("Ctrl+T did not hide the terminal output")
	}
	press()
	if !app.uiManager.terminal.Visible() {
		t.Error("Ctrl+T did not show the terminal output again")
	}
}
