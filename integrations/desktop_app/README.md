# Media Utopia Desktop App

GTK4 + libadwaita desktop controller for the Media Utopia multi-room audio
ecosystem. Connects to the MU daemon over MQTT to browse the library, manage
the playback queue, control renderers and zones, and display Now Playing
status with an optional GStreamer-based visualizer.

## Dependencies

### Build tools

- `meson` (>= 0.62)
- `ninja`
- `valac` (Vala >= 0.56)

### Required libraries

| Package                     | Pkg-config name        |
|-----------------------------|------------------------|
| GTK 4                       | `gtk4`                 |
| libadwaita                  | `libadwaita-1`         |
| GStreamer                   | `gstreamer-1.0`        |
| GStreamer Audio              | `gstreamer-audio-1.0`  |
| JSON-GLib                   | `json-glib-1.0`        |
| libsoup 3                   | `libsoup-3.0`          |
| libmosquitto                | `libmosquitto`         |

### Optional

| Package                     | Pkg-config name               | Purpose        |
|-----------------------------|-------------------------------|----------------|
| Ayatana AppIndicator        | `ayatana-appindicator3-0.1`   | Tray icon      |

### Debian/Ubuntu install

```sh
sudo apt install \
    valac meson ninja-build \
    libgtk-4-dev libadwaita-1-dev \
    libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
    libjson-glib-dev libsoup-3.0-dev \
    libmosquitto-dev \
    libayatana-appindicator3-dev   # optional
```

### Fedora install

```sh
sudo dnf install \
    vala meson ninja-build \
    gtk4-devel libadwaita-devel \
    gstreamer1-devel gstreamer1-plugins-base-devel \
    json-glib-devel libsoup3-devel \
    mosquitto-devel \
    libayatana-appindicator-gtk3-devel   # optional
```

## Build and Run

```sh
cd integrations/desktop_app

# One-step build + run (compiles local GSettings schemas automatically):
make run

# Or step by step:
make setup    # meson setup builddir
make build    # meson compile
make run      # set GSETTINGS_SCHEMA_DIR and launch
```

## Configuration

Settings are stored in GSettings under the schema `com.mediautopia.desktop`:

| Key                  | Type    | Default                      | Description                          |
|----------------------|---------|------------------------------|--------------------------------------|
| `broker-url`         | string  | `mqtt://mqtt.lan:1883`       | MQTT broker URL                      |
| `identity`           | string  | (empty — uses hostname)      | Client identity for MU daemon        |
| `visualizer-enabled` | bool    | `true`                       | Show audio visualizer                |
| `window-width`       | int     | `1100`                       | Saved window width                   |
| `window-height`      | int     | `700`                        | Saved window height                  |
| `window-maximized`   | bool    | `false`                      | Saved maximized state                |
| `active-renderer-id` | string  | (empty)                      | Currently selected renderer          |
| `close-to-tray`      | bool    | `true`                       | Hide to tray on close                |

You can override settings from the command line:

```sh
gsettings set com.mediautopia.desktop broker-url 'mqtt://192.168.1.50:1883'
```

## Architecture

```
src/
  main.vala               Entry point
  application.vala        Adw.Application — wires the service graph
  window.vala             Adw.ApplicationWindow — sidebar nav, content stack,
                          mini player, toast overlay, active renderer indicator
  ui/
    now_playing_view.vala  Artwork, transport, visualizer, routing panel,
                           lease-blocked banner with Take Control
    queue_view.vala        Drag-reorder, durations, Delete key, read-only
                           mode under a foreign lease
    library_view.vala      Libraries/Playlists tabs, album grid for container
                           levels, search, breadcrumbs, auto load-more
    renderers_view.vala    Discovery, selection, Release / Take Control
    zones_view.vala        Master volume, ZONES / SOURCES tabs, per-zone
                           volume/mute/source, source→zone assignment
    settings_view.vala     Broker URL, identity, behavior toggles
    widgets/               Seek bar, transport, mini player, visualizer,
                           artwork loader (LRU), hi-res badge, toaster
  mqtt/                   libmosquitto wrapper + topic builders
  protocol/               Wire types (envelope, state, presence, bodies)
  services/               Command correlator, lease manager, dedup
  repositories/           Node discovery, renderer/zone state, library,
                          playlists, active renderer selection
  renderer/               Local GStreamer renderer (playbin + spectrum)
  platform/               MPRIS2, tray hold/release, notifications
data/
  style.css               Sonic Curator dark theme
  mu.gresource.xml        GResource manifest
  com.mediautopia.desktop.gschema.xml   GSettings schema
  com.mediautopia.desktop.desktop.in    Desktop entry template
  icons/mu-motif.svg      App icon (waveform)
vapi/
  mosquitto.vapi          Vala bindings for libmosquitto
```
