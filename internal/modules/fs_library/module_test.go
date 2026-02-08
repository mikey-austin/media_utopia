package fslibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

func TestBrowseSearchResolve(t *testing.T) {
	root := t.TempDir()
	audioDir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	audioPath := filepath.Join(audioDir, "Artist - Track.mp3")
	if err := os.WriteFile(audioPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	videoPath := filepath.Join(root, "VideoTitle.mkv")
	if err := os.WriteFile(videoPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write video: %v", err)
	}

	indexPath := filepath.Join(root, "index.json")
	mod, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:         "mu:library:filesystem:test:default",
		Roots:          []string{root},
		IncludeExts:    []string{".mp3", ".mkv"},
		HTTPListen:     "127.0.0.1:0",
		IndexMode:      "separate",
		IndexPath:      indexPath,
		ScanIntervalMS: 0,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	if err := mod.scan(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	cmd := mu.CommandEnvelope{
		ID:   "c1",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{Start: 0, Count: 10}),
	}
	reply := mod.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var browse libraryItemsReply
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse unmarshal: %v", err)
	}
	if len(browse.Items) != 2 {
		t.Fatalf("expected 2 root items, got %d", len(browse.Items))
	}

	audioContainer := browse.Items[0].ItemID
	// Browse Audio → expect 4 sub-categories
	cmd = mu.CommandEnvelope{
		ID:   "c2",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: audioContainer, Start: 0, Count: 10}),
	}
	reply = mod.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse audio root unmarshal: %v", err)
	}
	if len(browse.Items) != 4 {
		t.Fatalf("expected 4 audio sub-categories, got %d", len(browse.Items))
	}

	// Browse By Artist → expect letter folders
	byArtistContainer := browse.Items[1].ItemID // "By Artist"
	cmd = mu.CommandEnvelope{
		ID:   "c2a",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: byArtistContainer, Start: 0, Count: 10}),
	}
	reply = mod.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse letters unmarshal: %v", err)
	}
	if len(browse.Items) == 0 {
		t.Fatalf("expected at least one letter folder")
	}
	// Pick the letter "A" for "Artist"
	letterContainer := browse.Items[0].ItemID

	// Browse letter → expect artist
	cmd = mu.CommandEnvelope{
		ID:   "c2b",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: letterContainer, Start: 0, Count: 10}),
	}
	reply = mod.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse artists unmarshal: %v", err)
	}
	if len(browse.Items) != 1 || browse.Items[0].Name != "Artist" {
		t.Fatalf("expected artist container, got %+v", browse.Items)
	}
	albumContainer := browse.Items[0].ItemID

	// Browse artist → expect album
	cmd = mu.CommandEnvelope{
		ID:   "c3",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: albumContainer, Start: 0, Count: 10}),
	}
	reply = mod.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse albums unmarshal: %v", err)
	}
	if len(browse.Items) != 1 || browse.Items[0].Name != "Album" {
		t.Fatalf("expected album container, got %+v", browse.Items)
	}
	trackContainer := browse.Items[0].ItemID

	cmd = mu.CommandEnvelope{
		ID:   "c4",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: trackContainer, Start: 0, Count: 10}),
	}
	reply = mod.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse tracks unmarshal: %v", err)
	}
	if len(browse.Items) != 1 {
		t.Fatalf("expected 1 track, got %d", len(browse.Items))
	}
	trackID := browse.Items[0].ItemID

	cmd = mu.CommandEnvelope{
		ID:   "s1",
		Type: "library.search",
		Body: mustJSON(mu.LibrarySearchBody{Query: "Track", Start: 0, Count: 10}),
	}
	reply = mod.librarySearch(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("search unmarshal: %v", err)
	}
	if len(browse.Items) != 1 || browse.Items[0].ItemID != trackID {
		t.Fatalf("expected search hit, got %+v", browse.Items)
	}

	if err := mod.startHTTPServer(); err != nil {
		t.Fatalf("http server: %v", err)
	}
	defer mod.shutdownHTTPServer()

	cmd = mu.CommandEnvelope{
		ID:   "r1",
		Type: "library.resolve",
		Body: mustJSON(mu.LibraryResolveBody{ItemID: trackID}),
	}
	reply = mod.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var resolve mu.LibraryResolveReply
	if err := json.Unmarshal(reply.Body, &resolve); err != nil {
		t.Fatalf("resolve unmarshal: %v", err)
	}
	if resolve.ItemID != trackID || len(resolve.Sources) != 1 || resolve.Sources[0].URL == "" {
		t.Fatalf("resolve unexpected: %+v", resolve)
	}

	cmd = mu.CommandEnvelope{
		ID:   "rb1",
		Type: "library.resolveBatch",
		Body: mustJSON(mu.LibraryResolveBatchBody{ItemIDs: []string{trackID, "missing"}}),
	}
	reply = mod.libraryResolveBatch(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var batch mu.LibraryResolveBatchReply
	if err := json.Unmarshal(reply.Body, &batch); err != nil {
		t.Fatalf("resolve batch unmarshal: %v", err)
	}
	if len(batch.Items) != 2 || batch.Items[1].Err == nil {
		t.Fatalf("expected batch error for missing item, got %+v", batch.Items)
	}
}

func mustJSON(v any) []byte {
	payload, _ := json.Marshal(v)
	return payload
}

func TestContainerResolveAndMetadataOnly(t *testing.T) {
	root := t.TempDir()
	audioDir := filepath.Join(root, "TestArtist", "TestAlbum")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	audioPath := filepath.Join(audioDir, "TestArtist - TestTrack.mp3")
	if err := os.WriteFile(audioPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	mod, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:         "mu:library:filesystem:test:container",
		Roots:          []string{root},
		IncludeExts:    []string{".mp3"},
		HTTPListen:     "127.0.0.1:0",
		ScanIntervalMS: 0,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	if err := mod.scan(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if err := mod.startHTTPServer(); err != nil {
		t.Fatalf("http server: %v", err)
	}
	defer mod.shutdownHTTPServer()

	// Test resolving container:audio
	cmd := mu.CommandEnvelope{
		ID:   "c1",
		Type: "library.resolve",
		Body: mustJSON(mu.LibraryResolveBody{ItemID: "container:audio"}),
	}
	reply := mod.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var resolve mu.LibraryResolveReply
	if err := json.Unmarshal(reply.Body, &resolve); err != nil {
		t.Fatalf("resolve container:audio unmarshal: %v", err)
	}
	if resolve.ItemID != "container:audio" {
		t.Errorf("expected itemId container:audio, got %s", resolve.ItemID)
	}
	if resolve.Metadata["type"] != "Folder" {
		t.Errorf("expected type Folder, got %v", resolve.Metadata["type"])
	}
	if len(resolve.Sources) != 0 {
		t.Errorf("expected no sources for container, got %d", len(resolve.Sources))
	}

	// Browse container:audio → 4 sub-categories
	browseCmd := mu.CommandEnvelope{
		ID:   "b0",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: "container:audio", Start: 0, Count: 10}),
	}
	browseReply := mod.libraryBrowse(browseCmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var browse libraryItemsReply
	if err := json.Unmarshal(browseReply.Body, &browse); err != nil {
		t.Fatalf("browse audio root unmarshal: %v", err)
	}
	if len(browse.Items) != 4 {
		t.Fatalf("expected 4 audio sub-categories, got %d", len(browse.Items))
	}

	// Browse By Artist → letters
	byArtistCmd := mu.CommandEnvelope{
		ID:   "b0a",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: "container:audio:byartist", Start: 0, Count: 10}),
	}
	browseReply = mod.libraryBrowse(byArtistCmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(browseReply.Body, &browse); err != nil {
		t.Fatalf("browse letters unmarshal: %v", err)
	}
	if len(browse.Items) == 0 {
		t.Fatal("expected at least one letter")
	}
	letterID := browse.Items[0].ItemID

	// Browse letter → artists
	letterCmd := mu.CommandEnvelope{
		ID:   "b0b",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: letterID, Start: 0, Count: 10}),
	}
	browseReply = mod.libraryBrowse(letterCmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(browseReply.Body, &browse); err != nil {
		t.Fatalf("browse letter artists unmarshal: %v", err)
	}
	if len(browse.Items) == 0 {
		t.Fatal("expected at least one artist")
	}
	artistID := browse.Items[0].ItemID

	// Test resolving artist container (hashed ID)
	cmd = mu.CommandEnvelope{
		ID:   "c2",
		Type: "library.resolve",
		Body: mustJSON(mu.LibraryResolveBody{ItemID: artistID}),
	}
	reply = mod.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &resolve); err != nil {
		t.Fatalf("resolve artist unmarshal: %v", err)
	}
	if resolve.Metadata["type"] != "MusicArtist" {
		t.Errorf("expected type MusicArtist, got %v", resolve.Metadata["type"])
	}
	if resolve.Metadata["title"] != "TestArtist" {
		t.Errorf("expected title TestArtist, got %v", resolve.Metadata["title"])
	}
	if len(resolve.Sources) != 0 {
		t.Errorf("expected no sources for artist container, got %d", len(resolve.Sources))
	}

	// Browse artist to get album IDs (hashed)
	browseCmd = mu.CommandEnvelope{
		ID:   "b1",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: artistID, Start: 0, Count: 10}),
	}
	browseReply = mod.libraryBrowse(browseCmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(browseReply.Body, &browse); err != nil {
		t.Fatalf("browse albums unmarshal: %v", err)
	}
	if len(browse.Items) == 0 {
		t.Fatal("expected at least one album")
	}
	albumID := browse.Items[0].ItemID

	// Test resolving album container (hashed ID)
	cmd = mu.CommandEnvelope{
		ID:   "c3",
		Type: "library.resolve",
		Body: mustJSON(mu.LibraryResolveBody{ItemID: albumID}),
	}
	reply = mod.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &resolve); err != nil {
		t.Fatalf("resolve album unmarshal: %v", err)
	}
	if resolve.Metadata["type"] != "MusicAlbum" {
		t.Errorf("expected type MusicAlbum, got %v", resolve.Metadata["type"])
	}
	if resolve.Metadata["title"] != "TestAlbum" {
		t.Errorf("expected title TestAlbum, got %v", resolve.Metadata["title"])
	}

	// Browse album to get track IDs
	browseCmd = mu.CommandEnvelope{
		ID:   "b2",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: albumID, Start: 0, Count: 10}),
	}
	browseReply = mod.libraryBrowse(browseCmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(browseReply.Body, &browse); err != nil {
		t.Fatalf("browse tracks unmarshal: %v", err)
	}
	if len(browse.Items) == 0 {
		t.Fatal("expected at least one track")
	}
	trackID := browse.Items[0].ItemID

	// Test metadataOnly=true returns no sources
	cmd = mu.CommandEnvelope{
		ID:   "m1",
		Type: "library.resolve",
		Body: mustJSON(mu.LibraryResolveBody{ItemID: trackID, MetadataOnly: true}),
	}
	reply = mod.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &resolve); err != nil {
		t.Fatalf("resolve metadataOnly unmarshal: %v", err)
	}
	if resolve.ItemID != trackID {
		t.Errorf("expected itemId %s, got %s", trackID, resolve.ItemID)
	}
	if len(resolve.Sources) != 0 {
		t.Errorf("expected no sources for metadataOnly, got %d", len(resolve.Sources))
	}
	if resolve.Metadata["title"] == nil {
		t.Error("expected metadata to be populated")
	}

	// Test metadataOnly=false returns sources
	cmd = mu.CommandEnvelope{
		ID:   "m2",
		Type: "library.resolve",
		Body: mustJSON(mu.LibraryResolveBody{ItemID: trackID, MetadataOnly: false}),
	}
	reply = mod.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &resolve); err != nil {
		t.Fatalf("resolve full unmarshal: %v", err)
	}
	if len(resolve.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(resolve.Sources))
	}
	if resolve.Sources[0].URL == "" {
		t.Error("expected source URL to be populated")
	}

	// Test batch with metadataOnly
	batchCmd := mu.CommandEnvelope{
		ID:   "batch1",
		Type: "library.resolveBatch",
		Body: mustJSON(mu.LibraryResolveBatchBody{
			ItemIDs:      []string{artistID, trackID},
			MetadataOnly: true,
		}),
	}
	batchReply := mod.libraryResolveBatch(batchCmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var batch mu.LibraryResolveBatchReply
	if err := json.Unmarshal(batchReply.Body, &batch); err != nil {
		t.Fatalf("batch unmarshal: %v", err)
	}
	if len(batch.Items) != 2 {
		t.Fatalf("expected 2 batch items, got %d", len(batch.Items))
	}
	// First item is artist container
	if batch.Items[0].Metadata["type"] != "MusicArtist" {
		t.Errorf("expected artist type MusicArtist, got %v", batch.Items[0].Metadata["type"])
	}
	if len(batch.Items[0].Sources) != 0 {
		t.Errorf("expected no sources for artist, got %d", len(batch.Items[0].Sources))
	}
	// Second item is track with metadataOnly
	if len(batch.Items[1].Sources) != 0 {
		t.Errorf("expected no sources for metadataOnly track, got %d", len(batch.Items[1].Sources))
	}
}

func TestLibraryRescan(t *testing.T) {
	root := t.TempDir()
	audioPath := filepath.Join(root, "track.mp3")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	mod, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:         "mu:library:filesystem:test:rescan",
		Roots:          []string{root},
		IncludeExts:    []string{".mp3"},
		HTTPListen:     "127.0.0.1:0",
		ScanIntervalMS: 0,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	// Initial scan
	if err := mod.scan(); err != nil {
		t.Fatalf("initial scan: %v", err)
	}

	// Test sync rescan
	cmd := mu.CommandEnvelope{
		ID:   "rescan1",
		Type: "library.rescan",
		Body: mustJSON(map[string]bool{"async": false}),
	}
	reply := mod.libraryRescan(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if !reply.OK {
		t.Fatalf("rescan failed: %+v", reply)
	}

	var result rescanReply
	if err := json.Unmarshal(reply.Body, &result); err != nil {
		t.Fatalf("unmarshal rescan reply: %v", err)
	}
	if result.Status != "complete" {
		t.Errorf("expected status 'complete', got %q", result.Status)
	}
	if result.Items != 1 {
		t.Errorf("expected 1 item, got %d", result.Items)
	}

	// Add another file and rescan
	audioPath2 := filepath.Join(root, "track2.mp3")
	if err := os.WriteFile(audioPath2, []byte("audio2"), 0o644); err != nil {
		t.Fatalf("write audio2: %v", err)
	}

	cmd = mu.CommandEnvelope{
		ID:   "rescan2",
		Type: "library.rescan",
	}
	reply = mod.libraryRescan(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &result); err != nil {
		t.Fatalf("unmarshal rescan2 reply: %v", err)
	}
	if result.Items != 2 {
		t.Errorf("expected 2 items after rescan, got %d", result.Items)
	}

	// Test async rescan
	cmd = mu.CommandEnvelope{
		ID:   "rescan3",
		Type: "library.rescan",
		Body: mustJSON(map[string]bool{"async": true}),
	}
	reply = mod.libraryRescan(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &result); err != nil {
		t.Fatalf("unmarshal async rescan reply: %v", err)
	}
	if result.Status != "started" {
		t.Errorf("expected status 'started' for async, got %q", result.Status)
	}
}

func TestRepairMetadata(t *testing.T) {
	tests := []struct {
		name     string
		item     mediaItem
		policy   RepairPolicy
		wantText string
	}{
		{
			name:     "no repair needed",
			item:     mediaItem{Title: "Existing Title", Artists: []string{"Artist"}},
			policy:   RepairPolicyBalanced,
			wantText: "Existing Title",
		},
		{
			name:     "extract from filename",
			item:     mediaItem{Name: "Artist - Track Name"},
			policy:   RepairPolicyBalanced,
			wantText: "Track Name",
		},
		{
			name:     "track number prefix",
			item:     mediaItem{Name: "01 - Track Title"},
			policy:   RepairPolicyBalanced,
			wantText: "Track Title",
		},
		{
			name:     "clean official video suffix",
			item:     mediaItem{Title: "Song Name (Official Video)"},
			policy:   RepairPolicyBalanced,
			wantText: "Song Name",
		},
		{
			name:     "policy none skips repair",
			item:     mediaItem{Name: "Artist - Track"},
			policy:   RepairPolicyNone,
			wantText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := repairMetadata(tt.item, tt.policy)
			if result.Title != tt.wantText {
				t.Errorf("repairMetadata() title = %q, want %q", result.Title, tt.wantText)
			}
		})
	}
}

func TestParseFilename(t *testing.T) {
	tests := []struct {
		filename    string
		wantTitle   string
		wantArtists []string
	}{
		{"Artist - Title", "Title", []string{"Artist"}},
		{"01 - Title", "Title", nil},
		{"01. Title", "Title", nil},
		{"Just A Title", "Just A Title", nil},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := parseFilename(tt.filename)
			if result.Title != tt.wantTitle {
				t.Errorf("parseFilename(%q) title = %q, want %q", tt.filename, result.Title, tt.wantTitle)
			}
			if len(result.Artists) != len(tt.wantArtists) {
				t.Errorf("parseFilename(%q) artists = %v, want %v", tt.filename, result.Artists, tt.wantArtists)
			}
		})
	}
}

func TestDuplicateIndex(t *testing.T) {
	idx := NewDuplicateIndex()

	// First item is not a duplicate
	if idx.Add("item1", "hash1") {
		t.Error("first item should not be duplicate")
	}

	// Second item with same hash is duplicate
	if !idx.Add("item2", "hash1") {
		t.Error("second item with same hash should be duplicate")
	}

	// Different hash is not duplicate
	if idx.Add("item3", "hash2") {
		t.Error("item with different hash should not be duplicate")
	}

	// Check IsDuplicate
	if !idx.IsDuplicate("item1") {
		t.Error("item1 should be marked as having duplicates")
	}
	if !idx.IsDuplicate("item2") {
		t.Error("item2 should be marked as duplicate")
	}
	if idx.IsDuplicate("item3") {
		t.Error("item3 should not be duplicate")
	}

	// Check Original
	if idx.Original("item2") != "item1" {
		t.Errorf("Original(item2) = %q, want item1", idx.Original("item2"))
	}

	// Check GetDuplicates
	groups := idx.GetDuplicates()
	if len(groups) != 1 || len(groups[0]) != 2 {
		t.Errorf("GetDuplicates() = %v, want 1 group with 2 items", groups)
	}
}

func TestVectorIndex(t *testing.T) {
	idx := NewVectorIndex()

	// Add some vectors
	idx.Add("item1", []float32{1, 0, 0})
	idx.Add("item2", []float32{0.9, 0.1, 0})
	idx.Add("item3", []float32{0, 1, 0})

	// Search for similar to item1's vector
	results := idx.Search([]float32{1, 0, 0}, 2)
	if len(results) != 2 {
		t.Fatalf("Search returned %d results, want 2", len(results))
	}

	// item1 should be most similar (exact match)
	if results[0].ID != "item1" || results[0].Score < 0.99 {
		t.Errorf("first result = %+v, want item1 with score ~1.0", results[0])
	}

	// item2 should be second (similar)
	if results[1].ID != "item2" {
		t.Errorf("second result = %+v, want item2", results[1])
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name  string
		a, b  []float32
		want  float32
		delta float32
	}{
		{"identical", []float32{1, 0}, []float32{1, 0}, 1.0, 0.001},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0.0, 0.001},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0, 0.001},
		{"similar", []float32{1, 1}, []float32{1, 0}, 0.707, 0.01},
		{"empty", []float32{}, []float32{}, 0.0, 0.001},
		{"mismatched", []float32{1, 0}, []float32{1, 0, 0}, 0.0, 0.001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if got < tt.want-tt.delta || got > tt.want+tt.delta {
				t.Errorf("cosineSimilarity(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSidecarNeedsRefresh(t *testing.T) {
	tests := []struct {
		name string
		meta *AlbumMetadata
		want bool
	}{
		{
			name: "v1 sidecar with data triggers refresh",
			meta: &AlbumMetadata{
				Version:     1,
				FetchedAt:   time.Now(),
				MusicBrainz: &MBMetadata{Genres: []string{"rock"}},
			},
			want: true,
		},
		{
			name: "v2 sidecar triggers refresh to v3",
			meta: &AlbumMetadata{
				Version:     2,
				FetchedAt:   time.Now(),
				MusicBrainz: &MBMetadata{Genres: []string{"rock"}},
			},
			want: true,
		},
		{
			name: "v3 sidecar with data does not refresh",
			meta: &AlbumMetadata{
				Version:     3,
				FetchedAt:   time.Now(),
				MusicBrainz: &MBMetadata{Genres: []string{"rock"}},
			},
			want: false,
		},
		{
			name: "v3 negative cache recent does not refresh",
			meta: &AlbumMetadata{
				Version:   3,
				FetchedAt: time.Now(),
			},
			want: false,
		},
		{
			name: "v3 negative cache old triggers refresh",
			meta: &AlbumMetadata{
				Version:   3,
				FetchedAt: time.Now().Add(-31 * 24 * time.Hour),
			},
			want: true,
		},
		{
			name: "v0 sidecar triggers refresh",
			meta: &AlbumMetadata{
				Version:   0,
				FetchedAt: time.Now(),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sidecarNeedsRefresh(tt.meta)
			if got != tt.want {
				t.Errorf("sidecarNeedsRefresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildEmbedText(t *testing.T) {
	t.Run("basic without enrichment", func(t *testing.T) {
		item := mediaItem{
			Title:     "Song Title",
			Artists:   []string{"Artist Name"},
			Album:     "Album Name",
			Name:      "Song Title",
			MediaType: "Audio",
		}
		text := buildEmbedText(item, nil)
		want := "type: audio\ntitle: Song Title\nartist: Artist Name\nalbum: Album Name"
		if text != want {
			t.Errorf("buildEmbedText() =\n%q\nwant:\n%q", text, want)
		}
	})

	t.Run("full enrichment", func(t *testing.T) {
		item := mediaItem{
			Title:     "So What",
			Artists:   []string{"Miles Davis"},
			Album:     "Kind of Blue",
			Name:      "So What",
			MediaType: "Audio",
		}
		enrich := &AlbumMetadata{
			Artist: "Miles Davis",
			Album:  "Kind of Blue",
			MusicBrainz: &MBMetadata{
				Genres:      []string{"Modal Jazz", "Cool Jazz"},
				Tags:        []string{"modal", "minimal harmony"},
				Year:        1959,
				Label:       "Columbia",
				ReleaseType: "Album",
			},
			Discogs: &DiscogsMetadata{
				Styles: []string{"Cool Jazz", "Post-Bop"},
				Credits: []DiscogsCredit{
					{Name: "John Coltrane", Role: "tenor sax"},
					{Name: "Bill Evans", Role: "piano"},
					{Name: "Cannonball Adderley", Role: "alto sax"},
				},
				ReleaseCredits: []DiscogsCredit{
					{Name: "Teo Macero", Role: "producer"},
				},
				Instruments: []string{"alto sax", "piano", "tenor sax"},
			},
			ArtistInfo: &ArtistInfo{
				Type:        "Group",
				Origin:      "US",
				ActiveBegin: "1944",
				Members:     []string{"Miles Davis", "John Coltrane"},
				Genres:      []string{"Jazz", "Cool Jazz"},
				Biography:   "Miles Davis was an American trumpeter and bandleader.",
			},
			Description: &AlbumDescription{
				WikipediaSummary: "Kind of Blue is a studio album by Miles Davis.",
			},
		}
		text := buildEmbedText(item, enrich)
		// Verify key labeled fields are present
		for _, want := range []string{
			"type: audio",
			"title: So What",
			"artist: Miles Davis",
			"album: Kind of Blue",
			"year: 1959",
			"genres: ",
			"styles: ",
			"instruments: alto sax; piano; tenor sax",
			"tags: ",
			"label: Columbia",
			"recording_type: Album",
			"personnel: John Coltrane; Bill Evans; Cannonball Adderley",
			"producers: Teo Macero",
			"artist_type: Group",
			"artist_origin: US",
			"artist_active: 1944",
			"members: Miles Davis; John Coltrane",
			"biography: Miles Davis was an American trumpeter",
			"description: Kind of Blue is a studio album",
			"album_context: Kind of Blue (1959, Columbia) --",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("buildEmbedText() missing %q in:\n%s", want, text)
			}
		}
	})

	t.Run("normalization dedup and sort", func(t *testing.T) {
		item := mediaItem{
			Title:     "Track",
			Artists:   []string{"Artist"},
			Album:     "Album",
			MediaType: "Audio",
		}
		enrich := &AlbumMetadata{
			Album: "Album",
			MusicBrainz: &MBMetadata{
				Genres: []string{"Rock", "rock", "Hip Hop"},
				Year:   2020,
			},
			ArtistInfo: &ArtistInfo{
				Genres: []string{"ROCK", "Pop"},
			},
		}
		text := buildEmbedText(item, enrich)
		// "Rock", "rock", "ROCK" should dedup to one "rock"; "Hip Hop" -> "hip-hop"
		if !strings.Contains(text, "genres: hip-hop; pop; rock") {
			t.Errorf("expected normalized deduped genres, got:\n%s", text)
		}
	})

	t.Run("field capping", func(t *testing.T) {
		item := mediaItem{
			Title:     "Track",
			Artists:   []string{"Artist"},
			Album:     "Album",
			MediaType: "Audio",
		}
		credits := make([]DiscogsCredit, 10)
		for i := range credits {
			credits[i] = DiscogsCredit{Name: fmt.Sprintf("Person%d", i)}
		}
		enrich := &AlbumMetadata{
			Album: "Album",
			Discogs: &DiscogsMetadata{
				Credits: credits,
			},
			ArtistInfo: &ArtistInfo{
				Members:   []string{"A", "B", "C", "D", "E", "F", "G"},
				Biography: strings.Repeat("x", 300),
			},
		}
		text := buildEmbedText(item, enrich)
		// Personnel should be capped at 5
		if strings.Contains(text, "Person5") {
			t.Errorf("personnel should be capped at 5, got:\n%s", text)
		}
		// Members should be capped at 5
		if strings.Contains(text, "members: A; B; C; D; E; F") {
			t.Errorf("members should be capped at 5, got:\n%s", text)
		}
		// Biography should be capped at 200
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "biography: ") {
				bio := strings.TrimPrefix(line, "biography: ")
				if len(bio) > 200 {
					t.Errorf("biography length %d > 200", len(bio))
				}
			}
		}
	})

	t.Run("video type", func(t *testing.T) {
		item := mediaItem{
			Title:     "My Video",
			MediaType: "Video",
		}
		text := buildEmbedText(item, nil)
		if !strings.HasPrefix(text, "type: video") {
			t.Errorf("expected type: video prefix, got:\n%s", text)
		}
	})
}

func TestNormalizeStringList(t *testing.T) {
	t.Run("lowercase dedup sort", func(t *testing.T) {
		got := normalizeStringList([]string{"Rock", "Jazz", "rock", "JAZZ", "Blues"}, nil)
		want := []string{"blues", "jazz", "rock"}
		if !slicesEqual(got, want) {
			t.Errorf("normalizeStringList() = %v, want %v", got, want)
		}
	})

	t.Run("synonyms applied", func(t *testing.T) {
		got := normalizeStringList([]string{"Hip Hop", "R&B", "Lo Fi"}, genreSynonyms)
		want := []string{"hip-hop", "lo-fi", "rhythm and blues"}
		if !slicesEqual(got, want) {
			t.Errorf("normalizeStringList() = %v, want %v", got, want)
		}
	})

	t.Run("whitespace trim and empty filtering", func(t *testing.T) {
		got := normalizeStringList([]string{"  rock ", "", "  ", "jazz"}, nil)
		want := []string{"jazz", "rock"}
		if !slicesEqual(got, want) {
			t.Errorf("normalizeStringList() = %v, want %v", got, want)
		}
	})

	t.Run("nil input", func(t *testing.T) {
		got := normalizeStringList(nil, nil)
		if len(got) != 0 {
			t.Errorf("normalizeStringList(nil) = %v, want empty", got)
		}
	})
}

func TestSearchDual(t *testing.T) {
	idx := NewVectorIndex()
	// Add card + summary for item "a"
	idx.Add("a"+vectorSuffixCard, []float32{1, 0, 0})
	idx.Add("a"+vectorSuffixSummary, []float32{0, 1, 0})
	// Add card + summary for item "b"
	idx.Add("b"+vectorSuffixCard, []float32{0, 1, 0})
	idx.Add("b"+vectorSuffixSummary, []float32{1, 0, 0})

	// Query aligned with card of "a" and summary of "b"
	query := []float32{1, 0, 0}
	results := idx.SearchDual(query, 0.6, 0.4, 10)

	if len(results) != 2 {
		t.Fatalf("SearchDual() returned %d results, want 2", len(results))
	}

	// "a" card=1.0 summary=0.0 => 0.6*1.0 + 0.4*0.0 = 0.6
	// "b" card=0.0 summary=1.0 => 0.6*0.0 + 0.4*1.0 = 0.4
	if results[0].ID != "a" {
		t.Errorf("SearchDual() first result ID = %q, want 'a'", results[0].ID)
	}
	if results[1].ID != "b" {
		t.Errorf("SearchDual() second result ID = %q, want 'b'", results[1].ID)
	}
	// Verify weighted scores
	if abs32(results[0].Score-0.6) > 0.01 {
		t.Errorf("SearchDual() first score = %f, want ~0.6", results[0].Score)
	}
	if abs32(results[1].Score-0.4) > 0.01 {
		t.Errorf("SearchDual() second score = %f, want ~0.4", results[1].Score)
	}
}

func TestSearchDualMissingSummary(t *testing.T) {
	idx := NewVectorIndex()
	// Item "a" has card only (no summary)
	idx.Add("a"+vectorSuffixCard, []float32{1, 0, 0})
	// Item "b" has card + summary
	idx.Add("b"+vectorSuffixCard, []float32{0.7, 0.7, 0})
	idx.Add("b"+vectorSuffixSummary, []float32{0.7, 0.7, 0})

	query := []float32{1, 0, 0}
	results := idx.SearchDual(query, 0.6, 0.4, 10)

	if len(results) != 2 {
		t.Fatalf("SearchDual() returned %d results, want 2", len(results))
	}

	// "a" has no summary, so card score used at full weight = cos({1,0,0}, {1,0,0}) = 1.0
	// "b" card+summary both {0.7,0.7,0} => cos = 0.7071..., score = 0.6*0.707 + 0.4*0.707 ≈ 0.707
	if results[0].ID != "a" {
		t.Errorf("SearchDual() first result ID = %q, want 'a' (no penalty for missing summary)", results[0].ID)
	}
	if results[0].Score < 0.99 {
		t.Errorf("SearchDual() missing summary score = %f, want ~1.0 (full card weight)", results[0].Score)
	}
}

func TestBuildSummaryText(t *testing.T) {
	item := mediaItem{
		Title:     "So What",
		Artists:   []string{"Miles Davis"},
		Album:     "Kind of Blue",
		MediaType: "Audio",
	}
	enrich := &AlbumMetadata{
		Album: "Kind of Blue",
		MusicBrainz: &MBMetadata{
			Genres: []string{"Modal Jazz", "Cool Jazz"},
			Tags:   []string{"modal"},
			Year:   1959,
		},
		Discogs: &DiscogsMetadata{
			Styles: []string{"Post-Bop"},
		},
		Description: &AlbumDescription{
			WikipediaSummary: "Kind of Blue is a studio album by Miles Davis.",
		},
	}

	text := buildSummaryText(item, enrich)
	for _, want := range []string{
		"type: audio summary",
		"title: So What",
		"artist: Miles Davis",
		"album: Kind of Blue",
		"year: 1959",
		"summary: Kind of Blue is a studio album by Miles Davis.",
		"keywords: ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("buildSummaryText() missing %q in:\n%s", want, text)
		}
	}
}

func TestBuildSummaryTextEmpty(t *testing.T) {
	item := mediaItem{
		Title:     "Track",
		Artists:   []string{"Artist"},
		Album:     "Album",
		MediaType: "Audio",
	}

	// No description data at all
	text := buildSummaryText(item, nil)
	if text != "" {
		t.Errorf("buildSummaryText(nil enrich) = %q, want empty", text)
	}

	// Enrichment but no summary/annotation
	enrich := &AlbumMetadata{
		Album: "Album",
		MusicBrainz: &MBMetadata{
			Genres: []string{"Rock"},
			Year:   2020,
		},
	}
	text = buildSummaryText(item, enrich)
	if text != "" {
		t.Errorf("buildSummaryText(no description) = %q, want empty", text)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func TestRepairFromSidecar(t *testing.T) {
	tests := []struct {
		name        string
		item        mediaItem
		meta        *AlbumMetadata
		policy      RepairPolicy
		wantArtists []string
		wantAlbum   string
		wantSource  string
	}{
		{
			name: "empty artist + ArtistInfo.Name repaired at strict",
			item: mediaItem{Artists: nil, Album: "SomeAlbum"},
			meta: &AlbumMetadata{
				Artist: "Sidecar Artist",
				Album:  "Sidecar Album",
				ArtistInfo: &ArtistInfo{
					Name: "MB Canonical Artist",
				},
			},
			policy:      RepairPolicyStrict,
			wantArtists: []string{"MB Canonical Artist"},
			wantAlbum:   "SomeAlbum",
			wantSource:  "sidecar",
		},
		{
			name: "empty artist + ArtistInfo.Name repaired at balanced",
			item: mediaItem{Artists: nil, Album: "SomeAlbum"},
			meta: &AlbumMetadata{
				Artist: "Sidecar Artist",
				Album:  "Sidecar Album",
				ArtistInfo: &ArtistInfo{
					Name: "MB Canonical Artist",
				},
			},
			policy:      RepairPolicyBalanced,
			wantArtists: []string{"MB Canonical Artist"},
			wantAlbum:   "SomeAlbum",
			wantSource:  "sidecar",
		},
		{
			name: "empty artist + ArtistInfo.Name repaired at aggressive",
			item: mediaItem{Artists: nil, Album: "SomeAlbum"},
			meta: &AlbumMetadata{
				Artist: "Sidecar Artist",
				Album:  "Sidecar Album",
				ArtistInfo: &ArtistInfo{
					Name: "MB Canonical Artist",
				},
			},
			policy:      RepairPolicyAggressive,
			wantArtists: []string{"MB Canonical Artist"},
			wantAlbum:   "SomeAlbum",
			wantSource:  "sidecar",
		},
		{
			name: "empty artist + only meta.Artist (no ArtistInfo) repaired at balanced",
			item: mediaItem{Artists: nil, Album: "SomeAlbum"},
			meta: &AlbumMetadata{
				Artist: "Sidecar Artist",
				Album:  "Sidecar Album",
			},
			policy:      RepairPolicyBalanced,
			wantArtists: []string{"Sidecar Artist"},
			wantAlbum:   "SomeAlbum",
			wantSource:  "sidecar",
		},
		{
			name: "empty artist + only meta.Artist NOT repaired at strict",
			item: mediaItem{Artists: nil, Album: "SomeAlbum"},
			meta: &AlbumMetadata{
				Artist: "Sidecar Artist",
				Album:  "Sidecar Album",
			},
			policy:      RepairPolicyStrict,
			wantArtists: nil,
			wantAlbum:   "SomeAlbum",
			wantSource:  "original",
		},
		{
			name: "existing artist not overwritten",
			item: mediaItem{Artists: []string{"Existing Artist"}, Album: "Existing Album"},
			meta: &AlbumMetadata{
				Artist: "Sidecar Artist",
				Album:  "Sidecar Album",
				ArtistInfo: &ArtistInfo{
					Name: "MB Canonical Artist",
				},
			},
			policy:      RepairPolicyAggressive,
			wantArtists: []string{"Existing Artist"},
			wantAlbum:   "Existing Album",
			wantSource:  "original",
		},
		{
			name: "empty album repaired from sidecar",
			item: mediaItem{Artists: []string{"Some Artist"}, Album: ""},
			meta: &AlbumMetadata{
				Artist: "Sidecar Artist",
				Album:  "Sidecar Album",
			},
			policy:      RepairPolicyBalanced,
			wantArtists: []string{"Some Artist"},
			wantAlbum:   "Sidecar Album",
			wantSource:  "sidecar",
		},
		{
			name: "Unknown Album repaired from sidecar",
			item: mediaItem{Artists: []string{"Some Artist"}, Album: "Unknown Album"},
			meta: &AlbumMetadata{
				Artist: "Sidecar Artist",
				Album:  "Sidecar Album",
			},
			policy:      RepairPolicyBalanced,
			wantArtists: []string{"Some Artist"},
			wantAlbum:   "Sidecar Album",
			wantSource:  "sidecar",
		},
		{
			name: "policy none returns original",
			item: mediaItem{Artists: nil, Album: ""},
			meta: &AlbumMetadata{
				Artist: "Sidecar Artist",
				Album:  "Sidecar Album",
				ArtistInfo: &ArtistInfo{
					Name: "MB Canonical Artist",
				},
			},
			policy:      RepairPolicyNone,
			wantArtists: nil,
			wantAlbum:   "",
			wantSource:  "original",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := repairFromSidecar(tt.item, tt.meta, tt.policy)
			if result.Source != tt.wantSource {
				t.Errorf("source = %q, want %q", result.Source, tt.wantSource)
			}
			if len(result.Artists) != len(tt.wantArtists) {
				t.Errorf("artists = %v, want %v", result.Artists, tt.wantArtists)
			} else {
				for i, a := range result.Artists {
					if a != tt.wantArtists[i] {
						t.Errorf("artists[%d] = %q, want %q", i, a, tt.wantArtists[i])
					}
				}
			}
			if result.Album != tt.wantAlbum {
				t.Errorf("album = %q, want %q", result.Album, tt.wantAlbum)
			}
		})
	}
}

func TestEmbeddingCache(t *testing.T) {
	cache, err := NewEmbeddingCache("")
	if err != nil {
		t.Fatalf("NewEmbeddingCache: %v", err)
	}

	// Cache miss
	if _, ok := cache.Get("item1", "text"); ok {
		t.Error("expected cache miss")
	}

	// Cache put and get
	vec := []float32{1, 2, 3}
	cache.Put("item1", "text", vec)

	got, ok := cache.Get("item1", "text")
	if !ok {
		t.Error("expected cache hit")
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Errorf("cache returned %v, want %v", got, vec)
	}

	// Different text is different key
	if _, ok := cache.Get("item1", "different"); ok {
		t.Error("expected cache miss for different text")
	}
}

func TestEmbeddingCacheWithDir(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewEmbeddingCache(dir)
	if err != nil {
		t.Fatalf("NewEmbeddingCache: %v", err)
	}

	vec := []float32{4, 5, 6}
	cache.Put("item2", "text2", vec)

	// Create new cache instance to test persistence
	cache2, err := NewEmbeddingCache(dir)
	if err != nil {
		t.Fatalf("NewEmbeddingCache second: %v", err)
	}

	got, ok := cache2.Get("item2", "text2")
	if !ok {
		t.Error("expected cache hit from disk")
	}
	if len(got) != 3 || got[0] != 4 {
		t.Errorf("cache returned %v, want %v", got, vec)
	}
}

func TestExtractInstruments(t *testing.T) {
	tests := []struct {
		name    string
		credits []DiscogsCredit
		want    []string
	}{
		{
			name: "basic roles",
			credits: []DiscogsCredit{
				{Name: "A", Role: "piano"},
				{Name: "B", Role: "drums"},
				{Name: "C", Role: "bass"},
			},
			want: []string{"bass", "drums", "piano"},
		},
		{
			name: "compound roles split on comma",
			credits: []DiscogsCredit{
				{Name: "A", Role: "guitar, vocals"},
				{Name: "B", Role: "bass, backing vocals"},
			},
			want: []string{"backing vocals", "bass", "guitar", "vocals"},
		},
		{
			name: "filters non-instrument roles",
			credits: []DiscogsCredit{
				{Name: "A", Role: "piano"},
				{Name: "B", Role: "producer"},
				{Name: "C", Role: "engineer"},
				{Name: "D", Role: "drums"},
				{Name: "E", Role: "mixed by"},
			},
			want: []string{"drums", "piano"},
		},
		{
			name: "dedup and case normalize",
			credits: []DiscogsCredit{
				{Name: "A", Role: "Piano"},
				{Name: "B", Role: "piano"},
				{Name: "C", Role: "PIANO"},
			},
			want: []string{"piano"},
		},
		{
			name: "empty credits",
			credits: nil,
			want:    nil,
		},
		{
			name: "capped at 15",
			credits: func() []DiscogsCredit {
				var credits []DiscogsCredit
				for i := 0; i < 20; i++ {
					credits = append(credits, DiscogsCredit{Name: "A", Role: fmt.Sprintf("instrument%02d", i)})
				}
				return credits
			}(),
			want: func() []string {
				var out []string
				for i := 0; i < 15; i++ {
					out = append(out, fmt.Sprintf("instrument%02d", i))
				}
				return out
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractInstruments(tt.credits)
			if !slicesEqual(got, tt.want) {
				t.Errorf("extractInstruments() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildEmbedTextWithInstruments(t *testing.T) {
	item := mediaItem{
		Title:     "Track",
		Artists:   []string{"Artist"},
		Album:     "Album",
		MediaType: "Audio",
	}
	enrich := &AlbumMetadata{
		Album: "Album",
		Discogs: &DiscogsMetadata{
			Instruments: []string{"bass", "drums", "guitar"},
		},
	}
	text := buildEmbedText(item, enrich)
	if !strings.Contains(text, "instruments: bass; drums; guitar") {
		t.Errorf("buildEmbedText() missing instruments line in:\n%s", text)
	}
}

func TestBuildSummaryTextGeneratedSummary(t *testing.T) {
	item := mediaItem{
		Title:     "So What",
		Artists:   []string{"Miles Davis"},
		Album:     "Kind of Blue",
		MediaType: "Audio",
	}

	t.Run("prefers GeneratedSummary over Wikipedia", func(t *testing.T) {
		enrich := &AlbumMetadata{
			Album: "Kind of Blue",
			MusicBrainz: &MBMetadata{
				Year: 1959,
			},
			Description: &AlbumDescription{
				GeneratedSummary: "A cool modal jazz album with spacious arrangements.",
				WikipediaSummary: "Kind of Blue is a studio album by Miles Davis.",
				MBAnnotation:     "Some annotation text.",
			},
		}
		text := buildSummaryText(item, enrich)
		if !strings.Contains(text, "summary: A cool modal jazz album") {
			t.Errorf("expected GeneratedSummary, got:\n%s", text)
		}
		if strings.Contains(text, "studio album by Miles Davis") {
			t.Errorf("should not contain WikipediaSummary when GeneratedSummary exists:\n%s", text)
		}
	})

	t.Run("falls back to Wikipedia when no GeneratedSummary", func(t *testing.T) {
		enrich := &AlbumMetadata{
			Album: "Kind of Blue",
			MusicBrainz: &MBMetadata{
				Year: 1959,
			},
			Description: &AlbumDescription{
				WikipediaSummary: "Kind of Blue is a studio album by Miles Davis.",
			},
		}
		text := buildSummaryText(item, enrich)
		if !strings.Contains(text, "summary: Kind of Blue is a studio album") {
			t.Errorf("expected WikipediaSummary fallback, got:\n%s", text)
		}
	})

	t.Run("includes instruments in keywords", func(t *testing.T) {
		enrich := &AlbumMetadata{
			Album: "Kind of Blue",
			Description: &AlbumDescription{
				GeneratedSummary: "A modal jazz album.",
			},
			Discogs: &DiscogsMetadata{
				Instruments: []string{"trumpet", "piano"},
			},
		}
		text := buildSummaryText(item, enrich)
		if !strings.Contains(text, "piano") || !strings.Contains(text, "trumpet") {
			t.Errorf("expected instruments in keywords, got:\n%s", text)
		}
	})
}

func TestOllamaGenerator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ollamaGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Model != "test-model" {
			t.Errorf("unexpected model: %s", req.Model)
		}
		if req.Stream {
			t.Error("expected stream=false")
		}

		resp := ollamaGenerateResponse{
			Response: "A spacious modal jazz album featuring trumpet and saxophone.",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	gen := NewOllamaGenerator(server.URL, "test-model")
	result, err := gen.Generate(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if result != "A spacious modal jazz album featuring trumpet and saxophone." {
		t.Errorf("Generate() = %q, want specific summary", result)
	}
}

func TestOllamaGeneratorError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer server.Close()

	gen := NewOllamaGenerator(server.URL, "missing-model")
	_, err := gen.Generate(context.Background(), "test prompt")
	if err == nil {
		t.Fatal("expected error from Generate()")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
}

// newTestModule creates a module, scans, and starts the HTTP server.
func newTestModule(t *testing.T, root string, exts []string) *Module {
	t.Helper()
	if exts == nil {
		exts = []string{".mp3", ".mkv"}
	}
	mod, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:         "mu:library:filesystem:test:browse",
		Roots:          []string{root},
		IncludeExts:    exts,
		HTTPListen:     "127.0.0.1:0",
		ScanIntervalMS: 0,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	if err := mod.scan(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return mod
}

func browseContainer(t *testing.T, mod *Module, containerID string) libraryItemsReply {
	t.Helper()
	cmd := mu.CommandEnvelope{
		ID:   "b",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: containerID, Start: 0, Count: 100}),
	}
	reply := mod.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var result libraryItemsReply
	if err := json.Unmarshal(reply.Body, &result); err != nil {
		t.Fatalf("browse unmarshal: %v", err)
	}
	return result
}

func resolveItem(t *testing.T, mod *Module, itemID string) mu.LibraryResolveReply {
	t.Helper()
	cmd := mu.CommandEnvelope{
		ID:   "r",
		Type: "library.resolve",
		Body: mustJSON(mu.LibraryResolveBody{ItemID: itemID}),
	}
	reply := mod.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var result mu.LibraryResolveReply
	if err := json.Unmarshal(reply.Body, &result); err != nil {
		t.Fatalf("resolve unmarshal: %v", err)
	}
	return result
}

func TestBrowseGenreHierarchy(t *testing.T) {
	root := t.TempDir()
	// Create two albums
	dir1 := filepath.Join(root, "ArtistA", "AlbumX")
	dir2 := filepath.Join(root, "ArtistB", "AlbumY")
	if err := os.MkdirAll(dir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir1, "ArtistA - Track1.mp3"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir2, "ArtistB - Track2.mp3"), []byte(""), 0o644)

	mod := newTestModule(t, root, []string{".mp3"})

	// Write a sidecar file for ArtistA/AlbumX with genres
	sidecar := AlbumMetadata{
		Version:   3,
		FetchedAt: time.Now(),
		Artist:    "ArtistA",
		Album:     "AlbumX",
		MusicBrainz: &MBMetadata{
			Genres: []string{"Rock", "Blues"},
		},
	}
	sidecarData, _ := json.Marshal(sidecar)
	os.WriteFile(filepath.Join(dir1, ".mu_album_metadata.json"), sidecarData, 0o644)

	// Re-scan to pick up sidecar and rebuild indexes
	if err := mod.scan(); err != nil {
		t.Fatalf("rescan: %v", err)
	}

	// Browse genre list
	genres := browseContainer(t, mod, "container:audio:bygenre")
	if genres.Total < 2 {
		t.Fatalf("expected at least 2 genres (Rock, Blues, Unknown), got %d", genres.Total)
	}

	// Find "Rock" genre and browse it
	var rockID string
	for _, g := range genres.Items {
		if g.Name == "Rock" {
			rockID = g.ItemID
		}
	}
	if rockID == "" {
		t.Fatal("expected to find Rock genre")
	}

	albums := browseContainer(t, mod, rockID)
	if albums.Total != 1 {
		t.Fatalf("expected 1 album in Rock, got %d", albums.Total)
	}
	if !strings.Contains(albums.Items[0].Name, "ArtistA") {
		t.Errorf("expected ArtistA in album name, got %q", albums.Items[0].Name)
	}

	// Verify "Unknown" exists for unenriched album
	var unknownID string
	for _, g := range genres.Items {
		if g.Name == "Unknown" {
			unknownID = g.ItemID
		}
	}
	if unknownID == "" {
		t.Fatal("expected to find Unknown genre for unenriched album")
	}
	unknownAlbums := browseContainer(t, mod, unknownID)
	if unknownAlbums.Total != 1 {
		t.Fatalf("expected 1 album in Unknown, got %d", unknownAlbums.Total)
	}
}

func TestBrowseLetterHierarchy(t *testing.T) {
	root := t.TempDir()
	// Create artists starting with different letters
	for _, name := range []string{"Alpha", "Beta", "123Band"} {
		dir := filepath.Join(root, name, "Album")
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, name+" - Track.mp3"), []byte(""), 0o644)
	}

	mod := newTestModule(t, root, []string{".mp3"})

	// Browse letter list
	letters := browseContainer(t, mod, "container:audio:byartist")
	if letters.Total < 3 {
		t.Fatalf("expected at least 3 letters (A, B, #), got %d", letters.Total)
	}

	// Verify A-Z sorted first, # at end
	lastItem := letters.Items[len(letters.Items)-1]
	if lastItem.Name != "#" {
		t.Errorf("expected # at end, got %q", lastItem.Name)
	}

	// Browse letter "A" → should have "Alpha"
	var aLetterID string
	for _, l := range letters.Items {
		if l.Name == "A" {
			aLetterID = l.ItemID
		}
	}
	if aLetterID == "" {
		t.Fatal("expected to find letter A")
	}
	artists := browseContainer(t, mod, aLetterID)
	if artists.Total != 1 || artists.Items[0].Name != "Alpha" {
		t.Fatalf("expected Alpha under A, got %+v", artists.Items)
	}

	// Browse "#" → should have "123Band"
	var hashLetterID string
	for _, l := range letters.Items {
		if l.Name == "#" {
			hashLetterID = l.ItemID
		}
	}
	if hashLetterID == "" {
		t.Fatal("expected to find letter #")
	}
	hashArtists := browseContainer(t, mod, hashLetterID)
	if hashArtists.Total != 1 || hashArtists.Items[0].Name != "123Band" {
		t.Fatalf("expected 123Band under #, got %+v", hashArtists.Items)
	}
}

func TestBrowseRecentAudio(t *testing.T) {
	root := t.TempDir()
	// Create albums with different mtimes
	for i, name := range []string{"OldArtist", "NewArtist", "NewestArtist"} {
		dir := filepath.Join(root, name, "Album")
		os.MkdirAll(dir, 0o755)
		fpath := filepath.Join(dir, name+" - Track.mp3")
		os.WriteFile(fpath, []byte("data"), 0o644)
		// Set different mtimes: oldest first
		mtime := time.Now().Add(time.Duration(i-3) * time.Hour)
		os.Chtimes(fpath, mtime, mtime)
	}

	mod := newTestModule(t, root, []string{".mp3"})

	recent := browseContainer(t, mod, "container:audio:recent")
	if recent.Total != 3 {
		t.Fatalf("expected 3 recent albums, got %d", recent.Total)
	}
	// Newest should be first
	if !strings.Contains(recent.Items[0].Name, "NewestArtist") {
		t.Errorf("expected newest album first, got %q", recent.Items[0].Name)
	}
	// Oldest should be last
	if !strings.Contains(recent.Items[2].Name, "OldArtist") {
		t.Errorf("expected oldest album last, got %q", recent.Items[2].Name)
	}
}

func TestBrowseRecentVideo(t *testing.T) {
	root := t.TempDir()
	// Create videos with different mtimes
	for i, name := range []string{"old.mkv", "new.mkv", "newest.mkv"} {
		fpath := filepath.Join(root, name)
		os.WriteFile(fpath, []byte("data"), 0o644)
		mtime := time.Now().Add(time.Duration(i-3) * time.Hour)
		os.Chtimes(fpath, mtime, mtime)
	}

	mod := newTestModule(t, root, []string{".mkv"})

	// Browse video root → 2 sub-categories
	videoRoot := browseContainer(t, mod, "container:video")
	if videoRoot.Total != 2 {
		t.Fatalf("expected 2 video sub-categories, got %d", videoRoot.Total)
	}

	recent := browseContainer(t, mod, "container:video:recent")
	if recent.Total != 3 {
		t.Fatalf("expected 3 recent videos, got %d", recent.Total)
	}
	// Newest should be first
	if recent.Items[0].Name != "newest" {
		t.Errorf("expected newest video first, got %q", recent.Items[0].Name)
	}
}

func TestBrowseFolderTree(t *testing.T) {
	root := t.TempDir()
	// Create nested directory structure
	subDir := filepath.Join(root, "sub")
	subSubDir := filepath.Join(root, "sub", "deep")
	os.MkdirAll(subSubDir, 0o755)
	os.WriteFile(filepath.Join(root, "root_track.mp3"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(subDir, "sub_track.mp3"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(subSubDir, "deep_track.mp3"), []byte(""), 0o644)

	mod := newTestModule(t, root, []string{".mp3"})

	// Browse folder roots
	roots := browseContainer(t, mod, "container:audio:byfolder")
	if roots.Total == 0 {
		t.Fatal("expected at least one folder root")
	}

	// Browse root folder → should have sub-dir + root_track
	rootFolderID := roots.Items[0].ItemID
	rootContents := browseContainer(t, mod, rootFolderID)
	hasFolder := false
	hasTrack := false
	for _, item := range rootContents.Items {
		if item.Type == "Folder" {
			hasFolder = true
		}
		if item.Type == "Audio" {
			hasTrack = true
		}
	}
	if !hasFolder {
		t.Error("expected folder in root dir contents")
	}
	if !hasTrack {
		t.Error("expected track in root dir contents")
	}
}

func TestResolveNewContainers(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "TestArtist", "TestAlbum")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "TestArtist - Track.mp3"), []byte(""), 0o644)

	mod := newTestModule(t, root, []string{".mp3"})

	// Resolve new fixed containers
	for _, tc := range []struct {
		id    string
		title string
	}{
		{"container:audio:bygenre", "By Genre"},
		{"container:audio:byartist", "By Artist"},
		{"container:audio:recent", "Recently Added"},
		{"container:audio:byfolder", "By Folder"},
		{"container:video:recent", "Recently Added"},
		{"container:video:byfolder", "By Folder"},
	} {
		r := resolveItem(t, mod, tc.id)
		if r.Metadata["title"] != tc.title {
			t.Errorf("resolve %s: expected title %q, got %v", tc.id, tc.title, r.Metadata["title"])
		}
		if r.Metadata["type"] != "Folder" {
			t.Errorf("resolve %s: expected type Folder, got %v", tc.id, r.Metadata["type"])
		}
	}

	// Resolve genre container
	genres := browseContainer(t, mod, "container:audio:bygenre")
	if genres.Total == 0 {
		t.Fatal("expected at least one genre")
	}
	genreResolve := resolveItem(t, mod, genres.Items[0].ItemID)
	if genreResolve.Metadata["type"] != "MusicGenre" {
		t.Errorf("expected genre type MusicGenre, got %v", genreResolve.Metadata["type"])
	}

	// Resolve letter container
	letters := browseContainer(t, mod, "container:audio:byartist")
	if letters.Total == 0 {
		t.Fatal("expected at least one letter")
	}
	letterResolve := resolveItem(t, mod, letters.Items[0].ItemID)
	if letterResolve.Metadata["type"] != "Folder" {
		t.Errorf("expected letter type Folder, got %v", letterResolve.Metadata["type"])
	}

	// Resolve folder container
	folders := browseContainer(t, mod, "container:audio:byfolder")
	if folders.Total == 0 {
		t.Fatal("expected at least one folder root")
	}
	folderResolve := resolveItem(t, mod, folders.Items[0].ItemID)
	if folderResolve.Metadata["type"] != "Folder" {
		t.Errorf("expected folder type Folder, got %v", folderResolve.Metadata["type"])
	}
}
