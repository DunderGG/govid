// queue_model.go — The download queue a session works through.
//
// Responsibilities:
//   - QueueModel: the session's queue items and the state of each (waiting,
//     checking, downloading with its progress, post-processing, done,
//     failed, or skipped), guarded by a mutex so the session goroutine,
//     yt-dlp's output readers, and the Queue panel can share it. The
//     session takes the next waiting item with Next, so waiting items can be
//     removed, moved, or (once failed) retried while the queue runs.
//   - queueEntry: a snapshot of one item, for the Queue panel.
package main

import (
	"fmt"
	"slices"
	"sync"
)

// queueStatus is how far one queued item has got.
type queueStatus int

const (
	queueWaiting queueStatus = iota
	queueChecking
	queueDownloading
	queuePostProcessing
	queueDone
	queueFailed
	queueSkipped
)

// String returns the status as the Queue panel shows it.
func (status queueStatus) String() string {
	switch status {
	case queueWaiting:
		return "Waiting"
	case queueChecking:
		return "Checking"
	case queueDownloading:
		return "Downloading"
	case queuePostProcessing:
		return "Post-processing"
	case queueDone:
		return "Done"
	case queueFailed:
		return "Failed"
	case queueSkipped:
		return "Skipped"
	default:
		return "Unknown"
	}
}

// queueEntry is one item of the queue and its state.
type queueEntry struct {
	id       int // stable identifier; positions change as items move
	item     queueItem
	status   queueStatus
	progress float64 // 0..1 while downloading
}

// label is the entry's status as the Queue panel shows it, e.g.
// "Downloading 42%".
func (entry queueEntry) label() string {
	if entry.status == queueDownloading && entry.isLive() {
		return "Recording"
	}
	if entry.status == queueDownloading {
		return fmt.Sprintf("Downloading %.0f%%", entry.progress*100)
	}
	return entry.status.String()
}

// isLive reports whether the entry is a live or scheduled stream, which is
// recorded rather than downloaded.
func (entry queueEntry) isLive() bool {
	info := entry.item.info
	return info != nil && (info.IsLive() || info.IsUpcoming())
}

// name is how the Queue panel names the entry: its title, or its URL.
func (entry queueEntry) name() string {
	return entry.item.displayName()
}

// QueueModel is a session's download queue. All methods are safe to call
// from any goroutine. OnChanged, when set, is called after every change,
// outside the lock.
type QueueModel struct {
	mu      sync.Mutex
	entries []*queueEntry
	nextID  int

	// OnChanged is called after every change; set it before the queue is
	// shared. It must not block.
	OnChanged func()
}

// NewQueueModel returns a queue of items, all waiting.
func NewQueueModel(items []queueItem) *QueueModel {
	queue := &QueueModel{}
	for _, item := range items {
		queue.entries = append(queue.entries, &queueEntry{id: queue.nextID, item: item})
		queue.nextID++
	}
	return queue
}

// change runs fn under the lock and then reports the change, when fn says
// it made one.
func (queue *QueueModel) change(fn func() bool) bool {
	queue.mu.Lock()
	changed := fn()
	queue.mu.Unlock()
	if changed && queue.OnChanged != nil {
		queue.OnChanged()
	}
	return changed
}

// find returns the entry with id, or nil. Call it with the lock held.
func (queue *QueueModel) find(id int) *queueEntry {
	for _, entry := range queue.entries {
		if entry.id == id {
			return entry
		}
	}
	return nil
}

// Len returns how many items the queue holds.
func (queue *QueueModel) Len() int {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return len(queue.entries)
}

// Next marks the first waiting item as checking and returns it, or returns
// ok == false when no item is waiting.
func (queue *QueueModel) Next() (id int, item queueItem, ok bool) {
	queue.change(func() bool {
		for _, entry := range queue.entries {
			if entry.status == queueWaiting {
				entry.status, entry.progress = queueChecking, 0
				id, item, ok = entry.id, entry.item, true
				return true
			}
		}
		return false
	})
	return id, item, ok
}

// HasWaiting reports whether any item is still waiting.
func (queue *QueueModel) HasWaiting() bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return slices.ContainsFunc(queue.entries, func(entry *queueEntry) bool { return entry.status == queueWaiting })
}

// Position returns the 1-based position of the item with id among the
// queue's items, and how many there are, for "URL 3 of 20".
func (queue *QueueModel) Position(id int) (position, total int) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	index := slices.IndexFunc(queue.entries, func(entry *queueEntry) bool { return entry.id == id })
	return index + 1, len(queue.entries)
}

// SetItem replaces the item with id, e.g. after a probe told more about it.
func (queue *QueueModel) SetItem(id int, item queueItem) {
	queue.change(func() bool {
		entry := queue.find(id)
		if entry == nil {
			return false
		}
		entry.item = item
		return true
	})
}

// SetStatus sets the status of the item with id.
func (queue *QueueModel) SetStatus(id int, status queueStatus) {
	queue.change(func() bool {
		entry := queue.find(id)
		if entry == nil || entry.status == status {
			return false
		}
		entry.status, entry.progress = status, 0
		return true
	})
}

// SetActiveProgress records the download progress (0..1) of the item that
// is downloading, if any. A change of less than 1% is not reported.
func (queue *QueueModel) SetActiveProgress(progress float64) {
	queue.change(func() bool {
		for _, entry := range queue.entries {
			if entry.status != queueDownloading {
				continue
			}
			if int(entry.progress*100) == int(progress*100) {
				return false
			}
			entry.progress = progress
			return true
		}
		return false
	})
}

// MarkAll sets every item with status from to status to.
func (queue *QueueModel) MarkAll(from, to queueStatus) {
	queue.change(func() bool {
		changed := false
		for _, entry := range queue.entries {
			if entry.status == from {
				entry.status, entry.progress = to, 0
				changed = true
			}
		}
		return changed
	})
}

// Remove takes a waiting item out of the queue and reports whether it did.
func (queue *QueueModel) Remove(id int) bool {
	return queue.change(func() bool {
		index := slices.IndexFunc(queue.entries, func(entry *queueEntry) bool { return entry.id == id })
		if index < 0 || queue.entries[index].status != queueWaiting {
			return false
		}
		queue.entries = slices.Delete(queue.entries, index, index+1)
		return true
	})
}

// Move swaps a waiting item with the waiting item delta positions away (-1
// is up, 1 is down) and reports whether it moved. Waiting items run in
// queue order, so this changes which runs next; items that have already
// run, or are running, stay where they are.
func (queue *QueueModel) Move(id, delta int) bool {
	return queue.change(func() bool {
		index := slices.IndexFunc(queue.entries, func(entry *queueEntry) bool { return entry.id == id })
		target := index + delta
		if index < 0 || target < 0 || target >= len(queue.entries) ||
			queue.entries[index].status != queueWaiting || queue.entries[target].status != queueWaiting {
			return false
		}
		queue.entries[index], queue.entries[target] = queue.entries[target], queue.entries[index]
		return true
	})
}

// Retry puts a failed or skipped item back at the end of the queue, waiting,
// and reports whether it did.
func (queue *QueueModel) Retry(id int) bool {
	return queue.change(func() bool {
		index := slices.IndexFunc(queue.entries, func(entry *queueEntry) bool { return entry.id == id })
		if index < 0 {
			return false
		}
		entry := queue.entries[index]
		if entry.status != queueFailed && entry.status != queueSkipped {
			return false
		}
		entry.status, entry.progress = queueWaiting, 0
		queue.entries = append(slices.Delete(queue.entries, index, index+1), entry)
		return true
	})
}

// Snapshot returns a copy of every entry, in queue order.
func (queue *QueueModel) Snapshot() []queueEntry {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	snapshot := make([]queueEntry, len(queue.entries))
	for i, entry := range queue.entries {
		snapshot[i] = *entry
	}
	return snapshot
}

// Summary describes the queue's progress, e.g. "7 of 20 done, 1 failed".
func (queue *QueueModel) Summary() string {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	counts := map[queueStatus]int{}
	for _, entry := range queue.entries {
		counts[entry.status]++
	}
	summary := fmt.Sprintf("%d of %d done", counts[queueDone], len(queue.entries))
	if n := counts[queueFailed]; n > 0 {
		summary += fmt.Sprintf(", %d failed", n)
	}
	if n := counts[queueSkipped]; n > 0 {
		summary += fmt.Sprintf(", %d skipped", n)
	}
	return summary
}
