package com.mediautopia.app.ui.screen.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mediautopia.app.data.cache.SettingsDataStore
import com.mediautopia.app.data.mqtt.ConnectionState
import com.mediautopia.app.data.mqtt.MqttConnectionManager
import com.mediautopia.app.data.mqtt.MqttTopics
import com.mediautopia.app.data.repository.NodeRepository
import com.mediautopia.app.domain.usecase.CommandCorrelator
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

@HiltViewModel
class SettingsViewModel @Inject constructor(
    private val settingsDataStore: SettingsDataStore,
    private val mqttConnectionManager: MqttConnectionManager,
    private val commandCorrelator: CommandCorrelator,
    private val nodeRepository: NodeRepository,
) : ViewModel() {

    private val _brokerUrl = MutableStateFlow("")
    val brokerUrl: StateFlow<String> = _brokerUrl.asStateFlow()

    private val _identity = MutableStateFlow("")
    val identity: StateFlow<String> = _identity.asStateFlow()

    val connectionState: StateFlow<ConnectionState> = mqttConnectionManager.connectionState
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ConnectionState.DISCONNECTED)

    val visualizerEnabled: StateFlow<Boolean> = settingsDataStore.visualizerEnabled
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), false)

    init {
        viewModelScope.launch {
            _brokerUrl.value = settingsDataStore.brokerUrl.first()
        }
        viewModelScope.launch {
            _identity.value = settingsDataStore.identity.first()
        }
    }

    fun updateBrokerUrl(url: String) {
        _brokerUrl.value = url
    }

    fun updateIdentity(identity: String) {
        _identity.value = identity
    }

    fun save() {
        viewModelScope.launch {
            settingsDataStore.setBrokerUrl(_brokerUrl.value)
            settingsDataStore.setIdentity(_identity.value)
        }
    }

    fun toggleVisualizer() {
        viewModelScope.launch {
            val current = settingsDataStore.visualizerEnabled.first()
            settingsDataStore.setVisualizerEnabled(!current)
        }
    }

    fun reconnect() {
        viewModelScope.launch {
            // Tear down existing connection and discovery.
            nodeRepository.stopDiscovery()
            commandCorrelator.cleanup()
            try {
                mqttConnectionManager.disconnect()
            } catch (_: Exception) {
                // Best-effort disconnect.
            }

            // Re-read persisted settings and reconnect.
            val brokerUrl = settingsDataStore.brokerUrl.first()
            val clientId = "mu-android-${settingsDataStore.clientId.first()}"
            val identity = settingsDataStore.identity.first()

            mqttConnectionManager.connect(brokerUrl, clientId)
            commandCorrelator.setup(
                topicBase = MqttTopics.BASE,
                controllerId = clientId,
                identity = identity,
            )

            // Restart discovery so renderers/libraries/zones re-appear.
            nodeRepository.startDiscovery()
        }
    }
}
