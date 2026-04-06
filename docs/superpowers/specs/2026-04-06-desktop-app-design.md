# Media Utopia Desktop App — Design Spec

## Context

Media Utopia has Android, Home Assistant, and Rhythmbox integrations, but no native Linux desktop application. The Android app provides a full-featured controller and local renderer with the "Sonic Curator" design language. This spec defines a Vala/GTK4 desktop app that brings full Android app parity to the Linux desktop with native GNOME integration.

## Technology Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Language | Vala | First-class GTK/GObject support, compiles to C, GLib async |
| UI toolkit | GTK4 + libadwaita | Modern GNOME standard, adaptive layouts, dark mode |
| MQTT | libmosquitto | Lightweight C library, easy Vala binding, MQTT 3.1.1 |
| Audio | GStreamer (playbin + spectrum) | Same as server-side renderer, FFT for visualizer |
| Build | Meson (+ Makefile wrapper) | Standard for Vala/GTK4 projects |
| Serialization | json-glib | Native GLib JSON, no extra dependencies |
| Settings | GSettings | GNOME-native persistence with schema |
| Tray | libappindicator3 | StatusNotifier protocol, wide DE support |

## Feature Set (Android App Parity)

### Screens

1. **Now Playing** — Album art (left) + metadata/visualizer/controls (right). HiRes badge overlay. 28-bar GStreamer spectrum visualizer. Seek bar with interpolated position (100ms ticker). Volume slider with mute toggle. Shuffle/repeat cycling.

2. **Queue** — Ordered track list. Drag-to-reorder (GtkListBox with DnD). Swipe/button delete. Clear all. Shuffle/repeat toggles. Click to jump.

3. **Library** — Dual-pane: Libraries tab (hierarchical browse with breadcrumbs, infinite scroll pagination at 50 items/page, search with 300ms debounce) and Playlists tab (server selection, playlist listing, content view). Play/queue actions for items and containers.

4. **Renderers** — "This PC" always first. Network-discovered renderers below. Per-renderer: name, playback status, current track, format badge, lease owner. Click to select (acquires lease). Release lease button.

5. **Zones** — Zone discovery and listing. Per-zone volume slider, mute toggle, source selection.

6. **Settings** — Broker URL, identity, visualizer toggle, connection status indicator. Persisted via GSettings.

### Desktop-Specific Features

- **MPRIS2**: D-Bus media player interface. System media controls (play/pause/next/prev/seek), track metadata, artwork. Replaces Android MediaSession.
- **System tray**: MU waveform icon. Right-click menu (transport controls, volume, renderer selector, quit). Left-click toggles window. Uses libappindicator3.
- **Desktop notifications**: GNotification on track change with artwork thumbnail and action buttons (Next, Pause).
- **Keyboard shortcuts**: Space (play/pause), arrow keys (seek/volume), N/P (next/prev), Ctrl+Q (quit to tray).
- **Window state**: Remembers size/position via GSettings. Close minimizes to tray (configurable).

### Local GStreamer Renderer

The app registers as a MU renderer node on the MQTT network, just like the Android app's local ExoPlayer renderer.

- **Node ID**: `mu:renderer:gstreamer:desktop:{hostname}:default`
- **GStreamer pipeline**: `playbin` element with `spectrum` tee for FFT data
- **Capabilities**: seek, volume, mute, crossfade (optional)
- **State machine**: Same as `renderer_core/engine.go` — processes session, playback, and queue commands
- **State publishing**: Debounced 50ms, position updates every 1s
- **Presence publishing**: Retained on connect, empty on disconnect (LWT)
- **Command dedup**: Ring buffer (128 entries) matching Go implementation
- **Queue persistence**: JSON snapshot to `~/.local/share/mediautopia/queue.json`

## Architecture

```
┌─────────────────────────────────┐
│  UI Layer (Gtk4 + libadwaita)   │
│  Custom Widget subclasses       │
│  .ui XML templates + CSS        │
├─────────────────────────────────┤
│  State Layer (GObject classes)  │
│  NodeRepository                 │
│  RendererStateRepository        │
│  LibraryRepository              │
│  ActiveRendererRepository       │
│  PlaylistRepository             │
│  (GLib properties + signals)    │
├─────────────────────────────────┤
│  Service Layer                  │
│  MqttClient (libmosquitto)     │
│  CommandCorrelator             │
│  LeaseManager                  │
│  LocalRenderer + GstDriver     │
├─────────────────────────────────┤
│  Platform Integration           │
│  MPRIS2 (D-Bus)               │
│  StatusNotifier (tray)         │
│  GNotification                 │
│  GSettings                     │
└─────────────────────────────────┘
```

### Pattern Translations (Android → Desktop)

| Android | Desktop Vala |
|---------|-------------|
| Hilt DI | Constructor injection (GObject construction) |
| Kotlin Flow/StateFlow | GObject property notify + GLib signals |
| Coroutines | Vala async/yield (GMainLoop-based) |
| DataStore | GSettings with XML schema |
| ExoPlayer | GStreamer playbin |
| MediaSession | MPRIS2 D-Bus interface |
| Foreground Service | Main process + tray icon (persist on close) |
| HiveMQ MQTT Client | libmosquitto with GLib source integration |
| Jetpack Compose | GTK4 widgets + libadwaita + CSS |
| Coil (image loading) | GdkPixbuf + Soup for HTTP fetch |

### MQTT Integration

Identical protocol to all other MU integrations:

- **Topics**: `mu/v1/node/{nodeId}/presence`, `/state`, `/cmd`, `/evt`, `mu/v1/reply/{controllerId}`
- **Envelopes**: CommandEnvelope (id, type, ts, from, replyTo, lease, ifRevision, body) and ReplyEnvelope (id, type, ok, ts, body, err)
- **Controller ID**: `mu:controller:desktop:{hostname}`
- **Wildcard subscriptions**: `mu/v1/node/+/presence` and `mu/v1/node/+/state` for discovery
- **QoS**: Commands QoS 0, state/presence QoS 1 retained
- **Reconnection**: Exponential backoff (2s initial, 30s max), network state monitoring via GLib NetworkMonitor
- **LWT**: Empty retained presence message for clean disconnect detection

### Command Correlation

Same pattern as Android's `CommandCorrelator`:
- Send command with UUID `id` and `replyTo` topic
- Subscribe to reply topic once at startup
- Match replies by `id` with 2-second async timeout
- Fire-and-forget option for best-effort commands

### Lease Management

Same pattern as Android's `LeaseManager`:
- 5-minute TTL, auto-renew every 30s check loop
- Acquire on first mutation command
- Renew before expiry
- Re-acquire on LEASE_MISMATCH error
- Release on renderer switch

## Visual Design

### Color Palette (Sonic Curator — current Android implementation)

| Token | Hex | Usage |
|-------|-----|-------|
| Primary | `#CCFF00` | Lime — buttons, sliders, active states, artist text |
| OnPrimary | `#1A1C1A` | Dark text on primary backgrounds |
| PrimaryContainer | `#123724` | Dark green container |
| Surface | `#121412` | Main background (green-tinted black) |
| SurfaceContainerLowest | `#0E100E` | Sidebar background |
| SurfaceContainerLow | `#1A1C1A` | Header bar, bottom bar |
| SurfaceContainer | `#1E201E` | Active sidebar item |
| SurfaceContainerHigh | `#282A28` | Cards, badges |
| SurfaceContainerHighest | `#333533` | Seek bar inactive track |
| SurfaceVariant | `#3A3E3A` | HiRes badge background |
| OnSurface | `#E2E3DE` | Primary text (off-white) |
| OnSurfaceVariant | `#9EA99C` | Secondary text (muted green) |
| Outline | `#6A7568` | Timestamps, subtle labels |
| Tertiary | `#7BC47F` | Soft green accent |
| Error | `#FFB4AB` | Error states |

Applied via GTK4 CSS stylesheet (`style.css`) using CSS custom properties.

### Typography

- **Font**: Inter (bundled) or system sans-serif fallback
- **Track title**: 28px semibold, OnSurface
- **Artist**: 16px normal, Primary (lime) — key accent color
- **Album**: 11px medium uppercase, OnSurfaceVariant, 0.08em letter-spacing
- **Labels/badges**: 10-11px medium uppercase, wide letter-spacing (hardware engraving style)

### Shape

Architectural, not bubbly: 2dp (small), 4dp (medium), 8dp (large), 12dp (album art). No borders — hierarchy through surface color shifts per the "no-line rule."

### Icons

Material Design Outlined style (thin-stroke), matching the Android app. Sourced as SVG and embedded in Vala resources or using symbolic icons where GTK provides equivalents.

### Layout (Desktop Adaptation)

- **Sidebar** (210px): MU waveform logo + nav items (Now Playing, Queue, Library, Renderers, Zones) + Settings pinned to bottom. Active item: lime text + SurfaceContainer background.
- **Header bar**: CSD (client-side decorations) via libadwaita. Shows view title, renderer chip (THIS PC badge), HiRes badge, window controls.
- **Content area**: View-specific. Now Playing uses horizontal layout (art left, controls right). Other views use full-width list/grid layouts.
- **Mini player**: Bottom bar with glassmorphism (semi-transparent + backdrop via CSS), visible on non-Now Playing views. Artwork thumbnail, title/artist, transport buttons.
- **Gradient**: Subtle vertical gradient from PrimaryContainer (0.5 alpha) to Surface on Now Playing, matching Android.

## File Structure

```
integrations/desktop_app/
├── meson.build
├── Makefile
├── README.md
├── data/
│   ├── com.mediautopia.desktop.gschema.xml
│   ├── com.mediautopia.desktop.desktop.in
│   ├── com.mediautopia.desktop.metainfo.xml
│   └── icons/
│       └── mu-motif.svg
├── src/
│   ├── main.vala
│   ├── application.vala
│   ├── window.vala
│   ├── mqtt/
│   │   ├── mqtt_client.vala
│   │   ├── command_correlator.vala
│   │   └── topics.vala
│   ├── protocol/
│   │   ├── envelope.vala
│   │   ├── bodies.vala
│   │   ├── presence.vala
│   │   └── state.vala
│   ├── domain/
│   │   ├── lease_manager.vala
│   │   ├── command_dedup.vala
│   │   └── metadata_cache.vala
│   ├── repository/
│   │   ├── node_repository.vala
│   │   ├── renderer_state_repository.vala
│   │   ├── active_renderer_repository.vala
│   │   ├── library_repository.vala
│   │   └── playlist_repository.vala
│   ├── renderer/
│   │   ├── local_renderer.vala
│   │   ├── gst_driver.vala
│   │   └── local_queue.vala
│   ├── ui/
│   │   ├── views/
│   │   │   ├── now_playing_view.vala
│   │   │   ├── queue_view.vala
│   │   │   ├── library_view.vala
│   │   │   ├── renderers_view.vala
│   │   │   ├── zones_view.vala
│   │   │   └── settings_view.vala
│   │   ├── widgets/
│   │   │   ├── audio_visualizer.vala
│   │   │   ├── hires_badge.vala
│   │   │   ├── mini_player.vala
│   │   │   ├── seek_bar.vala
│   │   │   └── transport_controls.vala
│   │   └── style.css
│   └── platform/
│       ├── mpris2.vala
│       ├── tray_icon.vala
│       └── notifications.vala
└── vapi/
    └── mosquitto.vapi
```

## Verification Plan

1. **Build**: `make build` succeeds with no warnings
2. **Launch**: App starts, shows sidebar + Now Playing view with correct theme
3. **MQTT**: Connect to broker, verify connection status in Settings
4. **Discovery**: Renderers and libraries appear from MQTT presence
5. **Remote control**: Select a remote renderer, play/pause/next/prev/seek/volume all work
6. **Library browse**: Navigate library hierarchy, search, play items → queued on active renderer
7. **Queue management**: Add, remove, reorder, clear, shuffle, repeat — all reflected on renderer
8. **Local playback**: Select "This PC", queue tracks, verify GStreamer audio output
9. **Visualizer**: FFT bars animate during local playback
10. **MPRIS2**: System media controls (GNOME/KDE media widget) control the app
11. **Tray icon**: Icon appears, right-click menu works, left-click toggles window
12. **Notifications**: Track change shows desktop notification with artwork
13. **Settings persistence**: Broker URL, identity, window geometry survive restart
14. **Zones**: Zone volume/mute/source controls work
15. **Playlists**: List, browse, load playlist to renderer queue
