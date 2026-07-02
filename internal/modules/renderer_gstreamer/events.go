package renderergstreamer

import (
	renderercore "github.com/mikey-austin/media_utopia/internal/modules/renderer_core"
)

// The driver's async events are the shared renderer_core driver events;
// these aliases keep the driver code readable in GStreamer terms.
type (
	// Event carries one asynchronous notification from the driver.
	Event = renderercore.DriverEvent
	// EventKind classifies asynchronous events surfaced by the driver.
	EventKind = renderercore.DriverEventKind
)

const (
	// EventEOS is emitted when GStreamer reports end-of-stream on the
	// current pipeline. Consumers should advance the queue.
	EventEOS = renderercore.DriverEventEOS
	// EventError is emitted on a fatal pipeline error (decoder failure,
	// pipewiresink socket loss, etc.). The pipeline is torn down before the
	// event is delivered.
	EventError = renderercore.DriverEventError
	// EventWarning is informational — typically `pipewiresink` reporting
	// that a target-object didn't exist and traffic fell back to the
	// default sink. Worth logging but not fatal.
	EventWarning = renderercore.DriverEventWarning
	// EventPipewireDown is emitted when the healthcheck loop can't reach
	// pipewire's socket for several consecutive probes. The driver has
	// already torn down its current pipeline; the next Play call will
	// rebuild it once pipewire is back.
	EventPipewireDown = renderercore.DriverEventAudioDown
)
