# Media Utopia Desktop App Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a full-featured Vala/GTK4+libadwaita desktop app with Android app parity — MQTT controller, local GStreamer renderer, MPRIS2, tray icon, and Sonic Curator visual design.

**Architecture:** Monolithic GObject app with layered architecture: MQTT client → protocol layer → service layer (correlator, lease, dedup) → repositories → local renderer (GStreamer) → UI views. GObject property/signal bindings wire state to UI. Custom CSS for Sonic Curator theme.

**Tech Stack:** Vala 0.56, GTK4 4.20, libadwaita 1.8, GStreamer 1.26, json-glib 1.10, libmosquitto 2.0.22, libayatana-appindicator3, Meson build system.

**MQTT Broker for testing:** `mqtt.lan` (has existing MU nodes)

**Spec:** `docs/superpowers/specs/2026-04-06-desktop-app-design.md`

---

## File Structure

```
integrations/desktop_app/
├── meson.build                          # Meson build definition
├── Makefile                             # Convenience wrapper
├── README.md                            # Integration documentation
├── data/
│   ├── com.mediautopia.desktop.gschema.xml   # GSettings schema
│   ├── com.mediautopia.desktop.desktop.in    # Desktop entry
│   ├── style.css                             # Sonic Curator GTK CSS
│   └── icons/
│       └── mu-motif.svg                      # App/tray icon
├── vapi/
│   └── mosquitto.vapi                        # Vala bindings for libmosquitto
├── src/
│   ├── main.vala                        # Entry point
│   ├── application.vala                 # MuApplication : Adw.Application
│   ├── window.vala                      # MuWindow : Adw.ApplicationWindow
│   ├── mqtt/
│   │   ├── mqtt_client.vala             # MqttClient : GLib.Object (libmosquitto wrapper)
│   │   └── topics.vala                  # MqttTopics namespace (topic builders)
│   ├── protocol/
│   │   ├── envelope.vala                # CommandEnvelope, ReplyEnvelope, Lease, ReplyError
│   │   ├── bodies.vala                  # All command body builders (Json.Object factories)
│   │   ├── presence.vala                # Presence parser
│   │   └── state.vala                   # RendererState, PlaybackState, QueueState, etc.
│   ├── services/
│   │   ├── command_correlator.vala      # CommandCorrelator : GLib.Object (send+await reply)
│   │   ├── lease_manager.vala           # LeaseManager : GLib.Object (session lifecycle)
│   │   └── command_dedup.vala           # CommandDedup (ring buffer)
│   ├── repositories/
│   │   ├── node_repository.vala         # NodeRepository : GLib.Object (discovery)
│   │   ├── renderer_state_repo.vala     # RendererStateRepository : GLib.Object
│   │   ├── active_renderer_repo.vala    # ActiveRendererRepository : GLib.Object
│   │   ├── library_repository.vala      # LibraryRepository : GLib.Object
│   │   └── playlist_repository.vala     # PlaylistRepository : GLib.Object
│   ├── renderer/
│   │   ├── gst_driver.vala              # GstDriver : GLib.Object (GStreamer playbin + spectrum)
│   │   ├── local_queue.vala             # LocalQueue : GLib.Object (queue state machine)
│   │   └── local_renderer.vala          # LocalRenderer : GLib.Object (engine + MQTT)
│   ├── ui/
│   │   ├── now_playing_view.vala        # NowPlayingView : Adw.Bin
│   │   ├── queue_view.vala              # QueueView : Adw.Bin
│   │   ├── library_view.vala            # LibraryView : Adw.Bin
│   │   ├── renderers_view.vala          # RenderersView : Adw.Bin
│   │   ├── zones_view.vala              # ZonesView : Adw.Bin
│   │   ├── settings_view.vala           # SettingsView : Adw.Bin
│   │   └── widgets/
│   │       ├── audio_visualizer.vala    # AudioVisualizer : Gtk.DrawingArea
│   │       ├── hires_badge.vala         # HiResBadge : Gtk.Box
│   │       ├── mini_player.vala         # MiniPlayer : Gtk.Box
│   │       ├── seek_bar.vala            # SeekBar : Gtk.Box
│   │       └── transport_controls.vala  # TransportControls : Gtk.Box
│   └── platform/
│       ├── mpris2.vala                  # Mpris2 : GLib.Object (D-Bus MPRIS)
│       ├── tray_icon.vala               # TrayIcon : GLib.Object (AppIndicator)
│       └── notifications.vala           # Notifications : GLib.Object (GNotification)
```

---

## Task 1: Project Scaffold + Build System

**Files:**
- Create: `integrations/desktop_app/meson.build`
- Create: `integrations/desktop_app/Makefile`
- Create: `integrations/desktop_app/src/main.vala`
- Create: `integrations/desktop_app/src/application.vala`
- Create: `integrations/desktop_app/src/window.vala`
- Create: `integrations/desktop_app/data/com.mediautopia.desktop.gschema.xml`
- Create: `integrations/desktop_app/data/com.mediautopia.desktop.desktop.in`
- Create: `integrations/desktop_app/data/style.css`
- Create: `integrations/desktop_app/data/icons/mu-motif.svg`
- Create: `integrations/desktop_app/README.md`

- [ ] **Step 1: Create Meson build file**

```meson
project('media-utopia', 'vala', 'c',
  version: '0.1.0',
  meson_version: '>= 0.62',
)

gnome = import('gnome')

# Dependencies
gtk4_dep = dependency('gtk4', version: '>= 4.10')
adw_dep = dependency('libadwaita-1', version: '>= 1.4')
gst_dep = dependency('gstreamer-1.0', version: '>= 1.20')
gst_audio_dep = dependency('gstreamer-audio-1.0')
json_dep = dependency('json-glib-1.0')
soup_dep = dependency('libsoup-3.0')
mosquitto_dep = dependency('libmosquitto')
posix_dep = meson.get_compiler('vala').find_library('posix')

# Optional: AppIndicator (graceful fallback)
ayatana_dep = dependency('ayatana-appindicator3-0.1', required: false)

vala_args = ['--target-glib=2.76']
if ayatana_dep.found()
  vala_args += ['-D', 'HAVE_APPINDICATOR']
endif

sources = files(
  'src/main.vala',
  'src/application.vala',
  'src/window.vala',
)

# Custom VAPI for mosquitto
add_project_arguments(
  '--vapidir=' + meson.current_source_dir() / 'vapi',
  language: 'vala',
)

css_resource = gnome.compile_resources(
  'mu-resources',
  'data/mu.gresource.xml',
  source_dir: 'data',
)

executable('media-utopia',
  sources,
  css_resource,
  dependencies: [
    gtk4_dep,
    adw_dep,
    gst_dep,
    gst_audio_dep,
    json_dep,
    soup_dep,
    mosquitto_dep,
    posix_dep,
  ],
  vala_args: vala_args,
  install: true,
)

# GSettings schema
install_data(
  'data/com.mediautopia.desktop.gschema.xml',
  install_dir: get_option('datadir') / 'glib-2.0' / 'schemas',
)
```

- [ ] **Step 2: Create GResource XML**

Create `integrations/desktop_app/data/mu.gresource.xml`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<gresources>
  <gresource prefix="/com/mediautopia/desktop">
    <file>style.css</file>
  </gresource>
</gresources>
```

- [ ] **Step 3: Create Sonic Curator CSS theme**

Create `integrations/desktop_app/data/style.css` with the full color palette from the Android app (Primary: #CCFF00, Surface: #121412, etc.), typography, and component styling. This CSS will be loaded by the application and override libadwaita defaults.

```css
/* Sonic Curator Theme — Media Utopia Desktop */
/* Color palette from Android app (Color.kt) */

@define-color mu_primary #CCFF00;
@define-color mu_on_primary #1A1C1A;
@define-color mu_primary_container #123724;
@define-color mu_on_primary_container #A5F0A8;
@define-color mu_secondary #CCFF00;
@define-color mu_tertiary #7BC47F;
@define-color mu_surface #121412;
@define-color mu_surface_container_lowest #0E100E;
@define-color mu_surface_container_low #1A1C1A;
@define-color mu_surface_container #1E201E;
@define-color mu_surface_container_high #282A28;
@define-color mu_surface_container_highest #333533;
@define-color mu_surface_variant #3A3E3A;
@define-color mu_on_surface #E2E3DE;
@define-color mu_on_surface_variant #9EA99C;
@define-color mu_outline #6A7568;
@define-color mu_outline_variant #3A3E3A;
@define-color mu_error #FFB4AB;

/* Global window */
window.background {
  background-color: @mu_surface;
  color: @mu_on_surface;
}

/* Header bar */
headerbar {
  background-color: @mu_surface_container_low;
  color: @mu_on_surface;
  border-bottom: none;
  box-shadow: none;
}

/* Sidebar navigation */
.sidebar {
  background-color: @mu_surface_container_lowest;
  color: @mu_on_surface_variant;
}

.sidebar .nav-item {
  padding: 10px 12px;
  border-radius: 8px;
  color: @mu_on_surface_variant;
}

.sidebar .nav-item:checked,
.sidebar .nav-item.active {
  background-color: @mu_surface_container;
  color: @mu_primary;
}

/* Primary button (play/pause) */
.transport-primary {
  background-color: @mu_primary;
  color: @mu_on_primary;
  border-radius: 8px;
  border: none;
  min-width: 56px;
  min-height: 56px;
}

.transport-primary:hover {
  background-color: lighter(@mu_primary);
}

/* Transport secondary buttons */
.transport-btn {
  color: @mu_on_surface;
  background: transparent;
  border: none;
  min-width: 32px;
  min-height: 32px;
}

.transport-btn.active {
  color: @mu_primary;
}

.transport-btn.muted {
  color: @mu_on_surface_variant;
}

/* Seek bar / sliders */
scale trough {
  background-color: @mu_surface_container_highest;
  border-radius: 2px;
  min-height: 4px;
}

scale highlight {
  background-color: @mu_primary;
  border-radius: 2px;
  min-height: 4px;
}

scale slider {
  background-color: @mu_primary;
  min-width: 12px;
  min-height: 12px;
  border-radius: 50%;
  border: none;
}

/* Volume slider */
.volume-slider scale highlight {
  background-color: @mu_primary;
}

/* HiRes badge */
.hires-badge {
  background-color: @mu_surface_variant;
  color: @mu_on_surface_variant;
  border-radius: 2px;
  padding: 3px 8px;
  font-size: 10px;
  font-weight: 500;
  letter-spacing: 0.08em;
}

/* Renderer/device chip */
.device-chip {
  background-color: @mu_surface_container;
  color: @mu_primary;
  border-radius: 4px;
  padding: 3px 10px;
  font-size: 10px;
  font-weight: 500;
  letter-spacing: 0.05em;
}

/* Cards (renderer items, queue items, library items) */
.mu-card {
  background-color: @mu_surface_container_high;
  border-radius: 8px;
  border: none;
  padding: 12px;
}

.mu-card:hover {
  background-color: @mu_surface_container_highest;
}

/* Active/selected card */
.mu-card.active {
  background-color: @mu_surface_container;
  outline: 1px solid alpha(@mu_primary, 0.3);
}

/* Mini player bottom bar */
.mini-player {
  background-color: alpha(@mu_surface_container_low, 0.88);
  border-top: 1px solid alpha(white, 0.05);
}

/* Now Playing gradient overlay */
.now-playing-gradient {
  background: linear-gradient(to bottom,
    alpha(@mu_primary_container, 0.5),
    @mu_surface 60%);
}

/* Text styles */
.track-title {
  font-size: 28px;
  font-weight: 600;
  color: @mu_on_surface;
}

.track-artist {
  font-size: 16px;
  color: @mu_primary;
}

.track-album {
  font-size: 11px;
  font-weight: 500;
  color: @mu_on_surface_variant;
  letter-spacing: 0.08em;
}

.timestamp {
  font-size: 11px;
  color: @mu_outline;
  letter-spacing: 0.05em;
}

/* Section headers */
.section-title {
  font-size: 24px;
  font-weight: 600;
  color: @mu_on_surface;
}

/* Metadata labels (uppercase engraving style) */
.meta-label {
  font-size: 11px;
  font-weight: 500;
  color: @mu_on_surface_variant;
  letter-spacing: 0.08em;
}

/* Entry/input fields */
entry {
  background-color: @mu_surface_container_low;
  color: @mu_on_surface;
  border: none;
  border-bottom: 1px solid alpha(@mu_outline_variant, 0.3);
  border-radius: 0;
}

entry:focus {
  border-bottom-color: @mu_secondary;
  box-shadow: none;
}

/* List rows (queue, library) */
list row {
  background-color: transparent;
  border: none;
  padding: 4px 0;
}

list row:hover {
  background-color: alpha(@mu_surface_container_high, 0.5);
}

list row:selected {
  background-color: @mu_surface_container;
}

/* Scrollbar */
scrollbar slider {
  background-color: @mu_surface_variant;
  border-radius: 4px;
  min-width: 6px;
}

/* Album artwork */
.album-art {
  border-radius: 12px;
}

/* Connection status indicator */
.status-connected {
  color: @mu_primary;
}

.status-disconnected {
  color: @mu_error;
}

.status-connecting {
  color: @mu_on_surface_variant;
}
```

- [ ] **Step 4: Create mosquitto VAPI**

Create `integrations/desktop_app/vapi/mosquitto.vapi`:

```vala
[CCode (cheader_filename = "mosquitto.h")]
namespace Mosquitto {
    [CCode (cname = "mosquitto_lib_init")]
    public static int lib_init ();

    [CCode (cname = "mosquitto_lib_cleanup")]
    public static int lib_cleanup ();

    [Compact]
    [CCode (cname = "struct mosquitto", free_function = "mosquitto_destroy")]
    public class Client {
        [CCode (cname = "mosquitto_new")]
        public Client (string? id, bool clean_session, void* userdata);

        [CCode (cname = "mosquitto_connect")]
        public int connect (string host, int port, int keepalive);

        [CCode (cname = "mosquitto_disconnect")]
        public int disconnect ();

        [CCode (cname = "mosquitto_reconnect")]
        public int reconnect ();

        [CCode (cname = "mosquitto_publish")]
        public int publish (out int mid, string topic, int payloadlen, [CCode (array_length = false)] uint8[]? payload, int qos, bool retain);

        [CCode (cname = "mosquitto_subscribe")]
        public int subscribe (out int mid, string topic, int qos);

        [CCode (cname = "mosquitto_unsubscribe")]
        public int unsubscribe (out int mid, string topic);

        [CCode (cname = "mosquitto_loop")]
        public int loop (int timeout, int max_packets);

        [CCode (cname = "mosquitto_loop_start")]
        public int loop_start ();

        [CCode (cname = "mosquitto_loop_stop")]
        public int loop_stop (bool force);

        [CCode (cname = "mosquitto_socket")]
        public int socket ();

        [CCode (cname = "mosquitto_loop_read")]
        public int loop_read (int max_packets);

        [CCode (cname = "mosquitto_loop_write")]
        public int loop_write (int max_packets);

        [CCode (cname = "mosquitto_loop_misc")]
        public int loop_misc ();

        [CCode (cname = "mosquitto_want_write")]
        public bool want_write ();

        [CCode (cname = "mosquitto_will_set")]
        public int will_set (string topic, int payloadlen, [CCode (array_length = false)] uint8[]? payload, int qos, bool retain);

        [CCode (cname = "mosquitto_connect_callback_set")]
        public void connect_callback_set (ConnectCallback cb);

        [CCode (cname = "mosquitto_disconnect_callback_set")]
        public void disconnect_callback_set (DisconnectCallback cb);

        [CCode (cname = "mosquitto_message_callback_set")]
        public void message_callback_set (MessageCallback cb);

        [CCode (cname = "mosquitto_reconnect_delay_set")]
        public int reconnect_delay_set (uint delay, uint delay_max, bool exponential);

        [CCode (cname = "mosquitto_threaded_set")]
        public int threaded_set (bool threaded);
    }

    [CCode (cname = "mosquitto_connect_callback", has_target = false)]
    public delegate void ConnectCallback (Client mosq, void* userdata, int rc);

    [CCode (cname = "mosquitto_disconnect_callback", has_target = false)]
    public delegate void DisconnectCallback (Client mosq, void* userdata, int rc);

    [CCode (cname = "mosquitto_message_callback", has_target = false)]
    public delegate void MessageCallback (Client mosq, void* userdata, Message msg);

    [Compact]
    [CCode (cname = "struct mosquitto_message")]
    public class Message {
        public int mid;
        public string topic;
        [CCode (array_length_cname = "payloadlen")]
        public uint8[] payload;
        public int payloadlen;
        public int qos;
        public bool retain;
    }

    [CCode (cname = "MOSQ_ERR_SUCCESS")]
    public const int ERR_SUCCESS;
    [CCode (cname = "MOSQ_ERR_CONN_PENDING")]
    public const int ERR_CONN_PENDING;
    [CCode (cname = "MOSQ_ERR_NOMEM")]
    public const int ERR_NOMEM;
    [CCode (cname = "MOSQ_ERR_CONN_REFUSED")]
    public const int ERR_CONN_REFUSED;
    [CCode (cname = "MOSQ_ERR_CONN_LOST")]
    public const int ERR_CONN_LOST;
    [CCode (cname = "MOSQ_ERR_NO_CONN")]
    public const int ERR_NO_CONN;
}
```

- [ ] **Step 5: Create GSettings schema**

Create `integrations/desktop_app/data/com.mediautopia.desktop.gschema.xml`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<schemalist>
  <schema id="com.mediautopia.desktop" path="/com/mediautopia/desktop/">
    <key name="broker-url" type="s">
      <default>'mqtt://mqtt.lan:1883'</default>
      <summary>MQTT broker URL</summary>
    </key>
    <key name="identity" type="s">
      <default>''</default>
      <summary>User identity for command attribution</summary>
    </key>
    <key name="visualizer-enabled" type="b">
      <default>true</default>
      <summary>Show audio frequency visualizer</summary>
    </key>
    <key name="window-width" type="i">
      <default>1280</default>
      <summary>Window width</summary>
    </key>
    <key name="window-height" type="i">
      <default>800</default>
      <summary>Window height</summary>
    </key>
    <key name="window-maximized" type="b">
      <default>false</default>
      <summary>Window maximized state</summary>
    </key>
    <key name="active-renderer-id" type="s">
      <default>''</default>
      <summary>Currently selected renderer node ID</summary>
    </key>
    <key name="close-to-tray" type="b">
      <default>true</default>
      <summary>Minimize to tray on close instead of quitting</summary>
    </key>
  </schema>
</schemalist>
```

- [ ] **Step 6: Create MU motif SVG icon**

Create `integrations/desktop_app/data/icons/mu-motif.svg` — convert the Android vector drawable from `integrations/android_app/app/src/main/res/drawable/mu_motif.xml` to SVG format.

- [ ] **Step 7: Create main.vala entry point**

```vala
int main (string[] args) {
    Gst.init (ref args);
    var app = new Mu.Application ();
    return app.run (args);
}
```

- [ ] **Step 8: Create application.vala**

```vala
namespace Mu {
    public class Application : Adw.Application {
        private Settings settings;

        public Application () {
            Object (
                application_id: "com.mediautopia.desktop",
                flags: ApplicationFlags.FLAGS_NONE
            );
        }

        construct {
            this.settings = new Settings ("com.mediautopia.desktop");
        }

        protected override void startup () {
            base.startup ();
            load_css ();
        }

        protected override void activate () {
            var window = this.active_window;
            if (window == null) {
                window = new Mu.Window (this, this.settings);
            }
            window.present ();
        }

        private void load_css () {
            var provider = new Gtk.CssProvider ();
            provider.load_from_resource ("/com/mediautopia/desktop/style.css");
            Gtk.StyleContext.add_provider_for_display (
                Gdk.Display.get_default (),
                provider,
                Gtk.STYLE_PROVIDER_PRIORITY_APPLICATION
            );
        }
    }
}
```

- [ ] **Step 9: Create window.vala with sidebar navigation**

```vala
namespace Mu {
    public class Window : Adw.ApplicationWindow {
        private Gtk.Stack content_stack;
        private Gtk.ListBox nav_list;
        private Settings settings;

        // Views
        private NowPlayingView now_playing_view;

        public Window (Mu.Application app, Settings settings) {
            Object (application: app, title: "Media Utopia");
            this.settings = settings;
            setup_window ();
            build_ui ();
        }

        private void setup_window () {
            this.set_default_size (
                settings.get_int ("window-width"),
                settings.get_int ("window-height")
            );
            if (settings.get_boolean ("window-maximized")) {
                this.maximize ();
            }
            this.close_request.connect (on_close_request);
            this.notify["default-width"].connect (save_window_state);
            this.notify["default-height"].connect (save_window_state);
            this.notify["maximized"].connect (save_window_state);
        }

        private void build_ui () {
            // Main horizontal layout: sidebar + content
            var main_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 0);

            // Sidebar
            var sidebar = build_sidebar ();
            main_box.append (sidebar);

            // Separator
            var sep = new Gtk.Separator (Gtk.Orientation.VERTICAL);
            main_box.append (sep);

            // Content area with header bar
            var content_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            content_box.hexpand = true;

            // Stack for views
            content_stack = new Gtk.Stack ();
            content_stack.transition_type = Gtk.StackTransitionType.CROSSFADE;
            content_stack.vexpand = true;

            // Create placeholder views
            now_playing_view = new NowPlayingView ();
            content_stack.add_named (now_playing_view, "now-playing");
            content_stack.add_named (new Gtk.Label ("Queue"), "queue");
            content_stack.add_named (new Gtk.Label ("Library"), "library");
            content_stack.add_named (new Gtk.Label ("Renderers"), "renderers");
            content_stack.add_named (new Gtk.Label ("Zones"), "zones");
            content_stack.add_named (new Gtk.Label ("Settings"), "settings");

            content_box.append (content_stack);
            main_box.append (content_box);

            this.set_content (main_box);

            // Select first nav item
            nav_list.select_row (nav_list.get_row_at_index (0));
        }

        private Gtk.Widget build_sidebar () {
            var sidebar_box = new Gtk.Box (Gtk.Orientation.VERTICAL, 0);
            sidebar_box.width_request = 210;
            sidebar_box.add_css_class ("sidebar");

            // Logo
            var logo_box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 10);
            logo_box.margin_start = 20;
            logo_box.margin_end = 20;
            logo_box.margin_top = 12;
            logo_box.margin_bottom = 20;

            var logo_label = new Gtk.Label ("MEDIA UTOPIA");
            logo_label.add_css_class ("heading");
            logo_box.append (logo_label);
            sidebar_box.append (logo_box);

            // Nav items
            nav_list = new Gtk.ListBox ();
            nav_list.selection_mode = Gtk.SelectionMode.SINGLE;
            nav_list.add_css_class ("navigation-sidebar");
            nav_list.margin_start = 8;
            nav_list.margin_end = 8;
            nav_list.vexpand = true;

            string[] nav_items = { "Now Playing", "Queue", "Library", "Renderers", "Zones" };
            string[] nav_icons = { "media-playback-start-symbolic", "view-list-symbolic", "library-music-symbolic", "audio-speakers-symbolic", "network-wireless-symbolic" };
            string[] nav_targets = { "now-playing", "queue", "library", "renderers", "zones" };

            for (int i = 0; i < nav_items.length; i++) {
                var row = new Gtk.ListBoxRow ();
                row.add_css_class ("nav-item");
                var box = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 12);
                box.margin_start = 12;
                box.margin_end = 12;
                box.margin_top = 8;
                box.margin_bottom = 8;
                var icon = new Gtk.Image.from_icon_name (nav_icons[i]);
                icon.pixel_size = 20;
                box.append (icon);
                var label = new Gtk.Label (nav_items[i]);
                box.append (label);
                row.child = box;
                row.set_data<string> ("target", nav_targets[i]);
                nav_list.append (row);
            }

            nav_list.row_selected.connect ((row) => {
                if (row != null) {
                    var target = row.get_data<string> ("target");
                    content_stack.visible_child_name = target;
                }
            });

            sidebar_box.append (nav_list);

            // Settings at bottom
            var settings_list = new Gtk.ListBox ();
            settings_list.selection_mode = Gtk.SelectionMode.NONE;
            settings_list.add_css_class ("navigation-sidebar");
            settings_list.margin_start = 8;
            settings_list.margin_end = 8;
            settings_list.margin_bottom = 8;

            var settings_row = new Gtk.ListBoxRow ();
            settings_row.add_css_class ("nav-item");
            var sbox = new Gtk.Box (Gtk.Orientation.HORIZONTAL, 12);
            sbox.margin_start = 12;
            sbox.margin_end = 12;
            sbox.margin_top = 8;
            sbox.margin_bottom = 8;
            sbox.append (new Gtk.Image.from_icon_name ("preferences-system-symbolic"));
            sbox.append (new Gtk.Label ("Settings"));
            settings_row.child = sbox;
            settings_list.append (settings_row);

            settings_list.row_activated.connect (() => {
                content_stack.visible_child_name = "settings";
                nav_list.unselect_all ();
            });

            sidebar_box.append (settings_list);

            return sidebar_box;
        }

        private bool on_close_request () {
            if (settings.get_boolean ("close-to-tray")) {
                this.hide ();
                return true; // prevent destroy
            }
            return false;
        }

        private void save_window_state () {
            if (!this.maximized) {
                settings.set_int ("window-width", this.default_width);
                settings.set_int ("window-height", this.default_height);
            }
            settings.set_boolean ("window-maximized", this.maximized);
        }
    }
}
```

- [ ] **Step 10: Create stub NowPlayingView**

```vala
namespace Mu {
    public class NowPlayingView : Adw.Bin {
        public NowPlayingView () {
            var label = new Gtk.Label ("Now Playing");
            label.add_css_class ("section-title");
            this.child = label;
        }
    }
}
```

- [ ] **Step 11: Create desktop entry**

Create `integrations/desktop_app/data/com.mediautopia.desktop.desktop.in`:

```ini
[Desktop Entry]
Name=Media Utopia
Comment=Network audio controller and renderer
Exec=media-utopia
Icon=mu-motif
Terminal=false
Type=Application
Categories=Audio;Music;Player;GTK;
Keywords=music;audio;player;mqtt;
StartupWMClass=com.mediautopia.desktop
```

- [ ] **Step 12: Create Makefile**

```makefile
.PHONY: setup build run clean install

BUILDDIR = builddir

setup:
	meson setup $(BUILDDIR) --prefix=/usr/local

build: setup
	meson compile -C $(BUILDDIR)

run: build
	# Compile schemas for local testing
	glib-compile-schemas data/
	GSETTINGS_SCHEMA_DIR=data/ ./$(BUILDDIR)/media-utopia

clean:
	rm -rf $(BUILDDIR)

install: build
	meson install -C $(BUILDDIR)
```

- [ ] **Step 13: Create README.md**

Document the integration: what it is, dependencies, build/run instructions, configuration.

- [ ] **Step 14: Build and verify window launches**

```bash
cd integrations/desktop_app
make run
```

Expected: Window opens with sidebar navigation and "Now Playing" placeholder. Dark theme from CSS is applied.

- [ ] **Step 15: Commit**

```bash
git add integrations/desktop_app/
git commit -m "feat(desktop): scaffold GTK4+libadwaita app with sidebar navigation"
```

---

## Task 2: MQTT Client (libmosquitto wrapper)

**Files:**
- Create: `integrations/desktop_app/src/mqtt/mqtt_client.vala`
- Create: `integrations/desktop_app/src/mqtt/topics.vala`
- Modify: `integrations/desktop_app/meson.build` (add sources)

- [ ] **Step 1: Create MqttTopics namespace**

Translates `MqttTopics.kt` to Vala. Simple string builders for topic paths.

```vala
namespace Mu.MqttTopics {
    public const string BASE = "mu/v1";

    public string presence (string node_id, string base = BASE) {
        return @"$base/node/$node_id/presence";
    }

    public string state (string node_id, string base = BASE) {
        return @"$base/node/$node_id/state";
    }

    public string commands (string node_id, string base = BASE) {
        return @"$base/node/$node_id/cmd";
    }

    public string events (string node_id, string base = BASE) {
        return @"$base/node/$node_id/evt";
    }

    public string reply (string controller_id, string base = BASE) {
        return @"$base/reply/$controller_id";
    }

    public string presence_wildcard (string base = BASE) {
        return @"$base/node/+/presence";
    }

    public string state_wildcard (string base = BASE) {
        return @"$base/node/+/state";
    }

    public string? extract_node_id (string topic) {
        var parts = topic.split ("/");
        for (int i = 0; i < parts.length; i++) {
            if (parts[i] == "node" && i + 1 < parts.length) {
                var node_id = parts[i + 1];
                return (node_id.length > 0) ? node_id : null;
            }
        }
        return null;
    }
}
```

- [ ] **Step 2: Create MqttClient GObject**

Wraps libmosquitto with GLib main loop integration. Uses `mosquitto_socket()` + `GLib.IOChannel` (or `GLib.UnixInputStream` with `GSource`) to pump the mosquitto event loop from GLib's main loop, avoiding threads.

Key features:
- `connect_to_broker(host, port)` async method
- `disconnect()` method
- `subscribe(topic, qos)` method
- `unsubscribe(topic)` method
- `publish(topic, payload, qos, retain)` method
- `signal message_received(topic: string, payload: Bytes)` for incoming messages
- `signal connection_changed(connected: bool)` for state changes
- Exponential backoff reconnection (2s to 30s)
- LWT support for presence cleanup

```vala
namespace Mu {
    public enum ConnectionState {
        DISCONNECTED,
        CONNECTING,
        CONNECTED,
        RECONNECTING;
    }

    public class MqttClient : Object {
        private Mosquitto.Client? mosq = null;
        private uint io_source_id = 0;
        private uint misc_source_id = 0;
        private uint reconnect_source_id = 0;
        private uint reconnect_delay = 2;
        private const uint RECONNECT_MAX = 30;

        private string _host = "";
        private int _port = 1883;
        private string _client_id = "";

        public ConnectionState connection_state { get; private set; default = ConnectionState.DISCONNECTED; }

        public signal void message_received (string topic, uint8[] payload);
        public signal void connection_changed (ConnectionState state);

        // Subscription tracking
        private HashTable<string, int> subscriptions;

        public MqttClient (string client_id) {
            this._client_id = client_id;
            this.subscriptions = new HashTable<string, int> (str_hash, str_equal);
            Mosquitto.lib_init ();
        }

        ~MqttClient () {
            disconnect_from_broker ();
            Mosquitto.lib_cleanup ();
        }

        public void set_will (string topic, uint8[]? payload, int qos, bool retain) {
            if (mosq != null) {
                mosq.will_set (topic, payload != null ? payload.length : 0, payload, qos, retain);
            }
        }

        public void connect_to_broker (string host, int port = 1883) {
            this._host = host;
            this._port = port;

            if (mosq != null) {
                stop_io ();
                mosq = null;
            }

            mosq = new Mosquitto.Client (_client_id, true, null);

            // Set callbacks — use static methods since libmosquitto callbacks don't support closures
            mosq.connect_callback_set (on_connect_cb);
            mosq.disconnect_callback_set (on_disconnect_cb);
            mosq.message_callback_set (on_message_cb);

            set_state (ConnectionState.CONNECTING);

            var rc = mosq.connect (host, port, 60);
            if (rc == Mosquitto.ERR_SUCCESS || rc == Mosquitto.ERR_CONN_PENDING) {
                start_io ();
            } else {
                warning ("MQTT connect failed: %d", rc);
                schedule_reconnect ();
            }
        }

        public void disconnect_from_broker () {
            cancel_reconnect ();
            stop_io ();
            if (mosq != null) {
                mosq.disconnect ();
                mosq = null;
            }
            set_state (ConnectionState.DISCONNECTED);
        }

        public void subscribe (string topic, int qos = 0) {
            subscriptions.insert (topic, qos);
            if (mosq != null && connection_state == ConnectionState.CONNECTED) {
                int mid;
                mosq.subscribe (out mid, topic, qos);
            }
        }

        public void unsubscribe (string topic) {
            subscriptions.remove (topic);
            if (mosq != null && connection_state == ConnectionState.CONNECTED) {
                int mid;
                mosq.unsubscribe (out mid, topic);
            }
        }

        public void publish (string topic, string payload, int qos = 0, bool retain = false) {
            if (mosq == null) return;
            int mid;
            var data = payload.data;
            mosq.publish (out mid, topic, (int) data.length, data, qos, retain);
        }

        // GLib main loop integration
        private void start_io () {
            if (mosq == null) return;
            // Periodic call to mosquitto_loop() — simple and reliable
            misc_source_id = Timeout.add (10, () => {
                if (mosq != null) {
                    mosq.loop (0, 1);
                }
                return Source.CONTINUE;
            });
        }

        private void stop_io () {
            if (misc_source_id > 0) {
                Source.remove (misc_source_id);
                misc_source_id = 0;
            }
        }

        private void resubscribe_all () {
            subscriptions.foreach ((topic, qos) => {
                int mid;
                mosq.subscribe (out mid, topic, qos);
            });
        }

        private void schedule_reconnect () {
            set_state (ConnectionState.RECONNECTING);
            cancel_reconnect ();
            reconnect_source_id = Timeout.add_seconds (reconnect_delay, () => {
                reconnect_source_id = 0;
                if (mosq != null) {
                    var rc = mosq.reconnect ();
                    if (rc != Mosquitto.ERR_SUCCESS) {
                        reconnect_delay = uint.min (reconnect_delay * 2, RECONNECT_MAX);
                        schedule_reconnect ();
                    }
                }
                return Source.REMOVE;
            });
            reconnect_delay = uint.min (reconnect_delay * 2, RECONNECT_MAX);
        }

        private void cancel_reconnect () {
            if (reconnect_source_id > 0) {
                Source.remove (reconnect_source_id);
                reconnect_source_id = 0;
            }
        }

        private void set_state (ConnectionState state) {
            if (this.connection_state != state) {
                this.connection_state = state;
                connection_changed (state);
            }
        }

        // Static callbacks (libmosquitto requires has_target=false)
        // We use a global instance reference since there's only one MqttClient
        private static MqttClient? _instance = null;

        public static void register_instance (MqttClient client) {
            _instance = client;
        }

        private static void on_connect_cb (Mosquitto.Client mosq, void* userdata, int rc) {
            if (_instance == null) return;
            Idle.add (() => {
                if (rc == 0) {
                    _instance.reconnect_delay = 2;
                    _instance.set_state (ConnectionState.CONNECTED);
                    _instance.resubscribe_all ();
                } else {
                    _instance.schedule_reconnect ();
                }
                return Source.REMOVE;
            });
        }

        private static void on_disconnect_cb (Mosquitto.Client mosq, void* userdata, int rc) {
            if (_instance == null) return;
            Idle.add (() => {
                if (rc != 0) {
                    _instance.schedule_reconnect ();
                } else {
                    _instance.set_state (ConnectionState.DISCONNECTED);
                }
                return Source.REMOVE;
            });
        }

        private static void on_message_cb (Mosquitto.Client mosq, void* userdata, Mosquitto.Message msg) {
            if (_instance == null) return;
            var topic = msg.topic;
            var payload = new uint8[msg.payloadlen];
            Memory.copy (payload, msg.payload, msg.payloadlen);
            Idle.add (() => {
                _instance.message_received (topic, payload);
                return Source.REMOVE;
            });
        }
    }
}
```

- [ ] **Step 3: Add sources to meson.build, rebuild and test MQTT connection**

Add the new files to the `sources` array in `meson.build`. Build, run, and verify that the app connects to `mqtt.lan` (will wire up in Settings view later — for now, hardcode in `application.vala` for testing and print connection status to stdout).

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(desktop): add MQTT client with libmosquitto + GLib main loop"
```

---

## Task 3: Protocol Layer

**Files:**
- Create: `integrations/desktop_app/src/protocol/envelope.vala`
- Create: `integrations/desktop_app/src/protocol/bodies.vala`
- Create: `integrations/desktop_app/src/protocol/presence.vala`
- Create: `integrations/desktop_app/src/protocol/state.vala`
- Modify: `integrations/desktop_app/meson.build` (add sources)

- [ ] **Step 1: Create envelope.vala**

Translate `Envelope.kt` to Vala using json-glib. Since Vala doesn't have data classes with automatic serialization, use Json.Object builders and parsers.

Define: `CommandEnvelope` (builder → Json.Object → string), `ReplyEnvelope` (parse from Json.Node), `Lease`, `ReplyError`. Each struct has a `to_json()` and/or `from_json()` static method.

- [ ] **Step 2: Create state.vala**

Translate `State.kt`. Define: `RendererState`, `SessionState`, `PlaybackState`, `QueueState`, `CurrentItemState`. All with `from_json(Json.Object)` static parsers. `RendererState` is the main state object received on the state MQTT topic.

- [ ] **Step 3: Create presence.vala**

Translate `Presence.kt`. Define: `Presence` with `from_json(Json.Object)` parser. Fields: `node_id`, `kind`, `name`, `caps`, `endpoints`, `source`, `sources`, `ts`.

- [ ] **Step 4: Create bodies.vala**

Translate the command body structures from `Bodies.kt`. Use static factory methods that return `Json.Object`. Group by command category:

Session: `SessionBodies.acquire(ttl_ms)`, `.renew(ttl_ms)`
Playback: `PlaybackBodies.play(index?)`, `.seek(position_ms)`, `.set_volume(vol)`, `.set_mute(mute)`
Queue: `QueueBodies.get(from, count, resolve)`, `.set(start_index, entries)`, `.add(position, entries, at_index?)`, `.remove(entry_id, index?)`, `.move(from_idx, to_idx)`, `.clear()`, `.jump(index)`, `.shuffle(seed)`, `.set_shuffle(shuffle)`, `.set_repeat(repeat, mode)`
Library: `LibraryBodies.browse(container_id, start, count)`, `.search(query, start, count)`, `.resolve(item_id, metadata_only)`, `.resolve_batch(item_ids, metadata_only)`
Playlist: `PlaylistBodies.list(owner)`, `.get(playlist_id)`, `.load_playlist(server_id, playlist_id, mode, resolve)`

Also define parsers for reply bodies: `QueueGetReply`, `LibraryBrowseReply`, `LibraryResolveReply`, `PlaylistListReply`, etc.

- [ ] **Step 5: Add to meson.build, build, verify compiles cleanly**

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(desktop): add MU protocol layer (envelopes, state, presence, bodies)"
```

---

## Task 4: Service Layer (Correlator, Lease, Dedup)

**Files:**
- Create: `integrations/desktop_app/src/services/command_correlator.vala`
- Create: `integrations/desktop_app/src/services/lease_manager.vala`
- Create: `integrations/desktop_app/src/services/command_dedup.vala`
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create CommandDedup**

Direct translation of `CommandDedup.kt`. Ring buffer of command IDs with HashSet for O(1) lookup. `seen(id)` returns true if duplicate. Static `should_dedup(cmd_type)` excludes session/read commands.

- [ ] **Step 2: Create CommandCorrelator**

Translate `CommandCorrelator.kt` to Vala async. Key difference: Vala uses `async`/`yield` with `SourceFunc` continuations instead of Kotlin's `CompletableDeferred`.

Pattern:
- `setup(mqtt, topic_base, controller_id, identity)` — subscribe to reply topic
- `async send(node_id, cmd_type, body, lease?, timeout_ms = 2000)` — build envelope, publish, await reply with timeout
- `send_fire_and_forget(node_id, cmd_type, body, lease?)` — publish without waiting
- Reply matching: `HashTable<string, SourceFunc>` keyed by command ID, completed by MQTT message callback

- [ ] **Step 3: Create LeaseManager**

Translate `LeaseManager.kt`. 

- `async ensure_lease(renderer_id)` — returns valid Lease, auto-acquiring/renewing
- `async release_lease(renderer_id)` — release or steal+release
- Background renewal via `Timeout.add_seconds(30, ...)` checking all cached leases
- Cache: `HashTable<string, CachedLease>` with session_id, token, expires_at

- [ ] **Step 4: Add to meson.build, build, verify**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(desktop): add command correlator, lease manager, dedup"
```

---

## Task 5: Repository Layer

**Files:**
- Create: `integrations/desktop_app/src/repositories/node_repository.vala`
- Create: `integrations/desktop_app/src/repositories/renderer_state_repo.vala`
- Create: `integrations/desktop_app/src/repositories/active_renderer_repo.vala`
- Create: `integrations/desktop_app/src/repositories/library_repository.vala`
- Create: `integrations/desktop_app/src/repositories/playlist_repository.vala`
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create NodeRepository**

Translates `NodeRepository.kt`. Subscribes to `mu/v1/node/+/presence` wildcard. Maintains `HashTable<string, Presence>` of discovered nodes. Exposes:
- `signal node_added(Presence)` / `signal node_removed(string node_id)`
- Properties: `renderers`, `libraries`, `playlist_servers`, `zones` (filtered arrays, or use signal-based notification)
- Local node registration for the desktop renderer

- [ ] **Step 2: Create RendererStateRepository**

Translates `RendererStateRepository.kt`. Subscribes to `mu/v1/node/+/state` wildcard. Maintains `HashTable<string, RendererState>` of renderer states. Signal: `state_changed(string node_id, RendererState state)`.
- Supports local state source (direct updates from LocalRenderer, bypassing MQTT)

- [ ] **Step 3: Create ActiveRendererRepository**

Translates `ActiveRendererRepository.kt`. Stores selected renderer ID in GSettings. Signal: `active_renderer_changed(string node_id)`. Default: local renderer ID.

- [ ] **Step 4: Create LibraryRepository**

Translates `LibraryRepository.kt`. Uses CommandCorrelator to send library commands.
- `async browse(library_id, container_id, start, count)` → list of items
- `async search(library_id, query, start, count)` → list of items
- `async resolve(library_id, item_id)` → metadata + sources
- `async resolve_batch(library_id, item_ids)` → list of resolved items
- In-memory metadata cache (`HashTable<string, Json.Object>`)

- [ ] **Step 5: Create PlaylistRepository**

Translates `PlaylistRepository.kt`. Uses CommandCorrelator.
- `async list_playlists(server_id, owner)` → list of playlist summaries
- `async get_playlist(server_id, playlist_id)` → playlist entries
- `async load_playlist(renderer_id, server_id, playlist_id, mode)` → queue.loadPlaylist command

- [ ] **Step 6: Add to meson.build, build, verify**

- [ ] **Step 7: Commit**

```bash
git commit -m "feat(desktop): add repositories (node, renderer state, library, playlist)"
```

---

## Task 6: Wire Up Core Services in Application

**Files:**
- Modify: `integrations/desktop_app/src/application.vala`
- Modify: `integrations/desktop_app/src/window.vala`

- [ ] **Step 1: Initialize service graph in Application**

In `application.vala`, create the full service graph on startup:
1. `MqttClient` (with client ID from hostname)
2. `CommandCorrelator` (wired to MqttClient)
3. `LeaseManager` (wired to CommandCorrelator)
4. `NodeRepository` (wired to MqttClient)
5. `RendererStateRepository` (wired to MqttClient)
6. `ActiveRendererRepository` (wired to GSettings)
7. `LibraryRepository` (wired to CommandCorrelator)
8. `PlaylistRepository` (wired to CommandCorrelator)

Connect to broker URL from GSettings on activate. Pass services to Window.

- [ ] **Step 2: Verify discovery works**

Run the app, connect to `mqtt.lan`, and print discovered nodes to stdout. Verify renderers and libraries appear from the existing MU system.

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(desktop): wire core services and verify MQTT discovery"
```

---

## Task 7: Settings View

**Files:**
- Create: `integrations/desktop_app/src/ui/settings_view.vala`
- Modify: `integrations/desktop_app/src/window.vala` (replace placeholder)
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create SettingsView**

Implement the settings view with:
- Broker URL text entry (bound to GSettings `broker-url`)
- Identity text entry (bound to GSettings `identity`)
- Visualizer toggle switch (bound to GSettings `visualizer-enabled`)
- Close-to-tray toggle switch (bound to GSettings `close-to-tray`)
- Connection status label (Connected/Connecting/Reconnecting/Disconnected) with color-coded CSS classes
- Use `Adw.PreferencesPage` with `Adw.PreferencesGroup` for clean libadwaita layout

- [ ] **Step 2: Wire into Window, build and verify**

Replace the Settings placeholder in the stack. Verify broker URL changes trigger reconnection. Verify settings persist across restarts.

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(desktop): add Settings view with GSettings persistence"
```

---

## Task 8: Renderers View

**Files:**
- Create: `integrations/desktop_app/src/ui/renderers_view.vala`
- Modify: `integrations/desktop_app/src/window.vala`
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create RenderersView**

Implement the renderers list:
- "This PC" always first with "LOCAL PLAYBACK" status
- Network renderers from NodeRepository, sorted by name
- Per-renderer row: icon, name, status text (playing/paused/ready/standby), current track metadata, HiRes badge, lease owner
- Click to select (sets active renderer via ActiveRendererRepository)
- Release lease button per renderer
- Uses `Gtk.ListBox` with custom row widgets
- Listens to `node_repository.node_added/removed` and `renderer_state_repo.state_changed` signals

- [ ] **Step 2: Wire into Window, build and verify**

Replace Renderers placeholder. Verify renderers from mqtt.lan appear. Verify selecting a renderer updates ActiveRendererRepository.

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(desktop): add Renderers view with discovery and selection"
```

---

## Task 9: Now Playing View (Full Implementation)

**Files:**
- Modify: `integrations/desktop_app/src/ui/now_playing_view.vala` (replace stub)
- Create: `integrations/desktop_app/src/ui/widgets/transport_controls.vala`
- Create: `integrations/desktop_app/src/ui/widgets/seek_bar.vala`
- Create: `integrations/desktop_app/src/ui/widgets/hires_badge.vala`
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create HiResBadge widget**

Small label widget showing format/bitDepth/sampleRate in uppercase engraved style. CSS class `hires-badge`. Visible only when metadata contains hi-res fields.

- [ ] **Step 2: Create SeekBar widget**

`Gtk.Scale` with position interpolation. Properties: `position_ms`, `duration_ms`, `is_playing`. When playing, a 100ms `Timeout` interpolates position forward smoothly. Time labels (current / remaining) below the slider. Slider drag sends seek command via callback. CSS-styled per theme.

- [ ] **Step 3: Create TransportControls widget**

Row of buttons: shuffle, prev, play/pause, next, repeat. Plus volume slider with mute toggle on the right.
- Play/pause button: large (56px), lime background, CSS class `transport-primary`
- Shuffle/repeat: toggle states with `active` CSS class
- Volume: `Gtk.Scale` horizontal, 100px wide, with speaker icon and percentage label
- Properties: `is_playing`, `shuffle`, `repeat_mode`, `volume`, `muted`
- Signals: `play_pause_clicked()`, `next_clicked()`, `prev_clicked()`, `shuffle_toggled()`, `repeat_toggled()`, `volume_changed(double)`, `mute_toggled()`

- [ ] **Step 4: Implement full NowPlayingView**

Horizontal layout:
- Left: Album artwork placeholder (300x300, rounded 12px, dark background)
  - HiResBadge overlaid top-right
- Right: Vertical stack:
  - Track title (28px semibold, OnSurface)
  - Artist (16px, lime Primary)
  - Album (11px uppercase, OnSurfaceVariant)
  - Visualizer placeholder (will be added in Task 12)
  - SeekBar
  - TransportControls

Connect to `RendererStateRepository` and `ActiveRendererRepository`:
- When active renderer state changes → update all UI fields
- Transport button clicks → send commands via `CommandCorrelator` with `LeaseManager.ensure_lease()`

Background gradient: CSS class `now-playing-gradient`

- [ ] **Step 5: Build and verify**

Run app connected to mqtt.lan. Select a renderer. Verify state updates appear (track title, position, volume). Verify transport controls send commands.

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(desktop): add Now Playing view with transport controls and seek"
```

---

## Task 10: Queue View

**Files:**
- Create: `integrations/desktop_app/src/ui/queue_view.vala`
- Modify: `integrations/desktop_app/src/window.vala`
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create QueueView**

Implement queue management:
- Header: "QUEUE" title, track count, shuffle/repeat toggles, "Clear" button
- `Gtk.ListBox` with custom rows: index number, artwork thumbnail placeholder, title, artist, drag handle
- Click row to jump (`queue.jump` command)
- Delete button per row (`queue.remove` command)
- Drag-and-drop reordering via `Gtk.DragSource`/`Gtk.DropTarget` on rows (`queue.move` command)
- Clear all button (`queue.clear` command)
- Fetches queue via `queue.get` command on active renderer, pages through all entries
- Current playing item highlighted (lime left border or background shift)
- Updates on queue state changes (revision bumps)

- [ ] **Step 2: Wire into Window, build and verify**

Replace Queue placeholder. Verify queue items from active renderer appear. Test jump, remove, reorder, clear.

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(desktop): add Queue view with reorder, remove, and jump"
```

---

## Task 11: Library View

**Files:**
- Create: `integrations/desktop_app/src/ui/library_view.vala`
- Modify: `integrations/desktop_app/src/window.vala`
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create LibraryView**

Dual-tab interface using `Adw.ViewStack` with `Adw.ViewSwitcher`:

**Libraries tab:**
- Library selector (if multiple libraries discovered)
- Breadcrumb navigation (Gtk.Box with clickable labels)
- Content list (Gtk.ListBox): containers (folders, albums, artists) and leaf items (tracks)
  - Container rows: icon, name, click to navigate deeper
  - Track rows: thumbnail, title, artist, duration. Right-click or button: "Play" / "Add to Queue"
- Search bar (`Gtk.SearchEntry`) with 300ms debounce via `Timeout`
- Pagination: load more on scroll to bottom (50 items per page)
- "Play All" / "Queue All" buttons for current container

**Playlists tab:**
- Playlist server selector (if multiple)
- Playlist list from PlaylistRepository
- Click playlist to view contents
- "Load Playlist" button (replace queue), "Append" button

- [ ] **Step 2: Implement play/queue actions**

When user clicks play on a track:
1. `library_repository.resolve(item_id)` → get source URL
2. `correlator.send(renderer, "queue.add", ...)` → add to queue at "next" position
3. `correlator.send(renderer, "playback.play", {index: new_index})` → play it

When user clicks queue:
1. Resolve → `queue.add` at "end" position

When user plays a container:
1. `library_repository.browse(container_id)` → get all items
2. Resolve all → `queue.set` to replace queue → `playback.play`

- [ ] **Step 3: Build and verify**

Browse libraries from mqtt.lan, navigate containers, search, play/queue items.

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(desktop): add Library view with browse, search, and play/queue"
```

---

## Task 12: Local GStreamer Renderer

**Files:**
- Create: `integrations/desktop_app/src/renderer/gst_driver.vala`
- Create: `integrations/desktop_app/src/renderer/local_queue.vala`
- Create: `integrations/desktop_app/src/renderer/local_renderer.vala`
- Modify: `integrations/desktop_app/src/application.vala`
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create GstDriver**

GStreamer playbin wrapper with spectrum element for FFT data.

```vala
namespace Mu {
    public class GstDriver : Object {
        private Gst.Element? pipeline = null;
        private Gst.Element? spectrum = null;

        public double volume { get; set; default = 0.7; }
        public bool muted { get; set; default = false; }

        public signal void track_finished ();
        public signal void spectrum_data (float[] magnitudes);

        public void play (string url, int64 start_position_ms = 0) {
            stop ();
            pipeline = Gst.ElementFactory.make ("playbin", "player");
            pipeline.set_property ("uri", url);

            // Tee audio for spectrum analysis
            var audio_bin = build_audio_bin ();
            if (audio_bin != null) {
                pipeline.set_property ("audio-sink", audio_bin);
            }

            apply_volume ();
            pipeline.set_state (Gst.State.PLAYING);

            if (start_position_ms > 0) {
                // Seek after state change
                Timeout.add (100, () => {
                    seek_to (start_position_ms);
                    return Source.REMOVE;
                });
            }

            watch_bus ();
        }

        public void pause () {
            if (pipeline != null) {
                pipeline.set_state (Gst.State.PAUSED);
            }
        }

        public void resume () {
            if (pipeline != null) {
                pipeline.set_state (Gst.State.PLAYING);
            }
        }

        public void stop () {
            if (pipeline != null) {
                pipeline.set_state (Gst.State.NULL);
                pipeline = null;
                spectrum = null;
            }
        }

        public void seek_to (int64 position_ms) {
            if (pipeline != null) {
                pipeline.seek_simple (
                    Gst.Format.TIME,
                    Gst.SeekFlags.FLUSH | Gst.SeekFlags.KEY_UNIT,
                    position_ms * Gst.MSECOND
                );
            }
        }

        public bool query_position (out int64 pos_ms, out int64 dur_ms) {
            pos_ms = 0;
            dur_ms = 0;
            if (pipeline == null) return false;
            int64 pos, dur;
            if (pipeline.query_position (Gst.Format.TIME, out pos) &&
                pipeline.query_duration (Gst.Format.TIME, out dur)) {
                pos_ms = pos / Gst.MSECOND;
                dur_ms = dur / Gst.MSECOND;
                return true;
            }
            return false;
        }

        private void apply_volume () {
            if (pipeline != null) {
                var vol = muted ? 0.00001 : volume;
                pipeline.set_property ("volume", vol);
            }
        }

        private Gst.Element? build_audio_bin () {
            // audio tee → spectrum + autoaudiosink
            var bin = new Gst.Bin ("audio-bin");
            var tee = Gst.ElementFactory.make ("tee", "audio-tee");
            var queue1 = Gst.ElementFactory.make ("queue", "audio-queue");
            var sink = Gst.ElementFactory.make ("autoaudiosink", "audio-sink");
            var queue2 = Gst.ElementFactory.make ("queue", "spectrum-queue");
            spectrum = Gst.ElementFactory.make ("spectrum", "spectrum");
            var fake = Gst.ElementFactory.make ("fakesink", "spectrum-sink");

            if (tee == null || queue1 == null || sink == null || spectrum == null) {
                return null;
            }

            spectrum.set_property ("bands", 28);
            spectrum.set_property ("threshold", -80);
            spectrum.set_property ("interval", (uint64)(50 * Gst.MSECOND));
            spectrum.set_property ("post-messages", true);
            fake.set_property ("sync", false);

            bin.add_many (tee, queue1, sink, queue2, spectrum, fake);
            tee.link (queue1);
            queue1.link (sink);
            tee.link (queue2);
            queue2.link (spectrum);
            spectrum.link (fake);

            var pad = tee.get_static_pad ("sink");
            bin.add_pad (new Gst.GhostPad ("sink", pad));

            return bin;
        }

        private void watch_bus () {
            var bus = pipeline.get_bus ();
            bus.add_watch (Priority.DEFAULT, (bus, msg) => {
                switch (msg.type) {
                case Gst.MessageType.EOS:
                    track_finished ();
                    break;
                case Gst.MessageType.ELEMENT:
                    if (msg.get_structure () != null && msg.get_structure ().get_name () == "spectrum") {
                        handle_spectrum_message (msg.get_structure ());
                    }
                    break;
                case Gst.MessageType.ERROR:
                    Error err;
                    string debug;
                    msg.parse_error (out err, out debug);
                    warning ("GStreamer error: %s", err.message);
                    track_finished (); // advance to next
                    break;
                default:
                    break;
                }
                return true;
            });
        }

        private void handle_spectrum_message (Gst.Structure structure) {
            var magnitudes = new float[28];
            var list = structure.get_value ("magnitude");
            if (list == null) return;
            // Extract magnitude values from GstValueList
            for (int i = 0; i < 28; i++) {
                var val = Gst.ValueList.get_value (list, i);
                if (val != null) {
                    magnitudes[i] = (float) val.get_float ();
                }
            }
            spectrum_data (magnitudes);
        }
    }
}
```

- [ ] **Step 2: Create LocalQueue**

Translate `LocalQueue.kt`. In-memory queue with:
- `entries: GenericArray<LocalQueueEntry>`
- `revision`, `index`, `shuffle`, `repeat`, `repeat_mode`
- Methods: `set_entries()`, `add()`, `remove()`, `move()`, `clear()`, `jump()`, `shuffle_entries()`, `next_entry()`, `prev_entry()`, `current_entry()`, `snapshot(from, count)`, `summary()`
- Each mutation bumps revision

- [ ] **Step 3: Create LocalRenderer (engine + MQTT integration)**

Translate `LocalRendererEngine.kt` + `LocalRendererService.kt` combined. This is the core engine:
- Owns: GstDriver, LocalQueue, LeaseManager (for incoming commands), CommandDedup
- Subscribes to `mu/v1/node/{node_id}/cmd` for incoming commands
- Publishes state to `mu/v1/node/{node_id}/state` (debounced 50ms)
- Publishes presence to `mu/v1/node/{node_id}/presence` (retained)
- LWT: empty presence message
- Command dispatch: session.acquire/renew/release, playback.play/pause/stop/seek/next/prev/setVolume/setMute, queue.get/set/add/remove/move/clear/jump/shuffle/setShuffle/setRepeat
- Position polling: 1s interval via GstDriver.query_position()
- End-of-stream: advance queue on EOS, respecting repeat mode
- State building: builds RendererState from current engine state
- Node ID: `mu:renderer:gstreamer:desktop:{hostname}:default`

- [ ] **Step 4: Wire into Application**

Start LocalRenderer in Application.startup(). Register local node in NodeRepository. Register local state source in RendererStateRepository. Set as default active renderer.

- [ ] **Step 5: Build and test local playback**

Queue a track from the library onto "This PC" renderer. Verify GStreamer plays audio. Verify state updates appear in Now Playing view. Verify other MU controllers can see the desktop renderer.

- [ ] **Step 6: Commit**

```bash
git commit -m "feat(desktop): add local GStreamer renderer with full MU protocol"
```

---

## Task 13: Audio Visualizer Widget

**Files:**
- Create: `integrations/desktop_app/src/ui/widgets/audio_visualizer.vala`
- Modify: `integrations/desktop_app/src/ui/now_playing_view.vala`
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create AudioVisualizer**

`Gtk.DrawingArea` that renders 28 bars from GStreamer spectrum data.

- Receives `float[] magnitudes` from GstDriver.spectrum_data signal
- Logarithmic frequency mapping (power-law distribution, exponent 1.8)
- Smoothing: fast rise (0.3 blend), slow decay (0.85 blend)
- Renders rounded rectangles with variable alpha (0.4 to 1.0 based on magnitude)
- Color: Primary lime (#CCFF00)
- Height: 48px, full width
- Animated via `queue_draw()` on each spectrum update
- Only visible when local renderer is playing and visualizer enabled in settings

- [ ] **Step 2: Wire into NowPlayingView**

Add visualizer between metadata and seek bar. Connect to GstDriver.spectrum_data signal. Show/hide based on active renderer being local + playing + visualizer enabled.

- [ ] **Step 3: Build and verify**

Play audio locally, verify bars animate in sync with music.

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(desktop): add audio frequency visualizer (GStreamer spectrum)"
```

---

## Task 14: Mini Player Widget

**Files:**
- Create: `integrations/desktop_app/src/ui/widgets/mini_player.vala`
- Modify: `integrations/desktop_app/src/window.vala`
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create MiniPlayer**

Bottom bar widget shown on all views except Now Playing:
- Artwork thumbnail (40x40, rounded 4px)
- Title + artist labels (truncated with ellipsis)
- Prev, play/pause, next buttons (compact)
- CSS class `mini-player` for glassmorphism styling
- Click anywhere (except buttons) navigates to Now Playing view
- Updates from RendererStateRepository

- [ ] **Step 2: Wire into Window**

Add MiniPlayer at bottom of content area. Show when `content_stack.visible_child_name != "now-playing"`. Hide on Now Playing view.

- [ ] **Step 3: Build and verify**

Navigate to Library/Queue/etc, verify mini player shows with current track. Click play/pause, verify it works.

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(desktop): add persistent mini player bottom bar"
```

---

## Task 15: Zones View

**Files:**
- Create: `integrations/desktop_app/src/ui/zones_view.vala`
- Modify: `integrations/desktop_app/src/window.vala`
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create ZonesView**

Zone management using discovered zone nodes from NodeRepository:
- List of zones from MQTT presence
- Per-zone: name, source label, volume slider, mute toggle
- Volume slider sends zone volume commands
- Mute toggle sends zone mute commands
- Source selector dropdown per zone
- Uses `Adw.PreferencesPage` layout with `Adw.ActionRow` per zone

- [ ] **Step 2: Wire into Window, build and verify**

Replace Zones placeholder. Verify zones from mqtt.lan appear. Test volume/mute controls.

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(desktop): add Zones view with volume and source control"
```

---

## Task 16: Platform Integration (MPRIS2 + Tray + Notifications)

**Files:**
- Create: `integrations/desktop_app/src/platform/mpris2.vala`
- Create: `integrations/desktop_app/src/platform/tray_icon.vala`
- Create: `integrations/desktop_app/src/platform/notifications.vala`
- Modify: `integrations/desktop_app/src/application.vala`
- Modify: `integrations/desktop_app/meson.build`

- [ ] **Step 1: Create MPRIS2 D-Bus interface**

Implement the `org.mpris.MediaPlayer2` and `org.mpris.MediaPlayer2.Player` D-Bus interfaces.

Register on session bus as `org.mpris.MediaPlayer2.mediautopia`.

MediaPlayer2 interface: Identity, DesktopEntry, CanQuit, CanRaise, Raise(), Quit()

Player interface:
- Properties: PlaybackStatus, Metadata (artist, title, album, artUrl, length), Volume, Position, CanGoNext, CanGoPrevious, CanPlay, CanPause, CanSeek
- Methods: Play(), Pause(), PlayPause(), Stop(), Next(), Previous(), Seek(offset), SetPosition(trackId, position)
- Signals: PropertiesChanged for state updates

Listen to RendererStateRepository changes → update D-Bus properties.
Route D-Bus method calls → CommandCorrelator commands on active renderer.

- [ ] **Step 2: Create TrayIcon**

`AppIndicator.Indicator` using libayatana-appindicator3:
- Icon: mu-motif (from app resources or installed icon path)
- Category: APPLICATION_STATUS
- Menu items: Now Playing info (title — artist), separator, Play/Pause, Next, Previous, separator, volume submenu, separator, Show Window, Quit
- Left-click: toggle window visibility
- Updates menu labels on track change

Guarded by `#if HAVE_APPINDICATOR` compile flag.

- [ ] **Step 3: Create Notifications**

GNotification on track change:
- Title: track title
- Body: artist — album
- Icon: album artwork (if available, fetched via Soup)
- Actions: "Next" and "Pause/Play"
- Only notify if window is not focused (avoid spamming when actively using the app)
- Debounce: skip notification if less than 2 seconds since last (rapid skip prevention)

- [ ] **Step 4: Wire into Application, build and verify**

Initialize MPRIS2, TrayIcon, and Notifications in Application.startup(). Verify:
- System media controls (GNOME media widget) show track info and respond to play/pause
- Tray icon appears with working menu
- Track change notification shows when window is minimized

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(desktop): add MPRIS2, system tray, and desktop notifications"
```

---

## Task 17: Keyboard Shortcuts

**Files:**
- Modify: `integrations/desktop_app/src/application.vala`
- Modify: `integrations/desktop_app/src/window.vala`

- [ ] **Step 1: Add keyboard shortcuts**

Use `Gtk.Application.set_accels_for_action()`:
- Space → `app.play-pause`
- N → `app.next`
- P → `app.previous`
- Left/Right arrow → seek back/forward 10s
- Up/Down arrow → volume up/down 5%
- M → toggle mute
- Ctrl+Q → quit (or minimize to tray)

Register GLib.SimpleAction for each, route to CommandCorrelator.

- [ ] **Step 2: Build and verify all shortcuts work**

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(desktop): add keyboard shortcuts for transport controls"
```

---

## Task 18: Album Artwork Loading

**Files:**
- Modify: `integrations/desktop_app/src/ui/now_playing_view.vala`
- Modify: `integrations/desktop_app/src/ui/widgets/mini_player.vala`
- Modify: `integrations/desktop_app/src/ui/queue_view.vala`

- [ ] **Step 1: Implement artwork loading**

Use `Soup.Session` to fetch artwork URLs from metadata `artworkUrl` field. Load into `Gdk.Texture` via `Gdk.Texture.from_bytes()`. Display in `Gtk.Picture` widgets.

Cache downloaded artwork in memory (`HashTable<string, Gdk.Texture>`) keyed by URL to avoid re-fetching.

Apply to: Now Playing (large art), Mini Player (thumbnail), Queue items (thumbnail), Library items (thumbnail).

- [ ] **Step 2: Build and verify artwork appears**

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(desktop): add album artwork loading and caching"
```

---

## Task 19: Final Polish + Queue Persistence

**Files:**
- Modify: `integrations/desktop_app/src/renderer/local_renderer.vala`

- [ ] **Step 1: Add queue persistence**

Save queue snapshot to `~/.local/share/mediautopia/queue.json` on mutations (debounced 2s). Restore on startup. Uses json-glib to serialize/deserialize queue entries, index, repeat mode, volume, position.

- [ ] **Step 2: Full integration test**

Run the complete app against mqtt.lan and verify all features end-to-end per the verification plan in the spec:
1. Build succeeds
2. App launches with sidebar + Now Playing
3. MQTT connects (check Settings)
4. Renderers and libraries appear
5. Remote renderer control works
6. Library browse + search + play/queue
7. Queue management (add, remove, reorder, clear, shuffle, repeat)
8. Local GStreamer playback
9. Visualizer animates
10. MPRIS2 system controls work
11. Tray icon works
12. Track change notifications
13. Settings persist
14. Zones work
15. Playlists work

- [ ] **Step 3: Commit**

```bash
git commit -m "feat(desktop): add queue persistence and final polish"
```
