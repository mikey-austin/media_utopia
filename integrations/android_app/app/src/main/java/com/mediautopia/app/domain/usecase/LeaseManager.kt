package com.mediautopia.app.domain.usecase

import android.util.Log
import com.mediautopia.app.data.protocol.Lease
import com.mediautopia.app.data.protocol.ReplyEnvelope
import com.mediautopia.app.data.protocol.SessionAcquireBody
import com.mediautopia.app.data.protocol.SessionRenewBody
import com.mediautopia.app.data.protocol.SessionReplyBody
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject
import javax.inject.Singleton

private data class CachedLease(
    val sessionId: String,
    val token: String,
    val expiresAt: Long,  // unix millis
)

/**
 * Information about a currently cached lease, suitable for display in the UI.
 */
data class LeaseInfo(
    val sessionId: String,
    val expiresAtMs: Long,
)

@Singleton
class LeaseManager @Inject constructor(
    private val transport: com.mediautopia.app.data.transport.TransportRouter,
) {
    private val tag = "LeaseManager"

    private val json = Json { ignoreUnknownKeys = true }

    private val leases = ConcurrentHashMap<String, CachedLease>()

    private var renewalJob: Job? = null

    /** Observable snapshot of all currently cached leases, keyed by node id. */
    private val _leaseInfos = MutableStateFlow<Map<String, LeaseInfo>>(emptyMap())
    val leaseInfos: StateFlow<Map<String, LeaseInfo>> = _leaseInfos.asStateFlow()

    companion object {
        private const val TTL_MS = 300_000L           // 5 minutes
        private const val RENEWAL_CHECK_INTERVAL = 30_000L  // 30 seconds
        private const val RENEWAL_THRESHOLD = 120_000L      // renew if expiring within 2 min
        private const val NEAR_EXPIRY_THRESHOLD = 60_000L   // considered near-expiry within 1 min
    }

    private fun publishLeases() {
        _leaseInfos.value = leases.mapValues { (_, c) ->
            LeaseInfo(sessionId = c.sessionId, expiresAtMs = c.expiresAt)
        }
    }

    // -------------------------------------------------------------------------
    // Public API
    // -------------------------------------------------------------------------

    /**
     * Get a valid lease for a renderer, auto-acquiring or renewing as needed.
     *
     * - If a cached lease exists and is not expiring within 30s, return it.
     * - If the lease is expiring within 30s, renew it first.
     * - If no lease exists, acquire a new one.
     */
    suspend fun ensureLease(rendererId: String): Lease {
        val cached = leases[rendererId]
        val nowMs = System.currentTimeMillis()

        if (cached != null) {
            val remainingMs = cached.expiresAt - nowMs
            if (remainingMs > NEAR_EXPIRY_THRESHOLD) {
                // Lease is valid and not near expiry.
                return Lease(sessionId = cached.sessionId, token = cached.token)
            }

            // Lease is expiring soon -- try to renew.
            return try {
                renewLease(rendererId, cached)
            } catch (e: Exception) {
                Log.w(tag, "Renewal failed for $rendererId, re-acquiring: ${e.message}")
                leases.remove(rendererId)
                acquireLease(rendererId)
            }
        }

        return acquireLease(rendererId)
    }

    /**
     * Release a lease explicitly. If we don't hold a cached lease, simply
     * drop any stale state and return — the contested-take path is handled
     * elsewhere via `takeControl`.
     */
    suspend fun releaseLease(rendererId: String) {
        val cached = leases.remove(rendererId) ?: run {
            publishLeases()
            return
        }
        publishLeases()

        try {
            val body = json.encodeToJsonElement(
                mapOf("sessionId" to cached.sessionId, "token" to cached.token)
            )
            transport.send(
                nodeId = rendererId,
                cmdType = "session.release",
                body = body,
                lease = Lease(sessionId = cached.sessionId, token = cached.token),
            )
            Log.i(tag, "Released lease for $rendererId")
        } catch (e: Exception) {
            Log.w(tag, "Failed to release lease for $rendererId: ${e.message}")
        }
    }

    /**
     * Release all leases. Called on shutdown.
     */
    suspend fun releaseAll() {
        renewalJob?.cancel()
        renewalJob = null

        for (rendererId in leases.keys.toList()) {
            releaseLease(rendererId)
        }
        publishLeases()
    }

    /**
     * Drop every cached lease without attempting any network release. Used by
     * hard-reset / reconnect flows where the broker state is about to be
     * thrown away anyway.
     */
    fun clearAll() {
        renewalJob?.cancel()
        renewalJob = null
        leases.clear()
        publishLeases()
        Log.i(tag, "Cleared all cached leases")
    }

    /**
     * Drop the cached lease for a single renderer without touching the
     * network. The next [ensureLease] call will acquire a fresh lease.
     */
    fun invalidate(rendererId: String) {
        if (leases.remove(rendererId) != null) {
            publishLeases()
            Log.i(tag, "Invalidated cached lease for $rendererId")
        }
    }

    /**
     * Run a block against a renderer with a valid lease, automatically
     * refreshing the cache and retrying once if the server rejects the lease
     * with a LEASE_* error. This avoids the trap where a stale cached lease
     * keeps being sent until its client-side TTL elapses.
     */
    suspend fun withLeaseRetry(
        rendererId: String,
        block: suspend (Lease) -> ReplyEnvelope,
    ): ReplyEnvelope {
        val lease = ensureLease(rendererId)
        val reply = block(lease)
        if (!reply.ok && isLeaseError(reply.err?.code)) {
            Log.w(tag, "Lease rejected for $rendererId (${reply.err?.code}), reacquiring")
            invalidate(rendererId)
            val fresh = ensureLease(rendererId)
            return block(fresh)
        }
        return reply
    }

    private fun isLeaseError(code: String?): Boolean =
        code == "LEASE_MISMATCH" || code == "LEASE_REQUIRED" || code == "LEASE_EXPIRED"

    /**
     * Start background renewal coroutine. Checks every 30s for leases that
     * are expiring within the renewal threshold and renews them proactively,
     * and sweeps expired entries out of the cache. Must be called for leases
     * to stay alive across idle periods; otherwise the server-side lease
     * expires silently and media-control commands start getting dropped.
     */
    fun startRenewal(scope: CoroutineScope) {
        renewalJob?.cancel()
        renewalJob = scope.launch {
            while (isActive) {
                delay(RENEWAL_CHECK_INTERVAL)
                renewExpiringLeases()
            }
        }
        Log.i(tag, "Started lease renewal loop")
    }

    // -------------------------------------------------------------------------
    // Internals
    // -------------------------------------------------------------------------

    private suspend fun acquireLease(rendererId: String): Lease {
        val body = json.encodeToJsonElement(SessionAcquireBody(ttlMs = TTL_MS))

        val reply = transport.send(
            nodeId = rendererId,
            cmdType = "session.acquire",
            body = body,
        )

        if (!reply.ok) {
            val errorCode = reply.err?.code ?: "UNKNOWN"
            throw LeaseException("session.acquire failed for $rendererId: $errorCode - ${reply.err?.message}")
        }

        val sessionReply = json.decodeFromJsonElement<SessionReplyBody>(
            reply.body ?: throw LeaseException("session.acquire reply missing body")
        )

        val cached = CachedLease(
            sessionId = sessionReply.session.id,
            token = sessionReply.session.token,
            expiresAt = sessionReply.session.leaseExpiresAt * 1000, // server sends unix seconds, we use millis
        )
        leases[rendererId] = cached
        publishLeases()

        Log.i(tag, "Acquired lease for $rendererId, session=${cached.sessionId}")
        return Lease(sessionId = cached.sessionId, token = cached.token)
    }

    private suspend fun renewLease(rendererId: String, cached: CachedLease): Lease {
        val body = json.encodeToJsonElement(SessionRenewBody(ttlMs = TTL_MS))

        val reply = transport.send(
            nodeId = rendererId,
            cmdType = "session.renew",
            body = body,
            lease = Lease(sessionId = cached.sessionId, token = cached.token),
        )

        if (!reply.ok) {
            val errorCode = reply.err?.code ?: "UNKNOWN"

            // On LEASE_MISMATCH, clear cache and let the caller re-acquire.
            if (errorCode == "LEASE_MISMATCH") {
                Log.w(tag, "LEASE_MISMATCH for $rendererId, clearing cache")
                leases.remove(rendererId)
                return acquireLease(rendererId)
            }

            throw LeaseException("session.renew failed for $rendererId: $errorCode - ${reply.err?.message}")
        }

        val sessionReply = json.decodeFromJsonElement<SessionReplyBody>(
            reply.body ?: throw LeaseException("session.renew reply missing body")
        )

        val renewed = CachedLease(
            sessionId = sessionReply.session.id,
            token = sessionReply.session.token,
            expiresAt = sessionReply.session.leaseExpiresAt * 1000, // server sends unix seconds, we use millis
        )
        leases[rendererId] = renewed
        publishLeases()

        Log.d(tag, "Renewed lease for $rendererId")
        return Lease(sessionId = renewed.sessionId, token = renewed.token)
    }

    private suspend fun renewExpiringLeases() {
        val nowMs = System.currentTimeMillis()
        var mutated = false

        for ((rendererId, cached) in leases) {
            val remainingMs = cached.expiresAt - nowMs
            if (remainingMs in 1..RENEWAL_THRESHOLD) {
                try {
                    renewLease(rendererId, cached)
                    // publishLeases() happens inside renewLease on success.
                } catch (e: Exception) {
                    Log.e(tag, "Background renewal failed for $rendererId: ${e.message}")
                    // Drop the dead cache entry so the next user action can
                    // cleanly re-acquire instead of being stuck on a stale
                    // token.
                    leases.remove(rendererId)
                    mutated = true
                }
            } else if (remainingMs <= 0) {
                // Already expired, remove from cache.
                Log.w(tag, "Lease expired for $rendererId, removing from cache")
                leases.remove(rendererId)
                mutated = true
            }
        }

        if (mutated) publishLeases()
    }
}

class LeaseException(message: String) : Exception(message)
