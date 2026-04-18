package com.mediautopia.app.ui.screen.library

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mediautopia.app.data.protocol.DisplayMetadata
import com.mediautopia.app.data.protocol.LibraryItemRef
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
import kotlinx.serialization.json.encodeToJsonElement
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
    val playlists: List<PlaylistInfo> = emptyList(),
    val playlistEntries: List<PlaylistEntryInfo> = emptyList(),
    val selectedPlaylist: PlaylistInfo? = null,
    val selectedPlaylistServer: String? = null,
    val playlistServers: List<Pair<String, String>> = emptyList(),
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

    private val searchInput = MutableSharedFlow<String>(extraBufferCapacity = 1)

    private var browseJob: Job? = null
    private var loadMoreJob: Job? = null

    private var selectedLibraryNodeId: String? = null

    init {
        loadLibraryList()

        viewModelScope.launch {
            searchInput
                .debounce(300)
                .collectLatest { query ->
                    if (query.isBlank()) {
                        val currentContainer = _uiState.value.breadcrumbs.lastOrNull()?.containerId ?: ""
                        _uiState.update { it.copy(isSearching = false, searchQuery = "") }
                        browseContainer(currentContainer)
                    } else {
                        performSearch(query)
                    }
                }
        }
    }

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

    fun navigateTo(containerId: String, label: String) {
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

        if (index == 1 && target.containerId.startsWith("__lib__:")) {
            val libId = selectedLibraryNodeId
            if (libId != null) {
                browseContainerOnLibrary(libId, "")
                return
            }
        }
        browseContainer(target.containerId)
    }

    fun search(query: String) {
        _uiState.update { it.copy(searchQuery = query) }
        searchInput.tryEmit(query)
    }

    fun clearSearch() {
        _uiState.update { it.copy(searchQuery = "", isSearching = false) }
        val currentContainer = _uiState.value.breadcrumbs.lastOrNull()?.containerId ?: ""
        browseContainer(currentContainer)
    }

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

    private fun findItem(itemId: String): BrowseItem? =
        _uiState.value.items.find { it.id == itemId }

    private fun makeRef(itemId: String): LibraryItemRef? {
        val cached = findItem(itemId)?.ref
        if (cached != null) return cached
        val libId = selectedLibraryNodeId ?: return null
        return LibraryItemRef(libraryId = libId, itemId = itemId)
    }

    private suspend fun buildQueueEntry(itemId: String): QueueEntry {
        val ref = makeRef(itemId) ?: error("no library selected for $itemId")
        Log.d(tag, "buildQueueEntry: ref=${ref.libraryId}/${ref.itemId}")
        val source = libraryRepository.resolveSources(ref)
        val display = displayFor(itemId, ref) ?: libraryRepository.getItem(ref)
        return QueueEntry(
            ref = ref,
            resolved = source,
            display = display,
        )
    }

    private suspend fun buildQueueEntries(itemIds: List<String>): List<QueueEntry> {
        val pairs = itemIds.mapNotNull { id -> makeRef(id)?.let { id to it } }
        if (pairs.isEmpty()) return emptyList()
        val refs = pairs.map { it.second }
        val sources = libraryRepository.resolveSourcesBatch(refs)
        // Single batched fallback for refs whose display isn't already cached
        // or attached to the visible browse item.
        val missingDisplay = pairs
            .filter { (id, ref) -> displayFor(id, ref) == null }
            .map { it.second }
        val fetched: Map<LibraryItemRef, DisplayMetadata> =
            if (missingDisplay.isNotEmpty()) libraryRepository.getItems(missingDisplay)
            else emptyMap()
        return pairs.map { (id, ref) ->
            QueueEntry(
                ref = ref,
                resolved = sources[ref],
                display = displayFor(id, ref) ?: fetched[ref],
            )
        }
    }

    private fun displayFor(itemId: String, ref: LibraryItemRef): DisplayMetadata? {
        val cached = libraryRepository.getCachedDisplay(ref)
        if (cached != null) return cached
        val item = findItem(itemId)
        return item?.display
    }

    fun playItem(itemId: String) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            Log.d(tag, "playItem: itemId=$itemId, renderer=$rendererId")
            try {
                val entry = buildQueueEntry(itemId)
                val lease = leaseManager.ensureLease(rendererId)

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

                // Insert directly after the current entry and advance, which
                // works without knowing the resulting queue length. Adding at
                // "end" and playing index=-1 fails because the renderer's
                // playback.play has no "play last" semantic — Jump rejects
                // negative indices outright.
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
                snackbarManager.show("Playing")
            } catch (e: Exception) {
                Log.e(tag, "enqueueAndPlay failed: ${e.message}", e)
            }
        }
    }

    fun addToQueue(itemId: String) {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
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
        // Walk the container's children. Don't write the discovered tracks
        // into _uiState.items — that's the visible parent listing, and
        // appending the container's tracks to it would mash the folder's
        // contents into the parent screen. buildQueueEntries already falls
        // back to library.getItems for refs that aren't in the visible list.
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
            for (item in result.items) {
                if (!item.isContainer) {
                    allItemIds.add(item.id)
                }
            }
            hasMore = result.hasMore
            start += pageSize
        }
        return allItemIds
    }

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

    fun switchTab(tab: LibraryTab) {
        _uiState.update { it.copy(activeTab = tab) }
        if (tab == LibraryTab.PLAYLISTS) {
            loadPlaylistServers()
        }
    }

    private var playlistJob: Job? = null

    private fun loadPlaylistServers() {
        playlistJob?.cancel()
        playlistJob = viewModelScope.launch {
            _uiState.update { it.copy(isPlaylistLoading = true) }
            val servers = nodeRepository.playlistServers.first()
            val serverPairs = servers.map { it.nodeId to it.name }

            if (servers.size == 1) {
                _uiState.update { it.copy(
                    playlistServers = serverPairs,
                    selectedPlaylistServer = servers.first().nodeId,
                ) }
                loadPlaylists(servers.first().nodeId)
            } else {
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

    private fun playlistEntryDisplay(entry: PlaylistEntryInfo): DisplayMetadata? {
        if (entry.title.isEmpty() && entry.artist.isEmpty() && entry.album.isEmpty()
            && entry.artworkUrl == null && entry.durationMs <= 0) return null
        return DisplayMetadata(
            title = entry.title.ifEmpty { null },
            artist = entry.artist.ifEmpty { null },
            album = entry.album.ifEmpty { null },
            artworkUrl = entry.artworkUrl,
            durationMs = entry.durationMs.takeIf { it > 0 },
        )
    }

    private suspend fun resolvePlaylistEntry(entry: PlaylistEntryInfo): QueueEntry? {
        val display = playlistEntryDisplay(entry)
            ?: entry.ref?.let { libraryRepository.getItem(it) }
        if (entry.url.isNotEmpty()) {
            return QueueEntry(
                ref = entry.ref,
                resolved = ResolvedSource(url = entry.url, mime = entry.mime),
                display = display,
            )
        }
        val ref = entry.ref ?: return null
        val source = libraryRepository.resolveSources(ref) ?: return null
        return QueueEntry(
            ref = ref,
            resolved = source,
            display = display,
        )
    }

    private suspend fun resolvePlaylistEntries(entries: List<PlaylistEntryInfo>): List<QueueEntry> {
        // Backfill display once for every ref-bearing entry that arrived without
        // one, batched per library. PlaylistRepository.getPlaylist already does
        // this on the listing path but `loadPlaylist` is also reachable from
        // contexts (snapshots, deep links, search) that might bypass the listing.
        val refsNeedingDisplay = entries
            .mapNotNull { it.ref }
            .filter { ref -> entries.first { it.ref == ref }.let(::playlistEntryDisplay) == null }
            .distinct()
        val fetched: Map<LibraryItemRef, DisplayMetadata> =
            if (refsNeedingDisplay.isNotEmpty()) libraryRepository.getItems(refsNeedingDisplay)
            else emptyMap()

        val result = mutableListOf<QueueEntry>()
        val needsResolve = mutableListOf<PlaylistEntryInfo>()

        for (entry in entries) {
            val display = playlistEntryDisplay(entry) ?: entry.ref?.let { fetched[it] }
            if (entry.url.isNotEmpty()) {
                result.add(QueueEntry(
                    ref = entry.ref,
                    resolved = ResolvedSource(url = entry.url, mime = entry.mime),
                    display = display,
                ))
            } else if (entry.ref != null) {
                needsResolve.add(entry)
            }
        }

        if (needsResolve.isNotEmpty()) {
            val refs = needsResolve.mapNotNull { it.ref }
            val sources = libraryRepository.resolveSourcesBatch(refs)
            for (entry in needsResolve) {
                val ref = entry.ref ?: continue
                val source = sources[ref] ?: continue
                result.add(QueueEntry(
                    ref = ref,
                    resolved = source,
                    display = playlistEntryDisplay(entry) ?: fetched[ref],
                ))
            }
        }

        return result
    }
}
