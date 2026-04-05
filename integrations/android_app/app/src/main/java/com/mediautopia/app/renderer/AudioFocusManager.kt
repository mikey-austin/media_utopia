package com.mediautopia.app.renderer

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.util.Log

/**
 * Manages Android audio focus for the local renderer.
 *
 * Requests [AudioManager.AUDIOFOCUS_GAIN] for music playback and dispatches
 * focus change events to the [AudioFocusListener] so the renderer can pause,
 * resume, or duck volume as appropriate.
 */
class AudioFocusManager(
    context: Context,
    private val listener: AudioFocusListener,
) {
    interface AudioFocusListener {
        fun onFocusGained()
        fun onFocusLost()
        fun onFocusLostTransient()
        fun onFocusDuck()
    }

    private val tag = "AudioFocusManager"

    private val audioManager = context.getSystemService(Context.AUDIO_SERVICE) as AudioManager

    private val focusChangeListener = AudioManager.OnAudioFocusChangeListener { focusChange ->
        when (focusChange) {
            AudioManager.AUDIOFOCUS_GAIN -> {
                Log.d(tag, "Audio focus gained")
                listener.onFocusGained()
            }
            AudioManager.AUDIOFOCUS_LOSS -> {
                Log.d(tag, "Audio focus lost (permanent)")
                listener.onFocusLost()
            }
            AudioManager.AUDIOFOCUS_LOSS_TRANSIENT -> {
                Log.d(tag, "Audio focus lost (transient)")
                listener.onFocusLostTransient()
            }
            AudioManager.AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK -> {
                Log.d(tag, "Audio focus lost (duck)")
                listener.onFocusDuck()
            }
        }
    }

    private val focusRequest: AudioFocusRequest = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN)
        .setAudioAttributes(
            AudioAttributes.Builder()
                .setUsage(AudioAttributes.USAGE_MEDIA)
                .setContentType(AudioAttributes.CONTENT_TYPE_MUSIC)
                .build()
        )
        .setWillPauseWhenDucked(false)
        .setOnAudioFocusChangeListener(focusChangeListener)
        .build()

    /**
     * Request audio focus for music playback.
     * @return true if focus was granted.
     */
    fun requestFocus(): Boolean {
        val result = audioManager.requestAudioFocus(focusRequest)
        val granted = result == AudioManager.AUDIOFOCUS_REQUEST_GRANTED
        Log.d(tag, "requestFocus: granted=$granted")
        return granted
    }

    /**
     * Abandon audio focus. Should be called when playback stops or the
     * renderer shuts down.
     */
    fun abandonFocus() {
        audioManager.abandonAudioFocusRequest(focusRequest)
        Log.d(tag, "Audio focus abandoned")
    }
}
