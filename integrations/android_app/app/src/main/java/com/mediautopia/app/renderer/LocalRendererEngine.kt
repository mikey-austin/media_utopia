package com.mediautopia.app.renderer

import android.util.Log
import com.mediautopia.app.data.protocol.CommandEnvelope
import com.mediautopia.app.data.protocol.CurrentItemState
import com.mediautopia.app.data.protocol.PlaybackSeekBody
import com.mediautopia.app.data.protocol.PlaybackSetMuteBody
import com.mediautopia.app.data.protocol.PlaybackSetVolumeBody
import com.mediautopia.app.data.protocol.PlaybackState
import com.mediautopia.app.data.protocol.QueueAddBody
import com.mediautopia.app.data.protocol.QueueGetBody
import com.mediautopia.app.data.protocol.QueueJumpBody
import com.mediautopia.app.data.protocol.QueueMoveBody
import com.mediautopia.app.data.protocol.QueueRemoveBody
import com.mediautopia.app.data.protocol.QueueRepeatBody
import com.mediautopia.app.data.protocol.QueueSetBody
import com.mediautopia.app.data.protocol.QueueSetShuffleBody
import com.mediautopia.app.data.protocol.QueueShuffleBody
import com.mediautopia.app.data.protocol.PlaybackPlayBody
import com.mediautopia.app.data.protocol.RendererState
import com.mediautopia.app.data.protocol.ReplyEnvelope
import com.mediautopia.app.data.protocol.ReplyError
import com.mediautopia.app.data.protocol.SessionAcquireBody
import com.mediautopia.app.data.protocol.SessionLease
import com.mediautopia.app.data.protocol.SessionRenewBody
import com.mediautopia.app.data.protocol.SessionReplyBody
import com.mediautopia.app.data.protocol.SessionState
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import java.security.SecureRandom
import java.util.concurrent.locks.ReentrantLock
import java.util.concurrent.locks.ReentrantReadWriteLock
import kotlin.concurrent.read
import kotlin.concurrent.withLock
import kotlin.concurrent.write

/**
 * Command dispatcher for the local renderer. Port of `renderer_core/engine.go`.
 *
 * Processes all MU renderer commands (session, playback, queue) and maintains
 * the authoritative renderer state. State changes are emitted to [stateFlow]
 * for debounced publishing to MQTT.
 *
 * Thread safety:
 * - Session commands use [sessionLock] independently so lease renewals are
 *   never blocked behind slow driver operations.
 * - All other commands use [engineLock] which also protects the queue and
 *   playback state.
 * - Position updates from the polling loop also use [engineLock].
 */
class LocalRendererEngine(
    val nodeId: String,
    val name: String,
    private val driver: MuDriver,
) {
    private val tag = "LocalRendererEngine"
    private val json = Json { ignoreUnknownKeys = true }

    val queue = LocalQueue()
    private val dedup = CommandDedup()
    private val leaseManager = RendererLeaseManager(idPrefix = nodeId)

    /** Main lock for queue + playback state. Read lock for queue.get only. */
    val engineLock = ReentrantReadWriteLock()

    /** Separate lock for session commands to avoid blocking on driver ops. */
    private val sessionLock = ReentrantLock()

    // State management
    private var stateVersion: Long = 0
    private var playbackStatus: String = "stopped"
    private var positionMs: Long = 0
    private var durationMs: Long = 0
    private var volume: Double = 1.0
    private var mute: Boolean = false
    private var currentItem: CurrentItemState? = null

    /** Track the queue entry ID that triggered end-of-stream, to avoid double-advance. */
    private var eosSeen: String = ""

    private val _stateFlow = MutableStateFlow(buildStateLocked())
    val stateFlow: StateFlow<RendererState> = _stateFlow

    // -----------------------------------------------------------------
    // Session commands (separate lock)
    // -----------------------------------------------------------------

    fun handleSessionCommand(cmd: CommandEnvelope): ReplyEnvelope {
        sessionLock.withLock {
            return when (cmd.type) {
                "session.acquire" -> handleSessionAcquire(cmd)
                "session.renew" -> handleSessionRenew(cmd)
                "session.release" -> handleSessionRelease(cmd)
                else -> errorReply(cmd, "INVALID", "not a session command")
            }
        }
    }

    // -----------------------------------------------------------------
    // General command dispatch
    // -----------------------------------------------------------------

    fun handleCommand(cmd: CommandEnvelope): ReplyEnvelope {
        // Dedup mutating commands.
        if (shouldDedup(cmd.type)) {
            if (dedup.seen(cmd.id)) {
                return ackReply(cmd)
            }
        }

        return when (cmd.type) {
            // Session commands can also arrive through this path.
            "session.acquire" -> {
                sessionLock.withLock { handleSessionAcquire(cmd) }
            }
            "session.renew" -> {
                sessionLock.withLock { handleSessionRenew(cmd) }
            }
            "session.release" -> {
                sessionLock.withLock { handleSessionRelease(cmd) }
            }

            // Queue read (uses read lock).
            "queue.get" -> handleQueueGet(cmd)

            // Queue mutations.
            "queue.set" -> handleQueueSet(cmd)
            "queue.add" -> handleQueueAdd(cmd)
            "queue.remove" -> handleQueueRemove(cmd)
            "queue.move" -> handleQueueMove(cmd)
            "queue.clear" -> handleQueueClear(cmd)
            "queue.jump" -> handleQueueJump(cmd)
            "queue.shuffle" -> handleQueueShuffle(cmd)
            "queue.setShuffle" -> handleQueueSetShuffle(cmd)
            "queue.setRepeat" -> handleQueueRepeat(cmd)

            // Playback.
            "playback.play" -> handlePlaybackPlay(cmd)
            "playback.pause" -> handlePlaybackPause(cmd)
            "playback.stop" -> handlePlaybackStop(cmd)
            "playback.seek" -> handlePlaybackSeek(cmd)
            "playback.next" -> handlePlaybackNext(cmd)
            "playback.prev" -> handlePlaybackPrev(cmd)
            "playback.setVolume" -> handleSetVolume(cmd)
            "playback.setMute" -> handleSetMute(cmd)

            else -> errorReply(cmd, "INVALID", "unsupported command")
        }
    }

    // -----------------------------------------------------------------
    // Position update (called from polling loop)
    // -----------------------------------------------------------------

    fun updatePosition() {
        val pos = driver.position()

        engineLock.write {
            if (playbackStatus != "playing") {
                eosSeen = ""
                return
            }
            if (!pos.isValid) return

            positionMs = pos.positionMs
            if (pos.durationMs > 0) {
                durationMs = pos.durationMs
            }

            // Check for end-of-stream and auto-advance.
            advanceOnEnd(pos.positionMs, pos.durationMs)
            emitState()
        }
    }

    /**
     * Called by the ExoPlayer onTrackFinished callback. Advances to the next
     * queue entry if available.
     */
    fun onTrackFinished() {
        engineLock.write {
            if (playbackStatus != "playing") return
            advanceAfterEnd()
        }
    }

    fun buildState(): RendererState {
        engineLock.read {
            return buildStateLocked()
        }
    }

    /**
     * Get the current queue entry (for metadata in MediaSession).
     */
    fun currentEntry(): LocalQueueEntry? {
        engineLock.read {
            val idx = queue.index.toInt()
            return queue.entries.getOrNull(idx)
        }
    }

    /**
     * Get the current lease as a protocol Lease, or null if no active session.
     * Used by MediaSession transport to build valid command envelopes.
     */
    fun currentLease(): com.mediautopia.app.data.protocol.Lease? {
        val session = leaseManager.current() ?: return null
        return com.mediautopia.app.data.protocol.Lease(
            sessionId = session.id,
            token = leaseManager.currentToken() ?: return null,
        )
    }

    // -----------------------------------------------------------------
    // Session handlers
    // -----------------------------------------------------------------

    private fun handleSessionAcquire(cmd: CommandEnvelope): ReplyEnvelope {
        val body = decodeBody<SessionAcquireBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
        val ttlMs = if (body.ttlMs <= 0) 300_000L else body.ttlMs
        val lease = leaseManager.acquire(cmd.from, ttlMs) ?: return errorReply(cmd, "LEASE_MISMATCH", "lease already held")

        val sessionState = SessionState(
            id = lease.id,
            owner = lease.owner,
            leaseExpiresAt = lease.leaseExpiresAt,
        )
        // Session state is updated under sessionLock only; we bump stateVersion
        // but do not read queue state (which requires engineLock).
        stateVersion++
        val replyBody = SessionReplyBody(session = lease, stateVersion = stateVersion)
        emitStateWithSession(sessionState)
        return withBody(cmd, json.encodeToJsonElement(replyBody))
    }

    private fun handleSessionRenew(cmd: CommandEnvelope): ReplyEnvelope {
        if (cmd.lease == null) return errorReply(cmd, "LEASE_REQUIRED", "lease required")
        val body = decodeBody<SessionRenewBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
        val ttlMs = if (body.ttlMs <= 0) 300_000L else body.ttlMs
        val lease = leaseManager.renew(cmd.lease.sessionId, cmd.lease.token, ttlMs)
            ?: return errorReply(cmd, "LEASE_MISMATCH", "lease mismatch")

        val sessionState = SessionState(
            id = lease.id,
            owner = lease.owner,
            leaseExpiresAt = lease.leaseExpiresAt,
        )
        stateVersion++
        val replyBody = SessionReplyBody(session = lease, stateVersion = stateVersion)
        emitStateWithSession(sessionState)
        return withBody(cmd, json.encodeToJsonElement(replyBody))
    }

    private fun handleSessionRelease(cmd: CommandEnvelope): ReplyEnvelope {
        if (cmd.lease == null) return errorReply(cmd, "LEASE_REQUIRED", "lease required")
        if (!leaseManager.release(cmd.lease.sessionId, cmd.lease.token)) {
            return errorReply(cmd, "LEASE_MISMATCH", "lease mismatch")
        }
        stateVersion++
        emitStateWithSession(null)
        return ackReply(cmd)
    }

    // -----------------------------------------------------------------
    // Queue handlers (engine lock held by caller in processCommand)
    // -----------------------------------------------------------------

    private fun handleQueueGet(cmd: CommandEnvelope): ReplyEnvelope {
        val body = decodeBody<QueueGetBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
        engineLock.read {
            val result = queue.snapshot(body.from, body.count)
            return withBody(cmd, json.encodeToJsonElement(result))
        }
    }

    private fun handleQueueSet(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<QueueSetBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
            val entries = toLocalEntries(body.entries)
            if (!queue.set(body.startIndex, entries, cmd.ifRevision)) {
                return errorReply(cmd, "CONFLICT", "revision mismatch")
            }
            bumpQueue()
            return ackReply(cmd)
        }
    }

    private fun handleQueueAdd(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<QueueAddBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
            val entries = toLocalEntries(body.entries)
            if (!queue.add(body.position, body.atIndex, entries)) {
                return errorReply(cmd, "INVALID", "invalid position")
            }
            bumpQueue()
            return ackReply(cmd)
        }
    }

    private fun handleQueueRemove(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<QueueRemoveBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
            if (!queue.remove(body.queueEntryId.takeIf { it.isNotEmpty() }, body.index)) {
                return errorReply(cmd, "INVALID", "queueEntryId or index required")
            }
            bumpQueue()
            return ackReply(cmd)
        }
    }

    private fun handleQueueMove(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<QueueMoveBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
            if (!queue.move(body.fromIndex, body.toIndex)) {
                return errorReply(cmd, "INVALID", "index out of range")
            }
            bumpQueue()
            return ackReply(cmd)
        }
    }

    private fun handleQueueClear(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            queue.clear()
            bumpQueue()
            return ackReply(cmd)
        }
    }

    private fun handleQueueJump(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<QueueJumpBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
            if (queue.jump(body.index) == null) {
                return errorReply(cmd, "INVALID", "index out of range")
            }
            bumpQueue()
            return startCurrentPlayback(cmd)
        }
    }

    private fun handleQueueShuffle(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<QueueShuffleBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
            queue.shuffleEntries(body.seed)
            bumpQueue()
            return ackReply(cmd)
        }
    }

    private fun handleQueueSetShuffle(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<QueueSetShuffleBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
            queue.setShuffle(body.shuffle)
            bumpQueue()
            return ackReply(cmd)
        }
    }

    private fun handleQueueRepeat(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<QueueRepeatBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
            if (body.mode.isNotEmpty()) {
                queue.setRepeatMode(body.mode)
            } else {
                queue.setRepeat(body.repeat)
            }
            bumpQueue()
            return ackReply(cmd)
        }
    }

    // -----------------------------------------------------------------
    // Playback handlers
    // -----------------------------------------------------------------

    private fun handlePlaybackPlay(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<PlaybackPlayBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")

            if (body.index != null) {
                if (queue.jump(body.index) == null) {
                    return errorReply(cmd, "INVALID", "index out of range")
                }
            }

            // No explicit index: try resume if paused, no-op if already playing.
            if (body.index == null) {
                when (playbackStatus) {
                    "paused" -> {
                        driver.resume()
                        playbackStatus = "playing"
                        bumpState()
                        return ackReply(cmd)
                    }
                    "playing" -> return ackReply(cmd)
                }
            }

            return startCurrentPlayback(cmd)
        }
    }

    private fun handlePlaybackPause(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            driver.pause()
            playbackStatus = "paused"
            bumpState()
            return ackReply(cmd)
        }
    }

    private fun handlePlaybackStop(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            driver.stop()
            playbackStatus = "stopped"
            bumpState()
            return ackReply(cmd)
        }
    }

    private fun handlePlaybackSeek(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<PlaybackSeekBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
            driver.seekTo(body.positionMs)
            positionMs = body.positionMs
            bumpState()
            return ackReply(cmd)
        }
    }

    private fun handlePlaybackNext(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            if (queue.nextEntry() == null) {
                return errorReply(cmd, "NOT_FOUND", "end of queue")
            }
            return startCurrentPlayback(cmd)
        }
    }

    private fun handlePlaybackPrev(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            if (queue.prevEntry() == null) {
                return errorReply(cmd, "NOT_FOUND", "start of queue")
            }
            return startCurrentPlayback(cmd)
        }
    }

    private fun handleSetVolume(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<PlaybackSetVolumeBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
            driver.setVolume(body.volume.toFloat())
            volume = body.volume
            bumpState()
            return ackReply(cmd)
        }
    }

    private fun handleSetMute(cmd: CommandEnvelope): ReplyEnvelope {
        engineLock.write {
            requireLease(cmd)?.let { return it }
            val body = decodeBody<PlaybackSetMuteBody>(cmd) ?: return errorReply(cmd, "INVALID", "invalid body")
            driver.setMute(body.mute)
            mute = body.mute
            bumpState()
            return ackReply(cmd)
        }
    }

    // -----------------------------------------------------------------
    // Playback helpers (must be called with write lock held)
    // -----------------------------------------------------------------

    private fun startCurrentPlayback(cmd: CommandEnvelope): ReplyEnvelope {
        val entry = queue.currentEntry()
        if (entry == null) {
            return errorReply(cmd, "INVALID", "queue empty")
        }
        val url = entry.url
        if (url.isEmpty()) {
            return errorReply(cmd, "INVALID", "entry not resolved")
        }
        driver.play(url, 0)
        playbackStatus = "playing"
        currentItem = CurrentItemState(
            queueEntryId = entry.queueEntryId,
            itemId = entry.itemId,
            metadata = entry.metadata.takeIf { it.isNotEmpty() }?.let { kotlinx.serialization.json.JsonObject(it) },
        )
        bumpState()
        return ackReply(cmd)
    }

    /**
     * Detect end-of-stream based on position reaching duration and advance.
     * Uses [eosSeen] to avoid double-triggering.
     */
    private fun advanceOnEnd(posMs: Long, durMs: Long) {
        if (durMs <= 0) return
        if (posMs < durMs - 250) {
            eosSeen = ""
            return
        }
        val currentId = currentItem?.queueEntryId ?: return
        if (eosSeen == currentId) return
        eosSeen = currentId
        advanceAfterEnd()
    }

    /** Advance the queue after the current track ends. */
    private fun advanceAfterEnd() {
        if (playbackStatus != "playing") return
        val next = queue.advance()
        if (next == null) {
            driver.stop()
            playbackStatus = "stopped"
            positionMs = 0
            bumpState()
            return
        }
        val url = next.url
        if (url.isEmpty()) {
            driver.stop()
            playbackStatus = "stopped"
            bumpState()
            return
        }
        driver.play(url, 0)
        currentItem = CurrentItemState(
            queueEntryId = next.queueEntryId,
            itemId = next.itemId,
            metadata = next.metadata.takeIf { it.isNotEmpty() }?.let { kotlinx.serialization.json.JsonObject(it) },
        )
        bumpState()
    }

    // -----------------------------------------------------------------
    // Lease validation
    // -----------------------------------------------------------------

    /** Returns an error reply if the lease is missing or invalid, null if OK. */
    private fun requireLease(cmd: CommandEnvelope): ReplyEnvelope? {
        if (cmd.lease == null) return errorReply(cmd, "LEASE_REQUIRED", "lease required")
        if (!leaseManager.validate(cmd.lease.sessionId, cmd.lease.token)) {
            return errorReply(cmd, "LEASE_REQUIRED", "lease mismatch")
        }
        return null
    }

    // -----------------------------------------------------------------
    // State management
    // -----------------------------------------------------------------

    private fun bumpState() {
        stateVersion++
        emitState()
    }

    private fun bumpQueue() {
        stateVersion++
        emitState()
    }

    /** Emit a state snapshot to the flow. Must be called with appropriate lock. */
    private fun emitState() {
        _stateFlow.value = buildStateLocked()
    }

    /**
     * Emit state with an updated session, for use from session handlers
     * which hold sessionLock but not engineLock. We read only the session
     * state and stateVersion to avoid contention.
     */
    private fun emitStateWithSession(session: SessionState?) {
        // We cannot safely build the full state under sessionLock alone
        // (queue and playback are protected by engineLock), so we update
        // the existing state snapshot with just the session field.
        val current = _stateFlow.value
        _stateFlow.value = current.copy(
            session = session,
            stateVersion = stateVersion,
            ts = System.currentTimeMillis() / 1000,
        )
    }

    private fun buildStateLocked(): RendererState {
        val sessionState = leaseManager.current()

        return RendererState(
            session = sessionState,
            playback = PlaybackState(
                status = playbackStatus,
                positionMs = positionMs,
                durationMs = durationMs,
                volume = volume,
                mute = mute,
            ),
            queue = queue.summary(),
            current = currentItem,
            stateVersion = stateVersion,
            ts = System.currentTimeMillis() / 1000,
        )
    }

    // -----------------------------------------------------------------
    // Entry conversion
    // -----------------------------------------------------------------

    private fun toLocalEntries(entries: List<com.mediautopia.app.data.protocol.QueueEntry>): List<LocalQueueEntry> {
        return entries.map { entry ->
            val itemId = entry.ref?.id ?: entry.resolved?.url ?: ""
            val url = entry.resolved?.url ?: ""
            val mime = entry.resolved?.mime ?: ""
            val meta: Map<String, kotlinx.serialization.json.JsonElement> = entry.metadata ?: emptyMap()
            LocalQueueEntry(
                itemId = itemId,
                url = url,
                mime = mime,
                metadata = meta,
            )
        }
    }

    // -----------------------------------------------------------------
    // Reply builders
    // -----------------------------------------------------------------

    private fun ackReply(cmd: CommandEnvelope): ReplyEnvelope = ReplyEnvelope(
        id = cmd.id,
        type = "ack",
        ok = true,
        ts = System.currentTimeMillis() / 1000,
    )

    private fun withBody(cmd: CommandEnvelope, body: JsonElement): ReplyEnvelope = ReplyEnvelope(
        id = cmd.id,
        type = "ack",
        ok = true,
        ts = System.currentTimeMillis() / 1000,
        body = body,
    )

    private fun errorReply(cmd: CommandEnvelope, code: String, message: String): ReplyEnvelope = ReplyEnvelope(
        id = cmd.id,
        type = "error",
        ok = false,
        ts = System.currentTimeMillis() / 1000,
        err = ReplyError(code = code, message = message),
    )

    private inline fun <reified T> decodeBody(cmd: CommandEnvelope): T? {
        return try {
            json.decodeFromJsonElement<T>(cmd.body)
        } catch (e: Exception) {
            Log.w(tag, "Failed to decode body for ${cmd.type}: ${e.message}")
            null
        }
    }

    companion object {
        fun isSessionCommand(cmdType: String): Boolean = when (cmdType) {
            "session.acquire", "session.renew", "session.release" -> true
            else -> false
        }
    }
}

// =====================================================================
// Internal lease manager for the renderer engine.
// Port of renderer_core/lease.go.
// =====================================================================

/**
 * Tracks the active session lease. Thread-safe via its own internal lock.
 */
internal class RendererLeaseManager(private val idPrefix: String = "renderer") {

    private val lock = ReentrantLock()

    private var sessionId: String? = null
    private var token: String? = null
    private var owner: String? = null
    private var expiresAtMs: Long = 0

    fun acquire(requestOwner: String, ttlMs: Long): SessionLease? {
        lock.withLock {
            if (isActiveLocked()) return null  // Already held
            return newLeaseLocked(requestOwner, ttlMs)
        }
    }

    fun renew(sid: String, tok: String, ttlMs: Long): SessionLease? {
        lock.withLock {
            if (!isActiveLocked()) return null
            if (sessionId != sid || token != tok) return null
            expiresAtMs = System.currentTimeMillis() + ttlMs
            return currentLeaseLocked()
        }
    }

    fun release(sid: String, tok: String): Boolean {
        lock.withLock {
            if (!isActiveLocked()) return false
            if (sessionId != sid || token != tok) return false
            clearLocked()
            return true
        }
    }

    fun validate(sid: String, tok: String): Boolean {
        lock.withLock {
            if (!isActiveLocked()) return false
            return sessionId == sid && token == tok
        }
    }

    fun currentToken(): String? {
        lock.withLock {
            if (!isActiveLocked()) return null
            return token
        }
    }

    fun current(): SessionState? {
        lock.withLock {
            if (!isActiveLocked()) return null
            return SessionState(
                id = sessionId!!,
                owner = owner!!,
                leaseExpiresAt = expiresAtMs / 1000,
            )
        }
    }

    private fun isActiveLocked(): Boolean =
        sessionId != null && System.currentTimeMillis() < expiresAtMs

    private fun newLeaseLocked(requestOwner: String, ttlMs: Long): SessionLease {
        sessionId = "mu:session:$idPrefix:${randHex()}"
        token = randHex()
        owner = requestOwner
        expiresAtMs = System.currentTimeMillis() + ttlMs
        return currentLeaseLocked()!!
    }

    private fun currentLeaseLocked(): SessionLease? {
        val sid = sessionId ?: return null
        val tok = token ?: return null
        val own = owner ?: return null
        return SessionLease(
            id = sid,
            token = tok,
            owner = own,
            leaseExpiresAt = expiresAtMs / 1000,
        )
    }

    private fun clearLocked() {
        sessionId = null
        token = null
        owner = null
        expiresAtMs = 0
    }

    companion object {
        private val secureRandom = SecureRandom()

        private fun randHex(): String {
            val bytes = ByteArray(16)
            secureRandom.nextBytes(bytes)
            return bytes.joinToString("") { "%02x".format(it) }
        }
    }
}
