package podcastlibrary

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

func TestLibraryBrowseAndResolve(t *testing.T) {
	feedCalls := int32(0)
	feedURL := "http://example.test/feed.xml"

	cacheDir := t.TempDir()
	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:          "mu:library:podcast:test",
		TopicBase:       mu.BaseTopic,
		Feeds:           []string{feedURL},
		CacheDir:        cacheDir,
		RefreshInterval: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	module.http = &http.Client{Transport: testTransport(func(_ *http.Request) (*http.Response, error) {
		atomic.AddInt32(&feedCalls, 1)
		return feedResponse(testFeed), nil
	})}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{})}
	reply := module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var browse libraryItemsReply
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse decode: %v", err)
	}
	if len(browse.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(browse.Items))
	}
	if browse.Items[0].ItemID != "latest" {
		t.Fatalf("expected latest folder first")
	}
	feedID := browse.Items[1].ItemID

	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: feedID})}
	reply = module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse episodes decode: %v", err)
	}
	if len(browse.Items) != 2 {
		t.Fatalf("expected 2 episodes, got %d", len(browse.Items))
	}

	episodeID := browse.Items[0].ItemID
	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveBody{ItemID: episodeID})}
	resolve := module.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var resolveBody mu.LibraryResolveReply
	if err := json.Unmarshal(resolve.Body, &resolveBody); err != nil {
		t.Fatalf("resolve decode: %v", err)
	}
	if resolveBody.ItemID != episodeID {
		t.Fatalf("expected item %s, got %s", episodeID, resolveBody.ItemID)
	}
	if resolveBody.Metadata["album"] == "" {
		t.Fatalf("expected album metadata")
	}
	if len(resolveBody.Sources) != 1 {
		t.Fatalf("expected 1 source")
	}

	if atomic.LoadInt32(&feedCalls) != 1 {
		t.Fatalf("expected 1 feed fetch, got %d", atomic.LoadInt32(&feedCalls))
	}

	cachePath := filepath.Join(cacheDir, safeFilename("mu:library:podcast:test"), "podcast_"+hashID("feed", feedURL)+".json")
	if !strings.Contains(cachePath, "podcast_feed_") {
		t.Fatalf("unexpected cache path %s", cachePath)
	}
}

func TestBrowseLatestFolder(t *testing.T) {
	feedURL := "http://example.test/feed.xml"

	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:          "mu:library:podcast:test",
		TopicBase:       mu.BaseTopic,
		Feeds:           []string{feedURL},
		CacheDir:        t.TempDir(),
		RefreshInterval: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	module.http = &http.Client{Transport: testTransport(func(_ *http.Request) (*http.Response, error) {
		return feedResponse(testFeed), nil
	})}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: "latest"})}
	reply := module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var browse libraryItemsReply
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse decode: %v", err)
	}
	if len(browse.Items) != 1 {
		t.Fatalf("expected 1 latest item, got %d", len(browse.Items))
	}
	if browse.Items[0].Album == "" {
		t.Fatalf("expected album name for latest item")
	}
}

func TestLibrarySearch(t *testing.T) {
	feedURL := "http://example.test/feed.xml"

	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:          "mu:library:podcast:test",
		TopicBase:       mu.BaseTopic,
		Feeds:           []string{feedURL},
		CacheDir:        t.TempDir(),
		RefreshInterval: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	module.http = &http.Client{Transport: testTransport(func(_ *http.Request) (*http.Response, error) {
		return feedResponse(testFeed), nil
	})}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibrarySearchBody{Query: "episode one"})}
	reply := module.librarySearch(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var out libraryItemsReply
	if err := json.Unmarshal(reply.Body, &out); err != nil {
		t.Fatalf("search decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(out.Items))
	}
}

func TestReverseSortByDate(t *testing.T) {
	feedURL := "http://example.test/feed.xml"

	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:            "mu:library:podcast:test",
		TopicBase:         mu.BaseTopic,
		Feeds:             []string{feedURL},
		CacheDir:          t.TempDir(),
		RefreshInterval:   24 * time.Hour,
		ReverseSortByDate: true,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	module.http = &http.Client{Transport: testTransport(func(_ *http.Request) (*http.Response, error) {
		return feedResponse(testFeed), nil
	})}

	feedID := hashID("feed", feedURL)
	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: feedID})}
	reply := module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var browse libraryItemsReply
	if err := json.Unmarshal(reply.Body, &browse); err != nil {
		t.Fatalf("browse decode: %v", err)
	}
	if len(browse.Items) != 2 {
		t.Fatalf("expected 2 episodes, got %d", len(browse.Items))
	}
	if !strings.Contains(browse.Items[0].Name, "Episode Two") {
		t.Fatalf("expected newest episode first, got %q", browse.Items[0].Name)
	}
}

func TestSearchNoResults(t *testing.T) {
	feedURL := "http://example.test/feed.xml"

	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:          "mu:library:podcast:test",
		TopicBase:       mu.BaseTopic,
		Feeds:           []string{feedURL},
		CacheDir:        t.TempDir(),
		RefreshInterval: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	module.http = &http.Client{Transport: testTransport(func(_ *http.Request) (*http.Response, error) {
		return feedResponse(testFeed), nil
	})}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibrarySearchBody{Query: "zzz_no_match_xyz"})}
	reply := module.librarySearch(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var out libraryItemsReply
	if err := json.Unmarshal(reply.Body, &out); err != nil {
		t.Fatalf("search decode: %v", err)
	}
	if len(out.Items) != 0 {
		t.Fatalf("expected 0 search results, got %d", len(out.Items))
	}
	if out.Total != 0 {
		t.Fatalf("expected total=0, got %d", out.Total)
	}
}

func TestBrowseNonexistentContainer(t *testing.T) {
	feedURL := "http://example.test/feed.xml"

	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:          "mu:library:podcast:test",
		TopicBase:       mu.BaseTopic,
		Feeds:           []string{feedURL},
		CacheDir:        t.TempDir(),
		RefreshInterval: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	module.http = &http.Client{Transport: testTransport(func(_ *http.Request) (*http.Response, error) {
		return feedResponse(testFeed), nil
	})}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: "nonexistent_container_id"})}
	reply := module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if reply.OK {
		t.Fatalf("expected error reply for nonexistent container")
	}
	if reply.Type != "error" {
		t.Fatalf("expected type=error, got %q", reply.Type)
	}
	if reply.Err == nil {
		t.Fatalf("expected Err to be set")
	}
	if reply.Err.Code != "INVALID" {
		t.Fatalf("expected error code INVALID, got %q", reply.Err.Code)
	}
}

func TestResolveNonexistentItem(t *testing.T) {
	feedURL := "http://example.test/feed.xml"

	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:          "mu:library:podcast:test",
		TopicBase:       mu.BaseTopic,
		Feeds:           []string{feedURL},
		CacheDir:        t.TempDir(),
		RefreshInterval: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	module.http = &http.Client{Transport: testTransport(func(_ *http.Request) (*http.Response, error) {
		return feedResponse(testFeed), nil
	})}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveBody{ItemID: "nonexistent_item_id"})}
	reply := module.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if reply.OK {
		t.Fatalf("expected error reply for nonexistent item")
	}
	if reply.Type != "error" {
		t.Fatalf("expected type=error, got %q", reply.Type)
	}
	if reply.Err == nil {
		t.Fatalf("expected Err to be set")
	}
	if reply.Err.Code != "INVALID" {
		t.Fatalf("expected error code INVALID, got %q", reply.Err.Code)
	}
}

func TestBrowsePagination(t *testing.T) {
	feedURL := "http://example.test/feed.xml"

	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:          "mu:library:podcast:test",
		TopicBase:       mu.BaseTopic,
		Feeds:           []string{feedURL},
		CacheDir:        t.TempDir(),
		RefreshInterval: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new module: %v", err)
	}
	module.http = &http.Client{Transport: testTransport(func(_ *http.Request) (*http.Response, error) {
		return feedResponse(testFeedFiveEpisodes), nil
	})}

	// First, browse root to get the feed ID.
	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{})}
	reply := module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var root libraryItemsReply
	if err := json.Unmarshal(reply.Body, &root); err != nil {
		t.Fatalf("root browse decode: %v", err)
	}
	if len(root.Items) < 2 {
		t.Fatalf("expected at least 2 root items, got %d", len(root.Items))
	}
	feedID := root.Items[1].ItemID

	// Browse all episodes (no pagination) to get the total count.
	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: feedID})}
	reply = module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var allEpisodes libraryItemsReply
	if err := json.Unmarshal(reply.Body, &allEpisodes); err != nil {
		t.Fatalf("all episodes decode: %v", err)
	}
	if allEpisodes.Total != 5 {
		t.Fatalf("expected total=5, got %d", allEpisodes.Total)
	}
	if len(allEpisodes.Items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(allEpisodes.Items))
	}

	// Page 1: start=0, count=2
	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: feedID, Start: 0, Count: 2})}
	reply = module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var page1 libraryItemsReply
	if err := json.Unmarshal(reply.Body, &page1); err != nil {
		t.Fatalf("page1 decode: %v", err)
	}
	if page1.Total != 5 {
		t.Fatalf("page1: expected total=5, got %d", page1.Total)
	}
	if len(page1.Items) != 2 {
		t.Fatalf("page1: expected 2 items, got %d", len(page1.Items))
	}
	if page1.Start != 0 {
		t.Fatalf("page1: expected start=0, got %d", page1.Start)
	}
	if page1.Count != 2 {
		t.Fatalf("page1: expected count=2, got %d", page1.Count)
	}

	// Page 2: start=2, count=2
	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: feedID, Start: 2, Count: 2})}
	reply = module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var page2 libraryItemsReply
	if err := json.Unmarshal(reply.Body, &page2); err != nil {
		t.Fatalf("page2 decode: %v", err)
	}
	if len(page2.Items) != 2 {
		t.Fatalf("page2: expected 2 items, got %d", len(page2.Items))
	}
	if page2.Start != 2 {
		t.Fatalf("page2: expected start=2, got %d", page2.Start)
	}

	// Page 3: start=4, count=2 (should return only 1 item)
	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: feedID, Start: 4, Count: 2})}
	reply = module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var page3 libraryItemsReply
	if err := json.Unmarshal(reply.Body, &page3); err != nil {
		t.Fatalf("page3 decode: %v", err)
	}
	if len(page3.Items) != 1 {
		t.Fatalf("page3: expected 1 item, got %d", len(page3.Items))
	}
	if page3.Total != 5 {
		t.Fatalf("page3: expected total=5, got %d", page3.Total)
	}

	// Page beyond range: start=10, count=2 (should return 0 items)
	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: feedID, Start: 10, Count: 2})}
	reply = module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	var pageBeyond libraryItemsReply
	if err := json.Unmarshal(reply.Body, &pageBeyond); err != nil {
		t.Fatalf("pageBeyond decode: %v", err)
	}
	if len(pageBeyond.Items) != 0 {
		t.Fatalf("pageBeyond: expected 0 items, got %d", len(pageBeyond.Items))
	}
	if pageBeyond.Total != 5 {
		t.Fatalf("pageBeyond: expected total=5, got %d", pageBeyond.Total)
	}

	// Verify no duplicate items across pages.
	seen := map[string]bool{}
	for _, item := range page1.Items {
		seen[item.ItemID] = true
	}
	for _, item := range page2.Items {
		if seen[item.ItemID] {
			t.Fatalf("duplicate item %s across pages", item.ItemID)
		}
		seen[item.ItemID] = true
	}
	for _, item := range page3.Items {
		if seen[item.ItemID] {
			t.Fatalf("duplicate item %s across pages", item.ItemID)
		}
	}
}

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

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

type testTransport func(*http.Request) (*http.Response, error)

func (t testTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t(r)
}

func feedResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/rss+xml"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

const testFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">
<channel>
  <title>Sample Podcast</title>
  <description>Sample podcast feed</description>
  <itunes:author>Sample Host</itunes:author>
  <image>
    <url>https://example.com/podcast.png</url>
  </image>
  <item>
    <title>Episode One</title>
    <guid>ep-1</guid>
    <description>First episode</description>
    <pubDate>Mon, 01 Jan 2024 10:00:00 GMT</pubDate>
    <enclosure url="https://example.com/audio1.mp3" length="123" type="audio/mpeg"/>
    <itunes:duration>01:02:03</itunes:duration>
    <itunes:image href="https://example.com/ep1.png"/>
  </item>
  <item>
    <title>Episode Two</title>
    <guid>ep-2</guid>
    <description>Second episode</description>
    <pubDate>Tue, 02 Jan 2024 10:00:00 GMT</pubDate>
    <enclosure url="https://example.com/audio2.mp3" length="456" type="audio/mpeg"/>
  </item>
</channel>
</rss>`

const testFeedFiveEpisodes = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">
<channel>
  <title>Big Podcast</title>
  <description>A podcast with many episodes</description>
  <itunes:author>Big Host</itunes:author>
  <item>
    <title>Alpha</title>
    <guid>ep-a</guid>
    <description>Alpha episode</description>
    <pubDate>Mon, 01 Jan 2024 10:00:00 GMT</pubDate>
    <enclosure url="https://example.com/a.mp3" length="100" type="audio/mpeg"/>
  </item>
  <item>
    <title>Bravo</title>
    <guid>ep-b</guid>
    <description>Bravo episode</description>
    <pubDate>Tue, 02 Jan 2024 10:00:00 GMT</pubDate>
    <enclosure url="https://example.com/b.mp3" length="200" type="audio/mpeg"/>
  </item>
  <item>
    <title>Charlie</title>
    <guid>ep-c</guid>
    <description>Charlie episode</description>
    <pubDate>Wed, 03 Jan 2024 10:00:00 GMT</pubDate>
    <enclosure url="https://example.com/c.mp3" length="300" type="audio/mpeg"/>
  </item>
  <item>
    <title>Delta</title>
    <guid>ep-d</guid>
    <description>Delta episode</description>
    <pubDate>Thu, 04 Jan 2024 10:00:00 GMT</pubDate>
    <enclosure url="https://example.com/d.mp3" length="400" type="audio/mpeg"/>
  </item>
  <item>
    <title>Echo</title>
    <guid>ep-e</guid>
    <description>Echo episode</description>
    <pubDate>Fri, 05 Jan 2024 10:00:00 GMT</pubDate>
    <enclosure url="https://example.com/e.mp3" length="500" type="audio/mpeg"/>
  </item>
</channel>
</rss>`
