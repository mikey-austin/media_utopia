package fslibrary

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	cmd = mu.CommandEnvelope{
		ID:   "c2",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: audioContainer, Start: 0, Count: 10}),
	}
	reply = mod.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse artists unmarshal: %v", err)
	}
	if len(browse.Items) != 1 || browse.Items[0].Name != "Artist" {
		t.Fatalf("expected artist container, got %+v", browse.Items)
	}
	albumContainer := browse.Items[0].ItemID

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

	// Browse container:audio to get artist IDs (now hashed)
	browseCmd := mu.CommandEnvelope{
		ID:   "b0",
		Type: "library.browse",
		Body: mustJSON(mu.LibraryBrowseBody{ContainerID: "container:audio", Start: 0, Count: 10}),
	}
	browseReply := mod.libraryBrowse(browseCmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var browse libraryItemsReply
	if err := json.Unmarshal(browseReply.Body, &browse); err != nil {
		t.Fatalf("browse artists unmarshal: %v", err)
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
			name: "v2 sidecar with data does not refresh",
			meta: &AlbumMetadata{
				Version:     2,
				FetchedAt:   time.Now(),
				MusicBrainz: &MBMetadata{Genres: []string{"rock"}},
			},
			want: false,
		},
		{
			name: "v2 negative cache recent does not refresh",
			meta: &AlbumMetadata{
				Version:   2,
				FetchedAt: time.Now(),
			},
			want: false,
		},
		{
			name: "v2 negative cache old triggers refresh",
			meta: &AlbumMetadata{
				Version:   2,
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
	item := mediaItem{
		Title:   "Song Title",
		Artists: []string{"Artist Name"},
		Album:   "Album Name",
		Name:    "Song Title",
	}
	text := buildEmbedText(item, nil)
	if text != "Song Title - Artist Name - Album Name" {
		t.Errorf("buildEmbedText() = %q, want 'Song Title - Artist Name - Album Name'", text)
	}

	// Different name should be included
	item.Name = "Different Name"
	text = buildEmbedText(item, nil)
	if text != "Song Title - Artist Name - Album Name - Different Name" {
		t.Errorf("buildEmbedText() = %q", text)
	}
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
