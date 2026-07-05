package fslibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

func TestFoldString(t *testing.T) {
	cases := map[string]string{
		"Motörhead":     "motorhead",
		"Beyoncé":       "beyonce",
		"Sigur Rós":     "sigur ros",
		"plain ascii":   "plain ascii",
		"BJÖRK":         "bjork",
		"Café Tacvba":   "cafe tacvba",
		"Dvořák":        "dvorak",
		"ÆON, œuvre, ß": "aeon, oeuvre, ss",
	}
	for in, want := range cases {
		if got := foldString(in); got != want {
			t.Errorf("foldString(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSearchExactMatchBeatsSemantic: the lexical scorer must always run —
// an exact title match has to be the first result even when semantic search
// returns a page full of fuzzy matches (previously the keyword branch was
// dead code whenever semantic returned anything).
func TestSearchExactMatchBeatsSemantic(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Artist", "Album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	names := []string{"Warning Sign", "Warm Nights", "Winter Song", "Wandering Star"}
	for i, n := range names {
		p := filepath.Join(dir, fmt.Sprintf("Artist - %s.mp3", n))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("c%d", i)), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	mod := newTestModule(t, root, []string{".mp3"})
	mod.embedProvider = &mockEmbeddingProvider{} // identical vector for everything
	mod.vectorIndex = NewVectorIndex()
	mod.buildEmbeddings(context.Background(), mod.index.Items)
	if mod.vectorIndex.Size() == 0 {
		t.Fatal("expected vectors")
	}

	results, total := mod.search("Warning Sign", 0, 10)
	if total == 0 || len(results) == 0 {
		t.Fatal("expected results")
	}
	if got := results[0].Name; got != "Artist - Warning Sign" && results[0].ItemID == "" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if want := "Warning Sign"; !containsFold(results[0].Name, want) {
		t.Fatalf("exact match not first: got %q", results[0].Name)
	}
	// No duplicate IDs after hybrid merge.
	seen := map[string]bool{}
	for _, r := range results {
		if seen[r.ItemID] {
			t.Fatalf("duplicate result %s", r.ItemID)
		}
		seen[r.ItemID] = true
	}
}

// TestSearchDiacriticFolding: "motorhead" must match "Motörhead".
func TestSearchDiacriticFolding(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Motörhead", "Overkill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Motörhead - Overkill.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mod := newTestModule(t, root, []string{".mp3"})
	results, total := mod.search("motorhead", 0, 10)
	if total == 0 || len(results) == 0 {
		t.Fatal("diacritic query returned nothing")
	}
	_ = results
}

// TestQueryEmbeddingCached: repeating a query must not re-embed it.
func TestQueryEmbeddingCached(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Artist - Song.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mod := newTestModule(t, root, []string{".mp3"})
	var calls atomic.Int64
	mod.embedProvider = &mockEmbeddingProvider{
		embedFn: func(_ context.Context, inputs []EmbedInput) ([]EmbedVector, error) {
			calls.Add(1)
			out := make([]EmbedVector, len(inputs))
			for i, in := range inputs {
				out[i] = EmbedVector{ID: in.ID, Vector: []float32{1, 0, 0}}
			}
			return out, nil
		},
	}
	mod.vectorIndex = NewVectorIndex()
	mod.buildEmbeddings(context.Background(), mod.index.Items)
	buildCalls := calls.Load()

	mod.search("some song", 0, 10)
	afterFirst := calls.Load()
	if afterFirst != buildCalls+1 {
		t.Fatalf("expected exactly one query embed call, got %d", afterFirst-buildCalls)
	}
	mod.search("some song", 0, 10)
	if calls.Load() != afterFirst {
		t.Fatalf("repeated query re-embedded: %d calls", calls.Load()-buildCalls)
	}
}

func TestOllamaQueryPrefix(t *testing.T) {
	p, err := NewOllamaProvider(OllamaConfig{Model: "mxbai-embed-large"})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if p.QueryPrefix() == "" {
		t.Fatal("mxbai models require the retrieval instruction prefix")
	}
	p2, err := NewOllamaProvider(OllamaConfig{Model: "nomic-embed-text"})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if p2.QueryPrefix() != "" {
		t.Fatalf("non-mxbai model must have no prefix, got %q", p2.QueryPrefix())
	}
}

func containsFold(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (foldString(haystack) == foldString(needle) ||
		len(foldString(haystack)) > 0 && stringsContains(foldString(haystack), foldString(needle)))
}

func stringsContains(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}

func TestSearchTypesAlbumsAndArtists(t *testing.T) {
	root := t.TempDir()
	layout := map[string][]string{
		"Nova Beats/First Light":  {"Nova Beats - Dawn.mp3", "Nova Beats - Dusk.mp3"},
		"Nova Beats/Second Wind":  {"Nova Beats - Gale.mp3"},
		"Other Crew/First Light2": {"Other Crew - Something.mp3"},
	}
	for dir, files := range layout {
		full := filepath.Join(root, dir)
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(full, f), []byte(f), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mod := newTestModule(t, root, []string{".mp3"})

	search := func(query string, types []string) libraryItemsReply {
		t.Helper()
		reply := mod.librarySearch(mu.CommandEnvelope{
			ID:   "st",
			Type: "library.search",
			Body: mustJSON(mu.LibrarySearchBody{Query: query, Start: 0, Count: 50, Types: types}),
		}, mu.ReplyEnvelope{Type: "ack", OK: true})
		if reply.Type == "error" {
			t.Fatalf("search %q types=%v returned error: %s", query, types, string(reply.Body))
		}
		var out libraryItemsReply
		if err := json.Unmarshal(reply.Body, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return out
	}

	// Album search by album name returns the album container.
	albums := search("First Light", []string{"musicalbum"})
	if albums.Total < 1 || albums.Items[0].Name != "First Light" {
		t.Fatalf("expected 'First Light' album first, got %+v", albums.Items)
	}
	if albums.Items[0].ItemID != containerHash("album", "Nova Beats", "First Light") {
		t.Fatalf("album ItemID is not the browse container hash: %q", albums.Items[0].ItemID)
	}
	if len(albums.Items[0].Artists) == 0 || albums.Items[0].Artists[0] != "Nova Beats" {
		t.Fatalf("album result missing artist: %+v", albums.Items[0])
	}

	// Album search by artist name surfaces that artist's albums.
	byArtist := search("Nova Beats", []string{"musicalbum"})
	if byArtist.Total != 2 {
		t.Fatalf("expected 2 albums for artist query, got %d: %+v", byArtist.Total, byArtist.Items)
	}

	// Artist search returns the artist container.
	artists := search("nova", []string{"musicartist"})
	if artists.Total != 1 || artists.Items[0].Name != "Nova Beats" {
		t.Fatalf("expected artist 'Nova Beats', got %+v", artists.Items)
	}
	if artists.Items[0].ItemID != containerHash("artist", "Nova Beats", "") {
		t.Fatalf("artist ItemID is not the browse container hash")
	}

	// Combined types: containers first, then tracks.
	combined := search("dawn", []string{"musicalbum", "audio"})
	found := false
	for _, it := range combined.Items {
		if it.Name == "Nova Beats - Dawn" || it.Name == "Dawn" {
			found = true
		}
	}
	if !found {
		t.Fatalf("combined search did not include the track: %+v", combined.Items)
	}

	// Default (no types) keeps tracks-only behaviour.
	tracks := search("dawn", nil)
	if tracks.Total != 1 {
		t.Fatalf("expected 1 track for default search, got %d", tracks.Total)
	}

	// Unsupported type errors.
	reply := mod.librarySearch(mu.CommandEnvelope{
		ID:   "st-bad",
		Type: "library.search",
		Body: mustJSON(mu.LibrarySearchBody{Query: "x", Types: []string{"vhs"}}),
	}, mu.ReplyEnvelope{Type: "ack", OK: true})
	if reply.Type != "error" {
		t.Fatalf("expected error for unsupported type, got %s", reply.Type)
	}
}
