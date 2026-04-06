/* bodies.vala — Command body builders and reply parsers for the MU protocol.
 * Ported from the Android app's Kotlin body classes.
 */

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

        public Json.Object resolve (string item_id, bool metadata_only = false) {
            var obj = new Json.Object ();
            obj.set_string_member ("itemId", item_id);
            obj.set_boolean_member ("metadataOnly", metadata_only);
            return obj;
        }

        public Json.Object resolve_batch (string[] item_ids, bool metadata_only = false) {
            var obj = new Json.Object ();
            var arr = new Json.Array ();
            foreach (var item_id in item_ids) {
                arr.add_string_element (item_id);
            }
            obj.set_array_member ("itemIds", arr);
            obj.set_boolean_member ("metadataOnly", metadata_only);
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

        public Json.Object build (string? ref_id, string? url, string? mime,
                                   bool byte_range, Json.Object? metadata) {
            var entry = new Json.Object ();

            if (ref_id != null) {
                entry.set_string_member ("ref", ref_id);
            }

            if (url != null) {
                var resolved = new Json.Object ();
                resolved.set_string_member ("url", url);
                if (mime != null) {
                    resolved.set_string_member ("mime", mime);
                }
                resolved.set_boolean_member ("byteRange", byte_range);
                entry.set_object_member ("resolved", resolved);
            }

            if (metadata != null) {
                entry.set_object_member ("metadata", metadata);
            }

            return entry;
        }
    }

    /* ---- Reply parsers ---- */

    public class QueueItem : GLib.Object {
        public string queue_entry_id { get; set; default = ""; }
        public string item_id { get; set; default = ""; }
        public Json.Object? metadata { get; set; default = null; }

        public QueueItem () {
            Object ();
        }

        public static QueueItem from_json (Json.Object obj) {
            var item = new QueueItem ();
            item.queue_entry_id = obj.has_member ("queueEntryId")
                ? obj.get_string_member ("queueEntryId") : "";
            item.item_id = obj.has_member ("itemId")
                ? obj.get_string_member ("itemId") : "";
            if (obj.has_member ("metadata") && !obj.get_null_member ("metadata")) {
                item.metadata = obj.get_object_member ("metadata");
            }
            return item;
        }
    }

    public class QueueGetReply : GLib.Object {
        public int64 revision { get; set; default = 0; }
        public int64 index { get; set; default = -1; }
        public GenericArray<QueueItem> entries { get; set; }

        public QueueGetReply () {
            Object ();
            entries = new GenericArray<QueueItem> ();
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
                    reply.entries.add (QueueItem.from_json (arr.get_object_element (i)));
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
}
