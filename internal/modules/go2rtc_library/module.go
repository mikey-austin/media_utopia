package go2rtclibrary

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

// Config configures the go2rtc library module.
type Config struct {
	NodeID          string
	TopicBase       string
	Name            string
	BaseURL         string
	Username        string
	Password        string
	Durations       []time.Duration
	RefreshInterval time.Duration
	Timeout         time.Duration
}

// Module provides go2rtc library behavior.
type Module struct {
	log      *zap.Logger
	client   *mqttserver.Client
	http     *http.Client
	config   Config
	cmdTopic string

	mu          sync.Mutex
	streams     map[string]streamInfo
	lastRefresh time.Time
}

type streamInfo struct {
	ID   string
	Name string
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

// NewModule initializes a go2rtc library module.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*Module, error) {
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, errors.New("node_id required")
	}
	if strings.TrimSpace(cfg.TopicBase) == "" {
		cfg.TopicBase = mu.BaseTopic
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = "go2rtc Library"
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("base_url required")
	}
	if len(cfg.Durations) == 0 {
		cfg.Durations = []time.Duration{30 * time.Second}
	}
	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = 5 * time.Minute
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}

	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	cmdTopic := mu.TopicCommands(cfg.TopicBase, cfg.NodeID)

	return &Module{
		log:      log,
		client:   client,
		http:     &http.Client{Timeout: cfg.Timeout},
		config:   cfg,
		cmdTopic: cmdTopic,
		streams:  make(map[string]streamInfo),
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
	items, total, err := m.browseItems(body.ContainerID, body.Start, body.Count)
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
	items, total, err := m.searchItems(body.Query, body.Start, body.Count)
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
	items := make([]mu.LibraryResolveBatchItem, 0, len(body.ItemIDs))
	for _, itemID := range body.ItemIDs {
		metadata, sources, err := m.resolveItem(itemID, body.MetadataOnly)
		if err != nil {
			items = append(items, mu.LibraryResolveBatchItem{
				ItemID: itemID,
				Err:    &mu.ReplyError{Code: "NOT_FOUND", Message: err.Error()},
			})
			continue
		}
		items = append(items, mu.LibraryResolveBatchItem{
			ItemID:   itemID,
			Metadata: metadata,
			Sources:  sources,
		})
	}
	payload, _ := json.Marshal(mu.LibraryResolveBatchReply{Items: items})
	reply.Body = payload
	return reply
}

func (m *Module) browseItems(containerID string, start int64, count int64) ([]libraryItem, int64, error) {
	streams, err := m.refreshStreams()
	if err != nil {
		return nil, 0, err
	}
	if containerID == "" {
		items := make([]libraryItem, 0, len(streams))
		for _, stream := range streams {
			items = append(items, libraryItem{
				ItemID:    stream.ID,
				Name:      stream.Name,
				Type:      "Camera",
				MediaType: "Unknown",
				ImageURL:  m.snapshotURL(stream.Name),
			})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		return paginateItems(items, start, count)
	}

	stream, ok := streams[containerID]
	if !ok {
		return nil, 0, errors.New("container not found")
	}
	items := m.streamEntries(stream)
	return paginateItems(items, start, count)
}

func (m *Module) searchItems(query string, start int64, count int64) ([]libraryItem, int64, error) {
	query = strings.TrimSpace(strings.ToLower(query))
	streams, err := m.refreshStreams()
	if err != nil {
		return nil, 0, err
	}
	if query == "" {
		return paginateItems(nil, start, count)
	}
	items := []libraryItem{}
	for _, stream := range streams {
		if strings.Contains(strings.ToLower(stream.Name), query) {
			items = append(items, libraryItem{
				ItemID:    stream.ID,
				Name:      stream.Name,
				Type:      "Camera",
				MediaType: "Unknown",
				ImageURL:  m.snapshotURL(stream.Name),
			})
		}
		for _, entry := range m.streamEntries(stream) {
			if strings.Contains(strings.ToLower(entry.Name), query) {
				items = append(items, entry)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return paginateItems(items, start, count)
}

func (m *Module) resolveItem(itemID string, metadataOnly bool) (map[string]any, []mu.ResolvedSource, error) {
	streams, err := m.refreshStreams()
	if err != nil {
		return nil, nil, err
	}
	streamID, live, duration, ok := parseItemID(itemID)
	if !ok {
		if stream, found := streams[itemID]; found {
			streamID = stream.ID
			live = true
			duration = 0
			ok = true
		}
	}
	if !ok {
		return nil, nil, errors.New("item not found")
	}
	stream, ok := streams[streamID]
	if !ok {
		return nil, nil, errors.New("item not found")
	}

	title := stream.Name
	durationMS := int64(0)
	if live {
		title = fmt.Sprintf("%s (Live)", stream.Name)
	} else {
		title = fmt.Sprintf("%s (%s clip)", stream.Name, duration)
		durationMS = int64(duration.Milliseconds())
	}
	metadata := map[string]any{
		"title":      title,
		"mediaType":  "Video",
		"type":       "Video",
		"durationMs": durationMS,
		"artworkUrl": m.snapshotURL(stream.Name),
	}
	if metadataOnly {
		return metadata, nil, nil
	}

	streamURL := m.streamURL(stream.Name, live, duration)
	return metadata, []mu.ResolvedSource{{
		URL:       streamURL,
		Mime:      "video/mp4",
		ByteRange: false,
	}}, nil
}

func (m *Module) streamEntries(stream streamInfo) []libraryItem {
	items := []libraryItem{
		{
			ItemID:      makeItemID(stream.ID, true, 0),
			Name:        fmt.Sprintf("%s (Live)", stream.Name),
			Type:        "Video",
			MediaType:   "Video",
			ContainerID: stream.ID,
			ImageURL:    m.snapshotURL(stream.Name),
		},
	}
	for _, dur := range m.config.Durations {
		if dur <= 0 {
			continue
		}
		items = append(items, libraryItem{
			ItemID:      makeItemID(stream.ID, false, dur),
			Name:        fmt.Sprintf("%s (%s clip)", stream.Name, dur),
			Type:        "Video",
			MediaType:   "Video",
			ContainerID: stream.ID,
			DurationMS:  int64(dur.Milliseconds()),
			ImageURL:    m.snapshotURL(stream.Name),
		})
	}
	return items
}

func (m *Module) refreshStreams() (map[string]streamInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if time.Since(m.lastRefresh) < m.config.RefreshInterval && len(m.streams) > 0 {
		return m.streams, nil
	}

	streams, err := m.fetchStreams()
	if err != nil {
		return nil, err
	}
	m.streams = streams
	m.lastRefresh = time.Now()
	return m.streams, nil
}

func (m *Module) fetchStreams() (map[string]streamInfo, error) {
	endpoint := m.apiURL("/api/streams")
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if m.config.Username != "" || m.config.Password != "" {
		req.SetBasicAuth(m.config.Username, m.config.Password)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("go2rtc error: %s", strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	names := parseStreamNames(body)
	streams := make(map[string]streamInfo, len(names))
	for _, name := range names {
		id := hashID("stream", name)
		streams[id] = streamInfo{ID: id, Name: name}
	}
	return streams, nil
}

func parseStreamNames(body []byte) []string {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err == nil {
		if val, ok := raw["streams"].(map[string]any); ok {
			return mapKeys(val)
		}
		if len(raw) > 0 {
			return mapKeys(raw)
		}
	}
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err == nil {
		names := []string{}
		for _, item := range arr {
			if name, ok := item["name"].(string); ok && name != "" {
				names = append(names, name)
			}
		}
		return names
	}
	return nil
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (m *Module) apiURL(pathSuffix string) string {
	base := strings.TrimRight(m.config.BaseURL, "/")
	return base + pathSuffix
}

func (m *Module) streamURL(name string, live bool, duration time.Duration) string {
	params := url.Values{}
	params.Set("src", name)
	if !live && duration > 0 {
		params.Set("duration", fmt.Sprintf("%d", int(duration.Seconds())))
	}
	return m.buildURL("/api/stream.mp4", params)
}

func (m *Module) snapshotURL(name string) string {
	params := url.Values{}
	params.Set("src", name)
	return m.buildURL("/api/frame.jpeg", params)
}

func (m *Module) buildURL(pathSuffix string, params url.Values) string {
	base, err := url.Parse(strings.TrimRight(m.config.BaseURL, "/"))
	if err != nil {
		return fmt.Sprintf("%s%s?%s", m.config.BaseURL, pathSuffix, params.Encode())
	}
	base.Path = strings.TrimRight(base.Path, "/") + pathSuffix
	base.RawQuery = params.Encode()
	if m.config.Username != "" || m.config.Password != "" {
		base.User = url.UserPassword(m.config.Username, m.config.Password)
	}
	return base.String()
}

func parseItemID(itemID string) (streamID string, live bool, duration time.Duration, ok bool) {
	if strings.HasPrefix(itemID, "live-") {
		return strings.TrimPrefix(itemID, "live-"), true, 0, true
	}
	if strings.HasPrefix(itemID, "clip-") {
		rest := strings.TrimPrefix(itemID, "clip-")
		idx := strings.LastIndex(rest, "-")
		if idx <= 0 || idx >= len(rest)-1 {
			return "", false, 0, false
		}
		streamID = rest[:idx]
		seconds := rest[idx+1:]
		parsed, err := time.ParseDuration(seconds + "s")
		if err != nil {
			return "", false, 0, false
		}
		return streamID, false, parsed, true
	}
	if strings.HasPrefix(itemID, "live:") {
		return strings.TrimPrefix(itemID, "live:"), true, 0, true
	}
	if strings.HasPrefix(itemID, "clip:") {
		rest := strings.TrimPrefix(itemID, "clip:")
		parts := strings.Split(rest, ":")
		if len(parts) != 2 {
			return "", false, 0, false
		}
		streamID = parts[0]
		seconds := parts[1]
		if seconds == "" {
			return "", false, 0, false
		}
		parsed, err := time.ParseDuration(seconds + "s")
		if err != nil {
			return "", false, 0, false
		}
		return streamID, false, parsed, true
	}
	return "", false, 0, false
}

func makeItemID(streamID string, live bool, duration time.Duration) string {
	if live {
		return "live-" + streamID
	}
	seconds := int(duration.Seconds())
	return fmt.Sprintf("clip-%s-%d", streamID, seconds)
}

func paginateItems(items []libraryItem, start int64, count int64) ([]libraryItem, int64, error) {
	if start < 0 {
		start = 0
	}
	total := int64(len(items))
	if count <= 0 {
		count = total
	}
	end := start + count
	if start > total {
		return []libraryItem{}, total, nil
	}
	if end > total {
		end = total
	}
	return items[start:end], total, nil
}

func hashID(prefix string, input string) string {
	sum := sha1.Sum([]byte(input))
	return fmt.Sprintf("%s_%x", prefix, sum)
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
