//go:build !upnp

package upnplibrary

import (
	"context"
	"errors"

	"github.com/mikey-austin/media_utopia/internal/adapters/mqttserver"
)

// Enabled indicates the upnp build tag is inactive.
const Enabled = false

// Config mirrors the real module config for build-time compatibility.
type Config struct {
	NodeID                 string
	TopicBase              string
	Name                   string
	Listen                 string
	Timeout                any
	CacheTTL               any
	CacheSize              int
	CacheCompress          bool
	BrowseCacheTTL         any
	BrowseCacheSize        int
	MaxConcurrentRequests  int
	PublishTimeoutCooldown any
	DiscoveryInterval      any
}

// Module is a stubbed module.
type Module struct{}

// NewModule returns an error when the upnp build tag is disabled.
func NewModule(_ any, _ *mqttserver.Client, _ Config) (*Module, error) {
	return nil, errors.New("upnp build tag not enabled")
}

// Run is a no-op stub.
func (m *Module) Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
