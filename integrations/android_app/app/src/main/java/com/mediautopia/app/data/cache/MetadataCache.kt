package com.mediautopia.app.data.cache

import java.util.concurrent.locks.ReentrantReadWriteLock
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.concurrent.read
import kotlin.concurrent.write

data class ResolvedMetadata(
    val title: String = "",
    val artist: String = "",
    val album: String = "",
    val artworkUrl: String? = null,
    val format: String = "",  // "FLAC", "MP3", etc.
    val sampleRate: Int = 0,
    val bitDepth: Int = 0,
    val durationMs: Long = 0,
)

@Singleton
class MetadataCache @Inject constructor() {

    companion object {
        private const val MAX_CAPACITY = 10_000
        private const val EVICT_COUNT = 2_000
        private const val RETRY_COOLDOWN_MS = 60_000L
    }

    /**
     * LRU cache backed by a LinkedHashMap with access-order ordering.
     * When capacity is exceeded, the oldest [EVICT_COUNT] entries are removed.
     */
    private val cache = object : LinkedHashMap<String, ResolvedMetadata>(
        /* initialCapacity = */ 256,
        /* loadFactor = */ 0.75f,
        /* accessOrder = */ true,
    ) {
        // We handle eviction manually in put/putAll rather than using
        // removeEldestEntry, so that we can evict in batches.
    }

    /** Tracks failed lookups: itemId -> failure timestamp (millis). */
    private val failures = HashMap<String, Long>()

    private val lock = ReentrantReadWriteLock()

    /**
     * Get cached metadata for an item ID, or null if not cached.
     * Accessing an entry promotes it in LRU order.
     */
    fun get(itemId: String): ResolvedMetadata? = lock.read {
        cache[itemId]
    }

    /**
     * Put resolved metadata into the cache.
     */
    fun put(itemId: String, metadata: ResolvedMetadata) = lock.write {
        cache[itemId] = metadata
        failures.remove(itemId)
        evictIfNeeded()
    }

    /**
     * Batch put resolved metadata.
     */
    fun putAll(items: Map<String, ResolvedMetadata>) = lock.write {
        for ((itemId, metadata) in items) {
            cache[itemId] = metadata
            failures.remove(itemId)
        }
        evictIfNeeded()
    }

    /**
     * Mark a failed lookup with the current timestamp. This prevents
     * retrying the lookup too aggressively.
     */
    fun markFailed(itemId: String) = lock.write {
        failures[itemId] = System.currentTimeMillis()
    }

    /**
     * Check if a previously failed item should be retried.
     * Returns true if the failure was recorded more than 60s ago,
     * or if no failure was recorded at all.
     */
    fun shouldRetry(itemId: String): Boolean = lock.read {
        val failedAt = failures[itemId] ?: return@read true
        System.currentTimeMillis() - failedAt > RETRY_COOLDOWN_MS
    }

    /**
     * Clear all cached metadata and failure records.
     */
    fun clear() = lock.write {
        cache.clear()
        failures.clear()
    }

    /**
     * Evict the oldest entries when the cache exceeds capacity.
     * Must be called while holding the write lock.
     */
    private fun evictIfNeeded() {
        if (cache.size <= MAX_CAPACITY) return

        val iterator = cache.entries.iterator()
        var evicted = 0
        while (iterator.hasNext() && evicted < EVICT_COUNT) {
            iterator.next()
            iterator.remove()
            evicted++
        }
    }
}
