package core

import (
	"context"
	"strings"
	"testing"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

type fakeBroker struct {
	presence []mu.Presence
}

func (f fakeBroker) ReplyTopic() string { return "" }
func (f fakeBroker) PublishCommand(ctx context.Context, nodeID string, cmd mu.CommandEnvelope) (mu.ReplyEnvelope, error) {
	return mu.ReplyEnvelope{}, nil
}
func (f fakeBroker) ListPresence(ctx context.Context) ([]mu.Presence, error) { return f.presence, nil }
func (f fakeBroker) GetRendererState(ctx context.Context, nodeID string) (mu.RendererState, error) {
	return mu.RendererState{}, nil
}
func (f fakeBroker) GetZoneState(ctx context.Context, nodeID string) (mu.ZoneState, error) {
	return mu.ZoneState{}, nil
}
func (f fakeBroker) WatchRenderer(ctx context.Context, nodeID string) (<-chan mu.RendererState, <-chan mu.Event, <-chan error) {
	stateCh := make(chan mu.RendererState)
	eventCh := make(chan mu.Event)
	errCh := make(chan error)
	close(stateCh)
	close(eventCh)
	close(errCh)
	return stateCh, eventCh, errCh
}

func TestResolverAlias(t *testing.T) {
	presence := []mu.Presence{{NodeID: "mu:renderer:one", Kind: "renderer", Name: "Living Room"}}
	resolver := Resolver{
		Presence: fakeBroker{presence: presence},
		Config: Config{
			Aliases: map[string]string{"livingroom": "mu:renderer:one"},
		},
	}
	got, err := resolver.ResolveRenderer(context.Background(), "livingroom")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.NodeID != "mu:renderer:one" {
		t.Fatalf("expected alias resolution")
	}
}

func TestResolverAmbiguous(t *testing.T) {
	presence := []mu.Presence{
		{NodeID: "mu:renderer:one", Kind: "renderer", Name: "Living Room"},
		{NodeID: "mu:renderer:two", Kind: "renderer", Name: "Living Room"},
	}
	resolver := Resolver{Presence: fakeBroker{presence: presence}}
	_, err := resolver.ResolveRenderer(context.Background(), "Living Room")
	if err == nil {
		t.Fatalf("expected ambiguous error")
	}
}

func TestResolverAutoResolve(t *testing.T) {
	presence := []mu.Presence{
		{NodeID: "mu:renderer:solo", Kind: "renderer", Name: "Solo Speaker"},
	}
	resolver := Resolver{Presence: fakeBroker{presence: presence}}
	got, err := resolver.ResolveRenderer(context.Background(), "")
	if err != nil {
		t.Fatalf("auto-resolve: %v", err)
	}
	if got.NodeID != "mu:renderer:solo" {
		t.Fatalf("expected auto-resolve to sole renderer, got %s", got.NodeID)
	}
}

func TestResolverSelectorRequired(t *testing.T) {
	presence := []mu.Presence{
		{NodeID: "mu:renderer:one", Kind: "renderer", Name: "Living Room"},
		{NodeID: "mu:renderer:two", Kind: "renderer", Name: "Bedroom"},
	}
	resolver := Resolver{Presence: fakeBroker{presence: presence}}
	_, err := resolver.ResolveRenderer(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error when multiple renderers and no selector")
	}
	cliErr, ok := err.(*CLIError)
	if !ok {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.Code != ExitUsage {
		t.Fatalf("expected ExitUsage (%d), got %d", ExitUsage, cliErr.Code)
	}
}

func TestResolverNotFound(t *testing.T) {
	presence := []mu.Presence{
		{NodeID: "mu:renderer:one", Kind: "renderer", Name: "Living Room"},
	}
	resolver := Resolver{Presence: fakeBroker{presence: presence}}
	_, err := resolver.ResolveRenderer(context.Background(), "nonexistent")
	if err == nil {
		t.Fatalf("expected not-found error")
	}
	cliErr, ok := err.(*CLIError)
	if !ok {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.Code != ExitNotFound {
		t.Fatalf("expected ExitNotFound (%d), got %d", ExitNotFound, cliErr.Code)
	}
}

func TestResolveLibrary(t *testing.T) {
	presence := []mu.Presence{
		{NodeID: "mu:renderer:one", Kind: "renderer", Name: "Living Room"},
		{NodeID: "mu:library:music", Kind: "library", Name: "Music Library"},
		{NodeID: "mu:library:video", Kind: "library", Name: "Video Library"},
	}
	resolver := Resolver{Presence: fakeBroker{presence: presence}}
	got, err := resolver.ResolveLibrary(context.Background(), "Music Library")
	if err != nil {
		t.Fatalf("resolve library: %v", err)
	}
	if got.NodeID != "mu:library:music" {
		t.Fatalf("expected mu:library:music, got %s", got.NodeID)
	}
	if got.Kind != "library" {
		t.Fatalf("expected kind=library, got %s", got.Kind)
	}
}

func TestResolvePlaylistServer(t *testing.T) {
	presence := []mu.Presence{
		{NodeID: "mu:renderer:one", Kind: "renderer", Name: "Living Room"},
		{NodeID: "mu:playlist:main", Kind: "playlist", Name: "Main Playlist"},
	}
	resolver := Resolver{Presence: fakeBroker{presence: presence}}
	got, err := resolver.ResolvePlaylistServer(context.Background(), "Main Playlist")
	if err != nil {
		t.Fatalf("resolve playlist server: %v", err)
	}
	if got.NodeID != "mu:playlist:main" {
		t.Fatalf("expected mu:playlist:main, got %s", got.NodeID)
	}
	if got.Kind != "playlist" {
		t.Fatalf("expected kind=playlist, got %s", got.Kind)
	}
}

func TestResolverExactNodeID(t *testing.T) {
	presence := []mu.Presence{
		{NodeID: "mu:renderer:one", Kind: "renderer", Name: "Living Room"},
		{NodeID: "mu:renderer:two", Kind: "renderer", Name: "Bedroom"},
	}
	resolver := Resolver{Presence: fakeBroker{presence: presence}}
	got, err := resolver.ResolveRenderer(context.Background(), "mu:renderer:two")
	if err != nil {
		t.Fatalf("resolve exact node ID: %v", err)
	}
	if got.NodeID != "mu:renderer:two" {
		t.Fatalf("expected mu:renderer:two, got %s", got.NodeID)
	}
	if got.Name != "Bedroom" {
		t.Fatalf("expected name=Bedroom, got %s", got.Name)
	}
}

func TestResolverCaseInsensitive(t *testing.T) {
	presence := []mu.Presence{
		{NodeID: "mu:renderer:one", Kind: "renderer", Name: "Living Room"},
	}
	resolver := Resolver{Presence: fakeBroker{presence: presence}}

	cases := []string{"living room", "LIVING ROOM", "Living Room", "lIvInG rOoM"}
	for _, sel := range cases {
		got, err := resolver.ResolveRenderer(context.Background(), sel)
		if err != nil {
			t.Fatalf("case-insensitive resolve %q: %v", sel, err)
		}
		if got.NodeID != "mu:renderer:one" {
			t.Fatalf("case-insensitive resolve %q: expected mu:renderer:one, got %s", sel, got.NodeID)
		}
	}
}

func TestResolveSelectorPrefixAndSubstring(t *testing.T) {
	presence := []mu.Presence{
		{Name: "Venus Music", NodeID: "mu:library:filesystem:venus:music", Kind: "library"},
		{Name: "Jellyfin Library", NodeID: "mu:library:jellyfin:coltrane:default", Kind: "library"},
		{Name: "Podcasts", NodeID: "mu:library:podcast:coltrane:default", Kind: "library"},
	}

	// Unique case-insensitive prefix resolves.
	got, err := resolveSelector("venus", presence, nil)
	if err != nil {
		t.Fatalf("prefix resolve: %v", err)
	}
	if got.Name != "Venus Music" {
		t.Fatalf("prefix resolve got %q", got.Name)
	}

	// Unique substring resolves.
	got, err = resolveSelector("jelly", presence, nil)
	if err != nil {
		t.Fatalf("substring resolve: %v", err)
	}
	if got.Name != "Jellyfin Library" {
		t.Fatalf("substring resolve got %q", got.Name)
	}

	// Ambiguous match errors and lists candidates.
	ambiguous := append(presence, mu.Presence{Name: "Venus Video", NodeID: "mu:library:filesystem:venus:video", Kind: "library"})
	_, err = resolveSelector("venus", ambiguous, nil)
	if err == nil || !strings.Contains(err.Error(), "Venus Music") || !strings.Contains(err.Error(), "Venus Video") {
		t.Fatalf("ambiguous error must list candidates, got %v", err)
	}

	// No match lists what exists.
	_, err = resolveSelector("nonexistent", presence, nil)
	if err == nil || !strings.Contains(err.Error(), "Venus Music") {
		t.Fatalf("not-found error must list available nodes, got %v", err)
	}
}
