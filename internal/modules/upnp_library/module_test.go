//go:build upnp

package upnplibrary

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
	"go.uber.org/zap"
)

func TestRootBrowseListsServers(t *testing.T) {
	mod := testModule(t, nil)
	mod.upnp = nil
	mod.servers["srv-1"] = &mediaServer{ID: "srv-1", FriendlyName: "Library A", BaseURL: "http://cd.test"}
	mod.servers["srv-2"] = &mediaServer{ID: "srv-2", FriendlyName: "Library B", BaseURL: "http://cd.test"}
	mod.lastDiscovery = time.Now()

	cmd := mu.CommandEnvelope{Body: mustJSON(t, mu.LibraryBrowseBody{ContainerID: "", Start: 0, Count: 10})}
	reply := mod.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	if !reply.OK {
		t.Fatalf("expected ok")
	}
	var payload libraryItemsReply
	if err := jsonUnmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 libraries, got %d", len(payload.Items))
	}
	if payload.Items[0].ItemID == "" || payload.Items[1].ItemID == "" {
		t.Fatalf("missing item ids")
	}
}

func TestBrowseContainerParsesDIDL(t *testing.T) {
	didl := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">
<container id="c1" parentID="0" restricted="1"><dc:title>Albums</dc:title><upnp:class>object.container.album.musicAlbum</upnp:class></container>
<item id="track-1" parentID="0" restricted="1"><dc:title>Song</dc:title><upnp:class>object.item.audioItem.musicTrack</upnp:class><upnp:album>Album</upnp:album><upnp:artist>Artist</upnp:artist><res protocolInfo="http-get:*:audio/flac:DLNA.ORG_OP=01">/media/song.flac</res><upnp:albumArtURI>/art.jpg</upnp:albumArtURI></item>
</DIDL-Lite>`
	var escaped bytes.Buffer
	if err := xml.EscapeText(&escaped, []byte(didl)); err != nil {
		t.Fatalf("escape text: %v", err)
	}
	soap := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>
<u:BrowseResponse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">
<Result>` + escaped.String() + `</Result><NumberReturned>2</NumberReturned><TotalMatches>2</TotalMatches><UpdateID>1</UpdateID>
</u:BrowseResponse></s:Body></s:Envelope>`

	var reqCount int
	handler := func(r *http.Request) *http.Response {
		reqCount++
		if r.Header.Get("SOAPAction") == "" {
			t.Fatalf("missing SOAPAction")
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(soap)),
			Header:     make(http.Header),
		}
	}
	server := &mediaServer{ID: "srv-1", FriendlyName: "Library A", BaseURL: "http://cd.test", ControlURL: "http://cd.test/control", IconURL: "http://cd.test/icon.png"}
	mod := testModule(t, handler)
	mod.upnp = nil
	mod.servers["srv-1"] = server
	mod.lastDiscovery = time.Now()

	cmd := mu.CommandEnvelope{Body: mustJSON(t, mu.LibraryBrowseBody{ContainerID: makeContainerID(server.ID, "0"), Start: 0, Count: 10})}
	reply := mod.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	if !reply.OK {
		t.Fatalf("expected ok")
	}
	if reqCount == 0 {
		t.Fatalf("expected SOAP call")
	}

	var payload libraryItemsReply
	if err := jsonUnmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(payload.Items))
	}
	var foundTrack bool
	for _, item := range payload.Items {
		if item.ItemID == "" {
			t.Fatalf("missing item id")
		}
		if item.MediaType == "Audio" {
			if item.ImageURL == "" {
				t.Fatalf("expected artwork url on track")
			}
			foundTrack = true
		}
	}
	if !foundTrack {
		t.Fatalf("expected audio item")
	}
}

func TestBrowsePaginationUsesDevicePage(t *testing.T) {
	didl := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">
<item id="track-2" parentID="0" restricted="1"><dc:title>Song 2</dc:title><upnp:class>object.item.audioItem.musicTrack</upnp:class><upnp:artist>Artist</upnp:artist><res protocolInfo="http-get:*:audio/flac:DLNA.ORG_OP=01">/media/song2.flac</res></item>
</DIDL-Lite>`
	var escaped bytes.Buffer
	if err := xml.EscapeText(&escaped, []byte(didl)); err != nil {
		t.Fatalf("escape text: %v", err)
	}
	soap := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>
<u:BrowseResponse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">
<Result>` + escaped.String() + `</Result><NumberReturned>1</NumberReturned><TotalMatches>2</TotalMatches><UpdateID>1</UpdateID>
</u:BrowseResponse></s:Body></s:Envelope>`

	var reqCount int
	handler := func(r *http.Request) *http.Response {
		reqCount++
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "<StartingIndex>1</StartingIndex>") {
			t.Fatalf("expected start index 1, got body: %s", string(body))
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(soap)),
			Header:     make(http.Header),
		}
	}
	server := &mediaServer{ID: "srv-1", FriendlyName: "Library A", BaseURL: "http://cd.test", ControlURL: "http://cd.test/control", IconURL: "http://cd.test/icon.png"}
	mod := testModule(t, handler)
	mod.upnp = nil
	mod.servers["srv-1"] = server
	mod.lastDiscovery = time.Now()

	cmd := mu.CommandEnvelope{Body: mustJSON(t, mu.LibraryBrowseBody{ContainerID: makeContainerID(server.ID, "0"), Start: 1, Count: 1})}
	reply := mod.libraryBrowse(cmd, mu.ReplyEnvelope{Type: "ack", OK: true})

	if !reply.OK {
		t.Fatalf("expected ok")
	}
	if reqCount != 1 {
		t.Fatalf("expected single SOAP call")
	}
	var payload libraryItemsReply
	if err := jsonUnmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Name != "Song 2" {
		t.Fatalf("unexpected items: %+v", payload.Items)
	}
	if payload.Total != 2 {
		t.Fatalf("expected total 2, got %d", payload.Total)
	}
}

func TestResolveBuildsSources(t *testing.T) {
	didl := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">
<item id="track-1" parentID="0" restricted="1"><dc:title>Song</dc:title><upnp:class>object.item.audioItem.musicTrack</upnp:class><upnp:album>Album</upnp:album><upnp:artist>Artist</upnp:artist><res protocolInfo="http-get:*:audio/flac:DLNA.ORG_OP=01">/media/song.flac</res><upnp:albumArtURI>/art.jpg</upnp:albumArtURI></item>
</DIDL-Lite>`
	var escaped bytes.Buffer
	if err := xml.EscapeText(&escaped, []byte(didl)); err != nil {
		t.Fatalf("escape text: %v", err)
	}
	soap := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>
<u:BrowseResponse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">
<Result>` + escaped.String() + `</Result><NumberReturned>1</NumberReturned><TotalMatches>1</TotalMatches><UpdateID>1</UpdateID>
</u:BrowseResponse></s:Body></s:Envelope>`

	var reqCount int
	handler := func(r *http.Request) *http.Response {
		reqCount++
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "<BrowseFlag>BrowseMetadata</BrowseFlag>") {
			t.Fatalf("expected metadata browse, got %s", string(body))
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(soap)),
			Header:     make(http.Header),
		}
	}
	server := &mediaServer{ID: "srv-1", FriendlyName: "Library A", BaseURL: "http://cd.test", ControlURL: "http://cd.test/control"}
	mod := testModule(t, handler)
	mod.upnp = nil
	mod.servers["srv-1"] = server
	mod.lastDiscovery = time.Now()
	mod.cache = newCache(mod.config.CacheSize)
	mod.cacheCtx = context.Background()

	itemID := makeContainerID(server.ID, "track-1")
	cmd := mu.CommandEnvelope{Body: mustJSON(t, mu.LibraryResolveBody{ItemID: itemID})}
	reply := mod.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true}, server.ID)

	if !reply.OK {
		t.Fatalf("expected ok")
	}
	if reqCount == 0 {
		t.Fatalf("expected SOAP call")
	}

	var payload mu.LibraryResolveReply
	if err := jsonUnmarshal(reply.Body, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Sources) != 1 || payload.Sources[0].URL == "" {
		t.Fatalf("expected source")
	}
	if payload.Metadata["artworkUrl"] == nil {
		t.Fatalf("expected artwork url")
	}

	// cached path should not call browse again
	reqCount = 0
	reply = mod.libraryResolve(cmd, mu.ReplyEnvelope{Type: "ack", OK: true}, server.ID)
	if reqCount != 0 {
		t.Fatalf("expected cache hit without extra request")
	}
	if !reply.OK {
		t.Fatalf("expected ok from cache")
	}
}

func TestIDRoundTrip(t *testing.T) {
	serverID := "srv-1"
	objectID := "music/track/1"
	itemID := makeContainerID(serverID, objectID)
	gotServer, gotObject, ok := parseItemID(itemID)
	if !ok {
		t.Fatalf("parse failed")
	}
	if gotServer != serverID || gotObject != objectID {
		t.Fatalf("roundtrip mismatch: %s %s", gotServer, gotObject)
	}
}

// Helpers.

type roundTripFunc func(*http.Request) *http.Response

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r), nil
}

func testModule(t *testing.T, rt roundTripFunc) *Module {
	t.Helper()
	if rt == nil {
		rt = func(r *http.Request) *http.Response {
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}
		}
	}
	return &Module{
		log:            zap.NewExample(),
		http:           &http.Client{Transport: rt},
		config:         Config{CacheSize: 4 * 1024 * 1024, BrowseCacheSize: 2 * 1024 * 1024},
		cache:          newCache(cacheSizeBytes(4 * 1024 * 1024)),
		cacheCtx:       context.Background(),
		browseCache:    newCache(cacheSizeBytes(2 * 1024 * 1024)),
		browseCacheCtx: context.Background(),
		servers:        map[string]*mediaServer{},
		lastDiscovery:  time.Now(),
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := jsonMarshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
