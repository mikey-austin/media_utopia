//go:build chromaprint

package chromaprint

// #cgo pkg-config: libchromaprint
// #include <chromaprint.h>
// #include <stdlib.h>
import "C"

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"unsafe"
)

// Enabled indicates chromaprint CGo bindings are compiled in.
const Enabled = true

var (
	// ErrFFmpegNotFound is returned when ffmpeg is not on PATH.
	ErrFFmpegNotFound = errors.New("ffmpeg not found on PATH")

	ffmpegOnce sync.Once
	ffmpegPath string
	ffmpegErr  error
)

func resolveFFmpeg() {
	ffmpegOnce.Do(func() {
		ffmpegPath, ffmpegErr = exec.LookPath("ffmpeg")
		if ffmpegErr != nil {
			ffmpegErr = ErrFFmpegNotFound
		}
	})
}

// FingerprintFile decodes the first 120 seconds of the audio file at path
// to raw PCM via ffmpeg, then computes a Chromaprint fingerprint.
// Returns the fingerprint string and the duration in seconds.
func FingerprintFile(path string) (fingerprint string, durationSec int, err error) {
	resolveFFmpeg()
	if ffmpegErr != nil {
		return "", 0, ffmpegErr
	}

	// Decode first 120s to mono 16-bit signed little-endian PCM at 44100 Hz.
	cmd := exec.Command(ffmpegPath,
		"-i", path,
		"-f", "s16le",
		"-ac", "1",
		"-ar", "44100",
		"-t", "120",
		"pipe:1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", 0, fmt.Errorf("ffmpeg decode: %w: %s", err, stderr.String())
	}

	raw := stdout.Bytes()
	if len(raw) < 2 {
		return "", 0, errors.New("ffmpeg produced no audio data")
	}

	// Convert raw bytes to []int16.
	numSamples := len(raw) / 2
	samples := make([]int16, numSamples)
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &samples); err != nil {
		return "", 0, fmt.Errorf("pcm decode: %w", err)
	}

	durationSec = numSamples / 44100

	// Compute fingerprint via chromaprint.
	ctx := C.chromaprint_new(C.CHROMAPRINT_ALGORITHM_DEFAULT)
	if ctx == nil {
		return "", 0, errors.New("chromaprint_new failed")
	}
	defer C.chromaprint_free(ctx)

	if C.chromaprint_start(ctx, 44100, 1) == 0 {
		return "", 0, errors.New("chromaprint_start failed")
	}

	if C.chromaprint_feed(ctx, (*C.int16_t)(unsafe.Pointer(&samples[0])), C.int(numSamples)) == 0 {
		return "", 0, errors.New("chromaprint_feed failed")
	}

	if C.chromaprint_finish(ctx) == 0 {
		return "", 0, errors.New("chromaprint_finish failed")
	}

	var fp *C.char
	if C.chromaprint_get_fingerprint(ctx, &fp) == 0 {
		return "", 0, errors.New("chromaprint_get_fingerprint failed")
	}
	fingerprint = C.GoString(fp)
	C.chromaprint_dealloc(unsafe.Pointer(fp))

	return fingerprint, durationSec, nil
}
