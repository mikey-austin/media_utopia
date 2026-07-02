package output

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/mikey-austin/media_utopia/internal/core"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

func TestJSONPrinterNodesResult(t *testing.T) {
	result := core.NodesResult{
		Nodes: []mu.Presence{
			{NodeID: "node-1", Kind: "renderer", Name: "Living Room"},
			{NodeID: "node-2", Kind: "library", Name: "Jellyfin"},
		},
	}
	out := captureJSON(t, result)
	var parsed struct {
		Nodes []struct {
			NodeID string `json:"nodeId"`
			Kind   string `json:"kind"`
			Name   string `json:"name"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(parsed.Nodes))
	}
	if parsed.Nodes[0].Name != "Living Room" {
		t.Errorf("expected Living Room, got %s", parsed.Nodes[0].Name)
	}
}

func TestJSONPrinterStatusResult(t *testing.T) {
	result := core.StatusResult{
		Renderer: mu.Presence{NodeID: "r-1", Kind: "renderer", Name: "Bedroom"},
		State: mu.RendererState{
			Playback: &mu.PlaybackState{
				Status:     "playing",
				PositionMS: 60000,
				DurationMS: 180000,
				Volume:     0.75,
			},
		},
	}
	out := captureJSON(t, result)
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	state, ok := parsed["state"].(map[string]any)
	if !ok {
		t.Fatal("missing State")
	}
	playback, ok := state["playback"].(map[string]any)
	if !ok {
		t.Fatal("missing playback")
	}
	if playback["status"] != "playing" {
		t.Errorf("expected playing, got %v", playback["status"])
	}
}

func TestJSONPrinterSessionResult(t *testing.T) {
	result := core.SessionResult{
		RendererID: "r-1",
		Session: mu.SessionLease{
			ID:             "sess-1",
			Token:          "tok-1",
			Owner:          "mikey",
			LeaseExpiresAt: 1700000000,
		},
	}
	out := captureJSON(t, result)
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["rendererId"] != "r-1" {
		t.Errorf("expected r-1, got %v", parsed["rendererId"])
	}
}

func TestJSONPrinterQueueResult(t *testing.T) {
	result := core.QueueResult{
		RendererID: "r-1",
		Queue: mu.QueueGetReply{
			Revision: 5,
			Index:    2,
			Entries: []mu.QueueEntry{
				{
					QueueEntryID: "q-1",
					Ref:          &mu.LibraryItemRef{Kind: mu.LibraryItemKind, LibraryID: "mu:library:fs:default:music", ItemID: "item-1"},
					Display:      &mu.DisplayMetadata{Title: "Song A"},
				},
			},
		},
	}
	out := captureJSON(t, result)
	if !json.Valid(out) {
		t.Error("invalid JSON output")
	}
}

func TestJSONPrinterPlaylistListResult(t *testing.T) {
	result := core.PlaylistListResult{
		Playlists: []mu.PlaylistSummary{
			{PlaylistID: "pl-1", Name: "Jazz", Revision: 3},
		},
	}
	out := captureJSON(t, result)
	var parsed struct {
		Playlists []struct {
			PlaylistID string `json:"playlistId"`
			Name       string `json:"name"`
		} `json:"Playlists"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.Playlists) != 1 || parsed.Playlists[0].Name != "Jazz" {
		t.Errorf("unexpected playlists: %+v", parsed.Playlists)
	}
}

func TestJSONPrinterEmptyResult(t *testing.T) {
	result := core.NodesResult{Nodes: nil}
	out := captureJSON(t, result)
	if !json.Valid(out) {
		t.Error("invalid JSON output")
	}
}

func TestJSONPrinterRawResult(t *testing.T) {
	result := core.RawResult{Data: map[string]string{"key": "value"}}
	out := captureJSON(t, result)
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// RawResult marshals as the payload itself — no envelope for scripts
	// to unwrap.
	if parsed["key"] != "value" {
		t.Errorf("expected inlined payload, got %v", parsed)
	}
}

// captureJSON redirects stdout and captures JSONPrinter output.
func captureJSON(t *testing.T, v any) []byte {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := JSONPrinter{}.Print(v)
	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("JSONPrinter.Print: %v", err)
	}
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.Bytes()
}
