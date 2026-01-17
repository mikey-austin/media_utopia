package fslibrary

import (
	"context"
	"crypto/sha1"
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
type Config struct {
	NodeID            string
	TopicBase         string
	Name              string
	Roots             []string
	IncludeExts       []string
	HTTPListen        string
	IndexMode         string
	IndexPath         string
	ScanIntervalMS    int64
	MetadataMode      string
	RepairPolicy      string
	DedupePolicy      string
	EmbeddingProvider string
	EmbeddingModel    string
	EmbeddingEndpoint string
	EmbeddingCache    string
}

// Module exposes a filesystem library to mu.
type Module struct {
	log      *zap.Logger
	client   *mqttserver.Client
	config   Config
	cmdTopic string

	mu      sync.RWMutex
	index   *libraryIndex
	baseURL string
	server  *http.Server
	ln      net.Listener
}

type libraryIndex struct {
	Items map[string]mediaItem   `json:"items"`
	Audio map[string]artistEntry `json:"audio"`
	Video []string               `json:"video"`
}

type artistEntry struct {
	Name   string                `json:"name"`
	Albums map[string]albumEntry `json:"albums"`
}

type albumEntry struct {
	Name   string   `json:"name"`
	Tracks []string `json:"tracks"`
}

type mediaItem struct {
	ID         string   `json:"id"`
	Path       string   `json:"path"`
	Name       string   `json:"name"`
	Title      string   `json:"title"`
	Artists    []string `json:"artists,omitempty"`
	Album      string   `json:"album,omitempty"`
	MediaType  string   `json:"mediaType"`
	DurationMS int64    `json:"durationMs,omitempty"`
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
	return &Module{
		log:      log,
		client:   client,
		config:   cfg,
		cmdTopic: cmdTopic,
		index:    &libraryIndex{Items: map[string]mediaItem{}, Audio: map[string]artistEntry{}},
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
	// TODO: replace with fuzzy search, semantic embeddings, and similarity queries.
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
	item, ok := m.getItem(body.ItemID)
	if !ok {
		return errorReply(cmd, "NOT_FOUND", "item not found")
	}
	sourceURL, err := m.sourceURL(item.ID)
	if err != nil {
		return errorReply(cmd, "INVALID", err.Error())
	}
	payload, _ := json.Marshal(mu.LibraryResolveReply{
		ItemID: item.ID,
		Metadata: map[string]any{
			"title":    item.Title,
			"artists":  item.Artists,
			"album":    item.Album,
			"duration": item.DurationMS,
			"type":     item.MediaType,
		},
		Sources: []mu.ResolvedSource{{URL: sourceURL, ByteRange: true}},
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
		item, ok := m.getItem(itemID)
		if !ok {
			items = append(items, mu.LibraryResolveBatchItem{
				ItemID: itemID,
				Err:    &mu.ReplyError{Code: "NOT_FOUND", Message: "item not found"},
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
			ItemID: item.ID,
			Metadata: map[string]any{
				"title":    item.Title,
				"artists":  item.Artists,
				"album":    item.Album,
				"duration": item.DurationMS,
				"type":     item.MediaType,
			},
			Sources: []mu.ResolvedSource{{URL: sourceURL, ByteRange: true}},
		})
	}
	payload, _ := json.Marshal(mu.LibraryResolveBatchReply{Items: items})
	reply.Body = payload
	return reply
}

func (m *Module) browse(containerID string, start int64, count int64) ([]libraryItem, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if containerID == "" {
		items := []libraryItem{
			{ItemID: "container:audio", Name: "Audio", Type: "Folder", MediaType: "Audio"},
			{ItemID: "container:video", Name: "Video", Type: "Folder", MediaType: "Video"},
		}
		return paginate(items, start, count), int64(len(items)), nil
	}

	if containerID == "container:audio" {
		artists := make([]string, 0, len(m.index.Audio))
		for artist := range m.index.Audio {
			artists = append(artists, artist)
		}
		sort.Strings(artists)
		items := make([]libraryItem, 0, len(artists))
		for _, artist := range artists {
			items = append(items, libraryItem{
				ItemID:    "artist:" + url.PathEscape(artist),
				Name:      artist,
				Type:      "Folder",
				MediaType: "Audio",
			})
		}
		return paginate(items, start, count), int64(len(items)), nil
	}

	if strings.HasPrefix(containerID, "artist:") {
		artistName, err := url.PathUnescape(strings.TrimPrefix(containerID, "artist:"))
		if err != nil {
			return nil, 0, errors.New("invalid artist container")
		}
		artist, ok := m.index.Audio[artistName]
		if !ok {
			return nil, 0, errors.New("artist not found")
		}
		albums := make([]string, 0, len(artist.Albums))
		for album := range artist.Albums {
			albums = append(albums, album)
		}
		sort.Strings(albums)
		items := make([]libraryItem, 0, len(albums))
		for _, album := range albums {
			albumID := "album:" + url.PathEscape(artistName) + ":" + url.PathEscape(album)
			items = append(items, libraryItem{
				ItemID:    albumID,
				Name:      album,
				Type:      "Folder",
				MediaType: "Audio",
			})
		}
		return paginate(items, start, count), int64(len(items)), nil
	}

	if strings.HasPrefix(containerID, "album:") {
		parts := strings.SplitN(strings.TrimPrefix(containerID, "album:"), ":", 2)
		if len(parts) != 2 {
			return nil, 0, errors.New("invalid album container")
		}
		artistName, err := url.PathUnescape(parts[0])
		if err != nil {
			return nil, 0, errors.New("invalid artist name")
		}
		albumName, err := url.PathUnescape(parts[1])
		if err != nil {
			return nil, 0, errors.New("invalid album name")
		}
		artist, ok := m.index.Audio[artistName]
		if !ok {
			return nil, 0, errors.New("artist not found")
		}
		album, ok := artist.Albums[albumName]
		if !ok {
			return nil, 0, errors.New("album not found")
		}
		items := make([]libraryItem, 0, len(album.Tracks))
		for _, itemID := range album.Tracks {
			item, ok := m.index.Items[itemID]
			if !ok {
				continue
			}
			items = append(items, libraryItem{
				ItemID:     item.ID,
				Name:       item.Name,
				Type:       item.MediaType,
				MediaType:  item.MediaType,
				Artists:    item.Artists,
				Album:      item.Album,
				DurationMS: item.DurationMS,
			})
		}
		return paginate(items, start, count), int64(len(items)), nil
	}

	if containerID == "container:video" {
		items := make([]libraryItem, 0, len(m.index.Video))
		for _, itemID := range m.index.Video {
			item, ok := m.index.Items[itemID]
			if !ok {
				continue
			}
			items = append(items, libraryItem{
				ItemID:     item.ID,
				Name:       item.Name,
				Type:       item.MediaType,
				MediaType:  item.MediaType,
				DurationMS: item.DurationMS,
			})
		}
		return paginate(items, start, count), int64(len(items)), nil
	}

	return nil, 0, errors.New("unsupported container")
}

func (m *Module) search(query string, start int64, count int64) ([]libraryItem, int64) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil, 0
	}
	terms := strings.Fields(query)
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]libraryItem, 0)
	for _, item := range m.index.Items {
		if !containsAllTerms(item, terms) {
			continue
		}
		items = append(items, libraryItem{
			ItemID:     item.ID,
			Name:       item.Name,
			Type:       item.MediaType,
			MediaType:  item.MediaType,
			Artists:    item.Artists,
			Album:      item.Album,
			DurationMS: item.DurationMS,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	total := int64(len(items))
	return paginate(items, start, count), total
}

func containsAllTerms(item mediaItem, terms []string) bool {
	searchText := strings.ToLower(strings.Join([]string{
		item.Name, item.Title, item.Album, strings.Join(item.Artists, " "),
	}, " "))
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

	next := &libraryIndex{Items: map[string]mediaItem{}, Audio: map[string]artistEntry{}}

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
			artist.Albums[albumName] = album
		}
		next.Audio[artistName] = artist
	}
	sort.Strings(next.Video)

	// TODO: hook metadata repair, dedupe, and embedding pipelines here.
	m.mu.Lock()
	m.index = next
	m.mu.Unlock()

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
		ID:         fmt.Sprintf("%s:%s", strings.ToLower(mediaType), itemID),
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
	h := sha1.New()
	_, _ = io.WriteString(h, path)
	_, _ = io.WriteString(h, fmt.Sprintf("|%d|%d", size, mod.UnixNano()))
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
