package mud

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

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
	Playlist              ModuleConfigSet[PlaylistConfig]          `toml:"playlist"`
	RendererGStreamer     ModuleConfigSet[RendererGStreamerConfig] `toml:"renderer_gstreamer"`
	RendererKodi          ModuleConfigSet[RendererKodiConfig]      `toml:"renderer_kodi"`
	RendererVLC           ModuleConfigSet[RendererVLCConfig]       `toml:"renderer_vlc"`
	BridgeUPNPLibrary     ModuleConfigSet[BridgeUPNPLibraryConfig] `toml:"bridge_upnp_library"`
	BridgeJellyfinLibrary ModuleConfigSet[JellyfinLibraryConfig]   `toml:"bridge_jellyfin_library"`
	PodcastLibrary        ModuleConfigSet[PodcastLibraryConfig]    `toml:"podcast"`
	Go2RTCLibrary         ModuleConfigSet[Go2RTCLibraryConfig]     `toml:"go2rtc"`
	EmbeddedMQTT          EmbeddedMQTTConfig                       `toml:"embedded_mqtt"`
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

// RendererVLCConfig configures the VLC renderer module.
type RendererVLCConfig struct {
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

// ModuleInstance holds a named config instance.
type ModuleInstance[T any] struct {
	Name   string
	Config T
}

// ModuleConfigSet supports single or multi-instance module configs.
type ModuleConfigSet[T any] struct {
	Items map[string]T
}

// UnmarshalTOML accepts a single table or a map of tables.
func (s *ModuleConfigSet[T]) UnmarshalTOML(data interface{}) error {
	if data == nil {
		return nil
	}
	raw, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid module config")
	}
	base := make(map[string]interface{})
	items := make(map[string]T)
	for key, value := range raw {
		if sub, ok := value.(map[string]interface{}); ok {
			var cfg T
			if err := decodeConfig(sub, &cfg); err != nil {
				return fmt.Errorf("invalid module instance %q: %w", key, err)
			}
			items[key] = cfg
			continue
		}
		base[key] = value
	}
	if len(base) > 0 {
		var cfg T
		if err := decodeConfig(base, &cfg); err != nil {
			return err
		}
		items["default"] = cfg
	}
	if len(items) == 0 && len(raw) > 0 {
		return fmt.Errorf("invalid module config")
	}
	s.Items = items
	return nil
}

// List returns instances sorted by key.
func (s ModuleConfigSet[T]) List() []ModuleInstance[T] {
	if len(s.Items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.Items))
	for key := range s.Items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ModuleInstance[T], 0, len(keys))
	for _, key := range keys {
		out = append(out, ModuleInstance[T]{Name: key, Config: s.Items[key]})
	}
	return out
}

func decodeConfig(raw map[string]interface{}, out interface{}) error {
	value := reflect.ValueOf(out)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("config target must be a pointer")
	}
	value = value.Elem()
	if value.Kind() != reflect.Struct {
		return fmt.Errorf("config target must be a struct")
	}
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("toml")
		name := strings.Split(tag, ",")[0]
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		rawVal, ok := raw[name]
		if !ok {
			continue
		}
		if err := assignField(value.Field(i), rawVal); err != nil {
			return fmt.Errorf("field %s: %w", field.Name, err)
		}
	}
	return nil
}

func assignField(field reflect.Value, raw interface{}) error {
	if !field.CanSet() {
		return nil
	}
	if prim, ok := raw.(toml.Primitive); ok {
		target := reflect.New(field.Type())
		if err := toml.PrimitiveDecode(prim, target.Interface()); err != nil {
			return err
		}
		field.Set(target.Elem())
		return nil
	}
	switch field.Kind() {
	case reflect.String:
		if val, ok := raw.(string); ok {
			field.SetString(val)
			return nil
		}
	case reflect.Bool:
		if val, ok := raw.(bool); ok {
			field.SetBool(val)
			return nil
		}
	case reflect.Int, reflect.Int64:
		switch val := raw.(type) {
		case int64:
			field.SetInt(val)
			return nil
		case int:
			field.SetInt(int64(val))
			return nil
		}
	case reflect.Slice:
		if field.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice type")
		}
		switch val := raw.(type) {
		case []string:
			field.Set(reflect.ValueOf(val))
			return nil
		case []interface{}:
			out := make([]string, 0, len(val))
			for _, item := range val {
				str, ok := item.(string)
				if !ok {
					return fmt.Errorf("expected string slice")
				}
				out = append(out, str)
			}
			field.Set(reflect.ValueOf(out))
			return nil
		}
	}
	return fmt.Errorf("unsupported value type %T", raw)
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
