package com.mediautopia.app.data.repository

import android.util.Log
import com.mediautopia.app.data.mqtt.MqttConnectionManager
import com.mediautopia.app.data.mqtt.MqttTopics
import com.mediautopia.app.data.protocol.PlaybackSetMuteBody
import com.mediautopia.app.data.protocol.PlaybackSetVolumeBody
import com.mediautopia.app.data.protocol.RendererState
import com.mediautopia.app.domain.usecase.CommandCorrelator
import com.mediautopia.app.domain.usecase.LeaseManager
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.encodeToJsonElement
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject
import javax.inject.Singleton

data class ZoneInfo(
    val nodeId: String,
    val name: String,
    val source: String = "",
    val volume: Float = 0f,
    val isMuted: Boolean = false,
    val isOnline: Boolean = true,
    val controllerId: String = "",
)

@Singleton
class ZoneRepository @Inject constructor(
    private val mqtt: MqttConnectionManager,
    private val nodeRepository: NodeRepository,
    private val correlator: CommandCorrelator,
    private val leaseManager: LeaseManager,
) {
    private val tag = "ZoneRepository"

    private val json = Json { ignoreUnknownKeys = true }

    /**
     * MQTT subscription IDs for zone state topics, keyed by nodeId.
     * Used to track which zones we are currently observing so we can
     * subscribe/unsubscribe as zones appear and disappear.
     */
    private val stateSubscriptions = ConcurrentHashMap<String, String>()

    /**
     * Latest parsed state per zone nodeId. Updated from MQTT state messages.
     */
    private val zoneStates = ConcurrentHashMap<String, ZoneStateSnapshot>()

    /**
     * Emitted whenever any zone's state changes. The map is keyed by nodeId.
     */
    private val _zoneStates = MutableStateFlow<Map<String, ZoneStateSnapshot>>(emptyMap())

    private data class ZoneStateSnapshot(
        val volume: Float = 0f,
        val isMuted: Boolean = false,
    )

    // -------------------------------------------------------------------------
    // Public API
    // -------------------------------------------------------------------------

    /**
     * A flow of all discovered zones combined with their real-time state.
     * Automatically subscribes to MQTT state topics for new zones and
     * unsubscribes when zones disappear.
     */
    val zones: Flow<List<ZoneInfo>> = combine(
        nodeRepository.zones,
        _zoneStates,
    ) { zoneNodes, states ->
        // Determine which zone nodeIds are currently known.
        val currentZoneIds = zoneNodes
            .filter { it.kind == "zone" }
            .map { it.nodeId }
            .toSet()

        // Find the zone controller (if any) for the controllerId field.
        val controller = zoneNodes.firstOrNull { it.kind == "zone_controller" }

        // Subscribe to new zones, unsubscribe from removed ones.
        reconcileSubscriptions(currentZoneIds)

        // Build ZoneInfo list for zone nodes only (not controllers).
        zoneNodes
            .filter { it.kind == "zone" }
            .map { node ->
                val state = states[node.nodeId]
                ZoneInfo(
                    nodeId = node.nodeId,
                    name = node.name,
                    source = node.source,
                    volume = state?.volume ?: 0f,
                    isMuted = state?.isMuted ?: false,
                    isOnline = true,
                    controllerId = controller?.nodeId ?: "",
                )
            }
    }

    /**
     * Set the volume on a zone node.
     */
    suspend fun setVolume(zoneNodeId: String, volume: Float) {
        try {
            val lease = leaseManager.ensureLease(zoneNodeId)
            correlator.send(
                nodeId = zoneNodeId,
                cmdType = "playback.setVolume",
                body = json.encodeToJsonElement(
                    PlaybackSetVolumeBody(volume = volume.toDouble().coerceIn(0.0, 1.0))
                ),
                lease = lease,
            )
        } catch (e: Exception) {
            Log.e(tag, "setVolume failed for $zoneNodeId: ${e.message}")
        }
    }

    /**
     * Set the mute state on a zone node.
     */
    suspend fun setMute(zoneNodeId: String, mute: Boolean) {
        try {
            val lease = leaseManager.ensureLease(zoneNodeId)
            correlator.send(
                nodeId = zoneNodeId,
                cmdType = "playback.setMute",
                body = json.encodeToJsonElement(
                    PlaybackSetMuteBody(mute = mute)
                ),
                lease = lease,
            )
        } catch (e: Exception) {
            Log.e(tag, "setMute failed for $zoneNodeId: ${e.message}")
        }
    }

    // -------------------------------------------------------------------------
    // Internals
    // -------------------------------------------------------------------------

    /**
     * Subscribe to state topics for newly discovered zones and unsubscribe
     * from zones that are no longer present.
     */
    private fun reconcileSubscriptions(currentZoneIds: Set<String>) {
        // Subscribe to new zones.
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

        // Unsubscribe from zones that have disappeared.
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

        val state = try {
            json.decodeFromString(RendererState.serializer(), payload.toString(Charsets.UTF_8))
        } catch (e: Exception) {
            Log.e(tag, "Failed to parse state for zone $zoneId: ${e.message}")
            return
        }

        val snapshot = ZoneStateSnapshot(
            volume = state.playback?.volume?.toFloat() ?: 0f,
            isMuted = state.playback?.mute ?: false,
        )

        zoneStates[zoneId] = snapshot
        emitSnapshot()
    }

    private fun emitSnapshot() {
        _zoneStates.value = HashMap(zoneStates)
    }
}
