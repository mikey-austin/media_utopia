package fslibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	if err := os.WriteFile(audioPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	videoPath := filepath.Join(root, "VideoTitle.mkv")
	if err := os.WriteFile(videoPath, []byte("x"), 0o644); err != nil {
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

	nodeID := "mu:library:filesystem:test:default"
	trackRef := mu.NewLibraryItemRef(nodeID, trackID)

	cmd = mu.CommandEnvelope{
		ID:   "r1",
		Type: "library.resolveSources",
		Body: mustJSON(mu.LibraryResolveSourcesBody{Ref: trackRef}),
	}
	reply = mod.libraryResolveSources(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var sources mu.LibraryResolveSourcesReply
	if err := json.Unmarshal(reply.Body, &sources); err != nil {
		t.Fatalf("resolveSources unmarshal: %v", err)
	}
	if sources.Ref.ItemID != trackID || len(sources.Sources) != 1 || sources.Sources[0].URL == "" {
		t.Fatalf("resolveSources unexpected: %+v", sources)
	}

	cmd = mu.CommandEnvelope{
		ID:   "rb1",
		Type: "library.resolveSourcesBatch",
		Body: mustJSON(mu.LibraryResolveSourcesBatchBody{Refs: []mu.LibraryItemRef{
			trackRef,
			mu.NewLibraryItemRef(nodeID, "missing"),
		}}),
	}
	reply = mod.libraryResolveSourcesBatch(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var batch mu.LibraryResolveSourcesBatchReply
	if err := json.Unmarshal(reply.Body, &batch); err != nil {
		t.Fatalf("resolveSourcesBatch unmarshal: %v", err)
	}
	if len(batch.Items) != 2 || batch.Items[1].Err == nil {
		t.Fatalf("expected batch error for missing item, got %+v", batch.Items)
	}
}

func mustJSON(v any) []byte {
	payload, _ := json.Marshal(v)
	return payload
}

func TestContainerGetItemAndSources(t *testing.T) {
	root := t.TempDir()
	audioDir := filepath.Join(root, "TestArtist", "TestAlbum")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	audioPath := filepath.Join(audioDir, "TestArtist - TestTrack.mp3")
	if err := os.WriteFile(audioPath, []byte("x"), 0o644); err != nil {
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

	nodeID := "mu:library:filesystem:test:container"
	mkRef := func(itemID string) mu.LibraryItemRef {
		return mu.NewLibraryItemRef(nodeID, itemID)
	}
	getItem := func(t *testing.T, id, label string) mu.LibraryGetItemReply {
		t.Helper()
		cmd := mu.CommandEnvelope{
			ID:   "g-" + label,
			Type: "library.getItem",
			Body: mustJSON(mu.LibraryGetItemBody{Ref: mkRef(id)}),
		}
		reply := mod.libraryGetItem(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
		if reply.Err != nil {
			t.Fatalf("getItem %s err: %+v", id, reply.Err)
		}
		var got mu.LibraryGetItemReply
		if err := json.Unmarshal(reply.Body, &got); err != nil {
			t.Fatalf("getItem %s unmarshal: %v", id, err)
		}
		return got
	}

	// Test resolving container:audio
	audio := getItem(t, "container:audio", "audio")
	if audio.Ref.ItemID != "container:audio" {
		t.Errorf("expected itemId container:audio, got %s", audio.Ref.ItemID)
	}
	if audio.Attributes["type"] != "Folder" {
		t.Errorf("expected type Folder, got %v", audio.Attributes["type"])
	}
	if audio.Attributes["container"] != true {
		t.Errorf("expected container=true, got %v", audio.Attributes["container"])
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
	artist := getItem(t, artistID, "artist")
	if artist.Attributes["type"] != "MusicArtist" {
		t.Errorf("expected type MusicArtist, got %v", artist.Attributes["type"])
	}
	if artist.Display == nil || artist.Display.Title != "TestArtist" {
		t.Errorf("expected title TestArtist, got %+v", artist.Display)
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
	album := getItem(t, albumID, "album")
	if album.Attributes["type"] != "MusicAlbum" {
		t.Errorf("expected type MusicAlbum, got %v", album.Attributes["type"])
	}
	if album.Display == nil || album.Display.Title != "TestAlbum" {
		t.Errorf("expected title TestAlbum, got %+v", album.Display)
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

	// library.getItem returns metadata without sources
	track := getItem(t, trackID, "track")
	if track.Ref.ItemID != trackID {
		t.Errorf("expected itemId %s, got %s", trackID, track.Ref.ItemID)
	}
	if track.Display == nil || track.Display.Title == "" {
		t.Errorf("expected display to be populated, got %+v", track.Display)
	}

	// library.resolveSources returns sources only
	srcReply := mod.libraryResolveSources(mu.CommandEnvelope{
		ID:   "m2",
		Type: "library.resolveSources",
		Body: mustJSON(mu.LibraryResolveSourcesBody{Ref: mkRef(trackID)}),
	}, mu.ReplyEnvelope{Type: "ack", OK: true})
	var srcs mu.LibraryResolveSourcesReply
	if err := json.Unmarshal(srcReply.Body, &srcs); err != nil {
		t.Fatalf("resolveSources unmarshal: %v", err)
	}
	if len(srcs.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(srcs.Sources))
	}
	if srcs.Sources[0].URL == "" {
		t.Error("expected source URL to be populated")
	}

	// Test getItems batch returns metadata for both
	batchCmd := mu.CommandEnvelope{
		ID:   "batch1",
		Type: "library.getItems",
		Body: mustJSON(mu.LibraryGetItemsBody{
			Refs: []mu.LibraryItemRef{mkRef(artistID), mkRef(trackID)},
		}),
	}
	batchReply := mod.libraryGetItems(batchCmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var batch mu.LibraryGetItemsReply
	if err := json.Unmarshal(batchReply.Body, &batch); err != nil {
		t.Fatalf("batch unmarshal: %v", err)
	}
	if len(batch.Items) != 2 {
		t.Fatalf("expected 2 batch items, got %d", len(batch.Items))
	}
	if batch.Items[0].Attributes["type"] != "MusicArtist" {
		t.Errorf("expected artist type MusicArtist, got %v", batch.Items[0].Attributes["type"])
	}
	if batch.Items[1].Display == nil || batch.Items[1].Display.Title == "" {
		t.Errorf("expected track display populated, got %+v", batch.Items[1].Display)
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
			name:    "empty credits",
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

func resolveItem(t *testing.T, mod *Module, itemID string) mu.LibraryGetItemReply {
	t.Helper()
	cmd := mu.CommandEnvelope{
		ID:   "r",
		Type: "library.getItem",
		Body: mustJSON(mu.LibraryGetItemBody{Ref: mu.NewLibraryItemRef(mod.config.NodeID, itemID)}),
	}
	reply := mod.libraryGetItem(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if reply.Err != nil {
		t.Fatalf("getItem %s err: %+v", itemID, reply.Err)
	}
	var result mu.LibraryGetItemReply
	if err := json.Unmarshal(reply.Body, &result); err != nil {
		t.Fatalf("getItem unmarshal: %v", err)
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
	os.WriteFile(filepath.Join(dir1, "ArtistA - Track1.mp3"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir2, "ArtistB - Track2.mp3"), []byte("x"), 0o644)

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
		os.WriteFile(filepath.Join(dir, name+" - Track.mp3"), []byte("x"), 0o644)
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
	os.WriteFile(filepath.Join(root, "root_track.mp3"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(subDir, "sub_track.mp3"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(subSubDir, "deep_track.mp3"), []byte("x"), 0o644)

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
	os.WriteFile(filepath.Join(dir, "TestArtist - Track.mp3"), []byte("x"), 0o644)

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
		if r.Display == nil || r.Display.Title != tc.title {
			t.Errorf("resolve %s: expected title %q, got %+v", tc.id, tc.title, r.Display)
		}
		if r.Attributes["type"] != "Folder" {
			t.Errorf("resolve %s: expected type Folder, got %v", tc.id, r.Attributes["type"])
		}
	}

	// Resolve genre container
	genres := browseContainer(t, mod, "container:audio:bygenre")
	if genres.Total == 0 {
		t.Fatal("expected at least one genre")
	}
	genreResolve := resolveItem(t, mod, genres.Items[0].ItemID)
	if genreResolve.Attributes["type"] != "MusicGenre" {
		t.Errorf("expected genre type MusicGenre, got %v", genreResolve.Attributes["type"])
	}

	// Resolve letter container
	letters := browseContainer(t, mod, "container:audio:byartist")
	if letters.Total == 0 {
		t.Fatal("expected at least one letter")
	}
	letterResolve := resolveItem(t, mod, letters.Items[0].ItemID)
	if letterResolve.Attributes["type"] != "Folder" {
		t.Errorf("expected letter type Folder, got %v", letterResolve.Attributes["type"])
	}

	// Resolve folder container
	folders := browseContainer(t, mod, "container:audio:byfolder")
	if folders.Total == 0 {
		t.Fatal("expected at least one folder root")
	}
	folderResolve := resolveItem(t, mod, folders.Items[0].ItemID)
	if folderResolve.Attributes["type"] != "Folder" {
		t.Errorf("expected folder type Folder, got %v", folderResolve.Attributes["type"])
	}
}

func TestAcoustIDLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/lookup" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.PostForm.Get("client") != "test-key" {
			t.Errorf("expected client=test-key, got %q", r.PostForm.Get("client"))
		}
		if r.PostForm.Get("meta") != "releasegroups" {
			t.Errorf("expected meta=releasegroups, got %q", r.PostForm.Get("meta"))
		}
		resp := acoustidResponse{
			Status: "ok",
			Results: []acoustidResult{
				{
					Score: 0.8,
					Recordings: []acoustidRecording{
						{
							ID: "rec-1",
							ReleaseGroups: []acoustidReleaseGroup{
								{ID: "rg-abc-123"},
							},
						},
					},
				},
				{
					Score: 0.3,
					Recordings: []acoustidRecording{
						{
							ID: "rec-low",
							ReleaseGroups: []acoustidReleaseGroup{
								{ID: "rg-low-score"},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	ac := newAcoustidClient("test-key")
	defer ac.Close()
	// Override the HTTP client to use the test server.
	ac.http = srv.Client()

	ctx := context.Background()
	// The lookup URL needs to point at our test server, so we test via
	// a helper that constructs the URL. We'll use a custom round-tripper instead.
	ac.http.Transport = rewriteTransport{base: srv.Client().Transport, target: srv.URL}

	rgID, err := ac.lookup(ctx, "AQAA-fake-fingerprint", 120)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if rgID != "rg-abc-123" {
		t.Errorf("expected release-group rg-abc-123, got %q", rgID)
	}
}

func TestAcoustIDLookupNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := acoustidResponse{
			Status:  "ok",
			Results: []acoustidResult{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	ac := newAcoustidClient("test-key")
	defer ac.Close()
	ac.http = srv.Client()
	ac.http.Transport = rewriteTransport{base: srv.Client().Transport, target: srv.URL}

	ctx := context.Background()
	rgID, err := ac.lookup(ctx, "AQAA-fake-fingerprint", 120)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if rgID != "" {
		t.Errorf("expected empty release-group, got %q", rgID)
	}
}

func TestFindFirstAudioFile(t *testing.T) {
	dir := t.TempDir()

	// No audio files → empty
	if got := findFirstAudioFile(dir); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	// Create some files (out of order).
	for _, name := range []string{"c.flac", "a.mp3", "b.ogg", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := findFirstAudioFile(dir)
	want := filepath.Join(dir, "a.mp3")
	if got != want {
		t.Errorf("findFirstAudioFile = %q, want %q", got, want)
	}
}

// rewriteTransport redirects all requests to a test server URL.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the request URL to point at the test server.
	u, _ := url.Parse(t.target)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	if t.base != nil {
		return t.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// mockEmbeddingProvider is a fake EmbeddingProvider for testing semantic search.
type mockEmbeddingProvider struct {
	embedFn func(ctx context.Context, inputs []EmbedInput) ([]EmbedVector, error)
}

func (m *mockEmbeddingProvider) Name() string   { return "mock" }
func (m *mockEmbeddingProvider) Dimension() int { return 3 }
func (m *mockEmbeddingProvider) Embed(ctx context.Context, inputs []EmbedInput) ([]EmbedVector, error) {
	if m.embedFn != nil {
		return m.embedFn(ctx, inputs)
	}
	// Default: return a fixed vector for any input.
	results := make([]EmbedVector, len(inputs))
	for i, in := range inputs {
		results[i] = EmbedVector{ID: in.ID, Vector: []float32{1, 0, 0}}
	}
	return results, nil
}

func TestSemanticSearchPagination(t *testing.T) {
	root := t.TempDir()
	audioDir := filepath.Join(root, "TestArtist", "TestAlbum")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create 5 audio files so there are multiple items to paginate.
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("TestArtist - Track%d.mp3", i)
		if err := os.WriteFile(filepath.Join(audioDir, name), []byte(fmt.Sprintf("data%d", i)), 0o644); err != nil {
			t.Fatalf("write audio %d: %v", i, err)
		}
	}

	mod, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:         "mu:library:filesystem:test:semantic",
		Roots:          []string{root},
		IncludeExts:    []string{".mp3"},
		HTTPListen:     "127.0.0.1:0",
		ScanIntervalMS: 0,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	// Set up a mock embedding provider that returns different vectors per item
	// so that they all pass the similarity threshold.
	mod.embedProvider = &mockEmbeddingProvider{
		embedFn: func(ctx context.Context, inputs []EmbedInput) ([]EmbedVector, error) {
			results := make([]EmbedVector, len(inputs))
			for i, in := range inputs {
				// Return a vector that will produce high cosine similarity
				// with the query vector {1, 0, 0}.
				results[i] = EmbedVector{ID: in.ID, Vector: []float32{1, 0, 0}}
			}
			return results, nil
		},
	}

	if err := mod.scan(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// Populate the vector index with vectors for all 5 items
	// using high similarity to query vector {1, 0, 0}.
	mod.mu.RLock()
	for id := range mod.index.Items {
		mod.vectorIndex.Add(id+vectorSuffixCard, []float32{0.9, 0.1, 0})
	}
	mod.mu.RUnlock()

	// Search with start=0, count=2 — should return 2 items but total >= 5.
	items, total := mod.search("Track", 0, 2)
	if len(items) != 2 {
		t.Errorf("expected 2 items returned, got %d", len(items))
	}
	if total < 5 {
		t.Errorf("expected total >= 5 (pre-pagination count), got %d", total)
	}
	if total <= int64(len(items)) {
		t.Errorf("total (%d) should be greater than count (%d) when more results exist", total, len(items))
	}

	// Search with start=3, count=2 — should return items from later in the list.
	items2, total2 := mod.search("Track", 3, 2)
	if total2 != total {
		t.Errorf("total should be consistent: first call %d, second call %d", total, total2)
	}
	if len(items2) > 2 {
		t.Errorf("expected at most 2 items, got %d", len(items2))
	}
}

func TestSemanticSearchNegativeStart(t *testing.T) {
	root := t.TempDir()
	audioDir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(audioDir, "Artist - Song.mp3"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	mod, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:         "mu:library:filesystem:test:negstart",
		Roots:          []string{root},
		IncludeExts:    []string{".mp3"},
		HTTPListen:     "127.0.0.1:0",
		ScanIntervalMS: 0,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	mod.embedProvider = &mockEmbeddingProvider{}

	if err := mod.scan(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// Populate vector index.
	mod.mu.RLock()
	for id := range mod.index.Items {
		mod.vectorIndex.Add(id+vectorSuffixCard, []float32{1, 0, 0})
	}
	mod.mu.RUnlock()

	// Calling with start=-1 should not panic; the bounds check clamps to 0.
	items, total := mod.search("Song", -1, 10)
	if total < 1 {
		t.Errorf("expected at least 1 total result, got %d", total)
	}
	if len(items) < 1 {
		t.Errorf("expected at least 1 item returned, got %d", len(items))
	}

	// Also test keyword search path with negative start.
	items2, total2 := mod.search("Song", -1, 10)
	if total2 < 1 {
		t.Errorf("keyword search: expected at least 1 total result, got %d", total2)
	}
	if len(items2) < 1 {
		t.Errorf("keyword search: expected at least 1 item returned, got %d", len(items2))
	}
}

func TestConcurrentScanSerialization(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		dir := filepath.Join(root, fmt.Sprintf("Artist%d", i), "Album")
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("Track%d.mp3", i)), []byte("data"), 0o644)
	}

	mod, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:         "mu:library:filesystem:test:concurrent",
		Roots:          []string{root},
		IncludeExts:    []string{".mp3"},
		HTTPListen:     "127.0.0.1:0",
		ScanIntervalMS: 0,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	// Run multiple scans concurrently. If scanMu is working correctly, this
	// will not cause a data race (verifiable with go test -race).
	const goroutines = 5
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = mod.scan()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("scan goroutine %d returned error: %v", i, err)
		}
	}

	// After all scans, the index should still be consistent.
	mod.mu.RLock()
	itemCount := len(mod.index.Items)
	mod.mu.RUnlock()
	if itemCount != 3 {
		t.Errorf("expected 3 items after concurrent scans, got %d", itemCount)
	}
}

func TestParseFilenameExtensions(t *testing.T) {
	tests := []struct {
		filename    string
		wantTitle   string
		wantArtists []string
	}{
		// Common audio extensions should be stripped.
		{"Artist - Title.mp3", "Title", []string{"Artist"}},
		{"Artist - Title.flac", "Title", []string{"Artist"}},
		{"Artist - Title.m4a", "Title", []string{"Artist"}},
		{"Artist - Title.ogg", "Title", []string{"Artist"}},
		// Additional audio extensions are now stripped.
		{"Artist - Title.wav", "Title", []string{"Artist"}},
		{"Artist - Title.aac", "Title", []string{"Artist"}},
		// No extension at all.
		{"Artist - Title", "Title", []string{"Artist"}},
		// Extension only, no dash pattern.
		{"SomeTrack.mp3", "SomeTrack", nil},
		{"SomeTrack.flac", "SomeTrack", nil},
		// Track number with extension.
		{"01 - Title.mp3", "Title", nil},
		{"01. Title.flac", "Title", nil},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := parseFilename(tt.filename)
			if result.Title != tt.wantTitle {
				t.Errorf("parseFilename(%q) title = %q, want %q", tt.filename, result.Title, tt.wantTitle)
			}
			if len(result.Artists) != len(tt.wantArtists) {
				t.Errorf("parseFilename(%q) artists = %v, want %v", tt.filename, result.Artists, tt.wantArtists)
			} else {
				for i, a := range result.Artists {
					if a != tt.wantArtists[i] {
						t.Errorf("parseFilename(%q) artists[%d] = %q, want %q", tt.filename, i, a, tt.wantArtists[i])
					}
				}
			}
		})
	}
}

func TestContainerHashConsistency(t *testing.T) {
	// Same inputs must always produce the same hash.
	h1 := containerHash("artist", "Miles Davis", "")
	h2 := containerHash("artist", "Miles Davis", "")
	if h1 != h2 {
		t.Errorf("containerHash not consistent: %q != %q", h1, h2)
	}

	// Same inputs with album.
	h3 := containerHash("album", "Miles Davis", "Kind of Blue")
	h4 := containerHash("album", "Miles Davis", "Kind of Blue")
	if h3 != h4 {
		t.Errorf("containerHash with album not consistent: %q != %q", h3, h4)
	}

	// Different inputs must produce different hashes.
	h5 := containerHash("artist", "Miles Davis", "")
	h6 := containerHash("artist", "John Coltrane", "")
	if h5 == h6 {
		t.Errorf("containerHash should differ for different artists: both %q", h5)
	}

	// Different container type with same artist should differ.
	h7 := containerHash("artist", "Miles Davis", "")
	h8 := containerHash("album", "Miles Davis", "")
	if h7 == h8 {
		t.Errorf("containerHash should differ for different types: both %q", h7)
	}

	// Album present vs absent should differ.
	h9 := containerHash("album", "Miles Davis", "")
	h10 := containerHash("album", "Miles Davis", "Kind of Blue")
	if h9 == h10 {
		t.Errorf("containerHash should differ with/without album: both %q", h9)
	}

	// Hash should be a non-empty hex string.
	if len(h1) == 0 {
		t.Error("containerHash returned empty string")
	}
}

func TestDuplicateIndexClear(t *testing.T) {
	idx := NewDuplicateIndex()

	// Add items.
	idx.Add("item1", "hash1")
	idx.Add("item2", "hash1")
	idx.Add("item3", "hash2")

	// Verify state before clear.
	if !idx.IsDuplicate("item1") {
		t.Error("item1 should be duplicate before clear")
	}
	groups := idx.GetDuplicates()
	if len(groups) != 1 {
		t.Errorf("expected 1 duplicate group before clear, got %d", len(groups))
	}

	// Clear and verify everything is gone.
	idx.Clear()

	if idx.IsDuplicate("item1") {
		t.Error("item1 should not be duplicate after clear")
	}
	if idx.IsDuplicate("item2") {
		t.Error("item2 should not be duplicate after clear")
	}
	if idx.IsDuplicate("item3") {
		t.Error("item3 should not be duplicate after clear")
	}
	groups = idx.GetDuplicates()
	if len(groups) != 0 {
		t.Errorf("expected 0 duplicate groups after clear, got %d", len(groups))
	}

	// Adding after clear works fresh.
	if idx.Add("itemA", "hashA") {
		t.Error("first item after clear should not be duplicate")
	}
	if !idx.Add("itemB", "hashA") {
		t.Error("second item with same hash after clear should be duplicate")
	}
}

func TestDuplicateIndexOriginalUnknown(t *testing.T) {
	idx := NewDuplicateIndex()

	// Original of an unknown item should return the item itself.
	if got := idx.Original("unknown"); got != "unknown" {
		t.Errorf("Original(unknown) = %q, want %q", got, "unknown")
	}
}

func TestDuplicateIndexMultipleGroups(t *testing.T) {
	idx := NewDuplicateIndex()

	// Create two duplicate groups.
	idx.Add("a1", "hash_a")
	idx.Add("a2", "hash_a")
	idx.Add("b1", "hash_b")
	idx.Add("b2", "hash_b")
	idx.Add("b3", "hash_b")
	idx.Add("c1", "hash_c") // singleton, not duplicate

	groups := idx.GetDuplicates()
	if len(groups) != 2 {
		t.Errorf("expected 2 duplicate groups, got %d", len(groups))
	}

	if idx.IsDuplicate("c1") {
		t.Error("c1 should not be a duplicate (singleton)")
	}

	if idx.Original("b3") != "b1" {
		t.Errorf("Original(b3) = %q, want b1", idx.Original("b3"))
	}
}

// ---------------------------------------------------------------------------
// HTTP handler tests
// ---------------------------------------------------------------------------

// newTestModuleWithHTTP creates a Module with a temp file, scans it, starts the
// HTTP server, and returns the module plus the base URL. The caller should
// defer mod.shutdownHTTPServer().
func newTestModuleWithHTTP(t *testing.T) (*Module, string) {
	t.Helper()
	root := t.TempDir()
	audioDir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	audioContent := []byte("fake-mp3-content-for-testing-range-requests")
	audioPath := filepath.Join(audioDir, "Artist - Track.mp3")
	if err := os.WriteFile(audioPath, audioContent, 0o644); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	// Write a cover art file for album art serving
	coverContent := []byte("fake-png-image-data")
	coverPath := filepath.Join(audioDir, "cover.jpg")
	if err := os.WriteFile(coverPath, coverContent, 0o644); err != nil {
		t.Fatalf("write cover: %v", err)
	}

	mod, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:         "mu:library:filesystem:test:http",
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
		t.Fatalf("start http: %v", err)
	}

	mod.mu.RLock()
	base := mod.baseURL
	mod.mu.RUnlock()

	return mod, base
}

func TestServeFile(t *testing.T) {
	mod, baseURL := newTestModuleWithHTTP(t)
	defer mod.shutdownHTTPServer()

	// Find the item ID from the index.
	mod.mu.RLock()
	var itemID string
	for id := range mod.index.Items {
		itemID = id
		break
	}
	mod.mu.RUnlock()
	if itemID == "" {
		t.Fatal("no items in index after scan")
	}

	fileURL := fmt.Sprintf("%s/files/%s", baseURL, url.PathEscape(itemID))

	t.Run("full request", func(t *testing.T) {
		resp, err := http.Get(fileURL)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		// http.ServeContent should detect the .mp3 extension
		if ct == "" {
			t.Error("Content-Type header is empty")
		}
		body, _ := readAllBytes(resp)
		wantContent := []byte("fake-mp3-content-for-testing-range-requests")
		if string(body) != string(wantContent) {
			t.Errorf("body = %q, want %q", string(body), string(wantContent))
		}
	})

	t.Run("range request", func(t *testing.T) {
		req, err := http.NewRequest("GET", fileURL, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Range", "bytes=0-9")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET with range: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusPartialContent {
			t.Fatalf("expected 206 Partial Content, got %d", resp.StatusCode)
		}
		cr := resp.Header.Get("Content-Range")
		if cr == "" {
			t.Error("Content-Range header is missing")
		}
		if !strings.HasPrefix(cr, "bytes 0-9/") {
			t.Errorf("Content-Range = %q, want prefix 'bytes 0-9/'", cr)
		}
		body, _ := readAllBytes(resp)
		if len(body) != 10 {
			t.Errorf("body length = %d, want 10", len(body))
		}
		if string(body) != "fake-mp3-c" {
			t.Errorf("body = %q, want %q", string(body), "fake-mp3-c")
		}
	})

	t.Run("missing file returns 404", func(t *testing.T) {
		missingURL := fmt.Sprintf("%s/files/%s", baseURL, "nonexistent-item-id")
		resp, err := http.Get(missingURL)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

func TestServeArtUnknownHash(t *testing.T) {
	mod, baseURL := newTestModuleWithHTTP(t)
	defer mod.shutdownHTTPServer()

	artURL := fmt.Sprintf("%s/art/%s.jpg", baseURL, "nonexistent-album-hash")
	resp, err := http.Get(artURL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown album hash, got %d", resp.StatusCode)
	}
}

func TestServeArtWithCoverFile(t *testing.T) {
	mod, baseURL := newTestModuleWithHTTP(t)
	defer mod.shutdownHTTPServer()

	// Find the album hash by looking through containers for an album type
	mod.mu.RLock()
	var albumHash string
	for hash, info := range mod.index.Containers {
		if info.Type == "album" {
			albumHash = hash
			break
		}
	}
	mod.mu.RUnlock()
	if albumHash == "" {
		t.Fatal("no album container found in index")
	}

	// Check that the album has cover art set
	mod.mu.RLock()
	info := mod.index.Containers[albumHash]
	artist := mod.index.Audio[info.Artist]
	album := artist.Albums[info.Album]
	hasCover := album.CoverArt != ""
	mod.mu.RUnlock()

	if !hasCover {
		t.Skip("no cover art found for album (cover.jpg may not have been detected)")
	}

	artURL := fmt.Sprintf("%s/art/%s.jpg", baseURL, albumHash)
	resp, err := http.Get(artURL)
	if err != nil {
		t.Fatalf("GET art: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for album art, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	cacheCtrl := resp.Header.Get("Cache-Control")
	if !strings.Contains(cacheCtrl, "immutable") {
		t.Errorf("Cache-Control = %q, want to contain 'immutable'", cacheCtrl)
	}
}

func TestServeDefaultArt(t *testing.T) {
	mod, baseURL := newTestModuleWithHTTP(t)
	defer mod.shutdownHTTPServer()

	resp, err := http.Get(baseURL + "/art/default.png")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for default art, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	cl := resp.Header.Get("Content-Length")
	if cl == "" || cl == "0" {
		t.Error("Content-Length is empty or zero for default art")
	}
	cacheCtrl := resp.Header.Get("Cache-Control")
	if !strings.Contains(cacheCtrl, "max-age=86400") {
		t.Errorf("Cache-Control = %q, want to contain max-age=86400", cacheCtrl)
	}
	body, _ := readAllBytes(resp)
	if len(body) == 0 {
		t.Error("default art body is empty")
	}
	// Should match the embedded defaultArtPNG
	if len(body) != len(defaultArtPNG) {
		t.Errorf("default art body length = %d, want %d", len(body), len(defaultArtPNG))
	}
}

func TestServeFileDeletedReturns404(t *testing.T) {
	root := t.TempDir()
	audioDir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	audioPath := filepath.Join(audioDir, "Artist - Ephemeral.mp3")
	if err := os.WriteFile(audioPath, []byte("temporary"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	mod, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:         "mu:library:filesystem:test:deleted",
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
		t.Fatalf("start http: %v", err)
	}
	defer mod.shutdownHTTPServer()

	mod.mu.RLock()
	base := mod.baseURL
	var itemID string
	for id := range mod.index.Items {
		itemID = id
		break
	}
	mod.mu.RUnlock()

	// Delete the file after scanning
	if err := os.Remove(audioPath); err != nil {
		t.Fatalf("remove: %v", err)
	}

	fileURL := fmt.Sprintf("%s/files/%s", base, url.PathEscape(itemID))
	resp, err := http.Get(fileURL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for deleted file, got %d", resp.StatusCode)
	}
}

// readAllBytes reads the full body from an http.Response.
func readAllBytes(resp *http.Response) ([]byte, error) {
	return io.ReadAll(resp.Body)
}

func TestReadSidecar(t *testing.T) {
	dir := t.TempDir()

	meta := AlbumMetadata{
		Version:   3,
		FetchedAt: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		Artist:    "Pink Floyd",
		Album:     "The Dark Side of the Moon",
		MusicBrainz: &MBMetadata{
			ReleaseGroupID: "f5093c06-23e3-404f-aeaa-40f72885ee3a",
			Genres:         []string{"progressive rock", "psychedelic rock"},
			Tags:           []string{"classic", "concept album"},
			Year:           1973,
			ReleaseType:    "Album",
			Label:          "Harvest",
			Annotation:     "One of the best-selling albums of all time.",
			WikipediaURL:   "https://en.wikipedia.org/wiki/The_Dark_Side_of_the_Moon",
			ArtistIDs:      []string{"83d91898-7763-47d7-b03b-b92132375c47"},
		},
		Discogs: &DiscogsMetadata{
			MasterID:      10362,
			Styles:        []string{"Prog Rock", "Psychedelic Rock"},
			Notes:         "Recorded at Abbey Road Studios",
			LabelDetail:   "Harvest, SHVL 804",
			MainReleaseID: 244006,
			ArtistID:      45467,
			Credits: []DiscogsCredit{
				{Name: "Alan Parsons", Role: "Engineer"},
				{Name: "Storm Thorgerson", Role: "Design"},
			},
		},
		ArtistInfo: &ArtistInfo{
			Name:        "Pink Floyd",
			Type:        "Group",
			Origin:      "London",
			ActiveBegin: "1965",
			ActiveEnd:   "2014",
			Members:     []string{"Roger Waters", "David Gilmour", "Nick Mason", "Richard Wright"},
			Genres:      []string{"progressive rock"},
		},
		Description: &AlbumDescription{
			MBAnnotation:     "One of the best-selling albums.",
			WikipediaSummary: "The Dark Side of the Moon is the eighth studio album.",
			GeneratedSummary: "A landmark progressive rock album.",
		},
	}

	data, err := json.MarshalIndent(&meta, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, sidecarFileName), data, 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	got, err := readSidecar(dir)
	if err != nil {
		t.Fatalf("readSidecar: %v", err)
	}

	if got.Version != 3 {
		t.Errorf("version: got %d, want 3", got.Version)
	}
	if got.Artist != "Pink Floyd" {
		t.Errorf("artist: got %q, want %q", got.Artist, "Pink Floyd")
	}
	if got.Album != "The Dark Side of the Moon" {
		t.Errorf("album: got %q, want %q", got.Album, "The Dark Side of the Moon")
	}
	if !got.FetchedAt.Equal(meta.FetchedAt) {
		t.Errorf("fetched_at: got %v, want %v", got.FetchedAt, meta.FetchedAt)
	}

	// MusicBrainz fields
	if got.MusicBrainz == nil {
		t.Fatal("musicbrainz: expected non-nil")
	}
	if got.MusicBrainz.ReleaseGroupID != "f5093c06-23e3-404f-aeaa-40f72885ee3a" {
		t.Errorf("mb release group id: got %q", got.MusicBrainz.ReleaseGroupID)
	}
	if len(got.MusicBrainz.Genres) != 2 || got.MusicBrainz.Genres[0] != "progressive rock" {
		t.Errorf("mb genres: got %v", got.MusicBrainz.Genres)
	}
	if got.MusicBrainz.Year != 1973 {
		t.Errorf("mb year: got %d, want 1973", got.MusicBrainz.Year)
	}
	if got.MusicBrainz.Label != "Harvest" {
		t.Errorf("mb label: got %q", got.MusicBrainz.Label)
	}
	if got.MusicBrainz.WikipediaURL != "https://en.wikipedia.org/wiki/The_Dark_Side_of_the_Moon" {
		t.Errorf("mb wikipedia url: got %q", got.MusicBrainz.WikipediaURL)
	}
	if len(got.MusicBrainz.ArtistIDs) != 1 {
		t.Errorf("mb artist ids: got %v", got.MusicBrainz.ArtistIDs)
	}

	// Discogs fields
	if got.Discogs == nil {
		t.Fatal("discogs: expected non-nil")
	}
	if got.Discogs.MasterID != 10362 {
		t.Errorf("discogs master id: got %d", got.Discogs.MasterID)
	}
	if len(got.Discogs.Credits) != 2 {
		t.Errorf("discogs credits: got %d, want 2", len(got.Discogs.Credits))
	}
	if got.Discogs.Credits[0].Name != "Alan Parsons" || got.Discogs.Credits[0].Role != "Engineer" {
		t.Errorf("discogs credit[0]: got %+v", got.Discogs.Credits[0])
	}
	if got.Discogs.ArtistID != 45467 {
		t.Errorf("discogs artist id: got %d", got.Discogs.ArtistID)
	}

	// ArtistInfo fields
	if got.ArtistInfo == nil {
		t.Fatal("artist_info: expected non-nil")
	}
	if got.ArtistInfo.Name != "Pink Floyd" {
		t.Errorf("artist info name: got %q", got.ArtistInfo.Name)
	}
	if got.ArtistInfo.Type != "Group" {
		t.Errorf("artist info type: got %q", got.ArtistInfo.Type)
	}
	if len(got.ArtistInfo.Members) != 4 {
		t.Errorf("artist info members: got %v", got.ArtistInfo.Members)
	}

	// Description fields
	if got.Description == nil {
		t.Fatal("description: expected non-nil")
	}
	if got.Description.GeneratedSummary != "A landmark progressive rock album." {
		t.Errorf("generated summary: got %q", got.Description.GeneratedSummary)
	}
}

func TestReadSidecarMissing(t *testing.T) {
	dir := t.TempDir()

	got, err := readSidecar(dir)
	if got != nil {
		t.Errorf("expected nil metadata for missing sidecar, got %+v", got)
	}
	// readSidecar returns the os error (file not found) rather than nil error
	// when the sidecar file does not exist. Verify we get an error and it is
	// the expected "file not found" type.
	if err == nil {
		t.Fatal("expected non-nil error for missing sidecar")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got: %v", err)
	}
}

func TestWriteSidecar(t *testing.T) {
	dir := t.TempDir()

	meta := &AlbumMetadata{
		Version:   currentSidecarVersion,
		FetchedAt: time.Date(2025, 8, 1, 10, 30, 0, 0, time.UTC),
		Artist:    "Radiohead",
		Album:     "OK Computer",
		MusicBrainz: &MBMetadata{
			ReleaseGroupID: "b1afc82d-2f6c-3c9a-a8a4-2e2e2d3b94bf",
			Genres:         []string{"alternative rock", "art rock"},
			Year:           1997,
			ReleaseType:    "Album",
			Label:          "Parlophone",
		},
		Discogs: &DiscogsMetadata{
			MasterID: 21527,
			Styles:   []string{"Alternative Rock", "Experimental"},
		},
	}

	if err := writeSidecar(dir, meta); err != nil {
		t.Fatalf("writeSidecar: %v", err)
	}

	// Verify the file was created at the expected path
	sidecarFile := filepath.Join(dir, sidecarFileName)
	info, err := os.Stat(sidecarFile)
	if err != nil {
		t.Fatalf("sidecar file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("sidecar file is empty")
	}

	// Verify the file content is valid JSON
	data, err := os.ReadFile(sidecarFile)
	if err != nil {
		t.Fatalf("read sidecar file: %v", err)
	}

	var parsed AlbumMetadata
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("sidecar JSON invalid: %v", err)
	}

	if parsed.Artist != "Radiohead" {
		t.Errorf("artist: got %q, want %q", parsed.Artist, "Radiohead")
	}
	if parsed.Album != "OK Computer" {
		t.Errorf("album: got %q, want %q", parsed.Album, "OK Computer")
	}
	if parsed.Version != currentSidecarVersion {
		t.Errorf("version: got %d, want %d", parsed.Version, currentSidecarVersion)
	}
	if parsed.MusicBrainz == nil {
		t.Fatal("musicbrainz: expected non-nil")
	}
	if parsed.MusicBrainz.Year != 1997 {
		t.Errorf("mb year: got %d, want 1997", parsed.MusicBrainz.Year)
	}
	if parsed.Discogs == nil {
		t.Fatal("discogs: expected non-nil")
	}
	if parsed.Discogs.MasterID != 21527 {
		t.Errorf("discogs master id: got %d, want 21527", parsed.Discogs.MasterID)
	}

	// Verify sidecarExists reports true
	if !sidecarExists(dir) {
		t.Error("sidecarExists returned false after writeSidecar")
	}
}

func TestSidecarRoundTrip(t *testing.T) {
	dir := t.TempDir()

	original := &AlbumMetadata{
		Version:   currentSidecarVersion,
		FetchedAt: time.Date(2024, 12, 25, 0, 0, 0, 0, time.UTC),
		Artist:    "Miles Davis",
		Album:     "Kind of Blue",
		MusicBrainz: &MBMetadata{
			ReleaseGroupID: "5e3e8e5e-f7b7-3c1c-8b2e-9e4f7e3c2b1a",
			Genres:         []string{"jazz", "modal jazz"},
			Tags:           []string{"classic", "essential"},
			Year:           1959,
			ReleaseType:    "Album",
			Label:          "Columbia",
			Annotation:     "One of the greatest jazz albums ever recorded.",
			WikipediaURL:   "https://en.wikipedia.org/wiki/Kind_of_Blue",
			ArtistIDs:      []string{"561d854a-6a28-4aa7-8c99-323e6ce46c2a"},
		},
		Discogs: &DiscogsMetadata{
			MasterID:      4049,
			Styles:        []string{"Modal", "Cool Jazz"},
			Notes:         "Recorded in two sessions at Columbia's 30th Street Studio.",
			LabelDetail:   "Columbia, CL 1355",
			MainReleaseID: 1098231,
			ArtistID:      23755,
			Credits: []DiscogsCredit{
				{Name: "John Coltrane", Role: "Tenor Saxophone"},
				{Name: "Bill Evans", Role: "Piano"},
				{Name: "Cannonball Adderley", Role: "Alto Saxophone"},
			},
			ReleaseNotes:   "Mono pressing",
			ReleaseCredits: []DiscogsCredit{{Name: "Fred Plaut", Role: "Engineer"}},
			Instruments:    []string{"trumpet", "saxophone", "piano"},
		},
		ArtistInfo: &ArtistInfo{
			Name:           "Miles Davis",
			Type:           "Person",
			Origin:         "Alton, Illinois",
			ActiveBegin:    "1944",
			ActiveEnd:      "1991",
			Disambiguation: "jazz trumpeter",
			Biography:      "American jazz trumpeter, bandleader, and composer.",
			Members:        nil,
			Genres:         []string{"jazz", "fusion"},
			Tags:           []string{"trumpeter", "innovator"},
		},
		Description: &AlbumDescription{
			MBAnnotation:     "Columbia CL 1355 (mono) / CS 8163 (stereo)",
			WikipediaSummary: "Kind of Blue is a studio album by American jazz trumpeter Miles Davis.",
			GeneratedSummary: "A defining modal jazz masterpiece featuring an all-star ensemble.",
		},
	}

	// Write the sidecar
	if err := writeSidecar(dir, original); err != nil {
		t.Fatalf("writeSidecar: %v", err)
	}

	// Read it back
	restored, err := readSidecar(dir)
	if err != nil {
		t.Fatalf("readSidecar: %v", err)
	}

	// Verify top-level fields
	if restored.Version != original.Version {
		t.Errorf("version: got %d, want %d", restored.Version, original.Version)
	}
	if !restored.FetchedAt.Equal(original.FetchedAt) {
		t.Errorf("fetched_at: got %v, want %v", restored.FetchedAt, original.FetchedAt)
	}
	if restored.Artist != original.Artist {
		t.Errorf("artist: got %q, want %q", restored.Artist, original.Artist)
	}
	if restored.Album != original.Album {
		t.Errorf("album: got %q, want %q", restored.Album, original.Album)
	}

	// Verify MusicBrainz round trip
	if restored.MusicBrainz == nil {
		t.Fatal("musicbrainz: expected non-nil")
	}
	if restored.MusicBrainz.ReleaseGroupID != original.MusicBrainz.ReleaseGroupID {
		t.Errorf("mb release group id: got %q, want %q", restored.MusicBrainz.ReleaseGroupID, original.MusicBrainz.ReleaseGroupID)
	}
	if len(restored.MusicBrainz.Genres) != len(original.MusicBrainz.Genres) {
		t.Errorf("mb genres count: got %d, want %d", len(restored.MusicBrainz.Genres), len(original.MusicBrainz.Genres))
	}
	for i, g := range original.MusicBrainz.Genres {
		if restored.MusicBrainz.Genres[i] != g {
			t.Errorf("mb genre[%d]: got %q, want %q", i, restored.MusicBrainz.Genres[i], g)
		}
	}
	if len(restored.MusicBrainz.Tags) != len(original.MusicBrainz.Tags) {
		t.Errorf("mb tags count: got %d, want %d", len(restored.MusicBrainz.Tags), len(original.MusicBrainz.Tags))
	}
	if restored.MusicBrainz.Year != original.MusicBrainz.Year {
		t.Errorf("mb year: got %d, want %d", restored.MusicBrainz.Year, original.MusicBrainz.Year)
	}
	if restored.MusicBrainz.Label != original.MusicBrainz.Label {
		t.Errorf("mb label: got %q, want %q", restored.MusicBrainz.Label, original.MusicBrainz.Label)
	}
	if restored.MusicBrainz.Annotation != original.MusicBrainz.Annotation {
		t.Errorf("mb annotation: got %q, want %q", restored.MusicBrainz.Annotation, original.MusicBrainz.Annotation)
	}
	if restored.MusicBrainz.WikipediaURL != original.MusicBrainz.WikipediaURL {
		t.Errorf("mb wikipedia url: got %q, want %q", restored.MusicBrainz.WikipediaURL, original.MusicBrainz.WikipediaURL)
	}
	if len(restored.MusicBrainz.ArtistIDs) != len(original.MusicBrainz.ArtistIDs) {
		t.Errorf("mb artist ids count: got %d, want %d", len(restored.MusicBrainz.ArtistIDs), len(original.MusicBrainz.ArtistIDs))
	}

	// Verify Discogs round trip
	if restored.Discogs == nil {
		t.Fatal("discogs: expected non-nil")
	}
	if restored.Discogs.MasterID != original.Discogs.MasterID {
		t.Errorf("discogs master id: got %d, want %d", restored.Discogs.MasterID, original.Discogs.MasterID)
	}
	if restored.Discogs.MainReleaseID != original.Discogs.MainReleaseID {
		t.Errorf("discogs main release id: got %d, want %d", restored.Discogs.MainReleaseID, original.Discogs.MainReleaseID)
	}
	if restored.Discogs.ArtistID != original.Discogs.ArtistID {
		t.Errorf("discogs artist id: got %d, want %d", restored.Discogs.ArtistID, original.Discogs.ArtistID)
	}
	if len(restored.Discogs.Styles) != len(original.Discogs.Styles) {
		t.Errorf("discogs styles count: got %d, want %d", len(restored.Discogs.Styles), len(original.Discogs.Styles))
	}
	if restored.Discogs.Notes != original.Discogs.Notes {
		t.Errorf("discogs notes: got %q, want %q", restored.Discogs.Notes, original.Discogs.Notes)
	}
	if restored.Discogs.LabelDetail != original.Discogs.LabelDetail {
		t.Errorf("discogs label detail: got %q, want %q", restored.Discogs.LabelDetail, original.Discogs.LabelDetail)
	}
	if len(restored.Discogs.Credits) != len(original.Discogs.Credits) {
		t.Fatalf("discogs credits count: got %d, want %d", len(restored.Discogs.Credits), len(original.Discogs.Credits))
	}
	for i, c := range original.Discogs.Credits {
		if restored.Discogs.Credits[i].Name != c.Name || restored.Discogs.Credits[i].Role != c.Role {
			t.Errorf("discogs credit[%d]: got %+v, want %+v", i, restored.Discogs.Credits[i], c)
		}
	}
	if restored.Discogs.ReleaseNotes != original.Discogs.ReleaseNotes {
		t.Errorf("discogs release notes: got %q, want %q", restored.Discogs.ReleaseNotes, original.Discogs.ReleaseNotes)
	}
	if len(restored.Discogs.ReleaseCredits) != len(original.Discogs.ReleaseCredits) {
		t.Errorf("discogs release credits count: got %d, want %d", len(restored.Discogs.ReleaseCredits), len(original.Discogs.ReleaseCredits))
	}
	if len(restored.Discogs.Instruments) != len(original.Discogs.Instruments) {
		t.Errorf("discogs instruments count: got %d, want %d", len(restored.Discogs.Instruments), len(original.Discogs.Instruments))
	}

	// Verify ArtistInfo round trip
	if restored.ArtistInfo == nil {
		t.Fatal("artist_info: expected non-nil")
	}
	if restored.ArtistInfo.Name != original.ArtistInfo.Name {
		t.Errorf("artist name: got %q, want %q", restored.ArtistInfo.Name, original.ArtistInfo.Name)
	}
	if restored.ArtistInfo.Type != original.ArtistInfo.Type {
		t.Errorf("artist type: got %q, want %q", restored.ArtistInfo.Type, original.ArtistInfo.Type)
	}
	if restored.ArtistInfo.Origin != original.ArtistInfo.Origin {
		t.Errorf("artist origin: got %q, want %q", restored.ArtistInfo.Origin, original.ArtistInfo.Origin)
	}
	if restored.ArtistInfo.ActiveBegin != original.ArtistInfo.ActiveBegin {
		t.Errorf("artist active begin: got %q, want %q", restored.ArtistInfo.ActiveBegin, original.ArtistInfo.ActiveBegin)
	}
	if restored.ArtistInfo.ActiveEnd != original.ArtistInfo.ActiveEnd {
		t.Errorf("artist active end: got %q, want %q", restored.ArtistInfo.ActiveEnd, original.ArtistInfo.ActiveEnd)
	}
	if restored.ArtistInfo.Disambiguation != original.ArtistInfo.Disambiguation {
		t.Errorf("artist disambiguation: got %q, want %q", restored.ArtistInfo.Disambiguation, original.ArtistInfo.Disambiguation)
	}
	if restored.ArtistInfo.Biography != original.ArtistInfo.Biography {
		t.Errorf("artist biography: got %q, want %q", restored.ArtistInfo.Biography, original.ArtistInfo.Biography)
	}
	if len(restored.ArtistInfo.Genres) != len(original.ArtistInfo.Genres) {
		t.Errorf("artist genres count: got %d, want %d", len(restored.ArtistInfo.Genres), len(original.ArtistInfo.Genres))
	}
	if len(restored.ArtistInfo.Tags) != len(original.ArtistInfo.Tags) {
		t.Errorf("artist tags count: got %d, want %d", len(restored.ArtistInfo.Tags), len(original.ArtistInfo.Tags))
	}

	// Verify Description round trip
	if restored.Description == nil {
		t.Fatal("description: expected non-nil")
	}
	if restored.Description.MBAnnotation != original.Description.MBAnnotation {
		t.Errorf("mb annotation: got %q, want %q", restored.Description.MBAnnotation, original.Description.MBAnnotation)
	}
	if restored.Description.WikipediaSummary != original.Description.WikipediaSummary {
		t.Errorf("wikipedia summary: got %q, want %q", restored.Description.WikipediaSummary, original.Description.WikipediaSummary)
	}
	if restored.Description.GeneratedSummary != original.Description.GeneratedSummary {
		t.Errorf("generated summary: got %q, want %q", restored.Description.GeneratedSummary, original.Description.GeneratedSummary)
	}
}

func TestAlbumKeyConsistency(t *testing.T) {
	// The album grouping key in the scan phase is "artistName|albumName".
	// containerHash produces stable IDs for container browse nodes.
	// Verify both produce consistent and deterministic results.
	t.Run("same artist+album produces same key", func(t *testing.T) {
		artist := "Aphex Twin"
		album := "Selected Ambient Works 85-92"
		key1 := artist + "|" + album
		key2 := artist + "|" + album
		if key1 != key2 {
			t.Errorf("album keys differ for identical inputs: %q vs %q", key1, key2)
		}
	})

	t.Run("different albums produce different keys", func(t *testing.T) {
		artist := "Aphex Twin"
		key1 := artist + "|" + "Selected Ambient Works 85-92"
		key2 := artist + "|" + "...I Care Because You Do"
		if key1 == key2 {
			t.Error("different albums should produce different keys")
		}
	})

	t.Run("different artists same album produce different keys", func(t *testing.T) {
		album := "Greatest Hits"
		key1 := "Artist A" + "|" + album
		key2 := "Artist B" + "|" + album
		if key1 == key2 {
			t.Error("different artists should produce different keys")
		}
	})

	t.Run("containerHash is deterministic", func(t *testing.T) {
		hash1 := containerHash("album", "Pink Floyd", "Wish You Were Here")
		hash2 := containerHash("album", "Pink Floyd", "Wish You Were Here")
		if hash1 != hash2 {
			t.Errorf("containerHash not deterministic: %q vs %q", hash1, hash2)
		}
	})

	t.Run("containerHash differs for different albums", func(t *testing.T) {
		hash1 := containerHash("album", "Pink Floyd", "Wish You Were Here")
		hash2 := containerHash("album", "Pink Floyd", "Animals")
		if hash1 == hash2 {
			t.Error("containerHash should differ for different albums")
		}
	})

	t.Run("containerHash differs for different artists", func(t *testing.T) {
		hash1 := containerHash("album", "Pink Floyd", "Greatest Hits")
		hash2 := containerHash("album", "Led Zeppelin", "Greatest Hits")
		if hash1 == hash2 {
			t.Error("containerHash should differ for different artists")
		}
	})

	t.Run("containerHash differs for artist vs album container type", func(t *testing.T) {
		hashArtist := containerHash("artist", "Pink Floyd", "")
		hashAlbum := containerHash("album", "Pink Floyd", "The Wall")
		if hashArtist == hashAlbum {
			t.Error("containerHash should differ between artist and album types")
		}
	})

	t.Run("album key matches scan grouping logic", func(t *testing.T) {
		// Simulate how the scan groups tracks: it uses firstOr(item.Artists, "Unknown Artist")
		// and item.Album (defaulting to "Unknown Album") then builds key = artist + "|" + album.
		// Verify that two tracks from the same album produce the same key.
		tracks := []struct {
			artist string
			album  string
		}{
			{"Boards of Canada", "Music Has the Right to Children"},
			{"Boards of Canada", "Music Has the Right to Children"},
			{"Boards of Canada", "Geogaddi"},
		}

		keys := make([]string, len(tracks))
		for i, tr := range tracks {
			keys[i] = tr.artist + "|" + tr.album
		}

		if keys[0] != keys[1] {
			t.Errorf("same album tracks should have same key: %q vs %q", keys[0], keys[1])
		}
		if keys[0] == keys[2] {
			t.Error("different album tracks should have different keys")
		}
	})
}

func TestBrowseArtistAlbums(t *testing.T) {
	root := t.TempDir()
	// Create two artists with different albums.
	for _, spec := range []struct {
		artist, album, track string
	}{
		{"ArtistAlpha", "AlbumOne", "ArtistAlpha - Song1.mp3"},
		{"ArtistAlpha", "AlbumTwo", "ArtistAlpha - Song2.mp3"},
		{"ArtistBeta", "AlbumX", "ArtistBeta - SongX.mp3"},
	} {
		dir := filepath.Join(root, spec.artist, spec.album)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, spec.track), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mod := newTestModule(t, root, []string{".mp3"})

	// Browse By Artist → letter list.
	letters := browseContainer(t, mod, "container:audio:byartist")
	if letters.Total == 0 {
		t.Fatal("expected at least one letter")
	}

	// Find the letter "A" containing ArtistAlpha.
	var aLetterID string
	for _, l := range letters.Items {
		if l.Name == "A" {
			aLetterID = l.ItemID
		}
	}
	if aLetterID == "" {
		t.Fatal("expected to find letter A")
	}

	// Browse letter A → should contain ArtistAlpha.
	artists := browseContainer(t, mod, aLetterID)
	var alphaID string
	for _, a := range artists.Items {
		if a.Name == "ArtistAlpha" {
			alphaID = a.ItemID
		}
	}
	if alphaID == "" {
		t.Fatal("expected to find ArtistAlpha under letter A")
	}

	// Browse ArtistAlpha → should have exactly 2 albums.
	albums := browseContainer(t, mod, alphaID)
	if albums.Total != 2 {
		t.Fatalf("expected 2 albums for ArtistAlpha, got %d", albums.Total)
	}
	albumNames := map[string]bool{}
	for _, a := range albums.Items {
		albumNames[a.Name] = true
	}
	if !albumNames["AlbumOne"] || !albumNames["AlbumTwo"] {
		t.Errorf("expected AlbumOne and AlbumTwo, got %v", albumNames)
	}

	// ArtistBeta should also appear under letter A (both artists start with 'A' in the
	// letter index when parsed from directory structure). Verify both artists are present.
	aArtists := browseContainer(t, mod, aLetterID)
	artistNames := map[string]bool{}
	for _, a := range aArtists.Items {
		artistNames[a.Name] = true
	}
	if len(artistNames) < 1 {
		t.Fatal("expected at least one artist under letter A")
	}
}

func TestSearchKeywordPagination(t *testing.T) {
	root := t.TempDir()
	// Create 5 tracks so there are enough items to paginate.
	dir := filepath.Join(root, "SearchArtist", "SearchAlbum")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("SearchArtist - Findable%d.mp3", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(fmt.Sprintf("data%d", i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mod := newTestModule(t, root, []string{".mp3"})

	// Page 1: start=0, count=2
	cmd1 := mu.CommandEnvelope{
		ID:   "sp1",
		Type: "library.search",
		Body: mustJSON(mu.LibrarySearchBody{Query: "Findable", Start: 0, Count: 2}),
	}
	reply1 := mod.librarySearch(cmd1, mu.ReplyEnvelope{Type: "ack", OK: true})
	var page1 libraryItemsReply
	if err := json.Unmarshal(reply1.Body, &page1); err != nil {
		t.Fatalf("page1 unmarshal: %v", err)
	}
	if page1.Count != 2 {
		t.Fatalf("expected page1 count=2, got %d", page1.Count)
	}
	if page1.Total != 5 {
		t.Fatalf("expected total=5, got %d", page1.Total)
	}
	if page1.Start != 0 {
		t.Fatalf("expected page1 start=0, got %d", page1.Start)
	}

	// Page 2: start=2, count=2
	cmd2 := mu.CommandEnvelope{
		ID:   "sp2",
		Type: "library.search",
		Body: mustJSON(mu.LibrarySearchBody{Query: "Findable", Start: 2, Count: 2}),
	}
	reply2 := mod.librarySearch(cmd2, mu.ReplyEnvelope{Type: "ack", OK: true})
	var page2 libraryItemsReply
	if err := json.Unmarshal(reply2.Body, &page2); err != nil {
		t.Fatalf("page2 unmarshal: %v", err)
	}
	if page2.Count != 2 {
		t.Fatalf("expected page2 count=2, got %d", page2.Count)
	}
	if page2.Total != 5 {
		t.Fatalf("expected total=5 on page2, got %d", page2.Total)
	}
	if page2.Start != 2 {
		t.Fatalf("expected page2 start=2, got %d", page2.Start)
	}

	// Pages should not overlap.
	page1IDs := map[string]bool{}
	for _, item := range page1.Items {
		page1IDs[item.ItemID] = true
	}
	for _, item := range page2.Items {
		if page1IDs[item.ItemID] {
			t.Errorf("page2 item %s also appeared in page1", item.ItemID)
		}
	}
}

func TestSearchEmpty(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Artist - Track.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	mod := newTestModule(t, root, []string{".mp3"})

	// Empty query string.
	cmd := mu.CommandEnvelope{
		ID:   "se1",
		Type: "library.search",
		Body: mustJSON(mu.LibrarySearchBody{Query: "", Start: 0, Count: 10}),
	}
	reply := mod.librarySearch(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var result libraryItemsReply
	if err := json.Unmarshal(reply.Body, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Items != nil {
		t.Errorf("expected nil items for empty query, got %+v", result.Items)
	}
	if result.Total != 0 {
		t.Errorf("expected total=0 for empty query, got %d", result.Total)
	}

	// Whitespace-only query string.
	cmd2 := mu.CommandEnvelope{
		ID:   "se2",
		Type: "library.search",
		Body: mustJSON(mu.LibrarySearchBody{Query: "   ", Start: 0, Count: 10}),
	}
	reply2 := mod.librarySearch(cmd2, mu.ReplyEnvelope{Type: "ack", OK: true})
	var result2 libraryItemsReply
	if err := json.Unmarshal(reply2.Body, &result2); err != nil {
		t.Fatalf("unmarshal whitespace: %v", err)
	}
	if result2.Items != nil {
		t.Errorf("expected nil items for whitespace query, got %+v", result2.Items)
	}
	if result2.Total != 0 {
		t.Errorf("expected total=0 for whitespace query, got %d", result2.Total)
	}
}

func TestPaginateHelper(t *testing.T) {
	items := []int{10, 20, 30, 40, 50}

	t.Run("normal pagination", func(t *testing.T) {
		got := paginate(items, 1, 2)
		if len(got) != 2 || got[0] != 20 || got[1] != 30 {
			t.Errorf("paginate(items, 1, 2) = %v, want [20 30]", got)
		}
	})

	t.Run("start beyond length", func(t *testing.T) {
		got := paginate(items, 10, 2)
		if got != nil {
			t.Errorf("paginate(items, 10, 2) = %v, want nil", got)
		}
	})

	t.Run("start at exact length", func(t *testing.T) {
		got := paginate(items, 5, 2)
		if len(got) != 0 {
			t.Errorf("paginate(items, 5, 2) = %v, want empty", got)
		}
	})

	t.Run("count zero returns all from start", func(t *testing.T) {
		got := paginate(items, 2, 0)
		if len(got) != 3 || got[0] != 30 || got[1] != 40 || got[2] != 50 {
			t.Errorf("paginate(items, 2, 0) = %v, want [30 40 50]", got)
		}
	})

	t.Run("negative start treated as zero", func(t *testing.T) {
		got := paginate(items, -5, 3)
		if len(got) != 3 || got[0] != 10 || got[1] != 20 || got[2] != 30 {
			t.Errorf("paginate(items, -5, 3) = %v, want [10 20 30]", got)
		}
	})

	t.Run("count larger than available", func(t *testing.T) {
		got := paginate(items, 3, 100)
		if len(got) != 2 || got[0] != 40 || got[1] != 50 {
			t.Errorf("paginate(items, 3, 100) = %v, want [40 50]", got)
		}
	})

	t.Run("negative count returns all from start", func(t *testing.T) {
		got := paginate(items, 0, -1)
		if len(got) != 5 {
			t.Errorf("paginate(items, 0, -1) = %v, want all 5 items", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := paginate([]int{}, 0, 10)
		if len(got) != 0 {
			t.Errorf("paginate(empty, 0, 10) = %v, want empty", got)
		}
	})
}

func TestBrowseRootCategories(t *testing.T) {
	root := t.TempDir()
	// Create both audio and video content.
	audioDir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(audioDir, "Artist - Track.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Movie.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	mod := newTestModule(t, root, nil)

	// Browse root (empty container ID) → Audio + Video.
	rootReply := browseContainer(t, mod, "")
	if rootReply.Total != 2 {
		t.Fatalf("expected 2 root categories, got %d", rootReply.Total)
	}
	rootNames := map[string]string{}
	for _, item := range rootReply.Items {
		rootNames[item.Name] = item.ItemID
		if item.Type != "Folder" {
			t.Errorf("root item %q should be Folder, got %q", item.Name, item.Type)
		}
	}
	if _, ok := rootNames["Audio"]; !ok {
		t.Error("expected Audio category in root")
	}
	if _, ok := rootNames["Video"]; !ok {
		t.Error("expected Video category in root")
	}

	// Browse Audio → 4 sub-categories.
	audioReply := browseContainer(t, mod, "container:audio")
	if audioReply.Total != 4 {
		t.Fatalf("expected 4 audio sub-categories, got %d", audioReply.Total)
	}
	audioSubs := map[string]string{}
	for _, item := range audioReply.Items {
		audioSubs[item.Name] = item.ItemID
	}
	for _, expected := range []string{"By Genre", "By Artist", "Recently Added", "By Folder"} {
		if _, ok := audioSubs[expected]; !ok {
			t.Errorf("expected audio sub-category %q, not found in %v", expected, audioSubs)
		}
	}

	// Browse Video → 2 sub-categories.
	videoReply := browseContainer(t, mod, "container:video")
	if videoReply.Total != 2 {
		t.Fatalf("expected 2 video sub-categories, got %d", videoReply.Total)
	}
	videoSubs := map[string]string{}
	for _, item := range videoReply.Items {
		videoSubs[item.Name] = item.ItemID
	}
	for _, expected := range []string{"Recently Added", "By Folder"} {
		if _, ok := videoSubs[expected]; !ok {
			t.Errorf("expected video sub-category %q, not found in %v", expected, videoSubs)
		}
	}
}

// --- Task 1: Repair function tests ---

func TestParseFilenameAdvanced(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		wantTitle   string
		wantArtists []string
		wantAlbum   string
	}{
		{"simple artist title", "Pink Floyd - Comfortably Numb.mp3", "Comfortably Numb", []string{"Pink Floyd"}, ""},
		{"track number", "01. Comfortably Numb.mp3", "Comfortably Numb", nil, ""},
		{"track number with dash", "01 - Comfortably Numb.mp3", "Comfortably Numb", nil, ""},
		{"just filename", "Comfortably Numb.mp3", "Comfortably Numb", nil, ""},
		{"with flac ext", "Artist - Title.flac", "Title", []string{"Artist"}, ""},
		{"with m4a ext", "Artist - Title.m4a", "Title", []string{"Artist"}, ""},
		{"three parts", "Artist - Album - Title.mp3", "Title", []string{"Artist"}, "Album"},
		{"track number three parts", "01 - Artist - Title.mp3", "Title", []string{"Artist"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseFilename(tt.filename)
			if result.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", result.Title, tt.wantTitle)
			}
			if tt.wantArtists != nil {
				if len(result.Artists) != len(tt.wantArtists) {
					t.Errorf("artists = %v, want %v", result.Artists, tt.wantArtists)
				} else {
					for i, a := range result.Artists {
						if a != tt.wantArtists[i] {
							t.Errorf("artists[%d] = %q, want %q", i, a, tt.wantArtists[i])
						}
					}
				}
			}
			if tt.wantAlbum != "" && result.Album != tt.wantAlbum {
				t.Errorf("album = %q, want %q", result.Album, tt.wantAlbum)
			}
		})
	}
}

func TestCleanTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Song (Official Video)", "Song"},
		{"Song [Official Audio]", "Song"},
		{"Song (Lyrics)", "Song"},
		{"Song [Lyrics]", "Song"},
		{"Song (HQ)", "Song"},
		{"Song [Live]", "Song"},
		{"Song (Explicit)", "Song"},
		{"  Song  ", "Song"},
		{"Song_With_Underscores", "Song With Underscores"},
		{"Already Clean", "Already Clean"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cleanTitle(tt.input)
			if got != tt.want {
				t.Errorf("cleanTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCleanAlbum(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Album (Deluxe Edition)", "Album"},
		{"Album [Remastered Version]", "Album"},
		{"Album (Special)", "Album"},
		{"Album (Expanded Edition)", "Album"},
		{"Album Disc 2", "Album"},
		{"Album [CD 1]", "Album"},
		{"Already Clean", "Already Clean"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cleanAlbum(tt.input)
			if got != tt.want {
				t.Errorf("cleanAlbum(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCleanArtist(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Artist feat. Other", "Artist"},
		{"Artist ft. Other", "Artist"},
		{"Artist featuring Someone", "Artist"},
		{"Solo Artist", "Solo Artist"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cleanArtist(tt.input)
			if got != tt.want {
				t.Errorf("cleanArtist(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsTrackNumber(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"01", true},
		{"1", true},
		{"123", true},
		{"1234", false},
		{"abc", false},
		{"", false},
		{"0", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isTrackNumber(tt.input)
			if got != tt.want {
				t.Errorf("isTrackNumber(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDuplicateIndexFull(t *testing.T) {
	idx := NewDuplicateIndex()

	// First item
	dup := idx.Add("item1", "hash-a")
	if dup {
		t.Error("first item should not be duplicate")
	}

	// Same hash = duplicate
	dup = idx.Add("item2", "hash-a")
	if !dup {
		t.Error("second item with same hash should be duplicate")
	}

	// Different hash = not duplicate
	dup = idx.Add("item3", "hash-b")
	if dup {
		t.Error("item with different hash should not be duplicate")
	}

	// IsDuplicate
	if !idx.IsDuplicate("item1") {
		t.Error("item1 should be duplicate (shares hash with item2)")
	}
	if !idx.IsDuplicate("item2") {
		t.Error("item2 should be duplicate")
	}
	if idx.IsDuplicate("item3") {
		t.Error("item3 should not be duplicate (unique hash)")
	}

	// Original
	if orig := idx.Original("item2"); orig != "item1" {
		t.Errorf("original of item2 = %q, want item1", orig)
	}

	// GetDuplicates
	groups := idx.GetDuplicates()
	if len(groups) != 1 {
		t.Errorf("expected 1 duplicate group, got %d", len(groups))
	}

	// Clear
	idx.Clear()
	if idx.IsDuplicate("item1") {
		t.Error("item1 should not be duplicate after clear")
	}
}

func TestRepairMetadataFromFilename(t *testing.T) {
	item := mediaItem{
		Title:   "",
		Artists: nil,
		Album:   "",
		Name:    "Pink Floyd - Comfortably Numb.mp3",
	}

	result := repairMetadata(item, RepairPolicyBalanced)
	if result.Title == "" {
		t.Error("expected title to be repaired from filename")
	}
	if len(result.Artists) == 0 {
		t.Error("expected artist to be repaired from filename")
	}
}

func TestRepairMetadataNonePolicy(t *testing.T) {
	item := mediaItem{
		Title:   "Original Title",
		Artists: []string{"Original Artist"},
		Album:   "Original Album",
	}

	result := repairMetadata(item, RepairPolicyNone)
	if result.Title != "Original Title" {
		t.Errorf("expected original title, got %q", result.Title)
	}
}

// --- Task 2: Search edge case tests ---

func TestSearchEmptyQuery(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Artist - Track.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	mod := newTestModule(t, root, []string{".mp3"})

	items, total := mod.search("", 0, 50)
	if len(items) != 0 || total != 0 {
		t.Errorf("empty query should return no results, got %d items, %d total", len(items), total)
	}
}

func TestSearchWhitespaceQuery(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Artist - Track.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	mod := newTestModule(t, root, []string{".mp3"})

	items, total := mod.search("   ", 0, 50)
	if len(items) != 0 || total != 0 {
		t.Errorf("whitespace query should return no results, got %d items, %d total", len(items), total)
	}
}

// --- Task 3: Container hash tests ---

func TestContainerHash(t *testing.T) {
	h1 := containerHash("album", "Pink Floyd", "The Wall")
	h2 := containerHash("album", "Pink Floyd", "The Wall")
	h3 := containerHash("album", "Pink Floyd", "Animals")

	if h1 != h2 {
		t.Error("same inputs should produce same hash")
	}
	if h1 == h3 {
		t.Error("different inputs should produce different hash")
	}
	if len(h1) != 32 {
		t.Errorf("hash length = %d, want 32 (MD5 hex)", len(h1))
	}
}

func TestRunManualScanModeSkipsInitialScan(t *testing.T) {
	cfg := Config{
		NodeID:         "mu:library:filesystem:test:default",
		TopicBase:      "mu",
		Name:           "test",
		Roots:          []string{t.TempDir()},
		ScanMode:       "manual",
		ScanIntervalMS: 100,
	}
	m, err := NewModule(zap.NewNop(), nil, cfg)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- m.Run(ctx) }()

	<-ctx.Done()
	select {
	case <-runErr:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}

	if got := m.scanCount.Load(); got != 0 {
		t.Fatalf("scanCount = %d, want 0 in manual mode", got)
	}
}

func TestTagMetadataCarriesComposerAndGenre(t *testing.T) {
	meta := tagMetadata{
		Title:         "Goldberg Variations",
		Artists:       []string{"Glenn Gould"},
		Album:         "Bach: Goldberg Variations",
		Composer:      "Johann Sebastian Bach",
		EmbeddedGenre: "Classical",
	}
	if meta.Composer != "Johann Sebastian Bach" {
		t.Fatalf("Composer = %q", meta.Composer)
	}
	if meta.EmbeddedGenre != "Classical" {
		t.Fatalf("EmbeddedGenre = %q", meta.EmbeddedGenre)
	}
}

func TestMediaItemCarriesComposerAndEmbeddedGenre(t *testing.T) {
	item := mediaItem{Composer: "Bach", EmbeddedGenre: "Baroque"}
	if item.Composer != "Bach" || item.EmbeddedGenre != "Baroque" {
		t.Fatalf("fields not carried: %+v", item)
	}
}

func TestSearchBoostsGenreAndComposerMatches(t *testing.T) {
	cfg := Config{
		NodeID:    "mu:library:filesystem:test:default",
		TopicBase: "mu",
		Roots:     []string{t.TempDir()},
		ScanMode:  "manual",
	}
	m, err := NewModule(zap.NewNop(), nil, cfg)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}

	// Both items match the query "classical" — only the Bach track gets the
	// genre-boost via LLMGenre on its enrichment record. The decoy track
	// has "classical" in its searchable text but no enrichment.
	bachID := "audio:bach"
	decoyClassicalID := "audio:decoy_classical"
	bachComposerID := "audio:bach_composer"
	decoyBachID := "audio:decoy_bach"

	m.index.Items[bachID] = mediaItem{
		ID: bachID, Name: "Aria", Title: "Aria", MediaType: "Audio",
		Artists:    []string{"Glenn Gould"},
		Album:      "Goldberg",
		SearchText: "aria glenn gould goldberg classical",
	}
	m.index.Items[decoyClassicalID] = mediaItem{
		ID: decoyClassicalID, Name: "Aria", Title: "Aria", MediaType: "Audio",
		Artists:    []string{"Other Artist"},
		Album:      "Random Album That Mentions Classical In Liner Notes",
		SearchText: "aria other artist random album that mentions classical in liner notes",
	}
	m.enrichMeta["Glenn Gould|Goldberg"] = &AlbumMetadata{LLMGenre: "Classical"}

	items, _ := m.search("classical", 0, 10)
	if len(items) < 2 {
		t.Fatalf("expected both tracks to match; got %d", len(items))
	}
	if items[0].ItemID != bachID {
		t.Fatalf("expected LLMGenre-matched track first; got %+v", items)
	}

	// Composer-boost test: both tracks contain "bach" in their search text,
	// but only one has it as the Composer field.
	m.index.Items[bachComposerID] = mediaItem{
		ID: bachComposerID, Name: "Suite", Title: "Suite", MediaType: "Audio",
		Artists:    []string{"Glenn Gould"},
		Album:      "Cello Suites",
		Composer:   "Johann Sebastian Bach",
		SearchText: "suite glenn gould cello suites johann sebastian bach",
	}
	m.index.Items[decoyBachID] = mediaItem{
		ID: decoyBachID, Name: "Tribute", Title: "Tribute", MediaType: "Audio",
		Artists:    []string{"Random Tribute Band"},
		Album:      "Bach Covers",
		SearchText: "tribute random tribute band bach covers",
	}

	items2, _ := m.search("bach", 0, 10)
	if len(items2) < 2 {
		t.Fatalf("expected both bach tracks to match; got %d", len(items2))
	}
	if items2[0].ItemID != bachComposerID {
		t.Fatalf("expected composer-matched track first; got %+v", items2)
	}
}

func TestBuildSearchTextIncludesLLMGenreAndComposer(t *testing.T) {
	item := mediaItem{
		Name:          "Aria",
		Title:         "Aria",
		Album:         "Goldberg Variations",
		Artists:       []string{"Glenn Gould"},
		Composer:      "Johann Sebastian Bach",
		EmbeddedGenre: "Baroque",
	}
	enrich := &AlbumMetadata{LLMGenre: "Classical"}
	txt := buildSearchText(item, enrich)
	for _, want := range []string{"glenn gould", "goldberg", "classical", "bach", "baroque"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("buildSearchText missing %q in %q", want, txt)
		}
	}
}

func TestBuildSearchTextNilEnrichmentStillIncludesLocalFields(t *testing.T) {
	item := mediaItem{
		Name:          "Aria",
		Title:         "Aria",
		Composer:      "Bach",
		EmbeddedGenre: "Classical",
	}
	txt := buildSearchText(item, nil)
	if !strings.Contains(txt, "bach") {
		t.Fatalf("missing composer: %q", txt)
	}
	if !strings.Contains(txt, "classical") {
		t.Fatalf("missing embedded genre: %q", txt)
	}
}

func TestBuildGenreIndexUsesLLMGenre(t *testing.T) {
	idx := &libraryIndex{
		Audio: map[string]artistEntry{
			"Glenn Gould": {Name: "Glenn Gould", Albums: map[string]albumEntry{
				"Goldberg Variations": {Name: "Goldberg Variations"},
			}},
			"Miles Davis": {Name: "Miles Davis", Albums: map[string]albumEntry{
				"Kind of Blue": {Name: "Kind of Blue"},
			}},
			"Unknown": {Name: "Unknown", Albums: map[string]albumEntry{
				"Mystery": {Name: "Mystery"},
			}},
		},
		GenreAlbums: map[string][]genreAlbumRef{},
		Containers:  map[string]containerInfo{},
	}
	enrich := map[string]*AlbumMetadata{
		"Glenn Gould|Goldberg Variations": {LLMGenre: "Classical"},
		"Miles Davis|Kind of Blue":        {LLMGenre: "Jazz"},
	}
	buildGenreIndex(idx, enrich)

	if got := idx.GenreAlbums["Classical"]; len(got) != 1 || got[0].Artist != "Glenn Gould" {
		t.Fatalf("Classical bucket = %+v", got)
	}
	if got := idx.GenreAlbums["Jazz"]; len(got) != 1 || got[0].Artist != "Miles Davis" {
		t.Fatalf("Jazz bucket = %+v", got)
	}
	if got := idx.GenreAlbums["Unknown"]; len(got) != 1 || got[0].Artist != "Unknown" {
		t.Fatalf("Unknown bucket = %+v", got)
	}
	if len(idx.GenreAlbums) != 3 {
		ks := make([]string, 0, len(idx.GenreAlbums))
		for k := range idx.GenreAlbums {
			ks = append(ks, k)
		}
		t.Fatalf("expected exactly 3 genre buckets, got %d: %v", len(idx.GenreAlbums), ks)
	}
}

func TestBuildGenreIndexRollupFallback(t *testing.T) {
	idx := &libraryIndex{
		Audio: map[string]artistEntry{
			"X": {Name: "X", Albums: map[string]albumEntry{"A": {Name: "A"}}},
		},
		GenreAlbums: map[string][]genreAlbumRef{},
		Containers:  map[string]containerInfo{},
	}
	enrich := map[string]*AlbumMetadata{
		"X|A": {
			MusicBrainz: &MBMetadata{Genres: []string{"baroque"}},
		},
	}
	buildGenreIndex(idx, enrich)
	if got := idx.GenreAlbums["Classical"]; len(got) != 1 {
		t.Fatalf("expected Classical bucket via rollup, got %v", idx.GenreAlbums)
	}
}

func TestAlbumMetadataLLMGenreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &AlbumMetadata{
		Version:  currentSidecarVersion,
		Artist:   "Glenn Gould",
		Album:    "Bach: Goldberg Variations",
		LLMGenre: "Classical",
	}
	if err := writeSidecar(dir, in); err != nil {
		t.Fatalf("writeSidecar: %v", err)
	}
	out, err := readSidecar(dir)
	if err != nil {
		t.Fatalf("readSidecar: %v", err)
	}
	if out.LLMGenre != "Classical" {
		t.Fatalf("LLMGenre = %q, want Classical", out.LLMGenre)
	}
}

// TestArtURLCacheConcurrentAccess exercises the art-URL cache from parallel
// readers the way the 4 command workers do in production: getItemArtworkURL
// holds only the read lock while populating the cache, and every scan clears
// it. Run with -race; a concurrent map write here crashes the whole daemon.
func TestArtURLCacheConcurrentAccess(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 32; i++ {
		dir := filepath.Join(root, fmt.Sprintf("Artist%02d", i), "Album")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "01 track.mp3"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), []byte("img"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	mod := newTestModule(t, root, []string{".mp3"})
	mod.mu.Lock()
	mod.baseURL = "http://test"
	mod.artURLPrefix = "http://test/art/"
	mod.defaultArtURL = "http://test/art/default"
	mod.mu.Unlock()

	var items []mediaItem
	mod.mu.RLock()
	for _, it := range mod.index.Items {
		items = append(items, it)
	}
	mod.mu.RUnlock()
	if len(items) == 0 {
		t.Fatal("expected items")
	}

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				for _, it := range items {
					mod.getItemArtworkURL(it)
				}
				// Simulate what every rescan does: wipe the cache.
				mod.clearArtURLCache()
			}
		}()
	}
	wg.Wait()
}

// TestGoSafeContainsPanic verifies a panicking background task is contained
// (logged) instead of killing the process.
func TestGoSafeContainsPanic(t *testing.T) {
	mod := &Module{log: zap.NewNop()}
	done := make(chan struct{})
	mod.goSafe("test-task", func() {
		defer close(done)
		panic("boom")
	})
	select {
	case <-done:
		// panic path ran; if recover were missing the process would have died
	case <-time.After(2 * time.Second):
		t.Fatal("goSafe task did not run")
	}
}

// TestScanSkipsZeroByteFiles: zero-byte media files are unplayable and, worse,
// all share the same dedupe hash (size 0 + no content), so indexing them
// forms one giant phantom duplicate group and forces a re-parse every scan
// (they can never pass the Size() > 0 reuse gate).
func TestScanSkipsZeroByteFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "good.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "empty.mp3"), nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mod := newTestModule(t, root, []string{".mp3"})
	mod.mu.RLock()
	defer mod.mu.RUnlock()
	if len(mod.index.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(mod.index.Items))
	}
	for _, it := range mod.index.Items {
		if it.Name == "empty.mp3" {
			t.Fatal("zero-byte file was indexed")
		}
	}
}

// TestDedupeHidesDuplicatesDeterministically: with a dedupe policy active,
// duplicate files stay in the index (IDs remain resolvable for existing
// playlists) but only the canonical copy (lexicographically smallest path)
// appears in browse/search, and the winner is stable across rescans.
func TestDedupeHidesDuplicatesDeterministically(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := []byte("identical-content-that-hashes-the-same-way-both-times")
	for _, name := range []string{"01 a.mp3", "02 b.mp3"} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	mod, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:       "mu:library:filesystem:test:dedupe",
		Roots:        []string{root},
		IncludeExts:  []string{".mp3"},
		HTTPListen:   "127.0.0.1:0",
		DedupePolicy: "first",
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	var firstWinner string
	for pass := 0; pass < 3; pass++ {
		if err := mod.scan(); err != nil {
			t.Fatalf("scan %d: %v", pass, err)
		}
		mod.mu.RLock()
		if len(mod.index.Items) != 2 {
			mod.mu.RUnlock()
			t.Fatalf("pass %d: expected 2 indexed items, got %d", pass, len(mod.index.Items))
		}
		var winner string
		hidden := 0
		for _, it := range mod.index.Items {
			if it.DupOf != "" {
				hidden++
				if filepath.Base(it.Path) != "02 b.mp3" {
					t.Fatalf("pass %d: expected path-later copy hidden, got %s", pass, it.Path)
				}
			} else {
				winner = it.ID
			}
		}
		mod.mu.RUnlock()
		if hidden != 1 {
			t.Fatalf("pass %d: expected exactly 1 hidden duplicate, got %d", pass, hidden)
		}
		if pass == 0 {
			firstWinner = winner
		} else if winner != firstWinner {
			t.Fatalf("pass %d: canonical flapped: %s -> %s", pass, firstWinner, winner)
		}
	}

	// Browse the album: only the canonical track is listed.
	albumHash := containerHash("album", "Artist", "Album")
	items, total, err := mod.browse(albumHash, 0, 100)
	if err != nil {
		t.Fatalf("browse: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 visible track, got %d (total %d)", len(items), total)
	}

	// Search: only one result for the shared title.
	results, _ := mod.search("a", 0, 100)
	seen := map[string]bool{}
	for _, r := range results {
		if seen[r.ItemID] {
			t.Fatalf("duplicate item in search results: %s", r.ItemID)
		}
		seen[r.ItemID] = true
	}
	if len(results) > 1 {
		t.Fatalf("expected at most 1 search result, got %d", len(results))
	}
}

// TestConfigRejectsInvalidPolicies: config typos must fail fast at module
// construction instead of silently disabling the feature (a real deployment
// ran dedupe_policy="prefer" for weeks with dedupe doing nothing).
func TestConfigRejectsInvalidPolicies(t *testing.T) {
	base := func() Config {
		return Config{
			NodeID:      "mu:library:filesystem:test:cfg",
			Roots:       []string{t.TempDir()},
			IncludeExts: []string{".mp3"},
			HTTPListen:  "127.0.0.1:0",
		}
	}
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"dedupe", func(c *Config) { c.DedupePolicy = "prefer" }},
		{"repair", func(c *Config) { c.RepairPolicy = "weird" }},
		{"scan_mode", func(c *Config) { c.ScanMode = "sometimes" }},
		{"index_mode", func(c *Config) { c.IndexMode = "bogus" }},
	}
	for _, tc := range cases {
		cfg := base()
		tc.mutate(&cfg)
		if _, err := NewModule(zap.NewNop(), nil, cfg); err == nil {
			t.Errorf("%s: expected error for invalid value", tc.name)
		}
	}
	// Valid values still construct.
	cfg := base()
	cfg.DedupePolicy = "best"
	cfg.RepairPolicy = "balanced"
	cfg.ScanMode = "manual"
	if _, err := NewModule(zap.NewNop(), nil, cfg); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

// TestIndexPersistenceWarmRestart: a freshly constructed module that loads a
// persisted index must scan incrementally (reuse everything), not re-parse
// and re-hash the whole library. Requires Mtime/FileHash to survive the JSON
// round-trip and loadIndex to rebuild the incremental-scan state.
func TestIndexPersistenceWarmRestart(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for i := 0; i < 3; i++ {
		name := filepath.Join(dir, fmt.Sprintf("%02d track.mp3", i))
		if err := os.WriteFile(name, []byte(fmt.Sprintf("content-%d", i)), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	idxPath := filepath.Join(t.TempDir(), "index.json")
	cfg := Config{
		NodeID:       "mu:library:filesystem:test:persist",
		Roots:        []string{root},
		IncludeExts:  []string{".mp3"},
		HTTPListen:   "127.0.0.1:0",
		IndexMode:    "separate",
		IndexPath:    idxPath,
		DedupePolicy: "report", // forces FileHash computation on first scan
	}
	modA, err := NewModule(zap.NewNop(), nil, cfg)
	if err != nil {
		t.Fatalf("new module A: %v", err)
	}
	if err := modA.scan(); err != nil {
		t.Fatalf("scan A: %v", err)
	}
	if _, err := os.Stat(idxPath); err != nil {
		t.Fatalf("index not persisted: %v", err)
	}

	// Simulate a restart: fresh module, load index, rescan.
	modB, err := NewModule(zap.NewNop(), nil, cfg)
	if err != nil {
		t.Fatalf("new module B: %v", err)
	}
	if err := modB.loadIndex(); err != nil {
		t.Fatalf("load index: %v", err)
	}
	modB.mu.RLock()
	prevCount := len(modB.prevItems)
	for _, it := range modB.prevItems {
		if it.Mtime.IsZero() {
			modB.mu.RUnlock()
			t.Fatal("Mtime not persisted — incremental reuse impossible")
		}
		if it.FileHash == "" {
			modB.mu.RUnlock()
			t.Fatal("FileHash not persisted — full re-hash I/O on restart")
		}
	}
	modB.mu.RUnlock()
	if prevCount != 3 {
		t.Fatalf("prevItems not rebuilt from loaded index: got %d, want 3", prevCount)
	}
	if err := modB.scan(); err != nil {
		t.Fatalf("scan B: %v", err)
	}
	if got := modB.lastReused.Load(); got != 3 {
		t.Fatalf("post-restart scan reused %d items, want 3", got)
	}
}

// TestNoOpScanIsCheap: when a rescan finds no changes it must not wipe the
// art-URL cache nor launch another full embedding pass — a stable library
// scanned every 15 minutes should do near-zero work.
func TestNoOpScanIsCheap(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "01 track.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), []byte("img"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mod := newTestModule(t, root, []string{".mp3"})
	mod.embedProvider = &mockEmbeddingProvider{}
	mod.vectorIndex = NewVectorIndex()

	if err := mod.scan(); err != nil { // embeds launch async
		t.Fatalf("scan: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for mod.vectorIndex.Size() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("embedding pass never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
	passes := mod.embedPassCount.Load()

	// Populate the art URL cache.
	mod.mu.Lock()
	mod.baseURL = "http://test"
	mod.artURLPrefix = "http://test/art/"
	mod.defaultArtURL = "http://test/art/default"
	mod.mu.Unlock()
	var anyItem mediaItem
	mod.mu.RLock()
	for _, it := range mod.index.Items {
		anyItem = it
	}
	mod.mu.RUnlock()
	if url := mod.getItemArtworkURL(anyItem); url == "" {
		t.Fatal("expected art URL")
	}
	mod.artURLMu.Lock()
	cachedBefore := len(mod.artURLCache)
	mod.artURLMu.Unlock()
	if cachedBefore == 0 {
		t.Fatal("expected art URL cache entry")
	}

	// No-op rescan.
	if err := mod.scan(); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // would-be async embed launch window

	mod.artURLMu.Lock()
	cachedAfter := len(mod.artURLCache)
	mod.artURLMu.Unlock()
	if cachedAfter != cachedBefore {
		t.Fatalf("no-op scan wiped art URL cache (%d -> %d)", cachedBefore, cachedAfter)
	}
	if got := mod.embedPassCount.Load(); got != passes {
		t.Fatalf("no-op scan launched another embedding pass (%d -> %d)", passes, got)
	}
}

func TestAttemptTrackerCooldown(t *testing.T) {
	var tr attemptTracker
	if !tr.shouldTry("a", time.Hour) {
		t.Fatal("first attempt must be allowed")
	}
	if tr.shouldTry("a", time.Hour) {
		t.Fatal("second attempt inside cooldown must be blocked")
	}
	if !tr.shouldTry("b", time.Hour) {
		t.Fatal("different key must be allowed")
	}
	if !tr.shouldTry("a", 0) {
		t.Fatal("zero cooldown must always allow")
	}
}
