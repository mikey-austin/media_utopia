package com.mediautopia.app.ui.screen.queue

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mediautopia.app.data.protocol.LibraryItemRef
import com.mediautopia.app.data.protocol.QueueEntry
import com.mediautopia.app.data.protocol.QueueGetBody
import com.mediautopia.app.data.protocol.QueueGetReply
import com.mediautopia.app.data.protocol.QueueMoveBody
import com.mediautopia.app.data.protocol.QueueRemoveBody
import com.mediautopia.app.data.protocol.QueueRepeatBody
import com.mediautopia.app.data.protocol.QueueSetShuffleBody
import com.mediautopia.app.data.protocol.QueueShuffleBody
import com.mediautopia.app.data.protocol.PlaybackPlayBody
import com.mediautopia.app.data.protocol.RendererState
import com.mediautopia.app.data.protocol.artistString
import com.mediautopia.app.data.repository.ActiveRendererRepository
import com.mediautopia.app.data.repository.LibraryRepository
import com.mediautopia.app.data.repository.NodeRepository
import com.mediautopia.app.data.repository.RendererStateRepository
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
import kotlinx.serialization.json.encodeToJsonElement
import javax.inject.Inject

data class QueueUiState(
    val entries: List<QueueEntryUi> = emptyList(),
    val currentIndex: Long = 0,
    val totalTracks: Int = 0,
    val totalDuration: String = "",
    val shuffle: Boolean = false,
    val repeatMode: String = "",
    val isLoading: Boolean = true,
    val canMutate: Boolean = true,
)

data class QueueEntryUi(
    val queueEntryId: String,
    val ref: LibraryItemRef? = null,
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
    private val libraryRepository: LibraryRepository,
    private val leaseManager: LeaseManager,
    private val transport: com.mediautopia.app.data.transport.TransportRouter,
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
        leaseManager.leaseInfos,
        activeRendererId,
    ) { state, entries, isLoading, leaseInfos, activeId ->
        val queue = state?.queue
        val currentIndex = queue?.index ?: 0

        val markedEntries = entries.mapIndexed { i, entry ->
            entry.copy(isActive = i.toLong() == currentIndex)
        }

        val totalDurationMs = markedEntries.sumOf { it.durationMs }

        // Mutations (remove, move, clear, shuffle, repeat) require holding
        // the renderer's lease. Without it the renderer rejects the command,
        // which would desync the optimistic local state from the server.
        val leaseOwner = state?.session?.owner
        val canMutate = leaseOwner == null || leaseInfos[activeId] != null

        QueueUiState(
            entries = markedEntries,
            currentIndex = currentIndex,
            totalTracks = markedEntries.size,
            totalDuration = formatTotalDuration(totalDurationMs),
            shuffle = queue?.shuffle ?: false,
            repeatMode = queue?.repeatMode ?: "",
            isLoading = isLoading,
            canMutate = canMutate,
        )
    }.stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000),
        initialValue = QueueUiState(),
    )

    private fun canMutateNow(): Boolean = uiState.value.canMutate

    init {
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

        viewModelScope.launch {
            activeRendererRepository.activeRendererId
                .distinctUntilChanged()
                .collect { rendererId ->
                    lastRevision = -1
                    _entries.value = emptyList()
                    _isLoading.value = true
                    fetchQueueFor(rendererId)
                }
        }
    }

    private fun fetchQueue() = fetchQueueFor(activeRendererId.value)

    private fun fetchQueueFor(rendererId: String) {
        viewModelScope.launch {
            try {
                _isLoading.value = true
                Log.d(tag, "fetchQueueFor: rendererId=$rendererId")

                val body = json.encodeToJsonElement(
                    QueueGetBody(from = 0, count = 200, resolve = "")
                )
                val reply = transport.send(
                    nodeId = rendererId,
                    cmdType = "queue.get",
                    body = body,
                )

                if (!reply.ok || reply.body == null) {
                    Log.e(tag, "queue.get failed for $rendererId: ok=${reply.ok} err=${reply.err?.message}")
                    _isLoading.value = false
                    return@launch
                }

                val queueReply = json.decodeFromJsonElement(
                    QueueGetReply.serializer(),
                    reply.body!!,
                )
                Log.d(tag, "queue.get success: ${queueReply.entries.size} entries, rev=${queueReply.revision}")
                lastRevision = queueReply.revision

                _entries.value = buildEntries(queueReply.entries)
            } catch (e: Exception) {
                Log.e(tag, "fetchQueue failed: ${e.message}")
            } finally {
                _isLoading.value = false
            }
        }
    }

    private fun buildEntries(items: List<QueueEntry>): List<QueueEntryUi> {
        return items.mapIndexed { index, item ->
            val display = item.display
            val title = display?.title?.takeIf { it.isNotEmpty() }
                ?: item.ref?.itemId?.substringAfterLast(":")
                ?: "Unknown"
            val artist = display?.artistString() ?: ""
            val artworkUrl = display?.artworkUrl
            val durationMs = display?.durationMs ?: 0L

            QueueEntryUi(
                queueEntryId = item.queueEntryId,
                ref = item.ref,
                index = index,
                title = title,
                artist = artist,
                artworkUrl = artworkUrl,
                durationMs = durationMs,
            )
        }
    }

    // -------------------------------------------------------------------------
    // Queue mutations
    // -------------------------------------------------------------------------

    fun moveTrack(fromIndex: Int, toIndex: Int) {
        if (fromIndex == toIndex) return
        if (!canMutateNow()) return

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
                val reply = transport.send(
                    nodeId = rendererId,
                    cmdType = "queue.move",
                    body = body,
                    lease = lease,
                    ifRevision = lastRevision,
                )
                if (!reply.ok) {
                    Log.e(tag, "queue.move failed: ${reply.err?.message}")
                    fetchQueue()
                }
            } catch (e: Exception) {
                Log.e(tag, "moveTrack failed: ${e.message}")
                fetchQueue()
            }
        }
    }

    fun removeTrack(queueEntryId: String) {
        if (!canMutateNow()) return
        val current = _entries.value.toMutableList()
        val index = current.indexOfFirst { it.queueEntryId == queueEntryId }
        if (index < 0) return

        val removed = current.removeAt(index)
        _entries.value = current.mapIndexed { i, e -> e.copy(index = i) }

        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                // Send the queueEntryId; the server uses it as a stable key
                // and the index hint avoids a scan when the local view of the
                // queue still matches the renderer's revision.
                val body = json.encodeToJsonElement(
                    QueueRemoveBody(queueEntryId = removed.queueEntryId, index = index.toLong())
                )
                transport.send(
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
        if (!canMutateNow()) return
        _entries.value = emptyList()

        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                transport.send(
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
        if (!canMutateNow()) return
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                val body = json.encodeToJsonElement(
                    QueueShuffleBody(seed = System.currentTimeMillis())
                )
                transport.send(
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
        if (!canMutateNow()) return
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            val currentShuffle = rendererState.value?.queue?.shuffle ?: false
            try {
                val lease = leaseManager.ensureLease(rendererId)
                transport.send(
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
        if (!canMutateNow()) return
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            val currentMode = rendererState.value?.queue?.repeatMode ?: ""
            val nextMode = when (currentMode) {
                "", "off" -> "all"
                "all" -> "one"
                else -> "off"
            }
            try {
                val lease = leaseManager.ensureLease(rendererId)
                transport.send(
                    nodeId = rendererId,
                    cmdType = "queue.setRepeat",
                    body = json.encodeToJsonElement(
                        QueueRepeatBody(repeat = nextMode != "off", mode = nextMode)
                    ),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "toggleRepeat failed: ${e.message}")
            }
        }
    }

    fun jumpToTrack(queueEntryId: String) {
        if (!canMutateNow()) return
        val current = _entries.value
        val index = current.indexOfFirst { it.queueEntryId == queueEntryId }
        if (index < 0) return
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                transport.send(
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
