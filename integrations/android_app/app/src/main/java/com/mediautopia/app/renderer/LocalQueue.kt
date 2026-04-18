package com.mediautopia.app.renderer

import com.mediautopia.app.data.protocol.CurrentItemState
import com.mediautopia.app.data.protocol.DisplayMetadata
import com.mediautopia.app.data.protocol.LibraryItemRef
import com.mediautopia.app.data.protocol.QueueEntry
import com.mediautopia.app.data.protocol.QueueGetReply
import com.mediautopia.app.data.protocol.QueueState
import com.mediautopia.app.data.protocol.ResolvedSource
import java.util.UUID
import kotlin.random.Random

/**
 * In-memory queue for the local renderer engine.
 *
 * Port of `renderer_core/queue.go`. All public methods must be called
 * under the engine's lock -- the queue itself does not synchronize.
 */
class LocalQueue {

    var entries: MutableList<LocalQueueEntry> = mutableListOf()
    var revision: Long = 0
        private set
    var index: Long = 0
    var shuffle: Boolean = false
        private set
    var repeat: Boolean = false
        private set
    var repeatMode: String = ""
        private set

    fun restoreState(rev: Long, shuf: Boolean, rep: Boolean, repMode: String) {
        revision = rev
        shuffle = shuf
        repeat = rep
        repeatMode = repMode
    }

    fun snapshot(from: Long, count: Long): QueueGetReply {
        val start = clampIndex(from, entries.size.toLong()).toInt()
        val end = clampIndex(from + count, entries.size.toLong()).toInt()
        val items = entries.subList(start, end).map { it.toWireEntry() }
        return QueueGetReply(revision = revision, index = index, entries = items)
    }

    fun summary(): QueueState = QueueState(
        revision = revision,
        length = entries.size.toLong(),
        index = index,
        repeat = repeat,
        repeatMode = repeatMode,
        shuffle = shuffle,
    )

    fun currentEntry(): LocalQueueEntry? {
        if (entries.isEmpty() || index < 0 || index >= entries.size) return null
        return entries[index.toInt()]
    }

    fun set(startIndex: Long, newEntries: List<LocalQueueEntry>, ifRevision: Long? = null): Boolean {
        if (ifRevision != null && ifRevision != revision) return false
        entries = newEntries.toMutableList()
        index = clampIndex(startIndex, (entries.size - 1).toLong().coerceAtLeast(0))
        bumpRevision()
        return true
    }

    fun add(position: String, atIndex: Long?, newEntries: List<LocalQueueEntry>): Boolean {
        when (position) {
            "end" -> entries.addAll(newEntries)
            "next" -> {
                val insertAt = (index + 1).toInt().coerceIn(0, entries.size)
                entries.addAll(insertAt, newEntries)
            }
            "at" -> {
                val idx = atIndex ?: return false
                val insertAt = idx.toInt().coerceIn(0, entries.size)
                entries.addAll(insertAt, newEntries)
            }
            else -> return false
        }
        bumpRevision()
        return true
    }

    fun remove(queueEntryId: String?, removeIndex: Long?): Boolean {
        if (!queueEntryId.isNullOrEmpty()) {
            entries.removeAll { it.queueEntryId == queueEntryId }
            index = clampIndex(index, (entries.size - 1).toLong().coerceAtLeast(0))
            bumpRevision()
            return true
        }
        if (removeIndex != null) {
            val idx = removeIndex.toInt()
            if (idx < 0 || idx >= entries.size) return false
            entries.removeAt(idx)
            index = clampIndex(index, (entries.size - 1).toLong().coerceAtLeast(0))
            bumpRevision()
            return true
        }
        return false
    }

    fun move(fromIndex: Long, toIndex: Long): Boolean {
        val from = fromIndex.toInt()
        val to = toIndex.toInt()
        if (from < 0 || from >= entries.size) return false
        if (to < 0 || to >= entries.size) return false
        val entry = entries.removeAt(from)
        entries.add(to, entry)
        index = clampIndex(index, (entries.size - 1).toLong().coerceAtLeast(0))
        bumpRevision()
        return true
    }

    fun clear() {
        entries.clear()
        index = 0
        bumpRevision()
    }

    fun jump(jumpIndex: Long): LocalQueueEntry? {
        val resolvedIndex = if (jumpIndex < 0) {
            (entries.size + jumpIndex).toLong()
        } else jumpIndex
        if (resolvedIndex < 0 || resolvedIndex >= entries.size) return null
        index = resolvedIndex
        bumpRevision()
        return entries[index.toInt()]
    }

    fun shuffleEntries(seed: Long) {
        if (entries.size < 2) return
        val currentId = currentEntry()?.queueEntryId
        val rng = if (seed != 0L) Random(seed) else Random(System.nanoTime())
        entries.shuffle(rng)
        if (currentId != null) {
            val newIdx = entries.indexOfFirst { it.queueEntryId == currentId }
            if (newIdx >= 0) index = newIdx.toLong()
        }
        shuffle = true
        bumpRevision()
    }

    fun setShuffle(value: Boolean) {
        shuffle = value
        bumpRevision()
    }

    fun setRepeat(value: Boolean) {
        repeat = value
        repeatMode = if (value) "all" else "off"
        bumpRevision()
    }

    fun setRepeatMode(mode: String) {
        when (mode.lowercase().trim()) {
            "one", "single" -> {
                repeatMode = "one"
                repeat = true
            }
            "all", "on", "true" -> {
                repeatMode = "all"
                repeat = true
            }
            else -> {
                repeatMode = "off"
                repeat = false
            }
        }
        bumpRevision()
    }

    fun nextEntry(): LocalQueueEntry? {
        if (entries.isEmpty()) return null
        if (repeatMode == "one") return currentEntry()
        val next = index + 1
        if (next >= entries.size) {
            if (!repeat) return null
            index = 0
        } else {
            index = next
        }
        bumpRevision()
        return currentEntry()
    }

    fun prevEntry(): LocalQueueEntry? {
        if (entries.isEmpty()) return null
        if (repeatMode == "one") return currentEntry()
        val prev = index - 1
        if (prev < 0) {
            if (!repeat) return null
            index = (entries.size - 1).toLong()
        } else {
            index = prev
        }
        bumpRevision()
        return currentEntry()
    }

    fun advance(): LocalQueueEntry? = nextEntry()

    private fun bumpRevision() {
        revision++
    }

    companion object {
        private fun clampIndex(index: Long, max: Long): Long {
            if (max < 0) return 0
            return index.coerceIn(0, max)
        }
    }
}

/**
 * In-memory representation of a queue entry for the local renderer engine.
 * Carries the structured ref/resolved/display fields so they can be re-emitted
 * unchanged on snapshot save and queue.get replies.
 */
data class LocalQueueEntry(
    val queueEntryId: String = "mu:queueentry:renderer:R:${UUID.randomUUID()}",
    val ref: LibraryItemRef? = null,
    val resolved: ResolvedSource? = null,
    val display: DisplayMetadata? = null,
) {
    val url: String get() = resolved?.url ?: ""

    fun toWireEntry(): QueueEntry = QueueEntry(
        queueEntryId = queueEntryId,
        ref = ref,
        resolved = resolved,
        display = display,
    )

    fun toCurrentItemState(): CurrentItemState = CurrentItemState(
        queueEntryId = queueEntryId,
        ref = ref,
        resolved = resolved,
        display = display,
    )

    companion object {
        fun fromWireEntry(entry: QueueEntry): LocalQueueEntry = LocalQueueEntry(
            queueEntryId = entry.queueEntryId.ifEmpty {
                "mu:queueentry:renderer:R:${UUID.randomUUID()}"
            },
            ref = entry.ref,
            resolved = entry.resolved,
            display = entry.display,
        )
    }
}
