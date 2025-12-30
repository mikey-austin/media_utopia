# Core Concepts (v1)

## Nodes
Media Utopia is a set of discoverable **nodes** communicating over MQTT:

- **Renderer**: executes playback
- **Library**: browse/search/resolve media items into HTTP stream URLs
- **Playlist Server** (**required in v1**): durable playlists + queue snapshots
- **Advisor** (optional / future): observes events and proposes suggestions

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
