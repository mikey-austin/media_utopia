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

## Capabilities

Renderers announce their capabilities in the `caps` field of their presence message.
Controllers SHOULD check capabilities before sending commands that require them.

### Standard Capabilities

| Capability | Type | Description |
|-----------|------|-------------|
| `seek` | bool | Supports `playback.seek` |
| `volume` | bool | Supports `playback.setVolume` and `playback.setMute` |
| `queue` | bool | Supports queue commands (`queue.add`, `queue.remove`, etc.) |
| `queueResolve` | bool | Renderer can resolve `lib:` references in queue entries |
| `crossfade` | bool | Supports gapless/crossfade transitions between tracks |
| `shuffle` | bool | Supports `queue.setShuffle` |
| `repeat` | bool | Supports `queue.setRepeat` |
| `gapless` | bool | Supports gapless playback (no silence between tracks) |

### Library Capabilities

Libraries announce their capabilities in presence:

| Capability | Type | Description |
|-----------|------|-------------|
| `browse` | bool | Supports `library.browse` |
| `search` | bool | Supports `library.search` |
| `resolve` | bool | Supports `library.resolve` |
| `resolveBatch` | bool | Supports `library.resolveBatch` |
| `rescan` | bool | Supports `library.rescan` |

### Example Presence with Capabilities

```json
{
  "nodeId": "mu:renderer:gstreamer:mud@office:default",
  "kind": "renderer",
  "name": "Office Renderer",
  "caps": {
    "seek": true,
    "volume": true,
    "queue": true,
    "queueResolve": false,
    "crossfade": true,
    "shuffle": true,
    "repeat": true,
    "gapless": true
  },
  "ts": 1774700000
}
```

## Error Codes

When a command fails, the reply envelope contains an `err` object with a `code` and `message`. The following error codes are defined:

### Session/Lease Errors

| Code | Description | Client Action |
|------|-------------|---------------|
| `LEASE_REQUIRED` | Command requires a lease but none was provided | Acquire a lease first |
| `LEASE_MISMATCH` | Provided lease token does not match current session | Reacquire lease and retry |
| `LEASE_EXPIRED` | Lease has expired | Acquire a new lease |
| `SESSION_CONFLICT` | Another controller holds the lease | Wait or force-acquire |

### Queue Errors

| Code | Description | Client Action |
|------|-------------|---------------|
| `REVISION_MISMATCH` | Queue revision guard failed (optimistic concurrency) | Reload queue and retry |
| `INDEX_OUT_OF_RANGE` | Queue index is beyond queue bounds | Reload queue state |
| `ENTRY_NOT_FOUND` | Queue entry ID does not exist | Reload queue |

### General Errors

| Code | Description | Client Action |
|------|-------------|---------------|
| `INVALID` | Malformed command body | Check command format |
| `NOT_FOUND` | Referenced resource not found | Verify node/item IDs |
| `TIMEOUT` | Operation timed out | Retry |
| `UNSUPPORTED` | Command not supported by this node | Check capabilities |
| `INTERNAL` | Internal error | Report bug |

## Events

Nodes publish events to `mu/v1/node/<nodeId>/evt` (QoS 0, not retained).
Events are informational — clients MUST NOT rely on events for state; always
read `/state` for the canonical state.

### Event Envelope

```json
{
  "type": "track.changed",
  "ts": 1774700000,
  "data": { ... }
}
```

### Standard Event Types

| Event Type | Publisher | Description |
|-----------|----------|-------------|
| `playback.started` | Renderer | Playback began (includes `itemId`) |
| `playback.stopped` | Renderer | Playback stopped (user or end-of-queue) |
| `playback.paused` | Renderer | Playback paused |
| `playback.resumed` | Renderer | Playback resumed from pause |
| `playback.ended` | Renderer | Current track finished naturally |
| `playback.error` | Renderer | Playback error (includes `error` message) |
| `track.changed` | Renderer | Current track changed (includes new `itemId`) |
| `queue.changed` | Renderer | Queue was modified (includes new `revision`) |
| `volume.changed` | Renderer | Volume or mute state changed |
| `session.acquired` | Renderer | Lease acquired (includes `owner`) |
| `session.released` | Renderer | Lease released |
| `session.expired` | Renderer | Lease expired without renewal |
| `library.scan.started` | Library | Rescan started |
| `library.scan.complete` | Library | Rescan completed (includes `items` count) |
