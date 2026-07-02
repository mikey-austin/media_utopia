# renderer_mpv: libmpv Renderer Module for mud

This document scopes `renderer_mpv`, a mud module driving playback through
libmpv via cgo. It replaces `renderer_gstreamer` as the native audio renderer,
reusing `renderer_core.Engine` and the existing module scaffolding verbatim —
only the Driver changes.

Companion document: `vala-renderer.md` scopes the alternative of a standalone
Vala/GStreamer daemon. This option was selected instead; see Rationale.

## Rationale

The instability in `renderer_gstreamer` was never Go — it was the shape of
the go-gst bridge. go-gst marshals GStreamer's entire refcounted object graph
across the cgo boundary: pipelines, elements, buses, and buffers are all
GObjects whose lifetimes Go's GC tracks with finalizers. Most of the 864
lines in `driver_gst.go` (serialized teardown queue, fade-job cancellation
choreography, watcher sequencing, seek debouncing) is defensive machinery
against that mismatch; the `gst_mini_object_unref` / `gst_buffer_resize_range`
CRITICALs are its symptoms.

libmpv presents a deliberately FFI-friendly surface instead:

- **One opaque handle.** The client holds a single `mpv_handle*`, pushes
  string/node commands in, and pulls copied event structs out. No shared
  object graph, no refcounting crosses the boundary. The bug class that
  motivated rewriting the renderer does not exist here.
- **FFmpeg stream handling.** mpv demuxes and decodes through FFmpeg, whose
  HTTP/HLS/ICY handling is substantially more battle-tested against broken
  internet streams than souphttpsrc + hlsdemux. Stability and compatibility
  with messy HTTP streaming media are the primary goals; this is where the
  option wins on merit rather than convenience.
- **Robust seeking.** `seek <pos> absolute` is safe to fire repeatedly; mpv
  coalesces internally. The 50 ms seek-debounce workaround (added because
  stacked flushing seeks corrupt the pipewiresink buffer pool) is deleted,
  not ported.
- **Everything else is already written.** `renderer_core.Engine` (queue,
  lease, publisher — ~2,500 lines) and the module layer
  (loadPlaylist/loadSnapshot, reply correlation, entry materialization,
  state debouncing, presence) carry over unchanged. This option is a driver
  swap, not a rewrite.

Costs accepted: one cgo module remains in mud (`libmpv` is a single dev
dependency versus the GStreamer dev stack; per-arch Docker builds avoid
cross-compilation anyway), and crossfade must be implemented in the driver
since mpv has none natively.

## Goals

- Drop-in replacement for `renderer_gstreamer`: same `renderer_core.Driver`
  contract, same module config shape, same MQTT surface. Existing node IDs
  are preserved via config so cutover is client-invisible.
- Markedly better tolerance of hostile HTTP streams (ICY radio, flaky HLS,
  redirect chains, servers with broken range support).
- Crossfade parity with the GStreamer module.
- Simpler driver: target ~200–300 lines versus 864, by deleting workarounds
  whose cause is gone rather than porting them.

## Non-Goals

- Video. Audio-only; the libmpv render API (where embedding complexity
  actually lives) is never touched.
- Replacing mud's process model. The module runs under the mud supervisor
  like every other module; one mud process can host multiple renderer
  instances as today. (Containerized one-renderer-per-process deployments
  remain possible by enabling only this module.)
- mpv JSON IPC. In-process libmpv avoids process supervision, socket
  lifecycle, and reconnect handling for no compatibility gain.

## Architecture

### Module layout

```
internal/modules/renderer_mpv/
  module.go        # ~copy of renderer_gstreamer/module.go, renamed
  driver_mpv.go    # //go:build mpv — libmpv driver
  driver_stub.go   # !mpv stub, mirrors existing pattern
  events.go        # EventEOS / EventError / EventWarning (+ EventAudioDown)
  module_test.go
```

`module.go` is intentionally a near-verbatim copy: it wires config, engine,
driver events, state publishing, and the loadPlaylist/loadSnapshot handlers
exactly as the GStreamer module does. Divergence is confined to the driver.

### Binding

Either the thin existing binding (`gen2brain/go-mpv`, a direct function
mapping) or a hand-rolled cgo file — the API surface used is roughly a dozen
functions: `mpv_create`, `mpv_set_option_string`, `mpv_initialize`,
`mpv_command` / `mpv_command_node`, `mpv_set_property`,
`mpv_observe_property`, `mpv_wait_event`, `mpv_set_wakeup_callback`,
`mpv_terminate_destroy`. Hand-rolling is preferred: it keeps the dependency
surface at zero, and the binding is small enough to own outright.

### Driver design

Each active track is one `mpv_handle` created with audio-only options:

```
vid=no  video=no  audio-display=no  terminal=no
ao=pipewire            (config: pipewire | alsa | pulse | ...)
audio-device=<device>  (optional)
cache=yes  demuxer-max-bytes=<n>  network-timeout=<s>
```

- **Event loop.** One goroutine per handle blocks on `mpv_wait_event` (no
  wakeup-callback → channel indirection needed when a dedicated goroutine
  owns the handle's event queue). It translates mpv events to driver events:
  `MPV_EVENT_END_FILE` with `MPV_END_FILE_REASON_EOF` → `EventEOS`; with
  `..._ERROR` → `EventError`; log-message events at warn level →
  `EventWarning`.
- **Position.** `mpv_observe_property` on `time-pos` and `duration`
  (`MPV_FORMAT_DOUBLE`) feeds cached values read by `Position()` — no
  polling calls across cgo on the hot path.
- **Commands.** `Play` = new handle + `loadfile <url>` (+
  `start=<positionMS>` option for resume); `Pause`/`Resume` = `pause`
  property; `SeekTo` = `seek <s> absolute`; `SetVolume`/`SetMute` = `volume`
  / `mute` properties. `Stop` / `Close` = `mpv_terminate_destroy`, which is
  synchronous and bounded — no serialized teardown queue, no shutdown
  budget machinery.
- **Volume scale.** mpv volume is 0–100 (softvol); the engine's 0.0–1.0
  scales linearly at the driver boundary. During crossfade, ramps apply to
  the handle-level `volume` property so the user-facing volume and fade
  gain compose multiplicatively.

### Crossfade

mpv has no native crossfade, so the driver ports the two-instance pattern:
on `Play` with crossfade configured, the outgoing handle keeps playing while
a fade goroutine ramps its `volume` property down over the window and the
incoming handle ramps up; pipewire mixes. Fade jobs are cancellable and any
Play/Stop/Close cancels all outstanding fades — this is the one piece of the
GStreamer driver's fade logic worth porting, minus the pipeline-pinning
concerns (a terminated handle releases its audio client node synchronously).
Two concurrent handles are safe by construction: each owns its threads and
its own AO connection.

### Audio backend health

The GStreamer driver's pipewire socket probe carries over in simplified
form: mpv surfaces AO failures as `MPV_EVENT_END_FILE`/error events, so
in-stream failures are already covered. A lightweight periodic probe of the
pipewire socket (only when `ao=pipewire`) is retained to distinguish "stream
ended" from "audio server gone" in published state — emitted as
`EventAudioDown`.

## Configuration

Same `Config` shape as `renderer_gstreamer` so mud TOML migrates by renaming
the section:

```toml
[modules.renderer_mpv.living_room]
node_id   = "mu:renderer:gstreamer:mud@livingroom:default"  # keep existing ID
name      = "Living Room"
ao        = "pipewire"        # replaces `pipeline`
device    = ""                # optional audio-device
crossfade = "3s"
volume    = 0.8
# mpv passthrough for stream tuning, applied as mpv options verbatim:
[modules.renderer_mpv.living_room.mpv_options]
network-timeout   = "10"
demuxer-max-bytes = "32MiB"
```

The `mpv_options` map is the escape hatch for stream-compatibility tuning
(user-agent, TLS options, cache sizing) without driver changes.

Preserving the existing `node_id` (even with `gstreamer` in the string) keeps
retained MQTT state, queue snapshots, and zone wiring intact across cutover.

## Build & Packaging

- `//go:build mpv` tag mirroring the existing `gstreamer` tag; stub driver
  otherwise. Dev dependency: `libmpv-dev` (one package).
- Runtime dependency: `libmpv2` + FFmpeg libs it pulls in. Debian-slim image
  delta is comparable to the GStreamer plugin set; no plugin curation needed
  since FFmpeg codec support is monolithic.
- Existing per-arch image builds are unaffected (no cross-compilation).

## Migration Plan

1. Land `renderer_mpv` behind its build tag alongside `renderer_gstreamer`.
2. Cut over zone by zone: rename the TOML section, keep the `node_id`,
   restart mud. Rollback is the reverse rename.
3. After stable runtime across all zones, delete
   `internal/modules/renderer_gstreamer`, the `gstreamer` build tag, and the
   go-gst dependency.

## Test Plan

- Unit: driver tested against the stub pattern as today; fade-job
  cancellation and volume-scale composition get dedicated tests.
- Integration: module tests mirror `renderer_gstreamer/module_test.go`
  (engine wiring, state publishing, load commands) — largely copied.
- Stream soak: a scripted pass over the real station/stream list (ICY
  metadata, HLS, redirects, servers without range support), asserting play,
  seek-where-supported, EOS, and error events. This is the acceptance
  gate, since stream compatibility is the point.

## Phases & Estimates

| Phase | Scope | Estimate |
| --- | --- | --- |
| 1. cgo binding | Hand-rolled libmpv binding (~dozen functions) | 3–5 h |
| 2. Driver | Handle lifecycle, event goroutine, property observation, command mapping | 6–9 h |
| 3. Crossfade | Dual-handle fades, cancellable fade jobs, volume composition | 6–10 h |
| 4. Module | Copy/adapt module.go, config, events, stub | 2–4 h |
| 5. Packaging | Build tag, Dockerfile delta, CI | 2–4 h |
| 6. Testing | Unit + integration + stream soak harness | 5–8 h |
| **Total** | | **~24–40 h** |

Versus ~64–106 h for the standalone Vala/GStreamer daemon, with the
stream-compatibility goal better served.

## Open Questions

- `gen2brain/go-mpv` versus hand-rolled binding (proposed: hand-rolled; the
  surface is small and owning it avoids a dependency on a thin wrapper).
- Whether `Pause` during crossfade pauses both handles or cancels the fade
  and pauses only the incoming track (proposed: cancel fade, keep incoming).
- Gapless: mpv supports `--gapless-audio` within one handle's playlist, but
  the engine advances the queue itself. Acceptable to defer; crossfade > 0
  masks the gap, and a prefetch-next-into-handle-playlist optimization can
  come later without protocol changes.
