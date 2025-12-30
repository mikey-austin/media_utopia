package renderercore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

type fakeDriver struct {
	playURL string
	seekMS  int64
	volume  float64
	mute    bool
}

func (d *fakeDriver) Play(url string, positionMS int64) error {
	d.playURL = url
	d.seekMS = positionMS
	return nil
}
func (d *fakeDriver) Pause() error  { return nil }
func (d *fakeDriver) Resume() error { return nil }
func (d *fakeDriver) Stop() error   { return nil }
func (d *fakeDriver) Seek(positionMS int64) error {
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
