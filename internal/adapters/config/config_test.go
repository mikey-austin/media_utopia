package config

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTripWithProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	t.Setenv("MU_CONFIG", path)

	cfg := Config{
		Broker:        "mqtt://mqtt.lan:1883",
		ActiveProfile: "office",
		Aliases:       map[string]string{"lr": "mu:renderer:mpv:coltrane:mpv1"},
		Defaults:      Defaults{Renderer: "mu:renderer:gstreamer:coltrane:gstreamer1"},
		Profiles: map[string]Defaults{
			"office": {
				Renderer: "mu:renderer:mpv:coltrane:mpv1",
				Library:  "mu:library:filesystem:venus:music",
			},
		},
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ActiveProfile != "office" || got.Broker != "mqtt://mqtt.lan:1883" {
		t.Fatalf("round trip lost fields: %+v", got)
	}
	if got.Profiles["office"].Renderer != "mu:renderer:mpv:coltrane:mpv1" {
		t.Fatalf("profile lost: %+v", got.Profiles)
	}
	if got.Aliases["lr"] == "" {
		t.Fatalf("aliases lost: %+v", got.Aliases)
	}
}

func TestEffectiveDefaults(t *testing.T) {
	cfg := Config{
		Defaults: Defaults{
			Renderer:       "base-renderer",
			PlaylistServer: "base-plsrv",
			Library:        "base-library",
		},
		Profiles: map[string]Defaults{
			"office": {Renderer: "office-renderer"}, // library/plsrv fall back
		},
	}

	// No active profile: top-level defaults.
	d := cfg.EffectiveDefaults()
	if d.Renderer != "base-renderer" || d.Library != "base-library" {
		t.Fatalf("no-profile defaults: %+v", d)
	}

	// Active profile overrides non-empty fields only.
	cfg.ActiveProfile = "office"
	d = cfg.EffectiveDefaults()
	if d.Renderer != "office-renderer" {
		t.Fatalf("profile override missing: %+v", d)
	}
	if d.PlaylistServer != "base-plsrv" || d.Library != "base-library" {
		t.Fatalf("profile must fall back for unset fields: %+v", d)
	}

	// Unknown active profile falls back cleanly.
	cfg.ActiveProfile = "ghost"
	if d := cfg.EffectiveDefaults(); d.Renderer != "base-renderer" {
		t.Fatalf("unknown profile must fall back: %+v", d)
	}
}
