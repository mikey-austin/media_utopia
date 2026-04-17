package com.mediautopia.app.data.repository

import android.util.Log
import com.mediautopia.app.data.cache.MetadataCache
import com.mediautopia.app.data.protocol.PlaylistGetBody
import com.mediautopia.app.data.protocol.PlaylistListBody
import com.mediautopia.app.data.protocol.PlaylistListReply
import kotlinx.coroutines.flow.first
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull
import javax.inject.Inject
import javax.inject.Singleton

data class PlaylistInfo(
    val playlistId: String,
    val name: String,
    val entryCount: Int = 0,
)

data class PlaylistEntryInfo(
    val entryId: String,
    val itemId: String,
    val title: String = "",
    val artist: String = "",
    val album: String = "",
    val artworkUrl: String? = null,
    val durationMs: Long = 0,
    val url: String = "",
    val mime: String = "",
)

@Serializable
private data class PlaylistGetReply(
    val playlistId: String,
    val name: String,
    val revision: Long = 0,
    val entries: List<PlaylistEntryRaw> = emptyList(),
)

@Serializable
private data class PlaylistEntryRaw(
    val entryId: String,
    val ref: RefRaw? = null,
    val resolved: ResolvedRaw? = null,
)

@Serializable
private data class RefRaw(val id: String)

@Serializable
private data class ResolvedRaw(
    val itemId: String = "",
    val url: String = "",
    val mime: String = "",
)

@Singleton
class PlaylistRepository @Inject constructor(
    private val nodeRepository: NodeRepository,
    private val transport: com.mediautopia.app.data.transport.TransportRouter,
    private val libraryRepository: LibraryRepository,
    private val metadataCache: MetadataCache,
) {
    private val tag = "PlaylistRepository"
    private val json = Json { ignoreUnknownKeys = true }

    /**
     * List playlists from a specific playlist server.
     */
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

    /**
     * Get playlist entries with metadata resolution.
     */
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

        // Build basic entries from playlist data.
        val entries = getReply.entries.map { raw ->
            val itemId = raw.ref?.id ?: raw.resolved?.itemId ?: ""
            PlaylistEntryInfo(
                entryId = raw.entryId,
                itemId = itemId,
                url = raw.resolved?.url ?: "",
                mime = raw.resolved?.mime ?: "",
            )
        }

        // Resolve metadata for entries that have item IDs.
        val itemIds = entries.mapNotNull { it.itemId.ifEmpty { null } }
        val resolved = if (itemIds.isNotEmpty()) {
            try {
                libraryRepository.resolveBatch(itemIds)
            } catch (e: Exception) {
                Log.w(tag, "metadata resolve failed: ${e.message}")
                emptyMap()
            }
        } else emptyMap()

        // Merge metadata into entries.
        return entries.map { entry ->
            val meta = resolved[entry.itemId] ?: metadataCache.get(entry.itemId)
            if (meta != null) {
                entry.copy(
                    title = meta.title.ifEmpty { entry.title },
                    artist = meta.artist.ifEmpty { entry.artist },
                    album = meta.album.ifEmpty { entry.album },
                    artworkUrl = meta.artworkUrl ?: entry.artworkUrl,
                    durationMs = if (meta.durationMs > 0) meta.durationMs else entry.durationMs,
                )
            } else entry
        }
    }
}
