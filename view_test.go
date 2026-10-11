package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
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
	if view := prefSvc.LoadView(); !view.ShowQueue || !view.ShowTerminal {
		t.Errorf("LoadView() = %+v by default, want everything shown", view)
	}
	saved := ViewSettings{ShowQueue: false, ShowTerminal: true}
	prefSvc.SaveView(saved)
	if got := prefSvc.LoadView(); got != saved {
		t.Errorf("LoadView() = %+v after saving %+v", got, saved)
	}
}

func TestBothTabsShowAtStartWithTheTerminalSelected(t *testing.T) {
	app, _ := newViewTestManager(t)
	mgr := app.uiManager
	if !mgr.tabShown(mgr.queueTab) || !mgr.tabShown(mgr.terminalTab) || !mgr.tabs.Visible() {
		t.Error("the Queue and Terminal Output tabs are not both shown")
	}
	if mgr.tabs.Selected() != mgr.terminalTab {
		t.Error("the Terminal Output tab is not selected while there is no queue")
	}
	if !mgr.queuePanel.empty.Visible() || mgr.queuePanel.list.Visible() {
		t.Error("the Queue tab does not show its note while there is no queue")
	}
}

func TestRemovingOneTabKeepsTheOtherAndTheWindowSize(t *testing.T) {
	app, window := newViewTestManager(t)
	mgr := app.uiManager
	before := window.Canvas().Size()

	mgr.setTerminalShown(false)
	if mgr.tabShown(mgr.terminalTab) || mgr.tabs.Selected() != mgr.queueTab {
		t.Error("removing Terminal Output did not leave only the Queue tab, selected")
	}
	if mgr.terminalMenuItem.Checked || !mgr.queueMenuItem.Checked {
		t.Error("the View menu's check marks do not match the tabs")
	}
	if app.prefSvc.LoadView().ShowTerminal {
		t.Error("removing Terminal Output was not saved")
	}
	if got := window.Canvas().Size(); got != before {
		t.Errorf("window size = %v with one tab left, want it unchanged at %v", got, before)
	}

	mgr.setTerminalShown(true)
	if !mgr.tabShown(mgr.terminalTab) || mgr.tabs.Selected() != mgr.terminalTab {
		t.Error("adding Terminal Output back did not show and select it")
	}
}

func TestRemovingBothTabsShrinksTheWindowAndAddingOneGivesTheHeightBack(t *testing.T) {
	app, window := newViewTestManager(t)
	mgr := app.uiManager
	before := window.Canvas().Size()
	tabsHeight := mgr.tabs.Size().Height

	mgr.setQueueShown(false)
	mgr.setTerminalShown(false)
	if mgr.tabs.Visible() {
		t.Error("the tabs are still shown with both removed")
	}
	if got, want := window.Canvas().Size().Height, before.Height-tabsHeight; got != want {
		t.Errorf("window height = %v with no tabs, want %v", got, want)
	}
	if got := window.Canvas().Size().Width; got != before.Width {
		t.Errorf("window width = %v with no tabs, want it unchanged at %v", got, before.Width)
	}

	mgr.setQueueShown(true)
	if !mgr.tabs.Visible() || mgr.tabs.Selected() != mgr.queueTab {
		t.Error("adding the Queue tab back did not show it, selected")
	}
	if got := window.Canvas().Size(); got != before {
		t.Errorf("window size = %v after adding a tab back, want %v", got, before)
	}
}

func TestTabsAndTheirSelectionSurviveARebuild(t *testing.T) {
	app, _ := newViewTestManager(t)
	mgr := app.uiManager
	mgr.selectTab(mgr.queueTab)

	mgr.createUI() // as a theme change or Restore Defaults does
	if mgr.tabs.Selected() != mgr.queueTab {
		t.Error("the rebuilt window did not keep the Queue tab selected")
	}

	mgr.setQueueShown(false)
	mgr.setTerminalShown(false)
	mgr.createUI()
	if mgr.tabs.Visible() {
		t.Error("the rebuilt window shows the tabs that were removed")
	}
}

func TestStartingWithNoTabsLeavesThemOutOfTheMinimumSize(t *testing.T) {
	_ = test.NewApp()
	fyne.CurrentApp().Preferences().SetBool(prefShowQueue, false)
	fyne.CurrentApp().Preferences().SetBool(prefShowTerminal, false)
	window := test.NewWindow(nil)
	app := newDownloaderApp(window)
	app.uiManager.createMainMenu()
	app.uiManager.createUI()
	if app.uiManager.terminalMenuItem.Checked || app.uiManager.queueMenuItem.Checked {
		t.Error("the View menu checks tabs that are removed")
	}
	hiddenMin := window.Content().MinSize().Height

	app.uiManager.setTerminalShown(true)
	if shownMin := window.Content().MinSize().Height; shownMin <= hiddenMin {
		t.Errorf("minimum height %v with a tab shown, want more than %v without", shownMin, hiddenMin)
	}
	if got, min := window.Canvas().Size().Height, window.Content().MinSize().Height; got < min {
		t.Errorf("window height = %v after adding a tab, below its minimum %v", got, min)
	}
}

func TestAMaximizedWindowKeepsItsSize(t *testing.T) {
	app, window := newViewTestManager(t)
	mgr := app.uiManager
	mgr.windowMaximized = func(fyne.Window) bool { return true }
	before := window.Canvas().Size()

	mgr.setQueueShown(false)
	mgr.setTerminalShown(false)
	if mgr.tabs.Visible() {
		t.Error("the tabs are still shown")
	}
	if got := window.Canvas().Size(); got != before {
		t.Errorf("maximized window resized to %v when hiding the tabs, want %v", got, before)
	}
	mgr.setTerminalShown(true)
	if got := window.Canvas().Size(); got != before {
		t.Errorf("maximized window resized to %v when showing a tab, want %v", got, before)
	}
}

func TestShowingATabUsesRoomTheWindowAlreadyHas(t *testing.T) {
	app, window := newViewTestManager(t)
	mgr := app.uiManager
	normal := window.Canvas().Size()

	// Hidden while maximized, then restored to the size it had before.
	mgr.windowMaximized = func(fyne.Window) bool { return true }
	window.Resize(fyne.NewSize(normal.Width, normal.Height+400))
	mgr.setQueueShown(false)
	mgr.setTerminalShown(false)
	mgr.windowMaximized = func(fyne.Window) bool { return false }
	window.Resize(normal)

	mgr.setTerminalShown(true)
	if got := window.Canvas().Size(); got != normal {
		t.Errorf("window size = %v after showing, want it unchanged at %v", got, normal)
	}
}

func TestWarningsLoggedOutOfSightAreMarked(t *testing.T) {
	app, _ := newViewTestManager(t)
	mgr := app.uiManager
	warning := []pendingLogLine{{text: "[WARNING] something", col: colWarning}}

	// In sight: nothing to mark.
	mgr.renderLogLines(warning)
	if mgr.terminalUnseen {
		t.Error("a warning logged in sight is marked as unseen")
	}

	// Behind the Queue tab: the Terminal Output tab gets an icon.
	mgr.selectTab(mgr.queueTab)
	mgr.renderLogLines([]pendingLogLine{{text: "[download] Destination: a.mp4"}})
	if mgr.terminalTab.Icon != nil {
		t.Error("the tab is marked for a plain line")
	}
	mgr.renderLogLines(warning)
	if mgr.terminalTab.Icon != theme.WarningIcon() || mgr.terminalHint.Visible() {
		t.Error("a warning behind the Queue tab does not mark the Terminal Output tab, or shows the button")
	}
	mgr.tabs.Select(mgr.terminalTab)
	if mgr.terminalTab.Icon != nil || mgr.terminalUnseen {
		t.Error("selecting the Terminal Output tab did not clear its mark")
	}

	// With the tab removed: the status card's button.
	mgr.setTerminalShown(false)
	mgr.renderLogLines([]pendingLogLine{{text: "[ERROR] failed", col: colError}})
	if !mgr.terminalHint.Visible() {
		t.Fatal("an error logged with the tab removed does not show the button")
	}
	mgr.createUI() // as a theme change does
	if !mgr.terminalHint.Visible() {
		t.Error("the rebuilt window lost the button")
	}
	test.Tap(mgr.terminalHint)
	if mgr.tabs.Selected() != mgr.terminalTab || mgr.terminalHint.Visible() {
		t.Error("tapping the button did not show the Terminal Output tab and hide the button")
	}
}

func TestClearingTheTerminalClearsTheMark(t *testing.T) {
	app, _ := newViewTestManager(t)
	mgr := app.uiManager
	mgr.setTerminalShown(false)
	mgr.renderLogLines([]pendingLogLine{{text: "[ERROR] failed", col: colError}})

	mgr.clearTerminalOutput()
	if mgr.terminalHint.Visible() {
		t.Error("the button still shows after clearing the terminal output")
	}
}

func TestCtrlTTogglesTheTerminal(t *testing.T) {
	app, window := newViewTestManager(t)
	press := func() { window.Canvas().(fyne.Shortcutable).TypedShortcut(shortcutTerminal) }

	press()
	if app.uiManager.tabShown(app.uiManager.terminalTab) {
		t.Error("Ctrl+T did not remove the Terminal Output tab")
	}
	press()
	if !app.uiManager.tabShown(app.uiManager.terminalTab) {
		t.Error("Ctrl+T did not add the Terminal Output tab back")
	}
}
