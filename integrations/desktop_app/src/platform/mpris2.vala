/* mpris2.vala — MPRIS2 D-Bus media player interface.
 * Registers org.mpris.MediaPlayer2.mediautopia on the session bus,
 * exposing root + player interfaces for desktop environment integration.
 */

namespace Mu {

    /**
     * org.mpris.MediaPlayer2 — root interface.
     */
    [DBus (name = "org.mpris.MediaPlayer2")]
    public class MprisRoot : Object {

        private Gtk.Application app;

        public MprisRoot (Gtk.Application app) {
            this.app = app;
        }

        /* Properties */

        public bool can_quit { get { return true; } }
        public bool can_raise { get { return true; } }
        public bool has_track_list { get { return false; } }

        public string identity { get { return "Media Utopia"; } }
        public string desktop_entry { get { return "com.mediautopia.desktop"; } }

        public string[] supported_uri_schemes {
            owned get { return { "file", "http", "https" }; }
        }

        public string[] supported_mime_types {
            owned get { return {
                "audio/mpeg", "audio/flac", "audio/ogg",
                "audio/x-wav", "audio/aac", "audio/mp4"
            }; }
        }

        /* Methods */

        public void raise () throws GLib.Error {
            app.activate ();
        }

        public void quit () throws GLib.Error {
            app.quit ();
        }
    }

    /**
     * org.mpris.MediaPlayer2.Player — player interface.
     */
    [DBus (name = "org.mpris.MediaPlayer2.Player")]
    public class MprisPlayer : Object {

        private RendererStateRepository state_repo;
        private ActiveRendererRepository active_repo;
        private CommandCorrelator correlator;
        private LeaseManager lease_mgr;
        private DBusConnection? conn = null;

        /* Cached state for property reads */
        private string _playback_status = "Stopped";
        private HashTable<string, Variant> _metadata;
        private double _volume = 1.0;
        private int64 _position = 0;  /* microseconds */

        public MprisPlayer (RendererStateRepository state_repo,
                            ActiveRendererRepository active_repo,
                            CommandCorrelator correlator,
                            LeaseManager lease_mgr) {
            this.state_repo = state_repo;
            this.active_repo = active_repo;
            this.correlator = correlator;
            this.lease_mgr = lease_mgr;

            _metadata = new HashTable<string, Variant> (str_hash, str_equal);
            _metadata.insert ("mpris:trackid",
                new Variant.object_path ("/org/mpris/MediaPlayer2/TrackList/NoTrack"));

            /* Listen for state changes on the active renderer */
            state_repo.state_changed.connect (on_state_changed);
            active_repo.active_renderer_changed.connect ((node_id) => {
                refresh_from_state ();
            });

            /* Apply initial state */
            refresh_from_state ();
        }

        /**
         * Store the D-Bus connection for emitting PropertiesChanged.
         * Not exposed over D-Bus — called internally after registration.
         */
        [DBus (visible = false)]
        public void set_connection (DBusConnection connection) {
            this.conn = connection;
        }

        /* ---- MPRIS Properties ---- */

        public string playback_status { get { return _playback_status; } }

        public HashTable<string, Variant> metadata {
            owned get {
                /* Return a copy */
                var copy = new HashTable<string, Variant> (str_hash, str_equal);
                _metadata.foreach ((k, v) => { copy.insert (k, v); });
                return copy;
            }
        }

        public double volume {
            get { return _volume; }
            set {
                /* MPRIS volume set — route to active renderer */
                var new_vol = value.clamp (0.0, 1.0);
                send_command ("playback.setVolume", PlaybackBodies.set_volume (new_vol));
            }
        }

        public int64 position { get { return _position; } }

        public double rate { get { return 1.0; } set {} }
        public double minimum_rate { get { return 1.0; } }
        public double maximum_rate { get { return 1.0; } }

        public bool can_go_next { get { return true; } }
        public bool can_go_previous { get { return true; } }
        public bool can_play { get { return true; } }
        public bool can_pause { get { return true; } }
        public bool can_seek { get { return true; } }
        public bool can_control { get { return true; } }

        /* ---- MPRIS Methods ---- */

        public void play () throws GLib.Error {
            send_command ("playback.play", PlaybackBodies.play ());
        }

        public void pause () throws GLib.Error {
            send_command ("playback.pause", new Json.Object ());
        }

        public void play_pause () throws GLib.Error {
            if (_playback_status == "Playing") {
                send_command ("playback.pause", new Json.Object ());
            } else {
                send_command ("playback.play", PlaybackBodies.play ());
            }
        }

        public void stop () throws GLib.Error {
            send_command ("playback.stop", new Json.Object ());
        }

        public void next () throws GLib.Error {
            send_command ("playback.next", new Json.Object ());
        }

        public void previous () throws GLib.Error {
            send_command ("playback.prev", new Json.Object ());
        }

        public void seek (int64 offset) throws GLib.Error {
            /* offset is in microseconds; convert current position + offset to ms */
            var new_position_us = _position + offset;
            if (new_position_us < 0) new_position_us = 0;
            var new_position_ms = new_position_us / 1000;
            send_command ("playback.seek", PlaybackBodies.seek (new_position_ms));
        }

        public void set_position (ObjectPath track_id, int64 position) throws GLib.Error {
            /* position is in microseconds; convert to ms */
            var position_ms = position / 1000;
            if (position_ms < 0) position_ms = 0;
            send_command ("playback.seek", PlaybackBodies.seek (position_ms));
        }

        public void open_uri (string uri) throws GLib.Error {
            /* Not supported — we route through the MU queue */
        }

        /* ---- Signal ---- */
        public signal void seeked (int64 position);

        /* ---- Internal ---- */

        private void on_state_changed (string node_id, RendererState state) {
            if (node_id != active_repo.active_renderer_id) return;
            apply_state (state);
        }

        private void refresh_from_state () {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) return;

            var state = state_repo.get_state (renderer_id);
            if (state != null) {
                apply_state (state);
            }
        }

        private void apply_state (RendererState state) {
            var changed_props = new HashTable<string, Variant> (str_hash, str_equal);

            /* Playback status */
            if (state.playback != null) {
                var new_status = status_to_mpris (state.playback.status);
                if (new_status != _playback_status) {
                    _playback_status = new_status;
                    changed_props.insert ("PlaybackStatus",
                        new Variant.string (_playback_status));
                }

                /* Volume */
                var new_vol = state.playback.volume;
                if ((new_vol - _volume).abs () > 0.001) {
                    _volume = new_vol;
                    changed_props.insert ("Volume",
                        new Variant.double (_volume));
                }

                /* Position (ms -> us) */
                _position = state.playback.position_ms * 1000;
            }

            /* Metadata */
            if (state.current != null && state.current.metadata != null) {
                var new_meta = build_metadata (state.current);
                if (!metadata_equal (new_meta, _metadata)) {
                    _metadata = new_meta;
                    /* Build variant dict for the signal */
                    var builder = new VariantBuilder (new VariantType ("a{sv}"));
                    _metadata.foreach ((k, v) => {
                        builder.add ("{sv}", k, v);
                    });
                    changed_props.insert ("Metadata", builder.end ());
                }
            }

            /* Emit PropertiesChanged if anything changed */
            if (changed_props.size () > 0) {
                emit_properties_changed (changed_props);
            }
        }

        private string status_to_mpris (string status) {
            switch (status) {
                case "playing": return "Playing";
                case "paused":  return "Paused";
                default:        return "Stopped";
            }
        }

        private HashTable<string, Variant> build_metadata (CurrentItemState current) {
            var meta = new HashTable<string, Variant> (str_hash, str_equal);

            /* Track ID (required) */
            var track_path = "/org/mpris/MediaPlayer2/Track/%s".printf (
                current.item_id.length > 0
                    ? sanitize_track_id (current.item_id)
                    : "1"
            );
            meta.insert ("mpris:trackid", new Variant.object_path (track_path));

            if (current.metadata == null) return meta;

            var json_meta = current.metadata;

            /* Title */
            if (json_meta.has_member ("title")) {
                meta.insert ("xesam:title",
                    new Variant.string (json_meta.get_string_member ("title")));
            }

            /* Artist (MPRIS wants string array) */
            if (json_meta.has_member ("artist")) {
                string[] artists = { json_meta.get_string_member ("artist") };
                meta.insert ("xesam:artist", new Variant.strv (artists));
            }

            /* Album */
            if (json_meta.has_member ("album")) {
                meta.insert ("xesam:album",
                    new Variant.string (json_meta.get_string_member ("album")));
            }

            /* Duration (ms -> us) */
            if (json_meta.has_member ("durationMs")) {
                var dur_us = json_meta.get_int_member ("durationMs") * 1000;
                meta.insert ("mpris:length", new Variant.int64 (dur_us));
            }

            /* Art URL */
            if (json_meta.has_member ("artUrl")) {
                meta.insert ("mpris:artUrl",
                    new Variant.string (json_meta.get_string_member ("artUrl")));
            }

            return meta;
        }

        /**
         * Sanitize an item_id into a valid D-Bus object path element.
         * Only allows [A-Za-z0-9_]; everything else becomes '_'.
         */
        private string sanitize_track_id (string id) {
            var sb = new StringBuilder ();
            for (int i = 0; i < id.length; i++) {
                var c = id[i];
                if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
                    (c >= '0' && c <= '9') || c == '_') {
                    sb.append_c (c);
                } else {
                    sb.append_c ('_');
                }
            }
            var result = sb.str;
            /* Object path elements must not be empty */
            return result.length > 0 ? result : "1";
        }

        private bool metadata_equal (HashTable<string, Variant> a,
                                     HashTable<string, Variant> b) {
            if (a.size () != b.size ()) return false;

            /* Quick check: compare trackid only for efficiency */
            var a_id = a.lookup ("mpris:trackid");
            var b_id = b.lookup ("mpris:trackid");
            if (a_id == null || b_id == null) return false;
            return a_id.equal (b_id);
        }

        private void emit_properties_changed (HashTable<string, Variant> changed) {
            if (conn == null) return;

            var builder = new VariantBuilder (new VariantType ("a{sv}"));
            changed.foreach ((k, v) => {
                builder.add ("{sv}", k, v);
            });

            var invalidated_builder = new VariantBuilder (new VariantType ("as"));

            try {
                conn.emit_signal (
                    null,
                    "/org/mpris/MediaPlayer2",
                    "org.freedesktop.DBus.Properties",
                    "PropertiesChanged",
                    new Variant (
                        "(sa{sv}as)",
                        "org.mpris.MediaPlayer2.Player",
                        builder,
                        invalidated_builder
                    )
                );
            } catch (GLib.Error e) {
                warning ("MPRIS2: failed to emit PropertiesChanged: %s", e.message);
            }
        }

        /**
         * Send a command to the active renderer via correlator + lease.
         */
        private void send_command (string cmd_type, Json.Object body) {
            var renderer_id = active_repo.active_renderer_id;
            if (renderer_id.length == 0) return;

            lease_mgr.ensure_lease.begin (renderer_id, (obj, res) => {
                var lease = lease_mgr.ensure_lease.end (res);
                if (lease != null) {
                    correlator.send_fire_and_forget (renderer_id, cmd_type, body, lease);
                }
            });
        }
    }

    /**
     * Mpris2 — owns the bus name and registers both D-Bus objects.
     */
    public class Mpris2 : Object {

        private MprisRoot root;
        private MprisPlayer player;
        private uint bus_owner_id = 0;
        private uint root_registration_id = 0;
        private uint player_registration_id = 0;

        public Mpris2 (Gtk.Application app,
                       RendererStateRepository state_repo,
                       ActiveRendererRepository active_repo,
                       CommandCorrelator correlator,
                       LeaseManager lease_mgr) {
            root = new MprisRoot (app);
            player = new MprisPlayer (state_repo, active_repo, correlator, lease_mgr);
        }

        /**
         * Register on the session D-Bus.
         */
        public void start () {
            bus_owner_id = Bus.own_name (
                BusType.SESSION,
                "org.mpris.MediaPlayer2.mediautopia",
                BusNameOwnerFlags.NONE,
                on_bus_acquired,
                on_name_acquired,
                on_name_lost
            );
        }

        /**
         * Release the bus name and unregister objects.
         */
        public void stop () {
            if (bus_owner_id != 0) {
                Bus.unown_name (bus_owner_id);
                bus_owner_id = 0;
            }
        }

        private void on_bus_acquired (DBusConnection conn, string name) {
            player.set_connection (conn);

            try {
                root_registration_id = conn.register_object (
                    "/org/mpris/MediaPlayer2", root);
                player_registration_id = conn.register_object (
                    "/org/mpris/MediaPlayer2", player);
            } catch (IOError e) {
                warning ("MPRIS2: failed to register D-Bus objects: %s", e.message);
            }

            message ("MPRIS2: registered on session bus as %s", name);
        }

        private void on_name_acquired (DBusConnection conn, string name) {
            /* Bus name successfully owned */
        }

        private void on_name_lost (DBusConnection conn, string name) {
            warning ("MPRIS2: lost bus name %s", name);
        }
    }
}
