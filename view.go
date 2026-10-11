// view.go — The View menu: which parts of the main window are shown.
//
// Responsibilities:
//   - ViewSettings: the parts of the main window the user chose to show.
//     PreferenceService keeps them under their own keys and, like the
//     presets, saves them whether or not "Save preferences" is on, since
//     they only change on purpose, from the View menu. Restore Defaults
//     leaves them alone.
//   - UIManager.setTerminalShown: View → Terminal Output. Hiding the
//     terminal output only hides the view: lines are still logged, kept,
//     and written to the log file, and show when it is shown again.
package main

import "fyne.io/fyne/v2"

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
	manager.terminalMenuItem = fyne.NewMenuItem("Terminal Output", func() {
		manager.setTerminalShown(!manager.view.ShowTerminal)
	})
	manager.terminalMenuItem.Checked = manager.view.ShowTerminal
	return fyne.NewMenu("View", manager.terminalMenuItem)
}

// setTerminalShown shows or hides the terminal output, remembers the
// choice, and resizes the window to suit: hiding it shrinks the window by
// the terminal's height, and showing it again gives that height back. Must
// be called on the UI thread.
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

	size := manager.mainWindow.Canvas().Size()
	if show {
		size.Height += manager.terminalHeight
	} else {
		manager.terminalHeight = manager.terminal.Size().Height
		size.Height -= manager.terminalHeight
	}
	manager.applyTerminalShown()
	content := manager.mainWindow.Content()
	content.Refresh()
	manager.mainWindow.Resize(size.Max(content.MinSize()))
	if show {
		manager.ui.download.output.ScrollToBottom()
	}
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
