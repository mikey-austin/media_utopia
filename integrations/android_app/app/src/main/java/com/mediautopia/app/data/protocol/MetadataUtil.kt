package com.mediautopia.app.data.protocol

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

/**
 * Extension functions for extracting metadata from MU's metadata maps.
 *
 * Key differences from a naive approach:
 * - `artists` is a JSON array, not a single string
 * - `artworkUrl` is the artwork URL field
 */

/** Get a string value from a metadata map. */
fun Map<String, JsonElement>.stringValue(key: String): String? {
    val element = get(key) ?: return null
    return (element as? JsonPrimitive)?.contentOrNull
}

/**
 * Get the artist string from metadata.
 * Handles both "artists" (array) and "artist" (string) fields.
 * Joins array entries with ", ".
 */
fun Map<String, JsonElement>.artistString(): String? {
    // Try "artists" array first (the canonical MU format).
    val artistsElement = get("artists")
    if (artistsElement is JsonArray) {
        val parts = artistsElement.mapNotNull {
            (it as? JsonPrimitive)?.contentOrNull
        }
        if (parts.isNotEmpty()) return parts.joinToString(", ")
    }

    // Fall back to "artist" string.
    return stringValue("artist")
}

/** Get the artwork URL from metadata. */
fun Map<String, JsonElement>.artworkUrl(): String? {
    return stringValue("artworkUrl")
}

/** Get the album from metadata. */
fun Map<String, JsonElement>.album(): String? {
    return stringValue("album")
}

/** Get the title from metadata. */
fun Map<String, JsonElement>.title(): String? {
    return stringValue("title")
}

/** Get the format string (e.g. "FLAC"). */
fun Map<String, JsonElement>.format(): String? {
    return stringValue("format")
}

/** Get the media type. */
fun Map<String, JsonElement>.mediaType(): String? {
    return stringValue("mediaType") ?: stringValue("type")
}
