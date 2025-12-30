package ports

import (
	"context"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// Broker publishes commands and reads retained state/presence.
type Broker interface {
	ReplyTopic() string
	PublishCommand(ctx context.Context, nodeID string, cmd mu.CommandEnvelope) (mu.ReplyEnvelope, error)
	ListPresence(ctx context.Context) ([]mu.Presence, error)
	GetRendererState(ctx context.Context, nodeID string) (mu.RendererState, error)
	WatchRenderer(ctx context.Context, nodeID string) (<-chan mu.RendererState, <-chan mu.Event, <-chan error)
}

// Clock returns the current unix time in seconds.
type Clock interface {
	NowUnix() int64
}

// IDGen returns unique correlation IDs.
type IDGen interface {
	NewID() string
}

// LeaseStore persists lease tokens between commands.
type LeaseStore interface {
	Get(rendererID string) (mu.Lease, bool, error)
	Put(rendererID string, lease mu.Lease) error
	Clear(rendererID string) error
}
