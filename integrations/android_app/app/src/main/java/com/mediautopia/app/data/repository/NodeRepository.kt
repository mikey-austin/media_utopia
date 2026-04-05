package com.mediautopia.app.data.repository

import android.util.Log
import com.mediautopia.app.data.mqtt.MqttConnectionManager
import com.mediautopia.app.data.mqtt.MqttTopics
import com.mediautopia.app.data.protocol.Presence
import com.mediautopia.app.domain.model.Node
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class NodeRepository @Inject constructor(
    private val mqtt: MqttConnectionManager,
) {
    private val tag = "NodeRepository"

    private val json = Json { ignoreUnknownKeys = true }

    private val nodeMap = ConcurrentHashMap<String, Node>()
    private val _nodes = MutableStateFlow<Map<String, Node>>(emptyMap())

    /** All discovered nodes keyed by nodeId. */
    val nodes: StateFlow<Map<String, Node>> = _nodes.asStateFlow()

    /** All renderers, sorted by name. */
    val renderers: Flow<List<Node>> = _nodes.map { map ->
        map.values.filter { it.kind == "renderer" }.sortedBy { it.name }
    }

    /** All libraries. */
    val libraries: Flow<List<Node>> = _nodes.map { map ->
        map.values.filter { it.kind == "library" }.sortedBy { it.name }
    }

    /** All playlist servers. */
    val playlistServers: Flow<List<Node>> = _nodes.map { map ->
        map.values.filter { it.kind == "playlist" }.sortedBy { it.name }
    }

    /** All zones and zone controllers. */
    val zones: Flow<List<Node>> = _nodes.map { map ->
        map.values.filter { it.kind == "zone" || it.kind == "zone_controller" }
            .sortedBy { it.name }
    }

    private var presenceSubscriptionId: String? = null

    // -------------------------------------------------------------------------
    // Discovery
    // -------------------------------------------------------------------------

    fun startDiscovery(topicBase: String = MqttTopics.BASE) {
        if (presenceSubscriptionId != null) return

        presenceSubscriptionId = mqtt.subscribe(
            topic = MqttTopics.presenceWildcard(topicBase),
            qos = 0,
        ) { topic, payload ->
            handlePresence(topic, payload)
        }

        Log.i(tag, "Started node discovery")
    }

    fun stopDiscovery() {
        presenceSubscriptionId?.let { mqtt.unsubscribe(it) }
        presenceSubscriptionId = null

        // Remove all non-local nodes.
        val localOnly = nodeMap.filter { (_, node) -> node.isLocal }
        nodeMap.clear()
        nodeMap.putAll(localOnly)
        emitSnapshot()

        Log.i(tag, "Stopped node discovery")
    }

    // -------------------------------------------------------------------------
    // Local node management
    // -------------------------------------------------------------------------

    /**
     * Register the local phone renderer. This is added directly to the node
     * map without going through MQTT presence.
     */
    fun registerLocalNode(node: Node) {
        val localNode = node.copy(isLocal = true, lastSeen = System.currentTimeMillis() / 1000)
        nodeMap[localNode.nodeId] = localNode
        emitSnapshot()
        Log.i(tag, "Registered local node: ${localNode.nodeId}")
    }

    fun unregisterLocalNode(nodeId: String) {
        nodeMap.remove(nodeId)
        emitSnapshot()
        Log.i(tag, "Unregistered local node: $nodeId")
    }

    // -------------------------------------------------------------------------
    // Internals
    // -------------------------------------------------------------------------

    private fun handlePresence(topic: String, payload: ByteArray) {
        Log.d(tag, "Presence message on $topic (${payload.size} bytes)")

        val nodeId = MqttTopics.extractNodeId(topic)
        if (nodeId == null) {
            Log.w(tag, "Could not extract nodeId from topic: $topic")
            return
        }

        // Empty retained message means the node has gone offline.
        if (payload.isEmpty()) {
            val removed = nodeMap.remove(nodeId)
            if (removed != null) {
                Log.i(tag, "Node gone: $nodeId")
                emitSnapshot()
            }
            return
        }

        val presence = try {
            json.decodeFromString(Presence.serializer(), payload.toString(Charsets.UTF_8))
        } catch (e: Exception) {
            Log.e(tag, "Failed to parse presence for $nodeId: ${e.message}")
            return
        }

        val node = mapPresenceToNode(presence)

        // Don't overwrite a local node with a remote presence message.
        val existing = nodeMap[nodeId]
        if (existing != null && existing.isLocal) return

        nodeMap[nodeId] = node
        emitSnapshot()
    }

    private fun mapPresenceToNode(presence: Presence): Node {
        return Node(
            nodeId = presence.nodeId,
            kind = presence.kind,
            name = presence.name,
            caps = presence.caps?.mapValues { (_, v) -> jsonElementToAny(v) } ?: emptyMap(),
            endpoints = presence.endpoints?.mapValues { (_, v) -> jsonElementToAny(v) } ?: emptyMap(),
            source = presence.source ?: "",
            lastSeen = presence.ts,
            isLocal = false,
        )
    }

    /**
     * Convert a JsonElement to a basic Any for the domain model.
     * This keeps the domain layer free of kotlinx.serialization types.
     */
    private fun jsonElementToAny(element: JsonElement): Any {
        return element.toString()
    }

    private fun emitSnapshot() {
        _nodes.value = HashMap(nodeMap)
    }
}
