//go:build mpv

package renderermpv

import (
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// eventWaitTimeout bounds each mpv_wait_event call so the event
	// goroutine can never hang forever on a wedged handle.
	eventWaitTimeout = 1.0 // seconds

	healthcheckInterval         = 30 * time.Second
	healthcheckFailureThreshold = 2 // ~60s of unhealth before we react

	// closeBudget bounds Close's wait for handle teardown. mpv's quit +
	// terminate_destroy is synchronous and fast for audio-only handles;
	// the budget is a backstop that keeps shutdown well inside docker's
	// 30s stop grace.
	closeBudget = 8 * time.Second
)

// track is one active mpv handle: the playing (or fading-out) instance of
// one URL. Position/duration arrive via property observation and are
// cached in atomics so Position() never crosses cgo on the hot path.
type track struct {
	h        *mpvHandle
	posMS    atomic.Int64
	durMS    atomic.Int64
	posSeen  atomic.Bool
	gainBits atomic.Uint64 // math.Float64bits of the crossfade gain (0..1)
	done     chan struct{} // closed after terminateDestroy
	quitOnce sync.Once
}

func (t *track) gain() float64       { return math.Float64frombits(t.gainBits.Load()) }
func (t *track) storeGain(g float64) { t.gainBits.Store(math.Float64bits(g)) }

// quit asks the handle's core to shut down. The event goroutine observes
// evShutdown and performs terminateDestroy — quit itself never blocks and
// is safe from any goroutine.
func (t *track) quit() {
	t.quitOnce.Do(func() {
		_ = t.h.command("quit")
	})
}

// Driver implements renderer_core.Driver on libmpv, one handle per track.
type Driver struct {
	mu        sync.Mutex
	ao        string
	device    string
	crossfade time.Duration
	extraOpts map[string]string

	volume  float64
	muted   bool
	current *track

	fades fadeSet

	// events carries EOS / error / warning / audio-down notifications up to
	// the Module. Buffered; emit drops rather than blocking event loops.
	events chan Event

	// wg tracks per-track event goroutines and the healthcheck loop.
	wg sync.WaitGroup

	healthCancel chan struct{}
	closeOnce    sync.Once
	closed       bool
}

// NewDriver creates an mpv driver. ao selects the audio output backend
// (pipewire, alsa, pulse, null, ...); mpvOptions are applied verbatim to
// every handle as the stream-tuning escape hatch.
func NewDriver(ao string, device string, crossfade time.Duration, mpvOptions map[string]string) (*Driver, error) {
	if ao == "" {
		ao = "pipewire"
	}
	d := &Driver{
		ao:        ao,
		device:    device,
		crossfade: crossfade,
		extraOpts: mpvOptions,
		volume:    1.0,
		events:    make(chan Event, 16),
	}
	if ao == "pipewire" {
		d.healthCancel = make(chan struct{})
		d.wg.Add(1)
		go d.healthcheckLoop()
	}
	return d, nil
}

// Events returns the async notification channel. Events may be dropped
// (with a stderr warning) if the consumer falls behind.
func (d *Driver) Events() <-chan Event {
	return d.events
}

func (d *Driver) emit(ev Event) {
	select {
	case d.events <- ev:
	default:
		fmt.Fprintf(os.Stderr, "mpv: dropped event kind=%d msg=%q (consumer slow)\n", ev.Kind, ev.Message)
	}
}

// newTrack builds and starts a handle for url. On any error the handle is
// destroyed before returning (no event goroutine is running yet, so a
// direct terminateDestroy is safe).
func (d *Driver) newTrack(url string, positionMS int64, initialGain float64) (*track, error) {
	h, err := newMPVHandle()
	if err != nil {
		return nil, err
	}
	t := &track{h: h, done: make(chan struct{})}
	t.storeGain(initialGain)
	fail := func(err error) (*track, error) {
		h.terminateDestroy()
		return nil, err
	}
	for _, kv := range handleOptions(d.ao, d.device, d.extraOpts, positionMS) {
		if err := h.setOptionString(kv[0], kv[1]); err != nil {
			return fail(err)
		}
	}
	if err := h.initialize(); err != nil {
		return fail(err)
	}
	if err := h.requestLogMessages("warn"); err != nil {
		return fail(err)
	}
	if err := h.observeDouble("time-pos"); err != nil {
		return fail(err)
	}
	if err := h.observeDouble("duration"); err != nil {
		return fail(err)
	}
	// Volume/mute are applied before loadfile so playback never starts at
	// the wrong level (avoids the audible pop at crossfade start).
	if err := h.setPropertyDouble("volume", effectiveVolume(d.volume, initialGain)); err != nil {
		return fail(err)
	}
	if err := h.setPropertyFlag("mute", d.muted); err != nil {
		return fail(err)
	}
	if err := h.command("loadfile", url); err != nil {
		return fail(err)
	}
	d.wg.Add(1)
	go d.eventLoop(t)
	return t, nil
}

// eventLoop owns t's event queue: it caches position/duration, forwards
// EOS/error/warning events (only while t is the current track — an
// outgoing crossfade handle finishing must not double-advance the queue),
// and performs the final terminateDestroy after quit.
func (d *Driver) eventLoop(t *track) {
	defer d.wg.Done()
	for {
		ev := t.h.waitEvent(eventWaitTimeout)
		switch ev.kind {
		case evShutdown:
			t.h.terminateDestroy()
			close(t.done)
			return
		case evPropertyChange:
			if !ev.propOK {
				continue
			}
			switch ev.propName {
			case "time-pos":
				t.posMS.Store(int64(ev.propDouble * 1000))
				t.posSeen.Store(true)
			case "duration":
				t.durMS.Store(int64(ev.propDouble * 1000))
			}
		case evEndFile:
			switch ev.endReason {
			case endEOF:
				if d.isCurrent(t) {
					d.emit(Event{Kind: EventEOS})
				}
			case endError:
				if d.isCurrent(t) {
					d.emit(Event{Kind: EventError, Message: ev.message})
				}
			}
		case evLogMessage:
			d.emit(Event{Kind: EventWarning, Message: ev.message})
		}
	}
}

func (d *Driver) isCurrent(t *track) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current == t
}

// applyGain is the fade goroutines' volume sink: it composes the fade gain
// with the user volume and pushes the result to the handle.
func (d *Driver) applyGain(t *track, gain float64) {
	t.storeGain(gain)
	d.mu.Lock()
	user := d.volume
	d.mu.Unlock()
	_ = t.h.setPropertyDouble("volume", effectiveVolume(user, gain))
}

// Play starts playback of url. With crossfade configured and a track
// already playing, the outgoing handle fades down while the incoming one
// fades up; the audio server mixes the two.
func (d *Driver) Play(url string, positionMS int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return errors.New("driver closed")
	}
	d.fades.cancelAll()

	old := d.current
	crossfading := d.crossfade > 0 && old != nil
	initialGain := 1.0
	if crossfading {
		initialGain = 0.0
	}
	t, err := d.newTrack(url, positionMS, initialGain)
	if err != nil {
		return err
	}
	d.current = t

	if crossfading {
		inCtx, inJob := d.fades.start()
		go func() {
			defer d.fades.finish(inJob)
			runFade(inCtx, d.crossfade, 0, 1, func(g float64) { d.applyGain(t, g) })
		}()
		outCtx, outJob := d.fades.start()
		go func() {
			defer d.fades.finish(outJob)
			runFade(outCtx, d.crossfade, old.gain(), 0, func(g float64) { d.applyGain(old, g) })
			old.quit()
		}()
	} else if old != nil {
		old.quit()
	}
	return nil
}

// Pause pauses playback. During a crossfade the fade is cancelled first
// (ramps snap to their targets) so pause applies to the incoming track.
func (d *Driver) Pause() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		return errors.New("not playing")
	}
	d.fades.cancelAll()
	return d.current.h.setPropertyFlag("pause", true)
}

// Resume resumes playback.
func (d *Driver) Resume() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		return errors.New("not playing")
	}
	return d.current.h.setPropertyFlag("pause", false)
}

// Stop stops playback and discards the current handle.
func (d *Driver) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.current == nil {
		return nil
	}
	d.fades.cancelAll()
	d.current.quit()
	d.current = nil
	return nil
}

// SeekTo seeks to an absolute position. mpv coalesces stacked seeks
// internally, so no debounce is needed (the GStreamer workaround is
// deliberately not ported).
func (d *Driver) SeekTo(positionMS int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		return errors.New("not playing")
	}
	return d.current.h.command("seek", strconv.FormatFloat(float64(positionMS)/1000, 'f', 3, 64), "absolute")
}

// SetVolume sets the user volume (0..1), composed with any in-flight fade
// gain.
func (d *Driver) SetVolume(volume float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.volume = volume
	if d.current != nil {
		return d.current.h.setPropertyDouble("volume", effectiveVolume(volume, d.current.gain()))
	}
	return nil
}

// SetMute toggles mpv's softvol mute; it is independent of the volume
// property so fades and user volume compose cleanly around it.
func (d *Driver) SetMute(mute bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.muted = mute
	if d.current != nil {
		return d.current.h.setPropertyFlag("mute", mute)
	}
	return nil
}

// Position returns cached position/duration for the current track.
func (d *Driver) Position() (int64, int64, bool) {
	d.mu.Lock()
	t := d.current
	d.mu.Unlock()
	if t == nil || !t.posSeen.Load() {
		return 0, 0, false
	}
	return t.posMS.Load(), t.durMS.Load(), true
}

// Close shuts the driver down: fades are cancelled and awaited, every
// outstanding handle is quit, and their event goroutines perform the
// bounded synchronous terminate_destroy. mpv's clean quit gives pipewire a
// proper client disconnect (rather than a socket EOF that leaves stale
// daemon state). After Close, driver calls fail.
func (d *Driver) Close() error {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		survivor := d.current
		d.current = nil
		d.fades.cancelAll()
		d.mu.Unlock()

		if d.healthCancel != nil {
			close(d.healthCancel)
		}
		// Cancelled fade-outs quit their tracks on exit; wait for that so
		// every handle has a quit in flight before we wait on teardown.
		d.fades.wait()
		if survivor != nil {
			survivor.quit()
		}
		done := make(chan struct{})
		go func() {
			d.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			close(d.events)
		case <-time.After(closeBudget):
			// Event goroutines still alive — leave events open so nothing
			// sends on a closed channel; the process is exiting anyway.
			fmt.Fprintf(os.Stderr, "mpv: close budget exceeded waiting for handle teardown\n")
		}
	})
	return nil
}

// healthcheckLoop probes the pipewire socket and emits EventAudioDown after
// consecutive failures — distinguishing "stream ended" from "audio server
// gone" in published state. mpv itself surfaces in-stream AO failures as
// end-file errors, so no teardown is performed here.
func (d *Driver) healthcheckLoop() {
	defer d.wg.Done()
	ticker := time.NewTicker(healthcheckInterval)
	defer ticker.Stop()
	failures := 0
	notified := false
	for {
		select {
		case <-d.healthCancel:
			return
		case <-ticker.C:
		}
		if probePipewire() {
			failures = 0
			notified = false
			continue
		}
		failures++
		if failures >= healthcheckFailureThreshold && !notified {
			notified = true
			d.emit(Event{Kind: EventAudioDown, Message: "pipewire socket unreachable"})
		}
	}
}

// probePipewire returns true if the pipewire control socket is connectable.
// Same path resolution libpipewire uses (PIPEWIRE_RUNTIME_DIR/PIPEWIRE_REMOTE
// falling back to XDG_RUNTIME_DIR/pipewire-0).
func probePipewire() bool {
	socket := os.Getenv("PIPEWIRE_REMOTE")
	if socket == "" {
		socket = "pipewire-0"
	}
	runtime := os.Getenv("PIPEWIRE_RUNTIME_DIR")
	if runtime == "" {
		runtime = os.Getenv("XDG_RUNTIME_DIR")
	}
	if runtime == "" {
		runtime = "/run/pipewire"
	}
	conn, err := net.DialTimeout("unix", filepath.Join(runtime, socket), 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
