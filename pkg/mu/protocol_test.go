package mu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateCommandEnvelopeLeaseRequired(t *testing.T) {
	cmd, err := NewCommand("playback.play", PlaybackPlayBody{})
	if err != nil {
		t.Fatalf("new command: %v", err)
	}
	cmd.ID = "id"
	cmd.TS = 1
	cmd.From = "tester"
	if err := ValidateCommandEnvelope(cmd); err == nil {
		t.Fatalf("expected lease error")
	}

	cmd.Lease = &Lease{SessionID: "s", Token: "t"}
	if err := ValidateCommandEnvelope(cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCommandEnvelopeMissingFields(t *testing.T) {
	cmd := CommandEnvelope{}
	if err := ValidateCommandEnvelope(cmd); err == nil {
		t.Fatalf("expected error")
	}
}

// ---------- TestTopicFunctions ----------

func TestTopicFunctions(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(string, string) string
		base   string
		nodeID string
		want   string
	}{
		// TopicPresence
		{
			name:   "TopicPresence with BaseTopic",
			fn:     TopicPresence,
			base:   BaseTopic,
			nodeID: "renderer-1",
			want:   "mu/v1/node/renderer-1/presence",
		},
		{
			name:   "TopicPresence with custom base",
			fn:     TopicPresence,
			base:   "custom/base",
			nodeID: "node-abc",
			want:   "custom/base/node/node-abc/presence",
		},
		{
			name:   "TopicPresence with empty nodeID",
			fn:     TopicPresence,
			base:   BaseTopic,
			nodeID: "",
			want:   "mu/v1/node//presence",
		},
		// TopicState
		{
			name:   "TopicState with BaseTopic",
			fn:     TopicState,
			base:   BaseTopic,
			nodeID: "renderer-1",
			want:   "mu/v1/node/renderer-1/state",
		},
		{
			name:   "TopicState with custom base",
			fn:     TopicState,
			base:   "home/audio",
			nodeID: "speaker",
			want:   "home/audio/node/speaker/state",
		},
		// TopicCommands
		{
			name:   "TopicCommands with BaseTopic",
			fn:     TopicCommands,
			base:   BaseTopic,
			nodeID: "renderer-1",
			want:   "mu/v1/node/renderer-1/cmd",
		},
		{
			name:   "TopicCommands with empty base",
			fn:     TopicCommands,
			base:   "",
			nodeID: "r1",
			want:   "/node/r1/cmd",
		},
		// TopicEvents
		{
			name:   "TopicEvents with BaseTopic",
			fn:     TopicEvents,
			base:   BaseTopic,
			nodeID: "renderer-2",
			want:   "mu/v1/node/renderer-2/evt",
		},
		// TopicReply
		{
			name:   "TopicReply with BaseTopic",
			fn:     TopicReply,
			base:   BaseTopic,
			nodeID: "ctrl-99",
			want:   "mu/v1/reply/ctrl-99",
		},
		{
			name:   "TopicReply with custom base",
			fn:     TopicReply,
			base:   "test",
			nodeID: "c1",
			want:   "test/reply/c1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.fn(tc.base, tc.nodeID)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------- TestCommandEnvelopeValidation ----------

func TestCommandEnvelopeValidation(t *testing.T) {
	validBody := json.RawMessage(`{"key":"val"}`)
	baseLease := &Lease{SessionID: "s1", Token: "tok1"}

	// helper that returns a fully valid command envelope
	validCmd := func() CommandEnvelope {
		return CommandEnvelope{
			ID:   "cmd-1",
			Type: "session.acquire",
			TS:   1700000000,
			From: "controller-1",
			Body: validBody,
		}
	}

	t.Run("valid command without lease for non-mutation", func(t *testing.T) {
		cmd := validCmd()
		if err := ValidateCommandEnvelope(cmd); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty ID", func(t *testing.T) {
		cmd := validCmd()
		cmd.ID = ""
		err := ValidateCommandEnvelope(cmd)
		if err == nil {
			t.Fatal("expected error for empty ID")
		}
		if !strings.Contains(err.Error(), "id") {
			t.Fatalf("expected id error, got: %v", err)
		}
	})

	t.Run("whitespace-only ID", func(t *testing.T) {
		cmd := validCmd()
		cmd.ID = "   "
		err := ValidateCommandEnvelope(cmd)
		if err == nil {
			t.Fatal("expected error for whitespace ID")
		}
	})

	t.Run("empty Type", func(t *testing.T) {
		cmd := validCmd()
		cmd.Type = ""
		err := ValidateCommandEnvelope(cmd)
		if err == nil {
			t.Fatal("expected error for empty Type")
		}
		if !strings.Contains(err.Error(), "type") {
			t.Fatalf("expected type error, got: %v", err)
		}
	})

	t.Run("zero TS", func(t *testing.T) {
		cmd := validCmd()
		cmd.TS = 0
		err := ValidateCommandEnvelope(cmd)
		if err == nil {
			t.Fatal("expected error for zero TS")
		}
		if !strings.Contains(err.Error(), "ts") {
			t.Fatalf("expected ts error, got: %v", err)
		}
	})

	t.Run("negative TS", func(t *testing.T) {
		cmd := validCmd()
		cmd.TS = -5
		err := ValidateCommandEnvelope(cmd)
		if err == nil {
			t.Fatal("expected error for negative TS")
		}
	})

	t.Run("empty From", func(t *testing.T) {
		cmd := validCmd()
		cmd.From = ""
		err := ValidateCommandEnvelope(cmd)
		if err == nil {
			t.Fatal("expected error for empty From")
		}
		if !strings.Contains(err.Error(), "from") {
			t.Fatalf("expected from error, got: %v", err)
		}
	})

	t.Run("empty Body", func(t *testing.T) {
		cmd := validCmd()
		cmd.Body = nil
		err := ValidateCommandEnvelope(cmd)
		if err == nil {
			t.Fatal("expected error for nil Body")
		}
		if !strings.Contains(err.Error(), "body") {
			t.Fatalf("expected body error, got: %v", err)
		}
	})

	t.Run("mutation command without lease", func(t *testing.T) {
		mutationCmds := []string{
			"session.renew", "session.release",
			"playback.play", "playback.pause", "playback.stop",
			"playback.seek", "playback.next", "playback.prev",
			"playback.setVolume", "playback.setMute",
			"queue.set", "queue.add", "queue.remove", "queue.move",
			"queue.clear", "queue.jump", "queue.shuffle",
			"queue.setShuffle", "queue.setRepeat",
			"queue.loadPlaylist", "queue.loadSnapshot",
			"queue.loadSuggestion",
		}
		for _, ct := range mutationCmds {
			cmd := validCmd()
			cmd.Type = ct
			err := ValidateCommandEnvelope(cmd)
			if err == nil {
				t.Errorf("expected lease error for %s", ct)
			}
			if err != nil && !strings.Contains(err.Error(), "lease") {
				t.Errorf("expected lease error for %s, got: %v", ct, err)
			}
		}
	})

	t.Run("mutation command with valid lease", func(t *testing.T) {
		cmd := validCmd()
		cmd.Type = "queue.add"
		cmd.Lease = baseLease
		if err := ValidateCommandEnvelope(cmd); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("non-mutation commands do not require lease", func(t *testing.T) {
		nonMutationCmds := []string{
			"session.acquire", "library.browse", "library.search",
			"library.getItem", "library.getItems",
			"library.resolveSources", "library.resolveSourcesBatch",
			"queue.get", "playlist.list",
		}
		for _, ct := range nonMutationCmds {
			cmd := validCmd()
			cmd.Type = ct
			if err := ValidateCommandEnvelope(cmd); err != nil {
				t.Errorf("unexpected error for %s: %v", ct, err)
			}
		}
	})

	t.Run("lease with empty sessionId", func(t *testing.T) {
		cmd := validCmd()
		cmd.Lease = &Lease{SessionID: "", Token: "tok"}
		err := ValidateCommandEnvelope(cmd)
		if err == nil {
			t.Fatal("expected error for empty lease sessionId")
		}
		if !strings.Contains(err.Error(), "sessionId") {
			t.Fatalf("expected sessionId error, got: %v", err)
		}
	})

	t.Run("lease with empty token", func(t *testing.T) {
		cmd := validCmd()
		cmd.Lease = &Lease{SessionID: "s1", Token: "  "}
		err := ValidateCommandEnvelope(cmd)
		if err == nil {
			t.Fatal("expected error for whitespace-only lease token")
		}
	})
}

// ---------- TestReplyEnvelopeErrorFields ----------

func TestReplyEnvelopeErrorFields(t *testing.T) {
	t.Run("error reply with code and message", func(t *testing.T) {
		env := ReplyEnvelope{
			ID:   "r1",
			Type: "playback.play",
			OK:   false,
			TS:   1700000000,
			Err: &ReplyError{
				Code:    "NO_SESSION",
				Message: "no active session",
			},
		}

		if env.OK {
			t.Fatal("expected OK to be false")
		}
		if env.Err == nil {
			t.Fatal("expected Err to be set")
		}
		if env.Err.Code != "NO_SESSION" {
			t.Errorf("expected code NO_SESSION, got %s", env.Err.Code)
		}
		if env.Err.Message != "no active session" {
			t.Errorf("expected message 'no active session', got %s", env.Err.Message)
		}
		if env.Body != nil {
			t.Error("expected Body to be nil on error reply")
		}
	})

	t.Run("error reply with detail", func(t *testing.T) {
		detail := json.RawMessage(`{"field":"volume","max":1.0}`)
		env := ReplyEnvelope{
			ID:   "r2",
			Type: "playback.setVolume",
			OK:   false,
			TS:   1700000001,
			Err: &ReplyError{
				Code:    "INVALID_VALUE",
				Message: "volume out of range",
				Detail:  detail,
			},
		}

		if env.Err.Detail == nil {
			t.Fatal("expected detail to be set")
		}

		var d map[string]interface{}
		if err := json.Unmarshal(env.Err.Detail, &d); err != nil {
			t.Fatalf("unmarshal detail: %v", err)
		}
		if d["field"] != "volume" {
			t.Errorf("expected field 'volume', got %v", d["field"])
		}
	})

	t.Run("error reply JSON round-trip", func(t *testing.T) {
		env := ReplyEnvelope{
			ID:   "r3",
			Type: "queue.add",
			OK:   false,
			TS:   1700000002,
			Err: &ReplyError{
				Code:    "LEASE_EXPIRED",
				Message: "session lease has expired",
			},
		}

		data, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded ReplyEnvelope
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if decoded.OK {
			t.Error("expected OK false after round-trip")
		}
		if decoded.Err == nil {
			t.Fatal("expected Err after round-trip")
		}
		if decoded.Err.Code != "LEASE_EXPIRED" {
			t.Errorf("expected code LEASE_EXPIRED, got %s", decoded.Err.Code)
		}
		if decoded.Err.Message != "session lease has expired" {
			t.Errorf("expected message 'session lease has expired', got %s", decoded.Err.Message)
		}
	})

	t.Run("success reply has nil Err", func(t *testing.T) {
		body := json.RawMessage(`{"stateVersion":5}`)
		env := ReplyEnvelope{
			ID:   "r4",
			Type: "session.acquire",
			OK:   true,
			TS:   1700000003,
			Body: body,
		}

		if !env.OK {
			t.Error("expected OK true")
		}
		if env.Err != nil {
			t.Error("expected Err nil on success reply")
		}

		data, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded ReplyEnvelope
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.Err != nil {
			t.Error("expected Err nil after round-trip")
		}
		if !decoded.OK {
			t.Error("expected OK true after round-trip")
		}
	})
}

// ---------- TestPresenceSerialization ----------

func TestPresenceSerialization(t *testing.T) {
	t.Run("full presence round-trip", func(t *testing.T) {
		p := Presence{
			NodeID: "renderer-1",
			Kind:   "renderer",
			Name:   "Living Room Speaker",
			Caps:   map[string]any{"audio": true, "maxSampleRate": 96000},
			EPs:    map[string]any{"http": "http://192.168.1.10:8080"},
			Source: "upnp",
			TS:     1700000000,
		}

		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded Presence
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if decoded.NodeID != p.NodeID {
			t.Errorf("NodeID: got %q, want %q", decoded.NodeID, p.NodeID)
		}
		if decoded.Kind != p.Kind {
			t.Errorf("Kind: got %q, want %q", decoded.Kind, p.Kind)
		}
		if decoded.Name != p.Name {
			t.Errorf("Name: got %q, want %q", decoded.Name, p.Name)
		}
		if decoded.Source != p.Source {
			t.Errorf("Source: got %q, want %q", decoded.Source, p.Source)
		}
		if decoded.TS != p.TS {
			t.Errorf("TS: got %d, want %d", decoded.TS, p.TS)
		}

		// Check caps
		if v, ok := decoded.Caps["audio"]; !ok || v != true {
			t.Errorf("Caps[audio]: got %v", decoded.Caps["audio"])
		}
		// JSON numbers decode as float64
		if v, ok := decoded.Caps["maxSampleRate"]; !ok {
			t.Error("Caps[maxSampleRate] missing")
		} else if v.(float64) != 96000 {
			t.Errorf("Caps[maxSampleRate]: got %v", v)
		}

		// Check endpoints
		if v, ok := decoded.EPs["http"]; !ok || v != "http://192.168.1.10:8080" {
			t.Errorf("EPs[http]: got %v", decoded.EPs["http"])
		}
	})

	t.Run("minimal presence with omitted fields", func(t *testing.T) {
		p := Presence{
			NodeID: "lib-1",
			Kind:   "library",
			Name:   "NAS Library",
			TS:     1700000001,
		}

		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		// Caps, EPs, and Source should be omitted
		if strings.Contains(string(data), `"caps"`) {
			t.Error("expected caps to be omitted")
		}
		if strings.Contains(string(data), `"endpoints"`) {
			t.Error("expected endpoints to be omitted")
		}
		if strings.Contains(string(data), `"source"`) {
			t.Error("expected source to be omitted")
		}

		var decoded Presence
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.Caps != nil {
			t.Errorf("expected nil Caps, got %v", decoded.Caps)
		}
		if decoded.EPs != nil {
			t.Errorf("expected nil EPs, got %v", decoded.EPs)
		}
		if decoded.Source != "" {
			t.Errorf("expected empty Source, got %q", decoded.Source)
		}
	})
}

// ---------- TestRendererStateSerialization ----------

func TestRendererStateSerialization(t *testing.T) {
	t.Run("full state round-trip", func(t *testing.T) {
		state := RendererState{
			Session: &SessionState{
				ID:            "sess-1",
				Owner:         "ctrl-1",
				LeaseExpireAt: 1700003600,
			},
			Playback: &PlaybackState{
				Status:     "playing",
				PositionMS: 45000,
				DurationMS: 240000,
				Volume:     0.75,
				Mute:       false,
			},
			Queue: &QueueState{
				Revision:   12,
				Length:     50,
				Index:      3,
				Repeat:     true,
				RepeatMode: "all",
				Shuffle:    true,
			},
			Current: &CurrentItemState{
				QueueEntryID: "qe-42",
				Ref: &LibraryItemRef{
					Kind:      LibraryItemKind,
					LibraryID: "mu:library:jellyfin:mud@home:default",
					ItemID:    "item-99",
				},
				Display: &DisplayMetadata{
					Title:  "Test Song",
					Artist: "Test Artist",
				},
			},
			StateVersion: 7,
			TS:           1700000000,
		}

		data, err := json.Marshal(state)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded RendererState
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		// Session
		if decoded.Session == nil {
			t.Fatal("expected Session to be set")
		}
		if decoded.Session.ID != "sess-1" {
			t.Errorf("Session.ID: got %q", decoded.Session.ID)
		}
		if decoded.Session.Owner != "ctrl-1" {
			t.Errorf("Session.Owner: got %q", decoded.Session.Owner)
		}
		if decoded.Session.LeaseExpireAt != 1700003600 {
			t.Errorf("Session.LeaseExpireAt: got %d", decoded.Session.LeaseExpireAt)
		}

		// Playback
		if decoded.Playback == nil {
			t.Fatal("expected Playback to be set")
		}
		if decoded.Playback.Status != "playing" {
			t.Errorf("Playback.Status: got %q", decoded.Playback.Status)
		}
		if decoded.Playback.PositionMS != 45000 {
			t.Errorf("Playback.PositionMS: got %d", decoded.Playback.PositionMS)
		}
		if decoded.Playback.DurationMS != 240000 {
			t.Errorf("Playback.DurationMS: got %d", decoded.Playback.DurationMS)
		}
		if decoded.Playback.Volume != 0.75 {
			t.Errorf("Playback.Volume: got %f", decoded.Playback.Volume)
		}
		if decoded.Playback.Mute {
			t.Error("Playback.Mute: expected false")
		}

		// Queue
		if decoded.Queue == nil {
			t.Fatal("expected Queue to be set")
		}
		if decoded.Queue.Revision != 12 {
			t.Errorf("Queue.Revision: got %d", decoded.Queue.Revision)
		}
		if decoded.Queue.Length != 50 {
			t.Errorf("Queue.Length: got %d", decoded.Queue.Length)
		}
		if decoded.Queue.Index != 3 {
			t.Errorf("Queue.Index: got %d", decoded.Queue.Index)
		}
		if !decoded.Queue.Repeat {
			t.Error("Queue.Repeat: expected true")
		}
		if decoded.Queue.RepeatMode != "all" {
			t.Errorf("Queue.RepeatMode: got %q", decoded.Queue.RepeatMode)
		}
		if !decoded.Queue.Shuffle {
			t.Error("Queue.Shuffle: expected true")
		}

		// Current
		if decoded.Current == nil {
			t.Fatal("expected Current to be set")
		}
		if decoded.Current.QueueEntryID != "qe-42" {
			t.Errorf("Current.QueueEntryID: got %q", decoded.Current.QueueEntryID)
		}
		if decoded.Current.Ref == nil || decoded.Current.Ref.ItemID != "item-99" {
			t.Errorf("Current.Ref: got %+v", decoded.Current.Ref)
		}
		if decoded.Current.Display == nil || decoded.Current.Display.Title != "Test Song" {
			t.Errorf("Current.Display: got %+v", decoded.Current.Display)
		}

		// Top-level
		if decoded.StateVersion != 7 {
			t.Errorf("StateVersion: got %d", decoded.StateVersion)
		}
		if decoded.TS != 1700000000 {
			t.Errorf("TS: got %d", decoded.TS)
		}
	})

	t.Run("minimal state with nil nested structs", func(t *testing.T) {
		state := RendererState{
			TS: 1700000001,
		}

		data, err := json.Marshal(state)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		// Omitted optional pointer fields
		if strings.Contains(string(data), `"session"`) {
			t.Error("expected session to be omitted")
		}
		if strings.Contains(string(data), `"playback"`) {
			t.Error("expected playback to be omitted")
		}
		if strings.Contains(string(data), `"queue"`) {
			t.Error("expected queue to be omitted")
		}
		if strings.Contains(string(data), `"current"`) {
			t.Error("expected current to be omitted")
		}

		var decoded RendererState
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.Session != nil {
			t.Error("expected nil Session")
		}
		if decoded.Playback != nil {
			t.Error("expected nil Playback")
		}
		if decoded.Queue != nil {
			t.Error("expected nil Queue")
		}
		if decoded.Current != nil {
			t.Error("expected nil Current")
		}
		if decoded.TS != 1700000001 {
			t.Errorf("TS: got %d", decoded.TS)
		}
	})
}

// ---------- TestQueueAddBodySerialization ----------

func TestQueueAddBodySerialization(t *testing.T) {
	libID := "mu:library:jellyfin:mud@home:default"
	mkRef := func(itemID string) *LibraryItemRef {
		r := NewLibraryItemRef(libID, itemID)
		return &r
	}

	t.Run("entries with refs only", func(t *testing.T) {
		body := QueueAddBody{
			Position: "end",
			Entries: []QueueEntry{
				{Ref: mkRef("item-1")},
				{Ref: mkRef("item-2"), Display: &DisplayMetadata{Title: "Song 2"}},
			},
		}

		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded QueueAddBody
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if decoded.Position != "end" {
			t.Errorf("Position: got %q", decoded.Position)
		}
		if len(decoded.Entries) != 2 {
			t.Fatalf("Entries length: got %d, want 2", len(decoded.Entries))
		}
		if decoded.Entries[0].Ref == nil || decoded.Entries[0].Ref.ItemID != "item-1" {
			t.Errorf("Entries[0].Ref: got %+v", decoded.Entries[0].Ref)
		}
		if decoded.Entries[0].Ref.Kind != LibraryItemKind {
			t.Errorf("Entries[0].Ref.Kind: got %q", decoded.Entries[0].Ref.Kind)
		}
		if decoded.Entries[1].Ref == nil || decoded.Entries[1].Ref.ItemID != "item-2" {
			t.Errorf("Entries[1].Ref: got %+v", decoded.Entries[1].Ref)
		}
		if decoded.Entries[1].Display == nil || decoded.Entries[1].Display.Title != "Song 2" {
			t.Errorf("Entries[1].Display: got %+v", decoded.Entries[1].Display)
		}
		if decoded.Entries[0].Resolved != nil {
			t.Error("Entries[0].Resolved: expected nil for ref-only entry")
		}
	})

	t.Run("entries with resolved sources", func(t *testing.T) {
		body := QueueAddBody{
			Position: "next",
			Entries: []QueueEntry{
				{
					Resolved: &ResolvedSource{
						URL:       "http://nas:8080/media/track10.flac",
						Mime:      "audio/flac",
						ByteRange: true,
					},
				},
			},
		}

		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded QueueAddBody
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if len(decoded.Entries) != 1 {
			t.Fatalf("Entries length: got %d, want 1", len(decoded.Entries))
		}

		entry := decoded.Entries[0]
		if entry.Ref != nil {
			t.Error("expected nil Ref for resolved-only entry")
		}
		if entry.Resolved == nil {
			t.Fatal("expected Resolved to be set")
		}
		if entry.Resolved.URL != "http://nas:8080/media/track10.flac" {
			t.Errorf("Resolved.URL: got %q", entry.Resolved.URL)
		}
		if entry.Resolved.Mime != "audio/flac" {
			t.Errorf("Resolved.Mime: got %q", entry.Resolved.Mime)
		}
		if !entry.Resolved.ByteRange {
			t.Error("Resolved.ByteRange: expected true")
		}
	})

	t.Run("with atIndex", func(t *testing.T) {
		idx := int64(5)
		body := QueueAddBody{
			Position: "at",
			AtIndex:  &idx,
			Entries: []QueueEntry{
				{Ref: mkRef("item-x")},
			},
		}

		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded QueueAddBody
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if decoded.AtIndex == nil {
			t.Fatal("expected AtIndex to be set")
		}
		if *decoded.AtIndex != 5 {
			t.Errorf("AtIndex: got %d, want 5", *decoded.AtIndex)
		}
	})

	t.Run("atIndex omitted when nil", func(t *testing.T) {
		body := QueueAddBody{
			Position: "end",
			Entries:  []QueueEntry{{Ref: mkRef("item-y")}},
		}

		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		if strings.Contains(string(data), `"atIndex"`) {
			t.Error("expected atIndex to be omitted when nil")
		}
	})

	t.Run("mixed ref and resolved entries", func(t *testing.T) {
		body := QueueAddBody{
			Position: "end",
			Entries: []QueueEntry{
				{Ref: mkRef("item-a")},
				{Resolved: &ResolvedSource{
					URL:       "http://example.com/track.mp3",
					Mime:      "audio/mpeg",
					ByteRange: false,
				}},
				{Ref: mkRef("item-b")},
			},
		}

		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var decoded QueueAddBody
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if len(decoded.Entries) != 3 {
			t.Fatalf("Entries length: got %d, want 3", len(decoded.Entries))
		}
		if decoded.Entries[0].Ref == nil {
			t.Error("Entries[0]: expected Ref")
		}
		if decoded.Entries[1].Resolved == nil {
			t.Error("Entries[1]: expected Resolved")
		}
		if decoded.Entries[2].Ref == nil {
			t.Error("Entries[2]: expected Ref")
		}
	})
}
