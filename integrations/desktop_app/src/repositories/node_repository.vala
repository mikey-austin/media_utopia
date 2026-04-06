/* node_repository.vala — Discovers all MU nodes via MQTT presence messages.
 * Maintains an in-memory table of known nodes, emitting signals on changes.
 */

namespace Mu {

    public class NodeRepository : Object {
        private MqttClient mqtt;
        private HashTable<string, Presence> nodes;
        private ulong handler_id = 0;

        public signal void node_added (Presence presence);
        public signal void node_removed (string node_id);
        public signal void node_updated (Presence presence);

        public NodeRepository (MqttClient mqtt) {
            this.mqtt = mqtt;
            this.nodes = new HashTable<string, Presence> (str_hash, str_equal);
        }

        /**
         * Subscribe to the presence wildcard and wire the message handler.
         */
        public void start () {
            var topic = MqttTopics.presence_wildcard ();
            mqtt.subscribe (topic, 1);
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

            var topic = MqttTopics.presence_wildcard ();
            mqtt.unsubscribe (topic);
        }

        /**
         * Register a local node directly (e.g. for the local GStreamer renderer).
         * Bypasses MQTT — stores presence and emits the appropriate signal.
         */
        public void register_local (Presence presence) {
            var existing = nodes.lookup (presence.node_id);
            nodes.set (presence.node_id, presence);

            if (existing == null) {
                node_added (presence);
            } else {
                node_updated (presence);
            }
        }

        /**
         * Get all nodes with kind == "renderer".
         */
        public GenericArray<Presence> get_renderers () {
            return filter_by_kind ("renderer");
        }

        /**
         * Get all nodes with kind == "library".
         */
        public GenericArray<Presence> get_libraries () {
            return filter_by_kind ("library");
        }

        /**
         * Get all nodes with kind == "playlist".
         */
        public GenericArray<Presence> get_playlist_servers () {
            return filter_by_kind ("playlist");
        }

        /**
         * Get all nodes with kind == "zone".
         */
        public GenericArray<Presence> get_zones () {
            return filter_by_kind ("zone");
        }

        /**
         * Look up a single node by ID.
         */
        public Presence? get_node (string node_id) {
            return nodes.lookup (node_id);
        }

        /* ---- Internal ---- */

        private GenericArray<Presence> filter_by_kind (string kind) {
            var result = new GenericArray<Presence> ();
            nodes.foreach ((id, presence) => {
                if (presence.kind == kind) {
                    result.add (presence);
                }
            });
            return result;
        }

        private void on_message (string topic, uint8[] payload) {
            /* Only handle presence topics */
            if (!topic.has_suffix ("/presence")) return;

            var node_id = MqttTopics.extract_node_id (topic);
            if (node_id == null) return;

            /* Empty payload means the node has gone offline (LWT or explicit removal) */
            if (payload.length == 0) {
                if (nodes.remove (node_id)) {
                    node_removed (node_id);
                }
                return;
            }

            Presence? presence = null;
            try {
                presence = Presence.from_json_string (payload_to_string (payload));
            } catch (GLib.Error e) {
                warning ("NodeRepository: failed to parse presence for %s: %s",
                         node_id, e.message);
                return;
            }

            if (presence == null) return;

            /* Ensure the node_id in the presence matches the topic */
            if (presence.node_id.length == 0) {
                presence.node_id = node_id;
            }

            var existing = nodes.lookup (node_id);
            nodes.set (node_id, presence);

            if (existing == null) {
                node_added (presence);
            } else {
                node_updated (presence);
            }
        }
    }
}
