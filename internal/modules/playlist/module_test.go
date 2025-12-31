package playlist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

func TestPlaylistCreateAndList(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	module := Module{
		storage: storage,
		config:  Config{NodeID: "mu:playlist:plsrv:default:main"},
	}

	cmd := mu.CommandEnvelope{
		ID:   "id-1",
		Type: "playlist.create",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistCreateBody{Name: "Test"}),
	}

	reply := module.playlistCreate(cmd, mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true})
	if reply.Type != "ack" {
		t.Fatalf("expected ack")
	}

	listReply := module.playlistList(cmd, mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true})
	var out mu.PlaylistListReply
	if err := json.Unmarshal(listReply.Body, &out); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(out.Playlists) != 1 {
		t.Fatalf("expected 1 playlist")
	}
}

func TestSnapshotSaveAndList(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	module := Module{
		storage: storage,
		config:  Config{NodeID: "mu:playlist:plsrv:default:main"},
	}

	cmd := mu.CommandEnvelope{
		ID:   "id-1",
		Type: "snapshot.save",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.SnapshotSaveBody{
			Name:       "Friday",
			RendererID: "r",
			SessionID:  "s",
			Capture:    mu.SnapshotCapture{},
			Items:      []string{"lib:mu:library:test:one", "http://example.com/a.mp3"},
		}),
	}

	reply := module.snapshotSave(cmd, mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true})
	if reply.Type != "ack" {
		t.Fatalf("expected ack")
	}

	listReply := module.snapshotList(cmd, mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true})
	var out mu.SnapshotListReply
	if err := json.Unmarshal(listReply.Body, &out); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(out.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot")
	}

	getCmd := mu.CommandEnvelope{
		ID:   "id-2",
		Type: "snapshot.get",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.SnapshotGetBody{SnapshotID: out.Snapshots[0].SnapshotID}),
	}
	getReply := module.snapshotGet(getCmd, mu.ReplyEnvelope{ID: getCmd.ID, Type: "ack", OK: true})
	var getOut mu.SnapshotGetReply
	if err := json.Unmarshal(getReply.Body, &getOut); err != nil {
		t.Fatalf("decode get reply: %v", err)
	}
	if len(getOut.Items) != 2 {
		t.Fatalf("expected 2 snapshot items")
	}

	rmCmd := mu.CommandEnvelope{
		ID:   "id-3",
		Type: "snapshot.remove",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.SnapshotRemoveBody{SnapshotID: out.Snapshots[0].SnapshotID}),
	}
	rmReply := module.snapshotRemove(rmCmd, mu.ReplyEnvelope{ID: rmCmd.ID, Type: "ack", OK: true})
	if rmReply.Type != "ack" {
		t.Fatalf("expected ack remove")
	}
}

func TestStorageWritesFiles(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	pl := Playlist{PlaylistID: "mu:playlist:plsrv:default:one", Name: "Test", Revision: 1}
	if err := storage.SavePlaylist(pl); err != nil {
		t.Fatalf("save: %v", err)
	}

	path := filepath.Join(root, "playlists")
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file")
	}
}

func TestPlaylistRemoveItems(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	module := Module{
		storage: storage,
		config:  Config{NodeID: "mu:playlist:plsrv:default:main"},
	}

	create := mu.CommandEnvelope{
		ID:   "id-1",
		Type: "playlist.create",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistCreateBody{Name: "Test"}),
	}
	module.playlistCreate(create, mu.ReplyEnvelope{ID: create.ID, Type: "ack", OK: true})

	playlists, err := storage.ListPlaylists()
	if err != nil || len(playlists) != 1 {
		t.Fatalf("expected playlist")
	}
	playlistID := playlists[0].PlaylistID

	add := mu.CommandEnvelope{
		ID:   "id-2",
		Type: "playlist.addItems",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistAddItemsBody{PlaylistID: playlistID, Entries: []mu.QueueEntry{{Ref: &mu.ItemRef{ID: "mu:track:one"}}}}),
	}
	module.playlistAddItems(add, mu.ReplyEnvelope{ID: add.ID, Type: "ack", OK: true})

	pl, err := storage.GetPlaylist(playlistID)
	if err != nil || len(pl.Entries) != 1 {
		t.Fatalf("expected entry")
	}

	remove := mu.CommandEnvelope{
		ID:   "id-3",
		Type: "playlist.removeItems",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistRemoveItemsBody{PlaylistID: playlistID, EntryIDs: []string{pl.Entries[0].EntryID}}),
	}
	module.playlistRemoveItems(remove, mu.ReplyEnvelope{ID: remove.ID, Type: "ack", OK: true})

	pl, err = storage.GetPlaylist(playlistID)
	if err != nil {
		t.Fatalf("get playlist: %v", err)
	}
	if len(pl.Entries) != 0 {
		t.Fatalf("expected entries removed")
	}
}

func TestPlaylistRename(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	module := Module{
		storage: storage,
		config:  Config{NodeID: "mu:playlist:plsrv:default:main"},
	}

	create := mu.CommandEnvelope{
		ID:   "id-1",
		Type: "playlist.create",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistCreateBody{Name: "Test"}),
	}
	module.playlistCreate(create, mu.ReplyEnvelope{ID: create.ID, Type: "ack", OK: true})

	playlists, err := storage.ListPlaylists()
	if err != nil || len(playlists) != 1 {
		t.Fatalf("expected playlist")
	}

	rename := mu.CommandEnvelope{
		ID:   "id-2",
		Type: "playlist.rename",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistRenameBody{PlaylistID: playlists[0].PlaylistID, Name: "Renamed"}),
	}
	module.playlistRename(rename, mu.ReplyEnvelope{ID: rename.ID, Type: "ack", OK: true})

	pl, err := storage.GetPlaylist(playlists[0].PlaylistID)
	if err != nil {
		t.Fatalf("get playlist: %v", err)
	}
	if pl.Name != "Renamed" {
		t.Fatalf("expected renamed playlist")
	}
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
