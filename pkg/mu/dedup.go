package mu

import (
	"strings"
	"sync"
)

// ShouldDedup returns true if a command type should be deduplicated.
// Session commands (acquire/renew/release) are excluded because their
// replies contain state (lease tokens) that cannot be cached. Queue
// read commands (queue.get) are excluded because they are idempotent.
func ShouldDedup(cmdType string) bool {
	// Never dedup session commands — their replies contain unique tokens
	if strings.HasPrefix(cmdType, "session.") {
		return false
	}
	// Never dedup read-only commands
	switch cmdType {
	case "queue.get", "library.browse", "library.search", "library.resolve",
		"library.resolveBatch", "library.rescan",
		"playlist.list", "playlist.get", "snapshot.list", "snapshot.get":
		return false
	}
	return true
}

// CommandDedup tracks recently processed command IDs to prevent duplicate
// execution from MQTT QoS 1 redelivery. It uses a fixed-size ring buffer
// that is safe for concurrent use.
//
// Usage:
//
//	dedup := mu.NewCommandDedup(128)
//
//	func processCommand(cmd mu.CommandEnvelope) mu.ReplyEnvelope {
//	    if dedup.Seen(cmd.ID) {
//	        return mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true}
//	    }
//	    // ... process command ...
//	}
type CommandDedup struct {
	mu   sync.Mutex
	ring []string
	next int
	set  map[string]struct{}
}

// NewCommandDedup creates a dedup tracker with the given ring buffer size.
// A size of 128 is sufficient for most modules.
func NewCommandDedup(size int) *CommandDedup {
	if size < 16 {
		size = 16
	}
	return &CommandDedup{
		ring: make([]string, size),
		set:  make(map[string]struct{}, size),
	}
}

// Seen returns true if the command ID was recently processed, and records
// it if not. This is a combined check-and-record operation that is safe
// for concurrent callers.
func (d *CommandDedup) Seen(id string) bool {
	if id == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.set[id]; ok {
		return true
	}

	// Evict the oldest entry from the ring
	idx := d.next % len(d.ring)
	if old := d.ring[idx]; old != "" {
		delete(d.set, old)
	}
	d.ring[idx] = id
	d.set[id] = struct{}{}
	d.next++
	return false
}
