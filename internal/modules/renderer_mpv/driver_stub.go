//go:build !mpv

package renderermpv

import (
	"errors"
	"time"
)

// Driver is a stub when the mpv build tag is not enabled.
type Driver struct{}

var errNoMPV = errors.New("mpv build tag not enabled")

// NewDriver returns an error when the mpv build tag is missing.
func NewDriver(ao string, device string, crossfade time.Duration, mpvOptions map[string]string) (*Driver, error) {
	return nil, errNoMPV
}

func (d *Driver) Play(url string, positionMS int64) error { return errNoMPV }
func (d *Driver) Pause() error                            { return errNoMPV }
func (d *Driver) Resume() error                           { return errNoMPV }
func (d *Driver) Stop() error                             { return errNoMPV }
func (d *Driver) SeekTo(positionMS int64) error           { return errNoMPV }
func (d *Driver) SetVolume(volume float64) error          { return errNoMPV }
func (d *Driver) SetMute(mute bool) error                 { return errNoMPV }
func (d *Driver) Position() (int64, int64, bool)          { return 0, 0, false }
