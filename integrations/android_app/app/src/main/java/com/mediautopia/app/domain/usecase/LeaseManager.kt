package com.mediautopia.app.domain.usecase

import android.util.Log
import com.mediautopia.app.data.protocol.Lease
import com.mediautopia.app.data.protocol.SessionAcquireBody
import com.mediautopia.app.data.protocol.SessionRenewBody
import com.mediautopia.app.data.protocol.SessionReplyBody
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
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

@Singleton
class LeaseManager @Inject constructor(
    private val correlator: CommandCorrelator,
) {
    private val tag = "LeaseManager"

    private val json = Json { ignoreUnknownKeys = true }

    private val leases = ConcurrentHashMap<String, CachedLease>()

    private var renewalJob: Job? = null

    companion object {
        private const val TTL_MS = 300_000L           // 5 minutes
        private const val RENEWAL_CHECK_INTERVAL = 30_000L  // 30 seconds
        private const val RENEWAL_THRESHOLD = 60_000L       // renew if expiring within 60s
        private const val NEAR_EXPIRY_THRESHOLD = 30_000L   // considered near-expiry within 30s
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
     * Release a lease explicitly. If we don't hold a cached lease,
     * acquire one first (stealing from any current holder) then release it.
     */
    suspend fun releaseLease(rendererId: String) {
        var cached = leases.remove(rendererId)

        if (cached == null) {
            // We don't hold a lease — acquire one (stealing from current holder) then release.
            try {
                val lease = acquireLease(rendererId)
                cached = leases.remove(rendererId)
                    ?: CachedLease(sessionId = lease.sessionId, token = lease.token, expiresAt = 0)
            } catch (e: Exception) {
                Log.w(tag, "Could not acquire lease to release for $rendererId: ${e.message}")
                return
            }
        }

        try {
            val body = json.encodeToJsonElement(
                mapOf("sessionId" to cached.sessionId, "token" to cached.token)
            )
            correlator.send(
                nodeId = rendererId,
                cmdType = "session.release",
                body = body,
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
    }

    /**
     * Start background renewal coroutine. Checks every 30s for leases that
     * are expiring within 60s and renews them proactively.
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

        val reply = correlator.send(
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

        Log.i(tag, "Acquired lease for $rendererId, session=${cached.sessionId}")
        return Lease(sessionId = cached.sessionId, token = cached.token)
    }

    private suspend fun renewLease(rendererId: String, cached: CachedLease): Lease {
        val body = json.encodeToJsonElement(SessionRenewBody(ttlMs = TTL_MS))

        val reply = correlator.send(
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

        Log.d(tag, "Renewed lease for $rendererId")
        return Lease(sessionId = renewed.sessionId, token = renewed.token)
    }

    private suspend fun renewExpiringLeases() {
        val nowMs = System.currentTimeMillis()

        for ((rendererId, cached) in leases) {
            val remainingMs = cached.expiresAt - nowMs
            if (remainingMs in 1..RENEWAL_THRESHOLD) {
                try {
                    renewLease(rendererId, cached)
                } catch (e: Exception) {
                    Log.e(tag, "Background renewal failed for $rendererId: ${e.message}")
                    // Don't remove -- ensureLease will handle re-acquire on next use.
                }
            } else if (remainingMs <= 0) {
                // Already expired, remove from cache.
                Log.w(tag, "Lease expired for $rendererId, removing from cache")
                leases.remove(rendererId)
            }
        }
    }
}

class LeaseException(message: String) : Exception(message)
