package core

import (
	"context"
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
