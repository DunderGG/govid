package main

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// newTestQueue returns a queue of URLs u1…un.
func newTestQueue(n int) *QueueModel {
	var items []queueItem
	for i := 1; i <= n; i++ {
		items = append(items, queueItem{url: "u" + string(rune('0'+i))})
	}
	return NewQueueModel(items)
}

// queueURLs lists the queue's URLs with their statuses, e.g. "u1:Done".
func queueURLs(queue *QueueModel) []string {
	var out []string
	for _, entry := range queue.Snapshot() {
		out = append(out, entry.item.url+":"+entry.status.String())
	}
	return out
}

// entryID returns the id of the queue entry for url.
func entryID(t *testing.T, queue *QueueModel, url string) int {
	t.Helper()
	for _, entry := range queue.Snapshot() {
		if entry.item.url == url {
			return entry.id
		}
	}
	t.Fatalf("no queue entry for %s", url)
	return -1
}

func TestQueueModelNextTakesFirstWaiting(t *testing.T) {
	queue := newTestQueue(3)
	id, item, ok := queue.Next()
	if !ok || item.url != "u1" {
		t.Fatalf("Next() = %v, %v", item, ok)
	}
	queue.SetStatus(id, queueDone)

	// Reordering a waiting item changes what runs next.
	if !queue.Move(entryID(t, queue, "u3"), -1) {
		t.Fatal("Move(u3, up) = false")
	}
	if _, item, _ := queue.Next(); item.url != "u3" {
		t.Errorf("Next() after moving u3 up = %s, want u3", item.url)
	}
	if got, want := queueURLs(queue), []string{"u1:Done", "u3:Checking", "u2:Waiting"}; !slices.Equal(got, want) {
		t.Errorf("queue = %q, want %q", got, want)
	}
}

func TestQueueModelOnlyWaitingItemsMoveOrGo(t *testing.T) {
	queue := newTestQueue(3)
	active, _, _ := queue.Next()
	if queue.Remove(active) || queue.Move(active, 1) {
		t.Error("the active item was removed or moved")
	}
	if queue.Move(entryID(t, queue, "u3"), 1) {
		t.Error("the last item moved past the end")
	}
	if queue.Move(entryID(t, queue, "u2"), -1) {
		t.Error("a waiting item moved above the active one")
	}
	if !queue.Remove(entryID(t, queue, "u2")) || queue.Len() != 2 {
		t.Errorf("Remove(u2) did not remove it: %q", queueURLs(queue))
	}
}

func TestQueueModelRetryRequeuesAtTheEnd(t *testing.T) {
	queue := newTestQueue(3)
	id, _, _ := queue.Next()
	if queue.Retry(id) {
		t.Error("Retry of a running item succeeded")
	}
	queue.SetStatus(id, queueFailed)
	if !queue.Retry(id) {
		t.Fatal("Retry of a failed item = false")
	}
	if got, want := queueURLs(queue), []string{"u2:Waiting", "u3:Waiting", "u1:Waiting"}; !slices.Equal(got, want) {
		t.Errorf("queue = %q, want %q", got, want)
	}
}

func TestQueueModelProgressAndSummary(t *testing.T) {
	queue := newTestQueue(4)
	changes := 0
	queue.OnChanged = func() { changes++ }

	first, _, _ := queue.Next()
	queue.SetStatus(first, queueDone)
	second, _, _ := queue.Next()
	queue.SetStatus(second, queueDownloading)
	before := changes
	queue.SetActiveProgress(0.421)
	queue.SetActiveProgress(0.424) // under 1%: not a change
	if changes != before+1 {
		t.Errorf("progress reported %d changes, want 1", changes-before)
	}
	if label := queue.Snapshot()[1].label(); label != "Downloading 42%" {
		t.Errorf("label = %q", label)
	}
	queue.SetStatus(second, queueFailed)
	third, _, _ := queue.Next()
	queue.SetStatus(third, queueSkipped)

	if got := queue.Summary(); got != "1 of 4 done, 1 failed, 1 skipped" {
		t.Errorf("Summary() = %q", got)
	}
	queue.MarkAll(queueWaiting, queueSkipped)
	if queue.HasWaiting() {
		t.Error("items still waiting after MarkAll")
	}
}

// ── Session ──────────────────────────────────────────────────────────────────

// TestQueueEditsDuringSession runs a five-URL batch in which the user
// removes a waiting item, moves another to the front, skips the stalled
// download, and retries the failed one, and checks the final status of
// every row.
func TestQueueEditsDuringSession(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-mixed")
	h.app.ui.download.batchMode.SetChecked(true)
	h.app.ui.download.entry.SetText(strings.Join([]string{
		"https://example.com/v/one", "https://example.com/v/fail", "https://example.com/v/hang",
		"https://example.com/v/four", "https://example.com/v/five",
	}, "\n"))

	var once, retryOnce sync.Once
	h.setHook(func(line string) {
		queue := h.app.queue.Load()
		switch {
		case strings.Contains(line, "── URL 1 of 5 ──"):
			once.Do(func() {
				queue.Remove(entryID(t, queue, "https://example.com/v/four"))
				five := entryID(t, queue, "https://example.com/v/five")
				for queue.Move(five, -1) { // up to just behind the active item
				}
			})
		case strings.Contains(line, "1.0%"): // the stalled download is running
			retryOnce.Do(func() {
				queue.Retry(entryID(t, queue, "https://example.com/v/fail"))
				h.app.RequestCancel() // the panel's Skip
			})
		}
	})

	h.startAndWait(t)

	queue := h.app.queue.Load()
	want := []string{
		"https://example.com/v/one:Done", "https://example.com/v/five:Done",
		"https://example.com/v/hang:Skipped", "https://example.com/v/fail:Failed",
	}
	if got := queueURLs(queue); !slices.Equal(got, want) {
		t.Errorf("queue = %q\nwant    %q\nlog:\n%s", got, want, h.joinedLogs())
	}
	if h.runs() != 5 {
		t.Errorf("yt-dlp runs = %d, want 5 (one, five, fail, hang, fail again)", h.runs())
	}
	var urls []string
	for _, entry := range h.history(t) {
		urls = append(urls, entry.URL)
	}
	if !slices.Equal(urls, []string{"https://example.com/v/one", "https://example.com/v/five"}) {
		t.Errorf("history = %q, want one then five", urls)
	}
	if got := queue.Summary(); got != "2 of 4 done, 1 failed, 1 skipped" {
		t.Errorf("Summary() = %q", got)
	}
}

// ── Panel ────────────────────────────────────────────────────────────────────

func TestQueuePanelShowsStatusAndSummary(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	queue := newTestQueue(3)
	id, _, _ := queue.Next()
	queue.SetStatus(id, queueDone)

	h.app.uiManager.showQueue(queue)
	panelState := func() (title string, rows int) {
		fyne.DoAndWait(func() {
			panel := h.app.uiManager.queuePanel
			title, rows = panel.item.Title, panel.list.Length()
		})
		return title, rows
	}
	if title, rows := panelState(); title != "Queue — 1 of 3 done" || rows != 3 {
		t.Errorf("panel = %q with %d rows", title, rows)
	}

	queue.Remove(entryID(t, queue, "u3"))
	fyne.DoAndWait(h.app.uiManager.refreshQueue)
	if title, rows := panelState(); title != "Queue — 1 of 2 done" || rows != 2 {
		t.Errorf("after a removal, panel = %q with %d rows", title, rows)
	}
}

func TestQueuePanelHiddenForASingleItem(t *testing.T) {
	h := newDownloadHarness(t, "ytdlp-download")
	h.app.uiManager.showQueue(newTestQueue(1))
	var visible bool
	fyne.DoAndWait(func() { visible = h.app.uiManager.queuePanel.accordion.Visible() })
	if visible {
		t.Error("the Queue panel is shown for a single download")
	}
}

func TestQueueRowOffersActionsByStatus(t *testing.T) {
	_ = test.NewApp()
	var moved, removed, retried []int
	skipped := 0
	actions := queueActions{
		move:   func(id, delta int) { moved = append(moved, id*10+delta) },
		remove: func(id int) { removed = append(removed, id) },
		skip:   func(int) { skipped++ },
		retry:  func(id int) { retried = append(retried, id) },
	}
	visible := func(row *queueRow) []string {
		var names []string
		for name, button := range map[string]*widget.Button{"up": row.up, "down": row.down, "remove": row.remove, "skip": row.skip, "retry": row.retry} {
			if button.Visible() {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		return names
	}
	row := newQueueRow()

	row.show(queueEntry{id: 2, item: queueItem{url: "u"}, status: queueWaiting}, true, actions)
	test.Tap(row.up)
	test.Tap(row.remove)
	if got := visible(row); !slices.Equal(got, []string{"down", "remove", "up"}) || !slices.Equal(moved, []int{19}) || !slices.Equal(removed, []int{2}) {
		t.Errorf("waiting row: buttons %q, moved %v, removed %v", got, moved, removed)
	}

	row.show(queueEntry{id: 3, item: queueItem{title: "T"}, status: queueDownloading, progress: 0.5}, true, actions)
	test.Tap(row.skip)
	if got := visible(row); !slices.Equal(got, []string{"skip"}) || skipped != 1 || row.status.Text != "Downloading 50%" || row.title.Text != "T" {
		t.Errorf("downloading row: buttons %q, skipped %d, status %q, title %q", got, skipped, row.status.Text, row.title.Text)
	}

	row.show(queueEntry{id: 4, status: queueFailed}, true, actions)
	test.Tap(row.retry)
	if got := visible(row); !slices.Equal(got, []string{"retry"}) || !slices.Equal(retried, []int{4}) {
		t.Errorf("failed row: buttons %q, retried %v", got, retried)
	}

	row.show(queueEntry{id: 4, status: queueFailed}, false, actions)
	if got := visible(row); len(got) != 0 {
		t.Errorf("failed row after the session: buttons %q, want none", got)
	}
}
