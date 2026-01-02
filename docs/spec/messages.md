# Media Utopia (mu) — CLI MQTT Message Formats (v1)

**Status:** Draft v1  
**Applies to:** `mu` CLI reference client + any controller implementation  
**MQTT base prefix:** `mu/v1`  
**Primary UI:** Home Assistant (first-class)  
**Mutation policy:** **Lease required for any mutation**

This document defines the **wire-level MQTT message formats** emitted by the `mu` CLI for v1 commands, including **topics**, **envelopes**, and **example payloads**.

---

## 0. Conventions

### Placeholders used in examples

- Renderer ID:  
  `R = mu:renderer:upnp:uuid:550e8400-e29b-41d4-a716-446655440000`
- Playlist server ID:  
  `P = mu:playlist:plsrv:default:main`
- Library ID:  
  `L = mu:library:upnp:synology:cds`
- CLI identity:  
  `C = mikey@pixel`
- Reply topic (unique per `mu` process):  
  `replyTo = mu/v1/reply/mu-4242-a1b2c3`
- Correlation UUID:  
  `id = 3b2c2a2a-7b8e-4f2a-a8e4-8b3ce9f1c0a1`
- Epoch seconds:  
  `ts = 1735580000`

### Node IDs (URN scheme)

All node IDs use:

```
mu:<kind>:<provider>:<namespace>:<resource>
```

- `kind`: node type (`renderer`, `library`, `playlist`, `advisor`, `session`, etc).
- `provider`: implementation/backend (`gstreamer`, `jellyfin`, `upnp`, `plsrv`).
- `namespace`: deployment scope (defaults to `mud` server identity).
- `resource`: instance name within the namespace (defaults to `default`).

Examples:

- `mu:renderer:gstreamer:mud@livingroom:default`
- `mu:library:jellyfin:mud@livingroom:default`
- `mu:playlist:plsrv:mud@livingroom:default`

### MQTT QoS / retained (normative)

- `/cmd` publishes: **QoS 1**, **not retained**
- replies to `replyTo`: **QoS 1**, **not retained**
- `/evt` publishes: QoS 0 or 1, **not retained**
- `/presence` and `/state`: **retained**

---

## 1. Common Command Envelope (normative)

All controller commands MUST publish to:

```

mu/v1/node/<nodeId>/cmd

````

### Envelope

```json
{
  "id": "3b2c2a2a-7b8e-4f2a-a8e4-8b3ce9f1c0a1",
  "type": "queue.add",
  "ts": 1735580000,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": {
    "sessionId": "mu:session:renderer:upnp-550e8400:1735579900",
    "token": "lease-token-opaque"
  },
  "ifRevision": 102,
  "body": {}
}
````

#### Fields

* `id` (string, required): correlation UUID for request/response matching.
* `type` (string, required): command name (e.g. `session.acquire`, `queue.add`).
* `ts` (number, required): unix epoch seconds.
* `from` (string, required): controller identity string.
* `replyTo` (string, required for CLI): reply topic to receive `ack`/`error`.
* `lease` (object, required for any mutation): lease/session authorization.
* `ifRevision` (number, optional): optimistic concurrency guard.
* `body` (object, required): command arguments.

---

## 2. Reply / Ack / Error (normative)

Replies are published to the `replyTo` topic given in the command.

### Ack

```json
{
  "id": "3b2c2a2a-7b8e-4f2a-a8e4-8b3ce9f1c0a1",
  "type": "ack",
  "ok": true,
  "ts": 1735580000,
  "body": {
    "stateVersion": 78,
    "queueRevision": 103
  }
}
```

### Error

```json
{
  "id": "3b2c2a2a-7b8e-4f2a-a8e4-8b3ce9f1c0a1",
  "type": "error",
  "ok": false,
  "ts": 1735580000,
  "err": {
    "code": "LEASE_REQUIRED | LEASE_MISMATCH | NOT_FOUND | CONFLICT | INVALID",
    "message": "human readable",
    "detail": {}
  }
}
```

---

## 3. Presence and State (read-only)

### Presence (retained)

Topic:

```
mu/v1/node/<nodeId>/presence
```

Example renderer presence:

```json
{
  "nodeId": "mu:renderer:upnp:uuid:550e8400-e29b-41d4-a716-446655440000",
  "kind": "renderer",
  "name": "Living Room",
  "caps": {
    "queueResolve": true,
    "seek": true,
    "volume": true,
    "mime": ["audio/flac", "audio/opus", "audio/mpeg"]
  },
  "endpoints": {
    "http": "http://rend-bridge.local:8080"
  },
  "ts": 1735580000
}
```

### Renderer state (retained)

Topic:

```
mu/v1/node/<rendererId>/state
```

```json
{
  "session": {
    "id": "mu:session:renderer:upnp-550e8400:1735579900",
    "owner": "mikey@pixel",
    "leaseExpiresAt": 1735580012
  },
  "playback": {
    "status": "playing",
    "positionMs": 64213,
    "durationMs": 322000,
    "volume": 0.72,
    "mute": false
  },
  "queue": {
    "revision": 102,
    "length": 12,
    "index": 3
  },
  "current": {
    "queueEntryId": "mu:queueentry:renderer:R:7f3c2f5e",
    "itemId": "mu:track:upnp:synology:uuid:track-123",
    "metadata": {
      "title": "So What",
      "artist": "Miles Davis",
      "album": "Kind of Blue"
    }
  },
  "stateVersion": 77,
  "ts": 1735580000
}
```

---

## 4. Session / Lease Commands

All session commands target the renderer:

Topic:

```
mu/v1/node/R/cmd
```

### 4.1 `session.acquire`

```json
{
  "id": "11111111-2222-3333-4444-555555555555",
  "type": "session.acquire",
  "ts": 1735580000,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": {
    "ttlMs": 15000
  }
}
```

Ack body:

```json
{
  "session": {
    "id": "mu:session:renderer:upnp-550e8400:1735580000",
    "token": "lease-token-opaque",
    "owner": "mikey@pixel",
    "leaseExpiresAt": 1735580015
  },
  "stateVersion": 78
}
```

### 4.2 `session.renew`

```json
{
  "id": "aaaa...",
  "type": "session.renew",
  "ts": 1735580005,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": {
    "sessionId": "mu:session:renderer:upnp-550e8400:1735580000",
    "token": "lease-token-opaque"
  },
  "body": { "ttlMs": 15000 }
}
```

### 4.3 `session.release`

```json
{
  "id": "bbbb...",
  "type": "session.release",
  "ts": 1735580010,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": {
    "sessionId": "mu:session:renderer:upnp-550e8400:1735580000",
    "token": "lease-token-opaque"
  },
  "body": {}
}
```

---

## 5. Playback Commands (lease required)

Topic:

```
mu/v1/node/R/cmd
```

### 5.1 `playback.play`

```json
{
  "id": "p1...",
  "type": "playback.play",
  "ts": 1735580100,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": { "index": 3 }
}
```

### 5.2 `playback.pause`

```json
{
  "id": "p2...",
  "type": "playback.pause",
  "ts": 1735580102,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": {}
}
```

### 5.3 `playback.stop`

```json
{
  "id": "p3...",
  "type": "playback.stop",
  "ts": 1735580103,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": {}
}
```

### 5.4 `playback.seek`

```json
{
  "id": "p4...",
  "type": "playback.seek",
  "ts": 1735580110,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": { "positionMs": 120000 }
}
```

### 5.5 `playback.next` / `playback.prev`

```json
{
  "id": "p5...",
  "type": "playback.next",
  "ts": 1735580120,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": {}
}
```

---

## 6. Volume Commands (lease required)

Topic:

```
mu/v1/node/R/cmd
```

### 6.1 `playback.setVolume` (0..1)

```json
{
  "id": "v1...",
  "type": "playback.setVolume",
  "ts": 1735580200,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": { "volume": 0.35 }
}
```

### 6.2 `playback.setMute`

```json
{
  "id": "v2...",
  "type": "playback.setMute",
  "ts": 1735580201,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": { "mute": true }
}
```

---

## 7. Queue (canonical) — Read Operations

For full queue listings, v1 uses a read command:

Topic:

```
mu/v1/node/R/cmd
```

### 7.1 `queue.get` (paged)

```json
{
  "id": "qget...",
  "type": "queue.get",
  "ts": 1735580300,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "from": 0, "count": 50 }
}
```

Reply body:

```json
{
  "revision": 102,
  "index": 3,
  "entries": [
    {
      "queueEntryId": "mu:queueentry:renderer:R:7f3c2f5e",
      "itemId": "mu:track:upnp:synology:uuid:track-123",
      "metadata": { "title": "So What", "artist": "Miles Davis" }
    }
  ]
}
```

---

## 8. Queue — Mutation Operations (lease required)

Topic:

```
mu/v1/node/R/cmd
```

### 8.1 `queue.set` (atomic replace)

```json
{
  "id": "qset...",
  "type": "queue.set",
  "ts": 1735580400,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "ifRevision": 102,
  "body": {
    "startIndex": 0,
    "entries": [
      { "ref": { "id": "mu:track:upnp:synology:uuid:track-123" } },
      { "ref": { "id": "mu:track:jellyfin:home:item-8f9c" } },
      { "resolved": { "url": "http://radio.example/stream", "mime": "audio/mpeg", "byteRange": false } }
    ]
  }
}
```

### 8.2 `queue.add` (append / next / at)

```json
{
  "id": "qadd...",
  "type": "queue.add",
  "ts": 1735580410,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": {
    "position": "end | next | at",
    "atIndex": 4,
    "entries": [
      { "ref": { "id": "mu:track:upnp:synology:uuid:track-999" } }
    ]
  }
}
```

### 8.3 `queue.remove` (by queueEntryId)

```json
{
  "id": "qrm...",
  "type": "queue.remove",
  "ts": 1735580420,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": { "queueEntryId": "mu:queueentry:renderer:R:7f3c2f5e" }
}
```

(Alternative body accepted: `{ "index": 7 }`.)

### 8.4 `queue.move`

```json
{
  "id": "qmv...",
  "type": "queue.move",
  "ts": 1735580430,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": { "fromIndex": 10, "toIndex": 3 }
}
```

### 8.5 `queue.clear`

```json
{
  "id": "qclr...",
  "type": "queue.clear",
  "ts": 1735580440,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": {}
}
```

### 8.6 `queue.jump`

```json
{
  "id": "qjmp...",
  "type": "queue.jump",
  "ts": 1735580450,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": { "index": 5 }
}
```

### 8.7 `queue.shuffle`

```json
{
  "id": "qshuf...",
  "type": "queue.shuffle",
  "ts": 1735580460,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": { "seed": 12345 }
}
```

### 8.8 `queue.setShuffle`

Toggle shuffle mode without reordering.

```json
{
  "id": "qshufset...",
  "type": "queue.setShuffle",
  "ts": 1735580465,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": { "shuffle": true }
}
```

### 8.9 `queue.setRepeat`

```json
{
  "id": "qrep...",
  "type": "queue.setRepeat",
  "ts": 1735580470,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": { "repeat": true, "mode": "all | one | off" }
}
```

---

## 9. Playlist Server Commands

All playlist server commands target the playlist server node:

Topic:

```
mu/v1/node/P/cmd
```

### 9.1 `playlist.list`

```json
{
  "id": "plls...",
  "type": "playlist.list",
  "ts": 1735580500,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "owner": "mikey@pixel" }
}
```

Reply body:

```json
{
  "playlists": [
    { "playlistId": "mu:playlist:plsrv:default:pl-42", "name": "Modal Miles", "revision": 7 }
  ]
}
```

### 9.2 `playlist.create`

```json
{
  "id": "plcr...",
  "type": "playlist.create",
  "ts": 1735580510,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "name": "Modal Miles", "snapshotId": "mu:snapshot:plsrv:default:snap-1" }
}
```

### 9.3 `playlist.get`

```json
{
  "id": "plget...",
  "type": "playlist.get",
  "ts": 1735580520,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "playlistId": "mu:playlist:plsrv:default:pl-42" }
}
```

### 9.4 `playlist.delete`

```json
{
  "id": "pldel...",
  "type": "playlist.delete",
  "ts": 1735580530,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "playlistId": "mu:playlist:plsrv:default:pl-42" }
}
```

### 9.5 `playlist.addItems` (revisioned)

```json
{
  "id": "pladd...",
  "type": "playlist.addItems",
  "ts": 1735580530,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "ifRevision": 7,
  "body": {
    "playlistId": "mu:playlist:plsrv:default:pl-42",
    "entries": [
      { "ref": { "id": "mu:track:jellyfin:home:item-8f9c" } }
    ]
  }
}
```

### 9.5 `playlist.removeItems`

```json
{
  "id": "plrm...",
  "type": "playlist.removeItems",
  "ts": 1735580540,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "ifRevision": 8,
  "body": {
    "playlistId": "mu:playlist:plsrv:default:pl-42",
    "entryIds": ["mu:playlistentry:plsrv:default:pl-42:e-001"]
  }
}
```

### 9.6 `playlist.rename`

```json
{
  "id": "plrn...",
  "type": "playlist.rename",
  "ts": 1735580550,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": {
    "playlistId": "mu:playlist:plsrv:default:pl-42",
    "name": "Evening Miles"
  }
}
```

---

## 10. Load Playlist into Renderer Queue

Topic:

```
mu/v1/node/R/cmd
```

### 10.1 `queue.loadPlaylist` (renderer pulls from playlist server)

```json
{
  "id": "ldpl...",
  "type": "queue.loadPlaylist",
  "ts": 1735580600,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": {
    "playlistServerId": "mu:playlist:plsrv:default:main",
    "playlistId": "mu:playlist:plsrv:default:pl-42",
    "mode": "replace | append | next",
    "resolve": "auto | yes | no"
  }
}
```

---

## 11. Snapshots (save/restore session queues)

### 11.1 `snapshot.list` (playlist server command)

Topic:

```
mu/v1/node/P/cmd
```

```json
{
  "id": "snls...",
  "type": "snapshot.list",
  "ts": 1735580690,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": {
    "owner": "mikey@pixel"
  }
}
```

Reply body:

```json
{
  "snapshots": [
    { "snapshotId": "mu:snapshot:plsrv:default:snap-20251228-001", "name": "Friday night", "revision": 3 }
  ]
}
```

### 11.2 `snapshot.save` (playlist server command)

Topic:

```
mu/v1/node/P/cmd
```

```json
{
  "id": "snsv...",
  "type": "snapshot.save",
  "ts": 1735580700,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": {
    "name": "Friday night",
    "rendererId": "mu:renderer:upnp:uuid:550e8400-e29b-41d4-a716-446655440000",
    "sessionId": "mu:session:renderer:upnp-550e8400:1735580000",
    "capture": {
      "queueRevision": 103,
      "index": 3,
      "positionMs": 64213,
      "repeat": false,
      "shuffle": false
    },
    "items": [
      "lib:mu:library:jellyfin:home:default:track-1",
      "http://example.com/audio.mp3"
    ]
  }
}
```

### 11.3 `snapshot.get` (playlist server command)

Topic:

```
mu/v1/node/P/cmd
```

```json
{
  "id": "sngt...",
  "type": "snapshot.get",
  "ts": 1735580710,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "snapshotId": "mu:snapshot:plsrv:default:snap-20251228-001" }
}
```

Reply body:

```json
{
  "snapshotId": "mu:snapshot:plsrv:default:snap-20251228-001",
  "name": "Friday night",
  "revision": 3,
  "items": [
    "lib:mu:library:jellyfin:home:default:track-1",
    "http://example.com/audio.mp3"
  ],
  "capture": {
    "queueRevision": 103,
    "index": 3,
    "positionMs": 64213,
    "repeat": false,
    "shuffle": false
  }
}
```

### 11.4 `snapshot.remove` (playlist server command)

Topic:

```
mu/v1/node/P/cmd
```

```json
{
  "id": "snrm...",
  "type": "snapshot.remove",
  "ts": 1735580720,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "snapshotId": "mu:snapshot:plsrv:default:snap-20251228-001" }
}
```

Reply body: empty on success.

### 11.5 `queue.loadSnapshot` (renderer command)

Topic:

```
mu/v1/node/R/cmd
```

```json
{
  "id": "snld...",
  "type": "queue.loadSnapshot",
  "ts": 1735580710,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": {
    "playlistServerId": "mu:playlist:plsrv:default:main",
    "snapshotId": "mu:snapshot:plsrv:default:snap-20251228-001",
    "mode": "replace | append | next",
    "resolve": "auto | yes | no"
  }
}
```

---

## 12. Suggestions (advisor-driven, future-ready)

All suggestion commands target the playlist server unless noted.

Topic:

```
mu/v1/node/P/cmd
```

### 12.1 `suggest.list`

```json
{
  "id": "sgls...",
  "type": "suggest.list",
  "ts": 1735580750,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "owner": "mikey@pixel" }
}
```

Reply body:

```json
{
  "suggestions": [
    { "suggestionId": "mu:suggest:plsrv:default:s-01", "name": "Late Night", "revision": 2 }
  ]
}
```

### 12.2 `suggest.get`

```json
{
  "id": "sgget...",
  "type": "suggest.get",
  "ts": 1735580760,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "suggestionId": "mu:suggest:plsrv:default:s-01" }
}
```

### 12.3 `suggest.promote`

```json
{
  "id": "sgpr...",
  "type": "suggest.promote",
  "ts": 1735580770,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": {
    "suggestionId": "mu:suggest:plsrv:default:s-01",
    "name": "Late Night Set"
  }
}
```

### 12.4 `queue.loadSuggestion` (renderer command, lease required)

Topic:

```
mu/v1/node/R/cmd
```

```json
{
  "id": "sgld...",
  "type": "queue.loadSuggestion",
  "ts": 1735580780,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "lease": { "sessionId": "mu:session:renderer:upnp-550e8400:1735580000", "token": "lease-token-opaque" },
  "body": {
    "playlistServerId": "mu:playlist:plsrv:default:main",
    "suggestionId": "mu:suggest:plsrv:default:s-01",
    "mode": "replace | append | next",
    "resolve": "auto | yes | no"
  }
}
```

---

## 13. Library Commands (browse/search/resolve)

All library commands target the library node:

Topic:

```
mu/v1/node/L/cmd
```

### 12.1 `library.browse`

```json
{
  "id": "lb1...",
  "type": "library.browse",
  "ts": 1735580800,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "containerId": "0", "start": 0, "count": 50 }
}
```

Reply body (example):

```json
{
  "items": [
    {
      "itemId": "ff8b...",
      "name": "So What",
      "type": "Audio",
      "mediaType": "Audio",
      "artists": ["Miles Davis"],
      "album": "Kind of Blue",
      "containerId": "root",
      "overview": "Lorem ipsum",
      "durationMs": 322000,
      "imageUrl": "http://jellyfin.local:8096/Items/ff8b/Images/Primary?api_key=..."
    }
  ],
  "start": 0,
  "count": 1,
  "total": 1
}
```

Items may include `containerId` when the library backend reports a parent container.

### 12.2 `library.search`

```json
{
  "id": "ls1...",
  "type": "library.search",
  "ts": 1735580810,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": {
    "query": "Miles Davis so what",
    "start": 0,
    "count": 25,
    "types": ["Audio", "MusicAlbum"]
  }
}
```

Optional request fields:

- `types` (array of string): canonical types to filter by. Supported values:
  `Audio,MusicAlbum,MusicArtist,Movie,Series,Episode,Video,Playlist,Folder`.

Reply body: same shape as `library.browse`.

### 12.3 `library.resolve`

```json
{
  "id": "lr1...",
  "type": "library.resolve",
  "ts": 1735580820,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "itemId": "mu:track:upnp:synology:uuid:track-123" }
}
```

Optional request fields:

- `metadataOnly` (bool): return only metadata, skip resolving sources (faster for list views).

Reply body:

```json
{
  "itemId": "mu:track:upnp:synology:uuid:track-123",
  "metadata": {
    "title": "So What",
    "artist": "Miles Davis",
    "album": "Kind of Blue",
    "artworkUrl": "http://library/Items/ff8b/Images/Primary"
  },
  "sources": [
    { "url": "http://synology:50001/music/123.flac", "mime": "audio/flac", "byteRange": true }
  ]
}
```

When resolving container items (albums/artists), `sources` may include multiple playable tracks.

### 12.4 `library.resolveBatch`

Bulk resolve multiple items with a single request to reduce round-trips.

```json
{
  "id": "lrb1...",
  "type": "library.resolveBatch",
  "ts": 1735580820,
  "from": "mikey@pixel",
  "replyTo": "mu/v1/reply/mu-4242-a1b2c3",
  "body": { "itemIds": ["track-123", "track-456"] }
}
```

Optional request fields:

- `metadataOnly` (bool): return only metadata, skip resolving sources (faster for list views).

Reply body:

```json
{
  "items": [
    {
      "itemId": "track-123",
      "metadata": { "title": "So What", "artist": "Miles Davis" },
      "sources": [
        { "url": "http://library/track-123.flac", "mime": "audio/flac", "byteRange": true }
      ]
    },
    {
      "itemId": "track-456",
      "err": { "code": "NOT_FOUND", "message": "item not found" }
    }
  ]
}
```

---

## 14. Renderer Events (watch/advisors/HA sensors)

Events are published (not retained) to:

```
mu/v1/node/R/evt
```

### 13.1 `playback.started`

```json
{
  "type": "playback.started",
  "ts": 1735580900,
  "sessionId": "mu:session:renderer:upnp-550e8400:1735580000",
  "queueRevision": 103,
  "index": 3,
  "queueEntryId": "mu:queueentry:renderer:R:7f3c2f5e",
  "itemId": "mu:track:upnp:synology:uuid:track-123",
  "stateVersion": 80
}
```

### 13.2 `playback.ended`

```json
{
  "type": "playback.ended",
  "ts": 1735581222,
  "sessionId": "mu:session:renderer:upnp-550e8400:1735580000",
  "queueEntryId": "mu:queueentry:renderer:R:7f3c2f5e",
  "reason": "eof | skip | error",
  "stateVersion": 81
}
```

### 13.3 `queue.changed`

```json
{
  "type": "queue.changed",
  "ts": 1735580930,
  "sessionId": "mu:session:renderer:upnp-550e8400:1735580000",
  "queueRevision": 104,
  "stateVersion": 82
}
```

---

## 15. Notes / Implementation Guidance

* Controllers SHOULD treat IDs as opaque strings; only providers parse their own IDs.
* Renderer state is the primary source of truth for:

  * playback state
  * queue summary
  * lease ownership
* Full queue listings are obtained via `queue.get` (paged).
* On success, nodes SHOULD publish updated retained `/state` and return `ack` with new `stateVersion` and/or `queueRevision`.
* Use MQTT ACLs to ensure only:

  * nodes publish their own `/state` and `/presence`
  * controllers publish `/cmd` (subject to auth)
  * everyone can subscribe to `/state` and `/evt` (policy-dependent)

---

## 16. Appendix: Topic Summary

* Presence (retained): `mu/v1/node/<nodeId>/presence`
* State (retained): `mu/v1/node/<nodeId>/state`
* Commands: `mu/v1/node/<nodeId>/cmd`
* Events: `mu/v1/node/<nodeId>/evt`
* Replies: `mu/v1/reply/<controller-instance-id>`

---
