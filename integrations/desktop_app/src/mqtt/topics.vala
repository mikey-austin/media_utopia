/* topics.vala — MQTT topic builder functions for the MU protocol.
 * Ported from MqttTopics.kt / pkg/mu/protocol.go.
 */

namespace Mu.MqttTopics {

    public const string BASE = "mu/v1";

    public string presence (string node_id, string topic_base = BASE) {
        return "%s/node/%s/presence".printf (topic_base, node_id);
    }

    public string state (string node_id, string topic_base = BASE) {
        return "%s/node/%s/state".printf (topic_base, node_id);
    }

    public string commands (string node_id, string topic_base = BASE) {
        return "%s/node/%s/cmd".printf (topic_base, node_id);
    }

    public string events (string node_id, string topic_base = BASE) {
        return "%s/node/%s/evt".printf (topic_base, node_id);
    }

    public string reply (string controller_id, string topic_base = BASE) {
        return "%s/reply/%s".printf (topic_base, controller_id);
    }

    public string presence_wildcard (string topic_base = BASE) {
        return "%s/node/+/presence".printf (topic_base);
    }

    public string state_wildcard (string topic_base = BASE) {
        return "%s/node/+/state".printf (topic_base);
    }

    /**
     * Extract node ID from a topic like "mu/v1/node/some-node/presence".
     * Returns null if the topic doesn't match the expected pattern.
     */
    public string? extract_node_id (string topic) {
        var parts = topic.split ("/");
        int node_index = -1;
        for (int i = 0; i < parts.length; i++) {
            if (parts[i] == "node") {
                node_index = i;
                break;
            }
        }
        if (node_index < 0 || node_index + 1 >= parts.length) {
            return null;
        }
        var node_id = parts[node_index + 1];
        if (node_id.length == 0) {
            return null;
        }
        return node_id;
    }
}
