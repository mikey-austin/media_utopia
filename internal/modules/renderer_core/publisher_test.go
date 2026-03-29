package renderercore

import (
	"errors"
	"testing"

	"github.com/mikey-austin/media_utopia/pkg/mu"
)

func testState() *mu.RendererState {
	return &mu.RendererState{
		Playback: &mu.PlaybackState{Status: "playing", Volume: 0.8},
		Queue:    &mu.QueueState{Revision: 1, Length: 2, Index: 0},
		TS:       1000,
	}
}

func TestChannelPublisher_SendsState(t *testing.T) {
	ch := make(chan *mu.RendererState, 1)
	pub := NewChannelStatePublisher(ch)
	state := testState()

	if err := pub.PublishState(state); err != nil {
		t.Fatal(err)
	}
	got := <-ch
	if got.Playback.Status != "playing" {
		t.Fatalf("expected playing, got %s", got.Playback.Status)
	}
}

func TestChannelPublisher_DropsWhenFull(t *testing.T) {
	ch := make(chan *mu.RendererState, 1)
	pub := NewChannelStatePublisher(ch)

	// Fill the channel
	pub.PublishState(testState())
	// Second send should not block and not error
	if err := pub.PublishState(testState()); err != nil {
		t.Fatal(err)
	}
	// Channel should still have exactly one item
	if len(ch) != 1 {
		t.Fatalf("expected channel length 1, got %d", len(ch))
	}
}

func TestMultiPublisher_FansOut(t *testing.T) {
	var called1, called2 bool
	p1 := StatePublisherFunc(func(s *mu.RendererState) error { called1 = true; return nil })
	p2 := StatePublisherFunc(func(s *mu.RendererState) error { called2 = true; return nil })
	multi := NewMultiPublisher(p1, p2)

	if err := multi.PublishState(testState()); err != nil {
		t.Fatal(err)
	}
	if !called1 || !called2 {
		t.Fatal("expected both publishers called")
	}
}

func TestMultiPublisher_CollectsErrors(t *testing.T) {
	e1 := errors.New("fail1")
	e2 := errors.New("fail2")
	p1 := StatePublisherFunc(func(s *mu.RendererState) error { return e1 })
	p2 := StatePublisherFunc(func(s *mu.RendererState) error { return e2 })
	multi := NewMultiPublisher(p1, p2)

	err := multi.PublishState(testState())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("expected both errors, got: %v", err)
	}
}

func TestMultiPublisher_Empty(t *testing.T) {
	multi := NewMultiPublisher()
	if err := multi.PublishState(testState()); err != nil {
		t.Fatal(err)
	}
}

func TestChannelPresencePublisher_SendsPresence(t *testing.T) {
	ch := make(chan *mu.Presence, 1)
	pub := NewChannelPresencePublisher(ch)
	p := &mu.Presence{NodeID: "test", Kind: "renderer", Name: "Test"}

	if err := pub.PublishPresence(p); err != nil {
		t.Fatal(err)
	}
	got := <-ch
	if got.NodeID != "test" {
		t.Fatalf("expected test, got %s", got.NodeID)
	}
}
