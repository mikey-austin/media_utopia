package com.mediautopia.app.ui.screen.zones

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mediautopia.app.data.protocol.PlaybackSetVolumeBody
import com.mediautopia.app.data.repository.ActiveRendererRepository
import com.mediautopia.app.data.repository.RendererStateRepository
import com.mediautopia.app.data.repository.ZoneRepository
import com.mediautopia.app.data.repository.ZoneSource
import com.mediautopia.app.domain.usecase.CommandCorrelator
import com.mediautopia.app.domain.usecase.LeaseManager
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.encodeToJsonElement
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject

data class ZoneUiItem(
    val nodeId: String,
    val name: String,
    val source: String,
    val sourceId: String = "",
    val volume: Float,
    val isMuted: Boolean,
    val isOnline: Boolean,
)

enum class ZonesTab { ZONES, SOURCES }

data class ZonesUiState(
    val activeTab: ZonesTab = ZonesTab.ZONES,
    val localVolume: Float = 0.5f,
    val zones: List<ZoneUiItem> = emptyList(),
    val activeCount: Int = 0,
    val availableSources: List<ZoneSource> = emptyList(),
)

@HiltViewModel
class ZonesViewModel @Inject constructor(
    private val zoneRepository: ZoneRepository,
    private val activeRendererRepository: ActiveRendererRepository,
    private val rendererStateRepository: RendererStateRepository,
    private val leaseManager: LeaseManager,
    private val correlator: CommandCorrelator,
) : ViewModel() {

    private val tag = "ZonesViewModel"
    private val json = Json { ignoreUnknownKeys = true }

    private val volumeJobs = ConcurrentHashMap<String, Job>()
    private var localVolumeJob: Job? = null

    private val activeRendererId = activeRendererRepository.activeRendererId
        .stateIn(viewModelScope, SharingStarted.Eagerly, ActiveRendererRepository.LOCAL_RENDERER_ID)

    private val _activeTab = MutableStateFlow(ZonesTab.ZONES)
    private val _localVolume = MutableStateFlow(0.5f)

    init {
        // Observe active renderer state for volume.
        viewModelScope.launch {
            activeRendererRepository.activeRendererId.collect { rendererId ->
                launch {
                    rendererStateRepository.observeState(rendererId).collect { state ->
                        val vol = state.playback?.volume?.toFloat() ?: 0.5f
                        _localVolume.value = vol
                    }
                }
            }
        }
    }

    val uiState: StateFlow<ZonesUiState> = combine(
        zoneRepository.zones,
        _localVolume,
        _activeTab,
    ) { zones, localVol, tab ->
        val items = zones.map { zone ->
            ZoneUiItem(
                nodeId = zone.nodeId,
                name = zone.name,
                source = zone.source,
                sourceId = zone.sourceId,
                volume = zone.volume,
                isMuted = zone.isMuted,
                isOnline = zone.isOnline,
            )
        }

        ZonesUiState(
            activeTab = tab,
            localVolume = localVol,
            zones = items,
            activeCount = items.count { it.isOnline },
            availableSources = zoneRepository.getAvailableSources(),
        )
    }.stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000),
        initialValue = ZonesUiState(),
    )

    fun setZoneVolume(nodeId: String, volume: Float) {
        volumeJobs[nodeId]?.cancel()
        volumeJobs[nodeId] = viewModelScope.launch {
            delay(200)
            try {
                zoneRepository.setVolume(nodeId, volume)
            } catch (e: Exception) {
                Log.e(tag, "setZoneVolume failed for $nodeId: ${e.message}")
            }
        }
    }

    fun toggleZoneMute(nodeId: String) {
        val currentZone = uiState.value.zones.find { it.nodeId == nodeId } ?: return
        viewModelScope.launch {
            try {
                zoneRepository.setMute(nodeId, !currentZone.isMuted)
            } catch (e: Exception) {
                Log.e(tag, "toggleZoneMute failed for $nodeId: ${e.message}")
            }
        }
    }

    fun setLocalVolume(volume: Float) {
        _localVolume.value = volume.coerceIn(0f, 1f)
        localVolumeJob?.cancel()
        localVolumeJob = viewModelScope.launch {
            delay(50)
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
                Log.e(tag, "setLocalVolume failed: ${e.message}")
            }
        }
    }

    fun selectSource(nodeId: String, sourceId: String) {
        viewModelScope.launch {
            try {
                zoneRepository.selectSource(nodeId, sourceId)
            } catch (e: Exception) {
                Log.e(tag, "selectSource failed for $nodeId: ${e.message}")
            }
        }
    }

    fun switchTab(tab: ZonesTab) {
        _activeTab.value = tab
    }
}
