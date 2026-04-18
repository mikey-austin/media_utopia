package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

type stubClock struct{}

func (stubClock) NowUnix() int64 { return 100 }

type stubIDGen struct{}

func (stubIDGen) NewID() string { return "id-1" }

type memoryLeaseStore struct {
	store map[string]mu.Lease
}

func (m *memoryLeaseStore) Get(rendererID string) (mu.Lease, bool, error) {
	lease, ok := m.store[rendererID]
	return lease, ok, nil
}

func (m *memoryLeaseStore) Put(rendererID string, lease mu.Lease) error {
	m.store[rendererID] = lease
	return nil
}

func (m *memoryLeaseStore) Clear(rendererID string) error {
	delete(m.store, rendererID)
	return nil
}

type stubBroker struct {
	presence   []mu.Presence
	replies    map[string]mu.ReplyEnvelope
	lastNode   string
	lastCmd    mu.CommandEnvelope
	replyTopic string
	state      mu.RendererState
	calls      []mu.CommandEnvelope
}

func (s *stubBroker) ReplyTopic() string { return s.replyTopic }

func (s *stubBroker) PublishCommand(ctx context.Context, nodeID string, cmd mu.CommandEnvelope) (mu.ReplyEnvelope, error) {
	s.lastNode = nodeID
	s.lastCmd = cmd
	s.calls = append(s.calls, cmd)
	if reply, ok := s.replies[cmd.Type]; ok {
		return reply, nil
	}
	return mu.ReplyEnvelope{ID: cmd.ID, Type: "ack", OK: true, TS: 101}, nil
}

func (s *stubBroker) ListPresence(ctx context.Context) ([]mu.Presence, error) {
	return s.presence, nil
}

func (s *stubBroker) GetRendererState(ctx context.Context, nodeID string) (mu.RendererState, error) {
	return s.state, nil
}

func (s *stubBroker) WatchRenderer(ctx context.Context, nodeID string) (<-chan mu.RendererState, <-chan mu.Event, <-chan error) {
	stateCh := make(chan mu.RendererState)
	eventCh := make(chan mu.Event)
	errCh := make(chan error)
	close(stateCh)
	close(eventCh)
	close(errCh)
	return stateCh, eventCh, errCh
}

func TestSuggestListUsesServerSelector(t *testing.T) {
	playlistServer := mu.Presence{NodeID: "mu:playlist:plsrv:default:main", Kind: "playlist", Name: "Main"}
	broker := &stubBroker{
		presence:   []mu.Presence{playlistServer},
		replyTopic: "mu/v1/reply/test",
	}

	replyBody, err := json.Marshal(mu.SuggestListReply{Suggestions: []mu.SuggestSummary{{SuggestionID: "mu:suggest:plsrv:default:s-01", Name: "Late Night"}}})
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	broker.replies = map[string]mu.ReplyEnvelope{
		"suggest.list": {ID: "id-1", Type: "ack", OK: true, TS: 101, Body: replyBody},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{"server": playlistServer.NodeID}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
		Config:     Config{Identity: "tester"},
	}

	_, err = service.SuggestList(context.Background(), "server")
	if err != nil {
		t.Fatalf("SuggestList: %v", err)
	}
	if broker.lastNode != playlistServer.NodeID {
		t.Fatalf("expected server node %s", playlistServer.NodeID)
	}
	if broker.lastCmd.Type != "suggest.list" {
		t.Fatalf("expected suggest.list command")
	}
}

func TestSnapshotListUsesServerSelector(t *testing.T) {
	playlistServer := mu.Presence{NodeID: "mu:playlist:plsrv:default:main", Kind: "playlist", Name: "Main"}
	broker := &stubBroker{
		presence:   []mu.Presence{playlistServer},
		replyTopic: "mu/v1/reply/test",
	}

	replyBody, err := json.Marshal(mu.SnapshotListReply{Snapshots: []mu.SnapshotSummary{{SnapshotID: "mu:snapshot:plsrv:default:snap-1", Name: "Friday"}}})
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	broker.replies = map[string]mu.ReplyEnvelope{
		"snapshot.list": {ID: "id-1", Type: "ack", OK: true, TS: 101, Body: replyBody},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{"server": playlistServer.NodeID}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
		Config:     Config{Identity: "tester"},
	}

	_, err = service.SnapshotList(context.Background(), "server")
	if err != nil {
		t.Fatalf("SnapshotList: %v", err)
	}
	if broker.lastCmd.Type != "snapshot.list" {
		t.Fatalf("expected snapshot.list command")
	}
}

func TestLeaseAutoRefreshesOnCommand(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test", Kind: "renderer", Name: "Test Renderer"}
	broker := &stubBroker{
		presence:   []mu.Presence{renderer},
		replyTopic: "mu/v1/reply/test",
		replies: map[string]mu.ReplyEnvelope{
			"session.renew": {ID: "id-1", Type: "ack", OK: true, TS: 101},
			"queue.clear":   {ID: "id-1", Type: "ack", OK: true, TS: 101},
		},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{renderer.NodeID: {SessionID: "s1", Token: "t1"}}},
		Config:     Config{Identity: "tester"},
	}

	if err := service.QueueClear(context.Background(), renderer.NodeID); err != nil {
		t.Fatalf("QueueClear: %v", err)
	}
	if len(broker.calls) < 2 {
		t.Fatalf("expected refresh + command")
	}
	if broker.calls[0].Type != "session.renew" {
		t.Fatalf("expected first call session.renew, got %s", broker.calls[0].Type)
	}
	if broker.calls[1].Type != "queue.clear" {
		t.Fatalf("expected second call queue.clear, got %s", broker.calls[1].Type)
	}
}

func TestPlaylistAddExpandsLibraryContainer(t *testing.T) {
	playlistServer := mu.Presence{NodeID: "mu:playlist:plsrv:default:main", Kind: "playlist", Name: "Main"}
	library := mu.Presence{NodeID: "mu:library:jellyfin:test", Kind: "library", Name: "Jellyfin"}
	broker := &stubBroker{
		presence:   []mu.Presence{playlistServer, library},
		replyTopic: "mu/v1/reply/test",
	}

	browseBody, err := json.Marshal(libraryItemsReply{Items: []libraryItem{{ItemID: "track-1"}, {ItemID: "track-2"}}})
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	getItemBody, err := json.Marshal(mu.LibraryGetItemReply{
		Ref:        mu.NewLibraryItemRef(library.NodeID, "album-1"),
		Display:    &mu.DisplayMetadata{Title: "Album"},
		Attributes: map[string]any{"container": true, "type": "MusicAlbum"},
	})
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	sourcesBody, err := json.Marshal(mu.LibraryResolveSourcesReply{
		Sources: []mu.ResolvedSource{{URL: "http://a", Mime: "audio/mp3", ByteRange: true}},
	})
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	listBody, err := json.Marshal(mu.PlaylistListReply{Playlists: []mu.PlaylistSummary{{PlaylistID: "pl-1", Name: "pl-1"}}})
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	broker.replies = map[string]mu.ReplyEnvelope{
		"playlist.list":          {ID: "id-0", Type: "ack", OK: true, TS: 101, Body: listBody},
		"library.browse":         {ID: "id-0", Type: "ack", OK: true, TS: 101, Body: browseBody},
		"library.getItem":        {ID: "id-1", Type: "ack", OK: true, TS: 101, Body: getItemBody},
		"library.resolveSources": {ID: "id-1", Type: "ack", OK: true, TS: 101, Body: sourcesBody},
		"playlist.addItems":      {ID: "id-2", Type: "ack", OK: true, TS: 101},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{"lib": library.NodeID}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
		Config:     Config{Identity: "tester"},
	}

	if err := service.PlaylistAdd(context.Background(), "pl-1", []string{"lib:lib:album-1"}, "yes", ""); err != nil {
		t.Fatalf("PlaylistAdd: %v", err)
	}

	if broker.lastCmd.Type != "playlist.addItems" {
		t.Fatalf("expected playlist.addItems, got %s", broker.lastCmd.Type)
	}
	var body mu.PlaylistAddItemsBody
	if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
		t.Fatalf("decode add body: %v", err)
	}
	if len(body.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(body.Entries))
	}
	if body.Entries[0].Ref == nil || body.Entries[1].Ref == nil {
		t.Fatalf("expected playlist entries to have refs")
	}
	if body.Entries[0].Ref.LibraryID != library.NodeID || body.Entries[0].Ref.ItemID != "track-1" {
		t.Fatalf("unexpected first ref: %+v", body.Entries[0].Ref)
	}
	if body.Entries[1].Ref.LibraryID != library.NodeID || body.Entries[1].Ref.ItemID != "track-2" {
		t.Fatalf("unexpected second ref: %+v", body.Entries[1].Ref)
	}
}

func TestBackfillQueueDisplayUsesGetItems(t *testing.T) {
	library := mu.Presence{NodeID: "mu:library:jellyfin:test", Kind: "library", Name: "Jellyfin"}
	broker := &stubBroker{
		presence:   []mu.Presence{library},
		replyTopic: "mu/v1/reply/test",
	}

	batchBody, err := json.Marshal(mu.LibraryGetItemsReply{
		Items: []mu.LibraryGetItemsReplyItem{
			{Ref: mu.NewLibraryItemRef(library.NodeID, "track-1"), Display: &mu.DisplayMetadata{Title: "A"}},
			{Ref: mu.NewLibraryItemRef(library.NodeID, "track-2"), Display: &mu.DisplayMetadata{Title: "B"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}

	broker.replies = map[string]mu.ReplyEnvelope{
		"library.getItems": {ID: "id-1", Type: "ack", OK: true, TS: 101, Body: batchBody},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{"lib": library.NodeID}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
		Config:     Config{Identity: "tester"},
	}

	ref1 := mu.NewLibraryItemRef(library.NodeID, "track-1")
	ref2 := mu.NewLibraryItemRef(library.NodeID, "track-2")
	entries := []mu.QueueEntry{
		{Ref: &ref1},
		{Ref: &ref2},
	}
	resolved := service.backfillQueueDisplay(context.Background(), entries)
	if resolved[0].Display == nil || resolved[0].Display.Title != "A" {
		t.Fatalf("expected resolved title A on entry 0")
	}
	if resolved[1].Display == nil || resolved[1].Display.Title != "B" {
		t.Fatalf("expected resolved title B on entry 1")
	}
	if len(broker.calls) != 1 || broker.calls[0].Type != "library.getItems" {
		t.Fatalf("expected single library.getItems call")
	}
}

func TestPlaylistGetResolvesName(t *testing.T) {
	server := mu.Presence{NodeID: "mu:playlist:plsrv:default:main", Kind: "playlist", Name: "Main"}
	broker := &stubBroker{
		presence:   []mu.Presence{server},
		replyTopic: "mu/v1/reply/test",
	}

	listBody, err := json.Marshal(mu.PlaylistListReply{Playlists: []mu.PlaylistSummary{{PlaylistID: "pl-1", Name: "Evening"}}})
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	getBody, err := json.Marshal(map[string]any{"playlistId": "pl-1"})
	if err != nil {
		t.Fatalf("marshal get: %v", err)
	}
	broker.replies = map[string]mu.ReplyEnvelope{
		"playlist.list": {ID: "id-1", Type: "ack", OK: true, TS: 101, Body: listBody},
		"playlist.get":  {ID: "id-2", Type: "ack", OK: true, TS: 101, Body: getBody},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
		Config:     Config{Identity: "tester"},
	}

	if _, err := service.PlaylistGet(context.Background(), "Evening", ""); err != nil {
		t.Fatalf("PlaylistGet: %v", err)
	}
	if broker.lastCmd.Type != "playlist.get" {
		t.Fatalf("expected playlist.get, got %s", broker.lastCmd.Type)
	}
	var body mu.PlaylistGetBody
	if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.PlaylistID != "pl-1" {
		t.Fatalf("expected playlist id pl-1")
	}
}

func TestSuggestLoadUsesLeaseAndServer(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	playlistServer := mu.Presence{NodeID: "mu:playlist:plsrv:default:main", Kind: "playlist", Name: "Main"}
	broker := &stubBroker{
		presence:   []mu.Presence{renderer, playlistServer},
		replyTopic: "mu/v1/reply/test",
	}

	leaseStore := &memoryLeaseStore{store: map[string]mu.Lease{renderer.NodeID: {SessionID: "s1", Token: "t1"}}}
	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{"server": playlistServer.NodeID}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: leaseStore,
		Config:     Config{Identity: "tester"},
	}

	err := service.SuggestLoad(context.Background(), renderer.NodeID, "mu:suggest:plsrv:default:s-01", "replace", "auto", "server")
	if err != nil {
		t.Fatalf("SuggestLoad: %v", err)
	}
	if broker.lastCmd.Type != "queue.loadSuggestion" {
		t.Fatalf("expected queue.loadSuggestion")
	}
	if broker.lastCmd.Lease == nil {
		t.Fatalf("expected lease in command")
	}
	var body mu.QueueLoadSuggestionBody
	if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.PlaylistServerID != playlistServer.NodeID {
		t.Fatalf("expected playlist server id")
	}
	if body.SuggestionID != "mu:suggest:plsrv:default:s-01" {
		t.Fatalf("expected suggestion id")
	}
}

func TestResolveSeekPositionRelative(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	broker := &stubBroker{
		presence: []mu.Presence{renderer},
		state:    mu.RendererState{Playback: &mu.PlaybackState{PositionMS: 10000}},
	}
	service := Service{
		Broker: broker,
		Resolver: Resolver{
			Presence: broker,
			Config:   Config{Defaults: Defaults{Renderer: renderer.NodeID}},
		},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
		Config:     Config{Identity: "tester"},
	}

	pos, err := service.resolveSeekPosition(context.Background(), "", "+5s")
	if err != nil {
		t.Fatalf("resolveSeekPosition: %v", err)
	}
	if pos != 15000 {
		t.Fatalf("expected 15000ms, got %d", pos)
	}
}

func TestResolveVolumeDeltaClamp(t *testing.T) {
	broker := &stubBroker{
		state: mu.RendererState{Playback: &mu.PlaybackState{Volume: 0.9}},
	}
	service := Service{
		Broker: broker,
		Resolver: Resolver{
			Presence: broker,
			Config:   Config{},
		},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
		Config:     Config{Identity: "tester"},
	}

	vol, err := service.resolveVolume(context.Background(), "renderer", "+20")
	if err != nil {
		t.Fatalf("resolveVolume: %v", err)
	}
	if vol != 1 {
		t.Fatalf("expected clamp to 1.0, got %f", vol)
	}
}

func TestParseQueueFileMuq(t *testing.T) {
	service := Service{}
	data := []byte("\n# comment\nhttp://example/stream\nhttps://other/stream2\n")
	entries, err := service.parseQueueFile("muq", data)
	if err != nil {
		t.Fatalf("parseQueueFile: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Resolved == nil || entries[0].Resolved.URL != "http://example/stream" {
		t.Fatalf("expected first resolved URL entry")
	}
	if entries[1].Resolved == nil || entries[1].Resolved.URL != "https://other/stream2" {
		t.Fatalf("expected second resolved URL entry")
	}
}

func TestQueueLoadPlaylistUsesServerOverride(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	server := mu.Presence{NodeID: "mu:playlist:plsrv:default:alt", Kind: "playlist", Name: "Alt"}
	broker := &stubBroker{
		presence:   []mu.Presence{renderer, server},
		replyTopic: "mu/v1/reply/test",
	}
	leaseStore := &memoryLeaseStore{store: map[string]mu.Lease{renderer.NodeID: {SessionID: "s1", Token: "t1"}}}
	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{"server": server.NodeID}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: leaseStore,
		Config:     Config{Identity: "tester"},
	}

	err := service.QueueLoadPlaylist(context.Background(), renderer.NodeID, "mu:playlist:plsrv:default:pl-1", "replace", "auto", "server")
	if err != nil {
		t.Fatalf("QueueLoadPlaylist: %v", err)
	}
	var body mu.QueueLoadPlaylistBody
	if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.PlaylistServerID != server.NodeID {
		t.Fatalf("expected playlist server override")
	}
}

func TestQueueLoadSnapshotUsesServerOverride(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	server := mu.Presence{NodeID: "mu:playlist:plsrv:default:alt", Kind: "playlist", Name: "Alt"}
	broker := &stubBroker{
		presence:   []mu.Presence{renderer, server},
		replyTopic: "mu/v1/reply/test",
	}
	leaseStore := &memoryLeaseStore{store: map[string]mu.Lease{renderer.NodeID: {SessionID: "s1", Token: "t1"}}}
	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{"server": server.NodeID}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: leaseStore,
		Config:     Config{Identity: "tester"},
	}

	err := service.QueueLoadSnapshot(context.Background(), renderer.NodeID, "mu:snapshot:plsrv:default:snap-1", "replace", "auto", "server")
	if err != nil {
		t.Fatalf("QueueLoadSnapshot: %v", err)
	}
	var body mu.QueueLoadSnapshotBody
	if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.PlaylistServerID != server.NodeID {
		t.Fatalf("expected playlist server override")
	}
}

func TestPlaylistRenameCommand(t *testing.T) {
	server := mu.Presence{NodeID: "mu:playlist:plsrv:default:main", Kind: "playlist", Name: "Main"}
	broker := &stubBroker{
		presence:   []mu.Presence{server},
		replyTopic: "mu/v1/reply/test",
	}
	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{"server": server.NodeID}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
		Config:     Config{Identity: "tester"},
	}

	err := service.PlaylistRename(context.Background(), "mu:playlist:plsrv:default:pl-1", "Evening", "server")
	if err != nil {
		t.Fatalf("PlaylistRename: %v", err)
	}
	if broker.lastCmd.Type != "playlist.rename" {
		t.Fatalf("expected playlist.rename command")
	}
	var body mu.PlaylistRenameBody
	if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Name != "Evening" {
		t.Fatalf("expected rename name")
	}
}

func TestListNodes(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room", TS: 1}
	library := mu.Presence{NodeID: "mu:library:jellyfin:test", Kind: "library", Name: "Jellyfin", TS: 2}
	broker := &stubBroker{
		presence: []mu.Presence{renderer, library},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
		Config:     Config{Identity: "tester"},
	}

	t.Run("all nodes", func(t *testing.T) {
		result, err := service.ListNodes(context.Background(), "", false)
		if err != nil {
			t.Fatalf("ListNodes: %v", err)
		}
		if len(result.Nodes) != 2 {
			t.Fatalf("expected 2 nodes, got %d", len(result.Nodes))
		}
	})

	t.Run("filter by kind", func(t *testing.T) {
		result, err := service.ListNodes(context.Background(), "renderer", false)
		if err != nil {
			t.Fatalf("ListNodes: %v", err)
		}
		if len(result.Nodes) != 1 {
			t.Fatalf("expected 1 renderer node, got %d", len(result.Nodes))
		}
		if result.Nodes[0].NodeID != renderer.NodeID {
			t.Fatalf("expected renderer node id")
		}
	})

	t.Run("online only filters by TS", func(t *testing.T) {
		offlineRenderer := mu.Presence{NodeID: "mu:renderer:offline", Kind: "renderer", Name: "Offline", TS: 0}
		brokerWithOffline := &stubBroker{
			presence: []mu.Presence{renderer, offlineRenderer},
		}
		svc := Service{
			Broker:     brokerWithOffline,
			Resolver:   Resolver{Presence: brokerWithOffline, Config: Config{}},
			Clock:      stubClock{},
			IDGen:      stubIDGen{},
			LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
			Config:     Config{Identity: "tester"},
		}
		result, err := svc.ListNodes(context.Background(), "", true)
		if err != nil {
			t.Fatalf("ListNodes: %v", err)
		}
		if len(result.Nodes) != 1 {
			t.Fatalf("expected 1 online node, got %d", len(result.Nodes))
		}
		if result.Nodes[0].NodeID != renderer.NodeID {
			t.Fatalf("expected online renderer")
		}
	})
}

func TestStatus(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	broker := &stubBroker{
		presence: []mu.Presence{renderer},
		state: mu.RendererState{
			Playback: &mu.PlaybackState{Status: "playing", Volume: 0.75, PositionMS: 5000, DurationMS: 200000},
		},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Defaults: Defaults{Renderer: renderer.NodeID}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
		Config:     Config{Identity: "tester"},
	}

	result, err := service.Status(context.Background(), "")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if result.Renderer.NodeID != renderer.NodeID {
		t.Fatalf("expected renderer node id %s, got %s", renderer.NodeID, result.Renderer.NodeID)
	}
	if result.State.Playback == nil {
		t.Fatalf("expected playback state")
	}
	if result.State.Playback.Status != "playing" {
		t.Fatalf("expected status playing, got %s", result.State.Playback.Status)
	}
	if result.State.Playback.Volume != 0.75 {
		t.Fatalf("expected volume 0.75, got %f", result.State.Playback.Volume)
	}
}

func TestPlaybackPlay(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	broker := &stubBroker{
		presence:   []mu.Presence{renderer},
		replyTopic: "mu/v1/reply/test",
		replies: map[string]mu.ReplyEnvelope{
			"session.renew": {ID: "id-1", Type: "ack", OK: true, TS: 101},
			"playback.play": {ID: "id-1", Type: "ack", OK: true, TS: 101},
		},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{renderer.NodeID: {SessionID: "s1", Token: "t1"}}},
		Config:     Config{Identity: "tester"},
	}

	err := service.PlaybackPlay(context.Background(), renderer.NodeID, nil)
	if err != nil {
		t.Fatalf("PlaybackPlay: %v", err)
	}
	if broker.lastCmd.Type != "playback.play" {
		t.Fatalf("expected playback.play command, got %s", broker.lastCmd.Type)
	}
	if broker.lastCmd.Lease == nil {
		t.Fatalf("expected lease in command")
	}
	if broker.lastCmd.Lease.SessionID != "s1" {
		t.Fatalf("expected session id s1")
	}

	// Verify body has nil index when no index specified
	var body mu.PlaybackPlayBody
	if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Index != nil {
		t.Fatalf("expected nil index for simple play")
	}
}

func TestPlaybackPlayWithIndex(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	broker := &stubBroker{
		presence:   []mu.Presence{renderer},
		replyTopic: "mu/v1/reply/test",
		replies: map[string]mu.ReplyEnvelope{
			"session.renew": {ID: "id-1", Type: "ack", OK: true, TS: 101},
			"playback.play": {ID: "id-1", Type: "ack", OK: true, TS: 101},
		},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{renderer.NodeID: {SessionID: "s1", Token: "t1"}}},
		Config:     Config{Identity: "tester"},
	}

	idx := int64(3)
	err := service.PlaybackPlay(context.Background(), renderer.NodeID, &idx)
	if err != nil {
		t.Fatalf("PlaybackPlay with index: %v", err)
	}
	var body mu.PlaybackPlayBody
	if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Index == nil || *body.Index != 3 {
		t.Fatalf("expected index 3")
	}
}

func TestPlaybackPause(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	broker := &stubBroker{
		presence:   []mu.Presence{renderer},
		replyTopic: "mu/v1/reply/test",
		replies: map[string]mu.ReplyEnvelope{
			"session.renew":  {ID: "id-1", Type: "ack", OK: true, TS: 101},
			"playback.pause": {ID: "id-1", Type: "ack", OK: true, TS: 101},
		},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{renderer.NodeID: {SessionID: "s1", Token: "t1"}}},
		Config:     Config{Identity: "tester"},
	}

	err := service.PlaybackPause(context.Background(), renderer.NodeID)
	if err != nil {
		t.Fatalf("PlaybackPause: %v", err)
	}
	if broker.lastCmd.Type != "playback.pause" {
		t.Fatalf("expected playback.pause command, got %s", broker.lastCmd.Type)
	}
	if broker.lastCmd.Lease == nil {
		t.Fatalf("expected lease in command")
	}
}

func TestPlaybackStop(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	broker := &stubBroker{
		presence:   []mu.Presence{renderer},
		replyTopic: "mu/v1/reply/test",
		replies: map[string]mu.ReplyEnvelope{
			"session.renew": {ID: "id-1", Type: "ack", OK: true, TS: 101},
			"playback.stop": {ID: "id-1", Type: "ack", OK: true, TS: 101},
		},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{renderer.NodeID: {SessionID: "s1", Token: "t1"}}},
		Config:     Config{Identity: "tester"},
	}

	err := service.PlaybackStop(context.Background(), renderer.NodeID)
	if err != nil {
		t.Fatalf("PlaybackStop: %v", err)
	}
	if broker.lastCmd.Type != "playback.stop" {
		t.Fatalf("expected playback.stop command, got %s", broker.lastCmd.Type)
	}
	if broker.lastCmd.Lease == nil {
		t.Fatalf("expected lease in command")
	}
}

func TestQueueAdd(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	library := mu.Presence{NodeID: "mu:library:jellyfin:test", Kind: "library", Name: "Jellyfin"}
	broker := &stubBroker{
		presence:   []mu.Presence{renderer, library},
		replyTopic: "mu/v1/reply/test",
	}

	sourcesBody, err := json.Marshal(mu.LibraryResolveSourcesReply{
		Ref:     mu.NewLibraryItemRef(library.NodeID, "track-1"),
		Sources: []mu.ResolvedSource{{URL: "http://host/track-1.flac", Mime: "audio/flac", ByteRange: true}},
	})
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	getItemBody, err := json.Marshal(mu.LibraryGetItemReply{
		Ref:        mu.NewLibraryItemRef(library.NodeID, "track-1"),
		Display:    &mu.DisplayMetadata{Title: "Song One"},
		Attributes: map[string]any{"container": false, "type": "audio"},
	})
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	broker.replies = map[string]mu.ReplyEnvelope{
		"session.renew":          {ID: "id-1", Type: "ack", OK: true, TS: 101},
		"library.getItem":        {ID: "id-1", Type: "ack", OK: true, TS: 101, Body: getItemBody},
		"library.resolveSources": {ID: "id-1", Type: "ack", OK: true, TS: 101, Body: sourcesBody},
		"queue.add":              {ID: "id-1", Type: "ack", OK: true, TS: 101},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{"lib": library.NodeID}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{renderer.NodeID: {SessionID: "s1", Token: "t1"}}},
		Config:     Config{Identity: "tester"},
	}

	err = service.QueueAdd(context.Background(), renderer.NodeID, []string{"lib:lib:track-1"}, "end", nil, "yes")
	if err != nil {
		t.Fatalf("QueueAdd: %v", err)
	}
	if broker.lastCmd.Type != "queue.add" {
		t.Fatalf("expected queue.add command, got %s", broker.lastCmd.Type)
	}
	if broker.lastCmd.Lease == nil {
		t.Fatalf("expected lease in command")
	}
	var body mu.QueueAddBody
	if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Position != "end" {
		t.Fatalf("expected position end, got %s", body.Position)
	}
	if len(body.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(body.Entries))
	}
	if body.Entries[0].Ref == nil {
		t.Fatalf("expected ref in entry")
	}
	if body.Entries[0].Resolved == nil {
		t.Fatalf("expected resolved source in entry")
	}
	if body.Entries[0].Resolved.URL != "http://host/track-1.flac" {
		t.Fatalf("expected resolved URL, got %s", body.Entries[0].Resolved.URL)
	}
}

func TestQueueAddURL(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	broker := &stubBroker{
		presence:   []mu.Presence{renderer},
		replyTopic: "mu/v1/reply/test",
		replies: map[string]mu.ReplyEnvelope{
			"session.renew": {ID: "id-1", Type: "ack", OK: true, TS: 101},
			"queue.add":     {ID: "id-1", Type: "ack", OK: true, TS: 101},
		},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{renderer.NodeID: {SessionID: "s1", Token: "t1"}}},
		Config:     Config{Identity: "tester"},
	}

	err := service.QueueAdd(context.Background(), renderer.NodeID, []string{"http://example.com/stream.mp3"}, "end", nil, "auto")
	if err != nil {
		t.Fatalf("QueueAdd URL: %v", err)
	}
	var body mu.QueueAddBody
	if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(body.Entries))
	}
	if body.Entries[0].Resolved == nil || body.Entries[0].Resolved.URL != "http://example.com/stream.mp3" {
		t.Fatalf("expected URL entry")
	}
}

func TestSetVolume(t *testing.T) {
	renderer := mu.Presence{NodeID: "mu:renderer:test:one", Kind: "renderer", Name: "Living Room"}
	broker := &stubBroker{
		presence:   []mu.Presence{renderer},
		replyTopic: "mu/v1/reply/test",
		state:      mu.RendererState{Playback: &mu.PlaybackState{Volume: 0.5}},
		replies: map[string]mu.ReplyEnvelope{
			"session.renew":      {ID: "id-1", Type: "ack", OK: true, TS: 101},
			"playback.setVolume": {ID: "id-1", Type: "ack", OK: true, TS: 101},
		},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{renderer.NodeID: {SessionID: "s1", Token: "t1"}}},
		Config:     Config{Identity: "tester"},
	}

	t.Run("absolute volume", func(t *testing.T) {
		err := service.SetVolume(context.Background(), renderer.NodeID, "80", nil)
		if err != nil {
			t.Fatalf("SetVolume: %v", err)
		}
		if broker.lastCmd.Type != "playback.setVolume" {
			t.Fatalf("expected playback.setVolume, got %s", broker.lastCmd.Type)
		}
		var body mu.PlaybackSetVolumeBody
		if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Volume != 0.8 {
			t.Fatalf("expected volume 0.8, got %f", body.Volume)
		}
	})

	t.Run("mute", func(t *testing.T) {
		broker.replies["playback.setMute"] = mu.ReplyEnvelope{ID: "id-1", Type: "ack", OK: true, TS: 101}
		muteVal := true
		err := service.SetVolume(context.Background(), renderer.NodeID, "", &muteVal)
		if err != nil {
			t.Fatalf("SetVolume mute: %v", err)
		}
		if broker.lastCmd.Type != "playback.setMute" {
			t.Fatalf("expected playback.setMute, got %s", broker.lastCmd.Type)
		}
		var body mu.PlaybackSetMuteBody
		if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if !body.Mute {
			t.Fatalf("expected mute true")
		}
	})
}

func TestLibraryBrowse(t *testing.T) {
	library := mu.Presence{NodeID: "mu:library:jellyfin:test", Kind: "library", Name: "Jellyfin"}
	broker := &stubBroker{
		presence:   []mu.Presence{library},
		replyTopic: "mu/v1/reply/test",
	}

	browseReply, err := json.Marshal(map[string]any{
		"items":      []map[string]any{{"itemId": "folder-1", "title": "Rock"}, {"itemId": "folder-2", "title": "Jazz"}},
		"totalCount": 2,
	})
	if err != nil {
		t.Fatalf("marshal reply: %v", err)
	}
	broker.replies = map[string]mu.ReplyEnvelope{
		"library.browse": {ID: "id-1", Type: "ack", OK: true, TS: 101, Body: browseReply},
	}

	service := Service{
		Broker:     broker,
		Resolver:   Resolver{Presence: broker, Config: Config{Aliases: map[string]string{"lib": library.NodeID}}},
		Clock:      stubClock{},
		IDGen:      stubIDGen{},
		LeaseStore: &memoryLeaseStore{store: map[string]mu.Lease{}},
		Config:     Config{Identity: "tester"},
	}

	result, err := service.LibraryBrowse(context.Background(), "lib", "root", 0, 50)
	if err != nil {
		t.Fatalf("LibraryBrowse: %v", err)
	}
	if result.Data == nil {
		t.Fatalf("expected browse data")
	}
	if broker.lastNode != library.NodeID {
		t.Fatalf("expected library node %s, got %s", library.NodeID, broker.lastNode)
	}
	if broker.lastCmd.Type != "library.browse" {
		t.Fatalf("expected library.browse command, got %s", broker.lastCmd.Type)
	}
	var body mu.LibraryBrowseBody
	if err := json.Unmarshal(broker.lastCmd.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.ContainerID != "root" {
		t.Fatalf("expected container id root, got %s", body.ContainerID)
	}
	if body.Start != 0 {
		t.Fatalf("expected start 0, got %d", body.Start)
	}
	if body.Count != 50 {
		t.Fatalf("expected count 50, got %d", body.Count)
	}
}
