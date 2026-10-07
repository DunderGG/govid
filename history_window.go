// history_window.go — The Download History window.
//
// Responsibilities:
//   - UIManager.showHistory: a searchable list of past downloads, newest
//     first, each with Re-add, Show in folder, and Copy URL actions, and a
//     Clear History button. Entries whose file no longer exists are greyed
//     out.
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

// showHistory opens a window listing previously downloaded URLs from disk.
// It is a singleton: if already open, the existing window is focused instead.
func (manager *UIManager) showHistory() {
	if focusOrCreate(&manager.historyWindow) {
		return
	}

	entries, err := manager.onLoadHistory()
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to load download history: %v", err), manager.mainWindow)
		return
	}
	view := newHistoryView(entries, fileExists)

	actions := historyActions{
		readd:   manager.readdHistoryURL,
		reveal:  manager.revealHistoryFile,
		copyURL: func(url string) { fyne.CurrentApp().Clipboard().SetContent(url) },
	}
	list := widget.NewList(
		func() int { return len(view.shown) },
		func() fyne.CanvasObject { return newHistoryRow() },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			entry, missing := view.shownEntry(id)
			obj.(*historyRow).show(entry, missing, actions)
		},
	)
	empty := widget.NewLabelWithStyle("No download history yet.", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	countLabel := widget.NewLabel(view.count())
	refresh := func() {
		countLabel.SetText(view.count())
		list.UnselectAll()
		list.Refresh()
		if len(view.entries) == 0 {
			empty.Show()
		} else {
			empty.Hide()
		}
	}

	search := widget.NewEntry()
	search.SetPlaceHolder("Search title, URL, or file name…")
	search.OnChanged = func(text string) {
		view.search(text)
		refresh()
	}

	clearBtn := widget.NewButton("Clear History", func() {
		manager.confirmClearHistory(manager.historyWindow, func() {
			view = newHistoryView(nil, fileExists)
			refresh()
		})
	})
	clearBtn.Importance = widget.DangerImportance

	top := container.NewBorder(nil, nil, nil, countLabel, search)
	bottomBar := container.NewHBox(layout.NewSpacer(), clearBtn)
	content := container.NewBorder(top, bottomBar, nil, nil, container.NewStack(list, container.NewCenter(empty)))
	refresh()

	manager.historyWindow = fyne.CurrentApp().NewWindow("Download History")
	manager.historyWindow.SetContent(container.NewPadded(content))
	manager.historyWindow.Resize(fyne.NewSize(900, 520))
	manager.historyWindow.SetOnClosed(onWindowClosed(&manager.historyWindow))
	closeOnEscape(manager.historyWindow)
	manager.historyWindow.Show()
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
