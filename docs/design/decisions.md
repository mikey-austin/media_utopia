# Design Decisions (v1)

This document captures decisions that are intentionally “locked” for v1 so the protocol remains coherent.

## 1) Name and namespace
- Project name: **Media Utopia**
- Protocol namespace: **`mu`**
- CLI name: **`mu`**

## 2) Control plane and data plane
- Control plane: **MQTT**
- Data plane: **HTTP**

Rationale:
- Both are ubiquitous and well-understood
- Both are debuggable with standard tools
- Both have mature security models (TLS, ACLs)

## 3) Home Assistant is first-class
- HA is the reference UI and primary controller surface
- The protocol is designed to map naturally to HA entities
- Retained state enables HA restart resilience

## 4) Reference UI and clients
- Reference UI: HA dashboards + automations
- Reference interactive client: `mu` CLI (like `mpc`, but lease-aware)

No standalone UI is required for v1.

## 5) Lease required for mutation (hard rule)
- Any command that changes state requires a valid lease
- Read-only operations are allowed without a lease
- This prevents controller fights and keeps automation safe

## 6) Playlist server is required (hard rule)
A dedicated **playlist server** is required in v1 to provide:
- durable playlists
- queue snapshots (save/restore session queues)
- a stable place for future “suggestions” objects

This avoids MPD-like ambiguity and Jellyfin-like casting queue gaps.

## 7) Canonical queue belongs to the renderer session
- The canonical “now/next/later” queue is part of renderer session state
- Renderer bridges MUST emulate a queue even if the underlying target cannot

This ensures queue consistency across UPnP, Kodi, and future renderers.

## 8) Bridge-first integration strategy
Legacy ecosystems are integrated via bridges, not by forcing controllers to speak legacy protocols:
- UPnP: renderer bridge + library bridge
- Jellyfin: library bridge
- Kodi: renderer bridge (library bridge optional later)

## 9) ID model (URN-style, opaque)
IDs follow:
```
mu:<kind>:<provider>:<namespace>:<resource>
```
IDs are opaque outside the minting provider.

Queue entries and playlist entries have their own IDs to avoid conflating
“the media item” with “a slot in a queue”.

## 10) Observability is a first-class property
- Retained `/state` and `/presence` topics
- Event stream `/evt` for transitions
- Debugging should be possible with `mosquitto_sub`

## 11) AI/Advisor features are observer-only
Advisor nodes:
- subscribe to events
- propose suggestions (stored by playlist server)
- never mutate playback or queues directly
Only controllers with leases can apply suggestions.
