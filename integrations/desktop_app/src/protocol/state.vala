/* state.vala — Read-only state data structures parsed from MQTT state messages.
 * Ported from the Android app's Kotlin data classes.
 */

namespace Mu {

    public class SessionState : GLib.Object {
        public string id { get; set; default = ""; }
        public string owner { get; set; default = ""; }
        public int64 lease_expires_at { get; set; default = 0; }

        public SessionState () {
            Object ();
        }

        public static SessionState from_json (Json.Object obj) {
            var state = new SessionState ();
            state.id = obj.has_member ("id") ? obj.get_string_member ("id") : "";
            state.owner = obj.has_member ("owner") ? obj.get_string_member ("owner") : "";
            state.lease_expires_at = obj.has_member ("leaseExpiresAt")
                ? obj.get_int_member ("leaseExpiresAt") : 0;
            return state;
        }
    }

    public class PlaybackState : GLib.Object {
        public string status { get; set; default = "stopped"; }
        public int64 position_ms { get; set; default = 0; }
        public int64 duration_ms { get; set; default = 0; }
        public double volume { get; set; default = 1.0; }
        public bool mute { get; set; default = false; }

        public PlaybackState () {
            Object ();
        }

        public static PlaybackState from_json (Json.Object obj) {
            var state = new PlaybackState ();
            state.status = obj.has_member ("status") ? obj.get_string_member ("status") : "stopped";
            state.position_ms = obj.has_member ("positionMs")
                ? obj.get_int_member ("positionMs") : 0;
            state.duration_ms = obj.has_member ("durationMs")
                ? obj.get_int_member ("durationMs") : 0;
            state.volume = obj.has_member ("volume")
                ? obj.get_double_member ("volume") : 1.0;
            state.mute = obj.has_member ("mute")
                ? obj.get_boolean_member ("mute") : false;
            return state;
        }
    }

    public class QueueState : GLib.Object {
        public int64 revision { get; set; default = 0; }
        public int64 length { get; set; default = 0; }
        public int64 index { get; set; default = -1; }
        public bool repeat { get; set; default = false; }
        public string repeat_mode { get; set; default = "off"; }
        public bool shuffle { get; set; default = false; }

        public QueueState () {
            Object ();
        }

        public static QueueState from_json (Json.Object obj) {
            var state = new QueueState ();
            state.revision = obj.has_member ("revision")
                ? obj.get_int_member ("revision") : 0;
            state.length = obj.has_member ("length")
                ? obj.get_int_member ("length") : 0;
            state.index = obj.has_member ("index")
                ? obj.get_int_member ("index") : -1;
            state.repeat = obj.has_member ("repeat")
                ? obj.get_boolean_member ("repeat") : false;
            state.repeat_mode = obj.has_member ("repeatMode")
                ? obj.get_string_member ("repeatMode") : "off";
            state.shuffle = obj.has_member ("shuffle")
                ? obj.get_boolean_member ("shuffle") : false;
            return state;
        }
    }

    public class CurrentItemState : GLib.Object {
        public string queue_entry_id { get; set; default = ""; }
        public string item_id { get; set; default = ""; }
        public Json.Object? metadata { get; set; default = null; }

        public CurrentItemState () {
            Object ();
        }

        public static CurrentItemState from_json (Json.Object obj) {
            var state = new CurrentItemState ();
            state.queue_entry_id = obj.has_member ("queueEntryId")
                ? obj.get_string_member ("queueEntryId") : "";
            state.item_id = obj.has_member ("itemId")
                ? obj.get_string_member ("itemId") : "";
            if (obj.has_member ("metadata") && !obj.get_null_member ("metadata")) {
                state.metadata = obj.get_object_member ("metadata");
            }
            return state;
        }
    }

    public class RendererState : GLib.Object {
        public SessionState? session { get; set; default = null; }
        public PlaybackState? playback { get; set; default = null; }
        public QueueState? queue { get; set; default = null; }
        public CurrentItemState? current { get; set; default = null; }
        public int64 state_version { get; set; default = 0; }
        public int64 ts { get; set; default = 0; }

        public RendererState () {
            Object ();
        }

        public static RendererState from_json (Json.Object obj) {
            var state = new RendererState ();

            if (obj.has_member ("session") && !obj.get_null_member ("session")) {
                state.session = SessionState.from_json (obj.get_object_member ("session"));
            }
            if (obj.has_member ("playback") && !obj.get_null_member ("playback")) {
                state.playback = PlaybackState.from_json (obj.get_object_member ("playback"));
            }
            if (obj.has_member ("queue") && !obj.get_null_member ("queue")) {
                state.queue = QueueState.from_json (obj.get_object_member ("queue"));
            }
            if (obj.has_member ("current") && !obj.get_null_member ("current")) {
                state.current = CurrentItemState.from_json (obj.get_object_member ("current"));
            }
            state.state_version = obj.has_member ("stateVersion")
                ? obj.get_int_member ("stateVersion") : 0;
            state.ts = obj.has_member ("ts")
                ? obj.get_int_member ("ts") : 0;

            return state;
        }

        public static RendererState from_json_string (string json_str) throws GLib.Error {
            var parser = new Json.Parser ();
            parser.load_from_data (json_str);
            return from_json (parser.get_root ().get_object ());
        }
    }
}
