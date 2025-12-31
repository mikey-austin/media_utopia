package jellyfinlibrary

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

func TestLibraryBrowse(t *testing.T) {
	handler := newJellyfinTestHandler(t)

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
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

func TestLibraryResolve(t *testing.T) {
	handler := newJellyfinTestHandler(t)

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveBody{ItemID: "item-1"})}
	reply := module.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	var payload mu.LibraryResolveReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if payload.ItemID != "item-1" {
		t.Fatalf("expected item id")
	}
	if len(payload.Sources) != 1 || payload.Sources[0].URL == "" {
		t.Fatalf("expected source")
	}
	if payload.Metadata["artworkUrl"] == nil {
		t.Fatalf("expected artwork url")
	}
}

func TestLibrarySearch(t *testing.T) {
	handler := newJellyfinTestHandler(t)

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
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

func TestLibraryResolveIncludesMetadata(t *testing.T) {
	handler := newJellyfinTestHandler(t)

	module := Module{
		log:  zap.NewNop(),
		http: newTestClient(handler),
		config: Config{
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveBody{ItemID: "item-1"})}
	reply := module.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	var payload mu.LibraryResolveReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if payload.Metadata["title"] != "Song" {
		t.Fatalf("expected title")
	}
	if payload.Metadata["artist"] != "Artist" {
		t.Fatalf("expected artist")
	}
}

func TestLibraryResolveExpandsAlbum(t *testing.T) {
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
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	cmd := mu.CommandEnvelope{Body: mustJSON(mu.LibraryResolveBody{ItemID: "album-1"})}
	reply := module.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	var payload mu.LibraryResolveReply
	if err := json.Unmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if len(payload.Sources) != 2 {
		t.Fatalf("expected 2 sources")
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
			BaseURL: "http://jellyfin.test",
			APIKey:  "key",
			UserID:  "user",
		},
	}

	_, _, err := module.fetchItems("", 0, 10, "")
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
		NodeID:  "mu:library:jellyfin:test",
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
