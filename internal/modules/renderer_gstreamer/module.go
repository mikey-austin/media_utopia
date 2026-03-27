package renderergstreamer

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
	Source       string
}

// cmdWork represents a command to be processed by a worker.
type cmdWork struct {
	cmd      mu.CommandEnvelope
	recvTime time.Time
}

// Module implements a GStreamer renderer.
type Module struct {
	log                 *zap.Logger
	client              mqttClient
	engine              *renderercore.Engine
	config              Config
	cmdTopic            string
	mu                  sync.RWMutex
	eosSeen             string
	publishTimeoutUntil int64

	// Async command processing
	cmdQueue chan cmdWork
	ctx      context.Context

	// Persistent reply handling
	replyMu       sync.RWMutex
	replyHandlers map[string]chan mu.ReplyEnvelope
	replyTopic    string

	// State publish debouncing
	stateDirty   int32         // atomic flag: 1 if state needs publishing
	debounceChan chan struct{} // signals state change for debouncer
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
	replyTopic := mu.TopicReply(cfg.TopicBase, cfg.NodeID)

	return &Module{
		log:           log,
		client:        client,
		engine:        engine,
		config:        cfg,
		cmdTopic:      cmdTopic,
		cmdQueue:      make(chan cmdWork, 64),
		replyHandlers: make(map[string]chan mu.ReplyEnvelope),
		replyTopic:    replyTopic,
		debounceChan:  make(chan struct{}, 1),
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
		if err := m.publishStatePayload(payload); err != nil {
			m.log.Warn("failed to publish initial state", zap.Error(err))
		}
	}

	// Subscribe to persistent reply topic for inter-module communication
	replyHandler := func(_ paho.Client, msg paho.Message) {
		m.handleReply(msg)
	}
	if err := m.client.Subscribe(m.replyTopic, 1, replyHandler); err != nil {
		return err
	}
	defer m.client.Unsubscribe(m.replyTopic)

	// Start command worker pool (4 workers for parallelism)
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
	go m.runStateDebouncer(ctx)

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
// This returns immediately to avoid blocking the MQTT client thread.
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
			m.processCommand(work.cmd, work.recvTime)
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
			// Handler already received a reply
		}
	}
}

// processCommand handles a single command with timing metrics.
func (m *Module) processCommand(cmd mu.CommandEnvelope, recvTime time.Time) {
	m.log.Debug("command received",
		zap.String("id", cmd.ID),
		zap.String("type", cmd.Type),
		zap.String("from", cmd.From),
		zap.String("replyTo", cmd.ReplyTo))

	// Load commands may involve network I/O, handle separately
	if cmd.Type == "queue.loadPlaylist" || cmd.Type == "queue.loadSnapshot" {
		m.processLoadCommand(cmd, recvTime)
		return
	}

	// Use read lock for commands that only query state without mutation.
	readOnly := cmd.Type == "queue.get"

	// Track mutex acquisition time for contention diagnostics
	lockStart := time.Now()
	if readOnly {
		m.mu.RLock()
	} else {
		m.mu.Lock()
	}
	lockWait := time.Since(lockStart)

	dispatchStart := time.Now()
	reply := m.dispatch(cmd)
	dispatchDuration := time.Since(dispatchStart)
	if readOnly {
		m.mu.RUnlock()
	} else {
		m.mu.Unlock()
	}

	totalDuration := time.Since(recvTime)

	m.log.Debug("command completed",
		zap.String("id", cmd.ID),
		zap.String("type", cmd.Type),
		zap.Duration("lock_wait", lockWait),
		zap.Duration("dispatch", dispatchDuration),
		zap.Duration("total", totalDuration),
		zap.Bool("ok", reply.OK))

	if totalDuration > 100*time.Millisecond {
		m.log.Warn("slow command",
			zap.String("id", cmd.ID),
			zap.String("type", cmd.Type),
			zap.Duration("lock_wait", lockWait),
			zap.Duration("dispatch", dispatchDuration),
			zap.Duration("total", totalDuration))
	}

	m.publishReply(cmd.ReplyTo, reply)
}

// processLoadCommand handles load commands which may involve network I/O.
// Network operations (fetchPlaylistEntries, fetchSnapshotItems, resolveRefs)
// happen outside the lock to avoid blocking command processing and position
// updates. Only the engine state mutation is performed under the lock.
func (m *Module) processLoadCommand(cmd mu.CommandEnvelope, recvTime time.Time) {
	m.log.Debug("load command started",
		zap.String("id", cmd.ID),
		zap.String("type", cmd.Type),
		zap.String("from", cmd.From))

	// Phase 1: Validation and network I/O happen outside the lock.
	// LeaseManager has its own internal mutex, so Require is safe here.
	var reply mu.ReplyEnvelope
	switch cmd.Type {
	case "queue.loadPlaylist":
		reply = m.handleQueueLoadPlaylist(cmd)
	case "queue.loadSnapshot":
		reply = m.handleQueueLoadSnapshot(cmd)
	default:
		reply = errorReply(cmd, "INVALID", "unsupported load command")
	}

	totalDuration := time.Since(recvTime)
	m.log.Debug("load command completed",
		zap.String("id", cmd.ID),
		zap.String("type", cmd.Type),
		zap.Duration("total", totalDuration),
		zap.Bool("ok", reply.OK))

	if totalDuration > 500*time.Millisecond {
		m.log.Warn("slow load command",
			zap.String("id", cmd.ID),
			zap.String("type", cmd.Type),
			zap.Duration("total", totalDuration))
	}

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
	if m.config.PublishState {
		m.scheduleStatePublish()
	}
}

// scheduleStatePublish signals the debouncer that state needs publishing.
func (m *Module) scheduleStatePublish() {
	atomic.StoreInt32(&m.stateDirty, 1)
	select {
	case m.debounceChan <- struct{}{}:
	default:
		// Debouncer already notified
	}
}

// runStateDebouncer coalesces rapid state changes into batched publishes.
func (m *Module) runStateDebouncer(ctx context.Context) {
	const debounceInterval = 50 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.debounceChan:
			// Wait for debounce interval to coalesce rapid changes
			timer := time.NewTimer(debounceInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}

			// Check if state is still dirty and publish
			if atomic.CompareAndSwapInt32(&m.stateDirty, 1, 0) {
				if !m.shouldShedState() {
					payload, err := m.buildStatePayload()
					if err != nil {
						m.log.Debug("failed to build state payload", zap.Error(err))
						continue
					}
					if err := m.publishStatePayload(payload); err != nil {
						m.log.Debug("failed to publish state", zap.Error(err))
					}
				}
			}
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
	// Query driver position outside the lock — the driver has its own
	// synchronisation and this avoids holding our mutex during the
	// (potentially slow) GStreamer query.
	posMS, durMS, ok := m.engine.Driver.Position()

	// Single lock acquisition: check state and update in one section.
	m.mu.Lock()
	playing := m.engine.State.Playback != nil && m.engine.State.Playback.Status == "playing"
	if !playing {
		m.eosSeen = ""
		m.mu.Unlock()
		return
	}
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
	m.mu.Unlock()

	if m.config.PublishState {
		m.scheduleStatePublish()
	}
}

func (m *Module) buildStatePayload() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
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
	return m.engine.HandleCommand(cmd)
}

func (m *Module) handleQueueLoadPlaylist(cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	// --- Phase 1: Validation and network I/O (no lock held) ---
	var body mu.QueueLoadPlaylistBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if cmd.Lease == nil {
		return errorReply(cmd, "LEASE_REQUIRED", "lease required")
	}
	// LeaseManager has its own mutex; safe to call without m.mu.
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

	// Network I/O: fetch playlist entries and resolve refs via MQTT.
	entries, err := m.fetchPlaylistEntries(cmd.From, body.PlaylistServerID, body.PlaylistID, body.Resolve)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}

	// Build the synthesized queue command from fetched data.
	var queueCmd mu.CommandEnvelope
	switch mode {
	case "replace":
		payload, _ := json.Marshal(mu.QueueSetBody{StartIndex: 0, Entries: entries})
		queueCmd = mu.CommandEnvelope{
			ID:    cmd.ID,
			Type:  "queue.set",
			TS:    time.Now().Unix(),
			From:  cmd.From,
			Lease: cmd.Lease,
			Body:  payload,
		}
	case "append":
		payload, _ := json.Marshal(mu.QueueAddBody{Position: "end", Entries: entries})
		queueCmd = mu.CommandEnvelope{
			ID:    cmd.ID,
			Type:  "queue.add",
			TS:    time.Now().Unix(),
			From:  cmd.From,
			Lease: cmd.Lease,
			Body:  payload,
		}
	case "next":
		payload, _ := json.Marshal(mu.QueueAddBody{Position: "next", Entries: entries})
		queueCmd = mu.CommandEnvelope{
			ID:    cmd.ID,
			Type:  "queue.add",
			TS:    time.Now().Unix(),
			From:  cmd.From,
			Lease: cmd.Lease,
			Body:  payload,
		}
	default:
		return errorReply(cmd, "INVALID", "mode must be replace|append|next")
	}

	// --- Phase 2: State mutation under lock ---
	m.mu.Lock()
	reply := m.engine.HandleCommand(queueCmd)
	m.mu.Unlock()
	return reply
}

func (m *Module) handleQueueLoadSnapshot(cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	// --- Phase 1: Validation and network I/O (no lock held) ---
	var body mu.QueueLoadSnapshotBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if cmd.Lease == nil {
		return errorReply(cmd, "LEASE_REQUIRED", "lease required")
	}
	// LeaseManager has its own mutex; safe to call without m.mu.
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

	// Network I/O: fetch snapshot items and resolve refs via MQTT.
	items, capture, err := m.fetchSnapshotItems(cmd.From, body.PlaylistServerID, body.SnapshotID)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	entries, err := m.buildSnapshotEntries(cmd.From, items, body.Resolve)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}

	// Build the synthesized queue command from fetched data.
	var queueCmd mu.CommandEnvelope
	switch mode {
	case "replace":
		payload, _ := json.Marshal(mu.QueueSetBody{StartIndex: capture.Index, Entries: entries})
		queueCmd = mu.CommandEnvelope{
			ID:    cmd.ID,
			Type:  "queue.set",
			TS:    time.Now().Unix(),
			From:  cmd.From,
			Lease: cmd.Lease,
			Body:  payload,
		}
	case "append":
		payload, _ := json.Marshal(mu.QueueAddBody{Position: "end", Entries: entries})
		queueCmd = mu.CommandEnvelope{
			ID:    cmd.ID,
			Type:  "queue.add",
			TS:    time.Now().Unix(),
			From:  cmd.From,
			Lease: cmd.Lease,
			Body:  payload,
		}
	case "next":
		payload, _ := json.Marshal(mu.QueueAddBody{Position: "next", Entries: entries})
		queueCmd = mu.CommandEnvelope{
			ID:    cmd.ID,
			Type:  "queue.add",
			TS:    time.Now().Unix(),
			From:  cmd.From,
			Lease: cmd.Lease,
			Body:  payload,
		}
	default:
		return errorReply(cmd, "INVALID", "mode must be replace|append|next")
	}

	// --- Phase 2: State mutation under lock ---
	m.mu.Lock()
	reply := m.engine.HandleCommand(queueCmd)
	if reply.OK {
		m.engine.Queue.SetRepeat(capture.Repeat)
		if capture.RepeatMode != "" {
			m.engine.Queue.SetRepeatMode(capture.RepeatMode)
		}
		m.engine.State.Playback.PositionMS = capture.PositionMS
		m.engine.State.TS = time.Now().Unix()
	}
	m.mu.Unlock()
	return reply
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
	refs := make([]string, 0, len(payload.Entries))
	for _, entry := range payload.Entries {
		if entry.Resolved == nil && entry.Ref != nil {
			if !needsResolve {
				return nil, errors.New("playlist contains unresolved refs; add with --resolve yes")
			}
			refs = append(refs, entry.Ref.ID)
			continue
		}
		if entry.Ref == nil && entry.Resolved == nil {
			continue
		}
		entries = append(entries, mu.QueueEntry{Ref: entry.Ref, Resolved: entry.Resolved})
	}
	if len(refs) > 0 {
		resolved, err := m.resolveRefs(owner, refs)
		if err != nil {
			return nil, err
		}
		for _, refID := range refs {
			sources := resolved[refID]
			if len(sources) == 0 {
				return nil, errors.New("library item has no sources")
			}
			ref := &mu.ItemRef{ID: refID}
			for _, source := range sources {
				entries = append(entries, mu.QueueEntry{Ref: ref, Resolved: &source})
			}
		}
	}
	return entries, nil
}

func (m *Module) fetchSnapshotItems(owner string, serverID string, snapshotID string) ([]string, mu.SnapshotCapture, error) {
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
	return payload.Items, payload.Capture, nil
}

func (m *Module) buildSnapshotEntries(owner string, items []string, resolve string) ([]mu.QueueEntry, error) {
	needsResolve := resolve != "no"
	entries := make([]mu.QueueEntry, 0, len(items))
	refs := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.HasPrefix(item, "http://") || strings.HasPrefix(item, "https://") {
			entries = append(entries, mu.QueueEntry{Resolved: &mu.ResolvedSource{URL: item, ByteRange: false}})
			continue
		}
		if strings.HasPrefix(item, "lib:") {
			if !needsResolve {
				return nil, errors.New("snapshot contains unresolved refs; load with --resolve yes")
			}
			refs = append(refs, item)
			continue
		}
		if strings.HasPrefix(item, "mu:") {
			return nil, errors.New("snapshot contains mu URN; load with --resolve yes")
		}
		return nil, errors.New("unsupported snapshot item")
	}
	if len(refs) > 0 {
		resolved, err := m.resolveRefs(owner, refs)
		if err != nil {
			return nil, err
		}
		for _, refID := range refs {
			sources := resolved[refID]
			if len(sources) == 0 {
				return nil, errors.New("library item has no sources")
			}
			ref := &mu.ItemRef{ID: refID}
			for _, source := range sources {
				entries = append(entries, mu.QueueEntry{Ref: ref, Resolved: &source})
			}
		}
	}
	return entries, nil
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
		ReplyTo: m.replyTopic, // Use persistent reply topic
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

	cmdPayload, err := json.Marshal(cmd)
	if err != nil {
		return mu.ReplyEnvelope{}, err
	}
	if err := m.client.Publish(mu.TopicCommands(m.config.TopicBase, targetID), 1, false, cmdPayload); err != nil {
		return mu.ReplyEnvelope{}, err
	}

	// Wait for reply with context cancellation support
	select {
	case reply := <-replyCh:
		return reply, nil
	case <-m.ctx.Done():
		return mu.ReplyEnvelope{}, m.ctx.Err()
	case <-time.After(commandTimeout):
		return mu.ReplyEnvelope{}, errors.New("timeout waiting for reply")
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

func (m *Module) resolveRefs(owner string, refIDs []string) (map[string][]mu.ResolvedSource, error) {
	libraryItems := make(map[string]map[string]string)
	for _, refID := range refIDs {
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
		if libraryItems[libraryID] == nil {
			libraryItems[libraryID] = make(map[string]string)
		}
		libraryItems[libraryID][itemID] = refID
	}

	resolved := make(map[string][]mu.ResolvedSource)
	for libraryID, itemMap := range libraryItems {
		itemIDs := make([]string, 0, len(itemMap))
		for itemID := range itemMap {
			itemIDs = append(itemIDs, itemID)
		}
		reply, err := m.publishCommand(libraryID, owner, "library.resolveBatch", mu.LibraryResolveBatchBody{ItemIDs: itemIDs})
		if err != nil {
			return nil, err
		}
		if reply.Err != nil {
			if reply.Err.Code == "INVALID" && strings.Contains(strings.ToLower(reply.Err.Message), "unsupported command") {
				for _, refID := range itemMap {
					sources, err := m.resolveRef(owner, refID)
					if err != nil {
						return nil, err
					}
					resolved[refID] = sources
				}
				continue
			}
			return nil, fmt.Errorf("%s", reply.Err.Message)
		}
		var payload mu.LibraryResolveBatchReply
		if err := json.Unmarshal(reply.Body, &payload); err != nil {
			return nil, errors.New("invalid library reply")
		}
		for _, item := range payload.Items {
			if item.Err != nil {
				return nil, fmt.Errorf("%s", item.Err.Message)
			}
			refID := itemMap[item.ItemID]
			resolved[refID] = item.Sources
		}
	}
	return resolved, nil
}
