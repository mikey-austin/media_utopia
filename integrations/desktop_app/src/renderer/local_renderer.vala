/* local_renderer.vala — Local GStreamer renderer engine.
 * Owns GstDriver + LocalQueue, processes incoming MQTT commands, publishes state.
 */

namespace Mu {

    public class LocalRenderer : Object {
        private MqttClient mqtt;
        private GstDriver driver;
        private LocalQueue queue;
        private CommandDedup dedup;

        /* Session lease (incoming commands must hold a valid lease) */
        private string? session_id = null;
        private string? session_token = null;
        private string? session_owner = null;
        private int64 session_expires_at = 0;
        private const int64 SESSION_TTL_MS = 300000;

        public string node_id { get; private set; }
        public string name { get; private set; }

        /* State */
        private string playback_status = "stopped";
        private int64 position_ms = 0;
        private int64 duration_ms = 0;
        private int64 state_version = 0;

        /* Debounce / polling */
        private uint state_publish_id = 0;
        private uint position_poll_id = 0;
        private ulong message_handler_id = 0;

        /* Queue persistence */
        private uint persist_source_id = 0;
        private int64 restored_position_ms = -1;

        public signal void state_updated (RendererState state);
        public signal void spectrum_data (float[] magnitudes);

        public LocalRenderer (MqttClient mqtt, string node_id, string name) {
            this.mqtt = mqtt;
            this.node_id = node_id;
            this.name = name;
            this.driver = new GstDriver ();
            this.queue = new LocalQueue ();
            this.dedup = new CommandDedup ();

            /* Wire driver signals */
            driver.track_finished.connect (on_track_finished);
            driver.spectrum_data.connect ((mags) => {
                spectrum_data (mags);
            });
        }

        /* ---- Lifecycle ---- */

        public void start () {
            /* Restore persisted queue state before anything else */
            restore_queue_snapshot ();

            /* Subscribe to command topic */
            var cmd_topic = MqttTopics.commands (node_id);
            mqtt.subscribe (cmd_topic, 1);
            message_handler_id = mqtt.message_received.connect (on_mqtt_message);

            /* Start position polling (1 second) */
            position_poll_id = Timeout.add (1000, on_position_poll);

            /* Publish presence */
            publish_presence ();
        }

        public void stop () {
            /* Flush a final queue snapshot before shutdown */
            if (persist_source_id != 0) {
                Source.remove (persist_source_id);
                persist_source_id = 0;
            }
            save_queue_snapshot ();

            /* Stop polling */
            if (position_poll_id != 0) {
                Source.remove (position_poll_id);
                position_poll_id = 0;
            }

            /* Cancel pending state publish */
            if (state_publish_id != 0) {
                Source.remove (state_publish_id);
                state_publish_id = 0;
            }

            /* Disconnect message handler */
            if (message_handler_id != 0) {
                mqtt.disconnect (message_handler_id);
                message_handler_id = 0;
            }

            /* Unsubscribe from commands */
            var cmd_topic = MqttTopics.commands (node_id);
            mqtt.unsubscribe (cmd_topic);

            /* Stop the GStreamer pipeline */
            driver.stop ();

            /* Clear presence */
            var presence_topic = MqttTopics.presence (node_id);
            mqtt.publish (presence_topic, "", 1, true);
        }

        public RendererState build_state () {
            var state = new RendererState ();

            /* Session */
            if (session_id != null) {
                var ss = new SessionState ();
                ss.id = session_id;
                ss.owner = session_owner ?? "";
                ss.lease_expires_at = session_expires_at;
                state.session = ss;
            }

            /* Playback */
            var pb = new PlaybackState ();
            pb.status = playback_status;
            pb.position_ms = position_ms;
            pb.duration_ms = duration_ms;
            pb.volume = driver.volume;
            pb.mute = driver.muted;
            state.playback = pb;

            /* Queue */
            state.queue = queue.summary ();

            /* Current item */
            var current = queue.current_entry ();
            if (current != null) {
                var ci = new CurrentItemState ();
                ci.queue_entry_id = current.queue_entry_id;
                ci.item_id = current.item_id;
                ci.metadata = current.metadata;
                state.current = ci;
            }

            state.state_version = state_version;
            state.ts = now_ms ();

            return state;
        }

        /* ---- MQTT message handling ---- */

        private void on_mqtt_message (string topic, uint8[] payload) {
            var cmd_topic = MqttTopics.commands (node_id);
            if (topic != cmd_topic) return;
            if (payload.length == 0) return;

            var json_str = payload_to_string (payload);

            Json.Object root;
            try {
                var parser = new Json.Parser ();
                parser.load_from_data (json_str);
                root = parser.get_root ().get_object ();
            } catch (GLib.Error e) {
                warning ("LocalRenderer: failed to parse command JSON: %s", e.message);
                return;
            }

            var cmd_id = root.has_member ("id") ? root.get_string_member ("id") : "";
            var cmd_type = root.has_member ("type") ? root.get_string_member ("type") : "";
            var cmd_from = root.has_member ("from") ? root.get_string_member ("from") : "";
            string? reply_to = null;
            if (root.has_member ("replyTo") && !root.get_null_member ("replyTo")) {
                reply_to = root.get_string_member ("replyTo");
            }

            Lease? cmd_lease = null;
            if (root.has_member ("lease") && !root.get_null_member ("lease")) {
                cmd_lease = Lease.from_json (root.get_object_member ("lease"));
            }

            Json.Object body = new Json.Object ();
            if (root.has_member ("body") && !root.get_null_member ("body")) {
                body = root.get_object_member ("body");
            }

            /* Dedup check */
            if (should_dedup (cmd_type) && dedup.seen (cmd_id)) {
                send_ack_reply (cmd_id, reply_to, cmd_from);
                return;
            }

            /* Dispatch */
            dispatch_command (cmd_id, cmd_type, cmd_from, reply_to, cmd_lease, body);
        }

        private void dispatch_command (string cmd_id, string cmd_type, string cmd_from,
                                        string? reply_to, Lease? cmd_lease, Json.Object body) {
            switch (cmd_type) {
                /* ---- Session commands ---- */
                case "session.acquire":
                    handle_session_acquire (cmd_id, cmd_from, reply_to, body);
                    break;

                case "session.renew":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    handle_session_renew (cmd_id, reply_to, cmd_from, body);
                    break;

                case "session.release":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    handle_session_release (cmd_id, reply_to, cmd_from);
                    break;

                /* ---- Playback commands ---- */
                case "playback.play":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    handle_playback_play (cmd_id, reply_to, cmd_from, body);
                    break;

                case "playback.pause":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    driver.pause ();
                    playback_status = "paused";
                    mutated (cmd_id, reply_to, cmd_from);
                    break;

                case "playback.stop":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    driver.stop ();
                    playback_status = "stopped";
                    position_ms = 0;
                    duration_ms = 0;
                    mutated (cmd_id, reply_to, cmd_from);
                    break;

                case "playback.seek":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    var seek_pos = body.has_member ("positionMs")
                        ? body.get_int_member ("positionMs") : 0;
                    driver.seek_to (seek_pos);
                    position_ms = seek_pos;
                    mutated (cmd_id, reply_to, cmd_from);
                    break;

                case "playback.next":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    handle_playback_next (cmd_id, reply_to, cmd_from);
                    break;

                case "playback.prev":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    handle_playback_prev (cmd_id, reply_to, cmd_from);
                    break;

                case "playback.setVolume":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    driver.volume = body.has_member ("volume")
                        ? body.get_double_member ("volume") : driver.volume;
                    mutated (cmd_id, reply_to, cmd_from);
                    break;

                case "playback.setMute":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    driver.muted = body.has_member ("mute")
                        ? body.get_boolean_member ("mute") : driver.muted;
                    mutated (cmd_id, reply_to, cmd_from);
                    break;

                /* ---- Queue commands ---- */
                case "queue.get":
                    handle_queue_get (cmd_id, reply_to, cmd_from, body);
                    break;

                case "queue.set":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    handle_queue_set (cmd_id, reply_to, cmd_from, body);
                    break;

                case "queue.add":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    handle_queue_add (cmd_id, reply_to, cmd_from, body);
                    break;

                case "queue.remove":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    handle_queue_remove (cmd_id, reply_to, cmd_from, body);
                    break;

                case "queue.move":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    var from_idx = body.has_member ("fromIndex")
                        ? body.get_int_member ("fromIndex") : -1;
                    var to_idx = body.has_member ("toIndex")
                        ? body.get_int_member ("toIndex") : -1;
                    queue.move_entry (from_idx, to_idx);
                    mutated (cmd_id, reply_to, cmd_from);
                    break;

                case "queue.clear":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    queue.clear ();
                    driver.stop ();
                    playback_status = "stopped";
                    position_ms = 0;
                    duration_ms = 0;
                    mutated (cmd_id, reply_to, cmd_from);
                    break;

                case "queue.jump":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    var jump_idx = body.has_member ("index")
                        ? body.get_int_member ("index") : -1;
                    queue.jump (jump_idx);
                    play_current ();
                    mutated (cmd_id, reply_to, cmd_from);
                    break;

                case "queue.shuffle":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    var seed = body.has_member ("seed")
                        ? body.get_int_member ("seed") : now_ms ();
                    queue.shuffle_entries (seed);
                    mutated (cmd_id, reply_to, cmd_from);
                    break;

                case "queue.setShuffle":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    var shuffle_val = body.has_member ("shuffle")
                        ? body.get_boolean_member ("shuffle") : false;
                    queue.set_shuffle_mode (shuffle_val);
                    mutated (cmd_id, reply_to, cmd_from);
                    break;

                case "queue.setRepeat":
                    if (!validate_lease (cmd_id, reply_to, cmd_from, cmd_lease)) return;
                    var mode = body.has_member ("mode")
                        ? body.get_string_member ("mode") : "off";
                    queue.apply_repeat_mode (mode);
                    mutated (cmd_id, reply_to, cmd_from);
                    break;

                default:
                    send_error_reply (cmd_id, reply_to, cmd_from,
                                      "UNKNOWN_COMMAND", "Unknown command type: %s".printf (cmd_type));
                    break;
            }
        }

        /* ---- Session handlers ---- */

        private void handle_session_acquire (string cmd_id, string cmd_from,
                                              string? reply_to, Json.Object body) {
            var ttl = body.has_member ("ttlMs") ? body.get_int_member ("ttlMs") : SESSION_TTL_MS;

            session_id = GLib.Uuid.string_random ();
            session_token = GLib.Uuid.string_random ();
            session_owner = cmd_from;
            session_expires_at = now_ms () + ttl;

            var session_obj = new Json.Object ();
            session_obj.set_string_member ("id", session_id);
            session_obj.set_string_member ("token", session_token);
            session_obj.set_string_member ("owner", session_owner);
            session_obj.set_int_member ("leaseExpiresAt", session_expires_at);

            var reply_body = new Json.Object ();
            reply_body.set_object_member ("session", session_obj);
            reply_body.set_int_member ("stateVersion", state_version);
            reply_body.set_int_member ("queueRevision", queue.revision);

            send_reply (cmd_id, "ack", true, reply_body, reply_to, cmd_from);
            bump_and_publish ();
        }

        private void handle_session_renew (string cmd_id, string? reply_to,
                                             string cmd_from, Json.Object body) {
            var ttl = body.has_member ("ttlMs") ? body.get_int_member ("ttlMs") : SESSION_TTL_MS;
            session_expires_at = now_ms () + ttl;

            var session_obj = new Json.Object ();
            session_obj.set_string_member ("id", session_id);
            session_obj.set_string_member ("token", session_token);
            session_obj.set_string_member ("owner", session_owner);
            session_obj.set_int_member ("leaseExpiresAt", session_expires_at);

            var reply_body = new Json.Object ();
            reply_body.set_object_member ("session", session_obj);
            reply_body.set_int_member ("stateVersion", state_version);
            reply_body.set_int_member ("queueRevision", queue.revision);

            send_reply (cmd_id, "ack", true, reply_body, reply_to, cmd_from);
        }

        private void handle_session_release (string cmd_id, string? reply_to,
                                               string cmd_from) {
            session_id = null;
            session_token = null;
            session_owner = null;
            session_expires_at = 0;

            send_ack_reply (cmd_id, reply_to, cmd_from);
            bump_and_publish ();
        }

        /* ---- Playback handlers ---- */

        private void handle_playback_play (string cmd_id, string? reply_to,
                                            string cmd_from, Json.Object body) {
            if (body.has_member ("index")) {
                var idx = body.get_int_member ("index");
                queue.jump (idx);
            }

            if (playback_status == "paused") {
                driver.resume ();
                playback_status = "playing";
            } else {
                play_current ();
            }

            mutated (cmd_id, reply_to, cmd_from);
        }

        private void handle_playback_next (string cmd_id, string? reply_to,
                                            string cmd_from) {
            var entry = queue.next_entry ();
            if (entry != null) {
                driver.play (entry.url);
                playback_status = "playing";
                position_ms = 0;
            } else {
                driver.stop ();
                playback_status = "stopped";
                position_ms = 0;
                duration_ms = 0;
            }
            mutated (cmd_id, reply_to, cmd_from);
        }

        private void handle_playback_prev (string cmd_id, string? reply_to,
                                            string cmd_from) {
            var entry = queue.prev_entry ();
            if (entry != null) {
                driver.play (entry.url);
                playback_status = "playing";
                position_ms = 0;
            } else {
                driver.stop ();
                playback_status = "stopped";
                position_ms = 0;
                duration_ms = 0;
            }
            mutated (cmd_id, reply_to, cmd_from);
        }

        /* ---- Queue handlers ---- */

        private void handle_queue_get (string cmd_id, string? reply_to,
                                        string cmd_from, Json.Object body) {
            var from_idx = body.has_member ("from") ? body.get_int_member ("from") : 0;
            var count = body.has_member ("count") ? body.get_int_member ("count") : 50;

            var snap = queue.snapshot (from_idx, count);

            var reply_body = new Json.Object ();
            reply_body.set_int_member ("revision", snap.revision);
            reply_body.set_int_member ("index", snap.index);
            reply_body.set_int_member ("length", (int64) queue.entries.length);

            var entries_arr = new Json.Array ();
            for (uint i = 0; i < snap.entries.length; i++) {
                var item = snap.entries[i];
                var item_obj = new Json.Object ();
                item_obj.set_string_member ("queueEntryId", item.queue_entry_id);
                item_obj.set_string_member ("itemId", item.item_id);
                if (item.metadata != null) {
                    item_obj.set_object_member ("metadata", item.metadata);
                }
                entries_arr.add_object_element (item_obj);
            }
            reply_body.set_array_member ("entries", entries_arr);

            send_reply (cmd_id, "ack", true, reply_body, reply_to, cmd_from);
        }

        private void handle_queue_set (string cmd_id, string? reply_to,
                                        string cmd_from, Json.Object body) {
            var start_index = body.has_member ("startIndex")
                ? body.get_int_member ("startIndex") : 0;

            var new_entries = parse_queue_entries (body);
            queue.set_entries (start_index, new_entries);
            play_current ();
            mutated (cmd_id, reply_to, cmd_from);
        }

        private void handle_queue_add (string cmd_id, string? reply_to,
                                        string cmd_from, Json.Object body) {
            var position = body.has_member ("position")
                ? body.get_string_member ("position") : "end";
            var at_index = body.has_member ("atIndex")
                ? body.get_int_member ("atIndex") : -1;

            var was_empty = queue.entries.length == 0;
            var new_entries = parse_queue_entries (body);
            queue.add (position, new_entries, at_index);

            /* Auto-start playback if queue was empty */
            if (was_empty && queue.entries.length > 0 && playback_status == "stopped") {
                play_current ();
            }

            mutated (cmd_id, reply_to, cmd_from);
        }

        private void handle_queue_remove (string cmd_id, string? reply_to,
                                           string cmd_from, Json.Object body) {
            string? qeid = null;
            if (body.has_member ("queueEntryId") && !body.get_null_member ("queueEntryId")) {
                qeid = body.get_string_member ("queueEntryId");
            }
            var remove_idx = body.has_member ("index") ? body.get_int_member ("index") : -1;

            queue.remove_entry (qeid, remove_idx);
            mutated (cmd_id, reply_to, cmd_from);
        }

        /* ---- Parsing helpers ---- */

        private GenericArray<LocalQueueEntry> parse_queue_entries (Json.Object body) {
            var result = new GenericArray<LocalQueueEntry> ();

            if (!body.has_member ("entries") || body.get_null_member ("entries")) {
                return result;
            }

            var arr = body.get_array_member ("entries");
            for (uint i = 0; i < arr.get_length (); i++) {
                var entry_obj = arr.get_object_element (i);

                var item_id = "";
                if (entry_obj.has_member ("ref") && !entry_obj.get_null_member ("ref")) {
                    item_id = entry_obj.get_string_member ("ref");
                }

                var url = "";
                var mime = "";
                if (entry_obj.has_member ("resolved") && !entry_obj.get_null_member ("resolved")) {
                    var resolved = entry_obj.get_object_member ("resolved");
                    url = resolved.has_member ("url") ? resolved.get_string_member ("url") : "";
                    mime = resolved.has_member ("mime") ? resolved.get_string_member ("mime") : "";
                }

                Json.Object? metadata = null;
                if (entry_obj.has_member ("metadata") && !entry_obj.get_null_member ("metadata")) {
                    metadata = entry_obj.get_object_member ("metadata");
                }

                result.add (new LocalQueueEntry (item_id, url, mime, metadata));
            }

            return result;
        }

        /* ---- Playback helpers ---- */

        private void play_current () {
            var entry = queue.current_entry ();
            if (entry == null || entry.url.length == 0) {
                driver.stop ();
                playback_status = "stopped";
                position_ms = 0;
                duration_ms = 0;
                return;
            }

            /* Use restored position on first play after snapshot restore */
            int64 start_ms = 0;
            if (restored_position_ms >= 0) {
                start_ms = restored_position_ms;
                position_ms = start_ms;
                restored_position_ms = -1;
            } else {
                position_ms = 0;
            }

            driver.play (entry.url, start_ms);
            playback_status = "playing";
        }

        private void on_track_finished () {
            var entry = queue.next_entry ();
            if (entry != null && entry.url.length > 0) {
                driver.play (entry.url);
                playback_status = "playing";
                position_ms = 0;
            } else {
                playback_status = "stopped";
                position_ms = 0;
                duration_ms = 0;
            }
            bump_and_publish ();
        }

        /* ---- Lease validation ---- */

        private bool validate_lease (string cmd_id, string? reply_to, string cmd_from,
                                      Lease? lease) {
            if (session_id == null) {
                send_error_reply (cmd_id, reply_to, cmd_from,
                                  "LEASE_REQUIRED", "No active session");
                return false;
            }

            if (lease == null) {
                send_error_reply (cmd_id, reply_to, cmd_from,
                                  "LEASE_REQUIRED", "Lease required");
                return false;
            }

            if (lease.session_id != session_id || lease.token != session_token) {
                send_error_reply (cmd_id, reply_to, cmd_from,
                                  "LEASE_MISMATCH", "Session ID or token mismatch");
                return false;
            }

            if (now_ms () > session_expires_at) {
                send_error_reply (cmd_id, reply_to, cmd_from,
                                  "LEASE_MISMATCH", "Session expired");
                return false;
            }

            return true;
        }

        /* ---- Reply helpers ---- */

        private void send_ack_reply (string cmd_id, string? reply_to, string cmd_from) {
            var reply_body = new Json.Object ();
            reply_body.set_int_member ("stateVersion", state_version);
            reply_body.set_int_member ("queueRevision", queue.revision);
            send_reply (cmd_id, "ack", true, reply_body, reply_to, cmd_from);
        }

        private void send_error_reply (string cmd_id, string? reply_to, string cmd_from,
                                        string code, string message) {
            var obj = new Json.Object ();
            obj.set_string_member ("id", cmd_id);
            obj.set_string_member ("type", "error");
            obj.set_boolean_member ("ok", false);
            obj.set_int_member ("ts", now_ms ());

            var err_obj = new Json.Object ();
            err_obj.set_string_member ("code", code);
            err_obj.set_string_member ("message", message);
            obj.set_object_member ("err", err_obj);

            var json = json_obj_to_string (obj);
            var topic = reply_to ?? MqttTopics.reply (cmd_from);
            mqtt.publish (topic, json, 1, false);
        }

        private void send_reply (string cmd_id, string reply_type, bool ok,
                                  Json.Object? body, string? reply_to, string cmd_from) {
            var obj = new Json.Object ();
            obj.set_string_member ("id", cmd_id);
            obj.set_string_member ("type", reply_type);
            obj.set_boolean_member ("ok", ok);
            obj.set_int_member ("ts", now_ms ());

            if (body != null) {
                obj.set_object_member ("body", body);
            }

            var json = json_obj_to_string (obj);
            var topic = reply_to ?? MqttTopics.reply (cmd_from);
            mqtt.publish (topic, json, 1, false);
        }

        /* ---- State publishing ---- */

        private void mutated (string cmd_id, string? reply_to, string cmd_from) {
            state_version++;
            send_ack_reply (cmd_id, reply_to, cmd_from);
            schedule_state_publish ();
            schedule_persist ();
        }

        private void bump_and_publish () {
            state_version++;
            schedule_state_publish ();
            schedule_persist ();
        }

        private void schedule_state_publish () {
            if (state_publish_id != 0) {
                Source.remove (state_publish_id);
            }

            state_publish_id = Timeout.add (50, () => {
                state_publish_id = 0;
                do_publish_state ();
                return Source.REMOVE;
            });
        }

        private void do_publish_state () {
            var state = build_state ();

            /* Publish to MQTT */
            var state_json = state_to_json (state);
            var topic = MqttTopics.state (node_id);
            mqtt.publish (topic, state_json, 1, true);

            /* Emit for local UI */
            state_updated (state);
        }

        /* ---- Position polling ---- */

        private bool on_position_poll () {
            if (playback_status == "playing") {
                int64 pos, dur;
                if (driver.query_position (out pos, out dur)) {
                    position_ms = pos;
                    duration_ms = dur;
                    schedule_state_publish ();
                }
            }
            return Source.CONTINUE;
        }

        /* ---- Presence ---- */

        private void publish_presence () {
            var obj = new Json.Object ();
            obj.set_string_member ("nodeId", node_id);
            obj.set_string_member ("kind", "renderer");
            obj.set_string_member ("name", name);

            var caps = new Json.Object ();
            caps.set_boolean_member ("seek", true);
            caps.set_boolean_member ("volume", true);
            obj.set_object_member ("caps", caps);

            obj.set_string_member ("source", "desktop_app");
            obj.set_int_member ("ts", now_ms ());

            var json = json_obj_to_string (obj);
            var topic = MqttTopics.presence (node_id);
            mqtt.publish (topic, json, 1, true);
        }

        /* ---- State serialization ---- */

        private string state_to_json (RendererState state) {
            var obj = new Json.Object ();

            /* Session */
            if (state.session != null) {
                var ss = new Json.Object ();
                ss.set_string_member ("id", state.session.id);
                ss.set_string_member ("owner", state.session.owner);
                ss.set_int_member ("leaseExpiresAt", state.session.lease_expires_at);
                obj.set_object_member ("session", ss);
            } else {
                obj.set_null_member ("session");
            }

            /* Playback */
            if (state.playback != null) {
                var pb = new Json.Object ();
                pb.set_string_member ("status", state.playback.status);
                pb.set_int_member ("positionMs", state.playback.position_ms);
                pb.set_int_member ("durationMs", state.playback.duration_ms);
                pb.set_double_member ("volume", state.playback.volume);
                pb.set_boolean_member ("mute", state.playback.mute);
                obj.set_object_member ("playback", pb);
            }

            /* Queue */
            if (state.queue != null) {
                var q = new Json.Object ();
                q.set_int_member ("revision", state.queue.revision);
                q.set_int_member ("length", state.queue.length);
                q.set_int_member ("index", state.queue.index);
                q.set_boolean_member ("repeat", state.queue.repeat);
                q.set_string_member ("repeatMode", state.queue.repeat_mode);
                q.set_boolean_member ("shuffle", state.queue.shuffle);
                obj.set_object_member ("queue", q);
            }

            /* Current item */
            if (state.current != null) {
                var ci = new Json.Object ();
                ci.set_string_member ("queueEntryId", state.current.queue_entry_id);
                ci.set_string_member ("itemId", state.current.item_id);
                if (state.current.metadata != null) {
                    ci.set_object_member ("metadata", state.current.metadata);
                }
                obj.set_object_member ("current", ci);
            } else {
                obj.set_null_member ("current");
            }

            obj.set_int_member ("stateVersion", state.state_version);
            obj.set_int_member ("ts", state.ts);

            return json_obj_to_string (obj);
        }

        /* ---- Queue persistence ---- */

        private static string get_snapshot_path () {
            return Environment.get_user_data_dir () + "/mediautopia/queue.json";
        }

        private void schedule_persist () {
            if (persist_source_id > 0) {
                Source.remove (persist_source_id);
            }
            persist_source_id = Timeout.add_seconds (2, () => {
                persist_source_id = 0;
                save_queue_snapshot ();
                return Source.REMOVE;
            });
        }

        private void save_queue_snapshot () {
            var obj = new Json.Object ();
            obj.set_int_member ("revision", queue.revision);
            obj.set_int_member ("index", queue.index);
            obj.set_boolean_member ("repeat", queue.repeat);
            obj.set_string_member ("repeatMode", queue.repeat_mode);
            obj.set_boolean_member ("shuffle", queue.shuffle);
            obj.set_double_member ("volume", driver.volume);
            obj.set_boolean_member ("mute", driver.muted);
            obj.set_int_member ("positionMs", position_ms);

            var entries_arr = new Json.Array ();
            for (uint i = 0; i < queue.entries.length; i++) {
                var entry = queue.entries[i];
                var entry_obj = new Json.Object ();
                entry_obj.set_string_member ("queueEntryId", entry.queue_entry_id);
                entry_obj.set_string_member ("itemId", entry.item_id);
                entry_obj.set_string_member ("url", entry.url);
                entry_obj.set_string_member ("mime", entry.mime);
                if (entry.metadata != null) {
                    entry_obj.set_object_member ("metadata", entry.metadata);
                }
                entries_arr.add_object_element (entry_obj);
            }
            obj.set_array_member ("entries", entries_arr);

            var json = json_obj_to_string (obj);
            var path = get_snapshot_path ();
            var dir = Path.get_dirname (path);

            DirUtils.create_with_parents (dir, 0755);
            try {
                FileUtils.set_contents (path, json);
            } catch (FileError e) {
                warning ("LocalRenderer: failed to save queue snapshot: %s", e.message);
            }
        }

        private void restore_queue_snapshot () {
            var path = get_snapshot_path ();

            string contents;
            if (!FileUtils.test (path, FileTest.EXISTS)) return;
            try {
                FileUtils.get_contents (path, out contents);
            } catch (FileError e) {
                warning ("LocalRenderer: failed to read queue snapshot: %s", e.message);
                return;
            }

            Json.Object root;
            try {
                var parser = new Json.Parser ();
                parser.load_from_data (contents);
                root = parser.get_root ().get_object ();
            } catch (GLib.Error e) {
                warning ("LocalRenderer: failed to parse queue snapshot: %s", e.message);
                return;
            }

            /* Restore queue entries */
            var restored_entries = new GenericArray<LocalQueueEntry> ();
            if (root.has_member ("entries") && !root.get_null_member ("entries")) {
                var arr = root.get_array_member ("entries");
                for (uint i = 0; i < arr.get_length (); i++) {
                    var entry_obj = arr.get_object_element (i);
                    var qeid = entry_obj.has_member ("queueEntryId")
                        ? entry_obj.get_string_member ("queueEntryId") : "";
                    var item_id = entry_obj.has_member ("itemId")
                        ? entry_obj.get_string_member ("itemId") : "";
                    var url = entry_obj.has_member ("url")
                        ? entry_obj.get_string_member ("url") : "";
                    var mime = entry_obj.has_member ("mime")
                        ? entry_obj.get_string_member ("mime") : "";
                    Json.Object? metadata = null;
                    if (entry_obj.has_member ("metadata") && !entry_obj.get_null_member ("metadata")) {
                        metadata = entry_obj.get_object_member ("metadata");
                    }
                    restored_entries.add (new LocalQueueEntry.with_id (qeid, item_id, url, mime, metadata));
                }
            }

            if (restored_entries.length == 0) return;

            /* Restore queue state */
            var start_index = root.has_member ("index") ? root.get_int_member ("index") : 0;
            queue.set_entries (start_index, restored_entries);

            /* Restore repeat/shuffle modes */
            if (root.has_member ("repeatMode")) {
                queue.apply_repeat_mode (root.get_string_member ("repeatMode"));
            }
            if (root.has_member ("shuffle")) {
                queue.set_shuffle_mode (root.get_boolean_member ("shuffle"));
            }

            /* Restore volume/mute */
            if (root.has_member ("volume")) {
                driver.volume = root.get_double_member ("volume");
            }
            if (root.has_member ("mute")) {
                driver.muted = root.get_boolean_member ("mute");
            }

            /* Store restored position — will seek on first play */
            if (root.has_member ("positionMs")) {
                restored_position_ms = root.get_int_member ("positionMs");
                position_ms = restored_position_ms;
            }

            message ("LocalRenderer: restored queue snapshot (%u entries, index %lld)",
                     queue.entries.length, queue.index);
        }

        /* ---- Utilities ---- */

        private static int64 now_ms () {
            return GLib.get_real_time () / 1000;
        }

        private static string json_obj_to_string (Json.Object obj) {
            var node = new Json.Node.alloc ().init_object (obj);
            return Json.to_string (node, false);
        }
    }
}
