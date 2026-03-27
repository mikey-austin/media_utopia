package main

import (
	"testing"

	"github.com/mikey-austin/media_utopia/internal/mud"
)

func TestApplyOverrides(t *testing.T) {
	cfg := mud.Config{}
	cfg.Server.Broker = "mqtt://original"
	cfg.Server.Identity = "original-id"
	cfg.Server.TopicBase = "original/base"
	cfg.Server.LogLevel = "info"

	applyOverrides(&cfg, "mqtt://override", "new-id", "new/base", "debug", "", "", false, false, false, false)

	if cfg.Server.Broker != "mqtt://override" {
		t.Fatalf("expected broker override, got %q", cfg.Server.Broker)
	}
	if cfg.Server.Identity != "new-id" {
		t.Fatalf("expected identity override, got %q", cfg.Server.Identity)
	}
	if cfg.Server.TopicBase != "new/base" {
		t.Fatalf("expected topic_base override, got %q", cfg.Server.TopicBase)
	}
	if cfg.Server.LogLevel != "debug" {
		t.Fatalf("expected log_level override, got %q", cfg.Server.LogLevel)
	}
}

func TestApplyOverridesDefaults(t *testing.T) {
	cfg := mud.Config{}
	cfg.Server.Broker = "mqtt://keep"
	cfg.Server.Identity = "keep-id"
	cfg.Server.TopicBase = "keep/base"
	cfg.Server.LogLevel = "warn"

	// Empty string overrides should not change existing values.
	applyOverrides(&cfg, "", "", "", "", "", "", false, false, false, false)

	if cfg.Server.Broker != "mqtt://keep" {
		t.Fatalf("expected broker unchanged, got %q", cfg.Server.Broker)
	}
	if cfg.Server.Identity != "keep-id" {
		t.Fatalf("expected identity unchanged, got %q", cfg.Server.Identity)
	}
	if cfg.Server.TopicBase != "keep/base" {
		t.Fatalf("expected topic_base unchanged, got %q", cfg.Server.TopicBase)
	}
	if cfg.Server.LogLevel != "warn" {
		t.Fatalf("expected log_level unchanged, got %q", cfg.Server.LogLevel)
	}
	// Namespace should be set to identity when empty.
	if cfg.Server.Namespace != "keep-id" {
		t.Fatalf("expected namespace set to identity, got %q", cfg.Server.Namespace)
	}
}

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
