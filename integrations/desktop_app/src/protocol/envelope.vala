/* envelope.vala — Command and reply envelope data structures for the MU protocol.
 * Ported from the Android app's Kotlin data classes.
 */

namespace Mu {

    public struct Lease {
        public string session_id;
        public string token;

        public static Lease from_json (Json.Object obj) {
            return Lease () {
                session_id = obj.has_member ("sessionId") ? obj.get_string_member ("sessionId") : "",
                token = obj.has_member ("token") ? obj.get_string_member ("token") : ""
            };
        }

        public Json.Object to_json () {
            var obj = new Json.Object ();
            obj.set_string_member ("sessionId", session_id);
            obj.set_string_member ("token", token);
            return obj;
        }
    }

    public struct ReplyError {
        public string code;
        public string message;
        public Json.Object? detail;

        public static ReplyError from_json (Json.Object obj) {
            Json.Object? detail_obj = null;
            if (obj.has_member ("detail") && !obj.get_null_member ("detail")) {
                detail_obj = obj.get_object_member ("detail");
            }
            return ReplyError () {
                code = obj.has_member ("code") ? obj.get_string_member ("code") : "",
                message = obj.has_member ("message") ? obj.get_string_member ("message") : "",
                detail = detail_obj
            };
        }
    }

    public class CommandEnvelope : GLib.Object {
        public string id { get; set; default = ""; }
        public string envelope_type { get; set; default = ""; }
        public int64 ts { get; set; default = 0; }
        public string from { get; set; default = ""; }
        public string? reply_to { get; set; default = null; }
        public Lease? lease { get; set; default = null; }
        public int64 if_revision { get; set; default = -1; }
        public Json.Object body { get; set; }

        public CommandEnvelope (string id, string envelope_type, int64 ts, string from,
                                Json.Object body) {
            Object ();
            this.id = id;
            this.envelope_type = envelope_type;
            this.ts = ts;
            this.from = from;
            this.body = body;
        }

        public string to_json_string () {
            var obj = new Json.Object ();
            obj.set_string_member ("id", id);
            obj.set_string_member ("type", envelope_type);
            obj.set_int_member ("ts", ts);
            obj.set_string_member ("from", from);

            if (reply_to != null) {
                obj.set_string_member ("replyTo", reply_to);
            }

            if (lease != null) {
                obj.set_object_member ("lease", lease.to_json ());
            }

            if (if_revision >= 0) {
                obj.set_int_member ("ifRevision", if_revision);
            }

            obj.set_object_member ("body", body);

            var node = new Json.Node.alloc ().init_object (obj);
            return Json.to_string (node, false);
        }
    }

    public class ReplyEnvelope : GLib.Object {
        public string id { get; set; default = ""; }
        public string envelope_type { get; set; default = ""; }
        public bool ok { get; set; default = false; }
        public int64 ts { get; set; default = 0; }
        public Json.Object? body { get; set; default = null; }
        public ReplyError? err { get; set; default = null; }

        public ReplyEnvelope () {
            Object ();
        }

        public static ReplyEnvelope from_json_string (string json_str) throws GLib.Error {
            var parser = new Json.Parser ();
            parser.load_from_data (json_str);
            var obj = parser.get_root ().get_object ();

            var envelope = new ReplyEnvelope ();
            envelope.id = obj.has_member ("id") ? obj.get_string_member ("id") : "";
            envelope.envelope_type = obj.has_member ("type") ? obj.get_string_member ("type") : "";
            envelope.ok = obj.has_member ("ok") ? obj.get_boolean_member ("ok") : false;
            envelope.ts = obj.has_member ("ts") ? obj.get_int_member ("ts") : 0;

            if (obj.has_member ("body") && !obj.get_null_member ("body")) {
                envelope.body = obj.get_object_member ("body");
            }

            if (obj.has_member ("err") && !obj.get_null_member ("err")) {
                envelope.err = ReplyError.from_json (obj.get_object_member ("err"));
            }

            return envelope;
        }
    }
}
