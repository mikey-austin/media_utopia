package com.mediautopia.app.domain.usecase

import android.util.Log
import com.mediautopia.app.data.mqtt.MqttConnectionManager
import com.mediautopia.app.data.mqtt.MqttTopics
import com.mediautopia.app.data.protocol.CommandEnvelope
import com.mediautopia.app.data.protocol.Lease
import com.mediautopia.app.data.protocol.ReplyEnvelope
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds

@Singleton
class CommandCorrelator @Inject constructor(
    private val mqtt: MqttConnectionManager,
) {
    private val tag = "CommandCorrelator"

    private val json = Json { ignoreUnknownKeys = true }

    private val pending = ConcurrentHashMap<String, CompletableDeferred<ReplyEnvelope>>()
    private var replyTopic: String? = null
    private var replySubscriptionId: String? = null
    private var controllerIdentity: String = ""

    // -------------------------------------------------------------------------
    // Setup / Teardown
    // -------------------------------------------------------------------------

    /**
     * Must be called once after MQTT connects. Subscribes to the reply topic
     * and routes incoming replies to pending deferreds by matching the `id`
     * field.
     *
     * @param topicBase  MQTT topic prefix (e.g. "mu/v1").
     * @param controllerId  Unique ID for this controller instance (used in
     *                      the reply topic).
     * @param identity  Human-readable identity string placed in the `from`
     *                  field of every outgoing command.
     */
    fun setup(topicBase: String, controllerId: String, identity: String) {
        controllerIdentity = identity
        replyTopic = MqttTopics.reply(topicBase, controllerId)

        replySubscriptionId = mqtt.subscribe(
            topic = replyTopic!!,
            qos = 1,
        ) { _, payload ->
            handleReply(payload)
        }

        Log.i(tag, "Subscribed to reply topic: $replyTopic")
    }

    fun cleanup() {
        replySubscriptionId?.let { mqtt.unsubscribe(it) }
        replySubscriptionId = null
        replyTopic = null

        // Complete any remaining pending deferreds with an exception so callers
        // are not stuck waiting forever.
        for ((id, deferred) in pending) {
            deferred.completeExceptionally(
                IllegalStateException("CommandCorrelator cleaned up while awaiting reply for $id")
            )
        }
        pending.clear()
    }

    // -------------------------------------------------------------------------
    // Send with reply
    // -------------------------------------------------------------------------

    /**
     * Serialize a command, publish it to the node's command topic, and wait
     * for the correlated reply.
     *
     * @throws kotlinx.coroutines.TimeoutCancellationException if no reply
     *         arrives within [timeout].
     */
    suspend fun send(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease? = null,
        ifRevision: Long? = null,
        topicBase: String = MqttTopics.BASE,
        timeout: Duration = 2.seconds,
    ): ReplyEnvelope {
        val commandId = UUID.randomUUID().toString()
        val envelope = buildEnvelope(commandId, cmdType, body, lease, ifRevision)

        val deferred = CompletableDeferred<ReplyEnvelope>()
        pending[commandId] = deferred

        val topic = MqttTopics.commands(topicBase, nodeId)
        val payloadBytes = json.encodeToString(CommandEnvelope.serializer(), envelope)
            .toByteArray(Charsets.UTF_8)

        mqtt.publish(topic = topic, qos = 1, retained = false, payload = payloadBytes)

        return try {
            withTimeout(timeout) {
                deferred.await()
            }
        } catch (e: Exception) {
            pending.remove(commandId)
            throw e
        }
    }

    // -------------------------------------------------------------------------
    // Fire-and-forget
    // -------------------------------------------------------------------------

    /**
     * Publish a command without waiting for a reply. Useful for commands where
     * the response is not needed (e.g. best-effort notifications).
     */
    fun sendFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease? = null,
        topicBase: String = MqttTopics.BASE,
    ) {
        val commandId = UUID.randomUUID().toString()
        val envelope = buildEnvelope(commandId, cmdType, body, lease, ifRevision = null)

        val topic = MqttTopics.commands(topicBase, nodeId)
        val payloadBytes = json.encodeToString(CommandEnvelope.serializer(), envelope)
            .toByteArray(Charsets.UTF_8)

        mqtt.publish(topic = topic, qos = 1, retained = false, payload = payloadBytes)
    }

    // -------------------------------------------------------------------------
    // Internals
    // -------------------------------------------------------------------------

    private fun buildEnvelope(
        commandId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
        ifRevision: Long?,
    ): CommandEnvelope {
        return CommandEnvelope(
            id = commandId,
            type = cmdType,
            ts = System.currentTimeMillis() / 1000,
            from = controllerIdentity,
            replyTo = replyTopic,
            lease = lease,
            ifRevision = ifRevision,
            body = body,
        )
    }

    private fun handleReply(payload: ByteArray) {
        val reply = try {
            json.decodeFromString(ReplyEnvelope.serializer(), payload.toString(Charsets.UTF_8))
        } catch (e: Exception) {
            Log.e(tag, "Failed to decode reply: ${e.message}")
            return
        }

        val deferred = pending.remove(reply.id)
        if (deferred == null) {
            Log.w(tag, "Received reply for unknown command ID: ${reply.id}")
            return
        }

        deferred.complete(reply)
    }
}
