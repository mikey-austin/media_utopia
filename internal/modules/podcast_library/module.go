package podcastlibrary

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/mmcdole/gofeed"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

// Config configures the podcast library module.
type Config struct {
	NodeID            string
	TopicBase         string
	Name              string
	Feeds             []string
	RefreshInterval   time.Duration
	CacheDir          string
	Timeout           time.Duration
	DefaultItemAuthor string
	ReverseSortByDate bool
}

// Module provides podcast library behavior.
type Module struct {
	log      *zap.Logger
	client   *mqttserver.Client
	http     *http.Client
	config   Config
	cmdTopic string
	cacheMu  sync.Mutex
	feeds    map[string]*feedCache
}

type feedCache struct {
	Feed cachedFeed
	ByID map[string]cachedEpisode
}

type cachedFeed struct {
	FeedURL     string          `json:"feedUrl"`
	FeedID      string          `json:"feedId"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Author      string          `json:"author"`
	ImageURL    string          `json:"imageUrl"`
	FetchedAt   int64           `json:"fetchedAt"`
	Episodes    []cachedEpisode `json:"episodes"`
}

type cachedEpisode struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Published   int64  `json:"published"`
	DurationMS  int64  `json:"durationMs"`
	AudioURL    string `json:"audioUrl"`
	AudioType   string `json:"audioType"`
	ImageURL    string `json:"imageUrl"`
	Author      string `json:"author"`
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

// NewModule initializes a podcast library module.
func NewModule(log *zap.Logger, client *mqttserver.Client, cfg Config) (*Module, error) {
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, errors.New("node_id required")
	}
	if len(cfg.Feeds) == 0 {
		return nil, errors.New("feeds required")
	}
	if strings.TrimSpace(cfg.TopicBase) == "" {
		cfg.TopicBase = mu.BaseTopic
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = "Podcast Library"
	}
	if cfg.RefreshInterval == 0 {
		cfg.RefreshInterval = 24 * time.Hour
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	if strings.TrimSpace(cfg.CacheDir) == "" {
		cfg.CacheDir = defaultCacheDir()
	}

	if err := os.MkdirAll(cfg.CacheDir, 0o750); err != nil {
		return nil, err
	}

	cmdTopic := mu.TopicCommands(cfg.TopicBase, cfg.NodeID)

	return &Module{
		log:      log,
		client:   client,
		http:     &http.Client{Timeout: cfg.Timeout},
		config:   cfg,
		cmdTopic: cmdTopic,
		feeds:    make(map[string]*feedCache),
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

	items, total, err := m.searchItems(body.Query, body.Start, body.Count, body.Types)
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
	if containerID == "" {
		items := make([]libraryItem, 0, len(m.config.Feeds))
		for _, feedURL := range m.config.Feeds {
			feed, err := m.loadFeed(feedURL)
			if err != nil {
				m.log.Warn("load feed", zap.String("feed", feedURL), zap.Error(err))
				continue
			}
			items = append(items, libraryItem{
				ItemID:    feed.Feed.FeedID,
				Name:      feed.Feed.Title,
				Type:      "Podcast",
				MediaType: "Unknown",
				Overview:  feed.Feed.Description,
				ImageURL:  feed.Feed.ImageURL,
			})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		return paginateItems(items, start, count)
	}

	feed, err := m.loadFeedByID(containerID)
	if err != nil {
		return nil, 0, err
	}
	episodes := make([]cachedEpisode, 0, len(feed.Feed.Episodes))
	for _, episode := range feed.Feed.Episodes {
		if episode.AudioURL == "" {
			continue
		}
		episodes = append(episodes, episode)
	}
	if m.config.ReverseSortByDate {
		sort.Slice(episodes, func(i, j int) bool {
			return episodes[i].Published > episodes[j].Published
		})
	} else {
		sort.Slice(episodes, func(i, j int) bool {
			return episodes[i].Title < episodes[j].Title
		})
	}
	items := make([]libraryItem, 0, len(episodes))
	for _, episode := range episodes {
		artists := splitArtist(episode.Author)
		items = append(items, libraryItem{
			ItemID:      episode.ID,
			Name:        episode.Title,
			Type:        "PodcastEpisode",
			MediaType:   "Audio",
			Artists:     artists,
			Album:       feed.Feed.Title,
			ContainerID: feed.Feed.FeedID,
			Overview:    episode.Description,
			DurationMS:  episode.DurationMS,
			ImageURL:    episode.ImageURL,
		})
	}
	return paginateItems(items, start, count)
}

func (m *Module) searchItems(query string, start int64, count int64, types []string) ([]libraryItem, int64, error) {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return paginateItems(nil, start, count)
	}
	typeSet := makeTypeSet(types)

	type searchItem struct {
		item      libraryItem
		published int64
	}
	results := []searchItem{}
	for _, feedURL := range m.config.Feeds {
		feed, err := m.loadFeed(feedURL)
		if err != nil {
			m.log.Warn("load feed", zap.String("feed", feedURL), zap.Error(err))
			continue
		}
		if matchesQuery(query, feed.Feed.Title, feed.Feed.Description) && typeAllowed(typeSet, "Podcast", "Unknown") {
			results = append(results, searchItem{item: libraryItem{
				ItemID:    feed.Feed.FeedID,
				Name:      feed.Feed.Title,
				Type:      "Podcast",
				MediaType: "Unknown",
				Overview:  feed.Feed.Description,
				ImageURL:  feed.Feed.ImageURL,
			}})
		}
		for _, episode := range feed.Feed.Episodes {
			if !matchesQuery(query, episode.Title, episode.Description) {
				continue
			}
			if !typeAllowed(typeSet, "PodcastEpisode", "Audio") {
				continue
			}
			if episode.AudioURL == "" {
				continue
			}
			results = append(results, searchItem{
				item: libraryItem{
					ItemID:      episode.ID,
					Name:        episode.Title,
					Type:        "PodcastEpisode",
					MediaType:   "Audio",
					Artists:     splitArtist(episode.Author),
					Album:       feed.Feed.Title,
					ContainerID: feed.Feed.FeedID,
					Overview:    episode.Description,
					DurationMS:  episode.DurationMS,
					ImageURL:    episode.ImageURL,
				},
				published: episode.Published,
			})
		}
	}

	if m.config.ReverseSortByDate {
		sort.Slice(results, func(i, j int) bool { return results[i].published > results[j].published })
	} else {
		sort.Slice(results, func(i, j int) bool { return results[i].item.Name < results[j].item.Name })
	}

	items := make([]libraryItem, 0, len(results))
	for _, result := range results {
		items = append(items, result.item)
	}
	return paginateItems(items, start, count)
}

func (m *Module) resolveItem(itemID string, metadataOnly bool) (map[string]any, []mu.ResolvedSource, error) {
	episode, feed := m.findEpisode(itemID)
	if episode == nil {
		return nil, nil, errors.New("item not found")
	}

	metadata := map[string]any{
		"title":      episode.Title,
		"artist":     episode.Author,
		"album":      feed.Title,
		"artworkUrl": episode.ImageURL,
		"durationMs": episode.DurationMS,
		"mediaType":  "Audio",
		"type":       "PodcastEpisode",
		"overview":   episode.Description,
	}

	if metadataOnly {
		return metadata, nil, nil
	}
	if episode.AudioURL == "" {
		return nil, nil, errors.New("episode has no audio url")
	}
	source := mu.ResolvedSource{
		URL:       episode.AudioURL,
		Mime:      episode.AudioType,
		ByteRange: false,
	}
	return metadata, []mu.ResolvedSource{source}, nil
}

func (m *Module) findEpisode(itemID string) (*cachedEpisode, *cachedFeed) {
	for _, feedURL := range m.config.Feeds {
		feed, err := m.loadFeed(feedURL)
		if err != nil {
			continue
		}
		if episode, ok := feed.ByID[itemID]; ok {
			ep := episode
			return &ep, &feed.Feed
		}
	}
	return nil, nil
}

func (m *Module) loadFeedByID(feedID string) (*feedCache, error) {
	for _, feedURL := range m.config.Feeds {
		if hashID("feed", feedURL) != feedID {
			continue
		}
		return m.loadFeed(feedURL)
	}
	return nil, errors.New("feed not found")
}

func (m *Module) loadFeed(feedURL string) (*feedCache, error) {
	feedID := hashID("feed", feedURL)

	m.cacheMu.Lock()
	if feed, ok := m.feeds[feedID]; ok && !m.isStale(feed.Feed.FetchedAt) {
		m.cacheMu.Unlock()
		return feed, nil
	}
	m.cacheMu.Unlock()

	cachePath := filepath.Join(m.config.CacheDir, fmt.Sprintf("podcast_%s.json", feedID))
	cached, err := readCache(cachePath)
	if err == nil && cached != nil && !m.isStale(cached.FetchedAt) {
		feed := &feedCache{Feed: *cached, ByID: indexEpisodes(cached.Episodes)}
		m.cacheMu.Lock()
		m.feeds[feedID] = feed
		m.cacheMu.Unlock()
		return feed, nil
	}

	fetched, fetchErr := m.fetchFeed(feedURL)
	if fetchErr != nil {
		if cached != nil {
			feed := &feedCache{Feed: *cached, ByID: indexEpisodes(cached.Episodes)}
			m.cacheMu.Lock()
			m.feeds[feedID] = feed
			m.cacheMu.Unlock()
			return feed, nil
		}
		return nil, fetchErr
	}

	if err := writeCache(cachePath, fetched); err != nil {
		m.log.Warn("write cache", zap.Error(err))
	}

	feed := &feedCache{Feed: *fetched, ByID: indexEpisodes(fetched.Episodes)}
	m.cacheMu.Lock()
	m.feeds[feedID] = feed
	m.cacheMu.Unlock()
	return feed, nil
}

func (m *Module) isStale(fetchedAt int64) bool {
	if fetchedAt == 0 {
		return true
	}
	return time.Since(time.Unix(fetchedAt, 0)) > m.config.RefreshInterval
}

func (m *Module) fetchFeed(feedURL string) (*cachedFeed, error) {
	req, err := http.NewRequest("GET", feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "media_utopia/1.0")

	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("feed fetch failed: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	parser := gofeed.NewParser()
	feed, err := parser.ParseString(string(body))
	if err != nil {
		return nil, err
	}

	feedID := hashID("feed", feedURL)
	feedTitle := strings.TrimSpace(feed.Title)
	if feedTitle == "" {
		feedTitle = feedURL
	}

	feedAuthor := bestFeedAuthor(feed)
	feedImage := bestFeedImage(feed)

	episodes := make([]cachedEpisode, 0, len(feed.Items))
	for _, item := range feed.Items {
		episode := buildEpisode(feedID, feed, item, feedImage, feedAuthor)
		if episode.ID == "" {
			continue
		}
		episodes = append(episodes, episode)
	}

	return &cachedFeed{
		FeedURL:     feedURL,
		FeedID:      feedID,
		Title:       feedTitle,
		Description: strings.TrimSpace(feed.Description),
		Author:      feedAuthor,
		ImageURL:    feedImage,
		FetchedAt:   time.Now().Unix(),
		Episodes:    episodes,
	}, nil
}

func buildEpisode(feedID string, feed *gofeed.Feed, item *gofeed.Item, fallbackImage string, fallbackAuthor string) cachedEpisode {
	if item == nil {
		return cachedEpisode{}
	}
	audioURL, audioType := pickEnclosure(item)
	key := strings.TrimSpace(item.GUID)
	if key == "" {
		key = audioURL
	}
	if key == "" {
		key = strings.TrimSpace(item.Link)
	}
	if key == "" {
		key = strings.TrimSpace(item.Title)
	}
	if key == "" {
		return cachedEpisode{}
	}

	imageURL := bestItemImage(item)
	if imageURL == "" {
		imageURL = fallbackImage
	}

	author := bestItemAuthor(item, feed)
	if author == "" {
		author = fallbackAuthor
	}

	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = key
	}

	return cachedEpisode{
		ID:          hashID("episode", feedID+":"+key),
		Title:       title,
		Description: strings.TrimSpace(item.Description),
		Published:   toUnix(item.PublishedParsed),
		DurationMS:  parseDurationMS(item),
		AudioURL:    audioURL,
		AudioType:   audioType,
		ImageURL:    imageURL,
		Author:      author,
	}
}

func pickEnclosure(item *gofeed.Item) (string, string) {
	for _, enc := range item.Enclosures {
		if enc == nil {
			continue
		}
		if enc.URL != "" {
			return enc.URL, enc.Type
		}
	}
	return "", ""
}

func bestFeedAuthor(feed *gofeed.Feed) string {
	if feed == nil {
		return ""
	}
	if feed.Author != nil && feed.Author.Name != "" {
		return strings.TrimSpace(feed.Author.Name)
	}
	if feed.ITunesExt != nil && feed.ITunesExt.Author != "" {
		return strings.TrimSpace(feed.ITunesExt.Author)
	}
	return ""
}

func bestItemAuthor(item *gofeed.Item, feed *gofeed.Feed) string {
	if item != nil && item.Author != nil && item.Author.Name != "" {
		return strings.TrimSpace(item.Author.Name)
	}
	if item != nil && item.ITunesExt != nil && item.ITunesExt.Author != "" {
		return strings.TrimSpace(item.ITunesExt.Author)
	}
	return bestFeedAuthor(feed)
}

func bestFeedImage(feed *gofeed.Feed) string {
	if feed == nil {
		return ""
	}
	if feed.Image != nil && feed.Image.URL != "" {
		return feed.Image.URL
	}
	if feed.ITunesExt != nil && feed.ITunesExt.Image != "" {
		return feed.ITunesExt.Image
	}
	return ""
}

func bestItemImage(item *gofeed.Item) string {
	if item == nil {
		return ""
	}
	if item.Image != nil && item.Image.URL != "" {
		return item.Image.URL
	}
	if item.ITunesExt != nil && item.ITunesExt.Image != "" {
		return item.ITunesExt.Image
	}
	return ""
}

func parseDurationMS(item *gofeed.Item) int64 {
	if item == nil || item.ITunesExt == nil {
		return 0
	}
	raw := strings.TrimSpace(item.ITunesExt.Duration)
	if raw == "" {
		return 0
	}
	if strings.Contains(raw, ":") {
		parts := strings.Split(raw, ":")
		total := 0
		for _, part := range parts {
			n := 0
			fmt.Sscanf(part, "%d", &n)
			total = total*60 + n
		}
		return int64(total * 1000)
	}
	seconds := 0
	if _, err := fmt.Sscanf(raw, "%d", &seconds); err == nil {
		return int64(seconds * 1000)
	}
	return 0
}

func toUnix(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.Unix()
}

func matchesQuery(query string, fields ...string) bool {
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func splitArtist(artist string) []string {
	artist = strings.TrimSpace(artist)
	if artist == "" {
		return nil
	}
	return []string{artist}
}

func makeTypeSet(types []string) map[string]struct{} {
	if len(types) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(types))
	for _, entry := range types {
		entry = strings.TrimSpace(strings.ToLower(entry))
		if entry != "" {
			set[entry] = struct{}{}
		}
	}
	return set
}

func typeAllowed(set map[string]struct{}, itemType string, mediaType string) bool {
	if len(set) == 0 {
		return true
	}
	itemType = strings.ToLower(itemType)
	mediaType = strings.ToLower(mediaType)
	if _, ok := set[itemType]; ok {
		return true
	}
	if mediaType != "" {
		if _, ok := set[mediaType]; ok {
			return true
		}
	}
	return false
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

func indexEpisodes(episodes []cachedEpisode) map[string]cachedEpisode {
	out := make(map[string]cachedEpisode, len(episodes))
	for _, episode := range episodes {
		if episode.ID != "" {
			out[episode.ID] = episode
		}
	}
	return out
}

func hashID(prefix string, input string) string {
	sum := sha1.Sum([]byte(input))
	return fmt.Sprintf("%s_%x", prefix, sum[:])
}

func readCache(path string) (*cachedFeed, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cached cachedFeed
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, err
	}
	return &cached, nil
}

func writeCache(path string, cached *cachedFeed) error {
	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func defaultCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(dir) == "" {
		return filepath.Join(os.TempDir(), "mu-podcasts")
	}
	return filepath.Join(dir, "mu", "podcasts")
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
