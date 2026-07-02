package renderercore

// DriverEventKind classifies asynchronous events surfaced by playback
// drivers.
type DriverEventKind int

const (
	// DriverEventEOS is emitted when the current track reaches
	// end-of-stream. Consumers should advance the queue.
	DriverEventEOS DriverEventKind = iota
	// DriverEventError is emitted on a fatal playback error (decoder
	// failure, network loss, audio sink death). The driver has already
	// torn the failed playback down when the event is delivered.
	DriverEventError
	// DriverEventWarning is informational — worth logging but not fatal.
	DriverEventWarning
	// DriverEventAudioDown is emitted when the driver detects the audio
	// backend (e.g. the pipewire socket) is unreachable, distinguishing
	// "stream ended" from "audio server gone" in published state.
	DriverEventAudioDown
)

// DriverEvent carries one asynchronous notification from a driver.
type DriverEvent struct {
	Kind    DriverEventKind
	Message string
}

// DriverEventSource is implemented by drivers that expose async events
// (EOS / errors / audio-backend health). The Module wires this up if
// present so EOS does not need to be detected by polling position against
// duration.
type DriverEventSource interface {
	Events() <-chan DriverEvent
}
