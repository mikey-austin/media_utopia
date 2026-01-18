package mu

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
	Revision int64       `json:"revision"`
	Index    int64       `json:"index"`
	Entries  []QueueItem `json:"entries"`
}

// QueueItem is an entry returned by queue.get.
type QueueItem struct {
	QueueEntryID string                 `json:"queueEntryId"`
	ItemID       string                 `json:"itemId"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
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

// QueueEntry is an entry reference or resolved source.
type QueueEntry struct {
	Ref      *ItemRef        `json:"ref,omitempty"`
	Resolved *ResolvedSource `json:"resolved,omitempty"`
}

// ItemRef is a reference to a library item.
type ItemRef struct {
	ID string `json:"id"`
}

// ResolvedSource is a fully resolved source reference.
type ResolvedSource struct {
	ItemID    string `json:"itemId,omitempty"`
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
	PlaylistID string   `json:"playlistId"`
	Items      []string `json:"items"` // Array of itemIds to replace all entries
}

// SnapshotSaveBody is the payload for snapshot.save.
type SnapshotSaveBody struct {
	Name       string          `json:"name"`
	RendererID string          `json:"rendererId"`
	SessionID  string          `json:"sessionId"`
	Capture    SnapshotCapture `json:"capture"`
	Items      []string        `json:"items,omitempty"`
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
	Items      []string        `json:"items,omitempty"`
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

// LibraryResolveBody is the payload for library.resolve.
type LibraryResolveBody struct {
	ItemID       string `json:"itemId"`
	MetadataOnly bool   `json:"metadataOnly,omitempty"`
}

// LibraryResolveReply describes the response for library.resolve.
type LibraryResolveReply struct {
	ItemID   string           `json:"itemId"`
	Metadata map[string]any   `json:"metadata,omitempty"`
	Sources  []ResolvedSource `json:"sources"`
}

// LibraryResolveBatchBody is the payload for library.resolveBatch.
type LibraryResolveBatchBody struct {
	ItemIDs      []string `json:"itemIds"`
	MetadataOnly bool     `json:"metadataOnly,omitempty"`
}

// LibraryResolveBatchItem is an item entry in library.resolveBatch reply.
type LibraryResolveBatchItem struct {
	ItemID   string           `json:"itemId"`
	Metadata map[string]any   `json:"metadata,omitempty"`
	Sources  []ResolvedSource `json:"sources,omitempty"`
	Err      *ReplyError      `json:"err,omitempty"`
}

// LibraryResolveBatchReply describes the response for library.resolveBatch.
type LibraryResolveBatchReply struct {
	Items []LibraryResolveBatchItem `json:"items"`
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
