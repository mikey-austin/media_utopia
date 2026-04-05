package com.mediautopia.app.renderer

/**
 * Abstraction over the audio playback engine. The local renderer engine
 * dispatches playback commands through this interface, making it possible
 * to swap implementations (e.g. ExoPlayer, test stub).
 */
interface MuDriver {
    fun play(url: String, positionMs: Long)
    fun pause()
    fun resume()
    fun stop()
    fun seekTo(positionMs: Long)
    fun setVolume(volume: Float)  // 0.0 to 1.0
    fun setMute(mute: Boolean)
    fun position(): DriverPosition
    fun release()
}

data class DriverPosition(
    val positionMs: Long,
    val durationMs: Long,
    val isValid: Boolean,
)
