# Protocol ID + Metadata Improvement Plan

> **For future implementation work:** treat this as a protocol cleanup and migration plan, not a one-shot rewrite. Land the wire-format changes behind compatibility shims first, then flip clients once both old and new forms coexist.

**Goal:** Fix two foundational protocol problems in Media Utopia v1: inconsistent item identity and expensive, late-bound metadata resolution. The system should use one canonical item reference model, stop depending on `lib:` wrapper parsing, and make queue/state display metadata available without requiring follow-up library lookups for ordinary UI rendering.

**Primary pain points observed in current code/docs:**
- The protocol claims canonical `mu:` IDs, but controllers and integrations heavily depend on `lib:<libraryNodeId>:<itemId>` shorthand and parse it structurally.
- Metadata is often missing from queue/state and has to be patched in later via `library.resolve` / `library.resolveBatch metadataOnly=true`.
- Snapshots and playlist-like objects persist lossy string item IDs instead of full queue entry references.
- Library resolution currently mixes two separate concerns: item description metadata and playable source resolution.

**Non-goal:** This plan does not redesign leases, MQTT topology, or playlist ownership semantics except where those areas are affected by item identity or metadata payload shape.

**Companion design spec:** `docs/superpowers/specs/2026-04-17-protocol-item-ref-design.md`.

---

## Problem summary

### 1. `lib:` is acting like a second ID scheme

The current system describes `mu:<kind>:<provider>:<namespace>:<resource>` as the canonical URN-like format, but runtime behavior depends on another grammar:

```text
lib:<libraryNodeId>:<itemId>
```

This has several problems:

- It is not opaque. Clients must know how many colons belong to the library node id and where the item id begins.
- It wraps one colon-delimited identifier inside another colon-delimited identifier.
- It is not a real canonical wire identity. It is a convenience string that leaked into state, queue, snapshots, playlists, and HA browse ids.
- It forces integrations to maintain fragile split/join helpers and special-case logic.

### 2. Metadata is resolved too late

Today a controller often gets queue entries or current state without enough display metadata, then has to issue a second request:

- `library.resolve` with `metadataOnly=true`
- `library.resolveBatch` with `metadataOnly=true`

This creates:

- extra round trips for common UI paths
- partially populated queue/state payloads
- duplicate caching/retry logic in controllers
- ambiguity over whether `library.resolve` is an identity API or a source-resolution API

### 3. Persisted queue-like objects are lossy

Snapshots and some playlist-related flows persist arrays of strings rather than structured queue entry references. That drops information needed to faithfully reconstruct:

- direct URLs
- library refs with embedded metadata
- already-resolved sources
- future per-entry fields

---

## Design goals

1. One canonical item reference model on the wire.
2. No protocol feature should require clients to parse nested colon grammars.
3. Metadata lookup and source resolution should be separate operations.
4. Queue/state should carry enough display metadata for normal UI rendering.
5. Persisted queue-like objects should preserve the same structure used in queue mutation APIs.
6. Migration must be incremental: new readers first, then new writers, then removal of legacy `lib:` strings.

---

## Proposed protocol direction

### 1. Replace stringly-typed `lib:` refs with structured item references

Introduce a canonical reference object for library-backed items:

```json
{
  "kind": "libraryItem",
  "libraryId": "mu:library:jellyfin:mud@home:default",
  "itemId": "track-123"
}
```

Use this object wherever the protocol currently sends or stores a library item as a magic string.

### Where it should appear

- `queue.set`
- `queue.add`
- `queue.get`
- `playlist.get`
- `playlist.addItems`
- `snapshot.save`
- `snapshot.get`
- renderer `current`
- any future browse/play command that accepts an item reference

### Why object form first

The object form avoids delimiter ambiguity entirely and is easier to evolve. It also makes JSON validation straightforward.

### 2. If a printable canonical string is still desired, use authority + path semantics

If Media Utopia wants a human-friendly canonical string for CLI use and copy/paste, prefer a form with a stable authority and slash-separated resource path instead of nested colon parsing.

Example direction:

```text
mu:library:jellyfin:mud@home:default/item/track-123
mu:library:upnp:mud@office:default/item/uuid::base64
mu:library:filesystem:mud@home:default/container/artist/radiohead
```

This preserves the useful `mu:` authority while moving resource-specific structure into a path segment. The protocol should still prefer structured JSON objects on the wire; the string form is mainly for CLI and logs.

### 3. Split metadata description from source resolution

Replace the current overloaded meaning of `library.resolve` with two distinct API roles:

- `library.getItem`
  Returns canonical item description and display metadata only.
- `library.getItems`
  Batch form of `library.getItem`.
- `library.resolveSources`
  Returns playable sources for one item.
- `library.resolveSourcesBatch`
  Batch form when needed.

### `library.getItem` reply shape

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
    "durationMs": 322000
  },
  "attributes": {
    "mediaType": "audio",
    "container": false
  }
}
```

### `library.resolveSources` reply shape

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

This separation makes the contract obvious:

- browse/search/getItem are catalog APIs
- resolveSources is a playback preparation API

### 4. Add a display snapshot to queue entries and current state

Queue entries should carry a normalized display block suitable for UI rendering:

```json
{
  "queueEntryId": "mu:queueentry:renderer:abc:123",
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
    "durationMs": 322000
  }
}
```

This display block is a denormalized snapshot, not the source of truth. Libraries still own catalog truth. But renderer state should be usable without immediately calling back into the library for normal rendering.

### Rules for `display`

- Writers SHOULD include it when known.
- Renderers MAY preserve it across queue mutations and snapshots.
- Controllers MAY refresh it via `library.getItem(s)` if needed.
- Missing `display` is allowed only during migration or for opaque URL-only entries.

### 5. Preserve full queue entry semantics in snapshots and playlists

Replace lossy string item arrays with the same entry structure used by queue commands.

### Snapshot shape

Current direction:

```json
{
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

Do the same for playlist entries where practical. Avoid parallel string-only storage formats for queue-like data.

---

## Migration plan

### Phase 0: Lock the new contract in docs

- Add a new protocol design doc for item references and metadata APIs.
- Mark `lib:` as legacy shorthand, not canonical protocol identity.
- Document `library.resolve` / `resolveBatch` as legacy compatibility APIs once replacement commands exist.

**Exit criteria:**
- New canonical item reference object is documented.
- New metadata/source API split is documented.
- All new examples use object refs, not `lib:`.

### Phase 1: Introduce new wire types alongside existing ones

Add new shared protocol types:

- `LibraryItemRef`
- `DisplayMetadata`
- updated `QueueEntry`
- new `library.getItem(s)` and `library.resolveSources*` bodies/replies

Compatibility rules:

- Readers accept both legacy `itemId: "lib:..."` and new structured refs.
- Writers continue emitting old fields until all major clients can read new ones.

**Exit criteria:**
- Shared protocol package can encode/decode both old and new shapes.
- No client breaks if a payload includes both old and new fields.

### Phase 2: Update libraries to provide split metadata/source APIs

For each library module:

- implement `library.getItem`
- implement `library.getItems`
- implement `library.resolveSources`
- keep `library.resolve` as a compatibility wrapper

Compatibility behavior:

- `library.resolve(metadataOnly=true)` delegates to `library.getItem`
- `library.resolve(metadataOnly=false)` composes `getItem + resolveSources`
- `library.resolveBatch` does the same for batch mode

**Exit criteria:**
- HA and CLI can use the new APIs against at least filesystem, Jellyfin, UPnP, and podcast libraries.
- Legacy resolve commands still work.

### Phase 3: Update renderer queue/state payloads

Renderers should:

- accept structured refs in queue mutations
- preserve `display` data in queue entries
- return `display` in `queue.get`
- include `display` in current item state

Renderer fallback:

- if only legacy `itemId` is present, preserve it during migration
- if `display` is missing, pass through empty rather than inventing library calls inside the renderer

**Exit criteria:**
- Queue browsers no longer need a metadata lookup round trip for ordinary rendering.
- Current track title/artist/album/artwork are present in state after queue load for library-backed items.

### Phase 4: Update controllers and integrations

### CLI

- accept object refs internally
- keep accepting `lib:` and shorthand strings as input syntax only
- convert CLI shorthand to canonical refs before publish

### Home Assistant

- stop treating `lib:` as the primary queue item identity
- prefer `display` from queue/state
- use `library.getItems` only for refresh or cache misses
- retain legacy parsing only as a compatibility path

### Android / other controllers

- same pattern: use `display` first, use catalog APIs only for refresh/details

**Exit criteria:**
- HA metadata patch-up logic is no longer on the hot path for queue rendering.
- Controllers can render queue/current state directly from retained renderer state plus `queue.get`.

### Phase 5: Migrate persisted objects

Update playlist and snapshot storage to write structured entries, not string item arrays.

Migration approach:

- readers accept old snapshots/playlists with string `items`
- writers emit new `entries`
- background migration or read-rewrite migration can convert stored data over time

**Exit criteria:**
- New snapshots and playlist edits preserve direct URLs, refs, and display metadata.
- Old saved objects still load.

### Phase 6: Deprecate legacy fields

After all major clients and libraries are upgraded:

- stop emitting `lib:` in protocol state
- remove `metadataOnly` from the primary happy path
- deprecate string-only snapshot item arrays
- keep `lib:` parsing in CLI as optional user-facing shorthand if it remains useful

**Exit criteria:**
- `lib:` exists only as optional CLI input sugar, not protocol state.
- Protocol examples, docs, and emitted payloads all use canonical refs.

---

## Concrete spec changes to make

### Shared types

- Add `LibraryItemRef`
- Add `DisplayMetadata`
- Replace `ItemRef.ID string` with a richer structure, or add a new `Ref` variant and deprecate the old one
- Update `QueueItem`, `QueueEntry`, renderer `current`, snapshot entry shapes, and playlist entry shapes

### Library commands

Add:

- `library.getItem`
- `library.getItems`
- `library.resolveSources`
- `library.resolveSourcesBatch`

Deprecate:

- `library.resolve.metadataOnly`
- `library.resolveBatch.metadataOnly`

### Queue commands

Update:

- `queue.set`
- `queue.add`
- `queue.get`

To use:

- structured `ref`
- optional `display`
- existing `resolved` source blocks for URL-backed entries

### Snapshot / playlist commands

Update:

- `snapshot.save`
- `snapshot.get`
- `playlist.get`
- `playlist.addItems`
- `playlist.replaceItems`

To preserve:

- refs
- resolved entries
- display metadata snapshots

---

## Risks

### 1. Dual-format complexity during migration

For a period, readers will need to accept:

- legacy `lib:` strings
- legacy `itemId` fields
- new structured refs
- legacy snapshots/playlists with string arrays

This is manageable, but the compatibility window should be intentionally short.

### 2. Metadata staleness

Denormalized `display` metadata can drift from library truth. That is acceptable if the contract is explicit:

- queue/state display is a snapshot for UX
- library catalog remains canonical

### 3. Provider-specific item ids may still contain delimiters

That is fine if they live inside `itemId` as opaque data. The whole point of the structured model is to stop delimiter collisions from mattering.

### 4. Backward compatibility pressure may tempt the project to keep both forever

Avoid that. Set a clear deprecation target once HA, CLI, and the main libraries are migrated.

---

## Recommended implementation order

1. Land docs and shared protocol types.
2. Add new library APIs with compatibility wrappers.
3. Update HA bridge to read new refs and prefer `display`.
4. Update renderer queue/state payloads to preserve and emit `display`.
5. Update snapshot/playlist persistence.
6. Flip CLI and examples to the new canonical form.
7. Remove legacy protocol emission.

---

## Acceptance criteria

The protocol cleanup is successful when all of the following are true:

1. No retained renderer state or queue payload emitted by current components requires parsing a `lib:` string.
2. Queue UIs can render title/artist/album/artwork without doing immediate follow-up metadata lookups in the common case.
3. `library.getItem(s)` and `library.resolveSources*` are distinct APIs with distinct responsibilities.
4. Snapshots and playlists preserve full queue-entry fidelity, including direct URLs and display metadata.
5. `lib:` remains, at most, a CLI input shorthand and compatibility parser, not canonical protocol identity.

---

## Suggested follow-up docs

- `docs/superpowers/specs/<date>-protocol-item-ref-design.md`
- update `docs/spec/overview.md`
- update `docs/spec/messages.md`
- update `docs/spec/cli.md`
- update `docs/design/ha-integration.md`
