package mud

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the top-level configuration for mud.
type Config struct {
	Server  ServerConfig  `toml:"server"`
	Modules ModulesConfig `toml:"modules"`
}

// ServerConfig defines shared server settings.
type ServerConfig struct {
	Broker    string     `toml:"broker"`
	Identity  string     `toml:"identity"`
	TopicBase string     `toml:"topic_base"`
	LogLevel  string     `toml:"log_level"`
	Daemonize bool       `toml:"daemonize"`
	TLS       TLSConfig  `toml:"tls"`
	Auth      AuthConfig `toml:"auth"`
}

// TLSConfig holds TLS paths for MQTT.
type TLSConfig struct {
	CA   string `toml:"ca"`
	Cert string `toml:"cert"`
	Key  string `toml:"key"`
}

// AuthConfig holds MQTT auth credentials.
type AuthConfig struct {
	User string `toml:"user"`
	Pass string `toml:"pass"`
}

// ModulesConfig holds module configurations.
type ModulesConfig struct {
	Playlist              PlaylistConfig          `toml:"playlist"`
	RendererGStreamer     RendererGStreamerConfig `toml:"renderer_gstreamer"`
	BridgeUPNPLibrary     BridgeUPNPLibraryConfig `toml:"bridge_upnp_library"`
	BridgeJellyfinLibrary JellyfinLibraryConfig   `toml:"bridge_jellyfin_library"`
	EmbeddedMQTT          EmbeddedMQTTConfig      `toml:"embedded_mqtt"`
}

// PlaylistConfig configures the playlist module.
type PlaylistConfig struct {
	Enabled     bool   `toml:"enabled"`
	NodeID      string `toml:"node_id"`
	StoragePath string `toml:"storage_path"`
}

// RendererGStreamerConfig configures the GStreamer renderer module.
type RendererGStreamerConfig struct {
	Enabled     bool   `toml:"enabled"`
	NodeID      string `toml:"node_id"`
	Pipeline    string `toml:"pipeline"`
	Device      string `toml:"device"`
	CrossfadeMS int64  `toml:"crossfade_ms"`
}

// BridgeUPNPLibraryConfig configures the UPnP library bridge.
type BridgeUPNPLibraryConfig struct {
	Enabled bool   `toml:"enabled"`
	NodeID  string `toml:"node_id"`
	Listen  string `toml:"listen"`
}

// JellyfinLibraryConfig configures the Jellyfin library bridge.
type JellyfinLibraryConfig struct {
	Enabled   bool   `toml:"enabled"`
	NodeID    string `toml:"node_id"`
	BaseURL   string `toml:"base_url"`
	APIKey    string `toml:"api_key"`
	UserID    string `toml:"user_id"`
	TimeoutMS int64  `toml:"timeout_ms"`
}

// EmbeddedMQTTConfig configures the embedded MQTT broker.
type EmbeddedMQTTConfig struct {
	Enabled        bool   `toml:"enabled"`
	Listen         string `toml:"listen"`
	AllowAnonymous bool   `toml:"allow_anonymous"`
	Username       string `toml:"username"`
	Password       string `toml:"password"`
	TLSCA          string `toml:"tls_ca"`
	TLSCert        string `toml:"tls_cert"`
	TLSKey         string `toml:"tls_key"`
}

// LoadConfig loads a config file from path.
func LoadConfig(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("config path required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return Config{}, err
	}
	if info.IsDir() {
		return Config{}, errors.New("config path is a directory")
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// DefaultConfigPath returns the default config location.
func DefaultConfigPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "mu", "mud.toml"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mu", "mud.toml"), nil
}
