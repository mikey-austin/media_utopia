package renderermpv

// EventKind classifies asynchronous events surfaced by the driver.
type EventKind int

const (
	// EventEOS is emitted when the current track reaches end-of-file.
	// Consumers should advance the queue.
	EventEOS EventKind = iota
	// EventError is emitted when the current track ends with a playback
	// error (demux/decode/network failure). The handle is torn down before
	// the event is delivered.
	EventError
	// EventWarning carries mpv log messages at warn level. Worth logging
	// but not fatal.
	EventWarning
	// EventAudioDown is emitted when the healthcheck loop can't reach the
	// pipewire socket for several consecutive probes. It distinguishes
	// "stream ended" from "audio server gone" in published state.
	EventAudioDown
)

// Event carries one asynchronous notification from the driver.
type Event struct {
	Kind    EventKind
	Message string
}
