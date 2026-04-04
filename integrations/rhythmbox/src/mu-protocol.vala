/*
 * MU wire protocol types — Vala port of pkg/mu/protocol.go and bodies.go.
 * All JSON serialisation uses json-glib.
 */

namespace Mu {

    public const string BASE_TOPIC = "mu/v1";

    /* ── Topic builders ──────────────���──────────────────────── */

    public string topic_presence (string topic_base, string node_id) {
        return @"$(topic_base)/node/$(node_id)/presence";
    }

    public string topic_state (string topic_base, string node_id) {
        return @"$(topic_base)/node/$(node_id)/state";
    }

    public string topic_commands (string topic_base, string node_id) {
        return @"$(topic_base)/node/$(node_id)/cmd";
    }

    public string topic_events (string topic_base, string node_id) {
        return @"$(topic_base)/node/$(node_id)/evt";
    }

    public string topic_reply (string topic_base, string controller_id) {
        return @"$(topic_base)/reply/$(controller_id)";
    }

    /* ── Lease requirement check ────────────────────────────── */

    public bool command_requires_lease (string cmd_type) {
        switch (cmd_type) {
            case "session.renew":
            case "session.release":
            case "playback.play":
            case "playback.pause":
            case "playback.stop":
            case "playback.seek":
            case "playback.next":
            case "playback.prev":
            case "playback.toggle":
            case "playback.setVolume":
            case "playback.setMute":
            case "queue.set":
            case "queue.add":
            case "queue.remove":
            case "queue.move":
            case "queue.clear":
            case "queue.jump":
            case "queue.shuffle":
            case "queue.setShuffle":
            case "queue.setRepeat":
            case "queue.loadPlaylist":
            case "queue.loadSnapshot":
            case "queue.loadSuggestion":
                return true;
            default:
                return false;
        }
    }

    /* ── Core envelope types ────────────────────────────────── */

    public class Lease : Object {
        public string session_id { get; set; default = ""; }
        public string token      { get; set; default = ""; }

        public Json.Node to_json () {
            var b = new Json.Builder ();
            b.begin_object ();
            b.set_member_name ("sessionId"); b.add_string_value (session_id);
            b.set_member_name ("token");     b.add_string_value (token);
            b.end_object ();
            return b.get_root ();
        }

        public static Lease? from_json (Json.Node node) {
            if (node.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = node.get_object ();
            var l = new Lease ();
            if (obj.has_member ("sessionId")) l.session_id = obj.get_string_member ("sessionId");
            if (obj.has_member ("token"))     l.token      = obj.get_string_member ("token");
            return l;
        }
    }

    public class CommandEnvelope : Object {
        public string    id          { get; set; default = ""; }
        public string    cmd_type    { get; set; default = ""; }
        public int64     ts          { get; set; default = 0; }
        public string    from        { get; set; default = ""; }
        public string    reply_to    { get; set; default = ""; }
        public Lease?    lease       { get; set; default = null; }
        public int64     if_revision { get; set; default = -1; }
        public Json.Node? body       { get; set; default = null; }

        public string to_json_string () {
            var b = new Json.Builder ();
            b.begin_object ();
            b.set_member_name ("id");   b.add_string_value (id);
            b.set_member_name ("type"); b.add_string_value (cmd_type);
            b.set_member_name ("ts");   b.add_int_value (ts);
            b.set_member_name ("from"); b.add_string_value (from);
            if (reply_to.length > 0) {
                b.set_member_name ("replyTo"); b.add_string_value (reply_to);
            }
            if (lease != null) {
                b.set_member_name ("lease"); b.add_value (lease.to_json ());
            }
            if (if_revision >= 0) {
                b.set_member_name ("ifRevision"); b.add_int_value (if_revision);
            }
            if (body != null) {
                b.set_member_name ("body"); b.add_value (body);
            } else {
                b.set_member_name ("body"); b.begin_object (); b.end_object ();
            }
            b.end_object ();
            var gen = new Json.Generator ();
            gen.root = b.get_root ();
            return gen.to_data (null);
        }

        public static CommandEnvelope? from_json_string (string data) {
            var parser = new Json.Parser ();
            try { parser.load_from_data (data); } catch { return null; }
            var root = parser.get_root ();
            if (root == null || root.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = root.get_object ();
            var cmd = new CommandEnvelope ();
            if (obj.has_member ("id"))       cmd.id       = obj.get_string_member ("id");
            if (obj.has_member ("type"))     cmd.cmd_type = obj.get_string_member ("type");
            if (obj.has_member ("ts"))       cmd.ts       = obj.get_int_member ("ts");
            if (obj.has_member ("from"))     cmd.from     = obj.get_string_member ("from");
            if (obj.has_member ("replyTo"))  cmd.reply_to = obj.get_string_member ("replyTo");
            if (obj.has_member ("lease"))    cmd.lease    = Lease.from_json (obj.get_member ("lease"));
            if (obj.has_member ("ifRevision")) cmd.if_revision = obj.get_int_member ("ifRevision");
            if (obj.has_member ("body"))     cmd.body     = obj.get_member ("body");
            return cmd;
        }
    }

    public class ReplyError : Object {
        public string    code    { get; set; default = ""; }
        public string    message { get; set; default = ""; }
        public Json.Node? detail { get; set; default = null; }

        public Json.Node to_json () {
            var b = new Json.Builder ();
            b.begin_object ();
            b.set_member_name ("code");    b.add_string_value (code);
            b.set_member_name ("message"); b.add_string_value (message);
            if (detail != null) {
                b.set_member_name ("detail"); b.add_value (detail);
            }
            b.end_object ();
            return b.get_root ();
        }

        public static ReplyError? from_json (Json.Node node) {
            if (node.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = node.get_object ();
            var e = new ReplyError ();
            if (obj.has_member ("code"))    e.code    = obj.get_string_member ("code");
            if (obj.has_member ("message")) e.message = obj.get_string_member ("message");
            if (obj.has_member ("detail"))  e.detail  = obj.get_member ("detail");
            return e;
        }
    }

    public class ReplyEnvelope : Object {
        public string      id       { get; set; default = ""; }
        public string      reply_type { get; set; default = ""; }
        public bool        ok       { get; set; default = false; }
        public int64       ts       { get; set; default = 0; }
        public Json.Node?  body     { get; set; default = null; }
        public ReplyError? err      { get; set; default = null; }

        public string to_json_string () {
            var b = new Json.Builder ();
            b.begin_object ();
            b.set_member_name ("id");   b.add_string_value (id);
            b.set_member_name ("type"); b.add_string_value (reply_type);
            b.set_member_name ("ok");   b.add_boolean_value (ok);
            b.set_member_name ("ts");   b.add_int_value (ts);
            if (body != null) {
                b.set_member_name ("body"); b.add_value (body);
            }
            if (err != null) {
                b.set_member_name ("err"); b.add_value (err.to_json ());
            }
            b.end_object ();
            var gen = new Json.Generator ();
            gen.root = b.get_root ();
            return gen.to_data (null);
        }

        public static ReplyEnvelope? from_json_string (string data) {
            var parser = new Json.Parser ();
            try { parser.load_from_data (data); } catch { return null; }
            var root = parser.get_root ();
            if (root == null || root.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = root.get_object ();
            var r = new ReplyEnvelope ();
            if (obj.has_member ("id"))   r.id         = obj.get_string_member ("id");
            if (obj.has_member ("type")) r.reply_type = obj.get_string_member ("type");
            if (obj.has_member ("ok"))   r.ok         = obj.get_boolean_member ("ok");
            if (obj.has_member ("ts"))   r.ts         = obj.get_int_member ("ts");
            if (obj.has_member ("body")) r.body       = obj.get_member ("body");
            if (obj.has_member ("err"))  r.err        = ReplyError.from_json (obj.get_member ("err"));
            return r;
        }
    }

    /* ── Presence ────────────────────���──────────────────────── */

    public class Presence : Object {
        public string node_id { get; set; default = ""; }
        public string kind    { get; set; default = ""; }
        public string name    { get; set; default = ""; }
        public HashTable<string, Json.Node> caps { get; owned set; }
        public HashTable<string, Json.Node> endpoints { get; owned set; }
        public string source  { get; set; default = ""; }
        public int64  ts      { get; set; default = 0; }

        construct {
            caps = new HashTable<string, Json.Node> (str_hash, str_equal);
            endpoints = new HashTable<string, Json.Node> (str_hash, str_equal);
        }

        public string to_json_string () {
            var b = new Json.Builder ();
            b.begin_object ();
            b.set_member_name ("nodeId"); b.add_string_value (node_id);
            b.set_member_name ("kind");   b.add_string_value (kind);
            b.set_member_name ("name");   b.add_string_value (name);
            if (caps.size () > 0) {
                b.set_member_name ("caps");
                b.begin_object ();
                caps.foreach ((k, v) => {
                    b.set_member_name (k);
                    b.add_value (v);
                });
                b.end_object ();
            }
            if (endpoints.size () > 0) {
                b.set_member_name ("endpoints");
                b.begin_object ();
                endpoints.foreach ((k, v) => {
                    b.set_member_name (k);
                    b.add_value (v);
                });
                b.end_object ();
            }
            if (source.length > 0) {
                b.set_member_name ("source"); b.add_string_value (source);
            }
            b.set_member_name ("ts"); b.add_int_value (ts);
            b.end_object ();
            var gen = new Json.Generator ();
            gen.root = b.get_root ();
            return gen.to_data (null);
        }

        public static Presence? from_json_string (string data) {
            var parser = new Json.Parser ();
            try { parser.load_from_data (data); } catch { return null; }
            var root = parser.get_root ();
            if (root == null || root.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = root.get_object ();
            var p = new Presence ();
            if (obj.has_member ("nodeId")) p.node_id = obj.get_string_member ("nodeId");
            if (obj.has_member ("kind"))   p.kind    = obj.get_string_member ("kind");
            if (obj.has_member ("name"))   p.name    = obj.get_string_member ("name");
            if (obj.has_member ("source")) p.source  = obj.get_string_member ("source");
            if (obj.has_member ("ts"))     p.ts      = obj.get_int_member ("ts");
            if (obj.has_member ("caps")) {
                var co = obj.get_object_member ("caps");
                co.foreach_member ((_, k, v) => { p.caps.insert (k, v); });
            }
            if (obj.has_member ("endpoints")) {
                var eo = obj.get_object_member ("endpoints");
                eo.foreach_member ((_, k, v) => { p.endpoints.insert (k, v); });
            }
            return p;
        }
    }

    /* ── Renderer state ─────────────────────────────────────── */

    public class SessionState : Object {
        public string id              { get; set; default = ""; }
        public string owner           { get; set; default = ""; }
        public int64  lease_expires_at { get; set; default = 0; }

        public Json.Node to_json () {
            var b = new Json.Builder ();
            b.begin_object ();
            b.set_member_name ("id");             b.add_string_value (id);
            b.set_member_name ("owner");          b.add_string_value (owner);
            b.set_member_name ("leaseExpiresAt"); b.add_int_value (lease_expires_at);
            b.end_object ();
            return b.get_root ();
        }

        public static SessionState? from_json (Json.Node node) {
            if (node.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = node.get_object ();
            var s = new SessionState ();
            if (obj.has_member ("id"))             s.id              = obj.get_string_member ("id");
            if (obj.has_member ("owner"))          s.owner           = obj.get_string_member ("owner");
            if (obj.has_member ("leaseExpiresAt")) s.lease_expires_at = obj.get_int_member ("leaseExpiresAt");
            return s;
        }
    }

    public class PlaybackState : Object {
        public string  status      { get; set; default = "stopped"; }
        public int64   position_ms { get; set; default = 0; }
        public int64   duration_ms { get; set; default = 0; }
        public double  volume      { get; set; default = 1.0; }
        public bool    mute        { get; set; default = false; }

        public Json.Node to_json () {
            var b = new Json.Builder ();
            b.begin_object ();
            b.set_member_name ("status");     b.add_string_value (status);
            b.set_member_name ("positionMs"); b.add_int_value (position_ms);
            b.set_member_name ("durationMs"); b.add_int_value (duration_ms);
            b.set_member_name ("volume");     b.add_double_value (volume);
            b.set_member_name ("mute");       b.add_boolean_value (mute);
            b.end_object ();
            return b.get_root ();
        }

        public static PlaybackState? from_json (Json.Node node) {
            if (node.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = node.get_object ();
            var p = new PlaybackState ();
            if (obj.has_member ("status"))     p.status      = obj.get_string_member ("status");
            if (obj.has_member ("positionMs")) p.position_ms = obj.get_int_member ("positionMs");
            if (obj.has_member ("durationMs")) p.duration_ms = obj.get_int_member ("durationMs");
            if (obj.has_member ("volume"))     p.volume      = obj.get_double_member ("volume");
            if (obj.has_member ("mute"))       p.mute        = obj.get_boolean_member ("mute");
            return p;
        }
    }

    public class QueueState : Object {
        public int64  revision    { get; set; default = 0; }
        public int64  length      { get; set; default = 0; }
        public int64  index       { get; set; default = 0; }
        public bool   repeat      { get; set; default = false; }
        public string repeat_mode { get; set; default = "off"; }
        public bool   shuffle     { get; set; default = false; }

        public Json.Node to_json () {
            var b = new Json.Builder ();
            b.begin_object ();
            b.set_member_name ("revision");   b.add_int_value (revision);
            b.set_member_name ("length");     b.add_int_value (length);
            b.set_member_name ("index");      b.add_int_value (index);
            b.set_member_name ("repeat");     b.add_boolean_value (repeat);
            b.set_member_name ("repeatMode"); b.add_string_value (repeat_mode);
            b.set_member_name ("shuffle");    b.add_boolean_value (shuffle);
            b.end_object ();
            return b.get_root ();
        }

        public static QueueState? from_json (Json.Node node) {
            if (node.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = node.get_object ();
            var q = new QueueState ();
            if (obj.has_member ("revision"))   q.revision    = obj.get_int_member ("revision");
            if (obj.has_member ("length"))     q.length      = obj.get_int_member ("length");
            if (obj.has_member ("index"))      q.index       = obj.get_int_member ("index");
            if (obj.has_member ("repeat"))     q.repeat      = obj.get_boolean_member ("repeat");
            if (obj.has_member ("repeatMode")) q.repeat_mode = obj.get_string_member ("repeatMode");
            if (obj.has_member ("shuffle"))    q.shuffle     = obj.get_boolean_member ("shuffle");
            return q;
        }
    }

    public class CurrentItemState : Object {
        public string queue_entry_id { get; set; default = ""; }
        public string item_id        { get; set; default = ""; }
        public HashTable<string, string> metadata { get; owned set; }

        construct {
            metadata = new HashTable<string, string> (str_hash, str_equal);
        }

        public Json.Node to_json () {
            var b = new Json.Builder ();
            b.begin_object ();
            b.set_member_name ("queueEntryId"); b.add_string_value (queue_entry_id);
            b.set_member_name ("itemId");       b.add_string_value (item_id);
            if (metadata.size () > 0) {
                b.set_member_name ("metadata");
                b.begin_object ();
                metadata.foreach ((k, v) => {
                    b.set_member_name (k);
                    b.add_string_value (v);
                });
                b.end_object ();
            }
            b.end_object ();
            return b.get_root ();
        }

        public static CurrentItemState? from_json (Json.Node node) {
            if (node.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = node.get_object ();
            var c = new CurrentItemState ();
            if (obj.has_member ("queueEntryId")) c.queue_entry_id = obj.get_string_member ("queueEntryId");
            if (obj.has_member ("itemId"))       c.item_id        = obj.get_string_member ("itemId");
            if (obj.has_member ("metadata")) {
                var mo = obj.get_object_member ("metadata");
                mo.foreach_member ((_, k, v) => {
                    if (v.get_node_type () == Json.NodeType.VALUE)
                        c.metadata.insert (k, v.get_string ());
                });
            }
            return c;
        }
    }

    public class RendererState : Object {
        public SessionState?     session       { get; set; default = null; }
        public PlaybackState?    playback      { get; set; default = null; }
        public QueueState?       queue         { get; set; default = null; }
        public CurrentItemState? current       { get; set; default = null; }
        public int64             state_version { get; set; default = 0; }
        public int64             ts            { get; set; default = 0; }

        public string to_json_string () {
            var b = new Json.Builder ();
            b.begin_object ();
            if (session != null) {
                b.set_member_name ("session"); b.add_value (session.to_json ());
            }
            if (playback != null) {
                b.set_member_name ("playback"); b.add_value (playback.to_json ());
            }
            if (queue != null) {
                b.set_member_name ("queue"); b.add_value (queue.to_json ());
            }
            if (current != null) {
                b.set_member_name ("current"); b.add_value (current.to_json ());
            }
            b.set_member_name ("stateVersion"); b.add_int_value (state_version);
            b.set_member_name ("ts");           b.add_int_value (ts);
            b.end_object ();
            var gen = new Json.Generator ();
            gen.root = b.get_root ();
            return gen.to_data (null);
        }

        public static RendererState? from_json_string (string data) {
            var parser = new Json.Parser ();
            try { parser.load_from_data (data); } catch { return null; }
            var root = parser.get_root ();
            if (root == null || root.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = root.get_object ();
            var s = new RendererState ();
            if (obj.has_member ("session"))      s.session       = SessionState.from_json (obj.get_member ("session"));
            if (obj.has_member ("playback"))     s.playback      = PlaybackState.from_json (obj.get_member ("playback"));
            if (obj.has_member ("queue"))        s.queue         = QueueState.from_json (obj.get_member ("queue"));
            if (obj.has_member ("current"))      s.current       = CurrentItemState.from_json (obj.get_member ("current"));
            if (obj.has_member ("stateVersion")) s.state_version = obj.get_int_member ("stateVersion");
            if (obj.has_member ("ts"))           s.ts            = obj.get_int_member ("ts");
            return s;
        }
    }

    /* ── Command body helpers ──────────────────────────���────── */

    public Json.Node build_empty_body () {
        var b = new Json.Builder ();
        b.begin_object ();
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_session_acquire_body (int64 ttl_ms) {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("ttlMs"); b.add_int_value (ttl_ms);
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_session_renew_body (int64 ttl_ms) {
        return build_session_acquire_body (ttl_ms);
    }

    public Json.Node build_playback_play_body (int64 index = -1) {
        var b = new Json.Builder ();
        b.begin_object ();
        if (index >= 0) {
            b.set_member_name ("index"); b.add_int_value (index);
        }
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_playback_seek_body (int64 position_ms) {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("positionMs"); b.add_int_value (position_ms);
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_playback_set_volume_body (double volume) {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("volume"); b.add_double_value (volume);
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_playback_set_mute_body (bool mute) {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("mute"); b.add_boolean_value (mute);
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_queue_get_body (int64 from, int64 count) {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("from");  b.add_int_value (from);
        b.set_member_name ("count"); b.add_int_value (count);
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_queue_jump_body (int64 index) {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("index"); b.add_int_value (index);
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_queue_set_shuffle_body (bool shuffle) {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("shuffle"); b.add_boolean_value (shuffle);
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_queue_set_repeat_body (bool repeat, string mode = "off") {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("repeat"); b.add_boolean_value (repeat);
        b.set_member_name ("mode");   b.add_string_value (mode);
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_library_browse_body (string container_id, int64 start, int64 count) {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("containerId"); b.add_string_value (container_id);
        b.set_member_name ("start");       b.add_int_value (start);
        b.set_member_name ("count");       b.add_int_value (count);
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_library_search_body (string query, int64 start, int64 count) {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("query"); b.add_string_value (query);
        b.set_member_name ("start"); b.add_int_value (start);
        b.set_member_name ("count"); b.add_int_value (count);
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_library_resolve_body (string item_id, bool metadata_only = false) {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("itemId"); b.add_string_value (item_id);
        if (metadata_only) {
            b.set_member_name ("metadataOnly"); b.add_boolean_value (true);
        }
        b.end_object ();
        return b.get_root ();
    }

    public Json.Node build_library_resolve_batch_body (string[] item_ids, bool metadata_only = false) {
        var b = new Json.Builder ();
        b.begin_object ();
        b.set_member_name ("itemIds");
        b.begin_array ();
        foreach (var id in item_ids) b.add_string_value (id);
        b.end_array ();
        if (metadata_only) {
            b.set_member_name ("metadataOnly"); b.add_boolean_value (true);
        }
        b.end_object ();
        return b.get_root ();
    }

    /* ── Reply body helper: parse session reply ─────────────── */

    public class SessionLease : Object {
        public string id               { get; set; default = ""; }
        public string token            { get; set; default = ""; }
        public string owner            { get; set; default = ""; }
        public int64  lease_expires_at { get; set; default = 0; }

        public static SessionLease? from_json (Json.Node node) {
            if (node.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = node.get_object ();
            var sl = new SessionLease ();
            if (obj.has_member ("id"))             sl.id              = obj.get_string_member ("id");
            if (obj.has_member ("token"))          sl.token           = obj.get_string_member ("token");
            if (obj.has_member ("owner"))          sl.owner           = obj.get_string_member ("owner");
            if (obj.has_member ("leaseExpiresAt")) sl.lease_expires_at = obj.get_int_member ("leaseExpiresAt");
            return sl;
        }
    }

    public class SessionReplyBody : Object {
        public SessionLease? session       { get; set; default = null; }
        public int64         state_version { get; set; default = 0; }

        public static SessionReplyBody? from_json (Json.Node node) {
            if (node.get_node_type () != Json.NodeType.OBJECT) return null;
            var obj = node.get_object ();
            var sr = new SessionReplyBody ();
            if (obj.has_member ("session"))      sr.session       = SessionLease.from_json (obj.get_member ("session"));
            if (obj.has_member ("stateVersion")) sr.state_version = obj.get_int_member ("stateVersion");
            return sr;
        }
    }
}
