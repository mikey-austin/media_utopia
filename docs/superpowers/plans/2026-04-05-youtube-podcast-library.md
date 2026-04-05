# YouTube Podcast Library Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the podcast library module to support YouTube channels/playlists as feed sources using yt-dlp, so YouTube podcasts appear alongside RSS podcasts in the library browser.

**Architecture:** Add a `youtube_playlists` config field alongside the existing `feeds` field. Internally, introduce a `feedRef` struct (`URL` + `Type`) and a helper `allFeeds()` that merges both lists. The five methods that iterate feeds switch to `allFeeds()`. A new `fetchYoutubeFeed()` function calls `yt-dlp --flat-playlist --dump-json` to produce the same `cachedFeed` structure as RSS. At resolve time, episodes with `ytid:` prefixed AudioURL trigger an on-demand `yt-dlp -f bestaudio -g` call. Resolved URLs are cached in memory for 4 hours.

**Tech Stack:** Go, yt-dlp (external binary via `os/exec`), existing podcast_library module patterns

**Spec:** `docs/superpowers/specs/2026-04-04-youtube-podcast-library-design.md`

---

### Task 1: Add config fields

**Files:**
- Modify: `internal/mud/config.go:199-210`
- Modify: `cmd/mud/main.go:446-455`
- Modify: `internal/modules/podcast_library/module.go:27-37`

- [ ] **Step 1: Add `YoutubePlaylists` and `YtDlpPath` to `PodcastLibraryConfig`**

In `internal/mud/config.go`, add two fields to `PodcastLibraryConfig`:

```go
type PodcastLibraryConfig struct {
	Enabled            bool     `toml:"enabled"`
	Name               string   `toml:"name"`
	Provider           string   `toml:"provider"`
	Resource           string   `toml:"resource"`
	Feeds              []string `toml:"feeds"`
	YoutubePlaylists   []string `toml:"youtube_playlists"`
	YtDlpPath          string   `toml:"yt_dlp_path"`
	RefreshIntervalMS  int64    `toml:"refresh_interval_ms"`
	CacheDir           string   `toml:"cache_dir"`
	TimeoutMS          int64    `toml:"timeout_ms"`
	ReverseSortByDate  bool     `toml:"reverse_sort_by_date"`
}
```

- [ ] **Step 2: Add `YoutubePlaylists` and `YtDlpPath` to module `Config`**

In `internal/modules/podcast_library/module.go`, add two fields to `Config`:

```go
type Config struct {
	NodeID            string
	TopicBase         string
	Name              string
	Feeds             []string
	YoutubePlaylists  []string
	YtDlpPath         string
	RefreshInterval   time.Duration
	CacheDir          string
	Timeout           time.Duration
	DefaultItemAuthor string
	ReverseSortByDate bool
}
```

- [ ] **Step 3: Pass new config fields through in `cmd/mud/main.go`**

In `cmd/mud/main.go`, update the `podcastlibrary.Config` literal (around line 446) to include:

```go
mod, err := podcastlibrary.NewModule(logFactory.ModuleLogger("podcast"), client, podcastlibrary.Config{
	NodeID:            nodeID,
	TopicBase:         cfg.Server.TopicBase,
	Name:              cfgItem.Name,
	Feeds:             cfgItem.Feeds,
	YoutubePlaylists:  cfgItem.YoutubePlaylists,
	YtDlpPath:         cfgItem.YtDlpPath,
	RefreshInterval:   refresh,
	CacheDir:          cfgItem.CacheDir,
	Timeout:           timeout,
	ReverseSortByDate: cfgItem.ReverseSortByDate,
})
```

- [ ] **Step 4: Relax the feeds-required validation in `NewModule`**

In `internal/modules/podcast_library/module.go`, the `NewModule` function currently requires `len(cfg.Feeds) == 0` to return an error. Change this to allow either feeds or youtube playlists:

```go
if len(cfg.Feeds) == 0 && len(cfg.YoutubePlaylists) == 0 {
	return nil, errors.New("feeds or youtube_playlists required")
}
```

Also add the yt-dlp path default:

```go
if strings.TrimSpace(cfg.YtDlpPath) == "" {
	cfg.YtDlpPath = "yt-dlp"
}
```

- [ ] **Step 5: Build and verify compilation**

Run: `go build ./...`
Expected: Compiles cleanly with no errors.

- [ ] **Step 6: Run existing tests**

Run: `go test ./internal/modules/podcast_library/ -v`
Expected: All existing tests pass (config changes are additive, no behavior change yet).

- [ ] **Step 7: Commit**

```bash
git add internal/mud/config.go cmd/mud/main.go internal/modules/podcast_library/module.go
git commit -m "feat(podcast): add youtube_playlists and yt_dlp_path config fields"
```

---

### Task 2: Introduce `feedRef` and `allFeeds()` helper

**Files:**
- Modify: `internal/modules/podcast_library/module.go`

- [ ] **Step 1: Write the failing test**

In `internal/modules/podcast_library/module_test.go`, add a test that creates a module with only `YoutubePlaylists` (no RSS feeds) and verifies that `allFeeds()` returns the correct entries:

```go
func TestAllFeedsIncludesYoutube(t *testing.T) {
	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:           "mu:library:podcast:test",
		TopicBase:        mu.BaseTopic,
		YoutubePlaylists: []string{"https://www.youtube.com/playlist?list=PLabc"},
		CacheDir:         t.TempDir(),
		RefreshInterval:  24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	feeds := module.allFeeds()
	if len(feeds) != 1 {
		t.Fatalf("expected 1 feed, got %d", len(feeds))
	}
	if feeds[0].URL != "https://www.youtube.com/playlist?list=PLabc" {
		t.Fatalf("unexpected URL: %s", feeds[0].URL)
	}
	if feeds[0].Type != "youtube" {
		t.Fatalf("expected type=youtube, got %s", feeds[0].Type)
	}
}

func TestAllFeedsMergesBoth(t *testing.T) {
	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:           "mu:library:podcast:test",
		TopicBase:        mu.BaseTopic,
		Feeds:            []string{"https://example.com/feed.xml"},
		YoutubePlaylists: []string{"https://www.youtube.com/playlist?list=PLabc"},
		CacheDir:         t.TempDir(),
		RefreshInterval:  24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	feeds := module.allFeeds()
	if len(feeds) != 2 {
		t.Fatalf("expected 2 feeds, got %d", len(feeds))
	}
	if feeds[0].Type != "rss" {
		t.Fatalf("expected first feed type=rss, got %s", feeds[0].Type)
	}
	if feeds[1].Type != "youtube" {
		t.Fatalf("expected second feed type=youtube, got %s", feeds[1].Type)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/modules/podcast_library/ -run TestAllFeeds -v`
Expected: FAIL — `allFeeds` not defined.

- [ ] **Step 3: Implement `feedRef` and `allFeeds()`**

In `internal/modules/podcast_library/module.go`, add after the `Config` struct:

```go
type feedRef struct {
	URL  string
	Type string // "rss" or "youtube"
}

func (m *Module) allFeeds() []feedRef {
	refs := make([]feedRef, 0, len(m.config.Feeds)+len(m.config.YoutubePlaylists))
	for _, u := range m.config.Feeds {
		refs = append(refs, feedRef{URL: u, Type: "rss"})
	}
	for _, u := range m.config.YoutubePlaylists {
		refs = append(refs, feedRef{URL: u, Type: "youtube"})
	}
	return refs
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/modules/podcast_library/ -run TestAllFeeds -v`
Expected: PASS

- [ ] **Step 5: Update all five feed iteration sites to use `allFeeds()`**

Replace every `for _, feedURL := range m.config.Feeds` with `for _, ref := range m.allFeeds()` and use `ref.URL` where `feedURL` was used. The five locations:

1. `browseItems` root listing (line ~361):
```go
for _, ref := range m.allFeeds() {
	feed, err := m.loadFeed(ref.URL)
```

2. `browseLatest` (line ~427):
```go
for _, ref := range m.allFeeds() {
	feed, err := m.loadFeed(ref.URL)
```

3. `searchItems` (line ~485):
```go
for _, ref := range m.allFeeds() {
	feed, err := m.loadFeed(ref.URL)
```

4. `findEpisode` (line ~574):
```go
for _, ref := range m.allFeeds() {
	feed, err := m.loadFeed(ref.URL)
```

5. `loadFeedByID` (line ~588):
```go
for _, ref := range m.allFeeds() {
	if hashID("feed", ref.URL) != feedID {
		continue
	}
	return m.loadFeed(ref.URL)
```

Also update the capacity hint in `browseItems`:
```go
items := make([]libraryItem, 0, len(m.allFeeds())+1)
```

- [ ] **Step 6: Run all existing tests**

Run: `go test ./internal/modules/podcast_library/ -v`
Expected: All tests pass (behavior unchanged, just refactored iteration).

- [ ] **Step 7: Commit**

```bash
git add internal/modules/podcast_library/module.go internal/modules/podcast_library/module_test.go
git commit -m "refactor(podcast): introduce feedRef and allFeeds() for unified iteration"
```

---

### Task 3: Implement yt-dlp execution helper

**Files:**
- Modify: `internal/modules/podcast_library/module.go`
- Test: `internal/modules/podcast_library/module_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/modules/podcast_library/module_test.go`:

```go
func TestRunYtDlp(t *testing.T) {
	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:           "mu:library:podcast:test",
		TopicBase:        mu.BaseTopic,
		YoutubePlaylists: []string{"https://www.youtube.com/playlist?list=PLabc"},
		CacheDir:         t.TempDir(),
		RefreshInterval:  24 * time.Hour,
		YtDlpPath:        "echo",
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := module.runYtDlp(ctx, "hello world")
	if err != nil {
		t.Fatalf("runYtDlp: %v", err)
	}
	if !strings.Contains(string(out), "hello world") {
		t.Fatalf("expected echo output, got %q", string(out))
	}
}

func TestRunYtDlpNotFound(t *testing.T) {
	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:           "mu:library:podcast:test",
		TopicBase:        mu.BaseTopic,
		YoutubePlaylists: []string{"https://www.youtube.com/playlist?list=PLabc"},
		CacheDir:         t.TempDir(),
		RefreshInterval:  24 * time.Hour,
		YtDlpPath:        "/nonexistent/binary",
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = module.runYtDlp(ctx, "--version")
	if err == nil {
		t.Fatalf("expected error for missing binary")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/modules/podcast_library/ -run TestRunYtDlp -v`
Expected: FAIL — `runYtDlp` not defined.

- [ ] **Step 3: Implement `runYtDlp`**

Add to `internal/modules/podcast_library/module.go`, along with the required `"os/exec"` import:

```go
func (m *Module) runYtDlp(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, m.config.YtDlpPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
```

Add `"bytes"` and `"os/exec"` to the imports (note: `"bytes"` is likely not yet imported; `"fmt"` and `"strings"` are already imported).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/modules/podcast_library/ -run TestRunYtDlp -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/modules/podcast_library/module.go internal/modules/podcast_library/module_test.go
git commit -m "feat(podcast): add runYtDlp execution helper"
```

---

### Task 4: Implement YouTube metadata fetch

**Files:**
- Modify: `internal/modules/podcast_library/module.go`
- Test: `internal/modules/podcast_library/module_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/modules/podcast_library/module_test.go`:

```go
func TestParseYtDlpPlaylist(t *testing.T) {
	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:           "mu:library:podcast:test",
		TopicBase:        mu.BaseTopic,
		YoutubePlaylists: []string{"https://www.youtube.com/playlist?list=PLabc"},
		CacheDir:         t.TempDir(),
		RefreshInterval:  24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	jsonLines := strings.Join([]string{
		`{"id":"abc123","title":"Episode One","description":"First ep description here","upload_date":"20240601","duration":3600,"uploader":"TestChannel","channel":"TestChannel","thumbnail":"https://i.ytimg.com/vi/abc123/hqdefault.jpg","playlist_title":"My Playlist"}`,
		`{"id":"def456","title":"Episode Two","description":"Second ep","upload_date":"20240615","duration":1800,"uploader":"TestChannel","channel":"TestChannel","thumbnail":"https://i.ytimg.com/vi/def456/hqdefault.jpg","playlist_title":"My Playlist"}`,
	}, "\n")

	feed, err := module.parseYtDlpPlaylist("https://www.youtube.com/playlist?list=PLabc", []byte(jsonLines))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if feed.Title != "My Playlist" {
		t.Fatalf("expected title 'My Playlist', got %q", feed.Title)
	}
	if feed.Author != "TestChannel" {
		t.Fatalf("expected author 'TestChannel', got %q", feed.Author)
	}
	if len(feed.Episodes) != 2 {
		t.Fatalf("expected 2 episodes, got %d", len(feed.Episodes))
	}

	ep := feed.Episodes[0]
	if ep.Title != "Episode One" {
		t.Fatalf("expected 'Episode One', got %q", ep.Title)
	}
	if ep.DurationMS != 3600000 {
		t.Fatalf("expected 3600000ms, got %d", ep.DurationMS)
	}
	if ep.AudioURL != "ytid:abc123" {
		t.Fatalf("expected 'ytid:abc123', got %q", ep.AudioURL)
	}
	if ep.ImageURL != "https://i.ytimg.com/vi/abc123/hqdefault.jpg" {
		t.Fatalf("unexpected image: %s", ep.ImageURL)
	}

	// Verify Published is parsed from upload_date 20240601.
	published := time.Unix(ep.Published, 0).UTC()
	if published.Year() != 2024 || published.Month() != 6 || published.Day() != 1 {
		t.Fatalf("unexpected published date: %v", published)
	}
}

func TestParseYtDlpPlaylistEmpty(t *testing.T) {
	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:           "mu:library:podcast:test",
		TopicBase:        mu.BaseTopic,
		YoutubePlaylists: []string{"https://www.youtube.com/playlist?list=PLabc"},
		CacheDir:         t.TempDir(),
		RefreshInterval:  24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	feed, err := module.parseYtDlpPlaylist("https://www.youtube.com/playlist?list=PLabc", []byte(""))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(feed.Episodes) != 0 {
		t.Fatalf("expected 0 episodes, got %d", len(feed.Episodes))
	}
	if feed.Title != "https://www.youtube.com/playlist?list=PLabc" {
		t.Fatalf("expected URL as fallback title, got %q", feed.Title)
	}
}

func TestParseYtDlpDescriptionTruncation(t *testing.T) {
	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:           "mu:library:podcast:test",
		TopicBase:        mu.BaseTopic,
		YoutubePlaylists: []string{"https://www.youtube.com/playlist?list=PLabc"},
		CacheDir:         t.TempDir(),
		RefreshInterval:  24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}

	longDesc := strings.Repeat("x", 1000)
	jsonLine := fmt.Sprintf(`{"id":"vid1","title":"Long","description":%q,"upload_date":"20240101","duration":60,"uploader":"Chan","channel":"Chan","thumbnail":"https://img.test/1.jpg","playlist_title":"PL"}`, longDesc)

	feed, err := module.parseYtDlpPlaylist("https://yt.test", []byte(jsonLine))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(feed.Episodes[0].Description) > 500 {
		t.Fatalf("description not truncated: len=%d", len(feed.Episodes[0].Description))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/modules/podcast_library/ -run TestParseYtDlp -v`
Expected: FAIL — `parseYtDlpPlaylist` not defined.

- [ ] **Step 3: Implement `parseYtDlpPlaylist` and `fetchYoutubeFeed`**

Add to `internal/modules/podcast_library/module.go`:

```go
const ytidPrefix = "ytid:"

type ytDlpEntry struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	UploadDate    string  `json:"upload_date"`
	Duration      float64 `json:"duration"`
	Uploader      string  `json:"uploader"`
	Channel       string  `json:"channel"`
	Thumbnail     string  `json:"thumbnail"`
	PlaylistTitle string  `json:"playlist_title"`
}

func (m *Module) parseYtDlpPlaylist(feedURL string, data []byte) (*cachedFeed, error) {
	feedID := hashID("feed", feedURL)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	var playlistTitle, author string
	episodes := make([]cachedEpisode, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry ytDlpEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			m.log.Warn("skip yt-dlp entry", zap.Error(err))
			continue
		}
		if entry.ID == "" {
			continue
		}

		if playlistTitle == "" {
			playlistTitle = entry.PlaylistTitle
		}
		if author == "" {
			author = entry.Channel
			if author == "" {
				author = entry.Uploader
			}
		}

		desc := strings.TrimSpace(entry.Description)
		if len(desc) > 500 {
			desc = desc[:500]
		}

		episodes = append(episodes, cachedEpisode{
			ID:          hashID("episode", feedID+":"+entry.ID),
			Title:       strings.TrimSpace(entry.Title),
			Description: desc,
			Published:   parseUploadDate(entry.UploadDate),
			DurationMS:  int64(entry.Duration * 1000),
			AudioURL:    ytidPrefix + entry.ID,
			ImageURL:    entry.Thumbnail,
			Author:      author,
		})
	}

	title := playlistTitle
	if title == "" {
		title = feedURL
	}

	return &cachedFeed{
		FeedURL:   feedURL,
		FeedID:    feedID,
		Title:     title,
		Author:    author,
		FetchedAt: time.Now().Unix(),
		Episodes:  episodes,
	}, nil
}

func parseUploadDate(s string) int64 {
	if len(s) != 8 {
		return 0
	}
	t, err := time.Parse("20060102", s)
	if err != nil {
		return 0
	}
	return t.Unix()
}

func (m *Module) fetchYoutubeFeed(feedURL string) (*cachedFeed, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := m.runYtDlp(ctx, "--flat-playlist", "--dump-json", feedURL)
	if err != nil {
		return nil, fmt.Errorf("fetch youtube feed: %w", err)
	}
	return m.parseYtDlpPlaylist(feedURL, out)
}
```

Add `"context"` to imports if not already present (it is, used in `Run`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/modules/podcast_library/ -run TestParseYtDlp -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/modules/podcast_library/module.go internal/modules/podcast_library/module_test.go
git commit -m "feat(podcast): implement yt-dlp playlist metadata parsing"
```

---

### Task 5: Wire YouTube feeds into `loadFeed` dispatch

**Files:**
- Modify: `internal/modules/podcast_library/module.go`
- Test: `internal/modules/podcast_library/module_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/modules/podcast_library/module_test.go`:

```go
func TestBrowseYoutubeFeed(t *testing.T) {
	playlistURL := "https://www.youtube.com/playlist?list=PLabc"

	ytOutput := strings.Join([]string{
		`{"id":"abc123","title":"YT Episode One","description":"First yt ep","upload_date":"20240601","duration":3600,"uploader":"TestChannel","channel":"TestChannel","thumbnail":"https://i.ytimg.com/vi/abc123/hq.jpg","playlist_title":"My YT Podcast"}`,
		`{"id":"def456","title":"YT Episode Two","description":"Second yt ep","upload_date":"20240615","duration":1800,"uploader":"TestChannel","channel":"TestChannel","thumbnail":"https://i.ytimg.com/vi/def456/hq.jpg","playlist_title":"My YT Podcast"}`,
	}, "\n")

	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:           "mu:library:podcast:test",
		TopicBase:        mu.BaseTopic,
		YoutubePlaylists: []string{playlistURL},
		CacheDir:         t.TempDir(),
		RefreshInterval:  24 * time.Hour,
		YtDlpPath:        "echo",
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	// Override runYtDlp to return test data.
	module.ytDlpRunner = func(ctx context.Context, args ...string) ([]byte, error) {
		return []byte(ytOutput), nil
	}

	// Browse root — should show the YouTube playlist.
	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{})}
	reply := module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var browse libraryItemsReply
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse decode: %v", err)
	}
	// "Latest" + 1 YouTube feed.
	if len(browse.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(browse.Items))
	}
	if browse.Items[1].Name != "My YT Podcast" {
		t.Fatalf("expected 'My YT Podcast', got %q", browse.Items[1].Name)
	}

	// Browse into the YouTube feed.
	feedID := browse.Items[1].ItemID
	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: feedID})}
	reply = module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("episodes decode: %v", err)
	}
	if len(browse.Items) != 2 {
		t.Fatalf("expected 2 episodes, got %d", len(browse.Items))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/modules/podcast_library/ -run TestBrowseYoutubeFeed -v`
Expected: FAIL — `ytDlpRunner` field not defined.

- [ ] **Step 3: Add `ytDlpRunner` function field to `Module` for testability**

In `internal/modules/podcast_library/module.go`, add a field to the `Module` struct:

```go
type Module struct {
	log         *zap.Logger
	client      *mqttserver.Client
	http        *http.Client
	config      Config
	cmdTopic    string
	cmdQueue    chan cmdWork
	dedup       *mu.CommandDedup
	cacheMu     sync.Mutex
	feeds       map[string]*feedCache
	ytDlpRunner func(ctx context.Context, args ...string) ([]byte, error)
}
```

In `NewModule`, initialize `ytDlpRunner` to the default implementation after creating the module:

```go
m := &Module{
	log:      log,
	client:   client,
	http:     &http.Client{Timeout: cfg.Timeout},
	config:   cfg,
	cmdTopic: cmdTopic,
	cmdQueue: make(chan cmdWork, 64),
	dedup:    mu.NewCommandDedup(128),
	feeds:    make(map[string]*feedCache),
}
m.ytDlpRunner = m.runYtDlp
return m, nil
```

Update `fetchYoutubeFeed` to use `m.ytDlpRunner` instead of `m.runYtDlp`:

```go
func (m *Module) fetchYoutubeFeed(feedURL string) (*cachedFeed, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := m.ytDlpRunner(ctx, "--flat-playlist", "--dump-json", feedURL)
	if err != nil {
		return nil, fmt.Errorf("fetch youtube feed: %w", err)
	}
	return m.parseYtDlpPlaylist(feedURL, out)
}
```

- [ ] **Step 4: Update `loadFeed` to dispatch based on feed type**

The current `loadFeed(feedURL string)` needs to know whether a feed is RSS or YouTube. Add a `feedType` parameter:

Change the signature to `loadFeed(feedURL string, feedType string)`:

```go
func (m *Module) loadFeed(feedURL string, feedType string) (*feedCache, error) {
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

	var fetched *cachedFeed
	var fetchErr error
	if feedType == "youtube" {
		fetched, fetchErr = m.fetchYoutubeFeed(feedURL)
	} else {
		fetched, fetchErr = m.fetchFeed(feedURL)
	}
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
```

- [ ] **Step 5: Update all `loadFeed` call sites to pass `ref.Type`**

Every call to `m.loadFeed(ref.URL)` becomes `m.loadFeed(ref.URL, ref.Type)`. There are five call sites (in `browseItems`, `browseLatest`, `searchItems`, `findEpisode`, `loadFeedByID`).

For `loadFeedByID`, it needs to know the type. Change its implementation to iterate `allFeeds()`:

```go
func (m *Module) loadFeedByID(feedID string) (*feedCache, error) {
	for _, ref := range m.allFeeds() {
		if hashID("feed", ref.URL) != feedID {
			continue
		}
		return m.loadFeed(ref.URL, ref.Type)
	}
	return nil, errors.New("feed not found")
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/modules/podcast_library/ -v`
Expected: All tests pass, including `TestBrowseYoutubeFeed`.

- [ ] **Step 7: Commit**

```bash
git add internal/modules/podcast_library/module.go internal/modules/podcast_library/module_test.go
git commit -m "feat(podcast): wire YouTube feeds into loadFeed dispatch"
```

---

### Task 6: Implement resolved URL cache and YouTube resolve

**Files:**
- Modify: `internal/modules/podcast_library/module.go`
- Test: `internal/modules/podcast_library/module_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/modules/podcast_library/module_test.go`:

```go
func TestResolveYoutubeEpisode(t *testing.T) {
	playlistURL := "https://www.youtube.com/playlist?list=PLabc"

	ytPlaylistOutput := `{"id":"abc123","title":"YT Episode","description":"desc","upload_date":"20240601","duration":3600,"uploader":"Chan","channel":"Chan","thumbnail":"https://img.test/1.jpg","playlist_title":"Playlist"}`
	resolvedStreamURL := "https://rr1---sn-abc.googlevideo.com/videoplayback?expire=999"

	ytDlpCalls := int32(0)
	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:           "mu:library:podcast:test",
		TopicBase:        mu.BaseTopic,
		YoutubePlaylists: []string{playlistURL},
		CacheDir:         t.TempDir(),
		RefreshInterval:  24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	module.ytDlpRunner = func(ctx context.Context, args ...string) ([]byte, error) {
		atomic.AddInt32(&ytDlpCalls, 1)
		// Check if this is a resolve call (-g flag) or a playlist fetch.
		for _, arg := range args {
			if arg == "-g" {
				return []byte(resolvedStreamURL + "\n"), nil
			}
		}
		return []byte(ytPlaylistOutput), nil
	}

	// Browse to get the episode ID.
	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{})}
	reply := module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var browse libraryItemsReply
	json.Unmarshal(reply.Body, &browse)
	feedID := browse.Items[1].ItemID

	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: feedID})}
	reply = module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	json.Unmarshal(reply.Body, &browse)
	episodeID := browse.Items[0].ItemID

	// Resolve the YouTube episode.
	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveBody{ItemID: episodeID})}
	resolve := module.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if !resolve.OK {
		t.Fatalf("expected OK resolve, got error: %+v", resolve.Err)
	}
	var resolveBody mu.LibraryResolveReply
	json.Unmarshal(resolve.Body, &resolveBody)

	if len(resolveBody.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(resolveBody.Sources))
	}
	if resolveBody.Sources[0].URL != resolvedStreamURL {
		t.Fatalf("expected stream URL, got %q", resolveBody.Sources[0].URL)
	}
	if resolveBody.Metadata["album"] != "Playlist" {
		t.Fatalf("expected album 'Playlist', got %q", resolveBody.Metadata["album"])
	}

	// Resolve again — should use cache, not call yt-dlp again.
	callsBefore := atomic.LoadInt32(&ytDlpCalls)
	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveBody{ItemID: episodeID})}
	resolve = module.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if !resolve.OK {
		t.Fatalf("second resolve failed")
	}
	json.Unmarshal(resolve.Body, &resolveBody)
	if resolveBody.Sources[0].URL != resolvedStreamURL {
		t.Fatalf("cached URL mismatch")
	}
	if atomic.LoadInt32(&ytDlpCalls) != callsBefore {
		t.Fatalf("expected cached resolve, but yt-dlp was called again")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/modules/podcast_library/ -run TestResolveYoutubeEpisode -v`
Expected: FAIL — resolveItem returns "episode has no audio url" because `ytid:` prefix isn't handled yet.

- [ ] **Step 3: Implement the resolved URL cache**

Add to `internal/modules/podcast_library/module.go`:

```go
type resolvedURLCache struct {
	mu      sync.Mutex
	entries map[string]resolvedURLEntry
}

type resolvedURLEntry struct {
	url       string
	expiresAt time.Time
}

const resolvedURLTTL = 4 * time.Hour

func (c *resolvedURLCache) get(videoID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[videoID]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(c.entries, videoID)
		return "", false
	}
	return entry.url, true
}

func (c *resolvedURLCache) set(videoID string, url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[videoID] = resolvedURLEntry{
		url:       url,
		expiresAt: time.Now().Add(resolvedURLTTL),
	}
}
```

Add the cache field to `Module`:

```go
type Module struct {
	log           *zap.Logger
	client        *mqttserver.Client
	http          *http.Client
	config        Config
	cmdTopic      string
	cmdQueue      chan cmdWork
	dedup         *mu.CommandDedup
	cacheMu       sync.Mutex
	feeds         map[string]*feedCache
	ytDlpRunner   func(ctx context.Context, args ...string) ([]byte, error)
	resolvedURLs  resolvedURLCache
}
```

Initialize it in `NewModule`:

```go
m := &Module{
	// ... existing fields ...
	resolvedURLs: resolvedURLCache{entries: make(map[string]resolvedURLEntry)},
}
```

- [ ] **Step 4: Update `resolveItem` to handle `ytid:` prefix**

Update `resolveItem` in `internal/modules/podcast_library/module.go`:

```go
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

	audioURL := episode.AudioURL
	if strings.HasPrefix(audioURL, ytidPrefix) {
		videoID := strings.TrimPrefix(audioURL, ytidPrefix)
		resolved, err := m.resolveYoutubeURL(videoID)
		if err != nil {
			return nil, nil, fmt.Errorf("youtube resolve: %w", err)
		}
		audioURL = resolved
	}

	if audioURL == "" {
		return nil, nil, errors.New("episode has no audio url")
	}
	source := mu.ResolvedSource{
		URL:       audioURL,
		Mime:      episode.AudioType,
		ByteRange: false,
	}
	return metadata, []mu.ResolvedSource{source}, nil
}

func (m *Module) resolveYoutubeURL(videoID string) (string, error) {
	if url, ok := m.resolvedURLs.get(videoID); ok {
		return url, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := m.ytDlpRunner(ctx, "-f", "bestaudio", "-g", "--no-playlist", "https://www.youtube.com/watch?v="+videoID)
	if err != nil {
		return "", err
	}

	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", errors.New("yt-dlp returned empty URL")
	}

	m.resolvedURLs.set(videoID, url)
	return url, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/modules/podcast_library/ -v`
Expected: All tests pass, including `TestResolveYoutubeEpisode` (both resolve and cache hit).

- [ ] **Step 6: Commit**

```bash
git add internal/modules/podcast_library/module.go internal/modules/podcast_library/module_test.go
git commit -m "feat(podcast): implement YouTube resolve with URL cache"
```

---

### Task 7: Add yt-dlp to Docker image

**Files:**
- Modify: `Dockerfile`

- [ ] **Step 1: Add yt-dlp to the `mud` runtime stage**

In `Dockerfile`, update the `mud` stage's `apt-get install` to add `python3` and install yt-dlp via pip:

```dockerfile
FROM ubuntu:24.04 AS mud
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    glib-networking \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav \
    gstreamer1.0-alsa \
    gstreamer1.0-tools \
    gstreamer1.0-pipewire \
    libchromaprint1 \
    alsa-utils \
    libupnp17t64 \
    libasound2t64 \
    libgstreamer1.0-0 \
    libglib2.0-0t64 \
    python3 \
    python3-pip \
    python3-certifi \
 && pip3 install --break-system-packages yt-dlp \
 && rm -rf /var/lib/apt/lists/*
```

- [ ] **Step 2: Verify Docker build**

Run: `docker build --target mud -t mu-mud:test .`
Expected: Builds successfully. `yt-dlp` is available in the image.

- [ ] **Step 3: Commit**

```bash
git add Dockerfile
git commit -m "build: add yt-dlp to mud Docker image"
```

---

### Task 8: End-to-end verification

This task is manual verification, not automated tests.

- [ ] **Step 1: Add a YouTube playlist to your config**

Add to your `mud.toml`:

```toml
[modules.podcast.default]
youtube_playlists = [
    "https://www.youtube.com/playlist?list=<your-playlist-id>",
]
```

- [ ] **Step 2: Start mud and verify browse**

Run mud and browse the podcast library. The YouTube playlist should appear as a container alongside RSS feeds. Browsing into it should show video episodes.

- [ ] **Step 3: Verify resolve and playback**

Select a YouTube episode and play it. The renderer should receive a direct audio stream URL and play it through GStreamer.

- [ ] **Step 4: Verify cache fallback**

Stop the service, delete the yt-dlp binary temporarily, restart. The cached feed metadata should still be browsable (resolve will fail gracefully).
