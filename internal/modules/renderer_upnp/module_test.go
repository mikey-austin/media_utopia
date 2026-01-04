//go:build upnp

package rendererupnp

import "testing"

func TestSanitizeResource(t *testing.T) {
	cases := map[string]string{
		"Living Room TV":  "living_room_tv",
		"TV@Home":         "tv_home",
		"   ":             "upnp_renderer",
		"Fancy-Renderer!": "fancy-renderer",
	}
	for in, want := range cases {
		if got := sanitizeResource(in); got != want {
			t.Fatalf("sanitize %q => %q, want %q", in, got, want)
		}
	}
}
