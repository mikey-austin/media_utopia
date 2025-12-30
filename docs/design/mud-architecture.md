# mud (mu daemon) Architecture

This document defines the lightweight, goroutine-based server architecture for
`mud` (the mu daemon). The goal is a single binary with a master supervisor
that runs pluggable components based on configuration, inspired by Postfix but
without process-per-service overhead.

## Goals

- Single binary (`mud`) with config-driven component enablement.
- Lightweight supervision using goroutines and contexts.
- Clear module boundaries and configuration namespaces.
- Stable, observable operation with consistent logging and shutdown.

## Process Model

- One OS process, one master supervisor.
- Each enabled module runs in its own goroutine.
- Modules report startup errors, runtime errors, and health status back to the
  supervisor via channels.
- Supervisor handles graceful shutdown by canceling a shared context.

This keeps the system lightweight while preserving the modular shape of a
Postfix-style architecture.

## Module Model

Each module implements a simple contract:

- `Name() string`
- `Run(ctx context.Context, cfg Config, deps Dependencies) error`

Modules are registered with the supervisor and started only if enabled.

Planned modules:

- `playlist`: playlist server (required for v1)
- `renderer_gstreamer`: native renderer using GStreamer pipelines
- `bridge_upnp_library`: UPnP library bridge
- `bridge_jellyfin_library`: Jellyfin library bridge

## Configuration Model

Configuration is file-first with optional CLI overrides. The config schema
includes shared settings plus per-module sections.

Example (TOML):

```toml
[server]
broker = "mqtts://broker.local:8883"
identity = "mud@livingroom"
topic_base = "mu/v1"
log_level = "info"
daemonize = false

[server.tls]
ca = "/etc/mud/ca.pem"
cert = "/etc/mud/cert.pem"
key = "/etc/mud/key.pem"

[modules.playlist]
enabled = true
storage_path = "/var/lib/mud/playlists"

[modules.renderer_gstreamer]
enabled = true
pipeline = "playbin uri={url} volume={volume}"
device = "default"
crossfade_ms = 500

[modules.bridge_upnp_library]
enabled = true
listen = "0.0.0.0:9000"

[modules.bridge_jellyfin_library]
enabled = true
base_url = "http://jellyfin.local:8096"
api_key = "YOUR_KEY"
user_id = "YOUR_USER_ID"
timeout_ms = 5000

[modules.embedded_mqtt]
enabled = true
listen = "127.0.0.1:1883"
allow_anonymous = true
username = ""
password = ""
tls_ca = ""
tls_cert = ""
tls_key = ""
```

## GStreamer Pipeline Notes

The renderer uses a template string for the `gst-launch-1.0` pipeline. The
module replaces the following placeholders:

- `{url}`: media URL to play
- `{volume}`: float in range 0..1
- `{device}`: optional device name
- `{start_ms}`: seek start in milliseconds (if used in template)

Example pipelines:

- Audio (simple):
  `playbin uri={url} volume={volume}`
- Video (autovideo/audio sinks):
  `playbin uri={url} video-sink=autovideosink audio-sink=autoaudiosink`
- Radio stream:
  `uridecodebin uri={url} ! audioconvert ! audioresample ! autoaudiosink`

## GStreamer Bindings Build

The GStreamer renderer uses Go bindings and requires GStreamer development
libraries. Build with the `gstreamer` tag:

```bash
go build -tags gstreamer ./cmd/mud
```

Bindings source: `https://github.com/go-gst/go-gst`

## CLI Flags

`mud` accepts flags to override the configuration file:

- `--config <path>`
- `--broker <url>`
- `--topic-base <prefix>`
- `--identity <id>`
- `--log-level <level>`
- `--daemonize`
- `--module <name>` (limit to a subset of modules)
- `--dry-run` (validate config and exit)
- `--print-config` (print resolved config and exit)

## Supervision Flow

1. Load config, apply CLI overrides.
2. Initialize shared dependencies (logger, broker settings, storage paths).
3. Start enabled modules (goroutines).
4. Listen for module errors or context cancellation.
5. On shutdown, cancel context and wait for modules to exit.

## Embedded MQTT Broker

When `modules.embedded_mqtt.enabled` is true, `mud` starts a local broker using
`github.com/mochi-mqtt/server`. If `server.broker` is empty, `mud` will default
to `mqtt://<listen>` (or `mqtts://<listen>` when TLS is configured) so other
modules connect to the embedded broker automatically.

Authentication modes:

- `allow_anonymous = true`: allow all connections and topics.
- `allow_anonymous = false` with `username`/`password`: require basic auth.

## Logging

- Structured logging with `module` field.
- Single logger instance shared by all modules.
- Optional log file path to be added if needed.

## Future Extensions

- Health endpoints for module status.
- Backoff/restart policy per module.
- Metrics export (Prometheus).
