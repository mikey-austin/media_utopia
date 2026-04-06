package com.mediautopia.app.data.protocol

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

@Serializable
data class PresenceSource(
    val id: String,
    val name: String,
)

@Serializable
data class Presence(
    val nodeId: String,
    val kind: String,
    val name: String,
    val caps: Map<String, JsonElement>? = null,
    val endpoints: Map<String, JsonElement>? = null,
    val source: String? = null,
    val sources: List<PresenceSource>? = null,
    val ts: Long,
)
