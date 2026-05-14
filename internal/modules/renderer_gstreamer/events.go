package renderergstreamer

// EventKind classifies asynchronous events surfaced by the driver.
type EventKind int

const (
	// EventEOS is emitted when GStreamer reports end-of-stream on the
	// current pipeline. Consumers should advance the queue.
	EventEOS EventKind = iota
	// EventError is emitted on a fatal pipeline error (decoder failure,
	// pipewiresink socket loss, etc.). The pipeline is torn down before the
	// event is delivered.
	EventError
	// EventWarning is informational — typically `pipewiresink` reporting
	// that a target-object didn't exist and traffic fell back to the
	// default sink. Worth logging but not fatal.
	EventWarning
	// EventPipewireDown is emitted when the healthcheck loop can't reach
	// pipewire's socket for several consecutive probes. The driver has
	// already torn down its current pipeline; the next Play call will
	// rebuild it once pipewire is back.
	EventPipewireDown
)

// Event carries one asynchronous notification from the driver.
type Event struct {
	Kind    EventKind
	Message string
}
