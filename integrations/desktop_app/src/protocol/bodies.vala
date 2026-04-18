/* bodies.vala — Command body builders and reply parsers for the MU protocol. */

namespace Mu {

    /* ---- Session commands ---- */

    namespace SessionBodies {

        public Json.Object acquire (int64 ttl_ms = 300000) {
            var obj = new Json.Object ();
            obj.set_int_member ("ttlMs", ttl_ms);
            return obj;
        }

        public Json.Object renew (int64 ttl_ms = 300000) {
            var obj = new Json.Object ();
            obj.set_int_member ("ttlMs", ttl_ms);
            return obj;
        }
    }

    /* ---- Playback commands ---- */

    namespace PlaybackBodies {

        public Json.Object play (int64 index = -1) {
            var obj = new Json.Object ();
            if (index >= 0) {
                obj.set_int_member ("index", index);
            }
            return obj;
        }

        public Json.Object seek (int64 position_ms) {
            var obj = new Json.Object ();
            obj.set_int_member ("positionMs", position_ms);
            return obj;
        }

        public Json.Object set_volume (double volume) {
            var obj = new Json.Object ();
            obj.set_double_member ("volume", volume);
            return obj;
        }

        public Json.Object set_mute (bool mute) {
            var obj = new Json.Object ();
            obj.set_boolean_member ("mute", mute);
            return obj;
        }
    }

    /* ---- Queue commands ---- */

    namespace QueueBodies {

        public Json.Object get (int64 from_idx, int64 count, string resolve = "") {
            var obj = new Json.Object ();
            obj.set_int_member ("from", from_idx);
            obj.set_int_member ("count", count);
            if (resolve.length > 0) {
                obj.set_string_member ("resolve", resolve);
            }
            return obj;
        }

        public Json.Object set_entries (int64 start_index, Json.Array entries) {
            var obj = new Json.Object ();
            obj.set_int_member ("startIndex", start_index);
            obj.set_array_member ("entries", entries);
            return obj;
        }

        public Json.Object add (string position, Json.Array entries, int64 at_index = -1) {
            var obj = new Json.Object ();
            obj.set_string_member ("position", position);
            obj.set_array_member ("entries", entries);
            if (at_index >= 0) {
                obj.set_int_member ("atIndex", at_index);
            }
            return obj;
        }

        public Json.Object remove (string queue_entry_id = "", int64 index = -1) {
            var obj = new Json.Object ();
            if (queue_entry_id.length > 0) {
                obj.set_string_member ("queueEntryId", queue_entry_id);
            }
            if (index >= 0) {
                obj.set_int_member ("index", index);
            }
            return obj;
        }

        public Json.Object move (int64 from_index, int64 to_index) {
            var obj = new Json.Object ();
            obj.set_int_member ("fromIndex", from_index);
            obj.set_int_member ("toIndex", to_index);
            return obj;
        }

        public Json.Object clear () {
            return new Json.Object ();
        }

        public Json.Object jump (int64 index) {
            var obj = new Json.Object ();
            obj.set_int_member ("index", index);
            return obj;
        }

        public Json.Object shuffle (int64 seed) {
            var obj = new Json.Object ();
            obj.set_int_member ("seed", seed);
            return obj;
        }

        public Json.Object set_shuffle (bool shuffle_on) {
            var obj = new Json.Object ();
            obj.set_boolean_member ("shuffle", shuffle_on);
            return obj;
        }

        public Json.Object set_repeat (bool repeat_on, string mode) {
            var obj = new Json.Object ();
            obj.set_boolean_member ("repeat", repeat_on);
            obj.set_string_member ("mode", mode);
            return obj;
        }

        public Json.Object load_playlist (string server_id, string playlist_id,
                                           string mode, string resolve = "auto") {
            var obj = new Json.Object ();
            obj.set_string_member ("playlistServerId", server_id);
            obj.set_string_member ("playlistId", playlist_id);
            obj.set_string_member ("mode", mode);
            obj.set_string_member ("resolve", resolve);
            return obj;
        }
    }

    /* ---- Library commands ---- */

    namespace LibraryBodies {

        public Json.Object browse (string container_id, int64 start, int64 count) {
            var obj = new Json.Object ();
            obj.set_string_member ("containerId", container_id);
            obj.set_int_member ("start", start);
            obj.set_int_member ("count", count);
            return obj;
        }

        public Json.Object search (string query, int64 start, int64 count) {
            var obj = new Json.Object ();
            obj.set_string_member ("query", query);
            obj.set_int_member ("start", start);
            obj.set_int_member ("count", count);
            return obj;
        }

        public Json.Object get_item (LibraryItemRef library_ref) {
            var obj = new Json.Object ();
            obj.set_object_member ("ref", library_ref.to_json ());
            return obj;
        }

        public Json.Object get_items (LibraryItemRef[] refs) {
            var obj = new Json.Object ();
            var arr = new Json.Array ();
            foreach (var r in refs) {
                arr.add_object_element (r.to_json ());
            }
            obj.set_array_member ("refs", arr);
            return obj;
        }

        public Json.Object resolve_sources (LibraryItemRef library_ref) {
            var obj = new Json.Object ();
            obj.set_object_member ("ref", library_ref.to_json ());
            return obj;
        }

        public Json.Object resolve_sources_batch (LibraryItemRef[] refs) {
            var obj = new Json.Object ();
            var arr = new Json.Array ();
            foreach (var r in refs) {
                arr.add_object_element (r.to_json ());
            }
            obj.set_array_member ("refs", arr);
            return obj;
        }
    }

    /* ---- Playlist commands ---- */

    namespace PlaylistBodies {

        public Json.Object list (string owner) {
            var obj = new Json.Object ();
            obj.set_string_member ("owner", owner);
            return obj;
        }

        public Json.Object get_playlist (string playlist_id) {
            var obj = new Json.Object ();
            obj.set_string_member ("playlistId", playlist_id);
            return obj;
        }
    }

    /* ---- Queue entry builder ---- */

    namespace QueueEntryBuilder {

        /**
         * Build a canonical queue entry: { library_ref?, resolved?, display? }.
         * At least one of library_ref/resolved must be supplied; display is optional.
         */
        public Json.Object build (LibraryItemRef? library_ref, ResolvedSource? resolved,
                                   DisplayMetadata? display) {
            var entry = new Json.Object ();

            if (library_ref != null && library_ref.is_valid ()) {
                entry.set_object_member ("ref", library_ref.to_json ());
            }

            if (resolved != null && resolved.url.length > 0) {
                entry.set_object_member ("resolved", resolved.to_json ());
            }

            if (display != null) {
                entry.set_object_member ("display", display.to_json ());
            }

            return entry;
        }
    }

    /* ---- Reply / queue entry parsing ---- */

    public class QueueEntry : GLib.Object {
        public string queue_entry_id { get; set; default = ""; }
        public LibraryItemRef? library_ref { get; set; default = null; }
        public ResolvedSource? resolved { get; set; default = null; }
        public DisplayMetadata? display { get; set; default = null; }

        public QueueEntry () {
            Object ();
        }

        public static QueueEntry from_json (Json.Object obj) {
            var item = new QueueEntry ();
            item.queue_entry_id = obj.has_member ("queueEntryId")
                ? obj.get_string_member ("queueEntryId") : "";
            if (obj.has_member ("ref") && !obj.get_null_member ("ref")) {
                item.library_ref = LibraryItemRef.from_json (obj.get_object_member ("ref"));
            }
            if (obj.has_member ("resolved") && !obj.get_null_member ("resolved")) {
                item.resolved = ResolvedSource.from_json (obj.get_object_member ("resolved"));
            }
            if (obj.has_member ("display") && !obj.get_null_member ("display")) {
                item.display = DisplayMetadata.from_json (obj.get_object_member ("display"));
            }
            return item;
        }

        public Json.Object to_json () {
            var obj = new Json.Object ();
            if (queue_entry_id.length > 0) {
                obj.set_string_member ("queueEntryId", queue_entry_id);
            }
            if (library_ref != null && library_ref.is_valid ()) {
                obj.set_object_member ("ref", library_ref.to_json ());
            }
            if (resolved != null && resolved.url.length > 0) {
                obj.set_object_member ("resolved", resolved.to_json ());
            }
            if (display != null) {
                obj.set_object_member ("display", display.to_json ());
            }
            return obj;
        }
    }

    public class QueueGetReply : GLib.Object {
        public int64 revision { get; set; default = 0; }
        public int64 index { get; set; default = -1; }
        public GenericArray<QueueEntry> entries { get; set; }

        public QueueGetReply () {
            Object ();
            entries = new GenericArray<QueueEntry> ();
        }

        public static QueueGetReply from_json (Json.Object obj) {
            var reply = new QueueGetReply ();
            reply.revision = obj.has_member ("revision")
                ? obj.get_int_member ("revision") : 0;
            reply.index = obj.has_member ("index")
                ? obj.get_int_member ("index") : -1;

            if (obj.has_member ("entries") && !obj.get_null_member ("entries")) {
                var arr = obj.get_array_member ("entries");
                for (uint i = 0; i < arr.get_length (); i++) {
                    reply.entries.add (QueueEntry.from_json (arr.get_object_element (i)));
                }
            }

            return reply;
        }
    }

    public class PlaylistSummary : GLib.Object {
        public string playlist_id { get; set; default = ""; }
        public string name { get; set; default = ""; }
        public int64 revision { get; set; default = 0; }

        public PlaylistSummary () {
            Object ();
        }

        public static PlaylistSummary from_json (Json.Object obj) {
            var summary = new PlaylistSummary ();
            summary.playlist_id = obj.has_member ("playlistId")
                ? obj.get_string_member ("playlistId") : "";
            summary.name = obj.has_member ("name")
                ? obj.get_string_member ("name") : "";
            summary.revision = obj.has_member ("revision")
                ? obj.get_int_member ("revision") : 0;
            return summary;
        }
    }

    public class PlaylistListReply : GLib.Object {
        public GenericArray<PlaylistSummary> playlists { get; set; }

        public PlaylistListReply () {
            Object ();
            playlists = new GenericArray<PlaylistSummary> ();
        }

        public static PlaylistListReply from_json (Json.Object obj) {
            var reply = new PlaylistListReply ();

            if (obj.has_member ("playlists") && !obj.get_null_member ("playlists")) {
                var arr = obj.get_array_member ("playlists");
                for (uint i = 0; i < arr.get_length (); i++) {
                    reply.playlists.add (
                        PlaylistSummary.from_json (arr.get_object_element (i))
                    );
                }
            }

            return reply;
        }
    }

    /* ---- Library reply parsers ---- */

    public class LibraryItemReply : GLib.Object {
        public LibraryItemRef? library_ref { get; set; default = null; }
        public DisplayMetadata? display { get; set; default = null; }
        public Json.Object? attributes { get; set; default = null; }
        public string err_code { get; set; default = ""; }
        public string err_message { get; set; default = ""; }

        public LibraryItemReply () {
            Object ();
        }

        public static LibraryItemReply from_json (Json.Object obj) {
            var item = new LibraryItemReply ();
            if (obj.has_member ("ref") && !obj.get_null_member ("ref")) {
                item.library_ref = LibraryItemRef.from_json (obj.get_object_member ("ref"));
            }
            if (obj.has_member ("display") && !obj.get_null_member ("display")) {
                item.display = DisplayMetadata.from_json (obj.get_object_member ("display"));
            }
            if (obj.has_member ("attributes") && !obj.get_null_member ("attributes")) {
                item.attributes = obj.get_object_member ("attributes");
            }
            if (obj.has_member ("err") && !obj.get_null_member ("err")) {
                var err = obj.get_object_member ("err");
                item.err_code = err.has_member ("code")
                    ? (err.get_string_member ("code") ?? "") : "";
                item.err_message = err.has_member ("message")
                    ? (err.get_string_member ("message") ?? "") : "";
            }
            return item;
        }
    }

    public class LibrarySourcesReply : GLib.Object {
        public LibraryItemRef? library_ref { get; set; default = null; }
        public GenericArray<ResolvedSource> sources { get; set; }
        public string err_code { get; set; default = ""; }
        public string err_message { get; set; default = ""; }

        public LibrarySourcesReply () {
            Object ();
            sources = new GenericArray<ResolvedSource> ();
        }

        public static LibrarySourcesReply from_json (Json.Object obj) {
            var item = new LibrarySourcesReply ();
            if (obj.has_member ("ref") && !obj.get_null_member ("ref")) {
                item.library_ref = LibraryItemRef.from_json (obj.get_object_member ("ref"));
            }
            if (obj.has_member ("sources") && !obj.get_null_member ("sources")) {
                var arr = obj.get_array_member ("sources");
                for (uint i = 0; i < arr.get_length (); i++) {
                    var src = ResolvedSource.from_json (arr.get_object_element (i));
                    if (src != null) item.sources.add (src);
                }
            }
            if (obj.has_member ("err") && !obj.get_null_member ("err")) {
                var err = obj.get_object_member ("err");
                item.err_code = err.has_member ("code")
                    ? (err.get_string_member ("code") ?? "") : "";
                item.err_message = err.has_member ("message")
                    ? (err.get_string_member ("message") ?? "") : "";
            }
            return item;
        }
    }
}
