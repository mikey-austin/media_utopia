# Desktop App Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring `integrations/desktop_app` to feature/UX parity with `integrations/android_app`, fixing known bugs along the way.

**Architecture:** Incremental refresh in place. The Vala service layer (mqtt/, services/, repositories/, renderer/, protocol/) stays as-is; UI files in src/ui/ are reworked phase by phase. `library_view.vala` gets decomposed during its overhaul.

**Tech Stack:** Vala 0.56, GTK4, libadwaita, GStreamer, libmosquitto, meson/ninja. Verify each task with `make -C integrations/desktop_app build` (treat new valac errors as failures; pre-existing C warnings are noise). Commit after each task.

**Spec:** docs/superpowers/specs/2026-06-10-desktop-app-parity-design.md

---

### Task 0.1: View signal lifecycle

**Files:** Modify all of `src/ui/now_playing_view.vala`, `queue_view.vala`, `library_view.vala`, `renderers_view.vala`, `zones_view.vala`, `src/ui/widgets/mini_player.vala`.

- [ ] Audit every `*.connect(...)` on long-lived services (repos, mqtt client, correlator) in each view; store handler ids.
- [ ] Disconnect them in a `dispose`/`unmap`-safe teardown (views live as long as the window, but pattern must be safe for future recycling).
- [ ] Build, commit `fix(desktop): disconnect view signal handlers on dispose`.

### Task 0.2: Playlist refresh

**Files:** `src/ui/library_view.vala` (playlists tab logic).

- [ ] Refresh playlist list on every tab activation (drop the `cached_playlists == null` guard or pair it with staleness), and add an explicit refresh button to the playlists header.
- [ ] Build, commit.

### Task 0.3: Zone source selection command

**Files:** `src/ui/zones_view.vala`, `src/protocol/bodies.vala` (verify body builder exists).

- [ ] Wire the source dropdown to actually send the zone source-select command (same command the Android ZoneRepository.selectSource sends).
- [ ] Build, commit.

### Task 0.4: Bounded artwork cache + coalesced volume debounce

**Files:** `src/ui/widgets/artwork_loader.vala`, `src/ui/zones_view.vala`, `src/ui/now_playing_view.vala`.

- [ ] LRU-bound artwork cache (~200 textures).
- [ ] One debounce timer per volume slider: reset on change, single trailing send (no queued duplicates).
- [ ] Build, commit.

### Task 0.5: Error surfaces

**Files:** `src/window.vala` (Adw.ToastOverlay), `src/ui/library_view.vala`, `src/renderer/local_renderer.vala` or `gst_driver.vala` (error signal), shared state widget (added in Phase 1, minimal version here if needed).

- [ ] Wrap content stack in `Adw.ToastOverlay`; views get a `show_toast` path.
- [ ] Library browse/search failure: replace frozen spinner with error state + Retry.
- [ ] GStreamer playback errors surface as toast instead of silent log.
- [ ] Build, commit.

### Task 0.6: Tray icon stabilization

**Files:** `src/platform/tray_icon.vala`, `src/ui/settings_view.vala`.

- [ ] Guard tray construction behind runtime check + setting; failure must not crash or warn-spam. Keep close-to-tray working when indicator unavailable (fall back to normal close).
- [ ] Build, commit.

### Task 1.1: Sonic Curator tokens in style.css

**Files:** `data/style.css`.

- [ ] Define color tokens exactly per Android theme (`#CCFF00`, surface ladder `#0E100E`→`#333533`, onSurface `#E2E3DE`, variant `#9EA99C`).
- [ ] Typography: Inter (fallback sans), letter-spacing on labels (uppercase metadata style), heading sizes per scale.
- [ ] No-line rule: remove 1px borders from cards/rows; depth via surface containers. Max radius 12px. Gradient CTA class (135° primary→primaryContainer).
- [ ] Build, run visual smoke, commit.

### Task 1.2: Shared state widgets

**Files:** Create `src/ui/widgets/state_pages.vala`.

- [ ] `MuStatusPage` helper producing loading / empty / error (with retry callback) pages, Sonic Curator styled; adopt in library, queue, zones, renderers views.
- [ ] Build, commit.

### Task 2.1: Position interpolation

**Files:** `src/ui/now_playing_view.vala`, `src/ui/widgets/seek_bar.vala`, `src/ui/widgets/mini_player.vala`.

- [ ] 100–250ms ticker interpolates position while status==playing between MQTT state updates; resync on each state message; pause/stop stops ticker.
- [ ] Build, commit.

### Task 2.2: Lease-blocked UX

**Files:** `src/ui/now_playing_view.vala`, `src/services/lease_manager.vala` (needs takeControl), `src/protocol/bodies.vala`.

- [ ] Detect foreign session owner from RendererState.session; disable transport; show "Controlled by {owner}" + Take Control button → `session.takeControl` then refresh lease.
- [ ] Build, commit.

### Task 2.3: Routing panel peek/expand + HiRes badge

**Files:** `src/ui/now_playing_view.vala`, `src/ui/widgets/hires_badge.vala`, `src/protocol/state.vala`.

- [ ] Routing/zones panel collapses to a peek row (renderer name, volume %, zone count) and expands to full controls (Gtk.Revealer).
- [ ] Parse optional `sampleRate`/`bitDepth`/`format` from display metadata into DisplayMetadata; badge shows when present.
- [ ] Build, commit.

### Task 3.1: Decompose library view

**Files:** Split `src/ui/library_view.vala` into `src/ui/library/browse_page.vala`, `src/ui/library/playlists_page.vala`, `src/ui/library/item_widgets.vala`; `library_view.vala` becomes a thin tab container. Update `meson.build`.

- [ ] Pure move/refactor, no behavior change. Build, commit.

### Task 3.2: Container grid + mixed mode

**Files:** `src/ui/library/browse_page.vala`, `item_widgets.vala`, `data/style.css`.

- [ ] Content-mode detection (pure containers / pure tracks / mixed) like Android.
- [ ] Containers → FlowBox grid of artwork cards (1:1 art, title, subtitle, hover play overlay → playAll).
- [ ] Mixed → container rows + bulk bar ("N TRACKS", Play All, Queue All) + track rows.
- [ ] Build, commit.

### Task 3.3: Auto load-more + track context menu

**Files:** `src/ui/library/browse_page.vala`, `playlists_page.vala`.

- [ ] ScrolledWindow edge-reached / adjustment threshold triggers load-more (replaces button).
- [ ] Per-track menu (popover, also on right-click): Play, Add to Queue.
- [ ] Build, commit.

### Task 4.1: Queue drag-to-reorder

**Files:** `src/ui/queue_view.vala`.

- [ ] GTK4 DragSource/DropTarget per row, drag handle icon, optimistic reorder, send `queue.move` with entry id + target index; revert on error reply.
- [ ] Build, commit.

### Task 4.2: Queue remove + read-only lease mode

**Files:** `src/ui/queue_view.vala`.

- [ ] Optimistic remove (row out immediately, revert on error), Delete key binding on selected row.
- [ ] When foreign lease owner: hide mutating controls, dim list, show "Controlled by {owner}" banner.
- [ ] Build, commit.

### Task 5.1: Renderer lease actions + header presence

**Files:** `src/ui/renderers_view.vala`, `src/window.vala`.

- [ ] Per-renderer menu: Take Control / Release / Acquire (wired through LeaseManager); scanning pulse indicator while connected.
- [ ] Window header (or sidebar status area) shows active renderer name, click → switch to renderers view.
- [ ] Build, commit.

### Task 5.2: Zones overhaul

**Files:** `src/ui/zones_view.vala`, `src/protocol/bodies.vala`.

- [ ] Master volume row (active renderer volume) at top.
- [ ] ZONES / SOURCES tabs: zones tab keeps per-zone cards (now collapsible via Gtk.Expander/Revealer); sources tab lists sources with zone-assignment checkboxes.
- [ ] Offline zones dimmed (0.4 alpha class).
- [ ] Build, commit.

### Task F: Final verification

- [ ] `rm -rf builddir && make setup build` clean build.
- [ ] Run app, smoke every view.
- [ ] Update desktop_app README (architecture section is stale — says now_playing is a stub).
- [ ] Final commit.
