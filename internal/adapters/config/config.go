package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds CLI configuration from config.toml.
type Config struct {
	Broker    string `toml:"broker,omitempty"`
	Identity  string `toml:"identity,omitempty"`
	TopicBase string `toml:"topic_base,omitempty"`
	// ActiveProfile selects which entry of Profiles overrides Defaults.
	ActiveProfile string              `toml:"active_profile,omitempty"`
	Aliases       map[string]string   `toml:"aliases,omitempty"`
	Defaults      Defaults            `toml:"defaults,omitempty"`
	Profiles      map[string]Defaults `toml:"profiles,omitempty"`
}

// Defaults defines default selector values.
type Defaults struct {
	Renderer       string `toml:"renderer,omitempty"`
	PlaylistServer string `toml:"playlist_server,omitempty"`
	Library        string `toml:"library,omitempty"`
}

// EffectiveDefaults returns the defaults with the active profile's
// non-empty fields layered over the top-level [defaults] section.
func (c Config) EffectiveDefaults() Defaults {
	out := c.Defaults
	if c.ActiveProfile == "" {
		return out
	}
	p, ok := c.Profiles[c.ActiveProfile]
	if !ok {
		return out
	}
	if p.Renderer != "" {
		out.Renderer = p.Renderer
	}
	if p.PlaylistServer != "" {
		out.PlaylistServer = p.PlaylistServer
	}
	if p.Library != "" {
		out.Library = p.Library
	}
	return out
}

// Load loads config.toml if present. Missing file returns an empty config.
func Load() (Config, error) {
	path, err := configPath()
	if err != nil {
		return Config{}, err
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	if info.IsDir() {
		return Config{}, errors.New("config path is a directory")
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Aliases == nil {
		cfg.Aliases = map[string]string{}
	}
	return cfg, nil
}

// Save writes the configuration back to the config path, creating parent
// directories as needed. Hand-written comments in the file are not
// preserved (the file is fully regenerated).
func Save(cfg Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("# mu CLI configuration (managed by 'mu config'; edits are preserved,\n")
	buf.WriteString("# comments are not).\n\n")
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func configPath() (string, error) {
	if confEnvOverride := os.Getenv("MU_CONFIG"); confEnvOverride != "" {
		return confEnvOverride, nil
	}

	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "mu", "config.toml"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mu", "config.toml"), nil
}
