# Home Assistant Integration (HACS Custom Integration)

This directory contains a Home Assistant custom integration called **Media Utopia** that bridges Mu MQTT to Home Assistant using MQTT discovery.

## Design

The integration acts as a small MQTT bridge inside Home Assistant:

- Subscribes to Mu presence/state topics.
- Publishes Home Assistant MQTT discovery for entities.
- Translates Home Assistant MQTT control topics into Mu commands.

MQTT is configured by Home Assistant. The integration assumes the HA MQTT integration is already connected to the broker (embedded mu daemon broker or external broker both work).

## Current Capabilities

- **Renderer media_player entities**
  - Play/pause/stop/next/previous
  - Volume/mute
  - Seek
  - Repeat (maps to Mu queue repeat)
  - Shuffle (reorders the queue)
  - Album art, title, artist, album, duration, position

- **Playlist sensors**
  - One sensor per playlist with name, id, revision
- Load playlists via `mu.load_playlist` service (suitable for automations)

## Future Features

- Library browsing entities and media browser integration
- Snapshot entities and management
- Renderer/playlist grouping
- More control surface (e.g., queue management)

## Local Development and Testing

### 1) Run Mud locally

Make sure `mud` is running with MQTT enabled (embedded or external broker). Example:

```bash
./bin/mud
```

### 2) Start Home Assistant in Docker

Option A: Use docker-compose (recommended):

```bash
cd integrations/home_assistant
docker compose up -d
```

This starts three containers:

- `homeassistant` (Media Utopia integration mounted)
- `mqtt` (Mosquitto broker)
- `mud` (Mu daemon built from the repo Dockerfile)

The `mud` container mounts `/dev/snd` for audio output.

Option B: Use host networking (Linux-only):

```bash
docker run -d --name homeassistant \
  --network host \
  -v "$(pwd)/integrations/home_assistant/custom_components/mu:/config/custom_components/mu" \
  -v "$(pwd)/integrations/home_assistant/ha_config:/config" \
  ghcr.io/home-assistant/home-assistant:stable
```

Option C: Bridge network (macOS/Windows):

```bash
docker run -d --name homeassistant \
  -p 8123:8123 \
  -v "$(pwd)/integrations/home_assistant/custom_components/mu:/config/custom_components/mu" \
  -v "$(pwd)/integrations/home_assistant/ha_config:/config" \
  ghcr.io/home-assistant/home-assistant:stable
```

### 3) Configure MQTT in Home Assistant

Recent Home Assistant versions require MQTT to be configured via the UI.

In Home Assistant:

- Settings → Devices & Services → Add Integration → MQTT
- Broker: `mqtt`
- Port: `2883`

If you use bridge networking, set the broker to `host.docker.internal` (or your host IP).

### 4) Add the integration

In Home Assistant:

- Settings → Devices & Services → Add Integration
- Search for **Media Utopia**
- Set topic base (default `mu/v1`) and discovery prefix (default `homeassistant`)

### 5) Verify entities

- A `media_player` is created for each renderer
- A `sensor` is created for each playlist
- A `select` is created for each playlist to choose a renderer
- A `button` is created for each playlist to load it into the selected renderer
- Lease control buttons are created for each renderer (acquire/renew/release)

### 6) Use automations

Example automation action:

```yaml
service: mu.load_playlist
data:
  renderer: "GStreamer Renderer"
  playlist: "jazz"
  mode: replace
  resolve: auto
```

## Notes

- The integration expects Mu to publish renderer state and playlist server presence.
- `play_media` supports `lib:` refs and URLs. Library name selectors are supported if a unique match exists.
