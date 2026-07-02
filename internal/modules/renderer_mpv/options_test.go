package renderermpv

import "testing"

func TestEffectiveVolume(t *testing.T) {
	cases := []struct {
		name       string
		user, gain float64
		want       float64
	}{
		{"full", 1.0, 1.0, 100},
		{"half user", 0.5, 1.0, 50},
		{"composed", 0.8, 0.5, 40},
		{"zero gain", 1.0, 0.0, 0},
		{"clamped high", 1.5, 2.0, 100},
		{"clamped low", -0.2, 1.0, 0},
	}
	for _, tc := range cases {
		if got := effectiveVolume(tc.user, tc.gain); got != tc.want {
			t.Errorf("%s: effectiveVolume(%v,%v)=%v want %v", tc.name, tc.user, tc.gain, got, tc.want)
		}
	}
}

func TestHandleOptions(t *testing.T) {
	get := func(opts [][2]string, name string) (string, bool) {
		var val string
		found := false
		for _, kv := range opts { // last write wins, like mpv option application
			if kv[0] == name {
				val = kv[1]
				found = true
			}
		}
		return val, found
	}

	opts := handleOptions("pipewire", "sink-1", map[string]string{"network-timeout": "10"}, 42000)
	for name, want := range map[string]string{
		"vid":             "no",
		"audio-display":   "no",
		"terminal":        "no",
		"idle":            "yes",
		"cache":           "yes",
		"ao":              "pipewire",
		"audio-device":    "sink-1",
		"start":           "42.000",
		"network-timeout": "10",
	} {
		if got, ok := get(opts, name); !ok || got != want {
			t.Errorf("option %s = %q (found=%v), want %q", name, got, ok, want)
		}
	}

	// No device / no start → options absent.
	opts = handleOptions("alsa", "", nil, 0)
	for _, kv := range opts {
		if kv[0] == "audio-device" || kv[0] == "start" {
			t.Errorf("unexpected option %s", kv[0])
		}
	}
	if got, ok := get(opts, "ao"); !ok || got != "alsa" {
		t.Errorf("ao = %q, want alsa", got)
	}

	// Extra opts override base opts (escape hatch wins).
	opts = handleOptions("pipewire", "", map[string]string{"cache": "no"}, 0)
	if got, _ := get(opts, "cache"); got != "no" {
		t.Errorf("extra opts must override base: cache=%q want no", got)
	}
}
