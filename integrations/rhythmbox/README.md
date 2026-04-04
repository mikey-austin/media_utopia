# Media Utopia — Rhythmbox Plugin

A native Vala plugin that makes Rhythmbox a first-class citizen in the Media Utopia MQTT-based media control plane. It serves three roles simultaneously:

- **Renderer** — Rhythmbox appears as an MU renderer that external controllers (Home Assistant, CLI, applet) can discover and control via MQTT.
- **Library Browser** — MU libraries (filesystem, Jellyfin, UPnP, podcast) appear as browsable sources in the Rhythmbox sidebar.
- **Controller** — Control any remote MU renderer (GStreamer, Kodi, VLC, UPnP) from within Rhythmbox.

## Build Dependencies

```
sudo apt install valac meson ninja-build \
    libmosquitto-dev libjson-glib-dev libpeas-dev \
    libgtk-3-dev rhythmbox-dev libtdb-dev
```

## Build & Install

```bash
# Build
make build

# Install to ~/.local (no sudo, recommended for development)
make dev-install

# Or install system-wide
sudo make install
```

Then restart Rhythmbox and enable **Media Utopia** in *Edit > Plugins*.

## Configuration

Settings are stored in GSettings under `org.gnome.rhythmbox.plugins.mu`:

| Key | Default | Description |
|-----|---------|-------------|
| `broker-host` | `localhost` | MQTT broker hostname |
| `broker-port` | `1883` | MQTT broker port |
| `broker-username` | *(empty)* | MQTT username (empty = anonymous) |
| `broker-password` | *(empty)* | MQTT password |
| `topic-base` | `mu/v1` | MU protocol topic prefix |
| `node-name` | `Rhythmbox` | Display name when appearing as a renderer |
| `renderer-enabled` | `true` | Expose RB as an MU renderer |
| `controller-enabled` | `true` | Enable remote renderer control |
| `library-enabled` | `true` | Show MU libraries in sidebar |

Configure via `gsettings` or `dconf-editor`:

```bash
gsettings set org.gnome.rhythmbox.plugins.mu broker-host '192.168.1.100'
gsettings set org.gnome.rhythmbox.plugins.mu node-name 'Desktop Speaker'
```

## Architecture

```
Rhythmbox Process
├── MuPlugin (libpeas entry point)
│   ├── MqttClient (libmosquitto + GLib.Idle.add thread marshaling)
│   ├── NodeRegistry (presence-based discovery of all MU nodes)
│   │
│   ├── MuRenderer
│   │   ├── RendererLease (exclusive session management)
│   │   ├── RendererQueue (canonical MU queue with revision counter)
│   │   └── Bridges: MQTT commands ↔ RB.ShellPlayer
│   │
│   ├── MuController
│   │   └── ControllerPanel (GTK3 widget — transport, seek, volume, queue)
│   │
│   └── MuLibrarySource[] (one per discovered MU library)
│       └── MuEntryType (custom RhythmDB entry type)
│
└── MQTT ↔ Broker ↔ mud, Home Assistant, CLI, applet
```

## How It Works

### Renderer Role

When enabled, Rhythmbox publishes its presence as `mu:renderer:rhythmbox:{user}@{host}:default` and subscribes for commands. Any MU controller can then:

- Acquire a session lease
- Send playback commands (play, pause, stop, seek, next, prev)
- Manage the queue (set, add, remove, shuffle, repeat)
- Control volume and mute

State is published as a retained MQTT message with 50ms debounce and 1-second position updates.

### Library Browser

MU libraries discovered via presence appear as sources in the Rhythmbox sidebar. Clicking a source sends `library.browse` commands over MQTT. Tracks are resolved to HTTP stream URLs and played through Rhythmbox's GStreamer backend.

### Controller

A GTK panel lets you select and control any remote MU renderer. It acquires a lease, displays now-playing info, and provides transport controls, seek bar, volume slider, and queue management.

## Protocol Compatibility

The plugin implements the full `mu/v1` wire protocol as defined in `docs/spec/messages.md`, including:

- Command envelope with UUID deduplication (128-slot ring buffer)
- Session lease management (acquire/renew/release with TTL)
- Queue operations with monotonic revision counter
- Optimistic concurrency via `ifRevision`
- LWT for offline detection (empty retained presence on disconnect)

## Development

```bash
make clean    # Remove build directory
make build    # Recompile
make dev-install   # Reinstall to ~/.local
```

The plugin is compiled to a single `libmu.so` shared library loaded by libpeas at runtime. Rhythmbox symbols are resolved when the plugin is loaded into the Rhythmbox process.
