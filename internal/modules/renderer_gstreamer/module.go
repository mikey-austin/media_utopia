package renderergstreamer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/mikey-austin/media_utopia/internal/adapters/idgen"
	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/internal/modules/renderer_core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

type mqttClient interface {
	Publish(topic string, qos byte, retained bool, payload []byte) error
	Subscribe(topic string, qos byte, handler paho.MessageHandler) error
	Unsubscribe(topic string) error
}

// Config configures the GStreamer renderer module.
type Config struct {
	NodeID       string
	TopicBase    string
	Name         string
	Pipeline     string
	Device       string
	Crossfade    time.Duration
	Volume       float64
	PublishState bool
}

// Module implements a GStreamer renderer.
type Module struct {
	log      *zap.Logger
	client   mqttClient
	engine   *renderercore.Engine
	config   Config
	cmdTopic string
	mu       sync.Mutex
}

// NewModule creates a renderer module.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*Module, error) {
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, errors.New("node_id required")
	}
	if strings.TrimSpace(cfg.TopicBase) == "" {
		cfg.TopicBase = mu.BaseTopic
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = "GStreamer Renderer"
	}
	if strings.TrimSpace(cfg.Pipeline) == "" {
		return nil, errors.New("pipeline required")
	}

	driver, err := NewDriver(cfg.Pipeline, cfg.Device, cfg.Crossfade)
	if err != nil {
		return nil, err
	}
	engine := renderercore.NewEngine(cfg.NodeID, cfg.Name, driver)
	if cfg.Volume > 0 {
		engine.State.Playback.Volume = cfg.Volume
	}

	cmdTopic := mu.TopicCommands(cfg.TopicBase, cfg.NodeID)

	return &Module{log: log, client: client, engine: engine, config: cfg, cmdTopic: cmdTopic}, nil
}

// Run starts the renderer module.
func (m *Module) Run(ctx context.Context) error {
	if err := m.publishPresence(); err != nil {
		return err
	}
	if m.config.PublishState {
		if err := m.publishState(); err != nil {
			return err
		}
	}

	go m.runPositionUpdates(ctx)

	handler := func(_ paho.Client, msg paho.Message) {
		m.handleMessage(msg)
	}
	if err := m.client.Subscribe(m.cmdTopic, 1, handler); err != nil {
		return err
	}
	defer m.client.Unsubscribe(m.cmdTopic)

	<-ctx.Done()
	return nil
}

func (m *Module) publishPresence() error {
	presence := mu.Presence{
		NodeID: m.config.NodeID,
		Kind:   "renderer",
		Name:   m.config.Name,
		Caps: map[string]any{
			"queueResolve": false,
			"seek":         true,
			"volume":       true,
		},
		TS: time.Now().Unix(),
	}
	payload, err := json.Marshal(presence)
	if err != nil {
		return err
	}
	return m.client.Publish(mu.TopicPresence(m.config.TopicBase, m.config.NodeID), 1, true, payload)
}

func (m *Module) publishState() error {
	payload, err := json.Marshal(m.engine.State)
	if err != nil {
		return err
	}
	return m.client.Publish(mu.TopicState(m.config.TopicBase, m.config.NodeID), 1, true, payload)
}

func (m *Module) handleMessage(msg paho.Message) {
	var cmd mu.CommandEnvelope
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		m.log.Warn("invalid command", zap.Error(err))
		return
	}

	if cmd.Type == "queue.loadPlaylist" {
		go m.handleLoadCommand(cmd)
		return
	}

	m.mu.Lock()
	reply := m.dispatch(cmd)
	m.mu.Unlock()
	m.publishReply(cmd.ReplyTo, reply)
}

func (m *Module) handleLoadCommand(cmd mu.CommandEnvelope) {
	m.mu.Lock()
	reply := m.dispatch(cmd)
	m.mu.Unlock()
	m.publishReply(cmd.ReplyTo, reply)
}

func (m *Module) publishReply(replyTo string, reply mu.ReplyEnvelope) {
	if replyTo != "" {
		payload, err := json.Marshal(reply)
		if err == nil {
			_ = m.client.Publish(replyTo, 1, false, payload)
		}
	}
	if m.config.PublishState {
		_ = m.publishState()
	}
}

func (m *Module) runPositionUpdates(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.updatePlaybackState()
		}
	}
}

func (m *Module) updatePlaybackState() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.engine.State.Playback == nil || m.engine.State.Playback.Status != "playing" {
		return
	}
	posMS, durMS, ok := m.engine.Driver.Position()
	if !ok {
		return
	}
	m.engine.State.Playback.PositionMS = posMS
	if durMS > 0 {
		m.engine.State.Playback.DurationMS = durMS
	}
	m.engine.State.TS = time.Now().Unix()
	if m.config.PublishState {
		_ = m.publishState()
	}
}

func (m *Module) dispatch(cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	reply := mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix()}

	switch cmd.Type {
	case "queue.loadPlaylist":
		return m.handleQueueLoadPlaylist(cmd, reply)
	case "queue.loadSnapshot":
		return errorReply(cmd, "INVALID", "queue.loadSnapshot not supported")
	default:
		return m.engine.HandleCommand(cmd)
	}
}

func (m *Module) handleQueueLoadPlaylist(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.QueueLoadPlaylistBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if cmd.Lease == nil {
		return errorReply(cmd, "LEASE_REQUIRED", "lease required")
	}
	if err := m.engine.Leases.Require(cmd.Lease.SessionID, cmd.Lease.Token); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	if strings.TrimSpace(body.PlaylistServerID) == "" || strings.TrimSpace(body.PlaylistID) == "" {
		return errorReply(cmd, "INVALID", "playlistServerId and playlistId required")
	}
	mode := body.Mode
	if mode == "" {
		mode = "replace"
	}

	entries, err := m.fetchPlaylistEntries(cmd.From, body.PlaylistServerID, body.PlaylistID, body.Resolve)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}

	switch mode {
	case "replace":
		payload, _ := json.Marshal(mu.QueueSetBody{StartIndex: 0, Entries: entries})
		queueCmd := mu.CommandEnvelope{
			ID:    cmd.ID,
			Type:  "queue.set",
			TS:    time.Now().Unix(),
			From:  cmd.From,
			Lease: cmd.Lease,
			Body:  payload,
		}
		return m.engine.HandleCommand(queueCmd)
	case "append":
		payload, _ := json.Marshal(mu.QueueAddBody{Position: "end", Entries: entries})
		queueCmd := mu.CommandEnvelope{
			ID:    cmd.ID,
			Type:  "queue.add",
			TS:    time.Now().Unix(),
			From:  cmd.From,
			Lease: cmd.Lease,
			Body:  payload,
		}
		return m.engine.HandleCommand(queueCmd)
	case "next":
		payload, _ := json.Marshal(mu.QueueAddBody{Position: "next", Entries: entries})
		queueCmd := mu.CommandEnvelope{
			ID:    cmd.ID,
			Type:  "queue.add",
			TS:    time.Now().Unix(),
			From:  cmd.From,
			Lease: cmd.Lease,
			Body:  payload,
		}
		return m.engine.HandleCommand(queueCmd)
	default:
		return errorReply(cmd, "INVALID", "mode must be replace|append|next")
	}
}

type playlistReply struct {
	Entries []playlistEntry `json:"entries"`
}

type playlistEntry struct {
	EntryID  string             `json:"entryId"`
	Ref      *mu.ItemRef        `json:"ref,omitempty"`
	Resolved *mu.ResolvedSource `json:"resolved,omitempty"`
}

func (m *Module) fetchPlaylistEntries(owner string, serverID string, playlistID string, resolve string) ([]mu.QueueEntry, error) {
	reply, err := m.publishCommand(serverID, owner, "playlist.get", mu.PlaylistGetBody{PlaylistID: playlistID})
	if err != nil {
		return nil, err
	}
	if reply.Err != nil {
		return nil, fmt.Errorf("%s", reply.Err.Message)
	}
	var payload playlistReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		return nil, errors.New("invalid playlist reply")
	}

	needsResolve := resolve != "no"
	entries := make([]mu.QueueEntry, 0, len(payload.Entries))
	for _, entry := range payload.Entries {
		if entry.Resolved == nil && entry.Ref != nil {
			if !needsResolve {
				return nil, errors.New("playlist contains unresolved refs; add with --resolve yes")
			}
			resolved, err := m.resolveRef(owner, entry.Ref.ID)
			if err != nil {
				return nil, err
			}
			for _, source := range resolved {
				entries = append(entries, mu.QueueEntry{Ref: entry.Ref, Resolved: &source})
			}
			continue
		}
		if entry.Ref == nil && entry.Resolved == nil {
			continue
		}
		entries = append(entries, mu.QueueEntry{Ref: entry.Ref, Resolved: entry.Resolved})
	}
	return entries, nil
}

func (m *Module) publishCommand(targetID string, owner string, cmdType string, body any) (mu.ReplyEnvelope, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return mu.ReplyEnvelope{}, err
	}

	cmd := mu.CommandEnvelope{
		ID:      idgen.Generator{}.NewID(),
		Type:    cmdType,
		TS:      time.Now().Unix(),
		From:    owner,
		ReplyTo: mu.TopicReply(m.config.TopicBase, fmt.Sprintf("%s-%d", m.config.NodeID, time.Now().UnixNano())),
		Body:    payload,
	}

	replyCh := make(chan mu.ReplyEnvelope, 1)
	handler := func(_ paho.Client, msg paho.Message) {
		var reply mu.ReplyEnvelope
		if err := json.Unmarshal(msg.Payload(), &reply); err != nil {
			return
		}
		select {
		case replyCh <- reply:
		default:
		}
	}

	if err := m.client.Subscribe(cmd.ReplyTo, 1, handler); err != nil {
		return mu.ReplyEnvelope{}, err
	}
	defer m.client.Unsubscribe(cmd.ReplyTo)

	cmdPayload, err := json.Marshal(cmd)
	if err != nil {
		return mu.ReplyEnvelope{}, err
	}
	if err := m.client.Publish(mu.TopicCommands(m.config.TopicBase, targetID), 1, false, cmdPayload); err != nil {
		return mu.ReplyEnvelope{}, err
	}

	select {
	case reply := <-replyCh:
		return reply, nil
	case <-time.After(2 * time.Second):
		return mu.ReplyEnvelope{}, errors.New("timeout waiting for playlist server")
	}
}

func errorReply(cmd mu.CommandEnvelope, code string, message string) mu.ReplyEnvelope {
	return mu.ReplyEnvelope{
		ID:   cmd.ID,
		Type: "error",
		OK:   false,
		TS:   time.Now().Unix(),
		Err:  &mu.ReplyError{Code: code, Message: message},
	}
}

func (m *Module) resolveRef(owner string, refID string) ([]mu.ResolvedSource, error) {
	if strings.HasPrefix(refID, "lib:") {
		ref := strings.TrimPrefix(refID, "lib:")
		idx := strings.LastIndex(ref, ":")
		if idx <= 0 || idx >= len(ref)-1 {
			return nil, errors.New("invalid library ref (expected lib:<libraryNodeId>:<itemId>)")
		}
		libraryID := ref[:idx]
		itemID := ref[idx+1:]
		if !strings.HasPrefix(libraryID, "mu:") {
			return nil, errors.New("library ref must use full node id")
		}
		reply, err := m.publishCommand(libraryID, owner, "library.resolve", mu.LibraryResolveBody{ItemID: itemID})
		if err != nil {
			return nil, err
		}
		if reply.Err != nil {
			return nil, fmt.Errorf("%s", reply.Err.Message)
		}
		var payload mu.LibraryResolveReply
		if err := json.Unmarshal(reply.Body, &payload); err != nil {
			return nil, errors.New("invalid library reply")
		}
		if len(payload.Sources) == 0 {
			return nil, errors.New("library item has no sources")
		}
		return payload.Sources, nil
	}

	if strings.HasPrefix(refID, "mu:") {
		return nil, errors.New("renderer cannot resolve mu URNs; add with --resolve yes")
	}
	return nil, errors.New("unsupported ref; re-add with lib:<libraryNodeId>:<itemId> or --resolve yes")
}
