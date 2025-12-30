package renderercore

import (
	"errors"
	"sync"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// Queue holds the canonical renderer queue.
type Queue struct {
	mu       sync.Mutex
	revision int64
	index    int64
	entries  []QueueEntry
	repeat   bool
	shuffle  bool
}

// QueueEntry describes a queued item.
type QueueEntry struct {
	QueueEntryID string             `json:"queueEntryId"`
	ItemID       string             `json:"itemId"`
	Metadata     map[string]any     `json:"metadata,omitempty"`
	Ref          *mu.ItemRef        `json:"ref,omitempty"`
	Resolved     *mu.ResolvedSource `json:"resolved,omitempty"`
}

// Snapshot returns a copy of the queue.
func (q *Queue) Snapshot(from int64, count int64) mu.QueueGetReply {
	q.mu.Lock()
	defer q.mu.Unlock()

	start := clampIndex(from, int64(len(q.entries)))
	end := clampIndex(from+count, int64(len(q.entries)))
	items := make([]mu.QueueItem, 0, end-start)
	for _, entry := range q.entries[start:end] {
		items = append(items, mu.QueueItem{
			QueueEntryID: entry.QueueEntryID,
			ItemID:       entry.ItemID,
			Metadata:     entry.Metadata,
		})
	}

	return mu.QueueGetReply{Revision: q.revision, Index: q.index, Entries: items}
}

// Set replaces the queue atomically.
func (q *Queue) Set(entries []QueueEntry, ifRevision *int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if ifRevision != nil && *ifRevision != q.revision {
		return errors.New("revision mismatch")
	}
	q.entries = entries
	q.index = 0
	q.revision++
	return nil
}

// Add appends or inserts entries.
func (q *Queue) Add(entries []QueueEntry, position string, atIndex *int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	switch position {
	case "end":
		q.entries = append(q.entries, entries...)
	case "next":
		insertAt := q.index + 1
		q.entries = insertEntries(q.entries, entries, insertAt)
	case "at":
		if atIndex == nil {
			return errors.New("atIndex required")
		}
		q.entries = insertEntries(q.entries, entries, *atIndex)
	default:
		return errors.New("invalid position")
	}

	q.revision++
	return nil
}

// Remove removes an entry by queueEntryId or index.
func (q *Queue) Remove(queueEntryID string, index *int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if queueEntryID != "" {
		filtered := q.entries[:0]
		for _, entry := range q.entries {
			if entry.QueueEntryID != queueEntryID {
				filtered = append(filtered, entry)
			}
		}
		q.entries = filtered
		q.revision++
		return nil
	}
	if index == nil {
		return errors.New("queueEntryId or index required")
	}
	if *index < 0 || *index >= int64(len(q.entries)) {
		return errors.New("index out of range")
	}
	q.entries = append(q.entries[:*index], q.entries[*index+1:]...)
	q.revision++
	return nil
}

// Move moves a queue entry.
func (q *Queue) Move(fromIndex int64, toIndex int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if fromIndex < 0 || fromIndex >= int64(len(q.entries)) {
		return errors.New("fromIndex out of range")
	}
	if toIndex < 0 || toIndex >= int64(len(q.entries)) {
		return errors.New("toIndex out of range")
	}
	entry := q.entries[fromIndex]
	q.entries = append(q.entries[:fromIndex], q.entries[fromIndex+1:]...)
	q.entries = insertEntries(q.entries, []QueueEntry{entry}, toIndex)
	q.revision++
	return nil
}

// Clear clears the queue.
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.entries = nil
	q.index = 0
	q.revision++
}

// Jump sets current index.
func (q *Queue) Jump(index int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if index < 0 || index >= int64(len(q.entries)) {
		return errors.New("index out of range")
	}
	q.index = index
	q.revision++
	return nil
}

// SetRepeat sets repeat mode.
func (q *Queue) SetRepeat(repeat bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.repeat = repeat
	q.revision++
}

// SetShuffle sets shuffle mode.
func (q *Queue) SetShuffle(shuffle bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.shuffle = shuffle
	q.revision++
}

// Current returns the current entry.
func (q *Queue) Current() (QueueEntry, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.entries) == 0 || q.index < 0 || q.index >= int64(len(q.entries)) {
		return QueueEntry{}, false
	}
	return q.entries[q.index], true
}

// Summary returns the queue summary.
func (q *Queue) Summary() mu.QueueState {
	q.mu.Lock()
	defer q.mu.Unlock()

	return mu.QueueState{Revision: q.revision, Length: int64(len(q.entries)), Index: q.index}
}

func insertEntries(entries []QueueEntry, insert []QueueEntry, index int64) []QueueEntry {
	if index < 0 {
		index = 0
	}
	if index > int64(len(entries)) {
		index = int64(len(entries))
	}
	result := make([]QueueEntry, 0, len(entries)+len(insert))
	result = append(result, entries[:index]...)
	result = append(result, insert...)
	result = append(result, entries[index:]...)
	return result
}

func clampIndex(index int64, max int64) int64 {
	if index < 0 {
		return 0
	}
	if index > max {
		return max
	}
	return index
}
