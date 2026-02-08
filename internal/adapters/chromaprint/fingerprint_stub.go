//go:build !chromaprint

package chromaprint

// Enabled indicates chromaprint CGo bindings are NOT compiled in.
const Enabled = false

// ErrDisabled is returned when the chromaprint build tag is not enabled.
var ErrDisabled = errDisabled("chromaprint build tag not enabled")

type errDisabled string

func (e errDisabled) Error() string { return string(e) }

// FingerprintFile is a no-op stub that returns ErrDisabled.
func FingerprintFile(path string) (string, int, error) {
	return "", 0, ErrDisabled
}
