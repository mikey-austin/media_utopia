package renderermpv

import (
	"fmt"
	"sort"
)

// handleOptions assembles the option list applied to a fresh mpv handle
// before initialization. Base audio-only options come first; per-config
// extras are appended last so they can override anything (the
// stream-compatibility escape hatch from the design doc).
func handleOptions(ao, device string, extra map[string]string, positionMS int64) [][2]string {
	opts := [][2]string{
		{"vid", "no"},
		{"audio-display", "no"},
		{"terminal", "no"},
		{"idle", "yes"},
		{"keep-open", "no"},
		{"cache", "yes"},
		{"ao", ao},
	}
	if device != "" {
		opts = append(opts, [2]string{"audio-device", device})
	}
	if positionMS > 0 {
		opts = append(opts, [2]string{"start", fmt.Sprintf("%.3f", float64(positionMS)/1000)})
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		opts = append(opts, [2]string{k, extra[k]})
	}
	return opts
}

// effectiveVolume composes the engine's 0.0–1.0 user volume with a 0.0–1.0
// crossfade gain into mpv's 0–100 softvol scale.
func effectiveVolume(user, gain float64) float64 {
	return clamp01(user) * clamp01(gain) * 100
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
