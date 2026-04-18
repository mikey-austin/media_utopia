package mu

import (
	"errors"
	"fmt"
)

// SessionAcquireBody is the payload for session.acquire.
type SessionAcquireBody struct {
	TTLMS int64 `json:"ttlMs"`
}

// SessionRenewBody is the payload for session.renew.
type SessionRenewBody struct {
	TTLMS int64 `json:"ttlMs"`
}

// SessionReplyBody is returned by session.acquire.
type SessionReplyBody struct {
	Session      SessionLease `json:"session"`
	StateVersion int64        `json:"stateVersion"`
}

// SessionLease describes a session lease response.
type SessionLease struct {
	ID             string `json:"id"`
	Token          string `json:"token"`
	Owner          string `json:"owner"`
	LeaseExpiresAt int64  `json:"leaseExpiresAt"`
}

// PlaybackPlayBody is the payload for playback.play.
type PlaybackPlayBody struct {
	Index *int64 `json:"index,omitempty"`
}

// PlaybackSeekBody is the payload for playback.seek.
type PlaybackSeekBody struct {
	PositionMS int64 `json:"positionMs"`
}

// PlaybackSetVolumeBody is the payload for playback.setVolume.
type PlaybackSetVolumeBody struct {
	Volume float64 `json:"volume"`
}

// PlaybackSetMuteBody is the payload for playback.setMute.
type PlaybackSetMuteBody struct {
	Mute bool `json:"mute"`
}

// QueueGetBody fetches queue entries.
type QueueGetBody struct {
	From    int64  `json:"from"`
	Count   int64  `json:"count"`
	Resolve string `json:"resolve,omitempty"`
}

// QueueGetReply is the reply body for queue.get.
type QueueGetReply struct {
	Revision int64        `json:"revision"`
	Index    int64        `json:"index"`
	Entries  []QueueEntry `json:"entries"`
}

// QueueEntry is the canonical wire representation of a queue position.
// Either Ref or Resolved (or both) must be set. Display is a denormalized
// UI snapshot included whenever known so controllers do not need to issue
// follow-up library catalog lookups for ordinary rendering.
type QueueEntry struct {
	QueueEntryID string           `json:"queueEntryId,omitempty"`
	Ref          *LibraryItemRef  `json:"ref,omitempty"`
	Resolved     *ResolvedSource  `json:"resolved,omitempty"`
	Display      *DisplayMetadata `json:"display,omitempty"`
}

// Validate reports whether a QueueEntry is well formed for wire transport.
func (e QueueEntry) Validate() error {
	if e.Ref == nil && e.Resolved == nil {
		return errors.New("queue entry requires ref or resolved")
	}
	if e.Ref != nil {
		if err := e.Ref.Validate(); err != nil {
			return fmt.Errorf("queue entry ref: %w", err)
		}
	}
	if e.Resolved != nil && e.Resolved.URL == "" {
		return errors.New("queue entry resolved.url is required")
	}
	return nil
}

// QueueSetBody replaces the queue atomically.
type QueueSetBody struct {
	StartIndex int64        `json:"startIndex"`
	Entries    []QueueEntry `json:"entries"`
}

// QueueAddBody adds entries to the queue.
type QueueAddBody struct {
	Position string       `json:"position"`
	AtIndex  *int64       `json:"atIndex,omitempty"`
	Entries  []QueueEntry `json:"entries"`
}

// QueueRemoveBody removes an entry by id or index.
type QueueRemoveBody struct {
	QueueEntryID string `json:"queueEntryId,omitempty"`
	Index        *int64 `json:"index,omitempty"`
}

// QueueMoveBody moves a queue entry.
type QueueMoveBody struct {
	FromIndex int64 `json:"fromIndex"`
	ToIndex   int64 `json:"toIndex"`
}

// QueueJumpBody jumps to an index.
type QueueJumpBody struct {
	Index int64 `json:"index"`
}

// QueueShuffleBody shuffles the queue.
type QueueShuffleBody struct {
	Seed int64 `json:"seed"`
}

// QueueSetShuffleBody toggles shuffle mode.
type QueueSetShuffleBody struct {
	Shuffle bool `json:"shuffle"`
}

// QueueRepeatBody sets repeat mode.
type QueueRepeatBody struct {
	Repeat bool   `json:"repeat"`
	Mode   string `json:"mode,omitempty"`
}

// QueueLoadPlaylistBody loads a playlist into the queue.
type QueueLoadPlaylistBody struct {
	PlaylistServerID string `json:"playlistServerId"`
	PlaylistID       string `json:"playlistId"`
	Mode             string `json:"mode"`
	Resolve          string `json:"resolve"`
}

// QueueLoadSnapshotBody loads a snapshot into the queue.
type QueueLoadSnapshotBody struct {
	PlaylistServerID string `json:"playlistServerId"`
	SnapshotID       string `json:"snapshotId"`
	Mode             string `json:"mode"`
	Resolve          string `json:"resolve"`
}

// QueueLoadSuggestionBody loads a suggestion into the queue.
type QueueLoadSuggestionBody struct {
	PlaylistServerID string `json:"playlistServerId"`
	SuggestionID     string `json:"suggestionId"`
	Mode             string `json:"mode"`
	Resolve          string `json:"resolve"`
}

// ResolvedSource is a fully resolved playable source.
type ResolvedSource struct {
	URL       string `json:"url"`
	Mime      string `json:"mime,omitempty"`
	ByteRange bool   `json:"byteRange"`
}

// PlaylistListBody is the payload for playlist.list.
type PlaylistListBody struct {
	Owner string `json:"owner"`
}

// PlaylistListReply is the reply for playlist.list.
type PlaylistListReply struct {
	Playlists []PlaylistSummary `json:"playlists"`
}

// PlaylistSummary is a playlist summary.
type PlaylistSummary struct {
	PlaylistID string `json:"playlistId"`
	Name       string `json:"name"`
	Revision   int64  `json:"revision"`
}

// PlaylistCreateBody is the payload for playlist.create.
type PlaylistCreateBody struct {
	Name       string `json:"name"`
	SnapshotID string `json:"snapshotId,omitempty"`
}

// PlaylistGetBody is the payload for playlist.get.
type PlaylistGetBody struct {
	PlaylistID string `json:"playlistId"`
}

// PlaylistAddItemsBody is the payload for playlist.addItems.
type PlaylistAddItemsBody struct {
	PlaylistID string       `json:"playlistId"`
	Entries    []QueueEntry `json:"entries"`
}

// PlaylistRemoveItemsBody is the payload for playlist.removeItems.
type PlaylistRemoveItemsBody struct {
	PlaylistID string   `json:"playlistId"`
	EntryIDs   []string `json:"entryIds"`
}

// PlaylistRenameBody is the payload for playlist.rename.
type PlaylistRenameBody struct {
	PlaylistID string `json:"playlistId"`
	Name       string `json:"name"`
}

// PlaylistDeleteBody is the payload for playlist.delete.
type PlaylistDeleteBody struct {
	PlaylistID string `json:"playlistId"`
}

// PlaylistReplaceItemsBody is the payload for playlist.replaceItems.
type PlaylistReplaceItemsBody struct {
	PlaylistID string       `json:"playlistId"`
	Entries    []QueueEntry `json:"entries"`
}

// SnapshotSaveBody is the payload for snapshot.save.
type SnapshotSaveBody struct {
	Name       string          `json:"name"`
	RendererID string          `json:"rendererId"`
	SessionID  string          `json:"sessionId"`
	Capture    SnapshotCapture `json:"capture"`
	Entries    []QueueEntry    `json:"entries,omitempty"`
}

// SnapshotCapture is the session queue capture.
type SnapshotCapture struct {
	QueueRevision int64  `json:"queueRevision"`
	Index         int64  `json:"index"`
	PositionMS    int64  `json:"positionMs"`
	Repeat        bool   `json:"repeat"`
	RepeatMode    string `json:"repeatMode,omitempty"`
	Shuffle       bool   `json:"shuffle"`
}

// SnapshotListBody is the payload for snapshot.list.
type SnapshotListBody struct {
	Owner string `json:"owner,omitempty"`
}

// SnapshotListReply is the reply for snapshot.list.
type SnapshotListReply struct {
	Snapshots []SnapshotSummary `json:"snapshots"`
}

// SnapshotRemoveBody is the payload for snapshot.remove.
type SnapshotRemoveBody struct {
	SnapshotID string `json:"snapshotId"`
}

// SnapshotGetBody is the payload for snapshot.get.
type SnapshotGetBody struct {
	SnapshotID string `json:"snapshotId"`
}

// SnapshotGetReply is the reply for snapshot.get.
type SnapshotGetReply struct {
	SnapshotID string          `json:"snapshotId"`
	Name       string          `json:"name"`
	Revision   int64           `json:"revision"`
	Entries    []QueueEntry    `json:"entries,omitempty"`
	Capture    SnapshotCapture `json:"capture"`
}

// SnapshotSummary describes a saved snapshot.
type SnapshotSummary struct {
	SnapshotID string `json:"snapshotId"`
	Name       string `json:"name"`
	Revision   int64  `json:"revision,omitempty"`
}

// LibraryBrowseBody is the payload for library.browse.
type LibraryBrowseBody struct {
	ContainerID string `json:"containerId"`
	Start       int64  `json:"start"`
	Count       int64  `json:"count"`
}

// LibrarySearchBody is the payload for library.search.
type LibrarySearchBody struct {
	Query string   `json:"query"`
	Start int64    `json:"start"`
	Count int64    `json:"count"`
	Types []string `json:"types,omitempty"`
}

// LibraryGetItemBody is the payload for library.getItem (catalog metadata only).
type LibraryGetItemBody struct {
	Ref LibraryItemRef `json:"ref"`
}

// LibraryGetItemReply describes the response for library.getItem.
type LibraryGetItemReply struct {
	Ref        LibraryItemRef   `json:"ref"`
	Display    *DisplayMetadata `json:"display,omitempty"`
	Attributes map[string]any   `json:"attributes,omitempty"`
}

// LibraryGetItemsBody is the batch payload for library.getItems.
type LibraryGetItemsBody struct {
	Refs []LibraryItemRef `json:"refs"`
}

// LibraryGetItemsReplyItem is one entry in a library.getItems reply.
type LibraryGetItemsReplyItem struct {
	Ref        LibraryItemRef   `json:"ref"`
	Display    *DisplayMetadata `json:"display,omitempty"`
	Attributes map[string]any   `json:"attributes,omitempty"`
	Err        *ReplyError      `json:"err,omitempty"`
}

// LibraryGetItemsReply describes the response for library.getItems.
type LibraryGetItemsReply struct {
	Items []LibraryGetItemsReplyItem `json:"items"`
}

// LibraryResolveSourcesBody is the payload for library.resolveSources (playable urls only).
type LibraryResolveSourcesBody struct {
	Ref LibraryItemRef `json:"ref"`
}

// LibraryResolveSourcesReply describes the response for library.resolveSources.
type LibraryResolveSourcesReply struct {
	Ref     LibraryItemRef   `json:"ref"`
	Sources []ResolvedSource `json:"sources"`
}

// LibraryResolveSourcesBatchBody is the batch payload for library.resolveSourcesBatch.
type LibraryResolveSourcesBatchBody struct {
	Refs []LibraryItemRef `json:"refs"`
}

// LibraryResolveSourcesBatchItem is one entry in a library.resolveSourcesBatch reply.
type LibraryResolveSourcesBatchItem struct {
	Ref     LibraryItemRef   `json:"ref"`
	Sources []ResolvedSource `json:"sources,omitempty"`
	Err     *ReplyError      `json:"err,omitempty"`
}

// LibraryResolveSourcesBatchReply describes the response for library.resolveSourcesBatch.
type LibraryResolveSourcesBatchReply struct {
	Items []LibraryResolveSourcesBatchItem `json:"items"`
}

// SuggestListBody is the payload for suggest.list.
type SuggestListBody struct {
	Owner string `json:"owner,omitempty"`
}

// SuggestListReply is the reply for suggest.list.
type SuggestListReply struct {
	Suggestions []SuggestSummary `json:"suggestions"`
}

// SuggestSummary describes a suggestion.
type SuggestSummary struct {
	SuggestionID string `json:"suggestionId"`
	Name         string `json:"name"`
	Revision     int64  `json:"revision,omitempty"`
}

// SuggestGetBody is the payload for suggest.get.
type SuggestGetBody struct {
	SuggestionID string `json:"suggestionId"`
}

// SuggestPromoteBody is the payload for suggest.promote.
type SuggestPromoteBody struct {
	SuggestionID string `json:"suggestionId"`
	Name         string `json:"name"`
}
