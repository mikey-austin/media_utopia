package com.mediautopia.app.ui.screen.renderers

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import android.util.Log
import com.mediautopia.app.data.cache.MetadataCache
import com.mediautopia.app.data.cache.ResolvedMetadata
import com.mediautopia.app.data.mqtt.ConnectionState
import com.mediautopia.app.data.mqtt.MqttConnectionManager
import com.mediautopia.app.data.protocol.RendererState
import com.mediautopia.app.data.protocol.artistString
import com.mediautopia.app.data.protocol.title
import com.mediautopia.app.data.cache.SettingsDataStore
import com.mediautopia.app.data.repository.ActiveRendererRepository
import com.mediautopia.app.data.repository.LibraryRepository
import com.mediautopia.app.data.repository.NodeRepository
import com.mediautopia.app.data.repository.RendererStateRepository
import com.mediautopia.app.domain.model.Node
import com.mediautopia.app.domain.usecase.LeaseManager
import com.mediautopia.app.service.MqttForegroundService
import com.mediautopia.app.ui.SnackbarManager
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject

data class RendererItem(
    val nodeId: String,
    val name: String,
    val isLocal: Boolean = false,
    val isActive: Boolean = false,
    val status: String = "",
    val formatBadge: String? = null,
    val currentTrack: String? = null,
    val leaseOwner: String? = null,
    val isOwnLease: Boolean = false,
    /** Unix millis at which our cached lease for this renderer expires, or null. */
    val leaseExpiresAtMs: Long? = null,
)

data class RenderersUiState(
    val renderers: List<RendererItem> = emptyList(),
    val activeRendererId: String = ActiveRendererRepository.LOCAL_RENDERER_ID,
    val isScanning: Boolean = true,
    val connectionState: ConnectionState = ConnectionState.DISCONNECTED,
)

@HiltViewModel
class RenderersViewModel @Inject constructor(
    @ApplicationContext private val appContext: Context,
    private val nodeRepository: NodeRepository,
    private val activeRendererRepository: ActiveRendererRepository,
    private val rendererStateRepository: RendererStateRepository,
    private val mqttConnectionManager: MqttConnectionManager,
    private val libraryRepository: LibraryRepository,
    private val metadataCache: MetadataCache,
    private val leaseManager: LeaseManager,
    private val snackbarManager: SnackbarManager,
    private val settingsDataStore: SettingsDataStore,
) : ViewModel() {
    private val tag = "RenderersViewModel"

    private val appIdentity = settingsDataStore.identity
        .stateIn(viewModelScope, SharingStarted.Eagerly, "")

    // Per-renderer state observations, keyed by nodeId.
    private val rendererStates = ConcurrentHashMap<String, RendererState>()
    private val rendererMetadata = ConcurrentHashMap<String, ResolvedMetadata>()
    private val resolvedItemIds = ConcurrentHashMap<String, String>() // nodeId -> last resolved itemId
    private val _statesUpdated = MutableStateFlow(0L) // bumped to trigger recomposition
    private val _clockTick = MutableStateFlow(0L)     // bumped every second for lease countdowns
    private val observedRenderers = mutableSetOf<String>()

    init {
        // Drive a 1 Hz tick so lease countdown labels stay fresh even when
        // no other state is changing.
        viewModelScope.launch {
            while (isActive) {
                delay(1_000)
                _clockTick.value = System.currentTimeMillis()
            }
        }
    }

    val uiState: StateFlow<RenderersUiState> = combine(
        listOf<kotlinx.coroutines.flow.Flow<Any?>>(
            nodeRepository.renderers,
            activeRendererRepository.activeRendererId,
            mqttConnectionManager.connectionState,
            leaseManager.leaseInfos,
            _statesUpdated,
            _clockTick,
        ),
    ) { values: Array<Any?> ->
        @Suppress("UNCHECKED_CAST")
        val renderers = values[0] as List<Node>
        val activeId = values[1] as String
        val connectionState = values[2] as ConnectionState
        @Suppress("UNCHECKED_CAST")
        val leaseInfos = values[3] as Map<String, com.mediautopia.app.domain.usecase.LeaseInfo>

        // Start observing any new renderers we haven't seen yet.
        ensureStateObservations(renderers)

        val isConnected = connectionState == ConnectionState.CONNECTED

        val items = buildList {
            // "This Phone" always first.
            val localNode = renderers.find { it.isLocal }
            val localId = localNode?.nodeId ?: ActiveRendererRepository.LOCAL_RENDERER_ID
            val isLocalActive = activeId == ActiveRendererRepository.LOCAL_RENDERER_ID ||
                activeId == localId

            val localState = rendererStates[localId]
            val localLeaseOwner = localState?.session?.owner

            add(
                RendererItem(
                    nodeId = localId,
                    name = "This Phone",
                    isLocal = true,
                    isActive = isLocalActive,
                    status = if (isLocalActive) "LOCAL PLAYBACK" else "Local playback",
                    leaseOwner = localLeaseOwner,
                    // We own the lease iff we have a token cached. By
                    // construction only this controller can populate
                    // leaseInfos, so this is more reliable than comparing
                    // identity strings (which can race with DataStore loads).
                    isOwnLease = leaseInfos[localId] != null,
                    leaseExpiresAtMs = leaseInfos[localId]?.expiresAtMs,
                )
            )

            // Network renderers.
            renderers
                .filter { !it.isLocal }
                .forEach { node ->
                    val isActive = node.nodeId == activeId
                    val state = rendererStates[node.nodeId]
                    add(
                        buildRendererItem(node, isActive, state, isConnected).copy(
                            leaseExpiresAtMs = leaseInfos[node.nodeId]?.expiresAtMs,
                            isOwnLease = leaseInfos[node.nodeId] != null,
                        )
                    )
                }
        }

        RenderersUiState(
            renderers = items,
            activeRendererId = activeId,
            isScanning = isConnected,
            connectionState = connectionState,
        )
    }.stateIn(
        scope = viewModelScope,
        started = SharingStarted.WhileSubscribed(5_000),
        initialValue = RenderersUiState(),
    )

    /**
     * Trigger a hard reconnect via the foreground service: stop the local
     * renderer, clear every cached lease, drop node state, tear down MQTT,
     * and re-initialise from scratch.
     */
    fun reconnect() {
        MqttForegroundService.reconnect(appContext)
        snackbarManager.show("Reconnecting...")
    }

    private fun buildRendererItem(
        node: Node,
        isActive: Boolean,
        state: RendererState?,
        isConnected: Boolean,
    ): RendererItem {
        val playbackStatus = state?.playback?.status ?: "stopped"
        val resolved = rendererMetadata[node.nodeId]
        val inlineMeta = state?.current?.metadata
        val title = resolved?.title?.ifBlank { null } ?: inlineMeta?.title()
        val artist = resolved?.artist?.ifBlank { null } ?: inlineMeta?.artistString()
        val leaseOwner = state?.session?.owner

        val status: String
        val formatBadge: String?
        val currentTrack: String?

        when (playbackStatus) {
            "playing" -> {
                val parts = listOfNotNull(title, artist)
                status = if (parts.isNotEmpty()) {
                    "Playing: ${parts.joinToString(" \u2014 ")}"
                } else {
                    "Playing"
                }
                formatBadge = buildFormatBadge(inlineMeta)
                currentTrack = title
            }
            "paused" -> {
                val parts = listOfNotNull(title, artist)
                status = if (parts.isNotEmpty()) {
                    "Paused: ${parts.joinToString(" \u2014 ")}"
                } else {
                    "Paused"
                }
                formatBadge = buildFormatBadge(inlineMeta)
                currentTrack = title
            }
            else -> {
                status = if (isConnected) "Ready to stream" else "Standby"
                formatBadge = null
                currentTrack = null
            }
        }

        return RendererItem(
            nodeId = node.nodeId,
            name = node.name,
            isLocal = false,
            isActive = isActive,
            status = status,
            formatBadge = formatBadge,
            currentTrack = currentTrack,
            leaseOwner = leaseOwner,
            isOwnLease = leaseOwner != null && leaseOwner == appIdentity.value,
        )
    }

    /**
     * Start observing state for any renderers we haven't subscribed to yet.
     */
    private fun ensureStateObservations(renderers: List<Node>) {
        for (node in renderers) {
            if (node.nodeId in observedRenderers) continue
            observedRenderers.add(node.nodeId)

            viewModelScope.launch {
                rendererStateRepository.observeState(node.nodeId).collect { state ->
                    rendererStates[node.nodeId] = state
                    // Resolve metadata if current item changed.
                    val itemId = state.current?.itemId
                    if (itemId != null && itemId != resolvedItemIds[node.nodeId]) {
                        resolvedItemIds[node.nodeId] = itemId
                        resolveMetadata(node.nodeId, itemId)
                    }
                    _statesUpdated.value = System.currentTimeMillis()
                }
            }
        }
    }

    private fun resolveMetadata(nodeId: String, itemId: String) {
        viewModelScope.launch {
            val cached = metadataCache.get(itemId)
            if (cached != null) {
                rendererMetadata[nodeId] = cached
                _statesUpdated.value = System.currentTimeMillis()
                return@launch
            }
            try {
                val resolved = libraryRepository.resolve(itemId)
                if (resolved != null) {
                    rendererMetadata[nodeId] = resolved
                    _statesUpdated.value = System.currentTimeMillis()
                }
            } catch (e: Exception) {
                Log.w(tag, "Failed to resolve metadata for $itemId: ${e.message}")
            }
        }
    }

    fun selectRenderer(nodeId: String) {
        viewModelScope.launch {
            activeRendererRepository.setActiveRenderer(nodeId)
        }
    }

    fun releaseLease(nodeId: String) {
        viewModelScope.launch {
            try {
                leaseManager.releaseLease(nodeId)
                Log.i(tag, "Released lease for $nodeId")
                snackbarManager.show("Lease released")
            } catch (e: Exception) {
                Log.e(tag, "releaseLease failed for $nodeId: ${e.message}", e)
                snackbarManager.show("Release failed: ${e.message}")
            }
        }
    }

    fun takeControl(nodeId: String) {
        viewModelScope.launch {
            try {
                leaseManager.takeControl(nodeId)
                Log.i(tag, "Took control of $nodeId")
                snackbarManager.show("You now control this renderer")
            } catch (e: Exception) {
                Log.e(tag, "takeControl failed for $nodeId: ${e.message}", e)
                snackbarManager.show("Take control failed: ${e.message}")
            }
        }
    }

    /**
     * Polite acquire for a renderer whose cached or displayed lease is foreign.
     * Succeeds if the server-side lease has expired (common case: stale state
     * display outliving the real TTL). Fails with a snackbar if the other
     * controller still actively holds it.
     */
    fun acquireLease(nodeId: String) {
        viewModelScope.launch {
            try {
                leaseManager.ensureLease(nodeId)
                Log.i(tag, "Acquired lease for $nodeId")
                snackbarManager.show("Lease acquired")
            } catch (e: Exception) {
                Log.e(tag, "acquireLease failed for $nodeId: ${e.message}", e)
                snackbarManager.show("Acquire failed: lease still held")
            }
        }
    }

    private fun buildFormatBadge(metadata: Map<String, JsonElement>?): String? {
        metadata ?: return null
        val bitDepth = metadata["bitDepth"]?.asPrimitiveOrNull()
        val sampleRate = metadata["sampleRate"]?.asPrimitiveOrNull()
        if (bitDepth != null && sampleRate != null) {
            val rate = sampleRate.replace("[^0-9]".toRegex(), "")
            val rateVal = rate.toLongOrNull() ?: return null
            val rateStr = if (rateVal >= 1000) "${rateVal / 1000}KHZ" else "${rateVal}HZ"
            return "${bitDepth}-BIT / $rateStr"
        }
        return null
    }

    companion object {
        private fun JsonElement?.asPrimitiveOrNull(): String? {
            return (this as? JsonPrimitive)?.contentOrNull
        }
    }
}
