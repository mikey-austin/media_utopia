package com.mediautopia.app.data.transport

import android.util.Log
import com.mediautopia.app.data.protocol.Lease
import com.mediautopia.app.data.protocol.ReplyEnvelope
import com.mediautopia.app.domain.usecase.CommandCorrelator
import kotlinx.serialization.json.JsonElement
import java.util.concurrent.atomic.AtomicReference
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds

/**
 * Uniform command-send interface used by controllers. Two implementations:
 *   - [MqttTransport]: the classic broker-mediated path.
 *   - [LocalTransport]: in-process direct dispatch to the phone's own
 *     [com.mediautopia.app.renderer.LocalRendererService], bypassing MQTT
 *     entirely for self-control.
 *
 * [TransportRouter] picks between them per-send by node id. Callers (e.g.
 * [com.mediautopia.app.domain.usecase.LeaseManager]) inject the router and
 * never branch on locality themselves.
 */
interface RendererTransport {
    suspend fun send(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease? = null,
        ifRevision: Long? = null,
        timeout: Duration = 2.seconds,
    ): ReplyEnvelope

    fun sendFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease? = null,
    )
}

/** Implementation that publishes commands over MQTT and awaits a correlated reply. */
@Singleton
class MqttTransport @Inject constructor(
    private val correlator: CommandCorrelator,
) : RendererTransport {

    override suspend fun send(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
        ifRevision: Long?,
        timeout: Duration,
    ): ReplyEnvelope = correlator.send(
        nodeId = nodeId,
        cmdType = cmdType,
        body = body,
        lease = lease,
        ifRevision = ifRevision,
        timeout = timeout,
    )

    override fun sendFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
    ) {
        correlator.sendFireAndForget(
            nodeId = nodeId,
            cmdType = cmdType,
            body = body,
            lease = lease,
        )
    }
}

/**
 * In-process transport. Holds a reference to the active
 * [com.mediautopia.app.renderer.LocalRendererService] (as a [LocalDispatcher])
 * via [register] / [unregister], called from the service's own start/stop.
 *
 * The interface dependency on [LocalDispatcher] (defined in this same file)
 * keeps the transport package decoupled from renderer internals.
 *
 * When no service is registered, [send] returns null so [TransportRouter]
 * can transparently fall back to MQTT — important during the brief windows
 * around service start/stop when a TOCTOU race could otherwise leak an
 * IllegalStateException to callers.
 */
@Singleton
class LocalTransport @Inject constructor() : RendererTransport {
    private val tag = "LocalTransport"

    /** Set to a non-null value while the local renderer service is alive. */
    private val serviceRef = AtomicReference<LocalDispatcher?>(null)

    fun register(dispatcher: LocalDispatcher) {
        serviceRef.set(dispatcher)
        Log.i(tag, "LocalTransport registered for node ${dispatcher.nodeId}")
    }

    fun unregister() {
        serviceRef.set(null)
        Log.i(tag, "LocalTransport unregistered")
    }

    fun isAvailable(nodeId: String): Boolean =
        serviceRef.get()?.nodeId == nodeId

    override suspend fun send(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
        ifRevision: Long?,
        timeout: Duration,
    ): ReplyEnvelope {
        val dispatcher = serviceRef.get()
            ?: throw IllegalStateException("LocalTransport has no active service")
        return dispatcher.dispatch(nodeId, cmdType, body, lease, ifRevision)
    }

    override fun sendFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
    ) {
        val dispatcher = serviceRef.get() ?: return
        // Best-effort: launch on the dispatcher's own scope.
        dispatcher.dispatchFireAndForget(nodeId, cmdType, body, lease)
    }

    /**
     * Try to dispatch in-process. Returns the reply if a [LocalDispatcher] for
     * [nodeId] is currently registered, or `null` if not (in which case the
     * caller should fall back to MQTT). Avoids the TOCTOU race between an
     * `isAvailable` check and a subsequent `send`.
     */
    suspend fun trySend(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
        ifRevision: Long?,
    ): ReplyEnvelope? {
        val dispatcher = serviceRef.get() ?: return null
        if (dispatcher.nodeId != nodeId) return null
        return dispatcher.dispatch(nodeId, cmdType, body, lease, ifRevision)
    }

    /**
     * Try to dispatch a fire-and-forget command in-process. Returns true if a
     * dispatcher was registered (and the call was forwarded), false if the
     * caller should fall back to MQTT.
     */
    fun trySendFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
    ): Boolean {
        val dispatcher = serviceRef.get() ?: return false
        if (dispatcher.nodeId != nodeId) return false
        dispatcher.dispatchFireAndForget(nodeId, cmdType, body, lease)
        return true
    }
}

/**
 * Minimal interface the in-process dispatcher exposes. Implemented by
 * [com.mediautopia.app.renderer.LocalRendererService]. Lives here to keep
 * [LocalTransport] agnostic of renderer internals.
 */
interface LocalDispatcher {
    val nodeId: String
    suspend fun dispatch(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
        ifRevision: Long?,
    ): ReplyEnvelope

    fun dispatchFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
    )
}

/**
 * Routes each send to the appropriate transport. Inject this (not the
 * individual transports) in consumers.
 */
@Singleton
class TransportRouter @Inject constructor(
    private val mqtt: MqttTransport,
    private val local: LocalTransport,
) : RendererTransport {

    override suspend fun send(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
        ifRevision: Long?,
        timeout: Duration,
    ): ReplyEnvelope {
        local.trySend(nodeId, cmdType, body, lease, ifRevision)?.let { return it }
        return mqtt.send(nodeId, cmdType, body, lease, ifRevision, timeout)
    }

    override fun sendFireAndForget(
        nodeId: String,
        cmdType: String,
        body: JsonElement,
        lease: Lease?,
    ) {
        if (local.trySendFireAndForget(nodeId, cmdType, body, lease)) return
        mqtt.sendFireAndForget(nodeId, cmdType, body, lease)
    }
}
