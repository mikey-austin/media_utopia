/* zone_state_repo.vala — Observes MU zone state updates from MQTT. */

namespace Mu {

    public class ZoneStateRepository : Object {
        private MqttClient mqtt;
        private HashTable<string, ZoneState> states;
        private ulong handler_id = 0;

        public signal void state_changed (string node_id, ZoneState state);

        public ZoneStateRepository (MqttClient mqtt) {
            this.mqtt = mqtt;
            this.states = new HashTable<string, ZoneState> (str_hash, str_equal);
        }

        public void start () {
            var topic = MqttTopics.state_wildcard ();
            mqtt.subscribe (topic, 0);
            handler_id = mqtt.message_received.connect (on_message);
        }

        public void stop () {
            if (handler_id != 0) {
                mqtt.disconnect (handler_id);
                handler_id = 0;
            }

            var topic = MqttTopics.state_wildcard ();
            mqtt.unsubscribe (topic);
        }

        public ZoneState? get_state (string node_id) {
            return states.lookup (node_id);
        }

        private void on_message (string topic, uint8[] payload) {
            if (!topic.has_suffix ("/state")) return;

            var node_id = MqttTopics.extract_node_id (topic);
            if (node_id == null) return;

            if (payload.length == 0) {
                var cleared = new ZoneState ();
                states.set (node_id, cleared);
                state_changed (node_id, cleared);
                return;
            }

            Json.Object root;
            try {
                var parser = new Json.Parser ();
                parser.load_from_data (payload_to_string (payload));
                root = parser.get_root ().get_object ();
            } catch (GLib.Error e) {
                warning ("ZoneStateRepository: failed to parse state for %s: %s",
                         node_id, e.message);
                return;
            }

            if (!looks_like_zone_state (root)) return;

            var state = ZoneState.from_json (root);
            states.set (node_id, state);
            state_changed (node_id, state);
        }

        private bool looks_like_zone_state (Json.Object obj) {
            if (obj.has_member ("playback") || obj.has_member ("session") ||
                obj.has_member ("queue") || obj.has_member ("current")) {
                return false;
            }

            return obj.has_member ("volume") ||
                obj.has_member ("mute") ||
                obj.has_member ("sourceId") ||
                obj.has_member ("connected");
        }
    }
}
