//go:build !gstreamer

package renderergstreamer

import (
	"errors"
	"time"
)

// Driver is a stub when gstreamer tag is not enabled.
type Driver struct{}

// NewDriver returns an error when gstreamer build tag is missing.
func NewDriver(pipeline string, device string, crossfade time.Duration) (*Driver, error) {
	return nil, errors.New("gstreamer build tag not enabled")
}

func (d *Driver) Play(url string, positionMS int64) error {
	return errors.New("gstreamer build tag not enabled")
}
func (d *Driver) Pause() error                { return errors.New("gstreamer build tag not enabled") }
func (d *Driver) Resume() error               { return errors.New("gstreamer build tag not enabled") }
func (d *Driver) Stop() error                 { return errors.New("gstreamer build tag not enabled") }
func (d *Driver) Seek(positionMS int64) error { return errors.New("gstreamer build tag not enabled") }
func (d *Driver) SetVolume(volume float64) error {
	return errors.New("gstreamer build tag not enabled")
}
func (d *Driver) SetMute(mute bool) error { return errors.New("gstreamer build tag not enabled") }
func (d *Driver) Position() (int64, int64, bool) {
	return 0, 0, false
}
