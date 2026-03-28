# Architecture Overview (v1)

## System Shape

Media Utopia is a set of nodes communicating over MQTT:

- **Renderer nodes**: execute playback (GStreamer, Kodi, VLC, UPnP bridge)
- **Library nodes**: browse/search/resolve media references into HTTP streams (filesystem, Jellyfin, UPnP, podcast, go2rtc)
- **Playlist server node** (required): durable playlists + queue snapshots
- **Zone controller nodes** (optional): manage multi-room audio zones (e.g., Snapcast)
- **Zone nodes**: logical speaker endpoints managed by a zone controller
- **Advisor nodes** (future): observe events and propose suggestions

```
┌─────────────┐     ┌──────────────┐     ┌────────────────┐
│  Controller │     │   Renderer   │     │    Library     │
│  (mu CLI,   │────▶│  (GStreamer,  │     │  (filesystem,  │
│   HA panel) │     │   Kodi, VLC) │     │   Jellyfin)    │
└──────┬──────┘     └──────┬───────┘     └───────┬────────┘
       │                   │                     │
       │     ┌─────────────┴─────────────┐       │
       └────▶│       MQTT Broker         │◀──────┘
             │   (control plane)         │
             └─────────────┬─────────────┘
                           │
             ┌─────────────┴─────────────┐
             │   Playlist Server         │
             │   (playlists, snapshots)  │
             └───────────────────────────┘
```

## Control and Data Planes

- **MQTT (control plane):** commands, retained state, presence, events
- **HTTP (data plane):** media bytes (streams), artwork, playlist manifests

Renderers **pull** from HTTP URLs (NAT-friendly, bufferable, Range-enabled).
Libraries serve files via HTTP and announce stream URLs through MQTT commands.

### Why Two Planes?

| Concern | MQTT | HTTP |
|---------|------|------|
| Commands & replies | ✓ | |
| State synchronization | ✓ (retained) | |
| Media streaming | | ✓ (Range, caching) |
| Album artwork | | ✓ (cacheable) |
| Debugging | mosquitto_sub | curl |

## Canonical State Model

### Presence (retained, QoS 1)
`mu/v1/node/<id>/presence`
— Announces node existence, kind, name, capabilities.

### State (retained, QoS 1)
`mu/v1/node/<id>/state`
— Current renderer state: playback, queue, session.

### Commands (non-retained, QoS 0)
`mu/v1/node/<id>/cmd`
— Controller-to-node commands. QoS 0 with application-level timeout/retry.
  Receivers MUST deduplicate by command ID.

### Events (non-retained, QoS 0)
`mu/v1/node/<id>/evt`
— Informational transitions (track changed, playback started, etc.)

### Replies (non-retained, QoS 0)
`mu/v1/reply/<controller-instance>`
— Per-controller reply channel.

## Session Ownership (Leases)

- Controllers acquire a time-limited lease from a renderer
- All mutations require the lease token
- Lease expires without renewal (TTL-based)
- Only one controller can hold a lease at a time
- Read-only operations (status, queue.get) are always allowed

This prevents "two controllers fighting" and makes HA automations safe.

## Queue Semantics (Critical)

Queues are part of renderer session state.
The canonical "now/next/later" queue lives on the renderer.
Renderer bridges (UPnP, Kodi) MUST emulate queue semantics
even when the underlying target only supports "play one URL at a time."

### Queue Optimistic Concurrency

Queue state includes a monotonic `revision` counter. Controllers MAY include
`ifRevision` in commands for optimistic concurrency control. If the revision
doesn't match, the command is rejected with `REVISION_MISMATCH`.

## Playlist Server Role

The playlist server provides durable user objects:
- **Playlists**: ordered lists of media references
- **Snapshots**: saved queue state (position, repeat, shuffle)
- **Suggestions** (via advisor): AI-generated playlist proposals

This avoids relying on renderer capabilities for persistence.

## Zone Controllers (Multi-Room Audio)

Zone controllers manage groups of speakers (zones) connected to audio sources:

- Each zone has volume, mute, and source selection
- Zone sources map to renderer audio outputs (e.g., Snapcast streams)
- Zone state is published via MQTT retained state
- Controllers query zones via zone controller commands

### Architecture

```
Renderer → Source (audio stream) → Zone Controller → Zones (speakers)
```

## Node Health and Liveness

### Presence-Based Discovery

Nodes announce themselves via retained `/presence` messages on startup.
Presence includes a `ts` (timestamp) field. Controllers can estimate liveness
by comparing `ts` to the current time.

### Recommendations

- Renderers SHOULD re-publish presence every 5 minutes
- Controllers SHOULD consider a node offline if `ts` is older than 10 minutes
- MQTT Last Will and Testament (LWT) messages are recommended for immediate
  offline detection

## Home Assistant Integration

The HA integration acts as a bridge between mu's MQTT protocol and HA's entity model:

- **MQTT → HA Entities**: Renderers become `media_player` entities, zones become speakers
- **WebSocket API**: Custom panel communicates via HA WebSocket commands
- **Artwork Proxy**: HA proxies album art to handle TLS/authentication
- **State Subscriptions**: Real-time push via WebSocket subscriptions

See `docs/design/ha-integration.md` for the full integration architecture.

## Command Idempotency

Every command has a unique `id` field. Receivers MUST track recently processed
IDs and skip re-execution of duplicates. This prevents MQTT redelivery from
causing double-processing of queue mutations.

See `docs/spec/messages.md` § 1.1 for the normative specification.
