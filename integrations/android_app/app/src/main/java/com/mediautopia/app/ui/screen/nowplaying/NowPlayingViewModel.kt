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
import com.mediautopia.app.data.repository.ActiveRendererRepository
import com.mediautopia.app.data.repository.NodeRepository
import com.mediautopia.app.data.repository.RendererStateRepository
import com.mediautopia.app.domain.usecase.CommandCorrelator
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
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.encodeToJsonElement
import javax.inject.Inject

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
)

@OptIn(ExperimentalCoroutinesApi::class)
@HiltViewModel
class NowPlayingViewModel @Inject constructor(
    private val activeRendererRepository: ActiveRendererRepository,
    private val rendererStateRepository: RendererStateRepository,
    private val nodeRepository: NodeRepository,
    private val leaseManager: LeaseManager,
    private val correlator: CommandCorrelator,
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

    // Active renderer ID flow for command targeting.
    private val activeRendererId = activeRendererRepository.activeRendererId
        .stateIn(viewModelScope, SharingStarted.Eagerly, ActiveRendererRepository.LOCAL_RENDERER_ID)

    // Observe the active renderer's state.
    private val rendererState: StateFlow<RendererState?> =
        activeRendererRepository.activeRendererId.flatMapLatest { rendererId ->
            rendererStateRepository.observeState(rendererId)
                .onStart<RendererState?> { emit(null) }
                .catch { emit(null) }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

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
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), "This Phone")

    val uiState: StateFlow<NowPlayingUiState> = combine(
        rendererState,
        rendererName,
        _interpolatedPositionMs,
    ) { state, name, interpolatedPosition ->
        if (state == null) {
            return@combine NowPlayingUiState(rendererName = name)
        }

        val metadata = state.current?.metadata
        val title = metadata?.let { com.mediautopia.app.data.protocol.MetadataUtil.run { it.title() } }
        val artist = metadata?.let { com.mediautopia.app.data.protocol.MetadataUtil.run { it.artistString() } }
        val album = metadata?.let { com.mediautopia.app.data.protocol.MetadataUtil.run { it.album() } }
        val artworkUrl = metadata?.let { com.mediautopia.app.data.protocol.MetadataUtil.run { it.artworkUrl() } }
        val format = metadata?.stringValue("format")
        val sampleRate = metadata?.stringValue("sampleRate")
        val bitDepth = metadata?.stringValue("bitDepth")

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
        )
    }.stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000),
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
                correlator.send(
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
                correlator.send(
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
                correlator.send(
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
                correlator.send(
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
                correlator.send(
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
            // Cycle: "" (off) -> "all" -> "one" -> "" (off)
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

    // -------------------------------------------------------------------------
    // Helpers
    // -------------------------------------------------------------------------

    private fun Map<String, JsonElement>.stringValue(key: String): String? {
        return (this[key] as? JsonPrimitive)?.contentOrNull
    }

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
