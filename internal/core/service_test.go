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
}

func (s *stubBroker) ReplyTopic() string { return s.replyTopic }

func (s *stubBroker) PublishCommand(ctx context.Context, nodeID string, cmd mu.CommandEnvelope) (mu.ReplyEnvelope, error) {
	s.lastNode = nodeID
	s.lastCmd = cmd
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
	data := []byte("\n# comment\nmu:track:one\nhttp://example/stream\n")
	entries, err := service.parseQueueFile("muq", data)
	if err != nil {
		t.Fatalf("parseQueueFile: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Ref == nil || entries[0].Ref.ID != "mu:track:one" {
		t.Fatalf("expected ref entry")
	}
	if entries[1].Resolved == nil || entries[1].Resolved.URL != "http://example/stream" {
		t.Fatalf("expected resolved entry")
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
