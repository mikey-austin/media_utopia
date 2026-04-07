package com.mediautopia.app.data.repository

import android.util.Log
import com.mediautopia.app.data.mqtt.MqttConnectionManager
import com.mediautopia.app.data.mqtt.MqttTopics
import com.mediautopia.app.data.protocol.RendererState
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.json.Json
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject
import javax.inject.Singleton

@Singleton
class RendererStateRepository @Inject constructor(
    private val mqtt: MqttConnectionManager,
) {
    private val tag = "RendererStateRepository"

    private val json = Json { ignoreUnknownKeys = true }

    /**
     * Active MQTT subscriptions for remote renderers.
     * Key: nodeId, Value: MQTT subscription ID from MqttConnectionManager.
     */
    private val remoteSubscriptions = ConcurrentHashMap<String, String>()

    /**
     * SharedFlows that emit state updates for remote renderers.
     * Key: nodeId.
     */
    private val remoteFlows = ConcurrentHashMap<String, MutableSharedFlow<RendererState>>()

    /**
     * Local renderer state sources that bypass MQTT.
     * Key: nodeId, Value: StateFlow provided by the local renderer engine.
     */
    private val localSources = ConcurrentHashMap<String, StateFlow<RendererState>>()

    // -------------------------------------------------------------------------
    // Observe
    // -------------------------------------------------------------------------

    /**
     * Observe state for a specific renderer. Returns a Flow that emits on
     * every state update.
     *
     * For local renderers (registered via [registerLocalStateSource]), the
     * returned flow comes directly from the local engine's StateFlow, bypassing
     * MQTT entirely.
     *
     * For remote renderers, subscribes to the renderer's MQTT state topic and
     * emits parsed [RendererState] updates.
     */
    fun observeState(nodeId: String, topicBase: String = MqttTopics.BASE): Flow<RendererState> {
        // Prefer local source if registered.
        localSources[nodeId]?.let { return it }

        // Return existing flow if already subscribed.
        remoteFlows[nodeId]?.let { return it }

        // Create a new SharedFlow and subscribe via MQTT.
        val flow = MutableSharedFlow<RendererState>(replay = 1)
        remoteFlows[nodeId] = flow

        val subscriptionId = mqtt.subscribe(
            topic = MqttTopics.state(topicBase, nodeId),
            qos = 0,
        ) { _, payload ->
            handleStateMessage(nodeId, payload)
        }

        remoteSubscriptions[nodeId] = subscriptionId
        Log.i(tag, "Subscribed to state for renderer: $nodeId")

        return flow
    }

    /**
     * Stop observing a renderer. Unsubscribes from MQTT and removes the
     * cached flow for remote renderers. No-op for local renderers.
     */
    fun stopObserving(nodeId: String) {
        // Unsubscribe from MQTT if this was a remote subscription.
        remoteSubscriptions.remove(nodeId)?.let { subscriptionId ->
            mqtt.unsubscribe(subscriptionId)
            Log.i(tag, "Unsubscribed from state for renderer: $nodeId")
        }

        remoteFlows.remove(nodeId)
    }

    /**
     * Re-issue MQTT subscriptions for every remote renderer we're currently
     * observing. Used after a hard reconnect: the MQTT connection manager's
     * subscription table has been wiped, but the ViewModels are still
     * collecting from the cached [remoteFlows]. We keep the flows in place
     * (so downstream collectors don't miss anything) and just re-wire the
     * underlying broker subscriptions.
     */
    fun reobserveAll(topicBase: String = MqttTopics.BASE) {
        remoteSubscriptions.clear()
        for (nodeId in remoteFlows.keys) {
            val subscriptionId = mqtt.subscribe(
                topic = MqttTopics.state(topicBase, nodeId),
                qos = 0,
            ) { _, payload ->
                handleStateMessage(nodeId, payload)
            }
            remoteSubscriptions[nodeId] = subscriptionId
            Log.i(tag, "Re-subscribed to state for renderer: $nodeId")
        }
    }

    // -------------------------------------------------------------------------
    // Local renderer state
    // -------------------------------------------------------------------------

    /**
     * Register a local renderer state source. When [observeState] is called
     * for this nodeId, the provided [stateFlow] is returned directly instead
     * of creating an MQTT subscription.
     */
    fun registerLocalStateSource(nodeId: String, stateFlow: StateFlow<RendererState>) {
        localSources[nodeId] = stateFlow
        Log.i(tag, "Registered local state source: $nodeId")
    }

    fun unregisterLocalStateSource(nodeId: String) {
        localSources.remove(nodeId)
        Log.i(tag, "Unregistered local state source: $nodeId")
    }

    // -------------------------------------------------------------------------
    // Internals
    // -------------------------------------------------------------------------

    private fun handleStateMessage(nodeId: String, payload: ByteArray) {
        if (payload.isEmpty()) return

        val state = try {
            json.decodeFromString(RendererState.serializer(), payload.toString(Charsets.UTF_8))
        } catch (e: Exception) {
            Log.e(tag, "Failed to parse state for $nodeId: ${e.message}")
            return
        }

        Log.d(tag, "State for $nodeId: status=${state.playback?.status} current=${state.current != null} meta=${state.current?.metadata?.keys} session=${state.session?.owner}")

        val flow = remoteFlows[nodeId] ?: return
        val emitted = flow.tryEmit(state)
        if (!emitted) {
            Log.w(tag, "Failed to emit state for $nodeId (flow full?)")
        }
    }
}
