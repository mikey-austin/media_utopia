package com.mediautopia.app.renderer

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Holds the ExoPlayer audio session ID so the Visualizer can attach to it.
 */
@Singleton
class AudioSessionHolder @Inject constructor() {
    private val _sessionId = MutableStateFlow(0)
    val sessionId: StateFlow<Int> = _sessionId

    fun set(id: Int) { _sessionId.value = id }
    fun clear() { _sessionId.value = 0 }
}
