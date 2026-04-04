/*
 * Presence-based node discovery and tracking.
 * Subscribes to mu/v1/node/+/presence and maintains a live map of all MU nodes.
 */

namespace Mu {

    public class NodeRegistry : Object {

        public signal void node_added (string node_id);
        public signal void node_removed (string node_id);
        public signal void node_updated (string node_id);

        private HashTable<string, Presence> nodes;
        private MqttClient mqtt;
        private string topic_base;

        public NodeRegistry (MqttClient mqtt, string topic_base) {
            this.mqtt = mqtt;
            this.topic_base = topic_base;
            nodes = new HashTable<string, Presence> (str_hash, str_equal);
        }

        public void start () {
            string pattern = @"$(topic_base)/node/+/presence";
            mqtt.subscribe (pattern, 1);
            mqtt.message_received.connect (on_message);
        }

        public void stop () {
            mqtt.message_received.disconnect (on_message);
        }

        public GenericArray<weak Presence> get_renderers () {
            var result = new GenericArray<weak Presence> ();
            nodes.foreach ((_, p) => {
                if (p.kind == "renderer") result.add (p);
            });
            return result;
        }

        public GenericArray<weak Presence> get_libraries () {
            var result = new GenericArray<weak Presence> ();
            nodes.foreach ((_, p) => {
                if (p.kind == "library") result.add (p);
            });
            return result;
        }

        public GenericArray<weak Presence> get_playlists () {
            var result = new GenericArray<weak Presence> ();
            nodes.foreach ((_, p) => {
                if (p.kind == "playlist") result.add (p);
            });
            return result;
        }

        public Presence? get_node (string node_id) {
            return nodes.lookup (node_id);
        }

        private void on_message (string topic, uint8[] payload) {
            /* Only handle presence topics. */
            string suffix = "/presence";
            if (!topic.has_suffix (suffix)) return;
            string prefix = @"$(topic_base)/node/";
            if (!topic.has_prefix (prefix)) return;

            /* Empty payload = node removed (LWT). */
            if (payload.length == 0) {
                /* Extract node ID from topic. */
                string node_part = topic.substring (prefix.length,
                    topic.length - prefix.length - suffix.length);
                if (nodes.contains (node_part)) {
                    nodes.remove (node_part);
                    node_removed (node_part);
                }
                return;
            }

            var presence = Presence.from_json_string ((string) payload);
            if (presence == null || presence.node_id.length == 0) return;

            bool existed = nodes.contains (presence.node_id);
            nodes.insert (presence.node_id, presence);

            if (existed) {
                node_updated (presence.node_id);
            } else {
                node_added (presence.node_id);
            }
        }

    }
}
