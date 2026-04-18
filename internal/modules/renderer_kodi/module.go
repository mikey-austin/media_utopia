package rendererkodi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/mikey-austin/media_utopia/internal/adapters/idgen"
	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	renderercore "github.com/mikey-austin/media_utopia/internal/modules/renderer_core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

type mqttClient interface {
	Publish(topic string, qos byte, retained bool, payload []byte) error
	Subscribe(topic string, qos byte, handler paho.MessageHandler) error
	Unsubscribe(topic string) error
}

// Config configures the Kodi renderer module.
type Config struct {
	NodeID       string
	TopicBase    string
	Name         string
	BaseURL      string
	Username     string
	Password     string
	Timeout      time.Duration
	Volume       float64
	PublishState bool
	Source       string
}

// cmdWork represents a command to be processed by a worker.
type cmdWork struct {
	cmd      mu.CommandEnvelope
	recvTime time.Time
}

// Module implements a Kodi renderer.
type Module struct {
	log                 *zap.Logger
	client              mqttClient
	engine              *renderercore.Engine
	config              Config
	cmdTopic            string
	mu                  sync.Mutex
	eosSeen             string
	publishTimeoutUntil int64

	// Async command processing
	cmdQueue chan cmdWork
	ctx      context.Context

	// Command deduplication
	dedup *mu.CommandDedup

	// Persistent reply handling
	replyMu       sync.RWMutex
	replyHandlers map[string]chan mu.ReplyEnvelope
	replyTopic    string
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
		cfg.Name = "Kodi Renderer"
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("base_url required")
	}

	driver, err := NewDriver(cfg.BaseURL, cfg.Username, cfg.Password, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	engine := renderercore.NewEngine(cfg.NodeID, cfg.Name, driver)
	if cfg.Volume > 0 {
		engine.State.Playback.Volume = cfg.Volume
	}

	cmdTopic := mu.TopicCommands(cfg.TopicBase, cfg.NodeID)
	replyTopic := mu.TopicReply(cfg.TopicBase, cfg.NodeID)

	return &Module{
		log:           log,
		client:        client,
		engine:        engine,
		config:        cfg,
		cmdTopic:      cmdTopic,
		cmdQueue:      make(chan cmdWork, 64),
		dedup:         mu.NewCommandDedup(128),
		replyHandlers: make(map[string]chan mu.ReplyEnvelope),
		replyTopic:    replyTopic,
	}, nil
}

// Run starts the renderer module.
func (m *Module) Run(ctx context.Context) error {
	m.ctx = ctx

	if err := m.publishPresence(); err != nil {
		return err
	}
	if m.config.PublishState {
		payload, err := m.buildStatePayload()
		if err != nil {
			return err
		}
		_ = m.publishStatePayload(payload)
	}

	// Subscribe to persistent reply topic for inter-module communication
	replyHandler := func(_ paho.Client, msg paho.Message) {
		m.handleReply(msg)
	}
	if err := m.client.Subscribe(m.replyTopic, 1, replyHandler); err != nil {
		return err
	}
	defer m.client.Unsubscribe(m.replyTopic)

	// Start command worker pool
	const numWorkers = 4
	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			m.commandWorker(ctx)
		}()
	}

	go m.runPositionUpdates(ctx)

	cmdHandler := func(_ paho.Client, msg paho.Message) {
		m.handleMessage(msg)
	}
	if err := m.client.Subscribe(m.cmdTopic, 1, cmdHandler); err != nil {
		return err
	}
	defer m.client.Unsubscribe(m.cmdTopic)

	<-ctx.Done()
	wg.Wait()
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
		Source: m.config.Source,
		TS:     time.Now().Unix(),
	}
	payload, err := json.Marshal(presence)
	if err != nil {
		return err
	}
	return m.client.Publish(mu.TopicPresence(m.config.TopicBase, m.config.NodeID), 1, true, payload)
}

func (m *Module) publishState() error {
	payload, err := m.buildStatePayload()
	if err != nil {
		return err
	}
	return m.publishStatePayload(payload)
}

// handleMessage receives MQTT messages and queues them for async processing.
func (m *Module) handleMessage(msg paho.Message) {
	var cmd mu.CommandEnvelope
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		m.log.Warn("invalid command", zap.Error(err))
		return
	}

	work := cmdWork{cmd: cmd, recvTime: time.Now()}

	select {
	case m.cmdQueue <- work:
		// Queued successfully
	default:
		// Queue full - apply backpressure
		m.log.Warn("command queue full",
			zap.String("id", cmd.ID),
			zap.String("type", cmd.Type))
		if cmd.ReplyTo != "" {
			reply := errorReply(cmd, "OVERLOADED", "command queue full")
			m.publishReply(cmd.ReplyTo, reply)
		}
	}
}

// commandWorker processes commands from the queue.
func (m *Module) commandWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case work := <-m.cmdQueue:
			m.processCommand(work.cmd)
		}
	}
}

// handleReply processes replies on the persistent reply topic.
func (m *Module) handleReply(msg paho.Message) {
	var reply mu.ReplyEnvelope
	if err := json.Unmarshal(msg.Payload(), &reply); err != nil {
		return
	}

	m.replyMu.RLock()
	ch, ok := m.replyHandlers[reply.ID]
	m.replyMu.RUnlock()

	if ok {
		select {
		case ch <- reply:
		default:
		}
	}
}

// processCommand handles a single command.
func (m *Module) processCommand(cmd mu.CommandEnvelope) {
	if mu.ShouldDedup(cmd.Type) && m.dedup.Seen(cmd.ID) {
		m.log.Debug("duplicate command skipped", zap.String("id", cmd.ID), zap.String("type", cmd.Type))
		reply := mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix()}
		payload, _ := json.Marshal(reply)
		_ = m.client.Publish(cmd.ReplyTo, 0, false, payload)
		return
	}
	// Session commands use Engine's internal lock, not the module lock.
	if renderercore.IsSessionCommand(cmd.Type) {
		reply := m.engine.HandleSessionCommand(cmd)
		m.publishReply(cmd.ReplyTo, reply)
		return
	}
	m.mu.Lock()
	reply := m.dispatch(cmd)
	m.mu.Unlock()
	m.publishReply(cmd.ReplyTo, reply)
}

func (m *Module) publishReply(replyTo string, reply mu.ReplyEnvelope) {
	if replyTo != "" {
		payload, err := json.Marshal(reply)
		if err == nil {
			if err := m.client.Publish(replyTo, 1, false, payload); err != nil {
				m.markPublishTimeout(err)
			}
		}
	}
	if m.config.PublishState && !m.shouldShedState() {
		payload, err := m.buildStatePayload()
		if err == nil {
			_ = m.publishStatePayload(payload)
		}
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

	if m.engine.State.Playback == nil || m.engine.State.Playback.Status != "playing" {
		m.eosSeen = ""
		m.mu.Unlock()
		return
	}
	posMS, durMS, ok := m.engine.Driver.Position()
	if !ok {
		m.mu.Unlock()
		return
	}
	m.engine.State.Playback.PositionMS = posMS
	if durMS > 0 {
		m.engine.State.Playback.DurationMS = durMS
	}
	m.engine.State.TS = time.Now().Unix()
	m.advanceOnEndLocked(posMS, durMS)
	payload, err := m.buildStatePayloadLocked()
	m.mu.Unlock()
	if m.config.PublishState {
		if err == nil && !m.shouldShedState() {
			_ = m.publishStatePayload(payload)
		}
	}
}

func (m *Module) buildStatePayload() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buildStatePayloadLocked()
}

func (m *Module) buildStatePayloadLocked() ([]byte, error) {
	return json.Marshal(m.engine.State)
}

func (m *Module) publishStatePayload(payload []byte) error {
	if payload == nil {
		return nil
	}
	if m.shouldShedState() {
		return nil
	}
	err := m.client.Publish(mu.TopicState(m.config.TopicBase, m.config.NodeID), 1, true, payload)
	m.markPublishTimeout(err)
	return err
}

func (m *Module) shouldShedState() bool {
	until := atomic.LoadInt64(&m.publishTimeoutUntil)
	if until == 0 {
		return false
	}
	return time.Now().UnixNano() < until
}

func (m *Module) markPublishTimeout(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, mqttserver.ErrPublishTimeout) {
		atomic.StoreInt64(&m.publishTimeoutUntil, time.Now().Add(2*time.Second).UnixNano())
	}
}

func (m *Module) advanceOnEndLocked(positionMS int64, durationMS int64) {
	if durationMS <= 0 {
		return
	}
	if positionMS < durationMS-250 {
		m.eosSeen = ""
		return
	}
	currentID := ""
	if m.engine.State.Current != nil {
		currentID = m.engine.State.Current.QueueEntryID
	}
	if currentID == "" {
		return
	}
	if m.eosSeen == currentID {
		return
	}
	m.eosSeen = currentID
	m.engine.AdvanceAfterEnd()
}

func (m *Module) dispatch(cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	reply := mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix()}

	switch cmd.Type {
	case "queue.loadPlaylist":
		return m.handleQueueLoadPlaylist(cmd, reply)
	case "queue.loadSnapshot":
		return m.handleQueueLoadSnapshot(cmd, reply)
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

func (m *Module) handleQueueLoadSnapshot(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.QueueLoadSnapshotBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if cmd.Lease == nil {
		return errorReply(cmd, "LEASE_REQUIRED", "lease required")
	}
	if err := m.engine.Leases.Require(cmd.Lease.SessionID, cmd.Lease.Token); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	if strings.TrimSpace(body.PlaylistServerID) == "" || strings.TrimSpace(body.SnapshotID) == "" {
		return errorReply(cmd, "INVALID", "playlistServerId and snapshotId required")
	}
	mode := body.Mode
	if mode == "" {
		mode = "replace"
	}

	rawEntries, capture, err := m.fetchSnapshotEntries(cmd.From, body.PlaylistServerID, body.SnapshotID)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	entries, err := m.buildSnapshotEntries(cmd.From, rawEntries, body.Resolve)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}

	switch mode {
	case "replace":
		payload, _ := json.Marshal(mu.QueueSetBody{StartIndex: capture.Index, Entries: entries})
		queueCmd := mu.CommandEnvelope{
			ID:    cmd.ID,
			Type:  "queue.set",
			TS:    time.Now().Unix(),
			From:  cmd.From,
			Lease: cmd.Lease,
			Body:  payload,
		}
		reply = m.engine.HandleCommand(queueCmd)
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
		reply = m.engine.HandleCommand(queueCmd)
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
		reply = m.engine.HandleCommand(queueCmd)
	default:
		return errorReply(cmd, "INVALID", "mode must be replace|append|next")
	}

	m.engine.Queue.SetRepeat(capture.Repeat)
	if capture.RepeatMode != "" {
		m.engine.Queue.SetRepeatMode(capture.RepeatMode)
	}
	m.engine.State.Playback.PositionMS = capture.PositionMS
	m.engine.State.TS = time.Now().Unix()
	return reply
}

type playlistReply struct {
	Entries []mu.QueueEntry `json:"entries"`
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
	return m.materializeEntries(owner, payload.Entries, resolve)
}

func (m *Module) fetchSnapshotEntries(owner string, serverID string, snapshotID string) ([]mu.QueueEntry, mu.SnapshotCapture, error) {
	reply, err := m.publishCommand(serverID, owner, "snapshot.get", mu.SnapshotGetBody{SnapshotID: snapshotID})
	if err != nil {
		return nil, mu.SnapshotCapture{}, err
	}
	if reply.Err != nil {
		return nil, mu.SnapshotCapture{}, fmt.Errorf("%s", reply.Err.Message)
	}
	var payload mu.SnapshotGetReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		return nil, mu.SnapshotCapture{}, errors.New("invalid snapshot reply")
	}
	return payload.Entries, payload.Capture, nil
}

func (m *Module) buildSnapshotEntries(owner string, entries []mu.QueueEntry, resolve string) ([]mu.QueueEntry, error) {
	return m.materializeEntries(owner, entries, resolve)
}

// materializeEntries returns a copy of entries with Resolved sources backfilled
// from libraries where missing. resolve == "no" disables backfill — entries
// without resolved sources cause an error.
func (m *Module) materializeEntries(owner string, entries []mu.QueueEntry, resolve string) ([]mu.QueueEntry, error) {
	needsResolve := resolve != "no"
	out := make([]mu.QueueEntry, 0, len(entries))
	var pending []mu.LibraryItemRef
	pendingIdx := make(map[string]int)
	for _, entry := range entries {
		if entry.Ref == nil && entry.Resolved == nil {
			continue
		}
		if entry.Resolved != nil {
			out = append(out, entry)
			continue
		}
		if !needsResolve {
			return nil, errors.New("entry contains unresolved ref; load with --resolve yes")
		}
		pendingIdx[entry.Ref.LibraryID+"\x00"+entry.Ref.ItemID] = len(out)
		pending = append(pending, *entry.Ref)
		out = append(out, entry)
	}
	if len(pending) == 0 {
		return out, nil
	}
	resolved, err := m.resolveLibraryRefs(owner, pending)
	if err != nil {
		return nil, err
	}
	for k, idx := range pendingIdx {
		sources := resolved[k]
		if len(sources) == 0 {
			return nil, errors.New("library item has no sources")
		}
		out[idx].Resolved = &sources[0]
	}
	return out, nil
}

// publishCommand sends a command to another module and waits for a reply.
// Uses the persistent reply topic to avoid Subscribe/Unsubscribe overhead.
func (m *Module) publishCommand(targetID string, owner string, cmdType string, body any) (mu.ReplyEnvelope, error) {
	const commandTimeout = 5 * time.Second

	payload, err := json.Marshal(body)
	if err != nil {
		return mu.ReplyEnvelope{}, err
	}

	cmdID := idgen.Generator{}.NewID()
	cmd := mu.CommandEnvelope{
		ID:      cmdID,
		Type:    cmdType,
		TS:      time.Now().Unix(),
		From:    owner,
		ReplyTo: m.replyTopic,
		Body:    payload,
	}

	// Register reply handler before sending command
	replyCh := make(chan mu.ReplyEnvelope, 1)
	m.replyMu.Lock()
	m.replyHandlers[cmdID] = replyCh
	m.replyMu.Unlock()

	defer func() {
		m.replyMu.Lock()
		delete(m.replyHandlers, cmdID)
		m.replyMu.Unlock()
	}()

	if err := m.client.Publish(mu.TopicCommands(m.config.TopicBase, targetID), 1, false, mustJSON(cmd)); err != nil {
		return mu.ReplyEnvelope{}, err
	}

	select {
	case reply := <-replyCh:
		return reply, nil
	case <-m.ctx.Done():
		return mu.ReplyEnvelope{}, m.ctx.Err()
	case <-time.After(commandTimeout):
		return mu.ReplyEnvelope{}, errors.New("timeout waiting for reply")
	}
}

// resolveLibraryRefs returns sources for each ref keyed by "<libraryId>\x00<itemId>".
// It batches one request per library node.
func (m *Module) resolveLibraryRefs(owner string, refs []mu.LibraryItemRef) (map[string][]mu.ResolvedSource, error) {
	byLibrary := make(map[string][]mu.LibraryItemRef)
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return nil, err
		}
		byLibrary[ref.LibraryID] = append(byLibrary[ref.LibraryID], ref)
	}

	out := make(map[string][]mu.ResolvedSource)
	for libraryID, libRefs := range byLibrary {
		reply, err := m.publishCommand(libraryID, owner, "library.resolveSourcesBatch",
			mu.LibraryResolveSourcesBatchBody{Refs: libRefs})
		if err != nil {
			return nil, err
		}
		if reply.Err != nil {
			return nil, fmt.Errorf("%s", reply.Err.Message)
		}
		var payload mu.LibraryResolveSourcesBatchReply
		if err := json.Unmarshal(reply.Body, &payload); err != nil {
			return nil, errors.New("invalid library reply")
		}
		for _, item := range payload.Items {
			if item.Err != nil {
				return nil, fmt.Errorf("%s", item.Err.Message)
			}
			out[item.Ref.LibraryID+"\x00"+item.Ref.ItemID] = item.Sources
		}
	}
	return out, nil
}

func mustJSON(v any) []byte {
	payload, _ := json.Marshal(v)
	return payload
}

func errorReply(cmd mu.CommandEnvelope, code string, message string) mu.ReplyEnvelope {
	return mu.ReplyEnvelope{
		ID:   cmd.ID,
		Type: "error",
		OK:   false,
		TS:   time.Now().Unix(),
		Err: &mu.ReplyError{
			Code:    code,
			Message: message,
		},
	}
}
