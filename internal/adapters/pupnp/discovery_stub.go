//go:build !upnp

package pupnp

import (
	"context"
	"time"
)

// Client is a stub when the upnp build tag is disabled.
type Client struct{}

// DiscoveryResult is empty in stub mode.
type DiscoveryResult struct{}

// NewClient returns an error when upnp tag is disabled.
func NewClient(_ string) (*Client, error) {
	return nil, ErrUPNPDisabled
}

// Close is a no-op in stub mode.
func (c *Client) Close() {}

// Discover always returns an error when upnp tag is disabled.
func (c *Client) Discover(_ context.Context, _ string, _ time.Duration) ([]DiscoveryResult, error) {
	return nil, ErrUPNPDisabled
}

// ErrUPNPDisabled indicates the upnp build tag is missing.
var ErrUPNPDisabled = errUPNPDisabled("upnp build tag not enabled")

type errUPNPDisabled string

func (e errUPNPDisabled) Error() string { return string(e) }
