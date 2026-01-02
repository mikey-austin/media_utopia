package podcastlibrary

import (
	"bytes"
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
