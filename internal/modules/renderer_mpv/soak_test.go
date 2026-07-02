//go:build mpv && integration

package renderermpv

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTestWAV writes a mono 16-bit 8kHz PCM wav (silence) of the given
// duration.
func writeTestWAV(t *testing.T, path string, dur time.Duration) {
	t.Helper()
	const rate = 8000
	n := int(float64(rate) * dur.Seconds())
	data := make([]byte, 44+2*n)
	copy(data[0:], "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(36+2*n))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1) // PCM
	binary.LittleEndian.PutUint16(data[22:], 1) // mono
	binary.LittleEndian.PutUint32(data[24:], rate)
	binary.LittleEndian.PutUint32(data[28:], rate*2)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(2*n))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write wav: %v", err)
	}
}

// newNullDriver builds a driver on the null AO so no audio stack is needed.
func newNullDriver(t *testing.T, crossfade time.Duration) *Driver {
	t.Helper()
	d, err := NewDriver("null", "", crossfade, nil)
	if err != nil {
		t.Fatalf("NewDriver: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func waitEvent(t *testing.T, d *Driver, kind EventKind, timeout time.Duration) Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-d.Events():
			if ev.Kind == kind {
				return ev
			}
			t.Logf("event kind=%d msg=%s", ev.Kind, ev.Message)
		case <-deadline:
			t.Fatalf("timed out waiting for event kind=%d", kind)
		}
	}
}

func TestPlayLocalFileToEOS(t *testing.T) {
	wav := filepath.Join(t.TempDir(), "tone.wav")
	writeTestWAV(t, wav, 2*time.Second)
	d := newNullDriver(t, 0)
	if err := d.Play(wav, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitEvent(t, d, EventEOS, 15*time.Second)
}

func TestPositionSeekPauseResume(t *testing.T) {
	wav := filepath.Join(t.TempDir(), "tone.wav")
	writeTestWAV(t, wav, 30*time.Second)
	d := newNullDriver(t, 0)
	if err := d.Play(wav, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, dur, ok := d.Position(); ok && dur > 25000 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("position/duration never observed")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := d.SeekTo(20000); err != nil {
		t.Fatalf("SeekTo: %v", err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		if pos, _, ok := d.Position(); ok && pos >= 19000 {
			break
		}
		if time.Now().After(deadline) {
			pos, _, _ := d.Position()
			t.Fatalf("seek not reflected, pos=%d", pos)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := d.Pause(); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if err := d.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := d.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestVolumeAndMute(t *testing.T) {
	wav := filepath.Join(t.TempDir(), "tone.wav")
	writeTestWAV(t, wav, 10*time.Second)
	d := newNullDriver(t, 0)
	if err := d.Play(wav, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if err := d.SetVolume(0.5); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	if err := d.SetMute(true); err != nil {
		t.Fatalf("SetMute: %v", err)
	}
	if err := d.SetMute(false); err != nil {
		t.Fatalf("SetMute off: %v", err)
	}
}

func TestPlayWithStartPosition(t *testing.T) {
	wav := filepath.Join(t.TempDir(), "tone.wav")
	writeTestWAV(t, wav, 30*time.Second)
	d := newNullDriver(t, 0)
	if err := d.Play(wav, 25000); err != nil {
		t.Fatalf("Play: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if pos, _, ok := d.Position(); ok && pos >= 24000 {
			return
		}
		if time.Now().After(deadline) {
			pos, _, ok := d.Position()
			t.Fatalf("start position not honoured, pos=%d ok=%v", pos, ok)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestCrossfadePlaySwitch(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.wav")
	b := filepath.Join(dir, "b.wav")
	writeTestWAV(t, a, 30*time.Second)
	writeTestWAV(t, b, 5*time.Second)
	d := newNullDriver(t, 1*time.Second)
	if err := d.Play(a, 0); err != nil {
		t.Fatalf("Play a: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if err := d.Play(b, 0); err != nil {
		t.Fatalf("Play b (crossfade): %v", err)
	}
	// b (5s) should reach EOS; a's fade-out handle must not emit events
	// after it stops being current.
	waitEvent(t, d, EventEOS, 20*time.Second)
}

func TestCloseIsBoundedAndIdempotent(t *testing.T) {
	wav := filepath.Join(t.TempDir(), "tone.wav")
	writeTestWAV(t, wav, 30*time.Second)
	d := newNullDriver(t, 2*time.Second)
	if err := d.Play(wav, 0); err != nil {
		t.Fatalf("Play: %v", err)
	}
	start := time.Now()
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close again: %v", err)
	}
	if elapsed := time.Since(start); elapsed > closeBudget+2*time.Second {
		t.Fatalf("Close took %s, budget is %s", elapsed, closeBudget)
	}
	if err := d.Play(wav, 0); err == nil {
		t.Fatal("Play after Close must fail")
	}
}

// TestStreamSoak exercises the real station/stream list — the acceptance
// gate for stream compatibility. Opt-in:
//
//	MU_SOAK_STREAMS="http://...,http://..." make integration-mpv
func TestStreamSoak(t *testing.T) {
	raw := os.Getenv("MU_SOAK_STREAMS")
	if raw == "" {
		t.Skip("MU_SOAK_STREAMS not set")
	}
	for _, url := range strings.Split(raw, ",") {
		url := strings.TrimSpace(url)
		if url == "" {
			continue
		}
		t.Run(url, func(t *testing.T) {
			d := newNullDriver(t, 0)
			if err := d.Play(url, 0); err != nil {
				t.Fatalf("Play: %v", err)
			}
			// A live stream never EOSes; assert playback position advances
			// and no error event arrives within the probe window.
			deadline := time.After(30 * time.Second)
			for {
				select {
				case ev := <-d.Events():
					if ev.Kind == EventError {
						t.Fatalf("playback error: %s", ev.Message)
					}
					t.Logf("event kind=%d msg=%s", ev.Kind, ev.Message)
				case <-time.After(500 * time.Millisecond):
					if pos, _, ok := d.Position(); ok && pos > 2000 {
						return // played >2s of stream — success
					}
				case <-deadline:
					t.Fatal("stream never reached 2s of playback")
				}
			}
		})
	}
}
