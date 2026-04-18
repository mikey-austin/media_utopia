package com.mediautopia.app.data.repository

import android.util.Log
import com.mediautopia.app.data.protocol.LibraryItemRef
import com.mediautopia.app.data.protocol.PlaylistGetBody
import com.mediautopia.app.data.protocol.PlaylistListBody
import com.mediautopia.app.data.protocol.PlaylistListReply
import com.mediautopia.app.data.protocol.DisplayMetadata
import com.mediautopia.app.data.protocol.ResolvedSource
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import javax.inject.Inject
import javax.inject.Singleton

data class PlaylistInfo(
    val playlistId: String,
    val name: String,
    val entryCount: Int = 0,
)

data class PlaylistEntryInfo(
    val entryId: String,
    val ref: LibraryItemRef? = null,
    val title: String = "",
    val artist: String = "",
    val album: String = "",
    val artworkUrl: String? = null,
    val durationMs: Long = 0,
    val url: String = "",
    val mime: String = "",
)

// Mirrors mud's on-disk + wire shape (internal/modules/playlist/storage.go).
// Note `entryId` is distinct from `queueEntryId` — playlist entries have their
// own stable identity assigned by the playlist server.
@Serializable
private data class WirePlaylistEntry(
    val entryId: String = "",
    val ref: LibraryItemRef? = null,
    val resolved: ResolvedSource? = null,
    val display: DisplayMetadata? = null,
)

@Serializable
private data class PlaylistGetReply(
    val playlistId: String,
    val name: String,
    val revision: Long = 0,
    val entries: List<WirePlaylistEntry> = emptyList(),
)

@Singleton
class PlaylistRepository @Inject constructor(
    private val nodeRepository: NodeRepository,
    private val transport: com.mediautopia.app.data.transport.TransportRouter,
    private val libraryRepository: LibraryRepository,
) {
    private val tag = "PlaylistRepository"
    private val json = Json { ignoreUnknownKeys = true }

    suspend fun listPlaylists(serverNodeId: String): List<PlaylistInfo> {
        val body = json.encodeToJsonElement(PlaylistListBody(owner = ""))

        val reply = try {
            transport.send(
                nodeId = serverNodeId,
                cmdType = "playlist.list",
                body = body,
            )
        } catch (e: Exception) {
            Log.e(tag, "playlist.list failed: ${e.message}")
            return emptyList()
        }

        if (!reply.ok || reply.body == null) {
            Log.w(tag, "playlist.list not ok: ${reply.err?.message}")
            return emptyList()
        }

        val listReply = json.decodeFromJsonElement<PlaylistListReply>(reply.body!!)
        return listReply.playlists.map { summary ->
            PlaylistInfo(
                playlistId = summary.playlistId,
                name = summary.name,
            )
        }
    }

    suspend fun getPlaylist(serverNodeId: String, playlistId: String): List<PlaylistEntryInfo> {
        val body = json.encodeToJsonElement(PlaylistGetBody(playlistId = playlistId))

        val reply = try {
            transport.send(
                nodeId = serverNodeId,
                cmdType = "playlist.get",
                body = body,
            )
        } catch (e: Exception) {
            Log.e(tag, "playlist.get failed: ${e.message}")
            return emptyList()
        }

        if (!reply.ok || reply.body == null) {
            Log.w(tag, "playlist.get not ok: ${reply.err?.message}")
            return emptyList()
        }

        val getReply = json.decodeFromJsonElement<PlaylistGetReply>(reply.body!!)

        // Build entries; prefer the inline display block carried in each entry.
        val entries = getReply.entries.map { raw ->
            val display = raw.display
            PlaylistEntryInfo(
                entryId = raw.entryId,
                ref = raw.ref,
                title = display?.title ?: "",
                artist = display?.let {
                    it.artists?.takeIf { a -> a.isNotEmpty() }?.joinToString(", ")
                        ?: it.artist
                } ?: "",
                album = display?.album ?: "",
                artworkUrl = display?.artworkUrl,
                durationMs = display?.durationMs ?: 0L,
                url = raw.resolved?.url ?: "",
                mime = raw.resolved?.mime ?: "",
            )
        }

        // Backfill display only for entries that came in without one. This is
        // an explicit user-driven listing action; the renderer never goes here.
        val refsNeedingDisplay = entries.mapNotNull { e ->
            if (e.title.isEmpty() && e.ref != null) e.ref else null
        }
        if (refsNeedingDisplay.isEmpty()) return entries

        val resolved = try {
            libraryRepository.getItems(refsNeedingDisplay)
        } catch (e: Exception) {
            Log.w(tag, "metadata getItems failed: ${e.message}")
            emptyMap()
        }

        return entries.map { entry ->
            val ref = entry.ref ?: return@map entry
            val display = resolved[ref] ?: return@map entry
            entry.copy(
                title = entry.title.ifEmpty { display.title ?: "" },
                artist = entry.artist.ifEmpty {
                    display.artists?.takeIf { it.isNotEmpty() }?.joinToString(", ")
                        ?: display.artist
                        ?: ""
                },
                album = entry.album.ifEmpty { display.album ?: "" },
                artworkUrl = entry.artworkUrl ?: display.artworkUrl,
                durationMs = if (entry.durationMs > 0) entry.durationMs else (display.durationMs ?: 0L),
            )
        }
    }
}
