package renderermpv

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

// Config configures the mpv renderer module.
type Config struct {
	NodeID            string
	TopicBase         string
	Name              string
	AO                string
	Device            string
	Crossfade         time.Duration
	Volume            float64
	PublishState      bool
	Source            string
	MPVOptions        map[string]string
	StatePublisher    renderercore.StatePublisher
	PresencePublisher renderercore.PresencePublisher
}

// cmdWork represents a command to be processed by a worker.
type cmdWork struct {
	cmd      mu.CommandEnvelope
	recvTime time.Time
}

// Module implements an mpv (libmpv) renderer.
type Module struct {
	log                 *zap.Logger
	client              mqttClient
	engine              *renderercore.Engine
	config              Config
	cmdTopic            string
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

	dedup *mu.CommandDedup
}

// driverEventSource is implemented by drivers that expose async events
// (EOS / errors / pipewire health). The Module wires this up if present so
// EOS no longer needs to be detected by polling position vs duration.
type driverEventSource interface {
	Events() <-chan Event
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
		cfg.Name = "MPV Renderer"
	}
	if cfg.StatePublisher == nil {
		return nil, errors.New("state_publisher required")
	}
	if cfg.PresencePublisher == nil {
		return nil, errors.New("presence_publisher required")
	}

	driver, err := NewDriver(cfg.AO, cfg.Device, cfg.Crossfade, cfg.MPVOptions)
	if err != nil {
		return nil, err
	}
	engine := renderercore.NewEngine(cfg.NodeID, cfg.Name, driver)
	if cfg.Volume > 0 {
		engine.SetInitialVolume(cfg.Volume)
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
		dedup:         mu.NewCommandDedup(128),
	}, nil
}

// Run starts the renderer module.
func (m *Module) Run(ctx context.Context) error {
	m.ctx = ctx
	defer func() {
		// Prefer Close() when the driver supports it. Close quits every
		// mpv handle and waits for the bounded terminate_destroy, so
		// pipewire observes clean client disconnects before the process
		// exits. Without that, pipewire only sees a socket EOF and leaves
		// stale node / buffer state in the daemon — known to accumulate
		// across nightly container restarts and eventually break audio.
		if closer, ok := m.engine.Driver.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				m.log.Warn("failed to close renderer driver", zap.Error(err))
			}
			return
		}
		if err := m.engine.Driver.Stop(); err != nil {
			m.log.Warn("failed to stop renderer driver", zap.Error(err))
		}
	}()

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
	// Drive EOS / error / warning / pipewire-down handling from the
	// driver's bus events when supported. This is what replaces the old
	// `advanceOnEndLocked` position-poll heuristic and its
	// minPlaybackBeforeEOS workaround.
	if src, ok := m.engine.Driver.(driverEventSource); ok {
		go m.consumeDriverEvents(ctx, src.Events())
	}

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
	presence := &mu.Presence{
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
	return m.config.PresencePublisher.PublishPresence(presence)
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
	if mu.ShouldDedup(cmd.Type) && m.dedup.Seen(cmd.ID) {
		m.log.Debug("duplicate command skipped", zap.String("id", cmd.ID), zap.String("type", cmd.Type))
		reply := mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix()}
		payload, _ := json.Marshal(reply)
		_ = m.client.Publish(cmd.ReplyTo, 0, false, payload)
		return
	}

	m.log.Debug("command received",
		zap.String("id", cmd.ID),
		zap.String("type", cmd.Type),
		zap.String("from", cmd.From),
		zap.String("replyTo", cmd.ReplyTo))

	// Session commands use the Engine's internal sessionMu instead of the
	// module lock, so lease renewals are never blocked behind slow driver
	// operations (GStreamer pipeline teardown, seek, etc.).
	if renderercore.IsSessionCommand(cmd.Type) {
		dispatchStart := time.Now()
		reply := m.engine.HandleSessionCommand(cmd)
		dispatchDuration := time.Since(dispatchStart)
		totalDuration := time.Since(recvTime)
		m.log.Debug("session command completed",
			zap.String("id", cmd.ID),
			zap.String("type", cmd.Type),
			zap.Duration("dispatch", dispatchDuration),
			zap.Duration("total", totalDuration),
			zap.Bool("ok", reply.OK))
		m.publishReply(cmd.ReplyTo, reply)
		return
	}

	// Load commands may involve network I/O, handle separately
	if cmd.Type == "queue.loadPlaylist" || cmd.Type == "queue.loadSnapshot" {
		m.processLoadCommand(cmd, recvTime)
		return
	}

	// The engine manages its own state locking (stateMu) and releases it
	// around driver calls — we no longer wrap dispatch in a module-level
	// lock. Workers therefore parallelise: state mutations serialise on
	// stateMu (briefly), driver operations don't.
	dispatchStart := time.Now()
	reply := m.dispatch(cmd)
	dispatchDuration := time.Since(dispatchStart)
	totalDuration := time.Since(recvTime)

	m.log.Debug("command completed",
		zap.String("id", cmd.ID),
		zap.String("type", cmd.Type),
		zap.Duration("dispatch", dispatchDuration),
		zap.Duration("total", totalDuration),
		zap.Bool("ok", reply.OK))

	if totalDuration > 100*time.Millisecond {
		m.log.Warn("slow command",
			zap.String("id", cmd.ID),
			zap.String("type", cmd.Type),
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
	posMS, durMS, ok := m.engine.Driver.Position()
	if !ok {
		return
	}
	if !m.engine.UpdatePosition(posMS, durMS) {
		return
	}
	if m.config.PublishState {
		m.scheduleStatePublish()
	}
}

func (m *Module) buildStatePayload() ([]byte, error) {
	return m.engine.SnapshotState()
}

func (m *Module) publishStatePayload(payload []byte) error {
	if payload == nil {
		return nil
	}
	if m.shouldShedState() {
		return nil
	}
	var state mu.RendererState
	if err := json.Unmarshal(payload, &state); err != nil {
		return err
	}
	err := m.config.StatePublisher.PublishState(&state)
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

// consumeDriverEvents bridges async driver events into engine state changes.
// EOS triggers the queue advance directly (instead of the old position-poll
// heuristic), errors and warnings get logged, pipewire-down events surface
// in the renderer state.
func (m *Module) consumeDriverEvents(ctx context.Context, events <-chan Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return // driver closed
			}
			switch ev.Kind {
			case EventEOS:
				m.log.Debug("mpv EOS — advancing queue")
				m.engine.AdvanceAfterEnd()
				if m.config.PublishState {
					m.scheduleStatePublish()
				}
			case EventError:
				m.log.Warn("mpv playback error", zap.String("message", ev.Message))
				// Treat unrecoverable playback errors like EOS for now —
				// advance to the next track rather than wedging on the
				// broken one.
				m.engine.AdvanceAfterEnd()
				if m.config.PublishState {
					m.scheduleStatePublish()
				}
			case EventWarning:
				// mpv log messages at warn level — stream quirks, AO
				// fallbacks, demuxer complaints. Logged loudly so stream
				// compatibility issues are visible.
				m.log.Warn("mpv warning", zap.String("message", ev.Message))
			case EventAudioDown:
				m.log.Error("audio backend unreachable — playback will fail until it returns",
					zap.String("message", ev.Message))
				if m.config.PublishState {
					m.scheduleStatePublish()
				}
			}
		}
	}
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

	// --- Phase 2: State mutation (engine manages its own locking) ---
	return m.engine.HandleCommand(queueCmd)
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

	// Network I/O: fetch snapshot entries and resolve refs via MQTT.
	rawEntries, capture, err := m.fetchSnapshotEntries(cmd.From, body.PlaylistServerID, body.SnapshotID)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	entries, err := m.buildSnapshotEntries(cmd.From, rawEntries, body.Resolve)
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

	// --- Phase 2: State mutation (engine manages its own locking) ---
	reply := m.engine.HandleCommand(queueCmd)
	if reply.OK {
		m.engine.Queue.SetRepeat(capture.Repeat)
		if capture.RepeatMode != "" {
			m.engine.Queue.SetRepeatMode(capture.RepeatMode)
		}
		m.engine.SetPlaybackPosition(capture.PositionMS)
	}
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
