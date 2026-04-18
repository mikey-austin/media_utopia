package mu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseLibraryNodeID(t *testing.T) {
	t.Run("valid id", func(t *testing.T) {
		provider, namespace, resource, err := ParseLibraryNodeID("mu:library:jellyfin:mud@home:default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if provider != "jellyfin" || namespace != "mud@home" || resource != "default" {
			t.Errorf("got (%q, %q, %q)", provider, namespace, resource)
		}
	})

	t.Run("rejects non-mu prefix", func(t *testing.T) {
		_, _, _, err := ParseLibraryNodeID("lib:mu:library:jellyfin:mud@home:default")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects wrong kind", func(t *testing.T) {
		_, _, _, err := ParseLibraryNodeID("mu:renderer:gstreamer:mud@home:default")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects too few parts", func(t *testing.T) {
		_, _, _, err := ParseLibraryNodeID("mu:library:jellyfin:mud@home")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects empty parts", func(t *testing.T) {
		_, _, _, err := ParseLibraryNodeID("mu:library::mud@home:default")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestMakeLibraryNodeID(t *testing.T) {
	got := MakeLibraryNodeID("jellyfin", "mud@home", "default")
	want := "mu:library:jellyfin:mud@home:default"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLibraryItemRefValidate(t *testing.T) {
	libID := "mu:library:jellyfin:mud@home:default"

	t.Run("valid", func(t *testing.T) {
		r := NewLibraryItemRef(libID, "track-123")
		if err := r.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing kind", func(t *testing.T) {
		r := LibraryItemRef{LibraryID: libID, ItemID: "track-123"}
		err := r.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "kind") {
			t.Errorf("expected kind error, got %v", err)
		}
	})

	t.Run("wrong kind", func(t *testing.T) {
		r := LibraryItemRef{Kind: "playlistItem", LibraryID: libID, ItemID: "track-123"}
		if err := r.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid library id", func(t *testing.T) {
		r := LibraryItemRef{Kind: LibraryItemKind, LibraryID: "lib:mu:library:jellyfin:mud@home:default", ItemID: "x"}
		if err := r.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty item id", func(t *testing.T) {
		r := NewLibraryItemRef(libID, "")
		if err := r.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("whitespace item id", func(t *testing.T) {
		r := NewLibraryItemRef(libID, "   ")
		if err := r.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestLibraryItemRefRoundTrip(t *testing.T) {
	r := NewLibraryItemRef("mu:library:jellyfin:mud@home:default", "track-123")
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"kind":"libraryItem","libraryId":"mu:library:jellyfin:mud@home:default","itemId":"track-123"}`
	if string(data) != want {
		t.Errorf("marshal: got %s, want %s", data, want)
	}

	var decoded LibraryItemRef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded != r {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, r)
	}
}

func TestDisplayMetadataOmitempty(t *testing.T) {
	t.Run("empty omits all", func(t *testing.T) {
		d := DisplayMetadata{}
		data, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(data) != "{}" {
			t.Errorf("expected {}, got %s", data)
		}
	})

	t.Run("partial fields", func(t *testing.T) {
		d := DisplayMetadata{Title: "So What", Artist: "Miles Davis", DurationMS: 322000}
		data, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(data)
		if !strings.Contains(s, `"title":"So What"`) {
			t.Errorf("title missing: %s", s)
		}
		if strings.Contains(s, `"album"`) {
			t.Errorf("album should be omitted: %s", s)
		}
		if !strings.Contains(s, `"durationMs":322000`) {
			t.Errorf("durationMs missing: %s", s)
		}
	})
}

func TestQueueEntryValidate(t *testing.T) {
	libID := "mu:library:jellyfin:mud@home:default"
	mkRef := func(itemID string) *LibraryItemRef {
		r := NewLibraryItemRef(libID, itemID)
		return &r
	}

	t.Run("ref only is valid", func(t *testing.T) {
		e := QueueEntry{Ref: mkRef("x")}
		if err := e.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("resolved only is valid", func(t *testing.T) {
		e := QueueEntry{Resolved: &ResolvedSource{URL: "http://x"}}
		if err := e.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("both is valid", func(t *testing.T) {
		e := QueueEntry{Ref: mkRef("x"), Resolved: &ResolvedSource{URL: "http://x"}}
		if err := e.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("neither rejected", func(t *testing.T) {
		e := QueueEntry{}
		if err := e.Validate(); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("invalid ref rejected", func(t *testing.T) {
		bad := LibraryItemRef{Kind: LibraryItemKind, LibraryID: "bad", ItemID: "x"}
		e := QueueEntry{Ref: &bad}
		if err := e.Validate(); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("resolved without url rejected", func(t *testing.T) {
		e := QueueEntry{Resolved: &ResolvedSource{}}
		if err := e.Validate(); err == nil {
			t.Error("expected error")
		}
	})
}
