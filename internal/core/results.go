package core

import (
	"encoding/json"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// NodesResult holds a list of presence records.
type NodesResult struct {
	Nodes []mu.Presence `json:"nodes"`
}

// StatusResult holds renderer presence and state.
type StatusResult struct {
	Renderer mu.Presence      `json:"renderer"`
	State    mu.RendererState `json:"state"`
}

// SessionResult reports session acquisition details.
type SessionResult struct {
	RendererID string          `json:"rendererId"`
	Session    mu.SessionLease `json:"session"`
	StateVer   int64           `json:"stateVersion"`
}

// QueueResult holds a queue listing.
type QueueResult struct {
	RendererID string           `json:"rendererId"`
	Queue      mu.QueueGetReply `json:"queue"`
	FullIDs    bool             `json:"-"`
}

// QueueNowResult shows the current queue item.
type QueueNowResult struct {
	RendererID string               `json:"rendererId"`
	Current    *mu.CurrentItemState `json:"current"`
}

// PlaylistListResult holds playlist summaries.
type PlaylistListResult struct {
	Playlists []mu.PlaylistSummary `json:"playlists"`
}

// PlaylistShowResult holds a playlist and resolved entry metadata.
type PlaylistShowResult struct {
	PlaylistID string                `json:"playlistId"`
	Name       string                `json:"name"`
	Entries    []PlaylistEntryResult `json:"entries"`
	FullIDs    bool                  `json:"-"`
}

// PlaylistEntryResult describes a playlist entry with its canonical ref,
// optional resolved source, and a display metadata snapshot.
type PlaylistEntryResult struct {
	EntryID  string              `json:"entryId,omitempty"`
	Ref      *mu.LibraryItemRef  `json:"ref,omitempty"`
	Resolved *mu.ResolvedSource  `json:"resolved,omitempty"`
	Display  *mu.DisplayMetadata `json:"display,omitempty"`
}

// SnapshotListResult holds snapshot summaries.
type SnapshotListResult struct {
	Snapshots []mu.SnapshotSummary `json:"snapshots"`
}

// SuggestListResult holds suggestion summaries.
type SuggestListResult struct {
	Suggestions []mu.SuggestSummary `json:"suggestions"`
}

// LibraryResolveResult holds the metadata reply for a library item,
// plus an optional resolved sources reply when the caller asked to include them.
type LibraryResolveResult struct {
	Item    mu.LibraryGetItemReply         `json:"item"`
	Sources *mu.LibraryResolveSourcesReply `json:"sources,omitempty"`
}

// LibraryRescanResult holds the result of a library rescan.
type LibraryRescanResult struct {
	LibraryID string `json:"-"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Items     int    `json:"items,omitempty"`
}

// LibraryImportResult acknowledges a started import job.
type LibraryImportResult struct {
	LibraryID string `json:"libraryId"`
	JobID     string `json:"jobId"`
	Status    string `json:"status"`
}

// ImportJobStatus mirrors one import job as reported by the library.
type ImportJobStatus struct {
	JobID      string `json:"jobId"`
	URL        string `json:"url"`
	Playlist   string `json:"playlist,omitempty"`
	State      string `json:"state"`
	Done       int    `json:"done"`
	Skipped    int    `json:"skipped"`
	Failed     int    `json:"failed"`
	Total      int    `json:"total"`
	StartedAt  int64  `json:"startedAt,omitempty"`
	FinishedAt int64  `json:"finishedAt,omitempty"`
	Error      string `json:"error,omitempty"`
}

// LibraryImportsResult lists import jobs, newest first.
type LibraryImportsResult struct {
	LibraryID string            `json:"libraryId"`
	Jobs      []ImportJobStatus `json:"jobs"`
}

// ZoneStatus pairs a zone's presence with its live state.
type ZoneStatus struct {
	Zone       mu.Presence  `json:"zone"`
	State      mu.ZoneState `json:"state"`
	SourceName string       `json:"sourceName,omitempty"`
}

// ZoneListResult holds all zones plus the controller's selectable sources.
type ZoneListResult struct {
	Zones   []ZoneStatus    `json:"zones"`
	Sources []mu.ZoneSource `json:"sources,omitempty"`
}

// RawResult holds arbitrary JSON data for output. It marshals as the
// payload itself — scripts should not have to unwrap an envelope.
type RawResult struct {
	Data any
}

// MarshalJSON inlines the payload.
func (r RawResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.Data)
}
