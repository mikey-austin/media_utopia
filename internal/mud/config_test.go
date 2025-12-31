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
	if !cfg.Modules.Playlist.Enabled {
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
