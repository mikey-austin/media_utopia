/* presence.vala — Presence data structures parsed from MQTT presence messages.
 * Ported from the Android app's Kotlin data classes.
 */

namespace Mu {

    public class PresenceSource : GLib.Object {
        public string id { get; set; default = ""; }
        public string name { get; set; default = ""; }

        public PresenceSource () {
            Object ();
        }

        public static PresenceSource from_json (Json.Object obj) {
            var source = new PresenceSource ();
            source.id = obj.has_member ("id") ? obj.get_string_member ("id") : "";
            source.name = obj.has_member ("name") ? obj.get_string_member ("name") : "";
            return source;
        }
    }

    public class Presence : GLib.Object {
        public string node_id { get; set; default = ""; }
        public string kind { get; set; default = ""; }
        public string name { get; set; default = ""; }
        public Json.Object? caps { get; set; default = null; }
        public Json.Object? endpoints { get; set; default = null; }
        public string? source { get; set; default = null; }
        public GenericArray<PresenceSource>? sources { get; set; default = null; }
        public int64 ts { get; set; default = 0; }

        public Presence () {
            Object ();
        }

        public static Presence from_json (Json.Object obj) {
            var presence = new Presence ();
            presence.node_id = obj.has_member ("nodeId")
                ? obj.get_string_member ("nodeId") : "";
            presence.kind = obj.has_member ("kind")
                ? obj.get_string_member ("kind") : "";
            presence.name = obj.has_member ("name")
                ? obj.get_string_member ("name") : "";

            if (obj.has_member ("caps") && !obj.get_null_member ("caps")) {
                presence.caps = obj.get_object_member ("caps");
            }
            if (obj.has_member ("endpoints") && !obj.get_null_member ("endpoints")) {
                presence.endpoints = obj.get_object_member ("endpoints");
            }
            if (obj.has_member ("source") && !obj.get_null_member ("source")) {
                presence.source = obj.get_string_member ("source");
            }

            if (obj.has_member ("sources") && !obj.get_null_member ("sources")) {
                var arr = obj.get_array_member ("sources");
                var list = new GenericArray<PresenceSource> ();
                for (uint i = 0; i < arr.get_length (); i++) {
                    list.add (PresenceSource.from_json (arr.get_object_element (i)));
                }
                presence.sources = list;
            }

            presence.ts = obj.has_member ("ts") ? obj.get_int_member ("ts") : 0;

            return presence;
        }

        public static Presence from_json_string (string json_str) throws GLib.Error {
            var parser = new Json.Parser ();
            parser.load_from_data (json_str);
            return from_json (parser.get_root ().get_object ());
        }
    }
}
