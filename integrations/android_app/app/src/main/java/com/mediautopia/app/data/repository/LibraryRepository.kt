package com.mediautopia.app.data.repository

import android.util.Log
import com.mediautopia.app.data.cache.MetadataCache
import com.mediautopia.app.data.protocol.DisplayMetadata
import com.mediautopia.app.data.protocol.LibraryBrowseBody
import com.mediautopia.app.data.protocol.LibraryGetItemBody
import com.mediautopia.app.data.protocol.LibraryGetItemReply
import com.mediautopia.app.data.protocol.LibraryGetItemsBody
import com.mediautopia.app.data.protocol.LibraryGetItemsReply
import com.mediautopia.app.data.protocol.LibraryItemRef
import com.mediautopia.app.data.protocol.LibraryResolveSourcesBatchBody
import com.mediautopia.app.data.protocol.LibraryResolveSourcesBatchReply
import com.mediautopia.app.data.protocol.LibraryResolveSourcesBody
import com.mediautopia.app.data.protocol.LibraryResolveSourcesReply
import com.mediautopia.app.data.protocol.LibrarySearchBody
import com.mediautopia.app.data.protocol.ResolvedSource
import kotlinx.coroutines.flow.first
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
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

data class BrowseResult(
    val items: List<BrowseItem>,
    val hasMore: Boolean,
)

/**
 * BrowseItem is the controller's view of one library entry. For non-container
 * items [ref] is the structured wire reference used to enqueue or resolve.
 * Containers carry their own [id]/[type] for in-library navigation.
 */
data class BrowseItem(
    val id: String,
    val type: String,
    val title: String,
    val subtitle: String = "",
    val artworkUrl: String? = null,
    val isContainer: Boolean = false,
    val ref: LibraryItemRef? = null,
    val display: DisplayMetadata? = null,
    val metadata: Map<String, Any> = emptyMap(),
)

@Singleton
class LibraryRepository @Inject constructor(
    private val nodeRepository: NodeRepository,
    private val transport: com.mediautopia.app.data.transport.TransportRouter,
    private val metadataCache: MetadataCache,
) {
    private val tag = "LibraryRepository"

    private val json = Json { ignoreUnknownKeys = true }

    private val containerPatterns = listOf("container", "artist", "album", "folder")
    private val explicitContainerTypes = setOf("podcast")
    private val explicitLeafTypes = setOf("podcastepisode")

    fun getCachedDisplay(ref: LibraryItemRef): DisplayMetadata? = metadataCache.get(ref)

    // -------------------------------------------------------------------------
    // Browse
    // -------------------------------------------------------------------------

    suspend fun browse(
        containerId: String,
        start: Long = 0,
        count: Long = 50,
        libraryNodeId: String? = null,
    ): BrowseResult {
        val libraryNode = libraryNodeId ?: findLibraryNode()
            ?: return BrowseResult(items = emptyList(), hasMore = false)

        val body = json.encodeToJsonElement(
            LibraryBrowseBody(
                containerId = containerId,
                start = start,
                count = count,
            )
        )

        val reply = transport.send(
            nodeId = libraryNode,
            cmdType = "library.browse",
            body = body,
        )

        if (!reply.ok || reply.body == null) {
            Log.w(tag, "browse failed: ${reply.err?.message}")
            return BrowseResult(items = emptyList(), hasMore = false)
        }

        return parseBrowseReply(reply.body!!, libraryNode, count)
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

        val reply = transport.send(
            nodeId = libraryNode,
            cmdType = "library.search",
            body = body,
        )

        if (!reply.ok || reply.body == null) {
            Log.w(tag, "search failed: ${reply.err?.message}")
            return emptyList()
        }

        return parseBrowseReply(reply.body!!, libraryNode, count).items
    }

    // -------------------------------------------------------------------------
    // Display metadata (catalog only)
    // -------------------------------------------------------------------------

    suspend fun getItem(ref: LibraryItemRef): DisplayMetadata? {
        metadataCache.get(ref)?.let { return it }

        val body = json.encodeToJsonElement(LibraryGetItemBody(ref = ref))

        val reply = try {
            transport.send(
                nodeId = ref.libraryId,
                cmdType = "library.getItem",
                body = body,
            )
        } catch (e: Exception) {
            Log.e(tag, "getItem failed for ${ref.itemId}: ${e.message}")
            metadataCache.markFailed(ref)
            return null
        }

        if (!reply.ok || reply.body == null) {
            Log.w(tag, "getItem reply not ok for ${ref.itemId}: ${reply.err?.message}")
            metadataCache.markFailed(ref)
            return null
        }

        val parsed = json.decodeFromJsonElement<LibraryGetItemReply>(reply.body!!)
        val display = parsed.display ?: DisplayMetadata()
        metadataCache.put(ref, display)
        return display
    }

    suspend fun getItems(refs: List<LibraryItemRef>): Map<LibraryItemRef, DisplayMetadata> {
        val result = mutableMapOf<LibraryItemRef, DisplayMetadata>()
        val uncached = mutableListOf<LibraryItemRef>()

        for (ref in refs) {
            val cached = metadataCache.get(ref)
            if (cached != null) {
                result[ref] = cached
            } else if (metadataCache.shouldRetry(ref)) {
                uncached.add(ref)
            }
        }

        if (uncached.isEmpty()) return result

        val byLibrary = uncached.groupBy { it.libraryId }

        for ((libraryNode, libRefs) in byLibrary) {
            for (batch in libRefs.chunked(20)) {
                val body = json.encodeToJsonElement(LibraryGetItemsBody(refs = batch))

                val reply = try {
                    transport.send(
                        nodeId = libraryNode,
                        cmdType = "library.getItems",
                        body = body,
                    )
                } catch (e: Exception) {
                    Log.e(tag, "getItems failed: ${e.message}")
                    batch.forEach { metadataCache.markFailed(it) }
                    continue
                }

                if (!reply.ok || reply.body == null) {
                    Log.w(tag, "getItems reply not ok: ${reply.err?.message}")
                    batch.forEach { metadataCache.markFailed(it) }
                    continue
                }

                val batchReply = json.decodeFromJsonElement<LibraryGetItemsReply>(reply.body!!)
                val batchResults = mutableMapOf<LibraryItemRef, DisplayMetadata>()
                for (item in batchReply.items) {
                    if (item.err != null) {
                        metadataCache.markFailed(item.ref)
                        continue
                    }
                    val display = item.display ?: DisplayMetadata()
                    batchResults[item.ref] = display
                    result[item.ref] = display
                }
                metadataCache.putAll(batchResults)
            }
        }

        return result
    }

    // -------------------------------------------------------------------------
    // Source resolution (playback URLs)
    // -------------------------------------------------------------------------

    suspend fun resolveSources(ref: LibraryItemRef): ResolvedSource? {
        val body = json.encodeToJsonElement(LibraryResolveSourcesBody(ref = ref))

        val reply = try {
            transport.send(
                nodeId = ref.libraryId,
                cmdType = "library.resolveSources",
                body = body,
            )
        } catch (e: Exception) {
            Log.e(tag, "resolveSources failed for ${ref.itemId}: ${e.message}")
            return null
        }

        if (!reply.ok || reply.body == null) {
            Log.w(tag, "resolveSources reply not ok for ${ref.itemId}: ${reply.err?.message}")
            return null
        }

        val parsed = json.decodeFromJsonElement<LibraryResolveSourcesReply>(reply.body!!)
        return parsed.sources.firstOrNull()
    }

    suspend fun resolveSourcesBatch(refs: List<LibraryItemRef>): Map<LibraryItemRef, ResolvedSource> {
        val result = mutableMapOf<LibraryItemRef, ResolvedSource>()
        val byLibrary = refs.groupBy { it.libraryId }

        for ((libraryNode, libRefs) in byLibrary) {
            for (batch in libRefs.chunked(20)) {
                val body = json.encodeToJsonElement(LibraryResolveSourcesBatchBody(refs = batch))

                val reply = try {
                    transport.send(
                        nodeId = libraryNode,
                        cmdType = "library.resolveSourcesBatch",
                        body = body,
                    )
                } catch (e: Exception) {
                    Log.e(tag, "resolveSourcesBatch failed: ${e.message}")
                    continue
                }

                if (!reply.ok || reply.body == null) continue

                val batchReply = json.decodeFromJsonElement<LibraryResolveSourcesBatchReply>(reply.body!!)
                for (item in batchReply.items) {
                    if (item.err != null) continue
                    val source = item.sources.firstOrNull() ?: continue
                    result[item.ref] = source
                }
            }
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
        val providerPriority = listOf("filesystem", "jellyfin")
        val preferred = providerPriority.firstNotNullOfOrNull { provider ->
            libraries.firstOrNull { it.nodeId.contains(provider) }
        }
        return (preferred ?: libraries.first()).nodeId
    }

    private fun parseBrowseReply(
        body: JsonElement,
        libraryNodeId: String,
        requestedCount: Long,
    ): BrowseResult {
        val obj = body.jsonObject
        val itemsArray = obj["items"]?.jsonArray ?: JsonArray(emptyList())
        val total = obj["total"]?.jsonPrimitive?.longOrNull ?: 0
        val start = obj["start"]?.jsonPrimitive?.longOrNull ?: 0
        val count = obj["count"]?.jsonPrimitive?.longOrNull ?: itemsArray.size.toLong()

        val items = itemsArray.map { element ->
            parseLibraryItem(element.jsonObject, libraryNodeId)
        }

        val hasMore = (start + count) < total

        return BrowseResult(items = items, hasMore = hasMore)
    }

    private fun parseLibraryItem(obj: JsonObject, libraryNodeId: String): BrowseItem {
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
        val isContainer = when {
            explicitLeafTypes.contains(typeLower) -> false
            explicitContainerTypes.contains(typeLower) -> true
            else -> containerPatterns.any { pattern ->
                typeLower.contains(pattern) || idLower.startsWith("$pattern:")
            }
        }

        val subtitle = when {
            artists.isNotEmpty() -> artists.joinToString(", ")
            overview.isNotEmpty() -> overview
            else -> ""
        }

        val metadata = buildMap<String, Any> {
            if (artists.isNotEmpty()) put("artist", artists.joinToString(", "))
            if (album.isNotEmpty()) put("album", album)
            if (durationMs > 0) put("durationMs", durationMs)
            if (mediaType.isNotEmpty()) put("mediaType", mediaType)
            if (imageUrl != null) put("artworkUrl", imageUrl)
        }

        val ref = if (!isContainer && itemId.isNotEmpty()) {
            LibraryItemRef(libraryId = libraryNodeId, itemId = itemId)
        } else null

        val display = if (!isContainer) {
            DisplayMetadata(
                title = name.ifEmpty { null },
                artist = artists.joinToString(", ").ifEmpty { null },
                artists = artists.takeIf { it.isNotEmpty() },
                album = album.ifEmpty { null },
                artworkUrl = imageUrl,
                durationMs = durationMs.takeIf { it > 0 },
                mediaType = mediaType.ifEmpty { null },
            )
        } else null

        if (ref != null && display != null) {
            metadataCache.put(ref, display)
        }

        return BrowseItem(
            id = itemId,
            type = type,
            title = name,
            subtitle = subtitle,
            artworkUrl = imageUrl,
            isContainer = isContainer,
            ref = ref,
            display = display,
            metadata = metadata,
        )
    }
}
