// shortcuts.go — Keyboard shortcuts.
//
// Responsibilities:
//   - The main window's shortcuts (Ctrl+Enter, Ctrl+O, Ctrl+L,
//     Ctrl+Shift+V, Ctrl+H, Ctrl+,, F1), shown on their menu items. Fyne
//     runs a menu item's shortcut before the focused widget sees the keys,
//     so they work while typing in the URL field. They are also added to
//     the window's canvas (registerShortcuts).
//   - F1 and Esc have no modifier, so Fyne passes them only to the focused
//     widget, or to the window when nothing has the focus: F1 opens the
//     guide from the main window, and Esc closes the secondary windows
//     (closeOnEscape) when no text field has the cursor.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// The shortcuts, with KeyModifierShortcutDefault: Ctrl on Windows and
// Linux, Cmd on macOS.
var (
	shortcutDownload    = &desktop.CustomShortcut{KeyName: fyne.KeyReturn, Modifier: fyne.KeyModifierShortcutDefault}
	shortcutOpenFolder  = &desktop.CustomShortcut{KeyName: fyne.KeyO, Modifier: fyne.KeyModifierShortcutDefault}
	shortcutLoadFile    = &desktop.CustomShortcut{KeyName: fyne.KeyL, Modifier: fyne.KeyModifierShortcutDefault}
	shortcutPasteURLs   = &desktop.CustomShortcut{KeyName: fyne.KeyV, Modifier: fyne.KeyModifierShortcutDefault | fyne.KeyModifierShift}
	shortcutHistory     = &desktop.CustomShortcut{KeyName: fyne.KeyH, Modifier: fyne.KeyModifierShortcutDefault}
	shortcutPreferences = &desktop.CustomShortcut{KeyName: fyne.KeyComma, Modifier: fyne.KeyModifierShortcutDefault}
	// shortcutGuide is shown on the Guide menu item; F1 itself is handled
	// by the window (see registerShortcuts), as a key without a modifier
	// is never a shortcut to Fyne.
	shortcutGuide = &desktop.CustomShortcut{KeyName: fyne.KeyF1}
)

// mainShortcut is one shortcut of the main window and what it does.
type mainShortcut struct {
	shortcut *desktop.CustomShortcut
	action   func()
}

// mainShortcuts returns the main window's shortcuts with modifiers.
func (manager *UIManager) mainShortcuts() []mainShortcut {
	return []mainShortcut{
		{shortcutDownload, manager.startDownloadFromKeyboard},
		{shortcutOpenFolder, func() { manager.onOpenFolder() }},
		{shortcutLoadFile, manager.showLoadURLFile},
		{shortcutPasteURLs, manager.pasteURLs},
		{shortcutHistory, manager.showHistory},
		{shortcutPreferences, manager.showPreferences},
	}
}

// startDownloadFromKeyboard is Ctrl+Enter: the Download button, when it can
// be pressed.
func (manager *UIManager) startDownloadFromKeyboard() {
	if manager.ui.download.downloadBtn.Disabled() {
		return
	}
	manager.onStartDownload()
}

// registerShortcuts adds the main window's shortcuts to its canvas, and F1
// for the guide. Must be called on the UI thread.
func (manager *UIManager) registerShortcuts() {
	canvas := manager.mainWindow.Canvas()
	for _, entry := range manager.mainShortcuts() {
		action := entry.action
		canvas.RemoveShortcut(entry.shortcut)
		canvas.AddShortcut(entry.shortcut, func(fyne.Shortcut) { action() })
	}
	canvas.SetOnTypedKey(func(event *fyne.KeyEvent) {
		if event.Name == fyne.KeyF1 {
			manager.showConfigHelp()
		}
	})
}

// closeOnEscape makes Esc close window, when no text field in it has the
// cursor.
func closeOnEscape(window fyne.Window) {
	window.Canvas().SetOnTypedKey(func(event *fyne.KeyEvent) {
		if event.Name == fyne.KeyEscape {
			window.Close()
		}
	})
}
