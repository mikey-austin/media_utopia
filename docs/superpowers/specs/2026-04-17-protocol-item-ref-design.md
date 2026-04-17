# Protocol Item References + Metadata API Design

**Date:** 2026-04-17  
**Scope:** core protocol types, library command surface, queue/state payloads, snapshot/playlist persistence  
**Status:** draft design for post-v1 cleanup with incremental migration

## Context

Media Utopia currently uses two overlapping identity schemes for library-backed media:

1. canonical node IDs with the `mu:<kind>:<provider>:<namespace>:<resource>` form
2. ad hoc library item refs using `lib:<libraryNodeId>:<itemId>`

At the same time, controllers often need to issue follow-up `library.resolve` or
`library.resolveBatch` requests with `metadataOnly=true` just to render queue or
current-track UI. That means:

- queue/state payloads are not self-sufficient for ordinary display
- item identity is not truly opaque
- controllers own too much metadata stitching logic
- `library.resolve` conflates item description with source resolution

This spec defines a replacement model:

- structured item references instead of `lib:` strings on the wire
- separate catalog metadata APIs and source-resolution APIs
- denormalized display metadata in queue/state payloads
- structured queue-entry persistence for snapshots and playlists

## Goals

1. One canonical library item reference model on the wire.
2. No controller should need to parse nested colon-delimited strings to identify items.
3. Queue/state payloads should be sufficient for normal UI display.
4. Metadata description and playable source resolution should be distinct APIs.
5. Migration should be reader-first and backward-compatible during rollout.

## Non-goals

- Redesigning MQTT topics or envelopes
- Changing lease semantics
- Changing browse/search pagination semantics
- Forcing immediate removal of all existing `lib:` parsing from CLI UX

---

## Design Summary

### New concepts

- `LibraryItemRef`: canonical structured reference to a library item
- `DisplayMetadata`: normalized UI-friendly display snapshot
- `library.getItem` / `library.getItems`: catalog metadata APIs
- `library.resolveSources` / `library.resolveSourcesBatch`: playback preparation APIs

### Legacy concepts kept temporarily

- `lib:<libraryId>:<itemId>` remains accepted during migration
- `library.resolve` / `library.resolveBatch` remain compatibility wrappers
- legacy string-only snapshot and playlist item arrays remain readable

---

## Canonical Item Reference

### `LibraryItemRef`

All protocol surfaces that refer to a library-backed item SHOULD use this JSON shape:

```json
{
  "kind": "libraryItem",
  "libraryId": "mu:library:jellyfin:mud@home:default",
  "itemId": "track-123"
}
```

### Field semantics

| Field | Type | Required | Meaning |
|---|---|---:|---|
| `kind` | string | yes | Fixed discriminator: `libraryItem` |
| `libraryId` | string | yes | Canonical library node id |
| `itemId` | string | yes | Opaque provider-owned item id |

### Rules

- `itemId` is opaque. Clients MUST NOT parse it.
- `libraryId` MUST be a canonical `mu:library:...` node id.
- The ref object is the canonical wire identity for library-backed media.

### Optional printable canonical string

For CLI and logs, Media Utopia MAY standardize a string form derived from the ref:

```text
mu:library:jellyfin:mud@home:default/item/track-123
mu:library:upnp:mud@office:default/item/uuid::base64
mu:library:filesystem:mud@home:default/container/artist/radiohead
```

This string form is not required on the wire. Structured JSON refs remain the primary protocol shape.

---

## Normalized Display Metadata

### `DisplayMetadata`

Queue entries, current item state, snapshot entries, and playlist entries SHOULD carry a denormalized display snapshot:

```json
{
  "title": "So What",
  "artist": "Miles Davis",
  "album": "Kind of Blue",
  "artworkUrl": "http://library/artwork/123",
  "durationMs": 322000,
  "mediaType": "audio"
}
```

### Recommended field set

| Field | Type | Required | Notes |
|---|---|---:|---|
| `title` | string | no | Human display title |
| `artist` | string | no | Flattened artist string for controllers |
| `artists` | array[string] | no | Optional richer representation |
| `album` | string | no | Album or collection title |
| `artworkUrl` | string | no | Library-owned or proxied artwork URL |
| `durationMs` | number | no | Duration in milliseconds |
| `mediaType` | string | no | `audio`, `video`, etc |

### Rules

- This is a snapshot for UX, not the source of truth.
- Libraries remain authoritative for catalog truth.
- Writers SHOULD include `DisplayMetadata` when known.
- Readers MUST tolerate missing or partial fields.

---

## Queue Entry Model

### New queue entry shape

```json
{
  "queueEntryId": "mu:queueentry:renderer:gstreamer:mud@home:qe-1",
  "ref": {
    "kind": "libraryItem",
    "libraryId": "mu:library:jellyfin:mud@home:default",
    "itemId": "track-123"
  },
  "resolved": {
    "url": "http://library/track-123.flac",
    "mime": "audio/flac",
    "byteRange": true
  },
  "display": {
    "title": "So What",
    "artist": "Miles Davis",
    "album": "Kind of Blue",
    "artworkUrl": "http://library/artwork/123"
  }
}
```

### Semantics

- `ref` identifies the catalog item, if applicable.
- `resolved` identifies the concrete playable source, if already materialized.
- `display` is the denormalized UI snapshot.
- A queue entry MAY have:
  - `ref` only
  - `resolved` only
  - both `ref` and `resolved`

### Typical cases

| Case | `ref` | `resolved` | `display` |
|---|---|---|---|
| Renderer resolves lazily | yes | no | yes |
| Controller resolves before enqueue | yes | yes | yes |
| Direct URL enqueue | no | yes | optional |

### Renderer obligations

- Queue mutation handlers MUST preserve `display` when supplied.
- `queue.get` SHOULD return `display`.
- Renderer `current` state SHOULD include `display`.
- Renderers MUST NOT fetch library metadata on their own just to synthesize missing `display`.

---

## Renderer State Changes

### Current item state

Current renderer state SHOULD evolve from:

```json
{
  "current": {
    "queueEntryId": "...",
    "itemId": "lib:...",
    "metadata": {
      "title": "..."
    }
  }
}
```

To:

```json
{
  "current": {
    "queueEntryId": "mu:queueentry:renderer:gstreamer:mud@home:qe-1",
    "ref": {
      "kind": "libraryItem",
      "libraryId": "mu:library:jellyfin:mud@home:default",
      "itemId": "track-123"
    },
    "resolved": {
      "url": "http://library/track-123.flac",
      "mime": "audio/flac",
      "byteRange": true
    },
    "display": {
      "title": "So What",
      "artist": "Miles Davis",
      "album": "Kind of Blue",
      "artworkUrl": "http://library/artwork/123"
    }
  }
}
```

### Compatibility

During migration, implementations MAY include legacy fields:

```json
{
  "itemId": "lib:mu:library:jellyfin:mud@home:default:track-123",
  "metadata": { "...": "..." }
}
```

Readers SHOULD prefer:

1. `display`
2. legacy `metadata`
3. follow-up catalog lookup only as a fallback

---

## Library Command Split

## `library.getItem`

Returns catalog metadata for one item without resolving playable sources.

### Request

Topic:

```text
mu/v1/node/<libraryId>/cmd
```

Body:

```json
{
  "ref": {
    "kind": "libraryItem",
    "libraryId": "mu:library:jellyfin:mud@home:default",
    "itemId": "track-123"
  }
}
```

If targeting the library topic already implies the library identity, an implementation MAY also accept:

```json
{
  "itemId": "track-123"
}
```

The canonical reply MUST still include a full `ref`.

### Reply

```json
{
  "ref": {
    "kind": "libraryItem",
    "libraryId": "mu:library:jellyfin:mud@home:default",
    "itemId": "track-123"
  },
  "display": {
    "title": "So What",
    "artist": "Miles Davis",
    "album": "Kind of Blue",
    "artworkUrl": "http://library/artwork/123",
    "durationMs": 322000,
    "mediaType": "audio"
  },
  "attributes": {
    "container": false
  }
}
```

## `library.getItems`

Batch form of `library.getItem`.

### Request

```json
{
  "refs": [
    {
      "kind": "libraryItem",
      "libraryId": "mu:library:jellyfin:mud@home:default",
      "itemId": "track-123"
    },
    {
      "kind": "libraryItem",
      "libraryId": "mu:library:jellyfin:mud@home:default",
      "itemId": "track-456"
    }
  ]
}
```

### Reply

```json
{
  "items": [
    {
      "ref": {
        "kind": "libraryItem",
        "libraryId": "mu:library:jellyfin:mud@home:default",
        "itemId": "track-123"
      },
      "display": {
        "title": "So What",
        "artist": "Miles Davis"
      }
    },
    {
      "ref": {
        "kind": "libraryItem",
        "libraryId": "mu:library:jellyfin:mud@home:default",
        "itemId": "track-456"
      },
      "err": {
        "code": "NOT_FOUND",
        "message": "item not found"
      }
    }
  ]
}
```

## `library.resolveSources`

Returns playable sources for one item without redefining catalog metadata.

### Request

```json
{
  "ref": {
    "kind": "libraryItem",
    "libraryId": "mu:library:jellyfin:mud@home:default",
    "itemId": "track-123"
  }
}
```

### Reply

```json
{
  "ref": {
    "kind": "libraryItem",
    "libraryId": "mu:library:jellyfin:mud@home:default",
    "itemId": "track-123"
  },
  "sources": [
    {
      "url": "http://library/track-123.flac",
      "mime": "audio/flac",
      "byteRange": true
    }
  ]
}
```

## `library.resolveSourcesBatch`

Batch form of `library.resolveSources`.

### Request

```json
{
  "refs": [
    {
      "kind": "libraryItem",
      "libraryId": "mu:library:jellyfin:mud@home:default",
      "itemId": "track-123"
    }
  ]
}
```

### Reply

```json
{
  "items": [
    {
      "ref": {
        "kind": "libraryItem",
        "libraryId": "mu:library:jellyfin:mud@home:default",
        "itemId": "track-123"
      },
      "sources": [
        {
          "url": "http://library/track-123.flac",
          "mime": "audio/flac",
          "byteRange": true
        }
      ]
    }
  ]
}
```

---

## Compatibility Behavior for `library.resolve`

Existing `library.resolve` and `library.resolveBatch` remain compatibility APIs during migration.

### Compatibility mapping

| Legacy call | New behavior |
|---|---|
| `library.resolve(metadataOnly=true)` | same as `library.getItem` |
| `library.resolve(metadataOnly=false)` | compose `library.getItem + library.resolveSources` |
| `library.resolveBatch(metadataOnly=true)` | same as `library.getItems` |
| `library.resolveBatch(metadataOnly=false)` | compose `library.getItems + library.resolveSourcesBatch` |

### Legacy reply compatibility

Legacy replies MAY continue to include:

```json
{
  "itemId": "track-123",
  "metadata": { "...": "..." },
  "sources": [ ... ]
}
```

Newer implementations SHOULD also be able to populate:

```json
{
  "ref": { ... },
  "display": { ... },
  "sources": [ ... ]
}
```

---

## Queue Mutation API Changes

## `queue.add`

### Request body

```json
{
  "position": "end",
  "entries": [
    {
      "ref": {
        "kind": "libraryItem",
        "libraryId": "mu:library:jellyfin:mud@home:default",
        "itemId": "track-123"
      },
      "display": {
        "title": "So What",
        "artist": "Miles Davis"
      }
    }
  ]
}
```

### Compatibility

During migration, implementations SHOULD accept old entry shapes:

```json
{
  "ref": {
    "id": "lib:mu:library:jellyfin:mud@home:default:track-123"
  }
}
```

Writers SHOULD migrate to structured refs first; legacy acceptance can remain temporarily.

## `queue.get`

### Reply body

```json
{
  "revision": 102,
  "index": 3,
  "entries": [
    {
      "queueEntryId": "mu:queueentry:renderer:gstreamer:mud@home:qe-1",
      "ref": {
        "kind": "libraryItem",
        "libraryId": "mu:library:jellyfin:mud@home:default",
        "itemId": "track-123"
      },
      "display": {
        "title": "So What",
        "artist": "Miles Davis",
        "album": "Kind of Blue"
      }
    }
  ]
}
```

Queue readers SHOULD stop depending on `itemId` strings once this is available.

---

## Snapshot and Playlist Persistence

## `snapshot.save`

Current snapshots preserve `items: []string`. That is lossy.

### New request body

```json
{
  "name": "Friday night",
  "rendererId": "mu:renderer:gstreamer:mud@home:default",
  "sessionId": "mu:session:renderer:gstreamer:mud@home:1735580000",
  "capture": {
    "queueRevision": 103,
    "index": 3,
    "positionMs": 64213,
    "repeat": false,
    "shuffle": false
  },
  "entries": [
    {
      "ref": {
        "kind": "libraryItem",
        "libraryId": "mu:library:jellyfin:mud@home:default",
        "itemId": "track-123"
      },
      "display": {
        "title": "So What",
        "artist": "Miles Davis"
      }
    },
    {
      "resolved": {
        "url": "http://radio.example/stream",
        "mime": "audio/mpeg",
        "byteRange": false
      },
      "display": {
        "title": "WFMU Stream"
      }
    }
  ]
}
```

### Compatibility

- Readers MUST accept legacy `items`.
- Writers SHOULD emit `entries`.
- Storage MAY retain both during migration if needed.

## `snapshot.get`

### New reply body

```json
{
  "snapshotId": "mu:snapshot:plsrv:mud@home:snap-1",
  "name": "Friday night",
  "revision": 3,
  "entries": [
    {
      "ref": {
        "kind": "libraryItem",
        "libraryId": "mu:library:jellyfin:mud@home:default",
        "itemId": "track-123"
      },
      "display": {
        "title": "So What",
        "artist": "Miles Davis"
      }
    }
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

## Playlist entry model

Playlists SHOULD converge on the same entry shape as queue and snapshot entries:

```json
{
  "entryId": "mu:playlistentry:plsrv:mud@home:e-1",
  "ref": {
    "kind": "libraryItem",
    "libraryId": "mu:library:jellyfin:mud@home:default",
    "itemId": "track-123"
  },
  "display": {
    "title": "So What",
    "artist": "Miles Davis"
  }
}
```

This lets playlists preserve:

- library item refs
- direct URL entries
- future resolved entries where needed
- display snapshots

---

## Migration Rules

## Reader-first compatibility

Phase order:

1. readers accept both old and new
2. writers emit both or prefer new
3. readers stop depending on old
4. writers stop emitting old

### Required compatibility reads

During migration, readers SHOULD accept:

- `itemId: "lib:..."`
- old `ref.id` strings
- new structured `ref`
- old `metadata`
- new `display`
- legacy snapshot/playlist `items`
- new snapshot/playlist `entries`

### Preferred read order for controllers

For display:

1. `display`
2. legacy `metadata`
3. explicit `library.getItem(s)` fallback

For item identity:

1. structured `ref`
2. legacy `ref.id`
3. legacy `itemId`

---

## Implementation Guidance

### Shared protocol package

Add or evolve types for:

- `LibraryItemRef`
- `DisplayMetadata`
- queue entry payloads
- current item state payloads
- `library.getItem(s)` request/reply bodies
- `library.resolveSources*` request/reply bodies

### Home Assistant

The HA bridge should:

- stop treating `lib:` as the primary queue/state identity
- prefer `display` for queue and now-playing rendering
- use `library.getItems` for refresh or cache miss paths
- keep `lib:` parsing only as a compatibility layer

### CLI

The CLI may keep accepting:

- `lib:<selector>:<itemId>`
- human-entered canonical strings

But it SHOULD convert them to structured refs before publish.

### Libraries

Libraries should:

- implement the new metadata/source split
- keep old resolve commands as wrappers during migration
- avoid returning provider-local bare item ids in new canonical replies

### Renderers

Renderers should:

- preserve `display` through queue mutations, snapshots, and playback state
- not perform catalog lookups themselves just to improve display metadata

---

## Open Questions

1. Should `library.getItem` allow the short `{ "itemId": "..." }` body when the topic already identifies the library, or should all new APIs require full refs everywhere?
2. Should `DisplayMetadata` stay a free-form map with recommended fields, or should the core fields become fully standardized in `pkg/mu`?
3. Should the printable canonical string form be standardized now, or left as an implementation detail for CLI UX?
4. Should playlist entries allow `resolved` blocks, or should playlists remain strictly catalog refs plus direct URL entries?

---

## Acceptance Criteria

This design is ready for implementation when:

1. Shared protocol types can model both legacy and new item/reference payloads.
2. At least one library can expose `getItem(s)` and `resolveSources*` without breaking old clients.
3. At least one controller can render queue/current state primarily from `display`.
4. At least one snapshot and playlist flow can preserve structured entries instead of only string item arrays.

