/* state.vala — Read-only state data structures parsed from MQTT state messages.
 * Mirrors the canonical pkg/mu wire types (LibraryItemRef, DisplayMetadata,
 * ResolvedSource, queue entries with structured library_ref/resolved/display).
 */

namespace Mu {

    public const string LIBRARY_ITEM_KIND = "libraryItem";

    /* ---- Library item reference ---- */

    public class LibraryItemRef : GLib.Object {
        public string kind { get; set; default = LIBRARY_ITEM_KIND; }
        public string library_id { get; set; default = ""; }
        public string item_id { get; set; default = ""; }

        public LibraryItemRef () {
            Object ();
        }

        public LibraryItemRef.with_ids (string library_id, string item_id) {
            Object ();
            this.library_id = library_id;
            this.item_id = item_id;
        }

        public bool is_valid () {
            return kind == LIBRARY_ITEM_KIND
                && library_id.length > 0
                && item_id.length > 0;
        }

        public static LibraryItemRef? from_json (Json.Object obj) {
            var r = new LibraryItemRef ();
            r.kind = obj.has_member ("kind") ? obj.get_string_member ("kind") : LIBRARY_ITEM_KIND;
            r.library_id = obj.has_member ("libraryId")
                ? obj.get_string_member ("libraryId") : "";
            r.item_id = obj.has_member ("itemId")
                ? obj.get_string_member ("itemId") : "";
            if (!r.is_valid ()) return null;
            return r;
        }

        public Json.Object to_json () {
            var obj = new Json.Object ();
            obj.set_string_member ("kind", LIBRARY_ITEM_KIND);
            obj.set_string_member ("libraryId", library_id);
            obj.set_string_member ("itemId", item_id);
            return obj;
        }
    }

    /* ---- Display metadata (denormalized UI snapshot) ---- */

    public class DisplayMetadata : GLib.Object {
        public string title { get; set; default = ""; }
        public string artist { get; set; default = ""; }
        public GenericArray<string>? artists { get; set; default = null; }
        public string album { get; set; default = ""; }
        public string artwork_url { get; set; default = ""; }
        public int64 duration_ms { get; set; default = 0; }
        public string media_type { get; set; default = ""; }

        /* Optional technical format info (hi-res badge). Not all backends
         * supply these yet — absent fields simply hide the badge. */
        public string format { get; set; default = ""; }
        public int64 bit_depth { get; set; default = 0; }
        public int64 sample_rate { get; set; default = 0; }

        public DisplayMetadata () {
            Object ();
        }

        public static DisplayMetadata from_json (Json.Object obj) {
            var d = new DisplayMetadata ();
            d.title = obj.has_member ("title") ? (obj.get_string_member ("title") ?? "") : "";
            d.artist = obj.has_member ("artist") ? (obj.get_string_member ("artist") ?? "") : "";
            if (obj.has_member ("artists") && !obj.get_null_member ("artists")) {
                var arr = obj.get_array_member ("artists");
                var list = new GenericArray<string> ();
                for (uint i = 0; i < arr.get_length (); i++) {
                    var node = arr.get_element (i);
                    if (node.get_node_type () == Json.NodeType.VALUE) {
                        var s = node.get_string ();
                        if (s != null && s.length > 0) list.add (s);
                    }
                }
                if (list.length > 0) d.artists = list;
            }
            d.album = obj.has_member ("album") ? (obj.get_string_member ("album") ?? "") : "";
            d.artwork_url = obj.has_member ("artworkUrl")
                ? (obj.get_string_member ("artworkUrl") ?? "") : "";
            d.duration_ms = obj.has_member ("durationMs")
                ? obj.get_int_member ("durationMs") : 0;
            d.media_type = obj.has_member ("mediaType")
                ? (obj.get_string_member ("mediaType") ?? "") : "";
            d.format = obj.has_member ("format")
                ? (obj.get_string_member ("format") ?? "") : "";
            d.bit_depth = obj.has_member ("bitDepth")
                ? obj.get_int_member ("bitDepth") : 0;
            d.sample_rate = obj.has_member ("sampleRate")
                ? obj.get_int_member ("sampleRate") : 0;
            return d;
        }

        public Json.Object to_json () {
            var obj = new Json.Object ();
            if (title.length > 0) obj.set_string_member ("title", title);
            if (artist.length > 0) obj.set_string_member ("artist", artist);
            if (artists != null && artists.length > 0) {
                var arr = new Json.Array ();
                for (uint i = 0; i < artists.length; i++) {
                    arr.add_string_element (artists[i]);
                }
                obj.set_array_member ("artists", arr);
            }
            if (album.length > 0) obj.set_string_member ("album", album);
            if (artwork_url.length > 0) obj.set_string_member ("artworkUrl", artwork_url);
            if (duration_ms > 0) obj.set_int_member ("durationMs", duration_ms);
            if (media_type.length > 0) obj.set_string_member ("mediaType", media_type);
            if (format.length > 0) obj.set_string_member ("format", format);
            if (bit_depth > 0) obj.set_int_member ("bitDepth", bit_depth);
            if (sample_rate > 0) obj.set_int_member ("sampleRate", sample_rate);
            return obj;
        }

        public string artist_display () {
            if (artists != null && artists.length > 0) {
                var sb = new StringBuilder ();
                for (uint i = 0; i < artists.length; i++) {
                    if (i > 0) sb.append (", ");
                    sb.append (artists[i]);
                }
                return sb.str;
            }
            return artist;
        }
    }

    /* ---- Resolved playable source ---- */

    public class ResolvedSource : GLib.Object {
        public string url { get; set; default = ""; }
        public string mime { get; set; default = ""; }
        public bool byte_range { get; set; default = false; }

        public ResolvedSource () {
            Object ();
        }

        public ResolvedSource.with_url (string url, string mime = "", bool byte_range = false) {
            Object ();
            this.url = url;
            this.mime = mime;
            this.byte_range = byte_range;
        }

        public static ResolvedSource? from_json (Json.Object obj) {
            var r = new ResolvedSource ();
            r.url = obj.has_member ("url") ? (obj.get_string_member ("url") ?? "") : "";
            if (r.url.length == 0) return null;
            r.mime = obj.has_member ("mime") ? (obj.get_string_member ("mime") ?? "") : "";
            r.byte_range = obj.has_member ("byteRange")
                ? obj.get_boolean_member ("byteRange") : false;
            return r;
        }

        public Json.Object to_json () {
            var obj = new Json.Object ();
            obj.set_string_member ("url", url);
            if (mime.length > 0) obj.set_string_member ("mime", mime);
            obj.set_boolean_member ("byteRange", byte_range);
            return obj;
        }
    }

    /* ---- Session, playback, queue ---- */

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
        public LibraryItemRef? library_ref { get; set; default = null; }
        public ResolvedSource? resolved { get; set; default = null; }
        public DisplayMetadata? display { get; set; default = null; }

        public CurrentItemState () {
            Object ();
        }

        public static CurrentItemState from_json (Json.Object obj) {
            var state = new CurrentItemState ();
            state.queue_entry_id = obj.has_member ("queueEntryId")
                ? obj.get_string_member ("queueEntryId") : "";
            if (obj.has_member ("ref") && !obj.get_null_member ("ref")) {
                state.library_ref = LibraryItemRef.from_json (obj.get_object_member ("ref"));
            }
            if (obj.has_member ("resolved") && !obj.get_null_member ("resolved")) {
                state.resolved = ResolvedSource.from_json (obj.get_object_member ("resolved"));
            }
            if (obj.has_member ("display") && !obj.get_null_member ("display")) {
                state.display = DisplayMetadata.from_json (obj.get_object_member ("display"));
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
