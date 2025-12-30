//go:build gstreamer

package renderergstreamer

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-gst/go-gst/gst"
)

// Driver implements a GStreamer-backed playback driver using Go bindings.
type Driver struct {
	mu        sync.Mutex
	pipeline  string
	device    string
	crossfade time.Duration
	volume    float64
	muted     bool
	current   *gst.Element
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

	return &Driver{pipeline: pipeline, device: device, crossfade: crossfade, volume: 1.0}, nil
}

// Play starts playback for the URL.
func (d *Driver) Play(url string, positionMS int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	volume := d.currentVolumeLocked()
	pipeline, err := d.buildPipeline(url, volume, positionMS)
	if err != nil {
		return err
	}
	if err := d.startPipeline(pipeline); err != nil {
		return err
	}

	if d.current != nil && d.crossfade > 0 {
		old := d.current
		go d.fadeOut(old, d.crossfade)
	}

	d.current = pipeline
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

// Seek seeks within the current pipeline.
func (d *Driver) Seek(positionMS int64) error {
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
	if d.current != nil {
		_ = d.current.SetProperty("volume", d.currentVolumeLocked())
	}
	return nil
}

// SetMute sets mute state.
func (d *Driver) SetMute(mute bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.muted = mute
	if d.current != nil {
		_ = d.current.SetProperty("volume", d.currentVolumeLocked())
	}
	return nil
}

func (d *Driver) buildPipeline(url string, volume float64, positionMS int64) (*gst.Element, error) {
	pipeline := d.pipeline
	pipeline = strings.ReplaceAll(pipeline, "{url}", url)
	pipeline = strings.ReplaceAll(pipeline, "{device}", d.device)
	pipeline = strings.ReplaceAll(pipeline, "{start_ms}", fmt.Sprintf("%d", positionMS))
	pipeline = strings.ReplaceAll(pipeline, "{volume}", fmt.Sprintf("%0.2f", volume))

	el, err := gst.ParseLaunch(pipeline)
	if err != nil {
		return nil, err
	}
	return el, nil
}

func (d *Driver) startPipeline(pipeline *gst.Element) error {
	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		return err
	}
	if d.crossfade > 0 {
		_ = pipeline.SetProperty("volume", 0.0)
		go d.fadeIn(pipeline, d.crossfade)
	}
	return nil
}

func (d *Driver) stopCurrentLocked() error {
	if d.current == nil {
		return nil
	}
	_ = d.current.SetState(gst.StateNull)
	d.current = nil
	return nil
}

func (d *Driver) seekLocked(pipeline *gst.Element, positionMS int64) error {
	positionNS := positionMS * int64(time.Millisecond)
	return pipeline.SeekSimple(gst.FormatTime, gst.SeekFlagFlush|gst.SeekFlagKeyUnit, positionNS)
}

func (d *Driver) fadeIn(pipeline *gst.Element, duration time.Duration) {
	steps := 10
	step := duration / time.Duration(steps)
	for i := 0; i <= steps; i++ {
		volume := (float64(i) / float64(steps)) * d.currentVolumeLocked()
		_ = pipeline.SetProperty("volume", volume)
		time.Sleep(step)
	}
}

func (d *Driver) fadeOut(pipeline *gst.Element, duration time.Duration) {
	steps := 10
	step := duration / time.Duration(steps)
	for i := steps; i >= 0; i-- {
		volume := (float64(i) / float64(steps)) * d.currentVolumeLocked()
		_ = pipeline.SetProperty("volume", volume)
		time.Sleep(step)
	}
	_ = pipeline.SetState(gst.StateNull)
}

func (d *Driver) currentVolumeLocked() float64 {
	if d.muted {
		return 0
	}
	return d.volume
}
