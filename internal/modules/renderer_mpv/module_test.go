package renderermpv

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/mikey-austin/media_utopia/internal/modules/renderer_core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

type fakeMQTTClient struct {
	mu        sync.Mutex
	subs      map[string]paho.MessageHandler
	onPublish func(topic string, payload []byte)
}

func (f *fakeMQTTClient) Publish(topic string, qos byte, retained bool, payload []byte) error {
	if f.onPublish != nil {
		f.onPublish(topic, payload)
	}
	return nil
}

func (f *fakeMQTTClient) Subscribe(topic string, qos byte, handler paho.MessageHandler) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subs == nil {
		f.subs = make(map[string]paho.MessageHandler)
	}
	f.subs[topic] = handler
	return nil
}

func (f *fakeMQTTClient) Unsubscribe(topic string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.subs, topic)
	return nil
}

func (f *fakeMQTTClient) emit(topic string, payload []byte) {
	f.mu.Lock()
	handler := f.subs[topic]
	f.mu.Unlock()
	if handler != nil {
		handler(nil, fakeMessage{topic: topic, payload: payload})
	}
}

type fakeMessage struct {
	topic   string
	payload []byte
}

func (m fakeMessage) Duplicate() bool   { return false }
func (m fakeMessage) Qos() byte         { return 0 }
func (m fakeMessage) Retained() bool    { return false }
func (m fakeMessage) Topic() string     { return m.topic }
func (m fakeMessage) MessageID() uint16 { return 0 }
func (m fakeMessage) Payload() []byte   { return m.payload }
func (m fakeMessage) Ack()              {}

type stubDriver struct{}

func (d stubDriver) Play(url string, positionMS int64) error { return nil }
func (d stubDriver) Pause() error                            { return nil }
func (d stubDriver) Resume() error                           { return nil }
func (d stubDriver) Stop() error                             { return nil }
func (d stubDriver) SeekTo(positionMS int64) error           { return nil }
func (d stubDriver) SetVolume(volume float64) error          { return nil }
func (d stubDriver) SetMute(mute bool) error                 { return nil }
func (d stubDriver) Position() (int64, int64, bool)          { return 0, 0, false }

func TestQueueLoadPlaylist(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &fakeMQTTClient{}
	engine := renderercore.NewEngine("mu:renderer:test", "Test Renderer", stubDriver{})
	replyTopic := mu.TopicReply(mu.BaseTopic, "mu:renderer:test")
	module := &Module{
		log:    zap.NewNop(),
		client: client,
		dedup:  mu.NewCommandDedup(128),
		engine: engine,
		config: Config{
			NodeID:            "mu:renderer:test",
			TopicBase:         mu.BaseTopic,
			Name:              "Test Renderer",
			StatePublisher:    renderercore.StatePublisherFunc(func(s *mu.RendererState) error { return nil }),
			PresencePublisher: renderercore.PresencePublisherFunc(func(p *mu.Presence) error { return nil }),
		},
		ctx:           ctx,
		cmdQueue:      make(chan cmdWork, 64),
		replyHandlers: make(map[string]chan mu.ReplyEnvelope),
		replyTopic:    replyTopic,
		debounceChan:  make(chan struct{}, 1),
	}

	// Subscribe to reply topic to receive replies
	if err := client.Subscribe(replyTopic, 1, func(_ paho.Client, msg paho.Message) {
		module.handleReply(msg)
	}); err != nil {
		t.Fatalf("subscribe reply topic: %v", err)
	}

	// Start a command worker
	go module.commandWorker(ctx)

	lease, err := engine.Leases.Acquire("tester", time.Minute)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	client.onPublish = func(topic string, payload []byte) {
		if topic != mu.TopicCommands(mu.BaseTopic, "mu:playlist:plsrv:test") {
			return
		}
		var cmd mu.CommandEnvelope
		if err := json.Unmarshal(payload, &cmd); err != nil {
			t.Fatalf("decode playlist request: %v", err)
		}
		pl := playlistReply{
			Entries: []mu.QueueEntry{{
				QueueEntryID: "entry-1",
				Resolved:     &mu.ResolvedSource{URL: "https://example.com/track.mp3"},
			}},
		}
		body, _ := json.Marshal(pl)
		reply := mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix(), Body: body}
		replyPayload, _ := json.Marshal(reply)
		client.emit(cmd.ReplyTo, replyPayload)
	}

	cmd := mu.CommandEnvelope{
		ID:      "cmd-1",
		Type:    "queue.loadPlaylist",
		TS:      time.Now().Unix(),
		From:    "tester",
		ReplyTo: "mu/v1/reply/tester",
		Lease:   &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:    mustJSON(mu.QueueLoadPlaylistBody{PlaylistServerID: "mu:playlist:plsrv:test", PlaylistID: "pl-1", Mode: "replace", Resolve: "auto"}),
	}

	module.handleMessage(fakeMessage{topic: module.cmdTopic, payload: mustJSON(cmd)})

	deadline := time.After(500 * time.Millisecond)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		state := engine.Queue.Snapshot(0, 10)
		if len(state.Entries) == 1 && state.Entries[0].Resolved != nil && state.Entries[0].Resolved.URL != "" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("expected queue to load")
		case <-tick.C:
		}
	}
}

func TestQueueLoadSnapshotCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &fakeMQTTClient{}
	engine := renderercore.NewEngine("mu:renderer:test", "Test Renderer", stubDriver{})
	replyTopic := mu.TopicReply(mu.BaseTopic, "mu:renderer:test")
	module := &Module{
		log:    zap.NewNop(),
		client: client,
		dedup:  mu.NewCommandDedup(128),
		engine: engine,
		config: Config{
			NodeID:            "mu:renderer:test",
			TopicBase:         mu.BaseTopic,
			Name:              "Test Renderer",
			StatePublisher:    renderercore.StatePublisherFunc(func(s *mu.RendererState) error { return nil }),
			PresencePublisher: renderercore.PresencePublisherFunc(func(p *mu.Presence) error { return nil }),
		},
		ctx:           ctx,
		cmdQueue:      make(chan cmdWork, 64),
		replyHandlers: make(map[string]chan mu.ReplyEnvelope),
		replyTopic:    replyTopic,
		debounceChan:  make(chan struct{}, 1),
	}

	// Subscribe to reply topic to receive replies
	if err := client.Subscribe(replyTopic, 1, func(_ paho.Client, msg paho.Message) {
		module.handleReply(msg)
	}); err != nil {
		t.Fatalf("subscribe reply topic: %v", err)
	}

	// Start a command worker
	go module.commandWorker(ctx)

	lease, err := engine.Leases.Acquire("tester", time.Minute)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	// Mock the playlist server to return a snapshot with items, capture state
	client.onPublish = func(topic string, payload []byte) {
		if topic != mu.TopicCommands(mu.BaseTopic, "mu:playlist:plsrv:test") {
			return
		}
		var cmd mu.CommandEnvelope
		if err := json.Unmarshal(payload, &cmd); err != nil {
			t.Fatalf("decode snapshot request: %v", err)
		}
		if cmd.Type == "snapshot.get" {
			snap := mu.SnapshotGetReply{
				SnapshotID: "snap-1",
				Name:       "Test Snapshot",
				Entries: []mu.QueueEntry{
					{Resolved: &mu.ResolvedSource{URL: "https://example.com/track1.mp3"}},
					{Resolved: &mu.ResolvedSource{URL: "https://example.com/track2.mp3"}},
					{Resolved: &mu.ResolvedSource{URL: "https://example.com/track3.mp3"}},
				},
				Capture: mu.SnapshotCapture{
					Index:      1,
					PositionMS: 42000,
					Repeat:     true,
					RepeatMode: "all",
				},
			}
			body, _ := json.Marshal(snap)
			reply := mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix(), Body: body}
			replyPayload, _ := json.Marshal(reply)
			client.emit(cmd.ReplyTo, replyPayload)
		}
	}

	cmd := mu.CommandEnvelope{
		ID:      "cmd-snap-1",
		Type:    "queue.loadSnapshot",
		TS:      time.Now().Unix(),
		From:    "tester",
		ReplyTo: "mu/v1/reply/tester",
		Lease:   &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:    mustJSON(mu.QueueLoadSnapshotBody{PlaylistServerID: "mu:playlist:plsrv:test", SnapshotID: "snap-1", Mode: "replace", Resolve: "no"}),
	}

	module.handleMessage(fakeMessage{topic: module.cmdTopic, payload: mustJSON(cmd)})

	deadline := time.After(500 * time.Millisecond)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		state := engine.Queue.Snapshot(0, 10)
		if len(state.Entries) == 3 {
			// Verify the queue index was set from the snapshot capture
			summary := engine.Queue.Summary()
			if summary.Index != 1 {
				t.Fatalf("expected queue index 1, got %d", summary.Index)
			}
			if !summary.Repeat {
				t.Fatalf("expected repeat=true")
			}
			if summary.RepeatMode != "all" {
				t.Fatalf("expected repeatMode=all, got %s", summary.RepeatMode)
			}
			// Verify the playback position was restored
			if engine.State.Playback.PositionMS != 42000 {
				t.Fatalf("expected positionMS=42000, got %d", engine.State.Playback.PositionMS)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("expected queue to load 3 entries, got %d", len(state.Entries))
		case <-tick.C:
		}
	}
}

func TestLoadCommandErrorDoesNotCorruptState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &fakeMQTTClient{}
	engine := renderercore.NewEngine("mu:renderer:test", "Test Renderer", stubDriver{})
	replyTopic := mu.TopicReply(mu.BaseTopic, "mu:renderer:test")
	module := &Module{
		log:    zap.NewNop(),
		client: client,
		dedup:  mu.NewCommandDedup(128),
		engine: engine,
		config: Config{
			NodeID:            "mu:renderer:test",
			TopicBase:         mu.BaseTopic,
			Name:              "Test Renderer",
			StatePublisher:    renderercore.StatePublisherFunc(func(s *mu.RendererState) error { return nil }),
			PresencePublisher: renderercore.PresencePublisherFunc(func(p *mu.Presence) error { return nil }),
		},
		ctx:           ctx,
		cmdQueue:      make(chan cmdWork, 64),
		replyHandlers: make(map[string]chan mu.ReplyEnvelope),
		replyTopic:    replyTopic,
		debounceChan:  make(chan struct{}, 1),
	}

	if err := client.Subscribe(replyTopic, 1, func(_ paho.Client, msg paho.Message) {
		module.handleReply(msg)
	}); err != nil {
		t.Fatalf("subscribe reply topic: %v", err)
	}

	go module.commandWorker(ctx)

	lease, err := engine.Leases.Acquire("tester", time.Minute)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}

	// Pre-load the queue with an existing entry so we can verify it's not corrupted
	setBody := mu.QueueSetBody{
		StartIndex: 0,
		Entries: []mu.QueueEntry{{
			Resolved: &mu.ResolvedSource{URL: "https://example.com/existing.mp3"},
		}},
	}
	setCmd := mu.CommandEnvelope{
		ID:    "cmd-pre",
		Type:  "queue.set",
		TS:    time.Now().Unix(),
		From:  "tester",
		Lease: &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:  mustJSON(setBody),
	}
	reply := engine.HandleCommand(setCmd)
	if !reply.OK {
		t.Fatalf("pre-load queue failed: %v", reply.Err)
	}

	// Capture state before the failing command
	snapshotBefore := engine.Queue.Snapshot(0, 10)
	summaryBefore := engine.Queue.Summary()

	// Mock the playlist server to return an error for the snapshot
	client.onPublish = func(topic string, payload []byte) {
		if topic != mu.TopicCommands(mu.BaseTopic, "mu:playlist:plsrv:missing") {
			return
		}
		var cmd mu.CommandEnvelope
		if err := json.Unmarshal(payload, &cmd); err != nil {
			return
		}
		errReply := mu.ReplyEnvelope{
			ID:   cmd.ID,
			Type: "error",
			OK:   false,
			TS:   time.Now().Unix(),
			Err:  &mu.ReplyError{Code: "NOT_FOUND", Message: "snapshot not found"},
		}
		replyPayload, _ := json.Marshal(errReply)
		client.emit(cmd.ReplyTo, replyPayload)
	}

	// Send a loadSnapshot command that will fail (missing snapshot)
	failCmd := mu.CommandEnvelope{
		ID:      "cmd-fail",
		Type:    "queue.loadSnapshot",
		TS:      time.Now().Unix(),
		From:    "tester",
		ReplyTo: "mu/v1/reply/tester",
		Lease:   &mu.Lease{SessionID: lease.ID, Token: lease.Token},
		Body:    mustJSON(mu.QueueLoadSnapshotBody{PlaylistServerID: "mu:playlist:plsrv:missing", SnapshotID: "snap-missing", Mode: "replace", Resolve: "no"}),
	}

	module.handleMessage(fakeMessage{topic: module.cmdTopic, payload: mustJSON(failCmd)})

	// Wait for the command to be processed
	time.Sleep(200 * time.Millisecond)

	// Verify the queue was not corrupted: entries, index, repeat should be unchanged
	snapshotAfter := engine.Queue.Snapshot(0, 10)
	summaryAfter := engine.Queue.Summary()

	if len(snapshotAfter.Entries) != len(snapshotBefore.Entries) {
		t.Fatalf("queue entries changed: before=%d, after=%d", len(snapshotBefore.Entries), len(snapshotAfter.Entries))
	}
	if summaryAfter.Index != summaryBefore.Index {
		t.Fatalf("queue index changed: before=%d, after=%d", summaryBefore.Index, summaryAfter.Index)
	}
	if summaryAfter.Repeat != summaryBefore.Repeat {
		t.Fatalf("queue repeat changed: before=%v, after=%v", summaryBefore.Repeat, summaryAfter.Repeat)
	}
	if summaryAfter.Revision != summaryBefore.Revision {
		t.Fatalf("queue revision changed: before=%d, after=%d", summaryBefore.Revision, summaryAfter.Revision)
	}
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
