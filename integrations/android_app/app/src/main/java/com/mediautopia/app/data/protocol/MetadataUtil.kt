package com.mediautopia.app.data.protocol

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.longOrNull

/**
 * Resolve the artist string from a DisplayMetadata block, joining the
 * canonical `artists` array if present, otherwise falling back to the
 * scalar `artist` field.
 */
fun DisplayMetadata.artistString(): String? {
    val list = artists
    if (!list.isNullOrEmpty()) return list.joinToString(", ")
    return artist?.takeIf { it.isNotEmpty() }
}

/** Get a string value from a free-form attribute map. */
fun Map<String, JsonElement>.stringValue(key: String): String? {
    val element = get(key) ?: return null
    return (element as? JsonPrimitive)?.contentOrNull
}

/** Get an int value from a free-form attribute map. */
fun Map<String, JsonElement>.intValue(key: String): Int {
    return (get(key) as? JsonPrimitive)?.intOrNull ?: 0
}

/** Get a long value from a free-form attribute map. */
fun Map<String, JsonElement>.longValue(key: String): Long {
    return (get(key) as? JsonPrimitive)?.longOrNull ?: 0L
}

/**
 * Joined artist string from an attributes map, supporting either the
 * canonical `artists` array or a scalar `artist` field.
 */
fun Map<String, JsonElement>.artistString(): String? {
    val list = get("artists") as? JsonArray
    if (list != null) {
        val parts = list.mapNotNull { (it as? JsonPrimitive)?.contentOrNull }
        if (parts.isNotEmpty()) return parts.joinToString(", ")
    }
    return stringValue("artist")
}
