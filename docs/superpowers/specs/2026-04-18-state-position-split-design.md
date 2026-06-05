# Renderer State / Position Topic Split Design

**Date:** 2026-04-18
**Scope:** renderer publish topology, `pkg/mu` topic helpers, all renderer modules, all controllers (HA bridge + panel, Android, desktop, applet, CLI watch)
**Status:** draft design, post-protocol-reset

## Context

Today every renderer publishes a full state JSON to `mu/v1/node/<rendererId>/state`
~1 Hz while playing, just to advance `playback.positionMs`. The payload is the
complete `RendererState` (~600–900 bytes including session, queue, current,
display, ref, resolved). At rest with 4 renderers and 2 active controllers
that's roughly:

> 4 renderers × ~700 B/state × 1 Hz × (broker fan-out + per-WS-subscriber
> fan-out) ≈ tens of KB/s on the wire and a steady stream of recompositions on
> every controller, *every second*, almost all of which carry zero new
> information.

Real-world impact:

- **Mobile over 5G + VPN.** Every position tick crosses the cell network,
  passes through the Wireguard tunnel, lands on the phone, wakes the radio
  briefly. Battery + data both suffer.
- **HA panel.** The frontend dedup filter at `mu-panel.js` rejects ~95 % of
  these events as "no meaningful change". The remaining 5 % still trigger Lit
  recompositions. The dedup filter exists *only* because the wire format
  conflates two very different update rates.
- **Android renderer state flow.** The `RendererStateRepository` SharedFlow
  buffer sizing problem we just fixed (commit `b701633`) only existed because
  the producer rate is 1 Hz and the consumer is Compose recomposition. With
  position split out, the state stream slows to "actual changes only" and
  buffer pressure disappears.
- **Snapcast / multi-renderer fan-out.** Each renderer is independent today;
  scaling to 8–16 zones makes the constant-rate state flood worse linearly.

The protocol-reset spec (`2026-04-17-protocol-item-ref-design.md`) made the
state payload self-sufficient (display + ref inline) — there is no longer any
reason for state events to fire on a clock.

## Goals

- State events fire **only** on meaningful change (status, queue revision,
  current entry, lease, volume/mute, seek discontinuity).
- Position is published on a separate, lightweight, non-retained topic at
  ~1 Hz while playing.
- Subscribers that don't care about position (lock-screen, MPRIS now-playing,
  scrobbler, dashboards) can subscribe only to `state` and never see a
  position tick.
- Subscribers that do care (now-playing UI seek bar) subscribe to both and
  interpolate locally between position ticks.
- Wire payload reduction of ~10× per tick. Total bandwidth reduction of more
  than that because we drop the per-tick state event entirely while playing.
- Cold start still works: a new subscriber joins, immediately sees retained
  state with the position-at-last-meaningful-change, then gets corrected by
  the next position tick within ≤1 s.

## Non-goals

- Variable position cadence based on subscriber count (sweat the simple thing
  first).
- Server-side interpolation. Clients do their own clock-based extrapolation
  between position events; renderers are the source of truth on every tick.
- Per-controller dedup beyond what falls out of the split. The HA-side
  signature dedup in `ws_subscribe_renderer_state` becomes redundant and is
  removed.
- A breaking refactor of `RendererState`. The struct stays as-is; what
  changes is *when* it's published and via which topic.

## Design

### Topics

| Topic | Retain | QoS | Cadence | Purpose |
|---|---|---|---|---|
| `mu/v1/node/<rendererId>/presence` | yes | 1 | on online/offline | (unchanged) |
| `mu/v1/node/<rendererId>/state` | **yes** | 0 | on meaningful change | full `RendererState` |
| `mu/v1/node/<rendererId>/position` | **no** | 0 | ~1 Hz while playing | tiny `{positionMs, ts}` |

### Payloads

**`state`** — unchanged shape, see `pkg/mu` `RendererState`. The
`playback.positionMs` and `playback.durationMs` fields are kept; they
represent the snapshot at the moment this state event was published. Late
joiners interpolate from `(state.playback.positionMs, state.ts)` until the
next position event arrives.

**`position`** — new, deliberately minimal:

```json
{
  "positionMs": 12345,
  "ts": 1776537890
}
```

Just two fields. No `playback.status`, no `durationMs`, no `current`. Those
all live on `state` and don't change between position ticks. Subscribers
that need them must also be subscribed to `state` — and any UI that draws a
seek bar already needs `current` and `durationMs` anyway.

A new Go type in `pkg/mu`:

```go
type PositionUpdate struct {
    PositionMs int64 `json:"positionMs"`
    TS         int64 `json:"ts"`
}
```

and a topic helper:

```go
func TopicPosition(base, nodeID string) string { ... }
```

### Renderer publishing rules

`internal/modules/renderer_core/engine.go` is the single owner of all
renderer state changes. The split lives there.

**State events** are published on:

- session acquire / renew / release / takeover
- queue mutation (queue.add / remove / move / clear / set / setShuffle / setRepeat / shuffle)
- queue index change (jump / next / prev / auto-advance on EOS)
- current entry change (track transition)
- volume / mute change
- playback status transition (playing ↔ paused ↔ stopped, plus loading→playing)
- seek (treat any position discontinuity > 2 s as a state-worthy event so
  late position-only subscribers don't drift on seek)
- on initial publish at module start

State events include a fresh `playback.positionMs` snapshot so any subscriber
joining immediately after the event has a sensible interpolation anchor.

**Position events** are published by a 1 Hz ticker that is started when
`playback.status` becomes `playing` and stopped when it leaves `playing`.
The ticker reads the driver's current position and publishes a `PositionUpdate`.
On pause/stop the ticker stops; the seek-bar UI freezes naturally because
clients only interpolate while their last-known status is `playing`.

The existing `tickStateEvery1s` style of code in each renderer module is
deleted.

### Lease publication semantics (unchanged)

Leases continue to ride state events. Lease take/release/renew are all
"meaningful changes" and trigger a state publish.

### Subscription rules

#### HA bridge (`integrations/home_assistant/custom_components/mu/bridge.py`)

- `_on_state` keeps current behavior, minus the dedup-by-signature inside
  the WS subscribe handler (signature filter becomes redundant; renderer
  only emits on real change).
- New `_on_position` handler attached to the `position` topic. Updates a
  per-renderer `_renderers[node_id]["position"]` snapshot, fans out to a
  small set of position-only listeners.
- New WS endpoint `mu/subscribe_renderer_position` that forwards
  `{positionMs, ts}` events. The panel uses this in addition to
  `mu/subscribe_renderer_state`.
- `mu/renderer_state` continues to return the full snapshot (state +
  current position). The pull endpoint composes them server-side so the
  panel never needs to make two HTTP-style WS calls.

#### HA panel (`mu-panel.js`)

- The `_onRendererStateEvent` filter that dedupes 1 Hz floods is removed.
  Every state event now causes a render — they are rare and meaningful.
- The local position interpolator (`_interpolatedPos` driven by
  `requestAnimationFrame`) keeps doing its job; it now syncs from a
  smaller, fresher position event rather than the full state.
- Same event-driven `_loadQueue` on revision-change behavior (now actually
  reliable since `_maybe_resolve_metadata` no longer eats the dispatch —
  see commit `045d3f8`).

#### Android (`RendererStateRepository.kt`, `NowPlayingViewModel.kt`)

- `RendererStateRepository.observeState(nodeId)` keeps its current contract
  (full `RendererState`).
- New `RendererStateRepository.observePosition(nodeId)` returns a
  lightweight `MutableSharedFlow<PositionUpdate>` keyed by node, mirroring
  the state observer pattern.
- `NowPlayingViewModel` subscribes to both. The existing `startTicker`
  interpolator stays; it just rebases on a smaller event.
- The 64-slot `DROP_OLDEST` buffer on the state flow stays (defense in
  depth), but the production rate drops from ~1 Hz per renderer to "events
  per minute" in steady state, so it'll almost never be exercised.
- `MqttForegroundService` adds a position subscription topic alongside
  the state one. When the app is in background it can choose to drop the
  position subscription entirely to save battery — design the API so
  this is an explicit choice (`observePositionWhileForeground` vs.
  `observePosition`).

#### Desktop (`renderer_state_repo.vala`)

- Same shape as Android: separate `state` and `position` signals.
- MPRIS adapters care only about state; they unsubscribe from position.

#### CLI watch (`cmd/mu/watch.go` if it exists, otherwise the place that
streams state) — same: subscribe to both, render position from the lighter
stream.

### Cold-start & late-join behavior

- New subscriber connects, gets the retained `state` immediately. State
  contains a `playback.positionMs` snapshot taken when the state was last
  published.
- If status is `playing`, the subscriber begins interpolating from
  `(state.playback.positionMs, state.ts, now())`.
- The next `position` event arrives within ≤1 s and snaps the local
  estimate to the truth. Worst-case visible drift is ~1 s, which is below
  the seek-bar's pixel-per-second resolution at typical track lengths.
- If status is `paused` or `stopped`, no position events are published.
  Subscriber holds `state.playback.positionMs` indefinitely. Correct.

### Edge cases

- **Seek.** Renderer publishes a state event (because position discontinuity
  is treated as meaningful) and emits the next position tick immediately
  rather than waiting up to 1 s. UI snaps to the seek target.
- **Track change.** State event fires (current changed). Position resets to
  0 in that state event. The 1 Hz ticker continues from there.
- **Renderer restart.** Retained state is whatever was last published before
  the crash. New position events resume after replay. Subscribers see a
  stale position briefly, then correction within 1 s.
- **Network partition (mobile entering tunnel).** Position events are
  non-retained, so on reconnect the subscriber gets the retained state and
  resumes interpolating until the next live position event. No catch-up
  storm.
- **Rapid seek + pause.** Edge between "playing ticker should keep firing"
  and "status just changed to paused so stop the ticker" — engine code
  must atomically gate ticker on status under the same lock used for state
  publish, otherwise we can publish a position event after a paused state
  event. The renderer engine already serializes lease/state under one mutex;
  ticker stop happens under that same critical section.

## `pkg/mu` API additions

```go
// In pkg/mu/protocol.go
const PositionTopicSuffix = "position"

func TopicPosition(base, nodeID string) string

// In pkg/mu/bodies.go (or a new file)
type PositionUpdate struct {
    PositionMs int64 `json:"positionMs"`
    TS         int64 `json:"ts"`
}
```

The state struct itself does not change. Just the topic and cadence.

## Migration

Lockstep deploy. The position topic is purely additive; the state topic's
shape is unchanged. The only breaking-ish thing is "state no longer fires
1 Hz" — controllers that depend on state events to drive a position UI need
to also subscribe to position before deploying renderers that stop the tick.

Order:

1. `pkg/mu` — add `PositionUpdate` + `TopicPosition`.
2. `internal/modules/renderer_core/engine.go` — add position-only ticker,
   gate state events on real change. Keep current behavior conditional on
   a build flag during initial rollout? *No — single-user system, just cut
   over.*
3. All four renderer modules — wire the new ticker.
4. HA bridge + panel — subscribe to both topics; remove WS-side dedup.
5. Android, desktop, applet — subscribe to both topics; remove their own
   dedup filters where present.

## Verification

End-to-end checks per layer:

### `pkg/mu`
- Unit tests for `TopicPosition`.
- JSON round-trip for `PositionUpdate`.

### `mud`
- Subscribe to `mu/v1/node/<r>/state` and verify it stays silent for >5 s
  while playback is steady.
- Subscribe to `mu/v1/node/<r>/position` and verify ~1 Hz cadence with
  monotonically increasing `positionMs`.
- On pause: state event fires once, position events stop.
- On seek: state event fires once with new `positionMs`; position event
  fires immediately after.
- On track change: state event fires once with new `current`; position
  event fires immediately at low value.

### HA
- Hard-reload panel; the now-playing seek bar updates smoothly with the
  new lightweight position events.
- `docker logs media_utopia_ha` shows no per-second state-publish traffic
  attributed to mu.
- `mosquitto_sub -t 'mu/v1/#' -v` shows position frames are tiny and state
  frames are rare.

### Android
- Logcat: `RendererStateRepository: State for ...` lines fire only on real
  events (track change, lease, queue mutation), no longer ~1 Hz.
- `RendererStateRepository: Position for ...` (new debug log) fires ~1 Hz
  while playing.
- Mobile data tracking before/after: roughly 10× drop while listening.

### Acceptance
- Total MQTT bytes/sec attributable to `mu/v1/node/+/state` over a 60 s
  window of steady playback drops by ≥ 90 %.
- Total MQTT bytes/sec attributable to `mu/v1/node/+/position` over the
  same window is < 10 % of the old state byte rate.
- Now-playing seek bar visible smoothness on Android over 5G + VPN is
  unchanged or better (subjective; record a screen capture before/after).
- Lock-screen / scrobbler / MPRIS update times do not regress: still
  driven by state events on real change.

## Open questions

1. **Should the renderer publish the *first* position event immediately when
   transitioning into `playing`, or wait for the 1 s tick?** Probably yes —
   immediate first emit gives subscribers a sub-1 s sync after track start.
2. **Should we drop `playback.positionMs` from `state` entirely once
   `position` exists?** No — keeping it in state makes late-joiner
   interpolation correct without requiring a position subscription.
3. **Does `position` deserve a `playback.status` mirror for self-contained
   "is this still playing?" checks?** Probably not — subscribers must already
   own state to know what's playing. Keep position as minimal as possible.
4. **Per-renderer cadence config?** Skip — 1 Hz everywhere is fine and
   keeps the ticker dumb.
5. **Should the ticker fire at half-second resolution to give nicer
   sub-second seek-bar accuracy?** No, local interpolation handles this.
   1 Hz network rate, 60 fps render rate.
