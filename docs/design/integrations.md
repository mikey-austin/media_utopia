# Integrations

Media Utopia integrates with popular systems via **bridges** to keep the core stable.

## UPnP/DLNA

### UPnP Renderer Bridge
Expose UPnP renderers as `mu` renderers.
The bridge:
- emulates a canonical queue
- maps play/pause/seek/volume to AVTransport/RenderingControl
- emits clean `mu` state and events even if UPnP eventing is unreliable

### UPnP Library Bridge
Expose UPnP media servers as `mu` libraries.
The bridge:
- maps browse/search to ContentDirectory
- resolves items into HTTP URLs from DIDL-Lite `res` elements
- optionally proxies or transcodes when needed

## Jellyfin

### Jellyfin Library Bridge
Expose Jellyfin as a `mu` library.
The bridge:
- uses Jellyfin API for browse/search/metadata
- resolves items into direct play URLs (preferred)
- provides optional transcode URLs when required

Rationale:
Jellyfin casting workflows often lack a durable queue model across targets; `mu`
restores queue stability through the renderer session queue + playlist server.

## Kodi

### Kodi Renderer Bridge
Expose Kodi as a `mu` renderer using Kodi JSON-RPC.
Kodi can play HTTP URLs directly and report state reliably.

### Kodi Library Bridge (optional future)
Possible via:
- UPnP (reuse UPnP library bridge), or
- JSON-RPC for richer metadata and faster browsing

## Home Assistant (first-class)

- `mu` is designed to map to HA entities via MQTT Discovery.
- Retained state enables instant recovery after HA restart.
- Lease-required mutation prevents accidental fights between automations and manual control.

## Advisors / Ollama (future)

Advisor nodes subscribe to playback events and propose:
- suggested playlists
- queue optimizations

These are stored as suggestion objects in the playlist server and can be
promoted or applied only by a controller holding a lease.
