package mu

import (
	"fmt"
	"sync"
	"testing"
)

func TestCommandDedupBasic(t *testing.T) {
	d := NewCommandDedup(4)

	if d.Seen("a") {
		t.Error("first time should not be seen")
	}
	if !d.Seen("a") {
		t.Error("second time should be seen")
	}
	if d.Seen("b") {
		t.Error("different ID should not be seen")
	}
}

func TestCommandDedupEviction(t *testing.T) {
	// Min ring size is 16, so use that
	d := NewCommandDedup(16)

	// Fill all 16 slots
	for i := range 16 {
		d.Seen(fmt.Sprintf("cmd-%d", i))
	}

	// cmd-0 should still be tracked
	if !d.Seen("cmd-0") {
		t.Error("cmd-0 should still be seen")
	}

	// Add 16 more to fully evict the originals
	for i := range 16 {
		d.Seen(fmt.Sprintf("new-%d", i))
	}

	// cmd-1 through cmd-15 should be evicted (cmd-0 was re-verified so it stayed longer)
	if d.Seen("cmd-5") {
		t.Error("cmd-5 should have been evicted")
	}
}

func TestCommandDedupEmptyID(t *testing.T) {
	d := NewCommandDedup(4)
	if d.Seen("") {
		t.Error("empty ID should never be seen")
	}
	if d.Seen("") {
		t.Error("empty ID should still never be seen")
	}
}

func TestCommandDedupConcurrent(t *testing.T) {
	d := NewCommandDedup(256)
	var wg sync.WaitGroup

	// Run 10 goroutines each checking 100 IDs
	for g := range 10 {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := range 100 {
				id := fmt.Sprintf("g%d-cmd%d", gid, i)
				d.Seen(id)
			}
		}(g)
	}
	wg.Wait()
	// No panic = success (testing thread safety)
}

func TestCommandDedupMinSize(t *testing.T) {
	d := NewCommandDedup(1) // Should be clamped to 16
	if len(d.ring) < 16 {
		t.Errorf("min size should be 16, got %d", len(d.ring))
	}
}
