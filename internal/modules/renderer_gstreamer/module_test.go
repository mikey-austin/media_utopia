package renderergstreamer

import (
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
func (d stubDriver) Seek(positionMS int64) error             { return nil }
func (d stubDriver) SetVolume(volume float64) error          { return nil }
func (d stubDriver) SetMute(mute bool) error                 { return nil }
func (d stubDriver) Position() (int64, int64, bool)          { return 0, 0, false }

func TestQueueLoadPlaylist(t *testing.T) {
	client := &fakeMQTTClient{}
	engine := renderercore.NewEngine("mu:renderer:test", "Test Renderer", stubDriver{})
	module := &Module{
		log:    zap.NewNop(),
		client: client,
		engine: engine,
		config: Config{
			NodeID:    "mu:renderer:test",
			TopicBase: mu.BaseTopic,
			Name:      "Test Renderer",
		},
	}

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
			Entries: []playlistEntry{{
				EntryID:  "entry-1",
				Resolved: &mu.ResolvedSource{URL: "https://example.com/track.mp3"},
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
		if len(state.Entries) == 1 && state.Entries[0].ItemID != "" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("expected queue to load")
		case <-tick.C:
		}
	}
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
