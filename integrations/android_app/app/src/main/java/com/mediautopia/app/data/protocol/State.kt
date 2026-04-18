package com.mediautopia.app.data.protocol

import kotlinx.serialization.Serializable

@Serializable
data class RendererState(
    val session: SessionState? = null,
    val playback: PlaybackState? = null,
    val queue: QueueState? = null,
    val current: CurrentItemState? = null,
    val stateVersion: Long = 0,
    val ts: Long,
)

@Serializable
data class SessionState(
    val id: String,
    val owner: String,
    val leaseExpiresAt: Long,
)

@Serializable
data class PlaybackState(
    val status: String,
    val positionMs: Long,
    val durationMs: Long,
    val volume: Double,
    val mute: Boolean,
)

@Serializable
data class QueueState(
    val revision: Long,
    val length: Long,
    val index: Long,
    val repeat: Boolean = false,
    val repeatMode: String = "",
    val shuffle: Boolean = false,
)

@Serializable
data class CurrentItemState(
    val queueEntryId: String,
    val ref: LibraryItemRef? = null,
    val resolved: ResolvedSource? = null,
    val display: DisplayMetadata? = null,
)
