// queue_panel.go — The Queue panel above the log.
//
// Responsibilities:
//   - UIManager.showQueue: attaches a session's QueueModel to the panel. The
//     panel is shown while the queue holds more than one item, as a
//     collapsible card titled with the queue's progress ("Queue — 7 of 20
//     done, 1 failed").
//   - queueRow: one row of the panel: the item's status and title, plus the
//     actions its status allows: Move up / Move down / Remove while waiting,
//     Skip while it runs, and Retry once it failed or was skipped (while the
//     session still runs).
//
// The model changes from the session goroutine and yt-dlp's output readers;
// queueChanged coalesces those changes through a latestValueThrottle so the
// panel is redrawn at most a few times a second.
package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// queueListHeight is the height of the Queue panel's list when it is open.
const queueListHeight = 170

// queuePanel holds the widgets of the Queue panel for one queue.
type queuePanel struct {
	accordion *widget.Accordion
	item      *widget.AccordionItem
	list      *widget.List
}

// queueActions are what a queue row's buttons do.
type queueActions struct {
	move    func(id, delta int)
	remove  func(id int)
	skip    func(id int)
	retry   func(id int)
	pause   func(id int)
	resume  func(id int)
	discard func(id int) // removes a paused item and its partial files
}

// queueRow is one item of the Queue panel.
type queueRow struct {
	widget.BaseWidget
	status *widget.Label
	title  *widget.Label
	up     *widget.Button
	down   *widget.Button
	remove *widget.Button
	skip   *widget.Button
	retry  *widget.Button
	pause  *widget.Button
	resume *widget.Button
}

// newQueueRow returns an empty row for the list to fill with show.
func newQueueRow() *queueRow {
	row := &queueRow{
		status: widget.NewLabel(""),
		title:  widget.NewLabel(""),
		up:     widget.NewButtonWithIcon("", theme.MoveUpIcon(), nil),
		down:   widget.NewButtonWithIcon("", theme.MoveDownIcon(), nil),
		remove: widget.NewButtonWithIcon("", theme.DeleteIcon(), nil),
		skip:   widget.NewButtonWithIcon("Skip", theme.MediaSkipNextIcon(), nil),
		retry:  widget.NewButtonWithIcon("Retry", theme.ViewRefreshIcon(), nil),
		pause:  widget.NewButtonWithIcon("Pause", theme.MediaPauseIcon(), nil),
		resume: widget.NewButtonWithIcon("Resume", theme.MediaPlayIcon(), nil),
	}
	row.title.Truncation = fyne.TextTruncateEllipsis
	row.ExtendBaseWidget(row)
	return row
}

// CreateRenderer lays the row out: status, title, then the buttons.
func (row *queueRow) CreateRenderer() fyne.WidgetRenderer {
	status := fixedWidth(row.status, 160)
	buttons := container.NewHBox(row.up, row.down, row.remove, row.pause, row.resume, row.skip, row.retry)
	return widget.NewSimpleRenderer(container.NewBorder(nil, nil, status, buttons, row.title))
}

// show fills the row with entry, offering the actions its status allows.
// Retry is offered only while the session runs, since only a running
// session takes items from the queue.
func (row *queueRow) show(entry queueEntry, running bool, actions queueActions) {
	row.status.SetText(entry.label())
	row.title.SetText(entry.name())
	switch entry.status {
	case queueDone:
		row.status.Importance = widget.SuccessImportance
	case queueFailed:
		row.status.Importance = widget.DangerImportance
	case queueSkipped:
		row.status.Importance = widget.LowImportance
	case queueChecking, queueDownloading, queuePostProcessing:
		row.status.Importance = widget.HighImportance
	default:
		row.status.Importance = widget.MediumImportance
	}
	row.status.Refresh()

	id := entry.id
	row.up.OnTapped = func() { actions.move(id, -1) }
	row.down.OnTapped = func() { actions.move(id, 1) }
	row.remove.OnTapped = func() { actions.remove(id) }
	if entry.status == queuePaused {
		row.remove.OnTapped = func() { actions.discard(id) }
	}
	row.pause.OnTapped = func() { actions.pause(id) }
	row.resume.OnTapped = func() { actions.resume(id) }
	row.skip.OnTapped = func() { actions.skip(id) }
	if entry.isLive() {
		row.skip.SetText("Stop recording")
	} else {
		row.skip.SetText("Skip")
	}
	row.retry.OnTapped = func() { actions.retry(id) }

	waiting := entry.status == queueWaiting
	active := entry.status == queueChecking || entry.status == queueDownloading
	retryable := running && (entry.status == queueFailed || entry.status == queueSkipped)
	paused := running && entry.status == queuePaused
	for button, visible := range map[*widget.Button]bool{
		row.up: waiting, row.down: waiting, row.remove: waiting || paused,
		row.pause: entry.status == queueDownloading && !entry.isLive(), row.resume: paused,
		row.skip: active, row.retry: retryable,
	} {
		if visible {
			button.Show()
		} else {
			button.Hide()
		}
	}
}

// showQueue shows queue in the Queue panel and keeps the panel up to date
// as it changes. Call it before the queue is shared with other goroutines,
// since it sets queue.OnChanged.
func (manager *UIManager) showQueue(queue *QueueModel) {
	queue.OnChanged = manager.queueChanged
	fyne.Do(func() {
		manager.queue = queue
		manager.queueSnapshot = queue.Snapshot()
		manager.renderQueuePanel()
	})
}

// queueChanged is QueueModel.OnChanged: it schedules a redraw of the panel.
func (manager *UIManager) queueChanged() {
	manager.queueThrottle.Set(manager.queueVersion.Add(1))
}

// flushQueue redraws the panel now if a change is waiting to be shown.
func (manager *UIManager) flushQueue() {
	manager.queueThrottle.Flush()
}

// renderQueuePanel builds the panel for manager.queue into queueBox,
// keeping it open or closed as it was. Must be called on the UI thread.
func (manager *UIManager) renderQueuePanel() {
	if manager.queueBox == nil {
		return
	}
	open := manager.queuePanel == nil || manager.queuePanel.item.Open
	manager.queuePanel = nil
	manager.queueBox.Objects = nil
	if manager.queue != nil {
		manager.queuePanel = manager.buildQueuePanel(open)
		manager.queueBox.Add(manager.queuePanel.accordion)
	}
	manager.refreshQueue()
}

// buildQueuePanel builds the collapsible card and its list.
func (manager *UIManager) buildQueuePanel(open bool) *queuePanel {
	actions := queueActions{
		move:   func(id, delta int) { manager.queueAction(func(queue *QueueModel) { queue.Move(id, delta) }) },
		remove: func(id int) { manager.queueAction(func(queue *QueueModel) { queue.Remove(id) }) },
		retry:  func(id int) { manager.queueAction(func(queue *QueueModel) { queue.Retry(id) }) },
		resume: func(id int) { manager.queueAction(func(queue *QueueModel) { queue.Resume(id) }) },
		pause:  func(id int) { manager.onPauseItem(id) },
		discard: func(id int) {
			manager.onDiscardPaused(id)
			manager.refreshQueue()
		},
		skip: func(id int) {
			if manager.onSkipItem(id) {
				manager.onLog("Download skipped by user.", colWarning)
			}
		},
	}
	list := widget.NewList(
		func() int { return len(manager.queueSnapshot) },
		func() fyne.CanvasObject { return newQueueRow() },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < len(manager.queueSnapshot) {
				obj.(*queueRow).show(manager.queueSnapshot[id], manager.onSessionRunning(), actions)
			}
		},
	)
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(0, queueListHeight))

	item := widget.NewAccordionItem("Queue", container.NewStack(spacer, list))
	item.Open = open
	return &queuePanel{accordion: widget.NewAccordion(item), item: item, list: list}
}

// queueAction runs a row action on the queue and redraws the panel at once.
func (manager *UIManager) queueAction(action func(queue *QueueModel)) {
	if manager.queue == nil {
		return
	}
	action(manager.queue)
	manager.refreshQueue()
}

// refreshQueue redraws the panel from the queue's current state. The panel
// is hidden unless the queue holds more than one item. Must be called on
// the UI thread.
func (manager *UIManager) refreshQueue() {
	panel := manager.queuePanel
	if panel == nil {
		return
	}
	manager.queueSnapshot = manager.queue.Snapshot()
	panel.item.Title = "Queue — " + manager.queue.Summary()
	panel.accordion.Refresh()
	panel.list.Refresh()
	if len(manager.queueSnapshot) > 1 {
		panel.accordion.Show()
	} else {
		panel.accordion.Hide()
	}
}
