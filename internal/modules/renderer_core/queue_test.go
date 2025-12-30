package renderercore

import "testing"

func TestQueueAddPositions(t *testing.T) {
	queue := &Queue{}

	entries := []QueueEntry{{QueueEntryID: "e1"}, {QueueEntryID: "e2"}}
	if err := queue.Add(entries, "end", nil); err != nil {
		t.Fatalf("add end: %v", err)
	}
	if len(queue.entries) != 2 {
		t.Fatalf("expected 2 entries")
	}

	addNext := []QueueEntry{{QueueEntryID: "e3"}}
	if err := queue.Add(addNext, "next", nil); err != nil {
		t.Fatalf("add next: %v", err)
	}
	if queue.entries[1].QueueEntryID != "e3" {
		t.Fatalf("expected next insert")
	}

	at := int64(1)
	addAt := []QueueEntry{{QueueEntryID: "e4"}}
	if err := queue.Add(addAt, "at", &at); err != nil {
		t.Fatalf("add at: %v", err)
	}
	if queue.entries[1].QueueEntryID != "e4" {
		t.Fatalf("expected at insert")
	}
}

func TestQueueRemoveByIDAndIndex(t *testing.T) {
	queue := &Queue{}
	_ = queue.Add([]QueueEntry{{QueueEntryID: "e1"}, {QueueEntryID: "e2"}}, "end", nil)

	if err := queue.Remove("e1", nil); err != nil {
		t.Fatalf("remove by id: %v", err)
	}
	if len(queue.entries) != 1 || queue.entries[0].QueueEntryID != "e2" {
		t.Fatalf("expected e2 remaining")
	}

	idx := int64(0)
	if err := queue.Remove("", &idx); err != nil {
		t.Fatalf("remove by index: %v", err)
	}
	if len(queue.entries) != 0 {
		t.Fatalf("expected empty")
	}
}

func TestQueueMoveJumpClear(t *testing.T) {
	queue := &Queue{}
	_ = queue.Add([]QueueEntry{{QueueEntryID: "e1"}, {QueueEntryID: "e2"}, {QueueEntryID: "e3"}}, "end", nil)

	if err := queue.Move(2, 0); err != nil {
		t.Fatalf("move: %v", err)
	}
	if queue.entries[0].QueueEntryID != "e3" {
		t.Fatalf("expected moved entry")
	}

	if err := queue.Jump(1); err != nil {
		t.Fatalf("jump: %v", err)
	}
	if queue.index != 1 {
		t.Fatalf("expected index 1")
	}

	queue.Clear()
	if len(queue.entries) != 0 {
		t.Fatalf("expected cleared queue")
	}
}

func TestQueueRepeatShuffle(t *testing.T) {
	queue := &Queue{}
	queue.SetRepeat(true)
	queue.SetShuffle(true)
	if !queue.repeat || !queue.shuffle {
		t.Fatalf("expected repeat and shuffle true")
	}
}
