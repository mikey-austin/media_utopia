# Architecture Overview (v1)

## System shape

Media Utopia is a set of nodes communicating over MQTT:

- **Renderer nodes**: execute playback
- **Library nodes**: browse/search/resolve media references into HTTP streams
- **Playlist server node** (required): durable playlists + snapshots
- **Advisor nodes** (optional future): observe and propose suggestions

## Control and data planes

- **MQTT:** commands, retained state, events
- **HTTP:** media bytes (streams), artwork, playlist manifests (optional)

Renderers **pull** from HTTP URLs (NAT-friendly, bufferable, Range-enabled).

## Canonical state model

### Presence (retained)
`mu/v1/node/<id>/presence`

### State (retained)
`mu/v1/node/<id>/state`

### Commands (QoS 1)
`mu/v1/node/<id>/cmd`

### Events (not retained)
`mu/v1/node/<id>/evt`

### Replies (per-controller)
`mu/v1/reply/<controller-instance>`

## Session ownership (leases)

- Controllers acquire a lease from a renderer
- Mutations require the lease token
- Lease expires without renewal

## Queue semantics (critical)
Queues are part of renderer session state.
Renderer bridges (UPnP, Kodi) are responsible for making queue semantics real
even when the underlying target only supports “play one URL at a time”.

## Playlist server role

The playlist server provides durable user objects:
- playlists
- snapshots
- (future) suggestions

This avoids relying on renderer capabilities for persistence.
