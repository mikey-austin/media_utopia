/*
 * Controller role: control remote MU renderers from within Rhythmbox.
 * Discovers renderers via NodeRegistry, acquires leases, sends commands,
 * and tracks renderer state for the UI panel.
 */

namespace Mu {

    public class Controller : Object {

        public signal void renderer_state_changed (RendererState? state);
        public signal void renderer_list_changed ();
        public signal void lease_changed (bool has_lease);

        private MqttClient mqtt;
        private Config config;
        private NodeRegistry registry;
        private RB.Shell shell;
        private ControllerPanel? panel = null;

        /* Currently selected renderer. */
        private string? selected_node_id = null;
        private RendererState? current_state = null;
        private Lease? current_lease = null;
        private uint lease_renew_timer = 0;

        public Controller (RB.Shell shell, MqttClient mqtt,
                           NodeRegistry registry, Config config) {
            this.shell = shell;
            this.mqtt = mqtt;
            this.registry = registry;
            this.config = config;
        }

        public void start () {
            /* Listen for renderer discovery. */
            registry.node_added.connect (on_node_changed);
            registry.node_removed.connect (on_node_removed);
            registry.node_updated.connect (on_node_updated);

            /* Listen for state messages. */
            mqtt.message_received.connect (on_mqtt_message);

            /* Subscribe to all renderer state topics. */
            string pattern = @"$(config.topic_base)/node/+/state";
            mqtt.subscribe (pattern, 1);

            /* Create and add the panel widget. */
            panel = new ControllerPanel (this);
            shell.add_widget (panel, RB.ShellUILocation.SIDEBAR, false, true);
            panel.show_all ();

            message ("MU controller started");
        }

        public void stop () {
            release_lease ();

            if (panel != null) {
                shell.remove_widget (panel, RB.ShellUILocation.SIDEBAR);
                panel = null;
            }

            registry.node_added.disconnect (on_node_changed);
            registry.node_removed.disconnect (on_node_removed);
            registry.node_updated.disconnect (on_node_updated);
            mqtt.message_received.disconnect (on_mqtt_message);

            message ("MU controller stopped");
        }

        /* ── Public API for the panel ──────────────────────── */

        public GenericArray<weak Presence> get_renderers () {
            return registry.get_renderers ();
        }

        public string? get_selected_node_id () {
            return selected_node_id;
        }

        public RendererState? get_current_state () {
            return current_state;
        }

        public bool has_lease () {
            return current_lease != null;
        }

        public void select_renderer (string? node_id) {
            if (selected_node_id == node_id) return;

            /* Release existing lease on old renderer. */
            release_lease ();
            current_state = null;
            selected_node_id = node_id;

            renderer_state_changed (current_state);
        }

        public void do_acquire_lease () {
            acquire_lease ();
        }

        public void do_release_lease () {
            release_lease ();
        }

        /* ── Playback commands ─────────────────────────────── */

        public void cmd_play () {
            send_mutation ("playback.play", build_empty_body ());
        }

        public void cmd_pause () {
            send_mutation ("playback.pause", build_empty_body ());
        }

        public void cmd_stop () {
            send_mutation ("playback.stop", build_empty_body ());
        }

        public void cmd_toggle () {
            send_mutation ("playback.toggle", build_empty_body ());
        }

        public void cmd_next () {
            send_mutation ("playback.next", build_empty_body ());
        }

        public void cmd_prev () {
            send_mutation ("playback.prev", build_empty_body ());
        }

        public void cmd_seek (int64 position_ms) {
            send_mutation ("playback.seek", build_playback_seek_body (position_ms));
        }

        public void cmd_set_volume (double volume) {
            send_mutation ("playback.setVolume", build_playback_set_volume_body (volume));
        }

        public void cmd_set_mute (bool mute) {
            send_mutation ("playback.setMute", build_playback_set_mute_body (mute));
        }

        /**
         * Add a resolved item to the selected renderer's queue and start playback.
         */
        public void queue_add_and_play (string item_id, string url, string mime) {
            if (!can_command ()) {
                message ("MU controller: can't queue — no renderer or lease");
                return;
            }

            send_mutation ("queue.add", build_queue_add_body (item_id, url, mime));

            /* Jump to the end and play. */
            var st = get_current_state ();
            if (st != null && st.queue != null) {
                int64 new_idx = st.queue.length;
                send_mutation ("playback.play", build_playback_play_body (new_idx));
            }
        }

        /**
         * Add a resolved item to the queue WITHOUT starting playback.
         */
        public void queue_add (string item_id, string url, string mime) {
            if (!can_command ()) {
                message ("MU controller: can't enqueue — no renderer or lease");
                return;
            }

            send_mutation ("queue.add", build_queue_add_body (item_id, url, mime));
            message ("MU controller: enqueued %s", item_id);
        }

        private Json.Node build_queue_add_body (string item_id, string url, string mime) {
            var b = new Json.Builder ();
            b.begin_object ();
            b.set_member_name ("position"); b.add_string_value ("end");
            b.set_member_name ("entries");
            b.begin_array ();
            b.begin_object ();
            b.set_member_name ("resolved");
            b.begin_object ();
            b.set_member_name ("itemId"); b.add_string_value (item_id);
            b.set_member_name ("url");    b.add_string_value (url);
            if (mime.length > 0) {
                b.set_member_name ("mime"); b.add_string_value (mime);
            }
            b.set_member_name ("byteRange"); b.add_boolean_value (true);
            b.end_object ();
            b.end_object ();
            b.end_array ();
            b.end_object ();
            return b.get_root ();
        }

        /**
         * Whether we can send commands to the selected renderer.
         */
        public bool can_command () {
            return selected_node_id != null && current_lease != null;
        }

        /* ── Lease management ──────────────────────────────── */

        private void acquire_lease () {
            if (selected_node_id == null) return;

            string target = topic_commands (config.topic_base, selected_node_id);
            string reply_topic = topic_reply (config.topic_base, config.build_controller_id ());
            var body = build_session_acquire_body (86400000);  /* 24h TTL */

            mqtt.build_and_send (target, "session.acquire", body,
                config.build_controller_id (), reply_topic, null,
                (reply) => {
                    if (!reply.ok || reply.body == null) {
                        string err_code = "unknown";
                        string err_msg = "no details";
                        if (reply.err != null) {
                            err_code = reply.err.code;
                            err_msg = reply.err.message;
                        }
                        message ("MU controller: lease acquire failed: %s — %s", err_code, err_msg);
                        lease_changed (false);
                        return;
                    }
                    var sr = SessionReplyBody.from_json (reply.body);
                    if (sr == null || sr.session == null) return;

                    current_lease = new Lease ();
                    current_lease.session_id = sr.session.id;
                    current_lease.token = sr.session.token;

                    /* Renew every 12 hours. */
                    lease_renew_timer = GLib.Timeout.add_seconds (43200, () => {
                        renew_lease ();
                        return Source.CONTINUE;
                    });

                    lease_changed (true);
                    message ("MU controller: lease acquired on %s", selected_node_id);
                });
        }

        private void renew_lease () {
            if (selected_node_id == null || current_lease == null) return;

            string target = topic_commands (config.topic_base, selected_node_id);
            string reply_topic = topic_reply (config.topic_base, config.build_controller_id ());
            var body = build_session_renew_body (86400000);

            mqtt.build_and_send (target, "session.renew", body,
                config.build_controller_id (), reply_topic, current_lease, null);
        }

        private void release_lease () {
            if (lease_renew_timer > 0) {
                Source.remove (lease_renew_timer);
                lease_renew_timer = 0;
            }

            if (selected_node_id != null && current_lease != null) {
                string target = topic_commands (config.topic_base, selected_node_id);
                string reply_topic = topic_reply (config.topic_base, config.build_controller_id ());

                mqtt.build_and_send (target, "session.release", build_empty_body (),
                    config.build_controller_id (), reply_topic, current_lease, null);
            }

            current_lease = null;
            lease_changed (false);
        }

        /* ── Command sending ───────────────────────────────── */

        private void send_mutation (string cmd_type, Json.Node body) {
            if (selected_node_id == null || current_lease == null) return;

            string target = topic_commands (config.topic_base, selected_node_id);
            string reply_topic = topic_reply (config.topic_base, config.build_controller_id ());

            mqtt.build_and_send (target, cmd_type, body,
                config.build_controller_id (), reply_topic, current_lease, null);
        }

        /* ── MQTT message handling ─────────────────────────── */

        private void on_mqtt_message (string topic, uint8[] payload) {
            /* Only process state topics for our selected renderer. */
            if (selected_node_id == null) return;

            string expected = topic_state (config.topic_base, selected_node_id);
            if (topic != expected) return;

            if (payload.length == 0) {
                current_state = null;
                renderer_state_changed (null);
                return;
            }

            current_state = RendererState.from_json_string ((string) payload);
            renderer_state_changed (current_state);
        }

        /* ── Node registry callbacks ───────────────────────── */

        private void on_node_changed (string node_id) {
            var presence = registry.get_node (node_id);
            if (presence != null && presence.kind == "renderer") {
                renderer_list_changed ();
            }
        }

        private void on_node_updated (string node_id) {
            on_node_changed (node_id);
        }

        private void on_node_removed (string node_id) {
            renderer_list_changed ();
            if (node_id == selected_node_id) {
                current_state = null;
                current_lease = null;
                selected_node_id = null;
                renderer_state_changed (null);
                lease_changed (false);
            }
        }
    }
}
