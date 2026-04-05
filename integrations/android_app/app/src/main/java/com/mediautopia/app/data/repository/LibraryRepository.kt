package com.mediautopia.app.data.repository

import android.util.Log
import com.mediautopia.app.data.cache.MetadataCache
import com.mediautopia.app.data.cache.ResolvedMetadata
import com.mediautopia.app.data.protocol.LibraryBrowseBody
import com.mediautopia.app.data.protocol.LibraryResolveBatchBody
import com.mediautopia.app.data.protocol.LibraryResolveBatchReply
import com.mediautopia.app.data.protocol.LibraryResolveBody
import com.mediautopia.app.data.protocol.LibraryResolveReply
import com.mediautopia.app.data.protocol.LibrarySearchBody
import com.mediautopia.app.domain.usecase.CommandCorrelator
import kotlinx.coroutines.flow.first
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull
import javax.inject.Inject
import javax.inject.Singleton

data class BrowseResult(
    val items: List<BrowseItem>,
    val hasMore: Boolean,
)

data class BrowseItem(
    val id: String,
    val type: String,
    val title: String,
    val subtitle: String = "",
    val artworkUrl: String? = null,
    val isContainer: Boolean = false,
    val metadata: Map<String, Any> = emptyMap(),
)

@Singleton
class LibraryRepository @Inject constructor(
    private val nodeRepository: NodeRepository,
    private val correlator: CommandCorrelator,
    private val metadataCache: MetadataCache,
) {
    private val tag = "LibraryRepository"

    private val json = Json { ignoreUnknownKeys = true }

    // Container type markers (case-insensitive match).
    private val containerPatterns = listOf("container", "artist", "album", "folder")

    // -------------------------------------------------------------------------
    // Browse
    // -------------------------------------------------------------------------

    suspend fun browse(
        containerId: String,
        start: Long = 0,
        count: Long = 50,
    ): BrowseResult {
        val libraryNode = findLibraryNode()
            ?: return BrowseResult(items = emptyList(), hasMore = false)

        val body = json.encodeToJsonElement(
            LibraryBrowseBody(
                containerId = containerId,
                start = start,
                count = count,
            )
        )

        val reply = correlator.send(
            nodeId = libraryNode,
            cmdType = "library.browse",
            body = body,
        )

        if (!reply.ok || reply.body == null) {
            Log.w(tag, "browse failed: ${reply.err?.message}")
            return BrowseResult(items = emptyList(), hasMore = false)
        }

        return parseBrowseReply(reply.body!!, count)
    }

    // -------------------------------------------------------------------------
    // Search
    // -------------------------------------------------------------------------

    suspend fun search(
        query: String,
        start: Long = 0,
        count: Long = 50,
        types: List<String> = emptyList(),
    ): List<BrowseItem> {
        val libraryNode = findLibraryNode() ?: return emptyList()

        val body = json.encodeToJsonElement(
            LibrarySearchBody(
                query = query,
                start = start,
                count = count,
                types = types,
            )
        )

        val reply = correlator.send(
            nodeId = libraryNode,
            cmdType = "library.search",
            body = body,
        )

        if (!reply.ok || reply.body == null) {
            Log.w(tag, "search failed: ${reply.err?.message}")
            return emptyList()
        }

        return parseBrowseReply(reply.body!!, count).items
    }

    // -------------------------------------------------------------------------
    // Resolve (single)
    // -------------------------------------------------------------------------

    suspend fun resolve(itemId: String): ResolvedMetadata? {
        // Check cache first.
        metadataCache.get(itemId)?.let { return it }

        val libraryNode = findLibraryNode() ?: return null

        val body = json.encodeToJsonElement(
            LibraryResolveBody(itemId = itemId, metadataOnly = true)
        )

        val reply = try {
            correlator.send(
                nodeId = libraryNode,
                cmdType = "library.resolve",
                body = body,
            )
        } catch (e: Exception) {
            Log.e(tag, "resolve failed for $itemId: ${e.message}")
            metadataCache.markFailed(itemId)
            return null
        }

        if (!reply.ok || reply.body == null) {
            Log.w(tag, "resolve reply not ok for $itemId: ${reply.err?.message}")
            metadataCache.markFailed(itemId)
            return null
        }

        val resolveReply = json.decodeFromJsonElement<LibraryResolveReply>(reply.body!!)
        val metadata = parseResolvedMetadata(resolveReply.metadata)
        metadataCache.put(itemId, metadata)
        return metadata
    }

    // -------------------------------------------------------------------------
    // Resolve batch
    // -------------------------------------------------------------------------

    suspend fun resolveBatch(itemIds: List<String>): Map<String, ResolvedMetadata> {
        val result = mutableMapOf<String, ResolvedMetadata>()
        val uncached = mutableListOf<String>()

        // Collect cached entries and identify uncached.
        for (id in itemIds) {
            val cached = metadataCache.get(id)
            if (cached != null) {
                result[id] = cached
            } else if (metadataCache.shouldRetry(id)) {
                uncached.add(id)
            }
        }

        if (uncached.isEmpty()) return result

        val libraryNode = findLibraryNode() ?: return result

        // Resolve in batches of 20.
        for (batch in uncached.chunked(20)) {
            val body = json.encodeToJsonElement(
                LibraryResolveBatchBody(itemIds = batch, metadataOnly = true)
            )

            val reply = try {
                correlator.send(
                    nodeId = libraryNode,
                    cmdType = "library.resolveBatch",
                    body = body,
                )
            } catch (e: Exception) {
                Log.e(tag, "resolveBatch failed: ${e.message}")
                batch.forEach { metadataCache.markFailed(it) }
                continue
            }

            if (!reply.ok || reply.body == null) {
                Log.w(tag, "resolveBatch reply not ok: ${reply.err?.message}")
                batch.forEach { metadataCache.markFailed(it) }
                continue
            }

            val batchReply = json.decodeFromJsonElement<LibraryResolveBatchReply>(reply.body!!)
            val batchResults = mutableMapOf<String, ResolvedMetadata>()

            for (item in batchReply.items) {
                if (item.err != null) {
                    metadataCache.markFailed(item.itemId)
                    continue
                }
                val metadata = parseResolvedMetadata(item.metadata)
                batchResults[item.itemId] = metadata
                result[item.itemId] = metadata
            }

            metadataCache.putAll(batchResults)
        }

        return result
    }

    // -------------------------------------------------------------------------
    // Internal helpers
    // -------------------------------------------------------------------------

    private suspend fun findLibraryNode(): String? {
        val libraries = nodeRepository.libraries.first()
        if (libraries.isEmpty()) {
            Log.w(tag, "No library nodes available")
            return null
        }
        return libraries.first().nodeId
    }

    private fun parseBrowseReply(body: JsonElement, requestedCount: Long): BrowseResult {
        val obj = body.jsonObject
        val itemsArray = obj["items"]?.jsonArray ?: JsonArray(emptyList())
        val total = obj["total"]?.jsonPrimitive?.longOrNull ?: 0
        val start = obj["start"]?.jsonPrimitive?.longOrNull ?: 0
        val count = obj["count"]?.jsonPrimitive?.longOrNull ?: itemsArray.size.toLong()

        val items = itemsArray.map { element ->
            parseLibraryItem(element.jsonObject)
        }

        val hasMore = (start + count) < total

        return BrowseResult(items = items, hasMore = hasMore)
    }

    private fun parseLibraryItem(obj: JsonObject): BrowseItem {
        val itemId = obj["itemId"]?.jsonPrimitive?.contentOrNull ?: ""
        val name = obj["name"]?.jsonPrimitive?.contentOrNull ?: ""
        val type = obj["type"]?.jsonPrimitive?.contentOrNull ?: ""
        val mediaType = obj["mediaType"]?.jsonPrimitive?.contentOrNull ?: ""
        val artists = obj["artists"]?.jsonArray
            ?.mapNotNull { it.jsonPrimitive.contentOrNull }
            ?: emptyList()
        val album = obj["album"]?.jsonPrimitive?.contentOrNull ?: ""
        val imageUrl = obj["imageUrl"]?.jsonPrimitive?.contentOrNull
        val durationMs = obj["durationMs"]?.jsonPrimitive?.longOrNull ?: 0
        val overview = obj["overview"]?.jsonPrimitive?.contentOrNull ?: ""

        val typeLower = type.lowercase()
        val idLower = itemId.lowercase()
        val isContainer = containerPatterns.any { pattern ->
            typeLower.contains(pattern) || idLower.startsWith("$pattern:")
        }

        // Build subtitle from available metadata.
        val subtitle = when {
            artists.isNotEmpty() -> artists.joinToString(", ")
            overview.isNotEmpty() -> overview
            else -> ""
        }

        // Metadata map for downstream use.
        val metadata = buildMap<String, Any> {
            if (artists.isNotEmpty()) put("artist", artists.joinToString(", "))
            if (album.isNotEmpty()) put("album", album)
            if (durationMs > 0) put("durationMs", durationMs)
            if (mediaType.isNotEmpty()) put("mediaType", mediaType)
            if (imageUrl != null) put("artworkUrl", imageUrl)
        }

        return BrowseItem(
            id = itemId,
            type = type,
            title = name,
            subtitle = subtitle,
            artworkUrl = imageUrl,
            isContainer = isContainer,
            metadata = metadata,
        )
    }

    private fun parseResolvedMetadata(
        metadata: Map<String, JsonElement>?,
    ): ResolvedMetadata {
        if (metadata == null) return ResolvedMetadata()

        return ResolvedMetadata(
            title = metadata.stringValue("title") ?: "",
            artist = metadata.stringValue("artist") ?: "",
            album = metadata.stringValue("album") ?: "",
            artworkUrl = metadata.stringValue("artworkUrl"),
            format = metadata.stringValue("format") ?: "",
            sampleRate = metadata.intValue("sampleRate"),
            bitDepth = metadata.intValue("bitDepth"),
            durationMs = metadata.longValue("durationMs"),
        )
    }

    private fun Map<String, JsonElement>.stringValue(key: String): String? {
        return (this[key] as? JsonPrimitive)?.contentOrNull
    }

    private fun Map<String, JsonElement>.intValue(key: String): Int {
        return (this[key] as? JsonPrimitive)?.intOrNull ?: 0
    }

    private fun Map<String, JsonElement>.longValue(key: String): Long {
        return (this[key] as? JsonPrimitive)?.longOrNull ?: 0
    }
}
