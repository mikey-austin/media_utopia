package playlist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestMigrateSnapshotsConvertsLegacyItems(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	legacy := map[string]any{
		"snapshotId": "mu:snapshot:plsrv:default:legacy-1",
		"name":       "Legacy",
		"owner":      "tester",
		"revision":   1,
		"rendererId": "r",
		"sessionId":  "s",
		"capture":    map[string]any{"index": 0},
		"items": []string{
			"http://example.com/a.mp3",
			"lib:mu:library:jellyfin:test:default:track-1",
			"lib:malformed",
			"https://example.com/b.flac",
		},
		"createdAt": 1,
		"updatedAt": 1,
	}
	raw, _ := json.MarshalIndent(legacy, "", "  ")
	path := filepath.Join(dir, safeFilename("mu:snapshot:plsrv:default:legacy-1")+".json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	if err := storage.MigrateOnDisk(zap.NewNop()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	migrated, err := storage.GetSnapshot("mu:snapshot:plsrv:default:legacy-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if migrated.Version != StorageVersion {
		t.Fatalf("version: got %d want %d", migrated.Version, StorageVersion)
	}
	if len(migrated.Entries) != 3 {
		t.Fatalf("entries: got %d want 3 (one malformed dropped)", len(migrated.Entries))
	}
	if migrated.Entries[0].Resolved == nil || migrated.Entries[0].Resolved.URL != "http://example.com/a.mp3" {
		t.Errorf("entry[0]: %+v", migrated.Entries[0])
	}
	if migrated.Entries[1].Ref == nil || migrated.Entries[1].Ref.ItemID != "track-1" {
		t.Errorf("entry[1]: %+v", migrated.Entries[1])
	}
	if migrated.Entries[1].Ref.LibraryID != "mu:library:jellyfin:test:default" {
		t.Errorf("entry[1] libraryId: %+v", migrated.Entries[1].Ref)
	}
	if migrated.Entries[2].Resolved == nil || migrated.Entries[2].Resolved.URL != "https://example.com/b.flac" {
		t.Errorf("entry[2]: %+v", migrated.Entries[2])
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	root := t.TempDir()
	storage, err := NewStorage(root)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	// Save a v2 snapshot first.
	if err := storage.SaveSnapshot(Snapshot{
		SnapshotID: "mu:snapshot:plsrv:default:v2-1",
		Name:       "v2",
		Revision:   1,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Migrating should be a no-op (no panic, no rewrite errors).
	if err := storage.MigrateOnDisk(zap.NewNop()); err != nil {
		t.Fatalf("migrate 1: %v", err)
	}
	if err := storage.MigrateOnDisk(zap.NewNop()); err != nil {
		t.Fatalf("migrate 2: %v", err)
	}

	got, err := storage.GetSnapshot("mu:snapshot:plsrv:default:v2-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Version != StorageVersion {
		t.Errorf("version: got %d want %d", got.Version, StorageVersion)
	}
}

