/*
Package fslibrary provides a filesystem-based media library for the mu system.

# Overview

The filesystem library module scans configured directory roots for media files,
extracts metadata from tags (ID3, Vorbis, etc.), and exposes the content through
the standard mu library protocol. It supports browsing by artist/album hierarchy,
full-text search, and optional semantic search via embeddings.

# Item ID Formats

The library uses different ID formats for containers (folders) and media items:

Container IDs (used for browsing):

	container:audio              Root audio folder
	container:video              Root video folder
	artist:<name>                Artist folder (name is URL-escaped)
	album:<artist>:<album>       Album folder (both URL-escaped)

Media Item IDs (used for playback/resolve):

	audio:<hash>                 Audio file
	video:<hash>                 Video file

The hash component is a SHA-1 of "path|size|mtime" which changes when the file
is modified, ensuring cache invalidation. Example: "audio:a1b2c3d4e5f6..."

When referenced externally (e.g., in playlists), items use the lib: prefix:

	lib:<library-node-id>:<item-id>
	lib:mu:library:filesystem:home:default:audio:a1b2c3d4...

# Browse Hierarchy

The browse structure organizes content as follows:

	(root)
	├── Audio/                    container:audio
	│   ├── Artist Name/          artist:Artist%20Name
	│   │   ├── Album One/        album:Artist%20Name:Album%20One
	│   │   │   ├── Track 1       audio:abc123...
	│   │   │   └── Track 2       audio:def456...
	│   │   └── Album Two/        album:Artist%20Name:Album%20Two
	│   └── Another Artist/       artist:Another%20Artist
	└── Video/                    container:video
	    ├── Movie.mkv             video:789abc...
	    └── Show.mp4              video:cde012...

Audio files are organized by Artist > Album > Track. Video files are listed
flat under the Video container.

# Supported File Types

Default extensions (configurable via IncludeExts):

	Audio: .mp3, .flac, .ogg, .m4a
	Video: .mp4, .mkv

# Metadata Extraction

Metadata is extracted in order of preference:

 1. Embedded tags (ID3v2, Vorbis Comment, MP4 atoms) via github.com/dhowden/tag
 2. Filename patterns: "Artist - Title.mp3" or "01 - Title.mp3"
 3. Directory structure: parent folder as album, grandparent as artist

# Optional Features

Metadata Repair (repair_policy):

	none        No repair (default)
	strict      Only high-confidence repairs
	balanced    Medium-confidence if no conflicts
	aggressive  Accept most repairs, log conflicts

Deduplication (dedupe_policy):

	none        No deduplication (default)
	report      Log duplicates only
	first       Keep first occurrence, remove duplicates
	best        Keep highest quality version

Semantic Search (embedding_provider):

	ollama      Use local Ollama server for embeddings

When embeddings are enabled, search queries are vectorized and matched against
item embeddings using cosine similarity, enabling fuzzy/semantic matching.

Album Metadata Enrichment (enrich_enabled):

When enabled, the module automatically enriches albums with metadata from
MusicBrainz, Discogs, and Wikipedia during each rescan. Enrichment runs in a
background goroutine and does not block the scan. Albums that already have an
up-to-date sidecar file are skipped.

Data sources:

	MusicBrainz   Free, no API key required. Provides genres, tags, release
	              year, release type, record label, album annotation,
	              Wikipedia URL-rels, artist credits, and artist details
	              (type, origin, active years, artist-level genres/tags).
	Discogs       Optional personal access token (discogs_token) for higher
	              rate limits. Provides styles, personnel credits, liner
	              notes from the main release (fuller than master notes),
	              album-level credits (producers, engineers), artist
	              biography (profile), and group members.
	Wikipedia     Album and artist summaries fetched via Wikipedia REST API
	              using URLs discovered in MusicBrainz url-rels. Artist
	              Wikipedia is used as a biography fallback when Discogs
	              profile is empty.

Enrichment data is stored as a JSON sidecar file named .mu_album_metadata.json
in each album's directory. The v2 sidecar schema includes:

	{
	  "version": 2,
	  "fetched_at": "2026-02-07T12:00:00Z",
	  "artist": "Pink Floyd",
	  "album": "The Dark Side of the Moon",
	  "musicbrainz": { "genres": [...], "tags": [...], "year": 1973,
	                    "annotation": "...", "wikipedia_url": "...",
	                    "artist_ids": [...] },
	  "discogs": { "styles": [...], "credits": [...], "notes": "...",
	               "release_notes": "...", "release_credits": [...] },
	  "artist_info": { "name": "...", "type": "Group", "origin": "...",
	                    "biography": "...", "members": [...], ... },
	  "description": { "mb_annotation": "...", "wikipedia_summary": "..." }
	}

Existing v1 sidecars are automatically re-enriched to v2 on the next scan via
the version check in sidecarNeedsRefresh. Artist data is cached by MB/Discogs
artist ID within each enrichment run, so artists with multiple albums are only
fetched once.

The enrichment data feeds into both keyword search and semantic search:

  - Keyword search matches against genres, tags, styles, labels, artist
    name, origin, type, and group members.
  - Semantic search embeddings include genre, tag, year, label, style,
    personnel names, artist info (type, origin, biography, members),
    album description (Wikipedia summary), and release credits,
    enabling queries like "progressive rock 1970s", "British group",
    or "produced by Brian Eno" to match relevant albums.

Negative caching: if neither API returns a match, a minimal sidecar is written
so the album is not re-queried on every scan. Negative cache entries expire
after 30 days, at which point the APIs are tried again.

Rate limiting: MusicBrainz requests are spaced at 1.1 second intervals.
Discogs requests are spaced at 2.5 seconds (unauthenticated) or 1.1 seconds
(with a personal access token). HTTP 429 responses trigger a single retry
after the Retry-After period.

# Configuration Example

The node_id is constructed automatically from provider, namespace, and resource:

	mu:library:<provider>:<namespace>:<resource>

Minimal configuration (uses defaults: provider=filesystem, resource=default):

	[server]
	namespace = "home"

	[modules.fs_library.default]
	enabled = true
	roots = ["/home/user/Music"]
	# Results in node_id: mu:library:filesystem:home:default

Full configuration with all options:

	[modules.fs_library.media]
	enabled = true
	name = "Home Media Library"
	provider = "filesystem"           # default: "filesystem"
	resource = "media"                # default: config key ("media" here)
	roots = ["/home/user/Music", "/home/user/Videos"]
	include_exts = [".mp3", ".flac", ".m4a", ".mp4", ".mkv"]
	http_listen = "127.0.0.1:0"
	index_mode = "near"
	scan_interval_ms = 900000
	repair_policy = "balanced"
	dedupe_policy = "report"
	embedding_provider = "ollama"
	embedding_model = "nomic-embed-text"
	embedding_endpoint = "http://localhost:11434"
	embedding_cache = "/var/lib/mud/library_fs/embeddings"
	enrich_enabled = true                 # auto-enrich albums via MusicBrainz + Discogs
	discogs_token = ""                    # optional Discogs personal access token

# Commands

The module responds to these command types:

	library.browse       Browse containers, returns items with pagination
	library.search       Full-text or semantic search
	library.resolve      Get metadata and playback URLs for an item
	library.resolveBatch Batch resolve multiple items
	library.rescan       Trigger manual rescan of filesystem
*/
package fslibrary

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dhowden/tag"
	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

// Config configures the filesystem library module.
//
// The NodeID is typically constructed by the mud daemon from component fields:
//
//	mu:library:<provider>:<namespace>:<resource>
//
// Where:
//   - provider: defaults to "filesystem" if not specified
//   - namespace: from server configuration (e.g., "home", "office")
//   - resource: defaults to config key name or "default"
//
// Example node IDs:
//
//	mu:library:filesystem:home:default
//	mu:library:filesystem:home:music
//	mu:library:filesystem:office:media
type Config struct {
	// NodeID is the unique identifier for this library instance.
	// Constructed by mud from: mu:library:<provider>:<namespace>:<resource>
	NodeID string

	// TopicBase is the MQTT topic prefix (default: "mu").
	TopicBase string

	// Name is the human-readable library name shown in listings.
	Name string

	// Roots lists directory paths to scan for media files.
	// All paths are scanned recursively.
	Roots []string

	// IncludeExts lists file extensions to include (e.g., ".mp3", ".flac").
	// Extensions should include the leading dot.
	IncludeExts []string

	// HTTPListen is the address for the file server (e.g., "127.0.0.1:8080").
	// Use ":0" for automatic port assignment.
	HTTPListen string

	// IndexMode controls where the index file is stored:
	//   "near"     - Store as .mu_fs_index.json in the first root
	//   "separate" - Store at IndexPath
	//   ""         - No persistence (index rebuilt on restart)
	IndexMode string

	// IndexPath is the file path for the index when IndexMode is "separate".
	IndexPath string

	// ScanIntervalMS is the interval between automatic rescans in milliseconds.
	// Default: 900000 (15 minutes).
	ScanIntervalMS int64

	// MetadataMode is reserved for future metadata handling options.
	MetadataMode string

	// RepairPolicy controls automatic metadata repair:
	//   "none"       - No repair
	//   "strict"     - High-confidence repairs only
	//   "balanced"   - Medium-confidence if no conflicts
	//   "aggressive" - Accept most repairs
	RepairPolicy string

	// DedupePolicy controls duplicate file handling:
	//   "none"   - No deduplication
	//   "report" - Log duplicates only
	//   "first"  - Keep first occurrence
	//   "best"   - Keep highest quality
	DedupePolicy string

	// EmbeddingProvider enables semantic search. Supported: "ollama".
	EmbeddingProvider string

	// EmbeddingModel is the model name for embeddings (e.g., "nomic-embed-text").
	EmbeddingModel string

	// EmbeddingEndpoint is the embedding API URL (e.g., "http://localhost:11434").
	EmbeddingEndpoint string

	// EmbeddingCache is the directory for caching computed embeddings.
	EmbeddingCache string

	// EnrichEnabled enables automatic album metadata enrichment during rescan.
	EnrichEnabled bool

	// DiscogsToken is an optional Discogs personal access token for higher rate limits.
	DiscogsToken string
}

// cmdWork represents a command to be processed by a worker.
type cmdWork struct {
	cmd mu.CommandEnvelope
}

// Module exposes a filesystem library to mu.
//
// The module scans configured roots for media files, maintains an in-memory
// index organized by artist/album, and serves files over HTTP. It subscribes
// to MQTT commands for browse/search/resolve operations.
//
// Lifecycle:
//  1. NewModule creates the module with configuration
//  2. Run starts the HTTP server, performs initial scan, and processes commands
//  3. Context cancellation triggers graceful shutdown
type Module struct {
	log      *zap.Logger
	client   *mqttserver.Client
	config   Config
	cmdTopic string
	cmdQueue chan cmdWork

	mu      sync.RWMutex
	index   *libraryIndex
	baseURL string
	server  *http.Server
	ln      net.Listener

	// Embedding support
	embedProvider EmbeddingProvider
	embedCache    *EmbeddingCache
	vectorIndex   *VectorIndex

	// Deduplication
	dupeIndex *DuplicateIndex

	// Enrichment metadata keyed by "artist|album"
	enrichMeta map[string]*AlbumMetadata
}

// libraryIndex is the in-memory index of all scanned media.
// It is persisted to disk based on IndexMode configuration.
type libraryIndex struct {
	// Items maps item IDs (e.g., "audio:abc123") to media items.
	Items map[string]mediaItem `json:"items"`

	// Audio organizes audio items by artist name -> albums -> track IDs.
	Audio map[string]artistEntry `json:"audio"`

	// Video lists video item IDs in the order they were scanned.
	Video []string `json:"video"`

	// Containers maps hashed container IDs to container info.
	// This enables opaque IDs while preserving fast lookups.
	Containers map[string]containerInfo `json:"containers,omitempty"`
}

// containerInfo stores the data needed to resolve a container by its hash ID.
type containerInfo struct {
	Type   string `json:"type"`   // "artist" or "album"
	Artist string `json:"artist"` // Artist name
	Album  string `json:"album"`  // Album name (only for album type)
}

// artistEntry groups albums under an artist.
type artistEntry struct {
	Name   string                `json:"name"`
	Albums map[string]albumEntry `json:"albums"`
}

// albumEntry groups tracks under an album.
type albumEntry struct {
	Name        string   `json:"name"`
	Tracks      []string `json:"tracks"`      // Item IDs
	CoverArt    string   `json:"coverArt"`    // Path to cover art file or audio file with embedded art
	CoverArtExt string   `json:"coverArtExt"` // Extension for cover art URL (e.g., ".jpg", ".png")
}

// mediaItem represents a single media file in the library.
type mediaItem struct {
	// ID is the unique identifier, format: "<mediatype>:<hash>"
	// Examples: "audio:a1b2c3d4e5f6", "video:789abcdef"
	ID string `json:"id"`

	// Path is the absolute filesystem path to the file.
	Path string `json:"path"`

	// Name is the display name (usually the title or filename).
	Name string `json:"name"`

	// Title is extracted from metadata tags or parsed from filename.
	Title string `json:"title"`

	// Artists lists performer names from metadata.
	Artists []string `json:"artists,omitempty"`

	// Album is the album name from metadata.
	Album string `json:"album,omitempty"`

	// MediaType is "Audio" or "Video".
	MediaType string `json:"mediaType"`

	// DurationMS is the track duration in milliseconds (0 if unknown).
	DurationMS int64 `json:"durationMs,omitempty"`
}

// libraryItemsReply is the response format for browse and search commands.
type libraryItemsReply struct {
	Items []libraryItem `json:"items"`
	Start int64         `json:"start"` // Offset of first item
	Count int64         `json:"count"` // Number of items returned
	Total int64         `json:"total"` // Total items available
}

// libraryItem describes an item returned by browse or search.
// It can represent either a container (folder) or a playable media item.
type libraryItem struct {
	// ItemID is the identifier for this item.
	// Container examples: "container:audio", "artist:Name", "album:Artist:Album"
	// Media examples: "audio:abc123", "video:def456"
	ItemID string `json:"itemId"`

	// Name is the display name.
	Name string `json:"name"`

	// Type indicates the item kind:
	//   "Folder" - Container that can be browsed
	//   "Audio"  - Playable audio file
	//   "Video"  - Playable video file
	Type string `json:"type"`

	// MediaType indicates the media category: "Audio" or "Video".
	MediaType string `json:"mediaType"`

	// Artists lists performer names (audio items only).
	Artists []string `json:"artists,omitempty"`

	// Album is the album name (audio items only).
	Album string `json:"album,omitempty"`

	// ContainerID is the parent container (for media items).
	ContainerID string `json:"containerId,omitempty"`
	Overview    string `json:"overview,omitempty"`
	DurationMS  int64  `json:"durationMs,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
}

// NewModule creates a filesystem library module.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*Module, error) {
	if log == nil {
		log = zap.NewNop()
	}
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, errors.New("node_id required")
	}
	if len(cfg.Roots) == 0 {
		return nil, errors.New("roots required")
	}
	if strings.TrimSpace(cfg.TopicBase) == "" {
		cfg.TopicBase = mu.BaseTopic
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = "Filesystem Library"
	}
	if cfg.ScanIntervalMS <= 0 {
		cfg.ScanIntervalMS = int64((15 * time.Minute) / time.Millisecond)
	}
	if strings.TrimSpace(cfg.HTTPListen) == "" {
		cfg.HTTPListen = "127.0.0.1:0"
	}
	if len(cfg.IncludeExts) == 0 {
		cfg.IncludeExts = []string{".mp3", ".flac", ".ogg", ".m4a", ".mp4", ".mkv"}
	}

	cmdTopic := mu.TopicCommands(cfg.TopicBase, cfg.NodeID)

	// Initialize embedding provider if configured
	var embedProvider EmbeddingProvider
	var embedCache *EmbeddingCache
	if strings.TrimSpace(cfg.EmbeddingProvider) != "" {
		switch strings.ToLower(cfg.EmbeddingProvider) {
		case "ollama":
			provider, err := NewOllamaProvider(OllamaConfig{
				Endpoint: cfg.EmbeddingEndpoint,
				Model:    cfg.EmbeddingModel,
			})
			if err != nil {
				log.Warn("failed to create ollama provider", zap.Error(err))
			} else {
				embedProvider = provider
			}
		default:
			log.Warn("unknown embedding provider", zap.String("provider", cfg.EmbeddingProvider))
		}

		if embedProvider != nil && strings.TrimSpace(cfg.EmbeddingCache) != "" {
			cache, err := NewEmbeddingCache(cfg.EmbeddingCache)
			if err != nil {
				log.Warn("failed to create embedding cache", zap.Error(err))
			} else {
				embedCache = cache
			}
		}
	}

	return &Module{
		log:           log,
		client:        client,
		config:        cfg,
		cmdTopic:      cmdTopic,
		cmdQueue:      make(chan cmdWork, 64),
		index:         &libraryIndex{Items: map[string]mediaItem{}, Audio: map[string]artistEntry{}, Containers: map[string]containerInfo{}},
		embedProvider: embedProvider,
		embedCache:    embedCache,
		vectorIndex:   NewVectorIndex(),
		dupeIndex:     NewDuplicateIndex(),
		enrichMeta:    make(map[string]*AlbumMetadata),
	}, nil
}

// Run starts the module.
func (m *Module) Run(ctx context.Context) error {
	if err := m.publishPresence(); err != nil {
		return err
	}
	if err := m.startHTTPServer(); err != nil {
		return err
	}
	if err := m.loadIndex(); err != nil {
		m.log.Debug("index load failed", zap.Error(err))
	}
	if err := m.scan(); err != nil {
		m.log.Warn("initial scan failed", zap.Error(err))
	}

	// Start command worker pool
	const numWorkers = 4
	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for range numWorkers {
		go func() {
			defer wg.Done()
			m.commandWorker(ctx)
		}()
	}

	handler := func(_ paho.Client, msg paho.Message) {
		m.handleMessage(msg)
	}
	if err := m.client.Subscribe(m.cmdTopic, 1, handler); err != nil {
		return err
	}
	defer m.client.Unsubscribe(m.cmdTopic)

	scanInterval := time.Duration(m.config.ScanIntervalMS) * time.Millisecond
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.shutdownHTTPServer()
			wg.Wait()
			return nil
		case <-ticker.C:
			if err := m.scan(); err != nil {
				m.log.Warn("scan failed", zap.Error(err))
			}
		}
	}
}

func (m *Module) publishPresence() error {
	presence := mu.Presence{
		NodeID: m.config.NodeID,
		Kind:   "library",
		Name:   m.config.Name,
		Caps: map[string]any{
			"resolve":      true,
			"resolveBatch": true,
			"browse":       true,
			"search":       true,
			"rescan":       true,
		},
		TS: time.Now().Unix(),
	}
	payload, err := json.Marshal(presence)
	if err != nil {
		return err
	}
	return m.client.Publish(mu.TopicPresence(m.config.TopicBase, m.config.NodeID), 1, true, payload)
}

// handleMessage receives MQTT messages and queues them for async processing.
func (m *Module) handleMessage(msg paho.Message) {
	var cmd mu.CommandEnvelope
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		m.log.Warn("invalid command", zap.Error(err))
		return
	}

	select {
	case m.cmdQueue <- cmdWork{cmd: cmd}:
		// Queued successfully
	default:
		// Queue full - apply backpressure
		m.log.Warn("command queue full",
			zap.String("id", cmd.ID),
			zap.String("type", cmd.Type))
		if cmd.ReplyTo != "" {
			reply := errorReply(cmd, "OVERLOADED", "command queue full")
			payload, _ := json.Marshal(reply)
			_ = m.client.Publish(cmd.ReplyTo, 1, false, payload)
		}
	}
}

// commandWorker processes commands from the queue.
func (m *Module) commandWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case work := <-m.cmdQueue:
			m.processCommand(work.cmd)
		}
	}
}

func (m *Module) processCommand(cmd mu.CommandEnvelope) {
	reply := m.dispatch(cmd)
	if cmd.ReplyTo == "" {
		return
	}
	payload, err := json.Marshal(reply)
	if err != nil {
		m.log.Error("marshal reply", zap.Error(err))
		return
	}
	if err := m.client.Publish(cmd.ReplyTo, 1, false, payload); err != nil {
		m.log.Error("publish reply", zap.Error(err))
	}
}

func (m *Module) dispatch(cmd mu.CommandEnvelope) mu.ReplyEnvelope {
	reply := mu.ReplyEnvelope{
		ID:   cmd.ID,
		Type: "ack",
		OK:   true,
		TS:   time.Now().Unix(),
	}
	switch cmd.Type {
	case "library.browse":
		return m.libraryBrowse(cmd, reply)
	case "library.search":
		return m.librarySearch(cmd, reply)
	case "library.resolve":
		return m.libraryResolve(cmd, reply)
	case "library.resolveBatch":
		return m.libraryResolveBatch(cmd, reply)
	case "library.rescan":
		return m.libraryRescan(cmd, reply)
	default:
		return errorReply(cmd, "INVALID", "unsupported command")
	}
}

func (m *Module) libraryBrowse(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryBrowseBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}

	items, total, err := m.browse(body.ContainerID, body.Start, body.Count)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(libraryItemsReply{
		Items: items,
		Start: body.Start,
		Count: int64(len(items)),
		Total: total,
	})
	reply.Body = payload
	return reply
}

func (m *Module) librarySearch(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibrarySearchBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	query := strings.TrimSpace(body.Query)
	if query == "" {
		payload, _ := json.Marshal(libraryItemsReply{Items: nil, Start: body.Start, Count: 0, Total: 0})
		reply.Body = payload
		return reply
	}
	items, total := m.search(query, body.Start, body.Count)
	payload, _ := json.Marshal(libraryItemsReply{
		Items: items,
		Start: body.Start,
		Count: int64(len(items)),
		Total: total,
	})
	reply.Body = payload
	return reply
}

func (m *Module) libraryResolve(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryResolveBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}

	// Try container resolution first (artist:, album:, container:)
	if meta, ok := m.resolveContainerMetadata(body.ItemID); ok {
		payload, _ := json.Marshal(mu.LibraryResolveReply{
			ItemID:   body.ItemID,
			Metadata: meta,
			Sources:  []mu.ResolvedSource{}, // Containers have no playable sources
		})
		reply.Body = payload
		return reply
	}

	// Fall back to media item resolution
	item, ok := m.getItem(body.ItemID)
	if !ok {
		return errorReply(cmd, "NOT_FOUND", "item not found")
	}

	metadata := map[string]any{
		"title":     item.Title,
		"artists":   item.Artists,
		"album":     item.Album,
		"duration":  item.DurationMS,
		"type":      item.MediaType,
		"mediaType": item.MediaType,
	}

	// Add artwork URL for audio tracks
	if artURL := m.getItemArtworkURL(item); artURL != "" {
		metadata["artworkUrl"] = artURL
	}

	// If metadataOnly, skip source URL generation
	if body.MetadataOnly {
		payload, _ := json.Marshal(mu.LibraryResolveReply{
			ItemID:   item.ID,
			Metadata: metadata,
			Sources:  []mu.ResolvedSource{},
		})
		reply.Body = payload
		return reply
	}

	sourceURL, err := m.sourceURL(item.ID)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(mu.LibraryResolveReply{
		ItemID:   item.ID,
		Metadata: metadata,
		Sources:  []mu.ResolvedSource{{URL: sourceURL, ByteRange: true}},
	})
	reply.Body = payload
	return reply
}

func (m *Module) libraryResolveBatch(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryResolveBatchBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	items := make([]mu.LibraryResolveBatchItem, 0, len(body.ItemIDs))
	for _, itemID := range body.ItemIDs {
		// Try container resolution first
		if meta, ok := m.resolveContainerMetadata(itemID); ok {
			items = append(items, mu.LibraryResolveBatchItem{
				ItemID:   itemID,
				Metadata: meta,
				Sources:  []mu.ResolvedSource{},
			})
			continue
		}

		// Fall back to media item resolution
		item, ok := m.getItem(itemID)
		if !ok {
			items = append(items, mu.LibraryResolveBatchItem{
				ItemID: itemID,
				Err:    &mu.ReplyError{Code: "NOT_FOUND", Message: "item not found"},
			})
			continue
		}

		metadata := map[string]any{
			"title":     item.Title,
			"artists":   item.Artists,
			"album":     item.Album,
			"duration":  item.DurationMS,
			"type":      item.MediaType,
			"mediaType": item.MediaType,
		}

		// Add artwork URL
		if artURL := m.getItemArtworkURL(item); artURL != "" {
			metadata["artworkUrl"] = artURL
		}

		// If metadataOnly, skip source URL generation
		if body.MetadataOnly {
			items = append(items, mu.LibraryResolveBatchItem{
				ItemID:   item.ID,
				Metadata: metadata,
				Sources:  []mu.ResolvedSource{},
			})
			continue
		}

		sourceURL, err := m.sourceURL(item.ID)
		if err != nil {
			items = append(items, mu.LibraryResolveBatchItem{
				ItemID: itemID,
				Err:    &mu.ReplyError{Code: "INVALID", Message: err.Error()},
			})
			continue
		}
		items = append(items, mu.LibraryResolveBatchItem{
			ItemID:   item.ID,
			Metadata: metadata,
			Sources:  []mu.ResolvedSource{{URL: sourceURL, ByteRange: true}},
		})
	}
	payload, _ := json.Marshal(mu.LibraryResolveBatchReply{Items: items})
	reply.Body = payload
	return reply
}

// rescanReply is the response for library.rescan.
type rescanReply struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Items   int    `json:"items,omitempty"`
}

func (m *Module) libraryRescan(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	// Parse optional body for sync mode
	var body struct {
		Async bool `json:"async"`
	}
	if len(cmd.Body) > 0 {
		_ = json.Unmarshal(cmd.Body, &body)
	}

	if body.Async {
		// Run scan asynchronously
		go func() {
			if err := m.scan(); err != nil {
				m.log.Warn("async rescan failed", zap.Error(err))
			}
		}()
		payload, _ := json.Marshal(rescanReply{
			Status:  "started",
			Message: "rescan started in background",
		})
		reply.Body = payload
		return reply
	}

	// Run scan synchronously
	if err := m.scan(); err != nil {
		return errorReply(cmd, "SCAN_FAILED", err.Error())
	}

	m.mu.RLock()
	itemCount := len(m.index.Items)
	m.mu.RUnlock()

	payload, _ := json.Marshal(rescanReply{
		Status: "complete",
		Items:  itemCount,
	})
	reply.Body = payload
	return reply
}

func (m *Module) browse(containerID string, start int64, count int64) ([]libraryItem, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Root: list audio and video containers
	if containerID == "" {
		defaultImg := m.defaultArtURLUnlocked()
		items := []libraryItem{
			{ItemID: "container:audio", Name: "Audio", Type: "Folder", ImageURL: defaultImg},
			{ItemID: "container:video", Name: "Video", Type: "Folder", ImageURL: defaultImg},
		}
		return paginate(items, start, count), int64(len(items)), nil
	}

	// Audio root: list artists
	if containerID == "container:audio" {
		artists := make([]string, 0, len(m.index.Audio))
		for artist := range m.index.Audio {
			artists = append(artists, artist)
		}
		sort.Strings(artists)
		defaultImg := m.defaultArtURLUnlocked()
		items := make([]libraryItem, 0, len(artists))
		for _, artistName := range artists {
			artistHash := containerHash("artist", artistName, "")
			items = append(items, libraryItem{
				ItemID:      artistHash,
				Name:        artistName,
				Type:        "Folder",
				ContainerID: "container:audio",
				ImageURL:    defaultImg,
			})
		}
		return paginate(items, start, count), int64(len(items)), nil
	}

	// Video root: list videos
	if containerID == "container:video" {
		defaultImg := m.defaultArtURLUnlocked()
		items := make([]libraryItem, 0, len(m.index.Video))
		for _, itemID := range m.index.Video {
			item, ok := m.index.Items[itemID]
			if !ok {
				continue
			}
			items = append(items, libraryItem{
				ItemID:      item.ID,
				Name:        item.Name,
				Type:        item.MediaType,
				MediaType:   item.MediaType,
				DurationMS:  item.DurationMS,
				ContainerID: "container:video",
				ImageURL:    defaultImg,
			})
		}
		return paginate(items, start, count), int64(len(items)), nil
	}

	// Look up container by hash
	info, ok := m.index.Containers[containerID]
	if !ok {
		return nil, 0, errors.New("container not found")
	}

	// Artist container: list albums
	if info.Type == "artist" {
		artist, ok := m.index.Audio[info.Artist]
		if !ok {
			return nil, 0, errors.New("artist not found")
		}
		albums := make([]string, 0, len(artist.Albums))
		for album := range artist.Albums {
			albums = append(albums, album)
		}
		sort.Strings(albums)
		items := make([]libraryItem, 0, len(albums))
		for _, albumName := range albums {
			albumHash := containerHash("album", info.Artist, albumName)
			items = append(items, libraryItem{
				ItemID:      albumHash,
				Name:        albumName,
				Type:        "Folder",
				ContainerID: containerID,
				ImageURL:    m.artURLUnlocked(albumHash),
			})
		}
		return paginate(items, start, count), int64(len(items)), nil
	}

	// Album container: list tracks
	if info.Type == "album" {
		artist, ok := m.index.Audio[info.Artist]
		if !ok {
			return nil, 0, errors.New("artist not found")
		}
		album, ok := artist.Albums[info.Album]
		if !ok {
			return nil, 0, errors.New("album not found")
		}
		// Get art URL (defaults to placeholder if no cover art)
		imageURL := m.artURLUnlocked(containerID)
		items := make([]libraryItem, 0, len(album.Tracks))
		for _, itemID := range album.Tracks {
			item, ok := m.index.Items[itemID]
			if !ok {
				continue
			}
			items = append(items, libraryItem{
				ItemID:      item.ID,
				Name:        item.Name,
				Type:        item.MediaType,
				MediaType:   item.MediaType,
				Artists:     item.Artists,
				Album:       item.Album,
				DurationMS:  item.DurationMS,
				ContainerID: containerID,
				ImageURL:    imageURL,
			})
		}
		return paginate(items, start, count), int64(len(items)), nil
	}

	return nil, 0, errors.New("unsupported container")
}

func (m *Module) search(query string, start int64, count int64) ([]libraryItem, int64) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, 0
	}

	// Try semantic search first if embeddings are available
	if m.embedProvider != nil && m.vectorIndex != nil {
		semanticResults := m.semanticSearch(query, start, count)
		if len(semanticResults) > 0 {
			return semanticResults, int64(len(semanticResults))
		}
	}

	// Fall back to keyword search
	terms := strings.Fields(strings.ToLower(query))
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]libraryItem, 0)
	for _, item := range m.index.Items {
		var enrich *AlbumMetadata
		if item.MediaType == "Audio" {
			key := firstOr(item.Artists, "Unknown Artist") + "|" + item.Album
			enrich = m.enrichMeta[key]
		}
		if !containsAllTerms(item, terms, enrich) {
			continue
		}
		artURL := ""
		if item.MediaType == "Audio" {
			artistName := firstOr(item.Artists, "Unknown Artist")
			albumName := item.Album
			if albumName == "" {
				albumName = "Unknown Album"
			}
			artURL = m.artURLUnlocked(containerHash("album", artistName, albumName))
		}
		items = append(items, libraryItem{
			ItemID:     item.ID,
			Name:       item.Name,
			Type:       item.MediaType,
			MediaType:  item.MediaType,
			Artists:    item.Artists,
			Album:      item.Album,
			DurationMS: item.DurationMS,
			ImageURL:   artURL,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	total := int64(len(items))
	return paginate(items, start, count), total
}

func (m *Module) semanticSearch(query string, start int64, count int64) []libraryItem {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m.log.Debug("semantic search started",
		zap.String("query", query),
		zap.Int64("start", start),
		zap.Int64("count", count))

	// Embed the query
	results, err := m.embedProvider.Embed(ctx, []EmbedInput{{ID: "query", Text: query}})
	if err != nil || len(results) == 0 || len(results[0].Vector) == 0 {
		m.log.Debug("query embedding failed", zap.Error(err))
		return nil
	}

	queryVec := results[0].Vector
	m.log.Debug("query embedded successfully",
		zap.Int("vector_dimension", len(queryVec)))

	// Search vector index
	limit := int(count)
	if limit <= 0 {
		limit = 100
	}
	// Request more to allow for pagination
	searchLimit := int(start) + limit + 10
	similar := m.vectorIndex.Search(queryVec, searchLimit)

	m.log.Debug("vector search complete",
		zap.Int("results_count", len(similar)),
		zap.Int("search_limit", searchLimit))

	// Log score distribution for debugging
	if len(similar) > 0 {
		var minScore, maxScore float32 = similar[0].Score, similar[0].Score
		var totalScore float64
		for _, r := range similar {
			if r.Score < minScore {
				minScore = r.Score
			}
			if r.Score > maxScore {
				maxScore = r.Score
			}
			totalScore += float64(r.Score)
		}
		avgScore := totalScore / float64(len(similar))
		m.log.Debug("similarity score distribution",
			zap.Float32("max_score", maxScore),
			zap.Float32("min_score", minScore),
			zap.Float64("avg_score", avgScore))

		// Log top 5 results with their scores
		topN := 5
		if len(similar) < topN {
			topN = len(similar)
		}
		m.mu.RLock()
		for i := 0; i < topN; i++ {
			r := similar[i]
			if item, ok := m.index.Items[r.ID]; ok {
				m.log.Debug("top semantic result",
					zap.Int("rank", i+1),
					zap.Float32("score", r.Score),
					zap.String("title", item.Title),
					zap.Strings("artists", item.Artists),
					zap.String("album", item.Album))
			}
		}
		m.mu.RUnlock()
	}

	// Filter by minimum similarity threshold
	// Note: Cosine similarity ranges from -1 to 1, where 1 is identical.
	// A threshold of 0.5 is a reasonable cutoff for "somewhat related" content.
	// Higher values (0.6-0.7) would be more strict.
	const minSimilarity = 0.5
	filtered := make([]SimilarityResult, 0, len(similar))
	for _, r := range similar {
		if r.Score >= minSimilarity {
			filtered = append(filtered, r)
		}
	}

	m.log.Debug("similarity threshold applied",
		zap.Float32("threshold", minSimilarity),
		zap.Int("before_filter", len(similar)),
		zap.Int("after_filter", len(filtered)))

	// Apply pagination
	if int64(len(filtered)) <= start {
		m.log.Debug("no results after pagination offset",
			zap.Int64("start", start),
			zap.Int("filtered_count", len(filtered)))
		return nil
	}
	end := start + count
	if end > int64(len(filtered)) {
		end = int64(len(filtered))
	}
	filtered = filtered[start:end]

	m.log.Debug("pagination applied",
		zap.Int64("start", start),
		zap.Int64("end", end),
		zap.Int("page_size", len(filtered)))

	// Convert to library items
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]libraryItem, 0, len(filtered))
	for _, r := range filtered {
		item, ok := m.index.Items[r.ID]
		if !ok {
			continue
		}
		artURL := ""
		if item.MediaType == "Audio" {
			artistName := firstOr(item.Artists, "Unknown Artist")
			albumName := item.Album
			if albumName == "" {
				albumName = "Unknown Album"
			}
			artURL = m.artURLUnlocked(containerHash("album", artistName, albumName))
		}
		items = append(items, libraryItem{
			ItemID:     item.ID,
			Name:       item.Name,
			Type:       item.MediaType,
			MediaType:  item.MediaType,
			Artists:    item.Artists,
			Album:      item.Album,
			DurationMS: item.DurationMS,
			ImageURL:   artURL,
		})
	}

	m.log.Debug("semantic search complete",
		zap.String("query", query),
		zap.Int("results_returned", len(items)))

	return items
}

func (m *Module) buildEmbeddings(items map[string]mediaItem) {
	if m.embedProvider == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	m.log.Debug("starting embedding build",
		zap.Int("total_items", len(items)),
		zap.String("provider", m.embedProvider.Name()))

	// Clear old vectors
	m.vectorIndex.Clear()

	// Collect items needing embeddings
	var inputs []EmbedInput
	cached := 0

	for id, item := range items {
		var enrich *AlbumMetadata
		if item.MediaType == "Audio" {
			key := firstOr(item.Artists, "Unknown Artist") + "|" + item.Album
			m.mu.RLock()
			enrich = m.enrichMeta[key]
			m.mu.RUnlock()
		}
		text := buildEmbedText(item, enrich)
		if text == "" {
			m.log.Debug("skipping item with empty embed text", zap.String("id", id))
			continue
		}

		// Check cache first
		if m.embedCache != nil {
			if vec, ok := m.embedCache.Get(id, text); ok {
				m.vectorIndex.Add(id, vec)
				cached++
				continue
			}
		}

		inputs = append(inputs, EmbedInput{ID: id, Text: text})
	}

	if cached > 0 {
		m.log.Debug("loaded embeddings from cache", zap.Int("count", cached))
	}

	if len(inputs) == 0 {
		m.log.Debug("all embeddings loaded from cache, nothing to compute")
		return
	}

	m.log.Info("building embeddings",
		zap.Int("to_compute", len(inputs)),
		zap.Int("from_cache", cached))

	// Log sample of texts being embedded
	sampleSize := 3
	if len(inputs) < sampleSize {
		sampleSize = len(inputs)
	}
	for i := 0; i < sampleSize; i++ {
		m.log.Debug("sample embed text",
			zap.Int("index", i),
			zap.String("id", inputs[i].ID),
			zap.String("text", inputs[i].Text))
	}

	// Batch embed
	const batchSize = 32
	for i := 0; i < len(inputs); i += batchSize {
		end := i + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[i:end]

		results, err := m.embedProvider.Embed(ctx, batch)
		if err != nil {
			m.log.Warn("embedding batch failed", zap.Error(err), zap.Int("batch", i/batchSize))
			continue
		}

		m.log.Debug("batch embedded",
			zap.Int("batch_num", i/batchSize),
			zap.Int("batch_size", len(batch)),
			zap.Int("results", len(results)))

		for j, result := range results {
			if len(result.Vector) == 0 {
				m.log.Debug("empty vector result", zap.String("id", result.ID))
				continue
			}
			m.vectorIndex.Add(result.ID, result.Vector)

			// Cache the embedding
			if m.embedCache != nil && j < len(batch) {
				m.embedCache.Put(result.ID, batch[j].Text, result.Vector)
			}
		}
	}

	m.log.Info("embeddings built",
		zap.Int("total", len(inputs)+cached),
		zap.Int("new", len(inputs)),
		zap.Int("cached", cached),
		zap.Int("vector_index_size", m.vectorIndex.Size()))
}

func containsAllTerms(item mediaItem, terms []string, enrich *AlbumMetadata) bool {
	parts := []string{item.Name, item.Title, item.Album, strings.Join(item.Artists, " ")}
	if enrich != nil {
		if mb := enrich.MusicBrainz; mb != nil {
			parts = append(parts, strings.Join(mb.Genres, " "))
			parts = append(parts, strings.Join(mb.Tags, " "))
			if mb.Label != "" {
				parts = append(parts, mb.Label)
			}
		}
		if dc := enrich.Discogs; dc != nil {
			parts = append(parts, strings.Join(dc.Styles, " "))
		}
		if ai := enrich.ArtistInfo; ai != nil {
			parts = append(parts, ai.Name)
			if ai.Origin != "" {
				parts = append(parts, ai.Origin)
			}
			if ai.Type != "" {
				parts = append(parts, ai.Type)
			}
			if len(ai.Members) > 0 {
				parts = append(parts, strings.Join(ai.Members, " "))
			}
		}
	}
	searchText := strings.ToLower(strings.Join(parts, " "))
	for _, term := range terms {
		if !strings.Contains(searchText, term) {
			return false
		}
	}
	return true
}

func (m *Module) scan() error {
	started := time.Now()
	exts := buildExtMap(m.config.IncludeExts)
	audioExts := defaultAudioExts()
	videoExts := defaultVideoExts()

	next := &libraryIndex{
		Items:      map[string]mediaItem{},
		Audio:      map[string]artistEntry{},
		Containers: map[string]containerInfo{},
	}

	for _, root := range m.config.Roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				m.log.Debug("walk error", zap.Error(err), zap.String("path", path))
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if !exts[ext] {
				return nil
			}
			item, err := m.buildItem(path, audioExts, videoExts)
			if err != nil {
				m.log.Debug("item build failed", zap.Error(err), zap.String("path", path))
				return nil
			}
			next.Items[item.ID] = item
			if item.MediaType == "Audio" {
				artistName := firstOr(item.Artists, "Unknown Artist")
				albumName := item.Album
				if albumName == "" {
					albumName = "Unknown Album"
				}
				artist := next.Audio[artistName]
				if artist.Albums == nil {
					artist = artistEntry{Name: artistName, Albums: map[string]albumEntry{}}
				}
				album := artist.Albums[albumName]
				album.Name = albumName
				album.Tracks = append(album.Tracks, item.ID)
				artist.Albums[albumName] = album
				next.Audio[artistName] = artist
			} else if item.MediaType == "Video" {
				next.Video = append(next.Video, item.ID)
			}
			return nil
		})
		if err != nil {
			m.log.Warn("walk failed", zap.Error(err), zap.String("root", root))
		}
	}

	for artistName, artist := range next.Audio {
		for albumName, album := range artist.Albums {
			sort.Strings(album.Tracks)
			// Look for cover art in the album directory or embedded in tracks
			if len(album.Tracks) > 0 {
				if item, ok := next.Items[album.Tracks[0]]; ok {
					dir := filepath.Dir(item.Path)
					// First try external cover art files
					album.CoverArt = findCoverArt(dir)
					if album.CoverArt != "" {
						album.CoverArtExt = strings.ToLower(filepath.Ext(album.CoverArt))
					} else {
						// Fall back to embedded art in the first track
						if ext := getEmbeddedArtExt(item.Path); ext != "" {
							album.CoverArt = item.Path // Store track path; serveArt will extract
							album.CoverArtExt = ext
						}
					}
				}
			}
			artist.Albums[albumName] = album
		}
		next.Audio[artistName] = artist
	}
	sort.Strings(next.Video)

	// Build container ID mappings for artists and albums
	for artistName, artist := range next.Audio {
		artistHash := containerHash("artist", artistName, "")
		next.Containers[artistHash] = containerInfo{Type: "artist", Artist: artistName}
		for albumName := range artist.Albums {
			albumHash := containerHash("album", artistName, albumName)
			next.Containers[albumHash] = containerInfo{Type: "album", Artist: artistName, Album: albumName}
		}
	}

	// Apply metadata repair pipeline
	repairPolicy := RepairPolicy(strings.ToLower(strings.TrimSpace(m.config.RepairPolicy)))
	if repairPolicy != RepairPolicyNone && repairPolicy != "" {
		for id, item := range next.Items {
			repaired := repairMetadata(item, repairPolicy)
			if repaired.Source != "original" {
				item.Title = repaired.Title
				item.Artists = repaired.Artists
				item.Album = repaired.Album
				item.Name = repaired.Title
				next.Items[id] = item
				m.log.Debug("repaired metadata",
					zap.String("id", id),
					zap.String("source", repaired.Source),
					zap.Float32("confidence", repaired.Confidence))
			}
		}
	}

	// Deduplication detection
	dedupePolicy := DedupePolicy(strings.ToLower(strings.TrimSpace(m.config.DedupePolicy)))
	m.dupeIndex.Clear()
	if dedupePolicy != DedupePolicyNone && dedupePolicy != "" {
		dupeCount := 0
		for id, item := range next.Items {
			hash, err := computeFileHash(item.Path)
			if err != nil {
				m.log.Debug("hash failed", zap.String("path", item.Path), zap.Error(err))
				continue
			}
			if m.dupeIndex.Add(id, hash) {
				dupeCount++
				original := m.dupeIndex.Original(id)
				m.log.Debug("duplicate detected",
					zap.String("id", id),
					zap.String("original", original),
					zap.String("path", item.Path))

				// Apply policy
				if dedupePolicy == DedupePolicyFirst {
					delete(next.Items, id)
				}
			}
		}
		if dupeCount > 0 {
			m.log.Info("duplicates found", zap.Int("count", dupeCount))
		}
	}

	// Load sidecar enrichment data and collect enrichment targets
	enrichMeta := make(map[string]*AlbumMetadata)
	var enrichTargets []enrichTarget
	for artistName, artist := range next.Audio {
		for albumName, album := range artist.Albums {
			if len(album.Tracks) == 0 {
				continue
			}
			item, ok := next.Items[album.Tracks[0]]
			if !ok {
				continue
			}
			dir := filepath.Dir(item.Path)
			if meta, err := readSidecar(dir); err == nil {
				if sidecarNeedsRefresh(meta) {
					enrichTargets = append(enrichTargets, enrichTarget{
						Artist: artistName, Album: albumName, Dir: dir,
					})
				} else {
					enrichMeta[artistName+"|"+albumName] = meta
				}
			} else if m.config.EnrichEnabled && !sidecarExists(dir) {
				enrichTargets = append(enrichTargets, enrichTarget{
					Artist: artistName, Album: albumName, Dir: dir,
				})
			}
		}
	}

	// Sidecar-informed repair pass
	if repairPolicy != RepairPolicyNone && repairPolicy != "" {
		for id, item := range next.Items {
			if item.MediaType != "Audio" {
				continue
			}
			artistName := firstOr(item.Artists, "Unknown Artist")
			albumName := item.Album
			if albumName == "" {
				albumName = "Unknown Album"
			}
			key := artistName + "|" + albumName
			meta, ok := enrichMeta[key]
			if !ok || meta == nil {
				continue
			}
			repaired := repairFromSidecar(item, meta, repairPolicy)
			if repaired.Source != "original" {
				item.Artists = repaired.Artists
				item.Album = repaired.Album
				next.Items[id] = item
				m.log.Debug("sidecar-repaired metadata",
					zap.String("id", id),
					zap.String("source", repaired.Source),
					zap.Float32("confidence", repaired.Confidence))
			}
		}
	}

	m.mu.Lock()
	m.index = next
	m.enrichMeta = enrichMeta
	m.mu.Unlock()

	// Build embeddings asynchronously
	if m.embedProvider != nil {
		go m.buildEmbeddings(next.Items)
	}

	// Launch enrichment in background
	if m.config.EnrichEnabled && len(enrichTargets) > 0 {
		go m.enrichAlbums(context.Background(), enrichTargets)
	}

	if err := m.saveIndex(); err != nil {
		m.log.Debug("index save failed", zap.Error(err))
	}
	m.log.Info("scan complete", zap.Duration("elapsed", time.Since(started)), zap.Int("items", len(next.Items)))
	return nil
}

func (m *Module) buildItem(path string, audioExts map[string]bool, videoExts map[string]bool) (mediaItem, error) {
	info, err := os.Stat(path)
	if err != nil {
		return mediaItem{}, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	mediaType := "Audio"
	switch {
	case videoExts[ext]:
		mediaType = "Video"
	case audioExts[ext]:
		mediaType = "Audio"
	default:
		mediaType = "Audio"
	}

	meta, err := readTags(path)
	if err != nil {
		meta = fallbackMetadata(path)
	}

	if meta.Title == "" {
		meta.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	itemID := hashID(path, info.Size(), info.ModTime())
	name := meta.Title
	if name == "" {
		name = filepath.Base(path)
	}
	return mediaItem{
		ID:         itemID, // Use hash only, no prefix - avoids lib: ref parsing ambiguity
		Path:       path,
		Name:       name,
		Title:      meta.Title,
		Artists:    meta.Artists,
		Album:      meta.Album,
		MediaType:  mediaType,
		DurationMS: meta.DurationMS,
	}, nil
}

type tagMetadata struct {
	Title      string
	Artists    []string
	Album      string
	DurationMS int64
}

func readTags(path string) (tagMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return tagMetadata{}, err
	}
	defer f.Close()

	metadata, err := tag.ReadFrom(f)
	if err != nil {
		return tagMetadata{}, err
	}

	var artists []string
	if artist := strings.TrimSpace(metadata.Artist()); artist != "" {
		artists = []string{artist}
	}
	return tagMetadata{
		Title:      strings.TrimSpace(metadata.Title()),
		Artists:    artists,
		Album:      strings.TrimSpace(metadata.Album()),
		DurationMS: 0,
	}, nil
}

func fallbackMetadata(path string) tagMetadata {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	parts := strings.SplitN(name, " - ", 2)
	meta := tagMetadata{}
	if len(parts) == 2 {
		meta.Artists = []string{strings.TrimSpace(parts[0])}
		meta.Title = strings.TrimSpace(parts[1])
	} else {
		meta.Title = name
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		meta.Album = filepath.Base(dir)
		parent := filepath.Base(filepath.Dir(dir))
		if len(meta.Artists) == 0 && parent != "" && parent != "." && parent != string(filepath.Separator) {
			meta.Artists = []string{parent}
		}
	}
	return meta
}

func buildExtMap(exts []string) map[string]bool {
	out := make(map[string]bool, len(exts))
	for _, ext := range exts {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		out[ext] = true
	}
	return out
}

func defaultAudioExts() map[string]bool {
	return map[string]bool{
		".mp3":  true,
		".flac": true,
		".ogg":  true,
		".m4a":  true,
	}
}

func defaultVideoExts() map[string]bool {
	return map[string]bool{
		".mp4": true,
		".mkv": true,
	}
}

func (m *Module) getItem(itemID string) (mediaItem, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.index.Items[itemID]
	return item, ok
}

// getItemArtworkURL returns the artwork URL for a media item.
// For audio items, this is the album artwork. For video or unknown items,
// returns the default placeholder.
func (m *Module) getItemArtworkURL(item mediaItem) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.baseURL == "" {
		return ""
	}

	// For audio items, look up album artwork
	if item.MediaType == "Audio" {
		artistName := firstOr(item.Artists, "Unknown Artist")
		albumName := item.Album
		if albumName == "" {
			albumName = "Unknown Album"
		}
		albumHash := containerHash("album", artistName, albumName)
		return m.artURLUnlocked(albumHash)
	}

	// For video or other items, use default
	return m.defaultArtURLUnlocked()
}

// resolveContainerMetadata returns metadata for container IDs.
// Handles both fixed containers (container:audio, container:video) and
// hashed container IDs for artists and albums.
// Returns nil, false if the itemID is not a container.
func (m *Module) resolveContainerMetadata(itemID string) (map[string]any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Fixed root containers
	switch itemID {
	case "container:audio":
		return map[string]any{
			"title": "Audio",
			"type":  "Folder",
		}, true
	case "container:video":
		return map[string]any{
			"title": "Video",
			"type":  "Folder",
		}, true
	}

	// Look up hashed container ID
	info, ok := m.index.Containers[itemID]
	if !ok {
		return nil, false
	}

	switch info.Type {
	case "artist":
		return map[string]any{
			"title": info.Artist,
			"type":  "MusicArtist",
		}, true
	case "album":
		return map[string]any{
			"title":   info.Album,
			"artists": []string{info.Artist},
			"type":    "MusicAlbum",
		}, true
	}

	return nil, false
}

func (m *Module) sourceURL(itemID string) (string, error) {
	m.mu.RLock()
	baseURL := m.baseURL
	m.mu.RUnlock()
	if baseURL == "" {
		return "", errors.New("http server not ready")
	}
	return fmt.Sprintf("%s/files/%s", strings.TrimRight(baseURL, "/"), url.PathEscape(itemID)), nil
}

func (m *Module) startHTTPServer() error {
	ln, err := net.Listen("tcp", m.config.HTTPListen)
	if err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		_ = ln.Close()
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	baseURL := fmt.Sprintf("http://%s:%s", host, port)
	mux := http.NewServeMux()
	mux.HandleFunc("/files/", m.serveFile)
	mux.HandleFunc("/art/default.png", m.serveDefaultArt)
	mux.HandleFunc("/art/", m.serveArt)
	server := &http.Server{Handler: mux}

	m.mu.Lock()
	m.baseURL = baseURL
	m.server = server
	m.ln = ln
	m.mu.Unlock()

	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.log.Warn("http server stopped", zap.Error(err))
		}
	}()
	m.log.Info("http server started", zap.String("base_url", baseURL))
	return nil
}

func (m *Module) shutdownHTTPServer() {
	m.mu.Lock()
	server := m.server
	m.server = nil
	ln := m.ln
	m.ln = nil
	m.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = server.Shutdown(ctx)
		cancel()
	}
}

func (m *Module) serveFile(w http.ResponseWriter, r *http.Request) {
	itemID := strings.TrimPrefix(r.URL.Path, "/files/")
	itemID, err := url.PathUnescape(itemID)
	if err != nil {
		http.Error(w, "invalid item id", http.StatusBadRequest)
		return
	}
	item, ok := m.getItem(itemID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(item.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	http.ServeContent(w, r, filepath.Base(item.Path), time.Now(), f)
}

func (m *Module) serveArt(w http.ResponseWriter, r *http.Request) {
	albumHash := strings.TrimPrefix(r.URL.Path, "/art/")
	albumHash, err := url.PathUnescape(albumHash)
	if err != nil {
		http.Error(w, "invalid album id", http.StatusBadRequest)
		return
	}
	// Strip image extension if present (e.g., ".jpg", ".png")
	if ext := filepath.Ext(albumHash); ext != "" {
		albumHash = strings.TrimSuffix(albumHash, ext)
	}

	m.mu.RLock()
	info, ok := m.index.Containers[albumHash]
	if !ok || info.Type != "album" {
		m.mu.RUnlock()
		http.NotFound(w, r)
		return
	}
	artist, ok := m.index.Audio[info.Artist]
	if !ok {
		m.mu.RUnlock()
		http.NotFound(w, r)
		return
	}
	album, ok := artist.Albums[info.Album]
	if !ok || album.CoverArt == "" {
		m.mu.RUnlock()
		http.NotFound(w, r)
		return
	}
	coverPath := album.CoverArt
	m.mu.RUnlock()

	// Check if coverPath is an audio file (embedded art) or image file
	ext := strings.ToLower(filepath.Ext(coverPath))
	audioExts := defaultAudioExts()
	if audioExts[ext] {
		// Extract embedded art from audio file
		data, mime, err := extractEmbeddedArt(coverPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Write(data)
		return
	}

	// Serve external image file with proper Content-Type
	mime := extToMime(ext)
	f, err := os.Open(coverPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info2, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	http.ServeContent(w, r, filepath.Base(coverPath), info2.ModTime(), f)
}

// extToMime converts a file extension to a MIME type.
func extToMime(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

func (m *Module) serveDefaultArt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(defaultArtPNG)))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(defaultArtPNG)
}

// defaultArtURLUnlocked returns the URL for the default placeholder image.
// Caller must hold the read lock.
func (m *Module) defaultArtURLUnlocked() string {
	if m.baseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/art/default.png", strings.TrimRight(m.baseURL, "/"))
}

// artURLUnlocked returns the URL for album art when caller already holds the lock.
// The albumHash is used to look up the extension from the album entry.
// Returns the default placeholder image URL if no cover art is available.
func (m *Module) artURLUnlocked(albumHash string) string {
	if m.baseURL == "" {
		return ""
	}
	base := strings.TrimRight(m.baseURL, "/")

	// Look up the extension from the container info
	info, ok := m.index.Containers[albumHash]
	if !ok || info.Type != "album" {
		return fmt.Sprintf("%s/art/default.png", base)
	}
	artist, ok := m.index.Audio[info.Artist]
	if !ok {
		return fmt.Sprintf("%s/art/default.png", base)
	}
	album, ok := artist.Albums[info.Album]
	if !ok || album.CoverArt == "" {
		return fmt.Sprintf("%s/art/default.png", base)
	}
	ext := album.CoverArtExt
	if ext == "" {
		ext = ".jpg"
	}
	return fmt.Sprintf("%s/art/%s%s", base, url.PathEscape(albumHash), ext)
}

func (m *Module) indexFilePath() (string, error) {
	mode := strings.ToLower(strings.TrimSpace(m.config.IndexMode))
	switch mode {
	case "":
		if strings.TrimSpace(m.config.IndexPath) == "" {
			return "", nil
		}
		return m.config.IndexPath, nil
	case "separate":
		if strings.TrimSpace(m.config.IndexPath) == "" {
			return "", errors.New("index_path required for separate mode")
		}
		return m.config.IndexPath, nil
	case "near":
		root := strings.TrimSpace(m.config.Roots[0])
		if root == "" {
			return "", errors.New("root required for near mode")
		}
		return filepath.Join(root, ".mu_fs_index.json"), nil
	default:
		return "", errors.New("invalid index_mode (use near|separate)")
	}
}

func (m *Module) loadIndex() error {
	path, err := m.indexFilePath()
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var idx libraryIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}
	if idx.Items == nil {
		idx.Items = map[string]mediaItem{}
	}
	if idx.Audio == nil {
		idx.Audio = map[string]artistEntry{}
	}
	if idx.Containers == nil {
		idx.Containers = map[string]containerInfo{}
	}
	m.mu.Lock()
	m.index = &idx
	m.mu.Unlock()
	return nil
}

func (m *Module) saveIndex() error {
	path, err := m.indexFilePath()
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	m.mu.RLock()
	data, err := json.Marshal(m.index)
	m.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o640)
}

func hashID(path string, size int64, mod time.Time) string {
	h := md5.New()
	_, _ = io.WriteString(h, path)
	_, _ = io.WriteString(h, fmt.Sprintf("|%d|%d", size, mod.UnixNano()))
	return hex.EncodeToString(h.Sum(nil))
}

// containerHash generates a stable hash ID for a container (artist or album).
func containerHash(containerType, artist, album string) string {
	h := md5.New()
	_, _ = io.WriteString(h, containerType)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, artist)
	if album != "" {
		_, _ = io.WriteString(h, "|")
		_, _ = io.WriteString(h, album)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func paginate[T any](items []T, start int64, count int64) []T {
	if start < 0 {
		start = 0
	}
	if count <= 0 {
		count = int64(len(items))
	}
	end := start + count
	if start > int64(len(items)) {
		return nil
	}
	if end > int64(len(items)) {
		end = int64(len(items))
	}
	return items[start:end]
}

func firstOr(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	if strings.TrimSpace(values[0]) == "" {
		return fallback
	}
	return values[0]
}

func errorReply(cmd mu.CommandEnvelope, code string, message string) mu.ReplyEnvelope {
	return mu.ReplyEnvelope{
		ID:   cmd.ID,
		Type: "error",
		OK:   false,
		TS:   time.Now().Unix(),
		Err:  &mu.ReplyError{Code: code, Message: message},
	}
}

// coverArtNames lists common cover art filenames in priority order.
var coverArtNames = []string{
	"cover.jpg", "cover.jpeg", "cover.png",
	"folder.jpg", "folder.jpeg", "folder.png",
	"album.jpg", "album.jpeg", "album.png",
	"front.jpg", "front.jpeg", "front.png",
	"albumart.jpg", "albumart.jpeg", "albumart.png",
}

// findCoverArt looks for a cover art file in the given directory.
// Returns the full path if found, empty string otherwise.
func findCoverArt(dir string) string {
	for _, name := range coverArtNames {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
		// Try case-insensitive match
		upper := filepath.Join(dir, strings.ToUpper(name))
		if _, err := os.Stat(upper); err == nil {
			return upper
		}
	}
	return ""
}

// getEmbeddedArtExt checks if an audio file has embedded album art and returns the file extension.
// Returns empty string if no embedded art is found.
func getEmbeddedArtExt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	metadata, err := tag.ReadFrom(f)
	if err != nil {
		return ""
	}
	pic := metadata.Picture()
	if pic == nil {
		return ""
	}
	return mimeToExt(pic.MIMEType)
}

// mimeToExt converts a MIME type to a file extension.
func mimeToExt(mime string) string {
	switch strings.ToLower(mime) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg" // Default to jpg for unknown types
	}
}

// extractEmbeddedArt extracts album art from an audio file.
// Returns the image data, MIME type, and any error.
func extractEmbeddedArt(path string) ([]byte, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	metadata, err := tag.ReadFrom(f)
	if err != nil {
		return nil, "", err
	}

	pic := metadata.Picture()
	if pic == nil {
		return nil, "", errors.New("no embedded art")
	}

	mime := pic.MIMEType
	if mime == "" {
		// Guess from extension field or default to JPEG
		mime = "image/jpeg"
	}

	return pic.Data, mime, nil
}
