// history_window.go — The Download History window.
//
// Responsibilities:
//   - UIManager.showHistory: a searchable list of past downloads, newest
//     first, each with Re-add, Show in folder, and Copy URL actions, and a
//     Clear History button. Entries whose file no longer exists are greyed
//     out. The window opens at once; the history is loaded off the UI thread.
//   - historyPanel: the window's widgets, loading until the history arrives.
//   - historyView: the list's state (entries, which files are missing, the
//     search result), kept apart from the widgets so it can be tested.
//   - historyRow: one row of the list.
package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// historyView holds the History window's entries and which of them the
// search shows.
type historyView struct {
	entries []DownloadHistoryEntry // newest first
	missing []bool                 // per entry: it names a file that no longer exists
	shown   []int                  // indexes into entries that match the search
}

// newHistoryView returns a view of entries (oldest first, as stored) that
// shows all of them, newest first. exists reports whether a file exists.
func newHistoryView(entries []DownloadHistoryEntry, exists func(path string) bool) *historyView {
	view := &historyView{entries: slices.Clone(entries)}
	slices.Reverse(view.entries)
	view.missing = make([]bool, len(view.entries))
	for i, entry := range view.entries {
		if path := entry.FilePath(); path != "" {
			view.missing[i] = !exists(path)
		}
	}
	view.search("")
	return view
}

// search shows the entries whose title, URL, file name, or format contains
// query, ignoring case; an empty query shows every entry.
func (view *historyView) search(query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	view.shown = view.shown[:0]
	for i, entry := range view.entries {
		if query == "" || historyMatches(entry, query) {
			view.shown = append(view.shown, i)
		}
	}
}

// historyMatches reports whether entry contains the lower-case query.
func historyMatches(entry DownloadHistoryEntry, query string) bool {
	for _, field := range []string{entry.OriginalTitle, entry.URL, entry.FinalFilename, entry.Format} {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

// shownEntry returns the entry at position id of the search result, and
// whether its file is missing.
func (view *historyView) shownEntry(id int) (DownloadHistoryEntry, bool) {
	index := view.shown[id]
	return view.entries[index], view.missing[index]
}

// count describes how many entries the search shows, e.g. "3 of 40 downloads".
func (view *historyView) count() string {
	if len(view.shown) == len(view.entries) {
		return plural(len(view.entries), "download", "downloads")
	}
	return fmt.Sprintf("%d of %s", len(view.shown), plural(len(view.entries), "download", "downloads"))
}

// historyDetails is the second line of a history row: when, in which format
// and quality, and the file name, marked when the file is gone.
func historyDetails(entry DownloadHistoryEntry, missing bool) string {
	details := []string{entry.DownloadedAt}
	if entry.Format != "" {
		details = append(details, strings.TrimSuffix(entry.Format+" / "+entry.Quality, " / "))
	}
	details = append(details, entry.DisplayFile())
	text := strings.Join(details, "  ·  ")
	if missing {
		text += "  (file not found)"
	}
	return text
}

// historyActions are what a history row's buttons do.
type historyActions struct {
	readd   func(url string)  // put the URL back in the URL field
	reveal  func(path string) // show the file in the file manager
	copyURL func(url string)  // copy the URL to the clipboard
}

// historyRow is one entry of the History list: its title and details, and
// its action buttons.
type historyRow struct {
	widget.BaseWidget
	title   *widget.Label
	details *widget.Label
	readd   *widget.Button
	reveal  *widget.Button
	copyURL *widget.Button
}

// newHistoryRow returns an empty row for the list to fill with show.
func newHistoryRow() *historyRow {
	row := &historyRow{
		title:   widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		details: widget.NewLabel(""),
		readd:   widget.NewButtonWithIcon("Re-add", theme.ContentAddIcon(), nil),
		reveal:  widget.NewButtonWithIcon("Show in folder", theme.FolderOpenIcon(), nil),
		copyURL: widget.NewButtonWithIcon("Copy URL", theme.ContentCopyIcon(), nil),
	}
	row.title.Truncation = fyne.TextTruncateEllipsis
	row.details.Truncation = fyne.TextTruncateEllipsis
	row.ExtendBaseWidget(row)
	return row
}

// CreateRenderer lays the row out: the text on the left, the buttons on
// the right.
func (row *historyRow) CreateRenderer() fyne.WidgetRenderer {
	buttons := container.NewHBox(row.readd, row.reveal, row.copyURL)
	text := container.NewVBox(row.title, row.details)
	return widget.NewSimpleRenderer(container.NewBorder(nil, nil, nil, container.NewCenter(buttons), text))
}

// show fills the row with entry. A missing file greys the row out and
// disables "Show in folder".
func (row *historyRow) show(entry DownloadHistoryEntry, missing bool, actions historyActions) {
	importance := widget.MediumImportance
	if missing {
		importance = widget.LowImportance
	}
	row.title.Importance = importance
	row.details.Importance = importance
	row.title.SetText(entry.DisplayTitle())
	row.details.SetText(historyDetails(entry, missing))

	row.readd.OnTapped = func() { actions.readd(entry.URL) }
	row.copyURL.OnTapped = func() { actions.copyURL(entry.URL) }
	row.reveal.OnTapped = func() { actions.reveal(entry.FilePath()) }
	if missing || entry.FilePath() == "" {
		row.reveal.Disable()
	} else {
		row.reveal.Enable()
	}
}

// fileExists reports whether a file exists at path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// historyPanel is the History window's content: the search field, the
// count, the list, and Clear History. It starts out loading (see
// loadHistory) and shows a historyView once the history has been read.
type historyPanel struct {
	view       *historyView
	list       *widget.List
	status     *widget.Label // over the list: loading, no history, or why it failed to load
	countLabel *widget.Label
	search     *widget.Entry
	clearBtn   *widget.Button
	content    fyne.CanvasObject
}

// newHistoryPanel returns the panel in its loading state, with the search
// and Clear History disabled. Clear History confirms over window.
func (manager *UIManager) newHistoryPanel(window fyne.Window) *historyPanel {
	panel := &historyPanel{view: newHistoryView(nil, fileExists)}
	actions := historyActions{
		readd:   manager.readdHistoryURL,
		reveal:  manager.revealHistoryFile,
		copyURL: func(url string) { fyne.CurrentApp().Clipboard().SetContent(url) },
	}
	panel.list = widget.NewList(
		func() int { return len(panel.view.shown) },
		func() fyne.CanvasObject { return newHistoryRow() },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			entry, missing := panel.view.shownEntry(id)
			obj.(*historyRow).show(entry, missing, actions)
		},
	)
	panel.status = widget.NewLabelWithStyle("Loading the download history…", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	panel.status.Wrapping = fyne.TextWrapWord
	panel.countLabel = widget.NewLabel("")

	panel.search = widget.NewEntry()
	panel.search.SetPlaceHolder("Search title, URL, or file name…")
	panel.search.OnChanged = func(text string) {
		panel.view.search(text)
		panel.refresh()
	}
	panel.search.Disable()

	panel.clearBtn = widget.NewButton("Clear History", func() {
		manager.confirmClearHistory(window, func() { panel.show(newHistoryView(nil, fileExists)) })
	})
	panel.clearBtn.Importance = widget.DangerImportance
	panel.clearBtn.Disable()

	top := container.NewBorder(nil, nil, nil, panel.countLabel, panel.search)
	bottomBar := container.NewHBox(layout.NewSpacer(), panel.clearBtn)
	panel.content = container.NewBorder(top, bottomBar, nil, nil, container.NewStack(panel.list, container.NewCenter(panel.status)))
	return panel
}

// show replaces the panel's entries with view's and enables the search and
// Clear History. Must be called on the UI thread.
func (panel *historyPanel) show(view *historyView) {
	panel.view = view
	panel.view.search(panel.search.Text)
	panel.status.SetText("No download history yet.")
	panel.search.Enable()
	panel.clearBtn.Enable()
	panel.refresh()
}

// showLoadError says why the history could not be loaded. Clear History
// stays available, so a damaged history file can still be cleared.
func (panel *historyPanel) showLoadError(err error) {
	panel.status.SetText(fmt.Sprintf("Failed to load the download history: %v", err))
	panel.clearBtn.Enable()
}

// refresh redraws the count and the list after the view has changed.
func (panel *historyPanel) refresh() {
	panel.countLabel.SetText(panel.view.count())
	panel.list.UnselectAll()
	panel.list.Refresh()
	if len(panel.view.entries) == 0 {
		panel.status.Show()
	} else {
		panel.status.Hide()
	}
}

// showHistory opens a window listing previously downloaded URLs from disk.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showHistory() {
	if focusOrCreate(&manager.historyWindow) {
		return
	}
	manager.openHistory()
}

// openHistory opens the History window, at once, and starts loading the
// history into it (see loadHistory). The returned channel is closed once
// the window shows the history, or why it could not be loaded.
func (manager *UIManager) openHistory() <-chan struct{} {
	window := fyne.CurrentApp().NewWindow("Download History")
	manager.historyWindow = window
	panel := manager.newHistoryPanel(window)
	window.SetContent(container.NewPadded(panel.content))
	window.Resize(fyne.NewSize(900, 520))
	window.SetOnClosed(onWindowClosed(&manager.historyWindow))
	closeOnEscape(window)
	window.Show()
	return manager.loadHistory(window, panel)
}

// loadHistory reads the history and checks which entries' files still
// exist, off the UI thread (§2.2): a file on a disconnected drive or network
// share can take seconds to answer, and every entry is checked. It then
// fills panel, unless window has been closed meanwhile. The returned channel
// is closed once that is done.
func (manager *UIManager) loadHistory(window fyne.Window, panel *historyPanel) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		entries, err := manager.onLoadHistory()
		var view *historyView
		if err == nil {
			view = newHistoryView(entries, fileExists)
		}
		fyne.DoAndWait(func() {
			switch {
			case manager.historyWindow != window:
				// Closed while loading.
			case err != nil:
				panel.showLoadError(err)
			default:
				panel.show(view)
			}
		})
	}()
	return done
}

// confirmClearHistory asks, over parent, whether to delete the download
// history, and deletes it on yes, then calls onCleared.
func (manager *UIManager) confirmClearHistory(parent fyne.Window, onCleared func()) {
	dialog.ShowConfirm(
		"Clear Download History",
		"Are you sure you want to clear all download history? This cannot be undone.",
		func(ok bool) {
			if !ok {
				return
			}
			if err := manager.onClearHistory(); err != nil {
				dialog.ShowError(fmt.Errorf("failed to clear history: %v", err), parent)
				return
			}
			onCleared()
		},
		parent,
	)
}

// readdHistoryURL puts a history entry's URL back in the URL field (see
// addURLs) and brings the main window forward.
func (manager *UIManager) readdHistoryURL(url string) {
	if manager.addURLs([]string{url}) == 0 {
		manager.onLog(fmt.Sprintf("[SYSTEM] %s is already in the URL field.", url), colSystem)
	}
	manager.mainWindow.RequestFocus()
}

// revealHistoryFile shows a downloaded file in the system file manager.
func (manager *UIManager) revealHistoryFile(path string) {
	cmd := revealFileCommand(path)
	if err := cmd.Start(); err != nil {
		dialog.ShowError(fmt.Errorf("could not show %s: %w", path, err), manager.historyWindow)
		return
	}
	go cmd.Wait() // reap the process; the file manager reports nothing useful
}
