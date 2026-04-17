package com.mediautopia.app.data.repository

import android.util.Log
import com.mediautopia.app.data.mqtt.MqttConnectionManager
import com.mediautopia.app.data.mqtt.MqttTopics
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject
import javax.inject.Singleton

@Serializable
private data class ZoneMqttState(
    val volume: Double = 0.0,
    val mute: Boolean = false,
    val sourceId: String = "",
    val connected: Boolean = false,
    val ts: Long = 0,
)

data class ZoneInfo(
    val nodeId: String,
    val name: String,
    val source: String = "",
    val sourceId: String = "",
    val volume: Float = 0f,
    val isMuted: Boolean = false,
    val isOnline: Boolean = true,
    val controllerId: String = "",
)

data class ZoneSource(
    val id: String,
    val name: String,
)

@Singleton
class ZoneRepository @Inject constructor(
    private val mqtt: MqttConnectionManager,
    private val nodeRepository: NodeRepository,
    private val transport: com.mediautopia.app.data.transport.TransportRouter,
) {
    private val tag = "ZoneRepository"

    private val json = Json { ignoreUnknownKeys = true }

    private val stateSubscriptions = ConcurrentHashMap<String, String>()
    private val zoneStates = ConcurrentHashMap<String, ZoneStateSnapshot>()
    private val _zoneStates = MutableStateFlow<Map<String, ZoneStateSnapshot>>(emptyMap())

    private data class ZoneStateSnapshot(
        val volume: Float = 0f,
        val isMuted: Boolean = false,
        val sourceId: String = "",
        val connected: Boolean = false,
    )

    // -------------------------------------------------------------------------
    // Public API
    // -------------------------------------------------------------------------

    val zones: Flow<List<ZoneInfo>> = combine(
        nodeRepository.zones,
        _zoneStates,
    ) { zoneNodes, states ->
        val currentZoneIds = zoneNodes
            .filter { it.kind == "zone" }
            .map { it.nodeId }
            .toSet()

        val controller = zoneNodes.firstOrNull { it.kind == "zone_controller" }

        reconcileSubscriptions(currentZoneIds)

        zoneNodes
            .filter { it.kind == "zone" }
            .map { node ->
                val state = states[node.nodeId]
                ZoneInfo(
                    nodeId = node.nodeId,
                    name = node.name,
                    source = state?.sourceId?.substringAfterLast(":") ?: node.source,
                    sourceId = state?.sourceId ?: "",
                    volume = state?.volume ?: 0f,
                    isMuted = state?.isMuted ?: false,
                    isOnline = state?.connected ?: true,
                    controllerId = controller?.nodeId ?: "",
                )
            }
    }

    /**
     * Get available sources from the zone controller's presence.
     */
    fun getAvailableSources(): List<ZoneSource> {
        val nodes = nodeRepository.nodes.value
        val controller = nodes.values.firstOrNull { it.kind == "zone_controller" }
            ?: return emptyList()
        return controller.sources.map { ZoneSource(id = it.id, name = it.name) }
    }

    /**
     * Set volume on a zone. Commands go to the zone controller with zoneId in body.
     * No lease required for zone commands.
     */
    suspend fun setVolume(zoneNodeId: String, volume: Float) {
        val controllerId = findControllerId(zoneNodeId) ?: run {
            Log.e(tag, "No zone controller found for $zoneNodeId")
            return
        }
        try {
            val body = buildJsonObject {
                put("zoneId", JsonPrimitive(zoneNodeId))
                put("volume", JsonPrimitive(volume.toDouble().coerceIn(0.0, 1.0)))
            }
            transport.send(
                nodeId = controllerId,
                cmdType = "zone.setVolume",
                body = body,
            )
        } catch (e: Exception) {
            Log.e(tag, "setVolume failed for $zoneNodeId: ${e.message}")
        }
    }

    /**
     * Set mute on a zone. Commands go to the zone controller.
     */
    suspend fun setMute(zoneNodeId: String, mute: Boolean) {
        val controllerId = findControllerId(zoneNodeId) ?: run {
            Log.e(tag, "No zone controller found for $zoneNodeId")
            return
        }
        try {
            val body = buildJsonObject {
                put("zoneId", JsonPrimitive(zoneNodeId))
                put("mute", JsonPrimitive(mute))
            }
            transport.send(
                nodeId = controllerId,
                cmdType = "zone.setMute",
                body = body,
            )
        } catch (e: Exception) {
            Log.e(tag, "setMute failed for $zoneNodeId: ${e.message}")
        }
    }

    /**
     * Select a source for a zone. Commands go to the zone controller.
     */
    suspend fun selectSource(zoneNodeId: String, sourceId: String) {
        val controllerId = findControllerId(zoneNodeId) ?: run {
            Log.e(tag, "No zone controller found for $zoneNodeId")
            return
        }
        try {
            val body = buildJsonObject {
                put("zoneId", JsonPrimitive(zoneNodeId))
                put("sourceId", JsonPrimitive(sourceId))
            }
            transport.send(
                nodeId = controllerId,
                cmdType = "zone.selectSource",
                body = body,
            )
        } catch (e: Exception) {
            Log.e(tag, "selectSource failed for $zoneNodeId: ${e.message}")
        }
    }

    /**
     * Set volume on all zones (master volume).
     */
    suspend fun setMasterVolume(volume: Float) {
        val currentZones = _zoneStates.value.keys.toList()
        for (zoneId in currentZones) {
            setVolume(zoneId, volume)
        }
    }

    // -------------------------------------------------------------------------
    // Internals
    // -------------------------------------------------------------------------

    private fun findControllerId(zoneNodeId: String): String? {
        val nodes = nodeRepository.nodes.value
        return nodes.values.firstOrNull { it.kind == "zone_controller" }?.nodeId
    }

    private fun reconcileSubscriptions(currentZoneIds: Set<String>) {
        for (zoneId in currentZoneIds) {
            if (!stateSubscriptions.containsKey(zoneId)) {
                val subscriptionId = mqtt.subscribe(
                    topic = MqttTopics.state(nodeId = zoneId),
                    qos = 0,
                ) { _, payload ->
                    handleZoneState(zoneId, payload)
                }
                stateSubscriptions[zoneId] = subscriptionId
                Log.i(tag, "Subscribed to state for zone: $zoneId")
            }
        }

        val toRemove = stateSubscriptions.keys - currentZoneIds
        for (zoneId in toRemove) {
            stateSubscriptions.remove(zoneId)?.let { subscriptionId ->
                mqtt.unsubscribe(subscriptionId)
                Log.i(tag, "Unsubscribed from state for zone: $zoneId")
            }
            zoneStates.remove(zoneId)
            emitSnapshot()
        }
    }

    private fun handleZoneState(zoneId: String, payload: ByteArray) {
        if (payload.isEmpty()) return

        val raw = payload.toString(Charsets.UTF_8)
        Log.d(tag, "Zone state for $zoneId: $raw")

        val state = try {
            json.decodeFromString(ZoneMqttState.serializer(), raw)
        } catch (e: Exception) {
            Log.e(tag, "Failed to parse state for zone $zoneId: ${e.message}")
            return
        }

        val snapshot = ZoneStateSnapshot(
            volume = state.volume.toFloat(),
            isMuted = state.mute,
            sourceId = state.sourceId,
            connected = state.connected,
        )

        Log.d(tag, "Zone $zoneId -> volume=${snapshot.volume}, muted=${snapshot.isMuted}, source=${snapshot.sourceId}")
        zoneStates[zoneId] = snapshot
        emitSnapshot()
    }

    private fun emitSnapshot() {
        _zoneStates.value = HashMap(zoneStates)
    }
}
