/* zone_state.vala — Dedicated state shape for MU zone nodes. */

namespace Mu {

    public class ZoneState : GLib.Object {
        public double volume { get; set; default = 0.0; }
        public bool mute { get; set; default = false; }
        public string source_id { get; set; default = ""; }
        public bool connected { get; set; default = false; }
        public int64 ts { get; set; default = 0; }

        public ZoneState () {
            Object ();
        }

        public static ZoneState from_json (Json.Object obj) {
            var state = new ZoneState ();
            state.volume = obj.has_member ("volume")
                ? obj.get_double_member ("volume") : 0.0;
            state.mute = obj.has_member ("mute")
                ? obj.get_boolean_member ("mute") : false;
            state.source_id = obj.has_member ("sourceId")
                ? obj.get_string_member ("sourceId") : "";
            state.connected = obj.has_member ("connected")
                ? obj.get_boolean_member ("connected") : false;
            state.ts = obj.has_member ("ts")
                ? obj.get_int_member ("ts") : 0;
            return state;
        }

        public static ZoneState from_json_string (string json_str) throws GLib.Error {
            var parser = new Json.Parser ();
            parser.load_from_data (json_str);
            return from_json (parser.get_root ().get_object ());
        }
    }
}
