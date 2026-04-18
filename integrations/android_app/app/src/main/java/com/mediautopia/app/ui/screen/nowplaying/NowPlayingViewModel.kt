package com.mediautopia.app.ui.screen.nowplaying

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mediautopia.app.data.protocol.PlaybackPlayBody
import com.mediautopia.app.data.protocol.PlaybackSeekBody
import com.mediautopia.app.data.protocol.PlaybackSetVolumeBody
import com.mediautopia.app.data.protocol.QueueRepeatBody
import com.mediautopia.app.data.protocol.QueueSetShuffleBody
import com.mediautopia.app.data.protocol.RendererState
import com.mediautopia.app.data.protocol.album
import com.mediautopia.app.data.protocol.artistString
import com.mediautopia.app.data.protocol.artworkUrl
import com.mediautopia.app.data.protocol.stringValue
import com.mediautopia.app.data.protocol.title
import com.mediautopia.app.data.cache.MetadataCache
import com.mediautopia.app.data.cache.ResolvedMetadata
import com.mediautopia.app.data.repository.ActiveRendererRepository
import com.mediautopia.app.data.repository.LibraryRepository
import com.mediautopia.app.data.repository.NodeRepository
import com.mediautopia.app.data.repository.RendererStateRepository
import com.mediautopia.app.data.repository.ZoneInfo
import com.mediautopia.app.data.repository.ZoneRepository
import com.mediautopia.app.domain.usecase.LeaseManager
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onStart
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.encodeToJsonElement
import javax.inject.Inject

data class PanelZone(
    val nodeId: String,
    val name: String,
    val volume: Float,
    val isMuted: Boolean,
    val isOnline: Boolean,
    val assignedToCurrent: Boolean,
)

data class NowPlayingUiState(
    val playbackStatus: String = "stopped",
    val trackTitle: String? = null,
    val artist: String? = null,
    val album: String? = null,
    val artworkUrl: String? = null,
    val positionMs: Long = 0,
    val durationMs: Long = 0,
    val volume: Float = 1f,
    val isMuted: Boolean = false,
    val shuffle: Boolean = false,
    val repeatMode: String = "",
    val hiResInfo: String? = null,
    val rendererName: String = "This Phone",
    val isConnected: Boolean = false,
    val visualizerEnabled: Boolean = false,
    val isLocalRenderer: Boolean = true,
    val leaseOwner: String? = null,
    val isOwnLease: Boolean = false,
    /** Zones visible in the slide-up panel. */
    val panelZones: List<PanelZone> = emptyList(),
    /** Count of zones currently playing the active renderer. */
    val assignedZoneCount: Int = 0,
    /**
     * Whether the zone controller exposes a source that maps to the
     * current renderer. When false, zone assignment toggles are hidden
     * (only volume/mute remain); the renderer/source mapping is keyed
     * by matching source.id to the active renderer's nodeId.
     */
    val zoneAssignmentSupported: Boolean = false,
)

@OptIn(ExperimentalCoroutinesApi::class)
@HiltViewModel
class NowPlayingViewModel @Inject constructor(
    private val activeRendererRepository: ActiveRendererRepository,
    private val rendererStateRepository: RendererStateRepository,
    private val nodeRepository: NodeRepository,
    private val libraryRepository: LibraryRepository,
    private val metadataCache: MetadataCache,
    private val leaseManager: LeaseManager,
    private val transport: com.mediautopia.app.data.transport.TransportRouter,
    private val settingsDataStore: com.mediautopia.app.data.cache.SettingsDataStore,
    private val zoneRepository: ZoneRepository,
    val audioSessionHolder: com.mediautopia.app.renderer.AudioSessionHolder,
) : ViewModel() {

    private val tag = "NowPlayingViewModel"
    private val json = Json { ignoreUnknownKeys = true }

    // Interpolated position, updated by the 100ms ticker when playing.
    private val _interpolatedPositionMs = MutableStateFlow(0L)

    // Volume debounce job.
    private var volumeJob: Job? = null

    // Position ticker job.
    private var tickerJob: Job? = null

    // The latest server-reported position and timestamp, used for interpolation.
    private var lastServerPositionMs: Long = 0
    private var lastServerTimestamp: Long = 0
    private var currentPlaybackStatus: String = "stopped"

    // Resolved metadata for the current item (fetched from library).
    private val _resolvedMetadata = MutableStateFlow<ResolvedMetadata?>(null)
    private var lastResolvedItemId: String? = null

    // Active renderer ID flow for command targeting.
    private val activeRendererId = activeRendererRepository.activeRendererId
        .stateIn(viewModelScope, SharingStarted.Eagerly, ActiveRendererRepository.LOCAL_RENDERER_ID)

    // Observe the active renderer's state.
    private val rendererState: StateFlow<RendererState?> =
        activeRendererRepository.activeRendererId.flatMapLatest { rendererId ->
            Log.d(tag, "Active renderer changed to: $rendererId")
            // Reset resolved metadata when switching renderers.
            _resolvedMetadata.value = null
            lastResolvedItemId = null
            _interpolatedPositionMs.value = 0
            rendererStateRepository.observeState(rendererId)
                .onStart<RendererState?> { emit(null) }
                .catch { emit(null) }
        }.stateIn(viewModelScope, SharingStarted.Eagerly, null)

    init {
        // Watch for current item changes and resolve metadata from library.
        viewModelScope.launch {
            rendererState.collect { state ->
                val itemId = state?.current?.itemId
                if (itemId != null && itemId != lastResolvedItemId) {
                    lastResolvedItemId = itemId
                    resolveCurrentItemMetadata(itemId)
                } else if (itemId == null) {
                    lastResolvedItemId = null
                    _resolvedMetadata.value = null
                }
            }
        }
    }

    private fun resolveCurrentItemMetadata(itemId: String) {
        viewModelScope.launch {
            // Check cache first.
            val cached = metadataCache.get(itemId)
            if (cached != null) {
                _resolvedMetadata.value = cached
                return@launch
            }
            // Resolve from library.
            try {
                val resolved = libraryRepository.resolve(itemId)
                if (resolved != null) {
                    _resolvedMetadata.value = resolved
                }
            } catch (e: Exception) {
                Log.w(tag, "Failed to resolve metadata for $itemId: ${e.message}")
            }
        }
    }

    // Renderer name, resolved from node repository.
    private val rendererName: StateFlow<String> =
        activeRendererRepository.activeRendererId.flatMapLatest { rendererId ->
            if (rendererId == ActiveRendererRepository.LOCAL_RENDERER_ID) {
                flowOf("This Phone")
            } else {
                nodeRepository.nodes.map { nodes ->
                    nodes[rendererId]?.name ?: "Unknown Renderer"
                }
            }
        }.stateIn(viewModelScope, SharingStarted.Eagerly, "This Phone")

    val uiState: StateFlow<NowPlayingUiState> = combine(
        listOf<kotlinx.coroutines.flow.Flow<Any?>>(
            rendererState,
            rendererName,
            _interpolatedPositionMs,
            _resolvedMetadata,
            settingsDataStore.visualizerEnabled,
            activeRendererId,
            leaseManager.leaseInfos,
            zoneRepository.zones,
            nodeRepository.nodes,
        ),
    ) { values: Array<Any?> ->
        @Suppress("UNCHECKED_CAST")
        val state = values[0] as RendererState?
        val name = values[1] as String
        val interpolatedPosition = values[2] as Long
        val resolved = values[3] as ResolvedMetadata?
        val vizEnabled = values[4] as Boolean
        val activeId = values[5] as String
        @Suppress("UNCHECKED_CAST")
        val leaseInfos = values[6] as Map<String, com.mediautopia.app.domain.usecase.LeaseInfo>
        @Suppress("UNCHECKED_CAST")
        val zones = values[7] as List<ZoneInfo>
        @Suppress("UNCHECKED_CAST")
        val nodes = values[8] as Map<String, com.mediautopia.app.domain.model.Node>

        // The renderer's presence advertises a `source` id that the zone
        // controller routes audio from. A zone is "assigned to the current
        // renderer" when the zone's current sourceId matches that advertised
        // source id. Checkboxes are hidden when the active renderer has no
        // source configured (e.g. local android renderer without a zone
        // source mapping).
        val rendererSourceId = nodes[activeId]?.source ?: ""
        val zoneAssignmentSupported = rendererSourceId.isNotEmpty()
        val panelZones = zones.map { z ->
            PanelZone(
                nodeId = z.nodeId,
                name = z.name,
                volume = z.volume,
                isMuted = z.isMuted,
                isOnline = z.isOnline,
                assignedToCurrent = zoneAssignmentSupported && z.sourceId == rendererSourceId,
            )
        }
        val assignedZoneCount = panelZones.count { it.assignedToCurrent }

        if (state == null) {
            return@combine NowPlayingUiState(
                rendererName = name,
                visualizerEnabled = vizEnabled,
                panelZones = panelZones,
                assignedZoneCount = assignedZoneCount,
                zoneAssignmentSupported = zoneAssignmentSupported,
            )
        }

        // Metadata comes from resolved library data (or inline if available).
        val inlineMeta = state.current?.metadata
        val title = resolved?.title?.ifBlank { null } ?: inlineMeta?.title()
        val artist = resolved?.artist?.ifBlank { null } ?: inlineMeta?.artistString()
        val album = resolved?.album?.ifBlank { null } ?: inlineMeta?.album()
        val artworkUrl = resolved?.artworkUrl ?: inlineMeta?.artworkUrl()
        val format = resolved?.format?.ifBlank { null } ?: inlineMeta?.stringValue("format")
        val sampleRate = inlineMeta?.stringValue("sampleRate")
        val bitDepth = inlineMeta?.stringValue("bitDepth")

        val hiResInfo = buildHiResInfo(format, bitDepth, sampleRate)

        val playbackStatus = state.playback?.status ?: "stopped"
        val durationMs = state.playback?.durationMs ?: 0
        val volume = state.playback?.volume?.toFloat() ?: 1f
        val isMuted = state.playback?.mute ?: false

        val positionMs = if (playbackStatus == "playing") {
            interpolatedPosition.coerceIn(0, durationMs)
        } else {
            (state.playback?.positionMs ?: 0).coerceIn(0, durationMs)
        }

        val leaseOwner = state.session?.owner
        // We own the lease iff we have a token cached for the active
        // renderer. Comparing identity strings races with DataStore loads
        // and causes the transport-disable note to flicker; the cached
        // token is authoritative.
        val isOwnLease = leaseInfos[activeId] != null

        NowPlayingUiState(
            playbackStatus = playbackStatus,
            trackTitle = title,
            artist = artist,
            album = album,
            artworkUrl = artworkUrl,
            positionMs = positionMs,
            durationMs = durationMs,
            volume = volume,
            isMuted = isMuted,
            shuffle = state.queue?.shuffle ?: false,
            repeatMode = state.queue?.repeatMode ?: "",
            hiResInfo = hiResInfo,
            rendererName = name,
            isConnected = state.session != null,
            visualizerEnabled = vizEnabled,
            isLocalRenderer = name == "This Phone",
            leaseOwner = leaseOwner,
            isOwnLease = isOwnLease,
            panelZones = panelZones,
            assignedZoneCount = assignedZoneCount,
            zoneAssignmentSupported = zoneAssignmentSupported,
        )
    }.stateIn(
        scope = viewModelScope,
        started = SharingStarted.Eagerly,
        initialValue = NowPlayingUiState(),
    )

    init {
        // Start/stop position ticker based on playback status.
        viewModelScope.launch {
            rendererState.collect { state ->
                val status = state?.playback?.status ?: "stopped"
                val serverPosition = state?.playback?.positionMs ?: 0

                // Sync interpolation anchor.
                lastServerPositionMs = serverPosition
                lastServerTimestamp = System.currentTimeMillis()
                currentPlaybackStatus = status
                _interpolatedPositionMs.value = serverPosition

                if (status == "playing") {
                    startTicker()
                } else {
                    stopTicker()
                }
            }
        }
    }

    // -------------------------------------------------------------------------
    // Position interpolation ticker
    // -------------------------------------------------------------------------

    private fun startTicker() {
        if (tickerJob?.isActive == true) return
        tickerJob = viewModelScope.launch {
            while (isActive) {
                delay(100)
                if (currentPlaybackStatus == "playing") {
                    val elapsed = System.currentTimeMillis() - lastServerTimestamp
                    _interpolatedPositionMs.value = lastServerPositionMs + elapsed
                }
            }
        }
    }

    private fun stopTicker() {
        tickerJob?.cancel()
        tickerJob = null
    }

    // -------------------------------------------------------------------------
    // Transport commands
    // -------------------------------------------------------------------------

    fun togglePlayPause() {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                val cmdType = if (currentPlaybackStatus == "playing") {
                    "playback.pause"
                } else {
                    "playback.play"
                }
                val body = if (cmdType == "playback.play") {
                    json.encodeToJsonElement(PlaybackPlayBody())
                } else {
                    json.encodeToJsonElement(mapOf<String, String>())
                }
                transport.send(
                    nodeId = rendererId,
                    cmdType = cmdType,
                    body = body,
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "togglePlayPause failed: ${e.message}")
            }
        }
    }

    fun next() {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                transport.send(
                    nodeId = rendererId,
                    cmdType = "playback.next",
                    body = json.encodeToJsonElement(mapOf<String, String>()),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "next failed: ${e.message}")
            }
        }
    }

    fun previous() {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                transport.send(
                    nodeId = rendererId,
                    cmdType = "playback.prev",
                    body = json.encodeToJsonElement(mapOf<String, String>()),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "previous failed: ${e.message}")
            }
        }
    }

    fun seek(positionMs: Long) {
        // Update local interpolation immediately for responsiveness.
        lastServerPositionMs = positionMs
        lastServerTimestamp = System.currentTimeMillis()
        _interpolatedPositionMs.value = positionMs

        viewModelScope.launch {
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                transport.send(
                    nodeId = rendererId,
                    cmdType = "playback.seek",
                    body = json.encodeToJsonElement(PlaybackSeekBody(positionMs = positionMs)),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "seek failed: ${e.message}")
            }
        }
    }

    fun setVolume(volume: Float) {
        volumeJob?.cancel()
        volumeJob = viewModelScope.launch {
            delay(50) // 50ms debounce
            val rendererId = activeRendererId.value
            try {
                val lease = leaseManager.ensureLease(rendererId)
                transport.send(
                    nodeId = rendererId,
                    cmdType = "playback.setVolume",
                    body = json.encodeToJsonElement(
                        PlaybackSetVolumeBody(volume = volume.toDouble())
                    ),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "setVolume failed: ${e.message}")
            }
        }
    }

    fun toggleMute() {
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            val currentMute = rendererState.value?.playback?.mute ?: false
            try {
                val lease = leaseManager.ensureLease(rendererId)
                transport.send(
                    nodeId = rendererId,
                    cmdType = "playback.setMute",
                    body = json.encodeToJsonElement(
                        com.mediautopia.app.data.protocol.PlaybackSetMuteBody(mute = !currentMute)
                    ),
                    lease = lease,
                )
            } catch (e: Exception) {
                Log.e(tag, "toggleMute failed: ${e.message}")
            }
        }
    }

    fun toggleShuffle() {
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
        viewModelScope.launch {
            val rendererId = activeRendererId.value
            val currentMode = rendererState.value?.queue?.repeatMode ?: ""
            // Cycle: off -> "all" -> "one" -> off
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

    // -------------------------------------------------------------------------
    // Slide-up panel: per-zone controls
    // -------------------------------------------------------------------------

    private val zoneVolumeJobs = java.util.concurrent.ConcurrentHashMap<String, Job>()

    fun setPanelZoneVolume(zoneNodeId: String, volume: Float) {
        zoneVolumeJobs[zoneNodeId]?.cancel()
        zoneVolumeJobs[zoneNodeId] = viewModelScope.launch {
            delay(200)
            try {
                zoneRepository.setVolume(zoneNodeId, volume)
            } catch (e: Exception) {
                Log.e(tag, "setPanelZoneVolume failed for $zoneNodeId: ${e.message}")
            }
        }
    }

    fun togglePanelZoneMute(zoneNodeId: String) {
        val z = uiState.value.panelZones.find { it.nodeId == zoneNodeId } ?: return
        viewModelScope.launch {
            try {
                zoneRepository.setMute(zoneNodeId, !z.isMuted)
            } catch (e: Exception) {
                Log.e(tag, "togglePanelZoneMute failed for $zoneNodeId: ${e.message}")
            }
        }
    }

    /**
     * Enable/disable this zone for the active renderer. The renderer
     * advertises a `source` id in its presence; matching a zone's sourceId
     * to that value routes the zone's audio from this renderer.
     *
     * If the active renderer has no source id configured (e.g. the local
     * android renderer without a zone source mapping), the UI hides the
     * checkbox and this call is a no-op.
     */
    fun togglePanelZoneAssignment(zoneNodeId: String, assign: Boolean) {
        val rendererId = activeRendererId.value
        val rendererSource = nodeRepository.nodes.value[rendererId]?.source ?: ""
        if (rendererSource.isEmpty()) return
        viewModelScope.launch {
            try {
                // Assigning: point the zone at our renderer's advertised
                // source. Unassigning: clear the zone's source — the server
                // interprets an empty sourceId as "no source selected".
                val target = if (assign) rendererSource else ""
                zoneRepository.selectSource(zoneNodeId, target)
            } catch (e: Exception) {
                Log.e(tag, "togglePanelZoneAssignment failed for $zoneNodeId: ${e.message}")
            }
        }
    }

    // -------------------------------------------------------------------------
    // Helpers
    // -------------------------------------------------------------------------

    private fun buildHiResInfo(
        format: String?,
        bitDepth: String?,
        sampleRate: String?,
    ): String? {
        if (bitDepth == null && sampleRate == null && format == null) return null

        val parts = mutableListOf<String>()
        format?.let { parts.add(it.uppercase()) }
        if (bitDepth != null) parts.add("${bitDepth}-BIT")
        if (sampleRate != null) parts.add("/ ${formatSampleRate(sampleRate)}")

        return if (parts.isNotEmpty()) parts.joinToString(" ") else null
    }

    companion object {
        private fun formatSampleRate(rate: String): String {
            val numeric = rate.replace("[^0-9]".toRegex(), "")
            val value = numeric.toLongOrNull() ?: return rate.uppercase()
            return if (value >= 1000) "${value / 1000}KHZ" else "${value}HZ"
        }
    }
}
