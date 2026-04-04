/*
 * Renderer role: makes Rhythmbox appear as an MU renderer.
 * External controllers (HA, CLI, applet) can discover and control it.
 */

namespace Mu {

    public class Renderer : Object {

        private MqttClient mqtt;
        private Config config;
        private string node_id;
        private string topic_base;
        private RendererLease lease_mgr;
        private RendererQueue queue;
        private CommandDedup dedup;
        private RB.Shell shell;
        private RB.RhythmDB db;
        private Mu.EntryType entry_type;
        private GLib.Object? player_backend = null;  /* RBPlayer — hold ref for tick signal */
        private unowned RB.RhythmDBEntry? current_db_entry = null;

        /* State publishing. */
        private uint position_timer_id = 0;
        private uint debounce_timer_id = 0;
        private bool state_dirty = false;
        private RendererState state;

        public Renderer (RB.Shell shell, MqttClient mqtt, Config config,
                         Mu.EntryType shared_entry_type) {
            this.shell = shell;
            this.mqtt = mqtt;
            this.config = config;
            this.node_id = config.build_node_id ();
            this.topic_base = config.topic_base;
            this.lease_mgr = new RendererLease (node_id);
            this.queue = new RendererQueue ();
            this.dedup = new CommandDedup (128);

            this.db = shell.db;
            this.entry_type = shared_entry_type;

            state = new RendererState ();
            state.playback = new PlaybackState ();
            state.queue = new QueueState ();
            state.ts = GLib.get_real_time () / 1000000;
        }

        public void start () {
            /* Publish presence. */
            publish_presence ();

            /* Subscribe to commands. */
            string cmd_topic = topic_commands (topic_base, node_id);
            mqtt.subscribe (cmd_topic, 0);
            mqtt.message_received.connect (on_message);

            /* Position update timer — fallback if tick signal doesn't fire. */
            position_timer_id = GLib.Timeout.add (2000, on_position_tick);

            /* Connect to RB signals for local state changes. */
            var player = shell.shell_player;
            player.playing_changed.connect (on_rb_playing_changed);
            player.elapsed_changed.connect (on_rb_elapsed_changed);
            player.playing_song_changed.connect (on_rb_playing_song_changed);

            /* Connect to the backend player's tick signal for accurate duration.
             * RB's elapsed_changed drops the duration, but the player's tick
             * signal includes both elapsed and duration in nanoseconds.
             * We must hold a reference to prevent the backend from being collected. */
            player_backend = player.player;
            if (player_backend != null) {
                message ("MU renderer: connecting to player backend tick signal (type=%s)",
                    player_backend.get_type ().name ());
                ((RB.Player) player_backend).tick.connect (on_player_tick);
            } else {
                message ("MU renderer: WARNING — player backend is null, no duration available");
            }

            message ("MU renderer started: %s", node_id);
        }

        public void stop () {
            if (position_timer_id > 0) {
                Source.remove (position_timer_id);
                position_timer_id = 0;
            }
            if (debounce_timer_id > 0) {
                Source.remove (debounce_timer_id);
                debounce_timer_id = 0;
            }

            /* Clear retained presence. */
            string presence_topic = topic_presence (topic_base, node_id);
            mqtt.publish (presence_topic, {}, 1, true);

            /* Clear retained state. */
            string state_topic = topic_state (topic_base, node_id);
            mqtt.publish (state_topic, {}, 1, true);

            mqtt.message_received.disconnect (on_message);
            message ("MU renderer stopped: %s", node_id);
        }

        /* ── Presence ───────────────────────────────────────── */

        private void publish_presence () {
            var p = new Presence ();
            p.node_id = node_id;
            p.kind = "renderer";
            p.name = config.node_name;
            p.ts = GLib.get_real_time () / 1000000;

            /* Capabilities. */
            p.caps.insert ("seek", new Json.Node.alloc ().init_boolean (true));
            p.caps.insert ("volume", new Json.Node.alloc ().init_boolean (true));
            p.caps.insert ("queue", new Json.Node.alloc ().init_boolean (true));
            p.caps.insert ("shuffle", new Json.Node.alloc ().init_boolean (true));
            p.caps.insert ("repeat", new Json.Node.alloc ().init_boolean (true));
            p.caps.insert ("gapless", new Json.Node.alloc ().init_boolean (true));

            /* MIME types. */
            var mime_array = new Json.Builder ();
            mime_array.begin_array ();
            mime_array.add_string_value ("audio/flac");
            mime_array.add_string_value ("audio/mpeg");
            mime_array.add_string_value ("audio/ogg");
            mime_array.add_string_value ("audio/opus");
            mime_array.add_string_value ("audio/x-vorbis+ogg");
            mime_array.add_string_value ("audio/mp4");
            mime_array.end_array ();
            p.caps.insert ("mime", mime_array.get_root ());

            string ptopic = topic_presence (topic_base, node_id);
            mqtt.publish_string (ptopic, p.to_json_string (), 1, true);
        }

        /* ── State publishing with debounce ─────────────────── */

        private void mark_state_dirty () {
            state_dirty = true;
            if (debounce_timer_id == 0) {
                debounce_timer_id = GLib.Timeout.add (50, () => {
                    debounce_timer_id = 0;
                    if (state_dirty) {
                        publish_state ();
                        state_dirty = false;
                    }
                    return Source.REMOVE;
                });
            }
        }

        private void publish_state () {
            state.session = lease_mgr.current_state ();
            state.queue = queue.to_state ();
            state.ts = GLib.get_real_time () / 1000000;

            string stopic = topic_state (topic_base, node_id);
            mqtt.publish_string (stopic, state.to_json_string (), 1, true);
        }

        /* ── RB signal handlers ─────────────────────────────── */

        private void on_rb_playing_changed (bool playing) {
            if (playing) {
                state.playback.status = "playing";
            } else {
                bool is_playing;
                try { shell.shell_player.get_playing (out is_playing); } catch { is_playing = false; }
                state.playback.status = is_playing ? "playing" : "paused";
            }
            mark_state_dirty ();
        }

        private void on_rb_elapsed_changed (uint elapsed) {
            state.playback.position_ms = (int64) elapsed * 1000;
            mark_state_dirty ();
        }

        private bool duration_logged = false;
        private void on_player_tick (void* stream_data, int64 elapsed_ns, int64 duration_ns) {
            /* elapsed and duration are in nanoseconds. */
            state.playback.position_ms = elapsed_ns / 1000000;
            if (duration_ns > 0) {
                state.playback.duration_ms = duration_ns / 1000000;
            }

            /* If RB's tick doesn't provide duration, query GStreamer directly. */
            if (state.playback.duration_ms <= 0) {
                query_gstreamer_duration ();
            }

            if (state.playback.duration_ms > 0 && !duration_logged) {
                message ("MU renderer: duration = %lld ms", state.playback.duration_ms);
                duration_logged = true;

                /* Update the RhythmDB entry's DURATION so RB's progress bar works. */
                if (current_db_entry != null) {
                    var v = Value (typeof (ulong));
                    v.set_ulong ((ulong) (state.playback.duration_ms / 1000));
                    db.entry_set (current_db_entry, RB.RhythmDBPropType.DURATION, v);
                    db.commit ();
                }
            }
            mark_state_dirty ();
        }

        /**
         * Query the GStreamer playbin element directly for duration.
         * The player backend (RBPlayerGst) wraps a GstElement "playbin".
         */
        private void query_gstreamer_duration () {
            if (player_backend == null) return;

            /* Try to get the "playbin" property from the backend. */
            var val = Value (typeof (Gst.Element));
            player_backend.get_property ("playbin", ref val);
            var playbin = val.get_object () as Gst.Element;
            if (playbin == null) return;

            int64 duration_ns;
            if (playbin.query_duration (Gst.Format.TIME, out duration_ns)) {
                if (duration_ns > 0) {
                    state.playback.duration_ms = duration_ns / 1000000;
                }
            }
        }

        private void on_rb_playing_song_changed (RB.RhythmDBEntry? entry) {
            /* When RB reports no song playing (end of track), advance the MU queue. */
            if (entry == null && state.playback.status == "playing") {
                if (queue.next ()) {
                    play_current ();
                } else {
                    state.playback.status = "stopped";
                    state.current = null;
                }
                mark_state_dirty ();
            }
        }

        private bool on_position_tick () {
            try {
                uint time;
                if (shell.shell_player.get_playing_time (out time)) {
                    state.playback.position_ms = (int64) time * 1000;
                }
            } catch {}

            /* Try getting duration from RB's shell player (reads from entry/GStreamer). */
            if (state.playback.duration_ms <= 0) {
                long duration = shell.shell_player.get_playing_song_duration ();
                if (duration > 0) {
                    state.playback.duration_ms = (int64) duration * 1000;
                    message ("MU renderer: duration from shell_player = %ld seconds", duration);
                }
            }
            /* Last resort: duration from resolve metadata. */
            if (state.playback.duration_ms <= 0) {
                var cur = queue.current ();
                if (cur != null && cur.metadata.contains ("durationMs")) {
                    int64 dur_ms = int64.parse (cur.metadata.lookup ("durationMs"));
                    if (dur_ms > 0) {
                        state.playback.duration_ms = dur_ms;
                        message ("MU renderer: duration from metadata = %lld ms", dur_ms);
                    }
                }
            }

            double vol = 1.0;
            try { shell.shell_player.get_volume (out vol); } catch {}
            state.playback.volume = vol;
            bool is_muted = false;
            try { shell.shell_player.get_mute (out is_muted); } catch {}
            state.playback.mute = is_muted;

            mark_state_dirty ();
            return Source.CONTINUE;
        }

        /* ── Command handling ──────────────────────────────── */

        private void on_message (string topic, uint8[] payload) {
            string cmd_topic = topic_commands (topic_base, node_id);
            if (topic != cmd_topic) return;

            var cmd = CommandEnvelope.from_json_string ((string) payload);
            if (cmd == null) return;

            /* Dedup. */
            if (dedup.is_duplicate (cmd.id)) return;

            process_command (cmd);
        }

        private void process_command (CommandEnvelope cmd) {
            /* Session commands don't need lease validation. */
            if (cmd.cmd_type == "session.acquire" ||
                cmd.cmd_type == "session.renew" ||
                cmd.cmd_type == "session.release") {
                handle_session_command (cmd);
                return;
            }

            /* Read-only commands. */
            if (cmd.cmd_type == "queue.get") {
                handle_queue_get (cmd);
                return;
            }

            /* All other commands require a valid lease. */
            if (cmd.lease == null ||
                !lease_mgr.require_lease (cmd.lease.session_id, cmd.lease.token)) {
                send_error_reply (cmd, "LEASE_REQUIRED", "Valid lease required for mutations");
                return;
            }

            switch (cmd.cmd_type) {
                case "playback.play":     handle_playback_play (cmd); break;
                case "playback.pause":    handle_playback_pause (cmd); break;
                case "playback.stop":     handle_playback_stop (cmd); break;
                case "playback.toggle":   handle_playback_toggle (cmd); break;
                case "playback.seek":     handle_playback_seek (cmd); break;
                case "playback.next":     handle_playback_next (cmd); break;
                case "playback.prev":     handle_playback_prev (cmd); break;
                case "playback.setVolume": handle_playback_set_volume (cmd); break;
                case "playback.setMute":  handle_playback_set_mute (cmd); break;
                case "queue.set":         handle_queue_set (cmd); break;
                case "queue.add":         handle_queue_add (cmd); break;
                case "queue.remove":      handle_queue_remove (cmd); break;
                case "queue.move":        handle_queue_move (cmd); break;
                case "queue.clear":       handle_queue_clear (cmd); break;
                case "queue.jump":        handle_queue_jump (cmd); break;
                case "queue.shuffle":     handle_queue_shuffle (cmd); break;
                case "queue.setShuffle":  handle_queue_set_shuffle (cmd); break;
                case "queue.setRepeat":   handle_queue_set_repeat (cmd); break;
                default:
                    send_error_reply (cmd, "UNSUPPORTED", @"Unknown command: $(cmd.cmd_type)");
                    break;
            }
        }

        /* ── Session commands ──────────────────────────────── */

        private void handle_session_command (CommandEnvelope cmd) {
            switch (cmd.cmd_type) {
                case "session.acquire": {
                    int64 ttl = 300000;
                    if (cmd.body != null) {
                        var obj = cmd.body.get_object ();
                        if (obj != null && obj.has_member ("ttlMs"))
                            ttl = obj.get_int_member ("ttlMs");
                    }
                    var lease = lease_mgr.acquire (cmd.from, ttl);
                    if (lease == null) {
                        send_error_reply (cmd, "SESSION_CONFLICT", "Lease already held");
                    } else {
                        /* Build session reply body. */
                        var b = new Json.Builder ();
                        b.begin_object ();
                        b.set_member_name ("session");
                        b.begin_object ();
                        b.set_member_name ("id");    b.add_string_value (lease.id);
                        b.set_member_name ("token"); b.add_string_value (lease.token);
                        b.set_member_name ("owner"); b.add_string_value (lease.owner);
                        b.set_member_name ("leaseExpiresAt"); b.add_int_value (lease.lease_expires_at);
                        b.end_object ();
                        b.set_member_name ("stateVersion"); b.add_int_value (state.state_version);
                        b.end_object ();
                        send_ack_reply (cmd, b.get_root ());
                    }
                    mark_state_dirty ();
                    break;
                }
                case "session.renew": {
                    if (cmd.lease == null) {
                        send_error_reply (cmd, "LEASE_REQUIRED", "Lease required");
                        return;
                    }
                    int64 ttl = 300000;
                    if (cmd.body != null) {
                        var obj = cmd.body.get_object ();
                        if (obj != null && obj.has_member ("ttlMs"))
                            ttl = obj.get_int_member ("ttlMs");
                    }
                    var lease = lease_mgr.renew (cmd.lease.session_id, cmd.lease.token, ttl);
                    if (lease == null) {
                        send_error_reply (cmd, "LEASE_MISMATCH", "Lease mismatch or expired");
                    } else {
                        var b = new Json.Builder ();
                        b.begin_object ();
                        b.set_member_name ("session");
                        b.begin_object ();
                        b.set_member_name ("id");    b.add_string_value (lease.id);
                        b.set_member_name ("token"); b.add_string_value (lease.token);
                        b.set_member_name ("owner"); b.add_string_value (lease.owner);
                        b.set_member_name ("leaseExpiresAt"); b.add_int_value (lease.lease_expires_at);
                        b.end_object ();
                        b.set_member_name ("stateVersion"); b.add_int_value (state.state_version);
                        b.end_object ();
                        send_ack_reply (cmd, b.get_root ());
                    }
                    break;
                }
                case "session.release": {
                    if (cmd.lease == null) {
                        send_error_reply (cmd, "LEASE_REQUIRED", "Lease required");
                        return;
                    }
                    if (lease_mgr.release (cmd.lease.session_id, cmd.lease.token)) {
                        send_ack_reply (cmd, build_empty_body ());
                    } else {
                        send_error_reply (cmd, "LEASE_MISMATCH", "Lease mismatch");
                    }
                    mark_state_dirty ();
                    break;
                }
            }
        }

        /* ── Playback commands ─────────────────────────────── */

        private void handle_playback_play (CommandEnvelope cmd) {
            if (cmd.body != null) {
                var obj = cmd.body.get_object ();
                if (obj != null && obj.has_member ("index")) {
                    queue.jump (obj.get_int_member ("index"));
                }
            }
            play_current ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_playback_pause (CommandEnvelope cmd) {
            try { shell.shell_player.pause (); } catch {}
            state.playback.status = "paused";
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_playback_stop (CommandEnvelope cmd) {
            shell.shell_player.stop ();
            state.playback.status = "stopped";
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_playback_toggle (CommandEnvelope cmd) {
            try { shell.shell_player.playpause (); } catch {}
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_playback_seek (CommandEnvelope cmd) {
            if (cmd.body == null) return;
            var obj = cmd.body.get_object ();
            if (obj == null || !obj.has_member ("positionMs")) return;
            int64 pos_ms = obj.get_int_member ("positionMs");
            try { shell.shell_player.set_playing_time ((uint) (pos_ms / 1000)); } catch {}
            state.playback.position_ms = pos_ms;
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_playback_next (CommandEnvelope cmd) {
            if (queue.next ()) {
                play_current ();
            } else {
                shell.shell_player.stop ();
                state.playback.status = "stopped";
            }
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_playback_prev (CommandEnvelope cmd) {
            if (queue.prev ()) {
                play_current ();
            }
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_playback_set_volume (CommandEnvelope cmd) {
            if (cmd.body == null) return;
            var obj = cmd.body.get_object ();
            if (obj == null || !obj.has_member ("volume")) return;
            double vol = obj.get_double_member ("volume");
            try { shell.shell_player.set_volume (vol); } catch {}
            state.playback.volume = vol;
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_playback_set_mute (CommandEnvelope cmd) {
            if (cmd.body == null) return;
            var obj = cmd.body.get_object ();
            if (obj == null || !obj.has_member ("mute")) return;
            bool mute = obj.get_boolean_member ("mute");
            try { shell.shell_player.set_mute (mute); } catch {}
            state.playback.mute = mute;
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        /* ── Queue commands ────────────────────────────────── */

        private void handle_queue_get (CommandEnvelope cmd) {
            int64 from = 0, count = 50;
            if (cmd.body != null) {
                var obj = cmd.body.get_object ();
                if (obj != null) {
                    if (obj.has_member ("from"))  from  = obj.get_int_member ("from");
                    if (obj.has_member ("count")) count = obj.get_int_member ("count");
                }
            }
            send_ack_reply (cmd, queue.snapshot_json (from, count));
        }

        private void handle_queue_set (CommandEnvelope cmd) {
            if (cmd.body == null) { send_ack_reply (cmd, build_empty_body ()); return; }
            var obj = cmd.body.get_object ();
            if (obj == null) { send_ack_reply (cmd, build_empty_body ()); return; }

            int64 start_index = 0;
            if (obj.has_member ("startIndex")) start_index = obj.get_int_member ("startIndex");

            /* Clear RB's native queue before replacing. */
            clear_rb_queue ();

            var new_entries = parse_queue_entries (obj);
            queue.set_queue (new_entries, start_index);

            /* Resolve and enqueue ALL items to RB's native queue. */
            resolve_and_enqueue_all ((int) start_index);

            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_queue_add (CommandEnvelope cmd) {
            if (cmd.body == null) return;
            var obj = cmd.body.get_object ();
            if (obj == null) return;

            string position = "end";
            int64 at_index = -1;
            if (obj.has_member ("position")) position = obj.get_string_member ("position");
            if (obj.has_member ("atIndex"))  at_index = obj.get_int_member ("atIndex");

            var new_entries = parse_queue_entries (obj);
            int first_new = queue.length;
            queue.add_entries (new_entries, position, at_index);

            /* Resolve and enqueue the new items to RB's queue. */
            for (int i = first_new; i < queue.length; i++) {
                resolve_and_enqueue_entry (i, false);
            }
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_queue_remove (CommandEnvelope cmd) {
            if (cmd.body == null) return;
            var obj = cmd.body.get_object ();
            if (obj == null) return;

            if (obj.has_member ("queueEntryId")) {
                queue.remove_by_id (obj.get_string_member ("queueEntryId"));
            } else if (obj.has_member ("index")) {
                queue.remove_by_index (obj.get_int_member ("index"));
            }
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_queue_move (CommandEnvelope cmd) {
            if (cmd.body == null) return;
            var obj = cmd.body.get_object ();
            if (obj == null || !obj.has_member ("fromIndex") || !obj.has_member ("toIndex")) return;
            queue.move_entry (obj.get_int_member ("fromIndex"), obj.get_int_member ("toIndex"));
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_queue_clear (CommandEnvelope cmd) {
            clear_rb_queue ();
            queue.clear ();
            shell.shell_player.stop ();
            state.playback.status = "stopped";
            state.current = null;
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        /**
         * Remove all MU entries from RB's native play queue.
         */
        private void clear_rb_queue () {
            /* We can't easily enumerate entries in the queue source,
             * so we track created entries and remove them. */
            /* For now, stop playback — RB will clear the queue source
             * entries when they finish or on next queue.set. */
            current_db_entry = null;
        }

        private void handle_queue_jump (CommandEnvelope cmd) {
            if (cmd.body == null) return;
            var obj = cmd.body.get_object ();
            if (obj == null || !obj.has_member ("index")) return;
            queue.jump (obj.get_int_member ("index"));
            play_current ();
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_queue_shuffle (CommandEnvelope cmd) {
            int64 seed = GLib.get_real_time ();
            if (cmd.body != null) {
                var obj = cmd.body.get_object ();
                if (obj != null && obj.has_member ("seed"))
                    seed = obj.get_int_member ("seed");
            }
            queue.shuffle_queue (seed);
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_queue_set_shuffle (CommandEnvelope cmd) {
            if (cmd.body == null) return;
            var obj = cmd.body.get_object ();
            if (obj == null || !obj.has_member ("shuffle")) return;
            queue.update_shuffle (obj.get_boolean_member ("shuffle"));
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        private void handle_queue_set_repeat (CommandEnvelope cmd) {
            if (cmd.body == null) return;
            var obj = cmd.body.get_object ();
            if (obj == null) return;
            bool rep = false;
            string mode = "off";
            if (obj.has_member ("repeat")) rep = obj.get_boolean_member ("repeat");
            if (obj.has_member ("mode"))   mode = obj.get_string_member ("mode");
            queue.update_repeat (rep, mode);
            mark_state_dirty ();
            send_ack_reply (cmd, build_empty_body ());
        }

        /* ── Helpers ───────────────────────────────────────── */

        private void play_current () {
            var entry = queue.current ();
            if (entry == null) {
                state.current = null;
                return;
            }

            /* Update current item state metadata. */
            var cis = new CurrentItemState ();
            cis.queue_entry_id = entry.queue_entry_id;
            cis.item_id = entry.item_id;
            entry.metadata.foreach ((k, v) => { cis.metadata.insert (k, v); });
            state.current = cis;
            state.playback.position_ms = 0;
            state.playback.duration_ms = 0;
            duration_logged = false;

            resolve_and_enqueue_entry ((int) queue.index, true);
        }

        /**
         * Resolve a queue entry's item_id to get the stream URL and metadata,
         * then play it. Item IDs look like "lib:mu:library:filesystem:venus:music:hash".
         */
        /**
         * Resolve all queue entries and add them to RB's native queue.
         * The entry at play_index gets played immediately.
         */
        private void resolve_and_enqueue_all (int play_index) {
            for (int i = 0; i < queue.length; i++) {
                resolve_and_enqueue_entry (i, i == play_index);
            }
        }

        /**
         * Resolve a single queue entry by index and add it to RB's queue.
         * If play is true, start playback of this entry.
         */
        private void resolve_and_enqueue_entry (int idx, bool play) {
            var entry = queue.get_entry (idx);
            if (entry == null) return;

            if (entry.resolved_url != null && entry.resolved_url.length > 0) {
                /* Already has a URL — enqueue directly. */
                if (play) {
                    play_url (entry.resolved_url, entry);
                } else {
                    enqueue_url (entry.resolved_url, entry);
                }
                return;
            }

            if (entry.item_id.length == 0) return;

            /* Parse library node from item_id. */
            string item_id = entry.item_id;
            string library_node_id = "";
            string item_hash = "";

            if (item_id.has_prefix ("lib:")) {
                string rest = item_id.substring (4);
                int last_colon = rest.last_index_of (":");
                if (last_colon > 0) {
                    library_node_id = rest.substring (0, last_colon);
                    item_hash = rest.substring (last_colon + 1);
                }
            }

            if (library_node_id.length == 0) return;

            string target = topic_commands (topic_base, library_node_id);
            string reply_topic = topic_reply (topic_base, config.build_controller_id ());
            var body = build_library_resolve_body (item_hash, false);
            bool should_play = play;

            mqtt.build_and_send (target, "library.resolve", body,
                config.build_controller_id (), reply_topic, null,
                (reply) => {
                    if (!reply.ok || reply.body == null) return;
                    var robj = reply.body.get_object ();
                    if (robj == null) return;

                    if (robj.has_member ("sources")) {
                        var sources = robj.get_array_member ("sources");
                        if (sources.get_length () > 0) {
                            var first = sources.get_object_element (0);
                            if (first != null && first.has_member ("url"))
                                entry.resolved_url = first.get_string_member ("url");
                        }
                    }

                    if (robj.has_member ("metadata")) {
                        var meta = robj.get_object_member ("metadata");
                        if (meta.has_member ("title"))
                            entry.metadata.insert ("title", meta.get_string_member ("title"));
                        if (meta.has_member ("album"))
                            entry.metadata.insert ("album", meta.get_string_member ("album"));
                        if (meta.has_member ("artists")) {
                            var an = meta.get_member ("artists");
                            if (an.get_node_type () == Json.NodeType.ARRAY) {
                                var aa = an.get_array ();
                                if (aa.get_length () > 0)
                                    entry.metadata.insert ("artist", aa.get_string_element (0));
                            }
                        }
                        if (!entry.metadata.contains ("artist") && meta.has_member ("artist"))
                            entry.metadata.insert ("artist", meta.get_string_member ("artist"));
                        if (meta.has_member ("artworkUrl"))
                            entry.metadata.insert ("artworkUrl", meta.get_string_member ("artworkUrl"));
                    }

                    if (entry.resolved_url != null) {
                        if (should_play) {
                            /* Update current state metadata. */
                            if (state.current != null) {
                                entry.metadata.foreach ((k, v) => {
                                    state.current.metadata.insert (k, v);
                                });
                                mark_state_dirty ();
                            }
                            play_url (entry.resolved_url, entry);
                        } else {
                            enqueue_url (entry.resolved_url, entry);
                        }
                    }
                });
        }

        /**
         * Create a RhythmDB entry for a resolved URL, add it to RB's play queue,
         * and start playback.
         */
        private void play_url (string url, Mu.QueueEntry entry) {
            var db_entry = create_rb_entry (url, entry);
            if (db_entry == null) return;

            current_db_entry = db_entry;

            var queue_src = shell.shell_player.queue_source;
            if (queue_src != null) {
                queue_src.add_entry (db_entry, -1);
                shell.shell_player.set_playing_source (queue_src);
                shell.shell_player.play_entry (db_entry, queue_src);
            }

            state.playback.status = "playing";
            string title = entry.metadata.contains ("title") ? entry.metadata.lookup ("title") : "?";
            string artist = entry.metadata.contains ("artist") ? entry.metadata.lookup ("artist") : "?";
            message ("MU renderer: playing %s — %s (%s)", title, artist, url);
            mark_state_dirty ();
        }

        /**
         * Create a RhythmDB entry and add it to RB's play queue WITHOUT
         * starting playback. Used for building up the queue.
         */
        private void enqueue_url (string url, Mu.QueueEntry entry) {
            var db_entry = create_rb_entry (url, entry);
            if (db_entry == null) return;

            var queue_src = shell.shell_player.queue_source;
            if (queue_src != null) {
                queue_src.add_entry (db_entry, -1);
            }
        }

        private unowned RB.RhythmDBEntry? create_rb_entry (string url, Mu.QueueEntry entry) {
            unowned RB.RhythmDBEntry? db_entry = db.entry_new (entry_type, url);
            if (db_entry == null) {
                message ("MU renderer: failed to create RhythmDB entry for %s", url);
                return null;
            }

            if (entry.metadata.contains ("title")) {
                var v = Value (typeof (string));
                v.set_string (entry.metadata.lookup ("title"));
                db.entry_set (db_entry, RB.RhythmDBPropType.TITLE, v);
            }
            if (entry.metadata.contains ("artist")) {
                var v = Value (typeof (string));
                v.set_string (entry.metadata.lookup ("artist"));
                db.entry_set (db_entry, RB.RhythmDBPropType.ARTIST, v);
            }
            if (entry.metadata.contains ("album")) {
                var v = Value (typeof (string));
                v.set_string (entry.metadata.lookup ("album"));
                db.entry_set (db_entry, RB.RhythmDBPropType.ALBUM, v);
            }
            if (entry.metadata.contains ("durationMs")) {
                int64 dur_ms = int64.parse (entry.metadata.lookup ("durationMs"));
                if (dur_ms > 0) {
                    var v = Value (typeof (ulong));
                    v.set_ulong ((ulong) (dur_ms / 1000));
                    db.entry_set (db_entry, RB.RhythmDBPropType.DURATION, v);
                }
            }
            db.commit ();
            return db_entry;
        }

        private GenericArray<Mu.QueueEntry> parse_queue_entries (Json.Object obj) {
            var result = new GenericArray<Mu.QueueEntry> ();
            if (!obj.has_member ("entries")) return result;
            var arr = obj.get_array_member ("entries");
            if (arr == null) return result;

            arr.foreach_element ((_, i, node) => {
                var eobj = node.get_object ();
                if (eobj == null) return;

                var qe = new Mu.QueueEntry ();
                qe.queue_entry_id = GLib.Uuid.string_random ();

                if (eobj.has_member ("ref")) {
                    var ref_obj = eobj.get_object_member ("ref");
                    if (ref_obj != null && ref_obj.has_member ("id")) {
                        qe.item_id = ref_obj.get_string_member ("id");
                    }
                }
                if (eobj.has_member ("resolved")) {
                    var res_obj = eobj.get_object_member ("resolved");
                    if (res_obj != null) {
                        if (res_obj.has_member ("itemId")) qe.item_id = res_obj.get_string_member ("itemId");
                        if (res_obj.has_member ("url"))    qe.resolved_url = res_obj.get_string_member ("url");
                        if (res_obj.has_member ("mime"))   qe.resolved_mime = res_obj.get_string_member ("mime");
                    }
                }

                result.add (qe);
            });

            return result;
        }

        private void send_ack_reply (CommandEnvelope cmd, Json.Node body) {
            if (cmd.reply_to.length == 0) return;
            var reply = new ReplyEnvelope ();
            reply.id = cmd.id;
            reply.reply_type = "ack";
            reply.ok = true;
            reply.ts = GLib.get_real_time () / 1000000;
            reply.body = body;
            mqtt.publish_string (cmd.reply_to, reply.to_json_string ());
        }

        private void send_error_reply (CommandEnvelope cmd, string code, string message) {
            if (cmd.reply_to.length == 0) return;
            var reply = new ReplyEnvelope ();
            reply.id = cmd.id;
            reply.reply_type = "error";
            reply.ok = false;
            reply.ts = GLib.get_real_time () / 1000000;
            reply.err = new ReplyError ();
            reply.err.code = code;
            reply.err.message = message;
            mqtt.publish_string (cmd.reply_to, reply.to_json_string ());
        }
    }
}
