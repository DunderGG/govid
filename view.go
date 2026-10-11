// view.go — The View menu: which parts of the main window are shown.
//
// Responsibilities:
//   - ViewSettings: the parts of the main window the user chose to show.
//     PreferenceService keeps them under their own keys and, like the
//     presets, saves them whether or not "Save preferences" is on, since
//     they only change on purpose, from the View menu. Restore Defaults
//     leaves them alone.
//   - UIManager.setTerminalShown: View → Terminal Output (Ctrl+T). Hiding
//     the terminal output only hides the view: lines are still logged,
//     kept, and written to the log file, and show when it is shown again.
//   - The hint: a button in the status card, shown when a warning or an
//     error is logged while the terminal output is hidden.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// prefShowTerminal is the preferences key View → Terminal Output is stored
// under.
const prefShowTerminal = "showTerminal"

// ViewSettings are the parts of the main window the View menu shows or hides.
type ViewSettings struct {
	ShowTerminal bool // the terminal output below the status card
}

// LoadView returns the stored view settings; everything is shown by default.
func (prefSvc *PreferenceService) LoadView() ViewSettings {
	return ViewSettings{
		ShowTerminal: prefSvc.store.BoolWithFallback(prefShowTerminal, true),
	}
}

// SaveView stores view.
func (prefSvc *PreferenceService) SaveView(view ViewSettings) {
	prefSvc.store.SetBool(prefShowTerminal, view.ShowTerminal)
}

// buildViewMenu returns the View menu, with each item checked while its
// part of the window is shown.
func (manager *UIManager) buildViewMenu() *fyne.Menu {
	manager.terminalMenuItem = fyne.NewMenuItem("Terminal Output", manager.toggleTerminal)
	manager.terminalMenuItem.Shortcut = shortcutTerminal
	manager.terminalMenuItem.Checked = manager.view.ShowTerminal
	return fyne.NewMenu("View", manager.terminalMenuItem)
}

// toggleTerminal is View → Terminal Output (Ctrl+T): it shows the terminal
// output if it is hidden, and hides it if it is shown.
func (manager *UIManager) toggleTerminal() {
	manager.setTerminalShown(!manager.view.ShowTerminal)
}

// setTerminalShown shows or hides the terminal output, remembers the
// choice, and resizes the window to suit. Hiding it shrinks the window by
// the room it leaves empty; showing it again gives back the height the
// window had, unless the window has room for it already, as when it was
// hidden while maximized and the window has been restored since. A
// maximized or full-screen window keeps its size: resizing it would leave
// Windows treating it as maximized at a size it no longer has. Must be
// called on the UI thread.
func (manager *UIManager) setTerminalShown(show bool) {
	if show == manager.view.ShowTerminal {
		return
	}
	manager.view.ShowTerminal = show
	manager.onSaveView(manager.view)
	if manager.terminalMenuItem != nil {
		manager.terminalMenuItem.Checked = show
		if menu := manager.mainWindow.MainMenu(); menu != nil {
			menu.Refresh()
		}
	}
	if show {
		manager.terminalUnseen = false
	}
	manager.applyTerminalHint()

	window := manager.mainWindow
	content := window.Content()
	// spare is how much taller the content is than it needs to be.
	spare := func() float32 { return content.Size().Height - content.MinSize().Height }
	compact := spare() < 1
	size := window.Canvas().Size()
	resizable := !window.FullScreen() && !manager.windowMaximized(window)
	if resizable && !show {
		manager.shownHeight = size.Height
	}

	manager.applyTerminalShown()
	content.Refresh()
	if show {
		manager.ui.download.output.ScrollToBottom()
	}
	if !resizable {
		return
	}

	minHeight := size.Height - spare() // the window's height with no room to spare
	switch {
	case !show:
		size.Height = minHeight
	case compact:
		size.Height = max(manager.shownHeight, minHeight)
	default:
		size.Height = max(size.Height, minHeight)
	}
	window.Resize(size)
}

// applyTerminalShown shows or hides the terminal output as the View menu
// says, without resizing the window.
func (manager *UIManager) applyTerminalShown() {
	if manager.terminal == nil {
		return
	}
	if manager.view.ShowTerminal {
		manager.terminal.Show()
	} else {
		manager.terminal.Hide()
	}
}

// ── Hint ─────────────────────────────────────────────────────────────────────

// buildTerminalHint returns the status card's hint that warnings or errors
// were logged while the terminal output was hidden. Tapping it shows the
// terminal output.
func (manager *UIManager) buildTerminalHint() *widget.Button {
	manager.terminalHint = widget.NewButtonWithIcon("New warnings: show Terminal Output", theme.WarningIcon(),
		func() { manager.setTerminalShown(true) })
	manager.terminalHint.Importance = widget.WarningImportance
	manager.applyTerminalHint()
	return manager.terminalHint
}

// noteHiddenWarnings shows the hint if lines, just added to the log view,
// hold a warning or an error while the terminal output is hidden. Must be
// called on the UI thread.
func (manager *UIManager) noteHiddenWarnings(lines []pendingLogLine) {
	if manager.view.ShowTerminal || manager.terminalUnseen {
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

// applyTerminalHint shows the hint while warnings logged with the terminal
// output hidden have not been seen.
func (manager *UIManager) applyTerminalHint() {
	if manager.terminalHint == nil {
		return
	}
	if manager.terminalUnseen && !manager.view.ShowTerminal {
		manager.terminalHint.Show()
	} else {
		manager.terminalHint.Hide()
	}
}
