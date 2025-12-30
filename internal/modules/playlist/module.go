package playlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/mikey-austin/media_utopia/internal/adapters/idgen"
	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// Config configures the playlist module.
type Config struct {
	NodeID      string
	TopicBase   string
	StoragePath string
	Identity    string
}

// Module provides playlist server behavior.
type Module struct {
	log      *slog.Logger
	client   *mqttserver.Client
	storage  *Storage
	idgen    idgen.Generator
	config   Config
	cmdTopic string
}

// NewModule initializes the playlist module.
func NewModule(log *slog.Logger, client *mqttserver.Client, cfg Config) (*Module, error) {
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, errors.New("playlist node_id required")
	}
	if strings.TrimSpace(cfg.TopicBase) == "" {
		cfg.TopicBase = mu.BaseTopic
	}
	if strings.TrimSpace(cfg.StoragePath) == "" {
		return nil, errors.New("storage_path required")
	}

	storage, err := NewStorage(cfg.StoragePath)
	if err != nil {
		return nil, err
	}

	cmdTopic := mu.TopicCommands(cfg.TopicBase, cfg.NodeID)

	return &Module{
		log:      log,
		client:   client,
		storage:  storage,
		idgen:    idgen.Generator{},
		config:   cfg,
		cmdTopic: cmdTopic,
	}, nil
}

// Run starts the playlist module.
func (m *Module) Run(ctx context.Context) error {
	handler := func(_ paho.Client, msg paho.Message) {
		m.handleMessage(msg)
	}

	if err := m.client.Subscribe(m.cmdTopic, 1, handler); err != nil {
		return err
	}
	defer m.client.Unsubscribe(m.cmdTopic)

	if err := m.publishPresence(); err != nil {
		return err
	}

	<-ctx.Done()
	return nil
}

func (m *Module) publishPresence() error {
	presence := mu.Presence{
		NodeID: m.config.NodeID,
		Kind:   "playlist",
		Name:   "Playlist Server",
		Caps:   map[string]any{},
		TS:     time.Now().Unix(),
	}

	payload, err := json.Marshal(presence)
	if err != nil {
		return err
	}
	return m.client.Publish(mu.TopicPresence(m.config.TopicBase, m.config.NodeID), 1, true, payload)
}

func (m *Module) handleMessage(msg paho.Message) {
	var cmd mu.CommandEnvelope
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		m.log.Warn("invalid command", "error", err)
		return
	}

	reply := m.dispatch(cmd)
	if cmd.ReplyTo == "" {
		return
	}
	payload, err := json.Marshal(reply)
	if err != nil {
		m.log.Error("marshal reply", "error", err)
		return
	}
	if err := m.client.Publish(cmd.ReplyTo, 1, false, payload); err != nil {
		m.log.Error("publish reply", "error", err)
	}
}

func (m *Module) dispatch(cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	reply := mu.ReplyEnvelope{
		ID:   cmd.ID,
		Type: "ack",
		OK:   true,
		TS:   time.Now().Unix(),
	}

	switch cmd.Type {
	case "playlist.list":
		return m.playlistList(cmd, reply)
	case "playlist.create":
		return m.playlistCreate(cmd, reply)
	case "playlist.get":
		return m.playlistGet(cmd, reply)
	case "playlist.addItems":
		return m.playlistAddItems(cmd, reply)
	case "playlist.removeItems":
		return m.playlistRemoveItems(cmd, reply)
	case "playlist.rename":
		return m.playlistRename(cmd, reply)
	case "snapshot.save":
		return m.snapshotSave(cmd, reply)
	case "snapshot.list":
		return m.snapshotList(cmd, reply)
	case "suggest.list":
		return m.suggestList(cmd, reply)
	case "suggest.get":
		return m.suggestGet(cmd, reply)
	case "suggest.promote":
		return m.suggestPromote(cmd, reply)
	default:
		return errorReply(cmd, "INVALID", "unsupported command")
	}
}

func (m *Module) playlistList(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	playlists, err := m.storage.ListPlaylists()
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}

	out := mu.PlaylistListReply{Playlists: make([]mu.PlaylistSummary, 0, len(playlists))}
	for _, pl := range playlists {
		out.Playlists = append(out.Playlists, mu.PlaylistSummary{PlaylistID: pl.PlaylistID, Name: pl.Name, Revision: pl.Revision})
	}
	payload, _ := json.Marshal(out)
	reply.Body = payload
	return reply
}

func (m *Module) playlistCreate(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.PlaylistCreateBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if strings.TrimSpace(body.Name) == "" {
		return errorReply(cmd, "INVALID", "name required")
	}

	playlistID := fmt.Sprintf("mu:playlist:plsrv:%s:%s", safeNodeSuffix(m.config.NodeID), m.idgen.NewID())
	now := time.Now().Unix()
	pl := Playlist{
		PlaylistID: playlistID,
		Name:       body.Name,
		Owner:      cmd.From,
		Revision:   1,
		Entries:    []PlaylistEntry{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := m.storage.SavePlaylist(pl); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	return reply
}

func (m *Module) playlistGet(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.PlaylistGetBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	pl, err := m.storage.GetPlaylist(body.PlaylistID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errorReply(cmd, "NOT_FOUND", "playlist not found")
		}
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(pl)
	reply.Body = payload
	return reply
}

func (m *Module) playlistAddItems(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.PlaylistAddItemsBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	pl, err := m.storage.GetPlaylist(body.PlaylistID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errorReply(cmd, "NOT_FOUND", "playlist not found")
		}
		return errorReply(cmd, "INVALID", err.Error())
	}
	if cmd.IfRevision != nil && *cmd.IfRevision != pl.Revision {
		return errorReply(cmd, "CONFLICT", "revision mismatch")
	}

	for _, entry := range body.Entries {
		pl.Entries = append(pl.Entries, PlaylistEntry{
			EntryID:  fmt.Sprintf("mu:playlistentry:plsrv:%s:%s", safeNodeSuffix(m.config.NodeID), m.idgen.NewID()),
			Ref:      entry.Ref,
			Resolved: entry.Resolved,
		})
	}
	pl.Revision++
	pl.UpdatedAt = time.Now().Unix()
	if err := m.storage.SavePlaylist(pl); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	return reply
}

func (m *Module) playlistRemoveItems(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.PlaylistRemoveItemsBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	pl, err := m.storage.GetPlaylist(body.PlaylistID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errorReply(cmd, "NOT_FOUND", "playlist not found")
		}
		return errorReply(cmd, "INVALID", err.Error())
	}
	if cmd.IfRevision != nil && *cmd.IfRevision != pl.Revision {
		return errorReply(cmd, "CONFLICT", "revision mismatch")
	}

	if len(body.EntryIDs) == 0 {
		return errorReply(cmd, "INVALID", "entryIds required")
	}
	set := map[string]struct{}{}
	for _, id := range body.EntryIDs {
		set[id] = struct{}{}
	}

	filtered := pl.Entries[:0]
	for _, entry := range pl.Entries {
		if _, ok := set[entry.EntryID]; !ok {
			filtered = append(filtered, entry)
		}
	}
	pl.Entries = filtered
	pl.Revision++
	pl.UpdatedAt = time.Now().Unix()
	if err := m.storage.SavePlaylist(pl); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	return reply
}

func (m *Module) playlistRename(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.PlaylistRenameBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if strings.TrimSpace(body.Name) == "" {
		return errorReply(cmd, "INVALID", "name required")
	}
	pl, err := m.storage.GetPlaylist(body.PlaylistID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errorReply(cmd, "NOT_FOUND", "playlist not found")
		}
		return errorReply(cmd, "INVALID", err.Error())
	}

	pl.Name = body.Name
	pl.Revision++
	pl.UpdatedAt = time.Now().Unix()
	if err := m.storage.SavePlaylist(pl); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	return reply
}

func (m *Module) snapshotSave(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.SnapshotSaveBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if strings.TrimSpace(body.Name) == "" {
		return errorReply(cmd, "INVALID", "name required")
	}
	id := fmt.Sprintf("mu:snapshot:plsrv:%s:%s", safeNodeSuffix(m.config.NodeID), m.idgen.NewID())
	now := time.Now().Unix()
	snapshot := Snapshot{
		SnapshotID: id,
		Name:       body.Name,
		Owner:      cmd.From,
		Revision:   1,
		RendererID: body.RendererID,
		SessionID:  body.SessionID,
		Capture:    body.Capture,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := m.storage.SaveSnapshot(snapshot); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	return reply
}

func (m *Module) snapshotList(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	snapshots, err := m.storage.ListSnapshots()
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}

	out := mu.SnapshotListReply{Snapshots: make([]mu.SnapshotSummary, 0, len(snapshots))}
	for _, snap := range snapshots {
		out.Snapshots = append(out.Snapshots, mu.SnapshotSummary{SnapshotID: snap.SnapshotID, Name: snap.Name, Revision: snap.Revision})
	}
	payload, _ := json.Marshal(out)
	reply.Body = payload
	return reply
}

func (m *Module) suggestList(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	suggestions, err := m.storage.ListSuggestions()
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}

	out := mu.SuggestListReply{Suggestions: make([]mu.SuggestSummary, 0, len(suggestions))}
	for _, sug := range suggestions {
		out.Suggestions = append(out.Suggestions, mu.SuggestSummary{SuggestionID: sug.SuggestionID, Name: sug.Name, Revision: sug.Revision})
	}
	payload, _ := json.Marshal(out)
	reply.Body = payload
	return reply
}

func (m *Module) suggestGet(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.SuggestGetBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	sug, err := m.storage.GetSuggestion(body.SuggestionID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errorReply(cmd, "NOT_FOUND", "suggestion not found")
		}
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(sug)
	reply.Body = payload
	return reply
}

func (m *Module) suggestPromote(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.SuggestPromoteBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if strings.TrimSpace(body.Name) == "" {
		return errorReply(cmd, "INVALID", "name required")
	}
	sug, err := m.storage.GetSuggestion(body.SuggestionID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errorReply(cmd, "NOT_FOUND", "suggestion not found")
		}
		return errorReply(cmd, "INVALID", err.Error())
	}

	playlistID := fmt.Sprintf("mu:playlist:plsrv:%s:%s", safeNodeSuffix(m.config.NodeID), m.idgen.NewID())
	now := time.Now().Unix()
	entries := make([]PlaylistEntry, 0, len(sug.Entries))
	for _, entry := range sug.Entries {
		entries = append(entries, PlaylistEntry{
			EntryID:  fmt.Sprintf("mu:playlistentry:plsrv:%s:%s", safeNodeSuffix(m.config.NodeID), m.idgen.NewID()),
			Ref:      entry.Ref,
			Resolved: entry.Resolved,
		})
	}
	pl := Playlist{
		PlaylistID: playlistID,
		Name:       body.Name,
		Owner:      cmd.From,
		Revision:   1,
		Entries:    entries,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := m.storage.SavePlaylist(pl); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	return reply
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

func safeNodeSuffix(nodeID string) string {
	base := filepath.Base(nodeID)
	base = strings.ReplaceAll(base, ":", "-")
	if base == "." || base == "" {
		return "main"
	}
	return base
}
