package com.mediautopia.app.renderer

import android.content.Context
import android.util.Log
import androidx.annotation.OptIn
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer

/**
 * [MuDriver] implementation backed by AndroidX Media3 ExoPlayer.
 *
 * IMPORTANT: ExoPlayer must be created and accessed on the application main
 * thread. All methods on this class must be called from the main thread.
 */
class ExoPlayerDriver(
    context: Context,
    private val onTrackFinished: () -> Unit,
) : MuDriver {

    private val tag = "ExoPlayerDriver"

    private val player: ExoPlayer = ExoPlayer.Builder(context).build().also { p ->
        p.addListener(object : Player.Listener {
            override fun onPlaybackStateChanged(playbackState: Int) {
                if (playbackState == Player.STATE_ENDED) {
                    Log.d(tag, "Track ended, notifying engine")
                    onTrackFinished()
                }
            }

            override fun onPlayerError(error: PlaybackException) {
                Log.e(tag, "ExoPlayer error: ${error.message}", error)
            }
        })
    }

    /** Volume saved before muting so we can restore it. */
    private var savedVolume: Float = 1.0f
    private var isMuted: Boolean = false

    @OptIn(UnstableApi::class)
    override fun play(url: String, positionMs: Long) {
        Log.d(tag, "play url=$url positionMs=$positionMs")
        player.stop()
        player.clearMediaItems()
        player.setMediaItem(MediaItem.fromUri(url))
        player.prepare()
        if (positionMs > 0) {
            player.seekTo(positionMs)
        }
        player.play()
    }

    override fun pause() {
        player.pause()
    }

    override fun resume() {
        player.play()
    }

    override fun stop() {
        player.stop()
        player.clearMediaItems()
    }

    override fun seekTo(positionMs: Long) {
        player.seekTo(positionMs)
    }

    override fun setVolume(volume: Float) {
        val clamped = volume.coerceIn(0.0f, 1.0f)
        player.volume = clamped
        if (!isMuted) {
            savedVolume = clamped
        }
    }

    override fun setMute(mute: Boolean) {
        isMuted = mute
        if (mute) {
            savedVolume = player.volume
            player.volume = 0.0f
        } else {
            player.volume = savedVolume
        }
    }

    override fun position(): DriverPosition {
        val valid = player.playbackState == Player.STATE_READY ||
            player.playbackState == Player.STATE_BUFFERING
        return DriverPosition(
            positionMs = player.currentPosition,
            durationMs = player.duration.coerceAtLeast(0),
            isValid = valid,
        )
    }

    override fun release() {
        Log.i(tag, "Releasing ExoPlayer")
        player.release()
    }
}
