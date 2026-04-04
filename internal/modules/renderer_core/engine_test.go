package renderercore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

type fakeDriver struct {
	playURL   string
	seekMS    int64
	volume    float64
	mute      bool
	playCnt   int
	resumeCnt int
	stopCnt   int
}

func (d *fakeDriver) Play(url string, positionMS int64) error {
	d.playURL = url
	d.seekMS = positionMS
	d.playCnt++
	return nil
}
func (d *fakeDriver) Pause() error { return nil }
func (d *fakeDriver) Resume() error {
	d.resumeCnt++
	return nil
}
func (d *fakeDriver) Stop() error {
	d.stopCnt++
	return nil
}
func (d *fakeDriver) SeekTo(positionMS int64) error {
	d.seekMS = positionMS
	return nil
}
func (d *fakeDriver) SetVolume(volume float64) error {
	d.volume = volume
	return nil
}
func (d *fakeDriver) SetMute(mute bool) error {
	d.mute = mute
	return nil
}
func (d *fakeDriver) Position() (int64, int64, bool) {
	return 0, 0, false
}

func TestEngineLeaseRequired(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)

	cmd := mu.CommandEnvelope{ID: "1", Type: "playback.play", Body: mustJSON(mu.PlaybackPlayBody{})}
	reply := engine.HandleCommand(cmd)
	if reply.Type != "error" || reply.Err.Code != "LEASE_REQUIRED" {
		t.Fatalf("expected lease required")
	}
}

func TestEngineQueueAddAndPlay(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)

	lease := acquireLease(t, engine)

	add := mu.CommandEnvelope{
		ID:    "2",
		Type:  "queue.add",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueAddBody{Position: "end", Entries: []mu.QueueEntry{{Resolved: &mu.ResolvedSource{URL: "http://stream"}}}}),
	}
	addReply := engine.HandleCommand(add)
	if addReply.Type != "ack" {
		t.Fatalf("expected ack")
	}

	play := mu.CommandEnvelope{
		ID:    "3",
		Type:  "playback.play",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.PlaybackPlayBody{}),
	}
	playReply := engine.HandleCommand(play)
	if playReply.Type != "ack" {
		t.Fatalf("expected ack")
	}
	if driver.playURL != "http://stream" {
		t.Fatalf("expected play url")
	}
}

func TestEngineQueueJumpStartsPlayback(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)

	lease := acquireLease(t, engine)

	add := mu.CommandEnvelope{
		ID:    "11",
		Type:  "queue.add",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body: mustJSON(mu.QueueAddBody{Position: "end", Entries: []mu.QueueEntry{
			{Resolved: &mu.ResolvedSource{URL: "http://track-1"}},
			{Resolved: &mu.ResolvedSource{URL: "http://track-2"}},
		}}),
	}
	engine.HandleCommand(add)

	jump := mu.CommandEnvelope{
		ID:    "12",
		Type:  "queue.jump",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueJumpBody{Index: 1}),
	}
	reply := engine.HandleCommand(jump)
	if reply.Type != "ack" {
		t.Fatalf("expected ack")
	}
	if driver.playURL != "http://track-2" {
		t.Fatalf("expected play track-2, got %s", driver.playURL)
	}
}

func TestEngineSetVolume(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	cmd := mu.CommandEnvelope{
		ID:    "4",
		Type:  "playback.setVolume",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.PlaybackSetVolumeBody{Volume: 0.5}),
	}
	reply := engine.HandleCommand(cmd)
	if reply.Type != "ack" {
		t.Fatalf("expected ack")
	}
	if driver.volume != 0.5 {
		t.Fatalf("expected volume 0.5")
	}
}

func TestEngineSeek(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	cmd := mu.CommandEnvelope{
		ID:    "5",
		Type:  "playback.seek",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.PlaybackSeekBody{PositionMS: 1200}),
	}
	reply := engine.HandleCommand(cmd)
	if reply.Type != "ack" {
		t.Fatalf("expected ack")
	}
	if driver.seekMS != 1200 {
		t.Fatalf("expected seek 1200")
	}
}

func TestEnginePlayResume(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	add := mu.CommandEnvelope{
		ID:    "7",
		Type:  "queue.add",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueAddBody{Position: "end", Entries: []mu.QueueEntry{{Resolved: &mu.ResolvedSource{URL: "http://stream"}}}}),
	}
	addReply := engine.HandleCommand(add)
	if addReply.Type != "ack" {
		t.Fatalf("expected ack")
	}

	play := mu.CommandEnvelope{
		ID:    "8",
		Type:  "playback.play",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.PlaybackPlayBody{}),
	}
	engine.HandleCommand(play)

	pause := mu.CommandEnvelope{
		ID:    "9",
		Type:  "playback.pause",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(struct{}{}),
	}
	engine.HandleCommand(pause)

	resume := mu.CommandEnvelope{
		ID:    "10",
		Type:  "playback.play",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.PlaybackPlayBody{}),
	}
	reply := engine.HandleCommand(resume)
	if reply.Type != "ack" {
		t.Fatalf("expected ack")
	}
	if driver.resumeCnt != 1 {
		t.Fatalf("expected resume called once")
	}
	if driver.playCnt != 1 {
		t.Fatalf("expected play called once")
	}
}

func TestQueueSetConflict(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	rev := int64(42)
	cmd := mu.CommandEnvelope{
		ID:         "6",
		Type:       "queue.set",
		Lease:      &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		IfRevision: &rev,
		Body:       mustJSON(mu.QueueSetBody{StartIndex: 0, Entries: []mu.QueueEntry{}}),
	}
	reply := engine.HandleCommand(cmd)
	if reply.Type != "error" || reply.Err.Code != "CONFLICT" {
		t.Fatalf("expected conflict")
	}
}

func TestEngineStop(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	// Add a track and start playing.
	add := mu.CommandEnvelope{
		ID:    "s1",
		Type:  "queue.add",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueAddBody{Position: "end", Entries: []mu.QueueEntry{{Resolved: &mu.ResolvedSource{URL: "http://track"}}}}),
	}
	engine.HandleCommand(add)

	play := mu.CommandEnvelope{
		ID:    "s2",
		Type:  "playback.play",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.PlaybackPlayBody{}),
	}
	engine.HandleCommand(play)
	if engine.State.Playback.Status != "playing" {
		t.Fatalf("expected playing, got %s", engine.State.Playback.Status)
	}

	// Stop playback.
	stop := mu.CommandEnvelope{
		ID:    "s3",
		Type:  "playback.stop",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(struct{}{}),
	}
	reply := engine.HandleCommand(stop)
	if reply.Type != "ack" || !reply.OK {
		t.Fatalf("expected ack, got type=%s ok=%v", reply.Type, reply.OK)
	}
	if engine.State.Playback.Status != "stopped" {
		t.Fatalf("expected stopped, got %s", engine.State.Playback.Status)
	}
	if driver.stopCnt != 1 {
		t.Fatalf("expected driver.Stop called once, got %d", driver.stopCnt)
	}
}

func TestEngineQueueRemove(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	// Add three tracks.
	add := mu.CommandEnvelope{
		ID:    "r1",
		Type:  "queue.add",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body: mustJSON(mu.QueueAddBody{Position: "end", Entries: []mu.QueueEntry{
			{Resolved: &mu.ResolvedSource{URL: "http://a"}},
			{Resolved: &mu.ResolvedSource{URL: "http://b"}},
			{Resolved: &mu.ResolvedSource{URL: "http://c"}},
		}}),
	}
	engine.HandleCommand(add)
	if engine.State.Queue.Length != 3 {
		t.Fatalf("expected 3 items, got %d", engine.State.Queue.Length)
	}

	// Remove by index.
	idx := int64(1)
	rem := mu.CommandEnvelope{
		ID:    "r2",
		Type:  "queue.remove",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueRemoveBody{Index: &idx}),
	}
	reply := engine.HandleCommand(rem)
	if reply.Type != "ack" {
		t.Fatalf("expected ack, got %s", reply.Type)
	}
	if engine.State.Queue.Length != 2 {
		t.Fatalf("expected 2 items after remove, got %d", engine.State.Queue.Length)
	}

	// Verify the remaining entries are "a" and "c".
	snap := engine.Queue.Snapshot(0, 10)
	if len(snap.Entries) != 2 {
		t.Fatalf("expected 2 entries in snapshot, got %d", len(snap.Entries))
	}
}

func TestEngineQueueRemoveByID(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	// Add two tracks.
	add := mu.CommandEnvelope{
		ID:    "rid1",
		Type:  "queue.add",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body: mustJSON(mu.QueueAddBody{Position: "end", Entries: []mu.QueueEntry{
			{Resolved: &mu.ResolvedSource{URL: "http://x"}},
			{Resolved: &mu.ResolvedSource{URL: "http://y"}},
		}}),
	}
	engine.HandleCommand(add)

	// Get the queue entry ID for the first entry.
	snap := engine.Queue.Snapshot(0, 10)
	entryID := snap.Entries[0].QueueEntryID

	rem := mu.CommandEnvelope{
		ID:    "rid2",
		Type:  "queue.remove",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueRemoveBody{QueueEntryID: entryID}),
	}
	reply := engine.HandleCommand(rem)
	if reply.Type != "ack" {
		t.Fatalf("expected ack, got %s", reply.Type)
	}
	if engine.State.Queue.Length != 1 {
		t.Fatalf("expected 1 item after remove, got %d", engine.State.Queue.Length)
	}
}

func TestEngineQueueMove(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	// Add three tracks.
	add := mu.CommandEnvelope{
		ID:    "m1",
		Type:  "queue.add",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body: mustJSON(mu.QueueAddBody{Position: "end", Entries: []mu.QueueEntry{
			{Resolved: &mu.ResolvedSource{URL: "http://a"}},
			{Resolved: &mu.ResolvedSource{URL: "http://b"}},
			{Resolved: &mu.ResolvedSource{URL: "http://c"}},
		}}),
	}
	engine.HandleCommand(add)

	// Capture original entry IDs.
	snapBefore := engine.Queue.Snapshot(0, 10)
	origFirst := snapBefore.Entries[0].QueueEntryID
	origThird := snapBefore.Entries[2].QueueEntryID

	// Move index 2 to index 0.
	move := mu.CommandEnvelope{
		ID:    "m2",
		Type:  "queue.move",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueMoveBody{FromIndex: 2, ToIndex: 0}),
	}
	reply := engine.HandleCommand(move)
	if reply.Type != "ack" {
		t.Fatalf("expected ack, got %s", reply.Type)
	}

	snapAfter := engine.Queue.Snapshot(0, 10)
	if snapAfter.Entries[0].QueueEntryID != origThird {
		t.Fatalf("expected third item to be at index 0")
	}
	if snapAfter.Entries[1].QueueEntryID != origFirst {
		t.Fatalf("expected first item to be at index 1")
	}
	if engine.State.Queue.Length != 3 {
		t.Fatalf("expected 3 items, got %d", engine.State.Queue.Length)
	}
}

func TestEngineQueueClear(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	// Add entries.
	add := mu.CommandEnvelope{
		ID:    "c1",
		Type:  "queue.add",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body: mustJSON(mu.QueueAddBody{Position: "end", Entries: []mu.QueueEntry{
			{Resolved: &mu.ResolvedSource{URL: "http://a"}},
			{Resolved: &mu.ResolvedSource{URL: "http://b"}},
		}}),
	}
	engine.HandleCommand(add)
	if engine.State.Queue.Length != 2 {
		t.Fatalf("expected 2 items, got %d", engine.State.Queue.Length)
	}

	// Clear queue.
	clear := mu.CommandEnvelope{
		ID:    "c2",
		Type:  "queue.clear",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(struct{}{}),
	}
	reply := engine.HandleCommand(clear)
	if reply.Type != "ack" {
		t.Fatalf("expected ack, got %s", reply.Type)
	}
	if engine.State.Queue.Length != 0 {
		t.Fatalf("expected 0 items after clear, got %d", engine.State.Queue.Length)
	}
	if engine.State.Queue.Index != 0 {
		t.Fatalf("expected index 0 after clear, got %d", engine.State.Queue.Index)
	}
}

func TestEngineQueueShuffle(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	// Add several tracks and set current index.
	add := mu.CommandEnvelope{
		ID:    "sh1",
		Type:  "queue.add",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body: mustJSON(mu.QueueAddBody{Position: "end", Entries: []mu.QueueEntry{
			{Resolved: &mu.ResolvedSource{URL: "http://a"}},
			{Resolved: &mu.ResolvedSource{URL: "http://b"}},
			{Resolved: &mu.ResolvedSource{URL: "http://c"}},
			{Resolved: &mu.ResolvedSource{URL: "http://d"}},
			{Resolved: &mu.ResolvedSource{URL: "http://e"}},
		}}),
	}
	engine.HandleCommand(add)

	// Jump to index 2 so there is a "current" item.
	jump := mu.CommandEnvelope{
		ID:    "sh2",
		Type:  "queue.jump",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueJumpBody{Index: 2}),
	}
	engine.HandleCommand(jump)

	// Get the queue entry ID of the current item before shuffle.
	currentBefore, ok := engine.Queue.Current()
	if !ok {
		t.Fatalf("expected current entry")
	}

	// Shuffle with a deterministic seed.
	shuffle := mu.CommandEnvelope{
		ID:    "sh3",
		Type:  "queue.shuffle",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueShuffleBody{Seed: 42}),
	}
	reply := engine.HandleCommand(shuffle)
	if reply.Type != "ack" {
		t.Fatalf("expected ack, got %s", reply.Type)
	}

	// Verify queue still has 5 items.
	if engine.State.Queue.Length != 5 {
		t.Fatalf("expected 5 items, got %d", engine.State.Queue.Length)
	}

	// Verify the current entry ID is still the same (index may have changed).
	currentAfter, ok := engine.Queue.Current()
	if !ok {
		t.Fatalf("expected current entry after shuffle")
	}
	if currentAfter.QueueEntryID != currentBefore.QueueEntryID {
		t.Fatalf("expected current item preserved after shuffle: got %s, want %s",
			currentAfter.QueueEntryID, currentBefore.QueueEntryID)
	}
}

func TestEngineRepeatModes(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	// Add two tracks and start playing.
	add := mu.CommandEnvelope{
		ID:    "rp1",
		Type:  "queue.add",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body: mustJSON(mu.QueueAddBody{Position: "end", Entries: []mu.QueueEntry{
			{Resolved: &mu.ResolvedSource{URL: "http://track-1"}},
			{Resolved: &mu.ResolvedSource{URL: "http://track-2"}},
		}}),
	}
	engine.HandleCommand(add)

	play := mu.CommandEnvelope{
		ID:    "rp2",
		Type:  "playback.play",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.PlaybackPlayBody{}),
	}
	engine.HandleCommand(play)

	// --- repeat mode "none" (off): next at end should fail ---
	setRepeat := mu.CommandEnvelope{
		ID:    "rp3",
		Type:  "queue.setRepeat",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueRepeatBody{Mode: "off"}),
	}
	engine.HandleCommand(setRepeat)

	// Move to last track.
	jump := mu.CommandEnvelope{
		ID:    "rp4",
		Type:  "queue.jump",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueJumpBody{Index: 1}),
	}
	engine.HandleCommand(jump)

	// Next should fail (end of queue, no repeat).
	next := mu.CommandEnvelope{
		ID:    "rp5",
		Type:  "playback.next",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(struct{}{}),
	}
	reply := engine.HandleCommand(next)
	if reply.OK {
		t.Fatalf("expected next to fail at end with repeat off")
	}
	if reply.Err == nil || reply.Err.Code != "NOT_FOUND" {
		t.Fatalf("expected NOT_FOUND error")
	}

	// --- repeat mode "all": next at end should wrap to start ---
	setRepeatAll := mu.CommandEnvelope{
		ID:    "rp6",
		Type:  "queue.setRepeat",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueRepeatBody{Mode: "all"}),
	}
	engine.HandleCommand(setRepeatAll)

	// Re-jump to last track.
	jump2 := mu.CommandEnvelope{
		ID:    "rp7",
		Type:  "queue.jump",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueJumpBody{Index: 1}),
	}
	engine.HandleCommand(jump2)

	next2 := mu.CommandEnvelope{
		ID:    "rp8",
		Type:  "playback.next",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(struct{}{}),
	}
	reply2 := engine.HandleCommand(next2)
	if !reply2.OK {
		t.Fatalf("expected next to succeed with repeat all, got err %v", reply2.Err)
	}
	if engine.State.Queue.Index != 0 {
		t.Fatalf("expected index to wrap to 0, got %d", engine.State.Queue.Index)
	}
	if driver.playURL != "http://track-1" {
		t.Fatalf("expected play track-1, got %s", driver.playURL)
	}

	// --- repeat mode "one": next should stay on same track ---
	setRepeatOne := mu.CommandEnvelope{
		ID:    "rp9",
		Type:  "queue.setRepeat",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.QueueRepeatBody{Mode: "one"}),
	}
	engine.HandleCommand(setRepeatOne)

	// Current is index 0 (track-1).
	next3 := mu.CommandEnvelope{
		ID:    "rp10",
		Type:  "playback.next",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(struct{}{}),
	}
	reply3 := engine.HandleCommand(next3)
	if !reply3.OK {
		t.Fatalf("expected next to succeed with repeat one")
	}
	// Should remain on the same index.
	if engine.State.Queue.Index != 0 {
		t.Fatalf("expected index to stay at 0 with repeat one, got %d", engine.State.Queue.Index)
	}
	if driver.playURL != "http://track-1" {
		t.Fatalf("expected replay of track-1, got %s", driver.playURL)
	}
}

func TestEngineLeaseRelease(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)
	lease := acquireLease(t, engine)

	// Commands should work with a valid lease.
	vol := mu.CommandEnvelope{
		ID:    "lr1",
		Type:  "playback.setVolume",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.PlaybackSetVolumeBody{Volume: 0.8}),
	}
	reply := engine.HandleCommand(vol)
	if !reply.OK {
		t.Fatalf("expected ack before release")
	}

	// Release the lease.
	rel := mu.CommandEnvelope{
		ID:    "lr2",
		Type:  "session.release",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(struct{}{}),
	}
	relReply := engine.HandleCommand(rel)
	if !relReply.OK {
		t.Fatalf("expected release to succeed")
	}
	if engine.State.Session != nil {
		t.Fatalf("expected session to be nil after release")
	}

	// Commands should now fail with LEASE_REQUIRED.
	vol2 := mu.CommandEnvelope{
		ID:    "lr3",
		Type:  "playback.setVolume",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(mu.PlaybackSetVolumeBody{Volume: 0.5}),
	}
	reply2 := engine.HandleCommand(vol2)
	if reply2.OK {
		t.Fatalf("expected command to fail after lease release")
	}
	if reply2.Err == nil || reply2.Err.Code != "LEASE_REQUIRED" {
		t.Fatalf("expected LEASE_REQUIRED error after release, got %v", reply2.Err)
	}

	// Acquire a new lease and verify commands work again.
	lease2 := acquireLease(t, engine)
	vol3 := mu.CommandEnvelope{
		ID:    "lr4",
		Type:  "playback.setVolume",
		Lease: &mu.Lease{SessionID: lease2.ID, Token: lease2.Token},
		Body:  mustJSON(mu.PlaybackSetVolumeBody{Volume: 0.3}),
	}
	reply3 := engine.HandleCommand(vol3)
	if !reply3.OK {
		t.Fatalf("expected ack with new lease")
	}
	if driver.volume != 0.3 {
		t.Fatalf("expected volume 0.3, got %f", driver.volume)
	}
}

func TestHandleSessionCommand(t *testing.T) {
	driver := &fakeDriver{}
	engine := NewEngine("mu:renderer:test", "Test", driver)

	// Acquire via HandleSessionCommand (the lock-free path).
	acq := mu.CommandEnvelope{
		ID: "s1", Type: "session.acquire", From: "ha",
		Body: mustJSON(mu.SessionAcquireBody{TTLMS: 30000}),
	}
	reply := engine.HandleSessionCommand(acq)
	if !reply.OK {
		t.Fatalf("acquire failed: %v", reply.Err)
	}
	var body mu.SessionReplyBody
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Session.ID == "" || body.Session.Token == "" {
		t.Fatalf("expected session ID and token")
	}
	if engine.State.Session == nil {
		t.Fatalf("expected State.Session to be set")
	}

	// Renew via HandleSessionCommand.
	ren := mu.CommandEnvelope{
		ID: "s2", Type: "session.renew",
		Lease: &mu.Lease{SessionID: body.Session.ID, Token: body.Session.Token},
		Body:  mustJSON(mu.SessionRenewBody{TTLMS: 30000}),
	}
	renReply := engine.HandleSessionCommand(ren)
	if !renReply.OK {
		t.Fatalf("renew failed: %v", renReply.Err)
	}

	// Release via HandleSessionCommand.
	rel := mu.CommandEnvelope{
		ID: "s3", Type: "session.release",
		Lease: &mu.Lease{SessionID: body.Session.ID, Token: body.Session.Token},
		Body:  mustJSON(struct{}{}),
	}
	relReply := engine.HandleSessionCommand(rel)
	if !relReply.OK {
		t.Fatalf("release failed: %v", relReply.Err)
	}
	if engine.State.Session != nil {
		t.Fatalf("expected State.Session nil after release")
	}

	// Duplicate acquire should succeed after release.
	acq2 := mu.CommandEnvelope{
		ID: "s4", Type: "session.acquire", From: "ha",
		Body: mustJSON(mu.SessionAcquireBody{TTLMS: 30000}),
	}
	reply2 := engine.HandleSessionCommand(acq2)
	if !reply2.OK {
		t.Fatalf("second acquire failed: %v", reply2.Err)
	}
}

func TestIsSessionCommand(t *testing.T) {
	for _, tc := range []struct {
		cmd    string
		expect bool
	}{
		{"session.acquire", true},
		{"session.renew", true},
		{"session.release", true},
		{"playback.play", false},
		{"queue.add", false},
		{"queue.get", false},
	} {
		if got := IsSessionCommand(tc.cmd); got != tc.expect {
			t.Errorf("IsSessionCommand(%q) = %v, want %v", tc.cmd, got, tc.expect)
		}
	}
}

func acquireLease(t *testing.T, engine *Engine) mu.SessionLease {
	cmd := mu.CommandEnvelope{ID: "lease", Type: "session.acquire", From: "tester", Body: mustJSON(mu.SessionAcquireBody{TTLMS: 1000})}
	reply := engine.HandleCommand(cmd)
	if reply.Type != "ack" {
		t.Fatalf("expected ack")
	}
	var body mu.SessionReplyBody
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	time.Sleep(1 * time.Millisecond)
	return body.Session
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
