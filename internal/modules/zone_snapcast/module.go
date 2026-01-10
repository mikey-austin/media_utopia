package zonesnapcast

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

// mqttClient abstracts MQTT operations.
type mqttClient interface {
	Publish(topic string, qos byte, retained bool, payload []byte) error
	Subscribe(topic string, qos byte, handler paho.MessageHandler) error
	Unsubscribe(topic string) error
}

// Config configures the Snapcast zone module.
type Config struct {
	NodeID       string            // Zone controller node ID (mu:zone_controller:snapcast:namespace:resource)
	TopicBase    string            // MQTT topic base (e.g. "mu/v1")
	Name         string            // Human-readable name
	ServerURL    string            // Snapcast server URL (ws://host:1780/jsonrpc)
	PollInterval time.Duration     // How often to poll Snapcast for updates
	Zones        map[string]string // Alias mapping for zones (clientID -> name)
}

// Source represents an audio source (Snapcast stream).
type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Zone represents a speaker zone (Snapcast client).
type Zone struct {
	NodeID       string  `json:"nodeId"`
	Name         string  `json:"name"`
	ControllerID string  `json:"controllerId"`
	Volume       float64 `json:"volume"`
	Mute         bool    `json:"mute"`
	SourceID     string  `json:"sourceId"`
	Connected    bool    `json:"connected"`
}

// Module implements the Snapcast zone controller.
type Module struct {
	log        *zap.Logger
	client     mqttClient
	snapClient *SnapcastClient
	config     Config
	mu         sync.Mutex
	sources    []Source
	zones      map[string]*Zone  // keyed by zone node ID
	clientMap  map[string]string // Snapcast client ID -> zone node ID
	groupMap   map[string]string // zone node ID -> Snapcast group ID
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewModule creates a new Snapcast zone module.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*Module, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("snapcast server_url is required")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Second
	}
	snapClient := NewSnapcastClient(log, cfg.ServerURL)
	m := &Module{
		log:        log.With(zap.String("module", "zone_snapcast")),
		client:     client,
		snapClient: snapClient,
		config:     cfg,
		zones:      make(map[string]*Zone),
		clientMap:  make(map[string]string),
		groupMap:   make(map[string]string),
	}
	snapClient.SetUpdateCallback(func() {
		// Re-discover on server notifications
		go m.discoverSnapcast()
	})
	return m, nil
}

// Run starts the zone module.
func (m *Module) Run(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.log.Info("starting snapcast zone module",
		zap.String("server", m.config.ServerURL),
		zap.String("node_id", m.config.NodeID),
	)
	m.log.Debug("snapcast zone aliases configured", zap.Any("zones", m.config.Zones))

	// Subscribe to zone controller commands (zone commands route through controller)
	cmdTopic := fmt.Sprintf("%s/node/%s/cmd", m.config.TopicBase, m.config.NodeID)
	handler := func(_ paho.Client, msg paho.Message) { m.handleMessage(msg) }
	if err := m.client.Subscribe(cmdTopic, 1, handler); err != nil {
		return fmt.Errorf("subscribe cmd: %w", err)
	}
	defer m.client.Unsubscribe(cmdTopic)

	// Initial discovery
	if err := m.discoverSnapcast(); err != nil {
		m.log.Warn("initial snapcast discovery failed", zap.Error(err))
	}

	// Publish zone controller presence
	if err := m.publishControllerPresence(); err != nil {
		m.log.Warn("failed to publish controller presence", zap.Error(err))
	}

	// Run poll loop
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return nil
		case <-ticker.C:
			if err := m.discoverSnapcast(); err != nil {
				m.log.Debug("snapcast poll failed", zap.Error(err))
			}
		}
	}
}

// discoverSnapcast connects to Snapcast and discovers streams/clients.
func (m *Module) discoverSnapcast() error {
	// Connect if not already connected
	if err := m.snapClient.Connect(m.ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Get server status
	status, err := m.snapClient.GetStatus(m.ctx)
	if err != nil {
		return fmt.Errorf("get status: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Parse streams into sources
	m.sources = make([]Source, 0, len(status.Server.Streams))
	for _, stream := range status.Server.Streams {
		name := stream.URI.Query.Name
		if name == "" {
			name = stream.ID
		}
		sourceID := m.buildSourceID(stream.ID)
		m.sources = append(m.sources, Source{
			ID:   sourceID,
			Name: name,
		})
	}

	// Parse clients into zones
	for _, group := range status.Server.Groups {
		sourceID := m.buildSourceID(group.Stream)
		for idx, client := range group.Clients {
			m.log.Debug("snapcast client",
				zap.String("group_id", group.ID),
				zap.String("client_id", client.ID),
				zap.String("config_name", client.Config.Name),
				zap.String("host_name", client.Host.Name),
				zap.String("host_mac", client.Host.MAC),
				zap.String("host_ip", client.Host.IP),
			)
			name := m.zoneAliasForClient(client)
			if name == "" {
				name = client.Config.Name
			}
			if name == "" {
				name = defaultZoneName(client, idx)
			}

			zoneID := m.buildZoneID(client.ID)
			zone, exists := m.zones[zoneID]
			if !exists {
				zone = &Zone{
					NodeID:       zoneID,
					ControllerID: m.config.NodeID,
				}
				m.zones[zoneID] = zone
			}

			// Update zone state
			zone.Name = name
			zone.Volume = float64(client.Config.Volume.Percent) / 100.0
			zone.Mute = client.Config.Volume.Muted
			zone.SourceID = sourceID
			zone.Connected = client.Connected

			// Track mappings for command handling
			m.clientMap[client.ID] = zoneID
			m.groupMap[zoneID] = group.ID
		}
	}

	// Publish presence/state updates
	go m.publishAllUpdates()
	return nil
}

// buildSourceID creates a source node ID from Snapcast stream ID.
func (m *Module) buildSourceID(streamID string) string {
	// Extract namespace and resource from controller node ID
	// mu:zone_controller:snapcast:namespace:resource -> mu:source:snapcast:namespace:streamID
	parts := splitNodeID(m.config.NodeID)
	if len(parts) >= 4 {
		return fmt.Sprintf("mu:source:snapcast:%s:%s", parts[3], streamID)
	}
	return fmt.Sprintf("mu:source:snapcast:default:%s", streamID)
}

// buildZoneID creates a zone node ID from Snapcast client ID.
func (m *Module) buildZoneID(clientID string) string {
	parts := splitNodeID(m.config.NodeID)
	if len(parts) >= 4 {
		return fmt.Sprintf("mu:zone:snapcast:%s:%s", parts[3], clientID)
	}
	return fmt.Sprintf("mu:zone:snapcast:default:%s", clientID)
}

// splitNodeID splits a node ID into parts.
func splitNodeID(nodeID string) []string {
	return strings.Split(nodeID, ":")
}

// publishAllUpdates publishes presence/state for controller and all zones.
func (m *Module) publishAllUpdates() {
	if err := m.publishControllerPresence(); err != nil {
		m.log.Debug("failed to publish controller presence", zap.Error(err))
	}

	m.mu.Lock()
	zones := make([]*Zone, 0, len(m.zones))
	for _, z := range m.zones {
		zones = append(zones, z)
	}
	m.mu.Unlock()

	for _, zone := range zones {
		if err := m.publishZonePresence(zone); err != nil {
			m.log.Debug("failed to publish zone presence", zap.String("zone", zone.NodeID), zap.Error(err))
		}
		if err := m.publishZoneState(zone); err != nil {
			m.log.Debug("failed to publish zone state", zap.String("zone", zone.NodeID), zap.Error(err))
		}
	}
}

// publishControllerPresence publishes the zone controller presence.
func (m *Module) publishControllerPresence() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	zoneIDs := make([]string, 0, len(m.zones))
	for id := range m.zones {
		zoneIDs = append(zoneIDs, id)
	}

	presence := map[string]any{
		"nodeId":  m.config.NodeID,
		"kind":    "zone_controller",
		"name":    m.config.Name,
		"sources": m.sources,
		"zones":   zoneIDs,
		"ts":      time.Now().Unix(),
	}

	payload, err := json.Marshal(presence)
	if err != nil {
		return err
	}

	topic := fmt.Sprintf("%s/node/%s/presence", m.config.TopicBase, m.config.NodeID)
	return m.client.Publish(topic, 1, true, payload)
}

// publishZonePresence publishes a zone's presence.
func (m *Module) publishZonePresence(zone *Zone) error {
	presence := map[string]any{
		"nodeId":       zone.NodeID,
		"kind":         "zone",
		"name":         zone.Name,
		"controllerId": zone.ControllerID,
		"caps":         map[string]bool{"volume": true, "mute": true, "selectSource": true},
		"ts":           time.Now().Unix(),
	}

	payload, err := json.Marshal(presence)
	if err != nil {
		return err
	}

	topic := fmt.Sprintf("%s/node/%s/presence", m.config.TopicBase, zone.NodeID)
	return m.client.Publish(topic, 1, true, payload)
}

// publishZoneState publishes a zone's state.
func (m *Module) publishZoneState(zone *Zone) error {
	state := map[string]any{
		"volume":    zone.Volume,
		"mute":      zone.Mute,
		"sourceId":  zone.SourceID,
		"connected": zone.Connected,
		"ts":        time.Now().Unix(),
	}

	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}

	topic := fmt.Sprintf("%s/node/%s/state", m.config.TopicBase, zone.NodeID)
	return m.client.Publish(topic, 1, true, payload)
}

// handleMessage handles incoming MQTT messages.
func (m *Module) handleMessage(msg paho.Message) {
	var cmd mu.CommandEnvelope
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		return
	}

	zone := m.zoneForCommand(cmd)
	if zone == nil {
		m.log.Debug("zone not found")
		return
	}

	var reply mu.ReplyEnvelope
	switch cmd.Type {
	case "zone.setVolume":
		reply = m.handleSetVolume(zone, cmd)
	case "zone.setMute":
		reply = m.handleSetMute(zone, cmd)
	case "zone.selectSource":
		reply = m.handleSelectSource(zone, cmd)
	default:
		return
	}

	m.publishReply(cmd.ReplyTo, reply)
}

func (m *Module) zoneForCommand(cmd mu.CommandEnvelope) *Zone {
	zoneID, err := extractZoneID(cmd.Body)
	if err != nil {
		m.log.Debug("invalid zone command body", zap.Error(err), zap.String("cmd_type", cmd.Type))
		return nil
	}
	if zoneID == "" {
		m.log.Debug("zoneId missing in controller command", zap.String("cmd_type", cmd.Type))
		return nil
	}
	m.mu.Lock()
	zone := m.zones[zoneID]
	m.mu.Unlock()
	if zone == nil {
		m.log.Debug("zoneId not found for controller command", zap.String("zone_id", zoneID), zap.String("cmd_type", cmd.Type))
	}
	return zone
}

func extractZoneID(body json.RawMessage) (string, error) {
	var payload struct {
		ZoneID string `json:"zoneId"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	return payload.ZoneID, nil
}

func (m *Module) handleSetVolume(zone *Zone, cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	var body struct {
		Volume float64 `json:"volume"`
	}
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return mu.ReplyEnvelope{ID: cmd.ID, Type: "error", OK: false, TS: time.Now().Unix()}
	}

	// Find Snapcast client ID from zone
	m.mu.Lock()
	var snapClientID string
	for cid, zid := range m.clientMap {
		if zid == zone.NodeID {
			snapClientID = cid
			break
		}
	}
	m.mu.Unlock()

	if snapClientID == "" {
		return mu.ReplyEnvelope{ID: cmd.ID, Type: "error", OK: false, TS: time.Now().Unix()}
	}

	percent := int(body.Volume * 100)
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	if err := m.snapClient.SetClientVolume(m.ctx, snapClientID, percent, zone.Mute); err != nil {
		m.log.Warn("failed to set volume", zap.Error(err))
		return mu.ReplyEnvelope{ID: cmd.ID, Type: "error", OK: false, TS: time.Now().Unix()}
	}

	// Update local state
	m.mu.Lock()
	zone.Volume = body.Volume
	m.mu.Unlock()

	go m.publishZoneState(zone)
	return mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix()}
}

func (m *Module) handleSetMute(zone *Zone, cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	var body struct {
		Mute bool `json:"mute"`
	}
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return mu.ReplyEnvelope{ID: cmd.ID, Type: "error", OK: false, TS: time.Now().Unix()}
	}

	// Find Snapcast client ID from zone
	m.mu.Lock()
	var snapClientID string
	for cid, zid := range m.clientMap {
		if zid == zone.NodeID {
			snapClientID = cid
			break
		}
	}
	currentVolume := int(zone.Volume * 100)
	m.mu.Unlock()

	if snapClientID == "" {
		return mu.ReplyEnvelope{ID: cmd.ID, Type: "error", OK: false, TS: time.Now().Unix()}
	}

	if err := m.snapClient.SetClientVolume(m.ctx, snapClientID, currentVolume, body.Mute); err != nil {
		m.log.Warn("failed to set mute", zap.Error(err))
		return mu.ReplyEnvelope{ID: cmd.ID, Type: "error", OK: false, TS: time.Now().Unix()}
	}
	m.log.Debug("set mute success", zap.String("client", snapClientID), zap.Bool("mute", body.Mute), zap.Int("vol", currentVolume))

	// Update local state
	m.mu.Lock()
	zone.Mute = body.Mute
	m.mu.Unlock()

	go m.publishZoneState(zone)
	return mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix()}
}

func (m *Module) handleSelectSource(zone *Zone, cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	var body struct {
		SourceID string `json:"sourceId"`
	}
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return mu.ReplyEnvelope{ID: cmd.ID, Type: "error", OK: false, TS: time.Now().Unix()}
	}

	// Extract stream ID from source node ID
	// mu:source:snapcast:namespace:streamID -> streamID
	parts := splitNodeID(body.SourceID)
	if len(parts) < 5 {
		return mu.ReplyEnvelope{ID: cmd.ID, Type: "error", OK: false, TS: time.Now().Unix()}
	}
	streamID := parts[4]

	// Get the group ID for this zone
	m.mu.Lock()
	groupID := m.groupMap[zone.NodeID]
	m.mu.Unlock()

	if groupID == "" {
		return mu.ReplyEnvelope{ID: cmd.ID, Type: "error", OK: false, TS: time.Now().Unix()}
	}

	if err := m.snapClient.SetGroupStream(m.ctx, groupID, streamID); err != nil {
		m.log.Warn("failed to set stream", zap.Error(err))
		return mu.ReplyEnvelope{ID: cmd.ID, Type: "error", OK: false, TS: time.Now().Unix()}
	}

	// Update local state
	m.mu.Lock()
	zone.SourceID = body.SourceID
	m.mu.Unlock()

	go m.publishZoneState(zone)
	return mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix()}
}

func (m *Module) publishReply(replyTo string, reply mu.ReplyEnvelope) {
	if replyTo == "" {
		return
	}
	payload, err := json.Marshal(reply)
	if err != nil {
		return
	}
	_ = m.client.Publish(replyTo, 1, false, payload)
}

func defaultZoneName(client SnapClient, index int) string {
	hostID := client.Host.Name
	if hostID == "" {
		hostID = client.Host.MAC
	}
	if hostID == "" {
		hostID = client.Host.IP
	}
	if hostID == "" {
		hostID = client.ID
	}
	if index >= 0 {
		return fmt.Sprintf("%s-%d", hostID, index+1)
	}
	return hostID
}

func (m *Module) zoneAliasForClient(client SnapClient) string {
	if len(m.config.Zones) == 0 {
		return ""
	}
	if name := m.config.Zones[client.ID]; name != "" {
		return name
	}
	if name := m.lookupZoneAlias(client.Config.Name); name != "" {
		return name
	}
	if name := m.lookupZoneAlias(client.Host.MAC); name != "" {
		return name
	}
	if name := m.lookupZoneAlias(client.Host.Name); name != "" {
		return name
	}
	if name := m.lookupZoneAlias(client.Host.IP); name != "" {
		return name
	}
	m.log.Debug("snapcast zone alias not found",
		zap.String("client_id", client.ID),
		zap.String("config_name", client.Config.Name),
		zap.String("host_mac", client.Host.MAC),
		zap.String("host_name", client.Host.Name),
		zap.String("host_ip", client.Host.IP),
		zap.Int("alias_count", len(m.config.Zones)),
	)
	return ""
}

func (m *Module) lookupZoneAlias(key string) string {
	if key == "" {
		return ""
	}
	if name := m.config.Zones[key]; name != "" {
		return name
	}
	if name := m.config.Zones[normalizeZoneKey(key)]; name != "" {
		return name
	}
	return ""
}

func normalizeZoneKey(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "-", ":")
	return normalized
}
