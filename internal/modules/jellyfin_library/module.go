package jellyfinlibrary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	freecache "github.com/coocood/freecache"
	paho "github.com/eclipse/paho.mqtt.golang"
	gocache "github.com/eko/gocache/lib/v4/cache"
	libstore "github.com/eko/gocache/lib/v4/store"
	gocachefreecache "github.com/eko/gocache/store/freecache/v4"
	"github.com/golang/snappy"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

// Config configures the Jellyfin library bridge.
type Config struct {
	NodeID                 string
	TopicBase              string
	Name                   string
	BaseURL                string
	StreamBaseURL          string
	ArtworkBaseURL         string
	APIKey                 string
	UserID                 string
	Timeout                time.Duration
	CacheTTL               time.Duration
	CacheSize              int
	CacheCompress          bool
	BrowseCacheTTL         time.Duration
	BrowseCacheSize        int
	MaxConcurrentRequests  int
	PublishTimeoutCooldown time.Duration
}

// Module handles library commands via Jellyfin.
type Module struct {
	log                 *zap.Logger
	client              *mqttserver.Client
	http                *http.Client
	config              Config
	cmdTopic            string
	cache               gocache.CacheInterface[[]byte]
	cacheCtx            context.Context
	browseCache         gocache.CacheInterface[[]byte]
	browseCacheCtx      context.Context
	reqSem              chan struct{}
	publishTimeoutUntil int64
}

// NewModule creates a Jellyfin library module.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*Module, error) {
	if log == nil {
		log = zap.NewNop()
	}
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, errors.New("node_id required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("base_url required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("api_key required")
	}
	if strings.TrimSpace(cfg.UserID) == "" {
		return nil, errors.New("user_id required")
	}
	if strings.TrimSpace(cfg.TopicBase) == "" {
		cfg.TopicBase = mu.BaseTopic
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = "Jellyfin Library"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 10 * time.Minute
	}
	if cfg.MaxConcurrentRequests <= 0 {
		cfg.MaxConcurrentRequests = 4
	}
	if cfg.PublishTimeoutCooldown <= 0 {
		cfg.PublishTimeoutCooldown = 2 * time.Second
	}
	if cfg.BrowseCacheSize == 0 {
		cfg.BrowseCacheSize = 16 * 1024 * 1024
	}
	cfg.CacheSize = cacheSizeBytes(cfg.CacheSize)
	cfg.BrowseCacheSize = cacheSizeBytes(cfg.BrowseCacheSize)

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	cfg.BaseURL = baseURL
	cfg.StreamBaseURL = strings.TrimRight(cfg.StreamBaseURL, "/")
	cfg.ArtworkBaseURL = strings.TrimRight(cfg.ArtworkBaseURL, "/")

	cmdTopic := mu.TopicCommands(cfg.TopicBase, cfg.NodeID)

	return &Module{
		log:            log,
		client:         client,
		http:           &http.Client{Timeout: cfg.Timeout},
		config:         cfg,
		cmdTopic:       cmdTopic,
		cache:          newCache(cfg.CacheSize),
		cacheCtx:       context.Background(),
		browseCache:    newCache(cfg.BrowseCacheSize),
		browseCacheCtx: context.Background(),
		reqSem:         make(chan struct{}, cfg.MaxConcurrentRequests),
	}, nil
}

// Run starts the module.
func (m *Module) Run(ctx context.Context) error {
	if err := m.publishPresence(); err != nil {
		return err
	}

	handler := func(_ paho.Client, msg paho.Message) {
		m.handleMessage(msg)
	}

	if err := m.client.Subscribe(m.cmdTopic, 1, handler); err != nil {
		return err
	}
	defer m.client.Unsubscribe(m.cmdTopic)

	<-ctx.Done()
	return nil
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
		},
		TS: time.Now().Unix(),
	}
	payload, err := json.Marshal(presence)
	if err != nil {
		return err
	}
	return m.client.Publish(mu.TopicPresence(m.config.TopicBase, m.config.NodeID), 1, true, payload)
}

func (m *Module) handleMessage(msg paho.Message) {
	var cmd mu.CommandEnvelope
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		m.log.Warn("invalid command", zap.Error(err))
		return
	}

	started := time.Now()
	reply := m.dispatch(cmd)
	if cmd.ReplyTo == "" {
		return
	}
	payload, err := json.Marshal(reply)
	if err != nil {
		m.log.Error("marshal reply", zap.Error(err))
		return
	}
	if len(payload) > 512*1024 {
		m.log.Warn("large reply payload", zap.String("type", cmd.Type), zap.Int("bytes", len(payload)))
	}
	if err := m.client.Publish(cmd.ReplyTo, 1, false, payload); err != nil {
		m.log.Error("publish reply", zap.Error(err))
		if errors.Is(err, mqttserver.ErrPublishTimeout) {
			m.markPublishTimeout()
		}
	} else {
		logFields := []zap.Field{
			zap.String("type", cmd.Type),
			zap.Duration("duration", time.Since(started)),
			zap.Int("bytes", len(payload)),
			zap.Bool("ok", reply.OK),
		}
		if reply.Err != nil {
			logFields = append(logFields, zap.String("err", reply.Err.Code))
		}
		m.log.Debug("command handled", logFields...)
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
	default:
		return errorReply(cmd, "INVALID", "unsupported command")
	}
}

func (m *Module) libraryBrowse(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryBrowseBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if m.shouldShedLoad() {
		m.log.Warn("load shed browse", zap.String("container", body.ContainerID))
		return errorReply(cmd, "OVERLOADED", "module overloaded, try again")
	}
	started := time.Now()
	cacheKey := browseCacheKey("browse", body.ContainerID, body.Start, body.Count, "")
	if cached, ok := m.browseCacheGet(cacheKey); ok {
		reply.Body = cached
		m.log.Debug(
			"library browse cache hit",
			zap.String("container", body.ContainerID),
			zap.Int64("start", body.Start),
			zap.Int64("count", body.Count),
			zap.Int("bytes", len(cached)),
			zap.Duration("duration", time.Since(started)),
		)
		return reply
	}
	items, total, err := m.fetchItems(body.ContainerID, body.Start, body.Count, "", nil, false)
	if err != nil {
		m.log.Debug(
			"library browse failed",
			zap.String("container", body.ContainerID),
			zap.Int64("start", body.Start),
			zap.Int64("count", body.Count),
			zap.Duration("duration", time.Since(started)),
			zap.Error(err),
		)
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(libraryItemsReply{Items: items, Start: body.Start, Count: int64(len(items)), Total: total})
	m.log.Debug(
		"library browse ok",
		zap.String("container", body.ContainerID),
		zap.Int64("start", body.Start),
		zap.Int64("count", body.Count),
		zap.Int("items", len(items)),
		zap.Int64("total", total),
		zap.Int("bytes", len(payload)),
		zap.Duration("duration", time.Since(started)),
	)
	m.browseCachePut(cacheKey, payload)
	reply.Body = payload
	return reply
}

func (m *Module) librarySearch(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibrarySearchBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if m.shouldShedLoad() {
		m.log.Warn("load shed search", zap.String("query", body.Query))
		return errorReply(cmd, "OVERLOADED", "module overloaded, try again")
	}
	started := time.Now()
	types, err := mapLibraryTypes(body.Types)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	cacheKey := browseCacheKey("search", "", body.Start, body.Count, body.Query)
	if cached, ok := m.browseCacheGet(cacheKey); ok {
		reply.Body = cached
		m.log.Debug(
			"library search cache hit",
			zap.String("query", body.Query),
			zap.Int64("start", body.Start),
			zap.Int64("count", body.Count),
			zap.Int("bytes", len(cached)),
			zap.Duration("duration", time.Since(started)),
		)
		return reply
	}
	items, total, err := m.fetchItems("", body.Start, body.Count, body.Query, types, true)
	if err != nil {
		m.log.Debug(
			"library search failed",
			zap.String("query", body.Query),
			zap.Int64("start", body.Start),
			zap.Int64("count", body.Count),
			zap.Duration("duration", time.Since(started)),
			zap.Error(err),
		)
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(libraryItemsReply{Items: items, Start: body.Start, Count: int64(len(items)), Total: total})
	m.log.Debug(
		"library search ok",
		zap.String("query", body.Query),
		zap.Int64("start", body.Start),
		zap.Int64("count", body.Count),
		zap.Int("items", len(items)),
		zap.Int64("total", total),
		zap.Int("bytes", len(payload)),
		zap.Duration("duration", time.Since(started)),
	)
	m.browseCachePut(cacheKey, payload)
	reply.Body = payload
	return reply
}

func (m *Module) libraryResolve(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryResolveBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	started := time.Now()
	metadata, sources, err := m.resolveItem(body.ItemID, body.MetadataOnly)
	if err != nil {
		m.log.Debug(
			"library resolve failed",
			zap.String("item", body.ItemID),
			zap.Bool("metadata_only", body.MetadataOnly),
			zap.Duration("duration", time.Since(started)),
			zap.Error(err),
		)
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(mu.LibraryResolveReply{ItemID: body.ItemID, Metadata: metadata, Sources: sources})
	m.log.Debug(
		"library resolve ok",
		zap.String("item", body.ItemID),
		zap.Bool("metadata_only", body.MetadataOnly),
		zap.Int("sources", len(sources)),
		zap.Int("bytes", len(payload)),
		zap.Duration("duration", time.Since(started)),
	)
	reply.Body = payload
	return reply
}

func (m *Module) libraryResolveBatch(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryResolveBatchBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	if len(body.ItemIDs) == 0 {
		return errorReply(cmd, "INVALID", "itemIds required")
	}
	items := make([]mu.LibraryResolveBatchItem, 0, len(body.ItemIDs))
	for _, itemID := range body.ItemIDs {
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			continue
		}
		metadata, sources, err := m.resolveItem(itemID, body.MetadataOnly)
		entry := mu.LibraryResolveBatchItem{ItemID: itemID, Metadata: metadata, Sources: sources}
		if err != nil {
			entry.Err = &mu.ReplyError{Code: "INVALID", Message: err.Error()}
		}
		items = append(items, entry)
	}
	payload, _ := json.Marshal(mu.LibraryResolveBatchReply{Items: items})
	reply.Body = payload
	return reply
}

type resolveCacheEntry struct {
	Metadata     map[string]any      `json:"metadata"`
	Sources      []mu.ResolvedSource `json:"sources"`
	SourcesReady bool                `json:"sourcesReady"`
}

func (m *Module) resolveItem(itemID string, metadataOnly bool) (map[string]any, []mu.ResolvedSource, error) {
	if metadata, sources, ok := m.cacheGet(itemID, metadataOnly); ok {
		m.log.Debug("jellyfin cache hit", zap.String("item", itemID), zap.Bool("metadata_only", metadataOnly))
		return metadata, sources, nil
	}
	m.log.Debug("jellyfin cache miss", zap.String("item", itemID), zap.Bool("metadata_only", metadataOnly))
	item, err := m.fetchItem(itemID)
	if err != nil {
		return nil, nil, err
	}
	metadata := m.buildMetadata(item)
	sources := []mu.ResolvedSource{}
	if !metadataOnly {
		var meta map[string]any
		sources, meta, err = m.resolveSources(item)
		if err != nil {
			return nil, nil, err
		}
		for k, v := range meta {
			if v != nil {
				metadata[k] = v
			}
		}
	}
	m.cachePut(itemID, metadata, sources, !metadataOnly)
	return metadata, sources, nil
}

func (m *Module) buildMetadata(item jfItem) map[string]any {
	metadata := map[string]any{
		"title":      item.Name,
		"type":       item.Type,
		"mediaType":  item.MediaType,
		"overview":   item.Overview,
		"durationMs": ticksToMS(item.RunTimeTicks),
	}
	if item.Album != "" {
		metadata["album"] = item.Album
	}
	if len(item.Artists) > 0 {
		metadata["artist"] = strings.Join(item.Artists, ", ")
		metadata["artists"] = item.Artists
	} else if item.AlbumArtist != "" {
		metadata["artist"] = item.AlbumArtist
	}
	if item.PrimaryImageTag != "" {
		metadata["artworkUrl"] = m.imageURL(item.ID)
	}
	return metadata
}

func (m *Module) cacheGet(itemID string, metadataOnly bool) (map[string]any, []mu.ResolvedSource, bool) {
	m.ensureCache()
	if m.cache == nil {
		return nil, nil, false
	}
	value, err := m.cache.Get(m.cacheCtx, itemID)
	if err != nil {
		return nil, nil, false
	}
	value, ok := m.cacheDecode(value)
	if !ok {
		return nil, nil, false
	}
	var entry resolveCacheEntry
	if err := json.Unmarshal(value, &entry); err != nil {
		return nil, nil, false
	}
	if metadataOnly {
		if entry.Metadata == nil {
			return nil, nil, false
		}
		return copyMetadata(entry.Metadata), nil, true
	}
	if entry.Metadata == nil || !entry.SourcesReady {
		return nil, nil, false
	}
	return copyMetadata(entry.Metadata), append([]mu.ResolvedSource(nil), entry.Sources...), true
}

func (m *Module) cachePut(itemID string, metadata map[string]any, sources []mu.ResolvedSource, sourcesReady bool) {
	if metadata == nil {
		return
	}
	m.ensureCache()
	if m.cache == nil {
		return
	}
	if len(sources) > 0 {
		sourcesReady = true
	}
	entry := resolveCacheEntry{
		Metadata:     copyMetadata(metadata),
		Sources:      append([]mu.ResolvedSource(nil), sources...),
		SourcesReady: sourcesReady,
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return
	}
	payload = m.cacheEncode(payload)
	ttl := m.cacheTTL()
	_ = m.cache.Set(m.cacheCtx, itemID, payload, libstore.WithExpiration(ttl))
}

func copyMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for k, v := range metadata {
		out[k] = v
	}
	return out
}

func (m *Module) cacheDecode(value []byte) ([]byte, bool) {
	if !m.config.CacheCompress {
		return value, true
	}
	decoded, err := snappy.Decode(nil, value)
	if err != nil {
		m.log.Debug("jellyfin cache decode failed", zap.Error(err))
		return nil, false
	}
	return decoded, true
}

func (m *Module) cacheEncode(value []byte) []byte {
	if !m.config.CacheCompress {
		return value
	}
	return snappy.Encode(nil, value)
}

func (m *Module) browseCacheGet(key string) ([]byte, bool) {
	m.ensureBrowseCache()
	if m.browseCache == nil {
		return nil, false
	}
	value, err := m.browseCache.Get(m.browseCacheCtx, key)
	if err != nil {
		return nil, false
	}
	value, ok := m.cacheDecode(value)
	if !ok {
		return nil, false
	}
	return value, true
}

func (m *Module) browseCachePut(key string, payload []byte) {
	if payload == nil {
		return
	}
	m.ensureBrowseCache()
	if m.browseCache == nil {
		return
	}
	value := m.cacheEncode(payload)
	ttl := m.browseCacheTTL()
	_ = m.browseCache.Set(m.browseCacheCtx, key, value, libstore.WithExpiration(ttl))
}

func (m *Module) ensureCache() {
	if m.cache != nil {
		return
	}
	m.cache = newCache(m.config.CacheSize)
}

func (m *Module) ensureBrowseCache() {
	if m.browseCache != nil {
		return
	}
	m.browseCache = newCache(m.config.BrowseCacheSize)
}

func (m *Module) cacheTTL() time.Duration {
	if m.config.CacheTTL <= 0 {
		return 10 * time.Minute
	}
	return m.config.CacheTTL
}

func (m *Module) browseCacheTTL() time.Duration {
	if m.config.BrowseCacheTTL <= 0 {
		return 5 * time.Minute
	}
	return m.config.BrowseCacheTTL
}

func cacheSizeBytes(size int) int {
	if size == 0 {
		return 64 * 1024 * 1024
	}
	if size > 0 && size < 1024*1024 {
		return size * 64 * 1024
	}
	return size
}

func newCache(size int) gocache.CacheInterface[[]byte] {
	size = cacheSizeBytes(size)
	if size <= 0 {
		return nil
	}
	store := gocachefreecache.NewFreecache(freecache.NewCache(size))
	return gocache.New[[]byte](store)
}

type libraryItemsReply struct {
	Items []libraryItem `json:"items"`
	Start int64         `json:"start"`
	Count int64         `json:"count"`
	Total int64         `json:"total"`
}

type libraryItem struct {
	ItemID      string   `json:"itemId"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	MediaType   string   `json:"mediaType"`
	Artists     []string `json:"artists,omitempty"`
	Album       string   `json:"album,omitempty"`
	ContainerID string   `json:"containerId,omitempty"`
	Overview    string   `json:"overview,omitempty"`
	DurationMS  int64    `json:"durationMs,omitempty"`
	ImageURL    string   `json:"imageUrl,omitempty"`
}

type jfItemsResponse struct {
	Items            []jfItem `json:"Items"`
	TotalRecordCount int64    `json:"TotalRecordCount"`
	StartIndex       int64    `json:"StartIndex"`
}

type jfItem struct {
	ID              string            `json:"Id"`
	Name            string            `json:"Name"`
	Type            string            `json:"Type"`
	MediaType       string            `json:"MediaType"`
	CollectionType  string            `json:"CollectionType"`
	Overview        string            `json:"Overview"`
	RunTimeTicks    int64             `json:"RunTimeTicks"`
	Artists         []string          `json:"Artists"`
	Album           string            `json:"Album"`
	ImageTags       map[string]string `json:"ImageTags"`
	AlbumArtist     string            `json:"AlbumArtist"`
	ParentID        string            `json:"ParentId"`
	PrimaryImageTag string
}

type jfPlaybackInfo struct {
	MediaSources []jfMediaSource `json:"MediaSources"`
}

type jfMediaSource struct {
	DirectStreamURL      string `json:"DirectStreamUrl"`
	Container            string `json:"Container"`
	SupportsDirectStream bool   `json:"SupportsDirectStream"`
}

func (m *Module) fetchItems(containerID string, start int64, count int64, search string, types []string, recursive bool) ([]libraryItem, int64, error) {
	if containerID != "" {
		if strings.HasPrefix(containerID, musicArtistPrefix) {
			libraryID := strings.TrimPrefix(containerID, musicArtistPrefix)
			return m.fetchMusicArtists(libraryID, start, count)
		}
		if strings.HasPrefix(containerID, musicGenrePrefix) {
			libraryID := strings.TrimPrefix(containerID, musicGenrePrefix)
			return m.fetchMusicGenres(libraryID, start, count)
		}
		if strings.HasPrefix(containerID, musicRecentPrefix) {
			libraryID := strings.TrimPrefix(containerID, musicRecentPrefix)
			return m.fetchRecentMusic(libraryID, start, count)
		}
		if strings.HasPrefix(containerID, musicFilesPrefix) {
			libraryID, parentID := parseFilesContainer(containerID)
			if parentID == "" {
				parentID = libraryID
			}
			return m.fetchFilesItems(libraryID, parentID, start, count, true)
		}
		if strings.HasPrefix(containerID, moviesRecentPrefix) {
			libraryID := strings.TrimPrefix(containerID, moviesRecentPrefix)
			return m.fetchRecentMovies(libraryID, start, count)
		}
		item, err := m.fetchItem(containerID)
		if err == nil {
			if strings.EqualFold(item.Type, "Playlist") {
				children, err := m.fetchPlaylistItems(containerID, start, count)
				if err != nil {
					return nil, 0, err
				}
				items := make([]libraryItem, 0, len(children))
				for _, child := range children {
					items = append(items, m.libraryItemFromJF(child))
				}
				return items, int64(len(items)), nil
			}
			if isMusicLibrary(item) {
				return m.musicLibraryRootItems(item.ID), 4, nil
			}
			if isMovieLibrary(item) {
				return m.fetchMoviesLibrary(item.ID, start, count)
			}
			if strings.EqualFold(item.Type, "Folder") {
				return m.fetchFilesItems("", item.ID, start, count, true)
			}
			if strings.EqualFold(item.Type, "MusicArtist") {
				return m.fetchMusicAlbumsByArtist(item.ID, start, count)
			}
			if strings.EqualFold(item.Type, "MusicGenre") {
				return m.fetchMusicAlbumsByGenre(item.ID, start, count)
			}
			if strings.EqualFold(item.Type, "MusicAlbum") {
				return m.fetchAlbumTracks(item.ID, start, count)
			}
		}
	}
	if containerID == "" && search == "" && len(types) == 0 {
		items, total, err := m.fetchItemsRaw(containerID, start, count, search, types, recursive)
		if err != nil {
			return nil, 0, err
		}
		if total <= int64(len(items)) {
			total = int64(len(items))
		}
		return items, total, nil
	}
	return m.fetchItemsRaw(containerID, start, count, search, types, recursive)
}

func (m *Module) fetchFilesItems(libraryID string, parentID string, start int64, count int64, forcePage bool) ([]libraryItem, int64, error) {
	endpoint := fmt.Sprintf("/Users/%s/Items", url.PathEscape(m.config.UserID))
	params := url.Values{}
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	params.Set("Fields", browseFieldsWithCollection())
	params.Set("IncludeItemTypes", "Folder,Audio,MusicAlbum,MusicVideo,Movie,Series,Episode,Video")
	if strings.TrimSpace(parentID) != "" {
		params.Set("ParentId", parentID)
	}
	items, total, err := m.fetchItemsWithParams(endpoint, params)
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(libraryID) != "" {
		for i := range items {
			if isFilesContainerType(items[i].Type) {
				items[i].ItemID = musicFilesPrefix + libraryID + ":" + items[i].ItemID
			}
		}
	}
	if forcePage && int64(len(items)) > 0 && total <= start+int64(len(items)) {
		total = start + int64(len(items)) + 1
	}
	return items, total, nil
}

func (m *Module) fetchMoviesLibrary(libraryID string, start int64, count int64) ([]libraryItem, int64, error) {
	includeRecent := start == 0
	movieStart := start
	movieCount := count
	if includeRecent && movieCount > 0 {
		movieCount = movieCount - 1
	} else if start > 0 {
		movieStart = start - 1
	}
	items, total, err := m.fetchMovies(libraryID, movieStart, movieCount)
	if err != nil {
		return nil, 0, err
	}
	if includeRecent {
		items = append([]libraryItem{{ItemID: moviesRecentPrefix + libraryID, Name: "Recently Added", Type: "Folder"}}, items...)
	}
	if total > 0 {
		total++
	} else if len(items) > 0 {
		total = movieStart + int64(len(items))
		if includeRecent {
			total++
		}
	}
	return items, total, nil
}

func (m *Module) fetchMovies(libraryID string, start int64, count int64) ([]libraryItem, int64, error) {
	endpoint := fmt.Sprintf("/Users/%s/Items", url.PathEscape(m.config.UserID))
	params := url.Values{}
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	params.Set("Recursive", "true")
	params.Set("Fields", browseFieldsWithCollection())
	params.Set("IncludeItemTypes", "Movie")
	if strings.TrimSpace(libraryID) != "" {
		params.Set("ParentId", libraryID)
	}
	return m.fetchItemsWithParams(endpoint, params)
}

func (m *Module) fetchRecentMovies(libraryID string, start int64, count int64) ([]libraryItem, int64, error) {
	endpoint := fmt.Sprintf("/Users/%s/Items", url.PathEscape(m.config.UserID))
	params := url.Values{}
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	params.Set("Recursive", "true")
	params.Set("SortBy", "DateCreated")
	params.Set("SortOrder", "Descending")
	params.Set("Fields", browseFieldsWithCollection())
	params.Set("IncludeItemTypes", "Movie")
	if strings.TrimSpace(libraryID) != "" {
		params.Set("ParentId", libraryID)
	}
	return m.fetchItemsWithParams(endpoint, params)
}

func (m *Module) fetchRecentMusic(libraryID string, start int64, count int64) ([]libraryItem, int64, error) {
	endpoint := fmt.Sprintf("/Users/%s/Items", url.PathEscape(m.config.UserID))
	params := url.Values{}
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	params.Set("Recursive", "true")
	params.Set("SortBy", "DateCreated")
	params.Set("SortOrder", "Descending")
	params.Set("Fields", browseFieldsWithCollection())
	params.Set("IncludeItemTypes", "Audio,MusicAlbum")
	if strings.TrimSpace(libraryID) != "" {
		params.Set("ParentId", libraryID)
	}
	return m.fetchItemsWithParams(endpoint, params)
}

func (m *Module) fetchItemsRaw(containerID string, start int64, count int64, search string, types []string, recursive bool) ([]libraryItem, int64, error) {
	endpoint := fmt.Sprintf("/Users/%s/Items", url.PathEscape(m.config.UserID))
	params := url.Values{}
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	if recursive {
		params.Set("Recursive", "true")
	}
	params.Set("Fields", browseFieldsWithCollection())
	typesParam := "Audio,MusicAlbum,MusicArtist,Movie,Series,Episode,Video"
	if len(types) > 0 {
		typesParam = strings.Join(types, ",")
	}
	params.Set("IncludeItemTypes", typesParam)
	if containerID != "" {
		params.Set("ParentId", containerID)
	}
	if search != "" {
		params.Set("SearchTerm", search)
	}
	return m.fetchItemsWithParams(endpoint, params)
}

func (m *Module) fetchItemsWithParams(endpoint string, params url.Values) ([]libraryItem, int64, error) {
	if params.Get("EnableTotalRecordCount") == "" {
		params.Set("EnableTotalRecordCount", "true")
	}
	var resp jfItemsResponse
	if err := m.doJSON("GET", endpoint, params, nil, &resp); err != nil {
		return nil, 0, err
	}
	items := make([]libraryItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		items = append(items, m.libraryItemFromJF(item))
	}
	start := parseIntParam(params.Get("StartIndex"))
	limit := parseIntParam(params.Get("Limit"))
	rawTotal := resp.TotalRecordCount
	total := adjustTotalCount(params, rawTotal, int64(len(items)))
	if rawTotal == 0 && total <= start+int64(len(items)) && int64(len(items)) > 0 {
		if m.hasMoreItems(endpoint, params, start+int64(len(items)), limit) {
			total = start + int64(len(items)) + 1
		}
	}
	return items, total, nil
}

func adjustTotalCount(params url.Values, total int64, count int64) int64 {
	start := parseIntParam(params.Get("StartIndex"))
	limit := parseIntParam(params.Get("Limit"))
	if count == 0 {
		return total
	}
	if limit > 0 && count >= limit && total <= start+count {
		return start + count + 1
	}
	if total == 0 {
		return start + count
	}
	return total
}

func parseIntParam(value string) int64 {
	if value == "" {
		return 0
	}
	out, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return out
}

func (m *Module) hasMoreItems(endpoint string, params url.Values, start int64, limit int64) bool {
	nextParams := cloneValues(params)
	nextParams.Set("StartIndex", fmt.Sprintf("%d", start))
	if limit > 0 {
		nextParams.Set("Limit", "1")
	}
	var resp jfItemsResponse
	if err := m.doJSON("GET", endpoint, nextParams, nil, &resp); err != nil {
		return false
	}
	return len(resp.Items) > 0
}

func cloneValues(src url.Values) url.Values {
	out := url.Values{}
	for key, vals := range src {
		copied := make([]string, len(vals))
		copy(copied, vals)
		out[key] = copied
	}
	return out
}

func (m *Module) libraryItemFromJF(item jfItem) libraryItem {
	imageTag := ""
	if item.ImageTags != nil {
		imageTag = item.ImageTags["Primary"]
	}
	imageURL := ""
	if imageTag != "" {
		imageURL = m.imageURL(item.ID)
	}
	return libraryItem{
		ItemID:      item.ID,
		Name:        item.Name,
		Type:        item.Type,
		MediaType:   item.MediaType,
		Artists:     item.Artists,
		Album:       item.Album,
		ContainerID: item.ParentID,
		Overview:    item.Overview,
		DurationMS:  ticksToMS(item.RunTimeTicks),
		ImageURL:    imageURL,
	}
}

const (
	musicArtistPrefix  = "music:artist:"
	musicGenrePrefix   = "music:genre:"
	musicRecentPrefix  = "music:recent:"
	musicFilesPrefix   = "music:files:"
	moviesRecentPrefix = "movies:recent:"
)

func isMusicLibrary(item jfItem) bool {
	return strings.EqualFold(item.Type, "CollectionFolder") && strings.EqualFold(item.CollectionType, "music")
}

func isMovieLibrary(item jfItem) bool {
	return strings.EqualFold(item.Type, "CollectionFolder") && strings.EqualFold(item.CollectionType, "movies")
}

func (m *Module) musicLibraryRootItems(libraryID string) []libraryItem {
	return []libraryItem{
		{ItemID: musicArtistPrefix + libraryID, Name: "Artist", Type: "Folder"},
		{ItemID: musicGenrePrefix + libraryID, Name: "Genre", Type: "Folder"},
		{ItemID: musicRecentPrefix + libraryID, Name: "Recently Added", Type: "Folder"},
		{ItemID: musicFilesPrefix + libraryID, Name: "Files", Type: "Folder"},
	}
}

func parseFilesContainer(containerID string) (string, string) {
	trimmed := strings.TrimPrefix(containerID, musicFilesPrefix)
	parts := strings.SplitN(trimmed, ":", 2)
	libraryID := parts[0]
	if len(parts) == 2 {
		return libraryID, parts[1]
	}
	return libraryID, ""
}

func isFilesContainerType(itemType string) bool {
	switch strings.ToLower(itemType) {
	case "folder", "musicalbum", "musicartist", "musicgenre", "collectionfolder":
		return true
	default:
		return false
	}
}

func (m *Module) fetchMusicArtists(libraryID string, start int64, count int64) ([]libraryItem, int64, error) {
	endpoint := "/Artists"
	params := url.Values{}
	params.Set("UserId", m.config.UserID)
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	params.Set("Recursive", "true")
	params.Set("Fields", browseFieldsWithCollection())
	if strings.TrimSpace(libraryID) != "" {
		params.Set("ParentId", libraryID)
	}
	return m.fetchItemsWithParams(endpoint, params)
}

func (m *Module) fetchMusicGenres(libraryID string, start int64, count int64) ([]libraryItem, int64, error) {
	endpoint := "/Genres"
	params := url.Values{}
	params.Set("UserId", m.config.UserID)
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	params.Set("Recursive", "true")
	params.Set("Fields", browseFieldsWithCollection())
	if strings.TrimSpace(libraryID) != "" {
		params.Set("ParentId", libraryID)
	}
	return m.fetchItemsWithParams(endpoint, params)
}

func (m *Module) fetchMusicAlbumsByArtist(artistID string, start int64, count int64) ([]libraryItem, int64, error) {
	endpoint := fmt.Sprintf("/Users/%s/Items", url.PathEscape(m.config.UserID))
	params := url.Values{}
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	params.Set("Recursive", "true")
	params.Set("Fields", browseFieldsWithCollection())
	params.Set("IncludeItemTypes", "MusicAlbum")
	params.Set("ArtistIds", artistID)
	return m.fetchItemsWithParams(endpoint, params)
}

func (m *Module) fetchMusicAlbumsByGenre(genreID string, start int64, count int64) ([]libraryItem, int64, error) {
	endpoint := fmt.Sprintf("/Users/%s/Items", url.PathEscape(m.config.UserID))
	params := url.Values{}
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	params.Set("Recursive", "true")
	params.Set("Fields", browseFieldsWithCollection())
	params.Set("IncludeItemTypes", "MusicAlbum")
	params.Set("GenreIds", genreID)
	return m.fetchItemsWithParams(endpoint, params)
}

func (m *Module) fetchAlbumTracks(albumID string, start int64, count int64) ([]libraryItem, int64, error) {
	children, err := m.fetchChildItems(albumID, start, count)
	if err != nil {
		return nil, 0, err
	}
	items := make([]libraryItem, 0, len(children))
	for _, child := range children {
		items = append(items, m.libraryItemFromJF(child))
	}
	return items, int64(len(items)), nil
}

func (m *Module) fetchChildItems(parentID string, start int64, count int64) ([]jfItem, error) {
	endpoint := fmt.Sprintf("/Users/%s/Items", url.PathEscape(m.config.UserID))
	params := url.Values{}
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	params.Set("Recursive", "true")
	params.Set("Fields", browseFieldsNoCollection())
	params.Set("IncludeItemTypes", "Audio,Video")
	params.Set("ParentId", parentID)

	var resp jfItemsResponse
	if err := m.doJSON("GET", endpoint, params, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (m *Module) fetchItem(itemID string) (jfItem, error) {
	endpoint := fmt.Sprintf("/Items/%s", url.PathEscape(itemID))
	params := url.Values{}
	params.Set("UserId", m.config.UserID)
	params.Set("Fields", resolveFields())

	var item jfItem
	if err := m.doJSON("GET", endpoint, params, nil, &item); err != nil {
		return jfItem{}, err
	}
	item.ID = itemID
	if item.ImageTags != nil {
		item.PrimaryImageTag = item.ImageTags["Primary"]
	}
	return item, nil
}

func (m *Module) resolveSource(itemID string, item jfItem) (mu.ResolvedSource, error) {
	endpoint := fmt.Sprintf("/Items/%s/PlaybackInfo", url.PathEscape(itemID))
	params := url.Values{}
	params.Set("UserId", m.config.UserID)

	var info jfPlaybackInfo
	if err := m.doJSON("POST", endpoint, params, map[string]any{}, &info); err == nil && len(info.MediaSources) > 0 {
		source := info.MediaSources[0]
		streamURL := source.DirectStreamURL
		if streamURL == "" {
			streamURL = m.streamURL(itemID, item)
		}
		return mu.ResolvedSource{
			URL:       m.absoluteURL(streamURL),
			Mime:      mimeForContainer(item, source.Container),
			ByteRange: source.SupportsDirectStream,
		}, nil
	}

	return mu.ResolvedSource{
		URL:       m.streamURL(itemID, item),
		Mime:      mimeForContainer(item, ""),
		ByteRange: false,
	}, nil
}

func (m *Module) resolveSources(item jfItem) ([]mu.ResolvedSource, map[string]any, error) {
	if !isContainerItem(item) {
		source, err := m.resolveSource(item.ID, item)
		if err != nil {
			return nil, nil, err
		}
		return []mu.ResolvedSource{source}, nil, nil
	}

	var children []jfItem
	var err error
	if strings.EqualFold(item.Type, "Playlist") {
		children, err = m.fetchPlaylistItems(item.ID, 0, 500)
	} else {
		children, err = m.fetchChildItems(item.ID, 0, 500)
	}
	if err != nil {
		return nil, nil, err
	}
	if len(children) == 0 {
		return []mu.ResolvedSource{}, nil, nil
	}

	sources := make([]mu.ResolvedSource, 0, len(children))
	meta := map[string]any{}
	for _, child := range children {
		if isContainerItem(child) {
			continue
		}
		if child.MediaType != "" {
			meta["mediaType"] = child.MediaType
		}
		if child.Type != "" {
			meta["type"] = child.Type
		}
		if child.RunTimeTicks > 0 {
			meta["durationMs"] = ticksToMS(child.RunTimeTicks)
		}
		if len(child.Artists) > 0 {
			meta["artist"] = strings.Join(child.Artists, ", ")
			meta["artists"] = child.Artists
		} else if child.AlbumArtist != "" {
			meta["artist"] = child.AlbumArtist
		}
		if child.Album != "" {
			meta["album"] = child.Album
		}
		if child.Name != "" {
			meta["title"] = child.Name
		}
		source, err := m.resolveSource(child.ID, child)
		if err != nil {
			return nil, nil, err
		}
		// Include the child's itemId in the source for proper metadata resolution
		source.ItemID = child.ID
		sources = append(sources, source)
	}
	if len(sources) == 0 {
		return []mu.ResolvedSource{}, meta, nil
	}
	return sources, meta, nil
}

func (m *Module) fetchPlaylistItems(playlistID string, start int64, count int64) ([]jfItem, error) {
	endpoint := fmt.Sprintf("/Playlists/%s/Items", url.PathEscape(playlistID))
	params := url.Values{}
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	params.Set("UserId", m.config.UserID)
	params.Set("Fields", browseFieldsNoCollection())

	var resp jfItemsResponse
	if err := m.doJSON("GET", endpoint, params, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func browseFieldsWithCollection() string {
	return "PrimaryImageAspectRatio,RunTimeTicks,Artists,Album,AlbumArtist,ImageTags,CollectionType"
}

func browseFieldsNoCollection() string {
	return "PrimaryImageAspectRatio,RunTimeTicks,Artists,Album,AlbumArtist,ImageTags"
}

func resolveFields() string {
	return "PrimaryImageAspectRatio,RunTimeTicks,Overview,Artists,Album,AlbumArtist,ImageTags,CollectionType"
}

func browseCacheKey(kind string, containerID string, start int64, count int64, query string) string {
	return fmt.Sprintf("browse:%s:%s:%d:%d:%s", kind, containerID, start, count, query)
}

func (m *Module) shouldShedLoad() bool {
	until := atomic.LoadInt64(&m.publishTimeoutUntil)
	if until == 0 {
		return false
	}
	return time.Now().UnixNano() < until
}

func (m *Module) markPublishTimeout() {
	until := time.Now().Add(m.config.PublishTimeoutCooldown).UnixNano()
	atomic.StoreInt64(&m.publishTimeoutUntil, until)
}

func (m *Module) doJSON(method string, endpoint string, params url.Values, body any, out any) error {
	m.acquireRequest()
	defer m.releaseRequest()

	started := time.Now()
	log := m.log
	if log == nil {
		log = zap.NewNop()
	}
	log.Debug(
		"jellyfin request",
		zap.String("method", method),
		zap.String("endpoint", endpoint),
		zap.String("params", formatQueryParams(params)),
		zap.Int("inflight", m.inflightRequests()),
	)
	endpointURL := m.config.BaseURL + endpoint
	if len(params) > 0 {
		endpointURL += "?" + params.Encode()
	}

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	req, err := http.NewRequest(method, endpointURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", m.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.http.Do(req)
	if err != nil {
		log.Debug(
			"jellyfin request failed",
			zap.String("method", method),
			zap.String("endpoint", endpoint),
			zap.Duration("duration", time.Since(started)),
			zap.Error(err),
		)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Debug(
			"jellyfin request error",
			zap.String("method", method),
			zap.String("endpoint", endpoint),
			zap.Duration("duration", time.Since(started)),
			zap.Int("status", resp.StatusCode),
		)
		return fmt.Errorf("jellyfin error: %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		log.Debug(
			"jellyfin decode error",
			zap.String("method", method),
			zap.String("endpoint", endpoint),
			zap.Duration("duration", time.Since(started)),
			zap.Error(err),
		)
		return err
	}
	log.Debug(
		"jellyfin request ok",
		zap.String("method", method),
		zap.String("endpoint", endpoint),
		zap.Duration("duration", time.Since(started)),
		zap.Int("status", resp.StatusCode),
	)
	return nil
}

func (m *Module) acquireRequest() {
	if m.reqSem == nil {
		return
	}
	m.reqSem <- struct{}{}
}

func (m *Module) releaseRequest() {
	if m.reqSem == nil {
		return
	}
	select {
	case <-m.reqSem:
	default:
	}
}

func (m *Module) inflightRequests() int {
	if m.reqSem == nil {
		return 0
	}
	return len(m.reqSem)
}

func formatQueryParams(params url.Values) string {
	if len(params) == 0 {
		return ""
	}
	keys := []string{
		"StartIndex",
		"Limit",
		"ParentId",
		"SearchTerm",
		"IncludeItemTypes",
		"Fields",
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := params.Get(key); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	return strings.Join(parts, " ")
}

func (m *Module) imageURL(itemID string) string {
	u, _ := url.Parse(m.artworkBaseURL())
	u.Path = path.Join(u.Path, "/Items/", itemID, "/Images/Primary")
	q := u.Query()
	q.Set("maxHeight", "500")
	q.Set("maxWidth", "500")
	q.Set("quality", "90")
	q.Set("api_key", m.config.APIKey)
	u.RawQuery = q.Encode()
	return u.String()
}

func (m *Module) downloadURL(itemID string) string {
	u, _ := url.Parse(m.streamBaseURL())
	u.Path = path.Join(u.Path, "/Items/", itemID, "/Download")
	q := u.Query()
	q.Set("api_key", m.config.APIKey)
	u.RawQuery = q.Encode()
	return u.String()
}

func (m *Module) streamURL(itemID string, item jfItem) string {
	u, _ := url.Parse(m.streamBaseURL())
	prefix := "Videos"
	if strings.EqualFold(item.MediaType, "Audio") {
		prefix = "Audio"
	}
	u.Path = path.Join(u.Path, "/", prefix, "/", itemID, "/stream")
	q := u.Query()
	q.Set("static", "true")
	q.Set("api_key", m.config.APIKey)
	u.RawQuery = q.Encode()
	return u.String()
}

func (m *Module) absoluteURL(streamURL string) string {
	if strings.HasPrefix(streamURL, "http://") || strings.HasPrefix(streamURL, "https://") {
		return streamURL
	}
	return m.streamBaseURL() + streamURL
}

func (m *Module) streamBaseURL() string {
	if strings.TrimSpace(m.config.StreamBaseURL) != "" {
		return m.config.StreamBaseURL
	}
	return m.config.BaseURL
}

func (m *Module) artworkBaseURL() string {
	if strings.TrimSpace(m.config.ArtworkBaseURL) != "" {
		return m.config.ArtworkBaseURL
	}
	return m.config.BaseURL
}

func ticksToMS(ticks int64) int64 {
	if ticks <= 0 {
		return 0
	}
	return ticks / 10000
}

func isContainerItem(item jfItem) bool {
	if strings.EqualFold(item.Type, "Playlist") {
		return true
	}
	if strings.EqualFold(item.MediaType, "Audio") || strings.EqualFold(item.MediaType, "Video") {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "musicalbum", "musicartist", "album", "artist", "series", "season", "boxset", "folder", "collectionfolder", "playlist":
		return true
	default:
		return false
	}
}

func mapLibraryTypes(types []string) ([]string, error) {
	if len(types) == 0 {
		return nil, nil
	}
	allowed := map[string][]string{
		"audio":       {"Audio"},
		"musicalbum":  {"MusicAlbum"},
		"musicartist": {"MusicArtist"},
		"movie":       {"Movie"},
		"series":      {"Series"},
		"episode":     {"Episode"},
		"video":       {"Video"},
		"playlist":    {"Playlist"},
		"folder":      {"Folder", "CollectionFolder"},
	}
	out := make([]string, 0, len(types))
	seen := map[string]bool{}
	for _, t := range types {
		key := strings.ToLower(strings.TrimSpace(t))
		if key == "" {
			continue
		}
		mapped, ok := allowed[key]
		if !ok {
			return nil, fmt.Errorf("unsupported type %q", t)
		}
		for _, v := range mapped {
			if !seen[v] {
				out = append(out, v)
				seen[v] = true
			}
		}
	}
	return out, nil
}

func mimeForContainer(item jfItem, container string) string {
	container = strings.TrimSpace(container)
	if container == "" {
		return ""
	}
	mediaType := strings.ToLower(item.MediaType)
	if mediaType == "video" {
		return "video/" + container
	}
	return "audio/" + container
}

func errorReply(cmd mu.CommandEnvelope, code string, message string) mu.ReplyEnvelope {
	return mu.ReplyEnvelope{
		ID:   cmd.ID,
		Type: "error",
		OK:   false,
		TS:   time.Now().Unix(),
		Err: &mu.ReplyError{
			Code:    code,
			Message: message,
		},
	}
}
