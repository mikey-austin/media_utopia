package com.mediautopia.app.ui.screen.library

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mediautopia.app.data.protocol.ItemRef
import com.mediautopia.app.data.protocol.QueueAddBody
import com.mediautopia.app.data.protocol.QueueEntry
import com.mediautopia.app.data.protocol.ResolvedSource
import com.mediautopia.app.data.repository.ActiveRendererRepository
import com.mediautopia.app.data.repository.BrowseItem
import com.mediautopia.app.data.repository.LibraryRepository
import com.mediautopia.app.data.repository.NodeRepository
import com.mediautopia.app.domain.usecase.CommandCorrelator
import com.mediautopia.app.domain.usecase.LeaseManager
import com.mediautopia.app.ui.SnackbarManager
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.debounce
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.encodeToJsonElement
import javax.inject.Inject

data class LibraryUiState(
    val breadcrumbs: List<BreadcrumbItem> = listOf(BreadcrumbItem("", "Library")),
    val items: List<BrowseItem> = emptyList(),
    val searchQuery: String = "",
    val isSearching: Boolean = false,
    val isLoading: Boolean = true,
    val hasMore: Boolean = false,
    val error: String? = null,
)

data class BreadcrumbItem(
    val containerId: String,
    val label: String,
)

@OptIn(FlowPreview::class)
@HiltViewModel
class LibraryViewModel @Inject constructor(
    private val libraryRepository: LibraryRepository,
    private val nodeRepository: NodeRepository,
    private val leaseManager: LeaseManager,
    private val correlator: CommandCorrelator,
    private val activeRendererRepository: ActiveRendererRepository,
    private val snackbarManager: SnackbarManager,
) : ViewModel() {

    private val tag = "LibraryViewModel"
    private val json = Json { ignoreUnknownKeys = true }
    private val pageSize = 50L

    private val _uiState = MutableStateFlow(LibraryUiState())
    val uiState: StateFlow<LibraryUiState> = _uiState.asStateFlow()

    private val activeRendererId = activeRendererRepository.activeRendererId
        .stateIn(viewModelScope, SharingStarted.Eagerly, ActiveRendererRepository.LOCAL_RENDERER_ID)

    // Debounced search input.
    private val searchInput = MutableSharedFlow<String>(extraBufferCapacity = 1)

    private var loadMoreJob: Job? = null

    // Which library we're currently browsing (null = show library selector).
    private var selectedLibraryNodeId: String? = null

    init {
        // Show available libraries as the top-level view.
        loadLibraryList()

        // Debounced search handler.
        viewModelScope.launch {
            searchInput
                .debounce(300)
                .collectLatest { query ->
                    if (query.isBlank()) {
                        // Return to current browse view.
                        val currentContainer = _uiState.value.breadcrumbs.lastOrNull()?.containerId ?: ""
                        _uiState.update { it.copy(isSearching = false, searchQuery = "") }
                        browseContainer(currentContainer)
                    } else {
                        performSearch(query)
                    }
                }
        }
    }

    // -------------------------------------------------------------------------
    // Library list (top-level view)
    // -------------------------------------------------------------------------

    private fun loadLibraryList() {
        viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true) }
            val libraries = nodeRepository.libraries.first()
            val items = libraries.map { node ->
                BrowseItem(
                    id = node.nodeId,
                    type = "Library",
                    title = node.name,
                    subtitle = node.nodeId.substringAfter("mu:library:").substringBefore(":"),
                    isContainer = true,
                )
            }
            _uiState.update { it.copy(items = items, isLoading = false) }
        }
    }

    /**
     * Select a library and browse its root.
     */
    fun selectLibrary(libraryNodeId: String, name: String) {
        selectedLibraryNodeId = libraryNodeId
        _uiState.update { state ->
            state.copy(
                breadcrumbs = listOf(
                    BreadcrumbItem("", "Libraries"),
                    BreadcrumbItem("__lib__:$libraryNodeId", name),
                ),
                isLoading = true,
            )
        }
        browseContainerOnLibrary(libraryNodeId, "")
    }

    // -------------------------------------------------------------------------
    // Navigation
    // -------------------------------------------------------------------------

    fun navigateTo(containerId: String, label: String) {
        // If at the library list level, this is a library selection.
        if (selectedLibraryNodeId == null) {
            selectLibrary(containerId, label)
            return
        }
        _uiState.update { state ->
            state.copy(
                breadcrumbs = state.breadcrumbs + BreadcrumbItem(containerId, label),
                isLoading = true,
                error = null,
            )
        }
        browseContainer(containerId)
    }

    fun navigateBack(): Boolean {
        val currentBreadcrumbs = _uiState.value.breadcrumbs
        if (currentBreadcrumbs.size <= 1) return false

        // Going back to library list.
        if (currentBreadcrumbs.size == 2 && selectedLibraryNodeId != null) {
            selectedLibraryNodeId = null
            _uiState.update { it.copy(
                breadcrumbs = listOf(BreadcrumbItem("", "Libraries")),
                isSearching = false,
                searchQuery = "",
            ) }
            loadLibraryList()
            return true
        }

        val newBreadcrumbs = currentBreadcrumbs.dropLast(1)
        val target = newBreadcrumbs.last()

        _uiState.update { state ->
            state.copy(
                breadcrumbs = newBreadcrumbs,
                isLoading = true,
                error = null,
                isSearching = false,
                searchQuery = "",
            )
        }
        browseContainer(target.containerId)
        return true
    }

    fun navigateToBreadcrumb(index: Int) {
        val currentBreadcrumbs = _uiState.value.breadcrumbs
        if (index < 0 || index >= currentBreadcrumbs.size) return

        // Going back to library list.
        if (index == 0 && selectedLibraryNodeId != null) {
            selectedLibraryNodeId = null
            _uiState.update { it.copy(
                breadcrumbs = listOf(BreadcrumbItem("", "Libraries")),
                isSearching = false,
                searchQuery = "",
            ) }
            loadLibraryList()
            return
        }

        val newBreadcrumbs = currentBreadcrumbs.take(index + 1)
        val target = newBreadcrumbs.last()

        _uiState.update { state ->
            state.copy(
                breadcrumbs = newBreadcrumbs,
                isLoading = true,
                error = null,
                isSearching = false,
                searchQuery = "",
            )
        }
        browseContainer(target.containerId)
    }

    // -------------------------------------------------------------------------
    // Search
    // -------------------------------------------------------------------------

    fun search(query: String) {
        _uiState.update { it.copy(searchQuery = query) }
        searchInput.tryEmit(query)
    }

    fun clearSearch() {
        _uiState.update { it.copy(searchQuery = "", isSearching = false) }
        val currentContainer = _uiState.value.breadcrumbs.lastOrNull()?.containerId ?: ""
        browseContainer(currentContainer)
    }

    // -------------------------------------------------------------------------
    // Pagination
    // -------------------------------------------------------------------------

    fun loadMore() {
        if (loadMoreJob?.isActive == true) return
        val state = _uiState.value
        if (!state.hasMore || state.isLoading) return

        loadMoreJob = viewModelScope.launch {
            val currentContainer = state.breadcrumbs.lastOrNull()?.containerId ?: ""
            val start = state.items.size.toLong()

            try {
                if (state.isSearching) {
                    val moreItems = libraryRepository.search(
                        query = state.searchQuery,
                        start = start,
                        count = pageSize,
                    )
                    _uiState.update { s ->
                        s.copy(
                            items = s.items + moreItems,
                            hasMore = moreItems.size.toLong() >= pageSize,
                        )
                    }
                } else {
                    val result = libraryRepository.browse(
                        containerId = currentContainer,
                        start = start,
                        count = pageSize,
                    )
                    _uiState.update { s ->
                        s.copy(
                            items = s.items + result.items,
                            hasMore = result.hasMore,
                        )
                    }
                }
            } catch (e: Exception) {
                Log.e(tag, "loadMore failed: ${e.message}")
            }
        }
    }

    // -------------------------------------------------------------------------
    // Playback actions
    // -------------------------------------------------------------------------

    private suspend fun buildQueueEntry(itemId: String): QueueEntry {
        val source = libraryRepository.resolveForPlayback(itemId)
        return if (source != null) {
            QueueEntry(
                ref = ItemRef(id = itemId),
                resolved = ResolvedSource(
                    itemId = itemId,
                    url = source.url,
                    mime = source.mime,
                ),
            )
        } else {
            QueueEntry(ref = ItemRef(id = itemId))
        }
    }

    private suspend fun buildQueueEntries(itemIds: List<String>): List<QueueEntry> {
        val sources = libraryRepository.resolveForPlaybackBatch(itemIds)
        return itemIds.map { itemId ->
            val source = sources[itemId]
            if (source != null) {
                QueueEntry(
                    ref = ItemRef(id = itemId),
                    resolved = ResolvedSource(
                        itemId = itemId,
                        url = source.url,
                        mime = source.mime,
                    ),
                )
            } else {
                QueueEntry(ref = ItemRef(id = itemId))
            }
        }
    }

    fun playItem(itemId: String) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val entry = buildQueueEntry(itemId)
                val lease = leaseManager.ensureLease(rendererId)

                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(
                        QueueAddBody(
                            position = "next",
                            entries = listOf(entry),
                        )
                    ),
                    lease = lease,
                )

                correlator.send(
                    nodeId = rendererId,
                    cmdType = "playback.next",
                    body = json.encodeToJsonElement(mapOf<String, String>()),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "playItem failed: ${e.message}")
            }
        }
    }

    fun addToQueue(itemId: String) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val entry = buildQueueEntry(itemId)
                val lease = leaseManager.ensureLease(rendererId)

                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(
                        QueueAddBody(
                            position = "end",
                            entries = listOf(entry),
                        )
                    ),
                    lease = lease,
                )
                snackbarManager.show("1 item added to queue")
            } catch (e: Exception) {
                Log.e(tag, "addToQueue failed: ${e.message}")
            }
        }
    }

    fun playContainer(containerId: String) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val allItemIds = collectContainerItemIds(containerId)
                if (allItemIds.isEmpty()) return@launch

                val entries = buildQueueEntries(allItemIds)
                val lease = leaseManager.ensureLease(rendererId)

                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(
                        QueueAddBody(position = "next", entries = entries)
                    ),
                    lease = lease,
                )

                correlator.send(
                    nodeId = rendererId,
                    cmdType = "playback.next",
                    body = json.encodeToJsonElement(mapOf<String, String>()),
                    lease = lease,
                )
                snackbarManager.show("Playing ${entries.size} items")
            } catch (e: Exception) {
                Log.e(tag, "playContainer failed: ${e.message}")
            }
        }
    }

    /**
     * Play all tracks currently displayed in the list.
     */
    fun playAllVisible() {
        val tracks = _uiState.value.items.filter { !it.isContainer }
        if (tracks.isEmpty()) return

        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val entries = buildQueueEntries(tracks.map { it.id })
                val lease = leaseManager.ensureLease(rendererId)

                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(
                        QueueAddBody(position = "next", entries = entries)
                    ),
                    lease = lease,
                )

                correlator.send(
                    nodeId = rendererId,
                    cmdType = "playback.next",
                    body = json.encodeToJsonElement(mapOf<String, String>()),
                    lease = lease,
                )
                snackbarManager.show("Playing ${entries.size} items")
            } catch (e: Exception) {
                Log.e(tag, "playAllVisible failed: ${e.message}")
            }
        }
    }

    /**
     * Queue all tracks currently displayed in the list.
     */
    fun queueAllVisible() {
        val tracks = _uiState.value.items.filter { !it.isContainer }
        if (tracks.isEmpty()) return

        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val entries = buildQueueEntries(tracks.map { it.id })
                val lease = leaseManager.ensureLease(rendererId)

                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(
                        QueueAddBody(position = "end", entries = entries)
                    ),
                    lease = lease,
                )
                snackbarManager.show("${entries.size} items added to queue")
            } catch (e: Exception) {
                Log.e(tag, "queueAllVisible failed: ${e.message}")
            }
        }
    }

    private suspend fun collectContainerItemIds(containerId: String): List<String> {
        val allItemIds = mutableListOf<String>()
        var start = 0L
        var hasMore = true

        while (hasMore) {
            val result = libraryRepository.browse(
                containerId = containerId,
                start = start,
                count = pageSize,
                libraryNodeId = selectedLibraryNodeId,
            )
            allItemIds.addAll(result.items.filter { !it.isContainer }.map { it.id })
            hasMore = result.hasMore
            start += pageSize
        }
        return allItemIds
    }

    // -------------------------------------------------------------------------
    // Internal
    // -------------------------------------------------------------------------

    private fun browseContainerOnLibrary(libraryNodeId: String, containerId: String) {
        viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true, error = null) }
            try {
                val result = libraryRepository.browse(
                    containerId = containerId,
                    start = 0,
                    count = pageSize,
                    libraryNodeId = libraryNodeId,
                )
                _uiState.update { state ->
                    state.copy(
                        items = result.items,
                        isLoading = false,
                        hasMore = result.hasMore,
                        error = null,
                    )
                }
            } catch (e: Exception) {
                Log.e(tag, "browseContainerOnLibrary failed: ${e.message}")
                _uiState.update { it.copy(isLoading = false, error = e.message) }
            }
        }
    }

    private fun browseContainer(containerId: String) {
        val libId = selectedLibraryNodeId
        if (libId != null) {
            browseContainerOnLibrary(libId, containerId)
            return
        }
        viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true, error = null) }

            try {
                val result = libraryRepository.browse(
                    containerId = containerId,
                    start = 0,
                    count = pageSize,
                )
                _uiState.update { state ->
                    state.copy(
                        items = result.items,
                        isLoading = false,
                        hasMore = result.hasMore,
                        error = null,
                    )
                }
            } catch (e: Exception) {
                Log.e(tag, "browse failed: ${e.message}")
                _uiState.update { state ->
                    state.copy(
                        isLoading = false,
                        error = "Failed to load library",
                    )
                }
            }
        }
    }

    private suspend fun performSearch(query: String) {
        _uiState.update { it.copy(isSearching = true, isLoading = true, error = null) }

        try {
            val results = libraryRepository.search(
                query = query,
                start = 0,
                count = pageSize,
            )
            _uiState.update { state ->
                state.copy(
                    items = results,
                    isLoading = false,
                    hasMore = results.size.toLong() >= pageSize,
                    error = null,
                )
            }
        } catch (e: Exception) {
            Log.e(tag, "search failed: ${e.message}")
            _uiState.update { state ->
                state.copy(
                    isLoading = false,
                    error = "Search failed",
                )
            }
        }
    }
}
