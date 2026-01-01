package renderercore

import (
	"errors"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// Queue holds the canonical renderer queue.
type Queue struct {
	mu         sync.Mutex
	revision   int64
	index      int64
	entries    []QueueEntry
	repeat     bool
	repeatMode string
	shuffle    bool
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
func (q *Queue) Set(entries []QueueEntry, startIndex int64, ifRevision *int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if ifRevision != nil && *ifRevision != q.revision {
		return errors.New("revision mismatch")
	}
	q.entries = entries
	q.index = clampIndex(startIndex, int64(len(q.entries))-1)
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
		q.index = clampIndex(q.index, int64(len(q.entries))-1)
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
	q.index = clampIndex(q.index, int64(len(q.entries))-1)
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
	q.index = clampIndex(q.index, int64(len(q.entries))-1)
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
	if repeat {
		q.repeatMode = "all"
	} else {
		q.repeatMode = "off"
	}
	q.revision++
}

// SetRepeatMode sets repeat mode (off|all|one).
func (q *Queue) SetRepeatMode(mode string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "one", "single":
		q.repeatMode = "one"
		q.repeat = true
	case "all", "on", "true":
		q.repeatMode = "all"
		q.repeat = true
	default:
		q.repeatMode = "off"
		q.repeat = false
	}
	q.revision++
}

// SetShuffle toggles shuffle mode.
func (q *Queue) SetShuffle(shuffle bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.shuffle = shuffle
	q.revision++
}

// Shuffle randomizes entry order while keeping current entry active.
func (q *Queue) Shuffle(seed int64) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.entries) < 2 {
		return
	}
	currentID := ""
	if q.index >= 0 && q.index < int64(len(q.entries)) {
		currentID = q.entries[q.index].QueueEntryID
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	if seed != 0 {
		rng = rand.New(rand.NewSource(seed))
	}
	rng.Shuffle(len(q.entries), func(i, j int) {
		q.entries[i], q.entries[j] = q.entries[j], q.entries[i]
	})
	if currentID != "" {
		for idx, entry := range q.entries {
			if entry.QueueEntryID == currentID {
				q.index = int64(idx)
				break
			}
		}
	}
	q.shuffle = true
	q.revision++
}

// Next advances to the next entry, respecting repeat.
func (q *Queue) Next() (int64, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.entries) == 0 {
		return 0, false
	}
	if q.repeatMode == "one" {
		return q.index, true
	}
	next := q.index + 1
	if next >= int64(len(q.entries)) {
		if !q.repeat {
			return 0, false
		}
		next = 0
	}
	q.index = next
	q.revision++
	return q.index, true
}

// Prev moves to the previous entry, respecting repeat.
func (q *Queue) Prev() (int64, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.entries) == 0 {
		return 0, false
	}
	if q.repeatMode == "one" {
		return q.index, true
	}
	prev := q.index - 1
	if prev < 0 {
		if !q.repeat {
			return 0, false
		}
		prev = int64(len(q.entries) - 1)
	}
	q.index = prev
	q.revision++
	return q.index, true
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

	return mu.QueueState{
		Revision:   q.revision,
		Length:     int64(len(q.entries)),
		Index:      q.index,
		Repeat:     q.repeat,
		RepeatMode: q.repeatMode,
		Shuffle:    q.shuffle,
	}
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
	if max < 0 {
		return 0
	}
	if index > max {
		return max
	}
	return index
}
