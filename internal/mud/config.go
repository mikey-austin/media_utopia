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
	Namespace string     `toml:"namespace"`
	LogLevel  string     `toml:"log_level"`
	LogFormat string     `toml:"log_format"`
	LogOutput string     `toml:"log_output"`
	LogSource bool       `toml:"log_source"`
	LogUTC    bool       `toml:"log_utc"`
	LogColor  bool       `toml:"log_color"`
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
	RendererKodi          RendererKodiConfig      `toml:"renderer_kodi"`
	BridgeUPNPLibrary     BridgeUPNPLibraryConfig `toml:"bridge_upnp_library"`
	BridgeJellyfinLibrary JellyfinLibraryConfig   `toml:"bridge_jellyfin_library"`
	PodcastLibrary        PodcastLibraryConfig    `toml:"podcast"`
	Go2RTCLibrary         Go2RTCLibraryConfig     `toml:"go2rtc"`
	EmbeddedMQTT          EmbeddedMQTTConfig      `toml:"embedded_mqtt"`
}

// PlaylistConfig configures the playlist module.
type PlaylistConfig struct {
	Enabled     bool   `toml:"enabled"`
	Name        string `toml:"name"`
	Provider    string `toml:"provider"`
	Resource    string `toml:"resource"`
	StoragePath string `toml:"storage_path"`
}

// RendererGStreamerConfig configures the GStreamer renderer module.
type RendererGStreamerConfig struct {
	Enabled     bool   `toml:"enabled"`
	Name        string `toml:"name"`
	Provider    string `toml:"provider"`
	Resource    string `toml:"resource"`
	Pipeline    string `toml:"pipeline"`
	Device      string `toml:"device"`
	CrossfadeMS int64  `toml:"crossfade_ms"`
}

// RendererKodiConfig configures the Kodi renderer module.
type RendererKodiConfig struct {
	Enabled   bool   `toml:"enabled"`
	Name      string `toml:"name"`
	Provider  string `toml:"provider"`
	Resource  string `toml:"resource"`
	BaseURL   string `toml:"base_url"`
	Username  string `toml:"username"`
	Password  string `toml:"password"`
	TimeoutMS int64  `toml:"timeout_ms"`
}

// BridgeUPNPLibraryConfig configures the UPnP library bridge.
type BridgeUPNPLibraryConfig struct {
	Enabled  bool   `toml:"enabled"`
	Name     string `toml:"name"`
	Provider string `toml:"provider"`
	Resource string `toml:"resource"`
	Listen   string `toml:"listen"`
}

// JellyfinLibraryConfig configures the Jellyfin library bridge.
type JellyfinLibraryConfig struct {
	Enabled    bool   `toml:"enabled"`
	Name       string `toml:"name"`
	Provider   string `toml:"provider"`
	Resource   string `toml:"resource"`
	BaseURL    string `toml:"base_url"`
	APIKey     string `toml:"api_key"`
	UserID     string `toml:"user_id"`
	TimeoutMS  int64  `toml:"timeout_ms"`
	CacheTTLMS int64  `toml:"cache_ttl_ms"`
	CacheSize  int    `toml:"cache_size"`
}

// PodcastLibraryConfig configures the podcast library module.
type PodcastLibraryConfig struct {
	Enabled           bool     `toml:"enabled"`
	Name              string   `toml:"name"`
	Provider          string   `toml:"provider"`
	Resource          string   `toml:"resource"`
	Feeds             []string `toml:"feeds"`
	RefreshIntervalMS int64    `toml:"refresh_interval_ms"`
	CacheDir          string   `toml:"cache_dir"`
	TimeoutMS         int64    `toml:"timeout_ms"`
	ReverseSortByDate bool     `toml:"reverse_sort_by_date"`
}

// Go2RTCLibraryConfig configures the go2rtc library module.
type Go2RTCLibraryConfig struct {
	Enabled           bool     `toml:"enabled"`
	Name              string   `toml:"name"`
	Provider          string   `toml:"provider"`
	Resource          string   `toml:"resource"`
	BaseURL           string   `toml:"base_url"`
	Username          string   `toml:"username"`
	Password          string   `toml:"password"`
	Durations         []string `toml:"durations"`
	RefreshIntervalMS int64    `toml:"refresh_interval_ms"`
	TimeoutMS         int64    `toml:"timeout_ms"`
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
