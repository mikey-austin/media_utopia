//go:build gstreamer

package renderergstreamer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-gst/go-gst/gst"
)

// seekDebounceWindow coalesces rapid SeekTo calls (e.g. progress-bar drag)
// into a single flushing seek. Stacking flushing seeks on top of each other
// before the previous flush has settled corrupts the buffer pool inside
// playbin3/pipewiresink and surfaces as GStreamer-CRITICAL
// gst_buffer_resize_range / gst_audio_buffer_map / gst_mini_object_unref
// assertions, eventually crashing the process.
const seekDebounceWindow = 50 * time.Millisecond

// Driver implements a GStreamer-backed playback driver using Go bindings.
type Driver struct {
	mu        sync.RWMutex
	pipeline  string
	device    string
	crossfade time.Duration
	volume    float64
	muted     bool
	current   *gst.Element
	volumeEl  *gst.Element

	// Active fade goroutines (fadeIn for current + fadeOuts in flight). Each
	// fade carries its own context; when a new Play()/Stop()/Close() arrives
	// we cancel *every* outstanding fade so old pipelines flush through
	// teardown promptly rather than holding pipewire client nodes for the
	// remainder of their fade timeline.
	fades   map[*fadeJob]struct{}
	fadesWG sync.WaitGroup

	// Seek coalescing — see seekDebounceWindow. Guarded by mu.
	seekTimer   *time.Timer
	seekPending int64
	seekHas     bool

	// cleanupCh serializes pipeline teardown in a dedicated goroutine.
	// Old pipelines are sent here instead of calling SetState(NULL)
	// inline, so the caller is not blocked by a slow GStreamer state
	// transition. Capacity is generous (16) because fade goroutines send to
	// this channel; if the channel were too small a fadeOut waiting on
	// ctx.Done() would block on send while pinning the pipeline (and its
	// pipewire client node).
	//
	// cleanupStop tells the serial loop to stop consuming so Close can tear
	// the remaining pipelines down *concurrently within a hard budget* — the
	// serial 5s-per-pipeline drain used to push total teardown past docker's
	// 30s stop grace, which got mud SIGKILLed mid-disconnect and left a stale
	// client node that wedged the pipewire daemon.
	cleanupCh   chan *gst.Element
	cleanupStop chan struct{}
	cleanupDone chan struct{}

	// events carries EOS / error / warning / pipewire-down notifications
	// up to the Module so the queue can advance and state can be reported.
	// Buffered so a slow consumer doesn't stall bus-watch goroutines.
	events chan Event

	// watchersWG tracks bus-watch goroutines (one per pipeline) and the
	// healthcheck loop. Close() waits on it before closing cleanupCh so
	// nothing tries to push a stale pipeline through after shutdown.
	watchersWG sync.WaitGroup

	// busCancels holds the cancel function for the bus-watch goroutine
	// attached to each live pipeline. The cleanup loop signals the
	// watcher to exit BEFORE calling teardownPipeline — otherwise the
	// watcher would spin reading nil from the flushed bus until Close.
	busCancels map[*gst.Element]context.CancelFunc

	// healthCancel stops the pipewire healthcheck loop. Set during
	// NewDriver, called from Close.
	healthCancel context.CancelFunc

	// closed is set during Close. Subsequent Play/Stop/etc. return an error
	// rather than racing against teardown.
	closeOnce sync.Once
	closed    bool
}

// fadeJob holds the cancel handle for one in-flight fade goroutine. The
// Driver tracks the set of live jobs so a new Play/Stop can cancel all of
// them, not just the most-recent one.
type fadeJob struct {
	cancel context.CancelFunc
}

var gstInitOnce sync.Once

// NewDriver creates a GStreamer driver using a pipeline template.
func NewDriver(pipeline string, device string, crossfade time.Duration) (*Driver, error) {
	if strings.TrimSpace(pipeline) == "" {
		return nil, errors.New("pipeline template required")
	}
	gstInitOnce.Do(func() {
		gst.Init(nil)
	})

	healthCtx, healthCancel := context.WithCancel(context.Background())
	d := &Driver{
		pipeline:     pipeline,
		device:       device,
		crossfade:    crossfade,
		volume:       1.0,
		fades:        make(map[*fadeJob]struct{}),
		busCancels:   make(map[*gst.Element]context.CancelFunc),
		cleanupCh:    make(chan *gst.Element, 16),
		cleanupStop:  make(chan struct{}),
		cleanupDone:  make(chan struct{}),
		events:       make(chan Event, 16),
		healthCancel: healthCancel,
	}
	go d.pipelineCleanupLoop()
	d.watchersWG.Add(1)
	go d.healthcheckLoop(healthCtx)
	return d, nil
}

// Events returns a channel of asynchronous notifications. The caller is
// expected to drain this channel; events may be dropped (with a warning to
// stderr) if the consumer falls behind.
func (d *Driver) Events() <-chan Event {
	return d.events
}

// emit publishes an event, dropping it on the floor if the channel is full
// rather than blocking the producer (a stuck consumer must not deadlock the
// bus-watch goroutine that owns the pipeline reference).
func (d *Driver) emit(ev Event) {
	select {
	case d.events <- ev:
	default:
		fmt.Fprintf(os.Stderr, "gstreamer: dropped event kind=%d msg=%q (consumer slow)\n", ev.Kind, ev.Message)
	}
}

// pipelineCleanupLoop processes old pipelines serially, ensuring each is
// fully transitioned to NULL (releasing PipeWire FDs) before the next.
// It runs until cleanupCh is closed by Close().
func (d *Driver) pipelineCleanupLoop() {
	defer close(d.cleanupDone)
	for {
		var el *gst.Element
		select {
		case <-d.cleanupStop:
			// Close is taking over: it drains whatever is still queued and
			// tears it down concurrently within a bounded budget.
			return
		case el = <-d.cleanupCh:
		}
		// Stop the bus watcher first — otherwise it would spin polling
		// the flushed bus until Close, holding a reference to the
		// element. Done under the lock so a concurrent Close sees a
		// consistent busCancels map.
		d.mu.Lock()
		cancel := d.busCancels[el]
		delete(d.busCancels, el)
		d.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		start := time.Now()
		err := teardownPipeline(el)
		if dur := time.Since(start); dur > 500*time.Millisecond {
			// Slow teardown suggests PipeWire pressure or network
			// stream buffering — log for diagnostics.
			fmt.Fprintf(os.Stderr, "gstreamer: slow pipeline teardown: %s err=%v\n", dur, err)
		}
	}
}

// startFadeLocked registers a new fade job and returns its context. Caller
// must hold d.mu.
func (d *Driver) startFadeLocked() (context.Context, *fadeJob) {
	ctx, cancel := context.WithCancel(context.Background())
	job := &fadeJob{cancel: cancel}
	d.fades[job] = struct{}{}
	d.fadesWG.Add(1)
	return ctx, job
}

// finishFade removes a fade job from the live set.
func (d *Driver) finishFade(job *fadeJob) {
	d.mu.Lock()
	delete(d.fades, job)
	d.mu.Unlock()
	job.cancel() // release ctx resources
	d.fadesWG.Done()
}

// cancelAllFadesLocked signals every in-flight fade to wind up. Caller must
// hold d.mu. Goroutines remove themselves from d.fades via finishFade once
// they observe the cancellation.
func (d *Driver) cancelAllFadesLocked() {
	for job := range d.fades {
		job.cancel()
	}
}

// busWatch reads GStreamer bus messages for one pipeline. EOS / ERROR /
// WARNING are surfaced as Events; other message types are ignored. The
// goroutine exits when ctx is cancelled — the cleanup loop cancels just
// before teardownPipeline so the watcher doesn't spin reading nil from a
// flushed bus.
func (d *Driver) busWatch(ctx context.Context, bus *gst.Bus) {
	defer d.watchersWG.Done()
	if bus == nil {
		return
	}
	const pollTimeout = gst.ClockTime(200 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		msg := bus.TimedPopFiltered(pollTimeout,
			gst.MessageEOS|gst.MessageError|gst.MessageWarning)
		if msg == nil {
			continue
		}
		// NOTE: do NOT call msg.Unref() here. go-gst's TimedPopFiltered
		// wraps the C message via FromGstMessageUnsafeFull, which installs
		// a finalizer that unrefs on GC. An explicit Unref would
		// double-unref the underlying GstMessage and trip a GLib CRITICAL
		// — under G_DEBUG=fatal-criticals that aborts the whole process.
		switch msg.Type() {
		case gst.MessageEOS:
			d.emit(Event{Kind: EventEOS})
			return // pipeline is finished, stop watching
		case gst.MessageError:
			gerr := msg.ParseError()
			text := "pipeline error"
			if gerr != nil {
				text = gerr.Error()
			}
			d.emit(Event{Kind: EventError, Message: text})
			return // pipeline will be torn down by the consumer
		case gst.MessageWarning:
			gerr := msg.ParseWarning()
			text := "pipeline warning"
			if gerr != nil {
				text = gerr.Error()
			}
			d.emit(Event{Kind: EventWarning, Message: text})
		}
	}
}

// healthcheckLoop probes the pipewire socket every healthcheckInterval. After
// healthcheckFailureThreshold consecutive failures it tears down the current
// pipeline (so the next Play rebuilds against a fresh pipewire) and emits
// EventPipewireDown. Recovery is automatic — once probes succeed again the
// next Play call from the Module proceeds normally.
func (d *Driver) healthcheckLoop(ctx context.Context) {
	defer d.watchersWG.Done()
	ticker := time.NewTicker(healthcheckInterval)
	defer ticker.Stop()

	failures := 0
	wasDown := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if d.probePipewire() {
			failures = 0
			wasDown = false
			continue
		}
		failures++
		if failures < healthcheckFailureThreshold {
			continue
		}
		if wasDown {
			// Already torn down; keep probing quietly.
			continue
		}
		wasDown = true
		d.mu.Lock()
		closed := d.closed
		current := d.current
		d.current = nil
		d.volumeEl = nil
		d.cancelAllFadesLocked()
		d.cancelPendingSeekLocked()
		d.mu.Unlock()
		if closed {
			return
		}
		if current != nil {
			// Send to cleanup loop — even though pipewire is gone,
			// SetState(Null) is what releases the in-process GStreamer
			// resources, and the loop's serialisation prevents us
			// hammering a flapping pipewire daemon.
			d.cleanupCh <- current
		}
		d.emit(Event{Kind: EventPipewireDown, Message: "pipewire socket unreachable"})
	}
}

// probePipewire returns true if the pipewire control socket is connectable.
// We dial the same path libpipewire uses (PIPEWIRE_RUNTIME_DIR/PIPEWIRE_REMOTE
// falling back to XDG_RUNTIME_DIR/pipewire-0) — a successful AF_UNIX connect
// means the daemon is alive and accepting clients.
func (d *Driver) probePipewire() bool {
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
	path := filepath.Join(runtime, socket)
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

const (
	healthcheckInterval         = 30 * time.Second
	healthcheckFailureThreshold = 2 // ~60s of unhealth before we react

	// teardownConfirmTimeout bounds how long a single teardown waits for the
	// NULL state to confirm. SetState(NULL) posts the client-node disconnect to
	// pipewire immediately; GetState only waits for the daemon to acknowledge,
	// so when pipewire is slow we cap the wait rather than letting each
	// pipeline cost a full 5s.
	teardownConfirmTimeout = 3 * time.Second

	// closeTeardownBudget is the hard ceiling on Driver.Close's concurrent
	// teardown of all outstanding pipelines. It MUST stay comfortably under the
	// mud container's docker stop grace (30s) so we always exit cleanly instead
	// of being SIGKILLed mid-disconnect — a SIGKILL abandons the pipewiresink
	// client node and is what wedges the pipewire daemon over time.
	closeTeardownBudget = 8 * time.Second
)

func teardownPipeline(el *gst.Element) error {
	if el == nil {
		return nil
	}
	bus := el.GetBus()
	if bus != nil {
		// Drop queued messages before teardown; stale bus messages otherwise
		// keep references to elements/pads longer than necessary.
		bus.SetFlushing(true)
	}
	if err := el.SetState(gst.StateNull); err != nil {
		return err
	}
	ret, state := el.GetState(gst.StateNull, gst.ClockTime(teardownConfirmTimeout))
	if ret == gst.StateChangeFailure {
		return fmt.Errorf("state change to NULL failed (state=%s)", state)
	}
	if state != gst.StateNull {
		return fmt.Errorf("timed out waiting for NULL (ret=%s state=%s)", ret, state)
	}
	return nil
}

// teardownConcurrent posts SetState(NULL) to every pipeline at once, then waits
// up to budget for them to confirm NULL. Posting the disconnect to all sinks
// immediately (rather than serially) means a slow or hung pipewire can never
// stretch shutdown past the budget: the daemon receives every client-node
// disconnect request right away, and the GetState confirmation is best-effort.
// This is the shutdown counterpart to the serial cleanup loop used at runtime.
func teardownConcurrent(pipelines []*gst.Element, budget time.Duration) {
	var wg sync.WaitGroup
	for _, el := range pipelines {
		if el == nil {
			continue
		}
		if bus := el.GetBus(); bus != nil {
			bus.SetFlushing(true)
		}
		_ = el.SetState(gst.StateNull) // post the disconnect immediately
		wg.Add(1)
		go func(el *gst.Element) {
			defer wg.Done()
			el.GetState(gst.StateNull, gst.ClockTime(budget))
		}(el)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(budget):
	}
}

// Play starts playback for the URL.
func (d *Driver) Play(url string, positionMS int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return errors.New("driver closed")
	}

	// Cancel any running fade goroutines and pending seek from previous playback
	d.cancelAllFadesLocked()
	d.cancelPendingSeekLocked()

	volume := d.currentVolumeLocked()
	pipeline, volumeEl, err := d.buildPipeline(url, volume, positionMS)
	if err != nil {
		return err
	}
	if err := d.startPipeline(pipeline, volumeEl); err != nil {
		_ = teardownPipeline(pipeline)
		return err
	}

	if d.current != nil {
		if d.crossfade > 0 && !d.muted {
			// Crossfade: fade out old pipeline in background
			old := d.current
			oldVol := d.volumeEl
			targetVolume := d.currentVolumeLocked()
			ctx, job := d.startFadeLocked()
			go d.fadeOut(ctx, job, old, oldVol, d.crossfade, targetVolume)
		} else {
			// No crossfade: hand old pipeline to the cleanup goroutine.
			d.cleanupCh <- d.current
		}
	}

	d.current = pipeline
	d.volumeEl = volumeEl
	return nil
}

// Pause pauses playback.
func (d *Driver) Pause() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.current == nil {
		return errors.New("not playing")
	}
	return d.current.SetState(gst.StatePaused)
}

// Resume resumes playback.
func (d *Driver) Resume() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.current == nil {
		return errors.New("not playing")
	}
	return d.current.SetState(gst.StatePlaying)
}

// Stop stops playback.
func (d *Driver) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil
	}
	return d.stopCurrentLocked()
}

// Close shuts the driver down: in-flight fades are cancelled and awaited, the
// healthcheck and bus watchers are stopped, and every outstanding pipeline
// (the survivor plus anything still queued for cleanup) is torn down to NULL
// concurrently within closeTeardownBudget. Posting SetState(Null) to every
// pipewiresink is what gives pipewire a clean client-disconnect rather than a
// socket EOF; the latter leaves stale node/buffer state in the daemon that
// accumulates across restarts and eventually wedges it. The bounded,
// concurrent teardown guarantees Close returns well inside docker's stop
// grace so the process is never SIGKILLed mid-disconnect.
//
// After Close returns, further Play/Stop/etc. calls return an error. Close
// is idempotent.
func (d *Driver) Close() error {
	var err error
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.cancelAllFadesLocked()
		d.cancelPendingSeekLocked()
		survivor := d.current
		d.current = nil
		d.volumeEl = nil
		// Cancel every live bus watcher up front so none spin on a flushed
		// bus while we tear pipelines down. Includes the survivor's.
		for el, cancel := range d.busCancels {
			cancel()
			delete(d.busCancels, el)
		}
		d.mu.Unlock()

		// Stop the healthcheck loop.
		if d.healthCancel != nil {
			d.healthCancel()
		}

		// Wait for cancelled fades to push their pipelines into cleanupCh
		// and exit. After this returns, nothing else will send on
		// cleanupCh from fade goroutines.
		d.fadesWG.Wait()

		// Take ownership of teardown from the serial cleanup loop. Once it
		// returns, we drain anything still queued and tear the whole set —
		// survivor + queued — down concurrently within one bounded budget,
		// so a slow/hung pipewire can't drag shutdown past docker's stop
		// grace (which would SIGKILL us mid-disconnect).
		close(d.cleanupStop)
		<-d.cleanupDone

		pipelines := make([]*gst.Element, 0, cap(d.cleanupCh)+1)
		if survivor != nil {
			pipelines = append(pipelines, survivor)
		}
		for drained := false; !drained; {
			select {
			case el := <-d.cleanupCh:
				if el != nil {
					pipelines = append(pipelines, el)
				}
			default:
				drained = true
			}
		}
		teardownConcurrent(pipelines, closeTeardownBudget)

		// All watchers + healthcheck should be exiting now. Wait for them
		// before closing the events channel so no goroutine sends on a
		// closed chan.
		d.watchersWG.Wait()
		close(d.events)
	})
	// Wait for cleanup loop to finish — idempotent reentries also block
	// until shutdown is fully complete.
	<-d.cleanupDone
	return err
}

// SeekTo seeks within the current pipeline. Rapid calls within
// seekDebounceWindow are coalesced — only the latest target is executed.
func (d *Driver) SeekTo(positionMS int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.current == nil {
		return errors.New("not playing")
	}
	d.seekPending = positionMS
	d.seekHas = true
	if d.seekTimer != nil {
		d.seekTimer.Stop()
	}
	d.seekTimer = time.AfterFunc(seekDebounceWindow, d.flushPendingSeek)
	return nil
}

// flushPendingSeek issues the most recent pending seek, if any. Runs from the
// debounce timer goroutine.
func (d *Driver) flushPendingSeek() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.seekHas || d.current == nil {
		return
	}
	pos := d.seekPending
	d.seekHas = false
	_ = d.seekLocked(d.current, pos)
}

// cancelPendingSeekLocked drops any queued seek. Caller must hold d.mu.
func (d *Driver) cancelPendingSeekLocked() {
	if d.seekTimer != nil {
		d.seekTimer.Stop()
	}
	d.seekHas = false
}

// SetVolume sets volume (0..1).
func (d *Driver) SetVolume(volume float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.volume = volume
	if d.current != nil && !d.muted {
		target := d.volumeTarget()
		if target != nil {
			_ = target.SetProperty("volume", d.volume)
		}
	}

	return nil
}

// SetMute sets mute state using both GStreamer's mute property and volume.
// The mute property preserves audio clock synchronization.
func (d *Driver) SetMute(mute bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.muted = mute
	if d.current != nil {
		// Use volumeEl which points to the actual playbin
		target := d.volumeTarget()
		if target != nil {
			// Disable for now as dmix alsa sinks screw with playback
			//_ = target.SetProperty("mute", mute)
			if mute {
				// Set to -100dB, completely inaudible in practice, while
				// still keeping the pipeline in sync.
				_ = target.SetProperty("volume", 0.00001)
			} else {
				_ = target.SetProperty("volume", d.volume)
			}
		}
	}
	return nil
}

// Position returns current position/duration in ms when available.
func (d *Driver) Position() (int64, int64, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.current == nil {
		return 0, 0, false
	}
	return d.queryPositionLocked()
}

func (d *Driver) buildPipeline(url string, volume float64, positionMS int64) (*gst.Element, *gst.Element, error) {
	pipeline := d.pipeline
	pipeline = replaceURL(pipeline, url)
	pipeline = strings.ReplaceAll(pipeline, "{device}", d.device)
	pipeline = strings.ReplaceAll(pipeline, "{start_ms}", fmt.Sprintf("%d", positionMS))
	pipeline = strings.ReplaceAll(pipeline, "{volume}", fmt.Sprintf("%0.2f", volume))

	if strings.Contains(pipeline, "!") {
		el, err := gst.NewPipelineFromString(pipeline)
		if err != nil {
			return nil, nil, err
		}
		return el.Element, el.Element, nil
	}
	bin, err := gst.NewBinFromString(pipeline, false)
	if err != nil {
		return nil, nil, err
	}
	if elems, err := bin.GetElements(); err == nil && len(elems) == 1 {
		return bin.Element, elems[0], nil
	}
	return bin.Element, bin.Element, nil
}

func (d *Driver) startPipeline(pipeline *gst.Element, volumeEl *gst.Element) error {
	target := volumeEl
	if target == nil {
		target = pipeline
	}

	// When crossfade is enabled, zero the volume BEFORE starting playback
	// to avoid an audible pop/glitch at the start of the crossfade.
	if d.crossfade > 0 && !d.muted {
		_ = target.SetProperty("volume", 0.0)
	}

	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		return err
	}

	// Spawn the bus watcher BEFORE returning so we catch the very first
	// ERROR (e.g. pipewiresink failing to connect) without waiting for
	// the next operation. The cleanup loop cancels this ctx before
	// teardownPipeline so the watcher exits promptly.
	if bus := pipeline.GetBus(); bus != nil {
		ctx, cancel := context.WithCancel(context.Background())
		d.busCancels[pipeline] = cancel
		d.watchersWG.Add(1)
		go d.busWatch(ctx, bus)
	}

	if d.muted {
		// Disable the mute property as it screws with some alsa sink
		// setups and causes playback to skip.
		//_ = target.SetProperty("mute", d.muted)
		_ = target.SetProperty("volume", 0.00001)
	} else if d.crossfade > 0 {
		// Volume already zeroed above; start fade-in
		targetVolume := d.currentVolumeLocked()
		ctx, job := d.startFadeLocked()
		go d.fadeIn(ctx, job, pipeline, target, d.crossfade, targetVolume)
	} else {
		_ = target.SetProperty("volume", d.volume)
	}

	return nil
}

func (d *Driver) stopCurrentLocked() error {
	if d.current == nil {
		return nil
	}
	d.cancelAllFadesLocked()
	d.cancelPendingSeekLocked()
	// Detach the pipeline and hand it to the cleanup goroutine.  The
	// caller returns immediately; the cleanup loop ensures SetState(NULL)
	// runs to completion so PipeWire FDs are released.  Channel capacity
	// is sized large enough that a burst of stops/skips never blocks the
	// sender (and therefore never pins a pipeline in a goroutine).
	d.cleanupCh <- d.current
	d.current = nil
	d.volumeEl = nil
	return nil
}

func (d *Driver) seekLocked(pipeline *gst.Element, positionMS int64) error {
	positionNS := positionMS * int64(time.Millisecond)
	if ok := pipeline.SeekSimple(positionNS, gst.FormatTime, gst.SeekFlagFlush|gst.SeekFlagKeyUnit); !ok {
		return errors.New("seek failed")
	}
	return nil
}

func (d *Driver) fadeIn(ctx context.Context, job *fadeJob, pipeline *gst.Element, target *gst.Element, duration time.Duration, targetVolume float64) {
	defer d.finishFade(job)

	steps := 10
	ticker := time.NewTicker(duration / time.Duration(steps))
	defer ticker.Stop()

	for i := 0; i <= steps; i++ {
		select {
		case <-ctx.Done():
			// Fade cancelled, set final volume immediately
			if target != nil {
				_ = target.SetProperty("volume", targetVolume)
			}
			return
		default:
		}

		volume := (float64(i) / float64(steps)) * targetVolume
		if target != nil {
			_ = target.SetProperty("volume", volume)
		}

		if i < steps {
			select {
			case <-ctx.Done():
				if target != nil {
					_ = target.SetProperty("volume", targetVolume)
				}
				return
			case <-ticker.C:
			}
		}
	}
}

func (d *Driver) fadeOut(ctx context.Context, job *fadeJob, pipeline *gst.Element, target *gst.Element, duration time.Duration, targetVolume float64) {
	defer d.finishFade(job)

	steps := 10
	ticker := time.NewTicker(duration / time.Duration(steps))
	defer ticker.Stop()

	for i := steps; i >= 0; i-- {
		select {
		case <-ctx.Done():
			// Fade cancelled — hand to cleanup goroutine (never inline
			// SetState(NULL) here to avoid blocking/leaking the fade goroutine).
			d.cleanupCh <- pipeline
			return
		default:
		}

		volume := (float64(i) / float64(steps)) * targetVolume
		if target != nil {
			_ = target.SetProperty("volume", volume)
		}

		if i > 0 {
			select {
			case <-ctx.Done():
				d.cleanupCh <- pipeline
				return
			case <-ticker.C:
			}
		}
	}
	// Fade completed normally — hand to cleanup goroutine.
	d.cleanupCh <- pipeline
}

func (d *Driver) currentVolumeLocked() float64 {
	return d.volume
}

func (d *Driver) volumeTarget() *gst.Element {
	if d.volumeEl != nil {
		return d.volumeEl
	}
	return d.current
}

func (d *Driver) queryPositionLocked() (int64, int64, bool) {
	posOK, pos := d.current.QueryPosition(gst.FormatTime)
	durOK, dur := d.current.QueryDuration(gst.FormatTime)
	if !posOK && d.volumeEl != nil {
		posOK, pos = d.volumeEl.QueryPosition(gst.FormatTime)
	}
	if !durOK && d.volumeEl != nil {
		durOK, dur = d.volumeEl.QueryDuration(gst.FormatTime)
	}
	if !posOK && !durOK {
		return 0, 0, false
	}
	return pos / int64(time.Millisecond), dur / int64(time.Millisecond), true
}

func replaceURL(pipeline string, url string) string {
	quoted := quotePipelineValue(url)
	needle := []string{
		"uri={url}",
		"uri='{url}'",
		`uri="{url}"`,
	}
	for _, item := range needle {
		pipeline = strings.ReplaceAll(pipeline, item, "uri="+quoted)
	}
	return strings.ReplaceAll(pipeline, "{url}", url)
}

func quotePipelineValue(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
