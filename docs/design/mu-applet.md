# mu-applet: GTK System Tray Media Renderer

A GTK3 system tray applet that runs a media renderer integrated into the Linux desktop. It reuses the existing renderer module and MQTT infrastructure, adding a composable StatePublisher pattern to bridge engine state to the GTK UI.

## Goals

- System tray applet for i3/Linux desktops (like nm-applet)
- Runs a GStreamer renderer in-process (audio + video via GStreamer's own video windows)
- Popup mini-player UI anchored to the tray icon with transport controls, seek, volume, and queue
- External control still works (mu CLI, Home Assistant) — commands and state flow through MQTT
- Maximum code reuse with mud — same config, same renderer module, new binary

## Architecture

```
┌─────────────────────────────────────┐
│           GTK Main Loop             │
│  ┌──────────┐  ┌─────────────────┐  │
│  │ Tray Icon │  │  Popup Window   │  │
│  │(StatusIcon)│ │ (controls/UI)   │  │
│  └──────────┘  └─────────────────┘  │
│         ▲ state updates (glib.IdleAdd)│
│         │                            │
├─────────┼────────────────────────────┤
│    ChannelStatePublisher             │
│    chan *mu.RendererState → GTK      │
├──────────────────────────────────────┤
│    renderer_gstreamer.Module         │
│    ┌──────────────────────────┐      │
│    │  renderer_core.Engine    │      │
│    │  StatePublisher (injected)│     │
│    └──────────────────────────┘      │
│    ┌──────────────────────────┐      │
│    │  GStreamer Driver        │      │
│    │  (playbin3 pipeline)     │      │
│    └──────────────────────────┘      │
│    ┌──────────────────────────┐      │
│    │  MQTT (commands + state) │      │
│    └──────────────────────────┘      │
├──────────────────────────────────────┤
│    Config (reuse mud's TOML loader)  │
└──────────────────────────────────────┘
```

### Threading Model

- **GTK main loop** owns the main goroutine (GTK requirement).
- **Renderer module** runs in a background goroutine, identical to how mud's supervisor runs it.
- **State bridge** uses `glib.IdleAdd()` to marshal state updates from the module goroutine to the GTK thread.
- **Shutdown**: tray quit → cancel context → module stops → `gtk.MainQuit()`.

### Command Flow

Local UI commands are published to MQTT (not sent directly to the engine). This ensures external observers (Home Assistant, mu CLI) see all state changes. The latency through a local broker is sub-millisecond.

```
Popup button click
  → publish CommandEnvelope to MQTT (own command topic)
  → module receives via MQTT subscription
  → engine processes command
  → MultiPublisher fans out:
      → MQTTStatePublisher (external observers)
      → ChannelStatePublisher (GTK popup)
```

### State Flow

State observation is in-process via the ChannelStatePublisher — no MQTT round-trip for UI updates. External observers still receive state via MQTT as before.

## StatePublisher Refactor

The renderer module currently publishes state directly via MQTT calls. This design extracts that into a composable interface.

### Interface

```go
type StatePublisher interface {
    PublishState(state *mu.RendererState) error
}

type PresencePublisher interface {
    PublishPresence(presence *mu.Presence) error
}
```

### Implementations

**MQTTStatePublisher** — wraps the existing MQTT publish calls extracted from the module. Publishes serialized state as a retained message to the node's state topic.

**ChannelStatePublisher** — sends state to a `chan *mu.RendererState`. Non-blocking send (drops if full) to avoid backpressure from a slow GTK consumer.

**MultiPublisher** — composite that fans out to N publishers. Calls each in order; collects errors.

```go
type MultiPublisher struct {
    publishers []StatePublisher
}

func (m *MultiPublisher) PublishState(state *mu.RendererState) error {
    var errs []error
    for _, p := range m.publishers {
        if err := p.PublishState(state); err != nil {
            errs = append(errs, err)
        }
    }
    return errors.Join(errs...)
}
```

### Wiring

**In mud** (unchanged behavior):
```go
publisher := NewMQTTStatePublisher(mqttClient, topicState)
module := renderer_gstreamer.New(..., publisher)
```

**In mu-applet**:
```go
stateCh := make(chan *mu.RendererState, 1)
publisher := NewMultiPublisher(
    NewMQTTStatePublisher(mqttClient, topicState),
    NewChannelStatePublisher(stateCh),
)
module := renderer_gstreamer.New(..., publisher)
```

Same pattern applies to PresencePublisher.

## GTK Popup UI

### Layout

~300px wide dark-themed popup window, anchored to the tray icon position. No window decorations, popup type hint. Click outside to dismiss.

**Playing state** (top to bottom):
1. **Track info header** — album art thumbnail (56x56, placeholder icon when unavailable), track title, artist, album. Gradient background.
2. **Seek bar** — thin progress bar with elapsed/total time labels.
3. **Transport controls** — previous, play/pause (round accent button), next. Centered.
4. **Volume** — speaker icon, horizontal slider, percentage label.
5. **Separator line**.
6. **Queue** — header with track count, scrollable list of tracks. Current track highlighted with accent color.

**Idle/stopped state**:
- Renderer name centered in header area with "Ready" subtitle.
- Transport controls dimmed/disabled.
- Empty queue message with `mu play` hint.

### Tray Icon

GtkStatusIcon with state-dependent icon:
- **Playing** — filled circle or media-playback-start icon
- **Paused** — media-playback-pause icon
- **Idle** — dimmed/outline icon

Left-click toggles the popup. Right-click opens a context menu with Quit.

### Popup Positioning

Position the popup window near the tray icon using `GtkStatusIcon.get_geometry()` to find the icon's screen location. Anchor the popup above or below the icon depending on panel position.

## File Organization

```
# New files
cmd/mu-applet/main.go                        # Entry point, GTK init, wiring
internal/applet/tray.go                       # GtkStatusIcon, click handler, context menu
internal/applet/popup.go                      # Popup window, widgets, layout, state rendering
internal/applet/bridge.go                     # ChannelStatePublisher, glib.IdleAdd bridge

# Modified files
internal/modules/renderer_core/publisher.go   # StatePublisher interface, Multi, MQTT, Channel impls
internal/modules/renderer_core/engine.go      # Accept StatePublisher + PresencePublisher via constructor
internal/modules/renderer_gstreamer/module.go # Thread publishers through to engine

# Unchanged (reused as-is)
pkg/mu/*                                      # Protocol types
internal/mud/config.go                        # Config loading
internal/mud/logging.go                       # Logger factory
internal/adapters/mqttserver/*                # MQTT client
```

## Build

### Dependencies

New Go dependency: `github.com/gotk3/gotk3` (GTK3 bindings for Go).

New system dependency: `libgtk-3-dev`.

Existing: `libgstreamer1.0-dev` (already required for mud).

### Build Tags

- `internal/applet/*.go` uses `//go:build gtk` — mud builds never pull in GTK.
- `renderer_gstreamer` keeps its existing GStreamer build constraint.
- `renderer_core/publisher.go` has no build tags — pure Go interfaces.

### Build Commands

```makefile
mu-applet:
	CGO_ENABLED=1 go build -tags "gstreamer,gtk" -o mu-applet ./cmd/mu-applet

mud:
	CGO_ENABLED=1 go build -tags "gstreamer" -o mud ./cmd/mud
```

## Configuration

Reuses mud's existing TOML config. Launch with:

```bash
mu-applet --config ~/.config/mu/mud-prod.toml
```

The applet reads `[server]` (broker, identity) and `[modules.renderer_gstreamer]` (pipeline, device, crossfade). All other module sections are ignored.

### i3 Integration

```
exec --no-startup-id mu-applet --config ~/.config/mu/mud-prod.toml
```

## Error Handling

- **MQTT connection lost**: tray icon shows disconnected state, module reconnects automatically (existing Paho auto-reconnect). Popup shows "Disconnected" in header.
- **GStreamer pipeline error**: engine handles this already (error state published). Popup shows error message in track info area.
- **GTK popup fails to position**: fall back to center of screen.

## Out of Scope

- Embedded video playback in the popup (GStreamer opens its own video windows via autovideosink).
- Library browsing or playlist management in the popup (use `mu` CLI for that).
- Controlling remote renderers (applet only controls itself).
- Theming/appearance settings (hardcoded dark theme).
