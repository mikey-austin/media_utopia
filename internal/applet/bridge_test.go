//go:build gtk

package applet

import (
	"testing"
	"time"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

func TestBridge_CallbackFires(t *testing.T) {
	stateCh := make(chan *mu.RendererState, 1)
	var received *mu.RendererState
	b := NewBridge(stateCh, func(s *mu.RendererState) {
		received = s
	})

	state := &mu.RendererState{
		Playback: &mu.PlaybackState{Status: "playing"},
		TS:       1000,
	}
	stateCh <- state

	// Drain one update manually (in tests we call drainOnce instead of the GTK loop)
	b.drainOnce()

	if received == nil {
		t.Fatal("callback not called")
	}
	if received.Playback.Status != "playing" {
		t.Fatalf("expected playing, got %s", received.Playback.Status)
	}
}

func TestBridge_NoBlockOnEmpty(t *testing.T) {
	stateCh := make(chan *mu.RendererState, 1)
	b := NewBridge(stateCh, func(s *mu.RendererState) {})

	// drainOnce with empty channel should return immediately
	done := make(chan struct{})
	go func() {
		b.drainOnce()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("drainOnce blocked on empty channel")
	}
}
