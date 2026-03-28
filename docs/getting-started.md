# Getting Started with Media Utopia

This guide walks you through setting up Media Utopia from scratch.

## Prerequisites

- Docker and Docker Compose
- An MQTT broker (or use the embedded one)
- Home Assistant (optional, but recommended)

## Quick Start with Docker Compose

### 1. Create the project directory

```bash
mkdir mu-setup && cd mu-setup
```

### 2. Create docker-compose.yml

```yaml
services:
  mqtt:
    image: eclipse-mosquitto:2
    restart: unless-stopped
    ports:
      - "1883:1883"
    volumes:
      - ./mosquitto/config:/mosquitto/config
      - ./mosquitto/data:/mosquitto/data

  mud:
    image: ghcr.io/mikey-austin/media_utopia/mud:latest
    restart: unless-stopped
    network_mode: host
    volumes:
      - ./mud_config:/config
      - ./music:/music:ro
    entrypoint: ["/usr/local/bin/mud"]
    command: ["--config", "/config/mud.toml"]
```

### 3. Create mosquitto configuration

```bash
mkdir -p mosquitto/config
cat > mosquitto/config/mosquitto.conf << 'EOF'
persistence true
persistence_location /mosquitto/data/
listener 1883
allow_anonymous true
EOF
```

### 4. Create mud configuration

```bash
mkdir -p mud_config
cat > mud_config/mud.toml << 'EOF'
[server]
broker = "mqtt://localhost:1883"
identity = "mud@home"
log_level = "info"

[modules.playlist]
enabled = true
name = "My Playlists"
provider = "playlist"
resource = "local"
storage_path = "/config/playlists"

[modules.renderer_gstreamer]
enabled = true
name = "My Speaker"
provider = "gstreamer"
resource = "default"
pipeline = "playbin uri={url} volume={volume}"

[modules.fs_library]
enabled = true
name = "My Music"
provider = "filesystem"
resource = "local"
roots = ["/music"]
include_exts = [".mp3", ".flac", ".ogg", ".m4a"]
http_listen = ":8484"
http_base_url = "http://localhost:8484"
metadata_mode = "ffprobe"
EOF
```

### 5. Add your music

Copy your music files into the `./music/` directory:

```bash
cp -r /path/to/your/music/* ./music/
```

### 6. Start everything

```bash
docker compose up -d
```

### 7. Verify

Check that mud started correctly:
```bash
docker compose logs mud | head -20
```

You should see:
- "starting module" for each enabled module
- "scan complete" from the filesystem library
- Presence messages published to MQTT

## Using the mu CLI

### Install

```bash
go install github.com/mikey-austin/media_utopia/cmd/mu@latest
```

Or download from releases.

### Configure

```bash
mkdir -p ~/.config/mu
cat > ~/.config/mu/config.toml << 'EOF'
broker = "mqtt://localhost:1883"
EOF
```

### Common Commands

```bash
# List all nodes
mu ls

# Show renderer status
mu status

# Browse library
mu lib browse

# Search for music
mu lib search "pink floyd"

# Add a track to queue
mu queue add lib:mu:library:filesystem:mud@home:local:ITEM_ID

# Play
mu play

# Control playback
mu pause
mu next
mu vol 50
mu seek +30s
```

## Home Assistant Integration

### Install the Custom Component

Copy the integration to your HA config:

```bash
cp -r custom_components/mu /path/to/ha/config/custom_components/mu
```

Or install via HACS (if published).

### Configure

1. Go to **Settings > Devices & Services > Add Integration**
2. Search for "Media Utopia"
3. Enter your MQTT settings:
   - Topic Base: `mu/v1` (default)
   - Discovery Prefix: `homeassistant` (default)
   - Identity: `homeassistant` (default)

### Use the Custom Panel

After setup, "MU" appears in the HA sidebar with:
- **Browse tab**: Browse and search your music library
- **Player**: Transport controls, queue management
- **Playlists**: Create, load, rename, delete playlists
- **Snapshots**: Save and restore queue state
- **Zones**: Multi-room audio control (if configured)

### Keyboard Shortcuts (in MU panel)

| Key | Action |
|-----|--------|
| Space | Play/Pause |
| → / ← | Seek ±10s |
| Shift+→ / Shift+← | Next/Previous track |
| ↑ / ↓ | Volume ±5% |
| M | Toggle mute |

## Adding More Renderers

Edit `mud.toml` to add multiple renderers:

```toml
[modules.renderer_gstreamer.living_room]
enabled = true
name = "Living Room"
provider = "gstreamer"
resource = "living-room"
pipeline = "playbin uri={url} volume={volume}"

[modules.renderer_gstreamer.bedroom]
enabled = true
name = "Bedroom"
provider = "gstreamer"
resource = "bedroom"
pipeline = "playbin uri={url} volume={volume}"
```

## Adding Jellyfin Library

```toml
[modules.bridge_jellyfin_library]
enabled = true
name = "Jellyfin"
provider = "jellyfin"
resource = "default"
base_url = "http://jellyfin:8096"
api_key = "YOUR_API_KEY"
user_id = "YOUR_USER_ID"
```

## Troubleshooting

### No renderers showing up
- Check mud logs: `docker compose logs mud`
- Verify MQTT connectivity: `mosquitto_sub -h localhost -t 'mu/v1/#' -v`
- Ensure the broker address matches in both mud and HA configs

### Queue adds items twice
- This was caused by MQTT QoS 1 redelivery. Ensure you're running the latest
  version which uses QoS 0 for commands and has renderer-side deduplication.

### Playlist operations fail
- Check which playlist server is selected in the MU panel's Playlists tab
- Verify the playlist server module is enabled and responsive
- Check HA logs for timeout messages

### Album art not showing
- Verify the library's HTTP server is accessible from HA
- Check the artwork_base_url configuration
- HA proxies artwork through `/api/mu/artwork` — check that endpoint
