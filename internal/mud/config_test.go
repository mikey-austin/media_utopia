package mud

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "mud.toml")
	data := []byte("" +
		"[server]\n" +
		"broker = \"mqtt://localhost\"\n" +
		"identity = \"mud-test\"\n" +
		"\n" +
		"[modules.playlist]\n" +
		"enabled = true\n" +
		"provider = \"plsrv\"\n" +
		"storage_path = \"/tmp/mud\"\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.Broker != "mqtt://localhost" {
		t.Fatalf("expected broker")
	}
	items := cfg.Modules.Playlist.List()
	if len(items) != 1 || !items[0].Config.Enabled {
		t.Fatalf("expected playlist enabled")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}
	if path == "" {
		t.Fatalf("expected path")
	}
}

func TestLoadConfigZonesMap(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "mud.toml")
	data := []byte("" +
		"[modules.zone_snapcast]\n" +
		"enabled = true\n" +
		"provider = \"snapcast\"\n" +
		"server_url = \"ws://127.0.0.1:1780/jsonrpc\"\n" +
		"zones = { \"34:5a:60:4a:a5:db\" = \"Titan Snapclient 1\" }\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	items := cfg.Modules.ZoneSnapcast.List()
	if len(items) != 1 || !items[0].Config.Enabled {
		t.Fatalf("expected zone_snapcast enabled")
	}
	got := items[0].Config.Zones["34:5a:60:4a:a5:db"]
	if got != "Titan Snapclient 1" {
		t.Fatalf("zones map not loaded, got %q", got)
	}
}
