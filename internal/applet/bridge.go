//go:build gtk

package applet

import (
	"github.com/gotk3/gotk3/glib"
	"github.com/mikey-austin/media_utopia/pkg/mu"
)

// Bridge marshals state updates from a Go channel to a callback.
// In the real app, the callback is invoked on the GTK main thread via glib.IdleAdd.
// For testing, drainOnce can be called directly.
type Bridge struct {
	stateCh  <-chan *mu.RendererState
	onUpdate func(*mu.RendererState)
}

// NewBridge creates a Bridge that reads from stateCh and dispatches
// each update to onUpdate.
func NewBridge(stateCh <-chan *mu.RendererState, onUpdate func(*mu.RendererState)) *Bridge {
	return &Bridge{stateCh: stateCh, onUpdate: onUpdate}
}

// Start begins polling the state channel and dispatching to the GTK main thread.
// Call this from a goroutine — it blocks until the channel is closed.
func (b *Bridge) Start() {
	for state := range b.stateCh {
		s := state // capture for closure
		glib.IdleAdd(func() bool {
			b.onUpdate(s)
			return false // remove idle source after one call
		})
	}
}

// drainOnce processes one pending state update if available. For testing.
func (b *Bridge) drainOnce() {
	select {
	case state := <-b.stateCh:
		b.onUpdate(state)
	default:
	}
}
