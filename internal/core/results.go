package core

import "github.com/mikey-austin/media_utopia/pkg/mu"

// NodesResult holds a list of presence records.
type NodesResult struct {
	Nodes []mu.Presence
}

// StatusResult holds renderer presence and state.
type StatusResult struct {
	Renderer mu.Presence
	State    mu.RendererState
}

// SessionResult reports session acquisition details.
type SessionResult struct {
	RendererID string
	Session    mu.SessionLease
	StateVer   int64
}

// QueueResult holds a queue listing.
type QueueResult struct {
	RendererID string
	Queue      mu.QueueGetReply
	FullIDs    bool
}

// QueueNowResult shows the current queue item.
type QueueNowResult struct {
	RendererID string
	Current    *mu.CurrentItemState
}

// PlaylistListResult holds playlist summaries.
type PlaylistListResult struct {
	Playlists []mu.PlaylistSummary
}

// PlaylistShowResult holds a playlist and resolved entry metadata.
type PlaylistShowResult struct {
	PlaylistID string
	Name       string
	Entries    []PlaylistEntryResult
	FullIDs    bool
}

// PlaylistEntryResult describes a playlist entry with its canonical ref,
// optional resolved source, and a display metadata snapshot.
type PlaylistEntryResult struct {
	EntryID  string
	Ref      *mu.LibraryItemRef
	Resolved *mu.ResolvedSource
	Display  *mu.DisplayMetadata
}

// SnapshotListResult holds snapshot summaries.
type SnapshotListResult struct {
	Snapshots []mu.SnapshotSummary
}

// SuggestListResult holds suggestion summaries.
type SuggestListResult struct {
	Suggestions []mu.SuggestSummary
}

// LibraryResolveResult holds the metadata reply for a library item,
// plus an optional resolved sources reply when the caller asked to include them.
type LibraryResolveResult struct {
	Item    mu.LibraryGetItemReply
	Sources *mu.LibraryResolveSourcesReply
}

// LibraryRescanResult holds the result of a library rescan.
type LibraryRescanResult struct {
	LibraryID string `json:"-"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Items     int    `json:"items,omitempty"`
}

// RawResult holds arbitrary JSON data for output.
type RawResult struct {
	Data any
}
