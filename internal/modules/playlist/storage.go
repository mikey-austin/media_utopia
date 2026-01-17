package playlist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// Storage provides playlist/snapshot/suggestion persistence.
type Storage struct {
	root string
	mu   sync.Mutex
}

// NewStorage creates a storage at root.
func NewStorage(root string) (*Storage, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("storage path required")
	}
	return &Storage{root: root}, nil
}

// Playlist is stored on disk as JSON.
type Playlist struct {
	PlaylistID string          `json:"playlistId"`
	Name       string          `json:"name"`
	Owner      string          `json:"owner"`
	Revision   int64           `json:"revision"`
	Entries    []PlaylistEntry `json:"entries"`
	CreatedAt  int64           `json:"createdAt"`
	UpdatedAt  int64           `json:"updatedAt"`
}

// PlaylistEntry stores a queue entry within a playlist.
type PlaylistEntry struct {
	EntryID  string             `json:"entryId"`
	Ref      *mu.ItemRef        `json:"ref,omitempty"`
	Resolved *mu.ResolvedSource `json:"resolved,omitempty"`
}

// Snapshot is stored on disk as JSON.
type Snapshot struct {
	SnapshotID string             `json:"snapshotId"`
	Name       string             `json:"name"`
	Owner      string             `json:"owner"`
	Revision   int64              `json:"revision"`
	RendererID string             `json:"rendererId"`
	SessionID  string             `json:"sessionId"`
	Capture    mu.SnapshotCapture `json:"capture"`
	Items      []string           `json:"items,omitempty"`
	CreatedAt  int64              `json:"createdAt"`
	UpdatedAt  int64              `json:"updatedAt"`
}

// Suggestion is stored on disk as JSON.
type Suggestion struct {
	SuggestionID string          `json:"suggestionId"`
	Name         string          `json:"name"`
	Owner        string          `json:"owner"`
	Revision     int64           `json:"revision"`
	Entries      []PlaylistEntry `json:"entries"`
	CreatedAt    int64           `json:"createdAt"`
	UpdatedAt    int64           `json:"updatedAt"`
}

func (s *Storage) playlistPath(id string) string {
	return filepath.Join(s.root, "playlists", safeFilename(id)+".json")
}

func (s *Storage) snapshotPath(id string) string {
	return filepath.Join(s.root, "snapshots", safeFilename(id)+".json")
}

func (s *Storage) suggestionPath(id string) string {
	return filepath.Join(s.root, "suggestions", safeFilename(id)+".json")
}

// ListPlaylists returns playlist summaries.
func (s *Storage) ListPlaylists() ([]Playlist, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	paths, err := filepath.Glob(filepath.Join(s.root, "playlists", "*.json"))
	if err != nil {
		return nil, err
	}

	playlists := make([]Playlist, 0, len(paths))
	for _, path := range paths {
		var pl Playlist
		if err := readJSON(path, &pl); err != nil {
			return nil, err
		}
		playlists = append(playlists, pl)
	}

	sort.Slice(playlists, func(i, j int) bool {
		return playlists[i].Name < playlists[j].Name
	})
	return playlists, nil
}

// GetPlaylist loads a playlist by id.
func (s *Storage) GetPlaylist(id string) (Playlist, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var pl Playlist
	if err := readJSON(s.playlistPath(id), &pl); err != nil {
		return Playlist{}, err
	}
	return pl, nil
}

// SavePlaylist writes a playlist to disk.
func (s *Storage) SavePlaylist(pl Playlist) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.playlistPath(pl.PlaylistID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeJSON(path, pl)
}

// DeletePlaylist deletes a playlist.
func (s *Storage) DeletePlaylist(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.playlistPath(id)
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// File not found at expected path, try searching all files
		paths, err := filepath.Glob(filepath.Join(s.root, "playlists", "*.json"))
		if err != nil {
			return err
		}
		for _, p := range paths {
			var pl Playlist
			if err := readJSON(p, &pl); err == nil && pl.PlaylistID == id {
				return os.Remove(p)
			}
		}
		return os.ErrNotExist
	}
	return nil
}

// ListSnapshots returns snapshot summaries.
func (s *Storage) ListSnapshots() ([]Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	paths, err := filepath.Glob(filepath.Join(s.root, "snapshots", "*.json"))
	if err != nil {
		return nil, err
	}

	snapshots := make([]Snapshot, 0, len(paths))
	for _, path := range paths {
		var snap Snapshot
		if err := readJSON(path, &snap); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt < snapshots[j].CreatedAt
	})
	return snapshots, nil
}

// SaveSnapshot writes a snapshot to disk.
func (s *Storage) SaveSnapshot(snapshot Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.snapshotPath(snapshot.SnapshotID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeJSON(path, snapshot)
}

// GetSnapshot loads a snapshot by id.
func (s *Storage) GetSnapshot(id string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var snap Snapshot
	if err := readJSON(s.snapshotPath(id), &snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// RemoveSnapshot deletes a snapshot by id.
func (s *Storage) RemoveSnapshot(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return os.Remove(s.snapshotPath(id))
}

// ListSuggestions returns suggestion summaries.
func (s *Storage) ListSuggestions() ([]Suggestion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	paths, err := filepath.Glob(filepath.Join(s.root, "suggestions", "*.json"))
	if err != nil {
		return nil, err
	}

	suggestions := make([]Suggestion, 0, len(paths))
	for _, path := range paths {
		var sug Suggestion
		if err := readJSON(path, &sug); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, sug)
	}

	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].CreatedAt < suggestions[j].CreatedAt
	})
	return suggestions, nil
}

// GetSuggestion loads a suggestion.
func (s *Storage) GetSuggestion(id string) (Suggestion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var sug Suggestion
	if err := readJSON(s.suggestionPath(id), &sug); err != nil {
		return Suggestion{}, err
	}
	return sug, nil
}

// SaveSuggestion writes a suggestion.
func (s *Storage) SaveSuggestion(sug Suggestion) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.suggestionPath(sug.SuggestionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeJSON(path, sug)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSON(path string, v any) error {
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func safeFilename(id string) string {
	replacer := strings.NewReplacer(":", "_", "/", "_", "\\", "_", " ", "_")
	return replacer.Replace(id)
}
