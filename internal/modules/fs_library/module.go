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

When referenced externally, items are addressed by a structured
LibraryItemRef{kind:"libraryItem", libraryId, itemId} (see pkg/mu).

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
	              biography (profile), group members, and instruments
	              (extracted from tracklist credit roles).
	Wikipedia     Album and artist summaries fetched via Wikipedia REST API
	              using URLs discovered in MusicBrainz url-rels. Artist
	              Wikipedia is used as a biography fallback when Discogs
	              profile is empty.
	Ollama        When embedding_provider is "ollama" (or summary_model is
	              set), an LLM-generated album summary is produced during
	              enrichment using the configured model (default: gemma3:12b).
	              The summary replaces raw Wikipedia text for the summary
	              embedding vector, improving semantic search quality.

Enrichment data is stored as a JSON sidecar file named .mu_album_metadata.json
in each album's directory. The v3 sidecar schema includes:

	{
	  "version": 3,
	  "fetched_at": "2026-02-07T12:00:00Z",
	  "artist": "Pink Floyd",
	  "album": "The Dark Side of the Moon",
	  "musicbrainz": { "genres": [...], "tags": [...], "year": 1973,
	                    "annotation": "...", "wikipedia_url": "...",
	                    "artist_ids": [...] },
	  "discogs": { "styles": [...], "credits": [...], "notes": "...",
	               "release_notes": "...", "release_credits": [...],
	               "instruments": ["guitar", "drums", "bass", ...] },
	  "artist_info": { "name": "...", "type": "Group", "origin": "...",
	                    "biography": "...", "members": [...], ... },
	  "description": { "mb_annotation": "...", "wikipedia_summary": "...",
	                    "generated_summary": "..." }
	}

Existing v1/v2 sidecars are automatically re-enriched to v3 on the next scan via
the version check in sidecarNeedsRefresh. Artist data is cached by MB/Discogs
artist ID within each enrichment run, so artists with multiple albums are only
fetched once.

The enrichment data feeds into both keyword search and semantic search:

  - Keyword search matches against genres, tags, styles, labels, artist
    name, origin, type, and group members.
  - Semantic search embeddings include genre, tag, year, label, style,
    instruments, personnel names, artist info (type, origin, biography,
    members), album description (LLM-generated summary preferred, with
    Wikipedia summary fallback), and release credits, enabling queries
    like "progressive rock 1970s", "British group", "tenor sax piano",
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
	summary_model = "gemma3:12b"          # Ollama model for LLM album summaries (default: gemma3:12b)
	summary_endpoint = ""                 # Ollama API URL for summaries (default: embedding_endpoint)

# Commands

The module responds to these command types:

	library.browse              Browse containers, returns items with pagination
	library.search              Full-text or semantic search
	library.getItem             Get catalog metadata for one item
	library.getItems            Batch metadata lookup
	library.resolveSources      Get playable sources for one item
	library.resolveSourcesBatch Batch source resolution
	library.rescan              Trigger manual rescan of filesystem
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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dhowden/tag"
	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/mikey-austin/media_utopia/internal/adapters/chromaprint"
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

	// HTTPBaseURL overrides the base URL advertised in resolve/browse responses.
	// When empty, the base URL is derived from the listener address.
	// Set this when the server is behind a proxy or in a container binding to
	// 0.0.0.0 but reachable externally (e.g., "http://bluenote.lan:8484").
	HTTPBaseURL string

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

	// ScanMode controls automatic scanning:
	//   "auto"   - Initial scan at startup, then periodic rescans every
	//              ScanIntervalMS (default).
	//   "manual" - No initial scan and no periodic ticker. The persisted
	//              index loads as normal, and the user must invoke
	//              library.rescan to scan.
	ScanMode string

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

	// EmbeddingBatchSize is the number of items per embedding request.
	// Lower values reduce memory usage and avoid timeouts on slow hardware.
	// Default: 32.
	EmbeddingBatchSize int

	// EnrichEnabled enables automatic album metadata enrichment during rescan.
	EnrichEnabled bool

	// DiscogsToken is an optional Discogs personal access token for higher rate limits.
	DiscogsToken string

	// AcoustIDAPIKey is the AcoustID application API key for fingerprint lookups.
	// Free registration at https://acoustid.org/. When set (and chromaprint build
	// tag enabled), enables fingerprint fallback for albums where MusicBrainz
	// text search returns no hits.
	AcoustIDAPIKey string

	// SummaryModel is the Ollama model for generating album summaries (default: "gemma3:12b").
	SummaryModel string

	// SummaryEndpoint is the Ollama API URL for summary generation.
	// Defaults to EmbeddingEndpoint if empty.
	SummaryEndpoint string

	// GenreModel is the Ollama model used for top-level genre classification.
	// Defaults to SummaryModel when empty.
	GenreModel string
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
	dedup    *mu.CommandDedup

	mu      sync.RWMutex
	scanMu        sync.Mutex // serializes concurrent scans
	scanCount     atomic.Int64 // incremented on every scanInner call; for tests
	index         *libraryIndex
	baseURL       string
	artURLPrefix  string            // pre-computed baseURL + "/art/", set once in startHTTPServer
	artURLCache   map[string]string // cached art URLs keyed by album hash, cleared on index rebuild
	defaultArtURL string            // cached default art URL, set once in startHTTPServer
	server        *http.Server
	ln            net.Listener

	// Embedding support
	embedProvider EmbeddingProvider
	embedCache    *EmbeddingCache
	vectorIndex   *VectorIndex

	// Deduplication
	dupeIndex *DuplicateIndex

	// Enrichment metadata keyed by "artist|album"
	enrichMeta map[string]*AlbumMetadata

	// Summary generator (Ollama)
	summaryGen *OllamaGenerator

	// Genre classifier (Ollama-backed). May be nil; in that case the
	// rollup-map fallback in buildGenreIndex still runs.
	genreClassifier GenreClassifier

	// Embedded art cache keyed by file path
	artCache   map[string]*artCacheEntry
	artCacheMu sync.RWMutex

	// Previous scan's items for incremental scanning (skip unchanged files)
	prevItems map[string]mediaItem // keyed by file path
}

// artCacheEntry holds cached embedded album art data.
type artCacheEntry struct {
	data       []byte
	mime       string
	lastAccess atomic.Int64 // UnixNano, for LRU eviction
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

	// GenreAlbums maps genre name → album refs for genre browsing.
	GenreAlbums map[string][]genreAlbumRef `json:"-"`

	// ArtistLetters maps uppercase letter (or "#") → sorted artist names.
	ArtistLetters map[string][]string `json:"-"`

	// RecentAudioAlbums holds up to 50 recently added albums sorted by newest first.
	RecentAudioAlbums []recentAlbumRef `json:"-"`

	// RecentVideos holds up to 50 recently added video IDs sorted by newest first.
	RecentVideos []string `json:"-"`

	// AudioFolderTree maps directory path → folder node for audio filesystem browsing.
	AudioFolderTree map[string]*folderNode `json:"-"`

	// VideoFolderTree maps directory path → folder node for video filesystem browsing.
	VideoFolderTree map[string]*folderNode `json:"-"`
}

// containerInfo stores the data needed to resolve a container by its hash ID.
// The Artist field is overloaded to store the type-specific key:
//   - "artist"/"album": artist name
//   - "genre": genre name
//   - "letter": letter ("A"-"Z" or "#")
//   - "audiodir"/"videodir": directory path
type containerInfo struct {
	Type   string `json:"type"`   // "artist", "album", "genre", "letter", "audiodir", "videodir"
	Artist string `json:"artist"` // Artist name (or genre/letter/dir path depending on Type)
	Album  string `json:"album"`  // Album name (only for album type)
}

// genreAlbumRef references an album within a genre index.
type genreAlbumRef struct {
	Artist string
	Album  string
}

// recentAlbumRef references a recently added album with its newest track mtime.
type recentAlbumRef struct {
	Artist   string
	Album    string
	MaxMtime time.Time
}

// folderNode represents a directory in the folder browse tree.
type folderNode struct {
	Path    string
	Name    string
	SubDirs []string // sorted child directory paths
	Items   []string // sorted item IDs in this directory
	// CoverAlbumHash is the album hash whose cover art represents this
	// folder — picked at scan time by walking the subtree for the first
	// audio album with real cover art. Populated by populateFolderCovers
	// and persisted to the sidecar so browse stays O(1) after restart.
	CoverAlbumHash string `json:"coverAlbumHash,omitempty"`
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

	// Composer is the file's Composer tag (TCOM/COMPOSER), if present.
	// Used to surface classical-music albums in search by composer name even
	// when the album-level artist is the performer.
	Composer string `json:"composer,omitempty"`

	// EmbeddedGenre is the raw genre value from the file's tags. Used as
	// input to the LLM genre classifier and as the fallback signal when no
	// LLM classification has been cached.
	EmbeddedGenre string `json:"embeddedGenre,omitempty"`

	// MediaType is "Audio" or "Video".
	MediaType string `json:"mediaType"`

	// DurationMS is the track duration in milliseconds (0 if unknown).
	DurationMS int64 `json:"durationMs,omitempty"`

	// Mtime is the file modification time, used for "recently added" sorting.
	Mtime time.Time `json:"-"`

	// SearchText is the precomputed lowercased search text for keyword matching.
	// Built at scan time to avoid per-query recomputation.
	SearchText string `json:"-"`

	// FileHash is a cached partial file hash (first/last 64KB) for deduplication.
	// Carried forward across incremental scans to avoid re-opening unchanged files.
	FileHash string `json:"-"`

	// EmbeddedArtExt is the extension for embedded art (e.g. ".jpg"), empty if none.
	// Detected during tag reading to avoid re-opening the file.
	EmbeddedArtExt string `json:"-"`
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
				Endpoint:  cfg.EmbeddingEndpoint,
				Model:     cfg.EmbeddingModel,
				BatchSize: cfg.EmbeddingBatchSize,
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

	// Initialize summary generator if configured
	var summaryGen *OllamaGenerator
	if cfg.SummaryModel != "" || cfg.EmbeddingProvider == "ollama" {
		model := cfg.SummaryModel
		if model == "" {
			model = "gemma3:12b"
		}
		endpoint := cfg.SummaryEndpoint
		if endpoint == "" {
			endpoint = cfg.EmbeddingEndpoint
		}
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		summaryGen = NewOllamaGenerator(endpoint, model)
		log.Info("summary generator initialized",
			zap.String("model", model),
			zap.String("endpoint", endpoint))
	} else {
		log.Debug("summary generator not configured",
			zap.String("summary_model", cfg.SummaryModel),
			zap.String("embedding_provider", cfg.EmbeddingProvider))
	}

	if cfg.AcoustIDAPIKey != "" && !chromaprint.Enabled {
		log.Warn("acoustid_api_key is set but chromaprint build tag is not enabled; fingerprint fallback will be inactive")
	}

	// Initialize genre classifier if a generator is available. GenreModel
	// falls back to SummaryModel; SummaryEndpoint to EmbeddingEndpoint. The
	// classifier is wholly optional — when nil, buildGenreIndex's rollup-map
	// fallback still runs.
	var genreClassifier GenreClassifier
	{
		genreModel := cfg.GenreModel
		if genreModel == "" {
			genreModel = cfg.SummaryModel
		}
		if genreModel != "" {
			endpoint := cfg.SummaryEndpoint
			if endpoint == "" {
				endpoint = cfg.EmbeddingEndpoint
			}
			if endpoint != "" {
				gen := NewOllamaGenerator(endpoint, genreModel)
				genreClassifier = NewOllamaGenreClassifier(gen)
				log.Info("genre classifier initialized",
					zap.String("model", genreModel),
					zap.String("endpoint", endpoint))
			}
		}
	}

	return &Module{
		log:             log,
		client:          client,
		config:          cfg,
		cmdTopic:        cmdTopic,
		cmdQueue:        make(chan cmdWork, 64),
		dedup:           mu.NewCommandDedup(128),
		index: &libraryIndex{
			Items: map[string]mediaItem{}, Audio: map[string]artistEntry{}, Containers: map[string]containerInfo{},
			GenreAlbums: map[string][]genreAlbumRef{}, ArtistLetters: map[string][]string{},
			AudioFolderTree: map[string]*folderNode{}, VideoFolderTree: map[string]*folderNode{},
		},
		embedProvider:   embedProvider,
		embedCache:      embedCache,
		vectorIndex:     NewVectorIndex(),
		dupeIndex:       NewDuplicateIndex(),
		enrichMeta:      make(map[string]*AlbumMetadata),
		summaryGen:      summaryGen,
		genreClassifier: genreClassifier,
		artCache:        make(map[string]*artCacheEntry),
	}, nil
}

// Run starts the module.
func (m *Module) Run(ctx context.Context) error {
	if m.client != nil {
		if err := m.publishPresence(); err != nil {
			return err
		}
	}
	if err := m.startHTTPServer(); err != nil {
		return err
	}
	if err := m.loadIndex(); err != nil {
		m.log.Debug("index load failed", zap.Error(err))
	}
	if m.config.ScanMode != "manual" {
		if err := m.scanCtx(ctx); err != nil && ctx.Err() == nil {
			m.log.Warn("initial scan failed", zap.Error(err))
		}
	} else {
		m.log.Info("scan_mode=manual: skipping initial scan")
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

	if m.client != nil {
		handler := func(_ paho.Client, msg paho.Message) {
			m.handleMessage(msg)
		}
		if err := m.client.Subscribe(m.cmdTopic, 1, handler); err != nil {
			return err
		}
		defer m.client.Unsubscribe(m.cmdTopic)
	}

	var tickerC <-chan time.Time
	if m.config.ScanMode != "manual" {
		scanInterval := time.Duration(m.config.ScanIntervalMS) * time.Millisecond
		ticker := time.NewTicker(scanInterval)
		defer ticker.Stop()
		tickerC = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			m.shutdownHTTPServer()
			wg.Wait()
			return nil
		case <-tickerC:
			if err := m.scanCtx(ctx); err != nil && ctx.Err() == nil {
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
	if mu.ShouldDedup(cmd.Type) && m.dedup.Seen(cmd.ID) {
		m.log.Debug("duplicate command skipped", zap.String("id", cmd.ID), zap.String("type", cmd.Type))
		reply := mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: time.Now().Unix()}
		payload, _ := json.Marshal(reply)
		_ = m.client.Publish(cmd.ReplyTo, 0, false, payload)
		return
	}

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
	case "library.getItem":
		return m.libraryGetItem(cmd, reply)
	case "library.getItems":
		return m.libraryGetItems(cmd, reply)
	case "library.resolveSources":
		return m.libraryResolveSources(cmd, reply)
	case "library.resolveSourcesBatch":
		return m.libraryResolveSourcesBatch(cmd, reply)
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

// describeItem returns Display + Attributes for an item id. The bool indicates
// whether the id refers to anything known (container or media item).
func (m *Module) describeItem(itemID string) (*mu.DisplayMetadata, map[string]any, bool) {
	if meta, ok := m.resolveContainerMetadata(itemID); ok {
		display := &mu.DisplayMetadata{}
		if v, _ := meta["title"].(string); v != "" {
			display.Title = v
		}
		if artists, ok := meta["artists"].([]string); ok && len(artists) > 0 {
			display.Artists = artists
			display.Artist = strings.Join(artists, ", ")
		}
		attrs := map[string]any{"container": true}
		if t, _ := meta["type"].(string); t != "" {
			attrs["type"] = t
		}
		return display, attrs, true
	}
	item, ok := m.getItem(itemID)
	if !ok {
		return nil, nil, false
	}
	display := &mu.DisplayMetadata{
		Title:      item.Title,
		Album:      item.Album,
		DurationMS: item.DurationMS,
		MediaType:  item.MediaType,
	}
	if len(item.Artists) > 0 {
		display.Artists = item.Artists
		display.Artist = strings.Join(item.Artists, ", ")
	}
	if artURL := m.getItemArtworkURL(item); artURL != "" {
		display.ArtworkURL = artURL
	}
	attrs := map[string]any{"container": false}
	if item.MediaType != "" {
		attrs["type"] = item.MediaType
	}
	return display, attrs, true
}

func (m *Module) refMatchesNode(ref mu.LibraryItemRef) bool {
	return ref.LibraryID == m.config.NodeID
}

func (m *Module) libraryGetItem(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryGetItemBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if err := body.Ref.Validate(); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	if !m.refMatchesNode(body.Ref) {
		return errorReply(cmd, "INVALID", "ref.libraryId does not match this library node")
	}
	display, attrs, ok := m.describeItem(body.Ref.ItemID)
	if !ok {
		return errorReply(cmd, "NOT_FOUND", "item not found")
	}
	payload, _ := json.Marshal(mu.LibraryGetItemReply{
		Ref:        body.Ref,
		Display:    display,
		Attributes: attrs,
	})
	reply.Body = payload
	return reply
}

func (m *Module) libraryGetItems(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryGetItemsBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	items := make([]mu.LibraryGetItemsReplyItem, 0, len(body.Refs))
	for _, ref := range body.Refs {
		if err := ref.Validate(); err != nil {
			items = append(items, mu.LibraryGetItemsReplyItem{
				Ref: ref,
				Err: &mu.ReplyError{Code: "INVALID", Message: err.Error()},
			})
			continue
		}
		if !m.refMatchesNode(ref) {
			items = append(items, mu.LibraryGetItemsReplyItem{
				Ref: ref,
				Err: &mu.ReplyError{Code: "INVALID", Message: "ref.libraryId does not match this library node"},
			})
			continue
		}
		display, attrs, ok := m.describeItem(ref.ItemID)
		if !ok {
			items = append(items, mu.LibraryGetItemsReplyItem{
				Ref: ref,
				Err: &mu.ReplyError{Code: "NOT_FOUND", Message: "item not found"},
			})
			continue
		}
		items = append(items, mu.LibraryGetItemsReplyItem{
			Ref:        ref,
			Display:    display,
			Attributes: attrs,
		})
	}
	payload, _ := json.Marshal(mu.LibraryGetItemsReply{Items: items})
	reply.Body = payload
	return reply
}

func (m *Module) libraryResolveSources(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryResolveSourcesBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if err := body.Ref.Validate(); err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	if !m.refMatchesNode(body.Ref) {
		return errorReply(cmd, "INVALID", "ref.libraryId does not match this library node")
	}
	if _, ok := m.getItem(body.Ref.ItemID); !ok {
		return errorReply(cmd, "NOT_FOUND", "item not found")
	}
	sourceURL, err := m.sourceURL(body.Ref.ItemID)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(mu.LibraryResolveSourcesReply{
		Ref:     body.Ref,
		Sources: []mu.ResolvedSource{{URL: sourceURL, ByteRange: true}},
	})
	reply.Body = payload
	return reply
}

func (m *Module) libraryResolveSourcesBatch(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryResolveSourcesBatchBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	items := make([]mu.LibraryResolveSourcesBatchItem, 0, len(body.Refs))
	for _, ref := range body.Refs {
		if err := ref.Validate(); err != nil {
			items = append(items, mu.LibraryResolveSourcesBatchItem{
				Ref: ref,
				Err: &mu.ReplyError{Code: "INVALID", Message: err.Error()},
			})
			continue
		}
		if !m.refMatchesNode(ref) {
			items = append(items, mu.LibraryResolveSourcesBatchItem{
				Ref: ref,
				Err: &mu.ReplyError{Code: "INVALID", Message: "ref.libraryId does not match this library node"},
			})
			continue
		}
		if _, ok := m.getItem(ref.ItemID); !ok {
			items = append(items, mu.LibraryResolveSourcesBatchItem{
				Ref: ref,
				Err: &mu.ReplyError{Code: "NOT_FOUND", Message: "item not found"},
			})
			continue
		}
		sourceURL, err := m.sourceURL(ref.ItemID)
		if err != nil {
			items = append(items, mu.LibraryResolveSourcesBatchItem{
				Ref: ref,
				Err: &mu.ReplyError{Code: "INVALID", Message: err.Error()},
			})
			continue
		}
		items = append(items, mu.LibraryResolveSourcesBatchItem{
			Ref:     ref,
			Sources: []mu.ResolvedSource{{URL: sourceURL, ByteRange: true}},
		})
	}
	payload, _ := json.Marshal(mu.LibraryResolveSourcesBatchReply{Items: items})
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
		Force bool `json:"force"`
	}
	if len(cmd.Body) > 0 {
		_ = json.Unmarshal(cmd.Body, &body)
	}

	if body.Async {
		// Run scan asynchronously using context-aware variants
		go func() {
			ctx := context.Background()
			var err error
			if body.Force {
				err = m.scanForceEnrichCtx(ctx)
			} else {
				err = m.scanCtx(ctx)
			}
			if err != nil {
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

	scanFn := m.scan
	if body.Force {
		scanFn = m.scanForceEnrich
	}
	if err := scanFn(); err != nil {
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
	switch containerID {
	case "":
		return m.browseRoot(start, count)
	case "container:audio":
		return m.browseAudioRoot(start, count)
	case "container:video":
		return m.browseVideoRoot(start, count)
	case "container:audio:bygenre":
		return m.browseGenreList(start, count)
	case "container:audio:byartist":
		return m.browseLetterList(start, count)
	case "container:audio:recent":
		return m.browseRecentAudio(start, count)
	case "container:audio:byfolder":
		return m.browseFolderRoots(start, count, "audiodir")
	case "container:video:recent":
		return m.browseRecentVideo(start, count)
	case "container:video:byfolder":
		return m.browseFolderRoots(start, count, "videodir")
	}

	// Look up hashed container, dispatch on type
	m.mu.RLock()
	info, ok := m.index.Containers[containerID]
	m.mu.RUnlock()
	if !ok {
		return nil, 0, errors.New("container not found")
	}

	switch info.Type {
	case "artist":
		return m.browseArtist(containerID, info, start, count)
	case "album":
		return m.browseAlbum(containerID, info, start, count)
	case "genre":
		return m.browseGenreAlbums(info, start, count)
	case "letter":
		return m.browseLetterArtists(info, start, count)
	case "audiodir", "videodir":
		return m.browseFolderNode(info, start, count)
	}

	return nil, 0, errors.New("unsupported container")
}

func (m *Module) browseRoot(start, count int64) ([]libraryItem, int64, error) {
	m.mu.RLock()
	defaultImg := m.defaultArtURLUnlocked()
	m.mu.RUnlock()
	items := []libraryItem{
		{ItemID: "container:audio", Name: "Audio", Type: "Folder", ImageURL: defaultImg},
		{ItemID: "container:video", Name: "Video", Type: "Folder", ImageURL: defaultImg},
	}
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseAudioRoot(start, count int64) ([]libraryItem, int64, error) {
	m.mu.RLock()
	defaultImg := m.defaultArtURLUnlocked()
	m.mu.RUnlock()
	items := []libraryItem{
		{ItemID: "container:audio:bygenre", Name: "By Genre", Type: "Folder", ImageURL: defaultImg},
		{ItemID: "container:audio:byartist", Name: "By Artist", Type: "Folder", ImageURL: defaultImg},
		{ItemID: "container:audio:recent", Name: "Recently Added", Type: "Folder", ImageURL: defaultImg},
		{ItemID: "container:audio:byfolder", Name: "By Folder", Type: "Folder", ImageURL: defaultImg},
	}
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseVideoRoot(start, count int64) ([]libraryItem, int64, error) {
	m.mu.RLock()
	defaultImg := m.defaultArtURLUnlocked()
	m.mu.RUnlock()
	items := []libraryItem{
		{ItemID: "container:video:recent", Name: "Recently Added", Type: "Folder", ImageURL: defaultImg},
		{ItemID: "container:video:byfolder", Name: "By Folder", Type: "Folder", ImageURL: defaultImg},
	}
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseGenreList(start, count int64) ([]libraryItem, int64, error) {
	m.mu.RLock()
	genres := make([]string, 0, len(m.index.GenreAlbums))
	for genre := range m.index.GenreAlbums {
		genres = append(genres, genre)
	}
	defaultImg := m.defaultArtURLUnlocked()
	m.mu.RUnlock()

	sort.Strings(genres)
	items := make([]libraryItem, 0, len(genres))
	for _, genre := range genres {
		items = append(items, libraryItem{
			ItemID:      containerHash("genre", genre, ""),
			Name:        genre,
			Type:        "Folder",
			ContainerID: "container:audio:bygenre",
			ImageURL:    defaultImg,
		})
	}
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseGenreAlbums(info containerInfo, start, count int64) ([]libraryItem, int64, error) {
	genreName := info.Artist
	m.mu.RLock()
	albums, ok := m.index.GenreAlbums[genreName]
	if !ok {
		m.mu.RUnlock()
		return nil, 0, nil
	}
	// Copy and collect art URLs under lock
	type albumRef struct {
		artist   string
		album    string
		imageURL string
	}
	type albumRefWithHash struct {
		artist   string
		album    string
		hash     string
		imageURL string
	}
	refsH := make([]albumRefWithHash, 0, len(albums))
	genreHash := containerHash("genre", genreName, "")
	for _, a := range albums {
		albumHash := containerHash("album", a.Artist, a.Album)
		refsH = append(refsH, albumRefWithHash{
			artist:   a.Artist,
			album:    a.Album,
			hash:     albumHash,
			imageURL: m.artURLUnlocked(albumHash),
		})
	}
	m.mu.RUnlock()

	items := make([]libraryItem, 0, len(refsH))
	for _, r := range refsH {
		items = append(items, libraryItem{
			ItemID:      r.hash,
			Name:        r.artist + " - " + r.album,
			Type:        "Folder",
			ContainerID: genreHash,
			ImageURL:    r.imageURL,
		})
	}
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseLetterList(start, count int64) ([]libraryItem, int64, error) {
	m.mu.RLock()
	letters := make([]string, 0, len(m.index.ArtistLetters))
	for letter := range m.index.ArtistLetters {
		letters = append(letters, letter)
	}
	defaultImg := m.defaultArtURLUnlocked()
	m.mu.RUnlock()

	// Sort A-Z, then # at end
	sort.Slice(letters, func(i, j int) bool {
		if letters[i] == "#" {
			return false
		}
		if letters[j] == "#" {
			return true
		}
		return letters[i] < letters[j]
	})
	items := make([]libraryItem, 0, len(letters))
	for _, letter := range letters {
		items = append(items, libraryItem{
			ItemID:      containerHash("letter", letter, ""),
			Name:        letter,
			Type:        "Folder",
			ContainerID: "container:audio:byartist",
			ImageURL:    defaultImg,
		})
	}
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseLetterArtists(info containerInfo, start, count int64) ([]libraryItem, int64, error) {
	letter := info.Artist
	m.mu.RLock()
	artists, ok := m.index.ArtistLetters[letter]
	if !ok {
		m.mu.RUnlock()
		return nil, 0, nil
	}
	// Copy under lock
	artistsCopy := make([]string, len(artists))
	copy(artistsCopy, artists)
	defaultImg := m.defaultArtURLUnlocked()
	m.mu.RUnlock()

	letterHash := containerHash("letter", letter, "")
	items := make([]libraryItem, 0, len(artistsCopy))
	for _, artistName := range artistsCopy {
		items = append(items, libraryItem{
			ItemID:      containerHash("artist", artistName, ""),
			Name:        artistName,
			Type:        "Folder",
			ContainerID: letterHash,
			ImageURL:    defaultImg,
		})
	}
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseRecentAudio(start, count int64) ([]libraryItem, int64, error) {
	m.mu.RLock()
	recent := m.index.RecentAudioAlbums
	type albumRef struct {
		artist   string
		album    string
		hash     string
		imageURL string
	}
	refs := make([]albumRef, 0, len(recent))
	for _, r := range recent {
		albumHash := containerHash("album", r.Artist, r.Album)
		refs = append(refs, albumRef{
			artist:   r.Artist,
			album:    r.Album,
			hash:     albumHash,
			imageURL: m.artURLUnlocked(albumHash),
		})
	}
	m.mu.RUnlock()

	items := make([]libraryItem, 0, len(refs))
	for _, r := range refs {
		items = append(items, libraryItem{
			ItemID:      r.hash,
			Name:        r.artist + " - " + r.album,
			Type:        "Folder",
			ContainerID: "container:audio:recent",
			ImageURL:    r.imageURL,
		})
	}
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseRecentVideo(start, count int64) ([]libraryItem, int64, error) {
	m.mu.RLock()
	recentIDs := make([]string, len(m.index.RecentVideos))
	copy(recentIDs, m.index.RecentVideos)
	defaultImg := m.defaultArtURLUnlocked()
	items := make([]libraryItem, 0, len(recentIDs))
	for _, vid := range recentIDs {
		item, ok := m.index.Items[vid]
		if !ok {
			continue
		}
		items = append(items, libraryItem{
			ItemID:      item.ID,
			Name:        item.Name,
			Type:        item.MediaType,
			MediaType:   item.MediaType,
			DurationMS:  item.DurationMS,
			ContainerID: "container:video:recent",
			ImageURL:    defaultImg,
		})
	}
	m.mu.RUnlock()
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseFolderRoots(start, count int64, containerType string) ([]libraryItem, int64, error) {
	m.mu.RLock()
	var tree map[string]*folderNode
	if containerType == "audiodir" {
		tree = m.index.AudioFolderTree
	} else {
		tree = m.index.VideoFolderTree
	}
	// Find root-level dirs (dirs whose parent is not in the tree)
	var roots []string
	for dirPath := range tree {
		parent := filepath.Dir(dirPath)
		if _, ok := tree[parent]; !ok {
			roots = append(roots, dirPath)
		}
	}
	defaultImg := m.defaultArtURLUnlocked()
	m.mu.RUnlock()

	sort.Strings(roots)
	parentID := "container:audio:byfolder"
	if containerType == "videodir" {
		parentID = "container:video:byfolder"
	}
	items := make([]libraryItem, 0, len(roots))
	for _, dirPath := range roots {
		items = append(items, libraryItem{
			ItemID:      containerHash(containerType, dirPath, ""),
			Name:        filepath.Base(dirPath),
			Type:        "Folder",
			ContainerID: parentID,
			ImageURL:    defaultImg,
		})
	}
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseFolderNode(info containerInfo, start, count int64) ([]libraryItem, int64, error) {
	dirPath := info.Artist
	m.mu.RLock()
	var tree map[string]*folderNode
	if info.Type == "audiodir" {
		tree = m.index.AudioFolderTree
	} else {
		tree = m.index.VideoFolderTree
	}
	node, ok := tree[dirPath]
	if !ok {
		m.mu.RUnlock()
		return nil, 0, nil
	}
	// Copy data under lock
	subDirs := make([]string, len(node.SubDirs))
	copy(subDirs, node.SubDirs)
	itemIDs := make([]string, len(node.Items))
	copy(itemIDs, node.Items)
	defaultImg := m.defaultArtURLUnlocked()

	// Collect items + their resolved artwork under lock. Subfolders get a
	// discovered URL by walking their subtree for the first album with cover
	// art; media items get their album's art directly. Both fall back to the
	// default placeholder if nothing real is available.
	type itemData struct {
		id        string
		name      string
		mediaType string
		duration  int64
		imageURL  string
	}
	var mediaItems []itemData
	for _, id := range itemIDs {
		if item, found := m.index.Items[id]; found {
			imgURL := m.itemArtworkURLUnlocked(item)
			if imgURL == "" {
				imgURL = defaultImg
			}
			mediaItems = append(mediaItems, itemData{
				id: item.ID, name: item.Name, mediaType: item.MediaType,
				duration: item.DurationMS, imageURL: imgURL,
			})
		}
	}
	type subDirData struct {
		path     string
		imageURL string
	}
	subDirImages := make([]subDirData, len(subDirs))
	for i, sd := range subDirs {
		img := defaultImg
		if child, ok := tree[sd]; ok && child.CoverAlbumHash != "" {
			if u := m.artURLUnlocked(child.CoverAlbumHash); u != "" {
				img = u
			}
		}
		subDirImages[i] = subDirData{path: sd, imageURL: img}
	}
	m.mu.RUnlock()

	containerID := containerHash(info.Type, dirPath, "")
	items := make([]libraryItem, 0, len(subDirImages)+len(mediaItems))
	for _, sd := range subDirImages {
		items = append(items, libraryItem{
			ItemID:      containerHash(info.Type, sd.path, ""),
			Name:        filepath.Base(sd.path),
			Type:        "Folder",
			ContainerID: containerID,
			ImageURL:    sd.imageURL,
		})
	}
	for _, mi := range mediaItems {
		items = append(items, libraryItem{
			ItemID:      mi.id,
			Name:        mi.name,
			Type:        mi.mediaType,
			MediaType:   mi.mediaType,
			DurationMS:  mi.duration,
			ContainerID: containerID,
			ImageURL:    mi.imageURL,
		})
	}
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseArtist(containerID string, info containerInfo, start, count int64) ([]libraryItem, int64, error) {
	m.mu.RLock()
	artist, ok := m.index.Audio[info.Artist]
	if !ok {
		m.mu.RUnlock()
		return nil, 0, errors.New("artist not found")
	}
	type albumItem struct {
		name     string
		hash     string
		imageURL string
	}
	albumItems := make([]albumItem, 0, len(artist.Albums))
	for albumName := range artist.Albums {
		albumHash := containerHash("album", info.Artist, albumName)
		albumItems = append(albumItems, albumItem{
			name:     albumName,
			hash:     albumHash,
			imageURL: m.artURLUnlocked(albumHash),
		})
	}
	m.mu.RUnlock()

	sort.Slice(albumItems, func(i, j int) bool {
		return albumItems[i].name < albumItems[j].name
	})
	items := make([]libraryItem, 0, len(albumItems))
	for _, ai := range albumItems {
		items = append(items, libraryItem{
			ItemID:      ai.hash,
			Name:        ai.name,
			Type:        "Folder",
			ContainerID: containerID,
			ImageURL:    ai.imageURL,
		})
	}
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) browseAlbum(containerID string, info containerInfo, start, count int64) ([]libraryItem, int64, error) {
	m.mu.RLock()
	artist, ok := m.index.Audio[info.Artist]
	if !ok {
		m.mu.RUnlock()
		return nil, 0, errors.New("artist not found")
	}
	album, ok := artist.Albums[info.Album]
	if !ok {
		m.mu.RUnlock()
		return nil, 0, errors.New("album not found")
	}
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
	m.mu.RUnlock()
	return paginate(items, start, count), int64(len(items)), nil
}

func (m *Module) search(query string, start int64, count int64) ([]libraryItem, int64) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, 0
	}

	start0 := time.Now()
	var total int64
	defer func() {
		elapsed := time.Since(start0)
		if elapsed > 100*time.Millisecond {
			m.log.Warn("slow search",
				zap.String("query", query),
				zap.Duration("elapsed", elapsed),
				zap.Int64("results", total))
		}
	}()

	// Try semantic search first if embeddings are available
	if m.embedProvider != nil && m.vectorIndex != nil {
		semanticResults, semanticTotal := m.semanticSearch(query, start, count)
		if len(semanticResults) > 0 {
			total = semanticTotal
			return semanticResults, semanticTotal
		}
	}

	// Fall back to keyword search
	terms := strings.Fields(strings.ToLower(query))
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Cap the number of collected matches to avoid unbounded allocation and
	// sorting costs.  This should be well above any practical pagination window.
	const maxSearchResults = 1000 // Increase from 500 to accommodate larger libraries

	type scoredItem struct {
		result libraryItem
		score  int
	}

	scored := make([]scoredItem, 0)
	queryLower := strings.ToLower(query)
	for _, item := range m.index.Items {
		if !containsAllTerms(item, terms) {
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
		result := libraryItem{
			ItemID:     item.ID,
			Name:       item.Name,
			Type:       item.MediaType,
			MediaType:  item.MediaType,
			Artists:    item.Artists,
			Album:      item.Album,
			DurationMS: item.DurationMS,
			ImageURL:   artURL,
		}

		s := 0
		nameLower := strings.ToLower(item.Name)

		if nameLower == queryLower {
			s += 100
		} else if strings.HasPrefix(nameLower, queryLower) {
			s += 80
		}

		allInName := true
		for _, term := range terms {
			if !strings.Contains(nameLower, term) {
				allInName = false
				break
			}
		}
		if allInName {
			s += 50
		}

		for _, artist := range item.Artists {
			artistLower := strings.ToLower(artist)
			for _, term := range terms {
				if strings.Contains(artistLower, term) {
					s += 20
					break
				}
			}
		}

		if item.Album != "" {
			albumLower := strings.ToLower(item.Album)
			for _, term := range terms {
				if strings.Contains(albumLower, term) {
					s += 10
					break
				}
			}
		}

		// LLMGenre boost: query terms hitting the album's classified genre
		// rank higher than incidental matches in liner notes / tags.
		if item.MediaType == "Audio" {
			artistName := firstOr(item.Artists, "")
			if enrich := m.enrichMeta[artistName+"|"+item.Album]; enrich != nil && enrich.LLMGenre != "" {
				lg := strings.ToLower(enrich.LLMGenre)
				for _, term := range terms {
					if strings.Contains(lg, term) {
						s += 30
						break
					}
				}
			}
		}

		// Composer boost: matches the per-track composer field directly.
		if item.Composer != "" {
			cl := strings.ToLower(item.Composer)
			for _, term := range terms {
				if strings.Contains(cl, term) {
					s += 25
					break
				}
			}
		}

		if len(nameLower) < 30 {
			s += 5
		}

		scored = append(scored, scoredItem{result: result, score: s})
		if len(scored) >= maxSearchResults {
			break
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return strings.ToLower(scored[i].result.Name) < strings.ToLower(scored[j].result.Name)
	})

	items := make([]libraryItem, len(scored))
	for i, si := range scored {
		items[i] = si.result
	}
	total = int64(len(items))
	return paginate(items, start, count), total
}

func (m *Module) semanticSearch(query string, start int64, count int64) ([]libraryItem, int64) {
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
		return nil, 0
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
	similar := m.vectorIndex.SearchDual(queryVec, DefaultCardWeight, DefaultSummaryWeight, searchLimit)
	if len(similar) == 0 {
		// Fallback for legacy index without :card/:summary suffixes
		similar = m.vectorIndex.Search(queryVec, searchLimit)
	}

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
	minSimilarity := DefaultSimilarityThreshold
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
	total := int64(len(filtered))
	if start < 0 {
		start = 0
	}
	if int64(len(filtered)) <= start {
		m.log.Debug("no results after pagination offset",
			zap.Int64("start", start),
			zap.Int("filtered_count", len(filtered)))
		return nil, total
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

	return items, total
}

func (m *Module) buildEmbeddings(ctx context.Context, items map[string]mediaItem) {
	if m.embedProvider == nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	m.log.Debug("starting embedding build",
		zap.Int("total_items", len(items)),
		zap.String("provider", m.embedProvider.Name()))

	// Build set of current item IDs for tracking what to keep
	currentIDs := make(map[string]bool, len(items))
	for id := range items {
		currentIDs[id] = true
	}

	// Remove vectors for items that no longer exist instead of clearing all
	m.vectorIndex.RemoveStale(currentIDs)

	// Collect items needing embeddings (card + optional summary).
	// Items already present in the vector index with matching cached text
	// are skipped entirely to make incremental rebuilds fast.
	var inputs []EmbedInput
	cached := 0
	skipped := 0

	for id, item := range items {
		var enrich *AlbumMetadata
		if item.MediaType == "Audio" {
			key := firstOr(item.Artists, "Unknown Artist") + "|" + item.Album
			m.mu.RLock()
			enrich = m.enrichMeta[key]
			m.mu.RUnlock()
		}

		// Card vector
		cardText := buildEmbedText(item, enrich)
		if cardText == "" {
			m.log.Debug("skipping item with empty embed text", zap.String("id", id))
			continue
		}
		cardID := id + vectorSuffixCard
		if m.embedCache != nil {
			if _, ok := m.embedCache.Get(cardID, cardText); ok {
				if _, inIdx := m.vectorIndex.Get(cardID); inIdx {
					// Already in index with same text — nothing to do
					skipped++
				} else {
					// Cached but not in index (e.g. first run after restart)
					vec, _ := m.embedCache.Get(cardID, cardText)
					m.vectorIndex.Add(cardID, vec)
					cached++
				}
			} else {
				inputs = append(inputs, EmbedInput{ID: cardID, Text: cardText})
			}
		} else {
			if _, inIdx := m.vectorIndex.Get(cardID); !inIdx {
				inputs = append(inputs, EmbedInput{ID: cardID, Text: cardText})
			} else {
				skipped++
			}
		}

		// Summary vector (only if non-empty)
		summaryText := buildSummaryText(item, enrich)
		if summaryText != "" {
			summaryID := id + vectorSuffixSummary
			if m.embedCache != nil {
				if _, ok := m.embedCache.Get(summaryID, summaryText); ok {
					if _, inIdx := m.vectorIndex.Get(summaryID); inIdx {
						skipped++
					} else {
						vec, _ := m.embedCache.Get(summaryID, summaryText)
						m.vectorIndex.Add(summaryID, vec)
						cached++
					}
				} else {
					inputs = append(inputs, EmbedInput{ID: summaryID, Text: summaryText})
				}
			} else {
				if _, inIdx := m.vectorIndex.Get(summaryID); !inIdx {
					inputs = append(inputs, EmbedInput{ID: summaryID, Text: summaryText})
				} else {
					skipped++
				}
			}
		}
	}

	if cached > 0 || skipped > 0 {
		m.log.Debug("incremental embedding status",
			zap.Int("loaded_from_cache", cached),
			zap.Int("already_in_index", skipped))
	}

	if len(inputs) == 0 {
		m.log.Debug("all embeddings up to date, nothing to compute")
		return
	}

	m.log.Info("building embeddings",
		zap.Int("to_compute", len(inputs)),
		zap.Int("from_cache", cached),
		zap.Int("already_indexed", skipped))

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
	batchSize := m.config.EmbeddingBatchSize
	if batchSize <= 0 {
		batchSize = 32
	}
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
		zap.Int("computed", len(inputs)),
		zap.Int("from_cache", cached),
		zap.Int("already_indexed", skipped),
		zap.Int("vector_index_size", m.vectorIndex.Size()))
}

// buildSearchText creates the lowercased search text for a media item.
// Called once at scan time to precompute the searchable string. Includes
// LLM-classified genre, composer, and embedded genre tags so queries for
// "classical" or composer names hit albums whose external metadata is
// fine-grained or missing.
func buildSearchText(item mediaItem, enrich *AlbumMetadata) string {
	parts := []string{item.Name, item.Title, item.Album, strings.Join(item.Artists, " ")}
	if item.Composer != "" {
		parts = append(parts, item.Composer)
	}
	if item.EmbeddedGenre != "" {
		parts = append(parts, item.EmbeddedGenre)
	}
	if enrich != nil {
		if enrich.LLMGenre != "" {
			parts = append(parts, enrich.LLMGenre)
		}
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
	return strings.ToLower(strings.Join(parts, " "))
}

func containsAllTerms(item mediaItem, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(item.SearchText, term) {
			return false
		}
	}
	return true
}

func (m *Module) scanForceEnrichCtx(ctx context.Context) error {
	return m.scanInner(ctx, true)
}

func (m *Module) scanCtx(ctx context.Context) error {
	return m.scanInner(ctx, false)
}

func (m *Module) scanForceEnrich() error {
	return m.scanInner(context.Background(), true)
}

func (m *Module) scan() error {
	return m.scanInner(context.Background(), false)
}

func (m *Module) scanInner(ctx context.Context, forceEnrich bool) error {
	m.scanCount.Add(1)
	m.scanMu.Lock()
	defer m.scanMu.Unlock()

	started := time.Now()

	// Clear embedded art cache on rescan (paths may have changed)
	m.artCacheMu.Lock()
	m.artCache = make(map[string]*artCacheEntry)
	m.artCacheMu.Unlock()

	exts := buildExtMap(m.config.IncludeExts)
	videoExts := defaultVideoExts()

	// Snapshot previous state for incremental scan (skip unchanged files)
	m.mu.RLock()
	prevByPath := m.prevItems
	prevIndex := m.index
	prevEnrichMeta := m.enrichMeta
	m.mu.RUnlock()

	next := &libraryIndex{
		Items:      map[string]mediaItem{},
		Audio:      map[string]artistEntry{},
		Containers: map[string]containerInfo{},
	}

	reused := 0
	newCount := 0
	changedDirs := make(map[string]struct{}) // dirs with new/modified files

	// Collect cover art candidates during the walk to avoid redundant os.ReadDir
	// calls later. Maps directory path → best cover art path found so far.
	type coverCandidate struct {
		path     string
		priority int
	}
	coverCandidates := make(map[string]coverCandidate)

	// Phase 1: Walk filesystem — reuse unchanged items, collect new files for
	// parallel tag parsing. The walk itself is fast (stat-only for new files);
	// expensive tag I/O is deferred to phase 2.
	type newFileEntry struct {
		path      string
		info      os.FileInfo
		mediaType string
	}
	var newFiles []newFileEntry

	for _, root := range m.config.Roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				m.log.Debug("walk error", zap.Error(err), zap.String("path", path))
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if !exts[ext] {
				// Check if this is a cover art candidate (image file matching known names)
				if pri, ok := coverArtPriority[strings.ToLower(d.Name())]; ok {
					dir := filepath.Dir(path)
					if cc, exists := coverCandidates[dir]; !exists || pri < cc.priority {
						coverCandidates[dir] = coverCandidate{path: path, priority: pri}
					}
				}
				return nil
			}

			// Incremental scan: reuse previous item if file unchanged
			if prevByPath != nil {
				if prev, ok := prevByPath[path]; ok {
					info, infoErr := d.Info()
					if infoErr == nil && info.Size() > 0 &&
						info.ModTime().Equal(prev.Mtime) {
						// File unchanged — reuse previous item as-is
						next.Items[prev.ID] = prev
						reused++
						// Still need to add to audio/video hierarchy below
						item := prev
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
					}
				}
			}

			// New or modified file — collect stat info for parallel tag parsing
			info, infoErr := d.Info()
			if infoErr != nil {
				m.log.Debug("stat failed", zap.Error(infoErr), zap.String("path", path))
				return nil
			}
			mediaType := "Audio"
			if videoExts[ext] {
				mediaType = "Video"
			}
			newFiles = append(newFiles, newFileEntry{path: path, info: info, mediaType: mediaType})
			return nil
		})
		if err != nil {
			m.log.Warn("walk failed", zap.Error(err), zap.String("root", root))
		}
	}

	// Phase 2: Parse tags for new/modified files using a bounded worker pool.
	// Tag reading (opening files, decoding ID3/Vorbis/MP4) is the bottleneck
	// on initial scans; parallelising it across CPUs keeps the I/O pipeline busy.
	if len(newFiles) > 0 {
		workers := runtime.NumCPU()
		if workers < 1 {
			workers = 1
		}
		if workers > len(newFiles) {
			workers = len(newFiles)
		}

		type buildResult struct {
			item mediaItem
			err  error
		}

		jobs := make(chan newFileEntry, len(newFiles))
		results := make(chan buildResult, len(newFiles))

		// Start workers
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for entry := range jobs {
					if ctx.Err() != nil {
						results <- buildResult{err: ctx.Err()}
						return
					}
					item, buildErr := m.buildItemFromInfo(entry.path, entry.info, entry.mediaType)
					results <- buildResult{item: item, err: buildErr}
				}
			}()
		}

		// Send all jobs (channel is buffered to len(newFiles), so this won't block)
		for _, entry := range newFiles {
			jobs <- entry
		}
		close(jobs)

		// Close results channel once all workers finish
		go func() {
			wg.Wait()
			close(results)
		}()

		// Collect results (single goroutine — no synchronisation needed on next)
		for res := range results {
			if res.err != nil {
				if errors.Is(res.err, context.Canceled) || errors.Is(res.err, context.DeadlineExceeded) {
					continue
				}
				m.log.Debug("item build failed", zap.Error(res.err))
				continue
			}
			item := res.item
			newCount++
			changedDirs[filepath.Dir(item.Path)] = struct{}{}
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
		}
		m.log.Debug("parallel tag parsing complete",
			zap.Int("new_files", len(newFiles)),
			zap.Int("workers", workers),
			zap.Int("parsed", newCount))
	}

	for artistName, artist := range next.Audio {
		for albumName, album := range artist.Albums {
			sort.Strings(album.Tracks)
			// Look for cover art in the album directory or embedded in tracks
			if len(album.Tracks) > 0 {
				if item, ok := next.Items[album.Tracks[0]]; ok {
					dir := filepath.Dir(item.Path)
					// Use cover art candidate collected during the walk (avoids redundant os.ReadDir)
					if cc, found := coverCandidates[dir]; found {
						album.CoverArt = cc.path
					} else {
						// Fall back to os.ReadDir for directories not visited during walk
						album.CoverArt = findCoverArt(dir)
					}
					if album.CoverArt != "" {
						album.CoverArtExt = strings.ToLower(filepath.Ext(album.CoverArt))
					} else if item.EmbeddedArtExt != "" {
						// Fall back to embedded art detected during tag reading (no extra file open)
						album.CoverArt = item.Path
						album.CoverArtExt = item.EmbeddedArtExt
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
				item.SearchText = "" // invalidate so it gets recomputed below
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
			hash := item.FileHash
			if hash == "" {
				var err error
				hash, err = computeFileHash(item.Path)
				if err != nil {
					m.log.Debug("hash failed", zap.String("path", item.Path), zap.Error(err))
					continue
				}
				item.FileHash = hash
				next.Items[id] = item
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

	// Load sidecar enrichment data and collect enrichment targets.
	// Optimisation: if an album directory had no new/modified files during this
	// scan, its sidecar file is unchanged too.  Reuse previously-loaded metadata
	// instead of re-reading the file from disk (saves 200 sequential file opens
	// on large libraries with few changes).
	enrichMeta := make(map[string]*AlbumMetadata)
	enrichDirs := make(map[string]string) // key -> album dir (for summary backfill)
	var enrichTargets []enrichTarget
	sidecarSkipped := 0
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
			key := artistName + "|" + albumName

			// Fast path: directory unchanged and we already have enrichment data
			if _, changed := changedDirs[dir]; !changed && !forceEnrich {
				if prev, ok := prevEnrichMeta[key]; ok {
					enrichMeta[key] = prev
					enrichDirs[key] = dir
					sidecarSkipped++
					continue
				}
			}

			if meta, err := readSidecar(dir); err == nil {
				if forceEnrich || sidecarNeedsRefresh(meta) {
					enrichTargets = append(enrichTargets, enrichTarget{
						Artist: artistName, Album: albumName, Dir: dir,
					})
				} else {
					enrichMeta[key] = meta
					enrichDirs[key] = dir
				}
			} else if m.config.EnrichEnabled && !sidecarExists(dir) {
				enrichTargets = append(enrichTargets, enrichTarget{
					Artist: artistName, Album: albumName, Dir: dir,
				})
			}
		}
	}

	// Log sidecar loading summary
	sidecarsWithSummary := 0
	for _, meta := range enrichMeta {
		if meta.Description != nil && meta.Description.GeneratedSummary != "" {
			sidecarsWithSummary++
		}
	}
	m.log.Debug("sidecar loading complete",
		zap.Int("loaded", len(enrichMeta)),
		zap.Int("reused_unchanged", sidecarSkipped),
		zap.Int("with_generated_summary", sidecarsWithSummary),
		zap.Int("queued_for_enrichment", len(enrichTargets)),
		zap.Bool("enrich_enabled", m.config.EnrichEnabled),
		zap.Bool("summary_gen_available", m.summaryGen != nil))

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
				item.SearchText = "" // invalidate so it gets recomputed below
				next.Items[id] = item
				m.log.Debug("sidecar-repaired metadata",
					zap.String("id", id),
					zap.String("source", repaired.Source),
					zap.Float32("confidence", repaired.Confidence))
			}
		}
	}

	// Precompute search text for each item using enrichment metadata.
	// Optimisation: reused items from unchanged directories already have
	// SearchText populated from the previous scan (carried forward via
	// prevItems).  Skip recomputation for those items to avoid O(n) string
	// allocation + hashing on every no-change scan.
	for id, item := range next.Items {
		if item.SearchText != "" {
			continue // already populated from previous scan
		}
		var enrich *AlbumMetadata
		if item.MediaType == "Audio" {
			key := firstOr(item.Artists, "Unknown Artist") + "|" + item.Album
			enrich = enrichMeta[key]
		}
		item.SearchText = buildSearchText(item, enrich)
		next.Items[id] = item
	}

	// Build browse hierarchy indexes — skip if the library is unchanged
	// (no new/modified files, no removed files) to save CPU on stable libraries.
	removedCount := 0
	if prevByPath != nil {
		removedCount = len(prevByPath) - reused
	}
	if newCount == 0 && removedCount == 0 && prevIndex != nil {
		// Library unchanged — reuse previous browse indexes
		next.GenreAlbums = prevIndex.GenreAlbums
		next.ArtistLetters = prevIndex.ArtistLetters
		next.RecentAudioAlbums = prevIndex.RecentAudioAlbums
		next.RecentVideos = prevIndex.RecentVideos
		next.AudioFolderTree = prevIndex.AudioFolderTree
		next.VideoFolderTree = prevIndex.VideoFolderTree
		m.log.Debug("library unchanged, skipped browse index rebuild",
			zap.Int("reused", reused))
	} else {
		buildGenreIndex(next, enrichMeta)
		buildArtistLetterIndex(next)
		buildRecentLists(next)
		buildFolderTrees(next, m.config.Roots)
		m.log.Debug("rebuilt browse indexes",
			zap.Int("new", newCount),
			zap.Int("removed", removedCount),
			zap.Int("reused", reused))
	}

	// Build path→item index for next incremental scan
	nextPrevItems := make(map[string]mediaItem, len(next.Items))
	for _, item := range next.Items {
		nextPrevItems[item.Path] = item
	}

	m.mu.Lock()
	m.index = next
	m.enrichMeta = enrichMeta
	m.prevItems = nextPrevItems
	m.artURLCache = make(map[string]string) // invalidate art URL cache on index rebuild
	m.mu.Unlock()

	// Build embeddings asynchronously
	if m.embedProvider != nil {
		go m.buildEmbeddings(ctx, next.Items)
	}

	// Launch enrichment in background (respects parent context for graceful shutdown)
	if m.config.EnrichEnabled && len(enrichTargets) > 0 {
		go m.enrichAlbums(ctx, enrichTargets)
	}

	// Backfill summaries for existing sidecars that have metadata but no generated summary
	if m.summaryGen != nil {
		go m.backfillSummaries(ctx, enrichMeta, enrichDirs)
	}

	// Backfill LLM-classified top-level genre for sidecars missing it.
	if m.genreClassifier != nil {
		go m.backfillGenres(ctx, enrichMeta, enrichDirs)
	}

	// Skip persisting the index when nothing changed — avoids rewriting the
	// same JSON file to disk every scan interval (typically every 15 minutes).
	if newCount > 0 || removedCount > 0 {
		if err := m.saveIndex(); err != nil {
			m.log.Debug("index save failed", zap.Error(err))
		}
	} else {
		m.log.Debug("index unchanged, skipped save to disk")
	}
	m.log.Info("scan complete", zap.Duration("elapsed", time.Since(started)), zap.Int("items", len(next.Items)), zap.Int("reused", reused))
	return nil
}

// buildItemFromInfo builds a mediaItem from pre-resolved os.FileInfo and
// mediaType, avoiding a redundant d.Info() call. It is safe for concurrent use
// from multiple goroutines because readTags and fallbackMetadata are pure
// functions with no shared mutable state.
func (m *Module) buildItemFromInfo(path string, info os.FileInfo, mediaType string) (mediaItem, error) {
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
		ID:             itemID,
		Path:           path,
		Name:           name,
		Title:          meta.Title,
		Artists:        meta.Artists,
		Album:          meta.Album,
		Composer:       meta.Composer,
		EmbeddedGenre:  meta.EmbeddedGenre,
		MediaType:      mediaType,
		DurationMS:     meta.DurationMS,
		Mtime:          info.ModTime(),
		EmbeddedArtExt: meta.ArtExt,
	}, nil
}

type tagMetadata struct {
	Title         string
	Artists       []string
	Album         string
	Composer      string
	EmbeddedGenre string
	DurationMS    int64
	ArtExt        string // embedded art extension (e.g. ".jpg"), empty if none
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

	// Check for embedded art while file is already parsed
	var artExt string
	if pic := metadata.Picture(); pic != nil {
		artExt = mimeToExt(pic.MIMEType)
	}

	return tagMetadata{
		Title:         strings.TrimSpace(metadata.Title()),
		Artists:       artists,
		Album:         strings.TrimSpace(metadata.Album()),
		Composer:      strings.TrimSpace(metadata.Composer()),
		EmbeddedGenre: strings.TrimSpace(metadata.Genre()),
		DurationMS:    0,
		ArtExt:        artExt,
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
		return map[string]any{"title": "Audio", "type": "Folder"}, true
	case "container:video":
		return map[string]any{"title": "Video", "type": "Folder"}, true
	case "container:audio:bygenre":
		return map[string]any{"title": "By Genre", "type": "Folder"}, true
	case "container:audio:byartist":
		return map[string]any{"title": "By Artist", "type": "Folder"}, true
	case "container:audio:recent":
		return map[string]any{"title": "Recently Added", "type": "Folder"}, true
	case "container:audio:byfolder":
		return map[string]any{"title": "By Folder", "type": "Folder"}, true
	case "container:video:recent":
		return map[string]any{"title": "Recently Added", "type": "Folder"}, true
	case "container:video:byfolder":
		return map[string]any{"title": "By Folder", "type": "Folder"}, true
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
	case "genre":
		return map[string]any{
			"title": info.Artist,
			"type":  "MusicGenre",
		}, true
	case "letter":
		return map[string]any{
			"title": info.Artist,
			"type":  "Folder",
		}, true
	case "audiodir", "videodir":
		return map[string]any{
			"title": filepath.Base(info.Artist),
			"type":  "Folder",
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
	return baseURL + "/files/" + url.PathEscape(itemID), nil
}

func (m *Module) startHTTPServer() error {
	ln, err := net.Listen("tcp", m.config.HTTPListen)
	if err != nil {
		return err
	}
	var baseURL string
	if u := strings.TrimSpace(m.config.HTTPBaseURL); u != "" {
		baseURL = strings.TrimRight(u, "/")
	} else {
		host, port, err := net.SplitHostPort(ln.Addr().String())
		if err != nil {
			_ = ln.Close()
			return err
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		baseURL = fmt.Sprintf("http://%s:%s", host, port)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/files/", m.serveFile)
	mux.HandleFunc("/art/default.png", m.serveDefaultArt)
	mux.HandleFunc("/art/", m.serveArt)
	server := &http.Server{Handler: mux}

	m.mu.Lock()
	m.baseURL = baseURL
	m.artURLPrefix = baseURL + "/art/"
	m.artURLCache = make(map[string]string)
	m.defaultArtURL = baseURL + "/art/default.png"
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
	fi, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, filepath.Base(item.Path), fi.ModTime(), f)
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

	// Album art URLs are hash-based and effectively immutable; cache aggressively.
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")

	// Check if coverPath is an audio file (embedded art) or image file
	ext := strings.ToLower(filepath.Ext(coverPath))
	audioExts := defaultAudioExts()
	if audioExts[ext] {
		// Extract embedded art from audio file (with in-memory cache)
		data, mime, err := m.getEmbeddedArt(coverPath)
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
	return m.defaultArtURL
}

// itemArtworkURLUnlocked is the lock-free counterpart of getItemArtworkURL.
// Caller must hold m.mu.
func (m *Module) itemArtworkURLUnlocked(item mediaItem) string {
	if m.baseURL == "" {
		return ""
	}
	if item.MediaType == "Audio" {
		artistName := firstOr(item.Artists, "Unknown Artist")
		albumName := item.Album
		if albumName == "" {
			albumName = "Unknown Album"
		}
		return m.artURLUnlocked(containerHash("album", artistName, albumName))
	}
	return m.defaultArtURL
}

// artURLUnlocked returns the URL for album art when caller already holds the lock.
// The albumHash is used to look up the extension from the album entry.
// Returns the default placeholder image URL if no cover art is available.
// Results are cached in artURLCache to avoid repeated string allocations.
func (m *Module) artURLUnlocked(albumHash string) string {
	if m.artURLPrefix == "" {
		return ""
	}

	// Check the cache first.
	if cached, ok := m.artURLCache[albumHash]; ok {
		return cached
	}

	// Look up the extension from the container info
	info, ok := m.index.Containers[albumHash]
	if !ok || info.Type != "album" {
		return m.defaultArtURL
	}
	artist, ok := m.index.Audio[info.Artist]
	if !ok {
		return m.defaultArtURL
	}
	album, ok := artist.Albums[info.Album]
	if !ok || album.CoverArt == "" {
		return m.defaultArtURL
	}
	ext := album.CoverArtExt
	if ext == "" {
		ext = ".jpg"
	}
	artURL := m.artURLPrefix + url.PathEscape(albumHash) + ext
	m.artURLCache[albumHash] = artURL
	return artURL
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
	if idx.GenreAlbums == nil {
		idx.GenreAlbums = map[string][]genreAlbumRef{}
	}
	if idx.ArtistLetters == nil {
		idx.ArtistLetters = map[string][]string{}
	}
	if idx.AudioFolderTree == nil {
		idx.AudioFolderTree = map[string]*folderNode{}
	}
	if idx.VideoFolderTree == nil {
		idx.VideoFolderTree = map[string]*folderNode{}
	}
	m.mu.Lock()
	m.index = &idx
	m.artURLCache = make(map[string]string) // invalidate art URL cache on index load
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
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, strconv.FormatInt(size, 10))
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, strconv.FormatInt(mod.UnixNano(), 10))
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

// artistFirstLetter returns the uppercase first letter of a name, or "#" for non-letter starts.
func artistFirstLetter(name string) string {
	if name == "" {
		return "#"
	}
	r := []rune(strings.ToUpper(name))[0]
	if r >= 'A' && r <= 'Z' {
		return string(r)
	}
	return "#"
}

// buildGenreIndex populates idx.GenreAlbums by grouping albums under their
// classified top-level genre. Precedence per album:
//
//  1. AlbumMetadata.LLMGenre (cached classifier output) — preferred.
//  2. Rollup-map lookup on MB genres + tags + ArtistInfo genres + tags
//     (fallback when the LLM has not yet classified the album).
//  3. "Unknown".
//
// Each album lands in exactly one bucket so browse-by-genre stays clean.
func buildGenreIndex(idx *libraryIndex, enrichMeta map[string]*AlbumMetadata) {
	idx.GenreAlbums = make(map[string][]genreAlbumRef)
	for artistName, artist := range idx.Audio {
		for albumName := range artist.Albums {
			key := artistName + "|" + albumName
			meta := enrichMeta[key]
			genre := classifyAlbumBucket(meta)
			ref := genreAlbumRef{Artist: artistName, Album: albumName}
			idx.GenreAlbums[genre] = append(idx.GenreAlbums[genre], ref)
		}
	}
	for genre, albums := range idx.GenreAlbums {
		sort.Slice(albums, func(i, j int) bool {
			if albums[i].Artist != albums[j].Artist {
				return albums[i].Artist < albums[j].Artist
			}
			return albums[i].Album < albums[j].Album
		})
		idx.GenreAlbums[genre] = albums
		hash := containerHash("genre", genre, "")
		idx.Containers[hash] = containerInfo{Type: "genre", Artist: genre}
	}
}

// classifyAlbumBucket picks the single bucket an album lives in for browse.
func classifyAlbumBucket(meta *AlbumMetadata) string {
	if meta == nil {
		return "Unknown"
	}
	if meta.LLMGenre != "" {
		return meta.LLMGenre
	}
	var candidates []string
	if meta.MusicBrainz != nil {
		candidates = append(candidates, meta.MusicBrainz.Genres...)
		candidates = append(candidates, meta.MusicBrainz.Tags...)
	}
	if meta.ArtistInfo != nil {
		candidates = append(candidates, meta.ArtistInfo.Genres...)
		candidates = append(candidates, meta.ArtistInfo.Tags...)
	}
	if g := rollupGenreFromCandidates(candidates); g != "" {
		return g
	}
	return "Unknown"
}

// buildArtistLetterIndex groups artists by first character.
func buildArtistLetterIndex(idx *libraryIndex) {
	idx.ArtistLetters = make(map[string][]string)
	for artistName := range idx.Audio {
		letter := artistFirstLetter(artistName)
		idx.ArtistLetters[letter] = append(idx.ArtistLetters[letter], artistName)
	}
	for letter, artists := range idx.ArtistLetters {
		sort.Strings(artists)
		idx.ArtistLetters[letter] = artists
		// Register letter container
		hash := containerHash("letter", letter, "")
		idx.Containers[hash] = containerInfo{Type: "letter", Artist: letter}
	}
}

// buildRecentLists builds the recently added album and video lists.
func buildRecentLists(idx *libraryIndex) {
	// Audio: compute max Mtime per album, sort descending, take top 50
	type albumMtime struct {
		artist   string
		album    string
		maxMtime time.Time
	}
	var albumMtimes []albumMtime
	for artistName, artist := range idx.Audio {
		for albumName, album := range artist.Albums {
			var maxMtime time.Time
			for _, trackID := range album.Tracks {
				if item, ok := idx.Items[trackID]; ok {
					if item.Mtime.After(maxMtime) {
						maxMtime = item.Mtime
					}
				}
			}
			albumMtimes = append(albumMtimes, albumMtime{
				artist: artistName, album: albumName, maxMtime: maxMtime,
			})
		}
	}
	sort.Slice(albumMtimes, func(i, j int) bool {
		return albumMtimes[i].maxMtime.After(albumMtimes[j].maxMtime)
	})
	if len(albumMtimes) > 50 {
		albumMtimes = albumMtimes[:50]
	}
	idx.RecentAudioAlbums = make([]recentAlbumRef, len(albumMtimes))
	for i, am := range albumMtimes {
		idx.RecentAudioAlbums[i] = recentAlbumRef{
			Artist: am.artist, Album: am.album, MaxMtime: am.maxMtime,
		}
	}

	// Video: sort by Mtime descending, take top 50
	type videoMtime struct {
		id    string
		mtime time.Time
	}
	var videoMtimes []videoMtime
	for _, vid := range idx.Video {
		if item, ok := idx.Items[vid]; ok {
			videoMtimes = append(videoMtimes, videoMtime{id: vid, mtime: item.Mtime})
		}
	}
	sort.Slice(videoMtimes, func(i, j int) bool {
		return videoMtimes[i].mtime.After(videoMtimes[j].mtime)
	})
	if len(videoMtimes) > 50 {
		videoMtimes = videoMtimes[:50]
	}
	idx.RecentVideos = make([]string, len(videoMtimes))
	for i, vm := range videoMtimes {
		idx.RecentVideos[i] = vm.id
	}
}

// ensureParentChain creates parent folder nodes up to the root boundary.
func ensureParentChain(tree map[string]*folderNode, dir string, roots map[string]bool) {
	// Ensure this dir's node exists
	if tree[dir] == nil {
		tree[dir] = &folderNode{
			Path: dir,
			Name: filepath.Base(dir),
		}
	}
	if roots[dir] {
		return
	}

	parent := filepath.Dir(dir)
	if parent == dir || parent == "." || parent == string(filepath.Separator) {
		return
	}

	// Ensure parent exists
	if tree[parent] == nil {
		tree[parent] = &folderNode{
			Path: parent,
			Name: filepath.Base(parent),
		}
	}

	// Add dir as subdir of parent (dedup)
	found := false
	for _, sd := range tree[parent].SubDirs {
		if sd == dir {
			found = true
			break
		}
	}
	if !found {
		tree[parent].SubDirs = append(tree[parent].SubDirs, dir)
	}

	if roots[parent] {
		return
	}

	// Recurse up
	ensureParentChain(tree, parent, roots)
}

// buildFolderTrees builds the audio and video folder trees.
func buildFolderTrees(idx *libraryIndex, roots []string) {
	idx.AudioFolderTree = make(map[string]*folderNode)
	idx.VideoFolderTree = make(map[string]*folderNode)

	rootSet := make(map[string]bool, len(roots))
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r != "" {
			rootSet[r] = true
		}
	}

	for _, item := range idx.Items {
		dir := filepath.Dir(item.Path)
		var tree map[string]*folderNode
		var containerType string
		if item.MediaType == "Audio" {
			tree = idx.AudioFolderTree
			containerType = "audiodir"
		} else if item.MediaType == "Video" {
			tree = idx.VideoFolderTree
			containerType = "videodir"
		} else {
			continue
		}

		ensureParentChain(tree, dir, rootSet)
		node := tree[dir]
		node.Items = append(node.Items, item.ID)
		_ = containerType // containers registered below
	}

	// Sort subdirs and items, register containers
	for _, tree := range []struct {
		t    map[string]*folderNode
		ctyp string
	}{
		{idx.AudioFolderTree, "audiodir"},
		{idx.VideoFolderTree, "videodir"},
	} {
		for dirPath, node := range tree.t {
			sort.Strings(node.SubDirs)
			sort.Strings(node.Items)
			hash := containerHash(tree.ctyp, dirPath, "")
			idx.Containers[hash] = containerInfo{Type: tree.ctyp, Artist: dirPath}
		}
	}

	// Pick a cover album for each folder once, post-build, so browse stays
	// O(1). Result is persisted to the sidecar via folderNode.CoverAlbumHash.
	populateFolderCovers(idx, idx.AudioFolderTree)
	populateFolderCovers(idx, idx.VideoFolderTree)
}

// populateFolderCovers walks each folder subtree depth-first and sets
// CoverAlbumHash on every node to the first audio album with real cover
// art it finds. Audio art is the only useful source today; video folders
// inherit nothing and stay empty (browse falls back to the placeholder).
// Each tree is walked once; per-folder lookups are memoized via the
// CoverAlbumHash field itself.
func populateFolderCovers(idx *libraryIndex, tree map[string]*folderNode) {
	for path := range tree {
		pickFolderCover(tree[path], tree, idx, make(map[string]bool))
	}
}

// pickFolderCover returns the album hash to use as cover for node, recursing
// into subdirs as needed. visiting guards against accidental cycles in the
// folder graph (e.g. symlinks in the source tree).
func pickFolderCover(node *folderNode, tree map[string]*folderNode, idx *libraryIndex, visiting map[string]bool) string {
	if node == nil {
		return ""
	}
	if node.CoverAlbumHash != "" {
		return node.CoverAlbumHash
	}
	if visiting[node.Path] {
		return ""
	}
	visiting[node.Path] = true

	for _, id := range node.Items {
		item, ok := idx.Items[id]
		if !ok || item.MediaType != "Audio" {
			continue
		}
		artistName := firstOr(item.Artists, "Unknown Artist")
		albumName := item.Album
		if albumName == "" {
			albumName = "Unknown Album"
		}
		artist, ok := idx.Audio[artistName]
		if !ok {
			continue
		}
		album, ok := artist.Albums[albumName]
		if !ok || album.CoverArt == "" {
			continue
		}
		node.CoverAlbumHash = containerHash("album", artistName, albumName)
		return node.CoverAlbumHash
	}
	for _, sd := range node.SubDirs {
		if h := pickFolderCover(tree[sd], tree, idx, visiting); h != "" {
			node.CoverAlbumHash = h
			return h
		}
	}
	return ""
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

// coverArtNames lists common cover art filenames in priority order (lowercase).
var coverArtNames = []string{
	"cover.jpg", "cover.jpeg", "cover.png",
	"folder.jpg", "folder.jpeg", "folder.png",
	"album.jpg", "album.jpeg", "album.png",
	"front.jpg", "front.jpeg", "front.png",
	"albumart.jpg", "albumart.jpeg", "albumart.png",
}

// coverArtPriority maps lowercase cover art filenames to their priority (lower = better).
var coverArtPriority = func() map[string]int {
	m := make(map[string]int, len(coverArtNames))
	for i, name := range coverArtNames {
		m[name] = i
	}
	return m
}()

// findCoverArt looks for a cover art file in the given directory.
// Uses a single os.ReadDir call instead of per-name os.Stat calls.
// Returns the full path if found, empty string otherwise.
func findCoverArt(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	bestPriority := len(coverArtNames) // worse than any valid priority
	bestPath := ""
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		lower := strings.ToLower(e.Name())
		if pri, ok := coverArtPriority[lower]; ok && pri < bestPriority {
			bestPriority = pri
			bestPath = filepath.Join(dir, e.Name())
			if pri == 0 {
				break // best possible match
			}
		}
	}
	return bestPath
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

// getEmbeddedArt returns embedded album art, using an in-memory cache to avoid
// re-parsing audio files on every HTTP request.
func (m *Module) getEmbeddedArt(path string) ([]byte, string, error) {
	now := time.Now().UnixNano()
	m.artCacheMu.RLock()
	if entry, ok := m.artCache[path]; ok {
		entry.lastAccess.Store(now)
		m.artCacheMu.RUnlock()
		return entry.data, entry.mime, nil
	}
	m.artCacheMu.RUnlock()

	data, mime, err := extractEmbeddedArt(path)
	if err != nil {
		return nil, "", err
	}

	m.artCacheMu.Lock()
	// Cap cache at 200 entries to bound memory usage
	if len(m.artCache) >= 200 {
		// Evict oldest half instead of clearing everything (avoids thundering herd)
		m.evictArtCacheHalf()
	}
	entry := &artCacheEntry{data: data, mime: mime}
	entry.lastAccess.Store(now)
	m.artCache[path] = entry
	m.artCacheMu.Unlock()

	return data, mime, nil
}

// evictArtCacheHalf removes roughly half the art cache entries at random.
// Go's map iteration order is non-deterministic, so simply deleting every
// other entry during iteration gives us random eviction without sorting.
// Caller must hold artCacheMu write lock.
func (m *Module) evictArtCacheHalf() {
	evictCount := len(m.artCache) / 2
	i := 0
	for path := range m.artCache {
		if i >= evictCount {
			break
		}
		delete(m.artCache, path)
		i++
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
