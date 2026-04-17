# Protocol Reset Breaking Checklist

> **Assumption:** all Media Utopia components can be updated and deployed in lockstep. This checklist intentionally ignores backward compatibility and migration shims. Deploy in this order: `pkg/mu` + `mud`, then Home Assistant, then Android app, then desktop app.

**Goal:** perform a clean protocol reset for item identity and metadata handling:

- remove `lib:` from the protocol
- make structured library item refs mandatory on the wire
- split metadata and source resolution APIs
- require queue/state/snapshot/playlist payloads to carry `display`

**Companion docs:**
- [2026-04-17-protocol-id-and-metadata-improvement-plan.md](/home/mikey/Workspace/media_utopia/docs/superpowers/plans/2026-04-17-protocol-id-and-metadata-improvement-plan.md)
- [2026-04-17-protocol-item-ref-design.md](/home/mikey/Workspace/media_utopia/docs/superpowers/specs/2026-04-17-protocol-item-ref-design.md)

---

## Reset rules

These are the protocol rules after the reset. Every component in this checklist should assume they are true.

1. No emitted payload may use `lib:<libraryId>:<itemId>` as canonical identity.
2. Library-backed media must be represented as a structured ref object.
3. `library.resolve(metadataOnly=...)` and `library.resolveBatch(metadataOnly=...)` are removed from the primary protocol surface.
4. Queue entries, current item state, snapshot entries, and playlist entries must include `display` whenever known.
5. Snapshots and playlists store structured entries, not string arrays of item ids.

---

## Canonical payload decisions

### Library ref

```json
{
  "kind": "libraryItem",
  "libraryId": "mu:library:jellyfin:mud@home:default",
  "itemId": "track-123"
}
```

### Display block

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

### Queue entry

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

---

## 1. `pkg/mu` checklist

This is the contract layer. Nothing else should move until this is settled.

- [ ] Add `LibraryItemRef` type.
- [ ] Add `DisplayMetadata` type.
- [ ] Replace the old string-only `ItemRef.ID` model with a structured library ref payload.
- [ ] Update `QueueEntry` to carry:
  - `Ref *LibraryItemRef`
  - `Resolved *ResolvedSource`
  - `Display *DisplayMetadata`
- [ ] Update `QueueItem` / `CurrentItemState` / any renderer state structs to use `Ref` + `Resolved` + `Display`.
- [ ] Remove protocol reliance on `itemId string` for queue/current item identity.
- [ ] Add request/reply body structs for:
  - `library.getItem`
  - `library.getItems`
  - `library.resolveSources`
  - `library.resolveSourcesBatch`
- [ ] Remove `MetadataOnly` from the primary request structs.
- [ ] Update snapshot structs to store `Entries []QueueEntry` instead of `Items []string`.
- [ ] Update playlist structs to store structured entries rather than item-id strings where applicable.
- [ ] Update protocol validation to reject old `lib:`-based canonical wire payloads.
- [ ] Update protocol tests to assert only structured refs are valid.

**Definition of done for `pkg/mu`:**
- all shared types compile
- no shared type requires `lib:` parsing
- no primary command body uses `metadataOnly`

---

## 2. `mud` core and module checklist

This covers command handlers, renderer state, library behavior, persistence, and tests.

### 2.1 Core service layer

- [ ] Update `internal/core/service.go` to construct structured refs for queue, playlist, snapshot, and library calls.
- [ ] Remove service-layer generation or acceptance of `lib:` canonical payloads.
- [ ] Update queue-related service methods to pass `display` when known.
- [ ] Change any snapshot-save flow to read/write structured queue entries.

### 2.2 Renderer modules

Applies to `renderer_core`, `renderer_gstreamer`, `renderer_kodi`, `renderer_vlc`, `renderer_upnp`, Android local renderer if shared contract is mirrored.

- [ ] Update queue mutation handlers to accept the new structured entry shape.
- [ ] Preserve `display` across:
  - `queue.set`
  - `queue.add`
  - `queue.move`
  - `queue.shuffle`
  - snapshot load/save
- [ ] Update `queue.get` replies to emit structured refs and `display`.
- [ ] Update retained renderer state `current` payload to emit `ref`, `resolved`, and `display`.
- [ ] Remove logic that assumes current item identity is a string `itemId`.
- [ ] Ensure renderers do not perform catalog metadata lookups to fill missing `display`.
- [ ] Update event payloads if they still refer to string item IDs as the canonical current-item identity.

### 2.3 Library modules

Applies to `fs_library`, `jellyfin_library`, `upnp_library`, `podcast_library`, `go2rtc_library`, and any others serving media catalog items.

- [ ] Add `library.getItem`.
- [ ] Add `library.getItems`.
- [ ] Add `library.resolveSources`.
- [ ] Add `library.resolveSourcesBatch`.
- [ ] Make `library.getItem(s)` return canonical structured refs in replies.
- [ ] Make source-resolution replies return sources only, not overloaded metadata blobs.
- [ ] Remove `metadataOnly`-driven behavior from the main path.
- [ ] Update browse/search replies so returned items can be turned directly into structured refs.
- [ ] Normalize `display` field production across libraries:
  - title
  - artist
  - album
  - artworkUrl
  - durationMs
  - mediaType when known

### 2.4 Playlist module

- [ ] Update stored playlist entry model to preserve structured queue-like entries.
- [ ] Update `playlist.get` replies to emit structured entries with `display`.
- [ ] Update `playlist.addItems` to accept structured refs and direct resolved entries.
- [ ] Update `playlist.replaceItems` to stop using string item arrays as the primary model.
- [ ] Ensure playlist create/load flows preserve `display`.

### 2.5 Snapshot module behavior

- [ ] Update `snapshot.save` to store `entries`, not `items`.
- [ ] Update `snapshot.get` and `snapshot.list` related flows to expose the new entry structure.
- [ ] Update renderer snapshot load behavior to restore `display` and `resolved` data.

### 2.6 Tests and docs in `mud`

- [ ] Rewrite integration tests that publish or expect `lib:` payloads.
- [ ] Rewrite module tests for library resolve behavior.
- [ ] Rewrite queue/snapshot/playlist tests for structured entry persistence.
- [ ] Update `docs/spec/messages.md`, `docs/spec/overview.md`, and `docs/spec/cli.md` after implementation is stable.

**Definition of done for `mud`:**
- broker-visible payloads use only structured refs
- `queue.get` and renderer state contain enough `display` for clients to render immediately
- library modules expose split metadata/source commands
- snapshots and playlists preserve full entry fidelity

---

## 3. Home Assistant checklist

The HA bridge is where most of the current pain lives, so this should get much simpler after the reset.

### 3.1 Bridge protocol changes

- [ ] Remove `_split_lib_ref` and all logic that depends on `lib:` parsing.
- [ ] Remove `lib:` as the queue/current item identity model in the bridge.
- [ ] Replace metadata fetch calls from `library.resolve(metadataOnly=true)` to `library.getItem(s)` only where needed.
- [ ] Prefer `display` from renderer state and `queue.get` over library follow-up requests.
- [ ] Keep source-resolution calls separate if HA needs to originate enqueue/play actions from library items.

### 3.2 Queue and current item handling

- [ ] Update queue browse logic to read:
  - `entry.ref`
  - `entry.display`
  - `entry.resolved`
- [ ] Update now-playing logic to read `current.display` first.
- [ ] Remove “metadata patch-up” as the common path for queue rendering.
- [ ] Keep metadata refresh only for explicit refresh/details behavior if desired.

### 3.3 Playlist and snapshot handling

- [ ] Update playlist browse UI to consume structured entries rather than item-id strings.
- [ ] Update snapshot browse UI to consume structured entries rather than item-id strings.
- [ ] Update save/load flows to send/receive `entries`.

### 3.4 WebSocket API

- [ ] Update WS payloads returned to the MU panel so they expose structured refs and `display`.
- [ ] Remove any panel assumptions that queue items are represented by `lib:` strings.

### 3.5 Tests

- [ ] Rewrite bridge helper tests that currently verify `lib:` splitting behavior.
- [ ] Add tests asserting HA can render queue/current state from `display` with no library round trip.
- [ ] Update panel and websocket tests to the new shapes.

**Definition of done for HA:**
- no HA code parses `lib:`
- queue browser and now playing render primarily from renderer-emitted `display`
- metadata fetches are no longer on the hot path for ordinary UI

---

## 4. Android app checklist

The Android app should become more renderer-state-driven and less dependent on metadata reconciliation.

### 4.1 Protocol layer

- [ ] Update protocol models for structured refs, `display`, and split library commands.
- [ ] Remove assumptions that item identity is a string `itemId` alone.
- [ ] Update serialization/deserialization for queue/current/snapshot/playlist payloads.

### 4.2 Library client behavior

- [ ] Replace `library.resolve(metadataOnly=true)` usage with `library.getItem(s)` where metadata lookup is still needed.
- [ ] Use `resolveSources*` only when preparing playback sources.

### 4.3 Queue and now-playing UI

- [ ] Render queue rows from `display` first.
- [ ] Render now-playing metadata from renderer state `current.display`.
- [ ] Stop depending on a second metadata fetch to populate common views.
- [ ] Preserve richer display fields in local caches if the app snapshots queue state.

### 4.4 Local renderer

- [ ] Update local renderer queue model to preserve `display`.
- [ ] Update local renderer state emission to publish `ref`, `resolved`, and `display`.
- [ ] Update local snapshot/queue persistence to use structured entries.

### 4.5 Tests / verification

- [ ] Update protocol parsing tests if any exist.
- [ ] Manually verify queue and current metadata are correct without metadata refresh traffic.
- [ ] Verify local renderer and remote renderer both surface the same shape.

**Definition of done for Android:**
- app queue/current UI renders correctly from retained state plus `queue.get`
- local renderer emits the same structured contract as Go renderers
- no client path depends on `lib:` parsing

---

## 5. Desktop app checklist

If the desktop app is not yet fully implemented, treat this checklist as the target contract from day one.

### 5.1 Protocol models

- [ ] Model structured refs directly in the desktop protocol layer.
- [ ] Model `display` and split library commands directly.
- [ ] Do not implement `lib:` parsing except optional CLI/input sugar if needed.

### 5.2 UI behavior

- [ ] Render queue and now-playing metadata from `display`.
- [ ] Use `library.getItem(s)` only for explicit detail refresh.
- [ ] Use `resolveSources*` only for playback preparation flows.

### 5.3 Local desktop renderer

- [ ] Emit `ref`, `resolved`, and `display` in state and queue APIs.
- [ ] Preserve `display` in local queue persistence and snapshot/playlist interactions.

**Definition of done for desktop:**
- desktop implementation ships directly against the post-reset protocol with no legacy branches

---

## 6. CLI checklist

The CLI can still offer shorthand input, but it should not publish shorthand as protocol identity.

- [ ] Change internal item argument parsing to resolve user input into structured refs before publish.
- [ ] Keep `lib:<selector>:<itemId>` only as an input convenience if still useful.
- [ ] Update human output to read `display`.
- [ ] Update JSON output to expose structured refs and `display`.
- [ ] Update queue, playlist, snapshot, and library commands to the split API model.
- [ ] Remove documentation that implies `lib:` is canonical.

**Definition of done for CLI:**
- protocol publishes structured refs only
- user shorthand stays a UX concern, not a wire concern

---

## 7. Deployment order

Given the explicit lockstep deployment model, deploy in this order:

1. `pkg/mu`
2. `mud`
3. Home Assistant integration
4. Android app
5. Desktop app
6. CLI release if separate from `mud`

Reason:

- `mud` defines the broker-visible contract.
- HA and Android are the current high-value controllers that need the new payloads immediately.
- Desktop should ship only after the server contract is stable.

---

## 8. Hard cut removals

These should be deleted, not deprecated, in the lockstep reset.

- [ ] Canonical `lib:` identity in protocol payloads
- [ ] `metadataOnly` request flag
- [ ] queue/current rendering code that depends on string item ids as the primary identity
- [ ] snapshot `items []string` as the primary storage model
- [ ] playlist replace/add flows that collapse structured entries into plain item-id strings

---

## 9. Final acceptance check

The reset is complete when all of the following are true:

1. A queue entry can be copied from renderer state, snapshot storage, or playlist storage without losing identity or display meaning.
2. HA and Android can render queue/current state immediately from `display`.
3. Libraries expose separate metadata and source-resolution commands.
4. No running component needs `lib:` parsing for normal operation.
5. No emitted payload treats `metadataOnly` as part of the protocol contract.

