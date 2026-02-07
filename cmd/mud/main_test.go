package main

import (
	"testing"

	"github.com/mikey-austin/media_utopia/internal/mud"
)

func TestBuildModulesModuleOnlyFilter(t *testing.T) {
	cfg := mud.Config{}
	cfg.Modules.Playlist = mud.ModuleConfigSet[mud.PlaylistConfig]{
		Items: map[string]mud.PlaylistConfig{
			"default": {
				Enabled:     true,
				Provider:    "plsrv",
				StoragePath: "/tmp",
			},
		},
	}
	cfg.Server.Identity = "test"
	cfg.Server.Namespace = "test"

	logFactory := mud.NewModuleLoggerFactory(mud.LogConfig{Level: "error"})
	modules, err := buildModules(cfg, nil, logFactory, "playlist", false)
	if err != nil {
		t.Fatalf("buildModules: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module")
	}

	_, err = buildModules(cfg, nil, logFactory, "renderer_gstreamer", false)
	if err == nil {
		t.Fatalf("expected error for filtered module")
	}
}
