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

// SnapshotListResult holds snapshot summaries.
type SnapshotListResult struct {
	Snapshots []mu.SnapshotSummary
}

// SuggestListResult holds suggestion summaries.
type SuggestListResult struct {
	Suggestions []mu.SuggestSummary
}

// LibraryResolveResult holds a resolved library item.
type LibraryResolveResult struct {
	Item mu.LibraryResolveReply
}

// RawResult holds arbitrary JSON data for output.
type RawResult struct {
	Data any
}
