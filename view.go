// view.go — The View menu and the tabs below the status card.
//
// Responsibilities:
//   - ViewSettings: the parts of the main window the user chose to show.
//     PreferenceService keeps them under their own keys and, like the
//     presets, saves them whether or not "Save preferences" is on, since
//     they only change on purpose, from the View menu. Restore Defaults
//     leaves them alone.
//   - The tabs: the Queue panel (queue_panel.go) and the terminal output
//     share the room below the status card, one tab each. View → Queue and
//     View → Terminal Output (Ctrl+T) add or remove their tab; with neither,
//     the tabs are hidden and the window shrinks to fit (changeView).
//     Hiding the terminal output only hides the view: lines are still
//     logged, kept, and written to the log file.
//   - The hint: when a warning or an error is logged while the terminal
//     output is out of sight, its tab shows a warning icon, or, while the
//     tab is removed, a button in the status card offers to show it.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The preferences keys the View menu's settings are stored under.
const (
	prefShowQueue    = "showQueue"
	prefShowTerminal = "showTerminal"
)

// tabAreaMinHeight is the least height of each tab's content: about three
// queue rows, or five log lines.
const tabAreaMinHeight = 120

// ViewSettings are the parts of the main window the View menu shows or hides.
type ViewSettings struct {
	ShowQueue    bool // the Queue tab
	ShowTerminal bool // the Terminal Output tab
}

// LoadView returns the stored view settings; everything is shown by default.
func (prefSvc *PreferenceService) LoadView() ViewSettings {
	return ViewSettings{
		ShowQueue:    prefSvc.store.BoolWithFallback(prefShowQueue, true),
		ShowTerminal: prefSvc.store.BoolWithFallback(prefShowTerminal, true),
	}
}

// SaveView stores view.
func (prefSvc *PreferenceService) SaveView(view ViewSettings) {
	prefSvc.store.SetBool(prefShowQueue, view.ShowQueue)
	prefSvc.store.SetBool(prefShowTerminal, view.ShowTerminal)
}

// ── Menu ─────────────────────────────────────────────────────────────────────

// buildViewMenu returns the View menu, with each item checked while its
// tab is shown.
func (manager *UIManager) buildViewMenu() *fyne.Menu {
	manager.queueMenuItem = fyne.NewMenuItem("Queue", func() {
		manager.setQueueShown(!manager.view.ShowQueue)
	})
	manager.terminalMenuItem = fyne.NewMenuItem("Terminal Output", manager.toggleTerminal)
	manager.terminalMenuItem.Shortcut = shortcutTerminal
	manager.refreshViewMenu()
	return fyne.NewMenu("View", manager.queueMenuItem, manager.terminalMenuItem)
}

// refreshViewMenu checks the View menu's items whose tab is shown.
func (manager *UIManager) refreshViewMenu() {
	if manager.queueMenuItem == nil {
		return
	}
	manager.queueMenuItem.Checked = manager.view.ShowQueue
	manager.terminalMenuItem.Checked = manager.view.ShowTerminal
	if menu := manager.mainWindow.MainMenu(); menu != nil {
		menu.Refresh()
	}
}

// toggleTerminal is View → Terminal Output (Ctrl+T): it adds the Terminal
// Output tab if it is removed, and removes it if it is shown.
func (manager *UIManager) toggleTerminal() {
	manager.setTerminalShown(!manager.view.ShowTerminal)
}

// setTerminalShown adds or removes the Terminal Output tab; an added tab is
// selected. Must be called on the UI thread.
func (manager *UIManager) setTerminalShown(show bool) {
	manager.changeView(func(view *ViewSettings) { view.ShowTerminal = show })
	if show {
		manager.selectTab(manager.terminalTab)
		manager.ui.download.output.ScrollToBottom()
	}
}

// setQueueShown adds or removes the Queue tab; an added tab is selected.
// Must be called on the UI thread.
func (manager *UIManager) setQueueShown(show bool) {
	manager.changeView(func(view *ViewSettings) { view.ShowQueue = show })
	if show {
		manager.selectTab(manager.queueTab)
	}
}

// changeView applies change to the view settings, saves them, and shows
// the tabs they ask for, resizing the window to suit. Removing the last tab
// hides the tabs and shrinks the window by the room they leave empty;
// adding a tab back gives back the height the window had, unless the
// window has room for the tabs already, as when they were hidden while it
// was maximized and it has been restored since. A maximized or full-screen
// window keeps its size: resizing it would leave Windows treating it as
// maximized at a size it no longer has. Must be called on the UI thread.
func (manager *UIManager) changeView(change func(view *ViewSettings)) {
	before := manager.view
	change(&manager.view)
	if manager.view == before {
		return
	}
	manager.onSaveView(manager.view)
	manager.refreshViewMenu()

	window := manager.mainWindow
	content := window.Content()
	// spare is how much taller the content is than it needs to be.
	spare := func() float32 { return content.Size().Height - content.MinSize().Height }
	compact := spare() < 1
	size := window.Canvas().Size()
	resizable := !window.FullScreen() && !manager.windowMaximized(window)
	tabsBefore, tabsAfter := before.ShowQueue || before.ShowTerminal, manager.tabsShown()
	if resizable && tabsBefore && !tabsAfter {
		manager.shownHeight = size.Height
	}

	manager.applyTabs()
	content.Refresh()
	if !resizable {
		return
	}
	minHeight := size.Height - spare() // the window's height with no room to spare
	switch {
	case tabsBefore && !tabsAfter:
		size.Height = minHeight
	case !tabsBefore && compact:
		size.Height = max(manager.shownHeight, minHeight)
	default:
		size.Height = max(size.Height, minHeight)
	}
	window.Resize(size)
}

// ── Tabs ─────────────────────────────────────────────────────────────────────

// buildTabs builds the tabs below the status card around the Queue panel
// and the terminal output, showing those the View menu asks for.
func (manager *UIManager) buildTabs(queue, terminal fyne.CanvasObject) *container.AppTabs {
	manager.queueTab = container.NewTabItem("Queue", queue)
	manager.terminalTab = container.NewTabItem("Terminal Output", terminal)
	manager.tabs = container.NewAppTabs()
	manager.applyTabs()
	manager.tabs.OnSelected = func(tab *container.TabItem) {
		manager.queueSelected = tab == manager.queueTab
		if tab == manager.terminalTab {
			manager.terminalUnseen = false
			manager.applyTerminalHint()
		}
	}
	return manager.tabs
}

// tabsShown reports whether the View menu asks for any tab.
func (manager *UIManager) tabsShown() bool {
	return manager.view.ShowQueue || manager.view.ShowTerminal
}

// applyTabs shows the tabs the View menu asks for, keeping the selected one
// selected while it is shown, and hides the tabs if it asks for none. Must
// be called on the UI thread.
func (manager *UIManager) applyTabs() {
	if manager.tabs == nil {
		return
	}
	queueSelected := manager.queueSelected
	var items []*container.TabItem
	if manager.view.ShowQueue {
		items = append(items, manager.queueTab)
	}
	if manager.view.ShowTerminal {
		items = append(items, manager.terminalTab)
	}
	manager.tabs.SetItems(items)
	if queueSelected || !manager.view.ShowTerminal {
		manager.selectTab(manager.queueTab)
	} else {
		manager.selectTab(manager.terminalTab)
	}
	if manager.tabsShown() {
		manager.tabs.Show()
	} else {
		manager.tabs.Hide()
	}
	manager.applyTerminalHint()
}

// selectTab selects tab if it is shown, or else whichever tab is. Must be
// called on the UI thread.
func (manager *UIManager) selectTab(tab *container.TabItem) {
	if manager.tabs == nil || len(manager.tabs.Items) == 0 {
		return
	}
	if !manager.tabShown(tab) {
		tab = manager.tabs.Items[0]
	}
	manager.tabs.Select(tab)
	manager.queueSelected = tab == manager.queueTab
	if tab == manager.terminalTab {
		manager.terminalUnseen = false
		manager.applyTerminalHint()
	}
}

// tabShown reports whether tab is one of the tabs shown.
func (manager *UIManager) tabShown(tab *container.TabItem) bool {
	for _, item := range manager.tabs.Items {
		if item == tab {
			return true
		}
	}
	return false
}

// terminalInSight reports whether the terminal output is on screen: its
// tab is shown and selected.
func (manager *UIManager) terminalInSight() bool {
	return manager.view.ShowTerminal && manager.tabs != nil && manager.tabs.Selected() == manager.terminalTab
}

// ── Hint ─────────────────────────────────────────────────────────────────────

// buildTerminalHint returns the status card's hint that warnings or errors
// were logged while the Terminal Output tab was removed. Tapping it adds
// the tab back.
func (manager *UIManager) buildTerminalHint() *widget.Button {
	manager.terminalHint = widget.NewButtonWithIcon("New warnings: show Terminal Output", theme.WarningIcon(),
		func() { manager.setTerminalShown(true) })
	manager.terminalHint.Importance = widget.WarningImportance
	manager.applyTerminalHint()
	return manager.terminalHint
}

// noteHiddenWarnings marks the terminal output as holding unseen warnings
// if lines, just added to the log view, hold a warning or an error while
// it is out of sight. Must be called on the UI thread.
func (manager *UIManager) noteHiddenWarnings(lines []pendingLogLine) {
	if manager.terminalUnseen || manager.terminalInSight() {
		return
	}
	for _, line := range lines {
		if line.col == colWarning || line.col == colError || line.col == colErrorSoft {
			manager.terminalUnseen = true
			manager.applyTerminalHint()
			return
		}
	}
}

// applyTerminalHint shows that warnings were logged out of sight: a warning
// icon on the Terminal Output tab while it is shown, or the status card's
// button while it is removed.
func (manager *UIManager) applyTerminalHint() {
	if manager.terminalHint != nil {
		if manager.terminalUnseen && !manager.view.ShowTerminal {
			manager.terminalHint.Show()
		} else {
			manager.terminalHint.Hide()
		}
	}
	if manager.terminalTab != nil && manager.tabs != nil {
		var icon fyne.Resource
		if manager.terminalUnseen && manager.view.ShowTerminal {
			icon = theme.WarningIcon()
		}
		if manager.terminalTab.Icon != icon {
			manager.terminalTab.Icon = icon
			manager.tabs.Refresh()
		}
	}
}
