package lease

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// Store saves leases under XDG_STATE_HOME or ~/.local/state.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore creates a lease store.
func NewStore() (*Store, error) {
	path, err := leasePath()
	if err != nil {
		return nil, err
	}
	return &Store{path: path}, nil
}

// Get returns a lease for a renderer if stored.
func (s *Store) Get(rendererID string) (mu.Lease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.readAll()
	if err != nil {
		return mu.Lease{}, false, err
	}
	lease, ok := data[rendererID]
	return lease, ok, nil
}

// Put stores a lease for a renderer.
func (s *Store) Put(rendererID string, lease mu.Lease) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.readAll()
	if err != nil {
		return err
	}
	data[rendererID] = lease
	return s.writeAll(data)
}

// Clear removes a lease for a renderer.
func (s *Store) Clear(rendererID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.readAll()
	if err != nil {
		return err
	}
	delete(data, rendererID)
	return s.writeAll(data)
}

func (s *Store) readAll() (map[string]mu.Lease, error) {
	data := map[string]mu.Lease{}
	file, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return data, nil
		}
		return nil, err
	}
	if len(file) == 0 {
		return data, nil
	}
	if err := json.Unmarshal(file, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func (s *Store) writeAll(data map[string]mu.Lease) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, payload, 0o600)
}

func leasePath() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "mu", "leases.json"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "mu", "leases.json"), nil
}
