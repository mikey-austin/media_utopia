package com.mediautopia.app.ui.screen.queue

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mediautopia.app.data.cache.MetadataCache
import com.mediautopia.app.data.cache.ResolvedMetadata
import com.mediautopia.app.data.protocol.LibraryResolveBatchBody
import com.mediautopia.app.data.protocol.LibraryResolveBatchReply
import com.mediautopia.app.data.protocol.QueueGetBody
import com.mediautopia.app.data.protocol.QueueGetReply
import com.mediautopia.app.data.protocol.QueueItem
import com.mediautopia.app.data.protocol.QueueMoveBody
import com.mediautopia.app.data.protocol.QueueRemoveBody
import com.mediautopia.app.data.protocol.QueueRepeatBody
import com.mediautopia.app.data.protocol.QueueSetShuffleBody
import com.mediautopia.app.data.protocol.QueueShuffleBody
import com.mediautopia.app.data.protocol.PlaybackPlayBody
import com.mediautopia.app.data.protocol.RendererState
import com.mediautopia.app.data.repository.ActiveRendererRepository
import com.mediautopia.app.data.repository.NodeRepository
import com.mediautopia.app.data.repository.RendererStateRepository
import com.mediautopia.app.domain.usecase.CommandCorrelator
import com.mediautopia.app.domain.usecase.LeaseManager
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onStart
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.longOrNull
import javax.inject.Inject

data class QueueUiState(
    val entries: List<QueueEntryUi> = emptyList(),
    val currentIndex: Long = 0,
    val totalTracks: Int = 0,
    val totalDuration: String = "",
    val shuffle: Boolean = false,
    val repeatMode: String = "",
    val isLoading: Boolean = true,
)

data class QueueEntryUi(
    val queueEntryId: String,
    val itemId: String,
    val index: Int,
    val title: String,
    val artist: String,
    val artworkUrl: String? = null,
    val durationMs: Long = 0,
    val isActive: Boolean = false,
)

@OptIn(ExperimentalCoroutinesApi::class)
@HiltViewModel
class QueueViewModel @Inject constructor(
    private val activeRendererRepository: ActiveRendererRepository,
    private val rendererStateRepository: RendererStateRepository,
    private val nodeRepository: NodeRepository,
    private val metadataCache: MetadataCache,
    private val leaseManager: LeaseManager,
    private val correlator: CommandCorrelator,
) : ViewModel() {

    private val tag = "QueueViewModel"
    private val json = Json { ignoreUnknownKeys = true }

    private val _entries = MutableStateFlow<List<QueueEntryUi>>(emptyList())
    private val _isLoading = MutableStateFlow(true)
    private var lastRevision: Long = -1

    private val activeRendererId = activeRendererRepository.activeRendererId
        .stateIn(viewModelScope, SharingStarted.Eagerly, ActiveRendererRepository.LOCAL_RENDERER_ID)

    private val rendererState: StateFlow<RendererState?> =
        activeRendererRepository.activeRendererId.flatMapLatest { rendererId ->
            rendererStateRepository.observeState(rendererId)
                .onStart<RendererState?> { emit(null) }
                .catch { emit(null) }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    val uiState: StateFlow<QueueUiState> = combine(
        rendererState,
        _entries,
        _isLoading,
    ) { state, entries, isLoading ->
        val queue = state?.queue
        val currentIndex = queue?.index ?: 0

        // Mark the active entry.
        val markedEntries = entries.mapIndexed { i, entry ->
            entry.copy(isActive = i.toLong() == currentIndex)
        }

        val totalDurationMs = markedEntries.sumOf { it.durationMs }

        QueueUiState(
            entries = markedEntries,
            currentIndex = currentIndex,
            totalTracks = markedEntries.size,
            totalDuration = formatTotalDuration(totalDurationMs),
            shuffle = queue?.shuffle ?: false,
            repeatMode = queue?.repeatMode ?: "",
            isLoading = isLoading,
        )
    }.stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000),
        initialValue = QueueUiState(),
    )

    init {
        // Watch for queue revision changes to trigger fetches.
        viewModelScope.launch {
            rendererState
                .map { it?.queue?.revision }
                .distinctUntilChanged()
                .collect { revision ->
                    if (revision != null && revision != lastRevision) {
                        lastRevision = revision
                        fetchQueue()
                    }
                }
        }

        // Initial load when renderer changes.
        viewModelScope.launch {
            activeRendererRepository.activeRendererId
                .distinctUntilChanged()
                .collect {
                    lastRevision = -1
                    _entries.value = emptyList()
                    _isLoading.value = true
                    fetchQueue()
                }
        }
    }

    // -------------------------------------------------------------------------
    // Queue fetching
    // -------------------------------------------------------------------------

    private fun fetchQueue() {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                _isLoading.value = true

                val body = json.encodeToJsonElement(
                    QueueGetBody(from = 0, count = 200)
                )
                val reply = correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.get",
                    body = body,
                )

                if (!reply.ok || reply.body == null) {
                    Log.e(tag, "queue.get failed: ${reply.err?.message}")
                    _isLoading.value = false
                    return@launch
                }

                val queueReply = json.decodeFromJsonElement<QueueGetReply>(reply.body!!)
                lastRevision = queueReply.revision

                val entries = buildEntries(queueReply.entries)
                _entries.value = entries

                // Resolve missing metadata in background.
                resolveMissingMetadata(rendererId, queueReply.entries, entries)
            } catch (e: Exception) {
                Log.e(tag, "fetchQueue failed: ${e.message}")
            } finally {
                _isLoading.value = false
            }
        }
    }

    private fun buildEntries(items: List<QueueItem>): List<QueueEntryUi> {
        return items.mapIndexed { index, item ->
            // Priority: inline metadata from response, then cache.
            val inlineMeta = item.metadata
            val cached = metadataCache.get(item.itemId)

            val title = inlineMeta?.stringValue("title")
                ?: cached?.title
                ?: item.itemId.substringAfterLast("/")
            val artist = inlineMeta?.stringValue("artist")
                ?: cached?.artist
                ?: ""
            val artworkUrl = inlineMeta?.stringValue("artworkUrl")
                ?: cached?.artworkUrl
            val durationMs = inlineMeta?.longValue("durationMs")
                ?: cached?.durationMs
                ?: 0

            // Cache inline metadata if present and not already cached.
            if (inlineMeta != null && cached == null) {
                cacheFromInline(item.itemId, inlineMeta)
            }

            QueueEntryUi(
                queueEntryId = item.queueEntryId,
                itemId = item.itemId,
                index = index,
                title = title,
                artist = artist,
                artworkUrl = artworkUrl,
                durationMs = durationMs,
            )
        }
    }

    private fun cacheFromInline(itemId: String, metadata: Map<String, JsonElement>) {
        val resolved = ResolvedMetadata(
            title = metadata.stringValue("title") ?: "",
            artist = metadata.stringValue("artist") ?: "",
            album = metadata.stringValue("album") ?: "",
            artworkUrl = metadata.stringValue("artworkUrl"),
            format = metadata.stringValue("format") ?: "",
            sampleRate = metadata.stringValue("sampleRate")?.replace("[^0-9]".toRegex(), "")?.toIntOrNull() ?: 0,
            bitDepth = metadata.stringValue("bitDepth")?.replace("[^0-9]".toRegex(), "")?.toIntOrNull() ?: 0,
            durationMs = metadata.longValue("durationMs") ?: 0,
        )
        metadataCache.put(itemId, resolved)
    }

    private fun resolveMissingMetadata(
        rendererId: String,
        rawItems: List<QueueItem>,
        currentEntries: List<QueueEntryUi>,
    ) {
        // Find item IDs that have no metadata (no inline, no cache).
        val missingIds = rawItems.filter { item ->
            item.metadata == null && metadataCache.get(item.itemId) == null && metadataCache.shouldRetry(item.itemId)
        }.map { it.itemId }.distinct()

        if (missingIds.isEmpty()) return

        viewModelScope.launch {
            try {
                // Find a library node to resolve against.
                val libraryNode = nodeRepository.nodes.value.values
                    .firstOrNull { it.kind == "library" }
                    ?: return@launch

                val body = json.encodeToJsonElement(
                    LibraryResolveBatchBody(itemIds = missingIds, metadataOnly = true)
                )
                val reply = correlator.send(
                    nodeId = libraryNode.nodeId,
                    cmdType = "library.resolveBatch",
                    body = body,
                )

                if (!reply.ok || reply.body == null) {
                    missingIds.forEach { metadataCache.markFailed(it) }
                    return@launch
                }

                val batchReply = json.decodeFromJsonElement<LibraryResolveBatchReply>(reply.body!!)

                // Cache resolved metadata.
                for (resolved in batchReply.items) {
                    if (resolved.err != null) {
                        metadataCache.markFailed(resolved.itemId)
                        continue
                    }
                    val meta = resolved.metadata ?: continue
                    cacheFromInline(resolved.itemId, meta)
                }

                // Update entries with newly resolved metadata.
                _entries.value = _entries.value.map { entry ->
                    if (entry.itemId in missingIds) {
                        val cached = metadataCache.get(entry.itemId)
                        if (cached != null) {
                            entry.copy(
                                title = cached.title.ifEmpty { entry.title },
                                artist = cached.artist,
                                artworkUrl = cached.artworkUrl ?: entry.artworkUrl,
                                durationMs = if (cached.durationMs > 0) cached.durationMs else entry.durationMs,
                            )
                        } else entry
                    } else entry
                }
            } catch (e: Exception) {
                Log.e(tag, "resolveMissingMetadata failed: ${e.message}")
                missingIds.forEach { metadataCache.markFailed(it) }
            }
        }
    }

    // -------------------------------------------------------------------------
    // Queue mutations
    // -------------------------------------------------------------------------

    fun moveTrack(fromIndex: Int, toIndex: Int) {
        if (fromIndex == toIndex) return

        // Optimistic UI update.
        val current = _entries.value.toMutableList()
        if (fromIndex !in current.indices || toIndex !in current.indices) return
        val item = current.removeAt(fromIndex)
        current.add(toIndex, item)
        _entries.value = current.mapIndexed { i, e -> e.copy(index = i) }

        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                val body = json.encodeToJsonElement(
                    QueueMoveBody(fromIndex = fromIndex.toLong(), toIndex = toIndex.toLong())
                )
                val reply = correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.move",
                    body = body,
                    lease = lease,
                    ifRevision = lastRevision,
                )
                if (!reply.ok) {
                    Log.e(tag, "queue.move failed: ${reply.err?.message}")
                    fetchQueue() // Revert by re-fetching.
                }
            } catch (e: Exception) {
                Log.e(tag, "moveTrack failed: ${e.message}")
                fetchQueue()
            }
        }
    }

    fun removeTrack(index: Int) {
        val current = _entries.value.toMutableList()
        if (index !in current.indices) return

        val removed = current.removeAt(index)
        _entries.value = current.mapIndexed { i, e -> e.copy(index = i) }

        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                val body = json.encodeToJsonElement(
                    QueueRemoveBody(queueEntryId = removed.queueEntryId, index = index.toLong())
                )
                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.remove",
                    body = body,
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "removeTrack failed: ${e.message}")
                fetchQueue()
            }
        }
    }

    fun clearQueue() {
        _entries.value = emptyList()

        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.clear",
                    body = json.encodeToJsonElement(mapOf<String, String>()),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "clearQueue failed: ${e.message}")
                fetchQueue()
            }
        }
    }

    fun shuffleQueue() {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                val body = json.encodeToJsonElement(
                    QueueShuffleBody(seed = System.currentTimeMillis())
                )
                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.shuffle",
                    body = body,
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "shuffleQueue failed: ${e.message}")
            }
        }
    }

    fun toggleShuffle() {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            val currentShuffle = rendererState.value?.queue?.shuffle ?: false
            try {
                val lease = leaseManager.ensureLease(rendererId)
                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.setShuffle",
                    body = json.encodeToJsonElement(
                        QueueSetShuffleBody(shuffle = !currentShuffle)
                    ),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "toggleShuffle failed: ${e.message}")
            }
        }
    }

    fun toggleRepeat() {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            val currentMode = rendererState.value?.queue?.repeatMode ?: ""
            val (nextRepeat, nextMode) = when (currentMode) {
                "" -> true to "all"
                "all" -> true to "one"
                "one" -> false to ""
                else -> false to ""
            }
            try {
                val lease = leaseManager.ensureLease(rendererId)
                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.setRepeat",
                    body = json.encodeToJsonElement(
                        QueueRepeatBody(repeat = nextRepeat, mode = nextMode)
                    ),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "toggleRepeat failed: ${e.message}")
            }
        }
    }

    fun jumpToTrack(index: Int) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                correlator.send(
                    nodeId = rendererId,
                    cmdType = "playback.play",
                    body = json.encodeToJsonElement(
                        PlaybackPlayBody(index = index.toLong())
                    ),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "jumpToTrack failed: ${e.message}")
            }
        }
    }

    // -------------------------------------------------------------------------
    // Helpers
    // -------------------------------------------------------------------------

    private fun Map<String, JsonElement>.stringValue(key: String): String? {
        return (this[key] as? JsonPrimitive)?.contentOrNull
    }

    private fun Map<String, JsonElement>.longValue(key: String): Long? {
        return (this[key] as? JsonPrimitive)?.longOrNull
    }

    companion object {
        fun formatTotalDuration(totalMs: Long): String {
            val totalSeconds = (totalMs / 1000).toInt()
            val hours = totalSeconds / 3600
            val minutes = (totalSeconds % 3600) / 60
            val seconds = totalSeconds % 60
            return if (hours > 0) {
                "%d:%02d:%02d".format(hours, minutes, seconds)
            } else {
                "%d:%02d".format(minutes, seconds)
            }
        }
    }
}
