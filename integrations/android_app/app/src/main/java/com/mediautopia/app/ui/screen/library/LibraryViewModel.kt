package com.mediautopia.app.ui.screen.library

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mediautopia.app.data.protocol.ItemRef
import com.mediautopia.app.data.protocol.QueueAddBody
import com.mediautopia.app.data.protocol.QueueEntry
import com.mediautopia.app.data.repository.ActiveRendererRepository
import com.mediautopia.app.data.repository.BrowseItem
import com.mediautopia.app.data.repository.LibraryRepository
import com.mediautopia.app.domain.usecase.CommandCorrelator
import com.mediautopia.app.domain.usecase.LeaseManager
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
    private val leaseManager: LeaseManager,
    private val correlator: CommandCorrelator,
    private val activeRendererRepository: ActiveRendererRepository,
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

    init {
        // Initial browse of root.
        browseContainer("")

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
    // Navigation
    // -------------------------------------------------------------------------

    fun navigateTo(containerId: String, label: String) {
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

    fun playItem(itemId: String) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)

                // Add to queue at "next" position.
                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(
                        QueueAddBody(
                            position = "next",
                            entries = listOf(QueueEntry(ref = ItemRef(id = itemId))),
                        )
                    ),
                    lease = lease,
                )

                // Advance to the newly added track.
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
                val lease = leaseManager.ensureLease(rendererId)

                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(
                        QueueAddBody(
                            position = "end",
                            entries = listOf(QueueEntry(ref = ItemRef(id = itemId))),
                        )
                    ),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "addToQueue failed: ${e.message}")
            }
        }
    }

    fun playContainer(containerId: String) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                // Browse all items in the container.
                val allItems = mutableListOf<BrowseItem>()
                var start = 0L
                var hasMore = true

                while (hasMore) {
                    val result = libraryRepository.browse(
                        containerId = containerId,
                        start = start,
                        count = pageSize,
                    )
                    allItems.addAll(result.items.filter { !it.isContainer })
                    hasMore = result.hasMore
                    start += pageSize
                }

                if (allItems.isEmpty()) return@launch

                val lease = leaseManager.ensureLease(rendererId)
                val entries = allItems.map { item ->
                    QueueEntry(ref = ItemRef(id = item.id))
                }

                // Queue all items.
                correlator.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(
                        QueueAddBody(
                            position = "next",
                            entries = entries,
                        )
                    ),
                    lease = lease,
                )

                // Start playback.
                correlator.send(
                    nodeId = rendererId,
                    cmdType = "playback.next",
                    body = json.encodeToJsonElement(mapOf<String, String>()),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "playContainer failed: ${e.message}")
            }
        }
    }

    // -------------------------------------------------------------------------
    // Internal
    // -------------------------------------------------------------------------

    private fun browseContainer(containerId: String) {
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
