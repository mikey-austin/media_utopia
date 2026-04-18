package go2rtclibrary

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBrowseRootSorted(t *testing.T) {
	mod := newTestModule()
	mod.streams = map[string]streamInfo{
		"stream_b": {ID: "stream_b", Name: "Back Door"},
		"stream_f": {ID: "stream_f", Name: "Front Door"},
	}
	mod.lastRefresh = time.Now()

	items, total, err := mod.browseItems("", 0, 0)
	if err != nil {
		t.Fatalf("browse root error: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Name != "Back Door" || items[1].Name != "Front Door" {
		t.Fatalf("unexpected sort order: %v, %v", items[0].Name, items[1].Name)
	}
	if items[0].Type != "Camera" || items[1].Type != "Camera" {
		t.Fatalf("expected camera types, got %s/%s", items[0].Type, items[1].Type)
	}
}

func TestBrowseContainerEntries(t *testing.T) {
	mod := newTestModule()
	mod.streams = map[string]streamInfo{
		"stream_f": {ID: "stream_f", Name: "Front Door"},
	}
	mod.lastRefresh = time.Now()

	items, total, err := mod.browseItems("stream_f", 0, 0)
	if err != nil {
		t.Fatalf("browse container error: %v", err)
	}
	if total != int64(len(items)) {
		t.Fatalf("expected total %d, got %d", len(items), total)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	seen := map[string]libraryItem{}
	for _, item := range items {
		seen[item.ItemID] = item
		if item.ContainerID != "stream_f" {
			t.Fatalf("expected container id stream_f, got %s", item.ContainerID)
		}
	}

	live := seen["live-stream_f"]
	if live.ItemID == "" {
		t.Fatal("expected live item")
	}
	if live.DurationMS != 0 {
		t.Fatalf("expected live duration 0, got %d", live.DurationMS)
	}

	clip := seen["clip-stream_f-30"]
	if clip.ItemID == "" {
		t.Fatal("expected clip item")
	}
	if clip.DurationMS != 30000 {
		t.Fatalf("expected clip duration 30000, got %d", clip.DurationMS)
	}
}

func TestDescribeAndResolveItem(t *testing.T) {
	mod := newTestModule()
	mod.streams = map[string]streamInfo{
		"stream_f": {ID: "stream_f", Name: "FrontDoor"},
	}
	mod.lastRefresh = time.Now()

	display, attrs, ok := mod.describeItem("live-stream_f")
	if !ok {
		t.Fatal("describe live not ok")
	}
	if display.DurationMS != 0 {
		t.Fatalf("expected display duration 0, got %d", display.DurationMS)
	}
	if display.MediaType != "Video" {
		t.Fatalf("expected mediaType Video, got %q", display.MediaType)
	}
	if display.Title != "FrontDoor (Live)" {
		t.Fatalf("expected title 'FrontDoor (Live)', got %q", display.Title)
	}
	if c, _ := attrs["container"].(bool); c {
		t.Fatalf("expected container false for live entry")
	}

	source, err := mod.resolveItemSource("live:stream_f")
	if err != nil {
		t.Fatalf("resolve live error: %v", err)
	}
	if source.URL != "http://go2rtc.local/api/stream.mp4?src=FrontDoor" {
		t.Fatalf("unexpected live url: %s", source.URL)
	}

	display, _, ok = mod.describeItem("clip-stream_f-30")
	if !ok {
		t.Fatal("describe clip not ok")
	}
	if display.DurationMS != 30000 {
		t.Fatalf("expected display duration 30000, got %d", display.DurationMS)
	}

	source, err = mod.resolveItemSource("clip-stream_f-30")
	if err != nil {
		t.Fatalf("resolve clip error: %v", err)
	}
	if source.URL != "http://go2rtc.local/api/stream.mp4?duration=30&src=FrontDoor" {
		t.Fatalf("unexpected clip url: %s", source.URL)
	}

	// Container itself describes as a Camera container.
	display, attrs, ok = mod.describeItem("stream_f")
	if !ok {
		t.Fatal("describe container not ok")
	}
	if display.Title != "FrontDoor" {
		t.Fatalf("expected container title 'FrontDoor', got %q", display.Title)
	}
	if c, _ := attrs["container"].(bool); !c {
		t.Fatalf("expected container true for stream entry")
	}
}

func TestParseStreamNames(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		expect []string
	}{
		{
			name:   "streams-object",
			body:   `{"streams":{"front":{},"back":{}}}`,
			expect: []string{"back", "front"},
		},
		{
			name:   "root-object",
			body:   `{"front":{},"back":{}}`,
			expect: []string{"back", "front"},
		},
		{
			name:   "array",
			body:   `[{"name":"front"},{"name":"back"}]`,
			expect: []string{"front", "back"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseStreamNames([]byte(tc.body))
			if !reflect.DeepEqual(tc.expect, got) {
				t.Fatalf("expected %v, got %v", tc.expect, got)
			}
		})
	}
}

func TestResolveNonexistentStream(t *testing.T) {
	mod := newTestModule()
	mod.streams = map[string]streamInfo{
		"stream_f": {ID: "stream_f", Name: "Front Door"},
	}
	mod.lastRefresh = time.Now()

	_, err := mod.resolveItemSource("live-nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent stream, got nil")
	}
	if err.Error() != "item not found" {
		t.Fatalf("expected 'item not found' error, got %q", err.Error())
	}

	// A bogus item ID that doesn't even parse as a valid item also fails.
	_, err = mod.resolveItemSource("totally-bogus-id")
	if err == nil {
		t.Fatal("expected error for bogus item id, got nil")
	}

	if _, _, ok := mod.describeItem("totally-bogus-id"); ok {
		t.Fatal("expected describeItem to report not ok for bogus id")
	}
}

func TestBrowseRootPagination(t *testing.T) {
	mod := newTestModule()
	mod.streams = map[string]streamInfo{
		"stream_a": {ID: "stream_a", Name: "Alpha"},
		"stream_b": {ID: "stream_b", Name: "Bravo"},
		"stream_c": {ID: "stream_c", Name: "Charlie"},
		"stream_d": {ID: "stream_d", Name: "Delta"},
	}
	mod.lastRefresh = time.Now()

	// Fetch first 2 items.
	items, total, err := mod.browseItems("", 0, 2)
	if err != nil {
		t.Fatalf("browse root page 1 error: %v", err)
	}
	if total != 4 {
		t.Fatalf("expected total 4, got %d", total)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items on page 1, got %d", len(items))
	}
	if items[0].Name != "Alpha" || items[1].Name != "Bravo" {
		t.Fatalf("unexpected page 1 items: %v, %v", items[0].Name, items[1].Name)
	}

	// Fetch next 2 items.
	items, total, err = mod.browseItems("", 2, 2)
	if err != nil {
		t.Fatalf("browse root page 2 error: %v", err)
	}
	if total != 4 {
		t.Fatalf("expected total 4, got %d", total)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items on page 2, got %d", len(items))
	}
	if items[0].Name != "Charlie" || items[1].Name != "Delta" {
		t.Fatalf("unexpected page 2 items: %v, %v", items[0].Name, items[1].Name)
	}

	// Fetch past the end.
	items, total, err = mod.browseItems("", 4, 2)
	if err != nil {
		t.Fatalf("browse root past end error: %v", err)
	}
	if total != 4 {
		t.Fatalf("expected total 4, got %d", total)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items past end, got %d", len(items))
	}

	// Partial last page: start=3, count=5 should return 1 item.
	items, total, err = mod.browseItems("", 3, 5)
	if err != nil {
		t.Fatalf("browse root partial page error: %v", err)
	}
	if total != 4 {
		t.Fatalf("expected total 4, got %d", total)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item on partial page, got %d", len(items))
	}
	if items[0].Name != "Delta" {
		t.Fatalf("expected Delta on partial page, got %s", items[0].Name)
	}
}

func TestSearchStreams(t *testing.T) {
	mod := newTestModule()
	mod.streams = map[string]streamInfo{
		"stream_f": {ID: "stream_f", Name: "Front Door"},
		"stream_b": {ID: "stream_b", Name: "Back Door"},
		"stream_g": {ID: "stream_g", Name: "Garage"},
	}
	mod.lastRefresh = time.Now()

	// Search for "door" should match Front Door and Back Door (case insensitive).
	items, total, err := mod.searchItems("Door", 0, 0)
	if err != nil {
		t.Fatalf("search error: %v", err)
	}
	// Should match the 2 stream-level items plus their sub-entries (live + 2 clips each).
	// Each stream has 3 entries (live + 30s clip + 60s clip) = 6 sub-entries
	// Plus 2 stream-level items = 8 total with "door" in the name.
	if total < 2 {
		t.Fatalf("expected at least 2 matches for 'Door', got %d", total)
	}
	// Verify all returned items contain "door" (case insensitive).
	for _, item := range items {
		if !containsCI(item.Name, "door") {
			t.Fatalf("search result %q does not contain 'door'", item.Name)
		}
	}

	// Search for "garage" should match only Garage and its entries.
	items, total, err = mod.searchItems("garage", 0, 0)
	if err != nil {
		t.Fatalf("search garage error: %v", err)
	}
	if total < 1 {
		t.Fatalf("expected at least 1 match for 'garage', got %d", total)
	}
	for _, item := range items {
		if !containsCI(item.Name, "garage") {
			t.Fatalf("search result %q does not contain 'garage'", item.Name)
		}
	}

	// Empty query should return nothing.
	items, total, err = mod.searchItems("", 0, 0)
	if err != nil {
		t.Fatalf("search empty error: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 results for empty query, got %d", total)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items for empty query, got %d", len(items))
	}

	// Search for something that doesn't match.
	items, total, err = mod.searchItems("nonexistent", 0, 0)
	if err != nil {
		t.Fatalf("search nonexistent error: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 results for 'nonexistent', got %d", total)
	}
}

func TestNewModuleValidation(t *testing.T) {
	// Missing node_id.
	_, err := NewModule(nil, nil, Config{
		BaseURL: "http://go2rtc.local",
	})
	if err == nil {
		t.Fatal("expected error for empty node_id, got nil")
	}
	if err.Error() != "node_id required" {
		t.Fatalf("expected 'node_id required', got %q", err.Error())
	}

	// Missing base_url.
	_, err = NewModule(nil, nil, Config{
		NodeID: "mu:library:go2rtc:test:default",
	})
	if err == nil {
		t.Fatal("expected error for empty base_url, got nil")
	}
	if err.Error() != "base_url required" {
		t.Fatalf("expected 'base_url required', got %q", err.Error())
	}

	// Whitespace-only node_id should also fail.
	_, err = NewModule(nil, nil, Config{
		NodeID:  "   ",
		BaseURL: "http://go2rtc.local",
	})
	if err == nil {
		t.Fatal("expected error for whitespace node_id, got nil")
	}

	// Whitespace-only base_url should also fail.
	_, err = NewModule(nil, nil, Config{
		NodeID:  "mu:library:go2rtc:test:default",
		BaseURL: "   ",
	})
	if err == nil {
		t.Fatal("expected error for whitespace base_url, got nil")
	}

	// Valid config should succeed and apply defaults.
	mod, err := NewModule(nil, nil, Config{
		NodeID:  "mu:library:go2rtc:test:default",
		BaseURL: "http://go2rtc.local/",
	})
	if err != nil {
		t.Fatalf("expected no error for valid config, got %v", err)
	}
	if mod.config.Name != "go2rtc Library" {
		t.Fatalf("expected default name 'go2rtc Library', got %q", mod.config.Name)
	}
	// BaseURL should have trailing slash trimmed.
	if mod.config.BaseURL != "http://go2rtc.local" {
		t.Fatalf("expected trimmed base_url, got %q", mod.config.BaseURL)
	}
	if len(mod.config.Durations) != 1 || mod.config.Durations[0] != 30*time.Second {
		t.Fatalf("expected default duration [30s], got %v", mod.config.Durations)
	}
}

func containsCI(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func newTestModule() *Module {
	mod, _ := NewModule(nil, nil, Config{
		NodeID:    "mu:library:go2rtc:test:default",
		BaseURL:   "http://go2rtc.local",
		Durations: []time.Duration{30 * time.Second, 60 * time.Second},
	})
	return mod
}
