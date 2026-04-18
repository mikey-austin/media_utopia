package com.mediautopia.app.data.protocol

import kotlinx.serialization.EncodeDefault
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject

// --- Session ---

@Serializable
data class SessionAcquireBody(
    val ttlMs: Long,
)

@Serializable
data class SessionRenewBody(
    val ttlMs: Long,
)

@Serializable
data class SessionReplyBody(
    val session: SessionLease,
    val stateVersion: Long,
)

@Serializable
data class SessionLease(
    val id: String,
    val token: String,
    val owner: String,
    val leaseExpiresAt: Long,
)

// --- Playback ---

@Serializable
data class PlaybackPlayBody(
    val index: Long? = null,
)

@Serializable
data class PlaybackSeekBody(
    val positionMs: Long,
)

@Serializable
data class PlaybackSetVolumeBody(
    val volume: Double,
)

@Serializable
data class PlaybackSetMuteBody(
    val mute: Boolean,
)

// --- Library item reference ---

/**
 * Canonical wire reference to a library-backed media item. Encoded as
 * { "kind": "libraryItem", "libraryId": "...", "itemId": "..." }. The
 * server validates that `kind == "libraryItem"`, so we force-emit it
 * even when callers use a Json config without `encodeDefaults = true`.
 */
@OptIn(ExperimentalSerializationApi::class)
@Serializable
data class LibraryItemRef(
    @EncodeDefault val kind: String = LIBRARY_ITEM_KIND,
    val libraryId: String,
    val itemId: String,
) {
    companion object {
        const val LIBRARY_ITEM_KIND = "libraryItem"
    }
}

/**
 * Denormalized UI snapshot carried alongside library refs so controllers
 * can render queue and now-playing without follow-up catalog lookups.
 * All fields are optional; consumers must tolerate missing values.
 */
@Serializable
data class DisplayMetadata(
    val title: String? = null,
    val artist: String? = null,
    val artists: List<String>? = null,
    val album: String? = null,
    val artworkUrl: String? = null,
    val durationMs: Long? = null,
    val mediaType: String? = null,
)

// --- Queue ---

@Serializable
data class QueueGetBody(
    val from: Long,
    val count: Long,
    val resolve: String = "",
)

@Serializable
data class QueueGetReply(
    val revision: Long,
    val index: Long,
    val entries: List<QueueEntry>,
)

@Serializable
data class QueueSetBody(
    val startIndex: Long,
    val entries: List<QueueEntry>,
)

@Serializable
data class QueueAddBody(
    val position: String,
    val atIndex: Long? = null,
    val entries: List<QueueEntry>,
)

@Serializable
data class QueueRemoveBody(
    val queueEntryId: String = "",
    val index: Long? = null,
)

@Serializable
data class QueueMoveBody(
    val fromIndex: Long,
    val toIndex: Long,
)

@Serializable
data class QueueJumpBody(
    val index: Long,
)

@Serializable
data class QueueShuffleBody(
    val seed: Long,
)

@Serializable
data class QueueSetShuffleBody(
    val shuffle: Boolean,
)

@Serializable
data class QueueRepeatBody(
    val repeat: Boolean,
    val mode: String = "",
)

@Serializable
data class QueueLoadPlaylistBody(
    val playlistServerId: String,
    val playlistId: String,
    val mode: String,
    val resolve: String,
)

@Serializable
data class QueueLoadSnapshotBody(
    val playlistServerId: String,
    val snapshotId: String,
    val mode: String,
    val resolve: String,
)

@Serializable
data class QueueLoadSuggestionBody(
    val playlistServerId: String,
    val suggestionId: String,
    val mode: String,
    val resolve: String,
)

@Serializable
data class QueueEntry(
    val queueEntryId: String = "",
    val ref: LibraryItemRef? = null,
    val resolved: ResolvedSource? = null,
    val display: DisplayMetadata? = null,
)

@Serializable
data class ResolvedSource(
    val url: String,
    val mime: String = "",
    val byteRange: Boolean = false,
)

// --- Library ---

@Serializable
data class LibraryBrowseBody(
    val containerId: String,
    val start: Long,
    val count: Long,
)

@Serializable
data class LibrarySearchBody(
    val query: String,
    val start: Long,
    val count: Long,
    val types: List<String> = emptyList(),
)

@Serializable
data class LibraryGetItemBody(
    val ref: LibraryItemRef,
)

@Serializable
data class LibraryGetItemReply(
    val ref: LibraryItemRef,
    val display: DisplayMetadata? = null,
    val attributes: JsonObject? = null,
)

@Serializable
data class LibraryGetItemsBody(
    val refs: List<LibraryItemRef>,
)

@Serializable
data class LibraryGetItemsReplyItem(
    val ref: LibraryItemRef,
    val display: DisplayMetadata? = null,
    val attributes: JsonObject? = null,
    val err: ReplyError? = null,
)

@Serializable
data class LibraryGetItemsReply(
    val items: List<LibraryGetItemsReplyItem>,
)

@Serializable
data class LibraryResolveSourcesBody(
    val ref: LibraryItemRef,
)

@Serializable
data class LibraryResolveSourcesReply(
    val ref: LibraryItemRef,
    val sources: List<ResolvedSource> = emptyList(),
)

@Serializable
data class LibraryResolveSourcesBatchBody(
    val refs: List<LibraryItemRef>,
)

@Serializable
data class LibraryResolveSourcesBatchItem(
    val ref: LibraryItemRef,
    val sources: List<ResolvedSource> = emptyList(),
    val err: ReplyError? = null,
)

@Serializable
data class LibraryResolveSourcesBatchReply(
    val items: List<LibraryResolveSourcesBatchItem>,
)

// --- Playlist ---

@Serializable
data class PlaylistListBody(
    val owner: String,
)

@Serializable
data class PlaylistListReply(
    val playlists: List<PlaylistSummary>,
)

@Serializable
data class PlaylistSummary(
    val playlistId: String,
    val name: String,
    val revision: Long,
)

@Serializable
data class PlaylistCreateBody(
    val name: String,
    val snapshotId: String = "",
)

@Serializable
data class PlaylistGetBody(
    val playlistId: String,
)

@Serializable
data class PlaylistAddItemsBody(
    val playlistId: String,
    val entries: List<QueueEntry>,
)

@Serializable
data class PlaylistRemoveItemsBody(
    val playlistId: String,
    val entryIds: List<String>,
)

@Serializable
data class PlaylistRenameBody(
    val playlistId: String,
    val name: String,
)

@Serializable
data class PlaylistDeleteBody(
    val playlistId: String,
)

@Serializable
data class PlaylistReplaceItemsBody(
    val playlistId: String,
    val entries: List<QueueEntry>,
)

// --- Snapshot ---

@Serializable
data class SnapshotSaveBody(
    val name: String,
    val rendererId: String,
    val sessionId: String,
    val capture: SnapshotCapture,
    val entries: List<QueueEntry> = emptyList(),
)

@Serializable
data class SnapshotCapture(
    val queueRevision: Long,
    val index: Long,
    val positionMs: Long,
    val repeat: Boolean,
    val repeatMode: String = "",
    val shuffle: Boolean,
)

@Serializable
data class SnapshotListBody(
    val owner: String = "",
)

@Serializable
data class SnapshotListReply(
    val snapshots: List<SnapshotSummary>,
)

@Serializable
data class SnapshotRemoveBody(
    val snapshotId: String,
)

@Serializable
data class SnapshotGetBody(
    val snapshotId: String,
)

@Serializable
data class SnapshotGetReply(
    val snapshotId: String,
    val name: String,
    val revision: Long,
    val entries: List<QueueEntry> = emptyList(),
    val capture: SnapshotCapture,
)

@Serializable
data class SnapshotSummary(
    val snapshotId: String,
    val name: String,
    val revision: Long = 0,
)

// --- Suggestion ---

@Serializable
data class SuggestListBody(
    val owner: String = "",
)

@Serializable
data class SuggestListReply(
    val suggestions: List<SuggestSummary>,
)

@Serializable
data class SuggestSummary(
    val suggestionId: String,
    val name: String,
    val revision: Long = 0,
)

@Serializable
data class SuggestGetBody(
    val suggestionId: String,
)

@Serializable
data class SuggestPromoteBody(
    val suggestionId: String,
    val name: String,
)
