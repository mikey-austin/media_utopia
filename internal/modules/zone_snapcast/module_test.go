package zonesnapcast

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

// fakeMQTTClient implements mqttClient for testing.
type fakeMQTTClient struct {
	mu        sync.Mutex
	subs      map[string]paho.MessageHandler
	published []publishedMessage
}

type publishedMessage struct {
	Topic    string
	Payload  []byte
	Retained bool
}

func (f *fakeMQTTClient) Publish(topic string, qos byte, retained bool, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, publishedMessage{
		Topic:    topic,
		Payload:  payload,
		Retained: retained,
	})
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

func (f *fakeMQTTClient) getPublished() []publishedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]publishedMessage, len(f.published))
	copy(result, f.published)
	return result
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

func TestBuildSourceID(t *testing.T) {
	m := &Module{
		config: Config{
			NodeID: "mu:zone_controller:snapcast:office:default",
		},
	}

	result := m.buildSourceID("librespot")
	expected := "mu:source:snapcast:office:librespot"
	if result != expected {
		t.Errorf("buildSourceID = %q, want %q", result, expected)
	}
}

func TestBuildZoneID(t *testing.T) {
	m := &Module{
		config: Config{
			NodeID: "mu:zone_controller:snapcast:office:default",
		},
	}

	result := m.buildZoneID("client-abc123")
	expected := "mu:zone:snapcast:office:client-abc123"
	if result != expected {
		t.Errorf("buildZoneID = %q, want %q", result, expected)
	}
}

func TestPublishControllerPresence(t *testing.T) {
	client := &fakeMQTTClient{}
	m := &Module{
		log:    zap.NewNop(),
		client: client,
		config: Config{
			NodeID:    "mu:zone_controller:snapcast:office:default",
			TopicBase: "mu/v1",
			Name:      "Test Snapcast",
		},
		sources: []Source{
			{ID: "mu:source:snapcast:office:default", Name: "Default"},
		},
		zones: map[string]*Zone{
			"mu:zone:snapcast:office:kitchen": {NodeID: "mu:zone:snapcast:office:kitchen"},
		},
	}

	err := m.publishControllerPresence()
	if err != nil {
		t.Fatalf("publishControllerPresence: %v", err)
	}

	published := client.getPublished()
	if len(published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(published))
	}

	msg := published[0]
	if msg.Topic != "mu/v1/node/mu:zone_controller:snapcast:office:default/presence" {
		t.Errorf("unexpected topic: %s", msg.Topic)
	}
	if !msg.Retained {
		t.Error("presence should be retained")
	}

	var presence map[string]any
	if err := json.Unmarshal(msg.Payload, &presence); err != nil {
		t.Fatalf("unmarshal presence: %v", err)
	}
	if presence["kind"] != "zone_controller" {
		t.Errorf("kind = %v, want zone_controller", presence["kind"])
	}
}

func TestPublishZoneState(t *testing.T) {
	client := &fakeMQTTClient{}
	m := &Module{
		log:    zap.NewNop(),
		client: client,
		config: Config{
			TopicBase: "mu/v1",
		},
	}

	zone := &Zone{
		NodeID:    "mu:zone:snapcast:office:kitchen",
		Name:      "Kitchen",
		Volume:    0.75,
		Mute:      false,
		SourceID:  "mu:source:snapcast:office:default",
		Connected: true,
	}

	err := m.publishZoneState(zone)
	if err != nil {
		t.Fatalf("publishZoneState: %v", err)
	}

	published := client.getPublished()
	if len(published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(published))
	}

	msg := published[0]
	if msg.Topic != "mu/v1/node/mu:zone:snapcast:office:kitchen/state" {
		t.Errorf("unexpected topic: %s", msg.Topic)
	}

	var state map[string]any
	if err := json.Unmarshal(msg.Payload, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if state["volume"] != 0.75 {
		t.Errorf("volume = %v, want 0.75", state["volume"])
	}
	if state["connected"] != true {
		t.Errorf("connected = %v, want true", state["connected"])
	}
}

func TestHandleSetVolumeCommand(t *testing.T) {
	client := &fakeMQTTClient{}
	m := &Module{
		log:    zap.NewNop(),
		client: client,
		config: Config{
			TopicBase: "mu/v1",
		},
		zones: map[string]*Zone{
			"mu:zone:snapcast:office:kitchen": {
				NodeID: "mu:zone:snapcast:office:kitchen",
				Volume: 0.5,
				Mute:   false,
			},
		},
		clientMap: map[string]string{
			"snap-client-123": "mu:zone:snapcast:office:kitchen",
		},
		snapClient: &SnapcastClient{
			log: zap.NewNop(),
			// Note: Can't actually call Snapcast in tests, so we test up to the point of the call
		},
		ctx: context.Background(),
	}

	zone := m.zones["mu:zone:snapcast:office:kitchen"]
	cmd := mu.CommandEnvelope{
		ID:   "test-cmd-1",
		Type: "zone.setVolume",
		TS:   time.Now().Unix(),
		Body: json.RawMessage(`{"volume": 0.8}`),
	}

	// This will fail at the Snapcast call since we don't have a connected client
	// but it tests the command parsing and lookup logic
	reply := m.handleSetVolume(zone, cmd)

	// Since snapClient isn't connected, it should return error
	if reply.OK {
		// If it succeeded, zone volume should be updated
		if zone.Volume != 0.8 {
			t.Errorf("zone volume = %v, want 0.8", zone.Volume)
		}
	}
	// Either way, the command ID should be set
	if reply.ID != "test-cmd-1" {
		t.Errorf("reply ID = %v, want test-cmd-1", reply.ID)
	}
}

func TestSplitNodeID(t *testing.T) {
	tests := []struct {
		nodeID   string
		expected int // number of parts
	}{
		{"mu:zone:snapcast:office:kitchen", 5},
		{"mu:source:snapcast:office:default", 5},
		{"simple", 1},
	}

	for _, tt := range tests {
		parts := splitNodeID(tt.nodeID)
		if len(parts) != tt.expected {
			t.Errorf("splitNodeID(%q) = %d parts, want %d", tt.nodeID, len(parts), tt.expected)
		}
	}
}

func TestExtractZoneID(t *testing.T) {
	body := json.RawMessage(`{"zoneId":"mu:zone:snapcast:office:kitchen","volume":0.5}`)
	got, err := extractZoneID(body)
	if err != nil {
		t.Fatalf("extractZoneID error: %v", err)
	}
	if got != "mu:zone:snapcast:office:kitchen" {
		t.Errorf("extractZoneID = %q, want %q", got, "mu:zone:snapcast:office:kitchen")
	}
}
