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
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

// Config configures the Jellyfin library bridge.
type Config struct {
	NodeID    string
	TopicBase string
	Name      string
	BaseURL   string
	APIKey    string
	UserID    string
	Timeout   time.Duration
	CacheTTL  time.Duration
	CacheSize int
}

// Module handles library commands via Jellyfin.
type Module struct {
	log      *zap.Logger
	client   *mqttserver.Client
	http     *http.Client
	config   Config
	cmdTopic string
	cacheMu  sync.Mutex
	cache    map[string]resolveCacheEntry
}

// NewModule creates a Jellyfin library module.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*Module, error) {
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
	if cfg.CacheSize == 0 {
		cfg.CacheSize = 1000
	}

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	cfg.BaseURL = baseURL

	cmdTopic := mu.TopicCommands(cfg.TopicBase, cfg.NodeID)

	return &Module{
		log:      log,
		client:   client,
		http:     &http.Client{Timeout: cfg.Timeout},
		config:   cfg,
		cmdTopic: cmdTopic,
		cache:    make(map[string]resolveCacheEntry),
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
	default:
		return errorReply(cmd, "INVALID", "unsupported command")
	}
}

func (m *Module) libraryBrowse(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryBrowseBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	items, total, err := m.fetchItems(body.ContainerID, body.Start, body.Count, "", nil, false)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(libraryItemsReply{Items: items, Start: body.Start, Count: int64(len(items)), Total: total})
	reply.Body = payload
	return reply
}

func (m *Module) librarySearch(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibrarySearchBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	types, err := mapLibraryTypes(body.Types)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	items, total, err := m.fetchItems("", body.Start, body.Count, body.Query, types, true)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(libraryItemsReply{Items: items, Start: body.Start, Count: int64(len(items)), Total: total})
	reply.Body = payload
	return reply
}

func (m *Module) libraryResolve(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope) mu.ReplyEnvelope {
	var body mu.LibraryResolveBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	metadata, sources, err := m.resolveItem(body.ItemID, body.MetadataOnly)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(mu.LibraryResolveReply{ItemID: body.ItemID, Metadata: metadata, Sources: sources})
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
	metadata  map[string]any
	sources   []mu.ResolvedSource
	expiresAt time.Time
	addedAt   time.Time
}

func (m *Module) resolveItem(itemID string, metadataOnly bool) (map[string]any, []mu.ResolvedSource, error) {
	if metadata, sources, ok := m.cacheGet(itemID, metadataOnly); ok {
		return metadata, sources, nil
	}
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
	m.cachePut(itemID, metadata, sources)
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
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if m.cache == nil {
		return nil, nil, false
	}
	entry, ok := m.cache[itemID]
	if !ok {
		return nil, nil, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(m.cache, itemID)
		return nil, nil, false
	}
	if metadataOnly {
		if entry.metadata == nil {
			return nil, nil, false
		}
		return copyMetadata(entry.metadata), nil, true
	}
	if entry.metadata == nil || len(entry.sources) == 0 {
		return nil, nil, false
	}
	return copyMetadata(entry.metadata), append([]mu.ResolvedSource(nil), entry.sources...), true
}

func (m *Module) cachePut(itemID string, metadata map[string]any, sources []mu.ResolvedSource) {
	if metadata == nil {
		return
	}
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if m.cache == nil {
		m.cache = make(map[string]resolveCacheEntry)
	}
	now := time.Now()
	m.cache[itemID] = resolveCacheEntry{
		metadata:  copyMetadata(metadata),
		sources:   append([]mu.ResolvedSource(nil), sources...),
		expiresAt: now.Add(m.config.CacheTTL),
		addedAt:   now,
	}
	m.evictCache()
}

func (m *Module) evictCache() {
	if m.config.CacheSize <= 0 || len(m.cache) <= m.config.CacheSize {
		return
	}
	oldestID := ""
	var oldest time.Time
	for id, entry := range m.cache {
		if oldestID == "" || entry.addedAt.Before(oldest) {
			oldestID = id
			oldest = entry.addedAt
		}
	}
	if oldestID != "" {
		delete(m.cache, oldestID)
	}
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
		item, err := m.fetchItem(containerID)
		if err == nil && strings.EqualFold(item.Type, "Playlist") {
			children, err := m.fetchPlaylistItems(containerID, start, count)
			if err != nil {
				return nil, 0, err
			}
			items := make([]libraryItem, 0, len(children))
			for _, child := range children {
				imageTag := ""
				if child.ImageTags != nil {
					imageTag = child.ImageTags["Primary"]
				}
				imageURL := ""
				if imageTag != "" {
					imageURL = m.imageURL(child.ID)
				}
				items = append(items, libraryItem{
					ItemID:      child.ID,
					Name:        child.Name,
					Type:        child.Type,
					MediaType:   child.MediaType,
					Artists:     child.Artists,
					Album:       child.Album,
					ContainerID: child.ParentID,
					Overview:    child.Overview,
					DurationMS:  ticksToMS(child.RunTimeTicks),
					ImageURL:    imageURL,
				})
			}
			return items, int64(len(items)), nil
		}
	}
	endpoint := fmt.Sprintf("/Users/%s/Items", url.PathEscape(m.config.UserID))
	params := url.Values{}
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	if recursive {
		params.Set("Recursive", "true")
	}
	params.Set("Fields", "PrimaryImageAspectRatio,RunTimeTicks,Overview,Artists,Album,AlbumArtist,ImageTags")
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

	var resp jfItemsResponse
	if err := m.doJSON("GET", endpoint, params, nil, &resp); err != nil {
		return nil, 0, err
	}

	items := make([]libraryItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		imageTag := ""
		if item.ImageTags != nil {
			imageTag = item.ImageTags["Primary"]
		}
		imageURL := ""
		if imageTag != "" {
			imageURL = m.imageURL(item.ID)
		}
		items = append(items, libraryItem{
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
		})
	}
	return items, resp.TotalRecordCount, nil
}

func (m *Module) fetchChildItems(parentID string, start int64, count int64) ([]jfItem, error) {
	endpoint := fmt.Sprintf("/Users/%s/Items", url.PathEscape(m.config.UserID))
	params := url.Values{}
	params.Set("StartIndex", fmt.Sprintf("%d", start))
	params.Set("Limit", fmt.Sprintf("%d", count))
	params.Set("Recursive", "true")
	params.Set("Fields", "PrimaryImageAspectRatio,RunTimeTicks,Overview,Artists,Album,AlbumArtist,ImageTags")
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
	params.Set("Fields", "PrimaryImageAspectRatio,RunTimeTicks,Overview,Artists,Album,AlbumArtist,ImageTags")

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
	params.Set("Fields", "PrimaryImageAspectRatio,RunTimeTicks,Overview,Artists,Album,AlbumArtist,ImageTags")

	var resp jfItemsResponse
	if err := m.doJSON("GET", endpoint, params, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (m *Module) doJSON(method string, endpoint string, params url.Values, body any, out any) error {
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
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("jellyfin error: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (m *Module) imageURL(itemID string) string {
	u, _ := url.Parse(m.config.BaseURL)
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
	u, _ := url.Parse(m.config.BaseURL)
	u.Path = path.Join(u.Path, "/Items/", itemID, "/Download")
	q := u.Query()
	q.Set("api_key", m.config.APIKey)
	u.RawQuery = q.Encode()
	return u.String()
}

func (m *Module) streamURL(itemID string, item jfItem) string {
	u, _ := url.Parse(m.config.BaseURL)
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
	return m.config.BaseURL + streamURL
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
