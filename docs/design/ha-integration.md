# Home Assistant Integration Architecture

The Media Utopia HA integration bridges the mu MQTT protocol with Home Assistant's
entity model, providing native media player controls, a custom panel, and automation
services.

## Component Overview

```
┌──────────────────────────────────────────────────────┐
│                  Home Assistant                        │
│                                                        │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────┐   │
│  │  HA Core │  │   MQTT    │  │  HTTP Server     │   │
│  │ (entity  │  │ Component │  │  (views, panel)  │   │
│  │  model)  │  │           │  │                  │   │
│  └────┬─────┘  └─────┬─────┘  └────────┬─────────┘   │
│       │              │                  │             │
│  ┌────┴──────────────┴──────────────────┴──────────┐  │
│  │              MU Bridge (bridge.py)               │  │
│  │  ┌─────────┐ ┌──────────┐ ┌────────────────┐   │  │
│  │  │Renderer │ │ Library  │ │   Playlist     │   │  │
│  │  │ Manager │ │ Manager  │ │   Manager      │   │  │
│  │  └─────────┘ └──────────┘ └────────────────┘   │  │
│  │  ┌─────────┐ ┌──────────┐ ┌────────────────┐   │  │
│  │  │ Lease   │ │ Metadata │ │   Zone         │   │  │
│  │  │ Manager │ │ Cache    │ │   Manager      │   │  │
│  │  └─────────┘ └──────────┘ └────────────────┘   │  │
│  └──────────────────┬──────────────────────────────┘  │
│                     │                                  │
│  ┌──────────────────┴──────────────────────────────┐  │
│  │           WebSocket API (websocket_api.py)       │  │
│  │  40+ commands for panel ↔ bridge communication   │  │
│  └──────────────────┬──────────────────────────────┘  │
│                     │                                  │
│  ┌──────────────────┴──────────────────────────────┐  │
│  │           MU Panel (mu-panel.js)                 │  │
│  │  Lit web component: browser + player + zones     │  │
│  └──────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────┘
```

## Bridge (bridge.py)

The bridge is the core component (~2000 lines). It:

1. **Subscribes** to MQTT topics for presence, state, and replies
2. **Discovers** renderers, libraries, playlist servers, and zone controllers
3. **Publishes** MQTT commands to control renderers and libraries
4. **Maps** mu protocol entities to HA entities
5. **Caches** metadata and artwork URLs for performance

### MQTT → HA Entity Mapping

| mu Node Type | HA Entity | Platform |
|-------------|-----------|----------|
| Renderer | `media_player` | media_player.py |
| Renderer (lease) | `sensor` (owner, ID, TTL) | sensor.py |
| Renderer (queue) | `sensor` (length, index, status) | sensor.py |
| Renderer (snapshot) | `text` (name input) | text.py |
| Renderer (lease) | `button` (acquire, renew, release) | button.py |
| Renderer (snapshot) | `button` (save) | button.py |
| Playlist | `select` (renderer selection) | select.py |
| Playlist | `button` (load) | button.py |
| Zone | `media_player` (speaker) | zone.py |
| Zone Controller | `sensor` (total, active, sources) | sensor.py |

### State Synchronization

The bridge maintains an in-memory mirror of all mu state:

- `_renderers`: dict of renderer presence + state
- `_libraries`: dict of library presence
- `_playlist_servers`: dict of playlist server presence
- `_playlists`: dict of known playlists
- `_zones` / `_zone_controllers`: zone state

State flows:
1. Renderer publishes state to MQTT (retained)
2. Bridge receives via subscription, updates in-memory state
3. Bridge notifies HA entities via `async_write_ha_state()`
4. Bridge notifies WS subscriptions via event messages

### Lease Management

The bridge auto-acquires and renews leases for mutating operations:

- Each renderer has an independent lease
- Leases are acquired on first mutation, renewed before expiry
- On `LEASE_MISMATCH` error, the bridge reacquires and retries once
- Lease state is NOT persisted — lost on HA restart

### Metadata Resolution

For library items (`lib:` references), the bridge resolves metadata:

1. **On state change**: If current track lacks metadata, fetch via `library.resolve`
2. **On queue display**: Batch-resolve metadata via `library.resolveBatch`
3. **Cache**: In-memory LRU cache (10K entries, evicts oldest 2K)
4. **Failure tracking**: Failed lookups are skipped for 60 seconds

### Artwork Proxy

Album art URLs from libraries may use internal IP addresses not accessible
from browsers. The bridge proxies artwork through HA's HTTP server:

- `ArtworkProxyView` at `/api/mu/artwork?url=<upstream_url>`
- Adds `Cache-Control: public, max-age=3600` headers
- Handles SSL/TLS mismatches between HA and media servers

## WebSocket API (websocket_api.py)

The WS API provides 40+ commands for the custom panel:

### Command Categories

| Category | Commands |
|----------|----------|
| Discovery | `mu/renderers`, `mu/renderer_state`, `mu/libraries_list` |
| Playback | `mu/transport`, `mu/seek`, `mu/volume`, `mu/shuffle`, `mu/repeat_mode` |
| Queue | `mu/queue_get`, `mu/queue_add`, `mu/queue_remove`, `mu/queue_move`, `mu/queue_clear`, `mu/queue_jump`, `mu/queue_shuffle` |
| Playlists | `mu/playlists_list`, `mu/playlist_get`, `mu/playlist_create`, `mu/playlist_rename`, `mu/playlist_add`, `mu/playlist_remove`, `mu/playlist_move`, `mu/playlist_delete` |
| Snapshots | `mu/snapshots_list`, `mu/snapshot_save`, `mu/snapshot_delete`, `mu/playlist_from_snapshot` |
| Library | `mu/library_browse`, `mu/library_search` |
| Zones | `mu/zone_controllers_list`, `mu/zones_list`, `mu/zone_set_volume`, `mu/zone_set_mute`, `mu/zone_select_source` |
| Sessions | `mu/lease_status`, `mu/lease_acquire`, `mu/lease_release` |
| Subscriptions | `mu/subscribe_renderer_state`, `mu/subscribe_queue` |
| Server | `mu/playlist_servers_list`, `mu/playlist_server_select`, `mu/playlist_save_from_queue` |

### State Subscriptions

Instead of polling, the panel subscribes to state changes:

1. Panel calls `mu/subscribe_renderer_state` with renderer ID
2. Bridge registers a listener for that renderer's state changes
3. On meaningful state changes (status, title, volume, queue revision, session),
   the listener pushes an event to the panel via WebSocket
4. Position-only changes are filtered to reduce message volume
5. When the subscription is cancelled, the listener is unregistered

## Custom Panel (mu-panel.js)

A Lit 3.x web component providing a two-pane interface:

### Left Pane (Browser)
- **Browse tab**: Library browsing with breadcrumbs, letter index, search
- **Playlists tab**: Create, load, rename, delete playlists
- **Snapshots tab**: Save, restore, promote-to-playlist, delete
- **Zones tab**: Multi-room volume/mute/source control

### Right Pane (Player)
- Renderer selector dropdown
- Now-playing: title, artist, album, artwork
- Transport controls: shuffle, prev, play/pause, next, repeat
- Progress bar with client-side interpolation
- Volume slider with keyboard control
- Queue: drag-drop reorder, track numbers, equalizer animation

### Performance

- **WebSocket subscription** replaces 2s polling (10s fallback)
- **Client-side position interpolation** (1s timer) for smooth progress bar
- **Server-side deduplication** skips position-only state updates
- **Queue loading spinner** for async operations

## HA Services

| Service | Description |
|---------|-------------|
| `mu.load_playlist` | Load playlist into renderer queue |
| `mu.clear_queue` | Clear renderer queue |
| `mu.shuffle_queue` | Shuffle renderer queue |
| `mu.save_snapshot` | Save current queue as snapshot |

## Configuration

### Config Flow

| Field | Default | Description |
|-------|---------|-------------|
| `topic_base` | `mu/v1` | MQTT topic prefix |
| `discovery_prefix` | `homeassistant` | HA MQTT discovery prefix |
| `entity_prefix` | `mu/ha` | Entity topic prefix |
| `identity` | `homeassistant` | Controller identity string |
| `playlist_refresh_seconds` | `30` | Playlist list refresh interval |
| `artwork_base_url` | (auto) | Override artwork base URL |

### Options Flow

All settings can be reconfigured via **Settings → Devices & Services → MU → Configure**
without removing and re-adding the integration.
