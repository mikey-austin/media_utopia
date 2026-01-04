//go:build upnp

package upnplibrary

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	freecache "github.com/coocood/freecache"
	paho "github.com/eclipse/paho.mqtt.golang"
	gocache "github.com/eko/gocache/lib/v4/cache"
	libstore "github.com/eko/gocache/lib/v4/store"
	gocachefreecache "github.com/eko/gocache/store/freecache/v4"
	"github.com/golang/snappy"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/internal/adapters/pupnp"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

// Enabled indicates the upnp build tag is active.
const Enabled = true

// Config configures the UPnP library bridge.
type Config struct {
	NodeID                 string
	TopicBase              string
	Name                   string
	Listen                 string
	Timeout                time.Duration
	CacheTTL               time.Duration
	CacheSize              int
	CacheCompress          bool
	BrowseCacheTTL         time.Duration
	BrowseCacheSize        int
	MaxConcurrentRequests  int
	PublishTimeoutCooldown time.Duration
	DiscoveryInterval      time.Duration
}

// Module handles library commands via UPnP ContentDirectory.
type Module struct {
	log    *zap.Logger
	client *mqttserver.Client
	http   *http.Client
	upnp   *pupnp.Client
	config Config

	cmdTopic string

	cache          gocache.CacheInterface[[]byte]
	cacheCtx       context.Context
	browseCache    gocache.CacheInterface[[]byte]
	browseCacheCtx context.Context

	reqSem              chan struct{}
	publishTimeoutUntil int64

	serversMu      sync.RWMutex
	servers        map[string]*mediaServer
	discoveryRev   uint64
	lastDiscovery  time.Time
	discoveryStop  chan struct{}
	discoveryOnce  sync.Once
	discoveryError atomic.Value

	subMu         sync.Mutex
	serverCmdSubs map[string]bool
}

// mediaServer represents a discovered UPnP MediaServer.
type mediaServer struct {
	ID                  string
	FriendlyName        string
	Location            string
	BaseURL             string
	ControlURL          string
	ServiceType         string
	IconURL             string
	LastSeen            time.Time
	ContentDirectoryVer string
}

// libraryItemsReply mirrors mu library responses.
type libraryItemsReply struct {
	Items []libraryItem `json:"items"`
	Start int64         `json:"start"`
	Count int64         `json:"count"`
	Total int64         `json:"total"`
}

// libraryItem describes a browse/search item.
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

// NewModule creates a UPnP library module.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*Module, error) {
	if log == nil {
		log = zap.NewNop()
	}
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, errors.New("node_id required")
	}
	if strings.TrimSpace(cfg.TopicBase) == "" {
		cfg.TopicBase = mu.BaseTopic
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = "UPnP Library"
	}
	if strings.TrimSpace(cfg.Listen) == "" {
		cfg.Listen = "0.0.0.0:0"
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
	if cfg.DiscoveryInterval <= 0 {
		cfg.DiscoveryInterval = 5 * time.Minute
	}
	cfg.CacheSize = cacheSizeBytes(cfg.CacheSize)
	cfg.BrowseCacheSize = cacheSizeBytes(cfg.BrowseCacheSize)

	cmdTopic := mu.TopicCommands(cfg.TopicBase, cfg.NodeID)

	upnpClient, err := pupnp.NewClient(cfg.Listen)
	if err != nil {
		return nil, err
	}

	return &Module{
		log:            log,
		client:         client,
		http:           &http.Client{Timeout: cfg.Timeout},
		upnp:           upnpClient,
		config:         cfg,
		cmdTopic:       cmdTopic,
		cache:          newCache(cfg.CacheSize),
		cacheCtx:       context.Background(),
		browseCache:    newCache(cfg.BrowseCacheSize),
		browseCacheCtx: context.Background(),
		reqSem:         make(chan struct{}, cfg.MaxConcurrentRequests),
		servers:        map[string]*mediaServer{},
		discoveryStop:  make(chan struct{}),
		serverCmdSubs:  map[string]bool{},
	}, nil
}

// Run starts the module.
func (m *Module) Run(ctx context.Context) error {
	defer m.upnp.Close()

	if err := m.publishPresence(); err != nil {
		return err
	}

	m.discoveryOnce.Do(func() {
		go m.discoveryLoop(ctx)
	})

	handler := func(_ paho.Client, msg paho.Message) {
		m.handleMessage(msg)
	}

	if err := m.client.Subscribe(m.cmdTopic, 1, handler); err != nil {
		return err
	}
	// Wildcard to catch per-server commands (nodeId:<serverID>/cmd).
	wildcardTopic := fmt.Sprintf("%s/node/+/cmd", m.config.TopicBase)
	if err := m.client.Subscribe(wildcardTopic, 1, handler); err != nil {
		m.log.Debug("upnp wildcard subscribe failed", zap.String("topic", wildcardTopic), zap.Error(err))
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

func (m *Module) discoveryLoop(ctx context.Context) {
	m.refreshServers(context.Background())
	ticker := time.NewTicker(m.config.DiscoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.discoveryStop:
			return
		case <-ticker.C:
			m.refreshServers(context.Background())
		}
	}
}

func (m *Module) handleMessage(msg paho.Message) {
	if !m.topicMatches(msg.Topic()) {
		return
	}
	var cmd mu.CommandEnvelope
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		m.log.Warn("invalid command", zap.Error(err))
		return
	}

	started := time.Now()
	serverID := m.serverIDFromTopic(msg.Topic())
	reply := m.dispatch(cmd, serverID)
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
		m.log.Debug("command handled",
			zap.String("type", cmd.Type),
			zap.Duration("duration", time.Since(started)),
			zap.Int("bytes", len(payload)),
			zap.Bool("ok", reply.OK),
		)
	}
}

func (m *Module) dispatch(cmd mu.CommandEnvelope, serverID string) mu.ReplyEnvelope {
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
		return m.libraryResolve(cmd, reply, serverID)
	case "library.resolveBatch":
		return m.libraryResolveBatch(cmd, reply, serverID)
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
	if m.needsDiscoveryRefresh() {
		m.refreshServers(context.Background())
	}
	started := time.Now()
	cacheKey := browseCacheKey("browse", body.ContainerID, body.Start, body.Count, "", m.discoveryRevision())
	if cached, ok := m.browseCacheGet(cacheKey); ok {
		reply.Body = cached
		return reply
	}

	items, total, err := m.browseItems(body.ContainerID, body.Start, body.Count)
	if err != nil {
		m.log.Debug("library browse failed",
			zap.String("container", body.ContainerID),
			zap.Error(err),
		)
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(libraryItemsReply{Items: items, Start: body.Start, Count: int64(len(items)), Total: total})
	m.log.Debug(
		"library browse ok",
		zap.String("container", body.ContainerID),
		zap.Int("items", len(items)),
		zap.Int64("total", total),
		zap.Int("bytes", len(payload)),
		zap.Duration("duration", time.Since(started)),
	)
	m.browseCachePut(cacheKey, payload)
	reply.Body = payload
	return reply
}

func (m *Module) browseItems(containerID string, start int64, count int64) ([]libraryItem, int64, error) {
	if containerID == "" {
		servers := m.listServers()
		items := make([]libraryItem, 0, len(servers))
		for _, srv := range servers {
			items = append(items, libraryItem{
				ItemID: makeContainerID(srv.ID, "0"),
				Name:   srv.FriendlyName,
				Type:   "Folder",
				ImageURL: func() string {
					if srv.IconURL == "" {
						return ""
					}
					return srv.IconURL
				}(),
			})
		}
		return paginateItems(items, start, count, int64(len(items)))
	}

	serverID, objectID, ok := parseItemID(containerID)
	if !ok {
		return nil, 0, errors.New("invalid containerId")
	}
	server := m.serverByID(serverID)
	if server == nil {
		return nil, 0, errors.New("library not found")
	}
	return m.fetchChildren(server, objectID, start, count)
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
	if m.needsDiscoveryRefresh() {
		m.refreshServers(context.Background())
	}
	started := time.Now()
	cacheKey := browseCacheKey("search", "", body.Start, body.Count, body.Query, m.discoveryRevision())
	if cached, ok := m.browseCacheGet(cacheKey); ok {
		reply.Body = cached
		return reply
	}
	items, total, err := m.searchItems(body.Query, body.Start, body.Count)
	if err != nil {
		m.log.Debug("library search failed", zap.String("query", body.Query), zap.Error(err))
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(libraryItemsReply{Items: items, Start: body.Start, Count: int64(len(items)), Total: total})
	m.log.Debug(
		"library search ok",
		zap.String("query", body.Query),
		zap.Int("items", len(items)),
		zap.Int64("total", total),
		zap.Int("bytes", len(payload)),
		zap.Duration("duration", time.Since(started)),
	)
	m.browseCachePut(cacheKey, payload)
	reply.Body = payload
	return reply
}

func (m *Module) libraryResolve(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope, serverID string) mu.ReplyEnvelope {
	var body mu.LibraryResolveBody
	if err := json.Unmarshal(cmd.Body, &body); err != nil {
		return errorReply(cmd, "INVALID", "invalid body")
	}
	body.ItemID = m.normalizeItemID(body.ItemID, serverID)
	started := time.Now()
	metadata, sources, err := m.resolveItem(body.ItemID, body.MetadataOnly)
	if err != nil {
		m.log.Debug(
			"library resolve failed",
			zap.String("item", body.ItemID),
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

func (m *Module) libraryResolveBatch(cmd mu.CommandEnvelope, reply mu.ReplyEnvelope, serverID string) mu.ReplyEnvelope {
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
		itemID = m.normalizeItemID(itemID, serverID)
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

// resolveItem resolves a single item into metadata and sources with caching.
func (m *Module) resolveItem(itemID string, metadataOnly bool) (map[string]any, []mu.ResolvedSource, error) {
	if metadata, sources, ok := m.cacheGet(itemID, metadataOnly); ok {
		m.log.Debug("upnp cache hit", zap.String("item", itemID), zap.Bool("metadata_only", metadataOnly))
		return metadata, sources, nil
	}
	serverID, objectID, ok := parseItemID(itemID)
	if !ok {
		return nil, nil, errors.New("invalid itemId")
	}
	server := m.serverByID(serverID)
	if server == nil {
		return nil, nil, errors.New("library not found")
	}
	obj, err := m.fetchObject(server, objectID)
	if err != nil {
		return nil, nil, err
	}
	if isContainerObject(obj) {
		return nil, nil, fmt.Errorf("item %s is a container", itemID)
	}
	metadata := m.buildMetadata(server, obj)
	sources := []mu.ResolvedSource{}
	if !metadataOnly {
		sources = buildSources(server, obj)
	}
	m.cachePut(itemID, metadata, sources, !metadataOnly)
	return metadata, sources, nil
}

// Discovery and browse helpers.

func (m *Module) refreshServers(ctx context.Context) {
	if m.upnp == nil {
		return
	}
	started := time.Now()
	results, err := m.upnp.Discover(ctx, "urn:schemas-upnp-org:device:MediaServer:1", 3*time.Second)
	if err != nil {
		m.discoveryError.Store(err)
		m.log.Debug("pupnp discover failed", zap.Error(err))
		return
	}
	now := time.Now()
	seen := map[string]bool{}
	for _, res := range results {
		server, derr := m.describeServer(ctx, res.Location)
		if derr != nil {
			m.log.Debug("describe server failed", zap.String("location", res.Location), zap.Error(derr))
			continue
		}
		seen[server.ID] = true
		m.serversMu.Lock()
		existing, ok := m.servers[server.ID]
		if !ok || existing.ControlURL != server.ControlURL || existing.FriendlyName != server.FriendlyName {
			m.discoveryRev++
			m.servers[server.ID] = server
			m.ensureServerSubscription(server.ID)
		} else {
			existing.LastSeen = now
		}
		m.serversMu.Unlock()
	}
	m.serversMu.Lock()
	for id, srv := range m.servers {
		if seen[id] {
			continue
		}
		if now.Sub(srv.LastSeen) > 15*time.Minute {
			delete(m.servers, id)
			m.discoveryRev++
		}
	}
	m.lastDiscovery = now
	m.log.Debug("pupnp discovery refreshed",
		zap.Int("results", len(results)),
		zap.Int("servers", len(m.servers)),
		zap.Duration("duration", time.Since(started)),
	)
	m.serversMu.Unlock()
}

func (m *Module) describeServer(ctx context.Context, location string) (*mediaServer, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", location, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("device description error: %s", resp.Status)
	}
	var desc deviceDescription
	if err := xml.NewDecoder(resp.Body).Decode(&desc); err != nil {
		return nil, err
	}
	service, ok := desc.ContentDirectory()
	if !ok {
		return nil, errors.New("content directory not found")
	}

	base := desc.BaseURL(location)
	controlURL := resolveURL(base, service.ControlURL)
	iconURL := desc.IconURL(base)
	serverID := strings.TrimPrefix(desc.Device.UDN, "uuid:")

	return &mediaServer{
		ID:                  serverID,
		FriendlyName:        desc.Device.FriendlyName,
		Location:            location,
		BaseURL:             base,
		ControlURL:          controlURL,
		ServiceType:         service.ServiceType,
		IconURL:             iconURL,
		LastSeen:            time.Now(),
		ContentDirectoryVer: service.ServiceType,
	}, nil
}

func (m *Module) needsDiscoveryRefresh() bool {
	m.serversMu.RLock()
	defer m.serversMu.RUnlock()
	if len(m.servers) == 0 {
		return true
	}
	return time.Since(m.lastDiscovery) > m.config.DiscoveryInterval
}

func (m *Module) listServers() []*mediaServer {
	m.serversMu.RLock()
	defer m.serversMu.RUnlock()
	out := make([]*mediaServer, 0, len(m.servers))
	for _, srv := range m.servers {
		out = append(out, srv)
	}
	return out
}

func (m *Module) serverByID(id string) *mediaServer {
	m.serversMu.RLock()
	defer m.serversMu.RUnlock()
	return m.servers[id]
}

func (m *Module) ensureServerSubscription(serverID string) {
	if m.client == nil {
		return
	}
	m.subMu.Lock()
	defer m.subMu.Unlock()
	if m.serverCmdSubs[serverID] {
		return
	}
	topic := mu.TopicCommands(m.config.TopicBase, m.config.NodeID+":"+serverID)
	if err := m.client.Subscribe(topic, 1, func(_ paho.Client, msg paho.Message) {
		m.handleMessage(msg)
	}); err == nil {
		m.serverCmdSubs[serverID] = true
	} else {
		m.log.Debug("server command subscribe failed", zap.String("topic", topic), zap.Error(err))
	}
}

// ContentDirectory operations.

func (m *Module) fetchChildren(server *mediaServer, objectID string, start int64, count int64) ([]libraryItem, int64, error) {
	resp, err := m.contentDirectoryBrowse(server, objectID, start, count, false)
	if err != nil {
		return nil, 0, err
	}
	items := make([]libraryItem, 0, len(resp.Containers)+len(resp.Items))
	for _, obj := range resp.Containers {
		items = append(items, m.libraryItemFromObject(server, obj))
	}
	for _, obj := range resp.Items {
		items = append(items, m.libraryItemFromObject(server, obj))
	}
	total := resp.TotalMatches
	if total == 0 && len(items) > 0 {
		total = start + int64(len(items))
	}
	return items, total, nil
}

func (m *Module) fetchObject(server *mediaServer, objectID string) (didlObject, error) {
	resp, err := m.contentDirectoryBrowse(server, objectID, 0, 1, true)
	if err != nil {
		return didlObject{}, err
	}
	if len(resp.Items) > 0 {
		return resp.Items[0], nil
	}
	if len(resp.Containers) > 0 {
		return resp.Containers[0], nil
	}
	return didlObject{}, errors.New("item not found")
}

func (m *Module) searchItems(query string, start int64, count int64) ([]libraryItem, int64, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return paginateItems(nil, start, count, 0)
	}
	servers := m.listServers()
	if len(servers) == 0 {
		return nil, 0, errors.New("no libraries available")
	}
	all := []libraryItem{}
	reqCount := count
	if reqCount <= 0 {
		reqCount = 100
	}
	desired := start + reqCount
	if desired <= 0 {
		desired = reqCount
	}
	for _, srv := range servers {
		resp, err := m.contentDirectorySearch(srv, query, 0, desired)
		if err != nil {
			m.log.Debug("content directory search failed", zap.String("server", srv.FriendlyName), zap.Error(err))
			continue
		}
		for _, obj := range resp.Items {
			all = append(all, m.libraryItemFromObject(srv, obj))
		}
		for _, obj := range resp.Containers {
			all = append(all, m.libraryItemFromObject(srv, obj))
		}
	}
	total := int64(len(all))
	return paginateItems(all, start, count, total)
}

func (m *Module) contentDirectoryBrowse(server *mediaServer, objectID string, start int64, count int64, metadata bool) (didlResponse, error) {
	m.acquireRequest()
	defer m.releaseRequest()

	browseFlag := "BrowseDirectChildren"
	if metadata {
		browseFlag = "BrowseMetadata"
	}
	envelope := buildBrowseEnvelope(server.ServiceType, objectID, browseFlag, start, count)
	req, err := http.NewRequest("POST", server.ControlURL, bytes.NewReader(envelope))
	if err != nil {
		return didlResponse{}, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", fmt.Sprintf(`"%s#Browse"`, server.ServiceType))

	started := time.Now()
	resp, err := m.http.Do(req)
	if err != nil {
		m.log.Debug("content directory browse failed",
			zap.String("server", server.FriendlyName),
			zap.String("object", objectID),
			zap.String("flag", browseFlag),
			zap.Error(err),
		)
		return didlResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		m.log.Debug("content directory browse http error",
			zap.String("server", server.FriendlyName),
			zap.String("object", objectID),
			zap.Int("status", resp.StatusCode),
		)
		return didlResponse{}, fmt.Errorf("content directory error: %s", resp.Status)
	}
	var env browseResponseEnvelope
	if err := xml.NewDecoder(resp.Body).Decode(&env); err != nil {
		m.log.Debug("content directory browse decode error",
			zap.String("server", server.FriendlyName),
			zap.String("object", objectID),
			zap.Error(err),
		)
		return didlResponse{}, err
	}
	if env.Body.Fault != nil {
		return didlResponse{}, fmt.Errorf("content directory fault: %s", env.Body.Fault.Error())
	}
	result := env.Body.BrowseResponse
	var didl didlLite
	if err := xml.Unmarshal([]byte(result.Result), &didl); err != nil {
		return didlResponse{}, err
	}
	m.log.Debug("content directory browse ok",
		zap.String("server", server.FriendlyName),
		zap.String("object", objectID),
		zap.String("flag", browseFlag),
		zap.Int64("returned", result.NumberReturned),
		zap.Int64("total", result.TotalMatches),
		zap.Duration("duration", time.Since(started)),
	)
	return didlResponse{
		Items:          didl.Items,
		Containers:     didl.Containers,
		NumberReturned: result.NumberReturned,
		TotalMatches:   result.TotalMatches,
	}, nil
}

func (m *Module) contentDirectorySearch(server *mediaServer, query string, start int64, count int64) (didlResponse, error) {
	m.acquireRequest()
	defer m.releaseRequest()

	criteria := searchCriteria(query)
	envelope := buildSearchEnvelope(server.ServiceType, "0", criteria, start, count)
	req, err := http.NewRequest("POST", server.ControlURL, bytes.NewReader(envelope))
	if err != nil {
		return didlResponse{}, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", fmt.Sprintf(`"%s#Search"`, server.ServiceType))

	started := time.Now()
	resp, err := m.http.Do(req)
	if err != nil {
		m.log.Debug("content directory search failed",
			zap.String("server", server.FriendlyName),
			zap.String("query", query),
			zap.Error(err),
		)
		return didlResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		m.log.Debug("content directory search http error",
			zap.String("server", server.FriendlyName),
			zap.String("query", query),
			zap.Int("status", resp.StatusCode),
		)
		return didlResponse{}, nil
	}
	var env searchResponseEnvelope
	if err := xml.NewDecoder(resp.Body).Decode(&env); err != nil {
		m.log.Debug("content directory search decode error",
			zap.String("server", server.FriendlyName),
			zap.String("query", query),
			zap.Error(err),
		)
		return didlResponse{}, err
	}
	if env.Body.Fault != nil {
		m.log.Debug("content directory search fault",
			zap.String("server", server.FriendlyName),
			zap.String("query", query),
			zap.String("fault", env.Body.Fault.Error()),
		)
		return didlResponse{}, nil
	}
	result := env.Body.SearchResponse
	var didl didlLite
	if err := xml.Unmarshal([]byte(result.Result), &didl); err != nil {
		return didlResponse{}, err
	}
	m.log.Debug("content directory search ok",
		zap.String("server", server.FriendlyName),
		zap.String("query", query),
		zap.Int64("returned", result.NumberReturned),
		zap.Int64("total", result.TotalMatches),
		zap.Duration("duration", time.Since(started)),
	)
	return didlResponse{
		Items:          didl.Items,
		Containers:     didl.Containers,
		NumberReturned: result.NumberReturned,
		TotalMatches:   result.TotalMatches,
	}, nil
}

func (m *Module) libraryItemFromObject(server *mediaServer, obj didlObject) libraryItem {
	itemID := makeContainerID(server.ID, obj.ID)
	itemType, mediaType := classifyObject(obj.Class, obj.ID, len(obj.Resources))
	if isContainerObject(obj) {
		itemType, mediaType = "Folder", "Folder"
	}
	image := firstAlbumArt(server, obj)
	if image == "" {
		image = server.IconURL
	}
	duration := int64(0)
	if len(obj.Resources) > 0 {
		duration = durationToMS(obj.Resources[0].Duration)
	}
	artists := []string{}
	if obj.Artist != "" {
		artists = append(artists, obj.Artist)
	}
	if obj.Creator != "" && obj.Creator != obj.Artist {
		artists = append(artists, obj.Creator)
	}
	return libraryItem{
		ItemID:      itemID,
		Name:        obj.Title,
		Type:        itemType,
		MediaType:   mediaType,
		Artists:     artists,
		Album:       obj.Album,
		ContainerID: makeContainerID(server.ID, obj.ParentID),
		DurationMS:  duration,
		ImageURL:    image,
	}
}

func (m *Module) buildMetadata(server *mediaServer, obj didlObject) map[string]any {
	itemType, mediaType := classifyObject(obj.Class, obj.ID, len(obj.Resources))
	meta := map[string]any{
		"title":     obj.Title,
		"type":      itemType,
		"mediaType": mediaType,
	}
	if obj.Album != "" {
		meta["album"] = obj.Album
	}
	if obj.Artist != "" {
		meta["artist"] = obj.Artist
		meta["artists"] = []string{obj.Artist}
	}
	if obj.Creator != "" && obj.Creator != obj.Artist {
		meta["creator"] = obj.Creator
	}
	if len(obj.Resources) > 0 {
		if ms := durationToMS(obj.Resources[0].Duration); ms > 0 {
			meta["durationMs"] = ms
		}
	}
	if art := firstAlbumArt(server, obj); art != "" {
		meta["artworkUrl"] = art
	} else if server.IconURL != "" {
		meta["artworkUrl"] = server.IconURL
	}
	return meta
}

// DIDL-Lite parsing helpers.

type didlResponse struct {
	Items          []didlObject
	Containers     []didlObject
	NumberReturned int64
	TotalMatches   int64
}

type didlLite struct {
	XMLName    xml.Name     `xml:"urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/ DIDL-Lite"`
	Items      []didlObject `xml:"urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/ item"`
	Containers []didlObject `xml:"urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/ container"`
}

type didlObject struct {
	ID             string    `xml:"id,attr"`
	ParentID       string    `xml:"parentID,attr"`
	Restricted     int       `xml:"restricted,attr"`
	Title          string    `xml:"http://purl.org/dc/elements/1.1/ title"`
	Class          string    `xml:"urn:schemas-upnp-org:metadata-1-0/upnp/ class"`
	Album          string    `xml:"urn:schemas-upnp-org:metadata-1-0/upnp/ album"`
	Artist         string    `xml:"urn:schemas-upnp-org:metadata-1-0/upnp/ artist"`
	Creator        string    `xml:"http://purl.org/dc/elements/1.1/ creator"`
	AlbumArtURI    []string  `xml:"urn:schemas-upnp-org:metadata-1-0/upnp/ albumArtURI"`
	AlbumArtURIRaw []string  `xml:"albumArtURI"`
	Resources      []didlRes `xml:"res"`
}

type didlRes struct {
	Value        string `xml:",chardata"`
	ProtocolInfo string `xml:"protocolInfo,attr"`
	Duration     string `xml:"duration,attr"`
	Size         int64  `xml:"size,attr,omitempty"`
}

type browseResponseEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		BrowseResponse browseResponse `xml:"BrowseResponse"`
		Fault          *soapFault     `xml:"Fault"`
	} `xml:"Body"`
}

type browseResponse struct {
	Result         string `xml:"Result"`
	NumberReturned int64  `xml:"NumberReturned"`
	TotalMatches   int64  `xml:"TotalMatches"`
	UpdateID       int64  `xml:"UpdateID"`
}

type searchResponseEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		SearchResponse searchResponse `xml:"SearchResponse"`
		Fault          *soapFault     `xml:"Fault"`
	} `xml:"Body"`
}

type searchResponse struct {
	Result         string `xml:"Result"`
	NumberReturned int64  `xml:"NumberReturned"`
	TotalMatches   int64  `xml:"TotalMatches"`
	UpdateID       int64  `xml:"UpdateID"`
}

type soapFault struct {
	Code   string `xml:"faultcode"`
	String string `xml:"faultstring"`
	Actor  string `xml:"faultactor"`
	Detail string `xml:"detail"`
}

func (f *soapFault) Error() string {
	if f == nil {
		return ""
	}
	if f.Detail != "" {
		return f.String + ": " + f.Detail
	}
	return f.String
}

func buildBrowseEnvelope(serviceType string, objectID string, flag string, start int64, count int64) []byte {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0"?>`)
	buf.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`)
	buf.WriteString(`<s:Body><u:Browse xmlns:u="` + serviceType + `">`)
	buf.WriteString(`<ObjectID>` + xmlEscape(objectID) + `</ObjectID>`)
	buf.WriteString(`<BrowseFlag>` + flag + `</BrowseFlag>`)
	buf.WriteString(`<Filter>*</Filter>`)
	buf.WriteString(fmt.Sprintf(`<StartingIndex>%d</StartingIndex>`, start))
	buf.WriteString(fmt.Sprintf(`<RequestedCount>%d</RequestedCount>`, count))
	buf.WriteString(`<SortCriteria></SortCriteria>`)
	buf.WriteString(`</u:Browse></s:Body></s:Envelope>`)
	return buf.Bytes()
}

func buildSearchEnvelope(serviceType string, containerID string, criteria string, start int64, count int64) []byte {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0"?>`)
	buf.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`)
	buf.WriteString(`<s:Body><u:Search xmlns:u="` + serviceType + `">`)
	buf.WriteString(`<ContainerID>` + xmlEscape(containerID) + `</ContainerID>`)
	buf.WriteString(`<SearchCriteria>` + xmlEscape(criteria) + `</SearchCriteria>`)
	buf.WriteString(`<Filter>*</Filter>`)
	buf.WriteString(fmt.Sprintf(`<StartingIndex>%d</StartingIndex>`, start))
	buf.WriteString(fmt.Sprintf(`<RequestedCount>%d</RequestedCount>`, count))
	buf.WriteString(`<SortCriteria></SortCriteria>`)
	buf.WriteString(`</u:Search></s:Body></s:Envelope>`)
	return buf.Bytes()
}

func searchCriteria(query string) string {
	q := strings.ReplaceAll(query, `"`, `\"`)
	return fmt.Sprintf(`dc:title contains "%s" or upnp:artist contains "%s" or upnp:album contains "%s"`, q, q, q)
}

// device description parsing.

type deviceDescription struct {
	URLBase string `xml:"URLBase"`
	Device  struct {
		DeviceType   string          `xml:"deviceType"`
		FriendlyName string          `xml:"friendlyName"`
		Manufacturer string          `xml:"manufacturer"`
		ModelName    string          `xml:"modelName"`
		UDN          string          `xml:"UDN"`
		IconList     []deviceIcon    `xml:"iconList>icon"`
		Services     []deviceService `xml:"serviceList>service"`
	} `xml:"device"`
}

type deviceService struct {
	ServiceType string `xml:"serviceType"`
	ServiceID   string `xml:"serviceId"`
	ControlURL  string `xml:"controlURL"`
	EventSubURL string `xml:"eventSubURL"`
	SCPDURL     string `xml:"SCPDURL"`
}

type deviceIcon struct {
	MimeType string `xml:"mimetype"`
	URL      string `xml:"url"`
	Width    int    `xml:"width"`
	Height   int    `xml:"height"`
	Depth    int    `xml:"depth"`
}

func (d deviceDescription) ContentDirectory() (deviceService, bool) {
	for _, svc := range d.Device.Services {
		if strings.Contains(strings.ToLower(svc.ServiceType), "contentdirectory") {
			return svc, true
		}
	}
	return deviceService{}, false
}

func (d deviceDescription) BaseURL(location string) string {
	if strings.TrimSpace(d.URLBase) != "" {
		return strings.TrimRight(d.URLBase, "/")
	}
	u, err := url.Parse(location)
	if err != nil {
		return location
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
}

func (d deviceDescription) IconURL(base string) string {
	if len(d.Device.IconList) == 0 {
		return ""
	}
	icon := d.Device.IconList[0]
	return resolveURL(base, icon.URL)
}

// Caching helpers.

type resolveCacheEntry struct {
	Metadata     map[string]any      `json:"metadata"`
	Sources      []mu.ResolvedSource `json:"sources"`
	SourcesReady bool                `json:"sourcesReady"`
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
		m.log.Debug("upnp cache decode failed", zap.Error(err))
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

func browseCacheKey(kind string, containerID string, start int64, count int64, query string, rev uint64) string {
	return fmt.Sprintf("browse:%s:%s:%d:%d:%s:%d", kind, containerID, start, count, query, rev)
}

// Flow control and utils.

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

// ID helpers.

func makeContainerID(serverID string, objectID string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(objectID))
	return serverID + "::" + encoded
}

func parseItemID(itemID string) (serverID string, objectID string, ok bool) {
	parts := strings.SplitN(itemID, "::", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", false
	}
	return parts[0], string(raw), true
}

func classifyObject(class string, objectID string, resourceCount int) (string, string) {
	class = strings.ToLower(class)
	if strings.HasPrefix(strings.ToLower(objectID), "folder_") {
		return "Folder", "Folder"
	}
	if resourceCount == 0 {
		return "Folder", "Folder"
	}
	switch {
	case strings.Contains(class, "audioitem"):
		return "Audio", "Audio"
	case strings.Contains(class, "videoclip") || strings.Contains(class, "videoitem"):
		return "Video", "Video"
	case strings.Contains(class, "imageitem") || strings.Contains(class, "photo"):
		return "Image", "Image"
	case strings.Contains(class, "playlist"):
		return "Playlist", "Playlist"
	case strings.Contains(class, "genre"):
		return "Folder", "Folder"
	case strings.Contains(class, "container.album"):
		return "Album", "Audio"
	default:
		if strings.Contains(class, "container") {
			return "Folder", "Folder"
		}
	}
	return "Unknown", "Unknown"
}

func durationToMS(duration string) int64 {
	if duration == "" {
		return 0
	}
	parts := strings.Split(duration, ":")
	if len(parts) < 2 {
		return 0
	}
	for len(parts) < 3 {
		parts = append([]string{"0"}, parts...)
	}
	hours := parseInt(parts[0])
	mins := parseInt(parts[1])
	secs := parseSeconds(parts[2])
	totalMS := hours*3600000 + mins*60000 + int64(secs*1000)
	return totalMS
}

func parseSeconds(part string) float64 {
	parsed, err := strconv.ParseFloat(part, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func parseInt(value string) int64 {
	val, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return val
}

func buildSources(server *mediaServer, obj didlObject) []mu.ResolvedSource {
	sources := []mu.ResolvedSource{}
	for _, res := range obj.Resources {
		if strings.TrimSpace(res.Value) == "" {
			continue
		}
		mime := mimeFromProtocolInfo(res.ProtocolInfo)
		byteRange := strings.Contains(strings.ToLower(res.ProtocolInfo), "dlna.org_op=01") || strings.Contains(strings.ToLower(res.ProtocolInfo), "http-get")
		sources = append(sources, mu.ResolvedSource{
			URL:       server.absoluteURL(res.Value),
			Mime:      mime,
			ByteRange: byteRange,
		})
	}
	return sources
}

func mimeFromProtocolInfo(protocolInfo string) string {
	parts := strings.Split(protocolInfo, ":")
	if len(parts) >= 3 && strings.TrimSpace(parts[2]) != "" {
		return strings.TrimSpace(parts[2])
	}
	return ""
}

func firstAlbumArt(server *mediaServer, obj didlObject) string {
	if len(obj.AlbumArtURI) > 0 {
		return server.absoluteURL(obj.AlbumArtURI[0])
	}
	if len(obj.AlbumArtURIRaw) > 0 {
		return server.absoluteURL(obj.AlbumArtURIRaw[0])
	}
	return ""
}

func isContainerObject(obj didlObject) bool {
	class := strings.ToLower(obj.Class)
	if strings.HasPrefix(strings.ToLower(obj.ID), "folder_") {
		return true
	}
	if len(obj.Resources) == 0 {
		return true
	}
	if strings.Contains(class, "container") || strings.Contains(class, "genre") {
		return true
	}
	return false
}

func (m *Module) normalizeItemID(itemID string, serverID string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return ""
	}
	if _, _, ok := parseItemID(itemID); ok {
		return itemID
	}
	if serverID == "" {
		return itemID
	}
	decoded, err := base64.RawURLEncoding.DecodeString(itemID)
	if err == nil && len(decoded) > 0 {
		itemID = string(decoded)
	}
	return makeContainerID(serverID, itemID)
}

func (m *Module) serverIDFromTopic(topic string) string {
	prefix := fmt.Sprintf("%s/node/%s:", m.config.TopicBase, m.config.NodeID)
	if !strings.HasPrefix(topic, prefix) {
		return ""
	}
	trimmed := strings.TrimPrefix(topic, prefix)
	trimmed = strings.TrimSuffix(trimmed, "/cmd")
	trimmed = strings.TrimSuffix(trimmed, ":")
	return trimmed
}

func (m *Module) topicMatches(topic string) bool {
	if topic == m.cmdTopic {
		return true
	}
	if serverID := m.serverIDFromTopic(topic); serverID != "" {
		return true
	}
	return false
}

func paginateItems(items []libraryItem, start int64, count int64, totalOverride int64) ([]libraryItem, int64, error) {
	if start < 0 {
		start = 0
	}
	total := int64(len(items))
	if totalOverride > total {
		total = totalOverride
	}
	if count <= 0 {
		count = total
	}
	if start > int64(len(items)) {
		return []libraryItem{}, total, nil
	}
	end := start + count
	if end > int64(len(items)) {
		end = int64(len(items))
	}
	return items[start:end], total, nil
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer(
		`&`, "&amp;",
		`<`, "&lt;",
		`>`, "&gt;",
		`'`, "&apos;",
		`"`, "&quot;",
	)
	return replacer.Replace(value)
}

func resolveURL(baseURL string, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return baseURL + ref
	}
	rel, err := url.Parse(ref)
	if err != nil {
		base.Path = path.Join(base.Path, ref)
		return base.String()
	}
	return base.ResolveReference(rel).String()
}

func (s *mediaServer) absoluteURL(ref string) string {
	return resolveURL(s.BaseURL, ref)
}

func (m *Module) discoveryRevision() uint64 {
	m.serversMu.RLock()
	defer m.serversMu.RUnlock()
	return m.discoveryRev
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
