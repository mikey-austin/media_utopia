package com.mediautopia.app.ui.screen.library

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mediautopia.app.data.protocol.ItemRef
import com.mediautopia.app.data.protocol.PlaybackPlayBody
import com.mediautopia.app.data.protocol.QueueAddBody
import com.mediautopia.app.data.protocol.QueueEntry
import com.mediautopia.app.data.protocol.QueueSetBody
import com.mediautopia.app.data.protocol.ResolvedSource
import com.mediautopia.app.data.repository.ActiveRendererRepository
import com.mediautopia.app.data.repository.BrowseItem
import com.mediautopia.app.data.repository.LibraryRepository
import com.mediautopia.app.data.repository.NodeRepository
import com.mediautopia.app.data.repository.PlaylistEntryInfo
import com.mediautopia.app.data.repository.PlaylistInfo
import com.mediautopia.app.data.repository.PlaylistRepository
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
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.put
import javax.inject.Inject

enum class LibraryTab { LIBRARIES, PLAYLISTS }

data class LibraryUiState(
    val activeTab: LibraryTab = LibraryTab.LIBRARIES,
    val breadcrumbs: List<BreadcrumbItem> = listOf(BreadcrumbItem("", "Library")),
    val items: List<BrowseItem> = emptyList(),
    val searchQuery: String = "",
    val isSearching: Boolean = false,
    val isLoading: Boolean = true,
    val hasMore: Boolean = false,
    val error: String? = null,
    // Playlist tab state.
    val playlists: List<PlaylistInfo> = emptyList(),
    val playlistEntries: List<PlaylistEntryInfo> = emptyList(),
    val selectedPlaylist: PlaylistInfo? = null,
    val selectedPlaylistServer: String? = null,
    val playlistServers: List<Pair<String, String>> = emptyList(), // nodeId to name
    val isPlaylistLoading: Boolean = false,
)

data class BreadcrumbItem(
    val containerId: String,
    val label: String,
)

@OptIn(FlowPreview::class)
@HiltViewModel
class LibraryViewModel @Inject constructor(
    private val libraryRepository: LibraryRepository,
    private val playlistRepository: PlaylistRepository,
    private val nodeRepository: NodeRepository,
    private val leaseManager: LeaseManager,
    private val transport: com.mediautopia.app.data.transport.TransportRouter,
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

    private var browseJob: Job? = null
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
        if (index == 0) {
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

        // Index 1 is the library root — browse with empty containerId.
        if (index == 1 && target.containerId.startsWith("__lib__:")) {
            val libId = selectedLibraryNodeId
            if (libId != null) {
                browseContainerOnLibrary(libId, "")
                return
            }
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

    private fun buildMetadataJson(itemId: String): JsonObject? {
        val cached = libraryRepository.getCachedMetadata(itemId)
        if (cached != null) {
            return buildJsonObject {
                if (cached.title.isNotEmpty()) put("title", cached.title)
                if (cached.artist.isNotEmpty()) put("artist", cached.artist)
                if (cached.album.isNotEmpty()) put("album", cached.album)
                if (cached.artworkUrl != null) put("artworkUrl", cached.artworkUrl)
                if (cached.durationMs > 0) put("durationMs", cached.durationMs)
                if (cached.format.isNotEmpty()) put("format", cached.format)
            }
        }

        // Fall back to BrowseItem data from current UI state.
        val browseItem = _uiState.value.items.find { it.id == itemId } ?: return null
        return buildJsonObject {
            if (browseItem.title.isNotEmpty()) put("title", browseItem.title)
            val artist = browseItem.metadata["artist"] as? String
            if (!artist.isNullOrEmpty()) put("artist", artist)
            val album = browseItem.metadata["album"] as? String
            if (!album.isNullOrEmpty()) put("album", album)
            if (browseItem.artworkUrl != null) put("artworkUrl", browseItem.artworkUrl)
            val durationMs = browseItem.metadata["durationMs"] as? Long ?: 0L
            if (durationMs > 0) put("durationMs", durationMs)
        }
    }

    private suspend fun buildQueueEntry(itemId: String): QueueEntry {
        Log.d(tag, "buildQueueEntry: itemId=$itemId, libraryNodeId=$selectedLibraryNodeId")
        val source = libraryRepository.resolveForPlayback(itemId, selectedLibraryNodeId)
        Log.d(tag, "buildQueueEntry: resolved=${source != null}, url=${source?.url?.take(80)}")
        val meta = buildMetadataJson(itemId)
        Log.d(tag, "buildQueueEntry: metadata keys=${meta?.keys}")
        return if (source != null) {
            QueueEntry(
                ref = ItemRef(id = itemId),
                resolved = ResolvedSource(
                    itemId = itemId,
                    url = source.url,
                    mime = source.mime,
                ),
                metadata = meta,
            )
        } else {
            QueueEntry(ref = ItemRef(id = itemId), metadata = meta)
        }
    }

    private suspend fun buildQueueEntries(itemIds: List<String>): List<QueueEntry> {
        val sources = libraryRepository.resolveForPlaybackBatch(itemIds, selectedLibraryNodeId)
        return itemIds.map { itemId ->
            val source = sources[itemId]
            val meta = buildMetadataJson(itemId)
            if (source != null) {
                QueueEntry(
                    ref = ItemRef(id = itemId),
                    resolved = ResolvedSource(
                        itemId = itemId,
                        url = source.url,
                        mime = source.mime,
                    ),
                    metadata = meta,
                )
            } else {
                QueueEntry(ref = ItemRef(id = itemId), metadata = meta)
            }
        }
    }

    fun playItem(itemId: String) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            Log.d(tag, "playItem: itemId=$itemId, renderer=$rendererId")
            try {
                val entry = buildQueueEntry(itemId)
                Log.d(tag, "playItem: entry built, hasResolved=${entry.resolved != null}")
                val lease = leaseManager.ensureLease(rendererId)
                Log.d(tag, "playItem: lease acquired, session=${lease.sessionId}")

                transport.send(
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

                transport.send(
                    nodeId = rendererId,
                    cmdType = "playback.next",
                    body = json.encodeToJsonElement(mapOf<String, String>()),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "playItem failed: ${e.message}", e)
                snackbarManager.show("Play failed: ${e.message}")
            }
        }
    }

    fun enqueueAndPlay(itemId: String) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val entry = buildQueueEntry(itemId)
                val lease = leaseManager.ensureLease(rendererId)

                // Add to end of queue, then jump to and play the last item.
                transport.send(
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

                transport.send(
                    nodeId = rendererId,
                    cmdType = "playback.play",
                    body = json.encodeToJsonElement(
                        PlaybackPlayBody(index = -1)
                    ),
                    lease = lease,
                )
                snackbarManager.show("Playing")
            } catch (e: Exception) {
                Log.e(tag, "enqueueAndPlay failed: ${e.message}", e)
            }
        }
    }

    fun addToQueue(itemId: String) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            Log.d(tag, "addToQueue: itemId=$itemId, renderer=$rendererId")
            try {
                val entry = buildQueueEntry(itemId)
                val lease = leaseManager.ensureLease(rendererId)

                transport.send(
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
                Log.e(tag, "addToQueue failed: ${e.message}", e)
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

                transport.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(
                        QueueAddBody(position = "next", entries = entries)
                    ),
                    lease = lease,
                )

                transport.send(
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

                transport.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(
                        QueueAddBody(position = "next", entries = entries)
                    ),
                    lease = lease,
                )

                transport.send(
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

                transport.send(
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
        browseJob?.cancel()
        browseJob = viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true, items = emptyList(), error = null) }
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
        browseJob?.cancel()
        browseJob = viewModelScope.launch {
            _uiState.update { it.copy(isLoading = true, items = emptyList(), error = null) }

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

    // -------------------------------------------------------------------------
    // Tab switching
    // -------------------------------------------------------------------------

    fun switchTab(tab: LibraryTab) {
        _uiState.update { it.copy(activeTab = tab) }
        if (tab == LibraryTab.PLAYLISTS) {
            loadPlaylistServers()
        }
    }

    // -------------------------------------------------------------------------
    // Playlists
    // -------------------------------------------------------------------------

    private var playlistJob: Job? = null

    private fun loadPlaylistServers() {
        playlistJob?.cancel()
        playlistJob = viewModelScope.launch {
            _uiState.update { it.copy(isPlaylistLoading = true) }
            val servers = nodeRepository.playlistServers.first()
            val serverPairs = servers.map { it.nodeId to it.name }

            if (servers.size == 1) {
                // Single server — load its playlists directly.
                _uiState.update { it.copy(
                    playlistServers = serverPairs,
                    selectedPlaylistServer = servers.first().nodeId,
                ) }
                loadPlaylists(servers.first().nodeId)
            } else {
                // Multiple or zero — show server picker.
                _uiState.update { it.copy(
                    playlistServers = serverPairs,
                    selectedPlaylistServer = null,
                    playlists = emptyList(),
                    playlistEntries = emptyList(),
                    selectedPlaylist = null,
                    isPlaylistLoading = false,
                ) }
            }
        }
    }

    fun selectPlaylistServer(serverNodeId: String) {
        _uiState.update { it.copy(selectedPlaylistServer = serverNodeId, selectedPlaylist = null, playlistEntries = emptyList()) }
        loadPlaylists(serverNodeId)
    }

    private fun loadPlaylists(serverNodeId: String) {
        playlistJob?.cancel()
        playlistJob = viewModelScope.launch {
            _uiState.update { it.copy(isPlaylistLoading = true, playlists = emptyList()) }
            try {
                val playlists = playlistRepository.listPlaylists(serverNodeId)
                _uiState.update { it.copy(playlists = playlists, isPlaylistLoading = false) }
            } catch (e: Exception) {
                Log.e(tag, "loadPlaylists failed: ${e.message}")
                _uiState.update { it.copy(isPlaylistLoading = false) }
            }
        }
    }

    fun selectPlaylist(playlist: PlaylistInfo) {
        _uiState.update { it.copy(selectedPlaylist = playlist, playlistEntries = emptyList()) }
        playlistJob?.cancel()
        playlistJob = viewModelScope.launch {
            _uiState.update { it.copy(isPlaylistLoading = true) }
            val serverNodeId = _uiState.value.selectedPlaylistServer ?: return@launch
            try {
                val entries = playlistRepository.getPlaylist(serverNodeId, playlist.playlistId)
                _uiState.update { it.copy(playlistEntries = entries, isPlaylistLoading = false) }
            } catch (e: Exception) {
                Log.e(tag, "selectPlaylist failed: ${e.message}")
                _uiState.update { it.copy(isPlaylistLoading = false) }
            }
        }
    }

    fun playlistBack() {
        val state = _uiState.value
        when {
            state.selectedPlaylist != null -> {
                _uiState.update { it.copy(selectedPlaylist = null, playlistEntries = emptyList()) }
            }
            state.selectedPlaylistServer != null && state.playlistServers.size > 1 -> {
                _uiState.update { it.copy(selectedPlaylistServer = null, playlists = emptyList()) }
            }
        }
    }

    /**
     * Load entire playlist: resolve all entries and add to queue.
     */
    fun loadPlaylist(playlistId: String, mode: String = "replace") {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            val entries = _uiState.value.playlistEntries
            if (entries.isEmpty()) return@launch

            try {
                val queueEntries = resolvePlaylistEntries(entries)
                if (queueEntries.isEmpty()) {
                    snackbarManager.show("No playable items")
                    return@launch
                }

                val lease = leaseManager.ensureLease(rendererId)

                if (mode == "replace") {
                    // Clear queue, set entries, then play from the start.
                    transport.send(
                        nodeId = rendererId,
                        cmdType = "queue.set",
                        body = json.encodeToJsonElement(
                            QueueSetBody(
                                startIndex = 0,
                                entries = queueEntries,
                            )
                        ),
                        lease = lease,
                    )
                    transport.send(
                        nodeId = rendererId,
                        cmdType = "playback.play",
                        body = json.encodeToJsonElement(PlaybackPlayBody(index = 0)),
                        lease = lease,
                    )
                    snackbarManager.show("Playing ${queueEntries.size} items")
                } else {
                    // Append to existing queue.
                    transport.send(
                        nodeId = rendererId,
                        cmdType = "queue.add",
                        body = json.encodeToJsonElement(
                            QueueAddBody(position = "end", entries = queueEntries)
                        ),
                        lease = lease,
                    )
                    snackbarManager.show("${queueEntries.size} items added to queue")
                }
            } catch (e: Exception) {
                Log.e(tag, "loadPlaylist failed: ${e.message}", e)
                snackbarManager.show("Failed: ${e.message}")
            }
        }
    }

    /**
     * Enqueue and play a single playlist entry.
     */
    fun playPlaylistEntry(entry: PlaylistEntryInfo) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val queueEntry = resolvePlaylistEntry(entry) ?: run {
                    snackbarManager.show("Could not resolve item")
                    return@launch
                }

                val lease = leaseManager.ensureLease(rendererId)
                transport.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(QueueAddBody(position = "end", entries = listOf(queueEntry))),
                    lease = lease,
                )
                transport.send(
                    nodeId = rendererId,
                    cmdType = "playback.play",
                    body = json.encodeToJsonElement(PlaybackPlayBody(index = -1)),
                    lease = lease,
                )
                snackbarManager.show("Playing")
            } catch (e: Exception) {
                Log.e(tag, "playPlaylistEntry failed: ${e.message}", e)
            }
        }
    }

    /**
     * Add a single playlist entry to the queue.
     */
    fun queuePlaylistEntry(entry: PlaylistEntryInfo) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val queueEntry = resolvePlaylistEntry(entry) ?: run {
                    snackbarManager.show("Could not resolve item")
                    return@launch
                }

                val lease = leaseManager.ensureLease(rendererId)
                transport.send(
                    nodeId = rendererId,
                    cmdType = "queue.add",
                    body = json.encodeToJsonElement(QueueAddBody(position = "end", entries = listOf(queueEntry))),
                    lease = lease,
                )
                snackbarManager.show("Added to queue")
            } catch (e: Exception) {
                Log.e(tag, "queuePlaylistEntry failed: ${e.message}", e)
            }
        }
    }

    private fun buildPlaylistEntryMetadata(entry: PlaylistEntryInfo) = buildJsonObject {
        if (entry.title.isNotEmpty()) put("title", entry.title)
        if (entry.artist.isNotEmpty()) put("artist", entry.artist)
        if (entry.album.isNotEmpty()) put("album", entry.album)
        if (entry.artworkUrl != null) put("artworkUrl", entry.artworkUrl!!)
        if (entry.durationMs > 0) put("durationMs", entry.durationMs)
    }

    /**
     * Resolve a single playlist entry to a playable QueueEntry with URL.
     */
    private suspend fun resolvePlaylistEntry(entry: PlaylistEntryInfo): QueueEntry? {
        val meta = buildPlaylistEntryMetadata(entry)

        // If already resolved with a URL, use it directly.
        if (entry.url.isNotEmpty()) {
            return QueueEntry(
                ref = if (entry.itemId.isNotEmpty()) ItemRef(id = entry.itemId) else null,
                resolved = ResolvedSource(itemId = entry.itemId, url = entry.url, mime = entry.mime),
                metadata = meta,
            )
        }

        // Resolve from library.
        if (entry.itemId.isNotEmpty()) {
            val source = libraryRepository.resolveForPlayback(entry.itemId)
            if (source != null) {
                return QueueEntry(
                    ref = ItemRef(id = entry.itemId),
                    resolved = ResolvedSource(itemId = entry.itemId, url = source.url, mime = source.mime),
                    metadata = meta,
                )
            }
        }
        return null
    }

    /**
     * Resolve a batch of playlist entries to playable QueueEntries.
     */
    private suspend fun resolvePlaylistEntries(entries: List<PlaylistEntryInfo>): List<QueueEntry> {
        // Separate already-resolved from needs-resolution.
        val result = mutableListOf<QueueEntry>()
        val needsResolve = mutableListOf<PlaylistEntryInfo>()

        for (entry in entries) {
            if (entry.url.isNotEmpty()) {
                result.add(QueueEntry(
                    ref = if (entry.itemId.isNotEmpty()) ItemRef(id = entry.itemId) else null,
                    resolved = ResolvedSource(itemId = entry.itemId, url = entry.url, mime = entry.mime),
                    metadata = buildPlaylistEntryMetadata(entry),
                ))
            } else if (entry.itemId.isNotEmpty()) {
                needsResolve.add(entry)
            }
        }

        if (needsResolve.isNotEmpty()) {
            val sources = libraryRepository.resolveForPlaybackBatch(needsResolve.map { it.itemId })
            for (entry in needsResolve) {
                val source = sources[entry.itemId]
                if (source != null) {
                    result.add(QueueEntry(
                        ref = ItemRef(id = entry.itemId),
                        resolved = ResolvedSource(itemId = entry.itemId, url = source.url, mime = source.mime),
                        metadata = buildPlaylistEntryMetadata(entry),
                    ))
                }
            }
        }

        return result
    }
}
