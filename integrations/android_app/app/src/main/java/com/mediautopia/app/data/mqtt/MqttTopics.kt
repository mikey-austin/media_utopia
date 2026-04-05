package com.mediautopia.app.data.mqtt

/**
 * Topic builder functions for the MU MQTT protocol.
 * Ported from pkg/mu/protocol.go.
 */
object MqttTopics {
    const val BASE = "mu/v1"

    fun presence(topicBase: String = BASE, nodeId: String): String =
        "$topicBase/node/$nodeId/presence"

    fun state(topicBase: String = BASE, nodeId: String): String =
        "$topicBase/node/$nodeId/state"

    fun commands(topicBase: String = BASE, nodeId: String): String =
        "$topicBase/node/$nodeId/cmd"

    fun events(topicBase: String = BASE, nodeId: String): String =
        "$topicBase/node/$nodeId/evt"

    fun reply(topicBase: String = BASE, controllerId: String): String =
        "$topicBase/reply/$controllerId"

    // Wildcard subscriptions for discovery.
    fun presenceWildcard(topicBase: String = BASE): String =
        "$topicBase/node/+/presence"

    fun stateWildcard(topicBase: String = BASE): String =
        "$topicBase/node/+/state"

    /**
     * Extract nodeId from a topic like "mu/v1/node/some-node/presence".
     * Returns null if the topic doesn't match the expected pattern.
     */
    fun extractNodeId(topic: String): String? {
        // Expected: {base}/node/{nodeId}/{suffix}
        val parts = topic.split("/")
        // Minimum structure: at least "mu", "v1", "node", "<id>", "<suffix>"
        val nodeIndex = parts.indexOf("node")
        if (nodeIndex < 0 || nodeIndex + 1 >= parts.size) return null
        val nodeId = parts[nodeIndex + 1]
        return nodeId.ifEmpty { null }
    }
}
