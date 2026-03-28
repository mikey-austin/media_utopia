# Media Utopia

A boringly reliable media control plane for the home.

Media Utopia connects renderers (speakers), libraries (music collections), and
controllers (Home Assistant, CLI) over MQTT, with HTTP for media streaming.

## Features

- **Multiple renderers**: GStreamer, Kodi, VLC, UPnP/DLNA
- **Multiple libraries**: filesystem, Jellyfin, UPnP, podcast, go2rtc
- **Queue management**: add, remove, reorder, shuffle, repeat, snapshots
- **Playlist persistence**: durable playlists and queue snapshots
- **Home Assistant integration**: media player entities, custom panel, automations
- **Semantic search**: AI-powered search with MusicBrainz/Discogs enrichment
- **Multi-room audio**: Snapcast zone controller support
- **CLI client**: full-featured `mu` command-line tool

## Quick Start

See the [Getting Started Guide](docs/getting-started.md) for Docker Compose setup.

## Architecture

```
Controller (CLI/HA) → MQTT Broker → Renderer (GStreamer/Kodi/VLC)
                         ↕                    ↕
                    Library (fs/Jellyfin)  HTTP streams
                         ↕
                   Playlist Server
```

- **Control plane**: MQTT (commands, state, presence, events)
- **Data plane**: HTTP (media streams, artwork)
- **Lease model**: mutations require exclusive session ownership

See [Architecture Overview](docs/design/architecture.md) for details.

## Building

```bash
# Full build (with GStreamer + UPnP)
go build -tags "gstreamer upnp" ./cmd/mud
go build ./cmd/mu

# Library-only build (no CGO)
CGO_ENABLED=0 go build ./cmd/mud
go build ./cmd/mu

# Docker
docker build --target mud -t mud .
docker build --target mu -t mu .
```

## Testing

```bash
go test ./...
go test -tags integration ./...  # integration tests (needs MQTT)
```

## Documentation

- [Getting Started](docs/getting-started.md)
- [Protocol Specification](docs/spec/messages.md)
- [CLI Reference](docs/spec/cli.md)
- [Architecture](docs/design/architecture.md)
- [All Documentation](docs/README.md)

## Project Structure

```
cmd/mu/             CLI client
cmd/mud/            Daemon (multi-module server)
pkg/mu/             Wire protocol types
internal/core/      Service layer (CLI orchestration)
internal/ports/     Interface definitions
internal/adapters/  MQTT, config, output adapters
internal/modules/   Module implementations
  ├── playlist/           Playlist server
  ├── renderer_gstreamer/ GStreamer renderer
  ├── renderer_kodi/      Kodi renderer
  ├── renderer_vlc/       VLC renderer
  ├── renderer_upnp/      UPnP renderer bridge
  ├── fs_library/         Filesystem library
  ├── jellyfin_library/   Jellyfin library bridge
  ├── podcast_library/    Podcast/RSS library
  ├── go2rtc_library/     go2rtc camera library
  ├── zone_snapcast/      Snapcast zone controller
  └── embedded_mqtt/      Embedded MQTT broker
integrations/
  └── home_assistant/     HA custom component + panel
docs/                     Specifications and design
```

## License

See LICENSE file.
