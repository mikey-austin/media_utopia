//go:build !upnp

package rendererupnp

import (
	"context"
	"errors"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
)

// Enabled indicates upnp build disabled.
const Enabled = false

// Config stub.
type Config struct {
	TopicBase         string
	Provider          string
	Namespace         string
	Listen            string
	DiscoveryInterval any
	Timeout           any
	NamePrefix        string
}

// Module stub.
type Module struct{}

// NewModule errors when upnp tag missing.
func NewModule(_ any, _ *mqttserver.Client, _ Config) (*Module, error) {
	return nil, errors.New("upnp build tag not enabled")
}

// Run is a no-op.
func (m *Module) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
