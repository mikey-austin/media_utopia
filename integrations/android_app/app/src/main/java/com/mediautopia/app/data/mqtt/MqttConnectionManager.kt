package com.mediautopia.app.data.mqtt

import android.content.Context
import android.util.Log
import com.hivemq.client.mqtt.MqttClient
import com.hivemq.client.mqtt.MqttGlobalPublishFilter
import com.hivemq.client.mqtt.lifecycle.MqttClientConnectedContext
import com.hivemq.client.mqtt.lifecycle.MqttClientConnectedListener
import com.hivemq.client.mqtt.lifecycle.MqttClientDisconnectedContext
import com.hivemq.client.mqtt.lifecycle.MqttClientDisconnectedListener
import com.hivemq.client.mqtt.mqtt3.Mqtt3AsyncClient
import com.hivemq.client.mqtt.mqtt3.message.connect.connack.Mqtt3ConnAck
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import java.net.URI
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

enum class ConnectionState {
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
    RECONNECTING,
}

@Singleton
class MqttConnectionManager @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    private val tag = "MqttConnectionManager"

    private val _connectionState = MutableStateFlow(ConnectionState.DISCONNECTED)
    val connectionState: StateFlow<ConnectionState> = _connectionState.asStateFlow()

    private var client: Mqtt3AsyncClient? = null

    private val callbackScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    private data class Subscription(
        val topic: String,
        val qos: Int,
        val handler: (topic: String, payload: ByteArray) -> Unit,
    )

    private val subscriptions = ConcurrentHashMap<String, Subscription>()

    // -------------------------------------------------------------------------
    // Connect
    // -------------------------------------------------------------------------

    suspend fun connect(brokerUrl: String, clientId: String) {
        if (_connectionState.value == ConnectionState.CONNECTED ||
            _connectionState.value == ConnectionState.CONNECTING
        ) {
            Log.w(tag, "Already connected or connecting")
            return
        }

        // If a previous client is still around (e.g. stuck RECONNECTING in the
        // HiveMQ auto-reconnect loop), tear it down first so we don't leave a
        // zombie client thrashing in the background. Best-effort: failures are
        // swallowed because the old client is about to be replaced.
        val previous = client
        if (previous != null) {
            try {
                previous.disconnect()
            } catch (e: Exception) {
                Log.w(tag, "Previous client disconnect failed: ${e.message}")
            }
            client = null
        }

        _connectionState.value = ConnectionState.CONNECTING

        val uri = URI(brokerUrl)
        val useSsl = uri.scheme == "mqtts" || uri.scheme == "ssl"
        val host = uri.host ?: throw IllegalArgumentException("Broker URL must have a host")
        val port = if (uri.port > 0) uri.port else if (useSsl) 8883 else 1883

        val builder = MqttClient.builder()
            .useMqttVersion3()
            .identifier(clientId)
            .serverHost(host)
            .serverPort(port)
            .automaticReconnect()
                .initialDelay(2, TimeUnit.SECONDS)
                .maxDelay(30, TimeUnit.SECONDS)
                .applyAutomaticReconnect()
            .addConnectedListener(object : MqttClientConnectedListener {
                override fun onConnected(ctx: MqttClientConnectedContext) {
                    Log.i(tag, "MQTT connected")
                    val wasReconnect = _connectionState.value == ConnectionState.RECONNECTING
                    _connectionState.value = ConnectionState.CONNECTED
                    if (wasReconnect) {
                        resubscribeAll()
                    }
                }
            })
            .addDisconnectedListener(object : MqttClientDisconnectedListener {
                override fun onDisconnected(ctx: MqttClientDisconnectedContext) {
                    Log.w(tag, "MQTT disconnected: ${ctx.cause?.message}")
                    if (_connectionState.value == ConnectionState.CONNECTED) {
                        _connectionState.value = ConnectionState.RECONNECTING
                    }
                }
            })

        if (useSsl) {
            builder.sslWithDefaultConfig()
        }

        val mqtt3Client = builder.buildAsync()
        client = mqtt3Client

        // Set up a global publish listener that routes messages to matching
        // subscriptions. This is more reliable than per-subscribe callbacks
        // for HiveMQ's MQTT 3 client.
        mqtt3Client.publishes(MqttGlobalPublishFilter.ALL) { publish ->
            val msgTopic = publish.topic.toString()
            val msgPayload = publish.payloadAsBytes
            callbackScope.launch {
                routeMessage(msgTopic, msgPayload)
            }
        }

        suspendCancellableCoroutine { cont ->
            mqtt3Client.connectWith()
                .cleanSession(true)
                .keepAlive(60)
                .send()
                .whenComplete { _: Mqtt3ConnAck?, error: Throwable? ->
                    if (error != null) {
                        _connectionState.value = ConnectionState.DISCONNECTED
                        cont.resumeWithException(error)
                    } else {
                        _connectionState.value = ConnectionState.CONNECTED
                        resubscribeAll()
                        cont.resume(Unit)
                    }
                }
        }
    }

    // -------------------------------------------------------------------------
    // Disconnect
    // -------------------------------------------------------------------------

    suspend fun disconnect() {
        val c = client ?: run {
            _connectionState.value = ConnectionState.DISCONNECTED
            return
        }
        try {
            suspendCancellableCoroutine<Unit> { cont ->
                c.disconnect()
                    .whenComplete { _, error ->
                        _connectionState.value = ConnectionState.DISCONNECTED
                        client = null
                        if (error != null) {
                            // Treat disconnect errors as non-fatal — the
                            // caller is about to reconnect anyway and we've
                            // already nulled the client reference.
                            Log.w(tag, "Disconnect completed with error: ${error.message}")
                        }
                        cont.resume(Unit)
                    }
            }
        } catch (e: Exception) {
            Log.w(tag, "Disconnect threw: ${e.message}")
            _connectionState.value = ConnectionState.DISCONNECTED
            client = null
        }
    }

    /**
     * Drop every tracked subscription without touching the broker. Only safe
     * to call when the caller is about to fully tear down every component
     * that owns a subscription and will re-register them from scratch.
     */
    fun clearSubscriptions() {
        subscriptions.clear()
    }

    // -------------------------------------------------------------------------
    // Subscribe
    // -------------------------------------------------------------------------

    fun subscribe(
        topic: String,
        qos: Int = 0,
        handler: (topic: String, payload: ByteArray) -> Unit,
    ): String {
        val subscriptionId = UUID.randomUUID().toString()
        val sub = Subscription(topic, qos, handler)
        subscriptions[subscriptionId] = sub

        val c = client
        if (c != null && _connectionState.value == ConnectionState.CONNECTED) {
            doSubscribe(c, sub)
        }

        return subscriptionId
    }

    fun unsubscribe(subscriptionId: String) {
        val sub = subscriptions.remove(subscriptionId) ?: return
        val stillNeeded = subscriptions.values.any { it.topic == sub.topic }
        if (!stillNeeded) {
            client?.unsubscribeWith()?.topicFilter(sub.topic)?.send()
        }
    }

    // -------------------------------------------------------------------------
    // Publish
    // -------------------------------------------------------------------------

    fun publish(
        topic: String,
        qos: Int = 0,
        retained: Boolean = false,
        payload: ByteArray,
    ) {
        val c = client ?: run {
            Log.w(tag, "publish called while client is null, dropping on $topic")
            return
        }

        c.publishWith()
            .topic(topic)
            .qos(qosFromInt(qos))
            .retain(retained)
            .payload(payload)
            .send()
            .whenComplete { _, error ->
                if (error != null) {
                    Log.e(tag, "Publish failed on $topic: ${error.message}")
                }
            }
    }

    fun publish(
        topic: String,
        qos: Int = 0,
        retained: Boolean = false,
        payload: String,
    ) {
        publish(topic, qos, retained, payload.toByteArray(Charsets.UTF_8))
    }

    // -------------------------------------------------------------------------
    // Internals
    // -------------------------------------------------------------------------

    /**
     * Subscribe to the broker (no per-message callback — messages are routed
     * via the global publish listener set up in [connect]).
     */
    private fun doSubscribe(c: Mqtt3AsyncClient, sub: Subscription) {
        c.subscribeWith()
            .topicFilter(sub.topic)
            .qos(qosFromInt(sub.qos))
            .send()
            .whenComplete { _, error ->
                if (error != null) {
                    Log.e(tag, "Subscribe failed for ${sub.topic}: ${error.message}")
                } else {
                    Log.i(tag, "Subscribed to ${sub.topic}")
                }
            }
    }

    /**
     * Route an incoming publish message to all matching subscription handlers.
     * Supports MQTT single-level wildcard (+).
     */
    private fun routeMessage(topic: String, payload: ByteArray) {
        for (sub in subscriptions.values) {
            if (topicMatches(sub.topic, topic)) {
                try {
                    sub.handler(topic, payload)
                } catch (e: Exception) {
                    Log.e(tag, "Handler error for ${sub.topic}: ${e.message}", e)
                }
            }
        }
    }

    /**
     * Check if a topic matches a filter (supports + and # wildcards).
     */
    private fun topicMatches(filter: String, topic: String): Boolean {
        val filterParts = filter.split("/")
        val topicParts = topic.split("/")

        var fi = 0
        var ti = 0
        while (fi < filterParts.size && ti < topicParts.size) {
            val fp = filterParts[fi]
            if (fp == "#") return true
            if (fp != "+" && fp != topicParts[ti]) return false
            fi++
            ti++
        }
        return fi == filterParts.size && ti == topicParts.size
    }

    private fun resubscribeAll() {
        val c = client ?: return
        for (sub in subscriptions.values) {
            doSubscribe(c, sub)
        }
    }

    private fun qosFromInt(qos: Int): com.hivemq.client.mqtt.datatypes.MqttQos =
        when (qos) {
            0 -> com.hivemq.client.mqtt.datatypes.MqttQos.AT_MOST_ONCE
            1 -> com.hivemq.client.mqtt.datatypes.MqttQos.AT_LEAST_ONCE
            2 -> com.hivemq.client.mqtt.datatypes.MqttQos.EXACTLY_ONCE
            else -> com.hivemq.client.mqtt.datatypes.MqttQos.AT_MOST_ONCE
        }
}
