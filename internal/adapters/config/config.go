package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds CLI configuration from config.toml.
type Config struct {
	Broker    string            `toml:"broker"`
	Identity  string            `toml:"identity"`
	TopicBase string            `toml:"topic_base"`
	Aliases   map[string]string `toml:"aliases"`
	Defaults  Defaults          `toml:"defaults"`
}

// Defaults defines default selector values.
type Defaults struct {
	Renderer       string `toml:"renderer"`
	PlaylistServer string `toml:"playlist_server"`
	Library        string `toml:"library"`
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

func configPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "mu", "config.toml"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mu", "config.toml"), nil
}
