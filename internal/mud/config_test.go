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

func TestLoadConfigMissingFile(t *testing.T) {
	_, err := LoadConfig("/tmp/does_not_exist_mud_test.toml")
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestLoadConfigInvalidTOML(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "bad.toml")
	data := []byte("[server\nbroken = !!!\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatalf("expected parse error for invalid TOML")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "minimal.toml")
	// Completely empty but valid TOML.
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	// Zero-value defaults: empty strings and nil module sets.
	if cfg.Server.Broker != "" {
		t.Fatalf("expected empty broker, got %q", cfg.Server.Broker)
	}
	if cfg.Server.Identity != "" {
		t.Fatalf("expected empty identity, got %q", cfg.Server.Identity)
	}
	if cfg.Server.LogLevel != "" {
		t.Fatalf("expected empty log_level, got %q", cfg.Server.LogLevel)
	}
	if len(cfg.Modules.Playlist.List()) != 0 {
		t.Fatalf("expected no playlist modules")
	}
	if len(cfg.Modules.FSLibrary.List()) != 0 {
		t.Fatalf("expected no fs_library modules")
	}
}

func TestLoadConfigMultipleInstances(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "multi.toml")
	data := []byte("" +
		"[server]\n" +
		"broker = \"mqtt://localhost\"\n" +
		"\n" +
		"[modules.fs_library.music]\n" +
		"enabled = true\n" +
		"provider = \"filesystem\"\n" +
		"roots = [\"/music\"]\n" +
		"\n" +
		"[modules.fs_library.video]\n" +
		"enabled = true\n" +
		"provider = \"filesystem\"\n" +
		"roots = [\"/video\"]\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	items := cfg.Modules.FSLibrary.List()
	if len(items) != 2 {
		t.Fatalf("expected 2 fs_library instances, got %d", len(items))
	}
	// List() returns sorted by key: music, video.
	if items[0].Name != "music" || items[1].Name != "video" {
		t.Fatalf("expected names music and video, got %q and %q", items[0].Name, items[1].Name)
	}
	if len(items[0].Config.Roots) != 1 || items[0].Config.Roots[0] != "/music" {
		t.Fatalf("expected music roots [\"/music\"], got %v", items[0].Config.Roots)
	}
	if len(items[1].Config.Roots) != 1 || items[1].Config.Roots[0] != "/video" {
		t.Fatalf("expected video roots [\"/video\"], got %v", items[1].Config.Roots)
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

func TestConfigRendererMultiple(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "multi_renderer.toml")
	data := []byte("" +
		"[server]\n" +
		"broker = \"mqtt://localhost\"\n" +
		"identity = \"multi-renderer\"\n" +
		"\n" +
		"[modules.renderer_gstreamer.living_room]\n" +
		"enabled = true\n" +
		"name = \"Living Room\"\n" +
		"provider = \"gstreamer\"\n" +
		"resource = \"renderer-lr\"\n" +
		"device = \"hw:0\"\n" +
		"crossfade_ms = 500\n" +
		"\n" +
		"[modules.renderer_gstreamer.bedroom]\n" +
		"enabled = true\n" +
		"name = \"Bedroom\"\n" +
		"provider = \"gstreamer\"\n" +
		"resource = \"renderer-br\"\n" +
		"device = \"hw:1\"\n" +
		"crossfade_ms = 250\n" +
		"pipeline = \"autoaudiosink\"\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	items := cfg.Modules.RendererGStreamer.List()
	if len(items) != 2 {
		t.Fatalf("expected 2 renderer_gstreamer instances, got %d", len(items))
	}
	// List() returns sorted by key: bedroom, living_room.
	if items[0].Name != "bedroom" {
		t.Fatalf("expected first instance name 'bedroom', got %q", items[0].Name)
	}
	if items[1].Name != "living_room" {
		t.Fatalf("expected second instance name 'living_room', got %q", items[1].Name)
	}
	// Verify bedroom config.
	br := items[0].Config
	if br.Name != "Bedroom" {
		t.Fatalf("expected bedroom name 'Bedroom', got %q", br.Name)
	}
	if br.Device != "hw:1" {
		t.Fatalf("expected bedroom device 'hw:1', got %q", br.Device)
	}
	if br.CrossfadeMS != 250 {
		t.Fatalf("expected bedroom crossfade_ms 250, got %d", br.CrossfadeMS)
	}
	if br.Pipeline != "autoaudiosink" {
		t.Fatalf("expected bedroom pipeline 'autoaudiosink', got %q", br.Pipeline)
	}
	if br.Resource != "renderer-br" {
		t.Fatalf("expected bedroom resource 'renderer-br', got %q", br.Resource)
	}
	// Verify living_room config.
	lr := items[1].Config
	if lr.Name != "Living Room" {
		t.Fatalf("expected living_room name 'Living Room', got %q", lr.Name)
	}
	if lr.Device != "hw:0" {
		t.Fatalf("expected living_room device 'hw:0', got %q", lr.Device)
	}
	if lr.CrossfadeMS != 500 {
		t.Fatalf("expected living_room crossfade_ms 500, got %d", lr.CrossfadeMS)
	}
	if lr.Pipeline != "" {
		t.Fatalf("expected living_room pipeline empty, got %q", lr.Pipeline)
	}
}

func TestConfigEmbeddedMQTT(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "mqtt.toml")
	data := []byte("" +
		"[modules.embedded_mqtt]\n" +
		"enabled = true\n" +
		"listen = \"0.0.0.0:1883\"\n" +
		"allow_anonymous = false\n" +
		"username = \"muduser\"\n" +
		"password = \"s3cret\"\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	mqtt := cfg.Modules.EmbeddedMQTT
	if !mqtt.Enabled {
		t.Fatalf("expected embedded_mqtt enabled")
	}
	if mqtt.Listen != "0.0.0.0:1883" {
		t.Fatalf("expected listen '0.0.0.0:1883', got %q", mqtt.Listen)
	}
	if mqtt.AllowAnonymous {
		t.Fatalf("expected allow_anonymous false")
	}
	if mqtt.Username != "muduser" {
		t.Fatalf("expected username 'muduser', got %q", mqtt.Username)
	}
	if mqtt.Password != "s3cret" {
		t.Fatalf("expected password 's3cret', got %q", mqtt.Password)
	}
}

func TestConfigTLSSettings(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "tls.toml")
	data := []byte("" +
		"[server]\n" +
		"broker = \"mqtts://secure.broker:8883\"\n" +
		"identity = \"tls-test\"\n" +
		"\n" +
		"[server.tls]\n" +
		"ca = \"/etc/mqtt/ca.pem\"\n" +
		"cert = \"/etc/mqtt/client.crt\"\n" +
		"key = \"/etc/mqtt/client.key\"\n" +
		"\n" +
		"[server.auth]\n" +
		"user = \"tlsuser\"\n" +
		"pass = \"tlspass\"\n" +
		"\n" +
		"[modules.embedded_mqtt]\n" +
		"enabled = true\n" +
		"listen = \"0.0.0.0:8883\"\n" +
		"tls_ca = \"/etc/mqtt/server-ca.pem\"\n" +
		"tls_cert = \"/etc/mqtt/server.crt\"\n" +
		"tls_key = \"/etc/mqtt/server.key\"\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	// Verify server-level TLS settings.
	if cfg.Server.TLS.CA != "/etc/mqtt/ca.pem" {
		t.Fatalf("expected TLS CA '/etc/mqtt/ca.pem', got %q", cfg.Server.TLS.CA)
	}
	if cfg.Server.TLS.Cert != "/etc/mqtt/client.crt" {
		t.Fatalf("expected TLS cert '/etc/mqtt/client.crt', got %q", cfg.Server.TLS.Cert)
	}
	if cfg.Server.TLS.Key != "/etc/mqtt/client.key" {
		t.Fatalf("expected TLS key '/etc/mqtt/client.key', got %q", cfg.Server.TLS.Key)
	}
	// Verify server-level auth settings.
	if cfg.Server.Auth.User != "tlsuser" {
		t.Fatalf("expected auth user 'tlsuser', got %q", cfg.Server.Auth.User)
	}
	if cfg.Server.Auth.Pass != "tlspass" {
		t.Fatalf("expected auth pass 'tlspass', got %q", cfg.Server.Auth.Pass)
	}
	// Verify embedded MQTT TLS settings.
	mqtt := cfg.Modules.EmbeddedMQTT
	if mqtt.TLSCA != "/etc/mqtt/server-ca.pem" {
		t.Fatalf("expected embedded MQTT TLS CA '/etc/mqtt/server-ca.pem', got %q", mqtt.TLSCA)
	}
	if mqtt.TLSCert != "/etc/mqtt/server.crt" {
		t.Fatalf("expected embedded MQTT TLS cert '/etc/mqtt/server.crt', got %q", mqtt.TLSCert)
	}
	if mqtt.TLSKey != "/etc/mqtt/server.key" {
		t.Fatalf("expected embedded MQTT TLS key '/etc/mqtt/server.key', got %q", mqtt.TLSKey)
	}
}

func TestConfigServerDefaults(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "server_defaults.toml")
	// Config with only server.identity set; everything else should be zero-value.
	data := []byte("" +
		"[server]\n" +
		"identity = \"minimal-node\"\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.Identity != "minimal-node" {
		t.Fatalf("expected identity 'minimal-node', got %q", cfg.Server.Identity)
	}
	// All other string fields should be empty.
	if cfg.Server.Broker != "" {
		t.Fatalf("expected empty broker, got %q", cfg.Server.Broker)
	}
	if cfg.Server.TopicBase != "" {
		t.Fatalf("expected empty topic_base, got %q", cfg.Server.TopicBase)
	}
	if cfg.Server.Namespace != "" {
		t.Fatalf("expected empty namespace, got %q", cfg.Server.Namespace)
	}
	if cfg.Server.LogLevel != "" {
		t.Fatalf("expected empty log_level, got %q", cfg.Server.LogLevel)
	}
	if cfg.Server.LogFormat != "" {
		t.Fatalf("expected empty log_format, got %q", cfg.Server.LogFormat)
	}
	if cfg.Server.LogOutput != "" {
		t.Fatalf("expected empty log_output, got %q", cfg.Server.LogOutput)
	}
	// Bool fields should default to false.
	if cfg.Server.LogSource {
		t.Fatalf("expected log_source false")
	}
	if cfg.Server.LogUTC {
		t.Fatalf("expected log_utc false")
	}
	if cfg.Server.LogColor {
		t.Fatalf("expected log_color false")
	}
	if cfg.Server.Daemonize {
		t.Fatalf("expected daemonize false")
	}
	if cfg.Server.ContinueOnError {
		t.Fatalf("expected continue_on_error false")
	}
	if cfg.Server.RPCBreakerEnabled {
		t.Fatalf("expected rpc_breaker_enabled false")
	}
	// Numeric fields should be zero.
	if cfg.Server.RPCBreakerTimeoutMS != 0 {
		t.Fatalf("expected rpc_breaker_timeout_ms 0, got %d", cfg.Server.RPCBreakerTimeoutMS)
	}
	if cfg.Server.RPCBreakerIntervalMS != 0 {
		t.Fatalf("expected rpc_breaker_interval_ms 0, got %d", cfg.Server.RPCBreakerIntervalMS)
	}
	if cfg.Server.RPCBreakerMaxRequests != 0 {
		t.Fatalf("expected rpc_breaker_max_requests 0, got %d", cfg.Server.RPCBreakerMaxRequests)
	}
	if cfg.Server.RPCBreakerFailureThreshold != 0 {
		t.Fatalf("expected rpc_breaker_failure_threshold 0, got %d", cfg.Server.RPCBreakerFailureThreshold)
	}
	// TLS and Auth sub-structs should be zero-value.
	if cfg.Server.TLS.CA != "" || cfg.Server.TLS.Cert != "" || cfg.Server.TLS.Key != "" {
		t.Fatalf("expected empty TLS config, got %+v", cfg.Server.TLS)
	}
	if cfg.Server.Auth.User != "" || cfg.Server.Auth.Pass != "" {
		t.Fatalf("expected empty auth config, got %+v", cfg.Server.Auth)
	}
	// LogLevels map should be nil.
	if cfg.Server.LogLevels != nil {
		t.Fatalf("expected nil log_levels map, got %v", cfg.Server.LogLevels)
	}
	// No modules should be loaded.
	if len(cfg.Modules.Playlist.List()) != 0 {
		t.Fatalf("expected no playlist modules")
	}
	if len(cfg.Modules.RendererGStreamer.List()) != 0 {
		t.Fatalf("expected no renderer_gstreamer modules")
	}
	if cfg.Modules.EmbeddedMQTT.Enabled {
		t.Fatalf("expected embedded_mqtt disabled")
	}
}

func TestConfigNamespaceDefault(t *testing.T) {
	// The config struct itself does not apply the namespace=identity default;
	// that logic lives in cmd/mud/main.go's applyOverrides. Verify that the
	// config struct faithfully preserves whatever is in the file and that an
	// empty namespace stays empty (no hidden defaults in LoadConfig).
	tmp := t.TempDir()

	t.Run("namespace_empty_when_omitted", func(t *testing.T) {
		path := filepath.Join(tmp, "no_ns.toml")
		data := []byte("" +
			"[server]\n" +
			"identity = \"node-1\"\n")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.Server.Namespace != "" {
			t.Fatalf("expected empty namespace when omitted, got %q", cfg.Server.Namespace)
		}
		if cfg.Server.Identity != "node-1" {
			t.Fatalf("expected identity 'node-1', got %q", cfg.Server.Identity)
		}
	})

	t.Run("namespace_preserved_when_set", func(t *testing.T) {
		path := filepath.Join(tmp, "with_ns.toml")
		data := []byte("" +
			"[server]\n" +
			"identity = \"node-2\"\n" +
			"namespace = \"custom-ns\"\n")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.Server.Namespace != "custom-ns" {
			t.Fatalf("expected namespace 'custom-ns', got %q", cfg.Server.Namespace)
		}
		if cfg.Server.Identity != "node-2" {
			t.Fatalf("expected identity 'node-2', got %q", cfg.Server.Identity)
		}
	})
}

func TestRendererMPVConfig(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "mud.toml")
	data := []byte("" +
		"[server]\n" +
		"identity = \"mud-test\"\n" +
		"\n" +
		"[modules.renderer_mpv.living_room]\n" +
		"enabled = true\n" +
		"name = \"Living Room\"\n" +
		"provider = \"gstreamer\"\n" +
		"resource = \"default\"\n" +
		"node_id = \"mu:renderer:gstreamer:mud@livingroom:default\"\n" +
		"ao = \"pipewire\"\n" +
		"device = \"sink-1\"\n" +
		"crossfade_ms = 3000\n" +
		"volume = 0.8\n" +
		"[modules.renderer_mpv.living_room.mpv_options]\n" +
		"network-timeout = \"10\"\n" +
		"demuxer-max-bytes = \"32MiB\"\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	items := cfg.Modules.RendererMPV.List()
	if len(items) != 1 {
		t.Fatalf("expected 1 renderer_mpv item, got %d", len(items))
	}
	item := items[0].Config
	if !item.Enabled || item.AO != "pipewire" || item.Device != "sink-1" ||
		item.CrossfadeMS != 3000 || item.Volume != 0.8 ||
		item.NodeID != "mu:renderer:gstreamer:mud@livingroom:default" {
		t.Fatalf("unexpected config: %+v", item)
	}
	if item.MPVOptions["network-timeout"] != "10" || item.MPVOptions["demuxer-max-bytes"] != "32MiB" {
		t.Fatalf("mpv_options not parsed: %+v", item.MPVOptions)
	}
}
