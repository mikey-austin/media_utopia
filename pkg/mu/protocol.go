package mu

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// BaseTopic is the default MQTT topic prefix for the protocol.
const BaseTopic = "mu/v1"

// CommandEnvelope is the common controller command envelope for MQTT.
type CommandEnvelope struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	TS         int64           `json:"ts"`
	From       string          `json:"from"`
	ReplyTo    string          `json:"replyTo,omitempty"`
	Lease      *Lease          `json:"lease,omitempty"`
	IfRevision *int64          `json:"ifRevision,omitempty"`
	Body       json.RawMessage `json:"body"`
}

// Lease represents a session lease token used for mutations.
type Lease struct {
	SessionID string `json:"sessionId"`
	Token     string `json:"token"`
}

// ReplyEnvelope is the response envelope for commands.
type ReplyEnvelope struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	OK   bool            `json:"ok"`
	TS   int64           `json:"ts"`
	Body json.RawMessage `json:"body,omitempty"`
	Err  *ReplyError     `json:"err,omitempty"`
}

// ReplyError describes an error response.
type ReplyError struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail,omitempty"`
}

// Presence describes a node presence payload.
type Presence struct {
	NodeID string         `json:"nodeId"`
	Kind   string         `json:"kind"`
	Name   string         `json:"name"`
	Caps   map[string]any `json:"caps,omitempty"`
	EPs    map[string]any `json:"endpoints,omitempty"`
	TS     int64          `json:"ts"`
}

// RendererState captures the retained state of a renderer.
type RendererState struct {
	Session      *SessionState     `json:"session,omitempty"`
	Playback     *PlaybackState    `json:"playback,omitempty"`
	Queue        *QueueState       `json:"queue,omitempty"`
	Current      *CurrentItemState `json:"current,omitempty"`
	StateVersion int64             `json:"stateVersion,omitempty"`
	TS           int64             `json:"ts"`
}

// SessionState reflects renderer session ownership and lease expiry.
type SessionState struct {
	ID            string `json:"id"`
	Owner         string `json:"owner"`
	LeaseExpireAt int64  `json:"leaseExpiresAt"`
}

// PlaybackState describes playback status and properties.
type PlaybackState struct {
	Status     string  `json:"status"`
	PositionMS int64   `json:"positionMs"`
	DurationMS int64   `json:"durationMs"`
	Volume     float64 `json:"volume"`
	Mute       bool    `json:"mute"`
}

// QueueState describes the queue summary.
type QueueState struct {
	Revision   int64  `json:"revision"`
	Length     int64  `json:"length"`
	Index      int64  `json:"index"`
	Repeat     bool   `json:"repeat,omitempty"`
	RepeatMode string `json:"repeatMode,omitempty"`
	Shuffle    bool   `json:"shuffle,omitempty"`
}

// CurrentItemState describes the current queue entry.
type CurrentItemState struct {
	QueueEntryID string                 `json:"queueEntryId"`
	ItemID       string                 `json:"itemId"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Event is a generic event payload.
type Event struct {
	Type string `json:"type"`
	TS   int64  `json:"ts"`
}

// NewCommand builds a command envelope with a JSON body.
func NewCommand(cmdType string, body any) (CommandEnvelope, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return CommandEnvelope{}, fmt.Errorf("marshal body: %w", err)
	}

	return CommandEnvelope{
		Type: cmdType,
		Body: payload,
	}, nil
}

// ValidateCommandEnvelope validates required fields and lease rules.
func ValidateCommandEnvelope(cmd CommandEnvelope) error {
	if strings.TrimSpace(cmd.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(cmd.Type) == "" {
		return errors.New("type is required")
	}
	if cmd.TS <= 0 {
		return errors.New("ts must be a positive unix timestamp")
	}
	if strings.TrimSpace(cmd.From) == "" {
		return errors.New("from is required")
	}
	if len(cmd.Body) == 0 {
		return errors.New("body is required")
	}
	if CommandRequiresLease(cmd.Type) && cmd.Lease == nil {
		return errors.New("lease is required for mutation commands")
	}
	if cmd.Lease != nil {
		if strings.TrimSpace(cmd.Lease.SessionID) == "" || strings.TrimSpace(cmd.Lease.Token) == "" {
			return errors.New("lease sessionId and token are required")
		}
	}
	return nil
}

// CommandRequiresLease reports whether a command needs a lease token.
func CommandRequiresLease(cmdType string) bool {
	switch cmdType {
	case "session.renew", "session.release":
		return true
	case "playback.play", "playback.pause", "playback.stop", "playback.seek", "playback.next", "playback.prev":
		return true
	case "playback.setVolume", "playback.setMute":
		return true
	case "queue.set", "queue.add", "queue.remove", "queue.move", "queue.clear", "queue.jump", "queue.shuffle", "queue.setShuffle", "queue.setRepeat":
		return true
	case "queue.loadPlaylist", "queue.loadSnapshot":
		return true
	case "queue.loadSuggestion":
		return true
	default:
		return false
	}
}

// TopicPresence builds the presence topic for a node.
func TopicPresence(topicBase, nodeID string) string {
	return fmt.Sprintf("%s/node/%s/presence", topicBase, nodeID)
}

// TopicState builds the state topic for a node.
func TopicState(topicBase, nodeID string) string {
	return fmt.Sprintf("%s/node/%s/state", topicBase, nodeID)
}

// TopicCommands builds the command topic for a node.
func TopicCommands(topicBase, nodeID string) string {
	return fmt.Sprintf("%s/node/%s/cmd", topicBase, nodeID)
}

// TopicEvents builds the events topic for a node.
func TopicEvents(topicBase, nodeID string) string {
	return fmt.Sprintf("%s/node/%s/evt", topicBase, nodeID)
}

// TopicReply builds the reply topic for a controller instance.
func TopicReply(topicBase, controllerID string) string {
	return fmt.Sprintf("%s/reply/%s", topicBase, controllerID)
}
