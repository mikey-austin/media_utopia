package fslibrary

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
