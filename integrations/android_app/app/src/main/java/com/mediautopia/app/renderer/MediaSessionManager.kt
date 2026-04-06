package com.mediautopia.app.renderer

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.util.Log
import androidx.media3.common.ForwardingPlayer
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.session.MediaSession
import android.graphics.BitmapFactory
import com.mediautopia.app.data.protocol.RendererState
import com.mediautopia.app.data.protocol.artistString
import com.mediautopia.app.data.protocol.artworkUrl
import com.mediautopia.app.data.protocol.title
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.ByteArrayOutputStream
import java.net.URL

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
    private var artworkJob: Job? = null
    private var cachedArtworkUrl: String? = null
    private var cachedArtworkBytes: ByteArray? = null

    fun create(): MediaSession {
        // Release any previous session to avoid "Session ID must be unique" crash.
        mediaSession?.release()
        mediaSession = null

        val fwd = MuForwardingPlayer(exoPlayer, onTransportCommand, initialHasLease = false)
        forwardingPlayer = fwd

        // PendingIntent that opens the app when user taps the system media panel.
        val launchIntent = context.packageManager.getLaunchIntentForPackage(context.packageName)
            ?: Intent()
        val sessionActivity = PendingIntent.getActivity(
            context, 0, launchIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        val session = MediaSession.Builder(context, fwd)
            .setId("mu-local-renderer-${android.os.Process.myPid()}")
            .setSessionActivity(sessionActivity)
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
            // The hasLease setter notifies MediaSession listeners that
            // available commands changed (play/pause/seek/etc).
            forwardingPlayer?.hasLease = newHasLease
        }

        val meta = currentEntry?.metadata
        if (meta != null && meta.isNotEmpty()) {
            val title = meta.title() ?: ""
            val artist = meta.artistString() ?: ""
            val artUrl = meta.artworkUrl()

            // Download artwork if URL changed.
            if (artUrl != null && artUrl != cachedArtworkUrl) {
                cachedArtworkUrl = artUrl
                cachedArtworkBytes = null
                artworkJob?.cancel()
                artworkJob = scope.launch {
                    try {
                        val bytes = withContext(Dispatchers.IO) {
                            val bitmap = BitmapFactory.decodeStream(URL(artUrl).openStream())
                                ?: return@withContext null
                            val out = ByteArrayOutputStream()
                            bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 90, out)
                            out.toByteArray()
                        }
                        if (bytes != null) {
                            cachedArtworkBytes = bytes
                            // Re-push metadata with artwork.
                            val updated = buildMetadata(title, artist, bytes)
                            forwardingPlayer?.currentMetadata = updated
                        }
                    } catch (e: Exception) {
                        Log.w(tag, "Failed to load artwork: ${e.message}")
                    }
                }
            }

            val metadata = buildMetadata(title, artist, cachedArtworkBytes)
            forwardingPlayer?.currentMetadata = metadata
        } else if (state.playback?.status == "stopped") {
            forwardingPlayer?.currentMetadata = null
            cachedArtworkUrl = null
            cachedArtworkBytes = null
        }
    }

    private fun buildMetadata(title: String, artist: String, artworkBytes: ByteArray?): MediaMetadata {
        val builder = MediaMetadata.Builder()
            .setTitle(title.ifEmpty { null })
            .setArtist(artist.ifEmpty { null })
        if (artworkBytes != null) {
            builder.setArtworkData(artworkBytes, MediaMetadata.PICTURE_TYPE_FRONT_COVER)
        }
        return builder.build()
    }

    fun release() {
        artworkJob?.cancel()
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
    initialHasLease: Boolean,
) : ForwardingPlayer(player) {

    var currentMetadata: MediaMetadata? = null

    // Track listeners so we can notify when available commands change
    // (e.g. when hasLease flips). ForwardingPlayer's listener list is
    // private, so we maintain our own parallel list.
    private val commandListeners = mutableListOf<Player.Listener>()

    var hasLease: Boolean = initialHasLease
        set(value) {
            if (field != value) {
                field = value
                val commands = getAvailableCommands()
                commandListeners.toList().forEach {
                    it.onAvailableCommandsChanged(commands)
                }
            }
        }

    override fun addListener(listener: Player.Listener) {
        super.addListener(listener)
        commandListeners.add(listener)
    }

    override fun removeListener(listener: Player.Listener) {
        super.removeListener(listener)
        commandListeners.remove(listener)
    }

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
                Player.COMMAND_SEEK_BACK,
                Player.COMMAND_SEEK_FORWARD,
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
    override fun seekToNext() {
        val targetMs = currentPosition + seekForwardIncrement
        onTransportCommand("playback.seek:$targetMs")
    }

    override fun seekToPrevious() {
        val targetMs = maxOf(0L, currentPosition - seekBackIncrement)
        onTransportCommand("playback.seek:$targetMs")
    }

    override fun seekBack() {
        val targetMs = maxOf(0L, currentPosition - seekBackIncrement)
        onTransportCommand("playback.seek:$targetMs")
    }

    override fun seekForward() {
        val targetMs = currentPosition + seekForwardIncrement
        onTransportCommand("playback.seek:$targetMs")
    }

    override fun getSeekBackIncrement(): Long = 10_000L
    override fun getSeekForwardIncrement(): Long = 10_000L

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
