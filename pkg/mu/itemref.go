package mu

import (
	"errors"
	"fmt"
	"strings"
)

// LibraryItemKind is the discriminator for LibraryItemRef.
const LibraryItemKind = "libraryItem"

// LibraryItemRef is the canonical wire reference for a library-backed media item.
// The on-wire JSON form is { "kind": "libraryItem", "libraryId": "...", "itemId": "..." }.
type LibraryItemRef struct {
	Kind      string `json:"kind"`
	LibraryID string `json:"libraryId"`
	ItemID    string `json:"itemId"`
}

// NewLibraryItemRef constructs a LibraryItemRef with Kind preset to "libraryItem".
func NewLibraryItemRef(libraryID, itemID string) LibraryItemRef {
	return LibraryItemRef{Kind: LibraryItemKind, LibraryID: libraryID, ItemID: itemID}
}

// Validate reports whether the ref is a well-formed library item reference.
func (r LibraryItemRef) Validate() error {
	if r.Kind != LibraryItemKind {
		return fmt.Errorf("LibraryItemRef.kind must be %q, got %q", LibraryItemKind, r.Kind)
	}
	if _, _, _, err := ParseLibraryNodeID(r.LibraryID); err != nil {
		return fmt.Errorf("LibraryItemRef.libraryId: %w", err)
	}
	if strings.TrimSpace(r.ItemID) == "" {
		return errors.New("LibraryItemRef.itemId is required")
	}
	return nil
}

// DisplayMetadata is a denormalized UI snapshot carried alongside library refs
// so controllers can render queue and now-playing without follow-up catalog lookups.
// All fields are optional; readers must tolerate missing values.
type DisplayMetadata struct {
	Title      string   `json:"title,omitempty"`
	Artist     string   `json:"artist,omitempty"`
	Artists    []string `json:"artists,omitempty"`
	Album      string   `json:"album,omitempty"`
	ArtworkURL string   `json:"artworkUrl,omitempty"`
	DurationMS int64    `json:"durationMs,omitempty"`
	MediaType  string   `json:"mediaType,omitempty"`
}

// ParseLibraryNodeID splits a canonical library node id of the form
// "mu:library:<provider>:<namespace>:<resource>" into its parts.
func ParseLibraryNodeID(id string) (provider, namespace, resource string, err error) {
	parts := strings.SplitN(id, ":", 5)
	if len(parts) != 5 {
		return "", "", "", fmt.Errorf("invalid library node id %q: expected mu:library:<provider>:<namespace>:<resource>", id)
	}
	if parts[0] != "mu" || parts[1] != "library" {
		return "", "", "", fmt.Errorf("invalid library node id %q: must start with mu:library:", id)
	}
	if parts[2] == "" || parts[3] == "" || parts[4] == "" {
		return "", "", "", fmt.Errorf("invalid library node id %q: provider, namespace, and resource must be non-empty", id)
	}
	return parts[2], parts[3], parts[4], nil
}

// MakeLibraryNodeID assembles a canonical library node id.
func MakeLibraryNodeID(provider, namespace, resource string) string {
	return fmt.Sprintf("mu:library:%s:%s:%s", provider, namespace, resource)
}
