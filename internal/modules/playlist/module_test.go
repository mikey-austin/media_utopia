package playlist

import (
	"encoding/json"
	"fmt"
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

func TestPlaylistCreateFromSnapshot(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	now := time.Now().Unix()
	snap := Snapshot{
		SnapshotID: "mu:snapshot:plsrv:default:snap-1",
		Name:       "Snap",
		Owner:      "tester",
		Revision:   1,
		RendererID: "r",
		SessionID:  "s",
		Capture:    mu.SnapshotCapture{},
		Items:      []string{"lib:mu:library:test:one", "http://example.com/a.mp3"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := storage.SaveSnapshot(snap); err != nil {
		t.Fatalf("save snapshot: %v", err)
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
		Body: mustJSON(mu.PlaylistCreateBody{Name: "FromSnap", SnapshotID: snap.SnapshotID}),
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
	getCmd := mu.CommandEnvelope{
		ID:   "id-2",
		Type: "playlist.get",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistGetBody{PlaylistID: out.Playlists[0].PlaylistID}),
	}
	getReply := module.playlistGet(getCmd, mu.ReplyEnvelope{ID: getCmd.ID, Type: "ack", OK: true})
	var getOut Playlist
	if err := json.Unmarshal(getReply.Body, &getOut); err != nil {
		t.Fatalf("decode get reply: %v", err)
	}
	if len(getOut.Entries) != 2 {
		t.Fatalf("expected 2 entries")
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

func TestPlaylistDelete(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	now := time.Now().Unix()
	pl := Playlist{
		PlaylistID: "mu:playlist:plsrv:default:one",
		Name:       "Test",
		Owner:      "tester",
		Revision:   1,
		Entries:    []PlaylistEntry{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := storage.SavePlaylist(pl); err != nil {
		t.Fatalf("save: %v", err)
	}

	module := Module{
		storage: storage,
		config:  Config{NodeID: "mu:playlist:plsrv:default:main"},
	}
	cmd := mu.CommandEnvelope{
		ID:   "id-1",
		Type: "playlist.delete",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistDeleteBody{PlaylistID: pl.PlaylistID}),
	}
	reply := module.playlistDelete(cmd, mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true})
	if reply.Type != "ack" {
		t.Fatalf("expected ack")
	}
	if _, err := storage.GetPlaylist(pl.PlaylistID); !os.IsNotExist(err) {
		t.Fatalf("expected playlist deleted")
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

func TestPlaylistAddItems(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	module := Module{
		storage: storage,
		config:  Config{NodeID: "mu:playlist:plsrv:default:main"},
	}

	// Create a playlist first.
	create := mu.CommandEnvelope{
		ID:   "id-1",
		Type: "playlist.create",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistCreateBody{Name: "AddTest"}),
	}
	module.playlistCreate(create, mu.ReplyEnvelope{ID: create.ID, Type: "ack", OK: true})

	playlists, err := storage.ListPlaylists()
	if err != nil || len(playlists) != 1 {
		t.Fatalf("expected 1 playlist")
	}
	playlistID := playlists[0].PlaylistID
	initialRevision := playlists[0].Revision

	// Add two items.
	add := mu.CommandEnvelope{
		ID:   "id-2",
		Type: "playlist.addItems",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistAddItemsBody{
			PlaylistID: playlistID,
			Entries: []mu.QueueEntry{
				{Ref: &mu.ItemRef{ID: "mu:track:alpha"}},
				{Ref: &mu.ItemRef{ID: "mu:track:beta"}},
			},
		}),
	}
	reply := module.playlistAddItems(add, mu.ReplyEnvelope{ID: add.ID, Type: "ack", OK: true})
	if !reply.OK {
		t.Fatalf("expected ok reply for addItems")
	}

	pl, err := storage.GetPlaylist(playlistID)
	if err != nil {
		t.Fatalf("get playlist: %v", err)
	}
	if len(pl.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(pl.Entries))
	}
	if pl.Entries[0].Ref.ID != "mu:track:alpha" {
		t.Fatalf("expected first entry ref mu:track:alpha, got %s", pl.Entries[0].Ref.ID)
	}
	if pl.Entries[1].Ref.ID != "mu:track:beta" {
		t.Fatalf("expected second entry ref mu:track:beta, got %s", pl.Entries[1].Ref.ID)
	}
	if pl.Revision != initialRevision+1 {
		t.Fatalf("expected revision %d, got %d", initialRevision+1, pl.Revision)
	}

	// Add a third item and verify it is appended.
	add2 := mu.CommandEnvelope{
		ID:   "id-3",
		Type: "playlist.addItems",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistAddItemsBody{
			PlaylistID: playlistID,
			Entries:    []mu.QueueEntry{{Ref: &mu.ItemRef{ID: "mu:track:gamma"}}},
		}),
	}
	module.playlistAddItems(add2, mu.ReplyEnvelope{ID: add2.ID, Type: "ack", OK: true})

	pl, err = storage.GetPlaylist(playlistID)
	if err != nil {
		t.Fatalf("get playlist: %v", err)
	}
	if len(pl.Entries) != 3 {
		t.Fatalf("expected 3 entries after second add, got %d", len(pl.Entries))
	}
	if pl.Entries[2].Ref.ID != "mu:track:gamma" {
		t.Fatalf("expected third entry appended with ref mu:track:gamma, got %s", pl.Entries[2].Ref.ID)
	}
	if pl.Revision != initialRevision+2 {
		t.Fatalf("expected revision %d, got %d", initialRevision+2, pl.Revision)
	}
}

func TestPlaylistMoveItems(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	module := Module{
		storage: storage,
		config:  Config{NodeID: "mu:playlist:plsrv:default:main"},
	}

	// Create a playlist and add three items.
	create := mu.CommandEnvelope{
		ID:   "id-1",
		Type: "playlist.create",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistCreateBody{Name: "MoveTest"}),
	}
	module.playlistCreate(create, mu.ReplyEnvelope{ID: create.ID, Type: "ack", OK: true})

	playlists, err := storage.ListPlaylists()
	if err != nil || len(playlists) != 1 {
		t.Fatalf("expected 1 playlist")
	}
	playlistID := playlists[0].PlaylistID

	add := mu.CommandEnvelope{
		ID:   "id-2",
		Type: "playlist.addItems",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistAddItemsBody{
			PlaylistID: playlistID,
			Entries: []mu.QueueEntry{
				{Ref: &mu.ItemRef{ID: "mu:track:A"}},
				{Ref: &mu.ItemRef{ID: "mu:track:B"}},
				{Ref: &mu.ItemRef{ID: "mu:track:C"}},
			},
		}),
	}
	module.playlistAddItems(add, mu.ReplyEnvelope{ID: add.ID, Type: "ack", OK: true})

	pl, err := storage.GetPlaylist(playlistID)
	if err != nil {
		t.Fatalf("get playlist: %v", err)
	}
	if len(pl.Entries) != 3 {
		t.Fatalf("expected 3 entries")
	}
	revisionBeforeReplace := pl.Revision

	// Reorder items using replaceItems: reverse the order (C, B, A).
	replace := mu.CommandEnvelope{
		ID:   "id-3",
		Type: "playlist.replaceItems",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistReplaceItemsBody{
			PlaylistID: playlistID,
			Items:      []string{"mu:track:C", "mu:track:B", "mu:track:A"},
		}),
	}
	replaceReply := module.playlistReplaceItems(replace, mu.ReplyEnvelope{ID: replace.ID, Type: "ack", OK: true})
	if !replaceReply.OK {
		t.Fatalf("expected ok reply for replaceItems")
	}

	pl, err = storage.GetPlaylist(playlistID)
	if err != nil {
		t.Fatalf("get playlist: %v", err)
	}
	if len(pl.Entries) != 3 {
		t.Fatalf("expected 3 entries after replace, got %d", len(pl.Entries))
	}

	// Verify the new order.
	expectedOrder := []string{"mu:track:C", "mu:track:B", "mu:track:A"}
	for i, want := range expectedOrder {
		if pl.Entries[i].Ref == nil || pl.Entries[i].Ref.ID != want {
			got := "<nil>"
			if pl.Entries[i].Ref != nil {
				got = pl.Entries[i].Ref.ID
			}
			t.Fatalf("entry %d: expected ref %s, got %s", i, want, got)
		}
	}
	if pl.Revision != revisionBeforeReplace+1 {
		t.Fatalf("expected revision %d, got %d", revisionBeforeReplace+1, pl.Revision)
	}
}

func TestSnapshotDelete(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	module := Module{
		storage: storage,
		config:  Config{NodeID: "mu:playlist:plsrv:default:main"},
	}

	// Save two snapshots.
	for i, name := range []string{"Snap1", "Snap2"} {
		cmd := mu.CommandEnvelope{
			ID:   fmt.Sprintf("save-%d", i),
			Type: "snapshot.save",
			TS:   time.Now().Unix(),
			From: "tester",
			Body: mustJSON(mu.SnapshotSaveBody{
				Name:       name,
				RendererID: "r",
				SessionID:  "s",
				Capture:    mu.SnapshotCapture{},
				Items:      []string{"mu:track:one"},
			}),
		}
		reply := module.snapshotSave(cmd, mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true})
		if !reply.OK {
			t.Fatalf("expected ok from snapshot save")
		}
	}

	// Verify both exist.
	listCmd := mu.CommandEnvelope{ID: "list-1", Type: "snapshot.list", TS: time.Now().Unix(), From: "tester"}
	listReply := module.snapshotList(listCmd, mu.ReplyEnvelope{ID: listCmd.ID, Type: "ack", OK: true})
	var listOut mu.SnapshotListReply
	if err := json.Unmarshal(listReply.Body, &listOut); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listOut.Snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(listOut.Snapshots))
	}

	// Delete the first snapshot.
	deleteID := listOut.Snapshots[0].SnapshotID
	rmCmd := mu.CommandEnvelope{
		ID:   "rm-1",
		Type: "snapshot.remove",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.SnapshotRemoveBody{SnapshotID: deleteID}),
	}
	rmReply := module.snapshotRemove(rmCmd, mu.ReplyEnvelope{ID: rmCmd.ID, Type: "ack", OK: true})
	if !rmReply.OK {
		t.Fatalf("expected ok from snapshot remove")
	}

	// Verify only one remains.
	listReply2 := module.snapshotList(listCmd, mu.ReplyEnvelope{ID: listCmd.ID, Type: "ack", OK: true})
	var listOut2 mu.SnapshotListReply
	if err := json.Unmarshal(listReply2.Body, &listOut2); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listOut2.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot after delete, got %d", len(listOut2.Snapshots))
	}
	if listOut2.Snapshots[0].SnapshotID == deleteID {
		t.Fatalf("deleted snapshot should not appear in listing")
	}
}

func TestSnapshotRestore(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	module := Module{
		storage: storage,
		config:  Config{NodeID: "mu:playlist:plsrv:default:main"},
	}

	// Save a snapshot with specific items and capture state.
	items := []string{"mu:track:x", "mu:track:y", "mu:track:z"}
	capture := mu.SnapshotCapture{
		QueueRevision: 5,
		Index:         2,
		PositionMS:    12345,
		Repeat:        true,
		RepeatMode:    "all",
		Shuffle:       false,
	}
	saveCmd := mu.CommandEnvelope{
		ID:   "save-1",
		Type: "snapshot.save",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.SnapshotSaveBody{
			Name:       "Restore Me",
			RendererID: "renderer-1",
			SessionID:  "session-1",
			Capture:    capture,
			Items:      items,
		}),
	}
	saveReply := module.snapshotSave(saveCmd, mu.ReplyEnvelope{ID: saveCmd.ID, Type: "ack", OK: true})
	if !saveReply.OK {
		t.Fatalf("expected ok from snapshot save")
	}

	// Get the snapshot ID from the listing.
	listCmd := mu.CommandEnvelope{ID: "list-1", Type: "snapshot.list", TS: time.Now().Unix(), From: "tester"}
	listReply := module.snapshotList(listCmd, mu.ReplyEnvelope{ID: listCmd.ID, Type: "ack", OK: true})
	var listOut mu.SnapshotListReply
	if err := json.Unmarshal(listReply.Body, &listOut); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listOut.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot")
	}
	snapshotID := listOut.Snapshots[0].SnapshotID

	// Restore (get) the snapshot and verify contents.
	getCmd := mu.CommandEnvelope{
		ID:   "get-1",
		Type: "snapshot.get",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.SnapshotGetBody{SnapshotID: snapshotID}),
	}
	getReply := module.snapshotGet(getCmd, mu.ReplyEnvelope{ID: getCmd.ID, Type: "ack", OK: true})
	if !getReply.OK {
		t.Fatalf("expected ok from snapshot get")
	}

	var getOut mu.SnapshotGetReply
	if err := json.Unmarshal(getReply.Body, &getOut); err != nil {
		t.Fatalf("decode get: %v", err)
	}

	if getOut.SnapshotID != snapshotID {
		t.Fatalf("expected snapshotId %s, got %s", snapshotID, getOut.SnapshotID)
	}
	if getOut.Name != "Restore Me" {
		t.Fatalf("expected name 'Restore Me', got %s", getOut.Name)
	}
	if len(getOut.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(getOut.Items))
	}
	for i, want := range items {
		if getOut.Items[i] != want {
			t.Fatalf("item %d: expected %s, got %s", i, want, getOut.Items[i])
		}
	}
	if getOut.Capture.QueueRevision != 5 {
		t.Fatalf("expected capture queueRevision 5, got %d", getOut.Capture.QueueRevision)
	}
	if getOut.Capture.Index != 2 {
		t.Fatalf("expected capture index 2, got %d", getOut.Capture.Index)
	}
	if getOut.Capture.PositionMS != 12345 {
		t.Fatalf("expected capture positionMs 12345, got %d", getOut.Capture.PositionMS)
	}
	if !getOut.Capture.Repeat {
		t.Fatalf("expected capture repeat true")
	}
	if getOut.Capture.RepeatMode != "all" {
		t.Fatalf("expected capture repeatMode 'all', got %s", getOut.Capture.RepeatMode)
	}
}

func TestPlaylistRevisionConflict(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	module := Module{
		storage: storage,
		config:  Config{NodeID: "mu:playlist:plsrv:default:main"},
	}

	// Create a playlist.
	create := mu.CommandEnvelope{
		ID:   "id-1",
		Type: "playlist.create",
		TS:   time.Now().Unix(),
		From: "tester",
		Body: mustJSON(mu.PlaylistCreateBody{Name: "RevTest"}),
	}
	module.playlistCreate(create, mu.ReplyEnvelope{ID: create.ID, Type: "ack", OK: true})

	playlists, err := storage.ListPlaylists()
	if err != nil || len(playlists) != 1 {
		t.Fatalf("expected 1 playlist")
	}
	playlistID := playlists[0].PlaylistID
	currentRevision := playlists[0].Revision

	// Use the correct revision -- should succeed.
	addOK := mu.CommandEnvelope{
		ID:         "id-2",
		Type:       "playlist.addItems",
		TS:         time.Now().Unix(),
		From:       "tester",
		IfRevision: &currentRevision,
		Body: mustJSON(mu.PlaylistAddItemsBody{
			PlaylistID: playlistID,
			Entries:    []mu.QueueEntry{{Ref: &mu.ItemRef{ID: "mu:track:one"}}},
		}),
	}
	okReply := module.playlistAddItems(addOK, mu.ReplyEnvelope{ID: addOK.ID, Type: "ack", OK: true})
	if !okReply.OK {
		t.Fatalf("expected ok reply with correct revision")
	}

	// Now the revision has incremented. Use the old (stale) revision -- should fail.
	staleRevision := currentRevision // this is now stale
	addConflict := mu.CommandEnvelope{
		ID:         "id-3",
		Type:       "playlist.addItems",
		TS:         time.Now().Unix(),
		From:       "tester",
		IfRevision: &staleRevision,
		Body: mustJSON(mu.PlaylistAddItemsBody{
			PlaylistID: playlistID,
			Entries:    []mu.QueueEntry{{Ref: &mu.ItemRef{ID: "mu:track:two"}}},
		}),
	}
	conflictReply := module.playlistAddItems(addConflict, mu.ReplyEnvelope{ID: addConflict.ID, Type: "ack", OK: true})
	if conflictReply.OK {
		t.Fatalf("expected error reply for stale revision")
	}
	if conflictReply.Err == nil {
		t.Fatalf("expected error details in reply")
	}
	if conflictReply.Err.Code != "CONFLICT" {
		t.Fatalf("expected CONFLICT error code, got %s", conflictReply.Err.Code)
	}

	// Verify the playlist still has only 1 entry (the conflicting add was rejected).
	pl, err := storage.GetPlaylist(playlistID)
	if err != nil {
		t.Fatalf("get playlist: %v", err)
	}
	if len(pl.Entries) != 1 {
		t.Fatalf("expected 1 entry after conflict rejection, got %d", len(pl.Entries))
	}

	// Also test stale revision on removeItems.
	removeConflict := mu.CommandEnvelope{
		ID:         "id-4",
		Type:       "playlist.removeItems",
		TS:         time.Now().Unix(),
		From:       "tester",
		IfRevision: &staleRevision,
		Body: mustJSON(mu.PlaylistRemoveItemsBody{
			PlaylistID: playlistID,
			EntryIDs:   []string{pl.Entries[0].EntryID},
		}),
	}
	rmConflict := module.playlistRemoveItems(removeConflict, mu.ReplyEnvelope{ID: removeConflict.ID, Type: "ack", OK: true})
	if rmConflict.OK {
		t.Fatalf("expected error reply for stale revision on removeItems")
	}
	if rmConflict.Err == nil || rmConflict.Err.Code != "CONFLICT" {
		t.Fatalf("expected CONFLICT error code on removeItems")
	}

	// Also test stale revision on replaceItems.
	replaceConflict := mu.CommandEnvelope{
		ID:         "id-5",
		Type:       "playlist.replaceItems",
		TS:         time.Now().Unix(),
		From:       "tester",
		IfRevision: &staleRevision,
		Body: mustJSON(mu.PlaylistReplaceItemsBody{
			PlaylistID: playlistID,
			Items:      []string{"mu:track:replaced"},
		}),
	}
	replConflict := module.playlistReplaceItems(replaceConflict, mu.ReplyEnvelope{ID: replaceConflict.ID, Type: "ack", OK: true})
	if replConflict.OK {
		t.Fatalf("expected error reply for stale revision on replaceItems")
	}
	if replConflict.Err == nil || replConflict.Err.Code != "CONFLICT" {
		t.Fatalf("expected CONFLICT error code on replaceItems")
	}
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
