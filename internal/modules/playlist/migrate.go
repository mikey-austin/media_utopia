package playlist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// MigrateOnDisk converts any pre-StorageVersion playlist, snapshot, or
// suggestion files in s.root to the current schema. It's safe to call
// repeatedly: files already at StorageVersion are skipped.
//
// Conversions:
//   - Snapshot.items []string → Snapshot.entries []QueueEntry. Each string is
//     parsed as either a direct URL (http://, https://) or a legacy
//     lib:<libraryNodeId>:<itemId> reference.
//   - PlaylistEntry.ref / Suggestion entries: legacy {"id": "..."} ItemRef
//     shape → structured LibraryItemRef. Strings that don't parse are dropped
//     with a warning so the file remains loadable.
//
// Migration is intentionally lossy for unparseable items rather than refusing
// to load: we want servers to come up even if a few entries can't be
// recovered.
func (s *Storage) MigrateOnDisk(log *zap.Logger) error {
	if log == nil {
		log = zap.NewNop()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.migrateSnapshots(log); err != nil {
		return fmt.Errorf("snapshots: %w", err)
	}
	if err := s.migratePlaylists(log); err != nil {
		return fmt.Errorf("playlists: %w", err)
	}
	if err := s.migrateSuggestions(log); err != nil {
		return fmt.Errorf("suggestions: %w", err)
	}
	return nil
}

func (s *Storage) migrateSnapshots(log *zap.Logger) error {
	paths, err := filepath.Glob(filepath.Join(s.root, "snapshots", "*.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		var probe struct {
			Version int             `json:"version"`
			Entries json.RawMessage `json:"entries"`
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			log.Warn("skip unreadable snapshot", zap.String("path", path), zap.Error(err))
			continue
		}
		if probe.Version >= StorageVersion && len(probe.Entries) > 0 {
			continue
		}

		var legacy struct {
			SnapshotID string             `json:"snapshotId"`
			Name       string             `json:"name"`
			Owner      string             `json:"owner"`
			Revision   int64              `json:"revision"`
			RendererID string             `json:"rendererId"`
			SessionID  string             `json:"sessionId"`
			Capture    mu.SnapshotCapture `json:"capture"`
			Items      []string           `json:"items"`
			CreatedAt  int64              `json:"createdAt"`
			UpdatedAt  int64              `json:"updatedAt"`
		}
		if err := json.Unmarshal(raw, &legacy); err != nil {
			log.Warn("skip unparseable snapshot", zap.String("path", path), zap.Error(err))
			continue
		}

		entries, dropped := convertLegacyItems(legacy.Items)
		if dropped > 0 {
			log.Warn("snapshot migration dropped entries",
				zap.String("snapshotId", legacy.SnapshotID),
				zap.Int("dropped", dropped))
		}

		migrated := Snapshot{
			Version:    StorageVersion,
			SnapshotID: legacy.SnapshotID,
			Name:       legacy.Name,
			Owner:      legacy.Owner,
			Revision:   legacy.Revision,
			RendererID: legacy.RendererID,
			SessionID:  legacy.SessionID,
			Capture:    legacy.Capture,
			Entries:    entries,
			CreatedAt:  legacy.CreatedAt,
			UpdatedAt:  legacy.UpdatedAt,
		}
		if err := writeJSON(path, migrated); err != nil {
			return fmt.Errorf("rewrite %s: %w", path, err)
		}
		log.Info("snapshot migrated to v2",
			zap.String("snapshotId", legacy.SnapshotID),
			zap.Int("entries", len(entries)))
	}
	return nil
}

func (s *Storage) migratePlaylists(log *zap.Logger) error {
	paths, err := filepath.Glob(filepath.Join(s.root, "playlists", "*.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var probe struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			log.Warn("skip unreadable playlist", zap.String("path", path), zap.Error(err))
			continue
		}
		if probe.Version >= StorageVersion {
			continue
		}
		var legacy struct {
			PlaylistID string                 `json:"playlistId"`
			Name       string                 `json:"name"`
			Owner      string                 `json:"owner"`
			Revision   int64                  `json:"revision"`
			Entries    []legacyPlaylistEntry  `json:"entries"`
			CreatedAt  int64                  `json:"createdAt"`
			UpdatedAt  int64                  `json:"updatedAt"`
		}
		if err := json.Unmarshal(raw, &legacy); err != nil {
			log.Warn("skip unparseable playlist", zap.String("path", path), zap.Error(err))
			continue
		}
		entries := make([]PlaylistEntry, 0, len(legacy.Entries))
		dropped := 0
		for _, e := range legacy.Entries {
			converted, ok := convertLegacyEntry(e)
			if !ok {
				dropped++
				continue
			}
			entries = append(entries, converted)
		}
		if dropped > 0 {
			log.Warn("playlist migration dropped entries",
				zap.String("playlistId", legacy.PlaylistID),
				zap.Int("dropped", dropped))
		}
		migrated := Playlist{
			Version:    StorageVersion,
			PlaylistID: legacy.PlaylistID,
			Name:       legacy.Name,
			Owner:      legacy.Owner,
			Revision:   legacy.Revision,
			Entries:    entries,
			CreatedAt:  legacy.CreatedAt,
			UpdatedAt:  legacy.UpdatedAt,
		}
		if err := writeJSON(path, migrated); err != nil {
			return fmt.Errorf("rewrite %s: %w", path, err)
		}
		log.Info("playlist migrated to v2",
			zap.String("playlistId", legacy.PlaylistID),
			zap.Int("entries", len(entries)))
	}
	return nil
}

func (s *Storage) migrateSuggestions(log *zap.Logger) error {
	paths, err := filepath.Glob(filepath.Join(s.root, "suggestions", "*.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var probe struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			log.Warn("skip unreadable suggestion", zap.String("path", path), zap.Error(err))
			continue
		}
		if probe.Version >= StorageVersion {
			continue
		}
		var legacy struct {
			SuggestionID string                `json:"suggestionId"`
			Name         string                `json:"name"`
			Owner        string                `json:"owner"`
			Revision     int64                 `json:"revision"`
			Entries      []legacyPlaylistEntry `json:"entries"`
			CreatedAt    int64                 `json:"createdAt"`
			UpdatedAt    int64                 `json:"updatedAt"`
		}
		if err := json.Unmarshal(raw, &legacy); err != nil {
			log.Warn("skip unparseable suggestion", zap.String("path", path), zap.Error(err))
			continue
		}
		entries := make([]PlaylistEntry, 0, len(legacy.Entries))
		dropped := 0
		for _, e := range legacy.Entries {
			converted, ok := convertLegacyEntry(e)
			if !ok {
				dropped++
				continue
			}
			entries = append(entries, converted)
		}
		if dropped > 0 {
			log.Warn("suggestion migration dropped entries",
				zap.String("suggestionId", legacy.SuggestionID),
				zap.Int("dropped", dropped))
		}
		migrated := Suggestion{
			Version:      StorageVersion,
			SuggestionID: legacy.SuggestionID,
			Name:         legacy.Name,
			Owner:        legacy.Owner,
			Revision:     legacy.Revision,
			Entries:      entries,
			CreatedAt:    legacy.CreatedAt,
			UpdatedAt:    legacy.UpdatedAt,
		}
		if err := writeJSON(path, migrated); err != nil {
			return fmt.Errorf("rewrite %s: %w", path, err)
		}
		log.Info("suggestion migrated to v2",
			zap.String("suggestionId", legacy.SuggestionID),
			zap.Int("entries", len(entries)))
	}
	return nil
}

type legacyPlaylistEntry struct {
	EntryID  string             `json:"entryId"`
	Ref      *legacyItemRef     `json:"ref,omitempty"`
	Resolved *mu.ResolvedSource `json:"resolved,omitempty"`
}

type legacyItemRef struct {
	ID string `json:"id"`
}

func convertLegacyEntry(e legacyPlaylistEntry) (PlaylistEntry, bool) {
	out := PlaylistEntry{
		EntryID:  e.EntryID,
		Resolved: e.Resolved,
	}
	if e.Ref != nil {
		ref, ok := parseLegacyLibRef(e.Ref.ID)
		if !ok {
			if out.Resolved == nil {
				return PlaylistEntry{}, false
			}
		} else {
			out.Ref = &ref
		}
	}
	if out.Ref == nil && out.Resolved == nil {
		return PlaylistEntry{}, false
	}
	return out, true
}

func convertLegacyItems(items []string) ([]mu.QueueEntry, int) {
	entries := make([]mu.QueueEntry, 0, len(items))
	dropped := 0
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.HasPrefix(item, "http://") || strings.HasPrefix(item, "https://") {
			entries = append(entries, mu.QueueEntry{Resolved: &mu.ResolvedSource{URL: item}})
			continue
		}
		ref, ok := parseLegacyLibRef(item)
		if !ok {
			dropped++
			continue
		}
		entries = append(entries, mu.QueueEntry{Ref: &ref})
	}
	return entries, dropped
}

// parseLegacyLibRef parses "lib:mu:library:<provider>:<namespace>:<resource>:<itemId>"
// using the same LastIndex(":") split the legacy renderers used.
func parseLegacyLibRef(s string) (mu.LibraryItemRef, bool) {
	if !strings.HasPrefix(s, "lib:") {
		return mu.LibraryItemRef{}, false
	}
	body := strings.TrimPrefix(s, "lib:")
	idx := strings.LastIndex(body, ":")
	if idx <= 0 || idx >= len(body)-1 {
		return mu.LibraryItemRef{}, false
	}
	libraryID := body[:idx]
	itemID := body[idx+1:]
	ref := mu.NewLibraryItemRef(libraryID, itemID)
	if err := ref.Validate(); err != nil {
		return mu.LibraryItemRef{}, false
	}
	return ref, true
}

// ErrMigrateRoot is returned when migration is attempted on an inaccessible root.
var ErrMigrateRoot = errors.New("migration root not accessible")
