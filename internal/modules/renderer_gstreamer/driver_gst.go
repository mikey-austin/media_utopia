//go:build gstreamer

package renderergstreamer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-gst/go-gst/gst"
)

// Driver implements a GStreamer-backed playback driver using Go bindings.
type Driver struct {
	mu         sync.RWMutex
	pipeline   string
	device     string
	crossfade  time.Duration
	volume     float64
	muted      bool
	current    *gst.Element
	volumeEl   *gst.Element
	ctx        context.Context
	cancelFade context.CancelFunc // cancels any running fade goroutines

	// cleanupCh serializes pipeline teardown in a dedicated goroutine.
	// Old pipelines are sent here instead of calling SetState(NULL)
	// inline, so the caller is not blocked by a slow GStreamer state
	// transition.  The bounded buffer (2) provides backpressure: if
	// teardown is slow, new Play() calls block until old pipelines are
	// cleaned up, preventing unbounded FD accumulation.
	cleanupCh chan *gst.Element
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

	d := &Driver{
		pipeline:  pipeline,
		device:    device,
		crossfade: crossfade,
		volume:    1.0,
		cleanupCh: make(chan *gst.Element, 2),
	}
	go d.pipelineCleanupLoop()
	return d, nil
}

// pipelineCleanupLoop processes old pipelines serially, ensuring each is
// fully transitioned to NULL (releasing PipeWire FDs) before the next.
// It runs for the lifetime of the Driver.
func (d *Driver) pipelineCleanupLoop() {
	for el := range d.cleanupCh {
		start := time.Now()
		err := teardownPipeline(el)
		if dur := time.Since(start); dur > 500*time.Millisecond {
			// Slow teardown suggests PipeWire pressure or network
			// stream buffering — log for diagnostics.
			fmt.Fprintf(os.Stderr, "gstreamer: slow pipeline teardown: %s err=%v\n", dur, err)
		}
	}
}

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
	ret, state := el.GetState(gst.StateNull, gst.ClockTime(5*time.Second))
	if ret == gst.StateChangeFailure {
		return fmt.Errorf("state change to NULL failed (state=%s)", state)
	}
	if state != gst.StateNull {
		return fmt.Errorf("timed out waiting for NULL (ret=%s state=%s)", ret, state)
	}
	return nil
}

// Play starts playback for the URL.
func (d *Driver) Play(url string, positionMS int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Cancel any running fade goroutines from previous playback
	if d.cancelFade != nil {
		d.cancelFade()
	}
	d.ctx, d.cancelFade = context.WithCancel(context.Background())

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
			go d.fadeOut(d.ctx, old, oldVol, d.crossfade, targetVolume)
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

	return d.stopCurrentLocked()
}

// SeekTo seeks within the current pipeline.
func (d *Driver) SeekTo(positionMS int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.current == nil {
		return errors.New("not playing")
	}
	return d.seekLocked(d.current, positionMS)
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

	if d.muted {
		// Disable the mute property as it screws with some alsa sink
		// setups and causes playback to skip.
		//_ = target.SetProperty("mute", d.muted)
		_ = target.SetProperty("volume", 0.00001)
	} else if d.crossfade > 0 {
		// Volume already zeroed above; start fade-in
		targetVolume := d.currentVolumeLocked()
		go d.fadeIn(d.ctx, pipeline, target, d.crossfade, targetVolume)
	} else {
		_ = target.SetProperty("volume", d.volume)
	}

	return nil
}

func (d *Driver) stopCurrentLocked() error {
	if d.current == nil {
		return nil
	}
	if d.cancelFade != nil {
		d.cancelFade()
	}
	// Detach the pipeline and hand it to the cleanup goroutine.  The
	// caller returns immediately; the cleanup loop ensures SetState(NULL)
	// runs to completion so PipeWire FDs are released.  The bounded
	// channel provides backpressure if teardown is slow.
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

func (d *Driver) fadeIn(ctx context.Context, pipeline *gst.Element, target *gst.Element, duration time.Duration, targetVolume float64) {
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

func (d *Driver) fadeOut(ctx context.Context, pipeline *gst.Element, target *gst.Element, duration time.Duration, targetVolume float64) {
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
