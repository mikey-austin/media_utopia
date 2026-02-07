package fslibrary

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"strings"
)

// RepairPolicy controls how aggressively metadata is repaired.
type RepairPolicy string

const (
	RepairPolicyNone       RepairPolicy = "none"
	RepairPolicyStrict     RepairPolicy = "strict"     // Only high confidence
	RepairPolicyBalanced   RepairPolicy = "balanced"   // Medium confidence if no conflicts
	RepairPolicyAggressive RepairPolicy = "aggressive" // Accept most, log conflicts
)

// DedupePolicy controls how duplicates are handled.
type DedupePolicy string

const (
	DedupePolicyNone   DedupePolicy = "none"
	DedupePolicyReport DedupePolicy = "report" // Log duplicates only
	DedupePolicyFirst  DedupePolicy = "first"  // Keep first occurrence
	DedupePolicyBest   DedupePolicy = "best"   // Keep best quality
)

// RepairResult contains repaired metadata with confidence.
type RepairResult struct {
	Title      string
	Artists    []string
	Album      string
	Confidence float32
	Source     string
}

// repairMetadata attempts to improve metadata using filename patterns.
func repairMetadata(item mediaItem, policy RepairPolicy) RepairResult {
	if policy == RepairPolicyNone {
		return RepairResult{
			Title:   item.Title,
			Artists: item.Artists,
			Album:   item.Album,
		}
	}

	result := RepairResult{
		Title:      item.Title,
		Artists:    item.Artists,
		Album:      item.Album,
		Confidence: 1.0,
		Source:     "original",
	}

	// Try to extract info from filename if missing
	if result.Title == "" || len(result.Artists) == 0 {
		extracted := parseFilename(item.Name)
		if extracted.Title != "" && result.Title == "" {
			result.Title = extracted.Title
			result.Source = "filename"
			result.Confidence = 0.7
		}
		if len(extracted.Artists) > 0 && len(result.Artists) == 0 {
			result.Artists = extracted.Artists
			result.Source = "filename"
			result.Confidence = 0.7
		}
	}

	// Clean up common issues
	result.Title = cleanTitle(result.Title)
	for i, artist := range result.Artists {
		result.Artists[i] = cleanArtist(artist)
	}
	result.Album = cleanAlbum(result.Album)

	return result
}

// parseFilename extracts metadata from common filename patterns.
func parseFilename(filename string) RepairResult {
	result := RepairResult{}

	// Remove extension and path
	name := strings.TrimSuffix(filename, ".mp3")
	name = strings.TrimSuffix(name, ".flac")
	name = strings.TrimSuffix(name, ".m4a")
	name = strings.TrimSuffix(name, ".ogg")

	// Common patterns:
	// "Artist - Title"
	// "01 - Title"
	// "01. Title"
	// "Artist - Album - Title"
	// "01 Artist - Title"

	// Pattern: "Artist - Title"
	if parts := strings.SplitN(name, " - ", 2); len(parts) == 2 {
		// Check if first part is a track number
		if isTrackNumber(parts[0]) {
			result.Title = strings.TrimSpace(parts[1])
		} else {
			result.Artists = []string{strings.TrimSpace(parts[0])}
			result.Title = strings.TrimSpace(parts[1])
		}
		return result
	}

	// Pattern: "01. Title" or "01 Title"
	trackNumPattern := regexp.MustCompile(`^(\d{1,3})[.\s]+(.+)$`)
	if matches := trackNumPattern.FindStringSubmatch(name); len(matches) == 3 {
		result.Title = strings.TrimSpace(matches[2])
		return result
	}

	// Fallback: use whole name as title
	result.Title = name
	return result
}

func isTrackNumber(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) > 3 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

func cleanTitle(title string) string {
	title = strings.TrimSpace(title)
	// Remove common suffixes
	title = regexp.MustCompile(`\s*\(Official\s*(Video|Audio|Music\s*Video)?\)\s*$`).ReplaceAllString(title, "")
	title = regexp.MustCompile(`\s*\[Official\s*(Video|Audio|Music\s*Video)?\]\s*$`).ReplaceAllString(title, "")
	title = regexp.MustCompile(`\s*\(Lyrics?\)\s*$`).ReplaceAllString(title, "")
	title = regexp.MustCompile(`\s*\[Lyrics?\]\s*$`).ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}

func cleanArtist(artist string) string {
	artist = strings.TrimSpace(artist)
	// Remove "feat." variations at the end
	artist = regexp.MustCompile(`\s*(feat\.|ft\.|featuring)\s+.+$`).ReplaceAllString(artist, "")
	return strings.TrimSpace(artist)
}

func cleanAlbum(album string) string {
	album = strings.TrimSpace(album)
	// Remove common edition suffixes
	album = regexp.MustCompile(`\s*\((Deluxe|Remastered|Expanded|Special)\s*(Edition|Version)?\)\s*$`).ReplaceAllString(album, "")
	album = regexp.MustCompile(`\s*\[(Deluxe|Remastered|Expanded|Special)\s*(Edition|Version)?\]\s*$`).ReplaceAllString(album, "")
	return strings.TrimSpace(album)
}

// repairFromSidecar fills missing artist/album fields using sidecar enrichment data.
// It only fills empty fields — existing non-empty values are never overwritten.
func repairFromSidecar(item mediaItem, meta *AlbumMetadata, policy RepairPolicy) RepairResult {
	result := RepairResult{
		Title:      item.Title,
		Artists:    item.Artists,
		Album:      item.Album,
		Confidence: 1.0,
		Source:     "original",
	}

	if policy == RepairPolicyNone {
		return result
	}

	var minConfidence float32
	switch policy {
	case RepairPolicyStrict:
		minConfidence = 0.9
	case RepairPolicyBalanced:
		minConfidence = 0.8
	case RepairPolicyAggressive:
		minConfidence = 0.7
	}

	// Artist repair: fill if empty or "Unknown Artist"
	artistEmpty := len(item.Artists) == 0 || (len(item.Artists) == 1 && item.Artists[0] == "Unknown Artist")
	if artistEmpty {
		// Prefer ArtistInfo.Name (MusicBrainz canonical, confidence 0.9)
		if meta.ArtistInfo != nil && meta.ArtistInfo.Name != "" && 0.9 >= minConfidence {
			result.Artists = []string{meta.ArtistInfo.Name}
			result.Source = "sidecar"
			result.Confidence = 0.9
		} else if meta.Artist != "" && 0.8 >= minConfidence {
			// Fall back to sidecar's stored artist name (confidence 0.8)
			result.Artists = []string{meta.Artist}
			result.Source = "sidecar"
			result.Confidence = 0.8
		}
	}

	// Album repair: fill if empty or "Unknown Album"
	albumEmpty := item.Album == "" || item.Album == "Unknown Album"
	if albumEmpty && meta.Album != "" && 0.8 >= minConfidence {
		result.Album = meta.Album
		result.Source = "sidecar"
		if result.Confidence > 0.8 {
			result.Confidence = 0.8
		}
	}

	return result
}

// DuplicateInfo represents a potential duplicate file.
type DuplicateInfo struct {
	ID       string
	Path     string
	Hash     string
	Size     int64
	Original string // ID of the original (first seen)
}

// DuplicateIndex tracks file hashes for deduplication.
type DuplicateIndex struct {
	byHash map[string][]string // hash -> list of item IDs
	byID   map[string]string   // itemID -> hash
}

// NewDuplicateIndex creates a duplicate index.
func NewDuplicateIndex() *DuplicateIndex {
	return &DuplicateIndex{
		byHash: make(map[string][]string),
		byID:   make(map[string]string),
	}
}

func (d *DuplicateIndex) Clear() {
	d.byHash = make(map[string][]string)
	d.byID = make(map[string]string)
}

// Add registers an item and returns true if it's a duplicate.
func (d *DuplicateIndex) Add(itemID, hash string) bool {
	d.byID[itemID] = hash
	existing := d.byHash[hash]
	d.byHash[hash] = append(existing, itemID)
	return len(existing) > 0
}

// IsDuplicate checks if an item is a duplicate.
func (d *DuplicateIndex) IsDuplicate(itemID string) bool {
	hash, ok := d.byID[itemID]
	if !ok {
		return false
	}
	return len(d.byHash[hash]) > 1
}

// GetDuplicates returns all duplicate groups.
func (d *DuplicateIndex) GetDuplicates() [][]string {
	var groups [][]string
	for _, ids := range d.byHash {
		if len(ids) > 1 {
			groups = append(groups, ids)
		}
	}
	return groups
}

// Original returns the original (first) item ID for a duplicate.
func (d *DuplicateIndex) Original(itemID string) string {
	hash, ok := d.byID[itemID]
	if !ok {
		return itemID
	}
	ids := d.byHash[hash]
	if len(ids) == 0 {
		return itemID
	}
	return ids[0]
}

// computeFileHash computes a partial hash of a file for deduplication.
// Uses first and last 64KB to detect duplicates without reading entire file.
func computeFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}

	h := sha256.New()
	size := info.Size()

	// Write size for uniqueness
	h.Write([]byte{byte(size >> 56), byte(size >> 48), byte(size >> 40), byte(size >> 32),
		byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size)})

	const chunkSize = 64 * 1024

	// Read first chunk
	buf := make([]byte, chunkSize)
	n, _ := io.ReadFull(f, buf)
	h.Write(buf[:n])

	// Read last chunk if file is large enough
	if size > int64(chunkSize*2) {
		if _, err := f.Seek(-chunkSize, io.SeekEnd); err == nil {
			n, _ = io.ReadFull(f, buf)
			h.Write(buf[:n])
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
