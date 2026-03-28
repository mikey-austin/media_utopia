# Media Utopia - Home Assistant Integration

[![Home Assistant](https://img.shields.io/badge/Home_Assistant-Custom_Integration-blue?logo=homeassistant)](https://www.home-assistant.io/)
[![MQTT](https://img.shields.io/badge/Protocol-MQTT-purple?logo=eclipsemosquitto)](https://mqtt.org/)
[![Version](https://img.shields.io/badge/version-0.2.0-green)](custom_components/mu/manifest.json)

A Home Assistant custom integration that bridges the Media Utopia (MU) ecosystem into Home Assistant via MQTT. It translates MU renderer state and control topics into native HA entities, services, and a dedicated custom panel for media management.

## Features

- **Media player entities** -- full playback control (play, pause, stop, next, previous, seek, volume, mute) with album art, title, artist, album, duration, and position
- **Library browsing** -- browse and search Jellyfin, filesystem, and podcast libraries from the HA media browser
- **Playlist management** -- create, load, rename, delete, and reorder playlists; save queues to playlists
- **Snapshot save/restore** -- save renderer queue state as named snapshots and restore them later
- **Queue management** -- add, remove, move, jump, clear, shuffle, and set repeat mode
- **Custom panel** -- Lit-based two-pane panel (browser + queue) accessible from the HA sidebar
- **Zone/multi-room audio** -- Snapcast zone controller support with per-zone volume, mute, and source selection
- **Album artwork proxying** -- HTTP proxy endpoint with caching headers for upstream artwork
- **Automation services** -- `mu.load_playlist`, `mu.clear_queue`, and `mu.shuffle_queue` for use in automations and scripts

## Installation

Copy the `custom_components/mu/` directory into your Home Assistant `config/custom_components/` directory:

```
config/
  custom_components/
    mu/
      __init__.py
      bridge.py
      media_player.py
      ...
```

Restart Home Assistant, then add the integration via the UI.

**Prerequisite:** The [MQTT integration](https://www.home-assistant.io/integrations/mqtt/) must be configured and connected to the same broker as MU.

## Configuration

Add the integration via **Settings > Devices & Services > Add Integration > Media Utopia**. The config flow presents the following fields:

| Field | Default | Description |
|---|---|---|
| `topic_base` | `mu/v1` | Base MQTT topic used by Media Utopia |
| `discovery_prefix` | `homeassistant` | MQTT discovery prefix for Home Assistant |
| `entity_prefix` | `mu/ha` | Prefix used for entity naming |
| `identity` | `homeassistant` | Controller identity string sent to MU |
| `playlist_refresh_seconds` | `30` | How often to refresh playlists from the server |
| `artwork_base_url` | *(empty)* | Override base URL for artwork images; leave empty to auto-detect |

## Docker Development

A `docker-compose.yml` is provided for local development. It starts three containers:

- **homeassistant** -- HA with the integration mounted at `/config/custom_components/mu`
- **mqtt** -- Mosquitto broker on port 2883
- **mud** -- MU daemon built from the repo Dockerfile, with audio device passthrough

```bash
cd integrations/home_assistant
docker compose up -d
```

After the stack is running:

1. Open Home Assistant at `http://localhost:8123`
2. Add the MQTT integration (broker: `mqtt`, port: `2883`)
3. Add the Media Utopia integration

### TLS / Self-Signed Certificates

If your media sources use self-signed TLS, copy your CA cert to `ha_config/ca/ca.crt` and restart the stack. The compose file sets `SSL_CERT_FILE` and `REQUESTS_CA_BUNDLE` to trust it.

### Custom Branding

The compose setup mounts `ha_config/brands/mu/` which includes `icon.png` and `dark_icon.png` for the integration card.

## Architecture

```
custom_components/mu/
  __init__.py         Setup, panel registration, platform forwarding
  bridge.py           MQTT bridge: subscribes to MU topics, publishes HA discovery,
                        translates HA commands into MU protocol messages
  media_player.py     MediaPlayerEntity per renderer (playback, queue, media browser)
  zone.py             Zone entities for Snapcast multi-room audio
  websocket_api.py    WebSocket API powering the custom panel (50+ commands)
  views.py            HTTP artwork proxy endpoint (/api/mu/artwork)
  www/mu-panel.js     Lit-based custom panel (two-pane browser/queue layout)
  config_flow.py      UI-based configuration flow
  services.yaml       Service definitions for automations
  button.py           Lease control buttons (acquire/renew/release) per renderer
  select.py           Playlist-to-renderer selector entities
  sensor.py           Playlist size/revision sensors and queue length sensors
  text.py             Snapshot name text input per renderer
  const.py            Shared constants and defaults
```

### Data Flow

```
MU Daemon  <--MQTT-->  bridge.py  -->  HA Entity State
                                  <--  HA Service Calls / WebSocket Commands
                                  -->  MQTT Discovery (auto-creates entities)
```

The bridge subscribes to MU presence and state topics, maintains an in-memory model of renderers, playlists, libraries, zones, and snapshots, and publishes HA MQTT discovery configs so entities appear automatically. Control flows in reverse: HA service calls and WebSocket commands are translated into MU MQTT commands.

## Services

### `mu.load_playlist`

Load a playlist into a renderer's playback queue.

```yaml
service: mu.load_playlist
data:
  renderer: "living-room"
  playlist: "Evening Jazz"
  mode: replace    # replace | append | next
  resolve: auto    # auto | yes | no
```

### `mu.clear_queue`

Remove all entries from a renderer's queue.

```yaml
service: mu.clear_queue
data:
  renderer: "living-room"
```

### `mu.shuffle_queue`

Randomly reorder a renderer's queue.

```yaml
service: mu.shuffle_queue
data:
  renderer: "living-room"
```

## Environment Variables

| Variable | Description |
|---|---|
| `MU_CONFIG` | Path to the MU daemon configuration file (used by the `mud` container) |
| `SSL_CERT_FILE` | Path to CA certificate for TLS trust (set in compose) |
| `REQUESTS_CA_BUNDLE` | Python requests CA bundle path (set in compose) |
| `TZ` | Timezone for the Home Assistant container |

## Entities Created

When the integration discovers MU components, it creates:

- **`media_player.*`** -- one per renderer (full playback control + media browser)
- **`media_player.*_zone`** -- one per Snapcast zone (volume, mute, source select)
- **`sensor.*_playlist`** -- one per playlist (size, ID, revision attributes)
- **`sensor.*_queue_length`** -- queue length per renderer
- **`select.*_playlist`** -- playlist-to-renderer assignment
- **`button.*_lease_*`** -- acquire/renew/release lease per renderer
- **`button.*_save_snapshot`** -- save snapshot button per renderer
- **`text.*_snapshot_name`** -- snapshot name input per renderer

## Notes

- The integration requires the HA MQTT integration to already be connected to the broker.
- `play_media` supports `lib:` reference URIs and direct URLs. Library name selectors work if a unique match exists.
- Snapshots are browsable under the media browser "Snapshots" folder and can be loaded, deleted, or converted to playlists.
- The custom panel uses WebSocket subscriptions for real-time state updates.
