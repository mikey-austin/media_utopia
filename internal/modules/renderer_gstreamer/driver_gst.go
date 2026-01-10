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
	volumeEl  *gst.Element
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
	pipeline, volumeEl, err := d.buildPipeline(url, volume, positionMS)
	if err != nil {
		return err
	}
	if err := d.startPipeline(pipeline, volumeEl); err != nil {
		return err
	}

	if d.current != nil && d.crossfade > 0 {
		old := d.current
		oldVol := d.volumeEl
		go d.fadeOut(old, oldVol, d.crossfade)
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
		target := d.volumeTarget()
		if target != nil {
			_ = target.SetProperty("volume", d.currentVolumeLocked())
		}
	}
	return nil
}

// SetMute sets mute state using both GStreamer's mute property and volume.
// The mute property preserves audio clock synchronization, while setting
// volume to 0 ensures the audio is actually silenced.
func (d *Driver) SetMute(mute bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.muted = mute
	if d.current != nil {
		// Set mute property for clock sync preservation.
		_ = d.current.SetProperty("mute", mute)
		// Also set volume to ensure audio is actually silenced.
		target := d.volumeTarget()
		if target != nil {
			_ = target.SetProperty("volume", d.currentVolumeLocked())
		}
	}
	return nil
}

// Position returns current position/duration in ms when available.
func (d *Driver) Position() (int64, int64, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

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
	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		return err
	}
	// Apply current mute state to new pipeline.
	if d.muted {
		_ = pipeline.SetProperty("mute", true)
	}
	if d.crossfade > 0 {
		_ = target.SetProperty("volume", 0.0)
		go d.fadeIn(pipeline, target, d.crossfade)
	}
	return nil
}

func (d *Driver) stopCurrentLocked() error {
	if d.current == nil {
		return nil
	}
	_ = d.current.SetState(gst.StateNull)
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

func (d *Driver) fadeIn(pipeline *gst.Element, target *gst.Element, duration time.Duration) {
	steps := 10
	step := duration / time.Duration(steps)
	for i := 0; i <= steps; i++ {
		volume := (float64(i) / float64(steps)) * d.currentVolumeLocked()
		if target != nil {
			_ = target.SetProperty("volume", volume)
		}
		time.Sleep(step)
	}
}

func (d *Driver) fadeOut(pipeline *gst.Element, target *gst.Element, duration time.Duration) {
	steps := 10
	step := duration / time.Duration(steps)
	for i := steps; i >= 0; i-- {
		volume := (float64(i) / float64(steps)) * d.currentVolumeLocked()
		if target != nil {
			_ = target.SetProperty("volume", volume)
		}
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
