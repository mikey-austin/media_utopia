package renderercore

import (
	"errors"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// StatePublisher publishes renderer state to an observer.
type StatePublisher interface {
	PublishState(state *mu.RendererState) error
}

// PresencePublisher publishes renderer presence to an observer.
type PresencePublisher interface {
	PublishPresence(presence *mu.Presence) error
}

// StatePublisherFunc adapts a function to StatePublisher.
type StatePublisherFunc func(*mu.RendererState) error

func (f StatePublisherFunc) PublishState(state *mu.RendererState) error { return f(state) }

// PresencePublisherFunc adapts a function to PresencePublisher.
type PresencePublisherFunc func(*mu.Presence) error

func (f PresencePublisherFunc) PublishPresence(p *mu.Presence) error { return f(p) }

// ChannelStatePublisher sends state to a channel. Non-blocking: drops if full.
type ChannelStatePublisher struct {
	ch chan<- *mu.RendererState
}

func NewChannelStatePublisher(ch chan<- *mu.RendererState) *ChannelStatePublisher {
	return &ChannelStatePublisher{ch: ch}
}

func (p *ChannelStatePublisher) PublishState(state *mu.RendererState) error {
	select {
	case p.ch <- state:
	default:
	}
	return nil
}

// ChannelPresencePublisher sends presence to a channel. Non-blocking: drops if full.
type ChannelPresencePublisher struct {
	ch chan<- *mu.Presence
}

func NewChannelPresencePublisher(ch chan<- *mu.Presence) *ChannelPresencePublisher {
	return &ChannelPresencePublisher{ch: ch}
}

func (p *ChannelPresencePublisher) PublishPresence(presence *mu.Presence) error {
	select {
	case p.ch <- presence:
	default:
	}
	return nil
}

// MultiStatePublisher fans out to N state publishers.
type MultiStatePublisher struct {
	publishers []StatePublisher
}

func NewMultiPublisher(publishers ...StatePublisher) *MultiStatePublisher {
	return &MultiStatePublisher{publishers: publishers}
}

func (m *MultiStatePublisher) PublishState(state *mu.RendererState) error {
	var errs []error
	for _, p := range m.publishers {
		if err := p.PublishState(state); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// MultiPresencePublisher fans out to N presence publishers.
type MultiPresencePublisher struct {
	publishers []PresencePublisher
}

func NewMultiPresencePublisher(publishers ...PresencePublisher) *MultiPresencePublisher {
	return &MultiPresencePublisher{publishers: publishers}
}

func (m *MultiPresencePublisher) PublishPresence(presence *mu.Presence) error {
	var errs []error
	for _, p := range m.publishers {
		if err := p.PublishPresence(presence); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
