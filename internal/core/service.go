package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mikey-austin/media_utopia/internal/ports"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// Service orchestrates mu CLI use cases.
type Service struct {
	Broker     ports.Broker
	Resolver   Resolver
	Clock      ports.Clock
	IDGen      ports.IDGen
	LeaseStore ports.LeaseStore
	Config     Config
}

// ListNodes returns presence entries with optional filters.
func (s Service) ListNodes(ctx context.Context, kind string, onlineOnly bool) (NodesResult, error) {
	nodes, err := s.Broker.ListPresence(ctx)
	if err != nil {
		return NodesResult{}, WrapError(ExitRuntime, "list nodes", err)
	}
	if kind != "" {
		filtered := nodes[:0]
		for _, node := range nodes {
			if node.Kind == kind {
				filtered = append(filtered, node)
			}
		}
		nodes = filtered
	}
	// Online filtering relies on presence; with retained presence this is best-effort.
	if onlineOnly {
		filtered := nodes[:0]
		for _, node := range nodes {
			if node.TS > 0 {
				filtered = append(filtered, node)
			}
		}
		nodes = filtered
	}
	return NodesResult{Nodes: nodes}, nil
}

// Status returns renderer state.
func (s Service) Status(ctx context.Context, selector string) (StatusResult, error) {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return StatusResult{}, err
	}
	state, err := s.Broker.GetRendererState(ctx, renderer.NodeID)
	if err != nil {
		return StatusResult{}, WrapError(ExitRuntime, "get renderer state", err)
	}
	return StatusResult{Renderer: renderer, State: state}, nil
}

// WatchStatus streams state and events for a renderer.
func (s Service) WatchStatus(ctx context.Context, selector string) (<-chan mu.RendererState, <-chan mu.Event, <-chan error, error) {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return nil, nil, nil, err
	}
	states, events, errs := s.Broker.WatchRenderer(ctx, renderer.NodeID)
	return states, events, errs, nil
}

// AcquireLease acquires a renderer lease and caches it.
func (s Service) AcquireLease(ctx context.Context, selector string, ttl time.Duration) (SessionResult, error) {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return SessionResult{}, err
	}

	cmd, err := mu.NewCommand("session.acquire", mu.SessionAcquireBody{TTLMS: ttl.Milliseconds()})
	if err != nil {
		return SessionResult{}, WrapError(ExitRuntime, "build command", err)
	}

	cmd = s.decorateCommand(cmd, nil, nil)
	reply, err := s.Broker.PublishCommand(ctx, renderer.NodeID, cmd)
	if err != nil {
		return SessionResult{}, WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return SessionResult{}, ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}

	var body mu.SessionReplyBody
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		return SessionResult{}, WrapError(ExitRuntime, "decode session reply", err)
	}

	lease := mu.Lease{SessionID: body.Session.ID, Token: body.Session.Token}
	if err := s.LeaseStore.Put(renderer.NodeID, lease); err != nil {
		return SessionResult{}, WrapError(ExitRuntime, "store lease", err)
	}

	return SessionResult{RendererID: renderer.NodeID, Session: body.Session, StateVer: body.StateVersion}, nil
}

// RenewLease renews the cached lease for a renderer.
func (s Service) RenewLease(ctx context.Context, selector string, ttl time.Duration) error {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return err
	}
	lease, err := s.lookupLease(renderer.NodeID)
	if err != nil {
		return err
	}

	cmd, err := mu.NewCommand("session.renew", mu.SessionRenewBody{TTLMS: ttl.Milliseconds()})
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}

	cmd = s.decorateCommand(cmd, &lease, nil)
	reply, err := s.Broker.PublishCommand(ctx, renderer.NodeID, cmd)
	if err != nil {
		return WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}
	return nil
}

// ReleaseLease releases a cached lease for a renderer.
func (s Service) ReleaseLease(ctx context.Context, selector string) error {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return err
	}
	lease, err := s.lookupLease(renderer.NodeID)
	if err != nil {
		return err
	}

	cmd, err := mu.NewCommand("session.release", struct{}{})
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}

	cmd = s.decorateCommand(cmd, &lease, nil)
	reply, err := s.Broker.PublishCommand(ctx, renderer.NodeID, cmd)
	if err != nil {
		return WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}

	if err := s.LeaseStore.Clear(renderer.NodeID); err != nil {
		return WrapError(ExitRuntime, "clear lease", err)
	}
	return nil
}

// Owner returns the renderer session owner.
func (s Service) Owner(ctx context.Context, selector string) (string, error) {
	result, err := s.Status(ctx, selector)
	if err != nil {
		return "", err
	}
	if result.State.Session == nil {
		return "", &CLIError{Code: ExitNotFound, Msg: "no session"}
	}
	return result.State.Session.Owner, nil
}

// PlaybackPlay sends playback.play.
func (s Service) PlaybackPlay(ctx context.Context, selector string, index *int64) error {
	return s.simplePlayback(ctx, selector, "playback.play", mu.PlaybackPlayBody{Index: index})
}

// PlaybackPause sends playback.pause.
func (s Service) PlaybackPause(ctx context.Context, selector string) error {
	return s.simplePlayback(ctx, selector, "playback.pause", struct{}{})
}

// PlaybackStop sends playback.stop.
func (s Service) PlaybackStop(ctx context.Context, selector string) error {
	return s.simplePlayback(ctx, selector, "playback.stop", struct{}{})
}

// PlaybackNext sends playback.next.
func (s Service) PlaybackNext(ctx context.Context, selector string) error {
	return s.simplePlayback(ctx, selector, "playback.next", struct{}{})
}

// PlaybackPrev sends playback.prev.
func (s Service) PlaybackPrev(ctx context.Context, selector string) error {
	return s.simplePlayback(ctx, selector, "playback.prev", struct{}{})
}

// PlaybackToggle toggles playback based on current state.
func (s Service) PlaybackToggle(ctx context.Context, selector string) error {
	status, err := s.Status(ctx, selector)
	if err != nil {
		return err
	}
	if status.State.Playback == nil {
		return &CLIError{Code: ExitRuntime, Msg: "no playback state"}
	}
	if status.State.Playback.Status == "playing" {
		return s.PlaybackPause(ctx, selector)
	}
	return s.PlaybackPlay(ctx, selector, nil)
}

// PlaybackSeek sends playback.seek with absolute or relative position.
func (s Service) PlaybackSeek(ctx context.Context, selector string, seekArg string) error {
	position, err := s.resolveSeekPosition(ctx, selector, seekArg)
	if err != nil {
		return err
	}
	return s.simplePlayback(ctx, selector, "playback.seek", mu.PlaybackSeekBody{PositionMS: position})
}

// SetVolume sets or adjusts volume.
func (s Service) SetVolume(ctx context.Context, selector string, arg string, mute *bool) error {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return err
	}
	lease, err := s.lookupLease(renderer.NodeID)
	if err != nil {
		return err
	}

	if mute != nil {
		cmd, err := mu.NewCommand("playback.setMute", mu.PlaybackSetMuteBody{Mute: *mute})
		if err != nil {
			return WrapError(ExitRuntime, "build command", err)
		}
		cmd = s.decorateCommand(cmd, &lease, nil)
		return s.publishSimple(ctx, renderer.NodeID, cmd)
	}

	vol, err := s.resolveVolume(ctx, renderer.NodeID, arg)
	if err != nil {
		return err
	}
	cmd, err := mu.NewCommand("playback.setVolume", mu.PlaybackSetVolumeBody{Volume: vol})
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, &lease, nil)
	return s.publishSimple(ctx, renderer.NodeID, cmd)
}

// QueueList returns a page of queue entries.
func (s Service) QueueList(ctx context.Context, selector string, from, count int64) (QueueResult, error) {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return QueueResult{}, err
	}
	cmd, err := mu.NewCommand("queue.get", mu.QueueGetBody{From: from, Count: count})
	if err != nil {
		return QueueResult{}, WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	reply, err := s.Broker.PublishCommand(ctx, renderer.NodeID, cmd)
	if err != nil {
		return QueueResult{}, WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return QueueResult{}, ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}
	var body mu.QueueGetReply
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		return QueueResult{}, WrapError(ExitRuntime, "decode queue reply", err)
	}
	return QueueResult{RendererID: renderer.NodeID, Queue: body}, nil
}

// QueueNow returns the current queue item.
func (s Service) QueueNow(ctx context.Context, selector string) (QueueNowResult, error) {
	status, err := s.Status(ctx, selector)
	if err != nil {
		return QueueNowResult{}, err
	}
	return QueueNowResult{RendererID: status.Renderer.NodeID, Current: status.State.Current}, nil
}

// QueueClear clears the queue.
func (s Service) QueueClear(ctx context.Context, selector string) error {
	return s.simplePlayback(ctx, selector, "queue.clear", struct{}{})
}

// QueueJump jumps to index.
func (s Service) QueueJump(ctx context.Context, selector string, index int64) error {
	return s.simplePlayback(ctx, selector, "queue.jump", mu.QueueJumpBody{Index: index})
}

// QueueRemove removes by index or entry id.
func (s Service) QueueRemove(ctx context.Context, selector string, arg string) error {
	body := mu.QueueRemoveBody{}
	if idx, err := strconv.ParseInt(arg, 10, 64); err == nil {
		body.Index = &idx
	} else {
		body.QueueEntryID = arg
	}
	return s.simplePlayback(ctx, selector, "queue.remove", body)
}

// QueueMove moves an entry.
func (s Service) QueueMove(ctx context.Context, selector string, from, to int64) error {
	return s.simplePlayback(ctx, selector, "queue.move", mu.QueueMoveBody{FromIndex: from, ToIndex: to})
}

// QueueShuffle shuffles the queue.
func (s Service) QueueShuffle(ctx context.Context, selector string, seed int64) error {
	return s.simplePlayback(ctx, selector, "queue.shuffle", mu.QueueShuffleBody{Seed: seed})
}

// QueueRepeat sets repeat mode.
func (s Service) QueueRepeat(ctx context.Context, selector string, repeat bool) error {
	return s.simplePlayback(ctx, selector, "queue.setRepeat", mu.QueueRepeatBody{Repeat: repeat})
}

// QueueAdd adds entries to the queue.
func (s Service) QueueAdd(ctx context.Context, selector string, items []string, position string, atIndex *int64, resolve string) error {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return err
	}
	lease, err := s.lookupLease(renderer.NodeID)
	if err != nil {
		return err
	}

	entries, err := s.buildQueueEntries(ctx, renderer, items, resolve)
	if err != nil {
		return err
	}
	body := mu.QueueAddBody{Position: position, AtIndex: atIndex, Entries: entries}
	cmd, err := mu.NewCommand("queue.add", body)
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, &lease, nil)
	return s.publishSimple(ctx, renderer.NodeID, cmd)
}

// QueueSet replaces the queue atomically.
func (s Service) QueueSet(ctx context.Context, selector string, entries []mu.QueueEntry, ifRev *int64) error {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return err
	}
	lease, err := s.lookupLease(renderer.NodeID)
	if err != nil {
		return err
	}
	body := mu.QueueSetBody{StartIndex: 0, Entries: entries}
	cmd, err := mu.NewCommand("queue.set", body)
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, &lease, ifRev)
	return s.publishSimple(ctx, renderer.NodeID, cmd)
}

// QueueLoadPlaylist loads a playlist into the queue.
func (s Service) QueueLoadPlaylist(ctx context.Context, selector string, playlistID string, mode string, resolve string, serverSelector string) error {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return err
	}
	lease, err := s.lookupLease(renderer.NodeID)
	if err != nil {
		return err
	}
	playlistServer, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return err
	}
	body := mu.QueueLoadPlaylistBody{PlaylistServerID: playlistServer.NodeID, PlaylistID: playlistID, Mode: mode, Resolve: resolve}
	cmd, err := mu.NewCommand("queue.loadPlaylist", body)
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, &lease, nil)
	return s.publishSimple(ctx, renderer.NodeID, cmd)
}

// QueueLoadSnapshot loads a snapshot into the queue.
func (s Service) QueueLoadSnapshot(ctx context.Context, selector string, snapshotID string, mode string, resolve string, serverSelector string) error {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return err
	}
	lease, err := s.lookupLease(renderer.NodeID)
	if err != nil {
		return err
	}
	playlistServer, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return err
	}
	body := mu.QueueLoadSnapshotBody{PlaylistServerID: playlistServer.NodeID, SnapshotID: snapshotID, Mode: mode, Resolve: resolve}
	cmd, err := mu.NewCommand("queue.loadSnapshot", body)
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, &lease, nil)
	return s.publishSimple(ctx, renderer.NodeID, cmd)
}

// PlaylistList lists playlists on the playlist server.
func (s Service) PlaylistList(ctx context.Context, serverSelector string) (PlaylistListResult, error) {
	server, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return PlaylistListResult{}, err
	}
	cmd, err := mu.NewCommand("playlist.list", mu.PlaylistListBody{Owner: s.Config.Identity})
	if err != nil {
		return PlaylistListResult{}, WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	reply, err := s.Broker.PublishCommand(ctx, server.NodeID, cmd)
	if err != nil {
		return PlaylistListResult{}, WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return PlaylistListResult{}, ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}
	var body mu.PlaylistListReply
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		return PlaylistListResult{}, WrapError(ExitRuntime, "decode playlist reply", err)
	}
	return PlaylistListResult{Playlists: body.Playlists}, nil
}

// PlaylistCreate creates a playlist.
func (s Service) PlaylistCreate(ctx context.Context, name string, serverSelector string) error {
	server, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return err
	}
	cmd, err := mu.NewCommand("playlist.create", mu.PlaylistCreateBody{Name: name})
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	return s.publishSimple(ctx, server.NodeID, cmd)
}

// PlaylistGet fetches a playlist by ID.
func (s Service) PlaylistGet(ctx context.Context, playlistID string, serverSelector string) (RawResult, error) {
	server, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return RawResult{}, err
	}
	cmd, err := mu.NewCommand("playlist.get", mu.PlaylistGetBody{PlaylistID: playlistID})
	if err != nil {
		return RawResult{}, WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	reply, err := s.Broker.PublishCommand(ctx, server.NodeID, cmd)
	if err != nil {
		return RawResult{}, WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return RawResult{}, ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}
	return RawResult{Data: json.RawMessage(reply.Body)}, nil
}

// PlaylistAdd adds items to a playlist.
func (s Service) PlaylistAdd(ctx context.Context, playlistID string, items []string, resolve string, serverSelector string) error {
	server, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return err
	}
	entries, err := s.buildPlaylistEntries(ctx, items, resolve)
	if err != nil {
		return err
	}
	body := mu.PlaylistAddItemsBody{PlaylistID: playlistID, Entries: entries}
	cmd, err := mu.NewCommand("playlist.addItems", body)
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	return s.publishSimple(ctx, server.NodeID, cmd)
}

// PlaylistRemove removes items from a playlist.
func (s Service) PlaylistRemove(ctx context.Context, playlistID string, entryIDs []string, serverSelector string) error {
	server, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return err
	}
	body := mu.PlaylistRemoveItemsBody{PlaylistID: playlistID, EntryIDs: entryIDs}
	cmd, err := mu.NewCommand("playlist.removeItems", body)
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	return s.publishSimple(ctx, server.NodeID, cmd)
}

// PlaylistRename renames a playlist.
func (s Service) PlaylistRename(ctx context.Context, playlistID string, name string, serverSelector string) error {
	server, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return err
	}
	body := mu.PlaylistRenameBody{PlaylistID: playlistID, Name: name}
	cmd, err := mu.NewCommand("playlist.rename", body)
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	return s.publishSimple(ctx, server.NodeID, cmd)
}

// SnapshotSave captures the current session queue into a snapshot.
func (s Service) SnapshotSave(ctx context.Context, selector string, name string, serverSelector string) error {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return err
	}
	lease, err := s.lookupLease(renderer.NodeID)
	if err != nil {
		return err
	}
	state, err := s.Broker.GetRendererState(ctx, renderer.NodeID)
	if err != nil {
		return WrapError(ExitRuntime, "get renderer state", err)
	}
	if state.Session == nil || state.Queue == nil || state.Playback == nil {
		return &CLIError{Code: ExitRuntime, Msg: "renderer state incomplete"}
	}
	server, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return err
	}
	body := mu.SnapshotSaveBody{
		Name:       name,
		RendererID: renderer.NodeID,
		SessionID:  state.Session.ID,
		Capture: mu.SnapshotCapture{
			QueueRevision: state.Queue.Revision,
			Index:         state.Queue.Index,
			PositionMS:    state.Playback.PositionMS,
			Repeat:        false,
			Shuffle:       false,
		},
	}
	cmd, err := mu.NewCommand("snapshot.save", body)
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, &lease, nil)
	return s.publishSimple(ctx, server.NodeID, cmd)
}

// SnapshotList lists snapshots on the playlist server.
func (s Service) SnapshotList(ctx context.Context, serverSelector string) (SnapshotListResult, error) {
	server, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return SnapshotListResult{}, err
	}
	cmd, err := mu.NewCommand("snapshot.list", mu.SnapshotListBody{Owner: s.Config.Identity})
	if err != nil {
		return SnapshotListResult{}, WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	reply, err := s.Broker.PublishCommand(ctx, server.NodeID, cmd)
	if err != nil {
		return SnapshotListResult{}, WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return SnapshotListResult{}, ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}
	var body mu.SnapshotListReply
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		return SnapshotListResult{}, WrapError(ExitRuntime, "decode snapshot reply", err)
	}
	return SnapshotListResult{Snapshots: body.Snapshots}, nil
}

// LibraryList returns library nodes.
func (s Service) LibraryList(ctx context.Context) (NodesResult, error) {
	return s.ListNodes(ctx, "library", false)
}

// LibraryBrowse sends library.browse.
func (s Service) LibraryBrowse(ctx context.Context, selector string, containerID string, start, count int64) (RawResult, error) {
	library, err := s.Resolver.ResolveLibrary(ctx, selector)
	if err != nil {
		return RawResult{}, err
	}
	cmd, err := mu.NewCommand("library.browse", mu.LibraryBrowseBody{ContainerID: containerID, Start: start, Count: count})
	if err != nil {
		return RawResult{}, WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	reply, err := s.Broker.PublishCommand(ctx, library.NodeID, cmd)
	if err != nil {
		return RawResult{}, WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return RawResult{}, ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}
	return RawResult{Data: json.RawMessage(reply.Body)}, nil
}

// LibrarySearch sends library.search.
func (s Service) LibrarySearch(ctx context.Context, selector string, query string, start, count int64) (RawResult, error) {
	library, err := s.Resolver.ResolveLibrary(ctx, selector)
	if err != nil {
		return RawResult{}, err
	}
	cmd, err := mu.NewCommand("library.search", mu.LibrarySearchBody{Query: query, Start: start, Count: count})
	if err != nil {
		return RawResult{}, WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	reply, err := s.Broker.PublishCommand(ctx, library.NodeID, cmd)
	if err != nil {
		return RawResult{}, WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return RawResult{}, ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}
	return RawResult{Data: json.RawMessage(reply.Body)}, nil
}

// LibraryResolve sends library.resolve.
func (s Service) LibraryResolve(ctx context.Context, selector string, itemID string) (LibraryResolveResult, error) {
	library, err := s.Resolver.ResolveLibrary(ctx, selector)
	if err != nil {
		return LibraryResolveResult{}, err
	}
	cmd, err := mu.NewCommand("library.resolve", mu.LibraryResolveBody{ItemID: itemID})
	if err != nil {
		return LibraryResolveResult{}, WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	reply, err := s.Broker.PublishCommand(ctx, library.NodeID, cmd)
	if err != nil {
		return LibraryResolveResult{}, WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return LibraryResolveResult{}, ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}
	var body mu.LibraryResolveReply
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		return LibraryResolveResult{}, WrapError(ExitRuntime, "decode library reply", err)
	}
	return LibraryResolveResult{Item: body}, nil
}

// SuggestList lists suggestions on the playlist server.
func (s Service) SuggestList(ctx context.Context, serverSelector string) (SuggestListResult, error) {
	server, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return SuggestListResult{}, err
	}
	cmd, err := mu.NewCommand("suggest.list", mu.SuggestListBody{Owner: s.Config.Identity})
	if err != nil {
		return SuggestListResult{}, WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	reply, err := s.Broker.PublishCommand(ctx, server.NodeID, cmd)
	if err != nil {
		return SuggestListResult{}, WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return SuggestListResult{}, ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}
	var body mu.SuggestListReply
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		return SuggestListResult{}, WrapError(ExitRuntime, "decode suggest reply", err)
	}
	return SuggestListResult{Suggestions: body.Suggestions}, nil
}

// SuggestShow fetches a suggestion.
func (s Service) SuggestShow(ctx context.Context, suggestionID string, serverSelector string) (RawResult, error) {
	server, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return RawResult{}, err
	}
	cmd, err := mu.NewCommand("suggest.get", mu.SuggestGetBody{SuggestionID: suggestionID})
	if err != nil {
		return RawResult{}, WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	reply, err := s.Broker.PublishCommand(ctx, server.NodeID, cmd)
	if err != nil {
		return RawResult{}, WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return RawResult{}, ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}
	return RawResult{Data: json.RawMessage(reply.Body)}, nil
}

// SuggestPromote promotes a suggestion to a playlist.
func (s Service) SuggestPromote(ctx context.Context, suggestionID string, name string, serverSelector string) error {
	server, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return err
	}
	body := mu.SuggestPromoteBody{SuggestionID: suggestionID, Name: name}
	cmd, err := mu.NewCommand("suggest.promote", body)
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, nil, nil)
	return s.publishSimple(ctx, server.NodeID, cmd)
}

// SuggestLoad loads a suggestion into the renderer queue.
func (s Service) SuggestLoad(ctx context.Context, selector string, suggestionID string, mode string, resolve string, serverSelector string) error {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return err
	}
	lease, err := s.lookupLease(renderer.NodeID)
	if err != nil {
		return err
	}
	playlistServer, err := s.Resolver.ResolvePlaylistServer(ctx, serverSelector)
	if err != nil {
		return err
	}
	body := mu.QueueLoadSuggestionBody{
		PlaylistServerID: playlistServer.NodeID,
		SuggestionID:     suggestionID,
		Mode:             mode,
		Resolve:          resolve,
	}
	cmd, err := mu.NewCommand("queue.loadSuggestion", body)
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, &lease, nil)
	return s.publishSimple(ctx, renderer.NodeID, cmd)
}

func (s Service) simplePlayback(ctx context.Context, selector string, cmdType string, body any) error {
	renderer, err := s.Resolver.ResolveRenderer(ctx, selector)
	if err != nil {
		return err
	}
	lease, err := s.lookupLease(renderer.NodeID)
	if err != nil {
		return err
	}
	cmd, err := mu.NewCommand(cmdType, body)
	if err != nil {
		return WrapError(ExitRuntime, "build command", err)
	}
	cmd = s.decorateCommand(cmd, &lease, nil)
	return s.publishSimple(ctx, renderer.NodeID, cmd)
}

func (s Service) publishSimple(ctx context.Context, nodeID string, cmd mu.CommandEnvelope) error {
	reply, err := s.Broker.PublishCommand(ctx, nodeID, cmd)
	if err != nil {
		return WrapError(ExitRuntime, "publish command", err)
	}
	if reply.Err != nil {
		return ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
	}
	return nil
}

func (s Service) decorateCommand(cmd mu.CommandEnvelope, lease *mu.Lease, ifRev *int64) mu.CommandEnvelope {
	cmd.ID = s.IDGen.NewID()
	cmd.TS = s.Clock.NowUnix()
	cmd.From = s.Config.Identity
	cmd.ReplyTo = s.Broker.ReplyTopic()
	cmd.Lease = lease
	cmd.IfRevision = ifRev
	return cmd
}

func (s Service) lookupLease(rendererID string) (mu.Lease, error) {
	lease, ok, err := s.LeaseStore.Get(rendererID)
	if err != nil {
		return mu.Lease{}, WrapError(ExitRuntime, "load lease", err)
	}
	if !ok {
		return mu.Lease{}, &CLIError{Code: ExitLease, Msg: "lease required: run 'mu acquire <renderer>'"}
	}
	return lease, nil
}

func (s Service) resolveSeekPosition(ctx context.Context, selector string, arg string) (int64, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 0, &CLIError{Code: ExitUsage, Msg: "seek position required"}
	}

	if strings.HasPrefix(arg, "+") || strings.HasPrefix(arg, "-") {
		delta, err := parseDurationToMS(arg)
		if err != nil {
			return 0, err
		}
		status, err := s.Status(ctx, selector)
		if err != nil {
			return 0, err
		}
		if status.State.Playback == nil {
			return 0, &CLIError{Code: ExitRuntime, Msg: "no playback state"}
		}
		pos := status.State.Playback.PositionMS + delta
		if pos < 0 {
			pos = 0
		}
		return pos, nil
	}
	return parseDurationToMS(arg)
}

func (s Service) resolveVolume(ctx context.Context, rendererID string, arg string) (float64, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 0, &CLIError{Code: ExitUsage, Msg: "volume argument required"}
	}

	if strings.HasPrefix(arg, "+") || strings.HasPrefix(arg, "-") {
		delta, err := strconv.ParseFloat(arg, 64)
		if err != nil {
			return 0, &CLIError{Code: ExitUsage, Msg: "invalid volume delta"}
		}
		state, err := s.Broker.GetRendererState(ctx, rendererID)
		if err != nil {
			return 0, WrapError(ExitRuntime, "get renderer state", err)
		}
		if state.Playback == nil {
			return 0, &CLIError{Code: ExitRuntime, Msg: "no playback state"}
		}
		current := state.Playback.Volume * 100
		return clampVolume((current + delta) / 100), nil
	}

	value, err := strconv.ParseFloat(arg, 64)
	if err != nil {
		return 0, &CLIError{Code: ExitUsage, Msg: "invalid volume"}
	}
	return clampVolume(value / 100), nil
}

func clampVolume(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func parseDurationToMS(arg string) (int64, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 0, &CLIError{Code: ExitUsage, Msg: "duration required"}
	}
	if strings.HasSuffix(arg, "ms") || strings.HasSuffix(arg, "s") || strings.HasSuffix(arg, "m") || strings.HasSuffix(arg, "h") {
		dur, err := time.ParseDuration(arg)
		if err != nil {
			return 0, &CLIError{Code: ExitUsage, Msg: "invalid duration"}
		}
		return int64(dur / time.Millisecond), nil
	}
	value, err := strconv.ParseInt(arg, 10, 64)
	if err != nil {
		return 0, &CLIError{Code: ExitUsage, Msg: "invalid duration"}
	}
	return value, nil
}

func (s Service) buildQueueEntries(ctx context.Context, renderer mu.Presence, items []string, resolve string) ([]mu.QueueEntry, error) {
	entries := make([]mu.QueueEntry, 0, len(items))
	needsResolve := resolve == "yes"
	if resolve == "auto" {
		needsResolve = !rendererCanResolve(renderer)
	}
	if resolve == "no" && !rendererCanResolve(renderer) {
		return nil, &CLIError{Code: ExitUsage, Msg: "renderer cannot resolve refs; use --resolve=auto|yes"}
	}

	for _, item := range items {
		entry, err := s.parseQueueItem(ctx, item, needsResolve)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s Service) buildPlaylistEntries(ctx context.Context, items []string, resolve string) ([]mu.QueueEntry, error) {
	entries := make([]mu.QueueEntry, 0, len(items))
	needsResolve := resolve == "yes"
	for _, item := range items {
		entry, err := s.parseQueueItem(ctx, item, needsResolve)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s Service) parseQueueItem(ctx context.Context, item string, resolve bool) (mu.QueueEntry, error) {
	item = strings.TrimSpace(item)
	if item == "" {
		return mu.QueueEntry{}, &CLIError{Code: ExitUsage, Msg: "empty item"}
	}
	if strings.HasPrefix(item, "http://") || strings.HasPrefix(item, "https://") {
		return mu.QueueEntry{Resolved: &mu.ResolvedSource{URL: item, ByteRange: false}}, nil
	}
	if strings.HasPrefix(item, "mu:") {
		return mu.QueueEntry{Ref: &mu.ItemRef{ID: item}}, nil
	}
	if strings.HasPrefix(item, "lib:") {
		parts := strings.SplitN(strings.TrimPrefix(item, "lib:"), ":", 2)
		if len(parts) != 2 {
			return mu.QueueEntry{}, &CLIError{Code: ExitUsage, Msg: "invalid library item (expected lib:<alias>:<id>)"}
		}
		if !resolve {
			return mu.QueueEntry{Ref: &mu.ItemRef{ID: parts[1]}}, nil
		}
		lib, err := s.Resolver.ResolveLibrary(ctx, parts[0])
		if err != nil {
			return mu.QueueEntry{}, err
		}
		cmd, err := mu.NewCommand("library.resolve", mu.LibraryResolveBody{ItemID: parts[1]})
		if err != nil {
			return mu.QueueEntry{}, WrapError(ExitRuntime, "build command", err)
		}
		cmd = s.decorateCommand(cmd, nil, nil)
		reply, err := s.Broker.PublishCommand(ctx, lib.NodeID, cmd)
		if err != nil {
			return mu.QueueEntry{}, WrapError(ExitRuntime, "publish command", err)
		}
		if reply.Err != nil {
			return mu.QueueEntry{}, ErrorForReplyCode(reply.Err.Code, reply.Err.Message)
		}
		var body mu.LibraryResolveReply
		if err := json.Unmarshal(reply.Body, &body); err != nil {
			return mu.QueueEntry{}, WrapError(ExitRuntime, "decode library reply", err)
		}
		if len(body.Sources) == 0 {
			return mu.QueueEntry{}, &CLIError{Code: ExitNotFound, Msg: "no sources returned"}
		}
		src := body.Sources[0]
		return mu.QueueEntry{Resolved: &mu.ResolvedSource{URL: src.URL, Mime: src.Mime, ByteRange: src.ByteRange}}, nil
	}
	if strings.HasPrefix(item, "playlist:") {
		return mu.QueueEntry{}, &CLIError{Code: ExitUsage, Msg: "playlist references require playlist load (not supported in queue add)"}
	}
	return mu.QueueEntry{}, &CLIError{Code: ExitUsage, Msg: fmt.Sprintf("unsupported item: %s", item)}
}

func rendererCanResolve(renderer mu.Presence) bool {
	if renderer.Caps == nil {
		return false
	}
	val, ok := renderer.Caps["queueResolve"]
	if !ok {
		return false
	}
	b, ok := val.(bool)
	if !ok {
		return false
	}
	return b
}

func (s Service) parseQueueFile(format string, data []byte) ([]mu.QueueEntry, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" || format == "muq" {
		lines := strings.Split(string(data), "\n")
		items := make([]string, 0, len(lines))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			items = append(items, line)
		}
		entries := make([]mu.QueueEntry, 0, len(items))
		for _, item := range items {
			entry, err := s.parseQueueItem(context.Background(), item, false)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
		return entries, nil
	}
	if format == "json" {
		var entries []mu.QueueEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, &CLIError{Code: ExitUsage, Msg: "invalid json queue file"}
		}
		return entries, nil
	}
	return nil, &CLIError{Code: ExitUsage, Msg: "unsupported queue file format"}
}

// QueueEntriesFromFile parses queue entries from a file payload.
func (s Service) QueueEntriesFromFile(format string, data []byte) ([]mu.QueueEntry, error) {
	return s.parseQueueFile(format, data)
}
