package com.mediautopia.app.renderer

import android.content.Context
import android.util.Log
import androidx.media3.common.ForwardingPlayer
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.session.MediaSession
import com.mediautopia.app.data.protocol.RendererState
import com.mediautopia.app.data.protocol.artistString
import com.mediautopia.app.data.protocol.artworkUrl
import com.mediautopia.app.data.protocol.title
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.longOrNull

/**
 * Bridges the local MU renderer to Android's MediaSession system.
 *
 * Uses a [ForwardingPlayer] around the real ExoPlayer to intercept
 * transport commands and route them through the MU engine (with lease
 * validation) while letting the system read real playback state.
 *
 * When no lease is held, available commands are restricted so the
 * system shows greyed-out controls.
 */
class MediaSessionManager(
    private val context: Context,
    private val exoPlayer: ExoPlayer,
    private val scope: CoroutineScope,
    private val onTransportCommand: (String) -> Unit,
) {
    private val tag = "MediaSessionManager"

    private var mediaSession: MediaSession? = null
    private var forwardingPlayer: MuForwardingPlayer? = null
    private var hasLease = false

    fun create(): MediaSession {
        // Release any previous session to avoid "Session ID must be unique" crash.
        mediaSession?.release()
        mediaSession = null

        val fwd = MuForwardingPlayer(exoPlayer, onTransportCommand, hasLease = false)
        forwardingPlayer = fwd

        val session = MediaSession.Builder(context, fwd)
            .setId("mu-local-renderer-${android.os.Process.myPid()}")
            .build()
        mediaSession = session
        Log.i(tag, "MediaSession created")
        return session
    }

    fun getSession(): MediaSession? = mediaSession

    /**
     * Update session state from the MU engine. Called on each debounced state update.
     */
    fun updateState(state: RendererState, currentEntry: LocalQueueEntry?) {
        val newHasLease = state.session != null
        if (newHasLease != hasLease) {
            hasLease = newHasLease
            forwardingPlayer?.hasLease = newHasLease
            // Notify listeners that available commands changed.
            mediaSession?.player?.let { player ->
                // The ForwardingPlayer delegates this automatically.
            }
        }

        // Update media metadata on the ExoPlayer's current MediaItem so
        // the system media controls show track info. We do this by setting
        // a MediaItem with metadata on the forwarding player which is
        // reflected via the session.
        val meta = currentEntry?.metadata
        if (meta != null && meta.isNotEmpty()) {
            val title = meta.title() ?: ""
            val artist = meta.artistString() ?: ""
            val artUrl = meta.artworkUrl()

            val metadata = MediaMetadata.Builder()
                .setTitle(title.ifEmpty { null })
                .setArtist(artist.ifEmpty { null })
                .setArtworkUri(artUrl?.let { android.net.Uri.parse(it) })
                .build()

            forwardingPlayer?.currentMetadata = metadata
        } else {
            forwardingPlayer?.currentMetadata = null
        }
    }

    fun release() {
        mediaSession?.release()
        mediaSession = null
        forwardingPlayer = null
        Log.i(tag, "MediaSession released")
    }
}

/**
 * Wraps the real ExoPlayer for MediaSession. Intercepts transport actions
 * (play/pause/next/prev) and routes them to the MU engine. The engine
 * will validate the lease and drive ExoPlayer, which updates the real state.
 *
 * When [hasLease] is false, transport commands are omitted from
 * [getAvailableCommands] so the system shows greyed-out controls.
 */
class MuForwardingPlayer(
    player: ExoPlayer,
    private val onTransportCommand: (String) -> Unit,
    var hasLease: Boolean,
) : ForwardingPlayer(player) {

    var currentMetadata: MediaMetadata? = null

    override fun getAvailableCommands(): Player.Commands {
        val builder = Player.Commands.Builder()
            .add(Player.COMMAND_GET_CURRENT_MEDIA_ITEM)
            .add(Player.COMMAND_GET_METADATA)
            .add(Player.COMMAND_GET_TIMELINE)

        if (hasLease) {
            builder.addAll(
                Player.COMMAND_PLAY_PAUSE,
                Player.COMMAND_STOP,
                Player.COMMAND_SEEK_IN_CURRENT_MEDIA_ITEM,
                Player.COMMAND_SEEK_TO_NEXT,
                Player.COMMAND_SEEK_TO_PREVIOUS,
            )
        }
        return builder.build()
    }

    override fun isCommandAvailable(command: Int): Boolean {
        return getAvailableCommands().contains(command)
    }

    // Intercept transport commands — route through MU engine instead of
    // directly controlling ExoPlayer.
    override fun play() { onTransportCommand("playback.play") }
    override fun pause() { onTransportCommand("playback.pause") }
    override fun stop() { onTransportCommand("playback.stop") }
    override fun seekToNext() { onTransportCommand("playback.next") }
    override fun seekToPrevious() { onTransportCommand("playback.prev") }

    override fun seekTo(positionMs: Long) {
        onTransportCommand("playback.seek:$positionMs")
    }

    override fun seekTo(mediaItemIndex: Int, positionMs: Long) {
        seekTo(positionMs)
    }

    override fun getMediaMetadata(): MediaMetadata {
        return currentMetadata ?: super.getMediaMetadata()
    }
}
