package output

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mikey-austin/media_utopia/internal/core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

func TestRenderNodes(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.NodesResult
		contains []string
	}{
		{
			name:     "empty list",
			input:    core.NodesResult{Nodes: []mu.Presence{}},
			contains: []string{"No nodes found. Is the broker running?"},
		},
		{
			name: "single node",
			input: core.NodesResult{Nodes: []mu.Presence{
				{NodeID: "n1", Kind: "renderer", Name: "Living Room"},
			}},
			contains: []string{"NAME", "KIND", "NODE_ID", "Living Room", "renderer", "n1"},
		},
		{
			name: "multiple nodes",
			input: core.NodesResult{Nodes: []mu.Presence{
				{NodeID: "n1", Kind: "renderer", Name: "Living Room"},
				{NodeID: "n2", Kind: "library", Name: "Music Server"},
			}},
			contains: []string{"Living Room", "renderer", "n1", "Music Server", "library", "n2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderStatus(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.StatusResult
		contains []string
	}{
		{
			name: "playing with all fields",
			input: core.StatusResult{
				Renderer: mu.Presence{Name: "Living Room"},
				State: mu.RendererState{
					Playback: &mu.PlaybackState{
						Status:     "playing",
						PositionMS: 61000,
						DurationMS: 240000,
						Volume:     0.75,
					},
					Current: &mu.CurrentItemState{
						QueueEntryID: "qe-1",
						Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-1"},
						Display:      &mu.DisplayMetadata{Title: "My Song", Artist: "The Band"},
					},
					Queue: &mu.QueueState{
						Length:   10,
						Index:    3,
						Revision: 5,
					},
					Session: &mu.SessionState{
						Owner: "mu-cli",
					},
				},
			},
			contains: []string{"Living Room", "playing", "My Song", "The Band", "vol 75%", "Queue: 10 tracks", "owner mu-cli"},
		},
		{
			name: "paused state",
			input: core.StatusResult{
				Renderer: mu.Presence{Name: "Bedroom"},
				State: mu.RendererState{
					Playback: &mu.PlaybackState{
						Status:     "paused",
						PositionMS: 30000,
						DurationMS: 180000,
						Volume:     0.50,
					},
				},
			},
			contains: []string{"Bedroom", "paused", "vol 50%"},
		},
		{
			name: "stopped state",
			input: core.StatusResult{
				Renderer: mu.Presence{Name: "Kitchen"},
				State: mu.RendererState{
					Playback: &mu.PlaybackState{
						Status: "stopped",
						Volume: 0.0,
					},
				},
			},
			contains: []string{"Kitchen", "stopped", "vol 0%"},
		},
		{
			name: "unknown state",
			input: core.StatusResult{
				Renderer: mu.Presence{Name: "Patio"},
				State:    mu.RendererState{},
			},
			contains: []string{"Patio", "unknown"},
		},
		{
			name: "nil playback, current, queue, session",
			input: core.StatusResult{
				Renderer: mu.Presence{Name: "Garage"},
				State: mu.RendererState{
					Playback: nil,
					Current:  nil,
					Queue:    nil,
					Session:  nil,
				},
			},
			contains: []string{"Garage", "unknown"},
		},
		{
			name: "muted",
			input: core.StatusResult{
				Renderer: mu.Presence{Name: "Office"},
				State: mu.RendererState{
					Playback: &mu.PlaybackState{
						Status: "playing",
						Volume: 0.80,
						Mute:   true,
					},
				},
			},
			contains: []string{"Office", "muted"},
		},
		{
			name: "repeat mode one",
			input: core.StatusResult{
				Renderer: mu.Presence{Name: "Den"},
				State: mu.RendererState{
					Queue: &mu.QueueState{
						Length:     5,
						Index:     1,
						Revision:  2,
						RepeatMode: "one",
					},
				},
			},
			contains: []string{"Den", "repeat-one", "Queue: 5 tracks"},
		},
		{
			name: "repeat mode all",
			input: core.StatusResult{
				Renderer: mu.Presence{Name: "Studio"},
				State: mu.RendererState{
					Queue: &mu.QueueState{
						Length:   8,
						Index:    0,
						Revision: 1,
						Repeat:   true,
					},
				},
			},
			contains: []string{"Studio", "repeat", "Queue: 8 tracks"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderSession(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.SessionResult
		contains []string
	}{
		{
			name: "session with ID and expiry",
			input: core.SessionResult{
				RendererID: "r1",
				Session: mu.SessionLease{
					ID:             "sess-abc",
					Token:          "tok-123",
					Owner:          "mu-cli",
					LeaseExpiresAt: 1700000000,
				},
			},
			contains: []string{
				"Session:  sess-abc",
				"Expires:",
				time.Unix(1700000000, 0).Format(time.RFC3339),
				"Renderer: r1",
				"Owner:    mu-cli",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderQueue(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.QueueResult
		contains []string
		absent   []string
	}{
		{
			name: "empty queue",
			input: core.QueueResult{
				Queue: mu.QueueGetReply{Entries: []mu.QueueEntry{}},
			},
			contains: []string{"Queue is empty."},
			absent:   []string{"QUEUE_ID", "ITEM_ID"},
		},
		{
			name: "queue with entries and metadata",
			input: core.QueueResult{
				Queue: mu.QueueGetReply{
					Entries: []mu.QueueEntry{
						{
							QueueEntryID: "qe1",
							Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-1"},
							Display: &mu.DisplayMetadata{
								Title:      "Song One",
								MediaType:  "audio",
								Artist:     "Artist A",
								Album:      "Album X",
								DurationMS: 180000,
							},
						},
						{
							QueueEntryID: "qe2",
							Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-2"},
							Display: &mu.DisplayMetadata{
								Title:     "Song Two",
								MediaType: "audio",
								Artist:    "Artist B",
							},
						},
					},
				},
			},
			contains: []string{"Song One", "Artist A", "Album X", "audio", "3:00", "Song Two", "Artist B"},
			absent:   []string{"QUEUE_ID", "ITEM_ID"},
		},
		{
			name: "queue with FullIDs",
			input: core.QueueResult{
				FullIDs: true,
				Queue: mu.QueueGetReply{
					Entries: []mu.QueueEntry{
						{
							QueueEntryID: "qe-full-1",
							Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-full-1"},
							Display:      &mu.DisplayMetadata{Title: "Track"},
						},
					},
				},
			},
			contains: []string{"QUEUE_ID", "ITEM_ID", "qe-full-1", "item-full-1", "Track"},
		},
		{
			name: "queue with missing display fields",
			input: core.QueueResult{
				Queue: mu.QueueGetReply{
					Entries: []mu.QueueEntry{
						{
							QueueEntryID: "qe3",
							Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-3"},
						},
					},
				},
			},
			contains: []string{"item-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
			for _, nope := range tt.absent {
				if strings.Contains(out, nope) {
					t.Errorf("output should not contain %q\noutput:\n%s", nope, out)
				}
			}
		})
	}
}

func TestRenderQueueNow(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.QueueNowResult
		contains []string
	}{
		{
			name:     "nil current shows none",
			input:    core.QueueNowResult{Current: nil},
			contains: []string{"(none)"},
		},
		{
			name: "current with title and artist",
			input: core.QueueNowResult{
				Current: &mu.CurrentItemState{
					QueueEntryID: "qe-1",
					Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-1"},
					Display:      &mu.DisplayMetadata{Title: "Song Title", Artist: "Song Artist"},
				},
			},
			contains: []string{"Song Artist - Song Title"},
		},
		{
			name: "current with only title",
			input: core.QueueNowResult{
				Current: &mu.CurrentItemState{
					QueueEntryID: "qe-2",
					Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-2"},
					Display:      &mu.DisplayMetadata{Title: "Only Title"},
				},
			},
			contains: []string{"Only Title"},
		},
		{
			name: "current with only itemID",
			input: core.QueueNowResult{
				Current: &mu.CurrentItemState{
					QueueEntryID: "qe-fallback",
					Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-fallback"},
				},
			},
			contains: []string{"item-fallback"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderPlaylists(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.PlaylistListResult
		contains []string
	}{
		{
			name:     "empty list",
			input:    core.PlaylistListResult{Playlists: []mu.PlaylistSummary{}},
			contains: []string{"No playlists found."},
		},
		{
			name: "multiple playlists",
			input: core.PlaylistListResult{Playlists: []mu.PlaylistSummary{
				{PlaylistID: "pl-1", Name: "Chill Vibes", Revision: 3},
				{PlaylistID: "pl-2", Name: "Workout Mix", Revision: 7},
			}},
			contains: []string{"Chill Vibes", "pl-1", "3", "Workout Mix", "pl-2", "7"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderPlaylistShow(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.PlaylistShowResult
		contains []string
		absent   []string
	}{
		{
			name: "empty playlist",
			input: core.PlaylistShowResult{
				PlaylistID: "pl-0",
				Name:       "Empty Playlist",
				Entries:    []core.PlaylistEntryResult{},
			},
			contains: []string{"Playlist: Empty Playlist (0 tracks)"},
		},
		{
			name: "with display no FullIDs",
			input: core.PlaylistShowResult{
				PlaylistID: "pl-1",
				Name:       "Test Playlist",
				Entries: []core.PlaylistEntryResult{
					{
						EntryID: "e1",
						Ref:     &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "i1"},
						Display: &mu.DisplayMetadata{
							Title:      "Track One",
							MediaType:  "audio",
							Artist:     "Artist One",
							Album:      "Album One",
							DurationMS: 200000,
						},
					},
				},
			},
			contains: []string{"Playlist: Test Playlist (1 tracks)", "Track One", "audio", "Artist One", "Album One", "3:20"},
			absent:   []string{"ENTRY_ID"},
		},
		{
			name: "with FullIDs",
			input: core.PlaylistShowResult{
				PlaylistID: "pl-2",
				Name:       "Full IDs Playlist",
				FullIDs:    true,
				Entries: []core.PlaylistEntryResult{
					{
						EntryID: "entry-abc",
						Ref:     &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-xyz"},
						Display: &mu.DisplayMetadata{Title: "Full Track"},
					},
				},
			},
			contains: []string{"Playlist: Full IDs Playlist (1 tracks)", "ENTRY_ID", "ITEM_ID", "entry-abc", "item-xyz", "Full Track"},
		},
		{
			name: "with missing display",
			input: core.PlaylistShowResult{
				PlaylistID: "pl-3",
				Name:       "Sparse Playlist",
				Entries: []core.PlaylistEntryResult{
					{
						EntryID: "e3",
						Ref:     &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "i3"},
					},
				},
			},
			contains: []string{"Playlist: Sparse Playlist (1 tracks)", "i3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
			for _, nope := range tt.absent {
				if strings.Contains(out, nope) {
					t.Errorf("output should not contain %q\noutput:\n%s", nope, out)
				}
			}
		})
	}
}

func TestRenderSnapshots(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.SnapshotListResult
		contains []string
	}{
		{
			name:     "empty",
			input:    core.SnapshotListResult{Snapshots: []mu.SnapshotSummary{}},
			contains: []string{"No snapshots found."},
		},
		{
			name: "multiple snapshots",
			input: core.SnapshotListResult{Snapshots: []mu.SnapshotSummary{
				{SnapshotID: "snap-1", Name: "Morning Session", Revision: 1},
				{SnapshotID: "snap-2", Name: "Evening Session", Revision: 4},
			}},
			contains: []string{"Morning Session", "snap-1", "1", "Evening Session", "snap-2", "4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderSuggestions(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.SuggestListResult
		contains []string
	}{
		{
			name:     "empty",
			input:    core.SuggestListResult{Suggestions: []mu.SuggestSummary{}},
			contains: []string{"No suggestions available."},
		},
		{
			name: "multiple suggestions",
			input: core.SuggestListResult{Suggestions: []mu.SuggestSummary{
				{SuggestionID: "sug-1", Name: "Jazz Night", Revision: 2},
				{SuggestionID: "sug-2", Name: "Road Trip", Revision: 5},
			}},
			contains: []string{"Jazz Night", "sug-1", "2", "Road Trip", "sug-2", "5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderSuggestShow(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    SuggestShowOutput
		contains []string
	}{
		{
			name: "empty suggestion",
			input: SuggestShowOutput{
				Payload: json.RawMessage(`{"suggestionId":"sug-1","name":"Empty Mix","entries":[]}`),
			},
			contains: []string{"Suggestion: Empty Mix (0 tracks)", "No tracks."},
		},
		{
			name: "suggestion with tracks",
			input: SuggestShowOutput{
				Payload: json.RawMessage(`{
					"suggestionId": "sug-2",
					"name": "Jazz Night",
					"entries": [
						{
							"entryId": "e1",
							"ref": {"kind": "libraryItem", "libraryId": "mu:library:fs:default:music", "itemId": "item-1"},
							"display": {
								"title": "Blue in Green",
								"artist": "Miles Davis",
								"album": "Kind of Blue",
								"durationMs": 327000
							}
						},
						{
							"entryId": "e2",
							"ref": {"kind": "libraryItem", "libraryId": "mu:library:fs:default:music", "itemId": "item-2"},
							"display": {
								"title": "Take Five",
								"artist": "Dave Brubeck",
								"album": "Time Out",
								"durationMs": 324000
							}
						}
					]
				}`),
			},
			contains: []string{
				"Suggestion: Jazz Night (2 tracks)",
				"TITLE", "ARTIST", "ALBUM", "LEN",
				"Blue in Green", "Miles Davis", "Kind of Blue", "5:27",
				"Take Five", "Dave Brubeck", "Time Out", "5:24",
			},
		},
		{
			name: "suggestion with missing display",
			input: SuggestShowOutput{
				Payload: json.RawMessage(`{
					"suggestionId": "sug-3",
					"name": "Sparse Mix",
					"entries": [
						{
							"entryId": "e-bare",
							"ref": {"kind": "libraryItem", "libraryId": "mu:library:fs:default:music", "itemId": "item-no-meta"}
						}
					]
				}`),
			},
			contains: []string{
				"Suggestion: Sparse Mix (1 tracks)",
				"item-no-meta",
			},
		},
		{
			name: "invalid JSON falls back",
			input: SuggestShowOutput{
				Payload: json.RawMessage(`not valid json`),
			},
			contains: []string{"not valid json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderLibraryResolve(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.LibraryResolveResult
		contains []string
	}{
		{
			name: "metadata only (no sources requested)",
			input: core.LibraryResolveResult{
				Item: mu.LibraryGetItemReply{
					Ref: mu.NewLibraryItemRef("mu:library:fs:default:music", "item-1"),
				},
			},
			contains: []string{"Item: item-1", "Sources: (not requested"},
		},
		{
			name: "sources requested but empty",
			input: core.LibraryResolveResult{
				Item: mu.LibraryGetItemReply{
					Ref: mu.NewLibraryItemRef("mu:library:fs:default:music", "item-1b"),
				},
				Sources: &mu.LibraryResolveSourcesReply{
					Ref:     mu.NewLibraryItemRef("mu:library:fs:default:music", "item-1b"),
					Sources: []mu.ResolvedSource{},
				},
			},
			contains: []string{"Item: item-1b", "Sources: (none)"},
		},
		{
			name: "multiple sources",
			input: core.LibraryResolveResult{
				Item: mu.LibraryGetItemReply{
					Ref: mu.NewLibraryItemRef("mu:library:fs:default:music", "item-2"),
				},
				Sources: &mu.LibraryResolveSourcesReply{
					Ref: mu.NewLibraryItemRef("mu:library:fs:default:music", "item-2"),
					Sources: []mu.ResolvedSource{
						{URL: "http://example.com/a.flac", Mime: "audio/flac"},
						{URL: "http://example.com/b.mp3", Mime: "audio/mpeg"},
					},
				},
			},
			contains: []string{
				"Item: item-2",
				"Sources: (2)",
				"http://example.com/a.flac (audio/flac)",
				"http://example.com/b.mp3 (audio/mpeg)",
			},
		},
		{
			name: "with display metadata",
			input: core.LibraryResolveResult{
				Item: mu.LibraryGetItemReply{
					Ref: mu.NewLibraryItemRef("mu:library:fs:default:music", "item-3"),
					Display: &mu.DisplayMetadata{
						Title:      "Great Song",
						Artist:     "Cool Band",
						Album:      "Best Album",
						DurationMS: 240000,
					},
				},
				Sources: &mu.LibraryResolveSourcesReply{
					Ref: mu.NewLibraryItemRef("mu:library:fs:default:music", "item-3"),
					Sources: []mu.ResolvedSource{
						{URL: "http://example.com/c.flac", Mime: "audio/flac"},
					},
				},
			},
			contains: []string{
				"Item: Great Song",
				"Artist: Cool Band",
				"Album: Best Album",
				"Duration: 4:00",
				"Sources: (1)",
				"http://example.com/c.flac (audio/flac)",
			},
		},
		{
			name: "source with empty mime",
			input: core.LibraryResolveResult{
				Item: mu.LibraryGetItemReply{
					Ref: mu.NewLibraryItemRef("mu:library:fs:default:music", "item-4"),
				},
				Sources: &mu.LibraryResolveSourcesReply{
					Ref: mu.NewLibraryItemRef("mu:library:fs:default:music", "item-4"),
					Sources: []mu.ResolvedSource{
						{URL: "http://example.com/d.bin", Mime: ""},
					},
				},
			},
			contains: []string{
				"Item: item-4",
				"Sources: (1)",
				"http://example.com/d.bin (unknown)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderLibraryRescan(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.LibraryRescanResult
		contains []string
	}{
		{
			name: "started status",
			input: core.LibraryRescanResult{
				Status:  "started",
				Message: "scan queued",
			},
			contains: []string{"rescan started: scan queued"},
		},
		{
			name: "complete status",
			input: core.LibraryRescanResult{
				Status: "complete",
				Items:  1234,
			},
			contains: []string{"rescan complete: 1234 items indexed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderRaw(t *testing.T) {
	p := HumanPrinter{}

	tests := []struct {
		name     string
		input    core.RawResult
		contains []string
	}{
		{
			name:     "json.RawMessage",
			input:    core.RawResult{Data: json.RawMessage(`{"key":"value"}`)},
			contains: []string{`{"key":"value"}`},
		},
		{
			name:     "byte slice",
			input:    core.RawResult{Data: []byte(`hello bytes`)},
			contains: []string{"hello bytes"},
		},
		{
			name:     "struct",
			input:    core.RawResult{Data: struct{ Foo string }{Foo: "bar"}},
			contains: []string{`"Foo":"bar"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestRenderLibraryItemsOutput(t *testing.T) {
	p := HumanPrinter{}

	payload := `{
		"items": [
			{
				"itemId": "id-1",
				"name": "Cool Track",
				"type": "track",
				"mediaType": "audio",
				"artists": ["Alice", "Bob"],
				"album": "Greatest Hits",
				"containerId": "container-abc"
			}
		],
		"start": 0,
		"count": 1,
		"total": 1
	}`

	tests := []struct {
		name     string
		input    LibraryItemsOutput
		contains []string
	}{
		{
			name: "with items including all fields",
			input: LibraryItemsOutput{
				LibraryID: "lib-1",
				Payload:   json.RawMessage(payload),
			},
			contains: []string{
				"NAME", "TYPE", "ARTIST", "ALBUM", "CONTAINER_ID", "ITEM_ID", "LIB_REF",
				"Cool Track", "track", "Alice, Bob", "Greatest Hits", "container-abc",
				"id-1", "lib:lib-1:id-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := p.Render(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestFormatMS(t *testing.T) {
	tests := []struct {
		name string
		ms   int64
		want string
	}{
		{"zero", 0, "0:00"},
		{"one second", 1000, "0:01"},
		{"one minute", 60000, "1:00"},
		{"one hour one minute one second", 3661000, "1:01:01"},
		{"negative", -5000, "0:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatMS(tt.ms)
			if got != tt.want {
				t.Errorf("formatMS(%d) = %q, want %q", tt.ms, got, tt.want)
			}
		})
	}
}

func TestFormatMSHours(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "0:00"},
		{1000, "0:01"},
		{60000, "1:00"},
		{3599000, "59:59"},
		{3600000, "1:00:00"},
		{3661000, "1:01:01"},
		{7200000, "2:00:00"},
		{-1, "0:00"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.ms), func(t *testing.T) {
			got := formatMS(tt.ms)
			if got != tt.want {
				t.Errorf("formatMS(%d) = %q, want %q", tt.ms, got, tt.want)
			}
		})
	}
}

func TestFormatPosition(t *testing.T) {
	tests := []struct {
		name string
		pos  int64
		dur  int64
		want string
	}{
		{"both zero", 0, 0, ""},
		{"with duration", 61000, 240000, "1:01 / 4:00 (25%)"},
		{"without duration", 30000, 0, "0:30 / 0:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPosition(tt.pos, tt.dur)
			if got != tt.want {
				t.Errorf("formatPosition(%d, %d) = %q, want %q", tt.pos, tt.dur, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"int64", int64(180000), "3:00"},
		{"float64", float64(90000), "1:30"},
		{"json.Number", json.Number("60000"), "1:00"},
		{"nil", nil, ""},
		{"string returns empty", "not a number", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.value)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestFormatItem(t *testing.T) {
	tests := []struct {
		name    string
		current *mu.CurrentItemState
		want    string
	}{
		{
			name: "title and artist",
			current: &mu.CurrentItemState{
				QueueEntryID: "qe-1",
				Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-1"},
				Display:      &mu.DisplayMetadata{Title: "My Song", Artist: "The Band"},
			},
			want: "The Band - My Song",
		},
		{
			name: "title only",
			current: &mu.CurrentItemState{
				QueueEntryID: "qe-2",
				Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-2"},
				Display:      &mu.DisplayMetadata{Title: "Solo Title"},
			},
			want: "Solo Title",
		},
		{
			name: "empty display",
			current: &mu.CurrentItemState{
				QueueEntryID: "qe-3",
				Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-3"},
				Display:      &mu.DisplayMetadata{},
			},
			want: "item-3",
		},
		{
			name: "nil display",
			current: &mu.CurrentItemState{
				QueueEntryID: "qe-4",
				Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-4"},
			},
			want: "item-4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatItem(tt.current)
			if got != tt.want {
				t.Errorf("formatItem() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateCell(t *testing.T) {
	tests := []struct {
		name  string
		value string
		max   int
		want  string
	}{
		{"within limit", "short", 10, "short"},
		{"over limit", "this is a very long string that exceeds", 15, "this is a ve..."},
		{"with pipe chars", "foo|bar", 20, "foo/bar"},
		{"with newlines", "line1\nline2", 20, "line1 line2"},
		{"max 3", "abcdef", 3, "abc"},
		{"max 0", "anything", 0, "anything"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateCell(tt.value, tt.max)
			if got != tt.want {
				t.Errorf("truncateCell(%q, %d) = %q, want %q", tt.value, tt.max, got, tt.want)
			}
		})
	}
}

func TestTruncateByWidth(t *testing.T) {
	tests := []struct {
		name  string
		value string
		max   int
		want  string
	}{
		{"normal ascii", "hello world", 5, "hello"},
		{"max 0", "anything", 0, ""},
		{"max negative", "anything", -1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateByWidth(tt.value, tt.max)
			if got != tt.want {
				t.Errorf("truncateByWidth(%q, %d) = %q, want %q", tt.value, tt.max, got, tt.want)
			}
		})
	}
}

func TestStyleStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
		check  string
	}{
		{"playing green", "playing", "playing"},
		{"paused yellow", "paused", "paused"},
		{"stopped red", "stopped", "stopped"},
		{"unknown gray", "buffering", "buffering"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := styleStatus(tt.status)
			if !strings.Contains(got, tt.check) {
				t.Errorf("styleStatus(%q) = %q, want it to contain %q", tt.status, got, tt.check)
			}
		})
	}
}

func TestDisplayWidth(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{"ascii", "hello", 5},
		{"empty string", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := displayWidth(tt.value)
			if got != tt.want {
				t.Errorf("displayWidth(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestHumanPrinterDefault(t *testing.T) {
	p := HumanPrinter{}

	// An unrecognized type should fall through to "ok\n".
	type unknownResult struct{ Foo string }
	out, err := p.Render(unknownResult{Foo: "bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok\n" {
		t.Errorf("expected %q for unknown type, got %q", "ok\n", out)
	}
}

func TestRenderProgressBar(t *testing.T) {
	tests := []struct {
		name  string
		pos   int64
		dur   int64
		width int
		want  string // what to check with strings.Contains
		empty bool   // expect empty string
	}{
		{"zero_duration", 0, 0, 30, "", true},
		{"negative_duration", 0, -1, 30, "", true},
		{"small_width", 0, 100, 3, "", true},
		{"at_start", 0, 100000, 30, "[", false},
		{"at_start_has_empty", 0, 100000, 30, "─", false},
		{"half_way", 50000, 100000, 30, "━", false},
		{"at_end", 100000, 100000, 30, "━", false},
		{"at_end_no_empty", 100000, 100000, 30, "]", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderProgressBar(tt.pos, tt.dur, tt.width)
			if tt.empty {
				if got != "" {
					t.Errorf("expected empty, got %q", got)
				}
				return
			}
			if got == "" {
				t.Error("expected non-empty progress bar")
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("expected %q to contain %q", got, tt.want)
			}
		})
	}
}

func TestRenderQueueListOutput(t *testing.T) {
	t.Run("with_pagination", func(t *testing.T) {
		data := QueueListOutput{
			Result: core.QueueResult{
				Queue: mu.QueueGetReply{
					Revision: 5,
					Entries: []mu.QueueEntry{
						{QueueEntryID: "q1", Ref: &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "i1"}, Display: &mu.DisplayMetadata{Title: "Song A"}},
						{QueueEntryID: "q2", Ref: &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "i2"}, Display: &mu.DisplayMetadata{Title: "Song B"}},
					},
				},
			},
			Offset: 10,
			Count:  50,
		}
		printer := HumanPrinter{}
		out, err := printer.Render(data)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(out, "Song A") {
			t.Error("expected Song A in output")
		}
		if !strings.Contains(out, "Showing 11-12") {
			t.Errorf("expected pagination 'Showing 11-12', got:\n%s", out)
		}
		if !strings.Contains(out, "rev 5") {
			t.Error("expected rev 5 in pagination line")
		}
	})

	t.Run("without_pagination", func(t *testing.T) {
		data := QueueListOutput{
			Result: core.QueueResult{
				Queue: mu.QueueGetReply{
					Entries: []mu.QueueEntry{
						{QueueEntryID: "q1", Ref: &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "i1"}, Display: &mu.DisplayMetadata{Title: "Song A"}},
					},
				},
			},
			Offset: 0,
			Count:  50,
		}
		printer := HumanPrinter{}
		out, err := printer.Render(data)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if strings.Contains(out, "Showing") {
			t.Error("should not show pagination when offset=0 and not full page")
		}
	})
}

func TestLibraryItemsPagination(t *testing.T) {
	payload := `{"items":[{"itemId":"i1","name":"Track 1","type":"Audio","artists":["Artist"],"album":"Album"}],"start":0,"count":50,"total":100}`
	data := LibraryItemsOutput{
		LibraryID: "lib-1",
		Payload:   json.RawMessage(payload),
	}
	printer := HumanPrinter{}
	out, err := printer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "Track 1") {
		t.Error("expected Track 1 in output")
	}
	if !strings.Contains(out, "Showing 1-1 of 100 items") {
		t.Errorf("expected pagination footer, got:\n%s", out)
	}
}

func TestLibraryItemsNoPagination(t *testing.T) {
	payload := `{"items":[{"itemId":"i1","name":"Track 1","type":"Audio"}],"start":0,"count":50,"total":0}`
	data := LibraryItemsOutput{
		LibraryID: "lib-1",
		Payload:   json.RawMessage(payload),
	}
	printer := HumanPrinter{}
	out, err := printer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out, "Showing") {
		t.Error("should not show pagination when total is 0")
	}
}
