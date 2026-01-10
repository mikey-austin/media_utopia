# Core Concepts (v1)

## Nodes
Media Utopia is a set of discoverable **nodes** communicating over MQTT:

- **Renderer**: executes playback
- **Library**: browse/search/resolve media items into HTTP stream URLs
- **Playlist Server** (**required in v1**): durable playlists + queue snapshots
- **Advisor** (optional / future): observes events and proposes suggestions
- **Zone Controller** (optional): manages multi-room audio zones (e.g. Snapcast)
- **Zone**: a logical speaker endpoint managed by a zone controller
- **Source**: an audio input stream available to zones

## Canonical queue
The **renderer session** owns the canonical “now/next/later” queue.
This remains true even when the physical target (UPnP renderer) doesn’t support queues: the **bridge emulates queue semantics** and provides a stable state model to controllers.

## Lease required for mutation
All mutations require a **lease** (session ownership). Read-only access is always allowed.
This prevents “two controllers fighting” and makes HA automations safe by default.

## IDs
All IDs use a simple URN-style scheme:
```
mu:<kind>:<provider>:<namespace>:<resource>
```

IDs are opaque outside the provider that minted them.

### Components

- `kind`: node type (`renderer`, `library`, `playlist`, `advisor`, `zone_controller`, `zone`, `source`, `session`, etc).
- `provider`: implementation or backend (`gstreamer`, `jellyfin`, `upnp`, `plsrv`, `go2rtc`, `vlc`, `snapcast`).
- `namespace`: deployment scope. Defaults to the server identity in `mud` (`mud@livingroom`, `media-hub`).
- `resource`: instance name within the namespace (defaults to `default`).

### Examples

- `mu:renderer:gstreamer:mud@livingroom:default`
- `mu:renderer:kodi:mud@livingroom:default`
- `mu:renderer:vlc:mud@livingroom:default`
- `mu:library:jellyfin:mud@livingroom:default`
- `mu:playlist:plsrv:mud@livingroom:default`
- `mu:library:upnp:mud@lab:basement-nas`
- `mu:library:podcast:mud@studio:default`
- `mu:library:go2rtc:mud@studio:default`
- `mu:zone_controller:snapcast:mud@office:default`
- `mu:zone:snapcast:mud@office:kitchen`
- `mu:source:snapcast:mud@office:librespot`

### Use cases

- **Multi-room:** same provider across namespaces (`mud@kitchen`, `mud@office`).
- **Multi-instance:** multiple renderers per namespace (`default`, `livingroom`, `patio`).
- **Bridges:** tie a provider name to a backend (`jellyfin`, `upnp`, `kodi`, `go2rtc`).
- **Zones:** multi-room audio with zone controller backends (`snapcast`, `pipewire`).
