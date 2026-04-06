/* renderer_state_repo.vala — Observes renderer state updates from MQTT.
 * Maintains a table of the latest RendererState per renderer node.
 */

namespace Mu {

    public class RendererStateRepository : Object {
        private MqttClient mqtt;
        private HashTable<string, RendererState> states;
        private ulong handler_id = 0;

        /* Local renderer direct state source — bypasses MQTT */
        private string? local_renderer_id = null;

        public signal void state_changed (string node_id, RendererState state);

        public RendererStateRepository (MqttClient mqtt) {
            this.mqtt = mqtt;
            this.states = new HashTable<string, RendererState> (str_hash, str_equal);
        }

        /**
         * Subscribe to the state wildcard and wire the message handler.
         */
        public void start () {
            var topic = MqttTopics.state_wildcard ();
            mqtt.subscribe (topic, 0);
            handler_id = mqtt.message_received.connect (on_message);
        }

        /**
         * Unsubscribe and disconnect the message handler.
         */
        public void stop () {
            if (handler_id != 0) {
                mqtt.disconnect (handler_id);
                handler_id = 0;
            }

            var topic = MqttTopics.state_wildcard ();
            mqtt.unsubscribe (topic);
        }

        /**
         * Register a local renderer ID so its state updates bypass MQTT.
         */
        public void register_local_source (string renderer_id) {
            local_renderer_id = renderer_id;
        }

        /**
         * Directly update state for the local renderer (no MQTT round-trip).
         */
        public void update_local_state (RendererState state) {
            if (local_renderer_id == null) {
                warning ("RendererStateRepository: no local renderer registered");
                return;
            }

            states.set (local_renderer_id, state);
            state_changed (local_renderer_id, state);
        }

        /**
         * Get the latest state for a specific renderer.
         */
        public RendererState? get_state (string node_id) {
            return states.lookup (node_id);
        }

        /* ---- Internal ---- */

        private void on_message (string topic, uint8[] payload) {
            /* Only handle state topics */
            if (!topic.has_suffix ("/state")) return;

            var node_id = MqttTopics.extract_node_id (topic);
            if (node_id == null) return;

            /* Skip MQTT updates for the local renderer — use direct path instead */
            if (local_renderer_id != null && node_id == local_renderer_id) return;

            if (payload.length == 0) return;

            RendererState? state = null;
            try {
                state = RendererState.from_json_string (payload_to_string (payload));
            } catch (GLib.Error e) {
                warning ("RendererStateRepository: failed to parse state for %s: %s",
                         node_id, e.message);
                return;
            }

            if (state == null) return;

            states.set (node_id, state);
            state_changed (node_id, state);
        }
    }
}
