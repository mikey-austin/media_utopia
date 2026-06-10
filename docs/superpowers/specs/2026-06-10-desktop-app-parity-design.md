# Desktop App Parity Design — bring desktop in line with (or better than) Android

**Date:** 2026-06-10
**Scope:** `integrations/desktop_app` (GTK4 + libadwaita + Vala)
**Reference:** `integrations/android_app` (Jetpack Compose, Material 3, "Sonic Curator" design)

## Goal

The Android app's layout, functionality and feel are the accepted reference. The
desktop app has all six screens implemented and a solid service layer (MQTT,
correlator, leases, repositories, local GStreamer renderer) that already mirrors
the Android architecture. The gap is interaction polish, missing UX states, a
handful of real bugs, and three screens (Library, Queue, Zones) lagging the
Android design. This is a refresh in place, not a rewrite.

## Approach

**Approach A — incremental in-place refresh** (chosen over a UI-layer rewrite and
a full rewrite): keep the service layer untouched, rework the UI screen by
screen in phases. The app stays buildable and usable after every phase. The one
structural cleanup folded in: decompose `library_view.vala` (1,856 lines) while
overhauling that screen.

## Gap analysis (desktop vs Android reference)

| Area | Android | Desktop today |
|---|---|---|
| Now Playing | ~100ms position ticker; lease-blocked UX; slide-up routing panel; HiRes badge | Position jumps on MQTT messages only; no lease messaging; flat zone list; badge stubbed |
| Library | Album grid; mixed-mode bulk bar; auto load-more; per-track menu | List-only; manual Load More button; monolithic view file |
| Queue | Drag-reorder, swipe-remove, optimistic, lease read-only | Reorder half-wired; remove button only |
| Renderers | Lease actions; scanning pulse; active renderer in header | Selection only |
| Zones | Master volume; ZONES/SOURCES tabs; collapsible cards; assignment | Flat cards; source selector stubbed |
| States | Loading/error/empty everywhere | Spinner freezes on failure; silent errors |
| Theme | Inter, type scale, no-line rule, gradient CTAs | Colors match; borders used; no type discipline |

**Bugs:** views never disconnect signal handlers; playlists refresh only on first
tab switch; unbounded artwork cache; fragile GTK3-in-GTK4 tray icon; volume drags
queue redundant commands.

## Phases

- **Phase 0 — bugs & hygiene:** signal lifecycle, playlist refresh, zone source
  command, LRU artwork cache, coalesced volume debounce, error surfaces
  (toasts/status pages), tray stabilization.
- **Phase 1 — design system:** style.css restructured into Sonic Curator tokens
  (Inter, type scale, no-line rule, gradient CTAs, 12px max radius); shared
  loading/empty/error widgets.
- **Phase 2 — Now Playing:** position interpolation, lease-blocked UX with Take
  Control, peek/expand routing panel, HiRes badge parsing (protocol extension
  flagged separately — cross-cutting with daemon/Android).
- **Phase 3 — Library:** decompose view; album grid for containers; mixed-mode
  bulk action bar; auto load-more; per-track context menu.
- **Phase 4 — Queue:** finish drag-to-reorder (optimistic + queue.move),
  optimistic remove + Delete key, foreign-lease read-only mode.
- **Phase 5 — Renderers & Zones:** lease action menus, scanning pulse, active
  renderer in header bar; zones master volume, ZONES/SOURCES tabs, collapsible
  cards, assignment checkboxes, offline dimming.
- **Phase 6 (optional, deferred):** desktop-native extras — shortcuts window,
  adaptive breakpoints, multi-select editing.

## Verification

No test infrastructure exists for this app; verification per task is
`make build` (Vala compile is strict) plus manual smoke runs. Each phase lands
as one or more focused commits so the app never regresses.
