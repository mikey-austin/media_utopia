package main

import (
	"testing"

	"github.com/mikey-austin/media_utopia/internal/mud"
)

func TestBuildModulesModuleOnlyFilter(t *testing.T) {
	cfg := mud.Config{}
	cfg.Modules.Playlist.Enabled = true
	cfg.Modules.Playlist.NodeID = "mu:playlist:plsrv:default:main"
	cfg.Modules.Playlist.StoragePath = "/tmp"

	logger := mud.NewLogger("error")
	modules, err := buildModules(cfg, nil, logger, "playlist")
	if err != nil {
		t.Fatalf("buildModules: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module")
	}

	_, err = buildModules(cfg, nil, logger, "renderer_gstreamer")
	if err == nil {
		t.Fatalf("expected error for filtered module")
	}
}
