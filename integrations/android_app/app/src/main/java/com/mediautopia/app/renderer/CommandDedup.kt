package com.mediautopia.app.renderer

/**
 * Ring-buffer deduplication tracker for MQTT QoS 1 command redelivery.
 *
 * Port of `pkg/mu/dedup.go`. Uses a fixed-size array plus a hash set for
 * O(1) lookups. When the ring wraps around, the evicted entry is removed
 * from the set.
 *
 * Not thread-safe internally -- the caller must synchronize access.
 */
class CommandDedup(capacity: Int = 64) {

    private val size = capacity.coerceAtLeast(16)
    private val ring = arrayOfNulls<String>(size)
    private val set = HashSet<String>(size)
    private var next = 0

    /**
     * Returns true if [commandId] was recently seen (i.e. is a duplicate),
     * and records it if not. This is a combined check-and-record operation,
     * matching the Go `Seen()` semantics.
     */
    fun seen(commandId: String): Boolean {
        if (commandId.isEmpty()) return false

        if (commandId in set) {
            return true
        }

        // Evict the oldest entry.
        val idx = next % size
        ring[idx]?.let { old -> set.remove(old) }
        ring[idx] = commandId
        set.add(commandId)
        next++
        return false
    }
}

/**
 * Returns true for command types that mutate state and should be deduplicated.
 * Session commands and read-only queries are excluded.
 */
fun shouldDedup(cmdType: String): Boolean {
    if (cmdType.startsWith("session.")) return false
    return when (cmdType) {
        "queue.get",
        "library.browse", "library.search", "library.resolve",
        "library.resolveBatch", "library.rescan",
        "playlist.list", "playlist.get",
        "snapshot.list", "snapshot.get",
        -> false
        else -> true
    }
}
