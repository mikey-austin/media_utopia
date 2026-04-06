package renderercore

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mikey-austin/media_utopia/internal/adapters/idgen"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// Driver executes playback actions.
type Driver interface {
	Play(url string, positionMS int64) error
	Pause() error
	Resume() error
	Stop() error
	SeekTo(positionMS int64) error
	SetVolume(volume float64) error
	SetMute(mute bool) error
	Position() (positionMS int64, durationMS int64, ok bool)
}

// ErrUnsupported indicates driver capability is missing.
var ErrUnsupported = errors.New("unsupported")

// Engine handles renderer commands, queue, and leases.
type Engine struct {
	Queue   *Queue
	Leases  *LeaseManager
	Driver  Driver
	IDGen   idgen.Generator
	NodeID  string
	Name    string
	State   mu.RendererState
	Updated int64

	// sessionMu protects State.Session writes from HandleSessionCommand,
	// allowing session commands to be processed without the caller's
	// module-level lock (which may be held by slow driver operations).
	sessionMu sync.Mutex

	// recentCmds tracks recently processed command IDs to deduplicate
	// MQTT QoS 1 redeliveries. Only mutating commands are tracked.
	recentCmds    [64]string
	recentCmdNext int
}

// NewEngine creates a renderer engine with default state.
func NewEngine(nodeID string, name string, driver Driver) *Engine {
	engine := &Engine{
		Queue:  &Queue{},
		Leases: &LeaseManager{},
		Driver: driver,
		NodeID: nodeID,
		Name:   name,
		State: mu.RendererState{
			Playback: &mu.PlaybackState{Status: "stopped", Volume: 1.0},
			Queue:    &mu.QueueState{Revision: 0, Length: 0, Index: 0},
			TS:       time.Now().Unix(),
		},
	}
	engine.Leases.idPrefix = nodeID
	return engine
}

// IsSessionCommand returns true for session.acquire/renew/release commands.
func IsSessionCommand(cmdType string) bool {
	switch cmdType {
	case "session.acquire", "session.renew", "session.release":
		return true
	}
	return false
}

// HandleSessionCommand processes a session command using only the Engine's
// internal sessionMu — it does NOT require the caller to hold the module-level
// lock. This prevents session renewals from being blocked behind slow driver
// operations (e.g. GStreamer pipeline teardown).
func (e *Engine) HandleSessionCommand(cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	reply := mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix()}

	e.sessionMu.Lock()
	defer e.sessionMu.Unlock()

	switch cmd.Type {
	case "session.acquire":
		return e.handleSessionAcquire(cmd, reply)
	case "session.renew":
		return e.handleSessionRenew(cmd, reply)
	case "session.release":
		return e.handleSessionRelease(cmd, reply)
	default:
		return errorReply(cmd, "INVALID", "not a session command")
	}
}

// HandleCommand executes a command and returns reply.
func (e *Engine) HandleCommand(cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	reply := mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix()}

	// Deduplicate mutating commands (MQTT QoS 1 can redeliver).
	// Read-only commands (queue.get, session.*) are safe to process twice.
	if isMutatingCommand(cmd.Type) {
		if e.seenCommand(cmd.ID) {
			return reply // Already processed, return ack without re-executing
		}
		e.recordCommand(cmd.ID)
	}

	switch cmd.Type {
	case "session.acquire":
		return e.handleSessionAcquire(cmd, reply)
	case "session.renew":
		return e.handleSessionRenew(cmd, reply)
	case "session.release":
		return e.handleSessionRelease(cmd, reply)
	case "queue.get":
		return e.handleQueueGet(cmd, reply)
	case "queue.set":
		return e.handleQueueSet(cmd, reply)
	case "queue.add":
		return e.handleQueueAdd(cmd, reply)
	case "queue.remove":
		return e.handleQueueRemove(cmd, reply)
	case "queue.move":
		return e.handleQueueMove(cmd, reply)
	case "queue.clear":
		return e.handleQueueClear(cmd, reply)
	case "queue.jump":
		return e.handleQueueJump(cmd, reply)
	case "queue.shuffle":
		return e.handleQueueShuffle(cmd, reply)
	case "queue.setShuffle":
		return e.handleQueueSetShuffle(cmd, reply)
	case "queue.setRepeat":
		return e.handleQueueRepeat(cmd, reply)
	case "playback.play":
		return e.handlePlaybackPlay(cmd, reply)
	case "playback.pause":
		return e.handlePlaybackPause(cmd, reply)
	case "playback.stop":
		return e.handlePlaybackStop(cmd, reply)
	case "playback.seek":
		return e.handlePlaybackSeek(cmd, reply)
	case "playback.next":
		return e.handlePlaybackNext(cmd, reply)
	case "playback.prev":
		return e.handlePlaybackPrev(cmd, reply)
	case "playback.setVolume":
		return e.handleSetVolume(cmd, reply)
	case "playback.setMute":
		return e.handleSetMute(cmd, reply)
	default:
		return errorReply(cmd, "INVALID", "unsupported command")
	}
}

func (e *Engine) handleSessionAcquire(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.SessionAcquireBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if body.TTLMS <= 0 {
		body.TTLMS = 300000
	}
	lease, err := e.Leases.Acquire(cmd.From, time.Duration(body.TTLMS)*time.Millisecond)
	if err != nil {
		return errorReply(cmd, "LEASE_MISMATCH", err.Error())
	}

	e.State.Session = &mu.SessionState{ID: lease.ID, Owner: lease.Owner, LeaseExpireAt: lease.LeaseExpiresAt}
	return withBody(reply, mu.SessionReplyBody{Session: lease, StateVersion: e.bumpSessionState()})
}

func (e *Engine) handleSessionRenew(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if cmd.Lease == nil {
		return errorReply(cmd, "LEASE_REQUIRED", "lease required")
	}
	var body mu.SessionRenewBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if body.TTLMS <= 0 {
		body.TTLMS = 300000
	}
	lease, err := e.Leases.Renew(cmd.Lease.SessionID, cmd.Lease.Token, time.Duration(body.TTLMS)*time.Millisecond)
	if err != nil {
		return errorReply(cmd, "LEASE_MISMATCH", err.Error())
	}
	e.State.Session = &mu.SessionState{ID: lease.ID, Owner: lease.Owner, LeaseExpireAt: lease.LeaseExpiresAt}
	return withBody(reply, mu.SessionReplyBody{Session: lease, StateVersion: e.bumpSessionState()})
}

func (e *Engine) handleSessionRelease(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if cmd.Lease == nil {
		return errorReply(cmd, "LEASE_REQUIRED", "lease required")
	}
	if err := e.Leases.Release(cmd.Lease.SessionID, cmd.Lease.Token); err != nil {
		return errorReply(cmd, "LEASE_MISMATCH", err.Error())
	}
	e.State.Session = nil
	e.bumpSessionState()
	return reply
}

func (e *Engine) handleQueueGet(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.QueueGetBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	result := e.Queue.Snapshot(body.From, body.Count)
	return withBody(reply, result)
}

func (e *Engine) handleQueueSet(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.QueueSetBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	entries := toQueueEntries(body.Entries)
	if err := e.Queue.Set(entries, body.StartIndex, cmd.IfRevision); err != nil {
		return errorReply(cmd, "CONFLICT", err.Error())
	}
	e.bumpQueue()
	return reply
}

func (e *Engine) handleQueueAdd(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.QueueAddBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	entries := toQueueEntries(body.Entries)
	if err := e.Queue.Add(entries, body.Position, body.AtIndex); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	e.bumpQueue()
	return reply
}

func (e *Engine) handleQueueRemove(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.QueueRemoveBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if err := e.Queue.Remove(body.QueueEntryID, body.Index); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	e.bumpQueue()
	return reply
}

func (e *Engine) handleQueueMove(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.QueueMoveBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if err := e.Queue.Move(body.FromIndex, body.ToIndex); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	e.bumpQueue()
	return reply
}

func (e *Engine) handleQueueClear(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	e.Queue.Clear()
	e.bumpQueue()
	return reply
}

func (e *Engine) handleQueueJump(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.QueueJumpBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if err := e.Queue.Jump(body.Index); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	e.bumpQueue()
	return e.startCurrentPlayback(reply)
}

func (e *Engine) handleQueueShuffle(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.QueueShuffleBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	e.Queue.Shuffle(body.Seed)
	e.bumpQueue()
	return reply
}

func (e *Engine) handleQueueSetShuffle(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.QueueSetShuffleBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	e.Queue.SetShuffle(body.Shuffle)
	e.bumpQueue()
	return reply
}

func (e *Engine) handleQueueRepeat(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.QueueRepeatBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if body.Mode != "" {
		e.Queue.SetRepeatMode(body.Mode)
	} else {
		e.Queue.SetRepeat(body.Repeat)
	}
	e.bumpQueue()
	return reply
}

func (e *Engine) handlePlaybackPlay(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.PlaybackPlayBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if body.Index != nil {
		if err := e.Queue.Jump(*body.Index); err != nil {
			return errorReply(cmd, "INVALID", err.Error())
		}
	}
	if body.Index == nil {
		switch e.State.Playback.Status {
		case "paused":
			if err := e.Driver.Resume(); err != nil {
				return errorReply(cmd, "INVALID", err.Error())
			}
			e.State.Playback.Status = "playing"
			e.bumpState()
			return reply
		case "playing":
			return reply
		}
	}
	return e.startCurrentPlayback(reply)
}

func (e *Engine) handlePlaybackPause(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	if err := e.Driver.Pause(); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	e.State.Playback.Status = "paused"
	e.bumpState()
	return reply
}

func (e *Engine) handlePlaybackStop(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	if err := e.Driver.Stop(); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	e.State.Playback.Status = "stopped"
	e.bumpState()
	return reply
}

func (e *Engine) handlePlaybackSeek(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.PlaybackSeekBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if err := e.Driver.SeekTo(body.PositionMS); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	e.State.Playback.PositionMS = body.PositionMS
	e.bumpState()
	return reply
}

func (e *Engine) handlePlaybackNext(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	if _, ok := e.Queue.Next(); !ok {
		return errorReply(cmd, "NOT_FOUND", "end of queue")
	}
	return e.startCurrentPlayback(reply)
}

func (e *Engine) handlePlaybackPrev(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	if _, ok := e.Queue.Prev(); !ok {
		return errorReply(cmd, "NOT_FOUND", "start of queue")
	}
	return e.startCurrentPlayback(reply)
}

func (e *Engine) startCurrentPlayback(reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.playCurrent(); err != nil {
		reply.OK = false
		reply.Type = "error"
		reply.Err = &mu.ReplyError{Code: "INVALID", Message: err.Error()}
		return reply
	}
	return reply
}

func (e *Engine) playCurrent() error {
	entry, ok := e.Queue.Current()
	if !ok {
		return errors.New("queue empty")
	}
	url := resolvedURL(entry)
	if url == "" {
		return errors.New("entry not resolved")
	}
	if err := e.Driver.Play(url, 0); err != nil {
		return err
	}
	e.State.Playback.Status = "playing"
	e.State.Current = &mu.CurrentItemState{QueueEntryID: entry.QueueEntryID, ItemID: entry.ItemID, Metadata: entry.Metadata}
	e.bumpState()
	return nil
}

// AdvanceAfterEnd advances the queue after playback ends.
func (e *Engine) AdvanceAfterEnd() {
	if e.State.Playback == nil || e.State.Playback.Status != "playing" {
		return
	}
	if _, ok := e.Queue.Next(); !ok {
		_ = e.Driver.Stop()
		e.State.Playback.Status = "stopped"
		e.State.Playback.PositionMS = 0
		e.bumpState()
		return
	}
	if err := e.playCurrent(); err != nil {
		_ = e.Driver.Stop()
		e.State.Playback.Status = "stopped"
		e.bumpState()
	}
}

func (e *Engine) handleSetVolume(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.PlaybackSetVolumeBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if err := e.Driver.SetVolume(body.Volume); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	e.State.Playback.Volume = body.Volume
	e.bumpState()
	return reply
}

func (e *Engine) handleSetMute(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	if err := e.requireLease(cmd); err != nil {
		return errorReply(cmd, "LEASE_REQUIRED", err.Error())
	}
	var body mu.PlaybackSetMuteBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if err := e.Driver.SetMute(body.Mute); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	e.State.Playback.Mute = body.Mute
	e.bumpState()
	return reply
}

func (e *Engine) bumpState() int64 {
	e.State.StateVersion++
	e.State.Queue = ptrQueueState(e.Queue.Summary())
	e.State.TS = time.Now().Unix()
	e.Updated = time.Now().Unix()
	return e.State.StateVersion
}

// bumpSessionState is like bumpState but safe to call from
// HandleSessionCommand where the module lock is not held — it avoids
// reading Queue state which may be concurrently modified.
func (e *Engine) bumpSessionState() int64 {
	e.State.StateVersion++
	e.State.TS = time.Now().Unix()
	e.Updated = time.Now().Unix()
	return e.State.StateVersion
}

func (e *Engine) bumpQueue() {
	e.State.Queue = ptrQueueState(e.Queue.Summary())
	e.bumpState()
}

func (e *Engine) requireLease(cmd mu.CommandEnvelope) error {
	if cmd.Lease == nil {
		return errors.New("lease required")
	}
	return e.Leases.Require(cmd.Lease.SessionID, cmd.Lease.Token)
}

func resolvedURL(entry QueueEntry) string {
	if entry.Resolved != nil {
		return entry.Resolved.URL
	}
	return ""
}

func toQueueEntries(entries []mu.QueueEntry) []QueueEntry {
	result := make([]QueueEntry, 0, len(entries))
	for _, entry := range entries {
		meta := entry.Metadata
		if meta == nil {
			meta = map[string]any{}
		}
		result = append(result, QueueEntry{
			QueueEntryID: fmt.Sprintf("mu:queueentry:renderer:R:%s", idgen.Generator{}.NewID()),
			ItemID:       itemID(entry),
			Metadata:     meta,
			Ref:          entry.Ref,
			Resolved:     entry.Resolved,
		})
	}
	return result
}

func itemID(entry mu.QueueEntry) string {
	if entry.Ref != nil {
		return entry.Ref.ID
	}
	if entry.Resolved != nil {
		return entry.Resolved.URL
	}
	return ""
}

func withBody(reply mu.ReplyEnvelope, body any) mu.ReplyEnvelope {
	payload, _ := json.Marshal(body)
	reply.Body = payload
	return reply
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

func ptrQueueState(state mu.QueueState) *mu.QueueState {
	copy := state
	return &copy
}

// isMutatingCommand returns true for commands that change state.
// These must be deduplicated to prevent MQTT QoS 1 redelivery issues.
func isMutatingCommand(cmdType string) bool {
	switch cmdType {
	case "queue.set", "queue.add", "queue.remove", "queue.move",
		"queue.clear", "queue.shuffle", "queue.setShuffle",
		"queue.setRepeat", "queue.loadPlaylist", "queue.loadSnapshot",
		"playback.play", "playback.pause", "playback.stop",
		"playback.next", "playback.prev", "playback.toggle",
		"playback.seek", "playback.setVolume", "playback.setMute":
		return true
	}
	return false
}

// seenCommand checks if a command ID was recently processed.
func (e *Engine) seenCommand(id string) bool {
	for _, seen := range e.recentCmds {
		if seen == id {
			return true
		}
	}
	return false
}

// recordCommand adds a command ID to the recent ring buffer.
func (e *Engine) recordCommand(id string) {
	e.recentCmds[e.recentCmdNext%len(e.recentCmds)] = id
	e.recentCmdNext++
}
