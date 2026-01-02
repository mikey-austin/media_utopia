package go2rtclibrary

import (
	"reflect"
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

func TestResolveItem(t *testing.T) {
	mod := newTestModule()
	mod.streams = map[string]streamInfo{
		"stream_f": {ID: "stream_f", Name: "FrontDoor"},
	}
	mod.lastRefresh = time.Now()

	meta, sources, err := mod.resolveItem("live:stream_f", false)
	if err != nil {
		t.Fatalf("resolve live error: %v", err)
	}
	if meta["durationMs"].(int64) != 0 {
		t.Fatalf("expected duration 0, got %v", meta["durationMs"])
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].URL != "http://go2rtc.local/api/stream.mp4?src=FrontDoor" {
		t.Fatalf("unexpected live url: %s", sources[0].URL)
	}

	meta, sources, err = mod.resolveItem("clip-stream_f-30", false)
	if err != nil {
		t.Fatalf("resolve clip error: %v", err)
	}
	if meta["durationMs"].(int64) != 30000 {
		t.Fatalf("expected duration 30000, got %v", meta["durationMs"])
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].URL != "http://go2rtc.local/api/stream.mp4?duration=30&src=FrontDoor" {
		t.Fatalf("unexpected clip url: %s", sources[0].URL)
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

func newTestModule() *Module {
	mod, _ := NewModule(nil, nil, Config{
		NodeID:    "mu:library:go2rtc:test:default",
		BaseURL:   "http://go2rtc.local",
		Durations: []time.Duration{30 * time.Second, 60 * time.Second},
	})
	return mod
}
