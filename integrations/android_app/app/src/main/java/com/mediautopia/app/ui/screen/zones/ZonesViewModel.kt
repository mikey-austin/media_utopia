package com.mediautopia.app.ui.screen.zones

import android.util.Log
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mediautopia.app.data.repository.ZoneRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject

data class ZoneUiItem(
    val nodeId: String,
    val name: String,
    val source: String,
    val volume: Float,
    val isMuted: Boolean,
    val isOnline: Boolean,
)

data class ZonesUiState(
    val masterVolume: Float = 0.84f,
    val zones: List<ZoneUiItem> = emptyList(),
    val activeCount: Int = 0,
)

@HiltViewModel
class ZonesViewModel @Inject constructor(
    private val zoneRepository: ZoneRepository,
) : ViewModel() {

    private val tag = "ZonesViewModel"

    /** Per-zone volume debounce jobs, keyed by nodeId. */
    private val volumeJobs = ConcurrentHashMap<String, Job>()

    /** Master volume is tracked locally as UI state. */
    private val _masterVolume = MutableStateFlow(0.84f)

    val uiState: StateFlow<ZonesUiState> = combine(
        zoneRepository.zones,
        _masterVolume,
    ) { zones, masterVol ->
        val items = zones.map { zone ->
            ZoneUiItem(
                nodeId = zone.nodeId,
                name = zone.name,
                source = zone.source,
                volume = zone.volume,
                isMuted = zone.isMuted,
                isOnline = zone.isOnline,
            )
        }

        ZonesUiState(
            masterVolume = masterVol,
            zones = items,
            activeCount = items.count { it.isOnline },
        )
    }.stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000),
        initialValue = ZonesUiState(),
    )

    // -------------------------------------------------------------------------
    // Actions
    // -------------------------------------------------------------------------

    /**
     * Set volume on a single zone. Debounced 200ms per zone so rapid slider
     * movements don't flood the MQTT bus.
     */
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

    /**
     * Toggle mute state on a zone.
     */
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

    /**
     * Set master volume. Currently tracked as UI-only state. A full
     * implementation would scale all zone volumes proportionally.
     */
    fun setMasterVolume(volume: Float) {
        _masterVolume.value = volume.coerceIn(0f, 1f)
    }
}
