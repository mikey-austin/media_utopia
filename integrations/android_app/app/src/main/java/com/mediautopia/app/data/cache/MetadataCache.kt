package com.mediautopia.app.data.cache

import com.mediautopia.app.data.protocol.DisplayMetadata
import com.mediautopia.app.data.protocol.LibraryItemRef
import java.util.concurrent.locks.ReentrantReadWriteLock
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.concurrent.read
import kotlin.concurrent.write

@Singleton
class MetadataCache @Inject constructor() {

    companion object {
        private const val MAX_CAPACITY = 10_000
        private const val EVICT_COUNT = 2_000
        private const val RETRY_COOLDOWN_MS = 60_000L
    }

    private val cache = object : LinkedHashMap<String, DisplayMetadata>(
        /* initialCapacity = */ 256,
        /* loadFactor = */ 0.75f,
        /* accessOrder = */ true,
    ) {}

    private val failures = HashMap<String, Long>()

    private val lock = ReentrantReadWriteLock()

    fun get(ref: LibraryItemRef): DisplayMetadata? = lock.read {
        cache[refKey(ref)]
    }

    fun put(ref: LibraryItemRef, display: DisplayMetadata) = lock.write {
        val key = refKey(ref)
        cache[key] = display
        failures.remove(key)
        evictIfNeeded()
    }

    fun putAll(items: Map<LibraryItemRef, DisplayMetadata>) = lock.write {
        for ((ref, display) in items) {
            val key = refKey(ref)
            cache[key] = display
            failures.remove(key)
        }
        evictIfNeeded()
    }

    fun markFailed(ref: LibraryItemRef) = lock.write {
        failures[refKey(ref)] = System.currentTimeMillis()
    }

    fun shouldRetry(ref: LibraryItemRef): Boolean = lock.read {
        val failedAt = failures[refKey(ref)] ?: return@read true
        System.currentTimeMillis() - failedAt > RETRY_COOLDOWN_MS
    }

    fun clear() = lock.write {
        cache.clear()
        failures.clear()
    }

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

    private fun refKey(ref: LibraryItemRef): String = "${ref.libraryId}|${ref.itemId}"
}
