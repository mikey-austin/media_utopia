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
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withTimeoutOrNull
import java.net.URI
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlin.time.Duration.Companion.seconds

enum class ConnectionState {
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
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

        val previous = client
        if (previous != null) {
            withTimeoutOrNull(2.seconds) {
                suspendCancellableCoroutine<Unit> { cont ->
                    previous.disconnect().whenComplete { _, _ -> cont.resume(Unit) }
                }
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
            // NOTE: automaticReconnect intentionally removed — we own the loop.
            .addConnectedListener(object : MqttClientConnectedListener {
                override fun onConnected(ctx: MqttClientConnectedContext) {
                    // State is driven from the coroutine path in connect(); this is log-only
                    // so a transient HiveMQ callback cannot invert state mid-coroutine.
                    Log.i(tag, "MQTT client onConnected fired")
                }
            })
            .addDisconnectedListener(object : MqttClientDisconnectedListener {
                override fun onDisconnected(ctx: MqttClientDisconnectedContext) {
                    Log.w(tag, "MQTT client onDisconnected fired: ${ctx.cause?.message}")
                    // We don't flip state here — disconnect() / markDisconnected() own that.
                    // The listener is a log-only observer.
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

        try {
            withTimeout(10.seconds) {
                suspendCancellableCoroutine<Unit> { cont ->
                    mqtt3Client.connectWith()
                        .cleanSession(true)
                        .keepAlive(60)
                        .send()
                        .whenComplete { _: Mqtt3ConnAck?, error: Throwable? ->
                            if (error != null) {
                                cont.resumeWithException(error)
                            } else {
                                cont.resume(Unit)
                            }
                        }
                }
            }
            // Guard against markDisconnected() firing between send() and here. If it
            // did, `client` was nulled and we'd report CONNECTED with no active
            // client — treat as a failed connect instead.
            if (client !== mqtt3Client) {
                Log.w(tag, "connect() succeeded but client was superseded; aborting")
                throw CancellationException("connect superseded by markDisconnected")
            }
            _connectionState.value = ConnectionState.CONNECTED
            resubscribeAll()
        } catch (e: Exception) {
            Log.w(tag, "connect() failed or timed out: ${e.message}")
            if (client === mqtt3Client) {
                client = null
            }
            _connectionState.value = ConnectionState.DISCONNECTED
            throw e
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
            withTimeout(5.seconds) {
                suspendCancellableCoroutine<Unit> { cont ->
                    c.disconnect()
                        .whenComplete { _, error ->
                            if (error != null) {
                                Log.w(tag, "Disconnect completed with error: ${error.message}")
                            }
                            cont.resume(Unit)
                        }
                }
            }
        } catch (e: Exception) {
            Log.w(tag, "Disconnect threw or timed out: ${e.message}")
        } finally {
            client = null
            _connectionState.value = ConnectionState.DISCONNECTED
        }
    }

    /**
     * Force the connection state to DISCONNECTED without waiting for broker
     * acknowledgement. Safe to call from any thread. Used by the watchdog, the
     * network callback, and the UI reconnect button to trip the reconnect loop
     * in [MqttForegroundService] without holding any mutex.
     *
     * Best-effort drops the underlying client reference so no stale callback
     * can resurrect the state later. The reconnect loop will build a fresh
     * client on the next attempt.
     */
    fun markDisconnected() {
        val previous = client
        client = null
        _connectionState.value = ConnectionState.DISCONNECTED
        if (previous != null) {
            callbackScope.launch {
                withTimeoutOrNull(2.seconds) {
                    suspendCancellableCoroutine<Unit> { cont ->
                        previous.disconnect().whenComplete { _, _ -> cont.resume(Unit) }
                    }
                }
            }
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
