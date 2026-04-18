package jellyfinlibrary

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

const testNodeID = "mu:library:jellyfin:test:default"

func mkRef(itemID string) mu.LibraryItemRef {
	return mu.NewLibraryItemRef(testNodeID, itemID)
}

func TestLibraryBrowse(t *testing.T) {
	handler := newJellyfinTestHandler(t)

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: "root", Start: 0, Count: 2})}
	reply := module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	var payload libraryItemsReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("expected 1 item")
	}
	if payload.Items[0].ContainerID == "" {
		t.Fatalf("expected container id")
	}
	if payload.Items[0].ImageURL == "" {
		t.Fatalf("expected image url")
	}
}

func TestLibraryGetItem(t *testing.T) {
	handler := newJellyfinTestHandler(t)

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryGetItemBody{Ref: mkRef("item-1")})}
	reply := module.libraryGetItem(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if !reply.OK {
		t.Fatalf("unexpected error reply: %+v", reply.Err)
	}

	var payload mu.LibraryGetItemReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if payload.Ref.ItemID != "item-1" {
		t.Fatalf("expected item id, got %s", payload.Ref.ItemID)
	}
	if payload.Display == nil {
		t.Fatalf("expected display populated")
	}
	if payload.Display.Title != "Song" {
		t.Fatalf("expected title Song, got %q", payload.Display.Title)
	}
	if payload.Display.Artist != "Artist" {
		t.Fatalf("expected artist, got %q", payload.Display.Artist)
	}
	if payload.Display.ArtworkURL == "" {
		t.Fatalf("expected artwork url")
	}
	if payload.Attributes["type"] != "Audio" {
		t.Fatalf("expected attributes.type=Audio, got %v", payload.Attributes["type"])
	}
}

func TestLibraryResolveSources(t *testing.T) {
	handler := newJellyfinTestHandler(t)

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveSourcesBody{Ref: mkRef("item-1")})}
	reply := module.libraryResolveSources(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if !reply.OK {
		t.Fatalf("unexpected error reply: %+v", reply.Err)
	}

	var payload mu.LibraryResolveSourcesReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if payload.Ref.ItemID != "item-1" {
		t.Fatalf("expected item id, got %s", payload.Ref.ItemID)
	}
	if len(payload.Sources) != 1 || payload.Sources[0].URL == "" {
		t.Fatalf("expected source")
	}
}

func TestLibraryResolveSourcesBatch(t *testing.T) {
	handler := http.NewServeMux()

	handler.HandleFunc("/Items/item-1", func(w http.ResponseWriter, r *http.Request) {
		resp := jfItem{ID: "item-1", Name: "Song 1", Type: "Audio", MediaType: "Audio"}
		writeJSON(t, w, resp)
	})
	handler.HandleFunc("/Items/item-2", func(w http.ResponseWriter, r *http.Request) {
		resp := jfItem{ID: "item-2", Name: "Song 2", Type: "Audio", MediaType: "Audio"}
		writeJSON(t, w, resp)
	})
	handler.HandleFunc("/Items/item-1/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		resp := jfPlaybackInfo{MediaSources: []jfMediaSource{{
			DirectStreamURL:      "/Audio/item-1/stream?api_key=key",
			Container:            "mp3",
			SupportsDirectStream: true,
		}}}
		writeJSON(t, w, resp)
	})
	handler.HandleFunc("/Items/item-2/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		resp := jfPlaybackInfo{MediaSources: []jfMediaSource{{
			DirectStreamURL:      "/Audio/item-2/stream?api_key=key",
			Container:            "mp3",
			SupportsDirectStream: true,
		}}}
		writeJSON(t, w, resp)
	})

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:   testNodeID,
			BaseURL:  "http://jellyfin.test",
			APIKey:   "key",
			UserID:   "user",
			CacheTTL: time.Minute,
		},
	}
	module.cache = newCache(module.config.CacheSize)

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveSourcesBatchBody{Refs: []mu.LibraryItemRef{
		mkRef("item-1"),
		mkRef("item-2"),
	}})}
	reply := module.libraryResolveSourcesBatch(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	var payload mu.LibraryResolveSourcesBatchReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 items")
	}
	for _, item := range payload.Items {
		if item.Err != nil {
			t.Fatalf("unexpected error: %v", item.Err.Message)
		}
		if len(item.Sources) != 1 {
			t.Fatalf("expected sources")
		}
	}
}

func TestLibraryGetItems(t *testing.T) {
	handler := newJellyfinTestHandler(t)

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryGetItemsBody{Refs: []mu.LibraryItemRef{
		mkRef("item-1"),
		mu.NewLibraryItemRef("mu:library:jellyfin:other:default", "item-1"),
	}})}
	reply := module.libraryGetItems(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	var payload mu.LibraryGetItemsReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(payload.Items))
	}
	if payload.Items[0].Err != nil {
		t.Fatalf("unexpected error on matching ref: %+v", payload.Items[0].Err)
	}
	if payload.Items[0].Display == nil || payload.Items[0].Display.Title == "" {
		t.Fatalf("expected display populated")
	}
	if payload.Items[1].Err == nil {
		t.Fatalf("expected error for mismatched libraryId")
	}
}

func TestLibraryResolveSourcesUsesCache(t *testing.T) {
	var itemHits int
	var playbackHits int
	handler := http.NewServeMux()

	handler.HandleFunc("/Items/item-1", func(w http.ResponseWriter, r *http.Request) {
		itemHits++
		resp := jfItem{ID: "item-1", Name: "Song", Type: "Audio", MediaType: "Audio"}
		writeJSON(t, w, resp)
	})
	handler.HandleFunc("/Items/item-1/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		playbackHits++
		resp := jfPlaybackInfo{MediaSources: []jfMediaSource{{
			DirectStreamURL:      "/Audio/item-1/stream?api_key=key",
			Container:            "mp3",
			SupportsDirectStream: true,
		}}}
		writeJSON(t, w, resp)
	})

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:   testNodeID,
			BaseURL:  "http://jellyfin.test",
			APIKey:   "key",
			UserID:   "user",
			CacheTTL: time.Minute,
		},
	}
	module.cache = newCache(module.config.CacheSize)

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveSourcesBody{Ref: mkRef("item-1")})}
	_ = module.libraryResolveSources(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	value, err := module.cache.Get(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("expected cached entry: %v", err)
	}
	var entry resolveCacheEntry
	if err := json.Unmarshal(value, &entry); err != nil {
		t.Fatalf("decode cache entry: %v", err)
	}
	if !entry.SourcesReady {
		t.Fatalf("expected sourcesReady=true in cache entry")
	}
	_ = module.libraryResolveSources(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	if itemHits != 1 || playbackHits != 1 {
		t.Fatalf("expected cache hits item=%d playback=%d", itemHits, playbackHits)
	}
}

func TestLibrarySearch(t *testing.T) {
	handler := newJellyfinTestHandler(t)

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibrarySearchBody{Query: "Song", Start: 0, Count: 10})}
	reply := module.librarySearch(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	var payload libraryItemsReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if payload.Total != 1 {
		t.Fatalf("expected total 1")
	}
}

func TestLibrarySearchTypes(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/Users/user/Items", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("IncludeItemTypes") != "Audio,MusicAlbum" {
			t.Fatalf("unexpected IncludeItemTypes: %s", r.URL.Query().Get("IncludeItemTypes"))
		}
		resp := jfItemsResponse{
			Items:            []jfItem{},
			TotalRecordCount: 0,
			StartIndex:       0,
		}
		writeJSON(t, w, resp)
	})

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibrarySearchBody{Query: "Song", Start: 0, Count: 10, Types: []string{"Audio", "MusicAlbum"}})}
	reply := module.librarySearch(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	if reply.Type != "ack" || !reply.OK {
		t.Fatalf("expected ack reply")
	}
}

func TestLibraryResolveSourcesExpandsAlbum(t *testing.T) {
	handler := http.NewServeMux()

	handler.HandleFunc("/Items/album-1", func(w http.ResponseWriter, r *http.Request) {
		resp := jfItem{
			ID:           "album-1",
			Name:         "Album",
			Type:         "MusicAlbum",
			MediaType:    "",
			RunTimeTicks: 0,
			Artists:      []string{"Artist"},
			Album:        "Album",
			ImageTags:    map[string]string{"Primary": "tag"},
		}
		writeJSON(t, w, resp)
	})

	handler.HandleFunc("/Users/user/Items", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ParentId") != "album-1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		resp := jfItemsResponse{
			Items: []jfItem{
				{ID: "track-1", Name: "Track 1", Type: "Audio", MediaType: "Audio"},
				{ID: "track-2", Name: "Track 2", Type: "Audio", MediaType: "Audio"},
			},
			TotalRecordCount: 2,
			StartIndex:       0,
		}
		writeJSON(t, w, resp)
	})

	handler.HandleFunc("/Items/track-1/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		resp := jfPlaybackInfo{MediaSources: []jfMediaSource{{
			DirectStreamURL:      "/Audio/track-1/stream?api_key=key",
			Container:            "mp3",
			SupportsDirectStream: true,
		}}}
		writeJSON(t, w, resp)
	})

	handler.HandleFunc("/Items/track-2/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		resp := jfPlaybackInfo{MediaSources: []jfMediaSource{{
			DirectStreamURL:      "/Audio/track-2/stream?api_key=key",
			Container:            "mp3",
			SupportsDirectStream: true,
		}}}
		writeJSON(t, w, resp)
	})

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveSourcesBody{Ref: mkRef("album-1")})}
	reply := module.libraryResolveSources(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	var payload mu.LibraryResolveSourcesReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(payload.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(payload.Sources))
	}
}

func TestLibraryResolveSourcesExpandsPlaylist(t *testing.T) {
	handler := http.NewServeMux()

	handler.HandleFunc("/Items/playlist-1", func(w http.ResponseWriter, r *http.Request) {
		resp := jfItem{
			ID:        "playlist-1",
			Name:      "Playlist",
			Type:      "Playlist",
			ImageTags: map[string]string{"Primary": "tag"},
		}
		writeJSON(t, w, resp)
	})

	handler.HandleFunc("/Playlists/playlist-1/Items", func(w http.ResponseWriter, r *http.Request) {
		resp := jfItemsResponse{
			Items: []jfItem{
				{ID: "track-1", Name: "Track 1", Type: "Audio", MediaType: "Audio"},
			},
			TotalRecordCount: 1,
			StartIndex:       0,
		}
		writeJSON(t, w, resp)
	})

	handler.HandleFunc("/Items/track-1/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		resp := jfPlaybackInfo{MediaSources: []jfMediaSource{{
			DirectStreamURL:      "/Audio/track-1/stream?api_key=key",
			Container:            "mp3",
			SupportsDirectStream: true,
		}}}
		writeJSON(t, w, resp)
	})

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveSourcesBody{Ref: mkRef("playlist-1")})}
	reply := module.libraryResolveSources(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	var payload mu.LibraryResolveSourcesReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(payload.Sources) != 1 {
		t.Fatalf("expected 1 source")
	}
}

func TestLibrarySearchRespectsTimeout(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(jfItemsResponse{})
	})

	module := Module{
		http: &http.Client{Timeout: 5 * time.Millisecond, Transport: roundTripper{handler: handler}},
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	_, _, err := module.fetchItems("", 0, 10, "", nil, true)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}

func TestResolveSourceFallback(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	module := Module{
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	source, err := module.resolveSource("item", jfItem{MediaType: "Audio"})
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if !strings.Contains(source.URL, "/Audio/item/stream") {
		t.Fatalf("expected stream url")
	}
}

func TestNewModuleDefaults(t *testing.T) {
	module, err := NewModule(zap.NewNop(), nil, Config{
		NodeID:  testNodeID,
		BaseURL: "http://example",
		APIKey:  "key",
		UserID:  "user",
	})
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	if module.config.TopicBase != mu.BaseTopic {
		t.Fatalf("expected default topic base")
	}
	if module.config.Timeout == 0 {
		t.Fatalf("expected timeout")
	}
}

func TestNewModuleValidation(t *testing.T) {
	_, err := NewModule(zap.NewNop(), nil, Config{})
	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestMimeForContainer(t *testing.T) {
	item := jfItem{MediaType: "Video"}
	mime := mimeForContainer(item, "mp4")
	if mime != "video/mp4" {
		t.Fatalf("expected video/mp4")
	}
	item.MediaType = "Audio"
	mime = mimeForContainer(item, "flac")
	if mime != "audio/flac" {
		t.Fatalf("expected audio/flac")
	}
}

func TestTicksToMS(t *testing.T) {
	if ticksToMS(10000) != 1 {
		t.Fatalf("expected 1ms")
	}
	if ticksToMS(0) != 0 {
		t.Fatalf("expected 0")
	}
}

func TestAbsoluteURL(t *testing.T) {
	module := Module{config: Config{BaseURL: "http://example"}}
	if module.absoluteURL("/foo") != "http://example/foo" {
		t.Fatalf("expected absolute url")
	}
	if module.absoluteURL("https://x") != "https://x" {
		t.Fatalf("expected passthrough url")
	}
}

func TestLibraryGetItemMissing(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/Items/nonexistent", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryGetItemBody{Ref: mkRef("nonexistent")})}
	reply := module.libraryGetItem(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	if reply.OK {
		t.Fatalf("expected error reply for missing item")
	}
	if reply.Type != "error" {
		t.Fatalf("expected type=error, got %s", reply.Type)
	}
	if reply.Err == nil {
		t.Fatalf("expected err to be set")
	}
	if !strings.Contains(reply.Err.Message, "404") {
		t.Fatalf("expected 404 in error message, got: %s", reply.Err.Message)
	}
}

func TestLibraryGetItemRejectsInvalidRef(t *testing.T) {
	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(http.NewServeMux()),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	// Wrong libraryId
	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryGetItemBody{Ref: mu.NewLibraryItemRef("mu:library:jellyfin:other:default", "item-1")})}
	reply := module.libraryGetItem(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if reply.OK || reply.Err == nil {
		t.Fatalf("expected mismatched library error")
	}

	// Empty itemId — Validate() should reject
	cmd = mu.CommandEnvelope{Body: mustJSON(mu.LibraryGetItemBody{Ref: mkRef("")})}
	reply = module.libraryGetItem(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})
	if reply.OK || reply.Err == nil {
		t.Fatalf("expected validation error for empty itemId")
	}
}

func TestLibraryBrowseEmpty(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/Items/empty-container", func(w http.ResponseWriter, r *http.Request) {
		resp := jfItem{
			ID:   "empty-container",
			Name: "Empty",
			Type: "CollectionFolder",
		}
		writeJSON(t, w, resp)
	})
	handler.HandleFunc("/Users/user/Items", func(w http.ResponseWriter, r *http.Request) {
		resp := jfItemsResponse{
			Items:            []jfItem{},
			TotalRecordCount: 0,
			StartIndex:       0,
		}
		writeJSON(t, w, resp)
	})

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: "empty-container", Start: 0, Count: 10})}
	reply := module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	if !reply.OK {
		t.Fatalf("expected ok reply, got err: %+v", reply.Err)
	}
	var payload libraryItemsReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(payload.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(payload.Items))
	}
	if payload.Total != 0 {
		t.Fatalf("expected total=0, got %d", payload.Total)
	}
}

func TestLibrarySearchEmpty(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/Users/user/Items", func(w http.ResponseWriter, r *http.Request) {
		resp := jfItemsResponse{
			Items:            []jfItem{},
			TotalRecordCount: 0,
			StartIndex:       0,
		}
		writeJSON(t, w, resp)
	})

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibrarySearchBody{Query: "zzz_no_match", Start: 0, Count: 10})}
	reply := module.librarySearch(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	if !reply.OK {
		t.Fatalf("expected ok reply, got err: %+v", reply.Err)
	}
	var payload libraryItemsReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(payload.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(payload.Items))
	}
	if payload.Total != 0 {
		t.Fatalf("expected total=0, got %d", payload.Total)
	}
}

func TestLibraryBrowsePagination(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/Users/user/Items", func(w http.ResponseWriter, r *http.Request) {
		startStr := r.URL.Query().Get("StartIndex")
		limitStr := r.URL.Query().Get("Limit")
		start, _ := strconv.ParseInt(startStr, 10, 64)
		limit, _ := strconv.ParseInt(limitStr, 10, 64)

		if start != 5 {
			t.Errorf("expected StartIndex=5, got %d", start)
		}
		if limit != 3 {
			t.Errorf("expected Limit=3, got %d", limit)
		}

		resp := jfItemsResponse{
			Items: []jfItem{
				{ID: "item-a", Name: "A", Type: "Audio", MediaType: "Audio"},
				{ID: "item-b", Name: "B", Type: "Audio", MediaType: "Audio"},
			},
			TotalRecordCount: 20,
			StartIndex:       start,
		}
		writeJSON(t, w, resp)
	})

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			NodeID:  testNodeID,
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryBrowseBody{ContainerID: "root", Start: 5, Count: 3})}
	reply := module.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	if !reply.OK {
		t.Fatalf("expected ok reply, got err: %+v", reply.Err)
	}
	var payload libraryItemsReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if payload.Start != 5 {
		t.Fatalf("expected start=5, got %d", payload.Start)
	}
	if payload.Count != 2 {
		t.Fatalf("expected count=2 (actual items returned), got %d", payload.Count)
	}
	if payload.Total != 20 {
		t.Fatalf("expected total=20, got %d", payload.Total)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(payload.Items))
	}
}

func TestNewModuleRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "missing base_url",
			cfg: Config{
				NodeID: testNodeID,
				APIKey: "key",
				UserID: "user",
			},
			want: "base_url required",
		},
		{
			name: "missing api_key",
			cfg: Config{
				NodeID:  testNodeID,
				BaseURL: "http://example",
				UserID:  "user",
			},
			want: "api_key required",
		},
		{
			name: "missing node_id",
			cfg: Config{
				BaseURL: "http://example",
				APIKey:  "key",
				UserID:  "user",
			},
			want: "node_id required",
		},
		{
			name: "missing user_id",
			cfg: Config{
				NodeID:  testNodeID,
				BaseURL: "http://example",
				APIKey:  "key",
			},
			want: "user_id required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewModule(zap.NewNop(), nil, tc.cfg)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if err.Error() != tc.want {
				t.Fatalf("expected error %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func newJellyfinTestHandler(t *testing.T) http.Handler {
	handler := http.NewServeMux()

	handler.HandleFunc("/Users/user/Items", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp := jfItemsResponse{
			Items: []jfItem{{
				ID:           "item-1",
				Name:         "Song",
				Type:         "Audio",
				MediaType:    "Audio",
				RunTimeTicks: 900000000,
				Artists:      []string{"Artist"},
				Album:        "Album",
				ParentID:     "root",
				ImageTags:    map[string]string{"Primary": "tag"},
			}},
			TotalRecordCount: 1,
			StartIndex:       0,
		}
		writeJSON(t, w, resp)
	})

	handler.HandleFunc("/Items/item-1", func(w http.ResponseWriter, r *http.Request) {
		resp := jfItem{
			ID:           "item-1",
			Name:         "Song",
			Type:         "Audio",
			MediaType:    "Audio",
			RunTimeTicks: 900000000,
			Artists:      []string{"Artist"},
			Album:        "Album",
			Overview:     "Overview",
			ImageTags:    map[string]string{"Primary": "tag"},
		}
		writeJSON(t, w, resp)
	})

	handler.HandleFunc("/Items/item-1/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		if len(strings.TrimSpace(string(buf))) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := jfPlaybackInfo{MediaSources: []jfMediaSource{{
			DirectStreamURL:      "/Videos/item-1/stream?api_key=key",
			Container:            "mp3",
			SupportsDirectStream: true,
		}}}
		writeJSON(t, w, resp)
	})

	handler.HandleFunc("/Items/item-1/Images/Primary", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return handler
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}

func newTestClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: roundTripper{handler: handler}}
}

type roundTripper struct {
	handler http.Handler
}

func (rt roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	respCh := make(chan *http.Response, 1)

	go func() {
		recorder := httptest.NewRecorder()
		bodyBytes, _ := io.ReadAll(req.Body)
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		rt.handler.ServeHTTP(recorder, req)
		respCh <- recorder.Result()
	}()

	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case resp := <-respCh:
		return resp, nil
	}
}
