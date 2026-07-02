package renderermpv

import (
	renderercore "github.com/mikey-austin/media_utopia/internal/modules/renderer_core"
)

// The driver's async events are the shared renderer_core driver events;
// these aliases keep the driver code readable in mpv terms.
type (
	// Event carries one asynchronous notification from the driver.
	Event = renderercore.DriverEvent
	// EventKind classifies asynchronous events surfaced by the driver.
	EventKind = renderercore.DriverEventKind
)

const (
	// EventEOS is emitted when the current track reaches end-of-file.
	// Consumers should advance the queue.
	EventEOS = renderercore.DriverEventEOS
	// EventError is emitted when the current track ends with a playback
	// error (demux/decode/network failure). The handle is torn down before
	// the event is delivered.
	EventError = renderercore.DriverEventError
	// EventWarning carries mpv log messages at warn level. Worth logging
	// but not fatal.
	EventWarning = renderercore.DriverEventWarning
	// EventAudioDown is emitted when the healthcheck loop can't reach the
	// pipewire socket for several consecutive probes. It distinguishes
	// "stream ended" from "audio server gone" in published state.
	EventAudioDown = renderercore.DriverEventAudioDown
)
