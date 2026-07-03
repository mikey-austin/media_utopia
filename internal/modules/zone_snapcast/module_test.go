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

func TestZoneStatePublish(t *testing.T) {
	// Verify that publishing zone state produces the correct topic, payload fields, and retained flag
	// for different zone configurations (muted, different volumes, different sources).
	tests := []struct {
		name     string
		zone     Zone
		wantMute bool
		wantVol  float64
		wantSrc  string
		wantConn bool
	}{
		{
			name: "unmuted full volume",
			zone: Zone{
				NodeID:    "mu:zone:snapcast:office:kitchen",
				Volume:    1.0,
				Mute:      false,
				SourceID:  "mu:source:snapcast:office:default",
				Connected: true,
			},
			wantMute: false,
			wantVol:  1.0,
			wantSrc:  "mu:source:snapcast:office:default",
			wantConn: true,
		},
		{
			name: "muted at half volume",
			zone: Zone{
				NodeID:    "mu:zone:snapcast:office:bedroom",
				Volume:    0.5,
				Mute:      true,
				SourceID:  "mu:source:snapcast:office:librespot",
				Connected: true,
			},
			wantMute: true,
			wantVol:  0.5,
			wantSrc:  "mu:source:snapcast:office:librespot",
			wantConn: true,
		},
		{
			name: "disconnected zone",
			zone: Zone{
				NodeID:    "mu:zone:snapcast:office:garage",
				Volume:    0.0,
				Mute:      false,
				SourceID:  "mu:source:snapcast:office:default",
				Connected: false,
			},
			wantMute: false,
			wantVol:  0.0,
			wantSrc:  "mu:source:snapcast:office:default",
			wantConn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMQTTClient{}
			m := &Module{
				log:    zap.NewNop(),
				client: client,
				config: Config{TopicBase: "mu/v1"},
			}

			zone := tt.zone
			if err := m.publishZoneState(&zone); err != nil {
				t.Fatalf("publishZoneState: %v", err)
			}

			published := client.getPublished()
			if len(published) != 1 {
				t.Fatalf("expected 1 published message, got %d", len(published))
			}

			msg := published[0]
			wantTopic := "mu/v1/node/" + tt.zone.NodeID + "/state"
			if msg.Topic != wantTopic {
				t.Errorf("topic = %q, want %q", msg.Topic, wantTopic)
			}
			if !msg.Retained {
				t.Error("state should be retained")
			}

			var state map[string]any
			if err := json.Unmarshal(msg.Payload, &state); err != nil {
				t.Fatalf("unmarshal state: %v", err)
			}
			if state["volume"] != tt.wantVol {
				t.Errorf("volume = %v, want %v", state["volume"], tt.wantVol)
			}
			if state["mute"] != tt.wantMute {
				t.Errorf("mute = %v, want %v", state["mute"], tt.wantMute)
			}
			if state["sourceId"] != tt.wantSrc {
				t.Errorf("sourceId = %v, want %v", state["sourceId"], tt.wantSrc)
			}
			if state["connected"] != tt.wantConn {
				t.Errorf("connected = %v, want %v", state["connected"], tt.wantConn)
			}
			if _, ok := state["ts"]; !ok {
				t.Error("state should contain ts field")
			}
		})
	}
}

func TestHandleSetMuteCommand(t *testing.T) {
	tests := []struct {
		name        string
		initialMute bool
		cmdMute     bool
	}{
		{"mute from unmuted", false, true},
		{"unmute from muted", true, false},
		{"mute already muted", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeMQTTClient{}
			m := &Module{
				log:    zap.NewNop(),
				client: client,
				config: Config{TopicBase: "mu/v1"},
				zones: map[string]*Zone{
					"mu:zone:snapcast:office:kitchen": {
						NodeID: "mu:zone:snapcast:office:kitchen",
						Volume: 0.65,
						Mute:   tt.initialMute,
					},
				},
				clientMap: map[string]string{
					"snap-client-123": "mu:zone:snapcast:office:kitchen",
				},
				zoneToClient: map[string]string{
					"mu:zone:snapcast:office:kitchen": "snap-client-123",
				},
				snapClient: &SnapcastClient{
					log: zap.NewNop(),
					// Not connected -- call will fail at the Snapcast RPC level
				},
				ctx: context.Background(),
			}

			zone := m.zones["mu:zone:snapcast:office:kitchen"]
			body, _ := json.Marshal(map[string]any{
				"zoneId": zone.NodeID,
				"mute":   tt.cmdMute,
			})
			cmd := mu.CommandEnvelope{
				ID:   "test-mute-cmd",
				Type: "zone.setMute",
				TS:   time.Now().Unix(),
				Body: json.RawMessage(body),
			}

			reply := m.handleSetMute(zone, cmd)

			// snapClient is not connected so the RPC call fails
			if reply.OK {
				t.Error("expected reply.OK=false because snapClient is not connected")
			}
			if reply.ID != "test-mute-cmd" {
				t.Errorf("reply ID = %q, want %q", reply.ID, "test-mute-cmd")
			}
			if reply.Type != "error" {
				t.Errorf("reply Type = %q, want %q", reply.Type, "error")
			}
		})
	}
}

func TestHandleSetMuteCommandBadBody(t *testing.T) {
	client := &fakeMQTTClient{}
	m := &Module{
		log:    zap.NewNop(),
		client: client,
		config: Config{TopicBase: "mu/v1"},
		zones: map[string]*Zone{
			"mu:zone:snapcast:office:kitchen": {
				NodeID: "mu:zone:snapcast:office:kitchen",
				Volume: 0.5,
				Mute:   false,
			},
		},
		zoneToClient: map[string]string{
			"mu:zone:snapcast:office:kitchen": "snap-client-123",
		},
		snapClient: &SnapcastClient{log: zap.NewNop()},
		ctx:        context.Background(),
	}

	zone := m.zones["mu:zone:snapcast:office:kitchen"]
	cmd := mu.CommandEnvelope{
		ID:   "test-bad-mute",
		Type: "zone.setMute",
		TS:   time.Now().Unix(),
		Body: json.RawMessage(`{invalid json`),
	}

	reply := m.handleSetMute(zone, cmd)
	if reply.OK {
		t.Error("expected error reply for invalid JSON body")
	}
	if reply.ID != "test-bad-mute" {
		t.Errorf("reply ID = %q, want %q", reply.ID, "test-bad-mute")
	}
}

func TestHandleSetMuteCommandNoClient(t *testing.T) {
	// Zone exists but has no reverse mapping in zoneToClient
	client := &fakeMQTTClient{}
	m := &Module{
		log:    zap.NewNop(),
		client: client,
		config: Config{TopicBase: "mu/v1"},
		zones: map[string]*Zone{
			"mu:zone:snapcast:office:kitchen": {
				NodeID: "mu:zone:snapcast:office:kitchen",
				Volume: 0.5,
				Mute:   false,
			},
		},
		zoneToClient: map[string]string{}, // empty -- no mapping
		snapClient:   &SnapcastClient{log: zap.NewNop()},
		ctx:          context.Background(),
	}

	zone := m.zones["mu:zone:snapcast:office:kitchen"]
	body, _ := json.Marshal(map[string]any{"zoneId": zone.NodeID, "mute": true})
	cmd := mu.CommandEnvelope{
		ID:   "test-no-client",
		Type: "zone.setMute",
		TS:   time.Now().Unix(),
		Body: json.RawMessage(body),
	}

	reply := m.handleSetMute(zone, cmd)
	if reply.OK {
		t.Error("expected error reply when zoneToClient mapping is missing")
	}
	if reply.Type != "error" {
		t.Errorf("reply Type = %q, want %q", reply.Type, "error")
	}
}

func TestHandleSelectSourceCommand(t *testing.T) {
	client := &fakeMQTTClient{}
	m := &Module{
		log:    zap.NewNop(),
		client: client,
		config: Config{TopicBase: "mu/v1"},
		zones: map[string]*Zone{
			"mu:zone:snapcast:office:kitchen": {
				NodeID:   "mu:zone:snapcast:office:kitchen",
				SourceID: "mu:source:snapcast:office:default",
			},
		},
		zoneToClient: map[string]string{
			"mu:zone:snapcast:office:kitchen": "snap-client-123",
		},
		groupMap: map[string]string{
			"mu:zone:snapcast:office:kitchen": "group-abc",
		},
		snapClient: &SnapcastClient{
			log: zap.NewNop(),
			// Not connected -- RPC will fail
		},
		ctx: context.Background(),
	}

	zone := m.zones["mu:zone:snapcast:office:kitchen"]
	body, _ := json.Marshal(map[string]any{
		"zoneId":   zone.NodeID,
		"sourceId": "mu:source:snapcast:office:librespot",
	})
	cmd := mu.CommandEnvelope{
		ID:   "test-source-cmd",
		Type: "zone.selectSource",
		TS:   time.Now().Unix(),
		Body: json.RawMessage(body),
	}

	reply := m.handleSelectSource(zone, cmd)

	// snapClient not connected, so the Snapcast RPC call will fail
	if reply.OK {
		t.Error("expected reply.OK=false because snapClient is not connected")
	}
	if reply.ID != "test-source-cmd" {
		t.Errorf("reply ID = %q, want %q", reply.ID, "test-source-cmd")
	}
}

func TestHandleSelectSourceCommandBadSourceID(t *testing.T) {
	// sourceId with fewer than 5 colon-separated parts should be rejected
	client := &fakeMQTTClient{}
	m := &Module{
		log:    zap.NewNop(),
		client: client,
		config: Config{TopicBase: "mu/v1"},
		zones: map[string]*Zone{
			"mu:zone:snapcast:office:kitchen": {
				NodeID: "mu:zone:snapcast:office:kitchen",
			},
		},
		groupMap: map[string]string{
			"mu:zone:snapcast:office:kitchen": "group-abc",
		},
		snapClient: &SnapcastClient{log: zap.NewNop()},
		ctx:        context.Background(),
	}

	zone := m.zones["mu:zone:snapcast:office:kitchen"]
	body, _ := json.Marshal(map[string]any{
		"zoneId":   zone.NodeID,
		"sourceId": "tooshort:parts",
	})
	cmd := mu.CommandEnvelope{
		ID:   "test-bad-source",
		Type: "zone.selectSource",
		TS:   time.Now().Unix(),
		Body: json.RawMessage(body),
	}

	reply := m.handleSelectSource(zone, cmd)
	if reply.OK {
		t.Error("expected error reply for malformed sourceId")
	}
	if reply.Type != "error" {
		t.Errorf("reply Type = %q, want %q", reply.Type, "error")
	}
}

func TestHandleSelectSourceCommandNoGroup(t *testing.T) {
	// Zone has no group mapping
	client := &fakeMQTTClient{}
	m := &Module{
		log:    zap.NewNop(),
		client: client,
		config: Config{TopicBase: "mu/v1"},
		zones: map[string]*Zone{
			"mu:zone:snapcast:office:kitchen": {
				NodeID: "mu:zone:snapcast:office:kitchen",
			},
		},
		groupMap:   map[string]string{}, // empty
		snapClient: &SnapcastClient{log: zap.NewNop()},
		ctx:        context.Background(),
	}

	zone := m.zones["mu:zone:snapcast:office:kitchen"]
	body, _ := json.Marshal(map[string]any{
		"zoneId":   zone.NodeID,
		"sourceId": "mu:source:snapcast:office:librespot",
	})
	cmd := mu.CommandEnvelope{
		ID:   "test-no-group",
		Type: "zone.selectSource",
		TS:   time.Now().Unix(),
		Body: json.RawMessage(body),
	}

	reply := m.handleSelectSource(zone, cmd)
	if reply.OK {
		t.Error("expected error reply when groupMap is missing")
	}
	if reply.Type != "error" {
		t.Errorf("reply Type = %q, want %q", reply.Type, "error")
	}
}

func TestZoneIDFromNodeID(t *testing.T) {
	// Test buildZoneID with various controller NodeID formats and edge cases.
	tests := []struct {
		name             string
		controllerNodeID string
		clientID         string
		expected         string
	}{
		{
			name:             "standard 5-part node ID",
			controllerNodeID: "mu:zone_controller:snapcast:office:default",
			clientID:         "client-abc123",
			expected:         "mu:zone:snapcast:office:client-abc123",
		},
		{
			name:             "different namespace",
			controllerNodeID: "mu:zone_controller:snapcast:home:main",
			clientID:         "aabbccdd",
			expected:         "mu:zone:snapcast:home:aabbccdd",
		},
		{
			name:             "short node ID falls back to default namespace",
			controllerNodeID: "mu:zone_controller:snapcast",
			clientID:         "client-xyz",
			expected:         "mu:zone:snapcast:default:client-xyz",
		},
		{
			name:             "single part node ID falls back to default",
			controllerNodeID: "simple",
			clientID:         "c1",
			expected:         "mu:zone:snapcast:default:c1",
		},
		{
			name:             "empty node ID falls back to default",
			controllerNodeID: "",
			clientID:         "c1",
			expected:         "mu:zone:snapcast:default:c1",
		},
		{
			name:             "exactly 4 parts uses namespace from index 3",
			controllerNodeID: "mu:zone_controller:snapcast:studio",
			clientID:         "client-1",
			expected:         "mu:zone:snapcast:studio:client-1",
		},
		{
			name:             "extra parts still uses index 3",
			controllerNodeID: "mu:zone_controller:snapcast:basement:primary:extra",
			clientID:         "client-2",
			expected:         "mu:zone:snapcast:basement:client-2",
		},
		{
			name:             "client ID with colons",
			controllerNodeID: "mu:zone_controller:snapcast:office:default",
			clientID:         "aa:bb:cc:dd",
			expected:         "mu:zone:snapcast:office:aa:bb:cc:dd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Module{
				config: Config{NodeID: tt.controllerNodeID},
			}
			got := m.buildZoneID(tt.clientID)
			if got != tt.expected {
				t.Errorf("buildZoneID(%q) = %q, want %q", tt.clientID, got, tt.expected)
			}
		})
	}
}

func TestMultipleZones(t *testing.T) {
	// Verify that multiple zones are tracked and published independently.
	client := &fakeMQTTClient{}
	m := &Module{
		log:    zap.NewNop(),
		client: client,
		config: Config{
			NodeID:    "mu:zone_controller:snapcast:home:default",
			TopicBase: "mu/v1",
			Name:      "Home Snapcast",
		},
		zones:        make(map[string]*Zone),
		clientMap:    make(map[string]string),
		zoneToClient: make(map[string]string),
		groupMap:     make(map[string]string),
		sources: []Source{
			{ID: "mu:source:snapcast:home:default", Name: "Default"},
			{ID: "mu:source:snapcast:home:librespot", Name: "Spotify"},
		},
	}

	// Add three zones with different states
	zonesData := []struct {
		nodeID    string
		name      string
		volume    float64
		mute      bool
		sourceID  string
		connected bool
	}{
		{"mu:zone:snapcast:home:kitchen", "Kitchen", 0.8, false, "mu:source:snapcast:home:default", true},
		{"mu:zone:snapcast:home:bedroom", "Bedroom", 0.3, true, "mu:source:snapcast:home:librespot", true},
		{"mu:zone:snapcast:home:garage", "Garage", 0.0, false, "mu:source:snapcast:home:default", false},
	}

	for _, zd := range zonesData {
		m.zones[zd.nodeID] = &Zone{
			NodeID:       zd.nodeID,
			Name:         zd.name,
			ControllerID: m.config.NodeID,
			Volume:       zd.volume,
			Mute:         zd.mute,
			SourceID:     zd.sourceID,
			Connected:    zd.connected,
		}
	}

	// Publish state for each zone independently
	for _, zone := range m.zones {
		if err := m.publishZoneState(zone); err != nil {
			t.Fatalf("publishZoneState(%s): %v", zone.NodeID, err)
		}
	}

	published := client.getPublished()
	if len(published) != 3 {
		t.Fatalf("expected 3 published messages, got %d", len(published))
	}

	// Verify each zone published to its own topic with correct state
	stateByTopic := make(map[string]map[string]any)
	for _, msg := range published {
		var state map[string]any
		if err := json.Unmarshal(msg.Payload, &state); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		stateByTopic[msg.Topic] = state
	}

	// Check kitchen
	kitchenState, ok := stateByTopic["mu/v1/node/mu:zone:snapcast:home:kitchen/state"]
	if !ok {
		t.Fatal("kitchen state not published")
	}
	if kitchenState["volume"] != 0.8 {
		t.Errorf("kitchen volume = %v, want 0.8", kitchenState["volume"])
	}
	if kitchenState["mute"] != false {
		t.Errorf("kitchen mute = %v, want false", kitchenState["mute"])
	}

	// Check bedroom
	bedroomState, ok := stateByTopic["mu/v1/node/mu:zone:snapcast:home:bedroom/state"]
	if !ok {
		t.Fatal("bedroom state not published")
	}
	if bedroomState["volume"] != 0.3 {
		t.Errorf("bedroom volume = %v, want 0.3", bedroomState["volume"])
	}
	if bedroomState["mute"] != true {
		t.Errorf("bedroom mute = %v, want true", bedroomState["mute"])
	}
	if bedroomState["sourceId"] != "mu:source:snapcast:home:librespot" {
		t.Errorf("bedroom sourceId = %v, want librespot", bedroomState["sourceId"])
	}

	// Check garage
	garageState, ok := stateByTopic["mu/v1/node/mu:zone:snapcast:home:garage/state"]
	if !ok {
		t.Fatal("garage state not published")
	}
	if garageState["connected"] != false {
		t.Errorf("garage connected = %v, want false", garageState["connected"])
	}

	// Also verify controller presence includes all zone IDs
	if err := m.publishControllerPresence(); err != nil {
		t.Fatalf("publishControllerPresence: %v", err)
	}

	published = client.getPublished()
	// 3 zone states + 1 controller presence = 4 total
	if len(published) != 4 {
		t.Fatalf("expected 4 published messages after controller presence, got %d", len(published))
	}

	ctrlMsg := published[3]
	var presence map[string]any
	if err := json.Unmarshal(ctrlMsg.Payload, &presence); err != nil {
		t.Fatalf("unmarshal presence: %v", err)
	}

	zonesField, ok := presence["zones"].([]any)
	if !ok {
		t.Fatalf("zones field missing or wrong type: %T", presence["zones"])
	}
	if len(zonesField) != 3 {
		t.Errorf("controller presence lists %d zones, want 3", len(zonesField))
	}

	// Verify all zone IDs are present
	zoneSet := make(map[string]bool)
	for _, z := range zonesField {
		zoneSet[z.(string)] = true
	}
	for _, zd := range zonesData {
		if !zoneSet[zd.nodeID] {
			t.Errorf("controller presence missing zone %q", zd.nodeID)
		}
	}
}

func TestMultipleZonesPresenceIndependent(t *testing.T) {
	// Verify that each zone's presence message is published independently with the correct caps.
	client := &fakeMQTTClient{}
	m := &Module{
		log:    zap.NewNop(),
		client: client,
		config: Config{
			NodeID:    "mu:zone_controller:snapcast:home:default",
			TopicBase: "mu/v1",
			Name:      "Home Snapcast",
		},
		zones: make(map[string]*Zone),
	}

	zone1 := &Zone{
		NodeID:       "mu:zone:snapcast:home:living",
		Name:         "Living Room",
		ControllerID: m.config.NodeID,
	}
	zone2 := &Zone{
		NodeID:       "mu:zone:snapcast:home:study",
		Name:         "Study",
		ControllerID: m.config.NodeID,
	}

	if err := m.publishZonePresence(zone1); err != nil {
		t.Fatalf("publishZonePresence(living): %v", err)
	}
	if err := m.publishZonePresence(zone2); err != nil {
		t.Fatalf("publishZonePresence(study): %v", err)
	}

	published := client.getPublished()
	if len(published) != 2 {
		t.Fatalf("expected 2 presence messages, got %d", len(published))
	}

	// Verify topics are distinct
	if published[0].Topic == published[1].Topic {
		t.Error("both zones published to the same topic")
	}

	// Verify each has correct kind and caps
	for _, msg := range published {
		var p map[string]any
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if p["kind"] != "zone" {
			t.Errorf("kind = %v, want zone", p["kind"])
		}
		if p["controllerId"] != m.config.NodeID {
			t.Errorf("controllerId = %v, want %v", p["controllerId"], m.config.NodeID)
		}
		caps, ok := p["caps"].(map[string]any)
		if !ok {
			t.Fatal("caps missing or wrong type")
		}
		for _, cap := range []string{"volume", "mute", "selectSource"} {
			if caps[cap] != true {
				t.Errorf("cap %q = %v, want true", cap, caps[cap])
			}
		}
		if !msg.Retained {
			t.Error("zone presence should be retained")
		}
	}
}
