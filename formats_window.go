// formats_window.go — The Format Browser window, and the formats users pick
// in it.
//
// Responsibilities:
//   - UIManager.showFormatWindow: a table of a video's formats (see
//     formatRows) with a filter (all / video only / audio only / video +
//     audio). The rows the current settings would download are marked,
//     and choosing a video row and an audio row (or one combined row) picks
//     them: "Use these formats" passes the -f value ("247+251") back,
//     "Automatic" clears it.
//   - formatChoice: the video and audio rows chosen so far.
//   - DownloaderApp.showFormatsForURL / showFormatsForItem: the Formats…
//     button next to the URL field, for the single URL there (the pick is
//     kept for that URL until a session queues it), and the Queue panel's
//     Formats… on a waiting row (the pick is set on the item).
package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// formatColumnWidths are the widths of the Format Browser's columns, in
// formatColumns order.
var formatColumnWidths = []float32{48, 96, 40, 56, 56, 56, 64, 72, 96}

// formatChoice is the video and audio format chosen in the Format Browser.
type formatChoice struct {
	video, audio FormatInfo
}

// choose adds format to the choice: a video-only format replaces the
// video, an audio-only one the audio, and a combined one both.
func (choice *formatChoice) choose(format FormatInfo) {
	switch format.kind() {
	case kindVideo:
		choice.video = format
	case kindAudio:
		choice.audio = format
		if choice.video.kind() == kindCombined {
			choice.video = FormatInfo{}
		}
	case kindCombined:
		choice.video, choice.audio = format, FormatInfo{}
	}
}

// pick returns the choice as a -f value.
func (choice formatChoice) pick() string {
	return pickFormats(choice.video, choice.audio)
}

// chosen reports whether format is part of the choice.
func (choice formatChoice) chosen(format FormatInfo) bool {
	return format.ID != "" && (format.ID == choice.video.ID || format.ID == choice.audio.ID)
}

// choiceFor returns the choice the current pick (or, without one, the
// probe's selection) makes.
func choiceFor(info MediaInfo, pick string) formatChoice {
	var choice formatChoice
	formats, _ := selectedFormats(info, pick)
	for _, format := range formats {
		choice.choose(format)
	}
	return choice
}

// showFormatWindow shows info's formats. extension is the output
// container (the Format setting), pick the current -f pick ("" for the
// automatic choice). onPick gets the new pick, "" for automatic. Must be
// called on the UI thread.
func (manager *UIManager) showFormatWindow(info MediaInfo, pick, extension string, onPick func(pick string)) dialog.Dialog {
	choice := choiceFor(info, pick)
	filter := filterAll
	rows := formatRows(info, filter)

	summary := widget.NewLabel("")
	summary.Wrapping = fyne.TextWrapWord
	useBtn := widget.NewButton("Use these formats", nil)
	useBtn.Importance = widget.HighImportance
	refreshSummary := func() {
		current := choice.pick()
		text := describeDownload(info, current, extension)
		if text == "" {
			summary.SetText("Choose a video row and an audio row, or one row with both.")
			useBtn.Disable()
			return
		}
		summary.SetText(fmt.Sprintf("Will download %s: %s", current, text))
		useBtn.Enable()
	}

	list := widget.NewList(
		func() int { return len(rows) },
		func() fyne.CanvasObject { return newFormatRowView() },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < len(rows) {
				showFormatRow(obj.(*fyne.Container), rows[id], choice.chosen(rows[id].format))
			}
		},
	)
	list.OnSelected = func(id widget.ListItemID) {
		if id < len(rows) {
			choice.choose(rows[id].format)
			refreshSummary()
			list.Refresh()
		}
		list.UnselectAll()
	}

	filterSelect := widget.NewSelect(formatFilterOptions, func(label string) {
		for i, option := range formatFilterOptions {
			if option == label {
				filter = formatFilter(i)
			}
		}
		rows = formatRows(info, filter)
		list.Refresh()
	})
	filterSelect.SetSelected(formatFilterOptions[filterAll])

	header := newFormatRowView()
	showFormatRow(header, formatRow{cells: formatColumns}, false)
	for _, label := range formatRowLabels(header) {
		label.TextStyle = fyne.TextStyle{Bold: true}
		label.Refresh()
	}
	title := widget.NewLabelWithStyle(info.Title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	title.Truncation = fyne.TextTruncateEllipsis
	top := container.NewVBox(title,
		container.NewBorder(nil, nil, widget.NewLabel("Show:"), nil, fixedWidth(filterSelect, 160)),
		widget.NewLabel("● marks what will download. Click a video row and an audio row (or one with both) to choose."),
		header)
	content := container.NewBorder(top, summary, nil, nil, list)

	var dlg *dialog.CustomDialog
	useBtn.OnTapped = func() {
		dlg.Hide()
		onPick(choice.pick())
	}
	autoBtn := widget.NewButton("Automatic", func() {
		dlg.Hide()
		onPick("")
	})
	dlg = dialog.NewCustomWithoutButtons("Formats", content, manager.mainWindow)
	dlg.SetButtons([]fyne.CanvasObject{widget.NewButton("Cancel", func() { dlg.Hide() }), autoBtn, useBtn})
	refreshSummary()
	dlg.Resize(fyne.NewSize(760, 560))
	dlg.Show()
	return dlg
}

// newFormatRowView returns an empty row of the Format Browser: a marker
// and one fixed-width label per column.
func newFormatRowView() *fyne.Container {
	row := container.NewHBox()
	row.Add(fixedWidthLabel(20))
	for _, width := range formatColumnWidths {
		row.Add(fixedWidthLabel(width))
	}
	return row
}

// fixedWidthLabel returns a truncating label laid out at width.
func fixedWidthLabel(width float32) fyne.CanvasObject {
	label := widget.NewLabel("")
	label.Truncation = fyne.TextTruncateEllipsis
	return container.New(&fixedWidthLayout{width: width}, label)
}

// fixedWidthLayout lays its one object out at a fixed width.
type fixedWidthLayout struct {
	width float32
}

func (layout *fixedWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(layout.width, objects[0].MinSize().Height)
}

func (layout *fixedWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Resize(size)
	objects[0].Move(fyne.NewPos(0, 0))
}

// showFormatRow fills a row view with row's cells, marking it when chosen.
func showFormatRow(view *fyne.Container, row formatRow, chosen bool) {
	labels := formatRowLabels(view)
	mark := ""
	if chosen {
		mark = "●"
	}
	labels[0].SetText(mark)
	for i, cell := range row.cells {
		if i+1 < len(labels) {
			labels[i+1].SetText(cell)
			labels[i+1].TextStyle = fyne.TextStyle{Bold: chosen}
		}
	}
}

// formatRowLabels returns a row view's labels, marker first.
func formatRowLabels(view *fyne.Container) []*widget.Label {
	labels := make([]*widget.Label, 0, len(view.Objects))
	for _, obj := range view.Objects {
		if cell, ok := obj.(*fyne.Container); ok {
			labels = append(labels, cell.Objects[0].(*widget.Label))
		} else if label, ok := obj.(*widget.Label); ok {
			labels = append(labels, label)
		}
	}
	return labels
}

// ── App ──────────────────────────────────────────────────────────────────────

// formatPicks holds the formats picked for URLs in the URL field, until a
// session queues them.
type formatPicks struct {
	mu    sync.Mutex
	byURL map[string]string
}

// set keeps pick for url; "" forgets it.
func (picks *formatPicks) set(url, pick string) {
	picks.mu.Lock()
	defer picks.mu.Unlock()
	if picks.byURL == nil {
		picks.byURL = map[string]string{}
	}
	if pick == "" {
		delete(picks.byURL, url)
		return
	}
	picks.byURL[url] = pick
}

// get returns the pick for url, or "".
func (picks *formatPicks) get(url string) string {
	picks.mu.Lock()
	defer picks.mu.Unlock()
	return picks.byURL[url]
}

// take returns the pick for url and forgets it.
func (picks *formatPicks) take(url string) string {
	picks.mu.Lock()
	defer picks.mu.Unlock()
	pick := picks.byURL[url]
	delete(picks.byURL, url)
	return pick
}

// checkingFormatsStatus is the status while Formats… probes a URL outside a
// session.
const checkingFormatsStatus = "Status: Checking the formats…"

// showFormatsForURL is the Formats… button next to the URL field: it
// probes the single URL there, off the UI thread, and shows its formats;
// a pick is kept for that URL until it is downloaded. Must be called on
// the UI thread.
func (app *DownloaderApp) showFormatsForURL() {
	urls, err := collectURLs(app.ui.download.entry.Text, app.ui.download.batchMode.Checked)
	switch {
	case err != nil:
		dialog.ShowError(err, app.window)
		return
	case len(urls) > 1:
		dialog.ShowInformation("Formats", "Formats… works on one URL. For videos in a batch, use the Formats button on their rows in the Queue panel once the queue has started.", app.window)
		return
	}
	url := urls[0]
	req := app.newDownloadRequest(url, strings.TrimSpace(app.ui.download.path.Text), "", "")
	req.FormatPick = app.formatPicks.get(url)
	app.probeFormatsForURL(url, req)
}

// probeFormatsForURL probes req off the UI thread and shows its formats, or
// why they could not be listed. It shows checkingFormatsStatus meanwhile,
// unless a session is running: the button works during a session too, and
// the session's status must stay. The returned channel is closed once the
// formats or the error are shown. Must be called on the UI thread.
func (app *DownloaderApp) probeFormatsForURL(url string, req DownloadRequest) <-chan struct{} {
	if !app.isRunning.Load() {
		app.updateStatus(checkingFormatsStatus)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		info, err := app.newDownloadEngine().ProbeVideo(context.Background(), req)
		// Back to Idle only if nothing has replaced the checking status, such
		// as a session started during the probe.
		app.statusThrottle.Replace(checkingFormatsStatus, "Status: Idle")
		fyne.DoAndWait(func() {
			if err != nil {
				dialog.ShowError(fmt.Errorf("could not list the formats: %w", err), app.window)
				return
			}
			app.uiManager.showFormatWindow(info, req.FormatPick, formatExtension(req.Format), func(pick string) {
				app.formatPicks.set(url, pick)
				app.logFormatPick(url, info, pick, req)
			})
		})
	}()
	return done
}

// showFormatsForItem is the Queue panel's Formats… on a waiting row: it
// shows the item's formats, probing it first when its probe answer is
// missing or old, and sets the pick on the item.
func (app *DownloaderApp) showFormatsForItem(id int) {
	queue := app.queue.Load()
	if queue == nil {
		return
	}
	item, ok := queue.Item(id)
	if !ok {
		return
	}
	go func() {
		req := item.downloadRequest()
		info := item.info
		if info == nil || len(info.Formats) == 0 {
			probed, err := app.newDownloadEngine().ProbeVideo(context.Background(), req)
			if err != nil {
				fyne.Do(func() { dialog.ShowError(fmt.Errorf("could not list the formats: %w", err), app.window) })
				return
			}
			info = &probed
		}
		fyne.Do(func() {
			app.uiManager.showFormatWindow(*info, req.FormatPick, formatExtension(req.Format), func(pick string) {
				if queue.SetFormatPick(id, pick) {
					app.logFormatPick(item.displayName(), *info, pick, req)
				}
			})
		})
	}()
}

// logFormatPick logs the formats picked for name.
func (app *DownloaderApp) logFormatPick(name string, info MediaInfo, pick string, req DownloadRequest) {
	if pick == "" {
		app.appendOutput(fmt.Sprintf("[SYSTEM] %s: formats chosen automatically.", name), colSystem)
		return
	}
	app.appendOutput(fmt.Sprintf("[SYSTEM] %s: will download formats %s (%s).", name, pick, describeDownload(info, pick, formatExtension(req.Format))), colSystem)
}
